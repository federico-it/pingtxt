package store

import (
	"encoding/json"
	"strings"
)

// PushDevice is a phone that gets notifications through the push relay. The
// handle is opaque (the relay encrypts the phone's FCM token in it) and
// belongs to the OAuth sign-in of the app that sent it.
type PushDevice struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"-"`
	GrantID    int64  `json:"-"`
	Handle     string `json:"-"`
	Name       string `json:"name"`
	CreatedAt  int64  `json:"created_at"`
	LastSeenAt int64  `json:"last_seen_at"`

	// Live Activity tokens (hex, from ActivityKit). StartToken starts one
	// remotely (iOS 17.2+) with HubURL in its attributes; Token updates and
	// ends the running one. LAStartedAt: last remote start, unix ms.
	LAStartToken string `json:"-"`
	LAHubURL     string `json:"-"`
	LAToken      string `json:"-"`
	LAStartedAt  int64  `json:"-"`

	// What the phone wants to hear about, set by its owner.
	PushPrefs
}

// Mute durations: MuteForever mutes until the owner turns it back on.
const MuteForever int64 = -1

// PushPrefs are the notification preferences of one phone. They only narrow
// down what the app channels send to it: a phone never gets an event no
// channel sends it.
type PushPrefs struct {
	// Events the phone gets (store.Event* names).
	Events []string `json:"events"`
	// Monitors the phone gets events of: every monitor, or those of the
	// groups (with their subgroups), tags and monitors listed. Agent events
	// are about no monitor and follow Events only.
	AllMonitors bool    `json:"all_monitors"`
	GroupIDs    []int64 `json:"group_ids"`
	TagIDs      []int64 `json:"tag_ids"`
	MonitorIDs  []int64 `json:"monitor_ids"`
	// MutedUntil mutes the phone (unix ms; 0 = not muted, MuteForever).
	MutedUntil int64 `json:"muted_until"`
	// MutedMonitors mutes single monitors on the phone.
	MutedMonitors []MonitorMute `json:"muted_monitors"`
}

// MonitorMute mutes one monitor on a phone until a time (unix ms, or MuteForever).
type MonitorMute struct {
	MonitorID int64 `json:"monitor_id"`
	Until     int64 `json:"until"`
}

// DefaultPushPrefs: every event of every monitor, not muted.
func DefaultPushPrefs() PushPrefs {
	return PushPrefs{Events: append([]string(nil), NotifyEvents...), AllMonitors: true,
		GroupIDs: []int64{}, TagIDs: []int64{}, MonitorIDs: []int64{}, MutedMonitors: []MonitorMute{}}
}

// Muted reports whether the phone is muted at t (unix ms).
func (p PushPrefs) Muted(t int64) bool { return mutedAt(p.MutedUntil, t) }

// MonitorMuted reports whether the monitor is muted on the phone at t.
func (p PushPrefs) MonitorMuted(id, t int64) bool {
	for _, m := range p.MutedMonitors {
		if m.MonitorID == id && mutedAt(m.Until, t) {
			return true
		}
	}
	return false
}

func mutedAt(until, t int64) bool { return until == MuteForever || until > t }

// Prune drops the mutes that ended before t.
func (p *PushPrefs) Prune(t int64) {
	if p.MutedUntil != MuteForever && p.MutedUntil <= t {
		p.MutedUntil = 0
	}
	out := []MonitorMute{}
	for _, m := range p.MutedMonitors {
		if mutedAt(m.Until, t) {
			out = append(out, m)
		}
	}
	p.MutedMonitors = out
}

func decodePushPrefs(s string) PushPrefs {
	p := DefaultPushPrefs()
	if s == "" {
		return p
	}
	_ = json.Unmarshal([]byte(s), &p)
	if p.Events == nil {
		p.Events = []string{}
	}
	for _, l := range []*[]int64{&p.GroupIDs, &p.TagIDs, &p.MonitorIDs} {
		if *l == nil {
			*l = []int64{}
		}
	}
	if p.MutedMonitors == nil {
		p.MutedMonitors = []MonitorMute{}
	}
	return p
}

const pushDeviceCols = `id, user_id, grant_id, handle, name, created_at, last_seen_at, la_start_token, la_hub_url, la_token, la_started_at, prefs`

func scanPushDevices(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
	Close() error
}) ([]*PushDevice, error) {
	defer rows.Close()
	out := []*PushDevice{}
	for rows.Next() {
		d := &PushDevice{}
		var prefs string
		if err := rows.Scan(&d.ID, &d.UserID, &d.GrantID, &d.Handle, &d.Name, &d.CreatedAt, &d.LastSeenAt,
			&d.LAStartToken, &d.LAHubURL, &d.LAToken, &d.LAStartedAt, &prefs); err != nil {
			return nil, err
		}
		d.PushPrefs = decodePushPrefs(prefs)
		out = append(out, d)
	}
	return out, rows.Err()
}

// SavePushDevice stores the handle of an app sign-in, replacing the one it
// sent before. It returns the device id.
func (s *Store) SavePushDevice(userID, grantID int64, handle, name string) (int64, error) {
	t := now()
	_, err := s.db.Exec(`INSERT INTO push_devices (user_id, grant_id, handle, name, created_at, last_seen_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(grant_id) DO UPDATE SET handle = excluded.handle, name = excluded.name, last_seen_at = excluded.last_seen_at`,
		userID, grantID, handle, name, t, t)
	if err != nil {
		return 0, err
	}
	var id int64
	err = s.db.QueryRow(`SELECT id FROM push_devices WHERE grant_id = ?`, grantID).Scan(&id)
	return id, err
}

// UserPushDevices lists the devices of a user, newest first.
func (s *Store) UserPushDevices(userID int64) ([]*PushDevice, error) {
	rows, err := s.db.Query(`SELECT `+pushDeviceCols+` FROM push_devices WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	return scanPushDevices(rows)
}

// PushDevicesFor lists the devices of the given users, or of every user when
// userIDs is nil.
func (s *Store) PushDevicesFor(userIDs []int64) ([]*PushDevice, error) {
	q := `SELECT ` + pushDeviceCols + ` FROM push_devices`
	args := []any{}
	if userIDs != nil {
		if len(userIDs) == 0 {
			return []*PushDevice{}, nil
		}
		q += ` WHERE user_id IN (?` + strings.Repeat(", ?", len(userIDs)-1) + `)`
		for _, id := range userIDs {
			args = append(args, id)
		}
	}
	rows, err := s.db.Query(q+` ORDER BY id`, args...)
	if err != nil {
		return nil, err
	}
	return scanPushDevices(rows)
}

// UserPushDevice returns a device of a user.
func (s *Store) UserPushDevice(userID, id int64) (*PushDevice, error) {
	rows, err := s.db.Query(`SELECT `+pushDeviceCols+` FROM push_devices WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return nil, err
	}
	list, err := scanPushDevices(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// UpdatePushDevice saves the name and preferences of a device of a user.
func (s *Store) UpdatePushDevice(userID int64, d *PushDevice) error {
	b, err := json.Marshal(d.PushPrefs)
	if err != nil {
		return err
	}
	res, err := s.db.Exec(`UPDATE push_devices SET name = ?, prefs = ? WHERE id = ? AND user_id = ?`, d.Name, string(b), d.ID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountPushDevices counts every registered device.
func (s *Store) CountPushDevices() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM push_devices`).Scan(&n)
	return n, err
}

// DeletePushDevice removes a device of a user.
func (s *Store) DeletePushDevice(userID, id int64) error {
	res, err := s.db.Exec(`DELETE FROM push_devices WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeletePushDeviceByID removes a device the relay reported gone. The handle
// must still match: the app may have sent a new one in the meantime.
func (s *Store) DeletePushDeviceByID(id int64, handle string) error {
	_, err := s.db.Exec(`DELETE FROM push_devices WHERE id = ? AND handle = ?`, id, handle)
	return err
}

// DeleteAllPushDevices removes every device (the hub registered again with
// the relay, so the old handles are useless).
func (s *Store) DeleteAllPushDevices() error {
	_, err := s.db.Exec(`DELETE FROM push_devices`)
	return err
}

// PushDeviceByGrant returns the device of an app sign-in.
func (s *Store) PushDeviceByGrant(grantID int64) (*PushDevice, error) {
	rows, err := s.db.Query(`SELECT `+pushDeviceCols+` FROM push_devices WHERE grant_id = ?`, grantID)
	if err != nil {
		return nil, err
	}
	list, err := scanPushDevices(rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, ErrNotFound
	}
	return list[0], nil
}

// SetLiveActivityStart stores the push-to-start token of a device and the
// hub URL the app knows ("" clears both).
func (s *Store) SetLiveActivityStart(id int64, token, hubURL string) error {
	_, err := s.db.Exec(`UPDATE push_devices SET la_start_token = ?, la_hub_url = ? WHERE id = ?`, token, hubURL, id)
	return err
}

// SetLiveActivityToken stores the token of the running activity of a device
// ("" when it ended).
func (s *Store) SetLiveActivityToken(id int64, token string) error {
	_, err := s.db.Exec(`UPDATE push_devices SET la_token = ? WHERE id = ?`, token, id)
	return err
}

// ClearLiveActivityToken forgets an activity token the relay reported gone,
// unless the app sent a new one in the meantime.
func (s *Store) ClearLiveActivityToken(id int64, token string) error {
	_, err := s.db.Exec(`UPDATE push_devices SET la_token = '' WHERE id = ? AND la_token = ?`, id, token)
	return err
}

// ClearLiveActivityStart forgets a push-to-start token the relay reported gone.
func (s *Store) ClearLiveActivityStart(id int64, token string) error {
	_, err := s.db.Exec(`UPDATE push_devices SET la_start_token = '' WHERE id = ? AND la_start_token = ?`, id, token)
	return err
}

// MarkLiveActivityStarted records a remote start of the activity of a device.
func (s *Store) MarkLiveActivityStarted(id, at int64) error {
	_, err := s.db.Exec(`UPDATE push_devices SET la_started_at = ? WHERE id = ?`, at, id)
	return err
}

// LiveActivityDevices lists the devices with a Live Activity token.
func (s *Store) LiveActivityDevices() ([]*PushDevice, error) {
	rows, err := s.db.Query(`SELECT ` + pushDeviceCols + ` FROM push_devices WHERE la_token <> '' OR la_start_token <> '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return scanPushDevices(rows)
}

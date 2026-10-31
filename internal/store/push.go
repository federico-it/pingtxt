package store

import "strings"

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
}

const pushDeviceCols = `id, user_id, grant_id, handle, name, created_at, last_seen_at`

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
		if err := rows.Scan(&d.ID, &d.UserID, &d.GrantID, &d.Handle, &d.Name, &d.CreatedAt, &d.LastSeenAt); err != nil {
			return nil, err
		}
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

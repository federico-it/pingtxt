package hub

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Each user picks what their phones hear about: the events, the monitors,
// mutes (everything, or single monitors) and optionally the one phone that
// gets the notifications. Each phone follows these account preferences or
// has events and monitors of its own, and can be muted by itself. The
// preferences are personal, so read-only users set them too; they only
// narrow down what the app channels send, and change nothing for other
// people or channels. Test notifications ignore them. They are set from the
// app only (no web UI, the owner's choice).

const maxMuteMinutes = 366 * 24 * 60

// ---------- delivery ----------

// pushFilter decides which phones get an event.
type pushFilter struct {
	n     *Notifier
	e     Event
	now   int64
	event string

	monitor  *store.Monitor // loaded on first use
	tree     *store.GroupTree
	loaded   bool
	accounts map[int64]store.AccountPushPrefs
}

func (f *pushFilter) load() {
	if f.loaded {
		return
	}
	f.loaded = true
	if f.e.MonitorID > 0 {
		if m, err := f.n.store.Monitor(f.e.MonitorID); err == nil {
			f.monitor = m
		}
	}
	if groups, err := f.n.store.ListGroups(); err == nil {
		f.tree = store.NewGroupTree(groups)
	}
}

// account returns the account preferences of a user (defaults on error).
func (f *pushFilter) account(userID int64) store.AccountPushPrefs {
	if p, ok := f.accounts[userID]; ok {
		return p
	}
	p, err := f.n.store.UserPushPrefs(userID)
	if err != nil {
		p = store.AccountPushPrefs{PushPrefs: store.DefaultPushPrefs()}
	}
	f.accounts[userID] = p
	return p
}

// wants reports whether the phone gets the event: neither the phone nor its
// owner's account muted it, and its events and monitors (the account's when
// it follows them) include it.
func (f *pushFilter) wants(d *store.PushDevice) bool {
	acc := f.account(d.UserID)
	for _, mutes := range []store.PushPrefs{acc.PushPrefs, d.PushPrefs} {
		if mutes.Muted(f.now) || (f.e.MonitorID > 0 && mutes.MonitorMuted(f.e.MonitorID, f.now)) {
			return false
		}
	}
	p := d.PushPrefs
	if d.FollowAccount {
		p = acc.PushPrefs
	}
	if !slices.Contains(p.Events, f.event) {
		return false
	}
	if f.e.MonitorID == 0 {
		return true // agent events
	}
	if p.AllMonitors || slices.Contains(p.MonitorIDs, f.e.MonitorID) {
		return true
	}
	f.load()
	m := f.monitor
	if m == nil {
		return false
	}
	groups := p.GroupIDs
	if f.tree != nil {
		groups = f.tree.Expand(groups)
	}
	if m.GroupID != 0 && slices.Contains(groups, m.GroupID) {
		return true
	}
	return slices.ContainsFunc(m.TagIDs, func(t int64) bool { return slices.Contains(p.TagIDs, t) })
}

// filterPushDevices keeps the phones whose preferences want the event. A
// user with a primary phone gets it on that phone only, as long as the
// phone is still signed in.
func (n *Notifier) filterPushDevices(e Event, devices []*store.PushDevice) []*store.PushDevice {
	if e.Test {
		return devices
	}
	f := &pushFilter{n: n, e: e, now: time.Now().UnixMilli(), event: e.notifyEvent(), accounts: map[int64]store.AccountPushPrefs{}}
	exists := map[int64]bool{}
	for _, d := range devices {
		exists[d.ID] = true
	}
	return slices.DeleteFunc(slices.Clone(devices), func(d *store.PushDevice) bool {
		if primary := f.account(d.UserID).PrimaryDeviceID; primary != 0 && primary != d.ID && exists[primary] {
			return true
		}
		return !f.wants(d)
	})
}

// ---------- API ----------

// mobileDevice is a phone as the API shows it: current marks the phone of
// the sign-in that asks.
type mobileDevice struct {
	*store.PushDevice
	Current bool `json:"current"`
}

func deviceView(d *store.PushDevice, tok *store.APIToken) mobileDevice {
	d.Prune(time.Now().UnixMilli())
	return mobileDevice{PushDevice: d, Current: tok != nil && tok.GrantID != 0 && tok.GrantID == d.GrantID}
}

// pathDevice loads the phone named by {id}: a device id, or "current" for
// the phone of the app sign-in that calls.
func (h *Hub) pathDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) (*store.PushDevice, bool) {
	var d *store.PushDevice
	var err error
	if r.PathValue("id") == "current" {
		if tok == nil || tok.GrantID == 0 {
			writeErr(w, 404, "device not found: only the app's sign-in has a current phone")
			return nil, false
		}
		d, err = h.store.PushDeviceByGrant(tok.GrantID)
		if err == nil && d.UserID != u.ID {
			err = store.ErrNotFound
		}
	} else {
		id, ok := pathID(r)
		if !ok {
			writeErr(w, 400, "invalid id")
			return nil, false
		}
		d, err = h.store.UserPushDevice(u.ID, id)
	}
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "device not found")
		return nil, false
	} else if err != nil {
		h.internalErr(w, err)
		return nil, false
	}
	return d, true
}

func (h *Hub) getMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	if d, ok := h.pathDevice(w, r, u, tok); ok {
		writeJSON(w, 200, deviceView(d, tok))
	}
}

// deviceBody is what PUT /api/mobile/devices/{id} changes.
type deviceBody struct {
	Name          string `json:"name"`
	FollowAccount bool   `json:"follow_account"`
	store.PushPrefs
}

// putMobileDevice changes the name and preferences of a phone; fields left
// out keep their value.
func (h *Hub) putMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	d, ok := h.pathDevice(w, r, u, tok)
	if !ok {
		return
	}
	cur := deviceBody{Name: d.Name, FollowAccount: d.FollowAccount, PushPrefs: d.PushPrefs}
	var body map[string]json.RawMessage
	if readJSON(r, &body) != nil || body == nil {
		writeErr(w, 400, "invalid body")
		return
	}
	// Choosing events or monitors for the phone stops it following the
	// account, unless the body says otherwise.
	if _, ok := body["follow_account"]; !ok {
		for _, k := range []string{"events", "all_monitors", "group_ids", "tag_ids", "monitor_ids"} {
			if _, ok := body[k]; ok {
				body["follow_account"] = json.RawMessage("false")
				break
			}
		}
	}
	var in deviceBody
	if mergeJSON(cur, body, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	d.FollowAccount = in.FollowAccount
	if name := truncateUTF8(strings.TrimSpace(in.Name), maxDeviceName); name != "" {
		d.Name = name
	}
	p, err := h.cleanPushPrefs(in.PushPrefs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	d.PushPrefs = p
	h.saveDevice(w, u, tok, d)
}

func (h *Hub) saveDevice(w http.ResponseWriter, u *store.User, tok *store.APIToken, d *store.PushDevice) {
	if err := h.store.UpdatePushDevice(u.ID, d); errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "device not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, deviceView(d, tok))
}

// cleanPushPrefs checks the preferences of a phone.
func (h *Hub) cleanPushPrefs(p store.PushPrefs) (store.PushPrefs, error) {
	t := time.Now().UnixMilli()
	p.Events = store.CleanEvents(p.Events)
	p.GroupIDs, p.TagIDs, p.MonitorIDs = cleanIDs(p.GroupIDs), cleanIDs(p.TagIDs), cleanIDs(p.MonitorIDs)
	if p.AllMonitors {
		p.GroupIDs, p.TagIDs, p.MonitorIDs = []int64{}, []int64{}, []int64{}
	}
	if err := h.checkGroupsTags(p.GroupIDs, p.TagIDs); err != nil {
		return p, err
	}
	for _, id := range p.MonitorIDs {
		if _, err := h.store.Monitor(id); err != nil {
			return p, errors.New("unknown monitor")
		}
	}
	if p.MutedUntil < store.MuteForever {
		return p, errors.New("muted_until must be 0, -1 or a time in unix ms")
	}
	mutes := []store.MonitorMute{}
	for _, m := range p.MutedMonitors {
		if m.Until < store.MuteForever || m.MonitorID <= 0 {
			return p, errors.New("muted_monitors: invalid entry")
		}
		if _, err := h.store.Monitor(m.MonitorID); err != nil {
			return p, errors.New("unknown monitor")
		}
		mutes = slices.DeleteFunc(mutes, func(o store.MonitorMute) bool { return o.MonitorID == m.MonitorID })
		mutes = append(mutes, m)
	}
	p.MutedMonitors = mutes
	p.Prune(t)
	return p, nil
}

// muteBody reads a mute request: a monitor (0 = everything) and minutes
// (0 or left out = until turned back on). It writes the error itself.
func (h *Hub) muteBody(w http.ResponseWriter, r *http.Request) (monitorID, until int64, ok bool) {
	var in struct {
		MonitorID int64 `json:"monitor_id"`
		Minutes   int64 `json:"minutes"`
	}
	if r.ContentLength != 0 && readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return 0, 0, false
	}
	if in.Minutes < 0 || in.Minutes > maxMuteMinutes {
		writeErr(w, 400, "minutes must be between 0 (until turned back on) and "+strconv.Itoa(maxMuteMinutes))
		return 0, 0, false
	}
	until = store.MuteForever
	if in.Minutes > 0 {
		until = time.Now().Add(time.Duration(in.Minutes) * time.Minute).UnixMilli()
	}
	if in.MonitorID != 0 {
		if _, err := h.store.Monitor(in.MonitorID); err != nil {
			writeErr(w, 404, "monitor not found")
			return 0, 0, false
		}
	}
	return in.MonitorID, until, true
}

// unmuteQuery reads ?monitor_id= of an unmute (0 = the mute of everything).
func unmuteQuery(w http.ResponseWriter, r *http.Request) (int64, bool) {
	s := r.URL.Query().Get("monitor_id")
	if s == "" {
		return 0, true
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		writeErr(w, 400, "invalid monitor_id")
		return 0, false
	}
	return v, true
}

// setMute mutes (until != 0) or unmutes (until == 0) a monitor, or
// everything when monitorID is 0.
func setMute(p *store.PushPrefs, monitorID, until int64) {
	if monitorID == 0 {
		p.MutedUntil = until
	} else {
		p.MutedMonitors = slices.DeleteFunc(p.MutedMonitors, func(m store.MonitorMute) bool { return m.MonitorID == monitorID })
		if until != 0 {
			p.MutedMonitors = append(p.MutedMonitors, store.MonitorMute{MonitorID: monitorID, Until: until})
		}
	}
	p.Prune(time.Now().UnixMilli())
}

// muteMobileDevice mutes a phone, or one monitor on it.
func (h *Hub) muteMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	d, ok := h.pathDevice(w, r, u, tok)
	if !ok {
		return
	}
	mid, until, ok := h.muteBody(w, r)
	if !ok {
		return
	}
	setMute(&d.PushPrefs, mid, until)
	h.saveDevice(w, u, tok, d)
}

// unmuteMobileDevice ends the mute of a phone, or of one monitor on it.
func (h *Hub) unmuteMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	d, ok := h.pathDevice(w, r, u, tok)
	if !ok {
		return
	}
	mid, ok := unmuteQuery(w, r)
	if !ok {
		return
	}
	setMute(&d.PushPrefs, mid, 0)
	h.saveDevice(w, u, tok, d)
}

// ---------- account preferences ----------

func (h *Hub) accountPrefs(w http.ResponseWriter, u *store.User) (store.AccountPushPrefs, bool) {
	p, err := h.store.UserPushPrefs(u.ID)
	if err != nil {
		h.internalErr(w, err)
		return p, false
	}
	p.Prune(time.Now().UnixMilli())
	return p, true
}

func (h *Hub) saveAccountPrefs(w http.ResponseWriter, u *store.User, p store.AccountPushPrefs) {
	if err := h.store.SetUserPushPrefs(u.ID, p); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, p)
}

// getMobilePrefs returns the user's preferences for all their phones.
func (h *Hub) getMobilePrefs(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	if p, ok := h.accountPrefs(w, u); ok {
		writeJSON(w, 200, p)
	}
}

// putMobilePrefs changes them; fields left out keep their value.
func (h *Hub) putMobilePrefs(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	cur, ok := h.accountPrefs(w, u)
	if !ok {
		return
	}
	var in store.AccountPushPrefs
	if readJSONOver(r, cur, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	p, err := h.cleanPushPrefs(in.PushPrefs)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if in.PrimaryDeviceID < 0 {
		in.PrimaryDeviceID = 0
	}
	if in.PrimaryDeviceID != 0 && in.PrimaryDeviceID != cur.PrimaryDeviceID {
		if _, err := h.store.UserPushDevice(u.ID, in.PrimaryDeviceID); err != nil {
			writeErr(w, 400, "primary_device_id must be one of your phones")
			return
		}
	}
	h.saveAccountPrefs(w, u, store.AccountPushPrefs{PushPrefs: p, PrimaryDeviceID: in.PrimaryDeviceID})
}

// muteMobilePrefs mutes every phone of the user, or one monitor on all of them.
func (h *Hub) muteMobilePrefs(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	p, ok := h.accountPrefs(w, u)
	if !ok {
		return
	}
	mid, until, ok := h.muteBody(w, r)
	if !ok {
		return
	}
	setMute(&p.PushPrefs, mid, until)
	h.saveAccountPrefs(w, u, p)
}

// unmuteMobilePrefs ends a mute of the user's account.
func (h *Hub) unmuteMobilePrefs(w http.ResponseWriter, r *http.Request, u *store.User, _ *store.APIToken) {
	p, ok := h.accountPrefs(w, u)
	if !ok {
		return
	}
	mid, ok := unmuteQuery(w, r)
	if !ok {
		return
	}
	setMute(&p.PushPrefs, mid, 0)
	h.saveAccountPrefs(w, u, p)
}

package hub

import (
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Each phone signed in to the app picks what it hears about: the events,
// the monitors, and mutes (the whole phone, or single monitors). The
// preferences belong to the phone's owner, so read-only users set them too;
// they only narrow down what the app channels send, and change nothing for
// other people or channels. Test notifications ignore them.

const maxMuteMinutes = 366 * 24 * 60

// ---------- delivery ----------

// pushFilter decides which phones get an event.
type pushFilter struct {
	n     *Notifier
	e     Event
	now   int64
	event string

	monitor *store.Monitor // loaded on first use
	tree    *store.GroupTree
	loaded  bool
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

// wants reports whether the phone gets the event.
func (f *pushFilter) wants(d *store.PushDevice) bool {
	p := d.PushPrefs
	if p.Muted(f.now) || !slices.Contains(p.Events, f.event) {
		return false
	}
	if f.e.MonitorID == 0 {
		return true // agent events
	}
	if p.MonitorMuted(f.e.MonitorID, f.now) {
		return false
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

// filterPushDevices keeps the phones whose preferences want the event.
func (n *Notifier) filterPushDevices(e Event, devices []*store.PushDevice) []*store.PushDevice {
	if e.Test {
		return devices
	}
	f := &pushFilter{n: n, e: e, now: time.Now().UnixMilli(), event: e.notifyEvent()}
	return slices.DeleteFunc(slices.Clone(devices), func(d *store.PushDevice) bool { return !f.wants(d) })
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
	Name string `json:"name"`
	store.PushPrefs
}

// putMobileDevice changes the name and preferences of a phone; fields left
// out keep their value.
func (h *Hub) putMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	d, ok := h.pathDevice(w, r, u, tok)
	if !ok {
		return
	}
	cur := deviceBody{Name: d.Name, PushPrefs: d.PushPrefs}
	var in deviceBody
	if readJSONOver(r, cur, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
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

// muteMobileDevice mutes a phone, or one monitor on it, for some minutes
// (0 or left out = until turned back on).
func (h *Hub) muteMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	d, ok := h.pathDevice(w, r, u, tok)
	if !ok {
		return
	}
	var in struct {
		MonitorID int64 `json:"monitor_id"`
		Minutes   int64 `json:"minutes"`
	}
	if r.ContentLength != 0 && readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if in.Minutes < 0 || in.Minutes > maxMuteMinutes {
		writeErr(w, 400, "minutes must be between 0 (until turned back on) and "+strconv.Itoa(maxMuteMinutes))
		return
	}
	until := store.MuteForever
	if in.Minutes > 0 {
		until = time.Now().Add(time.Duration(in.Minutes) * time.Minute).UnixMilli()
	}
	if in.MonitorID == 0 {
		d.MutedUntil = until
	} else {
		if _, err := h.store.Monitor(in.MonitorID); err != nil {
			writeErr(w, 404, "monitor not found")
			return
		}
		d.MutedMonitors = slices.DeleteFunc(d.MutedMonitors, func(m store.MonitorMute) bool { return m.MonitorID == in.MonitorID })
		d.MutedMonitors = append(d.MutedMonitors, store.MonitorMute{MonitorID: in.MonitorID, Until: until})
	}
	d.Prune(time.Now().UnixMilli())
	h.saveDevice(w, u, tok, d)
}

// unmuteMobileDevice ends the mute of a phone, or of one monitor on it
// (?monitor_id=).
func (h *Hub) unmuteMobileDevice(w http.ResponseWriter, r *http.Request, u *store.User, tok *store.APIToken) {
	d, ok := h.pathDevice(w, r, u, tok)
	if !ok {
		return
	}
	var mid int64
	if s := r.URL.Query().Get("monitor_id"); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil || v <= 0 {
			writeErr(w, 400, "invalid monitor_id")
			return
		}
		mid = v
	}
	if mid == 0 {
		d.MutedUntil = 0
	} else {
		d.MutedMonitors = slices.DeleteFunc(d.MutedMonitors, func(m store.MonitorMute) bool { return m.MonitorID == mid })
	}
	d.Prune(time.Now().UnixMilli())
	h.saveDevice(w, u, tok, d)
}

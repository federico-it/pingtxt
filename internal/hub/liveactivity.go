package hub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Live Activity of the iOS app: one per hub on each phone, listing the open
// incidents. The app gives the hub two ActivityKit tokens: the push-to-start
// token (iOS 17.2+), to start the activity with the app closed, and the
// token of the running activity, to update and end it. Whenever the open
// incidents change, the hub sends the new state through the relay
// (POST /v1/live-activity); while incidents stay open it refreshes the
// activity every laRefresh so it does not show as out of date.
//
// The ContentState sent is the app's OutageActivityAttributes.ContentState,
// encoded as Swift's default JSONEncoder does (dates are seconds since
// 2001-01-01). See /mnt/project-files/ios-live-activity/SPEC.md.

const (
	laAttributesType = "OutageActivityAttributes"
	laMaxShown       = 3                // outages listed, as the app's ContentState.maxShown
	laStale          = 30 * time.Minute // the activity shows "not updated" after this
	laRefresh        = 15 * time.Minute // refresh while incidents are open
	laRestartAfter   = 10 * time.Minute // no second remote start before the app sent the new token
	laDebounce       = 2 * time.Second  // several monitors failing together: one push
	laEndShown       = 15 * time.Minute // the ended activity stays on the lock screen with the summary
	laMaxToken       = 1024
	laMaxHubURL      = 1024
	laMaxName        = 100
	laMaxCause       = 200

	// Seconds between 1970-01-01 and 2001-01-01, Foundation's reference date.
	appleEpoch = 978307200
)

// liveActivities is the state of the Live Activity loop.
type liveActivities struct {
	wake chan struct{}

	mu       sync.Mutex
	ready    bool           // the loop loaded the incidents open at startup
	last     string         // content state last sent
	lastIDs  map[int64]bool // incident ids last sent
	lastSent time.Time
	// episode lists every incident open since the activities started, for
	// the summary sent when they end.
	episode map[int64]bool
}

// laOutage is HubStatus.Outage of the app.
type laOutage struct {
	ID          int64   `json:"id"`
	MonitorID   int64   `json:"monitorID"`
	MonitorName string  `json:"monitorName"`
	Since       float64 `json:"since"` // seconds since 2001-01-01
	Cause       string  `json:"cause"`
	// AckedBy is who acknowledged the incident (email), omitted when nobody
	// did. Older apps ignore it.
	AckedBy string `json:"ackedBy,omitempty"`
	// External is true on status page monitors: the outage is on a third
	// party's page, not a failure of a checked service. Older apps ignore it.
	External bool `json:"external,omitempty"`
}

// laState is OutageActivityAttributes.ContentState of the app.
type laState struct {
	Outages []laOutage `json:"outages"`
	Count   int        `json:"count"`
}

type laTarget struct {
	Device string `json:"device"`
	Token  string `json:"token"`
}

// laMessage is the body of POST /v1/live-activity.
type laMessage struct {
	Event          string          `json:"event"`
	Targets        []laTarget      `json:"targets"`
	ContentState   json.RawMessage `json:"content_state"`
	AttributesType string          `json:"attributes_type,omitempty"`
	Attributes     json.RawMessage `json:"attributes,omitempty"`
	Timestamp      int64           `json:"timestamp"`
	StaleDate      int64           `json:"stale_date,omitempty"`
	DismissalDate  int64           `json:"dismissal_date,omitempty"`
	Title          string          `json:"title,omitempty"`
	Body           string          `json:"body,omitempty"`
	Priority       string          `json:"priority"`
	TTL            int             `json:"ttl,omitempty"`
}

func appleTime(ms int64) float64 { return float64(ms)/1000 - appleEpoch }

// buildLAState turns the open incidents (oldest first) into the content
// state. mons (by id, may miss some) gives the short cause, see shortCause.
func buildLAState(open []*store.Incident, mons map[int64]*store.Monitor) laState {
	st := laState{Outages: []laOutage{}, Count: len(open)}
	for _, i := range open[:min(len(open), laMaxShown)] {
		m := mons[i.MonitorID]
		st.Outages = append(st.Outages, laOutage{
			ID: i.ID, MonitorID: i.MonitorID, MonitorName: truncateUTF8(i.MonitorName, laMaxName),
			Since: appleTime(i.StartedAt), Cause: truncateUTF8(monitorShortCause(i.Cause, m), laMaxCause),
			AckedBy:  truncateUTF8(i.AckedBy, laMaxName),
			External: m != nil && m.Type == check.TypeStatuspage,
		})
	}
	return st
}

// laMonitors loads the monitors of the incidents shown on the activity.
func (h *Hub) laMonitors(open []*store.Incident) map[int64]*store.Monitor {
	mons := map[int64]*store.Monitor{}
	for _, i := range open[:min(len(open), laMaxShown)] {
		if m, err := h.store.Monitor(i.MonitorID); err == nil && m != nil {
			mons[i.MonitorID] = m
		}
	}
	return mons
}

// LiveActivitiesChanged asks the loop to look at the open incidents again.
func (h *Hub) LiveActivitiesChanged() {
	if h.la.wake == nil {
		return
	}
	select {
	case h.la.wake <- struct{}{}:
	default:
	}
}

func (h *Hub) liveActivityLoop(ctx context.Context) {
	h.syncLiveActivities(ctx, time.Now())
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.la.wake:
			select {
			case <-ctx.Done():
				return
			case <-time.After(h.laDebounce()):
			}
			// Wakes during the wait are served by this run.
			select {
			case <-h.la.wake:
			default:
			}
		case <-t.C:
		}
		h.syncLiveActivities(ctx, time.Now())
	}
}

func (h *Hub) laDebounce() time.Duration {
	if h.laWait > 0 {
		return h.laWait
	}
	return laDebounce
}

// syncLiveActivities sends the open incidents to the phones when they
// changed, or refreshes the activities while incidents stay open.
func (h *Hub) syncLiveActivities(ctx context.Context, now time.Time) {
	h.la.mu.Lock()
	defer h.la.mu.Unlock()
	open, err := h.store.OpenIncidents()
	if err != nil {
		h.log.Error("live activity: open incidents", "err", err)
		return
	}
	mons := h.laMonitors(open)
	state := buildLAState(open, mons)
	raw := mustJSON(state)
	ids := map[int64]bool{}
	newIncident := false
	for _, i := range open {
		ids[i.ID] = true
		if !h.la.lastIDs[i.ID] {
			newIncident = true
		}
	}
	if h.la.episode == nil {
		h.la.episode = map[int64]bool{}
	}
	for id := range ids {
		h.la.episode[id] = true
	}
	if !h.la.ready {
		// Startup: the phones already have this state (or will load it).
		h.la.ready, h.la.last, h.la.lastIDs, h.la.lastSent = true, string(raw), ids, now
		return
	}
	changed := string(raw) != h.la.last
	refresh := len(open) > 0 && now.Sub(h.la.lastSent) >= laRefresh
	if !changed && !refresh {
		return
	}
	h.la.last, h.la.lastIDs, h.la.lastSent = string(raw), ids, now

	ps, err := h.notify.pushSettings()
	if err != nil || !ps.ready() {
		return
	}
	devices, err := h.store.LiveActivityDevices()
	if err != nil {
		h.log.Error("live activity: devices", "err", err)
		return
	}
	if len(devices) == 0 {
		return
	}
	var running, idle []*store.PushDevice
	for _, d := range devices {
		if d.LAToken != "" {
			running = append(running, d)
		} else if d.LAStartToken != "" && d.LAHubURL != "" {
			idle = append(idle, d)
		}
	}
	base := laMessage{ContentState: raw, Timestamp: now.Unix(), Priority: "high"}
	if len(open) == 0 {
		// Every incident closed: end the activities with a summary alert.
		// The ended activity stays on the lock screen for laEndShown.
		episode := h.la.episode
		h.la.episode = nil
		base.Event, base.DismissalDate = "end", now.Add(laEndShown).Unix()
		base.Title, base.Body = h.laSummary(episode)
		h.sendLiveActivity(ctx, ps, base, running, false)
		for _, d := range running {
			if err := h.store.ClearLiveActivityToken(d.ID, d.LAToken); err != nil {
				h.log.Error("live activity: clear token", "err", err)
			}
		}
		return
	}
	base.StaleDate = now.Add(laStale).Unix()
	upd := base
	upd.Event = "update"
	if !changed {
		upd.Priority = "normal" // a refresh: no hurry, and no budget spent
	}
	upd.TTL = int(laStale / time.Second)
	gone := h.sendLiveActivity(ctx, ps, upd, running, false)

	if !newIncident {
		return
	}
	// An activity the relay reported gone (dismissed on the phone while the
	// app was suspended) does not stop a new start.
	for _, d := range gone {
		if d.LAStartToken != "" && d.LAHubURL != "" {
			idle = append(idle, d)
		}
	}
	// A new incident: start the activity on the phones that have none.
	byHub := map[string][]*store.PushDevice{}
	hubs := []string{}
	for _, d := range idle {
		if now.UnixMilli()-d.LAStartedAt < laRestartAfter.Milliseconds() {
			continue // started a moment ago, the app has not sent its token yet
		}
		if _, ok := byHub[d.LAHubURL]; !ok {
			hubs = append(hubs, d.LAHubURL)
		}
		byHub[d.LAHubURL] = append(byHub[d.LAHubURL], d)
	}
	start := base
	start.Event, start.AttributesType = "start", laAttributesType
	start.TTL = int(laStale / time.Second)
	start.Title, start.Body = laAlert(open, mons)
	for _, hub := range hubs {
		start.Attributes = mustJSON(map[string]string{"hub": hub})
		list := byHub[hub]
		for _, d := range list {
			if err := h.store.MarkLiveActivityStarted(d.ID, now.UnixMilli()); err != nil {
				h.log.Error("live activity: mark started", "err", err)
			}
		}
		h.sendLiveActivity(ctx, ps, start, list, true)
	}
}

// laAlert is the alert iOS shows when the hub starts the activity (it asks
// for one). The normal notification rings already, so it has no sound.
func laAlert(open []*store.Incident, mons map[int64]*store.Monitor) (title, body string) {
	if len(open) == 1 {
		return truncateUTF8(open[0].MonitorName+" is down", maxPushTitle), truncateUTF8(monitorShortCause(open[0].Cause, mons[open[0].MonitorID]), maxPushBody)
	}
	names := []string{}
	for _, i := range open[:min(len(open), laMaxShown)] {
		names = append(names, i.MonitorName)
	}
	if len(open) > laMaxShown {
		names = append(names, fmt.Sprintf("+%d", len(open)-laMaxShown))
	}
	return fmt.Sprintf("%d monitors are down", len(open)), truncateUTF8(strings.Join(names, ", "), maxPushBody)
}

// laSummary is the alert of the end: which monitors were down and for how
// long. Incidents of deleted monitors are left out.
func (h *Hub) laSummary(episode map[int64]bool) (title, body string) {
	var list []*store.Incident
	for id := range episode {
		i, err := h.store.Incident(id)
		if err != nil || i.ResolvedAt == 0 {
			continue
		}
		list = append(list, i)
	}
	return laSummaryText(list)
}

func laSummaryText(list []*store.Incident) (title, body string) {
	sort.Slice(list, func(a, b int) bool { return list[a].StartedAt < list[b].StartedAt })
	down := func(i *store.Incident) string {
		return fmtDuration(max(time.Duration(i.ResolvedAt-i.StartedAt)*time.Millisecond, time.Minute))
	}
	switch len(list) {
	case 0:
		return "All monitors are up", ""
	case 1:
		return truncateUTF8(list[0].MonitorName+" is up", maxPushTitle), "Down for " + down(list[0])
	}
	parts := []string{}
	for _, i := range list[:min(len(list), laMaxShown)] {
		parts = append(parts, i.MonitorName+" "+down(i))
	}
	if len(list) > laMaxShown {
		parts = append(parts, fmt.Sprintf("+%d", len(list)-laMaxShown))
	}
	return "All monitors are up", truncateUTF8("Down: "+strings.Join(parts, ", "), maxPushBody)
}

// sendLiveActivity sends one event to the devices, in batches, and applies
// what the relay reports: gone tokens are forgotten, gone handles deleted.
// It returns the devices whose token was gone. No retries: the next change
// or refresh sends the state again.
func (h *Hub) sendLiveActivity(ctx context.Context, ps PushSettings, msg laMessage, devices []*store.PushDevice, start bool) []*store.PushDevice {
	var gone []*store.PushDevice
	for i := 0; i < len(devices); i += relayBatch {
		batch := devices[i:min(i+relayBatch, len(devices))]
		msg.Targets = make([]laTarget, 0, len(batch))
		sent := make([]*store.PushDevice, 0, len(batch))
		seen := map[string]bool{}
		for _, d := range batch {
			tok := d.LAToken
			if start {
				tok = d.LAStartToken
			}
			if seen[tok] {
				continue
			}
			seen[tok] = true
			msg.Targets = append(msg.Targets, laTarget{Device: d.Handle, Token: tok})
			sent = append(sent, d)
		}
		var out struct {
			Results []pushResult `json:"results"`
		}
		if err := h.notify.relayCall(ctx, http.MethodPost, ps.RelayURL, "/v1/live-activity", ps.HubToken, msg, &out); err != nil {
			h.log.Warn("live activity push failed", "event", msg.Event, "err", err)
			continue
		}
		for j, r := range out.Results {
			if j >= len(sent) {
				break
			}
			d := sent[j]
			var err error
			switch r.Status {
			case "gone":
				gone = append(gone, d)
				if start {
					err = h.store.ClearLiveActivityStart(d.ID, d.LAStartToken)
				} else {
					err = h.store.ClearLiveActivityToken(d.ID, d.LAToken)
				}
			case "device_gone":
				err = h.store.DeletePushDeviceByID(d.ID, d.Handle)
			case "sent":
			default:
				h.log.Warn("live activity push", "event", msg.Event, "status", r.Status, "err", r.Error)
			}
			if err != nil {
				h.log.Error("live activity: apply relay result", "err", err)
			}
		}
	}
	return gone
}

// ---------- API for the app ----------

// validLAToken checks an ActivityKit token: hex.
func validLAToken(s string) bool {
	if len(s) < 16 || len(s) > laMaxToken {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

// laDevice finds the phone of the app's sign-in, registered with
// POST /api/mobile/devices first.
func (h *Hub) laDevice(w http.ResponseWriter, tok *store.APIToken) (*store.PushDevice, bool) {
	if tok == nil || tok.GrantID == 0 {
		writeErr(w, 403, "only the pingtxt app can register a Live Activity (sign in with OAuth)")
		return nil, false
	}
	d, err := h.store.PushDeviceByGrant(tok.GrantID)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 409, "register this phone first (POST /api/mobile/devices)")
		return nil, false
	}
	if err != nil {
		h.internalErr(w, err)
		return nil, false
	}
	return d, true
}

// putLAStartToken stores the push-to-start token of the phone and the hub
// URL the app signed in with (it goes in the attributes of a remote start).
func (h *Hub) putLAStartToken(w http.ResponseWriter, r *http.Request, _ *store.User, tok *store.APIToken) {
	d, ok := h.laDevice(w, tok)
	if !ok {
		return
	}
	var in struct {
		Token  string `json:"token"`
		HubURL string `json:"hub_url"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	in.Token = strings.TrimSpace(in.Token)
	if !validLAToken(in.Token) {
		writeErr(w, 400, "token must be the hex push-to-start token from ActivityKit")
		return
	}
	u, err := url.Parse(strings.TrimSpace(in.HubURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || len(in.HubURL) > laMaxHubURL {
		writeErr(w, 400, "hub_url must be the URL of this hub as the app knows it")
		return
	}
	if err := h.store.SetLiveActivityStart(d.ID, in.Token, u.String()); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) deleteLAStartToken(w http.ResponseWriter, r *http.Request, _ *store.User, tok *store.APIToken) {
	d, ok := h.laDevice(w, tok)
	if !ok {
		return
	}
	if err := h.store.SetLiveActivityStart(d.ID, "", ""); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// putLAToken stores the token of the activity running on the phone.
func (h *Hub) putLAToken(w http.ResponseWriter, r *http.Request, _ *store.User, tok *store.APIToken) {
	d, ok := h.laDevice(w, tok)
	if !ok {
		return
	}
	var in struct {
		Token string `json:"token"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	in.Token = strings.TrimSpace(in.Token)
	if !validLAToken(in.Token) {
		writeErr(w, 400, "token must be the hex push token of the activity from ActivityKit")
		return
	}
	if err := h.store.SetLiveActivityToken(d.ID, in.Token); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) deleteLAToken(w http.ResponseWriter, r *http.Request, _ *store.User, tok *store.APIToken) {
	d, ok := h.laDevice(w, tok)
	if !ok {
		return
	}
	if err := h.store.SetLiveActivityToken(d.ID, ""); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

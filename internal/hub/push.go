package hub

import (
	"crypto/rand"
	"encoding/base32"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// PushLocation is the pseudo-location that holds heartbeat results: push
// monitors are not probed from anywhere, the monitored job calls the hub.
const PushLocation = "push"

const pushLocationName = "heartbeat"

// Push monitors accept periods up to a week, so weekly cron jobs fit.
const (
	maxPushIntervalSec = 7 * 24 * 3600
	maxPushGraceSec    = 24 * 3600
	maxPushMessage     = 200
)

// minPingGap drops pings that arrive faster than this, so a job stuck in a
// loop cannot flood the database.
const minPingGap = time.Second

// newPushToken returns a random token for a push URL.
func newPushToken() string {
	b := make([]byte, 15)
	_, _ = rand.Read(b)
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b))
}

// pushWindow is how long a push monitor waits for a heartbeat.
func pushWindow(m *store.Monitor) time.Duration {
	return time.Duration(max(m.IntervalSec, 10)+max(m.GraceSec, 0)) * time.Second
}

// eventTarget is the target shown in notifications.
func eventTarget(m *store.Monitor) string {
	if m.Type == check.TypePush {
		return pushLocationName
	}
	return check.RedactTarget(m.Target)
}

// missedHeartbeat builds the failed result recorded when a push monitor's
// deadline passes. While no heartbeat arrives one is recorded every period.
// Called with h.mu held.
func missedHeartbeat(st *monState, now time.Time) check.Result {
	st.next = now.Add(time.Duration(max(st.m.IntervalSec, 10)) * time.Second)
	msg := "no heartbeat received yet"
	if !st.lastPing.IsZero() {
		msg = "no heartbeat for " + now.Sub(st.lastPing).Round(time.Second).String()
	}
	return check.Result{MonitorID: st.m.ID, OK: false, Message: msg, At: now.UnixMilli()}
}

// Ping outcomes.
var (
	errPushNotFound = errors.New("unknown push token")
	errPushPaused   = errors.New("monitor is paused")
)

// HandlePush records a heartbeat for the push monitor owning token.
func (h *Hub) HandlePush(token string, ok bool, msg string, latencyMs float64) error {
	m, err := h.store.MonitorByPushToken(token)
	if err != nil {
		return errPushNotFound
	}
	now := time.Now()
	h.mu.Lock()
	st, found := h.monitors[m.ID]
	if !found || st.m.Type != check.TypePush {
		h.mu.Unlock()
		return errPushNotFound
	}
	if st.m.Paused {
		h.mu.Unlock()
		return errPushPaused
	}
	if !st.lastPing.IsZero() && now.Sub(st.lastPing) < minPingGap {
		h.mu.Unlock()
		return nil
	}
	st.lastPing = now
	st.next = now.Add(pushWindow(st.m))
	h.mu.Unlock()

	if msg == "" {
		msg = "heartbeat received"
		if !ok {
			msg = "job reported a failure"
		}
	}
	h.HandleResult(PushLocation, check.Result{MonitorID: m.ID, OK: ok, LatencyMs: latencyMs, Message: msg, At: now.UnixMilli()})
	return nil
}

// push serves /api/push/{token}. Any method works, so a cron job can call
// it with a bare `curl`. Query parameters (all optional):
//
//	status=up|down  report a failed run with status=down (default up)
//	msg=...         short message stored with the heartbeat
//	ping=123        duration of the run in ms, shown as latency
func (h *Hub) push(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	ok := true
	switch strings.ToLower(q.Get("status")) {
	case "", "up", "ok", "success", "0":
	case "down", "fail", "failure", "error", "1":
		ok = false
	default:
		writeErr(w, 400, "status must be up or down")
		return
	}
	msg := strings.TrimSpace(q.Get("msg"))
	if len(msg) > maxPushMessage {
		msg = msg[:maxPushMessage]
	}
	latency, _ := strconv.ParseFloat(q.Get("ping"), 64)
	if latency < 0 || latency > 1e9 {
		latency = 0
	}
	switch err := h.HandlePush(r.PathValue("token"), ok, msg, latency); {
	case errors.Is(err, errPushNotFound):
		writeErr(w, 404, "unknown push token")
	case errors.Is(err, errPushPaused):
		writeJSON(w, 200, map[string]any{"ok": true, "paused": true})
	default:
		writeJSON(w, 200, map[string]any{"ok": true})
	}
}

// rotatePushToken gives a push monitor a new URL; the old one stops working.
func (h *Hub) rotatePushToken(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	m, err := h.store.Monitor(id)
	if err != nil {
		writeErr(w, 404, "monitor not found")
		return
	}
	if m.Type != check.TypePush {
		writeErr(w, 400, "not a push monitor")
		return
	}
	if err := h.store.SetPushToken(id, newPushToken()); err != nil {
		h.internalErr(w, err)
		return
	}
	h.MonitorChanged(id)
	got, _ := h.store.Monitor(id)
	writeJSON(w, 200, got)
}

// pushURL is the public heartbeat URL of a push monitor.
func (h *Hub) pushURL(r *http.Request, m *store.Monitor) string {
	if m.Type != check.TypePush || m.PushToken == "" {
		return ""
	}
	return fmt.Sprintf("%s/api/push/%s", strings.TrimRight(h.hubURL(r), "/"), m.PushToken)
}

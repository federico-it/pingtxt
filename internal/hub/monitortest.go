package hub

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/protocol"
	"github.com/federico-it/pingtxt/internal/store"
)

// Test checks run a monitor once at each of its locations and return the
// results without storing them: no history, no status change, no alert.

// testJobPrefix marks the job ids of test checks, so the result an agent
// sends back goes to the waiting request instead of the monitor.
const testJobPrefix = "test-"

// testResult is the outcome of a test check at one location.
type testResult struct {
	Location   string  `json:"location"`
	Name       string  `json:"name"`
	OK         bool    `json:"ok"`
	Skipped    bool    `json:"skipped,omitempty"` // location offline or turned off
	LatencyMs  float64 `json:"latency_ms"`
	StatusCode int     `json:"status_code,omitempty"`
	Message    string  `json:"message,omitempty"`
}

// testChecks holds the test checks waiting for an agent's result.
type testChecks struct {
	mu      sync.Mutex
	waiting map[string]chan check.Result
}

func (t *testChecks) add(id string) chan check.Result {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.waiting == nil {
		t.waiting = map[string]chan check.Result{}
	}
	ch := make(chan check.Result, 1)
	t.waiting[id] = ch
	return ch
}

func (t *testChecks) remove(id string) {
	t.mu.Lock()
	delete(t.waiting, id)
	t.mu.Unlock()
}

// deliver hands a test result to its request. It reports whether the result
// belonged to a test check (even one that already timed out).
func (t *testChecks) deliver(r check.Result) bool {
	if !strings.HasPrefix(r.JobID, testJobPrefix) {
		return false
	}
	t.mu.Lock()
	ch := t.waiting[r.JobID]
	t.mu.Unlock()
	if ch != nil {
		select {
		case ch <- r:
		default:
		}
	}
	return true
}

// testDraftMonitor checks an unsaved monitor (the form's current values).
func (h *Hub) testDraftMonitor(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var m store.Monitor
	if readJSON(r, &m) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if m.Name == "" {
		m.Name = "test" // the name is not needed to run a check
	}
	m.ID = 0
	if err := h.validateMonitor(&m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	h.writeTest(w, r, &m)
}

// testSavedMonitor checks a saved monitor now.
func (h *Hub) testSavedMonitor(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	m, err := h.store.Monitor(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "monitor not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	h.writeTest(w, r, m)
}

func (h *Hub) writeTest(w http.ResponseWriter, r *http.Request, m *store.Monitor) {
	if m.Type == check.TypePush {
		writeErr(w, 400, "heartbeat monitors are checked when the job calls the push URL")
		return
	}
	writeJSON(w, 200, map[string]any{"results": h.runTest(r.Context(), m)})
}

// runTest runs one check of m at each of its locations, in parallel.
func (h *Hub) runTest(ctx context.Context, m *store.Monitor) []testResult {
	locs := h.locationsOf(m)
	names := h.locationNames()
	timeout := time.Duration(m.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	// Room for the agent's round trip on top of the check's own timeout.
	ctx, cancel := context.WithTimeout(ctx, timeout+5*time.Second)
	defer cancel()

	defFamily := h.generalSettings().IPFamily
	out := make([]testResult, len(locs))
	var wg sync.WaitGroup
	for i, loc := range locs {
		name := names[loc]
		if name == "" {
			name = loc
		}
		out[i] = testResult{Location: loc, Name: name}
		job := jobFor(m)
		job.ID = fmt.Sprintf("%s%d-%d", testJobPrefix, i, time.Now().UnixNano())
		h.mu.Lock()
		job.IPFamily = h.familyAtLocked(m, loc, defFamily)
		h.mu.Unlock()
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, ok := h.testAt(ctx, loc, job)
			if !ok {
				out[i].Skipped = true
				out[i].Message = res.Message
				return
			}
			res = checkAgentSupport(m, loc, job.IPFamily, res)
			out[i].OK = res.OK
			out[i].LatencyMs = res.LatencyMs
			out[i].StatusCode = res.StatusCode
			out[i].Message = res.Message
		}()
	}
	wg.Wait()
	return out
}

// testAt runs a job at one location. ok is false when the location could
// not run it (offline, turned off), with the reason in the result message.
func (h *Hub) testAt(ctx context.Context, loc string, job check.Job) (check.Result, bool) {
	if loc == LocalLocation {
		if !h.localEnabled() {
			return check.Result{Message: "the hub's own location is turned off"}, false
		}
		return check.Run(ctx, job), true
	}
	id, ok := parseAgentLocation(loc)
	if !ok {
		return check.Result{Message: "unknown location"}, false
	}
	h.mu.Lock()
	ac := h.agents[id]
	h.mu.Unlock()
	if ac == nil {
		return check.Result{Message: "agent offline"}, false
	}
	ch := h.tests.add(job.ID)
	defer h.tests.remove(job.ID)
	ac.send(protocol.Message{Type: protocol.TypeJob, Job: &job})
	select {
	case r := <-ch:
		return r, true
	case <-ctx.Done():
		return check.Result{Message: "no answer from the agent in time"}, false
	}
}

// checkAgentSupport fails a result from an agent too old for the monitor's
// response rules or IP family, like HandleResult does.
func checkAgentSupport(m *store.Monitor, loc string, fam int, r check.Result) check.Result {
	if loc == LocalLocation || !r.OK {
		return r
	}
	if len(m.Assertions) > 0 && r.Asserted < len(m.Assertions) {
		r.OK = false
		r.Message = "this agent cannot check response rules: update it"
	} else if fam != check.IPAny && (r.IPFamily != fam || (fam == check.IPBoth && !r.Both)) {
		r.OK = false
		r.Message = "this agent cannot limit checks to IPv4 or IPv6: update it"
	}
	return r
}

// ---------- bulk changes ----------

var bulkActions = []string{"pause", "resume", "delete", "group"}

// bulkMonitors pauses, resumes, deletes or moves to a group several monitors.
func (h *Hub) bulkMonitors(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in struct {
		IDs     []int64 `json:"ids"`
		Action  string  `json:"action"`
		GroupID int64   `json:"group_id"` // action "group": 0 removes the group
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if !slices.Contains(bulkActions, in.Action) {
		writeErr(w, 400, "action must be pause, resume, delete or group")
		return
	}
	ids := cleanIDs(in.IDs)
	if len(ids) == 0 {
		writeErr(w, 400, "no monitors selected")
		return
	}
	if len(ids) > 5000 {
		writeErr(w, 400, "too many monitors")
		return
	}
	if in.Action == "group" && in.GroupID > 0 {
		if _, err := h.store.Group(in.GroupID); err != nil {
			writeErr(w, 400, "unknown group")
			return
		}
	}
	monitors := make([]*store.Monitor, 0, len(ids))
	for _, id := range ids {
		m, err := h.store.Monitor(id)
		if errors.Is(err, store.ErrNotFound) {
			writeErr(w, 400, "unknown monitor")
			return
		} else if err != nil {
			h.internalErr(w, err)
			return
		}
		monitors = append(monitors, m)
	}
	now := time.Now().UnixMilli()
	for _, m := range monitors {
		var err error
		switch in.Action {
		case "pause", "resume":
			paused := in.Action == "pause"
			if m.Paused == paused {
				continue
			}
			if err = h.store.SetMonitorPaused(m.ID, paused); err == nil && paused {
				_ = h.store.ResolveIncident(m.ID, now)
			}
		case "delete":
			err = h.store.DeleteMonitor(m.ID)
		case "group":
			group := max(in.GroupID, 0)
			if m.GroupID == group {
				continue
			}
			m.GroupID = group
			err = h.store.UpdateMonitor(m)
		}
		if err != nil {
			h.internalErr(w, err)
			return
		}
		h.MonitorChanged(m.ID)
	}
	if in.Action == "group" {
		h.labelsChanged()
	}
	writeJSON(w, 200, map[string]int{"count": len(monitors)})
}

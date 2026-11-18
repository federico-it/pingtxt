package hub

import (
	"fmt"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestTimedPause(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	must(t, st.CreateMonitor(m))
	h.MonitorChanged(m.ID)
	c := newAPIClient(t, h, st)
	path := fmt.Sprintf("/api/monitors/%d/pause", m.ID)

	now := time.Now()
	for _, body := range []string{
		`{"paused":true,"minutes":-1}`,
		`{"paused":true,"minutes":525601}`,
		`{"paused":true,"minutes":5,"until":1}`,
		fmt.Sprintf(`{"paused":true,"until":%d}`, now.Add(-time.Minute).UnixMilli()),
		fmt.Sprintf(`{"paused":true,"until":%d}`, now.Add(400*24*time.Hour).UnixMilli()),
	} {
		if code := c.json("POST", path, body, nil); code != 400 {
			t.Fatalf("pause %s: %d", body, code)
		}
	}

	// Without a length the pause has no end (as before).
	var got store.Monitor
	if code := c.json("POST", path, `{"paused":true}`, &got); code != 200 || !got.Paused || got.PausedUntil != 0 {
		t.Fatalf("pause: %d %+v", code, got)
	}
	since := got.StatusSince

	// Paused again for 12 hours: the pause gets an end, status_since stays.
	if code := c.json("POST", path, `{"paused":true,"minutes":720}`, &got); code != 200 || !got.Paused ||
		got.PausedUntil < now.Add(719*time.Minute).UnixMilli() || got.PausedUntil > time.Now().Add(721*time.Minute).UnixMilli() || got.StatusSince != since {
		t.Fatalf("timed pause: %d %+v", code, got)
	}
	until := got.PausedUntil

	// PUT leaves the pause alone.
	if code := c.json("PUT", fmt.Sprintf("/api/monitors/%d", m.ID), `{"name":"api 2","paused_until":0,"paused":false}`, &got); code != 200 || !got.Paused || got.PausedUntil != until {
		t.Fatalf("put: %d %+v", code, got)
	}

	// Before the end nothing happens; at the end it resumes by itself.
	h.pauseTick(time.UnixMilli(until - 1000))
	if cur, _ := st.Monitor(m.ID); !cur.Paused {
		t.Fatal("resumed too early")
	}
	h.pauseTick(time.UnixMilli(until))
	cur, _ := st.Monitor(m.ID)
	if cur.Paused || cur.PausedUntil != 0 || cur.Status != "pending" {
		t.Fatalf("not resumed: %+v", cur)
	}
	h.mu.Lock()
	inMem := h.monitors[m.ID].m.Paused
	h.mu.Unlock()
	if inMem {
		t.Fatal("scheduler still sees the monitor paused")
	}
	list := auditEntries(t, st, store.AuditFilter{TargetType: "monitor", TargetID: m.ID, Limit: 1})
	if len(list) != 1 || list[0].Action != "monitor.resume" || list[0].Source != store.AuditSystem {
		t.Fatalf("audit: %+v", list)
	}

	// Resuming by hand clears the end; bulk pause takes a length too.
	must(t, st.PauseMonitor(m.ID, true, until))
	if code := c.json("POST", path, `{"paused":false,"minutes":60}`, &got); code != 200 || got.Paused || got.PausedUntil != 0 {
		t.Fatalf("resume: %d %+v", code, got)
	}
	var res map[string]int
	if code := c.json("POST", "/api/monitors/bulk", fmt.Sprintf(`{"ids":[%d],"action":"pause","minutes":60}`, m.ID), &res); code != 200 {
		t.Fatalf("bulk: %d", code)
	}
	if cur, _ := st.Monitor(m.ID); !cur.Paused || cur.PausedUntil < now.Add(59*time.Minute).UnixMilli() {
		t.Fatalf("bulk pause: %+v", cur)
	}
	if code := c.json("POST", "/api/monitors/bulk", fmt.Sprintf(`{"ids":[%d],"action":"pause","minutes":-5}`, m.ID), nil); code != 400 {
		t.Fatalf("bulk invalid: %d", code)
	}
}

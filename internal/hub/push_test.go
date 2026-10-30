package hub

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func newPushMonitor(t *testing.T, h *Hub, st *store.Store) *store.Monitor {
	t.Helper()
	m := &store.Monitor{Name: "backup", Type: "push", IntervalSec: 60, GraceSec: 30}
	if err := validatePush(m); err != nil {
		t.Fatal(err)
	}
	m.PushToken = newPushToken()
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	return m
}

func TestPushHeartbeatAndDeadline(t *testing.T) {
	h, st := newTestHub(t)
	m := newPushMonitor(t, h, st)

	// Nothing is due before the period plus grace has passed.
	h.tick(context.Background(), time.Now().Add(80*time.Second))
	if s := status(t, st, m.ID); s != "pending" {
		t.Fatalf("want pending before the deadline, got %s", s)
	}
	if err := h.HandlePush(m.PushToken, true, "", 0); err != nil {
		t.Fatal(err)
	}
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up after a heartbeat, got %s", s)
	}
	// Deadline missed: down with an open incident.
	h.tick(context.Background(), time.Now().Add(91*time.Second))
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("want down after a missed heartbeat, got %s", s)
	}
	inc, _ := st.ListIncidents(m.ID, 10)
	if len(inc) != 1 || inc[0].ResolvedAt != 0 || !strings.Contains(inc[0].Cause, "no heartbeat") {
		t.Fatalf("want 1 open incident about the missing heartbeat, got %+v", inc)
	}
	// Next heartbeat resolves it.
	h.mu.Lock()
	h.monitors[m.ID].lastPing = time.Time{}
	h.mu.Unlock()
	if err := h.HandlePush(m.PushToken, true, "", 0); err != nil {
		t.Fatal(err)
	}
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up after recovery, got %s", s)
	}
	inc, _ = st.ListIncidents(m.ID, 10)
	if inc[0].ResolvedAt == 0 {
		t.Fatal("incident should be resolved")
	}
	if err := h.HandlePush("nope", true, "", 0); err != errPushNotFound {
		t.Fatalf("want not found for an unknown token, got %v", err)
	}
}

func TestPushEndpoint(t *testing.T) {
	h, st := newTestHub(t)
	m := newPushMonitor(t, h, st)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()

	// A failed run reported by the job, as a form POST (no JSON, no cookie).
	res, err := http.Post(srv.URL+"/api/push/"+m.PushToken+"?status=down&msg=disk+full&ping=1200", "application/x-www-form-urlencoded", strings.NewReader("x=1"))
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, res.Body)
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("want 200, got %d", res.StatusCode)
	}
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("want down after status=down, got %s", s)
	}
	rows, _ := st.RecentResults(m.ID, 1)
	if len(rows) != 1 || rows[0].Message != "disk full" || rows[0].LatencyMs != 1200 || rows[0].Location != PushLocation {
		t.Fatalf("unexpected stored heartbeat %+v", rows)
	}

	res, err = http.Get(srv.URL + "/api/push/unknown")
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("want 404 for an unknown token, got %d", res.StatusCode)
	}
}

func TestPushPausedIgnored(t *testing.T) {
	h, st := newTestHub(t)
	m := newPushMonitor(t, h, st)
	_ = st.SetMonitorPaused(m.ID, true)
	h.MonitorChanged(m.ID)
	if err := h.HandlePush(m.PushToken, true, "", 0); err != errPushPaused {
		t.Fatalf("want paused, got %v", err)
	}
	h.tick(context.Background(), time.Now().Add(time.Hour))
	if s := status(t, st, m.ID); s != StatusPaused {
		t.Fatalf("want paused, got %s", s)
	}
}

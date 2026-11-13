package hub

import (
	"io"
	"log/slog"
	"strconv"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestIncidentSnooze(t *testing.T) {
	h, st := newTestHub(t)
	sink := newAlertSink(t)
	sink.channel(t, st, "now", 0, 0)   // alerted at once, no reminders
	sink.channel(t, st, "rem", 0, 5)   // alerted at once, reminders every 5 minutes
	sink.channel(t, st, "boss", 10, 0) // escalation after 10 minutes
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	must(t, st.CreateMonitor(m))
	h.MonitorChanged(m.ID)
	c := newAPIClient(t, h, st) // creates the admin a@b.c

	start := time.Now()
	r := result(m.ID, false)
	r.At = start.Add(-2 * time.Minute).UnixMilli()
	h.HandleResult(LocalLocation, r)
	if s := summary(sink.wait(t, 2)); s != "now:down rem:down" {
		t.Fatalf("first alerts: %s", s)
	}
	open := h.openIncidentOf(m.ID)
	if open == nil {
		t.Fatal("no open incident")
	}
	path := "/api/incidents/" + strconv.FormatInt(open.ID, 10) + "/snooze"

	for _, body := range []string{`{}`, `{"minutes":3}`, `{"minutes":2000}`} {
		if code := c.json("POST", path, body, nil); code != 400 {
			t.Fatalf("snooze %s: %d", body, code)
		}
	}
	var inc store.Incident
	if code := c.json("POST", path, `{"minutes":30}`, &inc); code != 200 || inc.SnoozedBy != "a@b.c" || inc.SnoozedUntil < start.Add(29*time.Minute).UnixMilli() {
		t.Fatalf("snooze: %d %+v", code, inc)
	}
	if o := h.openIncidentOf(m.ID); o == nil || o.SnoozedUntil != inc.SnoozedUntil || o.SnoozedBy != "a@b.c" {
		t.Fatalf("summary: %+v", o)
	}

	// While snoozed: no reminders, no escalation. Also after a restart.
	h.escalate(start.Add(20 * time.Minute))
	h2 := New(Config{LocalName: "hub"}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	must(t, h2.reloadAll())
	h2.escalate(start.Add(25 * time.Minute))
	sink.wait(t, 2)

	// The snooze ends: every alerted channel is reminded, also the one
	// without reminders, and the escalation goes on.
	h2.escalate(start.Add(31 * time.Minute))
	if s := summary(sink.wait(t, 5)); s != "boss:down now:down now:down#r rem:down rem:down#r" {
		t.Fatalf("after snooze: %s", s)
	}
	h2.escalate(start.Add(32 * time.Minute))
	sink.wait(t, 5)
	h2.escalate(start.Add(37 * time.Minute)) // rem's next reminder
	if s := summary(sink.wait(t, 6)); s != "boss:down now:down now:down#r rem:down rem:down#r rem:down#rr" {
		t.Fatalf("reminders again: %s", s)
	}

	// Ending a snooze early wakes it up without the extra reminder.
	u, err := st.UserByEmail("a@b.c")
	must(t, err)
	if _, err := h2.SnoozeIncident(open.ID, u, 60, start.Add(37*time.Minute)); err != nil {
		t.Fatal(err)
	}
	h2.escalate(start.Add(43 * time.Minute))
	sink.wait(t, 6)
	if _, err := h2.SnoozeIncident(open.ID, u, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if o := h2.openIncidentOf(m.ID); o == nil || o.SnoozedUntil != 0 || o.SnoozedBy != "" {
		t.Fatalf("after wake up: %+v", o)
	}
	h2.escalate(start.Add(43 * time.Minute)) // rem is due since 42
	if s := summary(sink.wait(t, 7)); s != "boss:down now:down now:down#r rem:down rem:down#r rem:down#rr rem:down#rrr" {
		t.Fatalf("after wake up: %s", s)
	}

	// An acknowledged incident stays quiet when its snooze ends.
	if _, err := h2.SnoozeIncident(open.ID, u, 5, start.Add(43*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if _, err := h2.AckIncident(open.ID, u, true); err != nil {
		t.Fatal(err)
	}
	h2.escalate(start.Add(60 * time.Minute))
	sink.wait(t, 7)

	// Resolved: no more snoozing.
	h2.HandleResult(LocalLocation, result(m.ID, true))
	sink.wait(t, 10)
	if _, err := h2.SnoozeIncident(open.ID, u, 30, time.Now()); err != errIncidentResolved {
		t.Fatalf("snooze resolved: %v", err)
	}
	if code := c.json("POST", "/api/incidents/999999/snooze", `{"minutes":30}`, nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
}

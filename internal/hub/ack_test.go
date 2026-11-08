package hub

import (
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestIncidentAck(t *testing.T) {
	h, st := newTestHub(t)
	sink := newAlertSink(t)
	sink.channel(t, st, "now", 0, 5)   // alerted at once, reminders every 5 minutes, wants acks
	sink.channel(t, st, "quiet", 0, 0) // alerted at once, no ack event
	sink.channel(t, st, "boss", 10, 0) // escalation after 10 minutes, wants acks
	for _, name := range []string{"now", "boss"} {
		list, _ := st.ListNotifiers()
		for _, n := range list {
			if n.Name == name {
				n.Events = append(slices.Clone(n.Events), store.EventAck)
				must(t, st.UpdateNotifier(n))
			}
		}
	}
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	must(t, st.CreateMonitor(m))
	h.MonitorChanged(m.ID)
	c := newAPIClient(t, h, st) // creates the admin a@b.c
	u, err := st.UserByEmail("a@b.c")
	must(t, err)

	r := result(m.ID, false)
	r.At = time.Now().Add(-11 * time.Minute).UnixMilli()
	h.HandleResult(LocalLocation, r)
	first := sink.wait(t, 2)
	if got := summary(first); got != "now:down quiet:down" {
		t.Fatalf("first alerts: %s", got)
	}
	open := h.openIncidentOf(m.ID)
	if open == nil || open.AckedAt != 0 {
		t.Fatalf("open incident: %+v", open)
	}
	if got := first["now"][0]; got.IncidentID != open.ID {
		t.Fatalf("down event incident %d, want %d", got.IncidentID, open.ID)
	}

	// Acknowledged over the API: the channels that got the alert and want
	// acks are told, the escalation and the reminders stop.
	path := "/api/incidents/" + strconv.FormatInt(open.ID, 10) + "/ack"
	var inc store.Incident
	if code := c.json("POST", path, "", &inc); code != 200 || inc.AckedBy != "a@b.c" || inc.AckedAt == 0 {
		t.Fatalf("ack: %d %+v", code, inc)
	}
	got := sink.wait(t, 3)
	if s := summary(got); s != "now:acknowledged now:down quiet:down" {
		t.Fatalf("after ack: %s", s)
	}
	if e := got["now"][1]; e.Kind != KindAck || e.AckBy != "a@b.c" || e.IncidentID != open.ID || !strings.HasPrefix(e.Text(), "[ACK] api (http://x) acknowledged by a@b.c") {
		t.Fatalf("ack event: %+v %q", e, e.Text())
	}
	again := inc
	if code := c.json("POST", path, "", &again); code != 200 || again.AckedAt != inc.AckedAt {
		t.Fatalf("ack again: %d %+v", code, again)
	}
	h.escalate(time.Now())
	h.escalate(time.Now().Add(30 * time.Minute))
	sink.wait(t, 3)
	if o := h.openIncidentOf(m.ID); o == nil || o.AckedBy != "a@b.c" {
		t.Fatalf("summary incident: %+v", o)
	}
	list, err := st.FindIncidents(store.IncidentQuery{MonitorID: m.ID, Limit: 10})
	must(t, err)
	if len(list) != 1 || list[0].AckedBy != "a@b.c" {
		t.Fatalf("stored: %+v", list)
	}

	// A restart keeps the acknowledgement.
	h2 := New(Config{LocalName: "hub"}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	must(t, h2.reloadAll())
	h2.escalate(time.Now().Add(30 * time.Minute))
	sink.wait(t, 3)

	// Taken back: reminders and escalation run again.
	if _, err := h2.AckIncident(open.ID, u, false); err != nil {
		t.Fatal(err)
	}
	if o := h2.openIncidentOf(m.ID); o == nil || o.AckedAt != 0 || o.AckedBy != "" {
		t.Fatalf("after unack: %+v", o)
	}
	h2.escalate(time.Now())
	if s := summary(sink.wait(t, 5)); s != "boss:down now:acknowledged now:down now:down#r quiet:down" {
		t.Fatalf("after unack: %s", s)
	}

	// Acknowledged again by someone else: boss now got the alert and hears too.
	other, err := st.CreateUser("o@b.c", "", store.RoleAdmin)
	must(t, err)
	if _, err := h2.AckIncident(open.ID, other, true); err != nil {
		t.Fatal(err)
	}
	if s := summary(sink.wait(t, 7)); s != "boss:acknowledged boss:down now:acknowledged now:acknowledged now:down now:down#r quiet:down" {
		t.Fatalf("second ack: %s", s)
	}

	// Recovery goes to every alerted channel; the incident can no longer be acknowledged.
	h2.HandleResult(LocalLocation, result(m.ID, true))
	if s := summary(sink.wait(t, 10)); !strings.Contains(s, "boss:up") || !strings.Contains(s, "now:up") || !strings.Contains(s, "quiet:up") {
		t.Fatalf("recovery: %s", s)
	}
	if _, err := h2.AckIncident(open.ID, u, true); err != errIncidentResolved {
		t.Fatalf("ack resolved: %v", err)
	}
	if code := c.json("POST", "/api/incidents/999999/ack", "", nil); code != 404 {
		t.Fatalf("missing: %d", code)
	}
	if code := c.json("DELETE", path, "", nil); code != 409 {
		t.Fatalf("unack resolved: %d", code)
	}
}

func TestAckAppMessage(t *testing.T) {
	down := appMessage(Event{MonitorID: 42, Monitor: "api", Target: "https://x", Status: StatusDown, IncidentID: 7})
	if down.Data["incident_id"] != "7" {
		t.Fatalf("down data: %+v", down.Data)
	}
	ack := appMessage(Event{Kind: KindAck, MonitorID: 42, Monitor: "api", Target: "https://x", Status: StatusAcked, IncidentID: 7, AckBy: "a@b.c", At: 600_000, DownSince: 1})
	if ack.Category != CategoryMonitorAcked || ack.CollapseID != down.CollapseID || ack.Title != "api acknowledged" ||
		!strings.Contains(ack.Body, "By a@b.c") || ack.Priority != "normal" || ack.Data["status"] != StatusAcked {
		t.Fatalf("ack push: %+v", ack)
	}
	if s := emailSubject(Event{Kind: KindAck, Monitor: "api", AckBy: "a@b.c"}); s != "[pingtxt] api acknowledged by a@b.c" {
		t.Fatalf("subject %q", s)
	}
}

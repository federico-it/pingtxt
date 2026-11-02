package hub

import (
	"strings"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func testDiag() *check.Diag {
	return &check.Diag{
		At: time.Now().UnixMilli(), Host: "example.com", Port: "443",
		DNS: []check.DiagDNS{{Resolver: "system", Query: "A/AAAA example.com", Answers: []string{"192.0.2.10"}, Ms: 12}},
		TCP: []check.DiagConn{{Addr: "192.0.2.10:443", Error: "i/o timeout", Ms: 3000}},
		Trace: &check.DiagTrace{Proto: "tcp", Dest: "192.0.2.10", Port: 443, Hops: []check.DiagHop{
			{TTL: 1, Addr: "10.0.0.1", RTTMs: 1}, {TTL: 2, Addr: "198.51.100.7", Name: "ae1.fra.example.net", RTTMs: 9}, {TTL: 3},
		}},
	}
}

func TestDiagnosticsFlow(t *testing.T) {
	h, st := newTestHub(t)
	h.smu.Lock()
	h.general.PublicURL = "https://hub.example"
	h.smu.Unlock()
	sink := newAlertSink(t)
	sink.channel(t, st, "now", 0, 5)
	m := &store.Monitor{Name: "api", Type: "http", Target: "https://example.com", IntervalSec: 60, Retries: 2, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)

	h.mu.Lock()
	st0 := h.monitors[m.ID]
	if !h.wantDiagLocked(st0, LocalLocation, h.generalSettings(), time.Now()) {
		t.Fatal("a location that is not failing should be asked for diagnostics")
	}
	h.mu.Unlock()

	// First failure: diagnostics, no incident yet (2 retries).
	r := result(m.ID, false)
	r.Message = "i/o timeout"
	r.Diag = testDiag()
	h.HandleResult(LocalLocation, r)
	h.mu.Lock()
	if h.wantDiagLocked(st0, LocalLocation, h.generalSettings(), time.Now()) {
		t.Fatal("a failing location must not be asked again")
	}
	h.mu.Unlock()
	list, err := st.MonitorDiagnostics(m.ID, 0, 10)
	if err != nil || len(list) != 1 || list[0].IncidentID != 0 {
		t.Fatalf("stored diagnostics: %+v %v", list, err)
	}

	// Second failure opens the incident: the alert carries the summary.
	h.HandleResult(LocalLocation, result(m.ID, false))
	got := sink.wait(t, 1)["now"][0]
	if len(got.Diagnostics) != 1 || !strings.Contains(got.Diagnostics[0].Summary, "TCP 443: i/o timeout") ||
		!strings.Contains(got.Diagnostics[0].Summary, "traceroute stops at hop 2 (ae1.fra.example.net)") {
		t.Fatalf("alert diagnostics: %+v", got.Diagnostics)
	}
	incs, _ := st.FindIncidents(store.IncidentQuery{MonitorID: m.ID, Limit: 10})
	if len(incs) != 1 {
		t.Fatalf("incidents: %d", len(incs))
	}
	if want := diagURL("https://hub.example", incs[0].ID); got.DiagnosticsURL != want {
		t.Fatalf("url %q, want %q", got.DiagnosticsURL, want)
	}
	if !strings.Contains(got.Text(), "\n  hub: DNS 192.0.2.10") || !strings.Contains(got.Text(), "diagnostics: "+got.DiagnosticsURL) {
		t.Fatalf("text: %q", got.Text())
	}
	linked, _ := st.IncidentDiagnostics(incs[0].ID)
	if len(linked) != 1 || linked[0].Location != LocalLocation {
		t.Fatalf("linked: %+v", linked)
	}

	// Reminders carry only the link.
	h.escalate(time.Now().Add(6 * time.Minute))
	rem := sink.wait(t, 2)["now"][1]
	if len(rem.Diagnostics) != 0 || rem.DiagnosticsURL == "" {
		t.Fatalf("reminder: %+v", rem)
	}

	// Turned off on the monitor: no diagnostics asked.
	m.Diagnostics = diagOff
	h.mu.Lock()
	if h.wantDiagLocked(&monState{m: m, locs: map[string]*locState{}}, LocalLocation, h.generalSettings(), time.Now()) {
		t.Fatal("diagnostics off on the monitor")
	}
	h.mu.Unlock()

	// Pruning: unlinked ones go after a day, linked ones after the retention.
	if _, err := st.InsertDiagnostic(&store.Diagnostic{MonitorID: m.ID, Location: "x", At: time.Now().Add(-48 * time.Hour).UnixMilli(), Data: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	h.HandleResult(LocalLocation, result(m.ID, true)) // closes the incident
	sink.wait(t, 3)
	if _, err := st.InsertDiagnostic(&store.Diagnostic{MonitorID: m.ID, Location: "y", At: time.Now().Add(-48 * time.Hour).UnixMilli(), Data: []byte("{}")}); err != nil {
		t.Fatal(err)
	}
	n, err := st.PruneDiagnostics(time.Now().Add(-24*time.Hour).UnixMilli(), time.Now().Add(-30*24*time.Hour).UnixMilli())
	if err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
}

func TestDiagEmail(t *testing.T) {
	e := Event{MonitorID: 1, Monitor: "api", Target: "https://example.com", Status: StatusDown, At: time.Now().UnixMilli(),
		Diagnostics: []EventDiag{{Location: "Frankfurt", Summary: "x", Diag: testDiag()}}, DiagnosticsURL: "https://hub.example/incidents/?diag=4"}
	v := eventMailView(e, "https://hub.example")
	text := v.renderText("")
	for _, want := range []string{"# diagnostics · Frankfurt", "192.0.2.10:443 i/o timeout", "TCP 443 → 10.0.0.1 › ae1.fra.example.net › * (destination not reached)", "full diagnostics: https://hub.example/incidents/?diag=4"} {
		if !strings.Contains(text, want) {
			t.Fatalf("missing %q in\n%s", want, text)
		}
	}
	if html := v.renderHTML(nil); !strings.Contains(html, "# diagnostics · Frankfurt") || strings.HasPrefix(html, "<pre>") {
		t.Fatal("html section missing")
	}
	if msg := appMessage(e); !strings.Contains(msg.Body, "Frankfurt: x") || msg.Data["diagnostics_url"] == "" {
		t.Fatalf("push: %+v", msg)
	}
}

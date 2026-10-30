package hub

import (
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestMaintenanceSchedule(t *testing.T) {
	rome, _ := time.LoadLocation("Europe/Rome")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, rome)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// Every Sunday 23:00-01:00 Rome time (crosses midnight).
	w := &store.Maintenance{Kind: store.MaintWeekly, Weekdays: []int{0}, StartTime: "23:00", DurationMin: 120, Timezone: "Europe/Rome", Enabled: true}
	cases := []struct {
		t    string
		want bool
	}{
		{"2026-11-01 22:59", false}, // Sunday
		{"2026-11-01 23:00", true},
		{"2026-11-02 00:30", true}, // Monday, still in Sunday's window
		{"2026-11-02 01:00", false},
		{"2026-11-03 23:30", false}, // Tuesday
	}
	for _, c := range cases {
		if _, _, ok := w.ActiveAt(at(c.t)); ok != c.want {
			t.Errorf("weekly at %s: got %v want %v", c.t, ok, c.want)
		}
	}
	s, e, ok := w.NextAfter(at("2026-11-02 00:30"))
	if !ok || !s.Equal(at("2026-11-08 23:00")) || !e.Equal(at("2026-11-09 01:00")) {
		t.Errorf("next: got %v-%v %v", s, e, ok)
	}

	// Monthly on the 1st and 15th, 02:00 for 30m, valid until Dec 1st.
	m := &store.Maintenance{Kind: store.MaintMonthly, MonthDays: []int{1, 15}, StartTime: "02:00", DurationMin: 30, Timezone: "Europe/Rome", Enabled: true, EndAt: at("2026-12-01 00:00").UnixMilli()}
	if _, _, ok := m.ActiveAt(at("2026-11-15 02:10")); !ok {
		t.Error("monthly should be active on the 15th")
	}
	if _, _, ok := m.ActiveAt(at("2026-12-01 02:10")); ok {
		t.Error("monthly should end at its validity range")
	}
	if _, _, ok := m.NextAfter(at("2026-11-20 00:00")); ok {
		t.Error("no occurrence expected after the validity range")
	}

	// One-off window, disabled windows never match.
	o := &store.Maintenance{Kind: store.MaintOnce, StartAt: at("2026-11-05 10:00").UnixMilli(), EndAt: at("2026-11-05 11:00").UnixMilli(), Enabled: true}
	if _, _, ok := o.ActiveAt(at("2026-11-05 10:30")); !ok {
		t.Error("once should be active")
	}
	o.Enabled = false
	if _, _, ok := o.ActiveAt(at("2026-11-05 10:30")); ok {
		t.Error("disabled window must not be active")
	}
}

func TestMaintenanceSuppressesAlerts(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	other := &store.Monitor{Name: "o", Type: "http", Target: "http://y", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	for _, x := range []*store.Monitor{m, other} {
		if err := st.CreateMonitor(x); err != nil {
			t.Fatal(err)
		}
		h.MonitorChanged(x.ID)
	}
	h.HandleResult(LocalLocation, result(m.ID, true))

	now := time.Now()
	w := &store.Maintenance{Name: "deploy", Kind: store.MaintOnce, MonitorIDs: []int64{m.ID}, StartAt: now.Add(-time.Minute).UnixMilli(), EndAt: now.Add(time.Hour).UnixMilli(), Timezone: "UTC", Enabled: true}
	if err := st.CreateMaintenance(w); err != nil {
		t.Fatal(err)
	}
	h.afterMaintChange()
	if s := status(t, st, m.ID); s != StatusMaintenance {
		t.Fatalf("want maintenance, got %s", s)
	}
	if s := status(t, st, other.ID); s == StatusMaintenance {
		t.Fatal("window must only cover the selected monitor")
	}

	// Failing checks during maintenance: no incident, status stays maintenance.
	h.HandleResult(LocalLocation, result(m.ID, false))
	if s := status(t, st, m.ID); s != StatusMaintenance {
		t.Fatalf("want maintenance during failures, got %s", s)
	}
	if inc, _ := st.ListIncidents(m.ID, 10); len(inc) != 0 {
		t.Fatalf("no incident expected during maintenance, got %d", len(inc))
	}
	rows, _ := st.RecentResults(m.ID, 1)
	if !rows[0].Maint {
		t.Fatal("result during maintenance should be flagged")
	}
	if up, _, _ := st.Uptime(m.ID, 0); up != 1 {
		t.Fatalf("maintenance checks must not count toward uptime, got %v", up)
	}

	// Window ends while still down: the incident opens then.
	w.EndAt = time.Now().Add(-time.Second).UnixMilli()
	w.StartAt = w.EndAt - 1000
	if err := st.UpdateMaintenance(w); err != nil {
		t.Fatal(err)
	}
	h.afterMaintChange()
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("want down after the window, got %s", s)
	}
	inc, _ := st.ListIncidents(m.ID, 10)
	if len(inc) != 1 || inc[0].ResolvedAt != 0 {
		t.Fatalf("want 1 open incident after the window, got %+v", inc)
	}
}

func TestMaintenanceResolvesOnRecovery(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	h.HandleResult(LocalLocation, result(m.ID, false)) // down, incident open

	w := &store.Maintenance{Name: "all", Kind: store.MaintDaily, AllMonitors: true, StartTime: "00:00", DurationMin: 24 * 60, Timezone: "UTC", Enabled: true}
	if err := st.CreateMaintenance(w); err != nil {
		t.Fatal(err)
	}
	h.afterMaintChange()
	h.HandleResult(LocalLocation, result(m.ID, true)) // recovers during maintenance
	if inc, _ := st.ListIncidents(m.ID, 10); inc[0].ResolvedAt != 0 {
		t.Fatal("incident should stay open while in maintenance")
	}
	if err := st.DeleteMaintenance(w.ID); err != nil {
		t.Fatal(err)
	}
	h.afterMaintChange()
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up, got %s", s)
	}
	if inc, _ := st.ListIncidents(m.ID, 10); inc[0].ResolvedAt == 0 {
		t.Fatal("incident should be resolved once the window ends")
	}
}

package store

import (
	"testing"
	"time"
)

func TestLatencyPercentiles(t *testing.T) {
	s := openTest(t)
	m := &Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}}
	if err := s.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	from := now - 3*RollupHour
	var rows []*ResultRow
	for i := 1; i <= 100; i++ { // latencies 1..100 ms, plus a failure and a check in maintenance
		rows = append(rows, &ResultRow{MonitorID: m.ID, Location: "local", TS: from + int64(i)*1000, OK: true, LatencyMs: float64(i)})
	}
	rows = append(rows,
		&ResultRow{MonitorID: m.ID, Location: "local", TS: from + 500_000, OK: false, LatencyMs: 9999},
		&ResultRow{MonitorID: m.ID, Location: "local", TS: from + 600_000, OK: true, LatencyMs: 9999, Maint: true})
	if err := s.InsertResults(rows); err != nil {
		t.Fatal(err)
	}
	p, err := s.LatencyPercentiles(m.ID, from, now)
	if err != nil {
		t.Fatal(err)
	}
	if p.N != 100 || p.P50 != 50 || p.P95 != 95 || p.P99 != 99 || p.From != from {
		t.Fatalf("percentiles: %+v", p)
	}
	// Results start more than an hour after the window: only partly covered.
	p, err = s.LatencyPercentiles(m.ID, from-2*RollupHour, now)
	if err != nil {
		t.Fatal(err)
	}
	if p.From != from+1000 {
		t.Fatalf("partial window from %d, want %d", p.From, from+1000)
	}
	if p, _ := s.LatencyPercentiles(m.ID, now-1000, now); p.N != 0 {
		t.Fatalf("empty window: %+v", p)
	}
}

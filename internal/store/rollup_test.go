package store

import (
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// openTest opens an empty store: SQLite in a temp dir, or a scratch
// database on the server of PINGTXT_TEST_DB_URL (postgres:// or mysql://).
func openTest(t *testing.T) *Store {
	t.Helper()
	if u := os.Getenv("PINGTXT_TEST_DB_URL"); u != "" {
		s, drop, err := OpenScratch(u)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(drop)
		return s
	}
	s, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

type snapshot struct {
	up, avg [3]float64
	buckets [3][]Bucket
}

func takeSnapshot(t *testing.T, s *Store, id, now int64) snapshot {
	t.Helper()
	day := 24 * RollupHour
	var sn snapshot
	for i, since := range []int64{now - 5*day, now - 3*day - 7*RollupHour, now - 6*RollupHour} {
		up, avg, err := s.Uptime(id, since)
		if err != nil {
			t.Fatal(err)
		}
		sn.up[i], sn.avg[i] = up, avg
	}
	from := now - 6*day
	for i, q := range []struct {
		size  int64
		perLo bool
	}{{day, false}, {2 * RollupHour, true}, {3 * RollupHour, false}} {
		b, err := s.Buckets(id, from, now+1, q.size, q.perLo)
		if err != nil {
			t.Fatal(err)
		}
		sn.buckets[i] = b
	}
	return sn
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9*math.Max(1, math.Abs(a)) }

func sameSnapshot(t *testing.T, name string, a, b snapshot) {
	t.Helper()
	for i := range a.up {
		if !near(a.up[i], b.up[i]) || !near(a.avg[i], b.avg[i]) {
			t.Fatalf("%s: uptime %d: %f/%f, want %f/%f", name, i, b.up[i], b.avg[i], a.up[i], a.avg[i])
		}
	}
	for i := range a.buckets {
		if len(a.buckets[i]) != len(b.buckets[i]) {
			t.Fatalf("%s: buckets %d: %d, want %d", name, i, len(b.buckets[i]), len(a.buckets[i]))
		}
		for j := range a.buckets[i] {
			x, y := a.buckets[i][j], b.buckets[i][j]
			if x.Start != y.Start || x.Location != y.Location || x.Total != y.Total || x.OK != y.OK || x.Degraded != y.Degraded || x.Maint != y.Maint ||
				!near(x.AvgMs, y.AvgMs) || !near(x.MinMs, y.MinMs) || !near(x.MaxMs, y.MaxMs) {
				t.Fatalf("%s: bucket %d/%d: %+v, want %+v", name, i, j, y, x)
			}
			if (x.Phases == nil) != (y.Phases == nil) || x.Phases != nil && *x.Phases != *y.Phases {
				t.Fatalf("%s: bucket %d/%d phases: %+v, want %+v", name, i, j, y.Phases, x.Phases)
			}
		}
	}
}

// Reads give the same answers before and after rolling up and pruning raw
// results (for hour-aligned windows).
func TestRollupKeepsHistory(t *testing.T) {
	s := openTest(t)
	m := &Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}}
	if err := s.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	now -= now % RollupHour
	var rows []*ResultRow
	for i := range 6 * 24 * 12 { // 6 days, every 5 min, two locations
		ts := now - int64(i)*5*60*1000 - 1
		for _, loc := range []string{"local", "agent:1"} {
			r := &ResultRow{MonitorID: m.ID, Location: loc, TS: ts, OK: i%9 != 0, LatencyMs: float64(5 + (i*7)%50), Maint: i%13 == 0}
			if i%3 != 0 { // some checks measured phases, others did not (old agents)
				r.Timing = &Phases{DNSMs: 0.4 + float64(i%5), ConnectMs: 1.25, TLSMs: float64(i % 3), TTFBMs: 2.5}
			}
			if !r.OK {
				r.MonState = MonDown
				if i%2 == 0 {
					r.MonState = MonDegraded
				}
			}
			rows = append(rows, r)
		}
	}
	if err := s.InsertResults(rows); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.PruneResults(now); n != 0 {
		t.Fatalf("nothing may be pruned before the rollup, pruned %d", n)
	}
	before := takeSnapshot(t, s, m.ID, now)
	if p := before.buckets[0][0].Phases; p == nil || p.ConnectMs != 1.3 || p.TTFBMs != 2.5 || p.TotalMs <= p.DNSMs+p.ConnectMs+p.TLSMs+p.TTFBMs-1 {
		t.Fatalf("phases of the first bucket: %+v", p)
	}

	hours, err := s.RollupResults(time.UnixMilli(now).Add(RollupLag), nil)
	if err != nil {
		t.Fatal(err)
	}
	if hours < 6*24 || s.RollupUntil() != now {
		t.Fatalf("rolled %d hours until %d, want until %d", hours, s.RollupUntil(), now)
	}
	sameSnapshot(t, "after rollup", before, takeSnapshot(t, s, m.ID, now))

	if n, err := s.PruneResults(now - 2*24*RollupHour); err != nil || n == 0 {
		t.Fatalf("prune: %d %v", n, err)
	}
	sameSnapshot(t, "after prune", before, takeSnapshot(t, s, m.ID, now))

	// The boundary survives a restart, and a second run does nothing.
	s.rollupUntil = 0
	if err := s.loadRollupUntil(); err != nil || s.RollupUntil() != now {
		t.Fatalf("reload: %d %v", s.RollupUntil(), err)
	}
	if n, _ := s.RollupResults(time.UnixMilli(now).Add(RollupLag), nil); n != 0 {
		t.Fatalf("second run rolled %d hours", n)
	}

	if n, err := s.PruneRollups(now - 3*24*RollupHour); err != nil || n == 0 {
		t.Fatalf("prune rollups: %d %v", n, err)
	}
	var left int
	_ = s.db.QueryRow(`SELECT COUNT(*) FROM result_rollups WHERE start < ?`, now-3*24*RollupHour).Scan(&left)
	if left != 0 {
		t.Fatalf("%d old rollups left", left)
	}
}

func TestResultPhasesRoundTrip(t *testing.T) {
	s := openTest(t)
	m := &Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}}
	if err := s.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	with := &ResultRow{MonitorID: m.ID, Location: "local", TS: now - 2, OK: true, LatencyMs: 40,
		Timing: &Phases{DNSMs: 3.04, ConnectMs: 7.25, TLSMs: 12, TTFBMs: 0.01}}
	without := &ResultRow{MonitorID: m.ID, Location: "local", TS: now - 1, OK: true, LatencyMs: 9}
	if err := s.InsertResult(with); err != nil {
		t.Fatal(err)
	}
	if err := s.InsertResults([]*ResultRow{without}); err != nil {
		t.Fatal(err)
	}
	got, err := s.RecentResults(m.ID, 10)
	if err != nil || len(got) != 2 {
		t.Fatalf("%v %v", got, err)
	}
	if got[0].Timing != nil {
		t.Fatalf("no phases measured, got %+v", got[0].Timing)
	}
	// Tenths of a ms; a measured wait is never stored as 0.
	if p := got[1].Timing; p == nil || *p != (Phases{DNSMs: 3, ConnectMs: 7.3, TLSMs: 12, TTFBMs: 0.1}) {
		t.Fatalf("phases %+v", p)
	}
	b, err := s.Buckets(m.ID, now-RollupHour, now+1, RollupHour, true)
	if err != nil || len(b) != 1 || b[0].Phases == nil || b[0].Phases.TotalMs != 40 || b[0].AvgMs != 24.5 {
		t.Fatalf("buckets %+v %v", b, err)
	}
}

// Migration 18 turns failed checks stored before migration 14 back into
// uptime when most locations of the same round were fine, in the raw
// results and in the rollup hours they still cover.
func TestRecomputeOldMonState(t *testing.T) {
	s := openTest(t)
	m := &Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}}
	if err := s.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	now -= now % RollupHour
	h := now - 2*RollupHour
	locs := []string{"local", "agent:1", "agent:2"}
	var rows []*ResultRow
	for i := range 60 {
		ts := h + int64(i)*60_000 + 5_000
		for j, loc := range locs {
			// Round 10: one location fails. Round 20: two of three fail.
			ok := !(i == 10 && j == 0) && !(i == 20 && j < 2)
			r := &ResultRow{MonitorID: m.ID, Location: loc, TS: ts + int64(j)*1000, OK: ok, LatencyMs: 1}
			if !ok {
				r.MonState = MonDown // as migration 14 left them
			}
			rows = append(rows, r)
		}
	}
	if err := s.InsertResults(rows); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RollupResults(time.UnixMilli(now).Add(RollupLag), nil); err != nil {
		t.Fatal(err)
	}
	if up, _, _ := s.Uptime(m.ID, h); up != 177.0/180 {
		t.Fatalf("before: uptime %v, want %v", up, 177.0/180)
	}
	tx, err := s.db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.execScript(migrations[18-1].text(s.db.driver)); err != nil { // migration 18
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	// Only the down round (two failed rows) still counts.
	if up, _, _ := s.Uptime(m.ID, h); up != 178.0/180 {
		t.Fatalf("after: uptime %v, want %v", up, 178.0/180)
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM results WHERE mon_state = 2`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("down rows: %d %v", n, err)
	}
}

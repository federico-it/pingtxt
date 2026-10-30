package hub

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestFirstRunSpread(t *testing.T) {
	perSec := map[int64]int{}
	for id := int64(1); id <= 6000; id++ {
		d := firstRunOffset(&store.Monitor{ID: id, IntervalSec: 60})
		if d < 0 || d >= time.Minute {
			t.Fatalf("offset %s out of the interval", d)
		}
		perSec[int64(d/time.Second)]++
	}
	if len(perSec) != 60 {
		t.Fatalf("want checks in all 60 seconds, got %d", len(perSec))
	}
	for s, n := range perSec {
		if n > 200 { // 100 on average
			t.Fatalf("second %d has %d checks", s, n)
		}
	}
	if d := firstRunOffset(&store.Monitor{ID: 7, IntervalSec: 3600}); d >= maxFirstRunSpread {
		t.Fatalf("first run must be within %s, got %s", maxFirstRunSpread, d)
	}
}

func TestBusyMonitorIsSkipped(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 10, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	now := time.Now()
	h.tick(context.Background(), now.Add(time.Second))
	h.tick(context.Background(), now.Add(12*time.Second)) // first check still queued
	if n := len(h.localQueue); n != 1 {
		t.Fatalf("want 1 queued check, got %d", n)
	}
	if n := h.skipped.Load(); n != 1 {
		t.Fatalf("want 1 skipped check, got %d", n)
	}
	<-h.localQueue
	h.setIdle(m.ID)
	h.tick(context.Background(), now.Add(23*time.Second))
	if n := len(h.localQueue); n != 1 {
		t.Fatalf("want a new check once idle, got %d", n)
	}
}

func TestNextRunDoesNotDrift(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	h.mu.Lock()
	start := h.monitors[m.ID].next
	h.mu.Unlock()
	// The tick runs late (900ms): the next run stays on the 60s grid.
	h.tick(context.Background(), start.Add(900*time.Millisecond))
	h.mu.Lock()
	next := h.monitors[m.ID].next
	h.mu.Unlock()
	if got := next.Sub(start); got != time.Minute {
		t.Fatalf("want next run 60s after the slot, got %s", got)
	}
}

// The in-memory dashboard stats must match what the database returns.
func TestStatsMatchDatabase(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	now := time.Now()
	for i := 0; i < 2000; i++ {
		at := now.Add(-time.Duration(i) * 47 * time.Second)
		r := check.Result{MonitorID: m.ID, OK: i%7 != 0, LatencyMs: float64(10 + i%13), At: at.UnixMilli()}
		h.mu.Lock()
		h.monitors[m.ID].inMaint = i%11 == 0
		h.mu.Unlock()
		h.HandleResult(LocalLocation, r)
	}
	verify := func(name string, s *monitorSummary) {
		t.Helper()
		size := statSize
		from := s.Bars[0].T
		buckets, err := st.Buckets(m.ID, from, now.UnixMilli()+size, size, false)
		if err != nil {
			t.Fatal(err)
		}
		byStart := map[int64]store.Bucket{}
		total, ok := 0, 0
		for _, b := range buckets {
			byStart[b.Start] = b
			total += b.Total
			ok += b.OK
		}
		if len(s.Bars) != barCount {
			t.Fatalf("%s: want %d bars, got %d", name, barCount, len(s.Bars))
		}
		for _, b := range s.Bars {
			db := byStart[b.T]
			if b.Total != db.Total || b.OK != db.OK || b.Maint != db.Maint || abs(b.AvgMs-db.AvgMs) > 1e-6 {
				t.Fatalf("%s: bar %d: memory %+v, database %+v", name, b.T, b, db)
			}
		}
		if want := float64(ok) / float64(total); abs(s.Uptime24h-want) > 1e-9 {
			t.Fatalf("%s: uptime %f, want %f", name, s.Uptime24h, want)
		}
		if s.Last == nil || s.Last.TS != now.UnixMilli() {
			t.Fatalf("%s: wrong last result %+v", name, s.Last)
		}
	}
	mon, _ := st.Monitor(m.ID)
	verify("live", h.summary(mon, nil))

	// After a restart the stats are rebuilt from the database.
	h2 := New(h.cfg, st, h.log)
	if err := h2.reloadAll(); err != nil {
		t.Fatal(err)
	}
	h2.warmStats()
	verify("reloaded", h2.summary(mon, nil))
	// Results after the restart are added on top of the stored ones.
	h2.HandleResult(LocalLocation, check.Result{MonitorID: m.ID, OK: false, At: time.Now().UnixMilli()})
	s := h2.summary(mon, nil)
	if b := s.Bars[len(s.Bars)-1]; b.Total == 0 {
		t.Fatal("live result missing")
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestResultWriterFlushesOnStop(t *testing.T) {
	_, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	w := newResultWriter(st, newTestLogger())
	go w.run()
	for i := 0; i < 2500; i++ {
		w.add(&store.ResultRow{MonitorID: m.ID, Location: LocalLocation, TS: int64(i), OK: true})
	}
	w.stop()
	w.add(&store.ResultRow{MonitorID: m.ID, Location: LocalLocation, TS: 9999, OK: true}) // after stop: direct insert
	var n int
	if err := st.DB().QueryRow(`SELECT COUNT(*) FROM results`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2501 {
		t.Fatalf("want 2501 rows, got %d", n)
	}
}

func newTestLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestLargeJSONIsGzipped(t *testing.T) {
	h, st := newTestHub(t)
	for i := range 200 {
		m := &store.Monitor{Name: fmt.Sprintf("m%03d", i), Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{LocalLocation}}
		if err := st.CreateMonitor(m); err != nil {
			t.Fatal(err)
		}
		h.MonitorChanged(m.ID)
	}
	handler := gzipAPI(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.listMonitors(w, r, nil) }))
	req := httptest.NewRequest("GET", "/api/monitors", nil)
	req.Header.Set("Accept-Encoding", "gzip, br")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Content-Encoding") != "gzip" {
		t.Fatal("want a gzip response")
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	var out []monitorSummary
	if err := json.NewDecoder(zr).Decode(&out); err != nil || len(out) != 200 {
		t.Fatalf("decode: %v (%d monitors)", err, len(out))
	}
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest("GET", "/api/monitors", nil))
	if rec.Header().Get("Content-Encoding") != "" {
		t.Fatal("no gzip without Accept-Encoding")
	}
}

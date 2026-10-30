//go:build loadtest

// Load tests for the scheduler and the store. They are slow and excluded from
// the normal test run:
//
//	go test -tags loadtest -run TestLoad -v -timeout 30m ./internal/hub
//
// Knobs (env): LOAD_MONITORS (5000), LOAD_INTERVAL (60s), LOAD_DURATION (3m),
// LOAD_LATENCY (50ms), LOAD_SLOW_PCT (0: share of targets that hang until the
// timeout), LOAD_TIMEOUT (10s), LOAD_HISTORY (24h of results for the dashboard
// test).
package hub

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func envInt(k string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func envDur(k string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(k)); err == nil {
		return v
	}
	return def
}

func pct(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	return xs[min(len(xs)-1, int(float64(len(xs))*p))]
}

func loadHub(t *testing.T) (*Hub, *store.Store) {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "load.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return New(Config{LocalName: "hub"}, st, slog.New(slog.NewTextHandler(io.Discard, nil))), st
}

// TestLoadScheduler runs N HTTP monitors against a local fake target and
// reports throughput, check spacing and goroutine growth.
func TestLoadScheduler(t *testing.T) {
	n := envInt("LOAD_MONITORS", 5000)
	interval := envDur("LOAD_INTERVAL", time.Minute)
	dur := envDur("LOAD_DURATION", 3*time.Minute)
	lat := envDur("LOAD_LATENCY", 50*time.Millisecond)
	slowPct := envInt("LOAD_SLOW_PCT", 0)
	timeout := envDur("LOAD_TIMEOUT", 10*time.Second)

	var hits, inflight, peakInflight atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		cur := inflight.Add(1)
		defer inflight.Add(-1)
		for {
			p := peakInflight.Load()
			if cur <= p || peakInflight.CompareAndSwap(p, cur) {
				break
			}
		}
		d := lat
		if r.URL.Query().Get("slow") == "1" {
			d = timeout + 5*time.Second
		}
		select {
		case <-time.After(d):
		case <-r.Context().Done():
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	h, st := loadHub(t)
	for i := 0; i < n; i++ {
		target := fmt.Sprintf("%s/m/%d", srv.URL, i)
		if slowPct > 0 && i%100 < slowPct {
			target += "?slow=1"
		}
		m := &store.Monitor{Name: fmt.Sprintf("m%05d", i), Type: "http", Target: target, IntervalSec: int(interval / time.Second),
			TimeoutMs: int(timeout / time.Millisecond), Retries: 1, Method: "GET", Locations: []string{LocalLocation}}
		if err := st.CreateMonitor(m); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), dur)
	defer cancel()
	start := time.Now()
	runDone := make(chan struct{})
	go func() {
		defer close(runDone)
		if err := h.Run(ctx); err != nil {
			t.Error(err)
		}
	}()

	var peakG, peakSem int
	sec := time.NewTicker(time.Second)
	defer sec.Stop()
	lastHits := int64(0)
	var perSec []int64
loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case <-sec.C:
			g := runtime.NumGoroutine()
			peakG = max(peakG, g)
			peakSem = max(peakSem, len(h.localQueue))
			cur := hits.Load()
			perSec = append(perSec, cur-lastHits)
			lastHits = cur
			if len(perSec)%10 == 0 {
				t.Logf("t=%3ds goroutines=%6d queue=%4d hits/s=%4d target-inflight=%d", len(perSec), g, len(h.localQueue), perSec[len(perSec)-1], inflight.Load())
			}
		}
	}
	<-runDone
	elapsed := time.Since(start)
	time.Sleep(200 * time.Millisecond)

	// Gaps between consecutive results of the same monitor, from the store.
	gaps := map[int64][]int64{}
	rows, err := st.DB().Query(`SELECT monitor_id, ts FROM results ORDER BY monitor_id, ts`)
	if err != nil {
		t.Fatal(err)
	}
	last := map[int64]int64{}
	total, checked := 0, map[int64]bool{}
	for rows.Next() {
		var id, ts int64
		_ = rows.Scan(&id, &ts)
		total++
		checked[id] = true
		if p, ok := last[id]; ok {
			gaps[id] = append(gaps[id], ts-p)
		}
		last[id] = ts
	}
	rows.Close()
	var all []float64
	for _, g := range gaps {
		for _, x := range g {
			all = append(all, float64(x)/1000)
		}
	}
	slices.Sort(all)
	slices.Sort(perSec)
	expected := float64(n) * elapsed.Seconds() / interval.Seconds()
	t.Logf("monitors=%d interval=%s run=%s latency=%s slow=%d%%", n, interval, elapsed.Round(time.Second), lat, slowPct)
	t.Logf("results stored=%d (ideal≈%.0f, %.0f%%) monitors checked at least once=%d/%d", total, expected, 100*float64(total)/expected, len(checked), n)
	t.Logf("target hits=%d peak concurrent at target=%d", hits.Load(), peakInflight.Load())
	t.Logf("hits/s p50=%d p99=%d max=%d", perSec[len(perSec)/2], perSec[min(len(perSec)-1, len(perSec)*99/100)], perSec[len(perSec)-1])
	t.Logf("gap between checks (s): p50=%.1f p99=%.1f max=%.1f (want %.0f)", pct(all, .5), pct(all, .99), pct(all, 1), interval.Seconds())
	t.Logf("peak goroutines=%d peak local queue=%d/%d skipped=%d", peakG, peakSem, cap(h.localQueue), h.skipped.Load())
}

// TestLoadDashboard fills the results table with LOAD_HISTORY of data for N
// monitors, then times GET /api/monitors while results keep being written.
func TestLoadDashboard(t *testing.T) {
	n := envInt("LOAD_MONITORS", 5000)
	interval := envDur("LOAD_INTERVAL", time.Minute)
	history := envDur("LOAD_HISTORY", 24*time.Hour)
	h, st := loadHub(t)
	ids := make([]int64, n)
	for i := range n {
		m := &store.Monitor{Name: fmt.Sprintf("m%05d", i), Type: "http", Target: "http://127.0.0.1/", IntervalSec: int(interval / time.Second), Retries: 1, Locations: []string{LocalLocation}}
		if err := st.CreateMonitor(m); err != nil {
			t.Fatal(err)
		}
		ids[i] = m.ID
	}
	now := time.Now().UnixMilli()
	perMon := int(history / interval)
	t0 := time.Now()
	db := st.DB()
	tx, _ := db.Begin()
	stmt, _ := tx.Prepare(`INSERT INTO results (monitor_id, location, ts, ok, latency_ms) VALUES (?, 'local', ?, 1, 42)`)
	rowsN := 0
	for k := range perMon {
		ts := now - history.Milliseconds() + int64(k)*interval.Milliseconds()
		for i, id := range ids {
			if _, err := stmt.Exec(id, ts+int64(i)); err != nil {
				t.Fatal(err)
			}
			rowsN++
			if rowsN%200000 == 0 {
				stmt.Close()
				if err := tx.Commit(); err != nil {
					t.Fatal(err)
				}
				tx, _ = db.Begin()
				stmt, _ = tx.Prepare(`INSERT INTO results (monitor_id, location, ts, ok, latency_ms) VALUES (?, 'local', ?, 1, 42)`)
			}
		}
	}
	stmt.Close()
	_ = tx.Commit()
	var pages, psize int64
	_ = db.QueryRow(`PRAGMA page_count`).Scan(&pages)
	_ = db.QueryRow(`PRAGMA page_size`).Scan(&psize)
	t.Logf("preloaded %d results (%s of history) in %s, db size %.0f MB", rowsN, history, time.Since(t0).Round(time.Second), float64(pages*psize)/1e6)
	t1 := time.Now()
	if err := h.reloadAll(); err != nil {
		t.Fatal(err)
	}
	t.Logf("startup load of monitors: %s", time.Since(t1).Round(time.Millisecond))
	t1 = time.Now()
	h.warmStats()
	t.Logf("background load of dashboard stats: %s", time.Since(t1).Round(time.Millisecond))
	t1 = time.Now()
	if n, err := st.PruneResults(now - history.Milliseconds() + time.Hour.Milliseconds()); err == nil {
		t.Logf("pruned the oldest hour (%d rows) in %s", n, time.Since(t1).Round(time.Millisecond))
	}

	// Writer: the expected result rate (n per interval) through HandleResult.
	var wmu sync.Mutex
	var wlat []float64
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		rate := float64(n) / interval.Seconds()
		tk := time.NewTicker(time.Duration(float64(time.Second) / rate))
		defer tk.Stop()
		i := 0
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				t1 := time.Now()
				h.HandleResult(LocalLocation, result(ids[i%n], true))
				d := time.Since(t1)
				wmu.Lock()
				wlat = append(wlat, float64(d.Microseconds())/1000)
				wmu.Unlock()
				i++
			}
		}
	}()
	time.Sleep(2 * time.Second)
	wmu.Lock()
	base := slices.Clone(wlat)
	wlat = nil
	wmu.Unlock()

	for i := range 3 {
		rec := httptest.NewRecorder()
		t1 := time.Now()
		h.listMonitors(rec, httptest.NewRequest("GET", "/api/monitors", nil), nil)
		d := time.Since(t1)
		var zb bytes.Buffer
		zw, _ := gzip.NewWriterLevel(&zb, gzip.BestSpeed)
		_, _ = zw.Write(rec.Body.Bytes())
		_ = zw.Close()
		t.Logf("GET /api/monitors #%d: %s, %d KB (%d KB gzipped), status %d", i+1, d.Round(time.Millisecond), rec.Body.Len()/1024, zb.Len()/1024, rec.Code)
	}
	close(stop)
	wg.Wait()
	slices.Sort(base)
	slices.Sort(wlat)
	t.Logf("HandleResult latency idle (ms): p50=%.2f p99=%.2f max=%.2f", pct(base, .5), pct(base, .99), pct(base, 1))
	t.Logf("HandleResult latency during dashboard (ms): p50=%.2f p99=%.2f max=%.2f (n=%d)", pct(wlat, .5), pct(wlat, .99), pct(wlat, 1), len(wlat))
}

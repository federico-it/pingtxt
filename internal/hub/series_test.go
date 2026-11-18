package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestMonitorSeriesWindowAndStats(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
	cookie := login(t, h, st)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	var rows []*store.ResultRow
	for i := range 120 { // two hours, one check a minute: 10 ms the first hour, 20 ms the last
		lat := 20.0
		if i >= 60 {
			lat = 10
		}
		rows = append(rows, &store.ResultRow{MonitorID: m.ID, Location: "local", TS: now - int64(i)*60_000 - 1, OK: true, LatencyMs: lat})
	}
	if err := st.InsertResults(rows); err != nil {
		t.Fatal(err)
	}
	get := func(q string, out any) int {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/monitors/"+itoa(m.ID)+"/series?"+q, nil)
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if out != nil {
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s: %s", q, b)
			}
		}
		return res.StatusCode
	}
	var got struct {
		From, To, Size int64
		Stats          struct{ Current, Previous seriesStats }
	}
	if c := get("range=1h", &got); c != 200 {
		t.Fatalf("range=1h: %d", c)
	}
	cur, prev := got.Stats.Current, got.Stats.Previous
	if cur.Percentiles.P50 != 20 || prev.Percentiles.P50 != 10 || cur.Uptime != 1 || prev.Percentiles.N == 0 {
		t.Fatalf("stats: %+v / %+v", cur, prev)
	}
	// Custom window: 30 minutes, an hour ago.
	start, end := now-90*60_000, now-60*60_000
	if c := get("start="+itoa(start)+"&end="+itoa(end), &got); c != 200 || got.To != end || got.From > start || got.Size != 20_000 {
		t.Fatalf("window: %d %+v", c, got)
	}
	if got.Stats.Current.Percentiles.P99 != 10 {
		t.Fatalf("window stats: %+v", got.Stats.Current)
	}
	// Uptime bars of each preset.
	var bars struct{ Bars []bar }
	for q, n := range map[string]int{"range=1h": 60, "range=24h": 48, "range=7d": 42, "range=30d": 60} {
		if c := get(q, &bars); c != 200 || len(bars.Bars) != n {
			t.Fatalf("%s: %d, %d bars, want %d", q, c, len(bars.Bars), n)
		}
		total, ok := 0, 0
		for i, b := range bars.Bars {
			if i > 0 && b.T <= bars.Bars[i-1].T {
				t.Fatalf("%s: bars out of order", q)
			}
			total += b.Total
			ok += b.OK
		}
		if q != "range=1h" && (total != 120 || ok != 120) {
			t.Fatalf("%s: %d checks, %d ok, want 120", q, total, ok)
		}
	}
	for _, q := range []string{"start=1&end=2", "start=x&end=" + itoa(end), "start=" + itoa(start)} {
		if c := get(q, nil); c != 400 {
			t.Fatalf("%s: %d, want 400", q, c)
		}
	}
}

func TestListMonitorsPeriod(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
	cookie := login(t, h, st)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	var rows []*store.ResultRow
	for i := range 120 { // two hours, one check a minute, down the last 30 minutes
		r := &store.ResultRow{MonitorID: m.ID, Location: "local", TS: now - int64(i)*60_000 - 1, OK: i >= 30, LatencyMs: 10}
		if i < 30 {
			r.MonState = store.MonDown
		}
		rows = append(rows, r)
	}
	if err := st.InsertResults(rows); err != nil {
		t.Fatal(err)
	}
	get := func(q string) (int, []monitorSummary) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+"/api/monitors?"+q, nil)
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		var out []monitorSummary
		if res.StatusCode == 200 {
			if err := json.Unmarshal(b, &out); err != nil {
				t.Fatalf("%s: %s", q, b)
			}
		}
		return res.StatusCode, out
	}
	if c, out := get(""); c != 200 || len(out) != 1 || out[0].Period != nil {
		t.Fatalf("no range: %d %+v", c, out)
	}
	if c, _ := get("range=2y"); c != 400 {
		t.Fatalf("range=2y: %d, want 400", c)
	}
	for q, want := range map[string]struct {
		bars   int
		checks int
		uptime float64
	}{
		"range=1h":  {60, 60, 0.5},
		"range=7d":  {42, 120, 0.75},
		"range=30d": {60, 120, 0.75},
	} {
		c, out := get(q)
		if c != 200 || len(out) != 1 || out[0].Period == nil {
			t.Fatalf("%s: %d %+v", q, c, out)
		}
		p := out[0].Period
		total := 0
		for _, b := range p.Bars {
			total += b.Total
		}
		// The 1h window may hold one check more or less at its edges.
		if len(p.Bars) != want.bars || total < want.checks-1 || total > want.checks+1 || p.AvgMs != 10 || p.Range != q[6:] {
			t.Fatalf("%s: %d bars, %d checks, %+v", q, len(p.Bars), total, p)
		}
		if p.Uptime < want.uptime-0.02 || p.Uptime > want.uptime+0.02 {
			t.Fatalf("%s: uptime %v, want %v", q, p.Uptime, want.uptime)
		}
	}
	// 24h comes from the in-memory stats, like uptime_24h.
	if c, out := get("range=24h"); c != 200 || out[0].Period == nil || out[0].Period.Uptime != out[0].Uptime24h || len(out[0].Period.Bars) != len(out[0].Bars) {
		t.Fatalf("range=24h: %d %+v", c, out)
	}
}

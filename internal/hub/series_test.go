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
	for _, q := range []string{"start=1&end=2", "start=x&end=" + itoa(end), "start=" + itoa(start)} {
		if c := get(q, nil); c != 400 {
			t.Fatalf("%s: %d, want 400", q, c)
		}
	}
}

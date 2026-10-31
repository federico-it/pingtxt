package hub

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// openTestStore opens an empty store: SQLite in dir, or a scratch database
// on the server of PINGTXT_TEST_DB_URL (postgres:// or mysql://).
func openTestStore(t *testing.T, dir string) *store.Store {
	t.Helper()
	if u := os.Getenv("PINGTXT_TEST_DB_URL"); u != "" {
		st, drop, err := store.OpenScratch(u)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(drop)
		return st
	}
	st, err := store.Open(filepath.Join(dir, "pingtxt.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return st
}

// sqliteOnly skips tests of features that only exist with SQLite (file backups).
func sqliteOnly(t *testing.T, st *store.Store) {
	t.Helper()
	if st.Driver() != store.DriverSQLite {
		t.Skip("SQLite only")
	}
}

func newTestHub(t *testing.T) (*Hub, *store.Store) {
	t.Helper()
	st := openTestStore(t, t.TempDir())
	h := New(Config{LocalName: "hub"}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := h.reloadAll(); err != nil {
		t.Fatal(err)
	}
	return h, st
}

func result(id int64, ok bool) check.Result {
	return check.Result{MonitorID: id, OK: ok, LatencyMs: 1, At: time.Now().UnixMilli()}
}

func status(t *testing.T, st *store.Store, id int64) string {
	t.Helper()
	m, err := st.Monitor(id)
	if err != nil {
		t.Fatal(err)
	}
	return m.Status
}

func TestRetriesAndIncidents(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 2, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)

	h.HandleResult(LocalLocation, result(m.ID, true))
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up, got %s", s)
	}
	h.HandleResult(LocalLocation, result(m.ID, false))
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("one failure with retries=2 should stay up, got %s", s)
	}
	h.HandleResult(LocalLocation, result(m.ID, false))
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("want down, got %s", s)
	}
	inc, _ := st.ListIncidents(m.ID, 10)
	if len(inc) != 1 || inc[0].ResolvedAt != 0 {
		t.Fatalf("want 1 open incident, got %+v", inc)
	}
	h.HandleResult(LocalLocation, result(m.ID, true))
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up after recovery, got %s", s)
	}
	inc, _ = st.ListIncidents(m.ID, 10)
	if inc[0].ResolvedAt == 0 {
		t.Fatal("incident should be resolved")
	}
}

func TestMajorityAcrossLocations(t *testing.T) {
	h, st := newTestHub(t)
	var locs []string
	for _, n := range []string{"a", "b"} {
		a, err := st.CreateAgent(n, n, n)
		if err != nil {
			t.Fatal(err)
		}
		h.agents[a.ID] = &agentConn{agent: a, done: make(chan struct{})}
		locs = append(locs, AgentLocation(a.ID))
	}
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: append(locs, LocalLocation)}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)

	h.HandleResult(LocalLocation, result(m.ID, true))
	h.HandleResult(locs[0], result(m.ID, true))
	h.HandleResult(locs[1], result(m.ID, false))
	// 33% failing is below the default 35% threshold.
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("1/3 failing: want up, got %s", s)
	}
	// A lower threshold on the monitor makes it degraded.
	m.DegradedPct = 30
	if err := st.UpdateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	h.HandleResult(locs[1], result(m.ID, false))
	if s := status(t, st, m.ID); s != StatusDegraded {
		t.Fatalf("1/3 failing with 30%% threshold: want degraded, got %s", s)
	}
	h.HandleResult(locs[0], result(m.ID, false))
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("2/3 failing: want down, got %s", s)
	}
	// Results from a location not assigned to the monitor are ignored.
	h.HandleResult("agent:999", result(m.ID, true))
	rows, _ := st.RecentResults(m.ID, 100)
	if len(rows) != 5 {
		t.Fatalf("want 5 stored results, got %d", len(rows))
	}
}

func TestClassify(t *testing.T) {
	cases := []struct {
		n, down, pct int
		want         string
	}{
		{1, 0, 35, StatusUp},
		{1, 1, 35, StatusDown},
		{2, 1, 35, StatusDegraded}, // 50% is not a majority
		{3, 1, 35, StatusUp},       // 33%
		{3, 2, 35, StatusDown},
		{4, 1, 35, StatusUp},       // 25%
		{4, 2, 35, StatusDegraded}, // 50%
		{10, 3, 35, StatusUp},
		{10, 4, 35, StatusDegraded},
		{10, 6, 35, StatusDown},
		{20, 7, 35, StatusDegraded}, // exactly 35%
		{3, 1, 30, StatusDegraded},
		{4, 1, 25, StatusDegraded},
		{0, 0, 35, StatusUp},
	}
	for _, c := range cases {
		if got := classify(c.n, c.down, c.pct); got != c.want {
			t.Errorf("classify(%d, %d, %d) = %s, want %s", c.n, c.down, c.pct, got, c.want)
		}
	}
}

func TestDegradedPctSetting(t *testing.T) {
	g := GeneralSettings{}
	g.normalize()
	if g.DegradedPct != defaultDegradedPct {
		t.Fatalf("default: got %d", g.DegradedPct)
	}
	g.DegradedPct = 90
	g.normalize()
	if g.DegradedPct != maxDegradedPct {
		t.Fatalf("clamp: got %d", g.DegradedPct)
	}
}

package hub

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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
		n, down, slow, pct int
		want               string
	}{
		{1, 0, 0, 35, StatusUp},
		{1, 1, 0, 35, StatusDown},
		{2, 1, 0, 35, StatusDegraded}, // 50% is not a majority
		{3, 1, 0, 35, StatusUp},       // 33%
		{3, 2, 0, 35, StatusDown},
		{4, 1, 0, 35, StatusUp},       // 25%
		{4, 2, 0, 35, StatusDegraded}, // 50%
		{10, 3, 0, 35, StatusUp},
		{10, 4, 0, 35, StatusDegraded},
		{10, 6, 0, 35, StatusDown},
		{20, 7, 0, 35, StatusDegraded}, // exactly 35%
		{3, 1, 0, 30, StatusDegraded},
		{4, 1, 0, 25, StatusDegraded},
		{0, 0, 0, 35, StatusUp},
		{1, 0, 1, 35, StatusDegraded}, // slow is never down
		{3, 0, 3, 35, StatusDegraded},
		{3, 0, 1, 35, StatusUp},       // one far slow agent of three
		{3, 1, 1, 35, StatusDegraded}, // failing and slow add up
		{3, 2, 1, 35, StatusDown},
	}
	for _, c := range cases {
		if got := classify(c.n, c.down, c.slow, c.pct); got != c.want {
			t.Errorf("classify(%d, %d, %d, %d) = %s, want %s", c.n, c.down, c.slow, c.pct, got, c.want)
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

func TestResponseRulesNeedAnUpdatedAgent(t *testing.T) {
	h, st := newTestHub(t)
	a, err := st.CreateAgent("old", "old", "old")
	if err != nil {
		t.Fatal(err)
	}
	h.agents[a.ID] = &agentConn{agent: a, done: make(chan struct{})}
	loc := AgentLocation(a.ID)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", Retries: 1, Locations: []string{loc},
		Assertions: []check.Assertion{{Path: "status", Op: check.OpEquals, Value: "ready"}}}
	if err := h.validateMonitor(m); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Monitor(m.ID); len(got.Assertions) != 1 || got.Assertions[0].Value != "ready" {
		t.Fatalf("assertions not stored: %+v", got.Assertions)
	}
	h.MonitorChanged(m.ID)

	// An old agent reports up without checking the rules.
	h.HandleResult(loc, result(m.ID, true))
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("want down, got %s", s)
	}
	rows, _ := st.RecentResults(m.ID, 1)
	if len(rows) != 1 || rows[0].Message != "this agent cannot check response rules: update it" {
		t.Fatalf("unexpected result: %+v", rows)
	}
	r := result(m.ID, true)
	r.Asserted = 1
	h.HandleResult(loc, r)
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up, got %s", s)
	}
}

func TestIPFamilyNeedsAnUpdatedAgent(t *testing.T) {
	h, st := newTestHub(t)
	a, err := st.CreateAgent("old", "old", "old")
	if err != nil {
		t.Fatal(err)
	}
	h.agents[a.ID] = &agentConn{agent: a, done: make(chan struct{})}
	loc := AgentLocation(a.ID)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", Retries: 1, Locations: []string{loc}, IPFamily: check.IPv6}
	if err := h.validateMonitor(m); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.Monitor(m.ID); got.IPFamily != check.IPv6 {
		t.Fatalf("ip_family not stored: %d", got.IPFamily)
	}
	if f := h.familyAtLocked(m, loc, ipEither); f != check.IPv6 {
		t.Fatalf("job ip_family %d", f)
	}
	h.MonitorChanged(m.ID)

	// An old agent reports up without knowing the family.
	h.HandleResult(loc, result(m.ID, true))
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("want down, got %s", s)
	}
	rows, _ := st.RecentResults(m.ID, 1)
	if len(rows) != 1 || rows[0].Message != "this agent cannot limit checks to IPv4 or IPv6: update it" {
		t.Fatalf("unexpected result: %+v", rows)
	}
	r := result(m.ID, true)
	r.IPFamily = check.IPv6
	h.HandleResult(loc, r)
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up, got %s", s)
	}
}

func TestValidateResponseRules(t *testing.T) {
	h, _ := newTestHub(t)
	bad := &store.Monitor{Name: "m", Type: "http", Target: "http://x",
		Assertions: []check.Assertion{{Path: "a", Op: check.OpLess, Value: "x"}}}
	if err := h.validateMonitor(bad); err == nil {
		t.Fatal("expected an error for a non-numeric bound")
	}
	tcp := &store.Monitor{Name: "m", Type: "tcp", Target: "x:1",
		Assertions: []check.Assertion{{Path: "a", Op: check.OpExists}}}
	if err := h.validateMonitor(tcp); err != nil || len(tcp.Assertions) != 0 {
		t.Fatalf("tcp monitors drop rules: %v %+v", err, tcp.Assertions)
	}
}

// Uptime follows the monitor status: one failing location among three
// leaves the monitor up (or degraded) and does not lower it; rounds where
// the monitor is down do.
func TestUptimeFollowsMonitorStatus(t *testing.T) {
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
	locs = append(locs, LocalLocation)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: locs}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	round := func(failing int) {
		for i, l := range locs {
			h.HandleResult(l, result(m.ID, i >= failing))
		}
	}
	for range 9 {
		round(1) // agent a always fails
	}
	if up, _, err := st.Uptime(m.ID, 0); err != nil || up != 1 {
		t.Fatalf("one failing location: uptime %v %v, want 1", up, err)
	}
	s := h.summary(m, nil)
	if s.Uptime24h != 1 {
		t.Fatalf("one failing location: dashboard uptime %v, want 1", s.Uptime24h)
	}
	// Two of three failing: the monitor is down from the second answer on.
	round(2)
	if up, _, _ := st.Uptime(m.ID, 0); up != 28.0/30 {
		t.Fatalf("after a down round: uptime %v, want %v", up, 28.0/30)
	}
	s = h.summary(m, nil)
	total, ok := 0, 0
	for _, b := range s.Bars {
		total += b.Total
		ok += b.OK
	}
	if total != 30 || ok != 28 || s.Uptime24h != 28.0/30 {
		t.Fatalf("bars: %d/%d ok, uptime %v", ok, total, s.Uptime24h)
	}
}

func TestIPv6Locations(t *testing.T) {
	h, st := newTestHub(t)
	noV6, _ := st.CreateAgent("ams", "Amsterdam", "h1")
	v6, _ := st.CreateAgent("fra", "Frankfurt", "h2")
	old, _ := st.CreateAgent("old", "old", "h3")
	h.agents[noV6.ID] = &agentConn{agent: noV6, done: make(chan struct{})}
	h.agents[noV6.ID].ipv6.Store(check.IPv6No)
	h.agents[v6.ID] = &agentConn{agent: v6, done: make(chan struct{})}
	h.agents[v6.ID].ipv6.Store(check.IPv6Yes)
	h.agents[old.ID] = &agentConn{agent: old, done: make(chan struct{})}
	h.localIPv6.Set(check.IPv6No)
	at := func(fam, def int, loc string) int {
		return h.familyAtLocked(&store.Monitor{Type: "http", IPFamily: fam}, loc, def)
	}
	local, yes, unknown := LocalLocation, AgentLocation(v6.ID), AgentLocation(old.ID)
	cases := []struct {
		fam, def int
		loc      string
		want     int
	}{
		{ipEither, ipEither, local, check.IPv4}, // no IPv6: IPv4
		{ipEither, ipEither, yes, check.IPAny},
		{ipEither, ipEither, unknown, check.IPAny},
		{ipDefault, ipEither, yes, check.IPAny},
		{ipDefault, check.IPBoth, yes, check.IPBoth},
		{ipDefault, check.IPBoth, local, check.IPv4}, // the default falls back
		{ipDefault, check.IPv6, local, check.IPv4},
		{check.IPv6, ipEither, local, check.IPv6}, // chosen on the monitor: kept
		{check.IPBoth, ipEither, yes, check.IPBoth},
		{check.IPv4, check.IPBoth, yes, check.IPv4},
	}
	for _, c := range cases {
		if got := at(c.fam, c.def, c.loc); got != c.want {
			t.Errorf("family %d default %d at %s: %d, want %d", c.fam, c.def, c.loc, got, c.want)
		}
	}
	if f := h.familyAtLocked(&store.Monitor{Type: "docker"}, yes, check.IPv6); f != check.IPAny {
		t.Errorf("docker: %d", f)
	}

	// IPv6 only and IPv4 and IPv6 are refused where IPv6 does not work.
	m := &store.Monitor{Name: "m", Type: "ping", Target: "example.com", IPFamily: check.IPv6,
		Locations: []string{yes, unknown}}
	if err := h.checkIPv6Locations(m); err != nil {
		t.Errorf("ipv6 and unknown locations: %v", err)
	}
	m.Locations = append(m.Locations, AgentLocation(noV6.ID), local)
	for _, f := range []int{check.IPv6, check.IPBoth} {
		m.IPFamily = f
		err := h.checkIPv6Locations(m)
		if err == nil || !strings.Contains(err.Error(), "ams · Amsterdam") || !strings.Contains(err.Error(), "hub") {
			t.Errorf("family %d: want refusal naming both, got %v", f, err)
		}
	}
	for _, f := range []int{ipDefault, ipEither, check.IPv4} {
		m.IPFamily = f
		if err := h.checkIPv6Locations(m); err != nil {
			t.Errorf("family %d: %v", f, err)
		}
	}

	// An offline agent keeps its last reported state.
	_ = st.SetAgentIPv6(noV6.ID, int(check.IPv6No))
	delete(h.agents, noV6.ID)
	if s := h.locationIPv6(AgentLocation(noV6.ID)); s != check.IPv6No {
		t.Errorf("offline agent: %d", s)
	}
}

func TestBothFamiliesNeedAnUpdatedAgent(t *testing.T) {
	h, st := newTestHub(t)
	a, _ := st.CreateAgent("a", "a", "a")
	h.agents[a.ID] = &agentConn{agent: a, done: make(chan struct{})}
	loc := AgentLocation(a.ID)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", Retries: 1, Locations: []string{loc}, IPFamily: check.IPBoth}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	// An agent from before IPBoth echoes the family but checked either one.
	r := result(m.ID, true)
	r.IPFamily = check.IPBoth
	h.HandleResult(loc, r)
	if s := status(t, st, m.ID); s != StatusDown {
		t.Fatalf("want down, got %s", s)
	}
	r.Both = true
	h.HandleResult(loc, r)
	if s := status(t, st, m.ID); s != StatusUp {
		t.Fatalf("want up, got %s", s)
	}
}

func TestValidateIPFamilies(t *testing.T) {
	h, _ := newTestHub(t)
	for _, f := range []int{0, 1, 4, 6, 46} {
		m := &store.Monitor{Name: "x", Type: "http", Target: "https://example.com", IPFamily: f}
		if err := h.validateMonitor(m); err != nil {
			t.Errorf("%d: %v", f, err)
		}
	}
	for _, f := range []int{2, 5, 64} {
		m := &store.Monitor{Name: "x", Type: "http", Target: "https://example.com", IPFamily: f}
		if err := h.validateMonitor(m); err == nil {
			t.Errorf("%d accepted", f)
		}
	}
	g := GeneralSettings{IPFamily: 0}
	g.normalize()
	if g.IPFamily != ipEither {
		t.Errorf("default family %d", g.IPFamily)
	}
}

func TestSlowThreshold(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, SlowMs: 500, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	send := func(ok bool, ms float64) {
		r := result(m.ID, ok)
		r.LatencyMs = ms
		h.HandleResult(LocalLocation, r)
	}
	steps := []struct {
		ok   bool
		ms   float64
		want string
	}{
		{true, 100, StatusUp},
		{true, 900, StatusUp},       // window not full yet
		{true, 900, StatusDegraded}, // median 900
		{true, 100, StatusDegraded}, // one fast check is not enough
		{true, 100, StatusUp},       // median 100
		{true, 2000, StatusUp},      // one spike changes nothing
		{false, 0, StatusDown},      // failing still wins
		{true, 2000, StatusDegraded},
	}
	for i, s := range steps {
		send(s.ok, s.ms)
		if got := status(t, st, m.ID); got != s.want {
			t.Fatalf("step %d: want %s, got %s", i, s.want, got)
		}
	}
	// Degraded counts as up for uptime.
	now := time.Now().UnixMilli()
	b, err := st.Buckets(m.ID, now-3600000, now+3600000, 7200000, false)
	if err != nil {
		t.Fatal(err)
	}
	total, ok, deg := 0, 0, 0
	for _, x := range b {
		total, ok, deg = total+x.Total, ok+x.OK, deg+x.Degraded
	}
	if total != 8 || ok != 7 || deg != 3 {
		t.Fatalf("want 8 checks, 7 counted as up, 3 degraded; got %d, %d, %d", total, ok, deg)
	}
	// Turning the threshold off makes the monitor up again.
	m.SlowMs = 0
	if err := st.UpdateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	send(true, 2000)
	if got := status(t, st, m.ID); got != StatusUp {
		t.Fatalf("threshold off: want up, got %s", got)
	}
}

func TestCheckSlowMs(t *testing.T) {
	cases := []struct {
		typ     string
		slow    int
		wantErr bool
	}{
		{"http", 0, false},
		{"http", 800, false},
		{"http", store.SlowAuto, false},
		{"http", -2, true},
		{"http", 10000, true}, // not below the timeout
		{check.TypePush, 60000, false},
		{check.TypePush, maxPushSlowMs + 1, true},
	}
	for _, c := range cases {
		m := &store.Monitor{Type: c.typ, TimeoutMs: 10000, SlowMs: c.slow}
		if err := checkSlowMs(m); (err != nil) != c.wantErr {
			t.Errorf("%s %d: err = %v", c.typ, c.slow, err)
		}
	}
}

func TestCheckPingCount(t *testing.T) {
	cases := []struct {
		typ       string
		count     int
		timeout   int
		wantCount int
		wantErr   bool
	}{
		{check.TypePing, 0, 10000, 3, false},
		{check.TypePing, 0, 500, 3, false}, // saved before the setting: kept as is
		{check.TypePing, 1, 500, 1, false},
		{check.TypePing, 10, 10000, 10, false},
		{check.TypePing, 20, 6200, 20, false},
		{check.TypePing, 20, 6199, 0, true}, // timeout too short for 20 packets
		{check.TypePing, 21, 60000, 0, true},
		{check.TypePing, -1, 10000, 0, true},
		{"http", 5, 10000, 0, false}, // dropped on other types
	}
	for _, loss := range []int{-1, 101} {
		if err := checkPingCount(&store.Monitor{Type: check.TypePing, TimeoutMs: 10000, PingLossPct: loss}); err == nil {
			t.Errorf("ping_loss_pct %d accepted", loss)
		}
	}
	for _, c := range cases {
		m := &store.Monitor{Type: c.typ, TimeoutMs: c.timeout, PingCount: c.count}
		err := checkPingCount(m)
		if (err != nil) != c.wantErr {
			t.Errorf("%s %d/%d ms: err = %v", c.typ, c.count, c.timeout, err)
		}
		if err == nil && m.PingCount != c.wantCount {
			t.Errorf("%s %d: count %d, want %d", c.typ, c.count, m.PingCount, c.wantCount)
		}
	}
}

func TestOldPingAgent(t *testing.T) {
	m := &store.Monitor{Type: check.TypePing, PingLossPct: 20}
	if !oldPingAgent(m, "agent:1", check.Result{OK: true}) {
		t.Error("old agent result accepted")
	}
	if oldPingAgent(m, "agent:1", check.Result{OK: true, PingSent: 3}) || oldPingAgent(m, LocalLocation, check.Result{OK: true}) {
		t.Error("new agent or hub result refused")
	}
	m.PingLossPct = 0
	if oldPingAgent(m, "agent:1", check.Result{OK: true}) {
		t.Error("old agent refused without a threshold")
	}
}

func TestAutoSlowLimit(t *testing.T) {
	flat := func(n int, v float64) []float64 {
		out := make([]float64, n)
		for i := range out {
			out[i] = v
		}
		return out
	}
	if got := autoSlowLimit(flat(23, 100)); got != 0 {
		t.Errorf("under 24 hours: want 0, got %v", got)
	}
	if got := autoSlowLimit(flat(24, 100)); got != 300 {
		t.Errorf("100 ms: want 3x = 300, got %v", got)
	}
	if got := autoSlowLimit(flat(24, 20)); got != 120 {
		t.Errorf("20 ms: want +100 = 120, got %v", got)
	}
	// A few slow hours do not move the median.
	v := append(flat(30, 100), flat(10, 900)...)
	if got := autoSlowLimit(v); got != 300 {
		t.Errorf("with slow hours: want 300, got %v", got)
	}
}

func TestSlowAuto(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, SlowMs: store.SlowAuto, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	// 30 hours of history at 100 ms, one check per hour.
	now := time.Now()
	hour := int64(3600000)
	start := now.UnixMilli() - now.UnixMilli()%hour - 30*hour
	var rows []*store.ResultRow
	for i := range int64(30) {
		rows = append(rows, &store.ResultRow{MonitorID: m.ID, Location: LocalLocation, TS: start + i*hour, OK: true, LatencyMs: 100})
	}
	if err := st.InsertResults(rows); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RollupResults(now, nil); err != nil {
		t.Fatal(err)
	}
	h.updateSlowLimits(now)
	if got := h.SlowLimits(m.ID)[LocalLocation]; got != 300 {
		t.Fatalf("want automatic threshold 300 ms, got %v", got)
	}
	send := func(ms float64) {
		r := result(m.ID, true)
		r.LatencyMs = ms
		h.HandleResult(LocalLocation, r)
	}
	for range 3 {
		send(250)
	}
	if got := status(t, st, m.ID); got != StatusUp {
		t.Fatalf("250 ms: want up, got %s", got)
	}
	for range 2 {
		send(400)
	}
	if got := status(t, st, m.ID); got != StatusDegraded {
		t.Fatalf("400 ms: want degraded, got %s", got)
	}
}

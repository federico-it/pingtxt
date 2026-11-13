package hub

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestServerMonitor(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	m := decode[store.Monitor](t, c.do("POST", "/api/monitors", map[string]any{
		"name": "box", "type": "server", "target": "/, /data/", "timeout_ms": 500, "ip_family": 6,
		"server_limits": map[string]any{"cpu_pct": 90, "disk_pct": 85, "load": 1.5},
	}))
	if m.ID == 0 || m.Target != "/, /data" || m.TimeoutMs != 3000 || m.IPFamily != 0 || m.ServerLimits.CPUPct != 90 || m.ServerLimits.Load != 1.5 {
		t.Fatalf("created: %+v", m)
	}
	if j := jobFor(&m); j.ServerLimits == nil || j.ServerLimits.DiskPct != 85 {
		t.Fatalf("job: %+v", j.ServerLimits)
	}
	// A PUT without server_limits keeps them.
	m = decode[store.Monitor](t, c.do("PUT", "/api/monitors/"+itoa(m.ID), map[string]any{"name": "box2"}))
	if m.ServerLimits.CPUPct != 90 {
		t.Fatalf("limits lost: %+v", m.ServerLimits)
	}
	if res := c.do("POST", "/api/monitors", map[string]any{"name": "x", "type": "server", "server_limits": map[string]any{"mem_pct": 150}}); res.StatusCode != 400 {
		t.Fatalf("150%% accepted: %d", res.StatusCode)
	}
	// Other types drop the limits.
	other := &store.Monitor{Name: "x", Type: check.TypeTCP, Target: "h:1", ServerLimits: check.ServerLimits{CPUPct: 5}}
	if err := h.validateMonitor(other); err != nil || other.ServerLimits.CPUPct != 0 {
		t.Fatalf("tcp: %+v %v", other.ServerLimits, err)
	}

	now := time.Now().UnixMilli()
	metrics := &check.ServerMetrics{CPUPct: 12, Cores: 2, Load5: 0.5, MemTotal: 100, MemUsed: 40,
		Disks: []check.DiskUsage{{Mount: "/", Used: 30, Free: 70}, {Mount: "/data", Used: 80, Free: 20}}}
	h.HandleResult(LocalLocation, check.Result{MonitorID: m.ID, OK: true, At: now - 1000, Server: metrics})
	h.HandleResult(LocalLocation, check.Result{MonitorID: m.ID, OK: true, At: now, Server: metrics})
	got := decode[ServerHistory](t, c.do("GET", "/api/monitors/"+itoa(m.ID)+"/server?hours=1", nil))
	if len(got.Locations) != 1 || got.Locations[0].At != now || got.Locations[0].Metrics == nil || len(got.Locations[0].Metrics.Disks) != 2 {
		t.Fatalf("locations: %+v", got.Locations)
	}
	if len(got.Points) != 1 || got.Points[0].DiskPct != 80 || got.Points[0].MemPct != 40 || got.Points[0].CPUPct != 12 {
		t.Fatalf("points: %+v", got.Points)
	}
	if res := c.do("GET", "/api/monitors/"+itoa(m.ID)+"/server?hours=500", nil); res.StatusCode != 400 {
		t.Fatalf("hours=500: %d", res.StatusCode)
	}
	if n, err := st.PruneServerSamples(now + 1); err != nil || n != 2 {
		t.Fatalf("prune: %d %v", n, err)
	}
}

package hub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestMetricsContent(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: `web "main"`, Type: "http", Target: "https://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	r := result(m.ID, true)
	r.LatencyMs = 250
	r.StatusCode = 200
	r.At -= 1000
	h.HandleResult(LocalLocation, r)
	h.HandleResult(LocalLocation, result(m.ID, false))
	now := time.Now()
	if err := st.SaveExpiryCheck(&store.ExpiryCheck{MonitorID: m.ID, Kind: store.ExpiryCert, Subject: "x", ExpiresAt: now.Add(10 * 24 * time.Hour).UnixMilli(), CheckedAt: now.UnixMilli(), AlertedDays: -1}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAgent("edge", "Milan", "hash"); err != nil {
		t.Fatal(err)
	}

	b, err := h.renderMetrics(now)
	if err != nil {
		t.Fatal(err)
	}
	out := string(b)
	ml := `monitor_id="1",monitor="web \"main\"",type="http"`
	for _, want := range []string{
		"# TYPE pingtxt_monitor_up gauge\n",
		`pingtxt_monitor_up{` + ml + `} 0`,
		`pingtxt_monitor_status{` + ml + `,status="down"} 1`,
		`pingtxt_monitor_status{` + ml + `,status="up"} 0`,
		`pingtxt_monitors{status="down"} 1`,
		`pingtxt_monitor_incident_open{` + ml + `} 1`,
		`pingtxt_check_up{` + ml + `,location="local",location_name="hub"} 0`,
		`pingtxt_monitor_uptime_ratio{` + ml + `,window="24h"} 0.5`,
		`pingtxt_monitor_latency_avg_seconds{` + ml + `,window="24h"} 0.25`,
		`pingtxt_cert_expiry_days{` + ml + `,subject="x"} 10`,
		`pingtxt_agent_connected{agent_id="1",agent="edge",location="agent:1",location_name="Milan",version=""} 0`,
		`pingtxt_agents{state="disconnected"} 1`,
		"pingtxt_incidents_open 1\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// Each family appears once, with its samples right after the header.
	if strings.Count(out, "# TYPE pingtxt_cert_expiry_days ") != 1 {
		t.Error("duplicate family header")
	}
}

func TestMetricsAuth(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})

	get := func(token string) int {
		req, _ := http.NewRequest("GET", srv.URL+"/metrics", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, res.Body)
		res.Body.Close()
		return res.StatusCode
	}
	if code := get(""); code != 401 {
		t.Fatalf("anonymous: %d", code)
	}
	if code := get("ptk_nope"); code != 401 {
		t.Fatalf("bad token: %d", code)
	}
	tok := decode[struct {
		Secret string `json:"secret"`
	}](t, c.do("POST", "/api/tokens", map[string]any{"name": "prometheus", "scope": "read"}))
	if code := get(tok.Secret); code != 200 {
		t.Fatalf("read token: %d", code)
	}
	res := c.do("GET", "/metrics", nil)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/plain; version=0.0.4") {
		t.Fatalf("session: %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}

	g := h.generalSettings()
	g.Metrics.Public = true
	if res := c.do("PUT", "/api/settings/general", g); res.StatusCode != 200 {
		t.Fatalf("save settings: %d", res.StatusCode)
	}
	if code := get(""); code != 200 {
		t.Fatalf("public: %d", code)
	}
}

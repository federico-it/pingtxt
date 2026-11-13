package hub

import (
	"context"
	"net/http/httptest"
	"os"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestPresetCatalog(t *testing.T) {
	color := regexp.MustCompile(`^#[0-9a-f]{6}$`)
	id := regexp.MustCompile(`^[a-z0-9]+$`)
	seen := map[string]bool{}
	urls := map[string]bool{}
	for _, p := range servicePresets {
		if !id.MatchString(p.ID) || seen[p.ID] {
			t.Errorf("%s: bad or duplicate id", p.ID)
		}
		seen[p.ID] = true
		if base, err := check.StatuspageBase(p.URL); err != nil || base != p.URL || urls[p.URL] {
			t.Errorf("%s: url %q is not a clean, unique status page address (%q, %v)", p.ID, p.URL, base, err)
		}
		urls[p.URL] = true
		if !color.MatchString(p.Color) {
			t.Errorf("%s: color %q", p.ID, p.Color)
		}
		if !slices.ContainsFunc(presetCategories, func(c PresetCategory) bool { return c.ID == p.Category }) {
			t.Errorf("%s: unknown category %q", p.ID, p.Category)
		}
		if p.Name == "" || p.Keywords == nil {
			t.Errorf("%s: missing name or keywords", p.ID)
		}
		for _, icon := range []string{p.Icon, p.IconDark} {
			if icon == "" {
				continue
			}
			if b, err := os.ReadFile("../../web/public" + icon); err != nil || !strings.Contains(string(b[:min(len(b), 512)]), "<svg") {
				t.Errorf("%s: icon %s missing or not an SVG (%v)", p.ID, icon, err)
			}
		}
	}
	for id := range presetIcons {
		if !seen[id] {
			t.Errorf("icon for unknown service %q", id)
		}
	}
}

func TestPresetsAPI(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})

	// A monitor that already follows GitHub, with components, counts.
	gh := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{"name": "gh", "type": "statuspage", "target": "https://www.githubstatus.com/api/v2/status.json"}))

	cat := decode[presetCatalog](t, admin.do("GET", "/api/monitors/presets", nil))
	if len(cat.Services) != len(servicePresets) || len(cat.Categories) != len(presetCategories) {
		t.Fatalf("catalog: %d services, %d categories", len(cat.Services), len(cat.Categories))
	}
	for _, s := range cat.Services {
		if (s.ID == "github") != (s.MonitorID == gh.ID) {
			t.Fatalf("%s: monitor_id %d", s.ID, s.MonitorID)
		}
	}
	if got := decode[presetCatalog](t, admin.do("GET", "/api/monitors/presets?q=chatgpt", nil)); len(got.Services) != 1 || got.Services[0].ID != "openai" {
		t.Fatalf("search chatgpt: %+v", got.Services)
	}
	if got := decode[presetCatalog](t, admin.do("GET", "/api/monitors/presets?category=ai", nil)); len(got.Services) < 3 || got.Services[0].ID != "claude" {
		t.Fatalf("category ai: %+v", got.Services)
	}

	if res := admin.do("POST", "/api/monitors/presets", map[string]any{"ids": []string{"claude", "nope"}}); res.StatusCode != 400 {
		t.Fatalf("unknown id: %d", res.StatusCode)
	}
	if res := admin.do("POST", "/api/monitors/presets", map[string]any{"ids": []string{}}); res.StatusCode != 400 {
		t.Fatalf("no ids: %d", res.StatusCode)
	}

	res := admin.do("POST", "/api/monitors/presets", map[string]any{"ids": []string{"claude", "OpenAI", "github", "claude"}, "group_name": "Services"})
	if res.StatusCode != 201 {
		t.Fatalf("add: %d", res.StatusCode)
	}
	out := decode[presetResult](t, res)
	if len(out.Created) != 2 || len(out.Skipped) != 1 || out.Skipped[0].MonitorID != gh.ID || out.Group == nil || out.Group.Name != "Services" {
		t.Fatalf("add: %+v", out)
	}
	m := out.Created[0]
	if m.Name != "Claude" || m.Type != check.TypeStatuspage || m.Target != "https://status.claude.com" || m.IntervalSec != presetDefaultInterval ||
		!slices.Equal(m.Locations, []string{LocalLocation}) || m.GroupID != out.Group.ID {
		t.Fatalf("created monitor: %+v", m)
	}

	// Again: nothing new, and the existing group (any case) is reused.
	again := decode[presetResult](t, admin.do("POST", "/api/monitors/presets", map[string]any{"ids": []string{"claude", "npm"}, "group_name": "services", "interval_sec": 300}))
	if len(again.Created) != 1 || again.Created[0].GroupID != out.Group.ID || again.Created[0].IntervalSec != 300 || len(again.Skipped) != 1 {
		t.Fatalf("add again: %+v", again)
	}
	if res := admin.do("POST", "/api/monitors/presets", map[string]any{"ids": []string{"figma"}, "interval_sec": 5}); res.StatusCode != 400 {
		t.Fatalf("bad interval: %d", res.StatusCode)
	}

	// Read-only users can see the catalog but not add.
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	viewer := newClient(t, srv.URL)
	viewer.do("POST", "/api/auth/login", map[string]string{"email": "v@b.co", "password": "password123"})
	if res := viewer.do("GET", "/api/monitors/presets", nil); res.StatusCode != 200 {
		t.Fatalf("viewer list: %d", res.StatusCode)
	}
	if res := viewer.do("POST", "/api/monitors/presets", map[string]any{"ids": []string{"figma"}}); res.StatusCode != 403 {
		t.Fatalf("viewer add: %d", res.StatusCode)
	}
}

// TestPresetsLive checks that every page of the catalog still answers a
// Statuspage summary. It needs the network: PINGTXT_LIVE=1 go test -run TestPresetsLive.
func TestPresetsLive(t *testing.T) {
	if os.Getenv("PINGTXT_LIVE") == "" {
		t.Skip("set PINGTXT_LIVE=1 to check the catalog against the real pages")
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, 12)
	for _, p := range servicePresets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			if _, _, err := check.FetchStatuspage(ctx, check.Job{Target: p.URL}, 20*time.Second); err != nil {
				t.Errorf("%s (%s): %v", p.ID, p.URL, err)
			}
		}()
	}
	wg.Wait()
}

func TestPresetsMCP(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	token := decode[struct {
		Secret string `json:"secret"`
	}](t, admin.do("POST", "/api/tokens", map[string]any{"name": "mcp", "scope": "write"})).Secret
	rw := &mcpClient{t: t, url: srv.URL, token: token}
	list := rw.callOK("list_service_presets", map[string]any{"q": "copilot"}).(map[string]any)["services"].([]any)
	if len(list) != 1 || list[0].(map[string]any)["id"] != "github" {
		t.Fatalf("list_service_presets: %v", list)
	}
	out := rw.callOK("add_service_presets", map[string]any{"ids": []any{"github", "claude"}, "group_name": "SaaS"}).(map[string]any)
	if len(out["created"].([]any)) != 2 || out["group"].(map[string]any)["name"] != "SaaS" {
		t.Fatalf("add_service_presets: %v", out)
	}
}

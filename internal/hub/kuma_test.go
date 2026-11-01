package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

const kumaSample = `{
  "version": "1.23.16",
  "notificationList": [{"id": 1, "name": "mail"}],
  "monitorList": [
    {"id": 1, "name": "Web", "type": "group", "active": 1},
    {"id": 2, "name": "site", "type": "http", "url": "https://example.com", "method": "GET", "interval": 60, "timeout": 48, "maxretries": 2, "active": 1, "parent": 1,
     "accepted_statuscodes": ["200-299"], "headers": "{\"X-Key\": \"abc\"}", "basic_auth_user": "u", "basic_auth_pass": "p",
     "tags": [{"name": "prod", "color": "#DC2626", "value": ""}]},
    {"id": 3, "name": "shop", "type": "keyword", "url": "https://shop.example.com", "keyword": "Add to cart", "invertKeyword": false, "interval": "120", "active": 0, "parent": 1},
    {"id": 4, "name": "db port", "type": "port", "hostname": "db.local", "port": 5432, "interval": 30, "active": true},
    {"id": 5, "name": "resolver", "type": "dns", "hostname": "example.com", "dns_resolve_type": "SOA", "interval": 60, "active": 1},
    {"id": 6, "name": "cron", "type": "push", "interval": 3600, "active": 1},
    {"id": 7, "name": "steam", "type": "steam", "hostname": "x", "active": 1},
    {"id": 8, "name": "pg", "type": "postgres", "databaseConnectionString": "postgres://u:secret@db:5432/app", "databaseQuery": "SELECT 1", "active": 1},
    {"id": 9, "name": "existing", "type": "ping", "hostname": "1.1.1.1", "active": 1},
    {"id": 10, "name": "flipped", "type": "http", "url": "https://x.example", "upsideDown": true, "active": 1}
  ]
}`

func TestImportKuma(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
	cookie := login(t, h, st)
	if err := st.CreateMonitor(&store.Monitor{Name: "Existing", Type: "ping", Target: "8.8.8.8", IntervalSec: 60, Locations: []string{LocalLocation}}); err != nil {
		t.Fatal(err)
	}
	post := func(path, body string) (int, kumaPlan) {
		t.Helper()
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		var p kumaPlan
		_ = json.Unmarshal(b, &p)
		return res.StatusCode, p
	}

	if c, _ := post("/api/import/uptime-kuma", `{"foo":1}`); c != 400 {
		t.Fatalf("not kuma: want 400, got %d", c)
	}

	c, plan := post("/api/import/uptime-kuma?dry_run=1", kumaSample)
	if c != 200 || plan.Created != 0 || len(plan.Monitors) != 9 {
		t.Fatalf("preview: %d %+v", c, plan)
	}
	actions := map[string]string{}
	for _, it := range plan.Monitors {
		actions[it.Name] = it.Action
	}
	want := map[string]string{"site": "create", "shop": "create", "db port": "create", "resolver": "skip", "cron": "create",
		"steam": "skip", "pg": "create", "existing": "exists", "flipped": "skip"}
	for n, a := range want {
		if actions[n] != a {
			t.Errorf("%s: want %s, got %s", n, a, actions[n])
		}
	}
	if len(plan.Warnings) != 1 || len(plan.Groups) != 1 || plan.Groups[0] != "Web" || len(plan.Tags) != 1 {
		t.Fatalf("preview extras: %+v", plan)
	}
	for _, it := range plan.Monitors {
		if it.Name == "pg" && strings.Contains(it.Target, "secret") {
			t.Fatal("password shown in the preview")
		}
	}
	if ms, _ := st.ListMonitors(); len(ms) != 1 {
		t.Fatal("preview created monitors")
	}

	c, plan = post("/api/import/uptime-kuma", kumaSample)
	if c != 200 || plan.Created != 5 {
		t.Fatalf("import: %d created %d", c, plan.Created)
	}
	ms, _ := st.ListMonitors()
	byName := map[string]*store.Monitor{}
	for _, m := range ms {
		byName[m.Name] = m
	}
	site := byName["site"]
	if site == nil || site.Type != "http" || site.Retries != 3 || site.TimeoutMs != 48000 || site.GroupID == 0 || len(site.TagIDs) != 1 ||
		site.Headers["X-Key"] != "abc" || !strings.HasPrefix(site.Headers["Authorization"], "Basic ") {
		t.Fatalf("site: %+v", site)
	}
	if shop := byName["shop"]; shop == nil || shop.Type != "keyword" || shop.Keyword != "Add to cart" || !shop.Paused || shop.IntervalSec != 120 || shop.GroupID != site.GroupID {
		t.Fatalf("shop: %+v", shop)
	}
	if p := byName["db port"]; p == nil || p.Type != "tcp" || p.Target != "db.local:5432" {
		t.Fatalf("port: %+v", p)
	}
	if p := byName["cron"]; p == nil || p.Type != "push" || p.PushToken == "" {
		t.Fatalf("push: %+v", p)
	}
	if p := byName["pg"]; p == nil || p.Body != "SELECT 1" {
		t.Fatalf("pg: %+v", p)
	}
	tags, _ := st.ListTags()
	if len(tags) != 1 || tags[0].Color != "red" {
		t.Fatalf("tags: %+v", tags)
	}

	// Importing again creates nothing: the names exist now.
	if _, plan = post("/api/import/uptime-kuma", kumaSample); plan.Created != 0 {
		t.Fatalf("second import created %d", plan.Created)
	}
}

func TestKumaColor(t *testing.T) {
	for hex, want := range map[string]string{"#DC2626": "red", "#2563EB": "blue", "#059669": "green", "#7C3AED": "purple", "#4B5563": "gray", "#D97706": "orange", "nope": ""} {
		if got := kumaColor(hex); got != want {
			t.Errorf("%s: want %s, got %s", hex, want, got)
		}
	}
}

func TestResultAndIncidentFilters(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	if err := h.reloadAll(); err != nil {
		t.Fatal(err)
	}
	base := time.Now().Add(-time.Hour).UnixMilli()
	for i := range 10 {
		r := result(m.ID, i%3 != 0)
		r.At = base + int64(i)*1000
		h.HandleResult(LocalLocation, r)
	}
	all, _ := st.QueryResults(m.ID, store.ResultQuery{Limit: 100})
	if len(all) != 10 {
		t.Fatalf("all: %d", len(all))
	}
	failed, _ := st.QueryResults(m.ID, store.ResultQuery{Limit: 100, Failed: true})
	if len(failed) != 4 {
		t.Fatalf("failed: %d", len(failed))
	}
	page, _ := st.QueryResults(m.ID, store.ResultQuery{Limit: 3, Before: all[2].TS})
	if len(page) != 3 || page[0].TS != all[3].TS {
		t.Fatalf("before: %+v", page)
	}
	win, _ := st.QueryResults(m.ID, store.ResultQuery{Limit: 100, From: base + 2000, To: base + 5000})
	if len(win) != 3 {
		t.Fatalf("window: %d", len(win))
	}

	open, _ := st.FindIncidents(store.IncidentQuery{State: "open", Limit: 10})
	resolved, _ := st.FindIncidents(store.IncidentQuery{State: "resolved", Limit: 10})
	every, _ := st.FindIncidents(store.IncidentQuery{Limit: 10})
	if len(open)+len(resolved) != len(every) || len(every) == 0 {
		t.Fatalf("incidents: open %d resolved %d all %d", len(open), len(resolved), len(every))
	}
	if later, _ := st.FindIncidents(store.IncidentQuery{Since: time.Now().Add(time.Hour).UnixMilli(), State: "resolved", Limit: 10}); len(later) != 0 {
		t.Fatalf("since: %d", len(later))
	}
}

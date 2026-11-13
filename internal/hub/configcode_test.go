package hub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

const codeFile = `
version: 1
groups:
  - name: production
  - name: api
    parent: production
tags:
  - {name: critical, color: red}
channels:
  - name: ops
    type: slack
    config: {url: "https://hooks.slack.com/services/T0/B0/xyz"}
    groups: [production]
  - name: hook
    type: webhook
    config: {url: "https://example.com/hook"}
    all_monitors: false
monitors:
  - name: website
    type: http
    target: https://example.com
    group: production
    tags: [critical]
    channels: [hook]
  - name: api health
    type: keyword
    target: https://api.example.com/health
    keyword: ok
    interval_sec: 30
    group: api
    depends_on: [website]
    channels:
      - {channel: hook, events: [down]}
  - name: nightly backup
    type: push
    interval_sec: 86400
  - name: db
    type: postgres
    target: postgres://app:hunter2@db.example:5432/app
  - name: private api
    type: http
    target: https://private.example.com
    headers: {Authorization: "Bearer s3cret", Accept: application/json}
maintenances:
  - name: sunday window
    kind: weekly
    weekdays: [0]
    start_time: "03:00"
    duration_min: 60
    groups: [production]
status_pages:
  - slug: status
    title: Example status
    groups: [production]
    monitors: [website]
`

func mustPlan(t *testing.T, h *Hub, src string, opt CodeOptions) *CodePlan {
	t.Helper()
	doc, err := ParseCode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	p, err := h.PlanCode(doc, opt)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func mustApply(t *testing.T, h *Hub, src string, opt CodeOptions) *CodeApplyResult {
	t.Helper()
	doc, err := ParseCode([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	res, err := h.ApplyCode(doc, opt)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plan.Errors) > 0 {
		t.Fatalf("plan errors: %v", res.Plan.Errors)
	}
	return res
}

func changes(p *CodePlan) []string {
	out := []string{}
	for _, c := range p.Changes {
		s := c.Action + " " + c.Kind + " " + c.Name
		if len(c.Fields) > 0 {
			s += " [" + strings.Join(c.Fields, ",") + "]"
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func TestConfigCodeApply(t *testing.T) {
	h, st := newTestHub(t)
	h.cfg.DataDir = t.TempDir() // backups before apply

	res := mustApply(t, h, codeFile, CodeOptions{})
	if !res.Applied || len(res.Plan.Changes) != 12 {
		t.Fatalf("first apply: %v", changes(res.Plan))
	}
	ms, _ := st.ListMonitors()
	byName := map[string]*store.Monitor{}
	for _, m := range ms {
		byName[m.Name] = m
	}
	api, web, push := byName["api health"], byName["website"], byName["nightly backup"]
	if api == nil || web == nil || push == nil {
		t.Fatalf("monitors: %v", ms)
	}
	if api.IntervalSec != 30 || web.IntervalSec != 60 || !slices.Equal(api.DependsOn, []int64{web.ID}) || api.GroupID == web.GroupID || len(web.TagIDs) != 1 {
		t.Fatalf("api %+v web %+v", api, web)
	}
	if len(api.Notifiers) != 1 || !slices.Equal(api.Notifiers[0].Events, []string{store.EventDown}) {
		t.Fatalf("api links: %+v", api.Notifiers)
	}
	if push.PushToken == "" {
		t.Fatal("push monitor without token")
	}

	// Applying the same file again changes nothing.
	if p := mustPlan(t, h, codeFile, CodeOptions{}); len(p.Changes) != 0 || len(p.Errors) != 0 {
		t.Fatalf("second plan: %v %v", changes(p), p.Errors)
	}
	// Nor does the export of the hub, without secrets.
	exp, err := h.ExportCode(false, false)
	if err != nil {
		t.Fatal(err)
	}
	y, err := MarshalCode(exp, "test")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(y), "hooks.slack.com") || strings.Contains(string(y), "hunter2") || strings.Contains(string(y), "s3cret") {
		t.Fatalf("secret in export:\n%s", y)
	}
	if strings.Contains(string(y), "interval_sec: 60") || strings.Contains(string(y), "push_token") || strings.Contains(string(y), "id:") {
		t.Fatalf("defaults or ids in export:\n%s", y)
	}
	if p := mustPlan(t, h, string(y), CodeOptions{Prune: true}); len(p.Changes) != 0 || len(p.Errors) != 0 {
		t.Fatalf("export plan: %v %v\n%s", changes(p), p.Errors, y)
	}

	// A change shows the fields; the stored Slack URL stays when the file has none.
	edited := strings.Replace(codeFile, "interval_sec: 30", "interval_sec: 45", 1)
	edited = strings.Replace(edited, `config: {url: "https://hooks.slack.com/services/T0/B0/xyz"}`, "", 1)
	p := mustPlan(t, h, edited, CodeOptions{})
	if got := changes(p); !slices.Equal(got, []string{"update monitors api health (keyword) [interval_sec]"}) {
		t.Fatalf("edit plan: %v", got)
	}
	mustApply(t, h, edited, CodeOptions{})
	if m, _ := st.Monitor(api.ID); m.IntervalSec != 45 {
		t.Fatalf("interval: %d", m.IntervalSec)
	}
	ns, _ := st.ListNotifiers()
	for _, n := range ns {
		if n.Type == "slack" && n.Config["url"] != "https://hooks.slack.com/services/T0/B0/xyz" {
			t.Fatalf("slack url lost: %v", n.Config)
		}
	}
	if m, _ := st.Monitor(push.ID); m.PushToken != push.PushToken {
		t.Fatal("heartbeat URL changed")
	}

	// Only the monitors section: without prune the rest stays, with prune
	// the monitors not listed go, other sections are left alone.
	only := "version: 1\nmonitors:\n  - {name: website, type: http, target: https://example.com, group: production, tags: [critical], channels: [hook]}\n"
	if p := mustPlan(t, h, only, CodeOptions{}); len(p.Changes) != 0 {
		t.Fatalf("partial plan: %v", changes(p))
	}
	p = mustPlan(t, h, only, CodeOptions{Prune: true})
	if got := changes(p); !slices.Equal(got, []string{"delete monitors api health (keyword)", "delete monitors db (postgres)", "delete monitors nightly backup (push)", "delete monitors private api (http)"}) {
		t.Fatalf("prune plan: %v", got)
	}
	res = mustApply(t, h, only, CodeOptions{Prune: true})
	if res.Deleted["monitors"] != 4 {
		t.Fatalf("deleted: %v", res.Deleted)
	}
	if ms, _ := st.ListMonitors(); len(ms) != 1 {
		t.Fatalf("monitors left: %d", len(ms))
	}
	if gs, _ := st.ListGroups(); len(gs) != 2 {
		t.Fatalf("groups touched: %d", len(gs))
	}
	// A monitor a status page still shows cannot be pruned.
	p = mustPlan(t, h, "version: 1\nmonitors: []\n", CodeOptions{Prune: true})
	if len(p.Errors) != 1 || !strings.Contains(p.Errors[0], `status page status: unknown monitor "website"`) {
		t.Fatalf("prune referenced: %v", p.Errors)
	}
}

func TestConfigCodeErrors(t *testing.T) {
	h, st := newTestHub(t)
	h.cfg.DataDir = t.TempDir() // backups before apply
	bad := `
version: 1
monitors:
  - name: a
    type: http
    target: https://a.example
    group: nope
    intervall: 30
  - name: b
    type: http
    target: https://b.example
    locations: [frankfurt]
    depends_on: [ghost]
`
	p := mustPlan(t, h, bad, CodeOptions{})
	joined := strings.Join(p.Errors, "\n")
	for _, want := range []string{`unknown group "nope"`, `intervall`, `unknown location "frankfurt"`, `unknown monitor "ghost"`} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing error %q in:\n%s", want, joined)
		}
	}
	doc, _ := ParseCode([]byte(bad))
	res, err := h.ApplyCode(doc, CodeOptions{})
	if err != nil || res.Applied {
		t.Fatalf("applied with errors: %v %+v", err, res)
	}
	if ms, _ := st.ListMonitors(); len(ms) != 0 {
		t.Fatal("changed the hub")
	}
	for _, src := range []string{"", "monitors: []", "version: 9", "version: 1\nfoo: 1", "version: 1\nsettings: {reports: {}}"} {
		if _, err := ParseCode([]byte(src)); err == nil {
			t.Errorf("%q: no error", src)
		}
	}
	// Login settings only from a session or the hub's own file.
	p = mustPlan(t, h, "version: 1\nsettings:\n  auth: {password_login: true}\n", CodeOptions{})
	if len(p.Errors) != 1 {
		t.Fatalf("auth with a token: %v", p.Errors)
	}
}

func TestConfigCodeFileSync(t *testing.T) {
	h, st := newTestHub(t)
	h.cfg.DataDir = t.TempDir() // backups before apply
	dir := t.TempDir()
	path := filepath.Join(dir, "pingtxt.yaml")
	t.Setenv("PT_TEST_HOOK", "https://example.com/secret-hook")
	src := "version: 1\nchannels:\n  - {name: hook, type: webhook, config: {url: \"${PT_TEST_HOOK}\"}}\nmonitors:\n  - {name: a, type: http, target: https://a.example}\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	h.codeSync = &codeSync{path: path, prune: true}
	h.syncCodeFile(true)
	if e := h.codeSync.status.Error; e != "" {
		t.Fatal(e)
	}
	ns, _ := st.ListNotifiers()
	if len(ns) != 1 || ns[0].Config["url"] != "https://example.com/secret-hook" {
		t.Fatalf("channel: %+v", ns)
	}
	// A missing variable is an error and changes nothing.
	os.WriteFile(path, []byte(strings.Replace(src, "PT_TEST_HOOK", "PT_TEST_MISSING", 1)), 0o600)
	h.syncCodeFile(false)
	if !strings.Contains(h.codeSync.status.Error, "PT_TEST_MISSING") {
		t.Fatalf("error: %q", h.codeSync.status.Error)
	}
	// With prune, a monitor added in the UI goes at the next change of the file.
	must(t, st.CreateMonitor(&store.Monitor{Name: "ui", Type: "http", Target: "https://ui.example", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}))
	os.WriteFile(path, []byte(src+"  - {name: b, type: http, target: https://b.example}\n"), 0o600)
	h.syncCodeFile(false)
	ms, _ := st.ListMonitors()
	names := []string{}
	for _, m := range ms {
		names = append(names, m.Name)
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"a", "b"}) {
		t.Fatalf("monitors: %v (%s)", names, h.codeSync.status.Error)
	}
}

func TestConfigCodeHTTP(t *testing.T) {
	h, _ := newTestHub(t)
	h.cfg.DataDir = t.TempDir()
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	post := func(path, ctype, body string) *http.Response {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", ctype)
		res, err := admin.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		return res
	}
	src := "version: 1\nmonitors:\n  - {name: a, type: http, target: https://a.example}\n"
	if res := post("/api/config/code/plan", "application/x-www-form-urlencoded", src); res.StatusCode != 415 {
		t.Fatalf("form post: %d", res.StatusCode)
	}
	p := decode[CodePlan](t, post("/api/config/code/plan", "application/yaml", src))
	if len(p.Changes) != 1 {
		t.Fatalf("plan: %+v", p)
	}
	r := decode[CodeApplyResult](t, post("/api/config/code/apply", "application/yaml", src))
	if !r.Applied {
		t.Fatalf("apply: %+v", r)
	}
	if res := post("/api/config/code/apply", "application/yaml", "version: 1\nmonitors:\n  - {name: b, type: http, target: https://b.example, group: x}\n"); res.StatusCode != 422 {
		t.Fatalf("errors: %d", res.StatusCode)
	}
	res := admin.do("GET", "/api/config/code", nil)
	b, _ := io.ReadAll(res.Body)
	if !strings.HasPrefix(res.Header.Get("Content-Type"), "application/yaml") || !strings.Contains(string(b), "target: https://a.example") {
		t.Fatalf("export: %s %s", res.Header.Get("Content-Type"), b)
	}
}

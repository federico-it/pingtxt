package hub

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

// Every write route behind the auth wrappers must say which audit action it
// is (or be listed as not audited), so new routes are not forgotten.
func TestAuditCoversWriteRoutes(t *testing.T) {
	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`mux\.HandleFunc\("((POST|PUT|DELETE|PATCH) /api/[^"]+)", (auth|session|admin)\(`)
	n := 0
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		n++
		if _, ok := auditRoutes[m[1]]; !ok {
			t.Errorf("route %q is missing from auditRoutes", m[1])
		}
	}
	if n < 50 {
		t.Fatalf("only %d write routes found: did routes() change shape?", n)
	}
	for pattern, spec := range auditRoutes {
		if spec.action != "" && !regexp.MustCompile(`^[a-z_]+\.[a-z_0-9]+$`).MatchString(spec.action) {
			t.Errorf("%s: bad action %q", pattern, spec.action)
		}
	}
}

func auditEntries(t *testing.T, st *store.Store, f store.AuditFilter) []store.AuditEntry {
	t.Helper()
	list, err := st.ListAudit(f)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func changesOf(t *testing.T, e store.AuditEntry) map[string][]any {
	t.Helper()
	var m map[string][]any
	if len(e.Changes) > 0 {
		if err := json.Unmarshal(e.Changes, &m); err != nil {
			t.Fatalf("changes %s: %v", e.Changes, err)
		}
	}
	return m
}

func TestAuditLog(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})

	// A monitor with secrets in the target, headers and body.
	m := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{
		"name": "db", "type": "http", "target": "https://app:s3cret@db.example.com/health",
		"headers": map[string]string{"Authorization": "Bearer abc", "Accept": "text/plain"}, "body": "token=xyz", "method": "POST",
	}))
	m.Name = "db primary"
	m.Target = "https://app:n3w@db.example.com/health"
	m.IntervalSec = 30
	if res := admin.do("PUT", "/api/monitors/"+itoa(m.ID), m); res.StatusCode != 200 {
		t.Fatalf("update: %d", res.StatusCode)
	}

	list := auditEntries(t, st, store.AuditFilter{TargetType: "monitor", TargetID: m.ID})
	if len(list) != 2 || list[0].Action != "monitor.update" || list[1].Action != "monitor.create" {
		t.Fatalf("entries: %+v", list)
	}
	up := list[0]
	if up.Source != store.AuditWeb || up.UserEmail != "admin@b.co" || up.Result != store.AuditOK || up.TargetName != "db primary" || up.IP == "" {
		t.Fatalf("update entry: %+v", up)
	}
	ch := changesOf(t, up)
	if ch["name"][0] != "db" || ch["name"][1] != "db primary" || ch["interval_sec"][1] != float64(30) {
		t.Fatalf("changes: %s", up.Changes)
	}
	if got := ch["target"]; got[0] != "https://app:********@db.example.com/health" || got[1] != got[0] {
		t.Fatalf("target: %v", got)
	}
	if _, ok := ch["headers"]; ok {
		t.Fatalf("unchanged headers in diff: %s", up.Changes)
	}
	for _, e := range list {
		for _, secret := range []string{"s3cret", "n3w", "Bearer abc", "xyz"} {
			if strings.Contains(string(e.Changes), secret) {
				t.Fatalf("secret %q in %s", secret, e.Changes)
			}
		}
	}
	if ch := changesOf(t, list[1]); ch["headers"] == nil || ch["body"][1] != "********" {
		t.Fatalf("create changes: %s", list[1].Changes)
	}

	// Pause and resume get their own actions; delete keeps the last name.
	admin.do("POST", "/api/monitors/"+itoa(m.ID)+"/pause", map[string]bool{"paused": true})
	admin.do("POST", "/api/monitors/"+itoa(m.ID)+"/pause", map[string]bool{"paused": false})
	admin.do("DELETE", "/api/monitors/"+itoa(m.ID), nil)
	list = auditEntries(t, st, store.AuditFilter{TargetType: "monitor", TargetID: m.ID, Limit: 3})
	if list[0].Action != "monitor.delete" || list[0].TargetName != "db primary" || list[1].Action != "monitor.resume" || list[2].Action != "monitor.pause" {
		t.Fatalf("pause/delete: %+v", list)
	}

	// Invalid input changes nothing and is not logged.
	before := len(auditEntries(t, st, store.AuditFilter{Limit: 1000}))
	admin.do("POST", "/api/monitors", map[string]any{"name": ""})
	if after := len(auditEntries(t, st, store.AuditFilter{Limit: 1000})); after != before {
		t.Fatalf("invalid request logged")
	}

	// A read-only user and a read token are logged as denied.
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	viewer := newClient(t, srv.URL)
	viewer.do("POST", "/api/auth/login", map[string]string{"email": "v@b.co", "password": "password123"})
	viewer.do("POST", "/api/monitors", map[string]any{"name": "x", "type": "http", "target": "https://example.com"})
	viewer.do("GET", "/api/audit", nil)
	denied := auditEntries(t, st, store.AuditFilter{Problems: true})
	if len(denied) != 1 || denied[0].Action != "monitor.create" || denied[0].Result != store.AuditDenied || denied[0].UserEmail != "v@b.co" {
		t.Fatalf("denied: %+v", denied)
	}
	if res := viewer.do("GET", "/api/audit", nil); res.StatusCode != 403 {
		t.Fatalf("viewer reads the audit log: %d", res.StatusCode)
	}

	// API tokens: source api with the token name; MCP: source mcp with the tool.
	tok := decode[struct {
		Secret string `json:"secret"`
	}](t, admin.do("POST", "/api/tokens", map[string]any{"name": "terraform", "scope": "write"})).Secret
	created := decode[store.Monitor](t, bearerDo(t, srv.URL, tok, "POST", "/api/monitors", map[string]any{"name": "api", "type": "http", "target": "https://example.com"}))
	e := auditEntries(t, st, store.AuditFilter{Action: "monitor.create", Limit: 1})[0]
	if e.Source != store.AuditAPI || !strings.HasPrefix(e.Credential, "terraform …") || e.TargetID != created.ID || e.TargetName != "api" {
		t.Fatalf("token entry: %+v", e)
	}
	mc := &mcpClient{t: t, url: srv.URL, token: tok}
	mc.rpc("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
	mc.callOK("pause_monitor", map[string]any{"id": created.ID, "paused": true})
	e = auditEntries(t, st, store.AuditFilter{TargetType: "monitor", TargetID: created.ID, Limit: 1})[0]
	if e.Source != store.AuditMCP || e.Via != "pause_monitor" || e.Action != "monitor.pause" {
		t.Fatalf("mcp entry: %+v", e)
	}

	// Logins: failed ones keep the typed email.
	anon := newClient(t, srv.URL)
	anon.do("POST", "/api/auth/login", map[string]string{"email": "nobody@b.co", "password": "wrong-password"})
	e = auditEntries(t, st, store.AuditFilter{Action: "account.login", Problems: true, Limit: 1})[0]
	if e.UserID != 0 || e.UserEmail != "nobody@b.co" || e.Result != store.AuditFailed {
		t.Fatalf("failed login: %+v", e)
	}

	// The admin's page, filters and export.
	page := decode[struct {
		Entries    []store.AuditEntry `json:"entries"`
		NextBefore int64              `json:"next_before"`
	}](t, admin.do("GET", "/api/audit?limit=2&category=monitor", nil))
	if len(page.Entries) != 2 || page.NextBefore == 0 {
		t.Fatalf("page: %+v", page)
	}
	res := admin.do("GET", "/api/audit/export?source=mcp", nil)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/csv") {
		t.Fatalf("export: %d", res.StatusCode)
	}
	if e := auditEntries(t, st, store.AuditFilter{Limit: 1})[0]; e.Action != "audit.export" {
		t.Fatalf("export not logged: %+v", e)
	}
	if res := bearerDo(t, srv.URL, tok, "GET", "/api/audit/export?format=jsonl", nil); res.StatusCode != 200 {
		t.Fatalf("export with admin token: %d", res.StatusCode)
	}

	// Each user sees their own account activity.
	mine := decode[[]store.AuditEntry](t, viewer.do("GET", "/api/account/activity", nil))
	if len(mine) != 1 || mine[0].Action != "account.login" || mine[0].UserEmail != "v@b.co" {
		t.Fatalf("activity: %+v", mine)
	}
}

func TestAuditBulkAndSettings(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	var ids []int64
	for _, n := range []string{"a", "b"} {
		ids = append(ids, decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{"name": n, "type": "http", "target": "https://example.com"})).ID)
	}
	admin.do("POST", "/api/monitors/bulk", map[string]any{"ids": ids, "action": "pause"})
	list := auditEntries(t, st, store.AuditFilter{Action: "monitor.pause"})
	if len(list) != 2 || list[0].RequestID != list[1].RequestID || list[0].TargetID == list[1].TargetID {
		t.Fatalf("bulk: %+v", list)
	}

	g := decode[struct {
		General GeneralSettings `json:"general"`
	}](t, admin.do("GET", "/api/settings", nil)).General
	if g.AuditRetentionDays != 365 {
		t.Fatalf("default retention: %d", g.AuditRetentionDays)
	}
	g.AuditRetentionDays = 5
	g.RetentionDays = 60
	admin.do("PUT", "/api/settings/general", g)
	if got := h.generalSettings().AuditRetentionDays; got != 30 {
		t.Fatalf("retention not clamped: %d", got)
	}
	e := auditEntries(t, st, store.AuditFilter{Action: "settings.general", Limit: 1})[0]
	ch := changesOf(t, e)
	if ch["audit_retention_days"][1] != float64(30) || ch["retention_days"][1] != float64(60) || len(ch) != 2 {
		t.Fatalf("settings diff: %s", e.Changes)
	}
}

func TestRequestIP(t *testing.T) {
	for _, c := range []struct {
		headers map[string]string
		remote  string
		want    string
	}{
		{nil, "203.0.113.9:4000", "203.0.113.9"},
		{map[string]string{"X-Forwarded-For": "10.0.0.1, 198.51.100.7, 172.16.0.2"}, "10.0.0.5:1", "198.51.100.7"},
		{map[string]string{"CF-Connecting-IP": "fd00::1", "X-Forwarded-For": "198.51.100.7"}, "10.0.0.5:1", "198.51.100.7"},
		{map[string]string{"CF-Connecting-IP": "2a01:4f8::1"}, "10.0.0.5:1", "2a01:4f8::1"},
		{map[string]string{"Forwarded": `for="[2a01:4f8::2]:443";proto=https`}, "10.0.0.5:1", "2a01:4f8::2"},
		{map[string]string{"X-Real-IP": "192.168.1.4"}, "127.0.0.1:9", "127.0.0.1"},
		{map[string]string{"X-Forwarded-For": "100.64.1.1, 8.8.8.8"}, "10.0.0.5:1", "8.8.8.8"},
	} {
		r := httptest.NewRequest("GET", "/", nil)
		r.RemoteAddr = c.remote
		for k, v := range c.headers {
			r.Header.Set(k, v)
		}
		if got := requestIP(r); got != c.want {
			t.Errorf("%v %s: got %s want %s", c.headers, c.remote, got, c.want)
		}
	}
}

func TestAuditDiffSecrets(t *testing.T) {
	before := &store.Notifier{Name: "slack", Type: "slack", Config: map[string]string{"url": "https://hooks.slack.com/T/B/old", "channel": "#ops"}}
	after := &store.Notifier{Name: "slack", Type: "slack", Config: map[string]string{"url": "https://hooks.slack.com/T/B/new", "channel": "#ops"}}
	b := string(auditJSON(auditDiff(before, after)))
	if b != `{"config.url":["********","********"]}` {
		t.Fatalf("diff: %s", b)
	}
	if d := auditDiff(before, before); len(d) != 0 {
		t.Fatalf("no change: %v", d)
	}
	big := map[string]any{"x": strings.Repeat("a", 40000)}
	if out := auditJSON(map[string]any{"body": []any{nil, big}}); len(out) > auditMaxChanges {
		t.Fatalf("not capped: %d", len(out))
	}
}

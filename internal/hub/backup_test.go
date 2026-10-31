package hub

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// newDataHub is a test hub with its own data directory (backups go there).
func newDataHub(t *testing.T) (*Hub, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st := openTestStore(t, dir)
	h := New(Config{LocalName: "hub", DataDir: dir}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := h.reloadAll(); err != nil {
		t.Fatal(err)
	}
	return h, st
}

type apiClient struct {
	t      *testing.T
	srv    *httptest.Server
	cookie *http.Cookie
	token  string
}

func newAPIClient(t *testing.T, h *Hub, st *store.Store) *apiClient {
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	t.Cleanup(srv.Close)
	return &apiClient{t: t, srv: srv, cookie: login(t, h, st)}
}

func (c *apiClient) do(method, path, ctype string, body []byte, out any) (int, []byte, http.Header) {
	c.t.Helper()
	req, _ := http.NewRequest(method, c.srv.URL+path, bytes.NewReader(body))
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	} else {
		req.AddCookie(c.cookie)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if out != nil && res.StatusCode < 300 {
		if err := json.Unmarshal(b, out); err != nil {
			c.t.Fatalf("%s %s: %s", method, path, b)
		}
	}
	return res.StatusCode, b, res.Header
}

func (c *apiClient) json(method, path, body string, out any) int {
	c.t.Helper()
	code, _, _ := c.do(method, path, "application/json", []byte(body), out)
	return code
}

// seedConfig creates one of everything, with secrets in the places that
// exports redact.
func seedConfig(t *testing.T, h *Hub, st *store.Store) (agent *store.Agent, push *store.Monitor) {
	t.Helper()
	agent, err := st.CreateAgent("eu-1", "Frankfurt", HashToken("pt_secret"))
	if err != nil {
		t.Fatal(err)
	}
	g := &store.Group{Name: "Web"}
	must(t, st.CreateGroup(g))
	tag := &store.Tag{Name: "prod", Color: "red"}
	must(t, st.CreateTag(tag))
	must(t, st.CreateNotifier(&store.Notifier{Name: "mail", Type: "email", Enabled: true, Config: map[string]string{
		"host": "smtp.example.com", "port": "587", "security": "starttls", "username": "u", "password": "smtp-pass", "from": "a@example.com", "to": "b@example.com"}}))
	must(t, st.CreateNotifier(&store.Notifier{Name: "hook", Type: "webhook", Enabled: true, Config: map[string]string{"url": "https://example.com/hook"}}))
	api := &store.Monitor{Name: "api", Type: check.TypeHTTP, Target: "https://api.example.com", IntervalSec: 60, TimeoutMs: 5000, Retries: 2,
		Headers: map[string]string{"Authorization": "Bearer abc", "Accept": "text/html"}, Locations: []string{LocalLocation, AgentLocation(agent.ID)},
		GroupID: g.ID, TagIDs: []int64{tag.ID}}
	must(t, h.validateMonitor(api))
	must(t, st.CreateMonitor(api))
	db := &store.Monitor{Name: "db", Type: check.TypePostgres, Target: "postgres://app:db-pass@db:5432/app", IntervalSec: 60}
	must(t, h.validateMonitor(db))
	must(t, st.CreateMonitor(db))
	push = &store.Monitor{Name: "backup job", Type: check.TypePush, IntervalSec: 3600}
	must(t, h.validateMonitor(push))
	push.PushToken = "pushtoken123"
	must(t, st.CreateMonitor(push))
	must(t, st.CreateMaintenance(&store.Maintenance{Name: "nightly", Kind: store.MaintDaily, StartTime: "02:00", DurationMin: 30, Timezone: "UTC",
		Enabled: true, MonitorIDs: []int64{db.ID}, GroupIDs: []int64{g.ID}, TagIDs: []int64{tag.ID}, Weekdays: []int{}, MonthDays: []int{}}))
	must(t, st.CreateStatusPage(&store.StatusPage{Slug: "main", Title: "Main", Published: true, MonitorIDs: []int64{api.ID, push.ID}, GroupIDs: []int64{g.ID}}))
	a := h.authSettings()
	a.Providers[ProviderGitHub] = ProviderSettings{Enabled: true, ClientID: "id", ClientSecret: "gh-secret"}
	must(t, st.SetSetting(keyAuth, a))
	must(t, h.loadSettings())
	return agent, push
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func exportJSON(t *testing.T, h *Hub, secrets bool) []byte {
	t.Helper()
	doc, err := h.ExportConfig(secrets)
	must(t, err)
	b, err := json.Marshal(doc)
	must(t, err)
	return b
}

func importJSON(t *testing.T, h *Hub, b []byte, replace bool) *ImportResult {
	t.Helper()
	doc, err := ParseConfig(b)
	must(t, err)
	res, err := h.ImportConfig(doc, ImportOptions{Replace: replace, Settings: true})
	must(t, err)
	return res
}

func byName(t *testing.T, st *store.Store) map[string]*store.Monitor {
	t.Helper()
	ms, err := st.ListMonitors()
	must(t, err)
	out := map[string]*store.Monitor{}
	for _, m := range ms {
		out[m.Name] = m
	}
	return out
}

func TestExportRedactsSecrets(t *testing.T) {
	h, st := newDataHub(t)
	seedConfig(t, h, st)
	plain := string(exportJSON(t, h, false))
	for _, secret := range []string{"smtp-pass", "db-pass", "Bearer abc", "gh-secret", "pushtoken123", HashToken("pt_secret")} {
		if strings.Contains(plain, secret) {
			t.Errorf("export without secrets contains %q", secret)
		}
	}
	for _, kept := range []string{"https://example.com/hook", "postgres://app:__redacted__@db:5432/app", "text/html", `"status_pages"`} {
		if !strings.Contains(plain, kept) {
			t.Errorf("export without secrets lacks %q", kept)
		}
	}
	for _, state := range []string{`"status":`, `"status_since"`, `"created_at"`} {
		if strings.Contains(plain, state) {
			t.Errorf("export contains state field %s", state)
		}
	}
	full := string(exportJSON(t, h, true))
	for _, secret := range []string{"smtp-pass", "db-pass", "Bearer abc", "gh-secret", "pushtoken123", HashToken("pt_secret")} {
		if !strings.Contains(full, secret) {
			t.Errorf("export with secrets lacks %q", secret)
		}
	}
}

func TestImportIntoNewHub(t *testing.T) {
	src, sst := newDataHub(t)
	agent, _ := seedConfig(t, src, sst)
	full := exportJSON(t, src, true)

	dst, dstore := newDataHub(t)
	// Ids in the new hub differ from the file: shift them with an extra row.
	must(t, dstore.CreateGroup(&store.Group{Name: "existing"}))
	res := importJSON(t, dst, full, false)
	if res.Created["monitors"] != 3 || res.Created["groups"] != 1 || res.Created["tags"] != 1 || res.Created["notifiers"] != 2 ||
		res.Created["maintenances"] != 1 || res.Created["status_pages"] != 1 || res.Created["agents"] != 1 {
		t.Fatalf("created: %+v warnings %v", res.Created, res.Warnings)
	}
	if len(res.Warnings) != 0 {
		t.Fatalf("unexpected warnings: %v", res.Warnings)
	}
	// The agent keeps its token, so it reconnects to the new hub.
	ag, err := dstore.AgentByToken(HashToken("pt_secret"))
	if err != nil || ag.Name != agent.Name {
		t.Fatalf("agent token not kept: %v", err)
	}
	ms := byName(t, dstore)
	groups, _ := dstore.ListGroups()
	var web *store.Group
	for _, g := range groups {
		if g.Name == "Web" {
			web = g
		}
	}
	api := ms["api"]
	if web == nil || api.GroupID != web.ID || len(api.TagIDs) != 1 || api.Headers["Authorization"] != "Bearer abc" {
		t.Fatalf("api monitor: %+v", api)
	}
	if want := []string{AgentLocation(ag.ID), LocalLocation}; strings.Join(api.Locations, ",") != strings.Join(want, ",") {
		t.Fatalf("locations: %v, want %v", api.Locations, want)
	}
	if ms["db"].Target != "postgres://app:db-pass@db:5432/app" || ms["backup job"].PushToken != "pushtoken123" {
		t.Fatal("secrets not imported")
	}
	page, err := dstore.StatusPageBySlug("main")
	must(t, err)
	if len(page.MonitorIDs) != 2 || page.MonitorIDs[0] != api.ID || len(page.GroupIDs) != 1 || page.GroupIDs[0] != web.ID {
		t.Fatalf("status page references: %+v", page)
	}
	maints, _ := dstore.ListMaintenances()
	if len(maints) != 1 || maints[0].MonitorIDs[0] != ms["db"].ID || maints[0].GroupIDs[0] != web.ID {
		t.Fatalf("maintenance references: %+v", maints[0])
	}
	if a := dst.authSettings(); a.Providers[ProviderGitHub].ClientSecret != "gh-secret" {
		t.Fatal("auth settings not imported")
	}
	// The scheduler knows the new monitors.
	dst.mu.Lock()
	n := len(dst.monitors)
	dst.mu.Unlock()
	if n != 3 {
		t.Fatalf("scheduler has %d monitors, want 3", n)
	}

	// Importing the same file again matches everything by name.
	again := importJSON(t, dst, full, false)
	if len(again.Created) != 0 || again.Updated["monitors"] != 3 {
		t.Fatalf("second import: created %+v updated %+v", again.Created, again.Updated)
	}
}

func TestImportWithoutSecrets(t *testing.T) {
	src, sst := newDataHub(t)
	seedConfig(t, src, sst)
	plain := exportJSON(t, src, false)

	// Into an empty hub: what needs a secret is skipped or paused, with warnings.
	dst, dstore := newDataHub(t)
	res := importJSON(t, dst, plain, false)
	if res.Skipped["notifiers"] != 1 || res.Created["notifiers"] != 1 {
		t.Fatalf("notifiers: created %+v skipped %+v", res.Created, res.Skipped)
	}
	ms := byName(t, dstore)
	if !ms["db"].Paused || !ms["api"].Paused {
		t.Fatal("monitors with redacted credentials must be paused")
	}
	if ms["backup job"].PushToken == "" || ms["backup job"].PushToken == "pushtoken123" {
		t.Fatal("push monitor needs a new token")
	}
	if dst.authSettings().Providers[ProviderGitHub].Enabled {
		t.Fatal("provider without secret must be turned off")
	}
	if len(res.Warnings) < 4 {
		t.Fatalf("warnings: %v", res.Warnings)
	}

	// Into the same hub: redacted values keep the stored secrets.
	res = importJSON(t, src, plain, false)
	if len(res.Created) != 0 || len(res.Skipped) != 0 || len(res.Warnings) != 0 {
		t.Fatalf("merge into source: created %+v skipped %+v warnings %v", res.Created, res.Skipped, res.Warnings)
	}
	ms = byName(t, sst)
	if ms["db"].Target != "postgres://app:db-pass@db:5432/app" || ms["api"].Headers["Authorization"] != "Bearer abc" || ms["db"].Paused {
		t.Fatalf("secrets lost: %+v", ms["db"])
	}
	if ms["backup job"].PushToken != "pushtoken123" {
		t.Fatal("push token changed")
	}
	ns, _ := sst.ListNotifiers()
	for _, n := range ns {
		if n.Type == "email" && n.Config["password"] != "smtp-pass" {
			t.Fatal("email password lost")
		}
	}
	if src.authSettings().Providers[ProviderGitHub].ClientSecret != "gh-secret" {
		t.Fatal("client secret lost")
	}
}

func TestImportReplace(t *testing.T) {
	h, st := newDataHub(t)
	seedConfig(t, h, st)
	file := exportJSON(t, h, false)
	extra := &store.Monitor{Name: "extra", Type: check.TypeTCP, Target: "x:1", IntervalSec: 60}
	must(t, h.validateMonitor(extra))
	must(t, st.CreateMonitor(extra))
	h.MonitorChanged(extra.ID)

	res := importJSON(t, h, file, true)
	if res.Deleted["monitors"] != 4 || res.Created["monitors"] != 3 {
		t.Fatalf("replace: deleted %+v created %+v", res.Deleted, res.Created)
	}
	ms := byName(t, st)
	if _, ok := ms["extra"]; ok || len(ms) != 3 {
		t.Fatalf("monitors after replace: %v", ms)
	}
	// Secrets come back from the replaced objects.
	if ms["db"].Target != "postgres://app:db-pass@db:5432/app" || ms["backup job"].PushToken != "pushtoken123" {
		t.Fatal("secrets lost in replace")
	}
	h.mu.Lock()
	_, stale := h.monitors[extra.ID]
	h.mu.Unlock()
	if stale {
		t.Fatal("deleted monitor still scheduled")
	}
}

func TestConfigAPI(t *testing.T) {
	h, st := newDataHub(t)
	seedConfig(t, h, st)
	c := newAPIClient(t, h, st)

	code, body, hdr := c.do("GET", "/api/config/export", "", nil, nil)
	if code != 200 || !strings.Contains(hdr.Get("Content-Disposition"), "pingtxt-config-") || strings.Contains(string(body), "smtp-pass") {
		t.Fatalf("export: %d %s", code, hdr)
	}
	var res ImportResult
	code, b, _ := c.do("POST", "/api/config/import?mode=merge", "application/json", body, &res)
	if code != 200 || res.Updated["monitors"] != 3 || (h.FileBackups() && res.Backup == "") {
		t.Fatalf("import: %d %s", code, b)
	}
	if h.FileBackups() && !fileExists(filepath.Join(h.backupDir(), res.Backup)) {
		t.Fatal("no backup before import")
	}
	if !h.FileBackups() && len(res.Warnings) == 0 {
		t.Fatal("no warning about the missing backup")
	}
	if code := c.json("POST", "/api/config/import", `{"format":"other"}`, nil); code != 400 {
		t.Fatalf("bad document: want 400, got %d", code)
	}
	if code := c.json("POST", "/api/config/import?mode=wipe", string(body), nil); code != 400 {
		t.Fatalf("bad mode: want 400, got %d", code)
	}

	// Tokens may export without secrets, never with them, and cannot import.
	u, _ := st.UserByEmail("a@b.c")
	secret, hash := NewToken(apiTokenPrefix)
	must(t, st.CreateAPIToken(&store.APIToken{UserID: u.ID, Name: "t", TokenHash: hash, Scope: ScopeWrite}))
	tc := &apiClient{t: t, srv: c.srv, token: secret}
	if code, _, _ := tc.do("GET", "/api/config/export", "", nil, nil); code != 200 {
		t.Fatalf("token export: %d", code)
	}
	if code, _, _ := tc.do("GET", "/api/config/export?secrets=1", "", nil, nil); code != 403 {
		t.Fatalf("token export with secrets: want 403, got %d", code)
	}
	if code, _, _ := tc.do("POST", "/api/config/import", "application/json", body, nil); code != 403 {
		t.Fatalf("token import: want 403, got %d", code)
	}
	if code, _, _ := tc.do("GET", "/api/backups", "", nil, nil); code != 403 {
		t.Fatalf("token backups: want 403, got %d", code)
	}

	// Read-only users can do none of it.
	v, err := st.CreateUser("viewer@b.c", "", store.RoleViewer)
	must(t, err)
	rec := httptest.NewRecorder()
	must(t, h.startSession(rec, httptest.NewRequest("GET", "/", nil), v))
	vc := &apiClient{t: t, srv: c.srv, cookie: rec.Result().Cookies()[0]}
	for _, r := range [][2]string{{"GET", "/api/config/export"}, {"GET", "/api/backups"}, {"POST", "/api/backups"}, {"POST", "/api/config/import"}} {
		if code := vc.json(r[0], r[1], "", nil); code != 403 {
			t.Fatalf("viewer %s %s: want 403, got %d", r[0], r[1], code)
		}
	}
}

func TestBackupAPI(t *testing.T) {
	h, st := newDataHub(t)
	sqliteOnly(t, st)
	seedConfig(t, h, st)
	c := newAPIClient(t, h, st)

	var b BackupFile
	if code := c.json("POST", "/api/backups", "", &b); code != 201 || b.Kind != backupManual || b.Size == 0 {
		t.Fatalf("create: %d %+v", code, b)
	}
	var list backupsView
	if code := c.json("GET", "/api/backups", "", &list); code != 200 || len(list.Backups) != 1 || list.PendingRestore != nil {
		t.Fatalf("list: %d %+v", code, list)
	}
	code, data, hdr := c.do("GET", "/api/backups/"+b.Name, "", nil, nil)
	if code != 200 || int64(len(data)) != b.Size || !strings.Contains(hdr.Get("Content-Disposition"), b.Name) {
		t.Fatalf("download: %d %d bytes", code, len(data))
	}
	for _, bad := range []string{"..%2Fpingtxt.db", "pingtxt.db", "pingtxt-manual-x.db"} {
		if code, _, _ := c.do("GET", "/api/backups/"+bad, "", nil, nil); code != 400 {
			t.Fatalf("download %s: want 400, got %d", bad, code)
		}
	}

	// Upload: the downloaded file is accepted, anything else refused.
	var up struct {
		Backup BackupFile       `json:"backup"`
		Info   store.BackupInfo `json:"info"`
	}
	if code, body, _ := c.do("POST", "/api/backups/upload", "application/octet-stream", data, &up); code != 201 || up.Info.Monitors != 3 || up.Backup.Kind != backupUpload {
		t.Fatalf("upload: %d %s", code, body)
	}
	if code, _, _ := c.do("POST", "/api/backups/upload", "application/octet-stream", []byte("hello"), nil); code != 400 {
		t.Fatalf("upload junk: want 400, got %d", code)
	}
	if code, _, _ := c.do("POST", "/api/backups/upload", "text/plain", data, nil); code != 415 {
		t.Fatalf("upload as text/plain: want 415, got %d", code)
	}

	// Settings.
	var s BackupSettings
	if code := c.json("PUT", "/api/settings/backups", `{"enabled":true,"interval_hours":0,"keep":3}`, &s); code != 200 || !s.Enabled || s.IntervalHours != 24 || s.Keep != 3 {
		t.Fatalf("settings: %d %+v", code, s)
	}

	// Stage a restore, then cancel it.
	if code := c.json("POST", "/api/backups/"+b.Name+"/restore", "", nil); code != 200 {
		t.Fatalf("stage restore: %d", code)
	}
	c.json("GET", "/api/backups", "", &list)
	if list.PendingRestore == nil || list.PendingRestore.Name != b.Name {
		t.Fatalf("pending restore: %+v", list.PendingRestore)
	}
	if code := c.json("DELETE", "/api/backups/restore", "", nil); code != 200 {
		t.Fatalf("cancel restore: %d", code)
	}
	c.json("GET", "/api/backups", "", &list)
	if list.PendingRestore != nil {
		t.Fatal("restore still pending")
	}
	if code := c.json("DELETE", "/api/backups/"+b.Name, "", nil); code != 200 || fileExists(filepath.Join(h.backupDir(), b.Name)) {
		t.Fatalf("delete: %d", code)
	}
}

func TestScheduledBackupsRetention(t *testing.T) {
	h, st := newDataHub(t)
	sqliteOnly(t, st)
	must(t, st.SetSetting(keyBackups, BackupSettings{Enabled: true, IntervalHours: 1, Keep: 2}))
	now := time.Now()
	h.scheduledBackup(now)
	h.scheduledBackup(now) // not due yet
	list, _ := h.ListBackups()
	if len(list) != 1 || list[0].Kind != backupAuto {
		t.Fatalf("first run: %+v", list)
	}
	manual, err := h.CreateBackup(backupManual)
	must(t, err)
	for i := 1; i <= 3; i++ {
		h.scheduledBackup(now.Add(time.Duration(i) * 2 * time.Hour))
	}
	list, _ = h.ListBackups()
	auto := 0
	for _, b := range list {
		if b.Kind == backupAuto {
			auto++
		}
	}
	if auto != 2 || !fileExists(filepath.Join(h.backupDir(), manual.Name)) {
		t.Fatalf("retention: %d auto backups, list %+v", auto, list)
	}
}

func TestRestoreStagedBackup(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "pingtxt.db")
	st, err := store.Open(dbPath)
	must(t, err)
	h := New(Config{LocalName: "hub", DataDir: dir}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	seedConfig(t, h, st)
	b, err := h.CreateBackup(backupManual)
	must(t, err)
	// Changes after the backup are undone by the restore.
	ms, _ := st.ListMonitors()
	for _, m := range ms {
		must(t, st.DeleteMonitor(m.ID))
	}
	must(t, store.CopyFile(filepath.Join(BackupDir(dir), b.Name), PendingRestorePath(dbPath)))
	st.Close()

	kept, err := ApplyPendingRestore(dbPath, dir)
	if err != nil || !strings.HasPrefix(kept, "pingtxt-pre-restore-") {
		t.Fatalf("apply: %q %v", kept, err)
	}
	if fileExists(PendingRestorePath(dbPath)) {
		t.Fatal("staged file left behind")
	}
	st, err = store.Open(dbPath)
	must(t, err)
	defer st.Close()
	ms, _ = st.ListMonitors()
	if len(ms) != 3 {
		t.Fatalf("restored monitors: %d", len(ms))
	}
	// The replaced database was kept and is a valid backup.
	info, err := store.ValidateBackup(filepath.Join(BackupDir(dir), kept))
	if err != nil || info.Monitors != 0 {
		t.Fatalf("kept copy: %+v %v", info, err)
	}

	// A broken staged file does not stop the hub from starting.
	must(t, os.WriteFile(PendingRestorePath(dbPath), []byte("junk"), 0o600))
	if _, err := ApplyPendingRestore(dbPath, dir); err == nil {
		t.Fatal("junk restore accepted")
	}
	if fileExists(PendingRestorePath(dbPath)) || !fileExists(PendingRestorePath(dbPath)+".failed") {
		t.Fatal("failed restore not moved away")
	}
}

// With PostgreSQL or MariaDB/MySQL the hub makes no backup files.
func TestBackupsNeedSQLite(t *testing.T) {
	h, st := newDataHub(t)
	if h.FileBackups() {
		t.Skip("SQLite: backups work")
	}
	c := newAPIClient(t, h, st)
	var list backupsView
	if code := c.json("GET", "/api/backups", "", &list); code != 200 || list.FileBackups || list.Driver != st.Driver() {
		t.Fatalf("list: %d %+v", code, list)
	}
	if code := c.json("POST", "/api/backups", "", nil); code != 400 {
		t.Fatalf("create: want 400, got %d", code)
	}
	if code, _, _ := c.do("POST", "/api/backups/upload", "application/octet-stream", []byte("x"), nil); code != 400 {
		t.Fatalf("upload: want 400, got %d", code)
	}
	must(t, h.store.SetSetting(keyBackups, BackupSettings{Enabled: true, IntervalHours: 1, Keep: 2}))
	h.scheduledBackup(time.Now())
	if l, _ := h.ListBackups(); len(l) != 0 {
		t.Fatalf("scheduled backup written: %+v", l)
	}
}

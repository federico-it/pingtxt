package hub

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestViewerRole(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})

	// The admin creates a monitor with secrets, a notifier and a read-only user.
	m := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{
		"name": "db", "type": "postgres", "target": "postgres://app:s3cret@db:5432/app",
	}))
	admin.do("POST", "/api/monitors", map[string]any{
		"name": "api", "type": "http", "target": "https://example.com", "headers": map[string]string{"Authorization": "Bearer xyz"},
	})
	admin.do("POST", "/api/notifiers", map[string]any{"name": "hook", "type": "webhook", "enabled": true, "config": map[string]string{"url": "https://hooks.example.com/secret"}})
	if res := admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "short", "role": "viewer"}); res.StatusCode != 400 {
		t.Fatalf("short password accepted: %d", res.StatusCode)
	}
	if res := admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "boss"}); res.StatusCode != 400 {
		t.Fatalf("bad role accepted: %d", res.StatusCode)
	}
	vu := decode[userView](t, admin.do("POST", "/api/users", map[string]any{"email": "V@b.co", "password": "password123", "role": "viewer"}))
	if vu.Role != store.RoleViewer || vu.Email != "v@b.co" {
		t.Fatalf("created: %+v", vu.User)
	}
	if res := admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "role": "viewer"}); res.StatusCode != 409 {
		t.Fatalf("duplicate accepted: %d", res.StatusCode)
	}

	viewer := newClient(t, srv.URL)
	if res := viewer.do("POST", "/api/auth/login", map[string]string{"email": "v@b.co", "password": "password123"}); res.StatusCode != 200 {
		t.Fatalf("viewer login: %d", res.StatusCode)
	}

	// Reads work, secrets are hidden.
	list := decode[[]store.Monitor](t, viewer.do("GET", "/api/monitors", nil))
	if len(list) != 2 {
		t.Fatalf("viewer monitors: %d", len(list))
	}
	for _, mm := range list {
		if mm.Type == "postgres" && mm.Target != "postgres://app:********@db:5432/app" {
			t.Fatalf("target not redacted: %s", mm.Target)
		}
		if mm.Type == "http" && mm.Headers["Authorization"] != "********" {
			t.Fatalf("headers not redacted: %v", mm.Headers)
		}
	}
	if one := decode[store.Monitor](t, viewer.do("GET", "/api/monitors/"+itoa(m.ID), nil)); one.Target != "postgres://app:********@db:5432/app" {
		t.Fatalf("detail target not redacted: %s", one.Target)
	}
	if ns := decode[[]store.Notifier](t, viewer.do("GET", "/api/notifiers", nil)); len(ns) != 1 || len(ns[0].Config) != 0 {
		t.Fatalf("notifier config visible: %+v", ns)
	}
	// Admins still see everything.
	if one := decode[store.Monitor](t, admin.do("GET", "/api/monitors/"+itoa(m.ID), nil)); one.Target != "postgres://app:s3cret@db:5432/app" {
		t.Fatalf("admin target: %s", one.Target)
	}

	// Writes and admin pages are refused.
	for _, p := range [][2]string{
		{"POST", "/api/monitors"}, {"PUT", "/api/monitors/" + itoa(m.ID)}, {"DELETE", "/api/monitors/" + itoa(m.ID)},
		{"POST", "/api/monitors/" + itoa(m.ID) + "/pause"}, {"POST", "/api/agents"}, {"POST", "/api/notifiers/test"},
		{"PUT", "/api/settings/agent-updates"}, {"GET", "/api/settings"}, {"PUT", "/api/settings/general"},
		{"GET", "/api/users"}, {"POST", "/api/users"}, {"PUT", "/api/users/1"}, {"DELETE", "/api/users/1"},
	} {
		if res := viewer.do(p[0], p[1], map[string]any{}); res.StatusCode != 403 {
			t.Fatalf("viewer %s %s: %d", p[0], p[1], res.StatusCode)
		}
	}

	// A viewer manages its own account and gets read tokens only.
	if res := viewer.do("POST", "/api/tokens", map[string]any{"name": "w", "scope": "write"}); res.StatusCode != 400 {
		t.Fatalf("viewer write token: %d", res.StatusCode)
	}
	if res := viewer.do("POST", "/api/tokens", map[string]any{"name": "r", "scope": "read"}); res.StatusCode != 201 {
		t.Fatalf("viewer read token: %d", res.StatusCode)
	}
	if res := viewer.do("POST", "/api/auth/password", map[string]string{"current": "password123", "new": "password456"}); res.StatusCode != 200 {
		t.Fatalf("viewer password change: %d", res.StatusCode)
	}
	if me := decode[map[string]any](t, viewer.do("GET", "/api/auth/me", nil)); me["user"].(map[string]any)["role"] != "viewer" {
		t.Fatalf("me role: %v", me["user"])
	}

	// An admin's write token becomes read-only when the admin is demoted.
	type created struct {
		Secret string `json:"secret"`
	}
	if res := admin.do("PUT", "/api/users/1", map[string]string{"role": "viewer"}); res.StatusCode != 400 {
		t.Fatalf("self demotion: %d", res.StatusCode)
	}
	promoted := decode[userView](t, admin.do("PUT", "/api/users/"+itoa(vu.ID), map[string]string{"role": "admin"}))
	if promoted.Role != store.RoleAdmin {
		t.Fatalf("promote: %+v", promoted.User)
	}
	rw := decode[created](t, viewer.do("POST", "/api/tokens", map[string]any{"name": "w", "scope": "write"}))
	monitor := map[string]any{"name": "x", "type": "http", "target": "https://example.com"}
	if res := bearerDo(t, srv.URL, rw.Secret, "POST", "/api/monitors", monitor); res.StatusCode != 201 {
		t.Fatalf("admin write token: %d", res.StatusCode)
	}
	if res := admin.do("PUT", "/api/users/"+itoa(vu.ID), map[string]string{"role": "viewer"}); res.StatusCode != 200 {
		t.Fatalf("demote: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, rw.Secret, "POST", "/api/monitors", monitor); res.StatusCode != 403 {
		t.Fatalf("demoted write token still writes: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, rw.Secret, "GET", "/api/monitors", nil); res.StatusCode != 200 {
		t.Fatalf("demoted token read: %d", res.StatusCode)
	}
	if res := admin.do("PUT", "/api/users/999", map[string]string{"role": "viewer"}); res.StatusCode != 404 {
		t.Fatalf("unknown user: %d", res.StatusCode)
	}
}

func TestDefaultRoleSetting(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})
	if s := decode[settingsView](t, c.do("GET", "/api/settings", nil)); s.Auth.DefaultRole != store.RoleViewer {
		t.Fatalf("default role: %q", s.Auth.DefaultRole)
	}
	s := decode[settingsView](t, c.do("PUT", "/api/settings/auth", map[string]any{"password_login": true, "default_role": "admin"}))
	if s.Auth.DefaultRole != store.RoleAdmin || h.authSettings().DefaultRole != store.RoleAdmin {
		t.Fatalf("saved role: %q", s.Auth.DefaultRole)
	}
	s = decode[settingsView](t, c.do("PUT", "/api/settings/auth", map[string]any{"password_login": true, "default_role": "root"}))
	if s.Auth.DefaultRole != store.RoleViewer {
		t.Fatalf("invalid role kept: %q", s.Auth.DefaultRole)
	}
}

func TestRedactTarget(t *testing.T) {
	for in, want := range map[string]string{
		"postgres://u:p@h:5432/db":  "postgres://u:********@h:5432/db",
		"app:pw@tcp(db:3306)/x":     "app:********@tcp(db:3306)/x",
		"redis://:pw@cache:6379":    "redis://:********@cache:6379",
		"mongodb://u:p%40ss@a,b/db": "mongodb://u:********@a,b/db",
		"https://example.com/a?b=c": "https://example.com/a?b=c",
		"https://user@example.com":  "https://user@example.com",
		"example.com:443":           "example.com:443",
		"smtp.example.com:587":      "smtp.example.com:587",
	} {
		if got := redactTarget(in); got != want {
			t.Errorf("%s: got %s want %s", in, got, want)
		}
	}
}

func TestMakeAdmin(t *testing.T) {
	h, st := newTestHub(t)
	u, _ := st.CreateUser("v@b.co", "", store.RoleViewer)
	if err := h.MakeAdmin("V@b.co"); err != nil {
		t.Fatal(err)
	}
	if got, _ := st.UserByID(u.ID); got.Role != store.RoleAdmin {
		t.Fatalf("role: %s", got.Role)
	}
	if h.MakeAdmin("none@b.co") == nil {
		t.Fatal("unknown user accepted")
	}
}

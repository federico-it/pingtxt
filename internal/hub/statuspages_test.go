package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestOverallStatus(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{nil, PageUnknown},
		{[]string{"pending"}, PageUnknown},
		{[]string{"up", "paused"}, PageOperational},
		{[]string{"up", "degraded"}, PageDegraded},
		{[]string{"up", "down"}, PagePartialOutage},
		{[]string{"down", "paused"}, PageMajorOutage},
	}
	for _, c := range cases {
		var ms []publicMonitor
		for _, s := range c.in {
			ms = append(ms, publicMonitor{Status: s})
		}
		if got := overallStatus(ms); got != c.want {
			t.Errorf("%v: want %s, got %s", c.in, c.want, got)
		}
	}
}

func TestStatusPages(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://secret.internal", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	h.HandleResult(LocalLocation, result(m.ID, true))
	h.HandleResult(LocalLocation, result(m.ID, false))

	ui := fstest.MapFS{
		"index.html":        {Data: []byte("admin")},
		"status/index.html": {Data: []byte("status page")},
	}
	srv := httptest.NewServer(h.Handler(ui))
	defer srv.Close()

	// Admin API needs a session.
	cookie := login(t, h, st)
	do := func(method, path, body, host string) (int, string) {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if host != "" {
			req.Host = host
		} else {
			req.AddCookie(cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := res.Body.Read(buf)
			sb.Write(buf[:n])
			if err != nil {
				break
			}
		}
		return res.StatusCode, sb.String()
	}

	body := `{"title":"Acme","slug":"acme","domain":"https://Status.Example.com/","published":true,"show_incidents":true,"monitor_ids":[` + itoa(m.ID) + `,` + itoa(m.ID) + `,999]}`
	code, out := do("POST", "/api/status-pages", body, "")
	if code != 201 {
		t.Fatalf("create: %d %s", code, out)
	}
	var created store.StatusPage
	_ = json.Unmarshal([]byte(out), &created)
	if created.Domain != "status.example.com" || len(created.MonitorIDs) != 1 {
		t.Fatalf("unexpected page %+v", created)
	}
	if code, _ := do("POST", "/api/status-pages", `{"title":"Dup","slug":"acme"}`, ""); code != 409 {
		t.Fatalf("duplicate slug: want 409, got %d", code)
	}
	if code, _ := do("POST", "/api/status-pages", `{"title":"Bad","slug":"Bad Slug"}`, ""); code != 400 {
		t.Fatalf("bad slug: want 400, got %d", code)
	}

	// Public payload: no targets or causes.
	code, out = do("GET", "/api/public/status/acme", "", "anyone.test")
	if code != 200 {
		t.Fatalf("public: %d %s", code, out)
	}
	if strings.Contains(out, "secret.internal") {
		t.Fatal("public payload leaks the monitor target")
	}
	var pp publicPage
	_ = json.Unmarshal([]byte(out), &pp)
	if pp.Status != PageMajorOutage || len(pp.Monitors) != 1 || len(pp.Monitors[0].Bars) != pp.Days || len(pp.Incidents) != 1 {
		t.Fatalf("unexpected public page %+v", pp)
	}

	// Custom domain serves the page at / and hides the admin API.
	if code, out := do("GET", "/", "", "status.example.com"); code != 200 || out != "status page" {
		t.Fatalf("custom domain root: %d %q", code, out)
	}
	if code, _ := do("GET", "/api/public/status", "", "status.example.com:443"); code != 200 {
		t.Fatalf("custom domain api: %d", code)
	}
	if code, _ := do("GET", "/api/setup", "", "status.example.com"); code != 404 {
		t.Fatalf("admin api on custom domain: want 404, got %d", code)
	}
	// /status/<slug>/ on the hub itself.
	if code, out := do("GET", "/status/acme/", "", "hub.test"); code != 200 || out != "status page" {
		t.Fatalf("status path: %d %q", code, out)
	}

	// Drafts are hidden from visitors but visible to logged-in users.
	draft := strings.Replace(body, `"published":true`, `"published":false`, 1)
	if code, out := do("PUT", "/api/status-pages/"+itoa(created.ID), draft, ""); code != 200 {
		t.Fatalf("update: %d %s", code, out)
	}
	if code, _ := do("GET", "/api/public/status/acme", "", "anyone.test"); code != 404 {
		t.Fatalf("draft for visitors: want 404, got %d", code)
	}
	if code, _ := do("GET", "/api/public/status/acme", "", ""); code != 200 {
		t.Fatalf("draft preview: want 200, got %d", code)
	}

	// Deleting the monitor removes it from the page.
	if err := st.DeleteMonitor(m.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.StatusPage(created.ID)
	if len(got.MonitorIDs) != 0 {
		t.Fatalf("monitor should be gone from the page, got %v", got.MonitorIDs)
	}
	if code, _ := do("DELETE", "/api/status-pages/"+itoa(created.ID), "", ""); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	if code, _ := do("GET", "/", "", "status.example.com"); code != 200 {
		t.Fatalf("after delete the domain falls back to the hub, got %d", code)
	}
	if slug := h.pageSlugForHost(&http.Request{Host: "status.example.com"}); slug != "" {
		t.Fatal("domain should be unregistered after delete")
	}
}

func TestStatusPageDomainCannotBeHub(t *testing.T) {
	h, _ := newTestHub(t)
	r := httptest.NewRequest("POST", "http://hub.example.com/api/status-pages", nil)
	p := &store.StatusPage{Title: "x", Slug: "x", Domain: "hub.example.com"}
	if err := h.validateStatusPage(r, p); err == nil {
		t.Fatal("using the hub's own host must be rejected")
	}
}

func login(t *testing.T, h *Hub, st *store.Store) *http.Cookie {
	t.Helper()
	u, err := st.CreateUser("a@b.c", "", store.RoleAdmin)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := h.startSession(rec, httptest.NewRequest("GET", "/", nil), u); err != nil {
		t.Fatal(err)
	}
	return rec.Result().Cookies()[0]
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// Other sites (pingtxt.com shows the state of its own status page) can read
// published pages without a password.
func TestPublicStatusCORS(t *testing.T) {
	h, st := newTestHub(t)
	sp := &store.StatusPage{Title: "Acme", Slug: "acme", Published: true}
	if err := st.CreateStatusPage(sp); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	get := func() *http.Response {
		req, _ := http.NewRequest("GET", srv.URL+"/api/public/status/acme", nil)
		req.Header.Set("Origin", "https://pingtxt.com")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res
	}
	if res := get(); res.StatusCode != 200 || res.Header.Get("Access-Control-Allow-Origin") != "*" {
		t.Fatalf("published: %d, allow origin %q", res.StatusCode, res.Header.Get("Access-Control-Allow-Origin"))
	}
}

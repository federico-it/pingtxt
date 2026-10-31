package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestPagePassword(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	ui := fstest.MapFS{"index.html": {Data: []byte("admin")}, "status/index.html": {Data: []byte("status page")}}
	srv := httptest.NewServer(h.Handler(ui))
	defer srv.Close()
	admin := login(t, h, st)

	do := func(method, path, body, host string, cookies ...*http.Cookie) (*http.Response, string) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		if host != "" {
			req.Host = host
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, string(b)
	}

	// A protected page needs a password.
	res, body := do("POST", "/api/status-pages", `{"slug":"acme","title":"Acme","published":true,"protected":true,"monitor_ids":[`+itoa(m.ID)+`]}`, "", admin)
	if res.StatusCode != 400 {
		t.Fatalf("protected without password: %d %s", res.StatusCode, body)
	}
	res, body = do("POST", "/api/status-pages", `{"slug":"acme","title":"Acme","published":true,"domain":"status.acme.test","password":"hunter22","monitor_ids":[`+itoa(m.ID)+`]}`, "", admin)
	if res.StatusCode != 201 || strings.Contains(body, "hunter22") || strings.Contains(body, "$2") {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var page store.StatusPage
	json.Unmarshal([]byte(body), &page)
	if !page.Protected {
		t.Fatal("page should be protected")
	}

	// Anonymous visitors get the password form; logged-in users see the page.
	res, body = do("GET", "/api/public/status/acme", "", "")
	if res.StatusCode != 403 || !strings.Contains(body, `"password_required":true`) || strings.Contains(body, "api") {
		t.Fatalf("anonymous: %d %s", res.StatusCode, body)
	}
	if res, _ = do("GET", "/api/public/status/acme", "", "", admin); res.StatusCode != 200 {
		t.Fatalf("logged in: %d", res.StatusCode)
	}
	if res, _ = do("GET", "/api/badge/acme/status.svg", "", ""); res.StatusCode != 403 {
		t.Fatalf("badge: %d", res.StatusCode)
	}
	if res, _ = do("GET", "/api/badge/acme/"+itoa(m.ID)+"/uptime.svg", "", ""); res.StatusCode != 403 {
		t.Fatalf("monitor badge: %d", res.StatusCode)
	}
	if res, _ = do("POST", "/api/public/status/acme/subscribe", `{"email":"a@b.test"}`, ""); res.StatusCode != 403 {
		t.Fatalf("subscribe: %d", res.StatusCode)
	}
	if res, _ = do("GET", "/api/public/status", "", "status.acme.test"); res.StatusCode != 403 {
		t.Fatalf("custom domain: %d", res.StatusCode)
	}
	// The hub session does not open the page on its custom domain.
	if res, _ = do("GET", "/api/public/status", "", "status.acme.test", admin); res.StatusCode != 403 {
		t.Fatalf("custom domain with session: %d", res.StatusCode)
	}

	// Wrong then right password.
	if res, _ = do("POST", "/api/public/status/acme/unlock", `{"password":"nope"}`, ""); res.StatusCode != 403 {
		t.Fatalf("wrong password: %d", res.StatusCode)
	}
	res, _ = do("POST", "/api/public/status/acme/unlock", `{"password":"hunter22"}`, "")
	if res.StatusCode != 200 || len(res.Cookies()) != 1 || !res.Cookies()[0].HttpOnly {
		t.Fatalf("unlock: %d %v", res.StatusCode, res.Cookies())
	}
	visitor := res.Cookies()[0]
	if res, _ = do("GET", "/api/public/status/acme", "", "", visitor); res.StatusCode != 200 || !strings.Contains(res.Header.Get("Cache-Control"), "no-store") {
		t.Fatalf("unlocked: %d %q", res.StatusCode, res.Header.Get("Cache-Control"))
	}
	if res, _ = do("GET", "/api/badge/acme/status.svg", "", "", visitor); res.StatusCode != 200 {
		t.Fatalf("unlocked badge: %d", res.StatusCode)
	}
	res, _ = do("POST", "/api/public/status/unlock", `{"password":"hunter22"}`, "status.acme.test")
	if res.StatusCode != 200 || len(res.Cookies()) != 1 {
		t.Fatalf("unlock on custom domain: %d", res.StatusCode)
	}
	if res, _ = do("GET", "/api/public/status", "", "status.acme.test", res.Cookies()[0]); res.StatusCode != 200 {
		t.Fatalf("custom domain unlocked: %d", res.StatusCode)
	}
	// A forged cookie does not work.
	forged := &http.Cookie{Name: visitor.Name, Value: "9999999999.AAAA"}
	if res, _ = do("GET", "/api/public/status/acme", "", "", forged); res.StatusCode != 403 {
		t.Fatalf("forged: %d", res.StatusCode)
	}

	// Saving without a password keeps it; a new password logs visitors out.
	upd := `{"slug":"acme","title":"Acme 2","published":true,"domain":"status.acme.test","protected":true,"monitor_ids":[` + itoa(m.ID) + `]}`
	if res, body = do("PUT", "/api/status-pages/"+itoa(page.ID), upd, "", admin); res.StatusCode != 200 || !strings.Contains(body, `"protected":true`) {
		t.Fatalf("keep: %d %s", res.StatusCode, body)
	}
	if res, _ = do("GET", "/api/public/status/acme", "", "", visitor); res.StatusCode != 200 {
		t.Fatalf("kept password: %d", res.StatusCode)
	}
	upd = `{"slug":"acme","title":"Acme 2","published":true,"protected":true,"password":"another1","monitor_ids":[` + itoa(m.ID) + `]}`
	if res, _ = do("PUT", "/api/status-pages/"+itoa(page.ID), upd, "", admin); res.StatusCode != 200 {
		t.Fatalf("change: %d", res.StatusCode)
	}
	if res, _ = do("GET", "/api/public/status/acme", "", "", visitor); res.StatusCode != 403 {
		t.Fatalf("old cookie after change: %d", res.StatusCode)
	}
	// Removing the password opens the page.
	upd = `{"slug":"acme","title":"Acme 2","published":true,"protected":false,"monitor_ids":[` + itoa(m.ID) + `]}`
	if res, body = do("PUT", "/api/status-pages/"+itoa(page.ID), upd, "", admin); res.StatusCode != 200 || !strings.Contains(body, `"protected":false`) {
		t.Fatalf("remove: %d %s", res.StatusCode, body)
	}
	if res, _ = do("GET", "/api/public/status/acme", "", ""); res.StatusCode != 200 {
		t.Fatalf("open page: %d", res.StatusCode)
	}
}

func TestPagePasswordRateLimit(t *testing.T) {
	h, st := newTestHub(t)
	hash, err := hashPagePassword("hunter22")
	if err != nil {
		t.Fatal(err)
	}
	p := &store.StatusPage{Slug: "acme", Title: "Acme", Published: true, PasswordHash: hash}
	if err := st.CreateStatusPage(p); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
	try := func(pw string) int {
		res, err := http.Post(srv.URL+"/api/public/status/acme/unlock", "application/json", strings.NewReader(`{"password":"`+pw+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	for i := range pagePassFailAddr {
		if c := try("wrong"); c != 403 {
			t.Fatalf("attempt %d: %d", i, c)
		}
	}
	// Blocked, even with the right password.
	if c := try("hunter22"); c != 429 {
		t.Fatalf("after %d failures: %d", pagePassFailAddr, c)
	}
}

func TestImportProtectedPageWithoutSecrets(t *testing.T) {
	h, _ := newTestHub(t)
	p := store.StatusPage{Slug: "acme", Title: "Acme", Published: true}
	p.Protected = true
	doc := &ConfigDoc{Format: configFormat, Version: configVersion, StatusPages: []cfgStatusPage{{StatusPage: p}}}
	if _, err := h.ImportConfig(doc, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	got, err := h.store.StatusPageBySlug("acme")
	if err != nil {
		t.Fatal(err)
	}
	if got.Published {
		t.Fatal("a protected page imported without its password must be a draft")
	}
}

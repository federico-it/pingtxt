package hub

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

// fakeIdP is a minimal OIDC provider.
func fakeIdP(t *testing.T, sub, email string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			json.NewEncoder(w).Encode(map[string]string{
				"issuer": srv.URL, "authorization_endpoint": srv.URL + "/authorize",
				"token_endpoint": srv.URL + "/token", "userinfo_endpoint": srv.URL + "/userinfo",
			})
		case "/token":
			r.ParseForm()
			if r.Form.Get("code") != "good-code" || r.Form.Get("code_verifier") == "" {
				w.WriteHeader(400)
				json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 3600})
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer at" {
				w.WriteHeader(401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"sub": sub, "email": email, "email_verified": "true"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

type testClient struct {
	t    *testing.T
	base string
	c    *http.Client
}

func newClient(t *testing.T, base string) *testClient {
	jar, _ := cookiejar.New(nil)
	return &testClient{t: t, base: base, c: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (c *testClient) do(method, path string, body any) *http.Response {
	c.t.Helper()
	var rd *strings.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = strings.NewReader(string(b))
	} else {
		rd = strings.NewReader("")
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.c.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

// login runs the OAuth dance against the fake IdP and returns the final redirect.
func (c *testClient) oauth(path, code string) string {
	c.t.Helper()
	res := c.do("GET", path, nil)
	if res.StatusCode != 302 {
		c.t.Fatalf("start: status %d", res.StatusCode)
	}
	auth, _ := url.Parse(res.Header.Get("Location"))
	q := auth.Query()
	if q.Get("code_challenge") == "" || q.Get("state") == "" {
		c.t.Fatalf("start: missing pkce/state in %s", auth)
	}
	cb := q.Get("redirect_uri") + "?code=" + code + "&state=" + q.Get("state")
	cb = strings.Replace(cb, c.base, "", 1)
	res = c.do("GET", cb, nil)
	return res.Header.Get("Location")
}

func TestOIDCLoginFlow(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	idp := fakeIdP(t, "user-1", "Admin@Example.com")

	admin := newClient(t, srv.URL)
	if res := admin.do("POST", "/api/setup", map[string]string{"email": "admin@example.com", "password": "password123"}); res.StatusCode != 201 {
		t.Fatalf("setup: %d", res.StatusCode)
	}
	cfg := map[string]any{
		"password_login": true, "allow_signup": false,
		"providers": map[string]any{"oidc": map[string]any{"enabled": true, "name": "Authentik", "issuer": idp.URL, "client_id": "cid", "client_secret": "s3cret"}},
	}
	if res := admin.do("PUT", "/api/settings/auth", cfg); res.StatusCode != 200 {
		t.Fatalf("put auth: %d", res.StatusCode)
	}

	// Secrets never come back from the API.
	res := admin.do("GET", "/api/settings", nil)
	var sv settingsView
	json.NewDecoder(res.Body).Decode(&sv)
	for _, p := range sv.Auth.Providers {
		if p.ID == "oidc" && (!p.SecretSet || p.ClientSecret != "" || !strings.HasSuffix(p.CallbackURL, "/api/auth/oauth/oidc/callback")) {
			t.Fatalf("bad provider view: %+v", p)
		}
	}
	// Saving with an empty secret keeps the stored one.
	cfg["providers"].(map[string]any)["oidc"].(map[string]any)["client_secret"] = ""
	admin.do("PUT", "/api/settings/auth", cfg)
	if h.authSettings().Providers["oidc"].ClientSecret != "s3cret" {
		t.Fatal("secret was cleared")
	}

	// Public options list the provider.
	res = newClient(t, srv.URL).do("GET", "/api/auth/options", nil)
	var opts struct {
		PasswordLogin bool                `json:"password_login"`
		Providers     []map[string]string `json:"providers"`
	}
	json.NewDecoder(res.Body).Decode(&opts)
	if !opts.PasswordLogin || len(opts.Providers) != 1 || opts.Providers[0]["label"] != "Authentik" {
		t.Fatalf("options: %+v", opts)
	}

	// Existing account is matched by email and linked.
	visitor := newClient(t, srv.URL)
	if loc := visitor.oauth("/api/auth/oauth/oidc/start", "good-code"); loc != "/" {
		t.Fatalf("login redirect: %s", loc)
	}
	if res := visitor.do("GET", "/api/auth/me", nil); res.StatusCode != 200 {
		t.Fatalf("me after sso: %d", res.StatusCode)
	}
	u, err := st.IdentityUser("oidc", "user-1")
	if err != nil || u.Email != "admin@example.com" {
		t.Fatalf("identity not linked: %v %v", u, err)
	}

	// Bad code or replayed state is rejected.
	if loc := newClient(t, srv.URL).oauth("/api/auth/oauth/oidc/start", "bad-code"); !strings.HasPrefix(loc, "/login/?error=") {
		t.Fatalf("bad code redirect: %s", loc)
	}
	if res := newClient(t, srv.URL).do("GET", "/api/auth/oauth/oidc/callback?code=good-code&state=forged", nil); !strings.HasPrefix(res.Header.Get("Location"), "/login/?error=") {
		t.Fatal("forged state accepted")
	}
}

func TestOIDCSignup(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	idp := fakeIdP(t, "user-2", "new@corp.io")
	st.CreateUser("admin@example.com", "x", store.RoleAdmin)
	a := defaultAuth()
	a.Providers["oidc"] = ProviderSettings{Enabled: true, Issuer: idp.URL, ClientID: "cid", ClientSecret: "s"}
	h.auth = a

	if loc := newClient(t, srv.URL).oauth("/api/auth/oauth/oidc/start", "good-code"); !strings.Contains(loc, "error=") {
		t.Fatalf("unknown user should be refused when sign-up is off: %s", loc)
	}
	a.AllowSignup, a.AllowedDomains = true, []string{"other.io"}
	h.auth = a
	if loc := newClient(t, srv.URL).oauth("/api/auth/oauth/oidc/start", "good-code"); !strings.Contains(loc, "error=") {
		t.Fatalf("domain not in allow list should be refused: %s", loc)
	}
	a.AllowedDomains = []string{"corp.io"}
	h.auth = a
	if loc := newClient(t, srv.URL).oauth("/api/auth/oauth/oidc/start", "good-code"); loc != "/" {
		t.Fatalf("sign-up failed: %s", loc)
	}
	if u, err := st.UserByEmail("new@corp.io"); err != nil || u.Role != store.RoleViewer {
		t.Fatalf("user not created as viewer: %+v %v", u, err)
	}
}

func TestPasswordLoginToggle(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})

	// Cannot disable password login without a provider.
	if res := c.do("PUT", "/api/settings/auth", map[string]any{"password_login": false}); res.StatusCode != 400 {
		t.Fatalf("expected 400, got %d", res.StatusCode)
	}
	cfg := map[string]any{"password_login": false, "providers": map[string]any{"github": map[string]any{"enabled": true, "client_id": "id", "client_secret": "sec"}}}
	if res := c.do("PUT", "/api/settings/auth", cfg); res.StatusCode != 200 {
		t.Fatalf("put: %d", res.StatusCode)
	}
	if res := newClient(t, srv.URL).do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"}); res.StatusCode != 403 {
		t.Fatalf("password login should be disabled, got %d", res.StatusCode)
	}
	if err := h.EnablePasswordLogin(); err != nil {
		t.Fatal(err)
	}
	if res := newClient(t, srv.URL).do("POST", "/api/auth/login", map[string]string{"email": "a@b.co", "password": "password123"}); res.StatusCode != 200 {
		t.Fatalf("recovery failed, got %d", res.StatusCode)
	}
}

func TestGeneralSettings(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	c.do("POST", "/api/setup", map[string]string{"email": "a@b.co", "password": "password123"})
	if res := c.do("PUT", "/api/settings/general", map[string]any{"public_url": "ftp://x"}); res.StatusCode != 400 {
		t.Fatalf("bad url accepted: %d", res.StatusCode)
	}
	res := c.do("PUT", "/api/settings/general", map[string]any{"public_url": "https://status.example.com/", "local_name": "eu-1", "local_enabled": false, "retention_days": 7})
	if res.StatusCode != 200 {
		t.Fatalf("put general: %d", res.StatusCode)
	}
	g := h.generalSettings()
	if g.PublicURL != "https://status.example.com" || h.localEnabled() || h.localName() != "eu-1" || g.MonitorDefaults.IntervalSec != 60 {
		t.Fatalf("settings not applied: %+v", g)
	}
	res = c.do("GET", "/api/agents", nil)
	var ag struct {
		HubURL string `json:"hub_url"`
	}
	json.NewDecoder(res.Body).Decode(&ag)
	if ag.HubURL != "https://status.example.com" {
		t.Fatalf("hub url: %s", ag.HubURL)
	}
}

func TestHubURLWithoutScheme(t *testing.T) {
	st := openTestStore(t, t.TempDir())
	h := New(Config{LocalName: "hub", PublicURL: "status.example.com"}, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := h.reloadAll(); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest("GET", "/", nil)
	if got := h.hubURL(r); got != "http://status.example.com" {
		t.Fatalf("plain request: %q", got)
	}
	r.Header.Set("X-Forwarded-Proto", "https")
	if got := h.hubURL(r); got != "https://status.example.com" {
		t.Fatalf("behind https proxy: %q", got)
	}
}

package hub

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// TestNativeAppOAuth runs the flow of the iOS app: built-in client, custom
// scheme redirect, tokens on the REST API, refresh rotation with reuse
// detection and sign out.
func TestNativeAppOAuth(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})

	verifier := "ios-verifier-0123456789-abcdefghijklmnopqrstuvwxyz"
	sum := sha256.Sum256([]byte(verifier))
	authURL := url.Values{"response_type": {"code"}, "client_id": {iosClientID}, "redirect_uri": {iosRedirectURI}, "state": {"s1"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"scope": {"read write"}, "resource": {srv.URL + "/api"}}
	token := func(form url.Values) (int, map[string]any) {
		t.Helper()
		res, err := http.PostForm(srv.URL+"/oauth/token", form)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}
	signIn := func(c *testClient) map[string]any {
		t.Helper()
		res := c.do("GET", "/oauth/authorize?"+authURL.Encode(), nil)
		page, _ := io.ReadAll(res.Body)
		m := regexp.MustCompile(`name="request" value="([^"]+)"`).FindSubmatch(page)
		if res.StatusCode != 200 || m == nil || !strings.Contains(string(page), "Pingtxt for iOS") || strings.Contains(string(page), "MCP") {
			t.Fatalf("consent page: %d %s", res.StatusCode, page)
		}
		req, _ := http.NewRequest("POST", srv.URL+"/oauth/authorize", strings.NewReader(url.Values{"request": {string(m[1])}, "scope": {"write"}, "action": {"allow"}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := c.c.Do(req)
		if err != nil || res.StatusCode != 302 {
			t.Fatalf("answer: %v %d", err, res.StatusCode)
		}
		loc, _ := url.Parse(res.Header.Get("Location"))
		if loc.Scheme != "pingtxt" || loc.Host != "oauth" || loc.Query().Get("state") != "s1" || loc.Query().Get("iss") != srv.URL {
			t.Fatalf("redirect: %s", loc)
		}
		st, tok := token(url.Values{"grant_type": {"authorization_code"}, "code": {loc.Query().Get("code")}, "client_id": {iosClientID},
			"redirect_uri": {iosRedirectURI}, "code_verifier": {verifier}})
		if st != 200 {
			t.Fatalf("token: %d %+v", st, tok)
		}
		return tok
	}

	// The built-in client needs no registration and only redirects to the app.
	bad := url.Values{}
	for k, v := range authURL {
		bad[k] = v
	}
	bad.Set("redirect_uri", "evil://oauth")
	if res := admin.do("GET", "/oauth/authorize?"+bad.Encode(), nil); res.StatusCode != 400 {
		t.Fatalf("foreign redirect: %d", res.StatusCode)
	}
	prm := decode[map[string]any](t, admin.do("GET", "/.well-known/oauth-protected-resource/api", nil))
	if prm["resource"] != srv.URL+"/api" {
		t.Fatalf("api resource metadata: %+v", prm)
	}

	tok := signIn(admin)
	if tok["scope"] != "write" {
		t.Fatalf("admin scope: %+v", tok)
	}
	access := tok["access_token"].(string)

	// The token works on the REST API, writes included, but not on account endpoints.
	if res := bearerDo(t, srv.URL, access, "GET", "/api/auth/me", nil); res.StatusCode != 200 {
		t.Fatalf("me: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, access, "POST", "/api/monitors", map[string]any{"name": "api", "type": "http", "target": "https://example.com"}); res.StatusCode != 201 {
		t.Fatalf("create monitor: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, access, "GET", "/api/tokens", nil); res.StatusCode != 403 {
		t.Fatalf("account endpoint: %d", res.StatusCode)
	}

	// The app shows up among connected apps.
	grants := decode[[]map[string]any](t, admin.do("GET", "/api/oauth/grants", nil))
	if len(grants) != 1 || grants[0]["client_id"] != iosClientID || grants[0]["client_name"] != "Pingtxt for iOS" {
		t.Fatalf("grants: %+v", grants)
	}

	// Refresh rotates; presenting the old refresh token again ends the grant.
	refresh := tok["refresh_token"].(string)
	st, tok2 := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {iosClientID}})
	if st != 200 {
		t.Fatalf("refresh: %d %+v", st, tok2)
	}
	if res := bearerDo(t, srv.URL, tok2["access_token"].(string), "GET", "/api/monitors", nil); res.StatusCode != 200 {
		t.Fatalf("new access token: %d", res.StatusCode)
	}
	if st, out := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {iosClientID}}); st != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("reused refresh token: %d %+v", st, out)
	}
	if res := bearerDo(t, srv.URL, tok2["access_token"].(string), "GET", "/api/monitors", nil); res.StatusCode != 401 {
		t.Fatalf("grant not revoked after reuse: %d", res.StatusCode)
	}
	if st, _ := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok2["refresh_token"].(string)}, "client_id": {iosClientID}}); st != 400 {
		t.Fatalf("refresh after reuse: %d", st)
	}

	// Sign out: the app revokes its refresh token.
	tok = signIn(admin)
	res, _ := http.PostForm(srv.URL+"/oauth/revoke", url.Values{"token": {tok["refresh_token"].(string)}, "client_id": {iosClientID}})
	if res.StatusCode != 200 {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, tok["access_token"].(string), "GET", "/api/monitors", nil); res.StatusCode != 401 {
		t.Fatalf("revoked token: %d", res.StatusCode)
	}

	// A read-only user gets a read token: reads work, writes do not.
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	viewer := newClient(t, srv.URL)
	viewer.do("POST", "/api/auth/login", map[string]string{"email": "v@b.co", "password": "password123"})
	vt := signIn(viewer)
	if vt["scope"] != "read" {
		t.Fatalf("viewer scope: %+v", vt)
	}
	if res := bearerDo(t, srv.URL, vt["access_token"].(string), "GET", "/api/monitors", nil); res.StatusCode != 200 {
		t.Fatalf("viewer read: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, vt["access_token"].(string), "POST", "/api/monitors", map[string]any{"name": "x", "type": "http", "target": "https://example.com"}); res.StatusCode != 403 {
		t.Fatalf("viewer write: %d", res.StatusCode)
	}

	// The cleanup never removes the built-in client.
	h.cleanOAuth(time.Now().Add(48 * time.Hour))
	if _, err := h.store.OAuthClient(iosClientID); err != nil {
		t.Fatalf("built-in client removed: %v", err)
	}
}

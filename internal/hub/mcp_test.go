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

type mcpClient struct {
	t     *testing.T
	url   string
	token string
	next  int
}

func (c *mcpClient) post(body string) *http.Response {
	c.t.Helper()
	req, _ := http.NewRequest("POST", c.url+"/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatal(err)
	}
	return res
}

// rpc sends a request and returns its result, failing on JSON-RPC errors.
func (c *mcpClient) rpc(method string, params any) map[string]any {
	c.t.Helper()
	c.next++
	b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": c.next, "method": method, "params": params})
	res := c.post(string(b))
	if res.StatusCode != 200 {
		c.t.Fatalf("%s: HTTP %d", method, res.StatusCode)
	}
	var out struct {
		ID     int            `json:"id"`
		Result map[string]any `json:"result"`
		Error  *rpcError      `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		c.t.Fatal(err)
	}
	if out.Error != nil {
		c.t.Fatalf("%s: %+v", method, out.Error)
	}
	if out.ID != c.next {
		c.t.Fatalf("id %d, want %d", out.ID, c.next)
	}
	return out.Result
}

// call runs a tool and returns its text and error flag.
func (c *mcpClient) call(name string, args map[string]any) (string, bool) {
	c.t.Helper()
	r := c.rpc("tools/call", map[string]any{"name": name, "arguments": args})
	content := r["content"].([]any)
	return content[0].(map[string]any)["text"].(string), r["isError"] == true
}

func (c *mcpClient) callOK(name string, args map[string]any) any {
	c.t.Helper()
	text, isErr := c.call(name, args)
	if isErr {
		c.t.Fatalf("%s: %s", name, text)
	}
	var v any
	if err := json.Unmarshal([]byte(text), &v); err != nil {
		c.t.Fatalf("%s: %v in %s", name, err, text)
	}
	return v
}

func (c *mcpClient) toolNames() []string {
	c.t.Helper()
	var names []string
	for _, t := range c.rpc("tools/list", map[string]any{})["tools"].([]any) {
		names = append(names, t.(map[string]any)["name"].(string))
	}
	return names
}

func TestMCPServer(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	token := func(c *testClient, scope string) string {
		return decode[struct {
			Secret string `json:"secret"`
		}](t, c.do("POST", "/api/tokens", map[string]any{"name": "mcp", "scope": scope})).Secret
	}
	rw := &mcpClient{t: t, url: srv.URL, token: token(admin, "write")}
	ro := &mcpClient{t: t, url: srv.URL, token: token(admin, "read")}

	// No token, a session cookie or a bad token: refused with a hint.
	if res := (&mcpClient{t: t, url: srv.URL}).post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`); res.StatusCode != 401 || res.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("no token: %d", res.StatusCode)
	}
	if res := admin.do("POST", "/mcp", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}); res.StatusCode != 401 {
		t.Fatalf("session cookie accepted: %d", res.StatusCode)
	}
	if res := (&mcpClient{t: t, url: srv.URL, token: "ptk_nope"}).post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`); res.StatusCode != 401 {
		t.Fatalf("bad token: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, rw.token, "GET", "/mcp", nil); res.StatusCode != 405 {
		t.Fatalf("GET: %d", res.StatusCode)
	}

	// Handshake.
	init := rw.rpc("initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "test", "version": "1"}})
	if init["protocolVersion"] != "2025-06-18" || init["serverInfo"].(map[string]any)["name"] != "pingtxt" {
		t.Fatalf("initialize: %+v", init)
	}
	if v := rw.rpc("initialize", map[string]any{"protocolVersion": "1999-01-01"})["protocolVersion"]; v != mcpVersions[0] {
		t.Fatalf("fallback version: %v", v)
	}
	if res := rw.post(`{"jsonrpc":"2.0","method":"notifications/initialized"}`); res.StatusCode != 202 {
		t.Fatalf("notification: %d", res.StatusCode)
	}
	var unknown struct{ Error *rpcError }
	json.NewDecoder(rw.post(`{"jsonrpc":"2.0","id":9,"method":"nope"}`).Body).Decode(&unknown)
	if unknown.Error == nil || unknown.Error.Code != rpcMethodNotFound {
		t.Fatalf("unknown method: %+v", unknown)
	}

	// Write tools are listed only for an admin with a write token.
	if names := rw.toolNames(); !strings.Contains(strings.Join(names, ","), "create_monitor") {
		t.Fatalf("write tools missing: %v", names)
	}
	if names := ro.toolNames(); strings.Contains(strings.Join(names, ","), "create_monitor") || len(names) == 0 {
		t.Fatalf("read token tools: %v", names)
	}
	if text, isErr := ro.call("create_monitor", map[string]any{"name": "x", "type": "http", "target": "https://example.com"}); !isErr || !strings.Contains(text, "read-only") {
		t.Fatalf("read token wrote: %s", text)
	}

	// Create, read, update, pause, delete.
	created := rw.callOK("create_monitor", map[string]any{"name": "api", "type": "http", "target": "https://example.com", "interval_sec": 30}).(map[string]any)
	id := created["id"].(float64)
	if _, ok := created["created_at"].(string); !ok {
		t.Fatalf("times not ISO: %v", created["created_at"])
	}
	if text, isErr := rw.call("create_monitor", map[string]any{"name": "bad", "type": "http", "target": "nope"}); !isErr || !strings.Contains(text, "http(s) URL") {
		t.Fatalf("validation error: %s", text)
	}
	list := ro.callOK("list_monitors", map[string]any{"query": "API"}).(map[string]any)
	if list["count"].(float64) != 1 {
		t.Fatalf("list: %+v", list)
	}
	if m := list["monitors"].([]any)[0].(map[string]any); m["bars"] != nil || m["name"] != "api" {
		t.Fatalf("compact monitor: %+v", m)
	}
	if n := ro.callOK("list_monitors", map[string]any{"status": "down"}).(map[string]any)["count"]; n.(float64) != 0 {
		t.Fatalf("status filter: %v", n)
	}
	got := ro.callOK("get_monitor", map[string]any{"id": id}).(map[string]any)
	if got["interval_sec"].(float64) != 30 || got["bars"] != nil {
		t.Fatalf("get: %+v", got)
	}
	upd := rw.callOK("update_monitor", map[string]any{"id": id, "name": "api v2"}).(map[string]any)
	if upd["name"] != "api v2" || upd["interval_sec"].(float64) != 30 || upd["target"] != "https://example.com" {
		t.Fatalf("update lost fields: %+v", upd)
	}
	if p := rw.callOK("pause_monitor", map[string]any{"id": id, "paused": true}).(map[string]any); p["paused"] != true {
		t.Fatalf("pause: %+v", p)
	}
	rw.callOK("get_overview", nil)
	rw.callOK("list_incidents", map[string]any{"open_only": true})
	rw.callOK("get_monitor_results", map[string]any{"id": id, "limit": 5})

	// Maintenance with ISO times.
	start := time.Now().Add(time.Hour).UTC()
	mw := rw.callOK("create_maintenance", map[string]any{"name": "upgrade", "start": start.Format(time.RFC3339), "duration_min": 30, "monitor_ids": []any{id}}).(map[string]any)
	if mw["end_at"] != start.Add(30*time.Minute).Format(time.RFC3339) {
		t.Fatalf("maintenance: %+v", mw)
	}
	if l := rw.callOK("list_maintenances", nil).([]any); len(l) != 1 {
		t.Fatalf("maintenances: %v", l)
	}
	rw.callOK("delete_maintenance", map[string]any{"id": mw["id"]})
	rw.callOK("delete_monitor", map[string]any{"id": id})
	if text, isErr := rw.call("get_monitor", map[string]any{"id": id}); !isErr || !strings.Contains(text, "not found") {
		t.Fatalf("deleted monitor: %s", text)
	}

	// A viewer's write token cannot change anything.
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	viewer := newClient(t, srv.URL)
	viewer.do("POST", "/api/auth/login", map[string]string{"email": "v@b.co", "password": "password123"})
	vt := &mcpClient{t: t, url: srv.URL, token: token(viewer, "read")}
	if names := vt.toolNames(); strings.Contains(strings.Join(names, ","), "delete_monitor") {
		t.Fatalf("viewer tools: %v", names)
	}
	if text, isErr := vt.call("delete_monitor", map[string]any{"id": 1}); !isErr || !strings.Contains(text, "read-only") {
		t.Fatalf("viewer delete: %s", text)
	}

	// Batches.
	res := rw.post(`[{"jsonrpc":"2.0","id":1,"method":"ping"},{"jsonrpc":"2.0","method":"notifications/initialized"},{"jsonrpc":"2.0","id":2,"method":"tools/list"}]`)
	var batch []map[string]any
	json.NewDecoder(res.Body).Decode(&batch)
	if len(batch) != 2 {
		t.Fatalf("batch: %+v", batch)
	}
}

func TestMCPOAuth(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	admin.do("POST", "/api/monitors", map[string]any{"name": "api", "type": "http", "target": "https://example.com"})

	// Discovery starts from a 401 on /mcp.
	res := (&mcpClient{t: t, url: srv.URL}).post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`)
	wa := res.Header.Get("WWW-Authenticate")
	if res.StatusCode != 401 || !strings.Contains(wa, `resource_metadata="`+srv.URL+`/.well-known/oauth-protected-resource/mcp"`) {
		t.Fatalf("401: %d %s", res.StatusCode, wa)
	}
	prm := decode[map[string]any](t, admin.do("GET", "/.well-known/oauth-protected-resource/mcp", nil))
	if prm["resource"] != srv.URL+"/mcp" || prm["authorization_servers"].([]any)[0] != srv.URL {
		t.Fatalf("resource metadata: %+v", prm)
	}
	asm := decode[map[string]any](t, admin.do("GET", "/.well-known/oauth-authorization-server", nil))
	if asm["token_endpoint"] != srv.URL+"/oauth/token" || asm["code_challenge_methods_supported"].([]any)[0] != "S256" {
		t.Fatalf("server metadata: %+v", asm)
	}

	// Dynamic client registration.
	if res := admin.do("POST", "/oauth/register", map[string]any{"redirect_uris": []string{"http://evil.example/cb"}}); res.StatusCode != 400 {
		t.Fatalf("plain http redirect accepted: %d", res.StatusCode)
	}
	const redirect = "https://claude.ai/api/mcp/auth_callback"
	reg := decode[map[string]any](t, admin.do("POST", "/oauth/register", map[string]any{"client_name": "Claude", "redirect_uris": []string{redirect, "http://localhost:3334/cb"}}))
	clientID := reg["client_id"].(string)

	verifier := "a-long-random-verifier-string-0123456789-abcdefghij"
	sum := sha256.Sum256([]byte(verifier))
	authURL := func(c *testClient, redirectURI, scope string) string {
		q := url.Values{"response_type": {"code"}, "client_id": {clientID}, "redirect_uri": {redirectURI}, "state": {"xyz"},
			"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
			"scope": {scope}, "resource": {srv.URL + "/mcp"}}
		return "/oauth/authorize?" + q.Encode()
	}
	// consent opens the consent page and answers it, returning the redirect.
	consent := func(c *testClient, redirectURI, scope, action string) *url.URL {
		t.Helper()
		res := c.do("GET", authURL(c, redirectURI, "read write"), nil)
		page, _ := io.ReadAll(res.Body)
		m := regexp.MustCompile(`name="request" value="([^"]+)"`).FindSubmatch(page)
		if res.StatusCode != 200 || m == nil {
			t.Fatalf("consent page: %d %s", res.StatusCode, page)
		}
		req, _ := http.NewRequest("POST", srv.URL+"/oauth/authorize", strings.NewReader(url.Values{"request": {string(m[1])}, "scope": {scope}, "action": {action}}.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.Header.Set("Origin", srv.URL)
		res, err := c.c.Do(req)
		if err != nil || res.StatusCode != 302 {
			t.Fatalf("answer: %v %d", err, res.StatusCode)
		}
		loc, _ := url.Parse(res.Header.Get("Location"))
		return loc
	}
	token := func(form url.Values) (int, map[string]any) {
		res, err := http.PostForm(srv.URL+"/oauth/token", form)
		if err != nil {
			t.Fatal(err)
		}
		var out map[string]any
		json.NewDecoder(res.Body).Decode(&out)
		return res.StatusCode, out
	}

	// Not logged in: off to the login page, coming back afterwards.
	anon := newClient(t, srv.URL)
	if res := anon.do("GET", authURL(anon, redirect, "read"), nil); res.StatusCode != 302 || !strings.HasPrefix(res.Header.Get("Location"), "/login?next=%2Foauth%2Fauthorize%3F") {
		t.Fatalf("anonymous: %d %s", res.StatusCode, res.Header.Get("Location"))
	}
	// An unregistered redirect is refused without redirecting.
	if res := admin.do("GET", authURL(admin, "https://evil.example/cb", "read"), nil); res.StatusCode != 400 {
		t.Fatalf("bad redirect: %d", res.StatusCode)
	}

	// Deny, then allow with write.
	if loc := consent(admin, redirect, "write", "deny"); loc.Query().Get("error") != "access_denied" || loc.Query().Get("state") != "xyz" {
		t.Fatalf("deny: %s", loc)
	}
	loc := consent(admin, "http://localhost:51000/cb", "write", "allow") // loopback: any port
	code := loc.Query().Get("code")
	if code == "" || loc.Query().Get("iss") != srv.URL || loc.Host != "localhost:51000" {
		t.Fatalf("allow: %s", loc)
	}
	if st, out := token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "code_verifier": {"wrong"}}); st != 400 || out["error"] != "invalid_grant" {
		t.Fatalf("bad verifier: %d %+v", st, out)
	}
	// The code is single use: the failed attempt burnt it.
	code = consent(admin, redirect, "write", "allow").Query().Get("code")
	st, tok := token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "redirect_uri": {redirect}, "code_verifier": {verifier}})
	if st != 200 || tok["scope"] != "write" || !strings.HasPrefix(tok["access_token"].(string), "pma_") {
		t.Fatalf("token: %d %+v", st, tok)
	}
	if st, _ := token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "code_verifier": {verifier}}); st != 400 {
		t.Fatalf("code reused: %d", st)
	}

	// The access token works on /mcp, with write tools, and on the REST API.
	mc := &mcpClient{t: t, url: srv.URL, token: tok["access_token"].(string)}
	if names := mc.toolNames(); !strings.Contains(strings.Join(names, ","), "create_monitor") {
		t.Fatalf("oauth tools: %v", names)
	}
	if n := mc.callOK("list_monitors", nil).(map[string]any)["count"]; n.(float64) != 1 {
		t.Fatalf("oauth list: %v", n)
	}
	if res := bearerDo(t, srv.URL, mc.token, "GET", "/api/monitors", nil); res.StatusCode != 200 {
		t.Fatalf("oauth token on REST API: %d", res.StatusCode)
	}

	// Refresh rotates both tokens.
	st, tok2 := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok["refresh_token"].(string)}, "client_id": {clientID}})
	if st != 200 || tok2["access_token"] == tok["access_token"] {
		t.Fatalf("refresh: %d %+v", st, tok2)
	}
	if res := mc.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`); res.StatusCode != 401 {
		t.Fatalf("old access token: %d", res.StatusCode)
	}
	mc.token = tok2["access_token"].(string)
	mc.rpc("ping", nil)
	// Reusing the old refresh token revokes the grant (it may have leaked).
	if st, _ := token(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok["refresh_token"].(string)}, "client_id": {clientID}}); st != 400 {
		t.Fatalf("old refresh token: %d", st)
	}
	if res := mc.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`); res.StatusCode != 401 {
		t.Fatalf("grant kept after refresh reuse: %d", res.StatusCode)
	}
	code = consent(admin, redirect, "write", "allow").Query().Get("code")
	if st, tok = token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "code_verifier": {verifier}}); st != 200 {
		t.Fatalf("token again: %d %+v", st, tok)
	}
	mc.token = tok["access_token"].(string)
	mc.rpc("ping", nil)

	// Connected apps: listed and revocable from settings.
	grants := decode[[]map[string]any](t, admin.do("GET", "/api/oauth/grants", nil))
	if len(grants) != 1 || grants[0]["client_name"] != "Claude" || grants[0]["scope"] != "write" {
		t.Fatalf("grants: %+v", grants)
	}
	if res := admin.do("DELETE", "/api/oauth/grants/"+itoa(int64(grants[0]["id"].(float64))), nil); res.StatusCode != 200 {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if res := mc.post(`{"jsonrpc":"2.0","id":1,"method":"ping"}`); res.StatusCode != 401 {
		t.Fatalf("revoked token: %d", res.StatusCode)
	}

	// A read-only user only gets read access, whatever the form says.
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	viewer := newClient(t, srv.URL)
	viewer.do("POST", "/api/auth/login", map[string]string{"email": "v@b.co", "password": "password123"})
	code = consent(viewer, redirect, "write", "allow").Query().Get("code")
	if _, tok := token(url.Values{"grant_type": {"authorization_code"}, "code": {code}, "client_id": {clientID}, "code_verifier": {verifier}}); tok["scope"] != "read" {
		t.Fatalf("viewer scope: %+v", tok)
	}
}

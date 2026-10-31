package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

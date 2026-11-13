package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// MCP server (Model Context Protocol) at /mcp, so AI assistants such as
// Claude can read monitors, incidents and maintenance windows and, with a
// write token of an admin, change them.
//
// Transport: Streamable HTTP in its stateless form. Every request is a POST
// carrying one JSON-RPC message (or a batch) and gets a JSON reply; there is
// no session and no server-sent event stream.
//
// Auth: OAuth 2.1 (see mcpauth.go) or a personal API token
// ("Authorization: Bearer ptk_..."). Tools call
// the REST API in-process with the same token, so scopes, roles, validation
// and secret redaction are exactly those of the API.

const (
	mcpPath       = "/mcp"
	mcpServerName = "pingtxt"
	mcpMaxBody    = 1 << 20
)

// Protocol versions we speak, newest first.
var mcpVersions = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

const mcpInstructions = `Pingtxt is an uptime monitor. Start with get_overview, then list_monitors to find monitor ids.
Monitor status is one of up, degraded, down, pending, paused or maintenance. Uptime is a percentage, -1 when there is no data yet.
Times in results are ISO 8601 in UTC. Changes need a write token of an admin: tools that change data are listed only then
(pin_monitor is the exception: pins are personal, so anyone can change their own).
Ask the user before deleting a monitor, a maintenance window, a group or a tag.`

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

const (
	rpcParseError     = -32700
	rpcInvalidRequest = -32600
	rpcMethodNotFound = -32601
	rpcInvalidParams  = -32602
)

// mcpCaller is the identity of an MCP request.
type mcpCaller struct {
	user   *store.User
	token  *store.APIToken
	auth   string // Authorization header, forwarded to the API
	host   string
	remote string
	ip     string // client address for the audit log
	agent  string // User-Agent
}

// canWrite reports whether the caller may use the tools that change data.
func (c *mcpCaller) canWrite() bool {
	return c.user.IsAdmin() && c.token.Scope == ScopeWrite
}

// serveMCP handles the MCP endpoint. api is the API router the tools call.
func (h *Hub) serveMCP(api http.Handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// DNS rebinding protection: a browser page may only reach the
		// endpoint from the hub's own origin.
		if o := r.Header.Get("Origin"); o != "" {
			if u, err := url.Parse(o); err != nil || u.Host != r.Host {
				writeErr(w, http.StatusForbidden, "cross-origin request")
				return
			}
		}
		if r.Method != http.MethodPost {
			// Stateless server: no event stream to open and no session to end.
			w.Header().Set("Allow", http.MethodPost)
			writeErr(w, http.StatusMethodNotAllowed, "use POST")
			return
		}
		if v := r.Header.Get("MCP-Protocol-Version"); v != "" && !slices.Contains(mcpVersions, v) {
			writeErr(w, http.StatusBadRequest, "unsupported MCP protocol version "+v)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeErr(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
			return
		}
		if t := bearer(r); !strings.HasPrefix(t, apiTokenPrefix) && !strings.HasPrefix(t, oauthAccessPrefix) {
			h.mcpUnauthorized(w, r, "sign in with OAuth or send an API token (Authorization: Bearer ptk_...)", t != "")
			return
		}
		u, tok, err := h.authenticate(r)
		if err != nil {
			h.mcpUnauthorized(w, r, err.Error(), true)
			return
		}
		c := &mcpCaller{user: u, token: tok, auth: r.Header.Get("Authorization"), host: r.Host, remote: r.RemoteAddr, ip: requestIP(r), agent: r.UserAgent()}

		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, mcpMaxBody))
		if err != nil {
			writeErr(w, http.StatusRequestEntityTooLarge, "request too large")
			return
		}
		body = bytes.TrimSpace(body)
		ctx := r.Context()

		if len(body) > 0 && body[0] == '[' {
			var batch []json.RawMessage
			if json.Unmarshal(body, &batch) != nil || len(batch) == 0 {
				writeJSON(w, 200, rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcInvalidRequest, "invalid batch"}})
				return
			}
			var out []rpcResponse
			for _, m := range batch {
				if resp := h.mcpMessage(ctx, api, c, m); resp != nil {
					out = append(out, *resp)
				}
			}
			if len(out) == 0 {
				w.WriteHeader(http.StatusAccepted)
				return
			}
			writeJSON(w, 200, out)
			return
		}
		resp := h.mcpMessage(ctx, api, c, body)
		if resp == nil {
			// Notifications and client responses get no reply.
			w.WriteHeader(http.StatusAccepted)
			return
		}
		writeJSON(w, 200, resp)
	}
}

// mcpMessage handles one JSON-RPC message; nil means no reply.
func (h *Hub) mcpMessage(ctx context.Context, api http.Handler, c *mcpCaller, raw json.RawMessage) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcParseError, "invalid JSON"}}
	}
	if req.Method == "" {
		// A response to a server request (we never send any) or garbage.
		if len(req.ID) > 0 {
			return nil
		}
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{rpcInvalidRequest, "missing method"}}
	}
	if len(req.ID) == 0 {
		return nil // notification: initialized, cancelled...
	}
	resp := &rpcResponse{JSONRPC: "2.0", ID: req.ID}
	result, rerr := h.mcpDispatch(ctx, api, c, req.Method, req.Params)
	if rerr != nil {
		resp.Error = rerr
	} else {
		resp.Result = result
	}
	return resp
}

func (h *Hub) mcpDispatch(ctx context.Context, api http.Handler, c *mcpCaller, method string, params json.RawMessage) (any, *rpcError) {
	switch method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(params, &p)
		version := mcpVersions[0]
		if slices.Contains(mcpVersions, p.ProtocolVersion) {
			version = p.ProtocolVersion
		}
		return map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{"listChanged": false}},
			"serverInfo":      map[string]any{"name": mcpServerName, "title": "Pingtxt", "version": h.cfg.Version},
			"instructions":    mcpInstructions,
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		tools := []map[string]any{}
		for _, t := range mcpTools {
			if t.write && !c.canWrite() {
				continue
			}
			tools = append(tools, t.describe())
		}
		return map[string]any{"tools": tools}, nil
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &rpcError{rpcInvalidParams, "invalid params"}
		}
		t := findTool(p.Name)
		if t == nil {
			return nil, &rpcError{rpcInvalidParams, "unknown tool: " + p.Name}
		}
		if p.Arguments == nil {
			p.Arguments = map[string]any{}
		}
		if t.write && !c.canWrite() {
			msg := "this token is read-only"
			if !c.user.IsAdmin() {
				msg = errReadOnlyUser
			}
			h.auditMCPDenied(c, p.Name)
			return toolError(msg), nil
		}
		ctx = withMCPOrigin(ctx, mcpOrigin{tool: p.Name, ip: c.ip, remote: c.remote, userAgent: c.agent})
		out, err := t.run(&mcpCall{ctx: ctx, api: api, caller: c, args: p.Arguments})
		if err != nil {
			return toolError(err.Error()), nil
		}
		b, _ := json.Marshal(out)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}, "isError": false}, nil
	}
	return nil, &rpcError{rpcMethodNotFound, "method not found: " + method}
}

func toolError(msg string) map[string]any {
	return map[string]any{"content": []map[string]any{{"type": "text", "text": msg}}, "isError": true}
}

// ---------- calling the API ----------

type mcpCall struct {
	ctx    context.Context
	api    http.Handler
	caller *mcpCaller
	args   map[string]any
}

// apiRecorder collects the reply of an in-process API call.
type apiRecorder struct {
	header http.Header
	code   int
	body   bytes.Buffer
}

func (r *apiRecorder) Header() http.Header         { return r.header }
func (r *apiRecorder) Write(b []byte) (int, error) { return r.body.Write(b) }
func (r *apiRecorder) WriteHeader(code int) {
	if r.code == 0 {
		r.code = code
	}
}

// do calls the REST API as the MCP caller and decodes the JSON reply, with
// times as ISO 8601.
func (c *mcpCall) do(method, path string, body any) (any, error) {
	out, err := c.raw(method, path, body)
	if err != nil {
		return nil, err
	}
	return humanTimes(out), nil
}

// raw calls the REST API and returns its JSON reply unchanged.
func (c *mcpCall) raw(method, path string, body any) (any, error) {
	var rd io.Reader = http.NoBody
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(c.ctx, method, "http://"+c.caller.host+path, rd)
	if err != nil {
		return nil, err
	}
	req.Host = c.caller.host
	req.RemoteAddr = c.caller.remote
	req.Header.Set("Authorization", c.caller.auth)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := &apiRecorder{header: http.Header{}}
	c.api.ServeHTTP(rec, req)
	if rec.code == 0 {
		rec.code = 200
	}
	var out any
	if err := json.Unmarshal(rec.body.Bytes(), &out); err != nil {
		return nil, fmt.Errorf("unexpected reply from the API (%d)", rec.code)
	}
	if rec.code >= 400 {
		if m, ok := out.(map[string]any); ok {
			if e, ok := m["error"].(string); ok {
				return nil, errors.New(e)
			}
		}
		return nil, fmt.Errorf("API error %d", rec.code)
	}
	return out, nil
}

// Fields holding unix milliseconds, shown to the model as ISO 8601.
var timeFields = map[string]bool{
	"ts": true, "started_at": true, "resolved_at": true, "status_since": true, "created_at": true,
	"start_at": true, "end_at": true, "active_end": true, "next_start": true, "next_end": true,
	"last_seen": true, "expires_at": true, "checked_at": true,
	"last_used_at": true, "from": true, "to": true,
}

// Fields holding unix seconds (TLS certificate expiry of check results and
// diagnostics), shown to the model as ISO 8601 too.
var secondFields = map[string]bool{"cert_expiry": true, "not_after": true}

func humanTimes(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			if n, ok := x.(float64); ok && (timeFields[k] || secondFields[k]) {
				if secondFields[k] {
					n *= 1000
				}
				if n <= 0 {
					t[k] = nil
				} else {
					t[k] = time.UnixMilli(int64(n)).UTC().Format(time.RFC3339)
				}
				continue
			}
			t[k] = humanTimes(x)
		}
	case []any:
		for i, x := range t {
			t[i] = humanTimes(x)
		}
	}
	return v
}

// ---------- arguments ----------

func (c *mcpCall) id(name string) (int64, error) {
	v, ok := c.args[name]
	if !ok {
		return 0, fmt.Errorf("%s is required", name)
	}
	n, ok := toInt(v)
	if !ok || n <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", name)
	}
	return n, nil
}

func (c *mcpCall) optInt(name string) (int64, bool) {
	v, ok := c.args[name]
	if !ok || v == nil {
		return 0, false
	}
	return toInt(v)
}

func (c *mcpCall) str(name string) string {
	s, _ := c.args[name].(string)
	return strings.TrimSpace(s)
}

func (c *mcpCall) boolean(name string) (bool, bool) {
	b, ok := c.args[name].(bool)
	return b, ok
}

func toInt(v any) (int64, bool) {
	switch n := v.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, false
		}
		return int64(n), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(n), 10, 64)
		return i, err == nil
	}
	return 0, false
}

// parseTime accepts ISO 8601 / RFC 3339 or unix milliseconds.
func parseTime(v any) (int64, error) {
	switch t := v.(type) {
	case float64:
		return int64(t), nil
	case string:
		t = strings.TrimSpace(t)
		for _, layout := range []string{time.RFC3339, "2006-01-02T15:04", "2006-01-02 15:04"} {
			if ts, err := time.Parse(layout, t); err == nil {
				return ts.UnixMilli(), nil
			}
		}
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n, nil
		}
	}
	return 0, errors.New("times must be ISO 8601, e.g. 2026-11-01T22:00:00Z")
}

// ---------- tools ----------

type mcpTool struct {
	name        string
	title       string
	description string
	schema      map[string]any
	write       bool // changes data: admins with a write token only
	personal    bool // changes only the caller's own data: anyone
	destructive bool
	idempotent  bool
	run         func(*mcpCall) (any, error)
}

func (t *mcpTool) describe() map[string]any {
	return map[string]any{
		"name":        t.name,
		"title":       t.title,
		"description": t.description,
		"inputSchema": t.schema,
		"annotations": map[string]any{
			"title":           t.title,
			"readOnlyHint":    !t.write && !t.personal,
			"destructiveHint": t.destructive,
			"idempotentHint":  (!t.write && !t.personal) || t.idempotent,
			"openWorldHint":   false,
		},
	}
}

func findTool(name string) *mcpTool {
	for i := range mcpTools {
		if mcpTools[i].name == name {
			return &mcpTools[i]
		}
	}
	return nil
}

func object(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func prop(typ, desc string) map[string]any { return map[string]any{"type": typ, "description": desc} }

func intList(desc string) map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "integer"}, "description": desc}
}

var idProp = map[string]any{"id": prop("integer", "Monitor id, from list_monitors")}

// monitorFields describes the fields of create_monitor and update_monitor.
func monitorFields() map[string]any {
	return map[string]any{
		"name":            prop("string", "Display name"),
		"type":            map[string]any{"type": "string", "enum": validTypes, "description": "Check type"},
		"target":          prop("string", "What to check: URL for http/keyword, host:port for tcp ([2001:db8::1]:443 for IPv6), host or IP for ping/dns, connection URL for databases, container for docker, grpc://host:port, smtp://host, host:port for udp, ws(s):// URL for websocket, the Atlassian Statuspage page (https://status.example.com) for statuspage, host[:port] for ssh and ntp, imap(s)://[user:pass@]host[:port] for imap, pop3(s)://[user:pass@]host[:port] for pop3 (with a user and password the check logs in, over TLS only), an IP address or a mail domain for rbl (a domain checks its MX servers' addresses and the domain itself). Empty for push."),
		"interval_sec":    prop("integer", "Seconds between checks (10 to 86400, default 60). For push monitors: expected heartbeat period."),
		"timeout_ms":      prop("integer", "Timeout in milliseconds (500 to 60000, default 10000)"),
		"retries":         prop("integer", "Failed checks in a row before the monitor is down (1 to 10)"),
		"method":          prop("string", "HTTP method (default GET). udp: text or hex, how body and keyword are written"),
		"expected_status": prop("string", "Accepted HTTP status codes, e.g. 200-299 or 200,301. ntp: largest clock offset in ms (default 1000)"),
		"keyword":         prop("string", "Text the response must contain (keyword type; udp and websocket: the reply; statuspage: comma-separated component ids to follow, empty = whole page; ssh: text the server banner must contain, or a host key fingerprint SHA256:... the server must have; rbl: blacklist zones to check, comma-separated, empty = the default lists)"),
		"keyword_invert":  prop("boolean", "Fail when the keyword is present instead"),
		"headers":         map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}, "description": "HTTP request headers"},
		"body":            prop("string", "HTTP request body. udp: payload to send. websocket: message sent after connecting"),
		"assertions": map[string]any{"type": "array", "description": "http/keyword: rules on the JSON response body, all must pass", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":  prop("string", "JSON path, e.g. checks.redis.status or items[0].id"),
				"op":    map[string]any{"type": "string", "enum": check.AssertionOps},
				"value": prop("string", "Expected value (not for exists/not_exists)"),
			},
			"required": []string{"path", "op"},
		}},
		"steps": map[string]any{"type": "array", "description": "http/keyword: requests made in order before the monitor's own one, e.g. a login. Extracted values are used as {{var}} in the URL, headers and body of later steps and of the monitor's request; cookies carry over. At most 10", "items": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":            prop("string", "Shown in messages"),
				"method":          prop("string", "HTTP method (default GET). udp: text or hex, how body and keyword are written"),
				"url":             prop("string", "http(s) URL, can use {{var}}"),
				"headers":         map[string]any{"type": "object", "additionalProperties": map[string]any{"type": "string"}},
				"body":            prop("string", "Request body, can use {{var}}"),
				"expected_status": prop("string", "Accepted status codes (default 200-399)"),
				"extract": map[string]any{"type": "array", "items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"var":  prop("string", "Variable name"),
						"from": map[string]any{"type": "string", "enum": []string{check.FromJSON, check.FromHeader, check.FromRegex}},
						"path": prop("string", "JSON path, header name or regular expression (first group)"),
					},
					"required": []string{"var", "from", "path"},
				}},
			},
			"required": []string{"url"},
		}},
		"dns_record":          prop("string", "DNS record type, e.g. A, AAAA, MX, TXT"),
		"dns_expected":        prop("string", "Expected DNS answer"),
		"dns_server":          prop("string", "dns and rbl: DNS server to ask (IP or host[:port]), empty = the system resolver. Blacklists refuse queries from big public resolvers"),
		"client_cert":         prop("string", "http, keyword, websocket, grpc, mcp: PEM client certificate (chain allowed) for mutual TLS; set client_key with it, \"\" for none. Agents older than this setting fail the check with update it"),
		"client_key":          prop("string", "PEM private key of client_cert"),
		"ignore_tls":          prop("boolean", "Ignore TLS certificate errors"),
		"ip_family":           map[string]any{"type": "integer", "enum": []int{0, 1, 4, 6, 46}, "description": "0 instance default, 1 IPv4 or IPv6 (first to connect), 4 IPv4 only, 6 IPv6 only, 46 IPv4 and IPv6 (both must work). 6 and 46 are refused on locations without IPv6"},
		"diagnostics":         map[string]any{"type": "integer", "enum": []int{0, 1, 2}, "description": "Diagnostics (DNS, TCP, TLS, response headers, traceroute) at the first failure: 0 instance default, 1 on, 2 off"},
		"grace_sec":           prop("integer", "Push monitors: extra seconds before a missing heartbeat is down"),
		"slow_ms":             prop("integer", "Response time in ms above which a location is slow (median of its last 3 successful checks). Slow locations can make the monitor degraded, never down. 0 = off, -1 = automatic from each location's usual response time (3x, at least +100 ms, after 24h of history). A fixed value must be below timeout_ms"),
		"ping_count":          prop("integer", "Ping monitors: echo requests per check, 1 to 20 (default 3). The timeout must cover them (300 ms apart). Agents older than this setting send 3"),
		"ping_loss_pct":       prop("integer", "Ping monitors: the check fails when at least this share (percent) of packets is lost, 0 = only when all are lost. Agents older than this setting fail the check with update it"),
		"locations":           map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Where to check from: ids from list_agents (\"local\" is the hub itself). Default local."},
		"group_id":            prop("integer", "Group id from list_groups, 0 for none"),
		"tag_ids":             intList("Tag ids from list_tags"),
		"depends_on":          intList("Ids of monitors this one depends on (e.g. the ping of its server). While one of them is down this monitor sends no alerts of its own; [] for none"),
		"cert_expiry_alert":   prop("boolean", "Alert before the TLS certificate expires"),
		"domain_expiry_alert": prop("boolean", "Alert before the domain registration expires"),
		"paused":              prop("boolean", "Create the monitor paused"),
	}
}

// compactMonitor keeps what a model needs from a monitor summary: no
// per-bar history and no probe settings.
func compactMonitor(v any) any {
	m, ok := v.(map[string]any)
	if !ok {
		return v
	}
	out := map[string]any{}
	for _, k := range []string{"id", "name", "type", "target", "status", "status_since", "paused", "group_id", "tag_ids", "depends_on", "uptime_24h", "avg_ms_24h", "maintenance", "blocked_by", "incident", "locations"} {
		if x, ok := m[k]; ok {
			out[k] = x
		}
	}
	if last, ok := m["last"].(map[string]any); ok {
		out["last_check"] = map[string]any{"ts": last["ts"], "ok": last["ok"], "latency_ms": last["latency_ms"], "message": last["message"]}
	}
	if exp, ok := m["expiry"].([]any); ok && len(exp) > 0 {
		out["expiry"] = exp
	}
	return out
}

// pins returns the position (from 1) of each monitor the caller pinned.
func (c *mcpCall) pins() (map[int64]int, error) {
	res, err := c.raw("GET", "/api/account/pins", nil)
	if err != nil {
		return nil, err
	}
	out := map[int64]int{}
	m, _ := res.(map[string]any)
	ids, _ := m["monitor_ids"].([]any)
	for i, x := range ids {
		if f, ok := x.(float64); ok {
			out[int64(f)] = i + 1
		}
	}
	return out, nil
}

func pinID(v any) int64 {
	m, _ := v.(map[string]any)
	f, _ := m["id"].(float64)
	return int64(f)
}

var mcpTools = []mcpTool{
	{
		name: "get_overview", title: "Overview",
		description: "Counts of monitors by status, agents online and open incidents.",
		schema:      object(map[string]any{}),
		run:         func(c *mcpCall) (any, error) { return c.do("GET", "/api/overview", nil) },
	},
	{
		name: "list_monitors", title: "List monitors",
		description: "Monitors with status, 24h uptime, average response time and last check, and whether the user pinned them (pinned monitors first). Filter by status, group, tag, name or pins.",
		schema: object(map[string]any{
			"pinned":   prop("boolean", "true: only the monitors the user pinned; false: only the others"),
			"status":   map[string]any{"type": "string", "enum": []string{StatusUp, StatusDegraded, StatusDown, StatusPending, StatusPaused, StatusMaintenance}, "description": "Only monitors with this status"},
			"group_id": prop("integer", "Only monitors of this group (0 = monitors without a group)"),
			"tag_id":   prop("integer", "Only monitors with this tag"),
			"query":    prop("string", "Only monitors whose name or target contains this text"),
		}),
		run: func(c *mcpCall) (any, error) {
			q := url.Values{}
			if g, ok := c.optInt("group_id"); ok {
				if g == 0 {
					q.Set("group", "none")
				} else {
					q.Set("group", strconv.FormatInt(g, 10))
				}
			}
			if t, ok := c.optInt("tag_id"); ok && t > 0 {
				q.Set("tag", strconv.FormatInt(t, 10))
			}
			path := "/api/monitors"
			if len(q) > 0 {
				path += "?" + q.Encode()
			}
			res, err := c.do("GET", path, nil)
			if err != nil {
				return nil, err
			}
			list, _ := res.([]any)
			pins, err := c.pins()
			if err != nil {
				return nil, err
			}
			pinnedOnly, filterPins := c.boolean("pinned")
			status, query := c.str("status"), strings.ToLower(c.str("query"))
			out := []any{}
			for _, x := range list {
				m, _ := x.(map[string]any)
				id, _ := m["id"].(float64)
				pinned := pins[int64(id)] > 0
				if filterPins && pinned != pinnedOnly {
					continue
				}
				if status != "" && m["status"] != status {
					continue
				}
				if query != "" {
					name, _ := m["name"].(string)
					target, _ := m["target"].(string)
					if !strings.Contains(strings.ToLower(name), query) && !strings.Contains(strings.ToLower(target), query) {
						continue
					}
				}
				cm := compactMonitor(m).(map[string]any)
				cm["pinned"] = pinned
				out = append(out, cm)
			}
			// Pinned monitors first, in pin order; the rest keep the API order.
			slices.SortStableFunc(out, func(a, b any) int {
				pa, pb := pins[pinID(a)], pins[pinID(b)]
				switch {
				case pa > 0 && pb > 0:
					return pa - pb
				case pa > 0:
					return -1
				case pb > 0:
					return 1
				}
				return 0
			})
			return map[string]any{"count": len(out), "monitors": out}, nil
		},
	},
	{
		name: "get_monitor", title: "Get monitor",
		description: "Full settings and state of one monitor: uptime over 24h, 7 and 30 days, status per location, certificate and domain expiry.",
		schema:      object(idProp, "id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			res, err := c.do("GET", fmt.Sprintf("/api/monitors/%d", id), nil)
			if m, ok := res.(map[string]any); ok {
				delete(m, "bars")
			}
			return res, err
		},
	},
	{
		name: "get_monitor_results", title: "Recent check results",
		description: "Latest check results of a monitor, newest first: time, location, ok, latency, status code and message.",
		schema: object(map[string]any{
			"id":    idProp["id"],
			"limit": prop("integer", "Number of results (1 to 500, default 50)"),
		}, "id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			limit, _ := c.optInt("limit")
			return c.do("GET", fmt.Sprintf("/api/monitors/%d/results?limit=%d", id, limit), nil)
		},
	},
	{
		name: "list_incidents", title: "List incidents",
		description: "Downtime incidents, newest first (up to 100). An incident with resolved_at null is still open; acked_by is who acknowledged it (reminders and escalation stopped); snoozed_until in the future means it is snoozed until then.",
		schema: object(map[string]any{
			"monitor_id": prop("integer", "Only incidents of this monitor"),
			"open_only":  prop("boolean", "Only incidents still open"),
		}),
		run: func(c *mcpCall) (any, error) {
			path := "/api/incidents"
			if id, ok := c.optInt("monitor_id"); ok && id > 0 {
				path += "?monitor=" + strconv.FormatInt(id, 10)
			}
			res, err := c.do("GET", path, nil)
			if err != nil {
				return nil, err
			}
			if open, _ := c.boolean("open_only"); open {
				list, _ := res.([]any)
				out := []any{}
				for _, x := range list {
					if m, ok := x.(map[string]any); ok && m["resolved_at"] == nil {
						out = append(out, m)
					}
				}
				return out, nil
			}
			return res, nil
		},
	},
	{
		name: "get_incident_diagnostics", title: "Get incident diagnostics",
		description: "What the failing locations found at the start of an incident: DNS answers, TCP connections, TLS certificate, HTTP status and response headers (secrets redacted, no body) and a traceroute (hops with address, reverse name and RTT; reached=false means the path stops at the last hop that answered), plus findings: the diagnosis in plain words, most telling first. list_incidents marks incidents that have some with diagnostics=true.",
		schema: object(map[string]any{
			"incident_id": prop("integer", "Incident id from list_incidents"),
		}, "incident_id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("incident_id")
			if err != nil {
				return nil, err
			}
			return c.do("GET", fmt.Sprintf("/api/incidents/%d/diagnostics", id), nil)
		},
	},
	{
		name: "list_maintenances", title: "List maintenance windows",
		description: "Maintenance windows with their schedule, whether they are active now and the next occurrence.",
		schema:      object(map[string]any{}),
		run:         func(c *mcpCall) (any, error) { return c.do("GET", "/api/maintenances", nil) },
	},
	{
		name: "list_groups", title: "List groups",
		description: fmt.Sprintf("Monitor groups with the number of monitors in each. A group can sit inside another one (parent_id, 0 = top level), up to %d levels. Give parent_id to list only the subgroups of one group.", store.MaxGroupDepth),
		schema: object(map[string]any{
			"parent_id": prop("integer", "Only the direct subgroups of this group (0 = top-level groups)"),
		}),
		run: func(c *mcpCall) (any, error) {
			out, err := c.do("GET", "/api/groups", nil)
			if err != nil {
				return nil, err
			}
			parent, ok := c.optInt("parent_id")
			if !ok {
				return out, nil
			}
			list, _ := out.([]any)
			sub := []any{}
			for _, g := range list {
				if m, ok := g.(map[string]any); ok {
					if p, _ := toInt(m["parent_id"]); p == parent {
						sub = append(sub, g)
					}
				}
			}
			return sub, nil
		},
	},
	{
		name: "list_tags", title: "List tags",
		description: "Tags with the number of monitors that have each.",
		schema:      object(map[string]any{}),
		run:         func(c *mcpCall) (any, error) { return c.do("GET", "/api/tags", nil) },
	},
	{
		name: "list_agents", title: "List check locations",
		description: "Locations monitors are checked from: the hub itself (local) and remote agents, with online state. Use the ids as monitor locations.",
		schema:      object(map[string]any{}),
		run:         func(c *mcpCall) (any, error) { return c.do("GET", "/api/locations", nil) },
	},
	{
		name: "list_status_pages", title: "List status pages",
		description: "Public status pages with their slug, domain and selected monitors.",
		schema:      object(map[string]any{}),
		run:         func(c *mcpCall) (any, error) { return c.do("GET", "/api/status-pages", nil) },
	},

	{
		name: "list_service_presets", title: "List popular services",
		description: "Catalog of popular services (Claude, OpenAI, GitHub, Cloudflare...) whose public status page can be followed with add_service_presets. Each has an id, category, status page url and monitor_id when a monitor already follows it. Filter with q (name, product or keyword) or category.",
		schema: object(map[string]any{
			"q":        prop("string", "Search: part of the name, a product (chatgpt, copilot) or a keyword"),
			"category": prop("string", "ai, dev, cloud, observability, comms, productivity, commerce or social"),
		}),
		run: func(c *mcpCall) (any, error) {
			v := url.Values{}
			if q := c.str("q"); q != "" {
				v.Set("q", q)
			}
			if cat := c.str("category"); cat != "" {
				v.Set("category", cat)
			}
			path := "/api/monitors/presets"
			if len(v) > 0 {
				path += "?" + v.Encode()
			}
			return c.do("GET", path, nil)
		},
	},

	// ---------- changes ----------
	{
		name: "add_service_presets", title: "Follow popular services", write: true, idempotent: true,
		description: "Add a status page monitor for each service id from list_service_presets, checked from the hub every 2 minutes unless told otherwise. Services that a monitor already follows are skipped. group_name puts them in that group, created when missing. Returns created and skipped.",
		schema: object(map[string]any{
			"ids":          map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Service ids from list_service_presets"},
			"group_id":     prop("integer", "Existing group for the new monitors, from list_groups"),
			"group_name":   prop("string", "Group by name, created when missing (instead of group_id)"),
			"interval_sec": prop("integer", "Seconds between checks, default 120"),
			"tag_ids":      intList("Tag ids from list_tags"),
		}, "ids"),
		run: func(c *mcpCall) (any, error) {
			return c.do("POST", "/api/monitors/presets", c.args)
		},
	},
	{
		name: "create_monitor", title: "Create monitor", write: true,
		description: "Create a monitor. Only name, type and target are required (no target for push). Returns the new monitor.",
		schema:      object(monitorFields(), "name", "type"),
		run: func(c *mcpCall) (any, error) {
			return c.do("POST", "/api/monitors", c.args)
		},
	},
	{
		name: "update_monitor", title: "Update monitor", write: true, idempotent: true,
		description: "Change some fields of a monitor; fields not given keep their value.",
		schema: func() map[string]any {
			p := monitorFields()
			delete(p, "paused")
			p["id"] = idProp["id"]
			return object(p, "id")
		}(),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			path := fmt.Sprintf("/api/monitors/%d", id)
			cur, err := c.raw("GET", path, nil)
			if err != nil {
				return nil, err
			}
			m, ok := cur.(map[string]any)
			if !ok {
				return nil, errors.New("monitor not found")
			}
			for k, v := range c.args {
				if k != "id" && k != "paused" {
					m[k] = v
				}
			}
			return c.do("PUT", path, m)
		},
	},
	{
		name: "pause_monitor", title: "Pause or resume monitor", write: true, idempotent: true,
		description: "Pause a monitor (no checks, no alerts, open incident closed) or resume it.",
		schema: object(map[string]any{
			"id":     idProp["id"],
			"paused": prop("boolean", "true to pause, false to resume"),
		}, "id", "paused"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			paused, ok := c.boolean("paused")
			if !ok {
				return nil, errors.New("paused must be true or false")
			}
			res, err := c.do("POST", fmt.Sprintf("/api/monitors/%d/pause", id), map[string]bool{"paused": paused})
			return compactMonitor(res), err
		},
	},
	{
		name: "acknowledge_incident", title: "Acknowledge incident", write: true, idempotent: true,
		description: "Acknowledge an open incident for the user of the token (someone is on it: its reminders and escalation stop, and channels that ask for it are told), or take the acknowledgement back with acknowledged=false.",
		schema: object(map[string]any{
			"incident_id":  prop("integer", "Incident id from list_incidents (or incident.id of a monitor)"),
			"acknowledged": prop("boolean", "false to take the acknowledgement back (default true)"),
		}, "incident_id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("incident_id")
			if err != nil {
				return nil, err
			}
			method := "POST"
			if ack, ok := c.boolean("acknowledged"); ok && !ack {
				method = "DELETE"
			}
			return c.do(method, fmt.Sprintf("/api/incidents/%d/ack", id), nil)
		},
	},
	{
		name: "snooze_incident", title: "Snooze incident", write: true, idempotent: true,
		description: "Snooze an open incident for some minutes (5 to 1440): no reminders and no escalation until then; afterwards, if the monitor is still down and nobody acknowledged it, every channel that got the alert is reminded. minutes=0 ends the snooze now.",
		schema: object(map[string]any{
			"incident_id": prop("integer", "Incident id from list_incidents (or incident.id of a monitor)"),
			"minutes":     prop("integer", "How long to snooze (5 to 1440), or 0 to end the snooze"),
		}, "incident_id", "minutes"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("incident_id")
			if err != nil {
				return nil, err
			}
			path := fmt.Sprintf("/api/incidents/%d/snooze", id)
			if min, _ := c.optInt("minutes"); min > 0 {
				return c.do("POST", path, map[string]int64{"minutes": min})
			}
			return c.do("DELETE", path, nil)
		},
	},
	{
		name: "get_configuration_code", title: "Get configuration as code",
		description: "The hub's monitors, groups, tags, notification channels, maintenance windows and status pages as YAML (configuration as code): references by name, default values left out, secrets left out. Admins only. Edit it and pass it to plan_configuration_code / apply_configuration_code.",
		schema: object(map[string]any{
			"settings": prop("boolean", "Include the instance settings (default false)"),
		}),
		run: func(c *mcpCall) (any, error) {
			path := "/api/config/code"
			if s, _ := c.boolean("settings"); s {
				path += "?settings=1"
			}
			y, err := c.text(path)
			if err != nil {
				return nil, err
			}
			return map[string]any{"yaml": y}, nil
		},
	},
	{
		name: "plan_configuration_code", title: "Plan configuration as code",
		description: "What applying a configuration as code YAML would create, update and delete, and the errors that would stop it. Changes nothing. Only the sections in the YAML are compared; with prune=true the objects of those sections that it does not list would be deleted.",
		schema: object(map[string]any{
			"yaml":  prop("string", "The configuration as code (starts with version: 1)"),
			"prune": prop("boolean", "Also delete what the YAML does not list in its sections (default false)"),
		}, "yaml"),
		run: func(c *mcpCall) (any, error) { return c.codeCall("/api/config/code/plan") },
	},
	{
		name: "apply_configuration_code", title: "Apply configuration as code", write: true, destructive: true,
		description: "Make the hub match a configuration as code YAML: objects matched by name are updated, missing ones created, and with prune=true the ones of the YAML's sections that it does not list are deleted. Nothing changes when the plan has errors. Run plan_configuration_code first and show the plan to the user.",
		schema: object(map[string]any{
			"yaml":  prop("string", "The configuration as code (starts with version: 1)"),
			"prune": prop("boolean", "Also delete what the YAML does not list in its sections (default false)"),
		}, "yaml"),
		run: func(c *mcpCall) (any, error) {
			plan, err := c.codeCall("/api/config/code/plan")
			if err != nil {
				return nil, err
			}
			if m, ok := plan.(map[string]any); ok {
				if errs, _ := m["errors"].([]any); len(errs) > 0 {
					return map[string]any{"applied": false, "plan": plan}, nil
				}
			}
			return c.codeCall("/api/config/code/apply")
		},
	},
	{
		name: "pin_monitor", title: "Pin monitor", personal: true, idempotent: true,
		description: "Pin a monitor to the top of the user's monitor list (web UI and app), or unpin it with pinned=false. Pins are personal: this works with any token, also read-only ones, and changes nothing for other users.",
		schema: object(map[string]any{
			"id":     prop("integer", "Monitor id, from list_monitors"),
			"pinned": prop("boolean", "false to unpin (default true)"),
		}, "id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			method := "POST"
			if p, ok := c.boolean("pinned"); ok && !p {
				method = "DELETE"
			}
			return c.do(method, fmt.Sprintf("/api/account/pins/%d", id), nil)
		},
	},
	{
		name: "delete_monitor", title: "Delete monitor", write: true, destructive: true, idempotent: true,
		description: "Delete a monitor with all its history. Cannot be undone: confirm with the user first.",
		schema:      object(idProp, "id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			return c.do("DELETE", fmt.Sprintf("/api/monitors/%d", id), nil)
		},
	},
	{
		name: "create_group", title: "Create group", write: true,
		description: "Create a monitor group, or a subgroup when parent_id is given. Put monitors in it with update_monitor (group_id). Returns the new group.",
		schema:      object(groupFields(), "name"),
		run: func(c *mcpCall) (any, error) {
			return c.do("POST", "/api/groups", c.args)
		},
	},
	{
		name: "update_group", title: "Update or move group", write: true, idempotent: true,
		description: "Rename a group, change its description or move it under another parent (parent_id, 0 = top level); its subgroups and monitors move with it. Fields not given keep their value.",
		schema: func() map[string]any {
			p := groupFields()
			p["id"] = prop("integer", "Group id, from list_groups")
			return object(p, "id")
		}(),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			return c.do("PUT", fmt.Sprintf("/api/groups/%d", id), withoutID(c.args))
		},
	},
	{
		name: "delete_group", title: "Delete group", write: true, destructive: true, idempotent: true,
		description: "Delete a group. Its monitors stay but no longer have a group, its subgroups move up to its parent, and maintenance windows stop covering it. Confirm with the user first.",
		schema:      object(map[string]any{"id": prop("integer", "Group id, from list_groups")}, "id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			return c.do("DELETE", fmt.Sprintf("/api/groups/%d", id), nil)
		},
	},
	{
		name: "create_tag", title: "Create tag", write: true,
		description: "Create a tag. Add it to monitors with update_monitor (tag_ids). Returns the new tag.",
		schema:      object(tagFields(), "name"),
		run: func(c *mcpCall) (any, error) {
			return c.do("POST", "/api/tags", c.args)
		},
	},
	{
		name: "update_tag", title: "Update tag", write: true, idempotent: true,
		description: "Rename a tag or change its color. Fields not given keep their value.",
		schema: func() map[string]any {
			p := tagFields()
			p["id"] = prop("integer", "Tag id, from list_tags")
			return object(p, "id")
		}(),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			return c.do("PUT", fmt.Sprintf("/api/tags/%d", id), withoutID(c.args))
		},
	},
	{
		name: "delete_tag", title: "Delete tag", write: true, destructive: true, idempotent: true,
		description: "Delete a tag and remove it from every monitor. Confirm with the user first.",
		schema:      object(map[string]any{"id": prop("integer", "Tag id, from list_tags")}, "id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			return c.do("DELETE", fmt.Sprintf("/api/tags/%d", id), nil)
		},
	},
	{
		name: "create_maintenance", title: "Create maintenance window", write: true,
		description: "Schedule a maintenance window: covered monitors show 'maintenance' and send no alerts. " +
			"For a one-off window give start (default now) and end or duration_min. " +
			"Recurring windows (daily, weekly, monthly) need start_time and duration_min, plus weekdays or month_days.",
		schema: object(map[string]any{
			"name":               prop("string", "Name shown on the dashboard, and on status pages when public"),
			"description":        prop("string", "Optional internal notes (not shown on status pages)"),
			"kind":               map[string]any{"type": "string", "enum": maintKinds, "description": "once (default), daily, weekly or monthly"},
			"start":              prop("string", "Start, ISO 8601 (e.g. 2026-11-01T22:00:00Z). once: default now. Recurring: first day the window applies."),
			"end":                prop("string", "End, ISO 8601. once: required unless duration_min is given. Recurring: last day (optional)."),
			"start_time":         prop("string", "Recurring: local start time HH:MM"),
			"duration_min":       prop("integer", "Length in minutes"),
			"weekdays":           intList("Weekly: days, 0 = Sunday … 6 = Saturday"),
			"month_days":         intList("Monthly: days of the month, 1 … 31"),
			"timezone":           prop("string", "IANA time zone of start_time, e.g. Europe/Rome (default UTC)"),
			"all_monitors":       prop("boolean", "Cover every monitor"),
			"monitor_ids":        intList("Monitors to cover"),
			"group_ids":          intList("Cover every monitor of these groups"),
			"tag_ids":            intList("Cover every monitor with one of these tags"),
			"public":             prop("boolean", "Announce the window on the status pages that show one of its monitors"),
			"announcement":       prop("string", "Public message shown with the window on status pages and in subscriber emails"),
			"notify_subscribers": prop("boolean", "Email the subscribers of those status pages when it is announced, starts, ends or is cancelled (needs public). Ask the user first."),
			"remind_min":         prop("integer", "With notify_subscribers, also email a reminder this many minutes before each occurrence"),
		}, "name"),
		run: func(c *mcpCall) (any, error) {
			body, err := maintenanceBody(c.args, time.Now())
			if err != nil {
				return nil, err
			}
			return c.do("POST", "/api/maintenances", body)
		},
	},
	{
		name: "delete_maintenance", title: "Delete maintenance window", write: true, destructive: true, idempotent: true,
		description: "Delete a maintenance window, ending it now if active. Confirm with the user first.",
		schema:      object(map[string]any{"id": prop("integer", "Maintenance id, from list_maintenances")}, "id"),
		run: func(c *mcpCall) (any, error) {
			id, err := c.id("id")
			if err != nil {
				return nil, err
			}
			return c.do("DELETE", fmt.Sprintf("/api/maintenances/%d", id), nil)
		},
	},
}

// groupFields describes the fields of create_group and update_group.
func groupFields() map[string]any {
	return map[string]any{
		"name":        prop("string", "Group name (up to 64 characters, unique)"),
		"description": prop("string", "Optional description"),
		"parent_id":   prop("integer", fmt.Sprintf("Parent group id from list_groups to make this a subgroup, 0 = top level. Groups nest at most %d levels.", store.MaxGroupDepth)),
	}
}

// tagFields describes the fields of create_tag and update_tag.
func tagFields() map[string]any {
	return map[string]any{
		"name":  prop("string", "Tag name (up to 32 characters, no commas, unique)"),
		"color": map[string]any{"type": "string", "enum": tagColors, "description": "Color, empty for the default"},
	}
}

// withoutID copies tool arguments without the id of the record they change.
func withoutID(args map[string]any) map[string]any {
	out := make(map[string]any, len(args))
	for k, v := range args {
		if k != "id" {
			out[k] = v
		}
	}
	return out
}

// maintenanceBody turns tool arguments into the API body of a maintenance window.
func maintenanceBody(args map[string]any, now time.Time) (map[string]any, error) {
	body := map[string]any{"enabled": true, "kind": store.MaintOnce}
	for _, k := range []string{"name", "description", "kind", "start_time", "duration_min", "weekdays", "month_days", "timezone", "all_monitors", "monitor_ids", "group_ids", "tag_ids", "public", "announcement", "notify_subscribers", "remind_min"} {
		if v, ok := args[k]; ok && v != nil {
			body[k] = v
		}
	}
	var start, end int64
	var err error
	if v, ok := args["start"]; ok && v != nil && v != "" {
		if start, err = parseTime(v); err != nil {
			return nil, err
		}
	}
	if v, ok := args["end"]; ok && v != nil && v != "" {
		if end, err = parseTime(v); err != nil {
			return nil, err
		}
	}
	if body["kind"] == store.MaintOnce {
		if start == 0 {
			start = now.UnixMilli()
		}
		if end == 0 {
			d, _ := toInt(args["duration_min"])
			if d <= 0 {
				return nil, errors.New("give end or duration_min")
			}
			end = start + d*time.Minute.Milliseconds()
		}
	}
	body["start_at"], body["end_at"] = start, end
	return body, nil
}

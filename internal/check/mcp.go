package check

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// MCP checks connect to a Model Context Protocol server, run the
// initialize handshake and, when the server has tools or the monitor asks
// for them, list its tools.
//
// Job fields used by this type:
//
//	Target          http(s) URL of the MCP endpoint
//	Method          transport: "auto" (default), "streamable" or "sse"
//	Headers         sent with every request, e.g. Authorization: Bearer ...
//	Keyword         comma-separated tool names that must exist
//	ExpectedStatus  minimum number of tools (empty = no minimum)
//	IgnoreTLS       skip certificate verification

// MCP transports.
const (
	MCPAuto       = "auto"
	MCPStreamable = "streamable"
	MCPSSE        = "sse"
)

// mcpProtocolVersion is the version proposed in initialize. Servers that
// do not support it answer with one they do, which is accepted as is.
const mcpProtocolVersion = "2025-11-25"

// Size limits for what the server sends back.
const (
	mcpMaxBody     = 8 << 20
	mcpMaxToolPage = 50
)

// MCPToolNames splits the Keyword field into tool names.
func MCPToolNames(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func runMCP(ctx context.Context, j Job, timeout time.Duration) (r Result) {
	u, err := url.Parse(j.Target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Result{Message: "target must be an http(s) URL"}
	}
	minTools := 0
	if s := strings.TrimSpace(j.ExpectedStatus); s != "" {
		if minTools, err = strconv.Atoi(s); err != nil || minTools < 0 {
			return Result{Message: "minimum tools must be a number"}
		}
	}
	tr := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		TLSClientConfig:     &tls.Config{InsecureSkipVerify: j.IgnoreTLS}, //nolint:gosec // user opt-in
		TLSHandshakeTimeout: timeout,
	}
	defer tr.CloseIdleConnections()
	c := &mcpConn{hc: &http.Client{Transport: tr}, j: j, target: u}

	start := time.Now()
	defer func() { r.StatusCode, r.CertExpiry = c.status, c.certExpiry }()

	init, err := c.connect(ctx)
	if err != nil {
		r.LatencyMs = ms(time.Since(start))
		r.Message = mcpErr(err)
		return r
	}
	defer c.close()

	var info struct {
		ProtocolVersion string `json:"protocolVersion"`
		ServerInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"serverInfo"`
		Capabilities struct {
			Tools json.RawMessage `json:"tools"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(init, &info); err != nil || info.ProtocolVersion == "" {
		r.LatencyMs = ms(time.Since(start))
		r.Message = "invalid initialize result"
		return r
	}
	c.version = info.ProtocolVersion
	if err := c.notify(ctx, "notifications/initialized"); err != nil {
		r.LatencyMs = ms(time.Since(start))
		r.Message = "notifications/initialized: " + mcpErr(err)
		return r
	}

	desc := strings.TrimSpace(info.ServerInfo.Name + " " + info.ServerInfo.Version)
	if desc == "" {
		desc = "mcp server"
	}
	desc += " · mcp " + info.ProtocolVersion + " · " + c.transport

	required := MCPToolNames(j.Keyword)
	hasTools := len(info.Capabilities.Tools) > 0 && string(info.Capabilities.Tools) != "null"
	if !hasTools && len(required) == 0 && minTools == 0 {
		r.LatencyMs = ms(time.Since(start))
		r.OK = true
		r.Message = desc
		return r
	}

	tools, err := c.listTools(ctx)
	r.LatencyMs = ms(time.Since(start))
	if err != nil {
		r.Message = "tools/list: " + mcpErr(err)
		return r
	}
	desc += fmt.Sprintf(" · %d tools", len(tools))
	var missing []string
	for _, name := range required {
		if !tools[name] {
			missing = append(missing, name)
		}
	}
	switch {
	case len(missing) == 1:
		r.Message = fmt.Sprintf("tool %q not found · %s", missing[0], desc)
	case len(missing) > 1:
		r.Message = fmt.Sprintf("tools %s not found · %s", strings.Join(missing, ", "), desc)
	case len(tools) < minTools:
		r.Message = fmt.Sprintf("%d tools, expected at least %d · %s", len(tools), minTools, desc)
	default:
		r.OK = true
		r.Message = desc
	}
	return r
}

// listTools follows the tools/list pagination and returns the tool names.
func (c *mcpConn) listTools(ctx context.Context) (map[string]bool, error) {
	tools := map[string]bool{}
	cursor := ""
	for page := 0; page < mcpMaxToolPage; page++ {
		var params any
		if cursor != "" {
			params = map[string]string{"cursor": cursor}
		}
		res, err := c.call(ctx, "tools/list", params)
		if err != nil {
			return nil, err
		}
		var list struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(res, &list); err != nil {
			return nil, errors.New("invalid result")
		}
		for _, t := range list.Tools {
			tools[t.Name] = true
		}
		if list.NextCursor == "" || list.NextCursor == cursor {
			return tools, nil
		}
		cursor = list.NextCursor
	}
	return tools, nil
}

// ---------- JSON-RPC ----------

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      *int   `json:"id,omitempty"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcMessage struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type rpcError struct {
	code int
	msg  string
}

func (e *rpcError) Error() string { return fmt.Sprintf("error %d: %s", e.code, e.msg) }

type httpStatusError struct {
	code int
	body string
}

func (e *httpStatusError) Error() string {
	msg := fmt.Sprintf("http status %d", e.code)
	switch e.code {
	case http.StatusUnauthorized, http.StatusForbidden:
		msg += " (check the auth header)"
	}
	if e.body != "" {
		msg += ": " + e.body
	}
	return msg
}

func newHTTPStatusError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 200))
	body := strings.Join(strings.Fields(string(b)), " ")
	if strings.HasPrefix(body, "<") { // an HTML error page says nothing useful
		body = ""
	}
	return &httpStatusError{code: resp.StatusCode, body: body}
}

// idMatches compares a response id with the numeric id of the request
// (some servers echo it as a string).
func idMatches(raw json.RawMessage, id int) bool {
	s := strings.Trim(strings.TrimSpace(string(raw)), `"`)
	return s == strconv.Itoa(id)
}

// response returns the result of the message answering id, or ok=false when
// the message is something else (a notification or a server request).
func (m *rpcMessage) response(id int) (json.RawMessage, bool, error) {
	if m.Method != "" || !idMatches(m.ID, id) {
		return nil, false, nil
	}
	if m.Error != nil {
		return nil, true, &rpcError{code: m.Error.Code, msg: m.Error.Message}
	}
	if len(m.Result) == 0 {
		return nil, true, errors.New("empty result")
	}
	return m.Result, true, nil
}

// findResponse looks for the answer to id in a JSON body: one message or a batch.
func findResponse(body []byte, id int) (json.RawMessage, bool, error) {
	body = bytes.TrimSpace(body)
	if len(body) > 0 && body[0] == '[' {
		var batch []rpcMessage
		if err := json.Unmarshal(body, &batch); err != nil {
			return nil, false, errors.New("invalid json-rpc response")
		}
		for i := range batch {
			if res, ok, err := batch[i].response(id); ok {
				return res, true, err
			}
		}
		return nil, false, nil
	}
	var m rpcMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, false, errors.New("invalid json-rpc response")
	}
	return m.response(id)
}

// ---------- server-sent events ----------

type sseEvent struct {
	event string
	data  string
}

type sseReader struct{ r *bufio.Reader }

func newSSEReader(r io.Reader) *sseReader {
	return &sseReader{r: bufio.NewReaderSize(io.LimitReader(r, mcpMaxBody), 64<<10)}
}

// next returns the next event with data, skipping comments and empty events.
func (s *sseReader) next() (sseEvent, error) {
	var ev sseEvent
	var data []string
	seen := false
	for {
		line, err := s.r.ReadString('\n')
		if err != nil && line == "" {
			return ev, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if seen {
				ev.data = strings.Join(data, "\n")
				return ev, nil
			}
			ev = sseEvent{}
			continue
		}
		if line[0] == ':' {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "event":
			ev.event = value
		case "data":
			data = append(data, value)
			seen = true
		}
	}
}

// ---------- connection ----------

// mcpConn talks to one server over streamable HTTP (one POST per message,
// answers as JSON or as an SSE stream) or over the legacy HTTP+SSE
// transport (a GET event stream carries the answers to POSTed messages).
type mcpConn struct {
	hc     *http.Client
	j      Job
	target *url.URL

	transport string // MCPStreamable or MCPSSE once connected
	session   string // Mcp-Session-Id (streamable)
	version   string // negotiated protocol version
	nextID    int

	// legacy SSE
	endpoint string
	events   chan sseEvent
	stop     context.CancelFunc

	// first HTTP response, for the result
	status     int
	certExpiry int64
}

func (c *mcpConn) connect(ctx context.Context) (json.RawMessage, error) {
	params := map[string]any{
		"protocolVersion": mcpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]string{"name": "pingtxt", "version": "1.0"},
	}
	mode := strings.ToLower(strings.TrimSpace(c.j.Method))
	if mode != MCPSSE {
		c.transport = MCPStreamable
		res, err := c.call(ctx, "initialize", params)
		var he *httpStatusError
		if err == nil || mode == MCPStreamable || !errors.As(err, &he) || !legacyFallback(he.code) {
			return res, err
		}
		// Servers on the 2024-11-05 transport reject the POST: try a GET
		// event stream, as the spec suggests for backwards compatibility.
		c.status, c.certExpiry = 0, 0
		res, err2 := c.connectSSE(ctx, params)
		if err2 != nil {
			return nil, fmt.Errorf("%v (legacy sse: %v)", err, mcpErr(err2))
		}
		return res, nil
	}
	return c.connectSSE(ctx, params)
}

func legacyFallback(code int) bool {
	switch code {
	case http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed,
		http.StatusNotAcceptable, http.StatusUnsupportedMediaType:
		return true
	}
	return false
}

func (c *mcpConn) connectSSE(ctx context.Context, params any) (json.RawMessage, error) {
	c.transport = MCPSSE
	sctx, stop := context.WithCancel(ctx)
	req, err := http.NewRequestWithContext(sctx, http.MethodGet, c.target.String(), nil)
	if err != nil {
		stop()
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Accept", "text/event-stream")
	resp, err := c.hc.Do(req)
	if err != nil {
		stop()
		return nil, err
	}
	c.record(resp)
	if resp.StatusCode != http.StatusOK {
		err := newHTTPStatusError(resp)
		resp.Body.Close()
		stop()
		return nil, err
	}
	if !strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		resp.Body.Close()
		stop()
		return nil, errors.New("not an event stream")
	}
	c.stop = func() { stop(); resp.Body.Close() }
	c.events = make(chan sseEvent, 16)
	go func() {
		defer close(c.events)
		rd := newSSEReader(resp.Body)
		for {
			ev, err := rd.next()
			if err != nil {
				return
			}
			select {
			case c.events <- ev:
			case <-sctx.Done():
				return
			}
		}
	}()

	for c.endpoint == "" {
		select {
		case ev, ok := <-c.events:
			if !ok {
				c.close()
				return nil, errors.New("stream closed before the endpoint event")
			}
			if ev.event != "endpoint" {
				continue
			}
			ep, err := c.target.Parse(strings.TrimSpace(ev.data))
			if err != nil || ep.Scheme != c.target.Scheme || ep.Host != c.target.Host {
				c.close()
				return nil, errors.New("invalid endpoint event")
			}
			c.endpoint = ep.String()
		case <-ctx.Done():
			c.close()
			return nil, errors.New("timeout waiting for the endpoint event (not a legacy sse server?)")
		}
	}
	return c.call(ctx, "initialize", params)
}

func (c *mcpConn) setHeaders(req *http.Request) {
	req.Header.Set("User-Agent", "pingtxt/1.0 (+https://github.com/federico-it/pingtxt)")
	for k, v := range c.j.Headers {
		req.Header.Set(k, v)
	}
}

func (c *mcpConn) record(resp *http.Response) {
	if c.status != 0 {
		return
	}
	c.status = resp.StatusCode
	if resp.TLS != nil && len(resp.TLS.PeerCertificates) > 0 {
		c.certExpiry = resp.TLS.PeerCertificates[0].NotAfter.Unix()
	}
}

// post sends one JSON-RPC message.
func (c *mcpConn) post(ctx context.Context, msg rpcRequest) (*http.Response, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}
	u := c.target.String()
	if c.transport == MCPSSE {
		u = c.endpoint
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	c.setHeaders(req)
	req.Header.Set("Content-Type", "application/json")
	if c.transport == MCPStreamable {
		req.Header.Set("Accept", "application/json, text/event-stream")
		if c.session != "" {
			req.Header.Set("Mcp-Session-Id", c.session)
		}
		if c.version != "" {
			req.Header.Set("MCP-Protocol-Version", c.version)
		}
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, err
	}
	c.record(resp)
	if resp.StatusCode/100 != 2 {
		err := newHTTPStatusError(resp)
		resp.Body.Close()
		return nil, err
	}
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" && c.transport == MCPStreamable {
		c.session = sid
	}
	return resp, nil
}

// call sends a request and waits for its response.
func (c *mcpConn) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.nextID++
	id := c.nextID
	resp, err := c.post(ctx, rpcRequest{JSONRPC: "2.0", ID: &id, Method: method, Params: params})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if c.transport == MCPSSE {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return c.waitSSE(ctx, id)
	}
	ct := strings.ToLower(resp.Header.Get("Content-Type"))
	switch {
	case strings.HasPrefix(ct, "text/event-stream"):
		rd := newSSEReader(resp.Body)
		for {
			ev, err := rd.next()
			if err != nil {
				if ctx.Err() != nil {
					return nil, ctx.Err()
				}
				return nil, errors.New("stream closed without a response")
			}
			if ev.event != "" && ev.event != "message" {
				continue
			}
			if res, ok, err := findResponse([]byte(ev.data), id); ok || err != nil {
				return res, err
			}
		}
	case strings.HasPrefix(ct, "application/json"):
		body, err := io.ReadAll(io.LimitReader(resp.Body, mcpMaxBody))
		if err != nil {
			return nil, err
		}
		res, ok, err := findResponse(body, id)
		if !ok && err == nil {
			err = errors.New("no response in the body")
		}
		return res, err
	default:
		return nil, fmt.Errorf("unexpected content type %q", resp.Header.Get("Content-Type"))
	}
}

// waitSSE reads the legacy event stream until the response to id arrives.
func (c *mcpConn) waitSSE(ctx context.Context, id int) (json.RawMessage, error) {
	for {
		select {
		case ev, ok := <-c.events:
			if !ok {
				return nil, errors.New("stream closed without a response")
			}
			if ev.event != "" && ev.event != "message" {
				continue
			}
			if res, ok, err := findResponse([]byte(ev.data), id); ok || err != nil {
				return res, err
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
}

// notify sends a notification; the answer has no body to read.
func (c *mcpConn) notify(ctx context.Context, method string) error {
	resp, err := c.post(ctx, rpcRequest{JSONRPC: "2.0", Method: method})
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	return resp.Body.Close()
}

// close ends the session: the legacy stream is closed, a streamable
// session is deleted (best effort, servers may answer 405).
func (c *mcpConn) close() {
	if c.stop != nil {
		c.stop()
		c.stop = nil
	}
	if c.transport != MCPStreamable || c.session == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.target.String(), nil)
	if err != nil {
		return
	}
	c.setHeaders(req)
	req.Header.Set("Mcp-Session-Id", c.session)
	if c.version != "" {
		req.Header.Set("MCP-Protocol-Version", c.version)
	}
	if resp, err := c.hc.Do(req); err == nil {
		resp.Body.Close()
	}
	c.session = ""
}

func mcpErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}
	return cleanErr(err)
}

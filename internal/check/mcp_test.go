package check

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

type fakeReq struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Cursor string `json:"cursor"`
	} `json:"params"`
}

// fakeMCP answers MCP requests like a small server with tools spread over
// two tools/list pages.
type fakeMCP struct {
	tools   []string
	noTools bool // no tools capability
	token   string

	mu       sync.Mutex
	methods  []string
	deleted  bool
	sessions map[string]bool
}

func (f *fakeMCP) answer(r fakeReq) any {
	switch r.Method {
	case "initialize":
		caps := map[string]any{"tools": map[string]any{}}
		if f.noTools {
			caps = map[string]any{}
		}
		return map[string]any{"jsonrpc": "2.0", "id": r.ID, "result": map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    caps,
			"serverInfo":      map[string]string{"name": "fake-mcp", "version": "1.2.3"},
		}}
	case "tools/list":
		half := len(f.tools) / 2
		page, next := f.tools[:half], "p2"
		if r.Params.Cursor == "p2" {
			page, next = f.tools[half:], ""
		}
		var list []map[string]any
		for _, t := range page {
			list = append(list, map[string]any{"name": t, "inputSchema": map[string]any{"type": "object"}})
		}
		res := map[string]any{"tools": list}
		if next != "" {
			res["nextCursor"] = next
		}
		return map[string]any{"jsonrpc": "2.0", "id": r.ID, "result": res}
	}
	return map[string]any{"jsonrpc": "2.0", "id": r.ID, "error": map[string]any{"code": -32601, "message": "method not found"}}
}

func (f *fakeMCP) authorized(w http.ResponseWriter, r *http.Request) bool {
	if f.token != "" && r.Header.Get("Authorization") != "Bearer "+f.token {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return false
	}
	return true
}

// streamable serves the streamable HTTP transport; sse=true answers
// requests with an event stream instead of a JSON body.
func (f *fakeMCP) streamable(sse bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.sessions == nil {
			f.sessions = map[string]bool{}
		}
		switch r.Method {
		case http.MethodDelete:
			f.deleted = f.sessions[r.Header.Get("Mcp-Session-Id")]
			return
		case http.MethodPost:
		default:
			http.Error(w, "", http.StatusMethodNotAllowed)
			return
		}
		if !strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			http.Error(w, "bad accept", http.StatusNotAcceptable)
			return
		}
		var req fakeReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad json", http.StatusBadRequest)
			return
		}
		f.methods = append(f.methods, req.Method)
		if req.Method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "s1")
			f.sessions["s1"] = true
		} else if !f.sessions[r.Header.Get("Mcp-Session-Id")] {
			http.Error(w, "no session", http.StatusBadRequest)
			return
		} else if r.Header.Get("MCP-Protocol-Version") != "2025-06-18" {
			http.Error(w, "bad version", http.StatusBadRequest)
			return
		}
		if req.ID == nil {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		b, _ := json.Marshal(f.answer(req))
		if sse {
			w.Header().Set("Content-Type", "text/event-stream")
			// A notification before the response must be skipped.
			fmt.Fprint(w, ": keepalive\n\nevent: message\ndata: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\",\"params\":{}}\n\n")
			fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}
}

// legacy serves the 2024-11-05 HTTP+SSE transport.
func (f *fakeMCP) legacy() http.Handler {
	var mu sync.Mutex
	streams := map[string]chan []byte{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /sse", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		ch := make(chan []byte, 8)
		mu.Lock()
		streams["abc"] = ch
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: endpoint\ndata: /messages?session=abc\n\n")
		w.(http.Flusher).Flush()
		for {
			select {
			case b := <-ch:
				fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
				w.(http.Flusher).Flush()
			case <-r.Context().Done():
				return
			}
		}
	})
	mux.HandleFunc("POST /messages", func(w http.ResponseWriter, r *http.Request) {
		if !f.authorized(w, r) {
			return
		}
		mu.Lock()
		ch := streams[r.URL.Query().Get("session")]
		mu.Unlock()
		var req fakeReq
		if ch == nil || json.NewDecoder(r.Body).Decode(&req) != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.methods = append(f.methods, req.Method)
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		if req.ID != nil {
			b, _ := json.Marshal(f.answer(req))
			ch <- b
		}
	})
	// The streamable POST of an "auto" monitor lands here.
	mux.HandleFunc("POST /sse", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "", http.StatusMethodNotAllowed)
	})
	return mux
}

func TestMCPStreamable(t *testing.T) {
	for _, sse := range []bool{false, true} {
		f := &fakeMCP{tools: []string{"search", "fetch", "create_issue"}, token: "tok"}
		srv := httptest.NewServer(f.streamable(sse))
		j := Job{Type: TypeMCP, Target: srv.URL + "/mcp", TimeoutMs: 3000,
			Headers: map[string]string{"Authorization": "Bearer tok"}, Keyword: "search, create_issue", ExpectedStatus: "3"}
		r := Run(context.Background(), j)
		srv.Close()
		if !r.OK {
			t.Fatalf("sse=%v: %+v", sse, r)
		}
		want := "fake-mcp 1.2.3 · mcp 2025-06-18 · streamable · 3 tools"
		if r.Message != want || r.StatusCode != 200 {
			t.Errorf("sse=%v: message %q status %d", sse, r.Message, r.StatusCode)
		}
		got := strings.Join(f.methods, ",")
		if got != "initialize,notifications/initialized,tools/list,tools/list" {
			t.Errorf("sse=%v: methods %s", sse, got)
		}
		if !f.deleted {
			t.Errorf("sse=%v: session not deleted", sse)
		}
	}
}

func TestMCPAssertions(t *testing.T) {
	f := &fakeMCP{tools: []string{"a", "b"}}
	srv := httptest.NewServer(f.streamable(false))
	defer srv.Close()
	cases := []struct {
		keyword, min string
		ok           bool
		msg          string
	}{
		{"", "", true, "2 tools"},
		{"a", "2", true, "2 tools"},
		{"c", "", false, `tool "c" not found`},
		{"a, c, d", "", false, "tools c, d not found"},
		{"", "3", false, "2 tools, expected at least 3"},
	}
	for _, c := range cases {
		r := Run(context.Background(), Job{Type: TypeMCP, Target: srv.URL, Keyword: c.keyword, ExpectedStatus: c.min})
		if r.OK != c.ok || !strings.Contains(r.Message, c.msg) {
			t.Errorf("%q/%q: ok=%v %q", c.keyword, c.min, r.OK, r.Message)
		}
	}
}

func TestMCPNoTools(t *testing.T) {
	f := &fakeMCP{noTools: true}
	srv := httptest.NewServer(f.streamable(false))
	defer srv.Close()
	r := Run(context.Background(), Job{Type: TypeMCP, Target: srv.URL})
	if !r.OK || strings.Contains(r.Message, "tools") {
		t.Fatalf("%+v", r)
	}
	if strings.Contains(strings.Join(f.methods, ","), "tools/list") {
		t.Error("tools/list called without the tools capability")
	}
}

func TestMCPUnauthorized(t *testing.T) {
	f := &fakeMCP{token: "secret"}
	srv := httptest.NewServer(f.streamable(false))
	defer srv.Close()
	r := Run(context.Background(), Job{Type: TypeMCP, Target: srv.URL, Headers: map[string]string{"Authorization": "Bearer wrong"}})
	if r.OK || r.StatusCode != 401 || !strings.Contains(r.Message, "http status 401") {
		t.Fatalf("%+v", r)
	}
}

func TestMCPLegacySSE(t *testing.T) {
	for _, mode := range []string{MCPSSE, MCPAuto} {
		f := &fakeMCP{tools: []string{"x", "y", "z", "w"}, token: "t"}
		srv := httptest.NewServer(f.legacy())
		j := Job{Type: TypeMCP, Target: srv.URL + "/sse", Method: mode, TimeoutMs: 3000,
			Headers: map[string]string{"Authorization": "Bearer t"}, Keyword: "z"}
		r := Run(context.Background(), j)
		srv.Close()
		if !r.OK || r.Message != "fake-mcp 1.2.3 · mcp 2025-06-18 · sse · 4 tools" {
			t.Errorf("%s: %+v", mode, r)
		}
	}
}

func TestMCPStreamableOnlyNoFallback(t *testing.T) {
	srv := httptest.NewServer((&fakeMCP{}).legacy())
	defer srv.Close()
	r := Run(context.Background(), Job{Type: TypeMCP, Target: srv.URL + "/sse", Method: MCPStreamable})
	if r.OK || !strings.Contains(r.Message, "405") || strings.Contains(r.Message, "legacy") {
		t.Fatalf("%+v", r)
	}
}

func TestMCPNotMCP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = io.WriteString(w, "<html>hello</html>")
	}))
	defer srv.Close()
	r := Run(context.Background(), Job{Type: TypeMCP, Target: srv.URL})
	if r.OK || !strings.Contains(r.Message, "unexpected content type") {
		t.Fatalf("%+v", r)
	}
}

func TestMCPLegacyEndpointOtherHost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: endpoint\ndata: https://evil.example.com/messages\n\n")
	}))
	defer srv.Close()
	r := Run(context.Background(), Job{Type: TypeMCP, Target: srv.URL, Method: MCPSSE, Headers: map[string]string{"Authorization": "Bearer t"}})
	if r.OK || !strings.Contains(r.Message, "invalid endpoint") {
		t.Fatalf("%+v", r)
	}
}

func TestSSEReader(t *testing.T) {
	rd := newSSEReader(strings.NewReader(": c\r\nevent: a\r\ndata: 1\r\ndata: 2\r\n\r\n\nid: 5\n\ndata: x\n\n"))
	ev, err := rd.next()
	if err != nil || ev.event != "a" || ev.data != "1\n2" {
		t.Fatalf("%+v %v", ev, err)
	}
	ev, err = rd.next()
	if err != nil || ev.event != "" || ev.data != "x" {
		t.Fatalf("%+v %v", ev, err)
	}
	if _, err := rd.next(); err != io.EOF {
		t.Fatalf("err = %v", err)
	}
}

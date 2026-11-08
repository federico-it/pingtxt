package check

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
)

// loginServer wants a login (POST /login with the right password), then a
// bearer token from its JSON answer and the session cookie on /api/me.
func loginServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login":
			b, _ := io.ReadAll(r.Body)
			if r.Method != http.MethodPost || !strings.Contains(string(b), `"password":"pw"`) {
				w.WriteHeader(401)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "sid", Value: "s1", Path: "/"})
			w.Header().Set("X-Csrf", "c1")
			enc := json.NewEncoder(w)
			enc.SetEscapeHTML(false)
			_ = enc.Encode(map[string]any{"data": map[string]any{"token": "t-123"}, "user": "<id>42</id>"})
		case "/api/me":
			c, err := r.Cookie("sid")
			if err != nil || c.Value != "s1" || r.Header.Get("Authorization") != "Bearer t-123" || r.Header.Get("X-Csrf") != "c1" {
				w.WriteHeader(403)
				return
			}
			_, _ = w.Write([]byte(`{"id": 42, "user": "` + r.URL.Query().Get("id") + `"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestHTTPSteps(t *testing.T) {
	srv := loginServer(t)
	login := Step{
		Name: "login", Method: "POST", URL: srv.URL + "/login", Body: `{"password":"pw"}`,
		Headers: map[string]string{"Content-Type": "application/json"},
		Extract: []Extract{
			{Var: "token", From: FromJSON, Path: "data.token"},
			{Var: "csrf", From: FromHeader, Path: "X-Csrf"},
			{Var: "uid", From: FromRegex, Path: `<id>(\d+)</id>`},
		},
	}
	if err := ValidateStep(&login); err != nil {
		t.Fatal(err)
	}
	j := Job{
		Type: TypeKeyword, Target: srv.URL + "/api/me?id={{uid}}", Keyword: `"user": "42"`,
		Headers: map[string]string{"Authorization": "Bearer {{token}}", "X-Csrf": "{{ csrf }}"},
		Steps:   []Step{login},
	}
	r := run(t, j)
	if !r.OK || r.Steps != 1 {
		t.Fatalf("steps: %+v", r)
	}

	// A wrong password fails the first step, named in the message.
	bad := login
	bad.Body = `{"password":"no"}`
	j.Steps = []Step{bad}
	r = run(t, j)
	if r.OK || r.Steps != 1 || r.StatusCode != 401 || !strings.HasPrefix(r.Message, "step 1 (login): unexpected status 401") {
		t.Fatalf("bad login: %+v", r)
	}

	// A value that is not in the response.
	missing := login
	missing.Extract = []Extract{{Var: "x", From: FromJSON, Path: "data.nope"}}
	j.Steps = []Step{missing}
	r = run(t, j)
	if r.OK || !strings.Contains(r.Message, "{{x}}: data.nope not found") {
		t.Fatalf("missing value: %+v", r)
	}

	// Without the steps the request is refused.
	j.Steps = nil
	r = run(t, j)
	if r.OK || r.Steps != 0 {
		t.Fatalf("no steps: %+v", r)
	}
}

func TestValidateStep(t *testing.T) {
	bad := []Step{
		{URL: "ftp://x"},
		{URL: "https://x", Method: "BREW"},
		{URL: "https://x", Extract: []Extract{{Var: "1a", From: FromJSON, Path: "a"}}},
		{URL: "https://x", Extract: []Extract{{Var: "a", From: "cookie", Path: "a"}}},
		{URL: "https://x", Extract: []Extract{{Var: "a", From: FromRegex, Path: "("}}},
		{URL: "https://x", Extract: []Extract{{Var: "a", From: FromHeader}}},
	}
	for _, s := range bad {
		if err := ValidateStep(&s); err == nil {
			t.Errorf("%+v: no error", s)
		}
	}
	if s := (Step{URL: "{{base}}/login"}); ValidateStep(&s) == nil {
		t.Error("a URL must start with http(s)://")
	}
	ok := Step{URL: " https://{{host}}/login ", Method: "post"}
	if err := ValidateStep(&ok); err != nil || ok.Method != "POST" {
		t.Fatalf("%v %+v", err, ok)
	}
}

func TestExpand(t *testing.T) {
	vars := map[string]string{"a": "1", "b_2": "x"}
	if got := expand("{{a}}-{{ b_2 }}-{{c}}", vars); got != "1-x-{{c}}" {
		t.Fatal(got)
	}
}

func TestUDP(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if string(buf[:n]) == "quiet" {
				continue
			}
			_, _ = pc.WriteTo(append([]byte("pong:"), buf[:n]...), addr)
		}
	}()
	addr := pc.LocalAddr().String()

	if r := run(t, Job{Type: TypeUDP, Target: addr, Body: "ping", Keyword: "pong:ping"}); !r.OK {
		t.Fatalf("text: %+v", r)
	}
	if r := run(t, Job{Type: TypeUDP, Target: addr, Method: UDPHex, Body: "0x01 ff", Keyword: "706f6e673a01ff"}); !r.OK {
		t.Fatalf("hex: %+v", r)
	}
	if r := run(t, Job{Type: TypeUDP, Target: addr, Body: "ping", Keyword: "nope"}); r.OK || !strings.Contains(r.Message, "does not contain") {
		t.Fatalf("keyword: %+v", r)
	}
	if r := run(t, Job{Type: TypeUDP, Target: addr, Body: "quiet", TimeoutMs: 300}); r.OK || r.Message != "no reply (timeout)" {
		t.Fatalf("no reply: %+v", r)
	}
	if r := run(t, Job{Type: TypeUDP, Target: addr, Method: UDPHex, Body: "zz"}); r.OK || !strings.Contains(r.Message, "invalid hex") {
		t.Fatalf("bad hex: %+v", r)
	}
}

func TestWebSocket(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Key") != "k" {
			w.WriteHeader(401)
			return
		}
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"chat"}})
		if err != nil {
			return
		}
		defer c.CloseNow()
		ctx := context.Background()
		_, msg, err := c.Read(ctx)
		if err != nil {
			return
		}
		_ = c.Write(ctx, websocket.MessageText, append([]byte("echo "), msg...))
		_, _, _ = c.Read(ctx)
	}))
	t.Cleanup(srv.Close)
	url := "ws" + strings.TrimPrefix(srv.URL, "http")
	hdr := map[string]string{"X-Key": "k", "Sec-WebSocket-Protocol": "chat"}

	if r := run(t, Job{Type: TypeWS, Target: url, Headers: hdr}); !r.OK || r.Message != "connected (chat)" {
		t.Fatalf("connect: %+v", r)
	}
	if r := run(t, Job{Type: TypeWS, Target: url, Headers: hdr, Body: "hi", Keyword: "echo hi"}); !r.OK {
		t.Fatalf("echo: %+v", r)
	}
	if r := run(t, Job{Type: TypeWS, Target: url, Headers: hdr, Body: "hi", Keyword: "nope"}); r.OK || !strings.Contains(r.Message, "does not contain") {
		t.Fatalf("keyword: %+v", r)
	}
	if r := run(t, Job{Type: TypeWS, Target: url}); r.OK || r.Message != "unexpected status 401 (no WebSocket upgrade)" {
		t.Fatalf("no upgrade: %+v", r)
	}
	// The server waits for a message that never comes.
	if r := run(t, Job{Type: TypeWS, Target: url, Headers: hdr, Keyword: "x", TimeoutMs: 300}); r.OK || !strings.HasPrefix(r.Message, "no reply") {
		t.Fatalf("no reply: %+v", r)
	}
}

package check

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
)

func TestDiagnoseHTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old" {
			http.Redirect(w, r, "/new?token=secret", http.StatusFound)
			return
		}
		w.Header().Set("Set-Cookie", "sid=abc")
		w.Header().Set("X-Api-Key", "k")
		w.Header().Set("Server", "nginx")
		w.WriteHeader(503)
		_, _ = w.Write([]byte("private body"))
	}))
	defer srv.Close()

	ok := Run(context.Background(), Job{Type: TypeHTTP, Target: srv.URL, ExpectedStatus: "503", TimeoutMs: 2000, Diagnose: true})
	if !ok.OK || ok.Diag != nil {
		t.Fatalf("a passing check has no diagnostics: %+v", ok)
	}

	r := Run(context.Background(), Job{Type: TypeHTTP, Target: srv.URL + "/old", TimeoutMs: 2000, Diagnose: true, Headers: map[string]string{"Authorization": "Bearer x"}})
	if r.OK || r.Diag == nil || r.Diag.HTTP == nil {
		t.Fatalf("diagnostics missing: %+v", r)
	}
	h := r.Diag.HTTP
	if h.Status != 503 || len(h.Redirects) != 1 || strings.Contains(h.Redirects[0], "secret") || h.RemoteAddr == "" {
		t.Fatalf("http: %+v", h)
	}
	b, _ := json.Marshal(r.Diag)
	for _, leak := range []string{"abc", `"k"`, "private body", "Bearer"} {
		if strings.Contains(string(b), leak) {
			t.Fatalf("%s leaked: %s", leak, b)
		}
	}
	if !strings.Contains(string(b), "nginx") {
		t.Fatalf("server header missing: %s", b)
	}
	if len(r.Diag.TCP) != 1 || !r.Diag.TCP[0].OK || r.Diag.Port == "" {
		t.Fatalf("tcp: %+v", r.Diag.TCP)
	}
	if s := r.Diag.Summary(); !strings.Contains(s, "HTTP 503 (nginx)") || !strings.Contains(s, "ok") {
		t.Fatalf("summary: %q", s)
	}

	// Without the flag nothing is collected.
	if r := Run(context.Background(), Job{Type: TypeHTTP, Target: srv.URL, TimeoutMs: 2000}); r.Diag != nil {
		t.Fatal("diagnostics without Diagnose")
	}
}

func TestDiagnoseTCPRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()
	r := Run(context.Background(), Job{Type: TypeTCP, Target: addr, TimeoutMs: 1000, Diagnose: true})
	if r.OK || r.Diag == nil || len(r.Diag.TCP) != 1 || r.Diag.TCP[0].OK || len(r.Diag.DNS) != 0 {
		t.Fatalf("diag: %+v", r.Diag)
	}
	if !strings.Contains(r.Diag.Summary(), "TCP "+r.Diag.Port+":") {
		t.Fatalf("summary: %q", r.Diag.Summary())
	}
}

func TestTracerouteLoopback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("traceroute is Linux only")
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	port := ln.Addr().(*net.TCPAddr).Port
	tr := Traceroute(context.Background(), net.ParseIP("127.0.0.1"), port, 5)
	if tr.Error != "" || !tr.Reached || len(tr.Hops) != 1 || tr.Hops[0].Addr != "127.0.0.1" {
		t.Fatalf("trace: %+v", tr)
	}
	// A closed port is reached too (refused).
	ln2, _ := net.Listen("tcp", "127.0.0.1:0")
	closed := ln2.Addr().(*net.TCPAddr).Port
	ln2.Close()
	if tr := Traceroute(context.Background(), net.ParseIP("127.0.0.1"), closed, 5); !tr.Reached {
		t.Fatalf("closed port: %+v", tr)
	}
}

func TestDiagTargets(t *testing.T) {
	cases := []struct {
		j          Job
		host, port string
		tls        bool
	}{
		{Job{Type: TypeHTTP, Target: "https://example.com/x"}, "example.com", "443", true},
		{Job{Type: TypeKeyword, Target: "http://[2001:db8::1]:8080/"}, "2001:db8::1", "8080", false},
		{Job{Type: TypeTCP, Target: "db.local:5432"}, "db.local", "5432", false},
		{Job{Type: TypePing, Target: "[2001:db8::2]"}, "2001:db8::2", "", false},
		{Job{Type: TypeDNS, Target: "example.com", DNSServer: "1.1.1.1"}, "1.1.1.1", "53", false},
		{Job{Type: TypeDNS, Target: "example.com"}, "", "", false},
		{Job{Type: TypePostgres, Target: "postgres://u:p@pg.local/db"}, "pg.local", "5432", false},
		{Job{Type: TypeRedis, Target: "rediss://r.local"}, "r.local", "6379", true},
		{Job{Type: TypeMongo, Target: "mongodb://a.local:27018,b.local/db"}, "a.local", "27018", false},
		{Job{Type: TypeSMTP, Target: "smtps://mail.local"}, "mail.local", "465", true},
		{Job{Type: TypeGRPC, Target: "grpcs://g.local:443/svc"}, "g.local", "443", true},
		{Job{Type: TypeDocker, Target: "web"}, "", "", false},
	}
	for _, c := range cases {
		got := targetFor(c.j)
		if got.host != c.host || got.port != c.port || got.tls != c.tls {
			t.Errorf("%s %s: got %+v", c.j.Type, c.j.Target, got)
		}
	}
	if targetFor(Job{Type: TypeDocker}).note == "" {
		t.Error("docker needs a note")
	}
}

func TestSecretHeaders(t *testing.T) {
	for _, h := range []string{"Set-Cookie", "Authorization", "X-Auth-Token", "X-Session-Id", "X-CSRF-Token", "WWW-Authenticate"} {
		if !secretHeader(h) {
			t.Errorf("%s should be redacted", h)
		}
	}
	for _, h := range []string{"Server", "Content-Type", "Cf-Ray", "Location"} {
		if secretHeader(h) {
			t.Errorf("%s should be kept", h)
		}
	}
}

func TestDiagShrink(t *testing.T) {
	d := &Diag{HTTP: &DiagHTTP{Status: 500}}
	for i := 0; i < 400; i++ {
		d.HTTP.Headers = append(d.HTTP.Headers, DiagHeader{Name: "X-H", Value: strings.Repeat("v", 100)})
	}
	d.shrink()
	if b, _ := json.Marshal(d); len(b) > diagMaxBytes {
		t.Fatalf("still %d bytes", len(b))
	}
}

package check

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStatusMatches(t *testing.T) {
	cases := []struct {
		spec string
		code int
		want bool
	}{
		{"", 200, true}, {"", 301, true}, {"", 404, false}, {"", 500, false},
		{"200-299", 204, true}, {"200-299", 302, false},
		{"200,301", 301, true}, {"200,301", 302, false},
		{"2xx,404", 404, true}, {"2xx", 201, true}, {"2xx", 500, false},
	}
	for _, c := range cases {
		if got := StatusMatches(c.spec, c.code); got != c.want {
			t.Errorf("StatusMatches(%q, %d) = %v, want %v", c.spec, c.code, got, c.want)
		}
	}
}

func TestHTTPAndKeyword(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/fail" {
			w.WriteHeader(503)
		}
		w.Write([]byte("hello pingtxt"))
	}))
	defer srv.Close()
	ctx := context.Background()

	if r := Run(ctx, Job{Type: TypeHTTP, Target: srv.URL}); !r.OK || r.StatusCode != 200 {
		t.Fatalf("http ok: %+v", r)
	}
	if r := Run(ctx, Job{Type: TypeHTTP, Target: srv.URL + "/fail"}); r.OK {
		t.Fatalf("http 503 should fail: %+v", r)
	}
	if r := Run(ctx, Job{Type: TypeKeyword, Target: srv.URL, Keyword: "pingtxt"}); !r.OK {
		t.Fatalf("keyword should match: %+v", r)
	}
	if r := Run(ctx, Job{Type: TypeKeyword, Target: srv.URL, Keyword: "nope"}); r.OK {
		t.Fatalf("keyword should not match: %+v", r)
	}
	if r := Run(ctx, Job{Type: TypeKeyword, Target: srv.URL, Keyword: "pingtxt", KeywordInvert: true}); r.OK {
		t.Fatalf("inverted keyword should fail: %+v", r)
	}
}

func TestTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	if r := Run(context.Background(), Job{Type: TypeTCP, Target: addr, TimeoutMs: 2000}); !r.OK {
		t.Fatalf("tcp open: %+v", r)
	}
	ln.Close()
	if r := Run(context.Background(), Job{Type: TypeTCP, Target: addr, TimeoutMs: 2000}); r.OK {
		t.Fatalf("tcp closed should fail: %+v", r)
	}
}

func TestHTTPPhases(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()
	r := Run(context.Background(), Job{Type: TypeHTTP, Target: srv.URL, IgnoreTLS: true, TimeoutMs: 5000})
	if !r.OK || r.Timing == nil {
		t.Fatalf("result %+v", r)
	}
	p := r.Timing
	if p.ConnectMs <= 0 || p.TLSMs <= 0 || p.TTFBMs < 20 || p.DNSMs != 0 {
		t.Fatalf("phases %+v", p)
	}
	if sum := p.DNSMs + p.ConnectMs + p.TLSMs + p.TTFBMs; sum > r.LatencyMs+0.01 {
		t.Fatalf("phases %.2f longer than latency %.2f", sum, r.LatencyMs)
	}

	// No response, no phases.
	srv.Close()
	if r := Run(context.Background(), Job{Type: TypeHTTP, Target: srv.URL, TimeoutMs: 2000}); r.OK || r.Timing != nil {
		t.Fatalf("closed server: %+v", r)
	}
}

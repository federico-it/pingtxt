package check

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
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

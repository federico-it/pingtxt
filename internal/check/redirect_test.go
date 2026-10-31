package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Monitor headers (API keys) must not follow a redirect to another host.
func TestHTTPHeadersNotForwardedToOtherHost(t *testing.T) {
	var got string
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Api-Key")
	}))
	defer other.Close()
	// Same server, other host name: 127.0.0.1 -> localhost.
	target := strings.Replace(other.URL, "127.0.0.1", "localhost", 1)
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "k" {
			t.Error("header missing on the first request")
		}
		http.Redirect(w, r, target+"/next", http.StatusFound)
	}))
	defer first.Close()

	r := Run(context.Background(), Job{Type: TypeHTTP, Target: first.URL, TimeoutMs: 5000, Headers: map[string]string{"X-Api-Key": "k"}})
	if !r.OK {
		t.Fatalf("check failed: %s", r.Message)
	}
	if got != "" {
		t.Fatalf("X-Api-Key forwarded to another host: %q", got)
	}
}

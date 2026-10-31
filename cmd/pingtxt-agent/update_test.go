package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/federico-it/pingtxt/internal/protocol"
)

func serveBinary(t *testing.T, body string) (*httptest.Server, string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/download/pingtxt-agent-test" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	s := sha256.Sum256([]byte(body))
	return srv, hex.EncodeToString(s[:])
}

func TestDownloadUpdate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the fake binary")
	}
	// A shell script stands in for the new binary: it only has to answer -version.
	script := "#!/bin/sh\necho v9.9.9\n"
	srv, sum := serveBinary(t, script)

	dir := t.TempDir()
	exe := filepath.Join(dir, "pingtxt-agent")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	cases := []struct {
		name string
		u    protocol.Update
		err  string
	}{
		{"bad path", protocol.Update{Version: "v9.9.9", Path: "/etc/passwd", SHA256: sum}, "invalid update"},
		{"bad checksum", protocol.Update{Version: "v9.9.9", Path: "/download/pingtxt-agent-test", SHA256: strings.Repeat("0", 64)}, "checksum mismatch"},
		{"wrong version", protocol.Update{Version: "v1.0.0", Path: "/download/pingtxt-agent-test", SHA256: sum}, "reports version"},
		{"missing file", protocol.Update{Version: "v9.9.9", Path: "/download/nope", SHA256: sum}, "404"},
	}
	for _, c := range cases {
		err := downloadUpdate(ctx, srv.Client(), srv.URL, exe, c.u)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Fatalf("%s: want error containing %q, got %v", c.name, c.err, err)
		}
		if b, _ := os.ReadFile(exe); string(b) != "old" {
			t.Fatalf("%s: binary replaced after a failed update", c.name)
		}
	}

	if err := downloadUpdate(ctx, srv.Client(), srv.URL, exe, protocol.Update{Version: "v9.9.9", Path: "/download/pingtxt-agent-test", SHA256: sum}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != script {
		t.Fatal("binary not replaced")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatalf("temporary files left behind: %v", entries)
	}
}

func TestToHTTP(t *testing.T) {
	for in, want := range map[string]string{
		"wss://hub.example.com" + protocol.Path:     "https://hub.example.com",
		"ws://127.0.0.1:8090" + protocol.Path:       "http://127.0.0.1:8090",
		"wss://example.com/pingtxt" + protocol.Path: "https://example.com/pingtxt",
	} {
		if got := toHTTP(in); got != want {
			t.Errorf("toHTTP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSecureHub(t *testing.T) {
	for u, want := range map[string]bool{
		"wss://status.example.com/api/agent/ws": true,
		"ws://status.example.com/api/agent/ws":  false,
		"ws://203.0.113.5:8090/api/agent/ws":    false,
		"ws://127.0.0.1:8090/api/agent/ws":      true,
		"ws://localhost:8090/api/agent/ws":      true,
		"ws://192.168.1.10:8090/api/agent/ws":   true,
		"ws://10.0.0.2/api/agent/ws":            true,
		"ws://[fd00::1]:8090/api/agent/ws":      true,
		"ws://pingtxt.lan:8090/api/agent/ws":    false,
	} {
		if got := secureHub(u); got != want {
			t.Errorf("%s: got %v want %v", u, got, want)
		}
	}
}

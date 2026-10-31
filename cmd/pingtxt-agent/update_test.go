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

func TestReleaseCheck(t *testing.T) {
	good := strings.Repeat("ab", 32)
	other := strings.Repeat("cd", 32)
	name := agentFileName()
	sums := "0000000000000000000000000000000000000000000000000000000000000000  pingtxt-agent-plan9-386\n" +
		good + "  " + name + "\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.2.3/SHA256SUMS":
			w.Write([]byte(sums))
		case "/v1.2.4/SHA256SUMS":
			w.Write([]byte("0000000000000000000000000000000000000000000000000000000000000000  pingtxt-linux-amd64\n"))
		case "/v1.2.5/SHA256SUMS":
			http.Error(w, "boom", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ctx := context.Background()

	cases := []struct {
		name, mode, version, sum string
		err, warn                string
	}{
		{"match", verifyAuto, "v1.2.3", good, "", ""},
		{"match upper case", verifyStrict, "v1.2.3", strings.ToUpper(good), "", ""},
		{"mismatch", verifyAuto, "v1.2.3", other, "does not match", ""},
		{"no entry for platform", verifyAuto, "v1.2.4", good, "no checksum", ""},
		{"release not found", verifyAuto, "v9.0.0", good, "no checksum", ""},
		{"github down, auto", verifyAuto, "v1.2.5", good, "", "cannot read"},
		{"github down, strict", verifyStrict, "v1.2.5", good, "cannot read", ""},
		{"dev build, auto", verifyAuto, "main-abc1234", other, "", "not a release"},
		{"dev build, strict", verifyStrict, "main-abc1234", good, "not a published release", ""},
		{"path in version", verifyStrict, "v1.2.3/../x", good, "not a published release", ""},
		{"off", verifyOff, "v1.2.3", other, "", ""},
	}
	for _, c := range cases {
		warn, err := releaseCheck(ctx, srv.Client(), srv.URL+"/", c.mode, c.version, c.sum)
		if c.err == "" && err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
		}
		if c.err != "" && (err == nil || !strings.Contains(err.Error(), c.err)) {
			t.Errorf("%s: want error containing %q, got %v", c.name, c.err, err)
		}
		if !strings.Contains(warn, c.warn) || (c.warn == "" && warn != "") {
			t.Errorf("%s: want warning %q, got %q", c.name, c.warn, warn)
		}
	}
}

func TestSelfUpdateSupportContainer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no self-update on windows")
	}
	t.Setenv("PINGTXT_CONTAINER", "1")
	if _, reason := selfUpdateSupport(false, ""); reason != protocol.NoUpdateContainer {
		t.Fatalf("no update folder: reason %q, want container", reason)
	}
	if _, reason := selfUpdateSupport(false, filepath.Join(t.TempDir(), "missing")); reason != protocol.NoUpdateContainer {
		t.Fatalf("missing update folder: reason %q, want container", reason)
	}
	dir := t.TempDir()
	target, reason := selfUpdateSupport(false, dir)
	if reason != "" || target != filepath.Join(dir, "pingtxt-agent") {
		t.Fatalf("got %q, %q", target, reason)
	}
	if _, reason := selfUpdateSupport(true, dir); reason != protocol.NoUpdateDisabled {
		t.Fatalf("disabled: reason %q", reason)
	}
}

func TestUpdatedBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script as the binary")
	}
	ctx := context.Background()
	setup := func(t *testing.T, script, base string) string {
		t.Helper()
		dir := t.TempDir()
		if script != "" {
			if err := os.WriteFile(filepath.Join(dir, updatedBinName), []byte(script), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		if base != "" {
			if err := os.WriteFile(filepath.Join(dir, imageVersionName), []byte(base+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}
	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }
	good := "#!/bin/sh\necho v1.2.0\n"

	t.Run("nothing installed", func(t *testing.T) {
		bin, _, err := updatedBinary(ctx, t.TempDir(), "v1.0.0")
		if bin != "" || err != nil {
			t.Fatalf("got %q, %v", bin, err)
		}
	})
	t.Run("same image", func(t *testing.T) {
		dir := setup(t, good, "v1.0.0")
		bin, v, err := updatedBinary(ctx, dir, "v1.0.0")
		if err != nil || bin != filepath.Join(dir, updatedBinName) || v != "v1.2.0" {
			t.Fatalf("got %q, %q, %v", bin, v, err)
		}
	})
	for name, c := range map[string]struct{ script, base string }{
		"new image":        {good, "v1.0.0"},
		"no image version": {good, ""},
		"broken binary":    {"#!/bin/sh\nexit 3\n", "v1.1.0"},
	} {
		t.Run(name, func(t *testing.T) {
			dir := setup(t, c.script, c.base)
			bin, _, err := updatedBinary(ctx, dir, "v1.1.0")
			if bin != "" || err == nil {
				t.Fatalf("got %q, %v, want the update discarded", bin, err)
			}
			if exists(filepath.Join(dir, updatedBinName)) || exists(filepath.Join(dir, imageVersionName)) {
				t.Fatal("update folder not cleared")
			}
		})
	}
}

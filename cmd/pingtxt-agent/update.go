package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/federico-it/pingtxt/internal/protocol"
)

// maxBinarySize caps the download, as a guard against a broken hub response.
const maxBinarySize = 200 << 20

// selfUpdateSupport reports whether this agent can replace its own binary,
// and if not, why. It runs once at start.
func selfUpdateSupport(disabled bool) (exe string, reason string) {
	if disabled {
		return "", protocol.NoUpdateDisabled
	}
	if runtime.GOOS == "windows" {
		return "", protocol.NoUpdateUnsupported
	}
	if inContainer() {
		return "", protocol.NoUpdateContainer
	}
	exe, err := os.Executable()
	if err == nil {
		exe, err = filepath.EvalSymlinks(exe)
	}
	if err != nil {
		return "", protocol.NoUpdateUnsupported
	}
	// The new binary is written next to the old one and renamed over it,
	// so the folder must be writable.
	f, err := os.CreateTemp(filepath.Dir(exe), ".pingtxt-agent-probe-*")
	if err != nil {
		return "", protocol.NoUpdateNotWritable
	}
	f.Close()
	os.Remove(f.Name())
	return exe, ""
}

// secureHub reports whether updates from the hub at wsURL can be trusted:
// it is reached over TLS, or over plain http on a loopback or private
// network address (a home lab without certificates).
func secureHub(wsURL string) bool {
	u, err := url.Parse(wsURL)
	if err != nil {
		return false
	}
	if u.Scheme == "wss" {
		return true
	}
	if strings.EqualFold(u.Hostname(), "localhost") {
		return true
	}
	ip := net.ParseIP(u.Hostname())
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate())
}

func inContainer() bool {
	if os.Getenv("PINGTXT_CONTAINER") != "" {
		return true
	}
	for _, p := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// downloadUpdate fetches the new binary from the hub, checks its digest and
// version, and puts it in place of exe. The running process keeps the old
// file open; the caller restarts with restart().
func downloadUpdate(ctx context.Context, client *http.Client, hubURL, exe string, u protocol.Update) error {
	if u.Version == "" || u.SHA256 == "" || !strings.HasPrefix(u.Path, "/download/") {
		return errors.New("invalid update request")
	}
	want, err := hex.DecodeString(u.SHA256)
	if err != nil || len(want) != sha256.Size {
		return errors.New("invalid checksum in update request")
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(dctx, http.MethodGet, strings.TrimRight(hubURL, "/")+u.Path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "pingtxt-agent/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download: hub answered %s", resp.Status)
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".pingtxt-agent-update-*")
	if err != nil {
		return err
	}
	keep := false
	defer func() {
		if !keep {
			tmp.Close()
			os.Remove(tmp.Name())
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, maxBinarySize+1))
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	if n > maxBinarySize {
		return errors.New("download: file too large")
	}
	if got := hash.Sum(nil); hex.EncodeToString(got) != hex.EncodeToString(want) {
		return errors.New("checksum mismatch, update discarded")
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return err
	}
	// Make sure the new binary starts on this machine and is the version we expect.
	vctx, vcancel := context.WithTimeout(ctx, 15*time.Second)
	defer vcancel()
	out, err := exec.CommandContext(vctx, tmp.Name(), "-version").Output()
	if err != nil {
		return fmt.Errorf("new binary does not run: %w", err)
	}
	if got := strings.TrimSpace(string(out)); got != u.Version {
		return fmt.Errorf("new binary reports version %q, expected %q", got, u.Version)
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		return err
	}
	keep = true
	return nil
}

// restart replaces the running process with the binary at exe, keeping the
// same arguments, environment and PID (so systemd sees no restart).
func restart(exe string) error {
	return syscall.Exec(exe, os.Args, os.Environ())
}

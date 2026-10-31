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
	"regexp"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/federico-it/pingtxt/internal/protocol"
)

// maxBinarySize caps the download, as a guard against a broken hub response.
const maxBinarySize = 200 << 20

// How an update is checked against the checksums published with the GitHub
// release of its version (see releaseCheck).
const (
	verifyAuto   = "auto"   // check release versions; go ahead if GitHub cannot be reached
	verifyStrict = "strict" // only install published releases whose checksum matches
	verifyOff    = "off"    // trust the hub (forks and private builds)
)

// defaultReleaseURL is where release assets are downloaded from: the
// version and the file name are appended.
const defaultReleaseURL = "https://github.com/federico-it/pingtxt/releases/download/"

// maxSumsSize caps the SHA256SUMS download.
const maxSumsSize = 64 << 10

var releaseTagRe = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

func validVerifyMode(m string) bool {
	return m == verifyAuto || m == verifyStrict || m == verifyOff
}

// agentFileName is the name of this platform's binary in a release.
func agentFileName() string {
	return "pingtxt-agent-" + runtime.GOOS + "-" + runtime.GOARCH
}

// releaseCheck compares the checksum the hub sent for an update with the one
// in the SHA256SUMS file of the GitHub release of that version. The hub
// bundles the same binaries the release workflow publishes (the builds are
// reproducible), so a hub that was tampered with cannot make agents install
// a binary that was not built from a release. SHA256SUMS itself is signed
// with cosign in the release workflow.
//
// It returns an error when the update must be refused, and a warning when
// the check was skipped.
func releaseCheck(ctx context.Context, client *http.Client, baseURL, mode, version, sum string) (warn string, err error) {
	if mode == verifyOff {
		return "", nil
	}
	if !releaseTagRe.MatchString(version) {
		if mode == verifyStrict {
			return "", fmt.Errorf("version %q is not a published release, refused by the strict update check", version)
		}
		return "not a release version: published checksums not checked", nil
	}
	name := agentFileName()
	want, found, err := releaseSum(ctx, client, strings.TrimRight(baseURL, "/")+"/"+version+"/SHA256SUMS", name)
	switch {
	case err != nil && mode == verifyStrict:
		return "", fmt.Errorf("cannot read the release checksums: %w", err)
	case err != nil:
		return "cannot read the release checksums, update not checked against them: " + err.Error(), nil
	case !found:
		return "", fmt.Errorf("release %s publishes no checksum for %s, update refused", version, name)
	case !strings.EqualFold(want, sum):
		return "", fmt.Errorf("the hub's binary does not match the checksum published in release %s, update refused", version)
	}
	return "", nil
}

// releaseSum fetches a SHA256SUMS file and returns the digest listed for
// name. found is false when the file or the entry does not exist.
func releaseSum(ctx context.Context, client *http.Client, sumsURL, name string) (sum string, found bool, err error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sumsURL, nil)
	if err != nil {
		return "", false, err
	}
	req.Header.Set("User-Agent", "pingtxt-agent/"+version)
	resp, err := client.Do(req)
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return "", false, fmt.Errorf("%s answered %s", req.URL.Host, resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxSumsSize))
	if err != nil {
		return "", false, err
	}
	// Lines look like "<hex>  <name>" (sha256sum output; "*" marks binary mode).
	for line := range strings.Lines(string(body)) {
		f := strings.Fields(line)
		if len(f) == 2 && strings.TrimPrefix(f[1], "*") == name {
			if b, err := hex.DecodeString(f[0]); err != nil || len(b) != sha256.Size {
				return "", false, errors.New("malformed SHA256SUMS")
			}
			return f[0], true, nil
		}
	}
	return "", false, nil
}

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

// update checks an update against the release checksums, then installs it.
func (a *agent) update(ctx context.Context, u protocol.Update) error {
	warn, err := releaseCheck(ctx, a.client, a.releaseURL, a.verify, u.Version, u.SHA256)
	if err != nil {
		return err
	}
	if warn != "" {
		a.log.Warn("update check", "version", u.Version, "note", warn)
	}
	return downloadUpdate(ctx, a.client, a.httpURL, a.exe, u)
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

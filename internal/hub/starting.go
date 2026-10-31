package hub

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"
)

// Boot phases reported by /api/health while the hub starts.
const (
	PhaseRestore  = "restore"  // applying a backup staged from the web UI
	PhaseDatabase = "database" // connecting and running migrations
	PhaseStarting = "starting" // loading settings, scheduler, agents
)

// HubHeader marks every response written by the hub. The service worker uses
// it to tell the hub's own answers from a reverse proxy's error page.
const HubHeader = "X-Pingtxt-Hub"

// startingPage is used when the web UI was not built (go test, go run).
const startingPage = `<!doctype html><meta charset="utf-8"><meta http-equiv="refresh" content="3"><title>pingtxt · starting</title><p style="font-family:monospace">pingtxt is starting, this page reloads by itself…</p>`

// Starting answers every request while the hub boots, so the browser shows an
// animated "starting / updating" page (web/public/starting.html) right away
// instead of a connection error. API calls get 503 with Retry-After, and
// /api/health says which phase the hub is in.
type Starting struct {
	page     []byte
	ui       fs.FS
	version  string
	previous string
	since    time.Time
	phase    atomic.Value // string
}

// NewStarting builds the boot handler. previous is the version that ran
// before this start ("" if unknown): when it differs, the page says the hub
// is updating.
func NewStarting(ui fs.FS, version, previous string) *Starting {
	s := &Starting{ui: ui, version: version, previous: previous, since: time.Now()}
	s.page = []byte(startingPage)
	if ui != nil {
		if b, err := fs.ReadFile(ui, "starting.html"); err == nil {
			s.page = b
		}
	}
	s.phase.Store(PhaseDatabase)
	return s
}

// SetPhase updates the phase shown on the page.
func (s *Starting) SetPhase(p string) { s.phase.Store(p) }

func (s *Starting) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h := w.Header()
	h.Set(HubHeader, "starting")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Referrer-Policy", "same-origin")
	h.Set("Cache-Control", "no-store")
	h.Set("Retry-After", "5")

	p := path.Clean("/" + r.URL.Path)
	switch {
	case p == "/api/health":
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"ok": false, "starting": true, "phase": s.phase.Load(),
			"version": s.version, "previous_version": s.previous, "since": s.since.Unix(),
		})
		return
	case strings.HasPrefix(p, "/api/"):
		writeErr(w, http.StatusServiceUnavailable, "pingtxt is starting, try again in a few seconds")
		return
	case p == "/sw.js" || p == "/icon.svg":
		// Static files the page and the service worker may ask for.
		if s.ui != nil {
			if b, err := fs.ReadFile(s.ui, p[1:]); err == nil {
				if p == "/sw.js" {
					h.Set("Content-Type", "text/javascript; charset=utf-8")
				} else {
					h.Set("Content-Type", "image/svg+xml")
				}
				h.Del("Retry-After")
				w.Write(b)
				return
			}
		}
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "pingtxt is starting, try again in a few seconds", http.StatusServiceUnavailable)
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	if r.Method == http.MethodGet {
		w.Write(s.page)
	}
}

// SwapHandler serves through a handler that can be replaced while running:
// the boot page first, then the real hub once it is ready.
type SwapHandler struct{ h atomic.Value }

// NewSwapHandler starts with h.
func NewSwapHandler(h http.Handler) *SwapHandler {
	s := &SwapHandler{}
	s.Set(h)
	return s
}

// Set replaces the handler for the next requests.
func (s *SwapHandler) Set(h http.Handler) { s.h.Store(&h) }

func (s *SwapHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	(*s.h.Load().(*http.Handler)).ServeHTTP(w, r)
}

// versionFile keeps the version of the last start in the data dir, so the
// boot page can tell an update from a plain restart before the database is open.
func versionFile(dataDir string) string { return filepath.Join(dataDir, "version") }

// PreviousVersion returns the version that ran before, "" if unknown.
func PreviousVersion(dataDir string) string {
	b, err := os.ReadFile(versionFile(dataDir))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// RecordVersion saves the running version for the next start.
func RecordVersion(dataDir, version string) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(versionFile(dataDir), []byte(version+"\n"), 0o644)
}

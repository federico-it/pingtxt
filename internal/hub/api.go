package hub

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	_ "embed"

	"golang.org/x/crypto/bcrypt"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/protocol"
	"github.com/federico-it/pingtxt/internal/store"
)

//go:embed install-agent.sh
var installScript []byte

const sessionCookie = "pingtxt_session"

// Handler returns the HTTP handler serving the API and the web UI.
func (h *Hub) Handler(ui fs.FS) http.Handler {
	mux := http.NewServeMux()
	auth := h.requireAuth       // session or API token
	session := h.requireSession // browser session only
	admin := h.requireAdmin     // browser session of an admin

	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "version": h.cfg.Version})
	})
	mux.HandleFunc("GET /api/setup", h.getSetup)
	mux.HandleFunc("POST /api/setup", h.postSetup)
	mux.HandleFunc("POST /api/auth/login", h.login)
	mux.HandleFunc("POST /api/auth/login/2fa", h.loginSecondFactor)
	mux.HandleFunc("POST /api/auth/logout", h.logout)
	mux.HandleFunc("GET /api/auth/me", auth(h.me))
	mux.HandleFunc("POST /api/auth/password", session(h.changePassword))
	mux.HandleFunc("GET /api/auth/options", h.authOptions)
	mux.HandleFunc("GET /api/auth/oauth/{provider}/start", h.oauthStart)
	mux.HandleFunc("GET /api/auth/oauth/{provider}/callback", h.oauthCallback)
	mux.HandleFunc("DELETE /api/auth/identities/{provider}", session(h.unlinkIdentity))
	mux.HandleFunc("POST /api/auth/2fa/setup", session(h.setupTwoFactor))
	mux.HandleFunc("POST /api/auth/2fa/enable", session(h.enableTwoFactor))
	mux.HandleFunc("POST /api/auth/2fa/disable", session(h.disableTwoFactor))
	mux.HandleFunc("POST /api/auth/2fa/recovery-codes", session(h.regenerateRecoveryCodes))
	mux.HandleFunc("GET /api/tokens", session(h.listTokens))
	mux.HandleFunc("POST /api/tokens", session(h.createToken))
	mux.HandleFunc("DELETE /api/tokens/{id}", session(h.deleteToken))

	mux.HandleFunc("GET /api/settings", admin(h.getSettings))
	mux.HandleFunc("PUT /api/settings/general", admin(h.putGeneralSettings))
	mux.HandleFunc("PUT /api/settings/auth", admin(h.putAuthSettings))
	mux.HandleFunc("GET /api/users", admin(h.listUsers))
	mux.HandleFunc("POST /api/users", admin(h.createUser))
	mux.HandleFunc("PUT /api/users/{id}", admin(h.updateUser))
	mux.HandleFunc("DELETE /api/users/{id}", admin(h.deleteUser))
	mux.HandleFunc("DELETE /api/users/{id}/2fa", admin(h.resetUserTwoFactor))

	// Backups and configuration export/import: admins only.
	mux.HandleFunc("GET /api/backups", admin(h.listBackups))
	mux.HandleFunc("POST /api/backups", admin(h.createBackup))
	mux.HandleFunc("POST /api/backups/upload", admin(h.uploadBackup))
	mux.HandleFunc("DELETE /api/backups/restore", admin(h.cancelRestore))
	mux.HandleFunc("GET /api/backups/{name}", admin(h.downloadBackup))
	mux.HandleFunc("DELETE /api/backups/{name}", admin(h.deleteBackup))
	mux.HandleFunc("POST /api/backups/{name}/restore", admin(h.stageRestore))
	mux.HandleFunc("PUT /api/settings/backups", admin(h.putBackupSettings))
	mux.HandleFunc("GET /api/config/export", auth(h.exportConfig)) // secrets only with a session
	mux.HandleFunc("POST /api/config/import", admin(h.importConfig))

	mux.HandleFunc("GET /api/overview", auth(h.overview))
	mux.HandleFunc("GET /api/monitors", auth(h.listMonitors))
	mux.HandleFunc("POST /api/monitors", auth(h.createMonitor))
	mux.HandleFunc("GET /api/monitors/{id}", auth(h.getMonitor))
	mux.HandleFunc("PUT /api/monitors/{id}", auth(h.updateMonitor))
	mux.HandleFunc("DELETE /api/monitors/{id}", auth(h.deleteMonitor))
	mux.HandleFunc("POST /api/monitors/{id}/pause", auth(h.pauseMonitor))
	mux.HandleFunc("GET /api/monitors/{id}/series", auth(h.monitorSeries))
	mux.HandleFunc("GET /api/monitors/{id}/results", auth(h.monitorResults))
	mux.HandleFunc("POST /api/monitors/{id}/push-token", auth(h.rotatePushToken))
	mux.HandleFunc("/api/push/{token}", h.push)
	mux.HandleFunc("POST /api/monitors/{id}/expiry", auth(h.checkMonitorExpiry))
	mux.HandleFunc("GET /api/incidents", auth(h.listIncidents))
	mux.HandleFunc("GET /api/maintenances", auth(h.listMaintenances))
	mux.HandleFunc("POST /api/maintenances", auth(h.createMaintenance))
	mux.HandleFunc("PUT /api/maintenances/{id}", auth(h.updateMaintenance))
	mux.HandleFunc("DELETE /api/maintenances/{id}", auth(h.deleteMaintenance))

	mux.HandleFunc("GET /api/groups", auth(h.listGroups))
	mux.HandleFunc("POST /api/groups", auth(h.saveGroup))
	mux.HandleFunc("PUT /api/groups/{id}", auth(h.saveGroup))
	mux.HandleFunc("DELETE /api/groups/{id}", auth(h.deleteGroup))
	mux.HandleFunc("PUT /api/groups/{id}/monitors", auth(h.setGroupMonitors))
	mux.HandleFunc("GET /api/tags", auth(h.listTags))
	mux.HandleFunc("POST /api/tags", auth(h.saveTag))
	mux.HandleFunc("PUT /api/tags/{id}", auth(h.saveTag))
	mux.HandleFunc("DELETE /api/tags/{id}", auth(h.deleteTag))

	mux.HandleFunc("GET /api/locations", auth(h.listLocations))
	mux.HandleFunc("GET /api/agents", auth(h.listAgents))
	mux.HandleFunc("POST /api/agents", auth(h.createAgent))
	mux.HandleFunc("PUT /api/agents/{id}", auth(h.updateAgent))
	mux.HandleFunc("DELETE /api/agents/{id}", auth(h.deleteAgent))
	mux.HandleFunc("POST /api/agents/{id}/token", auth(h.rotateAgentToken))
	mux.HandleFunc("POST /api/agents/{id}/update", auth(h.updateAgentNow))
	mux.HandleFunc("PUT /api/settings/agent-updates", auth(h.putAgentUpdateSettings))

	mux.HandleFunc("GET /api/notifiers", auth(h.listNotifiers))
	mux.HandleFunc("POST /api/notifiers", auth(h.createNotifier))
	mux.HandleFunc("PUT /api/notifiers/{id}", auth(h.updateNotifier))
	mux.HandleFunc("DELETE /api/notifiers/{id}", auth(h.deleteNotifier))
	mux.HandleFunc("POST /api/notifiers/test", auth(h.testDraftNotifier))
	mux.HandleFunc("POST /api/notifiers/{id}/test", auth(h.testNotifier))

	mux.HandleFunc("GET /api/status-pages", auth(h.listStatusPages))
	mux.HandleFunc("POST /api/status-pages", auth(h.saveStatusPage))
	mux.HandleFunc("GET /api/status-pages/{id}", auth(h.getStatusPage))
	mux.HandleFunc("PUT /api/status-pages/{id}", auth(h.saveStatusPage))
	mux.HandleFunc("DELETE /api/status-pages/{id}", auth(h.deleteStatusPage))
	mux.HandleFunc("GET /api/public/status/{slug}", h.publicStatus)

	mux.HandleFunc("GET /metrics", h.metrics)

	mux.HandleFunc("GET "+protocol.Path, h.ServeAgent)
	mux.HandleFunc("GET /install-agent.sh", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
		w.Write(installScript)
	})
	mux.HandleFunc("GET /download/{file}", h.download)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, "not found")
	})
	mux.Handle("/", spaHandler(ui))
	return securityHeaders(gzipAPI(h.customDomains(mux, ui)))
}

// gzipWriter marks API responses whose client accepts gzip; writeJSON
// compresses large bodies for them.
type gzipWriter struct{ http.ResponseWriter }

func (g gzipWriter) Unwrap() http.ResponseWriter { return g.ResponseWriter }

func gzipAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != protocol.Path &&
			strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w = gzipWriter{w}
		}
		next.ServeHTTP(w, r)
	})
}

func acceptsGzip(w http.ResponseWriter) bool {
	for w != nil {
		if _, ok := w.(gzipWriter); ok {
			return true
		}
		u, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = u.Unwrap()
	}
	return false
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		// Mutating API calls must be JSON: blocks cross-site form posts (CSRF).
		// Heartbeats are exempt: they are authenticated by the URL token, not
		// by the session cookie, and any client must be able to send them.
		if strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/api/push/") && r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			ctype := r.Header.Get("Content-Type")
			// Backup uploads are raw files. application/octet-stream is not a
			// CORS "simple" type either, so a cross-site form cannot send it.
			upload := r.URL.Path == "/api/backups/upload" && strings.HasPrefix(ctype, "application/octet-stream")
			if r.ContentLength != 0 && !strings.HasPrefix(ctype, "application/json") && !upload {
				writeErr(w, http.StatusUnsupportedMediaType, "content-type must be application/json")
				return
			}
			if o := r.Header.Get("Origin"); o != "" {
				if u, err := url.Parse(o); err != nil || u.Host != r.Host {
					writeErr(w, http.StatusForbidden, "cross-origin request")
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- helpers ----------

func writeJSON(w http.ResponseWriter, code int, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		http.Error(w, "encode response", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	// Large lists (the dashboard with thousands of monitors is megabytes of
	// JSON) are compressed when the client accepts it.
	if len(b) > 8<<10 && w.Header().Get("Content-Encoding") == "" && acceptsGzip(w) {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Add("Vary", "Accept-Encoding")
		w.WriteHeader(code)
		gz, _ := gzip.NewWriterLevel(w, gzip.BestSpeed)
		_, _ = gz.Write(b)
		_, _ = gz.Write([]byte{'\n'})
		_ = gz.Close()
		return
	}
	w.WriteHeader(code)
	_, _ = w.Write(append(b, '\n'))
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func readJSON(r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20))
	return dec.Decode(v)
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil && id > 0
}

func (h *Hub) internalErr(w http.ResponseWriter, err error) {
	h.log.Error("api error", "err", err)
	writeErr(w, 500, "internal error")
}

// ---------- auth ----------

func (h *Hub) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	u, err := h.store.SessionUser(HashToken(c.Value))
	if err != nil {
		return nil
	}
	return u
}

func isHTTPS(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (h *Hub) startSession(w http.ResponseWriter, r *http.Request, u *store.User) error {
	token, hash := NewToken("s_")
	exp := time.Now().Add(30 * 24 * time.Hour)
	if err := h.store.CreateSession(hash, u.ID, exp); err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: token, Path: "/", Expires: exp,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: isHTTPS(r),
	})
	return nil
}

func (h *Hub) getSetup(w http.ResponseWriter, r *http.Request) {
	n, err := h.store.CountUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"needs_setup": n == 0, "version": h.cfg.Version})
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Hub) postSetup(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if readJSON(r, &c) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	c.Email = strings.ToLower(strings.TrimSpace(c.Email))
	if !strings.Contains(c.Email, "@") || len(c.Password) < 8 {
		writeErr(w, 400, "valid email and a password of at least 8 characters are required")
		return
	}
	n, err := h.store.CountUsers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if n > 0 {
		writeErr(w, 409, "already configured")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(c.Password), bcrypt.DefaultCost)
	u, err := h.store.CreateUser(c.Email, string(hash), store.RoleAdmin)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 201, u)
}

func (h *Hub) login(w http.ResponseWriter, r *http.Request) {
	var c credentials
	if readJSON(r, &c) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if a := h.authSettings(); !a.passwordLoginAllowed() {
		writeErr(w, 403, "password login is disabled, use single sign-on")
		return
	}
	u, err := h.store.UserByEmail(strings.ToLower(strings.TrimSpace(c.Email)))
	if err != nil || u.PasswordHash == "" || bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(c.Password)) != nil {
		time.Sleep(400 * time.Millisecond)
		writeErr(w, 401, "invalid email or password")
		return
	}
	if h.store.TwoFactorEnabled(u.ID) {
		// The password is right: ask for the authenticator code before starting a session.
		writeJSON(w, 200, map[string]any{"two_factor": true, "challenge": h.challenges.put(u.ID)})
		return
	}
	if err := h.startSession(w, r, u); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, u)
}

func (h *Hub) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = h.store.DeleteSession(HashToken(c.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) me(w http.ResponseWriter, r *http.Request, u *store.User) {
	writeJSON(w, 200, map[string]any{"user": h.userView(u), "version": h.cfg.Version, "recovery_codes_left": h.store.RecoveryCodesLeft(u.ID)})
}

func (h *Hub) changePassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var b struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if readJSON(r, &b) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	// Accounts created through single sign-on have no password yet: let them set one.
	if u.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(b.Current)) != nil {
		writeErr(w, 400, "current password is wrong")
		return
	}
	if len(b.New) < 8 {
		writeErr(w, 400, "password must be at least 8 characters")
		return
	}
	hash, _ := bcrypt.GenerateFromPassword([]byte(b.New), bcrypt.DefaultCost)
	if err := h.store.UpdatePassword(u.ID, string(hash)); err != nil {
		h.internalErr(w, err)
		return
	}
	h.logoutOtherSessions(r, u)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- monitors ----------

var validTypes = []string{
	check.TypeHTTP, check.TypeKeyword, check.TypeTCP, check.TypePing, check.TypeDNS, check.TypePush,
	check.TypePostgres, check.TypeMySQL, check.TypeRedis, check.TypeMongo, check.TypeDocker, check.TypeGRPC, check.TypeSMTP, check.TypeMCP,
}

// urlSchemes lists the accepted target URL schemes of URL-based types.
var urlSchemes = map[string][]string{
	check.TypePostgres: {"postgres", "postgresql"},
	check.TypeMySQL:    {"mysql"},
	check.TypeRedis:    {"redis", "rediss"},
}

var containerRe = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]*$`)
var hostPortRe = regexp.MustCompile(`^[^\s/]+:\d{1,5}$`)

func (h *Hub) validateMonitor(m *store.Monitor) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Target = strings.TrimSpace(m.Target)
	if m.Name == "" {
		return errors.New("name is required")
	}
	if !slices.Contains(validTypes, m.Type) {
		return errors.New("invalid type")
	}
	if m.GroupID < 0 {
		m.GroupID = 0
	}
	m.TagIDs = cleanIDs(m.TagIDs)
	var groups []int64
	if m.GroupID > 0 {
		groups = []int64{m.GroupID}
	}
	if err := h.checkGroupsTags(groups, m.TagIDs); err != nil {
		return err
	}
	if m.Type != check.TypeHTTP && m.Type != check.TypeKeyword {
		m.Assertions = []check.Assertion{}
	}
	if m.Assertions == nil {
		m.Assertions = []check.Assertion{}
	}
	if len(m.Assertions) > check.MaxAssertions {
		return fmt.Errorf("at most %d response rules", check.MaxAssertions)
	}
	for i := range m.Assertions {
		if err := check.ValidateAssertion(&m.Assertions[i]); err != nil {
			return fmt.Errorf("response rule %d: %w", i+1, err)
		}
	}
	if m.Type == check.TypePush {
		return validatePush(m)
	}
	if m.Target == "" {
		return errors.New("target is required")
	}
	switch m.Type {
	case check.TypeHTTP, check.TypeKeyword:
		u, err := url.Parse(m.Target)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errors.New("target must be an http(s) URL")
		}
		if m.Type == check.TypeKeyword && m.Keyword == "" {
			return errors.New("keyword is required")
		}
		if m.Method == "" {
			m.Method = "GET"
		}
		m.Method = strings.ToUpper(m.Method)
	case check.TypeTCP:
		if !hostPortRe.MatchString(m.Target) {
			return errors.New("target must be host:port")
		}
	case check.TypePing, check.TypeDNS:
		if strings.ContainsAny(m.Target, " /") {
			return errors.New("target must be a hostname or IP")
		}
	case check.TypeMongo:
		if _, err := check.ParseMongoURL(m.Target); err != nil {
			return err
		}
	case check.TypePostgres, check.TypeMySQL, check.TypeRedis:
		u, err := url.Parse(m.Target)
		if err != nil || !slices.Contains(urlSchemes[m.Type], u.Scheme) || u.Host == "" {
			return fmt.Errorf("target must be a %s:// URL", urlSchemes[m.Type][0])
		}
	case check.TypeDocker:
		m.Target = strings.TrimPrefix(m.Target, "/")
		if !containerRe.MatchString(m.Target) {
			return errors.New("target must be a container name or id")
		}
	case check.TypeGRPC:
		t := m.Target
		if !strings.Contains(t, "://") {
			t = "grpc://" + t
		}
		u, err := url.Parse(t)
		if err != nil || (u.Scheme != "grpc" && u.Scheme != "grpcs") || u.Host == "" || u.Port() == "" {
			return errors.New("target must be grpc://host:port or grpcs://host:port, optionally followed by /service")
		}
	case check.TypeMCP:
		if err := validateMCP(m); err != nil {
			return err
		}
	case check.TypeSMTP:
		t := m.Target
		if !strings.Contains(t, "://") {
			t = "smtp://" + t
		}
		u, err := url.Parse(t)
		if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Hostname() == "" || strings.Trim(u.Path, "/") != "" {
			return errors.New("target must be smtp://host[:port] or smtps://host[:port]")
		}
	}

	if m.IntervalSec == 0 {
		m.IntervalSec = 60
	}
	if m.IntervalSec < 10 || m.IntervalSec > 86400 {
		return errors.New("interval must be between 10s and 24h")
	}
	if m.TimeoutMs == 0 {
		m.TimeoutMs = 10000
	}
	m.TimeoutMs = min(max(m.TimeoutMs, 500), 60000)
	m.Retries = min(max(m.Retries, 1), 10)
	if m.DegradedPct < 0 || m.DegradedPct > maxDegradedPct {
		return fmt.Errorf("degraded threshold must be between 1%% and %d%% (0 = instance default)", maxDegradedPct)
	}
	if m.Headers == nil {
		m.Headers = map[string]string{}
	}
	locs := []string{}
	for _, l := range m.Locations {
		if l == LocalLocation {
			locs = append(locs, l)
			continue
		}
		if id, ok := parseAgentLocation(l); ok {
			if _, err := h.store.Agent(id); err == nil {
				locs = append(locs, l)
			}
		}
	}
	slices.Sort(locs)
	m.Locations = slices.Compact(locs)
	if len(m.Locations) == 0 {
		m.Locations = []string{LocalLocation}
	}
	return nil
}

// validateMCP normalizes an MCP monitor: the method field holds the
// transport, keyword the required tools and expected_status the minimum
// number of tools.
func validateMCP(m *store.Monitor) error {
	u, err := url.Parse(m.Target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("target must be the http(s) URL of the MCP endpoint")
	}
	switch strings.ToLower(strings.TrimSpace(m.Method)) {
	case "", "get", check.MCPAuto:
		m.Method = check.MCPAuto
	case check.MCPStreamable:
		m.Method = check.MCPStreamable
	case check.MCPSSE:
		m.Method = check.MCPSSE
	default:
		return errors.New("transport must be auto, streamable or sse")
	}
	m.Keyword = strings.Join(check.MCPToolNames(m.Keyword), ", ")
	m.ExpectedStatus = strings.TrimSpace(m.ExpectedStatus)
	if m.ExpectedStatus != "" {
		n, err := strconv.Atoi(m.ExpectedStatus)
		if err != nil || n < 0 || n > 10000 {
			return errors.New("minimum tools must be a number between 0 and 10000")
		}
		if n == 0 {
			m.ExpectedStatus = ""
		}
	}
	return nil
}

// validatePush normalizes a heartbeat monitor: it has no target and no
// probe settings, only a period and a grace time.
func validatePush(m *store.Monitor) error {
	if m.IntervalSec == 0 {
		m.IntervalSec = 3600
	}
	if m.IntervalSec < 10 || m.IntervalSec > maxPushIntervalSec {
		return errors.New("period must be between 10s and 7 days")
	}
	if m.GraceSec < 0 || m.GraceSec > maxPushGraceSec {
		return errors.New("grace period must be between 0 and 24h")
	}
	*m = store.Monitor{
		ID: m.ID, Name: m.Name, Type: m.Type, IntervalSec: m.IntervalSec, GraceSec: m.GraceSec,
		TimeoutMs: 10000, Retries: 1, Method: "GET", Headers: map[string]string{},
		Locations: []string{PushLocation}, Paused: m.Paused, Assertions: []check.Assertion{},
		GroupID: m.GroupID, TagIDs: m.TagIDs,
	}
	return nil
}

type monitorSummary struct {
	*store.Monitor
	Uptime24h float64              `json:"uptime_24h"`
	AvgMs24h  float64              `json:"avg_ms_24h"`
	Last      *store.ResultRow     `json:"last"`
	Bars      []bar                `json:"bars"`
	Expiry    []*store.ExpiryCheck `json:"expiry"`
	// Maintenance is the window covering the monitor right now, if any.
	Maintenance *activeMaint `json:"maintenance"`
}

type activeMaint struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	EndAt int64  `json:"end_at"`
}

type bar struct {
	T     int64   `json:"t"`
	Total int     `json:"total"`
	OK    int     `json:"ok"`
	Maint int     `json:"maint"`
	AvgMs float64 `json:"avg_ms"`
}

const barCount = 48

func (h *Hub) summary(m *store.Monitor, expiry []*store.ExpiryCheck) *monitorSummary {
	s := &monitorSummary{Monitor: m, Expiry: expiry}
	if s.Expiry == nil {
		s.Expiry = []*store.ExpiryCheck{}
	}
	if w, end := h.ActiveMaintenance(m); w != nil && !m.Paused {
		s.Maintenance = &activeMaint{ID: w.ID, Name: w.Name, EndAt: end.UnixMilli()}
	}
	// Bars, uptime and the last result come from the in-memory stats kept by
	// HandleResult: no query on the results table.
	h.mu.Lock()
	st := h.monitors[m.ID]
	if st != nil && st.stats != nil {
		s.Bars, s.Uptime24h, s.AvgMs24h = st.stats.bars(time.Now().UnixMilli())
		s.Last = st.stats.last
	} else {
		s.Bars, s.Uptime24h, s.AvgMs24h = (&monStats{}).bars(time.Now().UnixMilli())
	}
	h.mu.Unlock()
	return s
}

func (h *Hub) listMonitors(w http.ResponseWriter, r *http.Request, u *store.User) {
	f, err := parseMonitorFilter(r)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	ms, err := h.store.ListMonitors()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	checks, err := h.store.ExpiryChecks(0)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	expiry := map[int64][]*store.ExpiryCheck{}
	for _, c := range checks {
		expiry[c.MonitorID] = append(expiry[c.MonitorID], c)
	}
	out := make([]*monitorSummary, 0, len(ms))
	for _, m := range ms {
		if f.active && !f.match(m) {
			continue
		}
		if !u.IsAdmin() {
			redactMonitor(m)
		}
		out = append(out, h.summary(m, expiry[m.ID]))
	}
	writeJSON(w, 200, out)
}

func (h *Hub) overview(w http.ResponseWriter, r *http.Request, _ *store.User) {
	ms, err := h.store.ListMonitors()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	counts := map[string]int{}
	for _, m := range ms {
		counts[m.Status]++
	}
	agents, _ := h.store.ListAgents()
	online := 0
	for _, a := range agents {
		if h.AgentOnline(a.ID) {
			online++
		}
	}
	open, _ := h.store.ListIncidents(0, 200)
	active := 0
	for _, i := range open {
		if i.ResolvedAt == 0 {
			active++
		}
	}
	writeJSON(w, 200, map[string]any{
		"monitors": len(ms), "status": counts,
		"agents": len(agents), "agents_online": online,
		"open_incidents": active, "version": h.cfg.Version,
	})
}

func (h *Hub) createMonitor(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var m store.Monitor
	if readJSON(r, &m) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := h.validateMonitor(&m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	paused := m.Paused
	m.Paused = false
	m.PushToken = ""
	if m.Type == check.TypePush {
		m.PushToken = newPushToken()
	}
	if err := h.store.CreateMonitor(&m); err != nil {
		h.internalErr(w, err)
		return
	}
	if paused {
		_ = h.store.SetMonitorPaused(m.ID, true)
	}
	h.MonitorChanged(m.ID)
	h.expirySoon(m.ID)
	got, _ := h.store.Monitor(m.ID)
	writeJSON(w, 201, got)
}

type monitorDetail struct {
	*monitorSummary
	Uptime7d  float64          `json:"uptime_7d"`
	Uptime30d float64          `json:"uptime_30d"`
	Locations []locationStatus `json:"location_status"`
	PushURL   string           `json:"push_url,omitempty"`
}

type locationStatus struct {
	ID     string           `json:"id"`
	Name   string           `json:"name"`
	Online bool             `json:"online"`
	Last   *store.ResultRow `json:"last"`
}

func (h *Hub) getMonitor(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	m, err := h.store.Monitor(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "monitor not found")
		return
	} else if err != nil {
		h.internalErr(w, err)
		return
	}
	expiry, err := h.store.ExpiryChecks(id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	if !u.IsAdmin() {
		redactMonitor(m)
	}
	s := h.summary(m, expiry)
	now := time.Now().UnixMilli()
	day := int64(24 * 3600 * 1000)
	d := &monitorDetail{monitorSummary: s, PushURL: h.pushURL(r, m)}
	d.Uptime7d, _, _ = h.store.Uptime(id, now-7*day)
	d.Uptime30d, _, _ = h.store.Uptime(id, now-30*day)
	latest, _ := h.store.LatestPerLocation(id)
	byLoc := map[string]*store.ResultRow{}
	for _, r := range latest {
		byLoc[r.Location] = r
	}
	names := h.locationNames()
	for _, l := range h.locationsOf(m) {
		d.Locations = append(d.Locations, locationStatus{ID: l, Name: nameOr(names, l), Online: h.locationOnline(l), Last: byLoc[l]})
	}
	writeJSON(w, 200, d)
}

func nameOr(names map[string]string, id string) string {
	if n, ok := names[id]; ok {
		return n
	}
	return id
}

func (h *Hub) locationOnline(loc string) bool {
	if loc == LocalLocation {
		return h.localEnabled()
	}
	if loc == PushLocation {
		return true
	}
	if id, ok := parseAgentLocation(loc); ok {
		return h.AgentOnline(id)
	}
	return false
}

func (h *Hub) locationNames() map[string]string {
	names := map[string]string{LocalLocation: h.localName(), PushLocation: pushLocationName}
	agents, _ := h.store.ListAgents()
	for _, a := range agents {
		names[AgentLocation(a.ID)] = agentLabel(a)
	}
	return names
}

func (h *Hub) updateMonitor(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	prev, err := h.store.Monitor(id)
	if err != nil {
		writeErr(w, 404, "monitor not found")
		return
	}
	var m store.Monitor
	if readJSON(r, &m) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	m.ID = id
	if err := h.validateMonitor(&m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.UpdateMonitor(&m); err != nil {
		h.internalErr(w, err)
		return
	}
	// Keep the push URL across edits; create one when switching to push.
	switch {
	case m.Type == check.TypePush && prev.PushToken == "":
		err = h.store.SetPushToken(id, newPushToken())
	case m.Type != check.TypePush && prev.PushToken != "":
		err = h.store.SetPushToken(id, "")
	}
	if err != nil {
		h.internalErr(w, err)
		return
	}
	h.MonitorChanged(id)
	h.expirySoon(id)
	got, _ := h.store.Monitor(id)
	writeJSON(w, 200, got)
}

// expirySoon looks up the expiry dates of a new or edited monitor in the
// background, so they show up without waiting for the next pass.
func (h *Hub) expirySoon(id int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		h.expiry.run.Lock()
		defer h.expiry.run.Unlock()
		h.checkExpiries(ctx, id, false)
	}()
}

// checkMonitorExpiry looks up the certificate and domain expiry now.
func (h *Hub) checkMonitorExpiry(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if _, err := h.store.Monitor(id); err != nil {
		writeErr(w, 404, "monitor not found")
		return
	}
	h.CheckExpiryNow(r.Context(), id)
	checks, err := h.store.ExpiryChecks(id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, checks)
}

func (h *Hub) deleteMonitor(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteMonitor(id); err != nil {
		h.internalErr(w, err)
		return
	}
	h.MonitorChanged(id)
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) pauseMonitor(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var b struct {
		Paused bool `json:"paused"`
	}
	if readJSON(r, &b) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := h.store.SetMonitorPaused(id, b.Paused); err != nil {
		h.internalErr(w, err)
		return
	}
	if b.Paused {
		_ = h.store.ResolveIncident(id, time.Now().UnixMilli())
	}
	h.MonitorChanged(id)
	got, err := h.store.Monitor(id)
	if err != nil {
		writeErr(w, 404, "monitor not found")
		return
	}
	writeJSON(w, 200, got)
}

var ranges = map[string]time.Duration{
	"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour,
	"7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour,
}

func (h *Hub) monitorSeries(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	rng, ok := ranges[r.URL.Query().Get("range")]
	if !ok {
		rng = 24 * time.Hour
	}
	now := time.Now().UnixMilli()
	size := rng.Milliseconds() / 90
	from := now - rng.Milliseconds()
	from -= from % size
	buckets, err := h.store.Buckets(id, from, now+1, size, true)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	names := h.locationNames()
	seen := map[string]bool{}
	locs := []map[string]string{}
	for _, b := range buckets {
		if !seen[b.Location] {
			seen[b.Location] = true
			locs = append(locs, map[string]string{"id": b.Location, "name": nameOr(names, b.Location)})
		}
	}
	slices.SortFunc(locs, func(a, b map[string]string) int { return strings.Compare(a["name"], b["name"]) })
	writeJSON(w, 200, map[string]any{"from": from, "to": now, "size": size, "locations": locs, "buckets": buckets})
}

func (h *Hub) monitorResults(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := h.store.RecentResults(id, limit)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"results": rows, "locations": h.locationNames()})
}

func (h *Hub) listIncidents(w http.ResponseWriter, r *http.Request, _ *store.User) {
	mid, _ := strconv.ParseInt(r.URL.Query().Get("monitor"), 10, 64)
	list, err := h.store.ListIncidents(mid, 100)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, list)
}

// ---------- agents ----------

type agentView struct {
	*store.Agent
	LocationID string `json:"location_id"`
	Online     bool   `json:"online"`
	updateView
}

func (h *Hub) listLocations(w http.ResponseWriter, r *http.Request, _ *store.User) {
	out := []map[string]any{}
	if h.localEnabled() {
		out = append(out, map[string]any{"id": LocalLocation, "name": h.localName(), "online": true, "kind": "local"})
	}
	agents, err := h.store.ListAgents()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	for _, a := range agents {
		out = append(out, map[string]any{"id": AgentLocation(a.ID), "name": agentLabel(a), "online": h.AgentOnline(a.ID), "kind": "agent"})
	}
	writeJSON(w, 200, out)
}

// hubURL is the public URL of the hub: the configured one, or guessed from the request.
func (h *Hub) hubURL(r *http.Request) string {
	u := h.generalSettings().PublicURL
	if u == "" {
		return requestURL(r)
	}
	// A URL seeded without a scheme (e.g. PINGTXT_PUBLIC_URL=status.example.com):
	// take the scheme of the current request until it is fixed in settings.
	if !strings.Contains(u, "://") {
		scheme, _, _ := strings.Cut(requestURL(r), "://")
		return scheme + "://" + u
	}
	return u
}

func requestURL(r *http.Request) string {
	scheme := "http"
	if isHTTPS(r) {
		scheme = "https"
	}
	host := r.Host
	if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
		host = fh
	}
	return scheme + "://" + host
}

func (h *Hub) listAgents(w http.ResponseWriter, r *http.Request, _ *store.User) {
	agents, err := h.store.ListAgents()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	out := make([]agentView, 0, len(agents))
	for _, a := range agents {
		out = append(out, agentView{Agent: a, LocationID: AgentLocation(a.ID), Online: h.AgentOnline(a.ID), updateView: h.agentUpdateView(a)})
	}
	writeJSON(w, 200, map[string]any{
		"agents": out, "hub_url": h.hubURL(r),
		"hub_version": h.cfg.Version, "auto_update": h.agentUpdateSettings().AutoUpdate,
	})
}

type agentInput struct {
	Name     string `json:"name"`
	Location string `json:"location"`
}

func (h *Hub) createAgent(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in agentInput
	if readJSON(r, &in) != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, 400, "name is required")
		return
	}
	token, hash := NewToken("pt_")
	a, err := h.store.CreateAgent(strings.TrimSpace(in.Name), strings.TrimSpace(in.Location), hash)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"agent": agentView{Agent: a, LocationID: AgentLocation(a.ID)}, "token": token, "hub_url": h.hubURL(r)})
}

func (h *Hub) updateAgent(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var in agentInput
	if readJSON(r, &in) != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, 400, "name is required")
		return
	}
	if err := h.store.UpdateAgent(id, strings.TrimSpace(in.Name), strings.TrimSpace(in.Location)); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) deleteAgent(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteAgent(id); err != nil {
		h.internalErr(w, err)
		return
	}
	h.DisconnectAgent(id)
	// Remove the location from monitors that used it.
	loc := AgentLocation(id)
	ms, _ := h.store.ListMonitors()
	for _, m := range ms {
		if i := slices.Index(m.Locations, loc); i >= 0 {
			m.Locations = slices.Delete(m.Locations, i, i+1)
			if len(m.Locations) == 0 {
				m.Locations = []string{LocalLocation}
			}
			_ = h.store.UpdateMonitor(m)
			h.MonitorChanged(m.ID)
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) rotateAgentToken(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if _, err := h.store.Agent(id); err != nil {
		writeErr(w, 404, "agent not found")
		return
	}
	token, hash := NewToken("pt_")
	if err := h.store.SetAgentToken(id, hash); err != nil {
		h.internalErr(w, err)
		return
	}
	h.DisconnectAgent(id)
	writeJSON(w, 200, map[string]any{"token": token, "hub_url": h.hubURL(r)})
}

// ---------- notifiers ----------

func validateNotifier(n *store.Notifier) error {
	n.Name = strings.TrimSpace(n.Name)
	if n.Name == "" {
		return errors.New("name is required")
	}
	if !slices.Contains(NotifierTypes, n.Type) {
		return errors.New("invalid type")
	}
	if n.Config == nil {
		n.Config = map[string]string{}
	}
	if n.Type == "telegram" {
		if n.Config["bot_token"] == "" || n.Config["chat_id"] == "" {
			return errors.New("bot token and chat id are required")
		}
		return nil
	}
	if n.Type == "email" {
		delete(n.Config, "password_set")
		_, err := parseEmailConfig(n.Config)
		return err
	}
	u, err := url.Parse(n.Config["url"])
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("a valid http(s) url is required")
	}
	return nil
}

func (h *Hub) listNotifiers(w http.ResponseWriter, r *http.Request, u *store.User) {
	list, err := h.store.ListNotifiers()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	for _, n := range list {
		maskNotifier(n)
		if !u.IsAdmin() {
			redactNotifier(n)
		}
	}
	writeJSON(w, 200, list)
}

// writeOnlyKeys are config values the API accepts but never returns.
// An empty value on update keeps the stored one.
var writeOnlyKeys = map[string][]string{"email": {"password"}}

// maskNotifier removes write-only secrets, replacing each with <key>_set.
func maskNotifier(n *store.Notifier) {
	for _, k := range writeOnlyKeys[n.Type] {
		if n.Config[k] != "" {
			n.Config[k+"_set"] = "true"
		}
		delete(n.Config, k)
	}
}

// keepSecrets fills empty write-only values of n from the stored channel.
func (h *Hub) keepSecrets(n *store.Notifier) {
	if n.ID == 0 || len(writeOnlyKeys[n.Type]) == 0 {
		return
	}
	cur, err := h.store.Notifier(n.ID)
	if err != nil || cur.Type != n.Type {
		return
	}
	for _, k := range writeOnlyKeys[n.Type] {
		if n.Config[k] == "" {
			n.Config[k] = cur.Config[k]
		}
	}
}

func (h *Hub) createNotifier(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var n store.Notifier
	if readJSON(r, &n) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := validateNotifier(&n); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.CreateNotifier(&n); err != nil {
		h.internalErr(w, err)
		return
	}
	maskNotifier(&n)
	writeJSON(w, 201, n)
}

func (h *Hub) updateNotifier(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var n store.Notifier
	if readJSON(r, &n) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	n.ID = id
	if err := validateNotifier(&n); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	h.keepSecrets(&n)
	if err := h.store.UpdateNotifier(&n); err != nil {
		h.internalErr(w, err)
		return
	}
	maskNotifier(&n)
	writeJSON(w, 200, n)
}

func (h *Hub) deleteNotifier(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteNotifier(id); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func (h *Hub) testNotifier(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	n, err := h.store.Notifier(id)
	if err != nil {
		writeErr(w, 404, "notifier not found")
		return
	}
	h.sendTest(w, r, n)
}

// testDraftNotifier sends a test with an unsaved configuration, so the
// form can be checked before saving. With an id, empty secrets are taken
// from that stored channel.
func (h *Hub) testDraftNotifier(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var n store.Notifier
	if readJSON(r, &n) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if n.Name == "" {
		n.Name = "test"
	}
	if err := validateNotifier(&n); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	h.keepSecrets(&n)
	h.sendTest(w, r, &n)
}

func (h *Hub) sendTest(w http.ResponseWriter, r *http.Request, n *store.Notifier) {
	ev := Event{Monitor: "example.com", Target: "https://example.com", Status: StatusDown, Cause: "this is a test notification", At: time.Now().UnixMilli(), Test: true}
	if err := h.notify.Deliver(r.Context(), n, ev); err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// ---------- static ----------

var agentFileRe = regexp.MustCompile(`^pingtxt-agent-(linux|darwin|windows)-(amd64|arm64|arm)(\.exe)?(\.sha256)?$`)

func (h *Hub) download(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !agentFileRe.MatchString(name) || h.cfg.AgentDir == "" {
		http.NotFound(w, r)
		return
	}
	if bin, ok := strings.CutSuffix(name, ".sha256"); ok {
		// Checksum file in the sha256sum format, used by the install script.
		sum, err := h.binarySHA256(bin)
		if err != nil {
			http.Error(w, "agent binary not bundled in this build", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(sum + "  " + bin + "\n"))
		return
	}
	p := filepath.Join(h.cfg.AgentDir, name)
	if _, err := os.Stat(p); err != nil {
		http.Error(w, "agent binary not bundled in this build", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
	http.ServeFile(w, r, p)
}

// spaHandler serves the Next.js static export.
func spaHandler(ui fs.FS) http.Handler {
	fileServer := http.FileServerFS(ui)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
		if p == "" {
			p = "index.html"
		}
		try := []string{p, p + "/index.html", p + ".html"}
		for _, t := range try {
			if st, err := fs.Stat(ui, t); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "_next/static/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				} else if strings.HasSuffix(t, ".html") {
					w.Header().Set("Cache-Control", "no-cache")
				}
				r2 := r.Clone(r.Context())
				r2.URL.Path = "/" + t
				if strings.HasSuffix(t, "index.html") {
					// FileServer redirects */index.html; serve the content directly.
					serveFile(w, r2, ui, t)
					return
				}
				fileServer.ServeHTTP(w, r2)
				return
			}
		}
		// Public status pages: /status/<slug>/ is rendered by status/index.html.
		if strings.HasPrefix(p, "status/") {
			if _, err := fs.Stat(ui, "status/index.html"); err == nil {
				w.Header().Set("Cache-Control", "no-cache")
				serveFile(w, r, ui, "status/index.html")
				return
			}
		}
		if b, err := fs.ReadFile(ui, "404.html"); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusNotFound)
			w.Write(b)
			return
		}
		if _, err := fs.Stat(ui, "index.html"); err != nil {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Write([]byte("pingtxt: web UI not built. Run `make web` or use the Docker image.\n"))
			return
		}
		http.NotFound(w, r)
	})
}

func serveFile(w http.ResponseWriter, r *http.Request, ui fs.FS, name string) {
	f, err := ui.Open(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, _ := f.Stat()
	rs, ok := f.(interface {
		Read([]byte) (int, error)
		Seek(int64, int) (int64, error)
	})
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	http.ServeContent(w, r, name, st.ModTime(), rs)
}

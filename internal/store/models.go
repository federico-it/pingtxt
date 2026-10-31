package store

import (
	"cmp"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
)

func now() int64 { return time.Now().UnixMilli() }

// ---------- users & sessions ----------

type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // RoleAdmin or RoleViewer
	CreatedAt    int64  `json:"created_at"`
}

// User roles: admins change everything, viewers only read.
const (
	RoleAdmin  = "admin"
	RoleViewer = "viewer"
)

// ValidRole reports whether r is a known role.
func ValidRole(r string) bool { return r == RoleAdmin || r == RoleViewer }

// IsAdmin reports whether the user can change monitors, settings and users.
func (u *User) IsAdmin() bool { return u != nil && u.Role == RoleAdmin }

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(email, hash, role string) (*User, error) {
	if !ValidRole(role) {
		return nil, errors.New("invalid role")
	}
	u := &User{Email: email, PasswordHash: hash, Role: role, CreatedAt: now()}
	id, err := s.db.insert(s.db, `INSERT INTO users (email, password_hash, role, created_at) VALUES (?, ?, ?, ?)`, u.Email, u.PasswordHash, u.Role, u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.ID = id
	return u, nil
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByEmail(email string) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT id, email, password_hash, role, created_at FROM users WHERE email = ?`, email))
}

func (s *Store) UserByID(id int64) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT id, email, password_hash, role, created_at FROM users WHERE id = ?`, id))
}

// SetUserRole changes the role of a user.
func (s *Store) SetUserRole(id int64, role string) error {
	if !ValidRole(role) {
		return errors.New("invalid role")
	}
	res, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdatePassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func (s *Store) CreateSession(tokenHash string, userID int64, expires time.Time) error {
	_, err := s.db.Exec(`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES (?, ?, ?)`, tokenHash, userID, expires.UnixMilli())
	return err
}

func (s *Store) SessionUser(tokenHash string) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT u.id, u.email, u.password_hash, u.role, u.created_at FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now()))
}

func (s *Store) DeleteSession(tokenHash string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *Store) DeleteUserSessions(userID int64, except string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ? AND token_hash != ?`, userID, except)
	return err
}

// ---------- agents ----------

type Agent struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	Location   string `json:"location"`
	TokenHash  string `json:"-"`
	Version    string `json:"version"`
	Hostname   string `json:"hostname"`
	Platform   string `json:"platform"`
	RemoteAddr string `json:"remote_addr"`
	LastSeen   int64  `json:"last_seen"`
	CreatedAt  int64  `json:"created_at"`
}

const agentCols = `id, name, location, token_hash, version, hostname, platform, remote_addr, last_seen, created_at`

func scanAgent(sc interface{ Scan(...any) error }) (*Agent, error) {
	a := &Agent{}
	err := sc.Scan(&a.ID, &a.Name, &a.Location, &a.TokenHash, &a.Version, &a.Hostname, &a.Platform, &a.RemoteAddr, &a.LastSeen, &a.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return a, err
}

func (s *Store) ListAgents() ([]*Agent, error) {
	rows, err := s.db.Query(`SELECT ` + agentCols + ` FROM agents ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Agent{}
	for rows.Next() {
		a, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) Agent(id int64) (*Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE id = ?`, id))
}

func (s *Store) AgentByToken(hash string) (*Agent, error) {
	return scanAgent(s.db.QueryRow(`SELECT `+agentCols+` FROM agents WHERE token_hash = ?`, hash))
}

func (s *Store) CreateAgent(name, location, tokenHash string) (*Agent, error) {
	a := &Agent{Name: name, Location: location, TokenHash: tokenHash, CreatedAt: now()}
	id, err := s.db.insert(s.db, `INSERT INTO agents (name, location, token_hash, created_at) VALUES (?, ?, ?, ?)`, a.Name, a.Location, a.TokenHash, a.CreatedAt)
	if err != nil {
		return nil, err
	}
	a.ID = id
	return a, nil
}

func (s *Store) UpdateAgent(id int64, name, location string) error {
	_, err := s.db.Exec(`UPDATE agents SET name = ?, location = ? WHERE id = ?`, name, location, id)
	return err
}

func (s *Store) SetAgentToken(id int64, tokenHash string) error {
	_, err := s.db.Exec(`UPDATE agents SET token_hash = ? WHERE id = ?`, tokenHash, id)
	return err
}

func (s *Store) TouchAgent(id int64, version, hostname, platform, remote string) error {
	_, err := s.db.Exec(`UPDATE agents SET version = ?, hostname = ?, platform = ?, remote_addr = ?, last_seen = ? WHERE id = ?`, version, hostname, platform, remote, now(), id)
	return err
}

func (s *Store) AgentSeen(id int64) error {
	_, err := s.db.Exec(`UPDATE agents SET last_seen = ? WHERE id = ?`, now(), id)
	return err
}

func (s *Store) DeleteAgent(id int64) error {
	_, err := s.db.Exec(`DELETE FROM agents WHERE id = ?`, id)
	return err
}

// ---------- monitors ----------

type Monitor struct {
	ID             int64             `json:"id"`
	Name           string            `json:"name"`
	Type           string            `json:"type"`
	Target         string            `json:"target"`
	IntervalSec    int               `json:"interval_sec"`
	TimeoutMs      int               `json:"timeout_ms"`
	Retries        int               `json:"retries"`
	Method         string            `json:"method"`
	ExpectedStatus string            `json:"expected_status"`
	Keyword        string            `json:"keyword"`
	KeywordInvert  bool              `json:"keyword_invert"`
	DNSRecord      string            `json:"dns_record"`
	DNSServer      string            `json:"dns_server"`
	DNSExpected    string            `json:"dns_expected"`
	IgnoreTLS      bool              `json:"ignore_tls"`
	Headers        map[string]string `json:"headers"`
	Body           string            `json:"body"`
	// Assertions are rules on the JSON body of http/keyword responses.
	Assertions []check.Assertion `json:"assertions"`
	PushToken  string            `json:"push_token"`
	GraceSec   int               `json:"grace_sec"`
	// DegradedPct is the share of failing locations (percent) from which the
	// monitor is degraded. 0 uses the instance default.
	DegradedPct int `json:"degraded_pct"`
	// IPFamily forces the checks over IPv4 (4) or IPv6 (6); 0 uses either.
	IPFamily  int      `json:"ip_family"`
	Locations []string `json:"locations"`
	GroupID   int64    `json:"group_id"` // 0 = no group
	TagIDs    []int64  `json:"tag_ids"`
	// Expiry alerts: TLS certificate (https targets) and domain registration.
	CertExpiryAlert   bool   `json:"cert_expiry_alert"`
	DomainExpiryAlert bool   `json:"domain_expiry_alert"`
	Paused            bool   `json:"paused"`
	Status            string `json:"status"`
	StatusSince       int64  `json:"status_since"`
	CreatedAt         int64  `json:"created_at"`
}

const monitorCols = `id, name, type, target, interval_sec, timeout_ms, retries, method, expected_status, keyword, keyword_invert, dns_record, dns_server, dns_expected, ignore_tls, headers, body, push_token, grace_sec, locations, cert_expiry_alert, domain_expiry_alert, paused, status, status_since, created_at, group_id, degraded_pct, assertions, ip_family`

func scanMonitor(sc interface{ Scan(...any) error }) (*Monitor, error) {
	m := &Monitor{}
	var headers, locations, assertions string
	var group sql.NullInt64
	err := sc.Scan(&m.ID, &m.Name, &m.Type, &m.Target, &m.IntervalSec, &m.TimeoutMs, &m.Retries, &m.Method, &m.ExpectedStatus, &m.Keyword, &m.KeywordInvert, &m.DNSRecord, &m.DNSServer, &m.DNSExpected, &m.IgnoreTLS, &headers, &m.Body, &m.PushToken, &m.GraceSec, &locations, &m.CertExpiryAlert, &m.DomainExpiryAlert, &m.Paused, &m.Status, &m.StatusSince, &m.CreatedAt, &group, &m.DegradedPct, &assertions, &m.IPFamily)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	m.GroupID = group.Int64
	m.TagIDs = []int64{}
	_ = json.Unmarshal([]byte(headers), &m.Headers)
	_ = json.Unmarshal([]byte(locations), &m.Locations)
	_ = json.Unmarshal([]byte(assertions), &m.Assertions)
	if m.Assertions == nil {
		m.Assertions = []check.Assertion{}
	}
	if m.Headers == nil {
		m.Headers = map[string]string{}
	}
	if m.Locations == nil {
		m.Locations = []string{}
	}
	return m, nil
}

func (s *Store) ListMonitors() ([]*Monitor, error) {
	return s.listMonitorsWhere(``)
}

func (s *Store) listMonitorsWhere(where string, args ...any) ([]*Monitor, error) {
	rows, err := s.db.Query(`SELECT `+monitorCols+` FROM monitors `+where+` ORDER BY LOWER(name)`, args...)
	if err != nil {
		return nil, err
	}
	out := []*Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// Loaded after closing the cursor: the pool has a single connection.
	tags, err := s.monitorTags()
	if err != nil {
		return nil, err
	}
	for _, m := range out {
		if t := tags[m.ID]; t != nil {
			m.TagIDs = t
		}
	}
	return out, nil
}

// MonitorsInGroups returns the monitors of the given groups, by name.
func (s *Store) MonitorsInGroups(groupIDs []int64) ([]*Monitor, error) {
	if len(groupIDs) == 0 {
		return []*Monitor{}, nil
	}
	args := make([]any, len(groupIDs))
	for i, id := range groupIDs {
		args[i] = id
	}
	return s.listMonitorsWhere(`WHERE group_id IN (?`+strings.Repeat(`, ?`, len(groupIDs)-1)+`)`, args...)
}

func (s *Store) Monitor(id int64) (*Monitor, error) {
	m, err := scanMonitor(s.db.QueryRow(`SELECT `+monitorCols+` FROM monitors WHERE id = ?`, id))
	if err != nil {
		return nil, err
	}
	return m, s.loadMonitorTags(m)
}

func (s *Store) CreateMonitor(m *Monitor) error {
	h, _ := json.Marshal(m.Headers)
	l, _ := json.Marshal(m.Locations)
	m.CreatedAt = now()
	m.Status = "pending"
	m.StatusSince = m.CreatedAt
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	id, err := s.db.insert(tx, `INSERT INTO monitors (name, type, target, interval_sec, timeout_ms, retries, method, expected_status, keyword, keyword_invert, dns_record, dns_server, dns_expected, ignore_tls, headers, body, push_token, grace_sec, locations, cert_expiry_alert, domain_expiry_alert, paused, status, status_since, created_at, group_id, degraded_pct, assertions, ip_family)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Type, m.Target, m.IntervalSec, m.TimeoutMs, m.Retries, m.Method, m.ExpectedStatus, m.Keyword, b2i(m.KeywordInvert), m.DNSRecord, m.DNSServer, m.DNSExpected, b2i(m.IgnoreTLS), string(h), m.Body, m.PushToken, m.GraceSec, string(l), b2i(m.CertExpiryAlert), b2i(m.DomainExpiryAlert), b2i(m.Paused), m.Status, m.StatusSince, m.CreatedAt, nullID(m.GroupID), m.DegradedPct, assertionsJSON(m.Assertions), m.IPFamily)
	if err != nil {
		return err
	}
	m.ID = id
	if err := setMonitorTags(tx, m.ID, m.TagIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) UpdateMonitor(m *Monitor) error {
	h, _ := json.Marshal(m.Headers)
	l, _ := json.Marshal(m.Locations)
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`UPDATE monitors SET name = ?, type = ?, target = ?, interval_sec = ?, timeout_ms = ?, retries = ?, method = ?, expected_status = ?, keyword = ?, keyword_invert = ?, dns_record = ?, dns_server = ?, dns_expected = ?, ignore_tls = ?, headers = ?, body = ?, grace_sec = ?, locations = ?, cert_expiry_alert = ?, domain_expiry_alert = ?, group_id = ?, degraded_pct = ?, assertions = ?, ip_family = ? WHERE id = ?`,
		m.Name, m.Type, m.Target, m.IntervalSec, m.TimeoutMs, m.Retries, m.Method, m.ExpectedStatus, m.Keyword, b2i(m.KeywordInvert), m.DNSRecord, m.DNSServer, m.DNSExpected, b2i(m.IgnoreTLS), string(h), m.Body, m.GraceSec, string(l), b2i(m.CertExpiryAlert), b2i(m.DomainExpiryAlert), nullID(m.GroupID), m.DegradedPct, assertionsJSON(m.Assertions), m.IPFamily, m.ID)
	if err != nil {
		return err
	}
	if err := setMonitorTags(tx, m.ID, m.TagIDs); err != nil {
		return err
	}
	return tx.Commit()
}

func assertionsJSON(a []check.Assertion) string {
	if len(a) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(a)
	return string(b)
}

// MonitorByPushToken returns the push monitor owning a heartbeat token.
func (s *Store) MonitorByPushToken(token string) (*Monitor, error) {
	if token == "" {
		return nil, ErrNotFound
	}
	return scanMonitor(s.db.QueryRow(`SELECT `+monitorCols+` FROM monitors WHERE push_token = ?`, token))
}

// SetPushToken sets the heartbeat token of a monitor ("" removes it).
func (s *Store) SetPushToken(id int64, token string) error {
	_, err := s.db.Exec(`UPDATE monitors SET push_token = ? WHERE id = ?`, token, id)
	return err
}

func (s *Store) SetMonitorPaused(id int64, paused bool) error {
	status := "pending"
	if paused {
		status = "paused"
	}
	_, err := s.db.Exec(`UPDATE monitors SET paused = ?, status = ?, status_since = ? WHERE id = ?`, b2i(paused), status, now(), id)
	return err
}

func (s *Store) SetMonitorStatus(id int64, status string, since int64) error {
	_, err := s.db.Exec(`UPDATE monitors SET status = ?, status_since = ? WHERE id = ?`, status, since, id)
	return err
}

func (s *Store) DeleteMonitor(id int64) error {
	_, err := s.db.Exec(`DELETE FROM monitors WHERE id = ?`, id)
	return err
}

// ---------- results ----------

type ResultRow struct {
	ID         int64   `json:"id"`
	MonitorID  int64   `json:"monitor_id"`
	Location   string  `json:"location"`
	TS         int64   `json:"ts"`
	OK         bool    `json:"ok"`
	LatencyMs  float64 `json:"latency_ms"`
	StatusCode int     `json:"status_code"`
	Message    string  `json:"message"`
	CertExpiry int64   `json:"cert_expiry"`
	Maint      bool    `json:"maint"` // taken during a maintenance window
	// MonState is the monitor status right after this result (MonUp,
	// MonDegraded or MonDown). Uptime counts the rows taken while the
	// monitor was not down, so one failing location among several does not
	// lower it. Written only, not read back by RecentResults.
	MonState int `json:"-"`
	// Timing splits the latency of HTTP checks into phases (nil: none).
	Timing *Phases `json:"timing,omitempty"`
}

// Phases is the time spent in each phase of an HTTP check, in ms. Results
// store them in tenths of a ms.
type Phases struct {
	DNSMs     float64 `json:"dns_ms"`
	ConnectMs float64 `json:"connect_ms"`
	TLSMs     float64 `json:"tls_ms"`
	TTFBMs    float64 `json:"ttfb_ms"`
	// TotalMs is the average latency of the checks the phases come from
	// (buckets only): what is left after the phases is reading the body.
	TotalMs float64 `json:"total_ms,omitempty"`
	// N is how many checks the averages come from (buckets only).
	N int64 `json:"n,omitempty"`
}

func tenths(v float64) int64 { return int64(math.Round(max(v, 0) * 10)) }

// phaseArgs returns the stored columns t_dns, t_conn, t_tls, t_ttfb. A
// result with phases always has t_ttfb > 0, which marks it.
func (r *ResultRow) phaseArgs() []any {
	p := r.Timing
	if p == nil {
		return []any{0, 0, 0, 0}
	}
	return []any{tenths(p.DNSMs), tenths(p.ConnectMs), tenths(p.TLSMs), max(1, tenths(p.TTFBMs))}
}

func phasesFrom(dns, conn, tls, ttfb int64) *Phases {
	if ttfb <= 0 {
		return nil
	}
	return &Phases{DNSMs: float64(dns) / 10, ConnectMs: float64(conn) / 10, TLSMs: float64(tls) / 10, TTFBMs: float64(ttfb) / 10}
}

const resultInsert = `INSERT INTO results (monitor_id, location, ts, ok, latency_ms, status_code, message, cert_expiry, maint, mon_state, t_dns, t_conn, t_tls, t_ttfb) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

func (r *ResultRow) insertArgs() []any {
	return append([]any{r.MonitorID, r.Location, r.TS, b2i(r.OK), r.LatencyMs, r.StatusCode, r.Message, r.CertExpiry, b2i(r.Maint), r.MonState}, r.phaseArgs()...)
}

const resultCols = `id, monitor_id, location, ts, ok, latency_ms, status_code, message, cert_expiry, maint, t_dns, t_conn, t_tls, t_ttfb`

func scanResult(rows *sql.Rows) (*ResultRow, error) {
	r := &ResultRow{}
	var dns, conn, tls, ttfb int64
	if err := rows.Scan(&r.ID, &r.MonitorID, &r.Location, &r.TS, &r.OK, &r.LatencyMs, &r.StatusCode, &r.Message, &r.CertExpiry, &r.Maint, &dns, &conn, &tls, &ttfb); err != nil {
		return nil, err
	}
	r.Timing = phasesFrom(dns, conn, tls, ttfb)
	return r, nil
}

// Monitor status kept with each result.
const (
	MonUp       = 0
	MonDegraded = 1
	MonDown     = 2
)

func (s *Store) InsertResult(r *ResultRow) error {
	id, err := s.db.insert(s.db, resultInsert, r.insertArgs()...)
	if err != nil {
		return err
	}
	r.ID = id
	return nil
}

// RecentResults returns the newest results of a monitor, newest first.
func (s *Store) RecentResults(monitorID int64, limit int) ([]*ResultRow, error) {
	rows, err := s.db.Query(`SELECT `+resultCols+` FROM results WHERE monitor_id = ? ORDER BY ts DESC LIMIT ?`, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ResultRow{}
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestPerLocation returns the most recent result for each location.
func (s *Store) LatestPerLocation(monitorID int64) ([]*ResultRow, error) {
	rows, err := s.db.Query(`SELECT r.id, r.monitor_id, r.location, r.ts, r.ok, r.latency_ms, r.status_code, r.message, r.cert_expiry, r.maint,
		r.t_dns, r.t_conn, r.t_tls, r.t_ttfb
		FROM results r JOIN (SELECT location, MAX(ts) AS ts FROM results WHERE monitor_id = ? AND ts > ? GROUP BY location) l
		ON r.location = l.location AND r.ts = l.ts WHERE r.monitor_id = ? ORDER BY r.id DESC`, monitorID, now()-7*24*3600*1000, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ResultRow{}
	seen := map[string]bool{}
	for rows.Next() {
		r, err := scanResult(rows)
		if err != nil {
			return nil, err
		}
		if !seen[r.Location] {
			seen[r.Location] = true
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// Uptime returns the share of checks since `since` (ms) taken while the
// monitor was up or degraded, or -1 without data, and the average latency of
// the OK checks. A check failing on one location while the monitor stays up
// does not lower it. Checks taken during maintenance windows are left out. History older than the raw results comes
// from the hourly rollups, with hour precision.
func (s *Store) Uptime(monitorID, since int64) (float64, float64, error) {
	return s.UptimeBetween(monitorID, since, math.MaxInt64)
}

// UptimeBetween is Uptime over the checks in [from, to). Rollup hours are
// counted when they start in that range.
func (s *Store) UptimeBetween(monitorID, from, to int64) (float64, float64, error) {
	var total, ok, latN int64
	var latSum float64
	w := s.RollupUntil()
	if from < w {
		var t, o, n sql.NullInt64
		var l sql.NullFloat64
		err := s.db.QueryRow(`SELECT SUM(total), SUM(up), SUM(up_lat_sum), SUM(up_lat_n) FROM result_rollups
			WHERE monitor_id = ? AND start >= ? AND start < ?`, monitorID, from-from%RollupHour, min(w, to)).Scan(&t, &o, &l, &n)
		if err != nil {
			return -1, 0, err
		}
		total, ok, latSum, latN = t.Int64, o.Int64, l.Float64, n.Int64
	}
	if to > w {
		var t, o, n sql.NullInt64
		var l sql.NullFloat64
		err := s.db.QueryRow(`SELECT COUNT(*), SUM(CASE WHEN mon_state < 2 THEN 1 ELSE 0 END), SUM(CASE WHEN ok = 1 THEN latency_ms END), SUM(CASE WHEN ok = 1 THEN 1 ELSE 0 END)
			FROM results WHERE monitor_id = ? AND ts >= ? AND ts < ? AND maint = 0`, monitorID, max(from, w), to).Scan(&t, &o, &l, &n)
		if err != nil {
			return -1, 0, err
		}
		total += t.Int64
		ok += o.Int64
		latSum += l.Float64
		latN += n.Int64
	}
	if total == 0 {
		return -1, 0, nil
	}
	avg := 0.0
	if latN > 0 {
		avg = latSum / float64(latN)
	}
	return float64(ok) / float64(total), avg, nil
}

// Bucket is an aggregated window of results.
type Bucket struct {
	Start    int64  `json:"t"`
	Location string `json:"location,omitempty"`
	Total    int    `json:"total"` // checks outside maintenance
	// OK counts the checks outside maintenance taken while the monitor was
	// up or degraded; per location, the checks that succeeded there.
	OK       int     `json:"ok"`
	Degraded int     `json:"degraded"` // checks taken while the monitor was degraded (not per location)
	Maint    int     `json:"maint"`    // checks during maintenance
	AvgMs    float64 `json:"avg_ms"`
	MinMs    float64 `json:"min_ms"`
	MaxMs    float64 `json:"max_ms"`
	// Phases is the average time per phase of the OK HTTP checks that
	// measured it (nil: none did, e.g. other check types or old agents).
	Phases *Phases `json:"phases,omitempty"`
}

// phaseSums adds up phases, in tenths of a ms, and the latency of the
// checks they come from.
type phaseSums struct {
	n                    int64
	lat                  float64
	dns, conn, tls, ttfb float64
}

func (a *phaseSums) add(b phaseSums) {
	a.n += b.n
	a.lat += b.lat
	a.dns += b.dns
	a.conn += b.conn
	a.tls += b.tls
	a.ttfb += b.ttfb
}

func (a phaseSums) avg() *Phases {
	if a.n == 0 {
		return nil
	}
	n := float64(a.n)
	r := func(v float64) float64 { return math.Round(v/n) / 10 }
	return &Phases{DNSMs: r(a.dns), ConnectMs: r(a.conn), TLSMs: r(a.tls), TTFBMs: r(a.ttfb), TotalMs: math.Round(a.lat/n*10) / 10, N: a.n}
}

// Phase sums of the OK checks with phases, raw results and rollups.
const (
	rawPhaseSums = `SUM(CASE WHEN ok = 1 AND t_ttfb > 0 THEN 1 ELSE 0 END), COALESCE(SUM(CASE WHEN ok = 1 AND t_ttfb > 0 THEN latency_ms END), 0),
		COALESCE(SUM(CASE WHEN ok = 1 AND t_ttfb > 0 THEN t_dns END), 0), COALESCE(SUM(CASE WHEN ok = 1 AND t_ttfb > 0 THEN t_conn END), 0),
		COALESCE(SUM(CASE WHEN ok = 1 AND t_ttfb > 0 THEN t_tls END), 0), COALESCE(SUM(CASE WHEN ok = 1 AND t_ttfb > 0 THEN t_ttfb END), 0)`
	rollupPhaseSums = `SUM(ph_n), SUM(ph_lat_sum), SUM(dns_sum), SUM(conn_sum), SUM(tls_sum), SUM(ttfb_sum)`
)

// Buckets aggregates results in [from, to) into fixed windows of `size` ms.
// When perLocation is true buckets are split by location and OK counts the
// checks that succeeded there; otherwise OK follows the monitor status (see
// Uptime). History older than
// the raw results comes from the hourly rollups: each hour falls in the
// window that contains its start.
func (s *Store) Buckets(monitorID, from, to, size int64, perLocation bool) ([]Bucket, error) {
	type key struct {
		idx int64
		loc string
	}
	type agg struct {
		Bucket
		latSum float64
		latN   int
		ph     phaseSums
	}
	acc := map[key]*agg{}
	var keys []key
	read := func(rows *sql.Rows) error {
		defer rows.Close()
		for rows.Next() {
			var k key
			var b agg
			var minMs, maxMs float64
			var ph phaseSums
			if err := rows.Scan(&k.idx, &k.loc, &b.Total, &b.OK, &b.Degraded, &b.Maint, &b.latSum, &b.latN, &minMs, &maxMs,
				&ph.n, &ph.lat, &ph.dns, &ph.conn, &ph.tls, &ph.ttfb); err != nil {
				return err
			}
			a := acc[k]
			if a == nil {
				a = &agg{}
				acc[k] = a
				keys = append(keys, k)
			}
			a.Total += b.Total
			a.OK += b.OK
			a.Degraded += b.Degraded
			a.Maint += b.Maint
			if b.latN > 0 {
				if a.latN == 0 || minMs < a.MinMs {
					a.MinMs = minMs
				}
				a.MaxMs = max(a.MaxMs, maxMs)
			}
			a.latSum += b.latSum
			a.latN += b.latN
			a.ph.add(ph)
		}
		return rows.Err()
	}
	loc, rollOK, rollDeg := `''`, `up`, `degraded`
	rawOK, rawDeg := `mon_state < 2`, `mon_state = 1`
	if perLocation {
		loc, rollOK, rollDeg = `location`, `ok`, `0`
		rawOK, rawDeg = `ok = 1`, `1 = 0`
	}
	w := s.RollupUntil()
	if from < w {
		rows, err := s.db.Query(`SELECT `+s.db.idiv(s.db.greatest("start", "?")+" - ?", "?")+` AS b, `+loc+` AS loc, SUM(total), SUM(`+rollOK+`), SUM(`+rollDeg+`), SUM(maint),
			SUM(lat_sum), SUM(lat_n), COALESCE(MIN(CASE WHEN lat_n > 0 THEN lat_min END), 0), MAX(lat_max),
			`+rollupPhaseSums+`
			FROM result_rollups WHERE monitor_id = ? AND start >= ? AND start < ? GROUP BY b, loc`,
			from, from, size, monitorID, from-from%RollupHour, min(w, to))
		if err != nil {
			return nil, err
		}
		if err := read(rows); err != nil {
			return nil, err
		}
	}
	rows, err := s.db.Query(`SELECT `+s.db.idiv("ts - ?", "?")+` AS b, `+loc+` AS loc, SUM(CASE WHEN maint = 0 THEN 1 ELSE 0 END), SUM(CASE WHEN `+rawOK+` AND maint = 0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN `+rawDeg+` AND maint = 0 THEN 1 ELSE 0 END), SUM(maint),
		COALESCE(SUM(CASE WHEN ok = 1 THEN latency_ms END), 0), SUM(CASE WHEN ok = 1 THEN 1 ELSE 0 END),
		COALESCE(MIN(CASE WHEN ok = 1 THEN latency_ms END), 0),
		COALESCE(MAX(CASE WHEN ok = 1 THEN latency_ms END), 0),
		`+rawPhaseSums+`
		FROM results WHERE monitor_id = ? AND ts >= ? AND ts < ? GROUP BY b, loc`, from, size, monitorID, max(from, w), to)
	if err != nil {
		return nil, err
	}
	if err := read(rows); err != nil {
		return nil, err
	}
	slices.SortFunc(keys, func(a, b key) int {
		if a.idx != b.idx {
			return cmp.Compare(a.idx, b.idx)
		}
		return strings.Compare(a.loc, b.loc)
	})
	out := make([]Bucket, 0, len(keys))
	for _, k := range keys {
		a := acc[k]
		b := a.Bucket
		b.Start = from + k.idx*size
		b.Location = k.loc
		if a.latN > 0 {
			b.AvgMs = a.latSum / float64(a.latN)
		}
		b.Phases = a.ph.avg()
		out = append(out, b)
	}
	return out, nil
}

// PruneResults deletes raw results older than before, but never results that
// are not summarized in the rollups yet. It works in small chunks so that the
// single database connection is never held for long.
func (s *Store) PruneResults(before int64) (int64, error) {
	before = min(before, s.RollupUntil())
	var total int64
	for {
		q := `DELETE FROM results WHERE id IN (SELECT id FROM results WHERE ts < ? LIMIT 5000)`
		if s.db.driver == DriverMySQL { // no LIMIT in IN subqueries, but DELETE ... LIMIT
			q = `DELETE FROM results WHERE ts < ? LIMIT 5000`
		}
		res, err := s.db.Exec(q, before)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 5000 {
			return total, nil
		}
	}
}

// InsertResults inserts many results in one transaction.
func (s *Store) InsertResults(rows []*ResultRow) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := resultInsert
	pg := s.db.driver == DriverPostgres
	if pg {
		q += ` RETURNING id`
	}
	stmt, err := tx.Prepare(q)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, r := range rows {
		args := r.insertArgs()
		if pg {
			if err := stmt.QueryRow(args...).Scan(&r.ID); err != nil {
				return err
			}
			continue
		}
		res, err := stmt.Exec(args...)
		if err != nil {
			return err
		}
		r.ID, _ = res.LastInsertId()
	}
	return tx.Commit()
}

// StatCell aggregates the results of one monitor in one time window.
type StatCell struct {
	MonitorID int64
	Start     int64
	Total     int // checks outside maintenance
	Up        int // checks outside maintenance taken while the monitor was not down
	Degraded  int // checks outside maintenance taken while the monitor was degraded
	Maint     int // checks during maintenance
	LatSum    float64
	LatN      int // OK checks
	UpLatSum  float64
	UpLatN    int // OK checks outside maintenance
}

// ResultStats aggregates the results in [from, to) per monitor in windows of
// `size` ms (aligned to from). One pass over that time range instead of a
// query per monitor.
func (s *Store) ResultStats(from, to, size int64) ([]StatCell, error) {
	rows, err := s.db.Query(`SELECT monitor_id, `+s.db.idiv("ts - ?", "?")+`, SUM(CASE WHEN maint = 0 THEN 1 ELSE 0 END),
		SUM(CASE WHEN mon_state < 2 AND maint = 0 THEN 1 ELSE 0 END), SUM(CASE WHEN mon_state = 1 AND maint = 0 THEN 1 ELSE 0 END), SUM(maint),
		COALESCE(SUM(CASE WHEN ok = 1 THEN latency_ms END), 0), SUM(CASE WHEN ok = 1 THEN 1 ELSE 0 END),
		COALESCE(SUM(CASE WHEN ok = 1 AND maint = 0 THEN latency_ms END), 0), SUM(CASE WHEN ok = 1 AND maint = 0 THEN 1 ELSE 0 END)
		FROM results INDEXED BY results_ts WHERE ts >= ? AND ts < ? GROUP BY 1, 2`, from, size, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StatCell
	for rows.Next() {
		var c StatCell
		var idx int64
		if err := rows.Scan(&c.MonitorID, &idx, &c.Total, &c.Up, &c.Degraded, &c.Maint, &c.LatSum, &c.LatN, &c.UpLatSum, &c.UpLatN); err != nil {
			return nil, err
		}
		c.Start = from + idx*size
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---------- incidents ----------

type Incident struct {
	ID          int64  `json:"id"`
	MonitorID   int64  `json:"monitor_id"`
	MonitorName string `json:"monitor_name"`
	StartedAt   int64  `json:"started_at"`
	ResolvedAt  int64  `json:"resolved_at"`
	Cause       string `json:"cause"`
}

// OpenIncident starts an incident and returns its id.
func (s *Store) OpenIncident(monitorID int64, at int64, cause string) (int64, error) {
	return s.db.insert(s.db, `INSERT INTO incidents (monitor_id, started_at, cause) VALUES (?, ?, ?)`, monitorID, at, cause)
}

// OpenIncidentAlerts returns the unresolved incidents with the alerts sent
// for each of them so far, keyed by monitor id (the latest incident wins).
func (s *Store) OpenIncidentAlerts() (map[int64]*IncidentAlerts, error) {
	rows, err := s.db.Query(`SELECT id, monitor_id, started_at, cause FROM incidents WHERE resolved_at = 0 ORDER BY started_at`)
	if err != nil {
		return nil, err
	}
	out := map[int64]*IncidentAlerts{}
	byID := map[int64]*IncidentAlerts{}
	for rows.Next() {
		a := &IncidentAlerts{Sent: map[int64]AlertSent{}}
		if err := rows.Scan(&a.IncidentID, &a.MonitorID, &a.StartedAt, &a.Cause); err != nil {
			rows.Close()
			return nil, err
		}
		out[a.MonitorID] = a
		byID[a.IncidentID] = a
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(out) == 0 {
		return out, err
	}
	rows, err = s.db.Query(`SELECT a.incident_id, a.notifier_id, a.sent_at, a.count FROM incident_alerts a JOIN incidents i ON i.id = a.incident_id WHERE i.resolved_at = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var inc, nt int64
		var sent AlertSent
		if err := rows.Scan(&inc, &nt, &sent.At, &sent.Count); err != nil {
			return nil, err
		}
		if a := byID[inc]; a != nil {
			a.Sent[nt] = sent
		}
	}
	return out, rows.Err()
}

// IncidentAlerts is an open incident and the alerts its channels received.
type IncidentAlerts struct {
	IncidentID int64
	MonitorID  int64
	StartedAt  int64
	Cause      string
	Sent       map[int64]AlertSent // by notifier id
}

// AlertSent is the last alert a channel received for an incident.
type AlertSent struct {
	At    int64 // unix ms
	Count int   // alerts sent so far: the first one, then reminders
}

// RecordIncidentAlert stores that a channel was alerted about an incident.
func (s *Store) RecordIncidentAlert(incidentID, notifierID int64, sent AlertSent) error {
	_, err := s.db.Exec(`INSERT INTO incident_alerts (incident_id, notifier_id, sent_at, count) VALUES (?, ?, ?, ?)
		ON CONFLICT(incident_id, notifier_id) DO UPDATE SET sent_at = excluded.sent_at, count = excluded.count`,
		incidentID, notifierID, sent.At, sent.Count)
	return err
}

func (s *Store) ResolveIncident(monitorID int64, at int64) error {
	_, err := s.db.Exec(`UPDATE incidents SET resolved_at = ? WHERE monitor_id = ? AND resolved_at = 0`, at, monitorID)
	return err
}

func (s *Store) ListIncidents(monitorID int64, limit int) ([]*Incident, error) {
	q := `SELECT i.id, i.monitor_id, m.name, i.started_at, i.resolved_at, i.cause FROM incidents i JOIN monitors m ON m.id = i.monitor_id`
	args := []any{}
	if monitorID > 0 {
		q += ` WHERE i.monitor_id = ?`
		args = append(args, monitorID)
	}
	q += ` ORDER BY i.started_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Incident{}
	for rows.Next() {
		i := &Incident{}
		if err := rows.Scan(&i.ID, &i.MonitorID, &i.MonitorName, &i.StartedAt, &i.ResolvedAt, &i.Cause); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// IncidentsBetween returns the incidents that were open at some point in
// [from, to), oldest first.
func (s *Store) IncidentsBetween(from, to int64) ([]*Incident, error) {
	rows, err := s.db.Query(`SELECT i.id, i.monitor_id, m.name, i.started_at, i.resolved_at, i.cause FROM incidents i JOIN monitors m ON m.id = i.monitor_id
		WHERE i.started_at < ? AND (i.resolved_at = 0 OR i.resolved_at >= ?) ORDER BY i.started_at, i.id`, to, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Incident{}
	for rows.Next() {
		i := &Incident{}
		if err := rows.Scan(&i.ID, &i.MonitorID, &i.MonitorName, &i.StartedAt, &i.ResolvedAt, &i.Cause); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

// OpenIncidentMonitors returns the ids of the monitors with an open incident.
func (s *Store) OpenIncidentMonitors() (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT DISTINCT monitor_id FROM incidents WHERE resolved_at = 0`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// ---------- notifiers ----------

type Notifier struct {
	ID      int64             `json:"id"`
	Name    string            `json:"name"`
	Type    string            `json:"type"`
	Config  map[string]string `json:"config"`
	Enabled bool              `json:"enabled"`
	// DelayMin waits this many minutes of downtime before the first alert
	// (0 = right away), so the channel can be an escalation contact.
	DelayMin int `json:"delay_min"`
	// RepeatMin repeats the alert every this many minutes while the monitor
	// stays down (0 = once).
	RepeatMin int   `json:"repeat_min"`
	CreatedAt int64 `json:"created_at"`
}

func (s *Store) ListNotifiers() ([]*Notifier, error) {
	rows, err := s.db.Query(`SELECT id, name, type, config, enabled, delay_min, repeat_min, created_at FROM notifiers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Notifier{}
	for rows.Next() {
		n := &Notifier{}
		var cfg string
		if err := rows.Scan(&n.ID, &n.Name, &n.Type, &cfg, &n.Enabled, &n.DelayMin, &n.RepeatMin, &n.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(cfg), &n.Config)
		if n.Config == nil {
			n.Config = map[string]string{}
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) Notifier(id int64) (*Notifier, error) {
	all, err := s.ListNotifiers()
	if err != nil {
		return nil, err
	}
	for _, n := range all {
		if n.ID == id {
			return n, nil
		}
	}
	return nil, ErrNotFound
}

func (s *Store) CreateNotifier(n *Notifier) error {
	cfg, _ := json.Marshal(n.Config)
	n.CreatedAt = now()
	id, err := s.db.insert(s.db, `INSERT INTO notifiers (name, type, config, enabled, delay_min, repeat_min, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`, n.Name, n.Type, string(cfg), b2i(n.Enabled), n.DelayMin, n.RepeatMin, n.CreatedAt)
	if err != nil {
		return err
	}
	n.ID = id
	return nil
}

func (s *Store) UpdateNotifier(n *Notifier) error {
	cfg, _ := json.Marshal(n.Config)
	_, err := s.db.Exec(`UPDATE notifiers SET name = ?, type = ?, config = ?, enabled = ?, delay_min = ?, repeat_min = ? WHERE id = ?`, n.Name, n.Type, string(cfg), b2i(n.Enabled), n.DelayMin, n.RepeatMin, n.ID)
	return err
}

func (s *Store) DeleteNotifier(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notifiers WHERE id = ?`, id)
	return err
}

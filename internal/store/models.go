package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

func now() int64 { return time.Now().UnixMilli() }

// ---------- users & sessions ----------

type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	PasswordHash string `json:"-"`
	CreatedAt    int64  `json:"created_at"`
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

func (s *Store) CreateUser(email, hash string) (*User, error) {
	u := &User{Email: email, PasswordHash: hash, CreatedAt: now()}
	res, err := s.db.Exec(`INSERT INTO users (email, password_hash, created_at) VALUES (?, ?, ?)`, u.Email, u.PasswordHash, u.CreatedAt)
	if err != nil {
		return nil, err
	}
	u.ID, _ = res.LastInsertId()
	return u, nil
}

func (s *Store) scanUser(row *sql.Row) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

func (s *Store) UserByEmail(email string) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT id, email, password_hash, created_at FROM users WHERE email = ?`, email))
}

func (s *Store) UserByID(id int64) (*User, error) {
	return s.scanUser(s.db.QueryRow(`SELECT id, email, password_hash, created_at FROM users WHERE id = ?`, id))
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
	return s.scanUser(s.db.QueryRow(`SELECT u.id, u.email, u.password_hash, u.created_at FROM sessions s JOIN users u ON u.id = s.user_id WHERE s.token_hash = ? AND s.expires_at > ?`, tokenHash, now()))
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
	res, err := s.db.Exec(`INSERT INTO agents (name, location, token_hash, created_at) VALUES (?, ?, ?, ?)`, a.Name, a.Location, a.TokenHash, a.CreatedAt)
	if err != nil {
		return nil, err
	}
	a.ID, _ = res.LastInsertId()
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
	Locations      []string          `json:"locations"`
	Paused         bool              `json:"paused"`
	Status         string            `json:"status"`
	StatusSince    int64             `json:"status_since"`
	CreatedAt      int64             `json:"created_at"`
}

const monitorCols = `id, name, type, target, interval_sec, timeout_ms, retries, method, expected_status, keyword, keyword_invert, dns_record, dns_server, dns_expected, ignore_tls, headers, body, locations, paused, status, status_since, created_at`

func scanMonitor(sc interface{ Scan(...any) error }) (*Monitor, error) {
	m := &Monitor{}
	var headers, locations string
	err := sc.Scan(&m.ID, &m.Name, &m.Type, &m.Target, &m.IntervalSec, &m.TimeoutMs, &m.Retries, &m.Method, &m.ExpectedStatus, &m.Keyword, &m.KeywordInvert, &m.DNSRecord, &m.DNSServer, &m.DNSExpected, &m.IgnoreTLS, &headers, &m.Body, &locations, &m.Paused, &m.Status, &m.StatusSince, &m.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(headers), &m.Headers)
	_ = json.Unmarshal([]byte(locations), &m.Locations)
	if m.Headers == nil {
		m.Headers = map[string]string{}
	}
	if m.Locations == nil {
		m.Locations = []string{}
	}
	return m, nil
}

func (s *Store) ListMonitors() ([]*Monitor, error) {
	rows, err := s.db.Query(`SELECT ` + monitorCols + ` FROM monitors ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Monitor{}
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) Monitor(id int64) (*Monitor, error) {
	return scanMonitor(s.db.QueryRow(`SELECT `+monitorCols+` FROM monitors WHERE id = ?`, id))
}

func (s *Store) CreateMonitor(m *Monitor) error {
	h, _ := json.Marshal(m.Headers)
	l, _ := json.Marshal(m.Locations)
	m.CreatedAt = now()
	m.Status = "pending"
	m.StatusSince = m.CreatedAt
	res, err := s.db.Exec(`INSERT INTO monitors (name, type, target, interval_sec, timeout_ms, retries, method, expected_status, keyword, keyword_invert, dns_record, dns_server, dns_expected, ignore_tls, headers, body, locations, paused, status, status_since, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.Name, m.Type, m.Target, m.IntervalSec, m.TimeoutMs, m.Retries, m.Method, m.ExpectedStatus, m.Keyword, b2i(m.KeywordInvert), m.DNSRecord, m.DNSServer, m.DNSExpected, b2i(m.IgnoreTLS), string(h), m.Body, string(l), b2i(m.Paused), m.Status, m.StatusSince, m.CreatedAt)
	if err != nil {
		return err
	}
	m.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateMonitor(m *Monitor) error {
	h, _ := json.Marshal(m.Headers)
	l, _ := json.Marshal(m.Locations)
	_, err := s.db.Exec(`UPDATE monitors SET name = ?, type = ?, target = ?, interval_sec = ?, timeout_ms = ?, retries = ?, method = ?, expected_status = ?, keyword = ?, keyword_invert = ?, dns_record = ?, dns_server = ?, dns_expected = ?, ignore_tls = ?, headers = ?, body = ?, locations = ? WHERE id = ?`,
		m.Name, m.Type, m.Target, m.IntervalSec, m.TimeoutMs, m.Retries, m.Method, m.ExpectedStatus, m.Keyword, b2i(m.KeywordInvert), m.DNSRecord, m.DNSServer, m.DNSExpected, b2i(m.IgnoreTLS), string(h), m.Body, string(l), m.ID)
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
}

func (s *Store) InsertResult(r *ResultRow) error {
	res, err := s.db.Exec(`INSERT INTO results (monitor_id, location, ts, ok, latency_ms, status_code, message, cert_expiry) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.MonitorID, r.Location, r.TS, b2i(r.OK), r.LatencyMs, r.StatusCode, r.Message, r.CertExpiry)
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	return nil
}

// RecentResults returns the newest results of a monitor, newest first.
func (s *Store) RecentResults(monitorID int64, limit int) ([]*ResultRow, error) {
	rows, err := s.db.Query(`SELECT id, monitor_id, location, ts, ok, latency_ms, status_code, message, cert_expiry FROM results WHERE monitor_id = ? ORDER BY ts DESC LIMIT ?`, monitorID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ResultRow{}
	for rows.Next() {
		r := &ResultRow{}
		if err := rows.Scan(&r.ID, &r.MonitorID, &r.Location, &r.TS, &r.OK, &r.LatencyMs, &r.StatusCode, &r.Message, &r.CertExpiry); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// LatestPerLocation returns the most recent result for each location.
func (s *Store) LatestPerLocation(monitorID int64) ([]*ResultRow, error) {
	rows, err := s.db.Query(`SELECT r.id, r.monitor_id, r.location, r.ts, r.ok, r.latency_ms, r.status_code, r.message, r.cert_expiry
		FROM results r JOIN (SELECT location, MAX(ts) AS ts FROM results WHERE monitor_id = ? AND ts > ? GROUP BY location) l
		ON r.location = l.location AND r.ts = l.ts WHERE r.monitor_id = ?`, monitorID, now()-7*24*3600*1000, monitorID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ResultRow{}
	seen := map[string]bool{}
	for rows.Next() {
		r := &ResultRow{}
		if err := rows.Scan(&r.ID, &r.MonitorID, &r.Location, &r.TS, &r.OK, &r.LatencyMs, &r.StatusCode, &r.Message, &r.CertExpiry); err != nil {
			return nil, err
		}
		if !seen[r.Location] {
			seen[r.Location] = true
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// Uptime returns the ratio of OK checks since `since` (ms), or -1 without data.
func (s *Store) Uptime(monitorID, since int64) (float64, float64, error) {
	var total, ok sql.NullInt64
	var avg sql.NullFloat64
	err := s.db.QueryRow(`SELECT COUNT(*), SUM(ok), AVG(CASE WHEN ok = 1 THEN latency_ms END) FROM results WHERE monitor_id = ? AND ts >= ?`, monitorID, since).Scan(&total, &ok, &avg)
	if err != nil {
		return -1, 0, err
	}
	if total.Int64 == 0 {
		return -1, 0, nil
	}
	return float64(ok.Int64) / float64(total.Int64), avg.Float64, nil
}

// Bucket is an aggregated window of results.
type Bucket struct {
	Start    int64   `json:"t"`
	Location string  `json:"location,omitempty"`
	Total    int     `json:"total"`
	OK       int     `json:"ok"`
	AvgMs    float64 `json:"avg_ms"`
	MinMs    float64 `json:"min_ms"`
	MaxMs    float64 `json:"max_ms"`
}

// Buckets aggregates results in [from, to) into fixed windows of `size` ms.
// When perLocation is true buckets are split by location.
func (s *Store) Buckets(monitorID, from, to, size int64, perLocation bool) ([]Bucket, error) {
	loc := `''`
	if perLocation {
		loc = `location`
	}
	q := `SELECT ((ts - ?) / ?) AS b, ` + loc + ` AS loc, COUNT(*), SUM(ok),
		COALESCE(AVG(CASE WHEN ok = 1 THEN latency_ms END), 0),
		COALESCE(MIN(CASE WHEN ok = 1 THEN latency_ms END), 0),
		COALESCE(MAX(CASE WHEN ok = 1 THEN latency_ms END), 0)
		FROM results WHERE monitor_id = ? AND ts >= ? AND ts < ? GROUP BY b, loc ORDER BY b`
	rows, err := s.db.Query(q, from, size, monitorID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Bucket{}
	for rows.Next() {
		var b Bucket
		var idx int64
		if err := rows.Scan(&idx, &b.Location, &b.Total, &b.OK, &b.AvgMs, &b.MinMs, &b.MaxMs); err != nil {
			return nil, err
		}
		b.Start = from + idx*size
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Store) PruneResults(before int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM results WHERE ts < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
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

func (s *Store) OpenIncident(monitorID int64, at int64, cause string) error {
	_, err := s.db.Exec(`INSERT INTO incidents (monitor_id, started_at, cause) VALUES (?, ?, ?)`, monitorID, at, cause)
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

// ---------- notifiers ----------

type Notifier struct {
	ID        int64             `json:"id"`
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Config    map[string]string `json:"config"`
	Enabled   bool              `json:"enabled"`
	CreatedAt int64             `json:"created_at"`
}

func (s *Store) ListNotifiers() ([]*Notifier, error) {
	rows, err := s.db.Query(`SELECT id, name, type, config, enabled, created_at FROM notifiers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Notifier{}
	for rows.Next() {
		n := &Notifier{}
		var cfg string
		if err := rows.Scan(&n.ID, &n.Name, &n.Type, &cfg, &n.Enabled, &n.CreatedAt); err != nil {
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
	res, err := s.db.Exec(`INSERT INTO notifiers (name, type, config, enabled, created_at) VALUES (?, ?, ?, ?, ?)`, n.Name, n.Type, string(cfg), b2i(n.Enabled), n.CreatedAt)
	if err != nil {
		return err
	}
	n.ID, _ = res.LastInsertId()
	return nil
}

func (s *Store) UpdateNotifier(n *Notifier) error {
	cfg, _ := json.Marshal(n.Config)
	_, err := s.db.Exec(`UPDATE notifiers SET name = ?, type = ?, config = ?, enabled = ? WHERE id = ?`, n.Name, n.Type, string(cfg), b2i(n.Enabled), n.ID)
	return err
}

func (s *Store) DeleteNotifier(id int64) error {
	_, err := s.db.Exec(`DELETE FROM notifiers WHERE id = ?`, id)
	return err
}

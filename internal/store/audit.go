package store

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// Where an audited action came from.
const (
	AuditWeb    = "web"    // browser session
	AuditAPI    = "api"    // personal API token (ptk_)
	AuditMCP    = "mcp"    // MCP client (OAuth or API token on /mcp)
	AuditApp    = "app"    // the mobile app (OAuth)
	AuditSystem = "system" // the hub itself (scheduler)
	AuditCLI    = "cli"    // command line flags (pingtxt -make-admin ...)
)

// Results of an audited action.
const (
	AuditOK     = "ok"
	AuditDenied = "denied" // role or token scope did not allow it
	AuditFailed = "failed" // wrong password, wrong 2FA code...
)

// AuditEntry is one line of the audit log: who did what, from where, and
// which fields changed. Email and target name are copies, so an entry stays
// readable after the user or the object is deleted.
type AuditEntry struct {
	ID         int64           `json:"id"`
	At         int64           `json:"at"`
	UserID     int64           `json:"user_id"`
	UserEmail  string          `json:"user_email"`
	Source     string          `json:"source"`
	Credential string          `json:"credential"` // token or app name
	Via        string          `json:"via"`        // MCP tool
	IP         string          `json:"ip"`
	RemoteAddr string          `json:"remote_addr"` // address of the connection
	UserAgent  string          `json:"user_agent"`
	Action     string          `json:"action"` // <object>.<verb>, e.g. monitor.update
	TargetType string          `json:"target_type"`
	TargetID   int64           `json:"target_id"`
	TargetName string          `json:"target_name"`
	Result     string          `json:"result"`
	Changes    json.RawMessage `json:"changes,omitempty"`
	RequestID  string          `json:"request_id"`
}

const auditCols = `id, at, user_id, user_email, source, credential, via, ip, remote_addr, user_agent,
	action, target_type, target_id, target_name, result, changes, request_id`

// Column sizes (the MySQL schema uses VARCHAR): longer values are cut,
// never in the middle of a character.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	s = s[:n]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return s
}

// AddAudit stores an entry. At must be set.
func (s *Store) AddAudit(e *AuditEntry) error {
	changes := ""
	if len(e.Changes) > 0 && string(e.Changes) != "null" {
		changes = string(e.Changes)
	}
	id, err := s.db.insert(s.db, `INSERT INTO audit_log (at, user_id, user_email, source, credential, via, ip,
		remote_addr, user_agent, action, target_type, target_id, target_name, result, changes, request_id)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.At, e.UserID, clip(e.UserEmail, 320), clip(e.Source, 16), clip(e.Credential, 255), clip(e.Via, 100),
		clip(e.IP, 64), clip(e.RemoteAddr, 255), clip(e.UserAgent, 512), clip(e.Action, 64),
		clip(e.TargetType, 32), e.TargetID, clip(e.TargetName, 255), clip(e.Result, 16), changes, clip(e.RequestID, 32))
	if err == nil {
		e.ID = id
	}
	return err
}

// AuditFilter narrows ListAudit. Zero values mean "any".
type AuditFilter struct {
	Before     int64    // entries with id < Before (cursor), 0 = newest
	Since      int64    // at >= Since
	Until      int64    // at < Until
	UserID     int64    // -1 = entries without a user (system, failed logins)
	Sources    []string // web, api, mcp, app, system, cli
	Category   string   // action prefix, e.g. "monitor" for monitor.*
	Action     string   // exact action
	TargetType string
	TargetID   int64
	Problems   bool   // only denied and failed
	Query      string // in user email, target name, credential or IP
	RequestID  string
	Limit      int // default 50, max 1000 (100000 for exports)
}

// ListAudit returns entries newest first.
func (s *Store) ListAudit(f AuditFilter) ([]AuditEntry, error) {
	var where []string
	var args []any
	add := func(cond string, a ...any) {
		where = append(where, cond)
		args = append(args, a...)
	}
	if f.Before > 0 {
		add("id < ?", f.Before)
	}
	if f.Since > 0 {
		add("at >= ?", f.Since)
	}
	if f.Until > 0 {
		add("at < ?", f.Until)
	}
	switch {
	case f.UserID > 0:
		add("user_id = ?", f.UserID)
	case f.UserID < 0:
		add("user_id = 0")
	}
	if len(f.Sources) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?, ", len(f.Sources)), ", ")
		where = append(where, "source IN ("+ph+")")
		for _, v := range f.Sources {
			args = append(args, v)
		}
	}
	if f.Category != "" {
		add("action LIKE ?", likeEscape(f.Category)+".%")
	}
	if f.Action != "" {
		add("action = ?", f.Action)
	}
	if f.TargetType != "" {
		add("target_type = ?", f.TargetType)
		if f.TargetID > 0 {
			add("target_id = ?", f.TargetID)
		}
	}
	if f.Problems {
		add("result <> ?", AuditOK)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		like := "%" + likeEscape(strings.ToLower(q)) + "%"
		add("(LOWER(user_email) LIKE ? OR LOWER(target_name) LIKE ? OR LOWER(credential) LIKE ? OR ip LIKE ?)", like, like, like, like)
	}
	if f.RequestID != "" {
		add("request_id = ?", f.RequestID)
	}
	if f.Limit <= 0 {
		f.Limit = 50
	}
	q := `SELECT ` + auditCols + ` FROM audit_log`
	if len(where) > 0 {
		q += ` WHERE ` + strings.Join(where, " AND ")
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		var changes string
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.UserEmail, &e.Source, &e.Credential, &e.Via, &e.IP,
			&e.RemoteAddr, &e.UserAgent, &e.Action, &e.TargetType, &e.TargetID, &e.TargetName, &e.Result,
			&changes, &e.RequestID); err != nil {
			return nil, err
		}
		if changes != "" {
			e.Changes = json.RawMessage(changes)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// likeEscape keeps % and _ typed by the user literal. The pattern has no
// ESCAPE clause (it differs between databases), so they are dropped.
func likeEscape(s string) string {
	return strings.NewReplacer("%", "", "_", "").Replace(s)
}

// AuditUser is a user that appears in the audit log, for the filter menu.
type AuditUser struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
}

// AuditUsers lists the users with at least one entry, latest email first.
func (s *Store) AuditUsers() ([]AuditUser, error) {
	rows, err := s.db.Query(`SELECT user_id, MAX(user_email) FROM audit_log WHERE user_id > 0 GROUP BY user_id ORDER BY 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AuditUser{}
	for rows.Next() {
		var u AuditUser
		if err := rows.Scan(&u.ID, &u.Email); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// PruneAudit deletes entries older than before.
func (s *Store) PruneAudit(before int64) (int64, error) {
	res, err := s.db.Exec(`DELETE FROM audit_log WHERE at < ?`, before)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

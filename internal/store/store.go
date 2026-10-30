// Package store persists hub data in SQLite (pure Go driver, no CGO).
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a unique value is already taken.
var ErrConflict = errors.New("already exists")

// Store wraps the database handle.
type Store struct {
	db   *sql.DB
	path string

	rmu         sync.RWMutex
	rollupUntil int64 // results before this are summarized in result_rollups
}

// Open opens (and migrates) the database at path.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // SQLite: one writer, avoids SQLITE_BUSY
	s := &Store{db: db, path: path}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	if err := s.loadRollupUntil(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

var migrations = []string{
	`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		email TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE sessions (
		token_hash TEXT PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		expires_at INTEGER NOT NULL
	);
	CREATE TABLE agents (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		location TEXT NOT NULL DEFAULT '',
		token_hash TEXT NOT NULL UNIQUE,
		version TEXT NOT NULL DEFAULT '',
		hostname TEXT NOT NULL DEFAULT '',
		platform TEXT NOT NULL DEFAULT '',
		remote_addr TEXT NOT NULL DEFAULT '',
		last_seen INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE monitors (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		target TEXT NOT NULL,
		interval_sec INTEGER NOT NULL DEFAULT 60,
		timeout_ms INTEGER NOT NULL DEFAULT 10000,
		retries INTEGER NOT NULL DEFAULT 1,
		method TEXT NOT NULL DEFAULT 'GET',
		expected_status TEXT NOT NULL DEFAULT '',
		keyword TEXT NOT NULL DEFAULT '',
		keyword_invert INTEGER NOT NULL DEFAULT 0,
		dns_record TEXT NOT NULL DEFAULT '',
		dns_server TEXT NOT NULL DEFAULT '',
		dns_expected TEXT NOT NULL DEFAULT '',
		ignore_tls INTEGER NOT NULL DEFAULT 0,
		headers TEXT NOT NULL DEFAULT '{}',
		body TEXT NOT NULL DEFAULT '',
		locations TEXT NOT NULL DEFAULT '["local"]',
		paused INTEGER NOT NULL DEFAULT 0,
		status TEXT NOT NULL DEFAULT 'pending',
		status_since INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE results (
		id INTEGER PRIMARY KEY,
		monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
		location TEXT NOT NULL,
		ts INTEGER NOT NULL,
		ok INTEGER NOT NULL,
		latency_ms REAL NOT NULL,
		status_code INTEGER NOT NULL DEFAULT 0,
		message TEXT NOT NULL DEFAULT '',
		cert_expiry INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX results_monitor_ts ON results(monitor_id, ts);
	CREATE INDEX results_ts ON results(ts);
	CREATE TABLE incidents (
		id INTEGER PRIMARY KEY,
		monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
		started_at INTEGER NOT NULL,
		resolved_at INTEGER NOT NULL DEFAULT 0,
		cause TEXT NOT NULL DEFAULT ''
	);
	CREATE INDEX incidents_monitor ON incidents(monitor_id, started_at);
	CREATE TABLE notifiers (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		type TEXT NOT NULL,
		config TEXT NOT NULL DEFAULT '{}',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL
	);`,
	`CREATE TABLE settings (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL
	);
	CREATE TABLE identities (
		provider TEXT NOT NULL,
		subject TEXT NOT NULL,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		email TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		PRIMARY KEY (provider, subject)
	);
	CREATE INDEX identities_user ON identities(user_id);`,
	`CREATE TABLE status_pages (
		id INTEGER PRIMARY KEY,
		slug TEXT NOT NULL UNIQUE,
		title TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		domain TEXT NOT NULL DEFAULT '',
		published INTEGER NOT NULL DEFAULT 1,
		show_incidents INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL
	);
	CREATE UNIQUE INDEX status_pages_domain ON status_pages(domain) WHERE domain != '';
	CREATE TABLE status_page_monitors (
		page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
		monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
		position INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (page_id, monitor_id)
	);
	CREATE INDEX status_page_monitors_monitor ON status_page_monitors(monitor_id);`,
	`CREATE TABLE user_totp (
		user_id INTEGER PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
		secret TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 0,
		last_step INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE recovery_codes (
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		code_hash TEXT NOT NULL,
		used_at INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (user_id, code_hash)
	);
	CREATE TABLE api_tokens (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		name TEXT NOT NULL,
		token_hash TEXT NOT NULL UNIQUE,
		hint TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		last_used_at INTEGER NOT NULL DEFAULT 0,
		expires_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX api_tokens_user ON api_tokens(user_id);`,
	`ALTER TABLE monitors ADD COLUMN push_token TEXT NOT NULL DEFAULT '';
	ALTER TABLE monitors ADD COLUMN grace_sec INTEGER NOT NULL DEFAULT 0;
	CREATE UNIQUE INDEX monitors_push_token ON monitors(push_token) WHERE push_token != '';`,
	`CREATE TABLE maintenances (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		description TEXT NOT NULL DEFAULT '',
		all_monitors INTEGER NOT NULL DEFAULT 0,
		monitor_ids TEXT NOT NULL DEFAULT '[]',
		kind TEXT NOT NULL,
		start_at INTEGER NOT NULL DEFAULT 0,
		end_at INTEGER NOT NULL DEFAULT 0,
		start_time TEXT NOT NULL DEFAULT '',
		duration_min INTEGER NOT NULL DEFAULT 0,
		weekdays TEXT NOT NULL DEFAULT '[]',
		month_days TEXT NOT NULL DEFAULT '[]',
		timezone TEXT NOT NULL DEFAULT 'UTC',
		enabled INTEGER NOT NULL DEFAULT 1,
		created_at INTEGER NOT NULL
	);
	ALTER TABLE results ADD COLUMN maint INTEGER NOT NULL DEFAULT 0;`,
	`ALTER TABLE monitors ADD COLUMN cert_expiry_alert INTEGER NOT NULL DEFAULT 1;
	ALTER TABLE monitors ADD COLUMN domain_expiry_alert INTEGER NOT NULL DEFAULT 1;
	CREATE TABLE expiry_checks (
		monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		subject TEXT NOT NULL DEFAULT '',
		expires_at INTEGER NOT NULL DEFAULT 0,
		issuer TEXT NOT NULL DEFAULT '',
		source TEXT NOT NULL DEFAULT '',
		error TEXT NOT NULL DEFAULT '',
		checked_at INTEGER NOT NULL DEFAULT 0,
		alerted_days INTEGER NOT NULL DEFAULT -1,
		PRIMARY KEY (monitor_id, kind)
	);`,
	`CREATE TABLE monitor_groups (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		description TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	);
	ALTER TABLE monitors ADD COLUMN group_id INTEGER REFERENCES monitor_groups(id) ON DELETE SET NULL;
	CREATE INDEX monitors_group ON monitors(group_id);
	CREATE TABLE tags (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE COLLATE NOCASE,
		color TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	);
	CREATE TABLE monitor_tags (
		monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
		tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
		PRIMARY KEY (monitor_id, tag_id)
	);
	CREATE INDEX monitor_tags_tag ON monitor_tags(tag_id);
	ALTER TABLE maintenances ADD COLUMN group_ids TEXT NOT NULL DEFAULT '[]';
	ALTER TABLE maintenances ADD COLUMN tag_ids TEXT NOT NULL DEFAULT '[]';
	CREATE TABLE status_page_groups (
		page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
		group_id INTEGER NOT NULL REFERENCES monitor_groups(id) ON DELETE CASCADE,
		position INTEGER NOT NULL DEFAULT 0,
		PRIMARY KEY (page_id, group_id)
	);`,
	`CREATE TABLE result_rollups (
		monitor_id INTEGER NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
		location TEXT NOT NULL,
		start INTEGER NOT NULL,
		total INTEGER NOT NULL,
		ok INTEGER NOT NULL,
		maint INTEGER NOT NULL,
		lat_sum REAL NOT NULL,
		lat_n INTEGER NOT NULL,
		up_lat_sum REAL NOT NULL,
		up_lat_n INTEGER NOT NULL,
		lat_min REAL NOT NULL,
		lat_max REAL NOT NULL,
		PRIMARY KEY (monitor_id, start, location)
	) WITHOUT ROWID;
	CREATE INDEX result_rollups_start ON result_rollups(start);`,
	// Existing users keep full access.
	`ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'admin';`,
}

func (s *Store) migrate() error {
	if _, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}
	var v int
	err := s.db.QueryRow(`SELECT version FROM schema_version`).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		if _, err := s.db.Exec(`INSERT INTO schema_version (version) VALUES (0)`); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.Exec(`UPDATE schema_version SET version = ?`, i+1); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DB exposes the handle for tests and tools.
func (s *Store) DB() *sql.DB { return s.db }

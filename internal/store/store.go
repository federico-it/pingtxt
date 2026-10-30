// Package store persists hub data in SQLite (pure Go driver, no CGO).
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a unique value is already taken.
var ErrConflict = errors.New("already exists")

// Store wraps the database handle.
type Store struct {
	db *sql.DB
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
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
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

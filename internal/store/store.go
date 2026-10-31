// Package store persists hub data in SQLite (pure Go driver, no CGO), or in
// PostgreSQL or MariaDB/MySQL when a database URL is given.
package store

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq" // registers the "postgres" driver
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row does not exist.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when a unique value is already taken.
var ErrConflict = errors.New("already exists")

// Store wraps the database handle.
type Store struct {
	db   *db
	path string // SQLite file, "" for PostgreSQL and MariaDB/MySQL

	rmu         sync.RWMutex
	rollupUntil int64 // results before this are summarized in result_rollups
}

// Open opens (and migrates) a database. dsn is either the path of a SQLite
// file or a URL: postgres://user:pass@host:5432/db?sslmode=disable or
// mysql://user:pass@host:3306/db (mariadb:// works too).
func Open(dsn string) (*Store, error) {
	driver, conn, err := parseDSN(dsn)
	if err != nil {
		return nil, err
	}
	s := &Store{}
	var sdb *sql.DB
	switch driver {
	case DriverSQLite:
		if dir := filepath.Dir(conn); dir != "" {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, err
			}
		}
		s.path = conn
		sdb, err = sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)", conn))
		if err != nil {
			return nil, err
		}
		sdb.SetMaxOpenConns(1) // SQLite: one writer, avoids SQLITE_BUSY
	default:
		sdb, err = sql.Open(driver, conn)
		if err != nil {
			return nil, err
		}
		sdb.SetMaxOpenConns(16)
		sdb.SetMaxIdleConns(4)
		sdb.SetConnMaxLifetime(30 * time.Minute)
		if err := waitReady(sdb, 30*time.Second); err != nil {
			sdb.Close()
			return nil, fmt.Errorf("connect to %s: %w", driver, err)
		}
	}
	s.db = &db{DB: sdb, driver: driver}
	if err := s.migrate(); err != nil {
		sdb.Close()
		return nil, err
	}
	if err := s.loadRollupUntil(); err != nil {
		sdb.Close()
		return nil, err
	}
	return s, nil
}

// waitReady pings the server until it answers: with docker compose the
// database container may still be starting.
func waitReady(sdb *sql.DB, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		err := sdb.Ping()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(time.Second)
	}
}

// parseDSN returns the driver and its connection string.
func parseDSN(dsn string) (driver, conn string, err error) {
	scheme, _, ok := strings.Cut(dsn, "://")
	if !ok {
		return DriverSQLite, strings.TrimPrefix(dsn, "sqlite:"), nil
	}
	switch strings.ToLower(scheme) {
	case "sqlite", "file":
		return DriverSQLite, strings.TrimPrefix(dsn, scheme+"://"), nil
	case "postgres", "postgresql":
		return DriverPostgres, dsn, nil
	case "mysql", "mariadb":
		u, err := url.Parse(dsn)
		if err != nil {
			return "", "", fmt.Errorf("database url: %w", err)
		}
		c := mysql.NewConfig()
		c.Net = "tcp"
		c.Addr = u.Host
		if u.Port() == "" {
			c.Addr = u.Hostname() + ":3306"
		}
		c.User = u.User.Username()
		c.Passwd, _ = u.User.Password()
		c.DBName = strings.TrimPrefix(u.Path, "/")
		c.ClientFoundRows = true // RowsAffected counts matched rows, like SQLite and PostgreSQL
		c.Params = map[string]string{"charset": "utf8mb4"}
		for k, v := range u.Query() {
			if len(v) > 0 {
				if k == "tls" {
					c.TLSConfig = v[0]
					continue
				}
				c.Params[k] = v[0]
			}
		}
		return DriverMySQL, c.FormatDSN(), nil
	}
	return "", "", fmt.Errorf("database url: unknown scheme %q (use postgres://, mysql://, mariadb:// or a SQLite file path)", scheme)
}

// Driver returns the database kind: DriverSQLite, DriverPostgres or DriverMySQL.
func (s *Store) Driver() string { return s.db.driver }

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// migrations are applied in order on SQLite; schema_version holds how many
// ran. PostgreSQL and MariaDB/MySQL start from baseSchema (the result of the
// first baseVersion migrations) and then run the later migrations, using the
// postgres/mysql variant when the SQLite text does not fit them.
//
// To change the schema, append a migration (never edit an old one) and test
// it on all three databases (see Development in the README).
type migration struct {
	sqlite   string
	postgres string // "" = same as sqlite
	mysql    string // "" = same as sqlite
}

func (m migration) text(driver string) string {
	switch {
	case driver == DriverPostgres && m.postgres != "":
		return m.postgres
	case driver == DriverMySQL && m.mysql != "":
		return m.mysql
	}
	return m.sqlite
}

var migrations = append(sqliteOnly(sqliteMigrations), laterMigrations...)

// laterMigrations come after baseVersion and run on every database, e.g.
//
//	{sqlite: `ALTER TABLE monitors ADD COLUMN notes TEXT NOT NULL DEFAULT ''`,
//	 mysql: `ALTER TABLE monitors ADD COLUMN notes TEXT NOT NULL DEFAULT ('')`},
var laterMigrations = []migration{
	// 11: share of failing locations that marks a monitor degraded (0 = instance default).
	{sqlite: `ALTER TABLE monitors ADD COLUMN degraded_pct INTEGER NOT NULL DEFAULT 0`},
	// 12: OAuth 2.1 for MCP clients: registered clients and the grants users gave them.
	{
		sqlite: `CREATE TABLE oauth_clients (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		redirect_uris TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE TABLE oauth_grants (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
		scope TEXT NOT NULL,
		access_hash TEXT NOT NULL UNIQUE,
		access_expires INTEGER NOT NULL,
		refresh_hash TEXT NOT NULL UNIQUE,
		refresh_expires INTEGER NOT NULL,
		created_at INTEGER NOT NULL,
		last_used_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX oauth_grants_user ON oauth_grants(user_id);`,
		postgres: `CREATE TABLE oauth_clients (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		redirect_uris TEXT NOT NULL,
		created_at BIGINT NOT NULL
	);
	CREATE TABLE oauth_grants (
		id BIGSERIAL PRIMARY KEY,
		user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		client_id TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
		scope TEXT NOT NULL,
		access_hash TEXT NOT NULL UNIQUE,
		access_expires BIGINT NOT NULL,
		refresh_hash TEXT NOT NULL UNIQUE,
		refresh_expires BIGINT NOT NULL,
		created_at BIGINT NOT NULL,
		last_used_at BIGINT NOT NULL DEFAULT 0
	);
	CREATE INDEX oauth_grants_user ON oauth_grants(user_id);`,
		mysql: `CREATE TABLE oauth_clients (
		id VARCHAR(64) PRIMARY KEY,
		name VARCHAR(255) NOT NULL,
		redirect_uris TEXT NOT NULL,
		created_at BIGINT NOT NULL
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	CREATE TABLE oauth_grants (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		user_id BIGINT NOT NULL,
		client_id VARCHAR(64) NOT NULL,
		scope VARCHAR(32) NOT NULL,
		access_hash VARCHAR(128) NOT NULL UNIQUE,
		access_expires BIGINT NOT NULL,
		refresh_hash VARCHAR(128) NOT NULL UNIQUE,
		refresh_expires BIGINT NOT NULL,
		created_at BIGINT NOT NULL,
		last_used_at BIGINT NOT NULL DEFAULT 0,
		INDEX oauth_grants_user (user_id),
		FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE,
		FOREIGN KEY (client_id) REFERENCES oauth_clients(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`,
	},
	// 13: rules on the JSON body of HTTP responses.
	{sqlite: `ALTER TABLE monitors ADD COLUMN assertions TEXT NOT NULL DEFAULT '[]'`,
		mysql: `ALTER TABLE monitors ADD COLUMN assertions TEXT NOT NULL DEFAULT ('[]')`},
	// 14: uptime follows the monitor status, not single checks. Each result
	// keeps the monitor status right after it (0 up, 1 degraded, 2 down) and
	// rollups count the rows taken while the monitor was up or degraded.
	// Older rows only know their own check, so ok stands in for the status.
	{sqlite: `ALTER TABLE results ADD COLUMN mon_state INTEGER NOT NULL DEFAULT 0;
	UPDATE results SET mon_state = 2 WHERE ok = 0;
	ALTER TABLE result_rollups ADD COLUMN up INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE result_rollups ADD COLUMN degraded INTEGER NOT NULL DEFAULT 0;
	UPDATE result_rollups SET up = ok;`},
	// 15: alert escalation and reminders. A channel can wait delay_min minutes
	// of downtime before its first alert and repeat it every repeat_min
	// minutes; incident_alerts remembers what each channel was sent.
	{
		sqlite: `ALTER TABLE notifiers ADD COLUMN delay_min INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE notifiers ADD COLUMN repeat_min INTEGER NOT NULL DEFAULT 0;
	CREATE TABLE incident_alerts (
		incident_id INTEGER NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
		notifier_id INTEGER NOT NULL REFERENCES notifiers(id) ON DELETE CASCADE,
		sent_at INTEGER NOT NULL,
		count INTEGER NOT NULL DEFAULT 1,
		PRIMARY KEY (incident_id, notifier_id)
	);`,
		postgres: `ALTER TABLE notifiers ADD COLUMN delay_min INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE notifiers ADD COLUMN repeat_min INTEGER NOT NULL DEFAULT 0;
	CREATE TABLE incident_alerts (
		incident_id BIGINT NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
		notifier_id BIGINT NOT NULL REFERENCES notifiers(id) ON DELETE CASCADE,
		sent_at BIGINT NOT NULL,
		count INTEGER NOT NULL DEFAULT 1,
		PRIMARY KEY (incident_id, notifier_id)
	);`,
		mysql: `ALTER TABLE notifiers ADD COLUMN delay_min INT NOT NULL DEFAULT 0;
	ALTER TABLE notifiers ADD COLUMN repeat_min INT NOT NULL DEFAULT 0;
	CREATE TABLE incident_alerts (
		incident_id BIGINT NOT NULL,
		notifier_id BIGINT NOT NULL,
		sent_at BIGINT NOT NULL,
		count INT NOT NULL DEFAULT 1,
		PRIMARY KEY (incident_id, notifier_id),
		FOREIGN KEY (incident_id) REFERENCES incidents(id) ON DELETE CASCADE,
		FOREIGN KEY (notifier_id) REFERENCES notifiers(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`,
	},
	// 16: HTTP response time per phase (DNS, connect, TLS, wait for the first
	// byte). Results keep each phase as an integer in tenths of a ms: 0 (no
	// data) costs no space in SQLite and small values take 1 to 3 bytes.
	// Rollups keep, for the OK checks that have phases (ph_n), the sums of
	// those tenths and of their latency in ms, so the rest (reading the
	// body) can be shown too.
	{sqlite: `ALTER TABLE results ADD COLUMN t_dns INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE results ADD COLUMN t_conn INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE results ADD COLUMN t_tls INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE results ADD COLUMN t_ttfb INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE result_rollups ADD COLUMN ph_n INTEGER NOT NULL DEFAULT 0;
	ALTER TABLE result_rollups ADD COLUMN ph_lat_sum DOUBLE PRECISION NOT NULL DEFAULT 0;
	ALTER TABLE result_rollups ADD COLUMN dns_sum DOUBLE PRECISION NOT NULL DEFAULT 0;
	ALTER TABLE result_rollups ADD COLUMN conn_sum DOUBLE PRECISION NOT NULL DEFAULT 0;
	ALTER TABLE result_rollups ADD COLUMN tls_sum DOUBLE PRECISION NOT NULL DEFAULT 0;
	ALTER TABLE result_rollups ADD COLUMN ttfb_sum DOUBLE PRECISION NOT NULL DEFAULT 0;`},
	// 17: incidents written on status pages (with their updates) and the
	// email subscribers of a page. subscribe_notifier_id is the email
	// channel that sends to subscribers (0 = subscriptions off).
	{
		sqlite: `ALTER TABLE status_pages ADD COLUMN subscribe_notifier_id INTEGER NOT NULL DEFAULT 0;
	CREATE TABLE page_incidents (
		id INTEGER PRIMARY KEY,
		page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
		title TEXT NOT NULL,
		impact TEXT NOT NULL DEFAULT 'minor',
		status TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL,
		resolved_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE INDEX page_incidents_page ON page_incidents(page_id, created_at);
	CREATE TABLE page_incident_updates (
		id INTEGER PRIMARY KEY,
		incident_id INTEGER NOT NULL REFERENCES page_incidents(id) ON DELETE CASCADE,
		status TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at INTEGER NOT NULL
	);
	CREATE INDEX page_incident_updates_incident ON page_incident_updates(incident_id, created_at);
	CREATE TABLE page_subscribers (
		id INTEGER PRIMARY KEY,
		page_id INTEGER NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
		email TEXT NOT NULL,
		token TEXT NOT NULL UNIQUE,
		confirmed_at INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		UNIQUE (page_id, email)
	);`,
		postgres: `ALTER TABLE status_pages ADD COLUMN subscribe_notifier_id BIGINT NOT NULL DEFAULT 0;
	CREATE TABLE page_incidents (
		id BIGSERIAL PRIMARY KEY,
		page_id BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
		title TEXT NOT NULL,
		impact TEXT NOT NULL DEFAULT 'minor',
		status TEXT NOT NULL,
		created_at BIGINT NOT NULL,
		updated_at BIGINT NOT NULL,
		resolved_at BIGINT NOT NULL DEFAULT 0
	);
	CREATE INDEX page_incidents_page ON page_incidents(page_id, created_at);
	CREATE TABLE page_incident_updates (
		id BIGSERIAL PRIMARY KEY,
		incident_id BIGINT NOT NULL REFERENCES page_incidents(id) ON DELETE CASCADE,
		status TEXT NOT NULL,
		message TEXT NOT NULL,
		created_at BIGINT NOT NULL
	);
	CREATE INDEX page_incident_updates_incident ON page_incident_updates(incident_id, created_at);
	CREATE TABLE page_subscribers (
		id BIGSERIAL PRIMARY KEY,
		page_id BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
		email TEXT NOT NULL,
		token TEXT NOT NULL UNIQUE,
		confirmed_at BIGINT NOT NULL DEFAULT 0,
		created_at BIGINT NOT NULL,
		UNIQUE (page_id, email)
	);`,
		mysql: `ALTER TABLE status_pages ADD COLUMN subscribe_notifier_id BIGINT NOT NULL DEFAULT 0;
	CREATE TABLE page_incidents (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		page_id BIGINT NOT NULL,
		title VARCHAR(255) NOT NULL,
		impact VARCHAR(16) NOT NULL DEFAULT 'minor',
		status VARCHAR(16) NOT NULL,
		created_at BIGINT NOT NULL,
		updated_at BIGINT NOT NULL,
		resolved_at BIGINT NOT NULL DEFAULT 0,
		INDEX page_incidents_page (page_id, created_at),
		FOREIGN KEY (page_id) REFERENCES status_pages(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	CREATE TABLE page_incident_updates (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		incident_id BIGINT NOT NULL,
		status VARCHAR(16) NOT NULL,
		message TEXT NOT NULL,
		created_at BIGINT NOT NULL,
		INDEX page_incident_updates_incident (incident_id, created_at),
		FOREIGN KEY (incident_id) REFERENCES page_incidents(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
	CREATE TABLE page_subscribers (
		id BIGINT AUTO_INCREMENT PRIMARY KEY,
		page_id BIGINT NOT NULL,
		email VARCHAR(254) NOT NULL,
		token VARCHAR(64) NOT NULL UNIQUE,
		confirmed_at BIGINT NOT NULL DEFAULT 0,
		created_at BIGINT NOT NULL,
		UNIQUE KEY page_subscribers_email (page_id, email),
		FOREIGN KEY (page_id) REFERENCES status_pages(id) ON DELETE CASCADE
	) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;`,
	},
	// 18: migration 14 marked every failed check stored before it as
	// downtime. Recompute those still in the raw results with the majority
	// rule: a failed check is downtime only when more than half of the
	// checks of that monitor within half an interval of it failed. Then
	// recompute up for the rollup hours whose raw results are all still there.
	// The derived table x lets MariaDB/MySQL read the table it updates.
	{sqlite: `UPDATE results SET mon_state = 0 WHERE id IN (SELECT id FROM (
		SELECT r.id AS id FROM results r
		JOIN monitors m ON m.id = r.monitor_id
		JOIN results o ON o.monitor_id = r.monitor_id AND o.ts >= r.ts - m.interval_sec * 500 AND o.ts < r.ts + m.interval_sec * 500
		WHERE r.ok = 0 AND r.mon_state = 2
		GROUP BY r.id
		HAVING 2 * SUM(CASE WHEN o.ok = 0 THEN 1 ELSE 0 END) <= COUNT(*)) x);
	UPDATE result_rollups SET up = (SELECT SUM(CASE WHEN r.mon_state < 2 AND r.maint = 0 THEN 1 ELSE 0 END) FROM results r
		WHERE r.monitor_id = result_rollups.monitor_id AND r.location = result_rollups.location
		AND r.ts >= result_rollups.start AND r.ts < result_rollups.start + 3600000)
		WHERE start > (SELECT MIN(ts) FROM results) - 3600000 AND total + maint = (SELECT COUNT(*) FROM results r
		WHERE r.monitor_id = result_rollups.monitor_id AND r.location = result_rollups.location
		AND r.ts >= result_rollups.start AND r.ts < result_rollups.start + 3600000);`},
	// 19: checks over IPv4 only (4), IPv6 only (6) or either (0).
	{sqlite: `ALTER TABLE monitors ADD COLUMN ip_family INTEGER NOT NULL DEFAULT 0`},
	// 20: IPv6 connectivity of each agent: 0 unknown, 1 yes, 2 no.
	{sqlite: `ALTER TABLE agents ADD COLUMN ipv6 INTEGER NOT NULL DEFAULT 0`},
}

func sqliteOnly(list []string) []migration {
	out := make([]migration, len(list))
	for i, q := range list {
		out[i] = migration{sqlite: q}
	}
	return out
}

// Migrations 1 to baseVersion, SQLite only (PostgreSQL and MariaDB/MySQL use baseSchema).
var sqliteMigrations = []string{
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
	if v == 0 && s.db.driver != DriverSQLite {
		if err := s.applyMigration(baseSchema(s.db.driver), baseVersion); err != nil {
			return fmt.Errorf("create schema: %w", err)
		}
		v = baseVersion
	}
	for i := v; i < len(migrations); i++ {
		if err := s.applyMigration(migrations[i].text(s.db.driver), i+1); err != nil {
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
	}
	return nil
}

// applyMigration runs a migration and records the new version. MariaDB/MySQL
// cannot roll back schema changes, so a failed migration there may need
// manual cleanup.
func (s *Store) applyMigration(text string, version int) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	if err := t.execScript(text); err != nil {
		return err
	}
	if _, err := t.Exec(`UPDATE schema_version SET version = ?`, version); err != nil {
		return err
	}
	return t.Commit()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DB exposes the handle for tests and tools. Queries on it are not rewritten
// for PostgreSQL or MariaDB/MySQL.
func (s *Store) DB() *sql.DB { return s.db.DB }

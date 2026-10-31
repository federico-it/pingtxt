package store

import "strings"

// baseVersion is the SQLite migration that baseSchema matches. A new
// PostgreSQL or MariaDB/MySQL database gets baseSchema and then runs the
// migrations after it. Do not change baseSchema: append migrations instead.
const baseVersion = 10

func baseSchema(driver string) string {
	if driver == DriverMySQL {
		return mysqlBase
	}
	return postgresBase
}

// Booleans are integers (0/1) like in SQLite, times are unix milliseconds.
var postgresBase = `
CREATE TABLE users (
	id BIGSERIAL PRIMARY KEY,
	email TEXT NOT NULL UNIQUE,
	password_hash TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	role TEXT NOT NULL DEFAULT 'admin'
);
CREATE TABLE sessions (
	token_hash TEXT PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	expires_at BIGINT NOT NULL
);
CREATE INDEX sessions_user ON sessions(user_id);
CREATE TABLE agents (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	location TEXT NOT NULL DEFAULT '',
	token_hash TEXT NOT NULL UNIQUE,
	version TEXT NOT NULL DEFAULT '',
	hostname TEXT NOT NULL DEFAULT '',
	platform TEXT NOT NULL DEFAULT '',
	remote_addr TEXT NOT NULL DEFAULT '',
	last_seen BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL
);
CREATE TABLE monitor_groups (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX monitor_groups_name ON monitor_groups(LOWER(name));
CREATE TABLE monitors (
	id BIGSERIAL PRIMARY KEY,
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
	status_since BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	push_token TEXT NOT NULL DEFAULT '',
	grace_sec INTEGER NOT NULL DEFAULT 0,
	cert_expiry_alert INTEGER NOT NULL DEFAULT 1,
	domain_expiry_alert INTEGER NOT NULL DEFAULT 1,
	group_id BIGINT REFERENCES monitor_groups(id) ON DELETE SET NULL
);
CREATE UNIQUE INDEX monitors_push_token ON monitors(push_token) WHERE push_token != '';
CREATE INDEX monitors_group ON monitors(group_id);
CREATE TABLE results (
	id BIGSERIAL PRIMARY KEY,
	monitor_id BIGINT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	location TEXT NOT NULL,
	ts BIGINT NOT NULL,
	ok INTEGER NOT NULL,
	latency_ms DOUBLE PRECISION NOT NULL,
	status_code INTEGER NOT NULL DEFAULT 0,
	message TEXT NOT NULL DEFAULT '',
	cert_expiry BIGINT NOT NULL DEFAULT 0,
	maint INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX results_monitor_ts ON results(monitor_id, ts);
CREATE INDEX results_ts ON results(ts);
CREATE TABLE incidents (
	id BIGSERIAL PRIMARY KEY,
	monitor_id BIGINT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	started_at BIGINT NOT NULL,
	resolved_at BIGINT NOT NULL DEFAULT 0,
	cause TEXT NOT NULL DEFAULT ''
);
CREATE INDEX incidents_monitor ON incidents(monitor_id, started_at);
CREATE TABLE notifiers (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	type TEXT NOT NULL,
	config TEXT NOT NULL DEFAULT '{}',
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at BIGINT NOT NULL
);
CREATE TABLE settings (
	key TEXT PRIMARY KEY,
	value TEXT NOT NULL
);
CREATE TABLE identities (
	provider TEXT NOT NULL,
	subject TEXT NOT NULL,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	email TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL,
	PRIMARY KEY (provider, subject)
);
CREATE INDEX identities_user ON identities(user_id);
CREATE TABLE status_pages (
	id BIGSERIAL PRIMARY KEY,
	slug TEXT NOT NULL UNIQUE,
	title TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	domain TEXT NOT NULL DEFAULT '',
	published INTEGER NOT NULL DEFAULT 1,
	show_incidents INTEGER NOT NULL DEFAULT 1,
	created_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX status_pages_domain ON status_pages(domain) WHERE domain != '';
CREATE TABLE status_page_monitors (
	page_id BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
	monitor_id BIGINT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	position INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (page_id, monitor_id)
);
CREATE INDEX status_page_monitors_monitor ON status_page_monitors(monitor_id);
CREATE TABLE user_totp (
	user_id BIGINT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
	secret TEXT NOT NULL,
	enabled INTEGER NOT NULL DEFAULT 0,
	last_step BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL
);
CREATE TABLE recovery_codes (
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	code_hash TEXT NOT NULL,
	used_at BIGINT NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, code_hash)
);
CREATE TABLE api_tokens (
	id BIGSERIAL PRIMARY KEY,
	user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	token_hash TEXT NOT NULL UNIQUE,
	hint TEXT NOT NULL DEFAULT '',
	scope TEXT NOT NULL,
	created_at BIGINT NOT NULL,
	last_used_at BIGINT NOT NULL DEFAULT 0,
	expires_at BIGINT NOT NULL DEFAULT 0
);
CREATE INDEX api_tokens_user ON api_tokens(user_id);
CREATE TABLE maintenances (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	description TEXT NOT NULL DEFAULT '',
	all_monitors INTEGER NOT NULL DEFAULT 0,
	monitor_ids TEXT NOT NULL DEFAULT '[]',
	kind TEXT NOT NULL,
	start_at BIGINT NOT NULL DEFAULT 0,
	end_at BIGINT NOT NULL DEFAULT 0,
	start_time TEXT NOT NULL DEFAULT '',
	duration_min INTEGER NOT NULL DEFAULT 0,
	weekdays TEXT NOT NULL DEFAULT '[]',
	month_days TEXT NOT NULL DEFAULT '[]',
	timezone TEXT NOT NULL DEFAULT 'UTC',
	enabled INTEGER NOT NULL DEFAULT 1,
	created_at BIGINT NOT NULL,
	group_ids TEXT NOT NULL DEFAULT '[]',
	tag_ids TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE expiry_checks (
	monitor_id BIGINT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	subject TEXT NOT NULL DEFAULT '',
	expires_at BIGINT NOT NULL DEFAULT 0,
	issuer TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT '',
	checked_at BIGINT NOT NULL DEFAULT 0,
	alerted_days INTEGER NOT NULL DEFAULT -1,
	PRIMARY KEY (monitor_id, kind)
);
CREATE TABLE tags (
	id BIGSERIAL PRIMARY KEY,
	name TEXT NOT NULL,
	color TEXT NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX tags_name ON tags(LOWER(name));
CREATE TABLE monitor_tags (
	monitor_id BIGINT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	tag_id BIGINT NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
	PRIMARY KEY (monitor_id, tag_id)
);
CREATE INDEX monitor_tags_tag ON monitor_tags(tag_id);
CREATE TABLE status_page_groups (
	page_id BIGINT NOT NULL REFERENCES status_pages(id) ON DELETE CASCADE,
	group_id BIGINT NOT NULL REFERENCES monitor_groups(id) ON DELETE CASCADE,
	position INTEGER NOT NULL DEFAULT 0,
	PRIMARY KEY (page_id, group_id)
);
CREATE TABLE result_rollups (
	monitor_id BIGINT NOT NULL REFERENCES monitors(id) ON DELETE CASCADE,
	location TEXT NOT NULL,
	start BIGINT NOT NULL,
	total INTEGER NOT NULL,
	ok INTEGER NOT NULL,
	maint INTEGER NOT NULL,
	lat_sum DOUBLE PRECISION NOT NULL,
	lat_n INTEGER NOT NULL,
	up_lat_sum DOUBLE PRECISION NOT NULL,
	up_lat_n INTEGER NOT NULL,
	lat_min DOUBLE PRECISION NOT NULL,
	lat_max DOUBLE PRECISION NOT NULL,
	PRIMARY KEY (monitor_id, start, location)
);
CREATE INDEX result_rollups_start ON result_rollups(start);
`

// MariaDB/MySQL: indexed text columns are VARCHAR (TEXT cannot be indexed
// without a prefix), TEXT columns use expression defaults (”) which need
// MariaDB 10.2+ or MySQL 8.0.13+. Unique "where not empty" indexes are
// generated NULL-when-empty columns with a plain unique index. Names are
// case-insensitive through the default collation.
var mysqlBase = strings.ReplaceAll(`
CREATE TABLE users (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	email VARCHAR(255) NOT NULL UNIQUE,
	password_hash VARCHAR(255) NOT NULL,
	created_at BIGINT NOT NULL,
	role VARCHAR(32) NOT NULL DEFAULT 'admin'
) $T;
CREATE TABLE sessions (
	token_hash VARCHAR(128) PRIMARY KEY,
	user_id BIGINT NOT NULL,
	expires_at BIGINT NOT NULL,
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) $T;
CREATE TABLE agents (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL,
	location VARCHAR(255) NOT NULL DEFAULT '',
	token_hash VARCHAR(128) NOT NULL UNIQUE,
	version VARCHAR(64) NOT NULL DEFAULT '',
	hostname VARCHAR(255) NOT NULL DEFAULT '',
	platform VARCHAR(64) NOT NULL DEFAULT '',
	remote_addr VARCHAR(255) NOT NULL DEFAULT '',
	last_seen BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL
) $T;
CREATE TABLE monitor_groups (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL UNIQUE,
	description TEXT NOT NULL DEFAULT (''),
	created_at BIGINT NOT NULL
) $T;
CREATE TABLE monitors (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL,
	type VARCHAR(32) NOT NULL,
	target TEXT NOT NULL,
	interval_sec INT NOT NULL DEFAULT 60,
	timeout_ms INT NOT NULL DEFAULT 10000,
	retries INT NOT NULL DEFAULT 1,
	method VARCHAR(16) NOT NULL DEFAULT 'GET',
	expected_status VARCHAR(255) NOT NULL DEFAULT '',
	keyword TEXT NOT NULL DEFAULT (''),
	keyword_invert INT NOT NULL DEFAULT 0,
	dns_record VARCHAR(16) NOT NULL DEFAULT '',
	dns_server VARCHAR(255) NOT NULL DEFAULT '',
	dns_expected TEXT NOT NULL DEFAULT (''),
	ignore_tls INT NOT NULL DEFAULT 0,
	headers TEXT NOT NULL DEFAULT ('{}'),
	body TEXT NOT NULL DEFAULT (''),
	locations TEXT NOT NULL DEFAULT ('["local"]'),
	paused INT NOT NULL DEFAULT 0,
	status VARCHAR(32) NOT NULL DEFAULT 'pending',
	status_since BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	push_token VARCHAR(128) NOT NULL DEFAULT '',
	push_token_key VARCHAR(128) AS (NULLIF(push_token, '')) STORED UNIQUE,
	grace_sec INT NOT NULL DEFAULT 0,
	cert_expiry_alert INT NOT NULL DEFAULT 1,
	domain_expiry_alert INT NOT NULL DEFAULT 1,
	group_id BIGINT NULL,
	INDEX monitors_group (group_id),
	FOREIGN KEY (group_id) REFERENCES monitor_groups(id) ON DELETE SET NULL
) $T;
CREATE TABLE results (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	monitor_id BIGINT NOT NULL,
	location VARCHAR(255) NOT NULL,
	ts BIGINT NOT NULL,
	ok INT NOT NULL,
	latency_ms DOUBLE NOT NULL,
	status_code INT NOT NULL DEFAULT 0,
	message TEXT NOT NULL DEFAULT (''),
	cert_expiry BIGINT NOT NULL DEFAULT 0,
	maint INT NOT NULL DEFAULT 0,
	INDEX results_monitor_ts (monitor_id, ts),
	INDEX results_ts (ts),
	FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
) $T;
CREATE TABLE incidents (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	monitor_id BIGINT NOT NULL,
	started_at BIGINT NOT NULL,
	resolved_at BIGINT NOT NULL DEFAULT 0,
	cause TEXT NOT NULL DEFAULT (''),
	INDEX incidents_monitor (monitor_id, started_at),
	FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
) $T;
CREATE TABLE notifiers (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL,
	type VARCHAR(32) NOT NULL,
	config TEXT NOT NULL DEFAULT ('{}'),
	enabled INT NOT NULL DEFAULT 1,
	created_at BIGINT NOT NULL
) $T;
CREATE TABLE settings (
	`+"`key`"+` VARCHAR(191) PRIMARY KEY,
	value MEDIUMTEXT NOT NULL
) $T;
CREATE TABLE identities (
	provider VARCHAR(64) NOT NULL,
	subject VARCHAR(255) NOT NULL,
	user_id BIGINT NOT NULL,
	email VARCHAR(255) NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL,
	PRIMARY KEY (provider, subject),
	INDEX identities_user (user_id),
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) $T;
CREATE TABLE status_pages (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	slug VARCHAR(255) NOT NULL UNIQUE,
	title VARCHAR(255) NOT NULL,
	description TEXT NOT NULL DEFAULT (''),
	domain VARCHAR(255) NOT NULL DEFAULT '',
	domain_key VARCHAR(255) AS (NULLIF(domain, '')) STORED UNIQUE,
	published INT NOT NULL DEFAULT 1,
	show_incidents INT NOT NULL DEFAULT 1,
	created_at BIGINT NOT NULL
) $T;
CREATE TABLE status_page_monitors (
	page_id BIGINT NOT NULL,
	monitor_id BIGINT NOT NULL,
	position INT NOT NULL DEFAULT 0,
	PRIMARY KEY (page_id, monitor_id),
	INDEX status_page_monitors_monitor (monitor_id),
	FOREIGN KEY (page_id) REFERENCES status_pages(id) ON DELETE CASCADE,
	FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
) $T;
CREATE TABLE user_totp (
	user_id BIGINT PRIMARY KEY,
	secret VARCHAR(255) NOT NULL,
	enabled INT NOT NULL DEFAULT 0,
	last_step BIGINT NOT NULL DEFAULT 0,
	created_at BIGINT NOT NULL,
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) $T;
CREATE TABLE recovery_codes (
	user_id BIGINT NOT NULL,
	code_hash VARCHAR(128) NOT NULL,
	used_at BIGINT NOT NULL DEFAULT 0,
	PRIMARY KEY (user_id, code_hash),
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) $T;
CREATE TABLE api_tokens (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	user_id BIGINT NOT NULL,
	name VARCHAR(255) NOT NULL,
	token_hash VARCHAR(128) NOT NULL UNIQUE,
	hint VARCHAR(64) NOT NULL DEFAULT '',
	scope VARCHAR(32) NOT NULL,
	created_at BIGINT NOT NULL,
	last_used_at BIGINT NOT NULL DEFAULT 0,
	expires_at BIGINT NOT NULL DEFAULT 0,
	INDEX api_tokens_user (user_id),
	FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
) $T;
CREATE TABLE maintenances (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL,
	description TEXT NOT NULL DEFAULT (''),
	all_monitors INT NOT NULL DEFAULT 0,
	monitor_ids TEXT NOT NULL DEFAULT ('[]'),
	kind VARCHAR(32) NOT NULL,
	start_at BIGINT NOT NULL DEFAULT 0,
	end_at BIGINT NOT NULL DEFAULT 0,
	start_time VARCHAR(16) NOT NULL DEFAULT '',
	duration_min INT NOT NULL DEFAULT 0,
	weekdays VARCHAR(255) NOT NULL DEFAULT '[]',
	month_days VARCHAR(255) NOT NULL DEFAULT '[]',
	timezone VARCHAR(64) NOT NULL DEFAULT 'UTC',
	enabled INT NOT NULL DEFAULT 1,
	created_at BIGINT NOT NULL,
	group_ids TEXT NOT NULL DEFAULT ('[]'),
	tag_ids TEXT NOT NULL DEFAULT ('[]')
) $T;
CREATE TABLE expiry_checks (
	monitor_id BIGINT NOT NULL,
	kind VARCHAR(16) NOT NULL,
	subject VARCHAR(255) NOT NULL DEFAULT '',
	expires_at BIGINT NOT NULL DEFAULT 0,
	issuer VARCHAR(255) NOT NULL DEFAULT '',
	source VARCHAR(16) NOT NULL DEFAULT '',
	error TEXT NOT NULL DEFAULT (''),
	checked_at BIGINT NOT NULL DEFAULT 0,
	alerted_days INT NOT NULL DEFAULT -1,
	PRIMARY KEY (monitor_id, kind),
	FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
) $T;
CREATE TABLE tags (
	id BIGINT AUTO_INCREMENT PRIMARY KEY,
	name VARCHAR(255) NOT NULL UNIQUE,
	color VARCHAR(32) NOT NULL DEFAULT '',
	created_at BIGINT NOT NULL
) $T;
CREATE TABLE monitor_tags (
	monitor_id BIGINT NOT NULL,
	tag_id BIGINT NOT NULL,
	PRIMARY KEY (monitor_id, tag_id),
	INDEX monitor_tags_tag (tag_id),
	FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE,
	FOREIGN KEY (tag_id) REFERENCES tags(id) ON DELETE CASCADE
) $T;
CREATE TABLE status_page_groups (
	page_id BIGINT NOT NULL,
	group_id BIGINT NOT NULL,
	position INT NOT NULL DEFAULT 0,
	PRIMARY KEY (page_id, group_id),
	FOREIGN KEY (page_id) REFERENCES status_pages(id) ON DELETE CASCADE,
	FOREIGN KEY (group_id) REFERENCES monitor_groups(id) ON DELETE CASCADE
) $T;
CREATE TABLE result_rollups (
	monitor_id BIGINT NOT NULL,
	location VARCHAR(255) NOT NULL,
	start BIGINT NOT NULL,
	total INT NOT NULL,
	ok INT NOT NULL,
	maint INT NOT NULL,
	lat_sum DOUBLE NOT NULL,
	lat_n INT NOT NULL,
	up_lat_sum DOUBLE NOT NULL,
	up_lat_n INT NOT NULL,
	lat_min DOUBLE NOT NULL,
	lat_max DOUBLE NOT NULL,
	PRIMARY KEY (monitor_id, start, location),
	INDEX result_rollups_start (start),
	FOREIGN KEY (monitor_id) REFERENCES monitors(id) ON DELETE CASCADE
) $T;
`, "$T", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4")

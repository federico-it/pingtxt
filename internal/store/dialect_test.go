package store

import (
	"errors"
	"testing"
	"time"
)

// TestDialectQueries runs the queries that differ between SQLite, PostgreSQL
// and MariaDB/MySQL (upserts, unique conflicts, inserted ids, integer
// division, chunked deletes). CI runs it on all three.
func TestDialectQueries(t *testing.T) {
	s := openTest(t)

	// Inserted ids.
	u, err := s.CreateUser("a@b.c", "hash", "admin")
	if err != nil || u.ID == 0 {
		t.Fatalf("user: %+v %v", u, err)
	}

	// Settings upsert (and the "key" column, reserved in MySQL).
	for _, v := range []string{"one", "two"} {
		if err := s.SetSetting("general", map[string]string{"v": v}); err != nil {
			t.Fatal(err)
		}
	}
	var got map[string]string
	if err := s.GetSetting("general", &got); err != nil || got["v"] != "two" {
		t.Fatalf("setting: %v %v", got, err)
	}
	if err := s.GetSetting("missing", &got); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing setting: %v", err)
	}

	// Identity upsert.
	u2, _ := s.CreateUser("x@y.z", "hash", "viewer")
	must(t, s.LinkIdentity("github", "42", u.ID, "a@b.c"))
	must(t, s.LinkIdentity("github", "42", u2.ID, "x@y.z"))
	if iu, err := s.IdentityUser("github", "42"); err != nil || iu.ID != u2.ID {
		t.Fatalf("identity: %+v %v", iu, err)
	}

	// Pending TOTP replaces a pending secret, never an enabled one.
	must(t, s.SetPendingTOTP(u.ID, "s1"))
	must(t, s.SetPendingTOTP(u.ID, "s2"))
	if tt, _ := s.UserTOTP(u.ID); tt.Secret != "s2" || tt.Enabled {
		t.Fatalf("pending totp: %+v", tt)
	}
	must(t, s.EnableTOTP(u.ID, 5, []string{"c1", "c2"}))
	must(t, s.SetPendingTOTP(u.ID, "s3"))
	if tt, _ := s.UserTOTP(u.ID); tt.Secret != "s2" || !tt.Enabled {
		t.Fatalf("enabled totp replaced: %+v", tt)
	}

	// Case-insensitive unique names; an update to the same values is not "not found".
	g := &Group{Name: "Web"}
	must(t, s.CreateGroup(g))
	if err := s.CreateGroup(&Group{Name: "web"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate group: %v", err)
	}
	must(t, s.UpdateGroup(g))
	tag := &Tag{Name: "Prod"}
	must(t, s.CreateTag(tag))
	if err := s.CreateTag(&Tag{Name: "PROD"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate tag: %v", err)
	}
	must(t, s.CreateGroup(&Group{Name: "api"}))
	gs, _ := s.ListGroups()
	if len(gs) != 2 || gs[0].Name != "api" {
		t.Fatalf("groups order: %+v", gs)
	}

	// Monitors with repeated tags (INSERT OR IGNORE) and empty push tokens (partial unique index).
	var ms []*Monitor
	for _, name := range []string{"b", "A"} {
		m := &Monitor{Name: name, Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}, GroupID: g.ID, TagIDs: []int64{tag.ID, tag.ID}}
		must(t, s.CreateMonitor(m))
		ms = append(ms, m)
	}
	all, _ := s.ListMonitors()
	if len(all) != 2 || all[0].Name != "A" || len(all[0].TagIDs) != 1 {
		t.Fatalf("monitors: %+v", all)
	}
	must(t, s.SetPushToken(ms[0].ID, "tok"))
	if err := s.SetPushToken(ms[1].ID, "tok"); err == nil {
		t.Fatal("duplicate push token accepted")
	}

	// Status pages: several without a domain, one domain only once.
	for _, slug := range []string{"one", "two"} {
		must(t, s.CreateStatusPage(&StatusPage{Slug: slug, Title: slug, MonitorIDs: []int64{ms[0].ID}}))
	}
	must(t, s.CreateStatusPage(&StatusPage{Slug: "three", Title: "three", Domain: "status.example.com"}))
	if err := s.CreateStatusPage(&StatusPage{Slug: "four", Title: "four", Domain: "status.example.com"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate domain: %v", err)
	}
	if err := s.CreateStatusPage(&StatusPage{Slug: "one", Title: "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate slug: %v", err)
	}

	// Expiry check upsert.
	for _, d := range []int{30, 7} {
		must(t, s.SaveExpiryCheck(&ExpiryCheck{MonitorID: ms[0].ID, Kind: ExpiryCert, ExpiresAt: 1, AlertedDays: d}))
	}
	if ec, _ := s.ExpiryChecks(ms[0].ID); len(ec) != 1 || ec[0].AlertedDays != 7 {
		t.Fatalf("expiry: %+v", ec)
	}

	// Results: ids from a batch, integer buckets, stats, latest per location, pruning.
	base := time.Now().UnixMilli() - 10*60_000
	base -= base % 60_000
	var rows []*ResultRow
	for i := range 10 {
		for _, loc := range []string{"local", "eu"} {
			rows = append(rows, &ResultRow{MonitorID: ms[0].ID, Location: loc, TS: base + int64(i)*60_000 + 1, OK: i%5 != 0, LatencyMs: float64(10 + i)})
		}
	}
	must(t, s.InsertResults(rows))
	if rows[0].ID == 0 || rows[1].ID == rows[0].ID {
		t.Fatalf("batch ids: %d %d", rows[0].ID, rows[1].ID)
	}
	bs, err := s.Buckets(ms[0].ID, base, base+10*60_000, 5*60_000, false)
	if err != nil || len(bs) != 2 || bs[0].Total != 10 || bs[0].OK != 8 || bs[1].Start != base+5*60_000 {
		t.Fatalf("buckets: %+v %v", bs, err)
	}
	if bs[0].MinMs != 11 || bs[0].MaxMs != 14 {
		t.Fatalf("bucket latency: %+v", bs[0])
	}
	cells, err := s.ResultStats(base, base+10*60_000, 5*60_000)
	if err != nil || len(cells) != 2 || cells[1].Total != 10 || cells[1].UpLatN != 8 {
		t.Fatalf("stats: %+v %v", cells, err)
	}
	up, avg, err := s.Uptime(ms[0].ID, base)
	if err != nil || up != 0.8 || avg <= 10 {
		t.Fatalf("uptime: %v %v %v", up, avg, err)
	}
	latest, _ := s.LatestPerLocation(ms[0].ID)
	if len(latest) != 2 {
		t.Fatalf("latest: %+v", latest)
	}
	n, err := s.RollupResults(time.UnixMilli(base+3*RollupHour), nil)
	if err != nil || n == 0 {
		t.Fatalf("rollup: %d %v", n, err)
	}
	if n, err := s.PruneResults(base + 5*60_000); err != nil || n != 10 {
		t.Fatalf("prune: %d %v", n, err)
	}
	if n, err := s.PruneRollups(base + 3*RollupHour); err != nil || n == 0 {
		t.Fatalf("prune rollups: %d %v", n, err)
	}

	// Deleting a group sets group_id to NULL; deleting a monitor cascades.
	must(t, s.DeleteGroup(g.ID))
	if m, _ := s.Monitor(ms[0].ID); m.GroupID != 0 {
		t.Fatalf("group not cleared: %d", m.GroupID)
	}
	must(t, s.DeleteMonitor(ms[0].ID))
	if r, _ := s.RecentResults(ms[0].ID, 10); len(r) != 0 {
		t.Fatalf("results left: %d", len(r))
	}
	must(t, s.ClearConfig())
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestParseDSN(t *testing.T) {
	for _, c := range []struct{ in, driver, conn string }{
		{"/data/pingtxt.db", DriverSQLite, "/data/pingtxt.db"},
		{"sqlite:///data/pingtxt.db", DriverSQLite, "/data/pingtxt.db"},
		{"postgres://u:p@db:5432/pingtxt?sslmode=disable", DriverPostgres, "postgres://u:p@db:5432/pingtxt?sslmode=disable"},
		{"mariadb://u:p%40ss@db/pingtxt", DriverMySQL, "u:p@ss@tcp(db:3306)/pingtxt?clientFoundRows=true&charset=utf8mb4"},
		{"mysql://u:p@db:3307/pingtxt?tls=true", DriverMySQL, "u:p@tcp(db:3307)/pingtxt?clientFoundRows=true&tls=true&charset=utf8mb4"},
	} {
		d, conn, err := parseDSN(c.in)
		if err != nil || d != c.driver || conn != c.conn {
			t.Errorf("%s: %s %s %v", c.in, d, conn, err)
		}
	}
	if _, _, err := parseDSN("redis://x"); err == nil {
		t.Error("unknown scheme accepted")
	}
}

func TestRebind(t *testing.T) {
	pg := &db{driver: DriverPostgres}
	if got := pg.rebind(`INSERT OR IGNORE INTO t (a, b) VALUES (?, ?)`); got != `INSERT INTO t (a, b) VALUES ($1, $2) ON CONFLICT DO NOTHING` {
		t.Errorf("postgres: %s", got)
	}
	my := &db{driver: DriverMySQL}
	got := my.rebind(`INSERT INTO settings ("key", value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`)
	if got != "INSERT INTO settings (`key`, value) VALUES (?, ?) ON DUPLICATE KEY UPDATE value = VALUES(value)" {
		t.Errorf("mysql: %s", got)
	}
	if got := my.rebind(`SELECT a FROM results INDEXED BY results_ts WHERE ts > ?`); got != `SELECT a FROM results WHERE ts > ?` {
		t.Errorf("indexed by: %s", got)
	}
}

func TestBaseVersion(t *testing.T) {
	if len(sqliteMigrations) != baseVersion {
		t.Fatalf("baseSchema matches migration %d but there are %d SQLite-only migrations: append new ones to laterMigrations", baseVersion, len(sqliteMigrations))
	}
}

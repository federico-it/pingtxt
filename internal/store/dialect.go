package store

import (
	"database/sql"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

// Driver names, as returned by Store.Driver.
const (
	DriverSQLite   = "sqlite"
	DriverPostgres = "postgres"
	DriverMySQL    = "mysql" // MariaDB and MySQL
)

// The queries in this package are written for SQLite with ? placeholders.
// db rewrites them for PostgreSQL and MariaDB/MySQL, so most of the code
// does not care which database is behind it. The few queries that really
// differ (upserts, chunked deletes, integer division) ask db for the right
// fragment or branch on db.driver.
type db struct {
	*sql.DB
	driver string
	cache  sync.Map // query -> rewritten query
}

type tx struct {
	*sql.Tx
	db *db
}

var (
	reIndexedBy  = regexp.MustCompile(`\s+INDEXED BY \w+`)
	reOnConflict = regexp.MustCompile(`ON CONFLICT\s*\([^)]*\)\s*DO UPDATE SET`)
	reExcluded   = regexp.MustCompile(`excluded\.(\w+)`)
	reOrIgnore   = regexp.MustCompile(`^(\s*)INSERT OR IGNORE INTO`)
)

// rebind rewrites a SQLite query for the current driver.
func (d *db) rebind(q string) string {
	if d.driver == DriverSQLite {
		return q
	}
	if v, ok := d.cache.Load(q); ok {
		return v.(string)
	}
	out := reIndexedBy.ReplaceAllString(q, "")
	switch d.driver {
	case DriverPostgres:
		if reOrIgnore.MatchString(out) {
			out = reOrIgnore.ReplaceAllString(out, "${1}INSERT INTO") + " ON CONFLICT DO NOTHING"
		}
		// ? -> $1, $2, ... (no query in this package has a ? inside a string literal).
		var b strings.Builder
		n := 0
		for i := 0; i < len(out); i++ {
			if out[i] == '?' {
				n++
				b.WriteByte('$')
				b.WriteString(strconv.Itoa(n))
				continue
			}
			b.WriteByte(out[i])
		}
		out = b.String()
	case DriverMySQL:
		out = reOrIgnore.ReplaceAllString(out, "${1}INSERT IGNORE INTO")
		out = reOnConflict.ReplaceAllString(out, "ON DUPLICATE KEY UPDATE")
		out = reExcluded.ReplaceAllString(out, "VALUES($1)")
		// Identifiers are quoted with "..." (SQLite, PostgreSQL); MySQL wants `...`.
		out = strings.ReplaceAll(out, `"`, "`")
	}
	d.cache.Store(q, out)
	return out
}

func (d *db) Exec(q string, args ...any) (sql.Result, error) { return d.DB.Exec(d.rebind(q), args...) }
func (d *db) Query(q string, args ...any) (*sql.Rows, error) { return d.DB.Query(d.rebind(q), args...) }
func (d *db) QueryRow(q string, args ...any) *sql.Row        { return d.DB.QueryRow(d.rebind(q), args...) }

func (d *db) Begin() (*tx, error) {
	t, err := d.DB.Begin()
	if err != nil {
		return nil, err
	}
	return &tx{Tx: t, db: d}, nil
}

func (t *tx) Exec(q string, args ...any) (sql.Result, error) {
	return t.Tx.Exec(t.db.rebind(q), args...)
}
func (t *tx) Query(q string, args ...any) (*sql.Rows, error) {
	return t.Tx.Query(t.db.rebind(q), args...)
}
func (t *tx) QueryRow(q string, args ...any) *sql.Row { return t.Tx.QueryRow(t.db.rebind(q), args...) }
func (t *tx) Prepare(q string) (*sql.Stmt, error)     { return t.Tx.Prepare(t.db.rebind(q)) }

type querier interface {
	Exec(q string, args ...any) (sql.Result, error)
	QueryRow(q string, args ...any) *sql.Row
}

// insert runs an INSERT into a table with an id column and returns the new id.
// PostgreSQL has no LastInsertId, so it gets RETURNING id.
func (d *db) insert(x querier, q string, args ...any) (int64, error) {
	if d.driver == DriverPostgres {
		var id int64
		err := x.QueryRow(q+` RETURNING id`, args...).Scan(&id)
		return id, err
	}
	res, err := x.Exec(q, args...)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// idiv is integer division of two SQL expressions.
func (d *db) idiv(a, b string) string {
	if d.driver == DriverMySQL {
		return "(" + a + ") DIV (" + b + ")"
	}
	return "(" + a + ") / (" + b + ")"
}

// greatest is the larger of two SQL expressions.
func (d *db) greatest(a, b string) string {
	if d.driver == DriverSQLite {
		return "MAX(" + a + ", " + b + ")"
	}
	return "GREATEST(" + a + ", " + b + ")"
}

// isUnique reports whether err is a unique constraint violation.
func isUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") || // SQLite
		strings.Contains(msg, "SQLSTATE 23505") || strings.Contains(msg, "duplicate key value") || // PostgreSQL
		strings.Contains(msg, "Error 1062") // MySQL / MariaDB: duplicate entry
}

// splitStatements splits a migration on ";" at line ends. Migrations have no
// ";" inside string literals.
func splitStatements(sqlText string) []string {
	var out []string
	for _, s := range strings.Split(sqlText, ";") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// execScript runs every statement of a migration.
func (t *tx) execScript(sqlText string) error {
	if t.db.driver == DriverSQLite {
		_, err := t.Tx.Exec(sqlText)
		return err
	}
	for _, s := range splitStatements(sqlText) {
		if _, err := t.Tx.Exec(s); err != nil {
			return fmt.Errorf("%w\n%s", err, s)
		}
	}
	return nil
}

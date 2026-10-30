package check

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq" // registers the "postgres" driver
)

// runSQL connects to PostgreSQL or MySQL/MariaDB and runs a query
// (SELECT 1 by default). When j.Keyword is set, the first column of the
// first row must equal it.
func runSQL(ctx context.Context, j Job) Result {
	driver, dsn, err := sqlDSN(j)
	if err != nil {
		return Result{Message: err.Error()}
	}
	query := strings.TrimSpace(j.Body)
	if query == "" {
		query = "SELECT 1"
	}
	start := time.Now()
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return Result{Message: cleanErr(err)}
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: sqlErr(ctx, err)}
	}
	defer rows.Close()
	var first sql.NullString
	gotRow := false
	if rows.Next() {
		gotRow = true
		cols, _ := rows.Columns()
		dest := make([]any, len(cols))
		for i := range dest {
			dest[i] = new(sql.RawBytes)
		}
		if len(dest) > 0 {
			dest[0] = &first
		}
		if err := rows.Scan(dest...); err != nil {
			return Result{LatencyMs: ms(time.Since(start)), Message: sqlErr(ctx, err)}
		}
	}
	if err := rows.Err(); err != nil {
		return Result{LatencyMs: ms(time.Since(start)), Message: sqlErr(ctx, err)}
	}
	lat := ms(time.Since(start))

	if j.Keyword != "" {
		switch {
		case !gotRow:
			return Result{LatencyMs: lat, Message: "query returned no rows, expected " + j.Keyword}
		case !first.Valid:
			return Result{LatencyMs: lat, Message: "query returned NULL, expected " + j.Keyword}
		case strings.TrimSpace(first.String) != j.Keyword:
			return Result{LatencyMs: lat, Message: fmt.Sprintf("query returned %q, expected %q", first.String, j.Keyword)}
		}
		return Result{OK: true, LatencyMs: lat, Message: "query returned " + first.String}
	}
	if gotRow && first.Valid {
		return Result{OK: true, LatencyMs: lat, Message: "query ok: " + first.String}
	}
	return Result{OK: true, LatencyMs: lat, Message: "query ok"}
}

func sqlErr(ctx context.Context, err error) string {
	if ctx.Err() != nil {
		return "timeout"
	}
	return cleanErr(err)
}

// sqlDSN turns the monitor target into a driver name and DSN.
//
//	postgres://user:pass@host:5432/db?sslmode=disable
//	mysql://user:pass@host:3306/db?tls=true
func sqlDSN(j Job) (driver, dsn string, err error) {
	u, err := url.Parse(j.Target)
	if err != nil || u.Host == "" {
		return "", "", errors.New("target must be a " + j.Type + ":// URL")
	}
	switch j.Type {
	case TypePostgres:
		q := u.Query()
		if q.Get("connect_timeout") == "" {
			q.Set("connect_timeout", fmt.Sprint(max(1, j.TimeoutMs/1000)))
		}
		if q.Get("sslmode") == "" && j.IgnoreTLS {
			q.Set("sslmode", "require") // encrypted, certificate not verified
		}
		if q.Get("sslmode") == "" {
			q.Set("sslmode", "prefer")
		}
		u.RawQuery = q.Encode()
		u.Scheme = "postgres"
		return "postgres", u.String(), nil
	case TypeMySQL:
		c := mysql.NewConfig()
		c.Net = "tcp"
		c.Addr = u.Host
		if u.Port() == "" {
			c.Addr = net.JoinHostPort(u.Hostname(), "3306")
		}
		c.User = u.User.Username()
		c.Passwd, _ = u.User.Password()
		c.DBName = strings.TrimPrefix(u.Path, "/")
		c.Timeout = time.Duration(max(j.TimeoutMs, 500)) * time.Millisecond
		c.ReadTimeout = c.Timeout
		c.WriteTimeout = c.Timeout
		c.AllowNativePasswords = true
		c.AllowCleartextPasswords = false
		q := u.Query()
		switch tls := q.Get("tls"); {
		case j.IgnoreTLS && tls != "false":
			c.TLSConfig = "skip-verify"
		case tls != "":
			c.TLSConfig = tls // true, false, skip-verify, preferred
		default:
			c.TLSConfig = "preferred"
		}
		return "mysql", c.FormatDSN(), nil
	}
	return "", "", errors.New("not a sql monitor")
}

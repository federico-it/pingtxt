package store

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/url"
	"time"
)

// OpenScratch creates an empty database on the PostgreSQL or MariaDB/MySQL
// server of baseURL, opens it and returns a function that closes and drops
// it. The user needs the right to create databases. Used by tests, so that
// parallel test packages each get their own database.
func OpenScratch(baseURL string) (*Store, func(), error) {
	driver, conn, err := parseDSN(baseURL)
	if err != nil {
		return nil, nil, err
	}
	if driver == DriverSQLite {
		return nil, nil, fmt.Errorf("OpenScratch needs a postgres:// or mysql:// url")
	}
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "pingtxt_test_" + hex.EncodeToString(b)
	admin, err := sql.Open(driver, conn)
	if err != nil {
		return nil, nil, err
	}
	if err := waitReady(admin, 30*time.Second); err != nil {
		admin.Close()
		return nil, nil, err
	}
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		admin.Close()
		return nil, nil, err
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		admin.Close()
		return nil, nil, err
	}
	u.Path = "/" + name
	s, err := Open(u.String())
	drop := func() {
		if s != nil {
			s.Close()
		}
		q := `DROP DATABASE IF EXISTS ` + name
		if driver == DriverPostgres {
			q += ` WITH (FORCE)`
		}
		_, _ = admin.Exec(q)
		admin.Close()
	}
	if err != nil {
		drop()
		return nil, nil, err
	}
	return s, drop, nil
}

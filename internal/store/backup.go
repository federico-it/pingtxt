package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"
)

// Path returns the database file path.
func (s *Store) Path() string { return s.path }

// SchemaVersion is the number of migrations this build knows.
func SchemaVersion() int { return len(migrations) }

// Snapshot writes a consistent copy of the database to dst with VACUUM INTO.
// It reads through its own connection, so the hub keeps writing results
// while the copy is made (WAL mode). dst must not exist.
func (s *Store) Snapshot(dst string) error {
	if s.db.driver != DriverSQLite {
		return errors.New("snapshots need SQLite: use pg_dump or mariadb-dump")
	}
	return SnapshotFile(s.path, dst)
}

// SnapshotFile writes a consistent copy of the database at src to dst.
// Another process (a running hub) may keep using src meanwhile.
func SnapshotFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return err
	}
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)", src))
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	// Written next to dst, then renamed: a half-written file never looks like a backup.
	tmp := dst + ".tmp"
	_ = os.Remove(tmp)
	if _, err := db.Exec(`VACUUM INTO ?`, tmp); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("snapshot: %w", err)
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// BackupInfo describes a database file checked by ValidateBackup.
type BackupInfo struct {
	SchemaVersion int `json:"schema_version"`
	Monitors      int `json:"monitors"`
	Users         int `json:"users"`
}

// ValidateBackup checks that path is a pingtxt database this build can open:
// a healthy SQLite file with a schema not newer than this build.
func ValidateBackup(path string) (*BackupInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	head := make([]byte, 16)
	_, err = io.ReadFull(f, head)
	f.Close()
	if err != nil || string(head) != "SQLite format 3\x00" {
		return nil, errors.New("not a SQLite database")
	}
	db, err := sql.Open("sqlite", fmt.Sprintf("file:%s?mode=ro", path))
	if err != nil {
		return nil, err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	var check string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&check); err != nil {
		return nil, fmt.Errorf("damaged database: %w", err)
	}
	if check != "ok" {
		return nil, fmt.Errorf("damaged database: %s", check)
	}
	info := &BackupInfo{}
	if err := db.QueryRow(`SELECT version FROM schema_version`).Scan(&info.SchemaVersion); err != nil {
		return nil, errors.New("not a pingtxt database")
	}
	if info.SchemaVersion > len(migrations) {
		return nil, fmt.Errorf("backup is from a newer pingtxt (schema %d, this build knows %d): upgrade first", info.SchemaVersion, len(migrations))
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM monitors`).Scan(&info.Monitors); err != nil {
		return nil, errors.New("not a pingtxt database")
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&info.Users); err != nil {
		return nil, errors.New("not a pingtxt database")
	}
	return info, nil
}

// Restore replaces the database at dbPath with the backup src. The hub must
// not be running. The current database is first saved as a snapshot in
// keepDir (returned), so a restore can be undone. Older schemas are migrated
// on the next Open.
func Restore(dbPath, src, keepDir string) (string, error) {
	if _, err := ValidateBackup(src); err != nil {
		return "", err
	}
	var kept string
	if _, err := os.Stat(dbPath); err == nil {
		kept = filepath.Join(keepDir, BackupName("pre-restore", time.Now()))
		if err := SnapshotFile(dbPath, kept); err != nil {
			return "", fmt.Errorf("save current database: %w", err)
		}
	}
	// Copy next to the database, then swap: never leaves a half-written file.
	tmp := dbPath + ".restoring"
	if err := CopyFile(src, tmp); err != nil {
		os.Remove(tmp)
		return kept, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := os.Remove(dbPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			os.Remove(tmp)
			return kept, err
		}
	}
	if err := os.Rename(tmp, dbPath); err != nil {
		os.Remove(tmp)
		return kept, err
	}
	return kept, nil
}

// BackupName returns the file name of a backup of the given kind
// (auto, manual, pre-import, pre-restore, upload) taken at t.
func BackupName(kind string, t time.Time) string {
	return "pingtxt-" + kind + "-" + t.UTC().Format("20060102-150405") + ".db"
}

// CopyFile copies src to dst (mode 0600), syncing it to disk.
func CopyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	if err := out.Sync(); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// ClearConfig deletes every monitor (with its history), group, tag,
// notification channel, maintenance window and status page. Used by a
// "replace" configuration import. Agents, users and settings are kept.
func (s *Store) ClearConfig() error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"status_pages", "maintenances", "monitors", "monitor_groups", "tags", "notifiers"} {
		if _, err := tx.Exec(`DELETE FROM ` + t); err != nil {
			return err
		}
	}
	return tx.Commit()
}

package hub

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Database backups are consistent SQLite snapshots (VACUUM INTO) kept in
// <data>/backups. They hold everything: settings, users, secrets and history.
//
// Restoring replaces the database file, which cannot be done under a running
// hub: the web UI stages the chosen backup as <data>/pingtxt.db.restore and
// the hub swaps it in on its next start (ApplyPendingRestore). From a shell,
// `pingtxt -restore <file>` does the same with the hub stopped.

const keyBackups = "backups"

// BackupSettings control scheduled backups.
type BackupSettings struct {
	Enabled       bool `json:"enabled"`
	IntervalHours int  `json:"interval_hours"`
	Keep          int  `json:"keep"` // scheduled backups to keep; older ones are deleted
}

func (b *BackupSettings) normalize() {
	if b.IntervalHours <= 0 {
		b.IntervalHours = 24
	}
	b.IntervalHours = min(b.IntervalHours, 24*30)
	if b.Keep <= 0 {
		b.Keep = 7
	}
	b.Keep = min(b.Keep, 365)
}

// Backup kinds, part of the file name.
const (
	backupAuto       = "auto"
	backupManual     = "manual"
	backupPreImport  = "pre-import"
	backupPreRestore = "pre-restore"
	backupUpload     = "upload"
)

var backupNameRe = regexp.MustCompile(`^pingtxt-(auto|manual|pre-import|pre-restore|upload)-(\d{8}-\d{6})(?:-(\d+))?\.db$`)

// maxUploadBytes caps an uploaded backup.
const maxUploadBytes = 4 << 30

// BackupDir is where backups are kept.
func BackupDir(dataDir string) string { return filepath.Join(dataDir, "backups") }

// PendingRestorePath is the staged backup applied at the next start.
func PendingRestorePath(dbPath string) string { return dbPath + ".restore" }

// ApplyPendingRestore swaps in a backup staged from the web UI. Call it
// before opening the database. It returns the name of the saved copy of the
// replaced database, or "" when nothing was staged.
func ApplyPendingRestore(dbPath, dataDir string) (string, error) {
	staged := PendingRestorePath(dbPath)
	if _, err := os.Stat(staged); errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	kept, err := store.Restore(dbPath, staged, BackupDir(dataDir))
	if err != nil {
		// Leave the staged file out of the way so the hub can still start.
		_ = os.Rename(staged, staged+".failed")
		return "", err
	}
	_ = os.Remove(staged)
	_ = os.Remove(staged + ".name")
	return filepath.Base(kept), nil
}

func (h *Hub) backupDir() string { return BackupDir(h.cfg.DataDir) }

func (h *Hub) backupSettings() BackupSettings {
	b := BackupSettings{}
	if err := h.store.GetSetting(keyBackups, &b); err != nil && !errors.Is(err, store.ErrNotFound) {
		h.log.Error("load backup settings", "err", err)
	}
	b.normalize()
	return b
}

// CreateBackup writes a snapshot of the database to the backups folder.
func (h *Hub) CreateBackup(kind string) (*BackupFile, error) {
	h.backupMu.Lock()
	defer h.backupMu.Unlock()
	now := time.Now()
	name := store.BackupName(kind, now)
	dir := h.backupDir()
	// Two backups in the same second get a counter.
	for i := 2; fileExists(filepath.Join(dir, name)); i++ {
		name = strings.TrimSuffix(store.BackupName(kind, now), ".db") + fmt.Sprintf("-%d.db", i)
	}
	if err := h.store.Snapshot(filepath.Join(dir, name)); err != nil {
		return nil, err
	}
	return backupFile(dir, name)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// BackupFile is one file in the backups folder.
type BackupFile struct {
	Name      string `json:"name"`
	Kind      string `json:"kind"`
	Size      int64  `json:"size"`
	CreatedAt int64  `json:"created_at"`
	seq       int    // counter of backups taken in the same second
}

func backupFile(dir, name string) (*BackupFile, error) {
	m := backupNameRe.FindStringSubmatch(name)
	if m == nil {
		return nil, errors.New("not a backup file")
	}
	fi, err := os.Stat(filepath.Join(dir, name))
	if err != nil {
		return nil, err
	}
	t, err := time.Parse("20060102-150405", m[2])
	if err != nil {
		t = fi.ModTime()
	}
	seq, _ := strconv.Atoi(m[3])
	return &BackupFile{Name: name, Kind: m[1], Size: fi.Size(), CreatedAt: t.UnixMilli(), seq: seq}, nil
}

// ListBackups returns the backups, newest first.
func (h *Hub) ListBackups() ([]*BackupFile, error) {
	dir := h.backupDir()
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []*BackupFile{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []*BackupFile{}
	for _, e := range entries {
		if e.IsDir() || !backupNameRe.MatchString(e.Name()) {
			continue
		}
		if b, err := backupFile(dir, e.Name()); err == nil {
			out = append(out, b)
		}
	}
	slices.SortFunc(out, func(a, b *BackupFile) int {
		if a.CreatedAt != b.CreatedAt {
			return cmp.Compare(b.CreatedAt, a.CreatedAt)
		}
		return cmp.Compare(b.seq, a.seq)
	})
	return out, nil
}

// backupLoop takes scheduled backups and deletes the old ones.
func (h *Hub) backupLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.scheduledBackup(now)
		}
	}
}

func (h *Hub) scheduledBackup(now time.Time) {
	s := h.backupSettings()
	if !s.Enabled {
		return
	}
	list, err := h.ListBackups()
	if err != nil {
		h.log.Error("list backups", "err", err)
		return
	}
	var auto []*BackupFile
	for _, b := range list {
		if b.Kind == backupAuto {
			auto = append(auto, b)
		}
	}
	due := len(auto) == 0 || now.Sub(time.UnixMilli(auto[0].CreatedAt)) >= time.Duration(s.IntervalHours)*time.Hour
	if due {
		t0 := time.Now()
		b, err := h.CreateBackup(backupAuto)
		if err != nil {
			h.log.Error("scheduled backup", "err", err)
			return
		}
		h.log.Info("scheduled backup", "file", b.Name, "bytes", b.Size, "took", time.Since(t0).Round(time.Millisecond))
		auto = append([]*BackupFile{b}, auto...)
	}
	for _, b := range auto[min(len(auto), s.Keep):] {
		if err := os.Remove(filepath.Join(h.backupDir(), b.Name)); err != nil {
			h.log.Error("delete old backup", "file", b.Name, "err", err)
		}
	}
}

// ---------- API ----------

type backupsView struct {
	Settings BackupSettings `json:"settings"`
	Backups  []*BackupFile  `json:"backups"`
	Dir      string         `json:"dir"`
	// PendingRestore is set when a backup is staged for the next start.
	PendingRestore *pendingRestore `json:"pending_restore"`
}

type pendingRestore struct {
	Name     string `json:"name"`
	StagedAt int64  `json:"staged_at"`
}

func (h *Hub) pendingRestore() *pendingRestore {
	fi, err := os.Stat(PendingRestorePath(h.store.Path()))
	if err != nil {
		return nil
	}
	name, _ := os.ReadFile(PendingRestorePath(h.store.Path()) + ".name")
	return &pendingRestore{Name: strings.TrimSpace(string(name)), StagedAt: fi.ModTime().UnixMilli()}
}

func (h *Hub) listBackups(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := h.ListBackups()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, backupsView{Settings: h.backupSettings(), Backups: list, Dir: h.backupDir(), PendingRestore: h.pendingRestore()})
}

func (h *Hub) createBackup(w http.ResponseWriter, r *http.Request, _ *store.User) {
	b, err := h.CreateBackup(backupManual)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 201, b)
}

func (h *Hub) putBackupSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var s BackupSettings
	if readJSON(r, &s) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	s.normalize()
	if err := h.store.SetSetting(keyBackups, s); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, s)
}

// backupPath returns the path of a backup named in the URL, refusing
// anything that is not a plain backup file name.
func (h *Hub) backupPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if !backupNameRe.MatchString(name) {
		writeErr(w, 400, "invalid backup name")
		return "", false
	}
	p := filepath.Join(h.backupDir(), name)
	if !fileExists(p) {
		writeErr(w, 404, "backup not found")
		return "", false
	}
	return p, true
}

func (h *Hub) downloadBackup(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := h.backupPath(w, r)
	if !ok {
		return
	}
	f, err := os.Open(p)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(p)+`"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeContent(w, r, "", fi.ModTime(), f)
}

func (h *Hub) deleteBackup(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := h.backupPath(w, r)
	if !ok {
		return
	}
	if err := os.Remove(p); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// uploadBackup stores a database file sent as the raw request body
// (Content-Type: application/octet-stream) in the backups folder.
func (h *Hub) uploadBackup(w http.ResponseWriter, r *http.Request, _ *store.User) {
	if err := os.MkdirAll(h.backupDir(), 0o700); err != nil {
		h.internalErr(w, err)
		return
	}
	name := store.BackupName(backupUpload, time.Now())
	for i := 2; fileExists(filepath.Join(h.backupDir(), name)); i++ {
		name = strings.TrimSuffix(store.BackupName(backupUpload, time.Now()), ".db") + fmt.Sprintf("-%d.db", i)
	}
	dst := filepath.Join(h.backupDir(), name)
	tmp := dst + ".tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	_, err = io.Copy(f, http.MaxBytesReader(w, r.Body, maxUploadBytes))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp)
		writeErr(w, 400, "upload failed: "+err.Error())
		return
	}
	info, err := store.ValidateBackup(tmp)
	if err != nil {
		os.Remove(tmp)
		writeErr(w, 400, err.Error())
		return
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		h.internalErr(w, err)
		return
	}
	b, err := backupFile(h.backupDir(), name)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 201, map[string]any{"backup": b, "info": info})
}

// stageRestore copies a backup to <db>.restore; the hub swaps it in at the next start.
func (h *Hub) stageRestore(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p, ok := h.backupPath(w, r)
	if !ok {
		return
	}
	info, err := store.ValidateBackup(p)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	staged := PendingRestorePath(h.store.Path())
	tmp := staged + ".tmp"
	if err := store.CopyFile(p, tmp); err != nil {
		os.Remove(tmp)
		h.internalErr(w, err)
		return
	}
	if err := os.Rename(tmp, staged); err != nil {
		os.Remove(tmp)
		h.internalErr(w, err)
		return
	}
	_ = os.WriteFile(staged+".name", []byte(filepath.Base(p)), 0o600)
	writeJSON(w, 200, map[string]any{"pending_restore": h.pendingRestore(), "info": info})
}

func (h *Hub) cancelRestore(w http.ResponseWriter, r *http.Request, _ *store.User) {
	staged := PendingRestorePath(h.store.Path())
	if err := os.Remove(staged); err != nil && !errors.Is(err, os.ErrNotExist) {
		h.internalErr(w, err)
		return
	}
	_ = os.Remove(staged + ".name")
	writeJSON(w, 200, map[string]bool{"ok": true})
}

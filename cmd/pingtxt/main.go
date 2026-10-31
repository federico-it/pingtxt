// Command pingtxt runs the hub: API, scheduler and web UI in one binary.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
	_ "time/tzdata" // maintenance windows use IANA time zones

	"github.com/federico-it/pingtxt/internal/hub"
	"github.com/federico-it/pingtxt/internal/store"
	"github.com/federico-it/pingtxt/internal/ui"
)

var version = "dev"

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	enablePassword := flag.Bool("enable-password-login", false, "turn password login back on (if single sign-on is broken) and exit")
	resetTwoFactor := flag.String("reset-2fa", "", "turn two-factor authentication off for the user with this email and exit")
	makeAdmin := flag.String("make-admin", "", "give the admin role to the user with this email and exit")
	backupTo := flag.String("backup", "", "write a consistent copy of the database to this file and exit (safe while the hub runs)")
	restoreFrom := flag.String("restore", "", "replace the database with this backup file and exit (stop the hub first)")
	exportTo := flag.String("export-config", "", "write the configuration as JSON to this file (- for stdout) and exit")
	exportSecrets := flag.Bool("export-secrets", false, "with -export-config: include passwords, tokens and secret webhook URLs")
	importFrom := flag.String("import-config", "", "apply a configuration JSON file and exit (restart the hub afterwards)")
	importMode := flag.String("import-mode", "merge", "with -import-config: merge (update by name, add the rest) or replace (delete current monitors first)")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	retention, _ := strconv.Atoi(env("PINGTXT_RETENTION_DAYS", "30"))
	// Only the address, data dir, database URL and agent dir are read at every start. The other
	// values seed the database on first start; then they are edited in the web UI.
	cfg := hub.Config{
		Addr:          env("PINGTXT_ADDR", ":8090"),
		DataDir:       env("PINGTXT_DATA", "./data"),
		RetentionDays: retention,
		PublicURL:     os.Getenv("PINGTXT_PUBLIC_URL"),
		AgentDir:      env("PINGTXT_AGENT_DIR", "./dist/agents"),
		LocalName:     env("PINGTXT_LOCAL_NAME", "hub"),
		DisableLocal:  os.Getenv("PINGTXT_DISABLE_LOCAL") == "true",
		// Off for offline or privacy-minded installs (first start only, then in settings).
		DisableUpdateCheck: os.Getenv("PINGTXT_DISABLE_UPDATE_CHECK") == "true",
		Version:            version,
	}

	// PINGTXT_DB_URL selects PostgreSQL (postgres://) or MariaDB/MySQL
	// (mysql://, mariadb://). Without it the hub uses SQLite in the data dir.
	dbURL := os.Getenv("PINGTXT_DB_URL")
	dbPath := filepath.Join(cfg.DataDir, "pingtxt.db")
	if dbURL != "" && (*restoreFrom != "" || *backupTo != "") {
		log.Error("-backup and -restore work with SQLite only: with PINGTXT_DB_URL use pg_dump / mariadb-dump (or -export-config)")
		os.Exit(1)
	}

	if *restoreFrom != "" {
		kept, err := store.Restore(dbPath, *restoreFrom, hub.BackupDir(cfg.DataDir))
		if err != nil {
			log.Error("restore", "err", err)
			os.Exit(1)
		}
		if kept != "" {
			fmt.Println("previous database saved as " + kept)
		}
		fmt.Println("database restored from " + *restoreFrom + ". Start the hub.")
		return
	}
	if *backupTo != "" {
		if err := store.SnapshotFile(dbPath, *backupTo); err != nil {
			log.Error("backup", "err", err)
			os.Exit(1)
		}
		fmt.Println("backup written to " + *backupTo)
		return
	}

	if dbURL == "" {
		// A restore chosen in the web UI is applied before opening the database.
		if kept, err := hub.ApplyPendingRestore(dbPath, cfg.DataDir); err != nil {
			log.Error("restore staged backup failed, starting with the current database", "err", err)
		} else if kept != "" {
			log.Info("restored staged backup", "previous_database", kept)
		}
	}

	dsn := dbPath
	if dbURL != "" {
		dsn = dbURL
	}
	st, err := store.Open(dsn)
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer st.Close()
	log.Info("database", "driver", st.Driver())

	if *enablePassword {
		h := hub.New(cfg, st, log)
		if err := h.EnablePasswordLogin(); err != nil {
			log.Error("enable password login", "err", err)
			os.Exit(1)
		}
		fmt.Println("password login enabled. Restart the hub if it is running.")
		return
	}

	if *resetTwoFactor != "" {
		h := hub.New(cfg, st, log)
		if err := h.ResetTwoFactor(*resetTwoFactor); err != nil {
			log.Error("reset two-factor", "err", err)
			os.Exit(1)
		}
		fmt.Println("two-factor authentication turned off for " + *resetTwoFactor + ".")
		return
	}

	if *makeAdmin != "" {
		h := hub.New(cfg, st, log)
		if err := h.MakeAdmin(*makeAdmin); err != nil {
			log.Error("make admin", "err", err)
			os.Exit(1)
		}
		fmt.Println(*makeAdmin + " is now an admin.")
		return
	}

	if *exportTo != "" {
		h := hub.New(cfg, st, log)
		doc, err := h.ExportConfig(*exportSecrets)
		if err != nil {
			log.Error("export config", "err", err)
			os.Exit(1)
		}
		b, _ := json.MarshalIndent(doc, "", "  ")
		b = append(b, '\n')
		if *exportTo == "-" {
			os.Stdout.Write(b)
		} else if err := os.WriteFile(*exportTo, b, 0o600); err != nil {
			log.Error("export config", "err", err)
			os.Exit(1)
		}
		return
	}

	if *importFrom != "" {
		if *importMode != "merge" && *importMode != "replace" {
			log.Error("-import-mode must be merge or replace")
			os.Exit(1)
		}
		b, err := os.ReadFile(*importFrom)
		if err != nil {
			log.Error("import config", "err", err)
			os.Exit(1)
		}
		doc, err := hub.ParseConfig(b)
		if err != nil {
			log.Error("import config", "err", err)
			os.Exit(1)
		}
		h := hub.New(cfg, st, log)
		res, err := h.ImportConfig(doc, hub.ImportOptions{Replace: *importMode == "replace", Settings: true, Backup: true})
		if res != nil {
			out, _ := json.MarshalIndent(res, "", "  ")
			fmt.Println(string(out))
		}
		if err != nil {
			log.Error("import config", "err", err)
			os.Exit(1)
		}
		fmt.Println("configuration imported. Restart the hub if it is running.")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	h := hub.New(cfg, st, log)
	go func() {
		if err := h.Run(ctx); err != nil {
			log.Error("scheduler", "err", err)
			stop()
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           h.Handler(ui.FS()),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	log.Info("pingtxt hub listening", "addr", cfg.Addr, "version", version, "data", cfg.DataDir)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Error("http server", "err", err)
		os.Exit(1)
	}
}

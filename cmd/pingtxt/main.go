// Command pingtxt runs the hub: API, scheduler and web UI in one binary.
package main

import (
	"context"
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
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}

	log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	retention, _ := strconv.Atoi(env("PINGTXT_RETENTION_DAYS", "30"))
	cfg := hub.Config{
		Addr:          env("PINGTXT_ADDR", ":8090"),
		DataDir:       env("PINGTXT_DATA", "./data"),
		RetentionDays: retention,
		PublicURL:     os.Getenv("PINGTXT_PUBLIC_URL"),
		AgentDir:      env("PINGTXT_AGENT_DIR", "./dist/agents"),
		LocalName:     env("PINGTXT_LOCAL_NAME", "hub"),
		DisableLocal:  os.Getenv("PINGTXT_DISABLE_LOCAL") == "true",
		Version:       version,
	}

	st, err := store.Open(filepath.Join(cfg.DataDir, "pingtxt.db"))
	if err != nil {
		log.Error("open database", "err", err)
		os.Exit(1)
	}
	defer st.Close()

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

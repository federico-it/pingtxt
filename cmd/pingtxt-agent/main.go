// Command pingtxt-agent is a lightweight probe that connects to a pingtxt hub
// and runs checks from its own network location. It only makes outbound
// connections, so no ports need to be opened.
//
//	PINGTXT_HUB=https://status.example.com PINGTXT_TOKEN=pt_xxx pingtxt-agent
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/protocol"
)

var version = "dev"

func main() {
	hubURL := flag.String("hub", os.Getenv("PINGTXT_HUB"), "hub URL (env PINGTXT_HUB)")
	token := flag.String("token", os.Getenv("PINGTXT_TOKEN"), "agent token (env PINGTXT_TOKEN)")
	concurrency := flag.Int("concurrency", 32, "max parallel checks")
	noUpdate := flag.Bool("no-auto-update", envBool("PINGTXT_NO_AUTO_UPDATE"), "never replace this binary when the hub asks (env PINGTXT_NO_AUTO_UPDATE)")
	insecureUpdate := flag.Bool("insecure-update", envBool("PINGTXT_INSECURE_UPDATE"), "allow self-update from a hub reached over plain http on a public address (env PINGTXT_INSECURE_UPDATE)")
	verifyUpdate := flag.String("verify-update", envOr("PINGTXT_VERIFY_UPDATE", verifyAuto), "check self-updates against the checksums published with the GitHub release: auto, strict or off (env PINGTXT_VERIFY_UPDATE)")
	releaseURL := flag.String("release-url", envOr("PINGTXT_RELEASE_URL", defaultReleaseURL), "base URL of the release downloads, for -verify-update (env PINGTXT_RELEASE_URL)")
	updateDir := flag.String("update-dir", os.Getenv("PINGTXT_UPDATE_DIR"), "in a container, folder (a volume) that keeps self-updates across restarts (env PINGTXT_UPDATE_DIR)")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()
	if *showVersion {
		fmt.Println(version)
		return
	}
	log := slog.New(slog.NewTextHandler(os.Stdout, nil))
	if *hubURL == "" || *token == "" {
		log.Error("hub URL and token are required (PINGTXT_HUB, PINGTXT_TOKEN)")
		os.Exit(2)
	}
	if !validVerifyMode(*verifyUpdate) {
		log.Error("-verify-update must be auto, strict or off", "value", *verifyUpdate)
		os.Exit(2)
	}
	wsURL, err := toWS(*hubURL)
	if err != nil {
		log.Error("invalid hub URL", "err", err)
		os.Exit(2)
	}

	if inContainer() && *updateDir != "" && !*noUpdate {
		// The image's binary records its version for the updated one, then
		// hands over to it if a previous self-update left one in the volume.
		if os.Getenv(envImageVersion) == "" {
			os.Setenv(envImageVersion, version)
		}
		useUpdatedBinary(*updateDir, log)
	} else {
		*updateDir = ""
	}

	exe, noUpdateReason := selfUpdateSupport(*noUpdate, *updateDir)
	if exe != "" && !*insecureUpdate && !secureHub(wsURL) {
		// Over plain http anyone on the path could swap the binary and its
		// checksum: never install code received that way.
		exe, noUpdateReason = "", protocol.NoUpdateInsecure
		log.Warn("self-update disabled: the hub URL is plain http, use https (or -insecure-update)")
	}
	ag := &agent{
		log: log, wsURL: wsURL, httpURL: toHTTP(wsURL), token: *token, concurrency: *concurrency,
		exe: exe, updateDir: *updateDir, noUpdateReason: noUpdateReason, verify: *verifyUpdate, releaseURL: *releaseURL,
		client: &http.Client{Timeout: 10 * time.Minute},
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := ag.session(ctx)
		if errors.Is(err, errRestart) {
			stop()
			log.Info("restarting with the new version")
			err := restart(exe)
			// Exec only returns on failure. The new binary is already in
			// place, so exiting lets the service manager start it.
			log.Error("restart failed", "err", err)
			os.Exit(1)
		}
		if ctx.Err() != nil {
			break
		}
		if time.Since(start) > time.Minute {
			backoff = time.Second
		}
		wait := backoff + time.Duration(rand.Int64N(int64(backoff/2)+1))
		log.Warn("disconnected from hub, retrying", "err", err, "in", wait.Round(time.Millisecond))
		select {
		case <-ctx.Done():
		case <-time.After(wait):
		}
		backoff = min(backoff*2, time.Minute)
	}
	log.Info("agent stopped")
}

func toWS(u string) (string, error) {
	u = strings.TrimRight(strings.TrimSpace(u), "/")
	switch {
	case strings.HasPrefix(u, "https://"):
		return "wss://" + strings.TrimPrefix(u, "https://") + protocol.Path, nil
	case strings.HasPrefix(u, "http://"):
		return "ws://" + strings.TrimPrefix(u, "http://") + protocol.Path, nil
	case strings.HasPrefix(u, "ws://"), strings.HasPrefix(u, "wss://"):
		return u + protocol.Path, nil
	}
	return "", fmt.Errorf("URL must start with http:// or https://: %q", u)
}

// toHTTP turns the WebSocket URL back into the hub's base http(s) URL.
func toHTTP(wsURL string) string {
	u := strings.TrimSuffix(wsURL, protocol.Path)
	if rest, ok := strings.CutPrefix(u, "wss://"); ok {
		return "https://" + rest
	}
	return "http://" + strings.TrimPrefix(u, "ws://")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envBool(k string) bool {
	switch strings.ToLower(os.Getenv(k)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// errRestart ends a session after the binary was replaced.
var errRestart = errors.New("restart for update")

type agent struct {
	log         *slog.Logger
	wsURL       string
	httpURL     string
	token       string
	concurrency int
	client      *http.Client

	exe            string // path updates are written to and run from, empty when it cannot self-update
	updateDir      string // in a container, the volume that keeps updates
	noUpdateReason string
	verify         string // release checksum check mode, see releaseCheck
	releaseURL     string
}

func (a *agent) session(ctx context.Context) error {
	log, token, concurrency := a.log, a.token, a.concurrency
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := websocket.Dial(dctx, a.wsURL, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": {"Bearer " + token}, "User-Agent": {"pingtxt-agent/" + version}},
	})
	cancel()
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("token rejected by hub")
		}
		return err
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)

	host, _ := os.Hostname()
	if err := wsjson.Write(ctx, conn, protocol.Message{Type: protocol.TypeHello, Hello: &protocol.Hello{
		Version: version, Hostname: host, OS: runtime.GOOS, Arch: runtime.GOARCH,
		SelfUpdate: a.exe != "", NoUpdateReason: a.noUpdateReason,
	}}); err != nil {
		return err
	}

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	results := make(chan check.Result, 256)
	statuses := make(chan protocol.UpdateStatus, 1)
	sem := make(chan struct{}, max(concurrency, 1))
	updating := false
	updated := make(chan error, 1)

	go func() {
		for {
			select {
			case <-sctx.Done():
				return
			case r := <-results:
				wctx, c := context.WithTimeout(sctx, 10*time.Second)
				err := wsjson.Write(wctx, conn, protocol.Message{Type: protocol.TypeResult, Result: &r})
				c()
				if err != nil {
					scancel()
					return
				}
			case st := <-statuses:
				wctx, c := context.WithTimeout(sctx, 10*time.Second)
				err := wsjson.Write(wctx, conn, protocol.Message{Type: protocol.TypeUpdateStatus, UpdateStatus: &st})
				c()
				if err != nil {
					scancel()
					return
				}
			}
		}
	}()

	type frame struct {
		data []byte
		err  error
	}
	frames := make(chan frame)
	go func() {
		for {
			_, data, err := conn.Read(sctx)
			select {
			case frames <- frame{data, err}:
			case <-sctx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()

	for {
		var data []byte
		select {
		case err := <-updated:
			updating = false
			if err == nil {
				conn.Close(websocket.StatusNormalClosure, "updating")
				return errRestart
			}
			log.Error("update failed", "err", err)
			select {
			case statuses <- protocol.UpdateStatus{Version: version, Error: err.Error()}:
			default:
			}
			continue
		case f := <-frames:
			if f.err != nil {
				return f.err
			}
			data = f.data
		}
		var msg protocol.Message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		switch msg.Type {
		case protocol.TypeWelcome:
			if w := msg.Welcome; w != nil {
				log.Info("connected to hub", "agent", w.Name, "location", w.Location)
			}
		case protocol.TypeJob:
			if msg.Job == nil {
				continue
			}
			job := *msg.Job
			select {
			case sem <- struct{}{}:
			default:
				log.Warn("too many checks in flight, skipping", "monitor", job.MonitorID)
				continue
			}
			go func() {
				defer func() { <-sem }()
				r := check.Run(sctx, job)
				select {
				case results <- r:
				case <-sctx.Done():
				}
			}()
		case protocol.TypeUpdate:
			u := msg.Update
			if u == nil || updating || u.Version == version {
				continue
			}
			if a.exe == "" {
				select {
				case statuses <- protocol.UpdateStatus{Version: version, Error: "self-update not available: " + a.noUpdateReason}:
				default:
				}
				continue
			}
			updating = true
			log.Info("updating agent", "from", version, "to", u.Version)
			go func() { updated <- a.update(sctx, *u) }()
		}
	}
}

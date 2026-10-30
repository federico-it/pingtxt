// Command pingtxt-agent is a lightweight probe that connects to a pingtxt hub
// and runs checks from its own network location. It only makes outbound
// connections, so no ports need to be opened.
//
//	PINGTXT_HUB=https://status.example.com PINGTXT_TOKEN=pt_xxx pingtxt-agent
package main

import (
	"context"
	"encoding/json"
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
	wsURL, err := toWS(*hubURL)
	if err != nil {
		log.Error("invalid hub URL", "err", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	backoff := time.Second
	for ctx.Err() == nil {
		start := time.Now()
		err := session(ctx, log, wsURL, *token, *concurrency)
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

func session(ctx context.Context, log *slog.Logger, wsURL, token string, concurrency int) error {
	dctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	conn, resp, err := websocket.Dial(dctx, wsURL, &websocket.DialOptions{
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
	}}); err != nil {
		return err
	}

	sctx, scancel := context.WithCancel(ctx)
	defer scancel()
	results := make(chan check.Result, 256)
	sem := make(chan struct{}, max(concurrency, 1))

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
			}
		}
	}()

	for {
		_, data, err := conn.Read(sctx)
		if err != nil {
			return err
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
		}
	}
}

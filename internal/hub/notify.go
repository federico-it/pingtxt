package hub

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Notifier types.
var NotifierTypes = []string{"webhook", "discord", "slack", "telegram", "ntfy"}

// Event is a status change sent to notification channels.
type Event struct {
	MonitorID int64  `json:"monitor_id"`
	Monitor   string `json:"monitor"`
	Target    string `json:"target"`
	Status    string `json:"status"` // "down" or "up"
	Cause     string `json:"cause,omitempty"`
	At        int64  `json:"at"`
	Test      bool   `json:"test,omitempty"`
}

func (e Event) Text() string {
	ts := time.UnixMilli(e.At).UTC().Format("2006-01-02 15:04:05 UTC")
	prefix := ""
	if e.Test {
		prefix = "[test] "
	}
	if e.Status == StatusDown {
		s := fmt.Sprintf("%s[DOWN] %s (%s) at %s", prefix, e.Monitor, e.Target, ts)
		if e.Cause != "" {
			s += "\n> " + e.Cause
		}
		return s
	}
	return fmt.Sprintf("%s[UP] %s (%s) is back up at %s", prefix, e.Monitor, e.Target, ts)
}

// Notifier dispatches events to the configured channels.
type Notifier struct {
	store  *store.Store
	log    *slog.Logger
	client *http.Client
}

func NewNotifier(st *store.Store, log *slog.Logger) *Notifier {
	return &Notifier{store: st, log: log, client: &http.Client{Timeout: 15 * time.Second}}
}

// Send delivers the event to every enabled channel, asynchronously.
func (n *Notifier) Send(e Event) {
	list, err := n.store.ListNotifiers()
	if err != nil {
		n.log.Error("list notifiers", "err", err)
		return
	}
	for _, nt := range list {
		if !nt.Enabled {
			continue
		}
		go func(nt *store.Notifier) {
			if err := n.Deliver(context.Background(), nt, e); err != nil {
				n.log.Warn("notification failed", "notifier", nt.Name, "err", err)
			}
		}(nt)
	}
}

// Deliver sends one event to one channel.
func (n *Notifier) Deliver(ctx context.Context, nt *store.Notifier, e Event) error {
	c := nt.Config
	text := e.Text()
	switch nt.Type {
	case "webhook":
		return n.post(ctx, c["url"], "application/json", mustJSON(e), nil)
	case "discord":
		return n.post(ctx, c["url"], "application/json", mustJSON(map[string]string{"content": "```\n" + text + "\n```"}), nil)
	case "slack":
		return n.post(ctx, c["url"], "application/json", mustJSON(map[string]string{"text": "```" + text + "```"}), nil)
	case "telegram":
		if c["bot_token"] == "" || c["chat_id"] == "" {
			return errors.New("bot_token and chat_id are required")
		}
		u := "https://api.telegram.org/bot" + url.PathEscape(c["bot_token"]) + "/sendMessage"
		return n.post(ctx, u, "application/json", mustJSON(map[string]string{"chat_id": c["chat_id"], "text": text}), nil)
	case "ntfy":
		hdr := map[string]string{"Title": "pingtxt: " + e.Monitor + " " + strings.ToUpper(e.Status)}
		if e.Status == StatusDown {
			hdr["Priority"] = "high"
			hdr["Tags"] = "red_circle"
		} else {
			hdr["Tags"] = "green_circle"
		}
		if c["token"] != "" {
			hdr["Authorization"] = "Bearer " + c["token"]
		}
		return n.post(ctx, c["url"], "text/plain", []byte(text), hdr)
	}
	return fmt.Errorf("unknown notifier type %q", nt.Type)
}

func (n *Notifier) post(ctx context.Context, u, ctype string, body []byte, hdr map[string]string) error {
	if u == "" {
		return errors.New("url is required")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("User-Agent", "pingtxt")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

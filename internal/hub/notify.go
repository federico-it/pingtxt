package hub

import (
	"bytes"
	"context"
	"crypto/tls"
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
var NotifierTypes = []string{"webhook", "discord", "slack", "telegram", "ntfy", "email", "app",
	"teams", "pagerduty", "opsgenie", "pushover", "gotify", "matrix", "twilio"}

// Event kinds. Status events have an empty kind for compatibility.
const (
	KindStatus = ""
	KindCert   = "cert_expiry"
	KindDomain = "domain_expiry"
	// Degraded events: status "degraded" when some locations start failing,
	// "up" when all of them work again.
	KindDegraded = "degraded"
)

// Expiry event statuses.
const (
	ExpiryExpiring = "expiring"
	ExpiryExpired  = "expired"
	ExpiryRenewed  = "renewed"
)

// Event is a status change sent to notification channels.
type Event struct {
	Kind      string `json:"kind,omitempty"` // "", "cert_expiry", "domain_expiry", "degraded" or "agent"
	MonitorID int64  `json:"monitor_id"`
	Monitor   string `json:"monitor"`
	Target    string `json:"target"`
	Status    string `json:"status"` // "down"/"up", "expiring"/"expired"/"renewed" for expiry events, "offline"/"online" for agent events
	Cause     string `json:"cause,omitempty"`
	At        int64  `json:"at"`
	Test      bool   `json:"test,omitempty"`
	// Status events of an incident: when the monitor went down (unix ms) and,
	// for "down", the reminder number (0 = first alert).
	DownSince int64 `json:"down_since,omitempty"`
	Reminder  int   `json:"reminder,omitempty"`
	// Expiry events only.
	Subject   string `json:"subject,omitempty"`    // host name or domain
	ExpiresAt int64  `json:"expires_at,omitempty"` // unix ms
	DaysLeft  *int   `json:"days_left,omitempty"`
	// Agent events only: the agent and its label (name · location). Target
	// is its host name, DownSince when it disconnected.
	AgentID int64  `json:"agent_id,omitempty"`
	Agent   string `json:"agent,omitempty"`
	// First alert of an incident: what the failing locations found out
	// (DNS, TCP, TLS, HTTP, traceroute). Reminders carry only the link.
	Diagnostics    []EventDiag `json:"diagnostics,omitempty"`
	DiagnosticsURL string      `json:"diagnostics_url,omitempty"`
}

// Alert reports whether the event is bad news (sent with high priority).
func (e Event) Alert() bool {
	return e.Status == StatusDown || e.Status == ExpiryExpiring || e.Status == ExpiryExpired || e.Status == AgentOffline
}

// Title is a one-line summary, used as subject or title by channels that have one.
func (e Event) Title() string {
	switch e.Kind {
	case KindCert, KindDomain:
		what := "certificate"
		if e.Kind == KindDomain {
			what = "domain"
		}
		return fmt.Sprintf("pingtxt: %s %s %s", e.Subject, what, e.Status)
	case KindAgent:
		return "pingtxt: agent " + e.Agent + " " + e.statusWord()
	}
	return "pingtxt: " + e.Monitor + " " + e.statusWord()
}

// statusWord is DOWN, STILL DOWN (reminders) or UP, DEGRADED or OK (no
// longer degraded).
func (e Event) statusWord() string {
	if e.Status == StatusDown && e.Reminder > 0 {
		return "STILL DOWN"
	}
	if e.Kind == KindDegraded && e.Status != StatusDegraded {
		return "OK"
	}
	return strings.ToUpper(e.Status)
}

// downFor is how long the monitor has been (or was) down, or "" when the
// alert is not late.
func (e Event) downFor() string {
	if e.DownSince == 0 || e.At-e.DownSince < 60_000 {
		return ""
	}
	return fmtDuration(time.Duration(e.At-e.DownSince) * time.Millisecond)
}

// fmtDuration renders a duration as "45m", "2h 5m" or "3d 4h".
func fmtDuration(d time.Duration) string {
	m := int(d / time.Minute)
	switch {
	case m < 60:
		return fmt.Sprintf("%dm", m)
	case m < 24*60:
		if m%60 == 0 {
			return fmt.Sprintf("%dh", m/60)
		}
		return fmt.Sprintf("%dh %dm", m/60, m%60)
	}
	h := m / 60
	if h%24 == 0 {
		return fmt.Sprintf("%dd", h/24)
	}
	return fmt.Sprintf("%dd %dh", h/24, h%24)
}

func (e Event) Text() string {
	ts := time.UnixMilli(e.At).UTC().Format("2006-01-02 15:04:05 UTC")
	prefix := ""
	if e.Test {
		prefix = "[test] "
	}
	if e.Kind == KindCert || e.Kind == KindDomain {
		return prefix + e.expiryText()
	}
	if e.Kind == KindAgent {
		return e.agentText(prefix)
	}
	if e.Kind == KindDegraded {
		if e.Status == StatusDegraded {
			s := fmt.Sprintf("%s[DEGRADED] %s (%s) at %s", prefix, e.Monitor, e.Target, ts)
			if e.Cause != "" {
				s += "\n> " + e.Cause
			}
			return s
		}
		return fmt.Sprintf("%s[OK] %s (%s) is no longer degraded at %s", prefix, e.Monitor, e.Target, ts)
	}
	if e.Status == StatusDown {
		var s string
		if d := e.downFor(); d != "" {
			since := time.UnixMilli(e.DownSince).UTC().Format("2006-01-02 15:04:05 UTC")
			s = fmt.Sprintf("%s[%s] %s (%s) down for %s, since %s", prefix, e.statusWord(), e.Monitor, e.Target, d, since)
		} else {
			s = fmt.Sprintf("%s[DOWN] %s (%s) at %s", prefix, e.Monitor, e.Target, ts)
		}
		if e.Cause != "" {
			s += "\n> " + e.Cause
		}
		return s + e.diagText()
	}
	s := fmt.Sprintf("%s[UP] %s (%s) is back up at %s", prefix, e.Monitor, e.Target, ts)
	if d := e.downFor(); d != "" {
		s += " after " + d + " down"
	}
	return s
}

func (e Event) expiryText() string {
	tag, what := "CERT", "TLS certificate"
	if e.Kind == KindDomain {
		tag, what = "DOMAIN", "domain registration"
	}
	date := time.UnixMilli(e.ExpiresAt).UTC().Format("2006-01-02")
	days := 0
	if e.DaysLeft != nil {
		days = *e.DaysLeft
	}
	var s string
	switch e.Status {
	case ExpiryExpired:
		s = fmt.Sprintf("[%s EXPIRED] %s: %s expired on %s", tag, e.Subject, what, date)
	case ExpiryRenewed:
		s = fmt.Sprintf("[%s OK] %s: %s renewed, now valid until %s", tag, e.Subject, what, date)
	default:
		s = fmt.Sprintf("[%s] %s: %s expires in %s (%s)", tag, e.Subject, what, plural(days, "day"), date)
	}
	return s + "\n> monitor " + e.Monitor + " (" + e.Target + ")"
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// Notifier dispatches events to the configured channels.
type Notifier struct {
	store  *store.Store
	log    *slog.Logger
	client *http.Client
	// tlsConfig overrides the TLS config used for SMTP (tests only).
	tlsConfig *tls.Config
	// resendURL overrides the Resend API address (tests only).
	resendURL string
	// resendWait overrides the waits between Resend requests (0) and after
	// a rate limit (the try number), for tests.
	resendWait func(try int) time.Duration
	// publicURL returns the public URL setting, to link the hub from emails.
	publicURL func() string
	// endpoints overrides the API addresses of hosted services (tests only).
	endpoints map[string]string
	// pushRetry overrides the waits before retrying failed phones (tests only).
	pushRetry []time.Duration
}

func NewNotifier(st *store.Store, log *slog.Logger) *Notifier {
	return &Notifier{store: st, log: log, client: &http.Client{Timeout: 15 * time.Second}}
}

// SendTo delivers the event to one channel, asynchronously.
func (n *Notifier) SendTo(nt *store.Notifier, e Event) {
	go func() {
		if err := n.Deliver(context.Background(), nt, e); err != nil {
			n.log.Warn("notification failed", "notifier", nt.Name, "err", err)
		}
	}()
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
		hdr := map[string]string{"Title": e.Title()}
		switch {
		case e.Status == StatusDown || e.Status == ExpiryExpired || e.Status == AgentOffline:
			hdr["Priority"] = "high"
			hdr["Tags"] = "red_circle"
		case e.Alert():
			hdr["Priority"] = "high"
			hdr["Tags"] = "warning"
		case e.Status == StatusDegraded:
			hdr["Tags"] = "warning"
		default:
			hdr["Tags"] = "green_circle"
		}
		if c["token"] != "" {
			hdr["Authorization"] = "Bearer " + c["token"]
		}
		return n.post(ctx, c["url"], "text/plain", []byte(text), hdr)
	case "teams":
		return n.sendTeams(ctx, c, e)
	case "pagerduty":
		return n.sendPagerDuty(ctx, c, e)
	case "opsgenie":
		return n.sendOpsgenie(ctx, c, e)
	case "pushover":
		return n.sendPushover(ctx, c, e)
	case "gotify":
		return n.sendGotify(ctx, c, e)
	case "matrix":
		return n.sendMatrix(ctx, c, e)
	case "twilio":
		return n.sendTwilio(ctx, c, e)
	case "email":
		return n.sendEmail(ctx, c, e)
	case "app":
		return n.sendApp(ctx, c, e)
	}
	return fmt.Errorf("unknown notifier type %q", nt.Type)
}

func (n *Notifier) post(ctx context.Context, u, ctype string, body []byte, hdr map[string]string) error {
	return n.request(ctx, http.MethodPost, u, ctype, body, hdr)
}

func (n *Notifier) request(ctx context.Context, method, u, ctype string, body []byte, hdr map[string]string) error {
	resp, err := n.do(ctx, method, u, ctype, body, hdr)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// getJSON reads a JSON answer into out.
func (n *Notifier) getJSON(ctx context.Context, u string, hdr map[string]string, out any) error {
	resp, err := n.do(ctx, http.MethodGet, u, "", nil, hdr)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(out)
}

// do sends a request and fails on answers other than 2xx, with the start
// of the body as the error.
func (n *Notifier) do(ctx context.Context, method, u, ctype string, body []byte, hdr map[string]string) (*http.Response, error) {
	if u == "" {
		return nil, errors.New("url is required")
	}
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return nil, err
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	req.Header.Set("User-Agent", "pingtxt")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := n.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return nil, fmt.Errorf("http %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return resp, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

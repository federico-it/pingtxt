package hub

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// fakeResend records the emails posted to a fake Resend API.
type fakeResend struct {
	srv *httptest.Server
	key string

	mu      sync.Mutex
	emails  []resendEmail
	idem    []string
	limit   int               // answer 429 to this many requests first
	refuse  map[string]bool   // recipients answered with 422
	headers map[string]string // last request headers
}

func newFakeResend(t *testing.T) *fakeResend {
	f := &fakeResend{key: "re_test_" + randomPass(), refuse: map[string]bool{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if r.Method != "POST" || r.URL.Path != "/emails" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+f.key {
			w.WriteHeader(401)
			io.WriteString(w, `{"statusCode":401,"message":"API key is invalid","name":"validation_error"}`)
			return
		}
		f.idem = append(f.idem, r.Header.Get("Idempotency-Key"))
		if f.limit > 0 {
			f.limit--
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			io.WriteString(w, `{"statusCode":429,"message":"Too many requests","name":"rate_limit_exceeded"}`)
			return
		}
		var e resendEmail
		if err := json.NewDecoder(r.Body).Decode(&e); err != nil {
			w.WriteHeader(400)
			return
		}
		for _, to := range e.To {
			if f.refuse[to] {
				w.WriteHeader(422)
				io.WriteString(w, `{"statusCode":422,"message":"Invalid to field","name":"validation_error"}`)
				return
			}
		}
		f.emails = append(f.emails, e)
		io.WriteString(w, `{"id":"49a3999c-0ce1-4ea6-ab68-afcd6dc2e794"}`)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeResend) received() []resendEmail {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]resendEmail(nil), f.emails...)
}

func noWait(int) time.Duration { return 0 }

func TestResendConfig(t *testing.T) {
	c := map[string]string{"provider": "resend", "api_key": "re_x", "host": "smtp.example.com", "password": "old", "from": "a@example.com", "to": "b@example.com"}
	ec, err := parseEmailConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	if ec.Provider != "resend" || ec.APIKey != "re_x" || c["host"] != "" || c["password"] != "" {
		t.Fatalf("resend config keeps smtp keys: %+v", c)
	}
	// SMTP is the default and is not stored; it drops the api key.
	c = map[string]string{"provider": "smtp", "api_key": "re_x", "host": "smtp.example.com", "from": "a@example.com", "to": "b@example.com"}
	if ec, err := parseEmailConfig(c); err != nil || ec.Provider != "smtp" {
		t.Fatal(err)
	}
	if _, ok := c["provider"]; ok || c["api_key"] != "" {
		t.Fatalf("smtp config: %+v", c)
	}
	for _, c := range []map[string]string{
		{"provider": "sendgrid", "from": "a@example.com", "to": "b@example.com"},
		{"provider": "resend", "api_key": "re_x\r\nX: y", "from": "a@example.com", "to": "b@example.com"},
		{"provider": "resend", "from": "a@example.com", "to": strings.Repeat("b@example.com,", 51) + "b@example.com"},
	} {
		if _, err := parseEmailConfig(c); err == nil {
			t.Errorf("want error for %+v", c)
		}
	}
}

func TestResendDelivery(t *testing.T) {
	f := newFakeResend(t)
	n := &Notifier{client: http.DefaultClient, resendURL: f.srv.URL, resendWait: noWait}
	cfg := map[string]string{"provider": "resend", "api_key": f.key, "from": "Pingtxt <alerts@example.com>", "to": "ops@example.com, bob@example.com"}

	// A rate limit is retried with the same idempotency key.
	f.limit = 2
	ev := Event{MonitorID: 3, Monitor: "api", Target: "https://api.example.com", Status: StatusDown, Cause: "http 503", At: time.Now().UnixMilli()}
	if err := n.sendEmail(context.Background(), cfg, ev); err != nil {
		t.Fatal(err)
	}
	got := f.received()
	if len(got) != 1 {
		t.Fatalf("want 1 email, got %d", len(got))
	}
	e := got[0]
	if e.From != `"Pingtxt" <alerts@example.com>` || strings.Join(e.To, ",") != "ops@example.com,bob@example.com" || e.Subject != "[pingtxt] api is DOWN" {
		t.Fatalf("email: %+v", e)
	}
	if !strings.Contains(e.Text, "http 503") || !strings.Contains(e.HTML, "color-scheme") || e.Headers["Auto-Submitted"] != "auto-generated" {
		t.Fatalf("content: %+v", e)
	}
	if len(f.idem) != 3 || f.idem[0] == "" || f.idem[0] != f.idem[2] {
		t.Fatalf("idempotency keys: %q", f.idem)
	}

	// Too many rate limits give up.
	f.limit = resendTries
	if err := n.sendEmail(context.Background(), cfg, ev); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("want rate limit error, got %v", err)
	}
	f.limit = 0

	// A bad key stops; no key is a setup error.
	bad := map[string]string{"provider": "resend", "api_key": "re_wrong", "from": "a@example.com", "to": "b@example.com"}
	if err := n.sendEmail(context.Background(), bad, ev); err == nil || !strings.Contains(err.Error(), "API key is invalid") {
		t.Fatalf("want key error, got %v", err)
	}
	delete(bad, "api_key")
	if err := n.sendEmail(context.Background(), bad, ev); err == nil || !strings.Contains(err.Error(), "api key is required") {
		t.Fatalf("want missing key error, got %v", err)
	}

	// Subscribers: a refused address is skipped, the others are sent, with
	// the one-click unsubscribe headers.
	ec, err := parseEmailConfig(map[string]string{"provider": "resend", "api_key": f.key, "from": "a@example.com", "to": "a@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	f.refuse["bad@example.com"] = true
	page := &store.StatusPage{Slug: "acme", Title: "Acme"}
	inc := &store.PageIncident{Title: "Slow API", Impact: store.ImpactMinor, CreatedAt: time.Now().UnixMilli()}
	links := pageLinks{page: "https://status.example.com/", api: "https://status.example.com"}
	var msgs []outMail
	for _, to := range []string{"x@example.com", "bad@example.com", "y@example.com"} {
		s := &store.PageSubscriber{Email: to, Token: strings.Repeat("ab", 24)}
		msgs = append(msgs, outMail{to: []string{to}, msg: buildUpdateEmail(page, inc, store.PageIncidentUpdate{Status: store.ReportInvestigating, Message: "looking"}, s, links, time.Now())})
	}
	before := len(f.received())
	var refused []int
	if err := n.deliver(context.Background(), ec, msgs, func(i int, err error) { refused = append(refused, i) }); err != nil {
		t.Fatal(err)
	}
	got = f.received()[before:]
	if len(got) != 2 || len(refused) != 1 || refused[0] != 1 {
		t.Fatalf("sent %d, refused %v", len(got), refused)
	}
	if h := got[0].Headers; !strings.HasSuffix(h["List-Unsubscribe"], "/unsubscribe>") || h["List-Unsubscribe-Post"] != "List-Unsubscribe=One-Click" {
		t.Fatalf("unsubscribe headers: %+v", h)
	}
}

func TestResendNotifierAPI(t *testing.T) {
	h, _ := newTestHub(t)
	f := newFakeResend(t)
	h.notify.resendURL = f.srv.URL
	h.notify.resendWait = noWait
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	if res := c.do("POST", "/api/setup", map[string]string{"email": "admin@example.com", "password": "password123"}); res.StatusCode != 201 {
		t.Fatalf("setup: %d", res.StatusCode)
	}
	cfg := map[string]string{"provider": "resend", "api_key": f.key, "from": "alerts@example.com", "to": "ops@example.com"}
	body := map[string]any{"name": "resend", "type": "email", "enabled": true, "config": cfg}
	res := c.do("POST", "/api/notifiers", body)
	if res.StatusCode != 201 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("create: %d %s", res.StatusCode, b)
	}
	var n store.Notifier
	json.NewDecoder(res.Body).Decode(&n)
	if _, ok := n.Config["api_key"]; ok || n.Config["api_key_set"] != "true" {
		t.Fatalf("api key must not be returned: %+v", n.Config)
	}
	id := strconv.FormatInt(n.ID, 10)

	// An empty key keeps the stored one.
	cfg["api_key"] = ""
	if res := c.do("PUT", "/api/notifiers/"+id, body); res.StatusCode != 200 {
		t.Fatalf("update: %d", res.StatusCode)
	}
	if stored, _ := h.store.Notifier(n.ID); stored.Config["api_key"] != f.key {
		t.Fatalf("stored config: %+v", stored.Config)
	}
	if res := c.do("POST", "/api/notifiers/"+id+"/test", nil); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("test: %d %s", res.StatusCode, b)
	}
	if got := f.received(); len(got) != 1 || got[0].Subject != "[test] [pingtxt] example.com is DOWN" {
		t.Fatalf("got %+v", got)
	}

	// Switching to SMTP does not carry the key over (as a password or else).
	smtpCfg := map[string]string{"host": "127.0.0.1", "port": "1", "from": "alerts@example.com", "to": "ops@example.com"}
	if res := c.do("PUT", "/api/notifiers/"+id, map[string]any{"name": "resend", "type": "email", "enabled": true, "config": smtpCfg}); res.StatusCode != 200 {
		t.Fatalf("switch: %d", res.StatusCode)
	}
	if stored, _ := h.store.Notifier(n.ID); stored.Config["api_key"] != "" || stored.Config["password"] != "" {
		t.Fatalf("secret kept after switching provider: %+v", stored.Config)
	}
}

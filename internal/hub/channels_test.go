package hub

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

// capture records the requests sent to a fake service.
type capture struct {
	mu   sync.Mutex
	reqs []captured
}

type captured struct {
	Method, Path, Query string
	Header              http.Header
	Body                string
}

func (c *capture) server(t *testing.T, answer func(r *http.Request) (int, string)) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.reqs = append(c.reqs, captured{r.Method, r.URL.Path, r.URL.RawQuery, r.Header.Clone(), string(b)})
		c.mu.Unlock()
		code, body := 200, "{}"
		if answer != nil {
			code, body = answer(r)
		}
		w.WriteHeader(code)
		io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func (c *capture) all() []captured {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]captured(nil), c.reqs...)
}

func (r captured) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(r.Body), &m); err != nil {
		t.Fatalf("body %q: %v", r.Body, err)
	}
	return m
}

func (r captured) form(t *testing.T) url.Values {
	t.Helper()
	v, err := url.ParseQuery(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func testNotifierFor(endpoints map[string]string) *Notifier {
	n := NewNotifier(nil, slog.New(slog.DiscardHandler))
	n.endpoints = endpoints
	n.publicURL = func() string { return "hub.example.com" }
	return n
}

var (
	downEvent = Event{MonitorID: 7, Monitor: "api", Target: "https://api.example.com", Status: StatusDown, Cause: "eu: timeout", At: 1_700_000_000_000}
	upEvent   = Event{MonitorID: 7, Monitor: "api", Target: "https://api.example.com", Status: StatusUp, At: 1_700_000_600_000, DownSince: 1_700_000_000_000}
)

func TestTeamsChannel(t *testing.T) {
	var c capture
	srv := c.server(t, func(*http.Request) (int, string) { return 202, "" })
	n := testNotifierFor(nil)
	nt := &store.Notifier{Type: "teams", Config: map[string]string{"url": srv.URL + "/workflows/abc"}}
	if err := n.Deliver(context.Background(), nt, downEvent); err != nil {
		t.Fatal(err)
	}
	m := c.all()[0].json(t)
	att := m["attachments"].([]any)[0].(map[string]any)
	if m["type"] != "message" || att["contentType"] != "application/vnd.microsoft.card.adaptive" {
		t.Fatalf("message: %v", m)
	}
	card := att["content"].(map[string]any)
	body := card["body"].([]any)
	if first := body[0].(map[string]any); first["text"] != "pingtxt: api DOWN" || first["color"] != "Attention" {
		t.Fatalf("title block: %v", first)
	}
	if !strings.Contains(c.all()[0].Body, "eu: timeout") {
		t.Fatalf("cause missing: %s", c.all()[0].Body)
	}
	if a := card["actions"].([]any)[0].(map[string]any); a["url"] != "https://hub.example.com/monitors/view/?id=7" {
		t.Fatalf("action: %v", a)
	}
}

func TestPagerDutyChannel(t *testing.T) {
	var c capture
	srv := c.server(t, func(*http.Request) (int, string) { return 202, `{"status":"success"}` })
	n := testNotifierFor(map[string]string{"pagerduty": srv.URL, "pagerduty-eu": srv.URL + "/eu"})
	nt := &store.Notifier{Type: "pagerduty", Config: map[string]string{"routing_key": "R0123"}}
	ctx := context.Background()
	if err := n.Deliver(ctx, nt, downEvent); err != nil {
		t.Fatal(err)
	}
	if err := n.Deliver(ctx, nt, upEvent); err != nil {
		t.Fatal(err)
	}
	reqs := c.all()
	trig, res := reqs[0].json(t), reqs[1].json(t)
	if reqs[0].Path != "/v2/enqueue" || trig["event_action"] != "trigger" || trig["routing_key"] != "R0123" || trig["dedup_key"] != "pingtxt-monitor-7" {
		t.Fatalf("trigger: %s %v", reqs[0].Path, trig)
	}
	p := trig["payload"].(map[string]any)
	if p["severity"] != "critical" || p["source"] != "https://api.example.com" || !strings.HasPrefix(p["summary"].(string), "api DOWN: ") {
		t.Fatalf("payload: %v", p)
	}
	if res["event_action"] != "resolve" || res["dedup_key"] != "pingtxt-monitor-7" {
		t.Fatalf("resolve: %v", res)
	}

	// EU region; a test triggers and resolves.
	nt.Config["region"] = "eu"
	test := downEvent
	test.Test = true
	if err := n.Deliver(ctx, nt, test); err != nil {
		t.Fatal(err)
	}
	reqs = c.all()[2:]
	if len(reqs) != 2 || reqs[0].Path != "/eu/v2/enqueue" || reqs[1].json(t)["event_action"] != "resolve" {
		t.Fatalf("test: %+v", reqs)
	}
	if k := reqs[0].json(t)["dedup_key"].(string); !strings.HasPrefix(k, "pingtxt-test-") || k != reqs[1].json(t)["dedup_key"] {
		t.Fatalf("test key %q", k)
	}
}

func TestOpsgenieChannel(t *testing.T) {
	var c capture
	srv := c.server(t, func(*http.Request) (int, string) { return 202, `{"result":"Request will be processed"}` })
	n := testNotifierFor(map[string]string{"opsgenie": srv.URL})
	nt := &store.Notifier{Type: "opsgenie", Config: map[string]string{"api_key": "k-1"}}
	ctx := context.Background()
	deg := Event{Kind: KindDegraded, MonitorID: 7, Monitor: "api", Target: "x", Status: StatusDegraded, At: 1}
	for _, e := range []Event{downEvent, upEvent, deg} {
		if err := n.Deliver(ctx, nt, e); err != nil {
			t.Fatal(err)
		}
	}
	reqs := c.all()
	if reqs[0].Header.Get("Authorization") != "GenieKey k-1" || reqs[0].Path != "/v2/alerts" {
		t.Fatalf("create: %s %v", reqs[0].Path, reqs[0].Header)
	}
	if a := reqs[0].json(t); a["alias"] != "pingtxt-monitor-7" || a["priority"] != "P1" || a["message"] != "api DOWN" {
		t.Fatalf("alert: %v", a)
	}
	if reqs[1].Path != "/v2/alerts/pingtxt-monitor-7/close" || reqs[1].Query != "identifierType=alias" {
		t.Fatalf("close: %s?%s", reqs[1].Path, reqs[1].Query)
	}
	if a := reqs[2].json(t); a["alias"] != "pingtxt-monitor-7-degraded" || a["priority"] != "P3" {
		t.Fatalf("degraded: %v", a)
	}
}

func TestPushoverChannel(t *testing.T) {
	var c capture
	srv := c.server(t, func(*http.Request) (int, string) { return 200, `{"status":1}` })
	n := testNotifierFor(map[string]string{"pushover": srv.URL})
	nt := &store.Notifier{Type: "pushover", Config: map[string]string{"app_token": "a1", "user_key": "u1", "device": "phone"}}
	if err := n.Deliver(context.Background(), nt, downEvent); err != nil {
		t.Fatal(err)
	}
	if err := n.Deliver(context.Background(), nt, upEvent); err != nil {
		t.Fatal(err)
	}
	reqs := c.all()
	f := reqs[0].form(t)
	if reqs[0].Path != "/1/messages.json" || f.Get("token") != "a1" || f.Get("user") != "u1" || f.Get("device") != "phone" || f.Get("priority") != "1" || f.Get("title") != "pingtxt: api DOWN" {
		t.Fatalf("down: %s %v", reqs[0].Path, f)
	}
	if reqs[1].form(t).Get("priority") != "0" {
		t.Fatalf("up: %v", reqs[1].form(t))
	}
	// Errors carry Pushover's answer.
	bad := testNotifierFor(map[string]string{"pushover": c.server(t, func(*http.Request) (int, string) {
		return 400, `{"user":"invalid","errors":["user identifier is not a valid user"],"status":0}`
	}).URL})
	if err := bad.Deliver(context.Background(), nt, downEvent); err == nil || !strings.Contains(err.Error(), "not a valid user") {
		t.Fatalf("err = %v", err)
	}
}

func TestGotifyChannel(t *testing.T) {
	var c capture
	srv := c.server(t, nil)
	n := testNotifierFor(nil)
	nt := &store.Notifier{Type: "gotify", Config: map[string]string{"url": srv.URL + "/gotify/", "token": "Atok"}}
	if err := n.Deliver(context.Background(), nt, downEvent); err != nil {
		t.Fatal(err)
	}
	r := c.all()[0]
	if r.Path != "/gotify/message" || r.Header.Get("X-Gotify-Key") != "Atok" {
		t.Fatalf("request: %s %v", r.Path, r.Header)
	}
	if m := r.json(t); m["priority"] != float64(8) || m["title"] != "pingtxt: api DOWN" {
		t.Fatalf("message: %v", m)
	}
}

func TestMatrixChannel(t *testing.T) {
	var c capture
	srv := c.server(t, func(r *http.Request) (int, string) {
		if strings.Contains(r.URL.Path, "/directory/room/") {
			return 200, `{"room_id":"!abc:example.org"}`
		}
		return 200, `{"event_id":"$1"}`
	})
	n := testNotifierFor(nil)
	nt := &store.Notifier{Type: "matrix", Config: map[string]string{"url": srv.URL, "access_token": "syt_x", "room_id": "#ops:example.org"}}
	if err := n.Deliver(context.Background(), nt, downEvent); err != nil {
		t.Fatal(err)
	}
	reqs := c.all()
	if len(reqs) != 2 || reqs[0].Method != "GET" || reqs[0].Path != "/_matrix/client/v3/directory/room/#ops:example.org" {
		t.Fatalf("lookup: %+v", reqs)
	}
	send := reqs[1]
	if send.Method != "PUT" || !strings.HasPrefix(send.Path, "/_matrix/client/v3/rooms/!abc:example.org/send/m.room.message/pingtxt") || send.Header.Get("Authorization") != "Bearer syt_x" {
		t.Fatalf("send: %s %s %v", send.Method, send.Path, send.Header)
	}
	if m := send.json(t); m["msgtype"] != "m.text" || !strings.Contains(m["body"].(string), "[DOWN] api") || !strings.Contains(m["formatted_body"].(string), "<b>pingtxt: api DOWN</b>") {
		t.Fatalf("message: %v", m)
	}
}

func TestTwilioChannel(t *testing.T) {
	var c capture
	srv := c.server(t, func(*http.Request) (int, string) { return 201, `{"sid":"SM1"}` })
	n := testNotifierFor(map[string]string{"twilio": srv.URL})
	nt := &store.Notifier{Type: "twilio", Config: map[string]string{"account_sid": "AC123", "auth_token": "secret", "from": "+15550001111", "to": "+391111111, +392222222"}}
	if err := n.Deliver(context.Background(), nt, downEvent); err != nil {
		t.Fatal(err)
	}
	reqs := c.all()
	if len(reqs) != 2 {
		t.Fatalf("%d requests", len(reqs))
	}
	user, pass, _ := (&http.Request{Header: reqs[0].Header}).BasicAuth()
	f := reqs[0].form(t)
	if reqs[0].Path != "/2010-04-01/Accounts/AC123/Messages.json" || user != "AC123" || pass != "secret" {
		t.Fatalf("request: %s %s:%s", reqs[0].Path, user, pass)
	}
	if f.Get("To") != "+391111111" || f.Get("From") != "+15550001111" || !strings.HasPrefix(f.Get("Body"), "[DOWN] api") || !strings.Contains(f.Get("Body"), "eu: timeout") {
		t.Fatalf("form: %v", f)
	}
	if reqs[1].form(t).Get("To") != "+392222222" {
		t.Fatalf("second: %v", reqs[1].form(t))
	}
	// A messaging service sends from its pool.
	nt.Config["from"] = "MG999"
	nt.Config["to"] = "+391111111"
	if err := n.Deliver(context.Background(), nt, upEvent); err != nil {
		t.Fatal(err)
	}
	if f := c.all()[2].form(t); f.Get("MessagingServiceSid") != "MG999" || f.Has("From") {
		t.Fatalf("service: %v", f)
	}
}

func TestServiceChannelValidation(t *testing.T) {
	cases := []struct {
		typ string
		cfg map[string]string
		ok  bool
	}{
		{"teams", map[string]string{"url": "https://x.webhook.office.com/a"}, true},
		{"teams", map[string]string{}, false},
		{"pagerduty", map[string]string{"routing_key": "abc"}, true},
		{"pagerduty", map[string]string{"routing_key": "abc", "region": "eu"}, true},
		{"pagerduty", map[string]string{"routing_key": "abc", "region": "asia"}, false},
		{"pagerduty", map[string]string{}, false},
		{"opsgenie", map[string]string{"api_key": "k"}, true},
		{"opsgenie", map[string]string{}, false},
		{"pushover", map[string]string{"app_token": "a", "user_key": "u"}, true},
		{"pushover", map[string]string{"app_token": "a"}, false},
		{"gotify", map[string]string{"url": "https://gotify.example.com", "token": "t"}, true},
		{"gotify", map[string]string{"url": "gotify.example.com", "token": "t"}, false},
		{"matrix", map[string]string{"url": "https://matrix.org", "access_token": "t", "room_id": "!a:matrix.org"}, true},
		{"matrix", map[string]string{"url": "https://matrix.org", "access_token": "t", "room_id": "#ops:matrix.org"}, true},
		{"matrix", map[string]string{"url": "https://matrix.org", "access_token": "t", "room_id": "ops"}, false},
		{"twilio", map[string]string{"account_sid": "AC1", "auth_token": "t", "from": "+15550001111", "to": "+391234567, +441234567890"}, true},
		{"twilio", map[string]string{"account_sid": "AC1", "auth_token": "t", "from": "MG1", "to": "+391234567"}, true},
		{"twilio", map[string]string{"account_sid": "AC1", "auth_token": "t", "from": "+15550001111", "to": "3331234567"}, false},
		{"twilio", map[string]string{"account_sid": "XX1", "auth_token": "t", "from": "+15550001111", "to": "+391234567"}, false},
	}
	for _, c := range cases {
		err := validateNotifier(&store.Notifier{Name: "x", Type: c.typ, Config: c.cfg})
		if (err == nil) != c.ok {
			t.Errorf("%s %v: err = %v", c.typ, c.cfg, err)
		}
	}
}

// Read-only users and exports without secrets never see the keys.
func TestServiceChannelSecrets(t *testing.T) {
	h, _ := newTestHub(t)
	var c capture
	srv0 := c.server(t, func(*http.Request) (int, string) { return 200, `{"status":1}` })
	h.notify.endpoints = map[string]string{"pushover": srv0.URL}
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	cl := newClient(t, srv.URL)
	if res := cl.do("POST", "/api/setup", map[string]string{"email": "admin@example.com", "password": "password123"}); res.StatusCode != 201 {
		t.Fatalf("setup: %d", res.StatusCode)
	}
	body := map[string]any{"name": "phone", "type": "pushover", "enabled": true, "config": map[string]string{"app_token": " a1 ", "user_key": "u1"}}
	res := cl.do("POST", "/api/notifiers", body)
	if res.StatusCode != 201 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("create: %d %s", res.StatusCode, b)
	}
	var n store.Notifier
	json.NewDecoder(res.Body).Decode(&n)
	if n.Config["app_token"] != "a1" {
		t.Fatalf("config not trimmed: %v", n.Config)
	}
	if res := cl.do("POST", "/api/notifiers/test", body); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("test: %d %s", res.StatusCode, b)
	}
	if f := c.all()[0].form(t); !strings.HasPrefix(f.Get("message"), "[test] [DOWN] example.com") {
		t.Fatalf("test message: %v", f)
	}
	for _, typ := range []string{"teams", "pagerduty", "opsgenie", "pushover", "gotify", "matrix", "twilio"} {
		if len(notifierSecrets[typ]) == 0 {
			t.Errorf("%s has no secrets listed for exports", typ)
		}
	}
}

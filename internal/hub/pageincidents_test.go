package hub

import (
	"encoding/json"
	"io"
	"mime/quotedprintable"
	"net/http"
	"net/http/httptest"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func decodeJSON[T any](t *testing.T, res *http.Response) T {
	t.Helper()
	defer res.Body.Close()
	var v T
	if err := json.NewDecoder(res.Body).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func wantStatus(t *testing.T, res *http.Response, code int, what string) {
	t.Helper()
	if res.StatusCode != code {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("%s: want %d, got %d %s", what, code, res.StatusCode, b)
	}
}

// waitMails waits until the fake server got n messages.
func waitMails(t *testing.T, s *fakeSMTP, n int) []fakeMail {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := s.received()
		if len(got) >= n || time.Now().After(deadline) {
			if len(got) != n {
				t.Fatalf("want %d mails, got %d", n, len(got))
			}
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type parsedMail struct {
	header mail.Header
	body   string
}

func parseMail(t *testing.T, m fakeMail) parsedMail {
	t.Helper()
	msg, err := mail.ReadMessage(strings.NewReader(m.data))
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(quotedprintable.NewReader(msg.Body))
	if err != nil {
		t.Fatal(err)
	}
	return parsedMail{header: msg.Header, body: string(b)}
}

var tokenRe = regexp.MustCompile(`[?&](confirm|unsubscribe)=([0-9a-f]{48})`)

func linkToken(t *testing.T, body, kind string) string {
	t.Helper()
	for _, m := range tokenRe.FindAllStringSubmatch(body, -1) {
		if m[1] == kind {
			return m[2]
		}
	}
	t.Fatalf("no %s link in %q", kind, body)
	return ""
}

func TestPageIncidentsAndSubscribers(t *testing.T) {
	h, st := newTestHub(t)
	smtpSrv, clientTLS := newFakeSMTP(t, false, true)
	h.notify.tlsConfig = clientTLS
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"status/index.html": {Data: []byte("status page")}}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	wantStatus(t, c.do("POST", "/api/setup", map[string]string{"email": "admin@example.com", "password": "password123"}), 201, "setup")

	// The email channel that sends to subscribers. Its own recipients are
	// not used for subscriber emails.
	ch := decodeJSON[store.Notifier](t, c.do("POST", "/api/notifiers", map[string]any{"name": "mail", "type": "email", "enabled": true, "config": map[string]string{
		"host": "127.0.0.1", "port": smtpSrv.port(), "security": "starttls",
		"username": smtpSrv.user, "password": smtpSrv.pass,
		"from": "Acme status <status@example.com>", "to": "ops@example.com",
	}}))
	hook := decodeJSON[store.Notifier](t, c.do("POST", "/api/notifiers", map[string]any{"name": "hook", "type": "webhook", "enabled": true, "config": map[string]string{"url": "https://example.com/hook"}}))

	page := map[string]any{"slug": "acme", "title": "Acme", "published": true, "show_incidents": true, "subscribe_notifier_id": hook.ID}
	wantStatus(t, c.do("POST", "/api/status-pages", page), 400, "subscriptions through a webhook channel")
	page["subscribe_notifier_id"] = 0
	sp := decodeJSON[store.StatusPage](t, c.do("POST", "/api/status-pages", page))
	base := "/api/status-pages/" + strconv.FormatInt(sp.ID, 10)

	// Without a channel visitors cannot subscribe.
	pub := decodeJSON[publicPage](t, c.do("GET", "/api/public/status/acme", nil))
	if pub.Subscribe || len(pub.Reports) != 0 {
		t.Fatalf("public page: %+v", pub)
	}
	visitor := newClient(t, srv.URL)
	wantStatus(t, visitor.do("POST", "/api/public/status/acme/subscribe", map[string]string{"email": "jane@example.com"}), 404, "subscribe without channel")

	page["subscribe_notifier_id"] = ch.ID
	sp = decodeJSON[store.StatusPage](t, c.do("PUT", base, page))
	if sp.SubscribeNotifierID != ch.ID {
		t.Fatalf("channel not saved: %+v", sp)
	}
	h.dropPageCache()
	if pub := decodeJSON[publicPage](t, visitor.do("GET", "/api/public/status/acme", nil)); !pub.Subscribe {
		t.Fatal("public page must offer subscriptions")
	}

	// Subscribe: a confirmation link is emailed, nothing else.
	wantStatus(t, visitor.do("POST", "/api/public/status/acme/subscribe", map[string]string{"email": "not an email"}), 400, "invalid email")
	wantStatus(t, visitor.do("POST", "/api/public/status/acme/subscribe", map[string]string{"email": "Jane@Example.com"}), 202, "subscribe")
	mails := waitMails(t, smtpSrv, 1)
	if strings.Join(mails[0].to, ",") != "jane@example.com" || mails[0].from != "status@example.com" {
		t.Fatalf("confirmation envelope: %+v", mails[0])
	}
	pm := parseMail(t, mails[0])
	if !strings.Contains(pm.header.Get("Subject"), "Confirm your subscription to Acme") {
		t.Fatalf("subject: %q", pm.header.Get("Subject"))
	}
	if !strings.Contains(pm.body, srv.URL+"/status/acme/?confirm=") {
		t.Fatalf("confirmation link: %s", pm.body)
	}
	confirm := linkToken(t, pm.body, "confirm")

	// Unconfirmed addresses get no updates.
	created := decodeJSON[struct {
		Incident store.PageIncident `json:"incident"`
		Notified int                `json:"notified"`
	}](t, c.do("POST", base+"/incidents", map[string]any{"title": "Slow API", "impact": "major", "message": "Looking into it."}))
	if created.Notified != 0 || created.Incident.Status != store.ReportInvestigating || len(created.Incident.Updates) != 1 {
		t.Fatalf("created: %+v", created)
	}
	inc := created.Incident
	ibase := base + "/incidents/" + strconv.FormatInt(inc.ID, 10)

	res := visitor.do("POST", "/api/public/subscriptions/"+confirm+"/confirm", nil)
	wantStatus(t, res, 200, "confirm")
	if got := decodeJSON[map[string]any](t, res); got["email"] != "jane@example.com" || got["page"] != "Acme" {
		t.Fatalf("confirm answer: %v", got)
	}
	wantStatus(t, visitor.do("POST", "/api/public/subscriptions/"+strings.Repeat("0", 48)+"/confirm", nil), 404, "unknown token")

	// Subscribing again does not tell anything nor send a new email.
	wantStatus(t, visitor.do("POST", "/api/public/status/acme/subscribe", map[string]string{"email": "jane@example.com"}), 202, "subscribe again")

	// An update is emailed to confirmed subscribers.
	upd := decodeJSON[struct {
		Incident store.PageIncident `json:"incident"`
		Notified int                `json:"notified"`
	}](t, c.do("POST", ibase+"/updates", map[string]any{"status": "identified", "message": "A database node is slow.\nFailing over."}))
	if upd.Notified != 1 || upd.Incident.Status != store.ReportIdentified || len(upd.Incident.Updates) != 2 {
		t.Fatalf("update: %+v", upd)
	}
	mails = waitMails(t, smtpSrv, 2)
	pm = parseMail(t, mails[1])
	if s := pm.header.Get("Subject"); s != "[Acme] Identified: Slow API" {
		t.Fatalf("update subject: %q", s)
	}
	if !strings.Contains(pm.body, "A database node is slow.") || !strings.Contains(pm.body, "Impact:  major") {
		t.Fatalf("update body: %s", pm.body)
	}
	oneClick := pm.header.Get("List-Unsubscribe")
	if !strings.HasPrefix(oneClick, "<"+srv.URL+"/api/public/subscriptions/") || pm.header.Get("List-Unsubscribe-Post") != "List-Unsubscribe=One-Click" {
		t.Fatalf("list-unsubscribe: %q", oneClick)
	}
	if linkToken(t, pm.body, "unsubscribe") != confirm {
		t.Fatal("unsubscribe link must carry the subscriber token")
	}

	// notify: false posts silently.
	wantStatus(t, c.do("POST", ibase+"/updates", map[string]any{"status": "resolved", "message": "Fixed.", "notify": false}), 201, "silent update")
	got := decodeJSON[store.PageIncident](t, c.do("PUT", ibase, map[string]any{"title": "Slow API in EU", "impact": "minor"}))
	if got.Title != "Slow API in EU" || got.Status != store.ReportResolved || got.ResolvedAt == 0 || len(got.Updates) != 3 {
		t.Fatalf("after edit: %+v", got)
	}
	wantStatus(t, c.do("POST", ibase+"/updates", map[string]any{"status": "fixed", "message": "x"}), 400, "invalid status")
	wantStatus(t, c.do("POST", ibase+"/updates", map[string]any{"status": "resolved", "message": "  "}), 400, "empty message")

	// The public page shows the timeline, without internal ids.
	h.dropPageCache()
	res = visitor.do("GET", "/api/public/status/acme", nil)
	raw, _ := io.ReadAll(res.Body)
	if strings.Contains(string(raw), `"page_id":1`) {
		t.Fatalf("public page leaks page id: %s", raw)
	}
	if err := json.Unmarshal(raw, &pub); err != nil {
		t.Fatal(err)
	}
	if len(pub.Reports) != 1 || pub.Reports[0].Title != "Slow API in EU" || len(pub.Reports[0].Updates) != 3 || pub.Reports[0].Updates[0].Message != "Fixed." {
		t.Fatalf("public reports: %s", raw)
	}

	// Deleting the latest update gives the incident the previous status.
	got = decodeJSON[store.PageIncident](t, c.do("DELETE", ibase+"/updates/"+strconv.FormatInt(got.Updates[0].ID, 10), nil))
	if got.Status != store.ReportIdentified || got.ResolvedAt != 0 || len(got.Updates) != 2 {
		t.Fatalf("after deleting an update: %+v", got)
	}
	wantStatus(t, c.do("DELETE", ibase+"/updates/"+strconv.FormatInt(got.Updates[0].ID, 10), nil), 200, "delete update")
	wantStatus(t, c.do("DELETE", ibase+"/updates/"+strconv.FormatInt(got.Updates[1].ID, 10), nil), 409, "delete last update")

	// Admins see the subscribers.
	subs := decodeJSON[struct {
		Subscribers []store.PageSubscriber `json:"subscribers"`
		Enabled     bool                   `json:"enabled"`
	}](t, c.do("GET", base+"/subscribers", nil))
	if !subs.Enabled || len(subs.Subscribers) != 1 || subs.Subscribers[0].Email != "jane@example.com" || subs.Subscribers[0].ConfirmedAt == 0 {
		t.Fatalf("subscribers: %+v", subs)
	}

	// Read-only users can read incidents but not change them or see emails.
	viewer, err := st.CreateUser("viewer@example.com", "", store.RoleViewer)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	if err := h.startSession(rec, httptest.NewRequest("GET", "/", nil), viewer); err != nil {
		t.Fatal(err)
	}
	vc := func(method, path string, body string) int {
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(rec.Result().Cookies()[0])
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := vc("GET", base+"/incidents", ""); code != 200 {
		t.Fatalf("viewer list incidents: %d", code)
	}
	if code := vc("POST", base+"/incidents", `{"title":"x","message":"y"}`); code != 403 {
		t.Fatalf("viewer create incident: %d", code)
	}
	if code := vc("GET", base+"/subscribers", ""); code != 403 {
		t.Fatalf("viewer subscribers: %d", code)
	}

	// One-click unsubscribe from a mail client: a form post, no JSON.
	req, _ := http.NewRequest("POST", srv.URL+"/api/public/subscriptions/"+confirm+"/unsubscribe", strings.NewReader("List-Unsubscribe=One-Click"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	wantStatus(t, res, 200, "one-click unsubscribe")
	if list, _ := st.PageSubscribers(sp.ID, false); len(list) != 0 {
		t.Fatalf("still subscribed: %+v", list)
	}
	upd = decodeJSON[struct {
		Incident store.PageIncident `json:"incident"`
		Notified int                `json:"notified"`
	}](t, c.do("POST", ibase+"/updates", map[string]any{"status": "resolved", "message": "Done."}))
	if upd.Notified != 0 {
		t.Fatalf("notified after unsubscribe: %d", upd.Notified)
	}

	wantStatus(t, c.do("DELETE", ibase, nil), 200, "delete incident")
	if list, _ := st.PageIncidents(sp.ID, 0); len(list) != 0 {
		t.Fatalf("incident not deleted: %+v", list)
	}
}

func TestSubscribeOnCustomDomainAndLimits(t *testing.T) {
	h, st := newTestHub(t)
	smtpSrv, clientTLS := newFakeSMTP(t, false, true)
	h.notify.tlsConfig = clientTLS
	n := &store.Notifier{Name: "mail", Type: "email", Enabled: true, Config: map[string]string{
		"host": "127.0.0.1", "port": smtpSrv.port(), "security": "starttls",
		"username": smtpSrv.user, "password": smtpSrv.pass, "from": "status@example.com", "to": "ops@example.com",
	}}
	if err := st.CreateNotifier(n); err != nil {
		t.Fatal(err)
	}
	sp := &store.StatusPage{Slug: "acme", Title: "Acme", Domain: "status.acme.test", Published: true, SubscribeNotifierID: n.ID}
	if err := st.CreateStatusPage(sp); err != nil {
		t.Fatal(err)
	}
	h.reloadPageDomains()
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"status/index.html": {Data: []byte("status page")}}))
	defer srv.Close()

	post := func(path, body string) int {
		req, _ := http.NewRequest("POST", srv.URL+path, strings.NewReader(body))
		req.Host = "status.acme.test"
		req.Header.Set("Content-Type", "application/json")
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		return res.StatusCode
	}
	if code := post("/api/public/status/subscribe", `{"email":"jane@example.com"}`); code != 202 {
		t.Fatalf("subscribe on custom domain: %d", code)
	}
	pm := parseMail(t, waitMails(t, smtpSrv, 1)[0])
	if !strings.Contains(pm.body, "https://status.acme.test/?confirm=") {
		t.Fatalf("link must use the custom domain: %s", pm.body)
	}
	token := linkToken(t, pm.body, "confirm")
	if code := post("/api/public/subscriptions/"+token+"/confirm", ""); code != 200 {
		t.Fatalf("confirm on custom domain: %d", code)
	}
	// The admin API stays hidden on the custom domain.
	if code := post("/api/status-pages/1/incidents", `{}`); code != 404 {
		t.Fatalf("admin API on custom domain: %d", code)
	}

	// Asking again and again for the same address is limited.
	codes := []int{}
	for range subscribesPerEmail + 1 {
		codes = append(codes, post("/api/public/status/subscribe", `{"email":"bob@example.com"}`))
	}
	if codes[len(codes)-1] != http.StatusTooManyRequests {
		t.Fatalf("no rate limit: %v", codes)
	}
}

func TestNormalizeEmail(t *testing.T) {
	cases := map[string]string{
		"Jane@Example.COM":               "jane@example.com",
		" jane@example.com ":             "jane@example.com",
		"Jane <jane@example.com>":        "",
		"jane@localhost":                 "",
		"a@b.c, d@e.f":                   "",
		"jane@example.com\r\nBcc: x@y.z": "",
		"":                               "",
	}
	for in, want := range cases {
		if got := normalizeEmail(in); got != want {
			t.Errorf("%q: want %q, got %q", in, want, got)
		}
	}
}

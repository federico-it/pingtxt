package hub

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

type pageTestServer struct {
	t     *testing.T
	h     *Hub
	st    *store.Store
	srv   *httptest.Server
	admin *http.Cookie
}

func newPageTestServer(t *testing.T) *pageTestServer {
	h, st := newTestHub(t)
	ui := fstest.MapFS{"index.html": {Data: []byte("admin")}, "status/index.html": {Data: []byte("status page")}}
	srv := httptest.NewServer(h.Handler(ui))
	t.Cleanup(srv.Close)
	return &pageTestServer{t: t, h: h, st: st, srv: srv, admin: login(t, h, st)}
}

func (s *pageTestServer) do(method, path, body, host string, cookies ...*http.Cookie) (*http.Response, string) {
	s.t.Helper()
	req, _ := http.NewRequest(method, s.srv.URL+path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if host != "" {
		req.Host = host
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res, string(b)
}

func TestPageFeeds(t *testing.T) {
	s := newPageTestServer(t)
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := s.st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	res, body := s.do("POST", "/api/status-pages", `{"slug":"acme","title":"Acme & Co","domain":"status.acme.test","monitor_ids":[`+itoa(m.ID)+`]}`, "", s.admin)
	wantCode(t, res, 201, body)
	var page store.StatusPage
	json.Unmarshal([]byte(body), &page)
	pid := itoa(page.ID)

	// Drafts have no public feed.
	if res, _ := s.do("GET", "/api/public/status/acme/feed.atom", "", ""); res.StatusCode != 404 {
		t.Fatalf("draft feed: %d", res.StatusCode)
	}
	res, body = s.do("PUT", "/api/status-pages/"+pid, `{"published":true}`, "", s.admin)
	wantCode(t, res, 200, body)
	res, body = s.do("POST", "/api/status-pages/"+pid+"/incidents", `{"title":"Slow <logins>","impact":"major","message":"Looking into it.\nMore soon."}`, "", s.admin)
	wantCode(t, res, 201, body)
	var created struct{ Incident store.PageIncident }
	json.Unmarshal([]byte(body), &created)
	res, body = s.do("POST", "/api/status-pages/"+pid+"/incidents/"+itoa(created.Incident.ID)+"/updates", `{"status":"resolved","message":"Fixed."}`, "", s.admin)
	wantCode(t, res, 201, body)

	res, body = s.do("GET", "/api/public/status/acme/feed.atom", "", "")
	wantCode(t, res, 200, body)
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/atom+xml") {
		t.Fatalf("atom content type %q", ct)
	}
	var atom struct {
		Title   string     `xml:"title"`
		Links   []atomLink `xml:"link"`
		Entries []struct {
			Title   string `xml:"title"`
			ID      string `xml:"id"`
			Content string `xml:"content"`
		} `xml:"entry"`
	}
	if err := xml.Unmarshal([]byte(body), &atom); err != nil {
		t.Fatalf("atom: %v\n%s", err, body)
	}
	if atom.Title != "Acme & Co" || len(atom.Entries) != 1 {
		t.Fatalf("atom: %+v", atom)
	}
	e := atom.Entries[0]
	if e.Title != "Resolved: Slow <logins>" || e.ID != "tag:status.acme.test,2026:incident-"+itoa(created.Incident.ID) ||
		!strings.Contains(e.Content, "<p>Looking into it.<br>More soon.</p>") || !strings.Contains(e.Content, "Fixed.") ||
		strings.Index(e.Content, "Fixed.") > strings.Index(e.Content, "Looking") {
		t.Fatalf("atom entry: %+v", e)
	}
	if atom.Links[0].Href != "https://status.acme.test/" || atom.Links[1].Href != "https://status.acme.test/api/public/status/feed.atom" {
		t.Fatalf("atom links: %+v", atom.Links)
	}

	res, body = s.do("GET", "/api/public/status/feed.rss", "", "status.acme.test")
	wantCode(t, res, 200, body)
	var rss struct {
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title string `xml:"title"`
				GUID  string `xml:"guid"`
			} `xml:"item"`
		} `xml:"channel"`
	}
	if err := xml.Unmarshal([]byte(body), &rss); err != nil {
		t.Fatalf("rss: %v\n%s", err, body)
	}
	if rss.Channel.Title != "Acme & Co" || len(rss.Channel.Items) != 1 || rss.Channel.Items[0].Title != "Resolved: Slow <logins>" {
		t.Fatalf("rss: %+v", rss)
	}

	// A password protected page: the feed needs the cookie, a session or the key.
	res, body = s.do("PUT", "/api/status-pages/"+pid, `{"password":"`+pagePass+`"}`, "", s.admin)
	wantCode(t, res, 200, body)
	if res, _ := s.do("GET", "/api/public/status/acme/feed.atom", "", ""); res.StatusCode != 403 {
		t.Fatalf("protected feed: %d", res.StatusCode)
	}
	if res, _ := s.do("GET", "/api/public/status/acme/feed.atom", "", "", s.admin); res.StatusCode != 200 {
		t.Fatalf("protected feed with session: %d", res.StatusCode)
	}
	res, body = s.do("GET", "/api/status-pages/"+pid+"/feeds", "", "", s.admin)
	wantCode(t, res, 200, body)
	var links struct {
		Atom, RSS string
		Private   bool
	}
	json.Unmarshal([]byte(body), &links)
	if !links.Private || !strings.HasPrefix(links.RSS, "https://status.acme.test/api/public/status/feed.rss?key=") {
		t.Fatalf("feed links: %s", body)
	}
	key := links.RSS[strings.Index(links.RSS, "?"):]
	res, body = s.do("GET", "/api/public/status/feed.rss"+key, "", "status.acme.test")
	wantCode(t, res, 200, body)
	if res.Header.Get("Cache-Control") != "private, no-store" {
		t.Fatalf("protected feed cache: %q", res.Header.Get("Cache-Control"))
	}
	if res, _ := s.do("GET", "/api/public/status/feed.rss?key=nope", "", "status.acme.test"); res.StatusCode != 403 {
		t.Fatalf("wrong key: %d", res.StatusCode)
	}
	// A new password changes the key.
	res, body = s.do("PUT", "/api/status-pages/"+pid, `{"password":"other-`+pagePass+`"}`, "", s.admin)
	wantCode(t, res, 200, body)
	s.h.dropPageCache()
	if res, _ := s.do("GET", "/api/public/status/feed.rss"+key, "", "status.acme.test"); res.StatusCode != 403 {
		t.Fatalf("old key: %d", res.StatusCode)
	}
}

func wantCode(t *testing.T, res *http.Response, code int, body string) {
	t.Helper()
	if res.StatusCode != code {
		t.Fatalf("%s %s: want %d, got %d %s", res.Request.Method, res.Request.URL.Path, code, res.StatusCode, body)
	}
}

type hookSink struct {
	mu   sync.Mutex
	got  []hookCall
	fail int // answer 500 to the first fail requests
	srv  *httptest.Server
}

type hookCall struct {
	event, sig string
	body       []byte
}

func newHookSink(t *testing.T) *hookSink {
	k := &hookSink{}
	k.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		k.mu.Lock()
		defer k.mu.Unlock()
		if k.fail > 0 {
			k.fail--
			http.Error(w, "busy", 500)
			return
		}
		k.got = append(k.got, hookCall{event: r.Header.Get("X-Pingtxt-Event"), sig: r.Header.Get("X-Pingtxt-Signature"), body: b})
	}))
	t.Cleanup(k.srv.Close)
	return k
}

func (k *hookSink) calls() []hookCall {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]hookCall(nil), k.got...)
}

func TestPageWebhooks(t *testing.T) {
	s := newPageTestServer(t)
	s.h.hooks.retry = []time.Duration{time.Millisecond, time.Millisecond}
	sink := newHookSink(t)
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := s.st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	res, body := s.do("POST", "/api/status-pages", `{"slug":"acme","title":"Acme","published":true,"monitor_ids":[`+itoa(m.ID)+`]}`, "", s.admin)
	wantCode(t, res, 201, body)
	var page store.StatusPage
	json.Unmarshal([]byte(body), &page)
	pid := itoa(page.ID)

	// Validation.
	res, body = s.do("POST", "/api/status-pages/"+pid+"/webhooks", `{"url":"ftp://x"}`, "", s.admin)
	wantCode(t, res, 400, body)
	res, body = s.do("POST", "/api/status-pages/"+pid+"/webhooks", `{"url":"`+sink.srv.URL+`","events":["nope"]}`, "", s.admin)
	wantCode(t, res, 400, body)

	res, body = s.do("POST", "/api/status-pages/"+pid+"/webhooks", `{"url":"`+sink.srv.URL+`"}`, "", s.admin)
	wantCode(t, res, 201, body)
	var hook store.PageWebhook
	json.Unmarshal([]byte(body), &hook)
	if !hook.Enabled || !strings.HasPrefix(hook.Secret, "whsec_") || len(hook.Events) != 0 {
		t.Fatalf("webhook: %s", body)
	}
	hid := itoa(hook.ID)

	// PUT keeps what the body leaves out; an empty secret makes a new one.
	res, body = s.do("PUT", "/api/status-pages/"+pid+"/webhooks/"+hid, `{"events":["incident.created","incident.resolved","page.status_changed","monitor.status_changed"]}`, "", s.admin)
	wantCode(t, res, 200, body)
	var upd store.PageWebhook
	json.Unmarshal([]byte(body), &upd)
	if upd.URL != hook.URL || upd.Secret != hook.Secret || len(upd.Events) != 4 {
		t.Fatalf("put: %s", body)
	}

	// Test event.
	res, body = s.do("POST", "/api/status-pages/"+pid+"/webhooks/"+hid+"/test", "", "", s.admin)
	wantCode(t, res, 200, body)
	if !strings.Contains(body, `"ok":true`) {
		t.Fatalf("test: %s", body)
	}

	// Incident events, signed. incident.updated is not picked.
	sink.mu.Lock()
	sink.fail = 1 // the first delivery is retried
	sink.mu.Unlock()
	res, body = s.do("POST", "/api/status-pages/"+pid+"/incidents", `{"title":"Down","impact":"major","message":"Looking."}`, "", s.admin)
	wantCode(t, res, 201, body)
	var created struct{ Incident store.PageIncident }
	json.Unmarshal([]byte(body), &created)
	iid := itoa(created.Incident.ID)
	s.h.hooks.wg.Wait()
	res, body = s.do("POST", "/api/status-pages/"+pid+"/incidents/"+iid+"/updates", `{"status":"identified","message":"Found it."}`, "", s.admin)
	wantCode(t, res, 201, body)
	res, body = s.do("POST", "/api/status-pages/"+pid+"/incidents/"+iid+"/updates", `{"status":"resolved","message":"Quiet fix.","notify":false}`, "", s.admin)
	wantCode(t, res, 201, body)
	res, body = s.do("POST", "/api/status-pages/"+pid+"/incidents/"+iid+"/updates", `{"status":"resolved","message":"Fixed."}`, "", s.admin)
	wantCode(t, res, 201, body)
	s.h.hooks.wg.Wait()

	calls := sink.calls()
	if len(calls) != 3 || calls[0].event != "ping" || calls[1].event != "incident.created" || calls[2].event != "incident.resolved" {
		t.Fatalf("calls: %+v", calls)
	}
	for _, c := range calls {
		ts, mac, _ := strings.Cut(strings.TrimPrefix(c.sig, "t="), ",v1=")
		hm := hmac.New(sha256.New, []byte(hook.Secret))
		hm.Write([]byte(ts + "."))
		hm.Write(c.body)
		if mac != hex.EncodeToString(hm.Sum(nil)) {
			t.Fatalf("bad signature %q", c.sig)
		}
	}
	var payload struct {
		Event    string
		Page     struct{ Slug string }
		Incident store.PageIncident
		Update   store.PageIncidentUpdate
	}
	json.Unmarshal(calls[2].body, &payload)
	if payload.Page.Slug != "acme" || payload.Incident.ID != created.Incident.ID || payload.Update.Message != "Fixed." || payload.Incident.Status != "resolved" {
		t.Fatalf("payload: %s", calls[2].body)
	}
	got, _ := s.st.PageWebhook(page.ID, hook.ID)
	if got.LastAt == 0 || got.LastStatus != 200 || got.LastError != "" {
		t.Fatalf("last result: %+v", got)
	}

	// Status changes: the first look only records the status.
	before := len(sink.calls())
	s.h.watchPages()
	if err := s.st.SetMonitorStatus(m.ID, StatusUp, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	s.h.dropPageCache()
	s.h.watchPages()
	if err := s.st.SetMonitorStatus(m.ID, StatusDown, time.Now().UnixMilli()); err != nil {
		t.Fatal(err)
	}
	s.h.dropPageCache()
	s.h.watchPages()
	s.h.hooks.wg.Wait()
	calls = sink.calls()[before:]
	if len(calls) != 2 || calls[0].event != "monitor.status_changed" || calls[1].event != "page.status_changed" {
		t.Fatalf("status calls: %+v", calls)
	}
	var change struct {
		Status         string `json:"status"`
		PreviousStatus string `json:"previous_status"`
	}
	json.Unmarshal(calls[1].body, &change)
	if change.Status != PageMajorOutage || change.PreviousStatus != PageOperational {
		t.Fatalf("page change: %s", calls[1].body)
	}

	// Disabled webhooks and drafts send nothing.
	res, body = s.do("PUT", "/api/status-pages/"+pid+"/webhooks/"+hid, `{"enabled":false}`, "", s.admin)
	wantCode(t, res, 200, body)
	before = len(sink.calls())
	res, body = s.do("POST", "/api/status-pages/"+pid+"/incidents", `{"title":"Again","message":"Hm."}`, "", s.admin)
	wantCode(t, res, 201, body)
	s.h.hooks.wg.Wait()
	if len(sink.calls()) != before {
		t.Fatal("disabled webhook got an event")
	}

	res, body = s.do("DELETE", "/api/status-pages/"+pid+"/webhooks/"+hid, "", "", s.admin)
	wantCode(t, res, 200, body)
	res, body = s.do("GET", "/api/status-pages/"+pid+"/webhooks", "", "", s.admin)
	wantCode(t, res, 200, body)
	if !strings.Contains(body, `"webhooks":[]`) {
		t.Fatalf("list after delete: %s", body)
	}
}

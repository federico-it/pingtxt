package hub

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestBadges(t *testing.T) {
	h, st := newTestHub(t)
	newMon := func(name string) *store.Monitor {
		m := &store.Monitor{Name: name, Type: "http", Target: "http://x.internal", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
		if err := st.CreateMonitor(m); err != nil {
			t.Fatal(err)
		}
		h.MonitorChanged(m.ID)
		return m
	}
	shown, hidden := newMon("api <&>"), newMon("secret")
	h.HandleResult(LocalLocation, result(shown.ID, false))
	h.HandleResult(LocalLocation, result(shown.ID, true))
	h.HandleResult(LocalLocation, result(shown.ID, true))

	page := &store.StatusPage{Slug: "acme", Title: "Acme", Published: true, MonitorIDs: []int64{shown.ID}}
	if err := st.CreateStatusPage(page); err != nil {
		t.Fatal(err)
	}
	draft := &store.StatusPage{Slug: "draft", Title: "Draft", MonitorIDs: []int64{shown.ID}}
	if err := st.CreateStatusPage(draft); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	cookie := login(t, h, st)
	get := func(path string, withSession bool) (int, string) {
		t.Helper()
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		if withSession {
			req.AddCookie(cookie)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "image/svg+xml") {
			t.Fatalf("%s: content type %q", path, ct)
		}
		return res.StatusCode, string(b)
	}
	id := itoa(shown.ID)

	cases := []struct {
		path string
		code int
		want []string
	}{
		{"/api/badge/acme/status.svg", 200, []string{"Acme", "operational"}},
		{"/api/badge/ACME/status", 200, []string{"operational"}},
		{"/api/badge/acme/" + id + "/status.svg", 200, []string{"api &lt;&amp;&gt;: up", badgeGreen}},
		{"/api/badge/acme/" + id + "/uptime.svg", 200, []string{"uptime 30d", "66.66%"}},
		{"/api/badge/acme/" + id + "/uptime.svg?period=24h&label=SLA", 200, []string{"SLA: 66.66%"}},
		{"/api/badge/acme/" + id + "/response.svg", 200, []string{"response 24h", " ms"}},
		{"/api/badge/acme/" + id + "/status.svg?style=flat-square", 200, []string{`rx="0"`}},
		{"/api/badge/acme/" + id + "/uptime.svg?period=1y", 400, []string{"bad period"}},
		{"/api/badge/acme/" + id + "/nope.svg", 404, []string{"not found"}},
		{"/api/badge/acme/" + itoa(hidden.ID) + "/status.svg", 404, []string{"not found"}},
		{"/api/badge/acme/abc/status.svg", 404, []string{"not found"}},
		{"/api/badge/missing/status.svg", 404, []string{"not found"}},
		{"/api/badge/draft/" + id + "/status.svg", 404, []string{"not found"}},
	}
	for _, c := range cases {
		code, body := get(c.path, false)
		if code != c.code {
			t.Errorf("%s: want %d, got %d", c.path, c.code, code)
		}
		for _, w := range c.want {
			if !strings.Contains(body, w) {
				t.Errorf("%s: %q not in %s", c.path, w, body)
			}
		}
		if strings.Contains(body, "secret") || strings.Contains(body, "x.internal") {
			t.Errorf("%s leaks data: %s", c.path, body)
		}
	}
	// On a custom domain, only the badges of that page.
	if err := st.UpdateStatusPage(&store.StatusPage{ID: page.ID, Slug: "acme", Title: "Acme", Domain: "status.acme.test", Published: true, MonitorIDs: []int64{shown.ID}}); err != nil {
		t.Fatal(err)
	}
	h.reloadPageDomains()
	for path, want := range map[string]int{"/api/badge/acme/status.svg": 200, "/api/badge/draft/status.svg": 404} {
		req, _ := http.NewRequest("GET", srv.URL+path, nil)
		req.Host = "status.acme.test"
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != want {
			t.Errorf("custom domain %s: want %d, got %d", path, want, res.StatusCode)
		}
	}

	// A session does not unlock badges of drafts: they are meant for READMEs.
	if code, _ := get("/api/badge/draft/status.svg", true); code != 404 {
		t.Fatalf("draft badge with session: want 404, got %d", code)
	}
}

func TestFormatUptime(t *testing.T) {
	for in, want := range map[float64]string{100: "100%", 99.999: "99.99%", 99.5: "99.5%", 0: "0%", 66.666: "66.66%"} {
		if got := formatUptime(in); got != want {
			t.Errorf("%v: want %s, got %s", in, want, got)
		}
	}
}

func TestBadgeWidthFitsText(t *testing.T) {
	short := string(renderBadge(badge{"a", "b", badgeGrey}, false))
	long := string(renderBadge(badge{"a much longer label", "b", badgeGrey}, false))
	if len(long) <= len(short) || !strings.Contains(long, `width="`) {
		t.Fatal("unexpected render")
	}
	if textWidth("WWW") <= textWidth("iii") {
		t.Fatal("width table looks wrong")
	}
}

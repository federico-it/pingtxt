package hub

import (
	"net/mail"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// mailSamples renders one message of every kind the hub sends.
func mailSamples(t *testing.T) map[string]fakeMail {
	t.Helper()
	ec := &emailConfig{From: &mail.Address{Name: "pingtxt", Address: "alerts@example.com"}, To: []*mail.Address{{Address: "ops@example.com"}}}
	now := time.Date(2026, 10, 31, 14, 5, 0, 0, time.UTC)
	at := now.UnixMilli()
	base := "https://hub.example.com"
	days := func(n int) *int { return &n }
	out := map[string]fakeMail{}
	add := func(name string, m mailMsg) { out[name] = fakeMail{data: string(writeMail(ec, m))} }

	add("alert-down", buildEmail(ec, Event{MonitorID: 3, Monitor: "hub api", Target: "https://api.example.com/health", Status: StatusDown, Cause: "http 503: Service Unavailable", At: at}, base, now))
	add("alert-still-down", buildEmail(ec, Event{MonitorID: 3, Monitor: "hub api", Target: "https://api.example.com/health", Status: StatusDown, Cause: "dial tcp 10.0.0.4:443: connect: connection refused", At: at, DownSince: at - 47*60_000, Reminder: 2}, base, now))
	add("alert-up", buildEmail(ec, Event{MonitorID: 3, Monitor: "hub api", Target: "https://api.example.com/health", Status: StatusUp, At: at, DownSince: at - 52*60_000}, base, now))
	add("alert-test", buildEmail(ec, Event{Monitor: "example.com", Target: "https://example.com", Status: StatusDown, Cause: "this is a test notification", At: at, Test: true}, "", now))
	add("cert-expiring", buildEmail(ec, Event{Kind: KindCert, MonitorID: 4, Monitor: "website", Target: "https://example.com", Status: ExpiryExpiring, Subject: "example.com", ExpiresAt: at + 9*86400_000, DaysLeft: days(9), At: at}, base, now))
	add("domain-expired", buildEmail(ec, Event{Kind: KindDomain, MonitorID: 4, Monitor: "website", Target: "https://example.com", Status: ExpiryExpired, Subject: "example.com", ExpiresAt: at - 86400_000, DaysLeft: days(-1), At: at}, base, now))

	from := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, 7)
	r := &Report{Kind: ReportWeekly, Timezone: "Europe/Rome", Uptime: 0.99412, PrevUptime: 0.9998, Incidents: 3, DowntimeMs: 7*3600_000 + 31*60_000, Monitors: 4, URL: "hub.example.com", Groups: []ReportGroup{
		{Name: "production", Uptime: 0.98824, PrevUptime: 0.9996, Incidents: 2, DowntimeMs: 7*3600_000 + 1*60_000, Monitors: []ReportMonitor{
			{Name: "api", Uptime: 0.9583, PrevUptime: 1, AvgMs: 142, Incidents: 2, DowntimeMs: 7*3600_000 + 60_000},
			{Name: "website", Uptime: 1, PrevUptime: 0.9992, AvgMs: 318, Incidents: 0},
		}, IncidentList: []ReportIncident{
			{Monitor: "api", StartedAt: from.Add(34 * time.Hour).UnixMilli(), ResolvedAt: 1, DurationMs: 7 * 3600_000, Cause: "timeout after 10s"},
			{Monitor: "api", StartedAt: from.Add(130 * time.Hour).UnixMilli(), Cause: "http 502: Bad Gateway"},
		}},
		{Name: "No group", Uptime: 0.9995, PrevUptime: 1, Incidents: 1, DowntimeMs: 30 * 60_000, Monitors: []ReportMonitor{
			{Name: "gateway ping", Uptime: 0.9995, PrevUptime: 1, AvgMs: 0.3, Incidents: 1, DowntimeMs: 30 * 60_000},
			{Name: "dns cloudflare", Uptime: 1, PrevUptime: 1, AvgMs: 6.4},
		}, IncidentList: []ReportIncident{
			{Monitor: "gateway ping", StartedAt: from.Add(80 * time.Hour).UnixMilli(), ResolvedAt: 1, DurationMs: 30 * 60_000, Cause: "100% packet loss"},
		}},
	}}
	r.Subject, r.Text = reportSubject(r, from, to), reportText(r, from, to)
	html, err := reportHTMLBody(r, from, to)
	if err != nil {
		t.Fatal(err)
	}
	add("report-weekly", buildReportEmail(ec, r.Subject, r.Text, html, now))

	page := &store.StatusPage{Title: "Acme <Status>", Slug: "acme"}
	sub := &store.PageSubscriber{Email: "jane@example.com", Token: strings.Repeat("ab", 24)}
	links := pageLinks{page: "https://status.acme.test/", api: "https://status.acme.test"}
	add("subscribe-confirm", buildConfirmEmail(page, sub, links, now))
	inc := &store.PageIncident{Title: "Slow API responses", Impact: store.ImpactMajor, CreatedAt: at - 40*60_000}
	add("incident-update", buildUpdateEmail(page, inc, store.PageIncidentUpdate{Status: store.ReportIdentified, Message: "A database node is slow.\nWe are failing over to the replica."}, sub, links, now))
	inc2 := *inc
	inc2.ResolvedAt = at
	add("incident-resolved", buildUpdateEmail(page, &inc2, store.PageIncidentUpdate{Status: store.ReportResolved, Message: "Latency is back to normal."}, sub, links, now))
	return out
}

func TestMailViews(t *testing.T) {
	dir := os.Getenv("PINGTXT_MAIL_PREVIEW") // write the HTML parts there, to look at them
	for name, m := range mailSamples(t) {
		pm := parseMail(t, m)
		if !strings.Contains(pm.html, `name="color-scheme"`) || !strings.Contains(pm.html, "&gt; pingtxt") || !strings.Contains(pm.body, "sent by pingtxt") {
			t.Errorf("%s: layout missing", name)
		}
		if strings.Contains(pm.html, "<Status>") || strings.Contains(pm.html, "ZgotmplZ") {
			t.Errorf("%s: unescaped or rejected value in html", name)
		}
		if dir != "" {
			must(t, os.WriteFile(filepath.Join(dir, name+".html"), []byte(pm.html), 0o644))
			must(t, os.WriteFile(filepath.Join(dir, name+".txt"), []byte(pm.body), 0o644))
		}
	}
	s := mailSamples(t)
	checks := map[string][]string{
		"alert-down":        {"[DOWN] hub api", "http 503", "https://hub.example.com/monitors/view/?id=3"},
		"alert-still-down":  {"[STILL DOWN] hub api", "47m", "#2"},
		"alert-up":          {"[UP] hub api", "52m"},
		"cert-expiring":     {"[CERT] example.com", "9 days"},
		"report-weekly":     {"## production", "95.83%", "timeout after 10s", "ongoing", "dashboard: https://hub.example.com/"},
		"subscribe-confirm": {"?confirm=" + strings.Repeat("ab", 24)},
		"incident-update":   {"[IDENTIFIED] Slow API responses", "> We are failing over to the replica.", "?unsubscribe="},
	}
	for name, want := range checks {
		pm := parseMail(t, s[name])
		all := pm.body + pm.html
		for _, w := range want {
			if !strings.Contains(all, w) && !strings.Contains(all, strings.ReplaceAll(w, "&", "&amp;")) {
				t.Errorf("%s: no %q in\n%s", name, w, pm.body)
			}
		}
	}
	// The test notification links nowhere.
	if pm := parseMail(t, s["alert-test"]); strings.Contains(pm.html, "monitors/view") {
		t.Errorf("test alert links a monitor")
	}
}

package hub

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestMaintenanceOnStatusPages(t *testing.T) {
	h, st := newTestHub(t)
	smtpSrv, clientTLS := newFakeSMTP(t, false, true)
	h.notify.tlsConfig = clientTLS
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"status/index.html": {Data: []byte("status page")}}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	wantStatus(t, c.do("POST", "/api/setup", map[string]string{"email": "admin@example.com", "password": "password123"}), 201, "setup")
	ch := decodeJSON[store.Notifier](t, c.do("POST", "/api/notifiers", map[string]any{"name": "mail", "type": "email", "enabled": true, "config": map[string]string{
		"host": "127.0.0.1", "port": smtpSrv.port(), "security": "starttls",
		"username": smtpSrv.user, "password": smtpSrv.pass, "from": "status@example.com", "to": "ops@example.com",
	}}))
	api := decodeJSON[store.Monitor](t, c.do("POST", "/api/monitors", map[string]any{"name": "api", "type": "http", "target": "https://api.example.com", "interval_sec": 60, "locations": []string{LocalLocation}}))
	other := decodeJSON[store.Monitor](t, c.do("POST", "/api/monitors", map[string]any{"name": "other", "type": "http", "target": "https://other.example.com", "interval_sec": 60, "locations": []string{LocalLocation}}))
	// The custom domain gives the scheduler's emails their links.
	sp := decodeJSON[store.StatusPage](t, c.do("POST", "/api/status-pages", map[string]any{"slug": "acme", "title": "Acme", "published": true, "domain": "status.acme.test", "monitor_ids": []int64{api.ID}, "subscribe_notifier_id": ch.ID}))
	sub := &store.PageSubscriber{PageID: sp.ID, Email: "jane@example.com", Token: newSubscriberToken()}
	must(t, st.CreatePageSubscriber(sub))
	must(t, st.ConfirmPageSubscriber(sub.ID))

	maints := func() []publicMaintenance {
		t.Helper()
		h.dropPageCache()
		return decodeJSON[publicPage](t, c.do("GET", "/api/public/status/acme", nil)).Maintenances
	}
	subject := func(m fakeMail) string { return parseMail(t, m).header.Get("Subject") }

	// A window that is not public, or that covers no monitor of the page,
	// stays off the page and sends nothing.
	now := time.Now()
	start := now.Add(2 * time.Hour).Truncate(time.Minute)
	win := map[string]any{"name": "Database upgrade", "kind": "once", "monitor_ids": []int64{api.ID}, "enabled": true,
		"start_at": start.UnixMilli(), "end_at": start.Add(time.Hour).UnixMilli(), "timezone": "Europe/Rome"}
	w := decodeJSON[maintView](t, c.do("POST", "/api/maintenances", win))
	if w.Public || w.NotifySubscribers || w.RemindMin != 0 {
		t.Fatalf("defaults: %+v", w.Maintenance)
	}
	// (a window on a monitor of no page: it never emails anyone)
	wantStatus(t, c.do("POST", "/api/maintenances", map[string]any{"name": "other", "kind": "once", "monitor_ids": []int64{other.ID}, "enabled": true, "public": true, "notify_subscribers": true,
		"start_at": start.UnixMilli(), "end_at": start.Add(time.Hour).UnixMilli()}), 201, "window elsewhere")
	if got := maints(); len(got) != 0 {
		t.Fatalf("private window on the page: %+v", got)
	}

	// Announced: on the page, and emailed to the subscribers.
	base := "/api/maintenances/" + strconv.FormatInt(w.ID, 10)
	w = decodeJSON[maintView](t, c.do("PUT", base, map[string]any{"public": true, "notify_subscribers": true, "announcement": "The API is read-only for an hour.", "remind_min": 60}))
	if !w.Public || !w.NotifySubscribers || w.RemindMin != 60 || w.Name != "Database upgrade" {
		t.Fatalf("saved: %+v", w.Maintenance)
	}
	got := maints()
	if len(got) != 1 || got[0].Status != MaintScheduled || got[0].Title != "Database upgrade" || got[0].Description != "The API is read-only for an hour." ||
		got[0].StartsAt != start.UnixMilli() || len(got[0].MonitorIDs) != 1 || got[0].MonitorIDs[0] != api.ID {
		t.Fatalf("public maintenances: %+v", got)
	}
	mails := waitMails(t, smtpSrv, 1)
	pm := parseMail(t, mails[0])
	if s := pm.header.Get("Subject"); s != "[Acme] Scheduled maintenance: Database upgrade" {
		t.Fatalf("subject: %q", s)
	}
	for _, want := range []string{"read-only for an hour", "CET", "affects", "api", "https://status.acme.test/?unsubscribe=" + sub.Token} {
		if !strings.Contains(pm.body, want) {
			t.Errorf("announcement has no %q:\n%s", want, pm.body)
		}
	}
	if strings.Contains(pm.body, "other") {
		t.Error("the announcement lists a monitor that is not on the page")
	}

	// Saving again without a new schedule sends nothing; rescheduling does.
	c.do("PUT", base, map[string]any{"announcement": "Read-only for an hour."}).Body.Close()
	start = start.Add(time.Hour)
	c.do("PUT", base, map[string]any{"start_at": start.UnixMilli(), "end_at": start.Add(time.Hour).UnixMilli()}).Body.Close()
	waitMails(t, smtpSrv, 2)

	// The scheduler: reminder, start, end; each once.
	tick := func(at time.Time) {
		h.maintNoticeAt = time.Time{}
		h.maintNoticeTick(at)
	}
	tick(start.Add(-2 * time.Hour)) // too early
	tick(start.Add(-30 * time.Minute))
	tick(start.Add(-29 * time.Minute))
	mails = waitMails(t, smtpSrv, 3)
	if s := subject(mails[2]); s != "[Acme] Maintenance starts soon: Database upgrade" {
		t.Fatalf("reminder: %q", s)
	}
	tick(start.Add(time.Minute))
	tick(start.Add(2 * time.Minute))
	mails = waitMails(t, smtpSrv, 4)
	if s := subject(mails[3]); s != "[Acme] Maintenance in progress: Database upgrade" {
		t.Fatalf("start: %q", s)
	}
	tick(start.Add(61 * time.Minute))
	tick(start.Add(62 * time.Minute))
	mails = waitMails(t, smtpSrv, 5)
	if s := subject(mails[4]); s != "[Acme] Maintenance completed: Database upgrade" {
		t.Fatalf("end: %q", s)
	}
	stored, err := st.Maintenance(w.ID)
	must(t, err)
	if stored.NoticeStart != start.UnixMilli() || stored.NoticeEnd != start.UnixMilli() || stored.NoticeRemind != start.UnixMilli() {
		t.Fatalf("notices: %+v", stored)
	}

	// A start missed by more than a few minutes is not emailed late.
	late := start.Add(24 * time.Hour)
	c.do("PUT", base, map[string]any{"notify_subscribers": false, "start_at": late.UnixMilli(), "end_at": late.Add(time.Hour).UnixMilli()}).Body.Close()
	c.do("PUT", base, map[string]any{"notify_subscribers": true}).Body.Close() // announced again
	waitMails(t, smtpSrv, 6)
	tick(late.Add(30 * time.Minute))
	tick(late.Add(31 * time.Minute))
	time.Sleep(100 * time.Millisecond)
	waitMails(t, smtpSrv, 6)

	// Deleting an upcoming window cancels it.
	next := late.Add(24 * time.Hour)
	c.do("PUT", base, map[string]any{"start_at": next.UnixMilli(), "end_at": next.Add(time.Hour).UnixMilli()}).Body.Close()
	waitMails(t, smtpSrv, 7)
	wantStatus(t, c.do("DELETE", base, nil), 200, "delete")
	mails = waitMails(t, smtpSrv, 8)
	if s := subject(mails[7]); s != "[Acme] Maintenance cancelled: Database upgrade" {
		t.Fatalf("cancel: %q", s)
	}
	if got := maints(); len(got) != 0 {
		t.Fatalf("deleted window still on the page: %+v", got)
	}

	wantStatus(t, c.do("PUT", base, map[string]any{"name": "x"}), 404, "deleted")
	wantStatus(t, c.do("POST", "/api/maintenances", map[string]any{"name": "bad", "kind": "once", "all_monitors": true, "start_at": start.UnixMilli(), "end_at": start.Add(time.Hour).UnixMilli(), "remind_min": -1}), 400, "negative reminder")
}

func TestMaintenanceOnPageRecurring(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "web", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{LocalLocation}}
	must(t, st.CreateMonitor(m))
	w := &store.Maintenance{Name: "nightly", Kind: store.MaintDaily, StartTime: "02:00", DurationMin: 30, Timezone: "UTC", Enabled: true, Public: true, AllMonitors: true}
	must(t, st.CreateMaintenance(w))
	must(t, h.MaintenancesChanged())
	ms := []pageMonitor{{m: m}}
	at := time.Date(2026, 11, 8, 2, 10, 0, 0, time.UTC)
	got := h.pageMaintenances(ms, at)
	if len(got) != 1 || got[0].Status != MaintInProgress || got[0].EndsAt != time.Date(2026, 11, 8, 2, 30, 0, 0, time.UTC).UnixMilli() || got[0].Kind != store.MaintDaily {
		t.Fatalf("in progress: %+v", got)
	}
	got = h.pageMaintenances(ms, at.Add(time.Hour))
	if len(got) != 1 || got[0].Status != MaintScheduled || got[0].StartsAt != time.Date(2026, 11, 9, 2, 0, 0, 0, time.UTC).UnixMilli() {
		t.Fatalf("next: %+v", got)
	}
	if got := h.pageMaintenances(nil, at); len(got) != 0 {
		t.Fatalf("a page without monitors: %+v", got)
	}
	if rep := maintRepeat(w); rep != "every day at 02:00 UTC, for 30m" {
		t.Errorf("repeat: %q", rep)
	}
}

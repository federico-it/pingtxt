package hub

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http/httptest"
	"net/mail"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestReportPeriod(t *testing.T) {
	rome, _ := time.LoadLocation("Europe/Rome")
	at := func(s string) time.Time {
		v, err := time.ParseInLocation("2006-01-02 15:04", s, rome)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	day := func(v time.Time) string { return v.Format("2006-01-02 15:04 MST") }

	// Saturday 31 Oct 2026: last week is Mon 19 - Mon 26 Oct (DST ends on 25 Oct).
	from, to := reportPeriod(ReportWeekly, at("2026-10-31 12:00"), rome)
	if day(from) != "2026-10-19 00:00 CEST" || day(to) != "2026-10-26 00:00 CET" {
		t.Fatalf("weekly: %s - %s", day(from), day(to))
	}
	// On Monday the week that just ended is the last complete one.
	from, _ = reportPeriod(ReportWeekly, at("2026-11-02 00:30"), rome)
	if day(from) != "2026-10-26 00:00 CET" {
		t.Fatalf("weekly on monday: %s", day(from))
	}
	from, to = reportPeriod(ReportMonthly, at("2026-11-01 09:00"), rome)
	if day(from) != "2026-10-01 00:00 CEST" || day(to) != "2026-11-01 00:00 CET" {
		t.Fatalf("monthly: %s - %s", day(from), day(to))
	}

	s := ReportSettings{Weekly: true, Monthly: true, Hour: 8, Timezone: "Europe/Rome"}
	now := at("2026-10-31 12:00")
	// Never sent: the last week is due at once (Monday 26 at 8:00 is past).
	if n := nextReport(ReportWeekly, s, reportState{}, now); day(n) != "2026-10-26 08:00 CET" {
		t.Fatalf("next weekly, never sent: %s", day(n))
	}
	st := reportState{ReportWeekly: {From: at("2026-10-19 00:00").UnixMilli()}}
	if n := nextReport(ReportWeekly, s, st, now); day(n) != "2026-11-02 08:00 CET" {
		t.Fatalf("next weekly: %s", day(n))
	}
	st[ReportMonthly] = reportSent{From: at("2026-09-01 00:00").UnixMilli()}
	if n := nextReport(ReportMonthly, s, st, now); day(n) != "2026-11-01 08:00 CET" {
		t.Fatalf("next monthly: %s", day(n))
	}
}

func TestShortDuration(t *testing.T) {
	for ms, want := range map[int64]string{0: "0m", 30_000: "<1m", 42 * 60_000: "42m", 3 * 3600_000: "3h", (3*60 + 5) * 60_000: "3h 5m", 50 * 3600_000: "2d 2h", 48 * 3600_000: "2d"} {
		if got := shortDuration(ms); got != want {
			t.Errorf("shortDuration(%d) = %q, want %q", ms, got, want)
		}
	}
	if pct(0.99999) != "99.99%" || pct(1) != "100%" || pct(-1) != "n/a" {
		t.Fatalf("pct: %s %s %s", pct(0.99999), pct(1), pct(-1))
	}
}

// reportFixture makes two groups and three monitors with a week of history.
func reportFixture(t *testing.T, st *store.Store, from time.Time) (prod, staging *store.Group) {
	t.Helper()
	prod, staging = &store.Group{Name: "production"}, &store.Group{Name: "staging"}
	must(t, st.CreateGroup(prod))
	must(t, st.CreateGroup(staging))
	mk := func(name string, group int64) *store.Monitor {
		m := &store.Monitor{Name: name, Type: "http", Target: "http://" + name, IntervalSec: 60, Retries: 1, GroupID: group}
		must(t, st.CreateMonitor(m))
		return m
	}
	api, web, solo := mk("api", prod.ID), mk("web", staging.ID), mk("solo", 0)
	mk("new", prod.ID) // no data: left out

	f := from.UnixMilli()
	var rows []*store.ResultRow
	// One check per hour for 7 days; api is down for 7 of the 168 hours.
	for i := range 168 {
		ts := f + int64(i)*int64(time.Hour/time.Millisecond)
		state := store.MonUp
		if i >= 10 && i < 17 {
			state = store.MonDown
		}
		rows = append(rows,
			&store.ResultRow{MonitorID: api.ID, Location: "hub", TS: ts, OK: state == store.MonUp, LatencyMs: 100, MonState: state},
			&store.ResultRow{MonitorID: web.ID, Location: "hub", TS: ts, OK: true, LatencyMs: 50},
			&store.ResultRow{MonitorID: solo.ID, Location: "hub", TS: ts, OK: true, LatencyMs: 20})
	}
	// The week before: api was always up.
	rows = append(rows, &store.ResultRow{MonitorID: api.ID, Location: "hub", TS: f - 3600_000, OK: true, LatencyMs: 100})
	must(t, st.InsertResults(rows))
	h := int64(time.Hour / time.Millisecond)
	_, err := st.OpenIncident(api.ID, f+10*h, "timeout")
	must(t, err)
	must(t, st.ResolveIncident(api.ID, f+17*h))
	// Started the week before, resolved 30 minutes into this one.
	_, err = st.OpenIncident(web.ID, f-2*h, "502")
	must(t, err)
	must(t, st.ResolveIncident(web.ID, f+h/2))
	return prod, staging
}

func TestBuildReport(t *testing.T) {
	h, st := newTestHub(t)
	now := time.Now().UTC()
	from, to := reportPeriod(ReportWeekly, now, time.UTC)
	prod, staging := reportFixture(t, st, from)

	check := func(label string) {
		t.Helper()
		r, err := h.buildReport(ReportWeekly, from, to, ReportSettings{Timezone: "UTC"})
		if err != nil {
			t.Fatal(err)
		}
		if r.Monitors != 3 || r.Incidents != 2 || len(r.Groups) != 3 {
			t.Fatalf("%s: %d monitors, %d incidents, %d groups", label, r.Monitors, r.Incidents, len(r.Groups))
		}
		names := []string{r.Groups[0].Name, r.Groups[1].Name, r.Groups[2].Name}
		if strings.Join(names, ",") != "production,staging,No group" {
			t.Fatalf("%s: groups %v", label, names)
		}
		p := r.Groups[0]
		if len(p.Monitors) != 1 || p.Monitors[0].Uptime != 161.0/168 || p.Monitors[0].PrevUptime != 1 || p.DowntimeMs != 7*3600_000 {
			t.Fatalf("%s: production %+v", label, p)
		}
		if s := r.Groups[1]; s.DowntimeMs != 1800_000 || s.Uptime != 1 || len(s.IncidentList) != 1 {
			t.Fatalf("%s: staging %+v", label, s)
		}
		if want := (161.0/168 + 1 + 1) / 3; r.Uptime != want {
			t.Fatalf("%s: uptime %v, want %v", label, r.Uptime, want)
		}
		if !strings.Contains(r.Subject, "Weekly report") || !strings.Contains(r.Subject, "2 incidents") {
			t.Fatalf("%s: subject %q", label, r.Subject)
		}
		for _, s := range []string{"== production ==", "api", "95.83%", "timeout", "7h", "== No group =="} {
			if !strings.Contains(r.Text, s) {
				t.Fatalf("%s: text has no %q:\n%s", label, s, r.Text)
			}
		}
	}
	check("raw results")
	// The same from the hourly rollups.
	if _, err := st.RollupResults(now, nil); err != nil {
		t.Fatal(err)
	}
	check("rollups")

	r, err := h.buildReport(ReportWeekly, from, to, ReportSettings{Timezone: "UTC", GroupIDs: []int64{staging.ID}})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Groups) != 1 || r.Groups[0].ID != staging.ID || r.Incidents != 1 {
		t.Fatalf("filtered: %+v", r.Groups)
	}
	_ = prod
}

func TestReportEmailAndSchedule(t *testing.T) {
	h, st := newTestHub(t)
	smtpSrv, clientTLS := newFakeSMTP(t, false, true)
	h.notify.tlsConfig = clientTLS
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	c := newClient(t, srv.URL)
	if res := c.do("POST", "/api/setup", map[string]string{"email": "admin@example.com", "password": "password123"}); res.StatusCode != 201 {
		t.Fatalf("setup: %d", res.StatusCode)
	}
	now := time.Now().UTC()
	from, _ := reportPeriod(ReportWeekly, now, time.UTC)
	reportFixture(t, st, from)

	mailCh := &store.Notifier{Name: "mail", Type: "email", Enabled: true, Config: map[string]string{
		"host": "127.0.0.1", "port": smtpSrv.port(), "security": "starttls", "username": smtpSrv.user, "password": smtpSrv.pass,
		"from": "pingtxt <alerts@example.com>", "to": "ops@example.com"}}
	must(t, st.CreateNotifier(mailCh))
	hook := &store.Notifier{Name: "hook", Type: "webhook", Enabled: true, Config: map[string]string{"url": "http://127.0.0.1:1"}}
	must(t, st.CreateNotifier(hook))

	// Only email channels, and one is needed to turn the reports on.
	if res := c.do("PUT", "/api/reports", map[string]any{"weekly": true, "notifier_ids": []int64{hook.ID}}); res.StatusCode != 400 {
		t.Fatalf("webhook channel: %d", res.StatusCode)
	}
	if res := c.do("PUT", "/api/reports", map[string]any{"weekly": true}); res.StatusCode != 400 {
		t.Fatalf("no channel: %d", res.StatusCode)
	}
	if res := c.do("PUT", "/api/reports", map[string]any{"weekly": true, "notifier_ids": []int64{mailCh.ID}, "timezone": "Mars/Base"}); res.StatusCode != 400 {
		t.Fatalf("bad time zone: %d", res.StatusCode)
	}
	res := c.do("PUT", "/api/reports", map[string]any{"weekly": true, "hour": 0, "timezone": "UTC", "notifier_ids": []int64{mailCh.ID}})
	if res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("save: %d %s", res.StatusCode, b)
	}
	var view reportsView
	json.NewDecoder(res.Body).Decode(&view)
	// Turned on after this week's send time: the first report is next Monday.
	if next := time.UnixMilli(view.Next[ReportWeekly]); !next.After(now) || next.Weekday() != time.Monday {
		t.Fatalf("next: %v", next)
	}
	h.scheduledReports(t.Context(), now)
	if n := len(smtpSrv.received()); n != 0 {
		t.Fatalf("report sent right after turning it on: %d mails", n)
	}

	// Preview.
	res = c.do("GET", "/api/reports/preview?kind=weekly", nil)
	var rep Report
	json.NewDecoder(res.Body).Decode(&rep)
	if res.StatusCode != 200 || rep.Monitors != 3 || !strings.Contains(rep.Text, "production") {
		t.Fatalf("preview: %d %+v", res.StatusCode, rep)
	}
	if res := c.do("GET", "/api/reports/preview?kind=daily", nil); res.StatusCode != 400 {
		t.Fatalf("bad kind: %d", res.StatusCode)
	}

	// Send now: a multipart email with text and HTML.
	if res := c.do("POST", "/api/reports/send", map[string]string{"kind": "weekly"}); res.StatusCode != 200 {
		b, _ := io.ReadAll(res.Body)
		t.Fatalf("send: %d %s", res.StatusCode, b)
	}
	mails := smtpSrv.received()
	if len(mails) != 1 || mails[0].to[0] != "ops@example.com" {
		t.Fatalf("mails: %+v", mails)
	}
	msg, err := mail.ReadMessage(strings.NewReader(mails[0].data))
	if err != nil {
		t.Fatal(err)
	}
	dec := new(mime.WordDecoder)
	subj, _ := dec.DecodeHeader(msg.Header.Get("Subject"))
	if !strings.HasPrefix(subj, "[pingtxt] Weekly report") {
		t.Fatalf("subject: %q", subj)
	}
	mt, params, _ := mime.ParseMediaType(msg.Header.Get("Content-Type"))
	if mt != "multipart/alternative" {
		t.Fatalf("content type %q", mt)
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var types []string
	for {
		p, err := mr.NextPart()
		if err != nil {
			break
		}
		b, _ := io.ReadAll(p) // quoted-printable is decoded by the reader
		types = append(types, strings.SplitN(p.Header.Get("Content-Type"), ";", 2)[0])
		if !strings.Contains(string(b), "production") || !strings.Contains(string(b), "timeout") {
			t.Fatalf("part %s: %s", p.Header.Get("Content-Type"), b)
		}
	}
	if strings.Join(types, ",") != "text/plain,text/html" {
		t.Fatalf("parts: %v", types)
	}

	// Next Monday the scheduler sends the week that just ended, once.
	nextMon := time.UnixMilli(view.Next[ReportWeekly]).Add(time.Minute)
	h.scheduledReports(t.Context(), nextMon)
	h.scheduledReports(t.Context(), nextMon.Add(time.Hour))
	if n := len(smtpSrv.received()); n != 2 {
		t.Fatalf("scheduled: %d mails, want 2", n)
	}
	st2 := h.reportState()
	if wantFrom, _ := reportPeriod(ReportWeekly, nextMon, time.UTC); st2[ReportWeekly].From != wantFrom.UnixMilli() {
		t.Fatalf("state: %+v", st2)
	}

	// A deleted channel disappears from the settings.
	must(t, st.DeleteNotifier(mailCh.ID))
	res = c.do("GET", "/api/reports", nil)
	view = reportsView{}
	json.NewDecoder(res.Body).Decode(&view)
	if len(view.NotifierIDs) != 0 {
		t.Fatalf("deleted channel still listed: %v", view.NotifierIDs)
	}
}

// Report settings move with the configuration, with the ids of the new hub.
func TestReportSettingsExportImport(t *testing.T) {
	src, sst := newDataHub(t)
	seedConfig(t, src, sst)
	var mailID, groupID int64
	ns, _ := sst.ListNotifiers()
	for _, n := range ns {
		if n.Type == "email" {
			mailID = n.ID
		}
	}
	gs, _ := sst.ListGroups()
	groupID = gs[0].ID
	if mailID == 0 {
		t.Fatal("no email channel in the seed")
	}
	must(t, src.saveReportSettings(ReportSettings{Monthly: true, Hour: 7, Timezone: "Europe/Rome", NotifierIDs: []int64{mailID}, GroupIDs: []int64{groupID}}, time.Now()))
	full := exportJSON(t, src, true)

	dst, dst2 := newDataHub(t)
	// Shift the ids in the new hub.
	must(t, dst2.CreateNotifier(&store.Notifier{Name: "other", Type: "webhook", Enabled: true, Config: map[string]string{"url": "https://x"}}))
	must(t, dst2.CreateGroup(&store.Group{Name: "other"}))
	importJSON(t, dst, full, false)
	got := dst.reportSettings()
	n, err := dst2.Notifier(got.NotifierIDs[0])
	must(t, err)
	g, err := dst2.Group(got.GroupIDs[0])
	must(t, err)
	if !got.Monthly || got.Hour != 7 || got.Timezone != "Europe/Rome" || n.Type != "email" || g.Name != gs[0].Name {
		t.Fatalf("imported: %+v (%s, %s)", got, n.Name, g.Name)
	}
	// Without secrets the email channel is skipped: the reports are turned off.
	dst, _ = newDataHub(t)
	res := importJSON(t, dst, exportJSON(t, src, false), false)
	if got := dst.reportSettings(); got.Monthly || !strings.Contains(strings.Join(res.Warnings, "\n"), "periodic reports turned off") {
		t.Fatalf("without secrets: %+v %v", got, res.Warnings)
	}
}

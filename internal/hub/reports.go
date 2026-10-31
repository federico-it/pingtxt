package hub

import (
	"bytes"
	"cmp"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"html/template"
	"mime"
	"mime/quotedprintable"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Periodic reports are emails with the uptime and the incidents of the last
// week (Monday to Sunday) or month, per monitor group. They are sent through
// the email notification channels picked in the settings, at the chosen hour
// of the first day after the period, in the chosen time zone.
//
// reports_state keeps the start of the last period sent for each kind, so a
// hub that was down when a report was due sends it when it starts again, and
// never twice.

const (
	keyReports      = "reports"
	keyReportsState = "reports_state"
)

// Report kinds.
const (
	ReportWeekly  = "weekly"
	ReportMonthly = "monthly"
)

var reportKinds = []string{ReportWeekly, ReportMonthly}

// reportRetry is how long to wait after a report that no channel accepted.
const reportRetry = time.Hour

// maxReportIncidents caps the incidents listed per group in an email.
const maxReportIncidents = 50

// ReportSettings configure the periodic email reports.
type ReportSettings struct {
	Weekly  bool `json:"weekly"`
	Monthly bool `json:"monthly"`
	// Hour (0-23, in Timezone) at which a report is sent, on Monday for the
	// weekly report and on the 1st for the monthly one.
	Hour     int    `json:"hour"`
	Timezone string `json:"timezone"`
	// NotifierIDs are the email channels that receive the reports.
	NotifierIDs []int64 `json:"notifier_ids"`
	// GroupIDs limit the reports to these groups. Empty: every monitor.
	GroupIDs []int64 `json:"group_ids"`
}

func defaultReportSettings() ReportSettings {
	return ReportSettings{Hour: 8, Timezone: "UTC", NotifierIDs: []int64{}, GroupIDs: []int64{}}
}

func uniqueIDs(in []int64) []int64 {
	out := []int64{}
	for _, id := range in {
		if id > 0 && !slices.Contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

func (s *ReportSettings) normalize() {
	s.Hour = min(max(s.Hour, 0), 23)
	s.Timezone = strings.TrimSpace(s.Timezone)
	if s.Timezone == "" {
		s.Timezone = "UTC"
	}
	s.NotifierIDs = uniqueIDs(s.NotifierIDs)
	s.GroupIDs = uniqueIDs(s.GroupIDs)
}

func (s *ReportSettings) enabled(kind string) bool {
	switch kind {
	case ReportWeekly:
		return s.Weekly
	case ReportMonthly:
		return s.Monthly
	}
	return false
}

func (s *ReportSettings) location() *time.Location {
	if loc, err := time.LoadLocation(s.Timezone); err == nil {
		return loc
	}
	return time.UTC
}

// validate checks the time zone and that the channels and groups exist.
func (h *Hub) validateReportSettings(s *ReportSettings) error {
	if _, err := time.LoadLocation(s.Timezone); err != nil {
		return fmt.Errorf("unknown time zone %q", s.Timezone)
	}
	for _, id := range s.NotifierIDs {
		n, err := h.store.Notifier(id)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("notification channel %d not found", id)
		} else if err != nil {
			return err
		}
		if n.Type != "email" {
			return fmt.Errorf("notification channel %q is not an email channel", n.Name)
		}
	}
	for _, id := range s.GroupIDs {
		if _, err := h.store.Group(id); errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("group %d not found", id)
		} else if err != nil {
			return err
		}
	}
	if (s.Weekly || s.Monthly) && len(s.NotifierIDs) == 0 {
		return errors.New("pick at least one email channel for the reports")
	}
	return nil
}

func (h *Hub) reportSettings() ReportSettings {
	s := defaultReportSettings()
	if err := h.store.GetSetting(keyReports, &s); err != nil && !errors.Is(err, store.ErrNotFound) {
		h.log.Error("load report settings", "err", err)
	}
	s.normalize()
	return s
}

// reportSent records the last report sent of one kind.
type reportSent struct {
	From   int64 `json:"from"`    // start of the period (unix ms)
	SentAt int64 `json:"sent_at"` // unix ms
}

type reportState map[string]reportSent

func (h *Hub) reportState() reportState {
	st := reportState{}
	if err := h.store.GetSetting(keyReportsState, &st); err != nil && !errors.Is(err, store.ErrNotFound) {
		h.log.Error("load report state", "err", err)
	}
	if st == nil {
		st = reportState{}
	}
	return st
}

// reportRunner serializes scheduled reports and remembers failed attempts.
type reportRunner struct {
	mu      sync.Mutex
	retryAt map[string]time.Time
}

// reportPeriod returns the last complete week (Monday to Monday) or month
// before now, in loc.
func reportPeriod(kind string, now time.Time, loc *time.Location) (from, to time.Time) {
	t := now.In(loc)
	if kind == ReportMonthly {
		to = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
		return to.AddDate(0, -1, 0), to
	}
	back := (int(t.Weekday()) + 6) % 7 // days since Monday
	to = time.Date(t.Year(), t.Month(), t.Day()-back, 0, 0, 0, 0, loc)
	return to.AddDate(0, 0, -7), to
}

// previousPeriod returns the period before [from, ..).
func previousPeriod(kind string, from time.Time) time.Time {
	if kind == ReportMonthly {
		return from.AddDate(0, -1, 0)
	}
	return from.AddDate(0, 0, -7)
}

// reportSendAt is when the report of the period ending at `to` is sent.
func reportSendAt(to time.Time, hour int) time.Time {
	return time.Date(to.Year(), to.Month(), to.Day(), hour, 0, 0, 0, to.Location())
}

// nextReport returns when the next report of a kind goes out.
func nextReport(kind string, s ReportSettings, st reportState, now time.Time) time.Time {
	from, to := reportPeriod(kind, now, s.location())
	if st[kind].From < from.UnixMilli() {
		return reportSendAt(to, s.Hour)
	}
	var next time.Time
	if kind == ReportMonthly {
		next = to.AddDate(0, 1, 0)
	} else {
		next = to.AddDate(0, 0, 7)
	}
	return reportSendAt(next, s.Hour)
}

// reportLoop sends the reports when they are due.
func (h *Hub) reportLoop(ctx context.Context) {
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.scheduledReports(ctx, now)
		}
	}
}

func (h *Hub) scheduledReports(ctx context.Context, now time.Time) {
	s := h.reportSettings()
	if !s.Weekly && !s.Monthly {
		return
	}
	h.reports.mu.Lock()
	defer h.reports.mu.Unlock()
	if h.reports.retryAt == nil {
		h.reports.retryAt = map[string]time.Time{}
	}
	st := h.reportState()
	for _, kind := range reportKinds {
		if !s.enabled(kind) {
			continue
		}
		from, to := reportPeriod(kind, now, s.location())
		if st[kind].From >= from.UnixMilli() || now.Before(reportSendAt(to, s.Hour)) || now.Before(h.reports.retryAt[kind]) {
			continue
		}
		sent, err := h.sendReport(ctx, kind, from, to, s)
		if sent == 0 {
			h.log.Error("periodic report not sent", "kind", kind, "err", err)
			h.reports.retryAt[kind] = now.Add(reportRetry)
			continue
		}
		if err != nil {
			h.log.Warn("periodic report not sent to every channel", "kind", kind, "err", err)
		}
		h.log.Info("periodic report sent", "kind", kind, "from", from.Format(time.DateOnly), "channels", sent)
		delete(h.reports.retryAt, kind)
		st[kind] = reportSent{From: from.UnixMilli(), SentAt: now.UnixMilli()}
		if err := h.store.SetSetting(keyReportsState, st); err != nil {
			h.log.Error("save report state", "err", err)
		}
	}
}

// ---------- report content ----------

// Report is the content of one periodic report.
type Report struct {
	Kind     string `json:"kind"`
	From     int64  `json:"from"` // unix ms, included
	To       int64  `json:"to"`   // unix ms, excluded
	Timezone string `json:"timezone"`
	// Uptime is the average uptime of the monitors with data, -1 without any.
	Uptime     float64       `json:"uptime"`
	PrevUptime float64       `json:"prev_uptime"` // same for the period before, -1 without data
	Incidents  int           `json:"incidents"`
	DowntimeMs int64         `json:"downtime_ms"`
	Monitors   int           `json:"monitors"`
	Groups     []ReportGroup `json:"groups"`
	// The email: subject and plain text body.
	Subject string `json:"subject"`
	Text    string `json:"text"`
	// URL of the hub, linked from the email when the public URL is set.
	URL string `json:"url,omitempty"`
}

// ReportGroup sums up one monitor group (ID 0: monitors without a group).
type ReportGroup struct {
	ID           int64            `json:"id"`
	Name         string           `json:"name"`
	Uptime       float64          `json:"uptime"`
	PrevUptime   float64          `json:"prev_uptime"`
	Incidents    int              `json:"incidents"`
	DowntimeMs   int64            `json:"downtime_ms"`
	Monitors     []ReportMonitor  `json:"monitors"`
	IncidentList []ReportIncident `json:"incident_list"`
}

// ReportMonitor is one monitor line of a report.
type ReportMonitor struct {
	ID         int64   `json:"id"`
	Name       string  `json:"name"`
	Uptime     float64 `json:"uptime"`      // -1 without data
	PrevUptime float64 `json:"prev_uptime"` // -1 without data
	AvgMs      float64 `json:"avg_ms"`
	Incidents  int     `json:"incidents"`
	DowntimeMs int64   `json:"downtime_ms"`
}

// ReportIncident is an incident open at some point in the period.
type ReportIncident struct {
	ID         int64  `json:"id"`
	MonitorID  int64  `json:"monitor_id"`
	Monitor    string `json:"monitor"`
	StartedAt  int64  `json:"started_at"`
	ResolvedAt int64  `json:"resolved_at"` // 0: still open
	// DurationMs is the part of the incident inside the period.
	DurationMs int64  `json:"duration_ms"`
	Cause      string `json:"cause"`
}

// meanUptime averages the uptimes that have data, -1 if none.
func meanUptime(vals []float64) float64 {
	sum, n := 0.0, 0
	for _, v := range vals {
		if v >= 0 {
			sum += v
			n++
		}
	}
	if n == 0 {
		return -1
	}
	return sum / float64(n)
}

// buildReport computes the report of [from, to).
func (h *Hub) buildReport(kind string, from, to time.Time, s ReportSettings) (*Report, error) {
	f, t := from.UnixMilli(), to.UnixMilli()
	pf := previousPeriod(kind, from).UnixMilli()
	monitors, err := h.store.ListMonitors()
	if err != nil {
		return nil, err
	}
	groups, err := h.store.ListGroups()
	if err != nil {
		return nil, err
	}
	incidents, err := h.store.IncidentsBetween(f, t)
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, g := range groups {
		names[g.ID] = g.Name
	}
	byMonitor := map[int64][]*store.Incident{}
	for _, i := range incidents {
		byMonitor[i.MonitorID] = append(byMonitor[i.MonitorID], i)
	}

	r := &Report{Kind: kind, From: f, To: t, Timezone: s.Timezone, Groups: []ReportGroup{}, URL: h.generalSettings().PublicURL}
	byGroup := map[int64]*ReportGroup{}
	var all, allPrev []float64
	for _, m := range monitors {
		gid := m.GroupID
		if _, ok := names[gid]; !ok {
			gid = 0
		}
		if len(s.GroupIDs) > 0 && !slices.Contains(s.GroupIDs, gid) {
			continue
		}
		up, avg, err := h.store.UptimeBetween(m.ID, f, t)
		if err != nil {
			return nil, err
		}
		prev, _, err := h.store.UptimeBetween(m.ID, pf, f)
		if err != nil {
			return nil, err
		}
		inc := byMonitor[m.ID]
		if up < 0 && len(inc) == 0 {
			continue // paused or created after the period
		}
		g := byGroup[gid]
		if g == nil {
			g = &ReportGroup{ID: gid, Name: cmp.Or(names[gid], "No group"), Monitors: []ReportMonitor{}, IncidentList: []ReportIncident{}}
			byGroup[gid] = g
		}
		rm := ReportMonitor{ID: m.ID, Name: m.Name, Uptime: up, PrevUptime: prev, AvgMs: avg, Incidents: len(inc)}
		for _, i := range inc {
			end := t
			if i.ResolvedAt > 0 {
				end = min(i.ResolvedAt, t)
			}
			d := max(end-max(i.StartedAt, f), 0)
			rm.DowntimeMs += d
			g.IncidentList = append(g.IncidentList, ReportIncident{ID: i.ID, MonitorID: m.ID, Monitor: m.Name,
				StartedAt: i.StartedAt, ResolvedAt: i.ResolvedAt, DurationMs: d, Cause: i.Cause})
		}
		g.Monitors = append(g.Monitors, rm)
		g.Incidents += rm.Incidents
		g.DowntimeMs += rm.DowntimeMs
		all = append(all, up)
		allPrev = append(allPrev, prev)
	}
	for _, g := range byGroup {
		var ups, prevs []float64
		for _, m := range g.Monitors {
			ups = append(ups, m.Uptime)
			prevs = append(prevs, m.PrevUptime)
		}
		g.Uptime, g.PrevUptime = meanUptime(ups), meanUptime(prevs)
		// Worst monitors first, then by name.
		slices.SortStableFunc(g.Monitors, func(a, b ReportMonitor) int {
			return cmp.Or(cmp.Compare(a.Uptime, b.Uptime), cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)))
		})
		slices.SortStableFunc(g.IncidentList, func(a, b ReportIncident) int {
			return cmp.Or(cmp.Compare(a.StartedAt, b.StartedAt), cmp.Compare(a.ID, b.ID))
		})
		r.Groups = append(r.Groups, *g)
		r.Incidents += g.Incidents
		r.DowntimeMs += g.DowntimeMs
		r.Monitors += len(g.Monitors)
	}
	// Groups by name, monitors without a group last.
	slices.SortFunc(r.Groups, func(a, b ReportGroup) int {
		if (a.ID == 0) != (b.ID == 0) {
			if a.ID == 0 {
				return 1
			}
			return -1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	r.Uptime, r.PrevUptime = meanUptime(all), meanUptime(allPrev)
	r.Subject = reportSubject(r, from, to)
	r.Text = reportText(r, from, to)
	return r, nil
}

// ---------- formatting ----------

func pct(v float64) string {
	if v < 0 {
		return "n/a"
	}
	if v >= 1 {
		return "100%"
	}
	// Never round a bad uptime up to 100%.
	return fmt.Sprintf("%.2f%%", float64(int64(v*10000))/100)
}

// shortDuration formats a duration as "<1m", "42m", "3h 5m" or "2d 4h".
func shortDuration(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	switch {
	case d < time.Minute:
		if d == 0 {
			return "0m"
		}
		return "<1m"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d/time.Minute))
	case d < 24*time.Hour:
		if m := int(d%time.Hour) / int(time.Minute); m > 0 {
			return fmt.Sprintf("%dh %dm", int(d/time.Hour), m)
		}
		return fmt.Sprintf("%dh", int(d/time.Hour))
	}
	if hrs := int(d%(24*time.Hour)) / int(time.Hour); hrs > 0 {
		return fmt.Sprintf("%dd %dh", int(d/(24*time.Hour)), hrs)
	}
	return fmt.Sprintf("%dd", int(d/(24*time.Hour)))
}

func periodLabel(kind string, from, to time.Time) string {
	if kind == ReportMonthly {
		return from.Format("January 2006")
	}
	last := to.AddDate(0, 0, -1)
	if from.Year() != last.Year() {
		return from.Format("2 Jan 2006") + " - " + last.Format("2 Jan 2006")
	}
	return from.Format("2 Jan") + " - " + last.Format("2 Jan 2006")
}

func reportSubject(r *Report, from, to time.Time) string {
	what := "Weekly"
	if r.Kind == ReportMonthly {
		what = "Monthly"
	}
	s := fmt.Sprintf("[pingtxt] %s report %s: %s uptime", what, periodLabel(r.Kind, from, to), pct(r.Uptime))
	if r.Incidents > 0 {
		s += ", " + plural(r.Incidents, "incident")
	}
	return s
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n-1]) + "…"
	}
	return s
}

func padRight(s string, n int) string {
	if l := len([]rune(s)); l < n {
		return s + strings.Repeat(" ", n-l)
	}
	return s
}

func padLeft(s string, n int) string {
	if l := len([]rune(s)); l < n {
		return strings.Repeat(" ", n-l) + s
	}
	return s
}

func prevWord(kind string) string {
	if kind == ReportMonthly {
		return "previous month"
	}
	return "previous week"
}

func incidentWhen(i ReportIncident, loc *time.Location) string {
	return time.UnixMilli(i.StartedAt).In(loc).Format("Mon 02 Jan 15:04")
}

func incidentLength(i ReportIncident) string {
	if i.ResolvedAt == 0 {
		return "ongoing"
	}
	return shortDuration(i.DurationMs)
}

// reportText is the plain text body, laid out for a monospace font.
func reportText(r *Report, from, to time.Time) string {
	loc := from.Location()
	var b strings.Builder
	what := "weekly"
	if r.Kind == ReportMonthly {
		what = "monthly"
	}
	fmt.Fprintf(&b, "pingtxt %s report: %s (%s)\n\n", what, periodLabel(r.Kind, from, to), r.Timezone)
	if r.Monitors == 0 {
		b.WriteString("No monitor has data for this period.\n")
	} else {
		fmt.Fprintf(&b, "uptime     %s (%s: %s)\n", pct(r.Uptime), prevWord(r.Kind), pct(r.PrevUptime))
		fmt.Fprintf(&b, "incidents  %d, %s down\n", r.Incidents, shortDuration(r.DowntimeMs))
		fmt.Fprintf(&b, "monitors   %d\n", r.Monitors)
	}
	for _, g := range r.Groups {
		fmt.Fprintf(&b, "\n== %s ==\n", g.Name)
		fmt.Fprintf(&b, "uptime %s (prev %s), %s, %s down\n\n", pct(g.Uptime), pct(g.PrevUptime), plural(g.Incidents, "incident"), shortDuration(g.DowntimeMs))
		fmt.Fprintf(&b, "  %s %8s %8s %7s %5s %8s\n", padRight("monitor", 28), "uptime", "prev", "avg ms", "inc", "down")
		for _, m := range g.Monitors {
			avg := "-"
			if m.AvgMs > 0 {
				avg = fmt.Sprintf("%.0f", m.AvgMs)
			}
			fmt.Fprintf(&b, "  %s %8s %8s %7s %5d %8s\n", padRight(clip(m.Name, 28), 28), pct(m.Uptime), pct(m.PrevUptime), avg, m.Incidents, shortDuration(m.DowntimeMs))
		}
		if len(g.IncidentList) > 0 {
			b.WriteString("\n  incidents\n")
			for n, i := range g.IncidentList {
				if n == maxReportIncidents {
					fmt.Fprintf(&b, "  ... and %d more\n", len(g.IncidentList)-n)
					break
				}
				fmt.Fprintf(&b, "  %s  %s %s", incidentWhen(i, loc), padRight(clip(i.Monitor, 28), 28), padLeft(incidentLength(i), 8))
				if i.Cause != "" {
					fmt.Fprintf(&b, "  %s", clip(strings.ReplaceAll(i.Cause, "\n", " "), 80))
				}
				b.WriteString("\n")
			}
		}
	}
	if r.URL != "" {
		fmt.Fprintf(&b, "\ndashboard: %s\n", r.URL)
	}
	return b.String()
}

var reportHTML = template.Must(template.New("report").Funcs(template.FuncMap{
	"pct":      pct,
	"duration": shortDuration,
	"when":     incidentWhen,
	"length":   incidentLength,
	"clip":     clip,
	"avg": func(v float64) string {
		if v <= 0 {
			return "-"
		}
		return fmt.Sprintf("%.0f", v)
	},
	"bad": func(v float64) bool { return v >= 0 && v < 0.999 },
	"more": func(l []ReportIncident) int {
		return len(l) - maxReportIncidents
	},
	"cap": func(l []ReportIncident) []ReportIncident { return l[:min(len(l), maxReportIncidents)] },
}).Parse(`<!doctype html>
<html><body style="margin:0;padding:24px;background:#ffffff;color:#111111;font-family:ui-monospace,SFMono-Regular,Menlo,Consolas,monospace;font-size:13px;line-height:1.5">
<div style="max-width:760px">
<div style="color:#666666">$ pingtxt report --{{.R.Kind}}</div>
<h1 style="font-size:18px;margin:4px 0 16px">{{.Title}}</h1>
{{if eq .R.Monitors 0}}<p>No monitor has data for this period.</p>{{else}}
<table cellpadding="0" cellspacing="0" style="border-collapse:collapse;margin-bottom:8px">
<tr><td style="padding:2px 16px 2px 0;color:#666666">uptime</td><td style="padding:2px 0"><b>{{pct .R.Uptime}}</b> <span style="color:#666666">({{.Prev}}: {{pct .R.PrevUptime}})</span></td></tr>
<tr><td style="padding:2px 16px 2px 0;color:#666666">incidents</td><td style="padding:2px 0">{{.R.Incidents}}, {{duration .R.DowntimeMs}} down</td></tr>
<tr><td style="padding:2px 16px 2px 0;color:#666666">monitors</td><td style="padding:2px 0">{{.R.Monitors}}</td></tr>
</table>{{end}}
{{range .R.Groups}}
<h2 style="font-size:14px;margin:24px 0 4px;border-bottom:1px dashed #cccccc;padding-bottom:4px">{{.Name}}</h2>
<div style="color:#666666;margin-bottom:8px">uptime <b style="color:{{if bad .Uptime}}#c2410c{{else}}#15803d{{end}}">{{pct .Uptime}}</b> (prev {{pct .PrevUptime}}) &middot; {{.Incidents}} incidents &middot; {{duration .DowntimeMs}} down</div>
<table cellpadding="0" cellspacing="0" style="border-collapse:collapse;width:100%">
<tr style="color:#666666;text-align:right"><td style="text-align:left;padding:3px 8px 3px 0">monitor</td><td style="padding:3px 8px">uptime</td><td style="padding:3px 8px">prev</td><td style="padding:3px 8px">avg ms</td><td style="padding:3px 8px">incidents</td><td style="padding:3px 0 3px 8px">down</td></tr>
{{range .Monitors}}<tr style="text-align:right;border-top:1px dashed #e5e5e5"><td style="text-align:left;padding:3px 8px 3px 0">{{clip .Name 48}}</td><td style="padding:3px 8px;color:{{if bad .Uptime}}#c2410c{{else}}#15803d{{end}}">{{pct .Uptime}}</td><td style="padding:3px 8px;color:#666666">{{pct .PrevUptime}}</td><td style="padding:3px 8px">{{avg .AvgMs}}</td><td style="padding:3px 8px">{{.Incidents}}</td><td style="padding:3px 0 3px 8px">{{duration .DowntimeMs}}</td></tr>
{{end}}</table>
{{if .IncidentList}}<div style="color:#666666;margin:12px 0 4px">incidents</div>
<table cellpadding="0" cellspacing="0" style="border-collapse:collapse;width:100%">
{{range cap .IncidentList}}<tr style="border-top:1px dashed #e5e5e5"><td style="padding:3px 8px 3px 0;white-space:nowrap;color:#666666">{{when . $.Loc}}</td><td style="padding:3px 8px">{{clip .Monitor 48}}</td><td style="padding:3px 8px;white-space:nowrap;text-align:right">{{length .}}</td><td style="padding:3px 0 3px 8px;color:#666666">{{clip .Cause 120}}</td></tr>
{{end}}</table>{{if gt (more .IncidentList) 0}}<div style="color:#666666">... and {{more .IncidentList}} more</div>{{end}}{{end}}
{{end}}
<p style="margin-top:32px;color:#666666;font-size:12px">{{if .R.URL}}<a href="{{.R.URL}}" style="color:#666666">{{.R.URL}}</a> &middot; {{end}}sent by pingtxt &middot; times in {{.R.Timezone}}</p>
</div></body></html>`))

func reportHTMLBody(r *Report, from, to time.Time) (string, error) {
	what := "Weekly"
	if r.Kind == ReportMonthly {
		what = "Monthly"
	}
	var b bytes.Buffer
	err := reportHTML.Execute(&b, map[string]any{
		"R":     r,
		"Title": what + " report: " + periodLabel(r.Kind, from, to),
		"Prev":  prevWord(r.Kind),
		"Loc":   from.Location(),
	})
	return b.String(), err
}

// buildReportEmail renders a multipart/alternative message: plain text and HTML.
func buildReportEmail(ec *emailConfig, subject, text, html string, now time.Time) []byte {
	to := make([]string, len(ec.To))
	for i, a := range ec.To {
		to[i] = a.String()
	}
	domain := ec.From.Address[strings.LastIndexByte(ec.From.Address, '@')+1:]
	rb := make([]byte, 12)
	_, _ = rand.Read(rb)
	boundary := "pingtxt-" + hex.EncodeToString(rb)
	var b bytes.Buffer
	hdr := func(k, v string) { b.WriteString(k + ": " + v + "\r\n") }
	hdr("From", ec.From.String())
	hdr("To", strings.Join(to, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", subject))
	hdr("Date", now.Format(time.RFC1123Z))
	hdr("Message-ID", newMessageID(domain))
	hdr("MIME-Version", "1.0")
	hdr("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	hdr("Auto-Submitted", "auto-generated")
	b.WriteString("\r\n")
	part := func(ctype, body string) {
		b.WriteString("--" + boundary + "\r\n")
		b.WriteString("Content-Type: " + ctype + "; charset=utf-8\r\n")
		b.WriteString("Content-Transfer-Encoding: quoted-printable\r\n\r\n")
		qp := quotedprintable.NewWriter(&b)
		_, _ = qp.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(body, "\r\n", "\n"), "\n", "\r\n")))
		_ = qp.Close()
		b.WriteString("\r\n")
	}
	part("text/plain", text+"\n-- \nsent by pingtxt\n")
	part("text/html", html)
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes()
}

// sendReport builds the report of [from, to) and emails it to the channels
// of the settings (disabled channels are skipped). It returns the number of
// channels that accepted it.
func (h *Hub) sendReport(ctx context.Context, kind string, from, to time.Time, s ReportSettings) (int, error) {
	r, err := h.buildReport(kind, from, to, s)
	if err != nil {
		return 0, err
	}
	html, err := reportHTMLBody(r, from, to)
	if err != nil {
		return 0, err
	}
	sent := 0
	var errs []error
	for _, id := range s.NotifierIDs {
		nt, err := h.store.Notifier(id)
		if errors.Is(err, store.ErrNotFound) {
			continue
		} else if err != nil {
			errs = append(errs, err)
			continue
		}
		if nt.Type != "email" || !nt.Enabled {
			continue
		}
		cfg := make(map[string]string, len(nt.Config))
		for k, v := range nt.Config {
			cfg[k] = v
		}
		ec, err := parseEmailConfig(cfg)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", nt.Name, err))
			continue
		}
		if err := h.notify.deliverEmail(ctx, ec, buildReportEmail(ec, r.Subject, r.Text, html, time.Now())); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", nt.Name, err))
			continue
		}
		sent++
	}
	if sent == 0 && len(errs) == 0 {
		return 0, errors.New("no enabled email channel is selected for the reports")
	}
	return sent, errors.Join(errs...)
}

// ---------- API ----------

type reportsView struct {
	ReportSettings
	// Next is when the next report of each enabled kind goes out (unix ms).
	Next map[string]int64 `json:"next"`
	// Last is the last report sent of each kind.
	Last map[string]reportSent `json:"last"`
}

func (h *Hub) reportsResponse(now time.Time) reportsView {
	s := h.reportSettings()
	st := h.reportState()
	// Leave out channels and groups deleted since the settings were saved.
	if list, err := h.store.ListNotifiers(); err == nil {
		s.NotifierIDs = slices.DeleteFunc(s.NotifierIDs, func(id int64) bool {
			return !slices.ContainsFunc(list, func(n *store.Notifier) bool { return n.ID == id })
		})
	}
	if list, err := h.store.ListGroups(); err == nil {
		s.GroupIDs = slices.DeleteFunc(s.GroupIDs, func(id int64) bool {
			return !slices.ContainsFunc(list, func(g *store.Group) bool { return g.ID == id })
		})
	}
	v := reportsView{ReportSettings: s, Next: map[string]int64{}, Last: map[string]reportSent{}}
	for _, kind := range reportKinds {
		if l := st[kind]; l.SentAt > 0 {
			v.Last[kind] = l
		}
		if s.enabled(kind) {
			v.Next[kind] = nextReport(kind, s, st, now).UnixMilli()
		}
	}
	return v
}

func (h *Hub) getReports(w http.ResponseWriter, r *http.Request, _ *store.User) {
	writeJSON(w, 200, h.reportsResponse(time.Now()))
}

func (h *Hub) putReports(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var s ReportSettings
	if readJSON(r, &s) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	s.normalize()
	if err := h.validateReportSettings(&s); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.saveReportSettings(s, time.Now()); err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, h.reportsResponse(time.Now()))
}

// saveReportSettings stores the settings. A kind that is turned on starts
// with the next period: the report of a period that ended before is not
// sent late.
func (h *Hub) saveReportSettings(s ReportSettings, now time.Time) error {
	h.reports.mu.Lock()
	defer h.reports.mu.Unlock()
	cur := h.reportSettings()
	st := h.reportState()
	changed := false
	for _, kind := range reportKinds {
		if !s.enabled(kind) || cur.enabled(kind) {
			continue
		}
		from, to := reportPeriod(kind, now, s.location())
		if !now.Before(reportSendAt(to, s.Hour)) && st[kind].From < from.UnixMilli() {
			st[kind] = reportSent{From: from.UnixMilli(), SentAt: st[kind].SentAt}
			changed = true
		}
	}
	if err := h.store.SetSetting(keyReports, s); err != nil {
		return err
	}
	if changed {
		return h.store.SetSetting(keyReportsState, st)
	}
	return nil
}

func reportKindParam(v string) (string, bool) {
	if v == "" {
		return ReportWeekly, true
	}
	return v, slices.Contains(reportKinds, v)
}

// previewReport returns the report of the last complete period, as it would
// be sent now.
func (h *Hub) previewReport(w http.ResponseWriter, r *http.Request, _ *store.User) {
	kind, ok := reportKindParam(r.URL.Query().Get("kind"))
	if !ok {
		writeErr(w, 400, "kind must be weekly or monthly")
		return
	}
	s := h.reportSettings()
	from, to := reportPeriod(kind, time.Now(), s.location())
	rep, err := h.buildReport(kind, from, to, s)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, rep)
}

// sendReportNow emails the report of the last complete period to the saved
// channels. It does not change the schedule.
func (h *Hub) sendReportNow(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in struct {
		Kind string `json:"kind"`
	}
	if r.ContentLength != 0 && readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	kind, ok := reportKindParam(in.Kind)
	if !ok {
		writeErr(w, 400, "kind must be weekly or monthly")
		return
	}
	s := h.reportSettings()
	if len(s.NotifierIDs) == 0 {
		writeErr(w, 400, "pick at least one email channel for the reports")
		return
	}
	from, to := reportPeriod(kind, time.Now(), s.location())
	sent, err := h.sendReport(r.Context(), kind, from, to, s)
	if sent == 0 {
		writeErr(w, 502, err.Error())
		return
	}
	res := map[string]any{"ok": true, "sent": sent}
	if err != nil {
		res["error"] = err.Error()
	}
	writeJSON(w, 200, res)
}

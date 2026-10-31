package hub

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"html/template"
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
	if u := publicBase(r.URL); u != "" {
		fmt.Fprintf(&b, "\ndashboard: %s/\n", u)
	}
	return b.String()
}

// uptimeTone colors an uptime: green from 99.9%, yellow from 99%, else red.
func uptimeTone(v float64) string {
	switch {
	case v < 0:
		return ""
	case v >= 0.999:
		return toneUp
	case v >= 0.99:
		return toneWarn
	}
	return toneDown
}

// reportMailHTML adds the group tables to the email layout.
var reportMailHTML = template.Must(template.Must(mailLayout.Clone()).Funcs(template.FuncMap{
	"pct":      pct,
	"duration": shortDuration,
	"when":     incidentWhen,
	"length":   incidentLength,
	"clip":     clip,
	"utone":    uptimeTone,
	"plural":   plural,
	"avg": func(v float64) string {
		switch {
		case v <= 0:
			return "-"
		case v < 10:
			return fmt.Sprintf("%.1f", v)
		}
		return fmt.Sprintf("%.0f", v)
	},
	"more": func(l []ReportIncident) int {
		return len(l) - maxReportIncidents
	},
	"cap": func(l []ReportIncident) []ReportIncident { return l[:min(len(l), maxReportIncidents)] },
}).Parse(`{{define "extra"}}{{$loc := .Loc}}{{range .R.Groups}}
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:32px 0 0">
<tr><td class="pt-fg" style="font-weight:700;font-size:14px;color:#171613;word-break:break-word">## {{.Name}}</td>
<td align="right" class="pt-{{utone .Uptime}}" style="font-weight:700;font-size:14px;white-space:nowrap;color:{{tone (utone .Uptime)}}">{{pct .Uptime}}</td></tr>
<tr><td colspan="2" class="pt-mu" style="color:#6a6966;font-size:12px;padding:2px 0 8px">prev {{pct .PrevUptime}} &middot; {{plural .Incidents "incident"}} &middot; {{duration .DowntimeMs}} down</td></tr>
</table>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
<tr class="pt-mu" style="color:#6a6966;font-size:11px;letter-spacing:0.04em">
<td class="pt-bd" width="100%" style="padding:6px 8px 6px 0;border-top:1px dashed #d8d7d4">NAME</td>
<td class="pt-bd" align="right" style="padding:6px 8px;border-top:1px dashed #d8d7d4">UPTIME</td>
<td class="pt-bd pt-hide" align="right" style="padding:6px 8px;border-top:1px dashed #d8d7d4">PREV</td>
<td class="pt-bd pt-hide" align="right" style="padding:6px 8px;border-top:1px dashed #d8d7d4">AVG</td>
<td class="pt-bd" align="right" style="padding:6px 8px;border-top:1px dashed #d8d7d4">INC</td>
<td class="pt-bd" align="right" style="padding:6px 0 6px 8px;border-top:1px dashed #d8d7d4">DOWN</td></tr>
{{range .Monitors}}<tr>
<td class="pt-bd" style="padding:7px 8px 7px 0;border-top:1px dashed #d8d7d4;word-break:break-word"><span class="pt-{{utone .Uptime}}" style="color:{{tone (utone .Uptime)}}">&#9679;</span> {{clip .Name 48}}</td>
<td class="pt-bd pt-{{utone .Uptime}}" align="right" style="padding:7px 8px;border-top:1px dashed #d8d7d4;white-space:nowrap;color:{{tone (utone .Uptime)}}">{{pct .Uptime}}</td>
<td class="pt-bd pt-mu pt-hide" align="right" style="padding:7px 8px;border-top:1px dashed #d8d7d4;white-space:nowrap;color:#6a6966">{{pct .PrevUptime}}</td>
<td class="pt-bd pt-hide" align="right" style="padding:7px 8px;border-top:1px dashed #d8d7d4;white-space:nowrap">{{avg .AvgMs}}{{if gt .AvgMs 0.0}}<span class="pt-mu" style="color:#6a6966">ms</span>{{end}}</td>
<td class="pt-bd" align="right" style="padding:7px 8px;border-top:1px dashed #d8d7d4">{{.Incidents}}</td>
<td class="pt-bd" align="right" style="padding:7px 0 7px 8px;border-top:1px dashed #d8d7d4;white-space:nowrap">{{duration .DowntimeMs}}</td></tr>
{{end}}<tr><td class="pt-bd" colspan="6" style="border-top:1px dashed #d8d7d4;font-size:0;line-height:0">&nbsp;</td></tr>
</table>
{{if .IncidentList}}<div class="pt-mu" style="color:#6a6966;font-size:11px;letter-spacing:0.04em;margin:16px 0 0;padding-bottom:6px">INCIDENTS</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0">
{{range cap .IncidentList}}<tr>
<td class="pt-bd pt-mu" valign="top" style="padding:7px 12px 7px 0;border-top:1px dashed #d8d7d4;white-space:nowrap;color:#6a6966">{{when . $loc}}</td>
<td class="pt-bd" valign="top" width="100%" style="padding:7px 8px;border-top:1px dashed #d8d7d4;word-break:break-word">{{clip .Monitor 48}}{{if .Cause}}<div class="pt-mu" style="color:#6a6966;font-size:12px">&gt; {{clip .Cause 120}}</div>{{end}}</td>
<td class="pt-bd{{if not .ResolvedAt}} pt-down{{end}}" valign="top" align="right" style="padding:7px 0 7px 8px;border-top:1px dashed #d8d7d4;white-space:nowrap{{if not .ResolvedAt}};color:#d72828{{end}}">{{length .}}</td></tr>
{{end}}<tr><td class="pt-bd" colspan="3" style="border-top:1px dashed #d8d7d4;font-size:0;line-height:0">&nbsp;</td></tr>
</table>{{if gt (more .IncidentList) 0}}<div class="pt-mu" style="color:#6a6966;padding-top:6px">... and {{more .IncidentList}} more</div>{{end}}{{end}}
{{end}}{{end}}`))

// reportMailView lays out a report email.
func reportMailView(r *Report, from, to time.Time) mailView {
	what := "Weekly"
	if r.Kind == ReportMonthly {
		what = "Monthly"
	}
	period := periodLabel(r.Kind, from, to)
	v := mailView{
		Preheader: fmt.Sprintf("%s uptime, %s, %s down", pct(r.Uptime), plural(r.Incidents, "incident"), shortDuration(r.DowntimeMs)),
		Command:   "pingtxt report --" + r.Kind,
		Title:     what + " report",
		Lead:      period,
		Tone:      uptimeTone(r.Uptime),
		Footer:    []string{"Times in " + r.Timezone + "."},
		Data:      map[string]any{"R": r, "Loc": from.Location()},
	}
	if r.Monitors == 0 {
		v.Lead = period + ": no monitor has data for this period."
		v.Preheader = "No monitor has data for this period."
	} else {
		v.Rows = []mailRow{
			{Key: "uptime", Value: pct(r.Uptime), Tone: uptimeTone(r.Uptime)},
			{Key: prevWord(r.Kind), Value: pct(r.PrevUptime)},
			{Key: "incidents", Value: fmt.Sprint(r.Incidents)},
			{Key: "downtime", Value: shortDuration(r.DowntimeMs)},
			{Key: "monitors", Value: fmt.Sprint(r.Monitors)},
		}
	}
	if base := publicBase(r.URL); base != "" {
		v.Action = &mailLink{Label: "open dashboard", Href: base + "/"}
	}
	return v
}

func reportHTMLBody(r *Report, from, to time.Time) (string, error) {
	var b bytes.Buffer
	err := reportMailHTML.Execute(&b, reportMailView(r, from, to))
	return b.String(), err
}

// buildReportEmail renders a multipart/alternative message: plain text and HTML.
func buildReportEmail(ec *emailConfig, subject, text, html string, now time.Time) []byte {
	to := make([]string, len(ec.To))
	for i, a := range ec.To {
		to[i] = a.String()
	}
	return writeMail(ec, mailHeaders{to: strings.Join(to, ", "), subject: subject}, text+"\n-- \nsent by pingtxt\n", html, now)
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

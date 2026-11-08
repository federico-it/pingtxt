package hub

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/federico-it/pingtxt/internal/store"
)

// Maintenance windows announced on status pages.
//
// A public window shows on every status page that lists one of the monitors
// it covers: in progress, or the next occurrence when it is scheduled. With
// notify_subscribers on, the page's email subscribers hear about it when it
// is announced (saved), remind_min minutes before each occurrence, when an
// occurrence starts and ends, and when it is cancelled. Every one of these
// goes through maintenanceEvent.

// Maintenance events, also the status of the window in the event payload.
const (
	MaintScheduled  = "scheduled"
	MaintReminder   = "reminder"
	MaintInProgress = "in_progress"
	MaintCompleted  = "completed"
	MaintCancelled  = "cancelled"
)

const (
	// maxAnnouncement caps the public message of a window.
	maxAnnouncement = 5000
	// maxRemindMin is the longest reminder lead time (7 days).
	maxRemindMin = 7 * 24 * 60
	// maintNoticeLate is how late a start or end email may still go out
	// (after a restart, say). Later ones are skipped.
	maintNoticeLate = 15 * time.Minute
	// maintNoticeEvery is how often the start / end / reminder emails are
	// looked for.
	maintNoticeEvery = 20 * time.Second
	// publicMaintLimit caps the windows shown on a public page.
	publicMaintLimit = 20
)

// publicMaintenance is a window as visitors of a status page see it.
type publicMaintenance struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Status is "scheduled" or "in_progress" on the page; events also use
	// "completed" and "cancelled".
	Status   string `json:"status"`
	StartsAt int64  `json:"starts_at"`
	EndsAt   int64  `json:"ends_at"`
	// Kind is once, daily, weekly or monthly: a recurring window shows its
	// current or next occurrence.
	Kind string `json:"kind"`
	// MonitorIDs are the monitors of the page that the window covers.
	MonitorIDs []int64 `json:"monitor_ids"`
}

// announced reports whether a window shows on status pages.
func announced(w *store.Maintenance) bool { return w.Enabled && w.Public }

// notifiable reports whether a window emails the subscribers of its pages.
func notifiable(w *store.Maintenance) bool { return announced(w) && w.NotifySubscribers }

// occurrence returns the occurrence of a window in progress at now, or else
// the next one.
func occurrence(w *store.Maintenance, now time.Time) (start, end time.Time, active, ok bool) {
	if s, e, ok := w.ActiveAt(now); ok {
		return s, e, true, true
	}
	s, e, ok := w.NextAfter(now)
	return s, e, false, ok
}

func validateAnnouncement(m *store.Maintenance) error {
	m.Announcement = strings.TrimSpace(strings.ReplaceAll(m.Announcement, "\r\n", "\n"))
	if utf8.RuneCountInString(m.Announcement) > maxAnnouncement {
		return fmt.Errorf("public message is longer than %d characters", maxAnnouncement)
	}
	if m.RemindMin < 0 || m.RemindMin > maxRemindMin {
		return errors.New("reminder must be between 0 minutes and 7 days before the start")
	}
	if !m.Public {
		m.NotifySubscribers = false
	}
	return nil
}

// expandedMaint returns a copy of a window whose groups include their
// subgroups, as the scheduler sees it.
func (h *Hub) expandedMaint(w *store.Maintenance) *store.Maintenance {
	c := *w
	if t, err := h.groupTree(); err == nil {
		c.GroupIDs = t.Expand(w.GroupIDs)
	}
	return &c
}

// maintSnapshot returns the loaded windows (groups expanded).
func (h *Hub) maintSnapshot() []*store.Maintenance {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.maints)
}

// pageMaintenances lists the public windows that cover a monitor of a page.
func (h *Hub) pageMaintenances(ms []pageMonitor, now time.Time) []publicMaintenance {
	out := []publicMaintenance{}
	if len(ms) == 0 {
		return out
	}
	for _, w := range h.maintSnapshot() {
		if !announced(w) {
			continue
		}
		ids := coveredOnPage(w, ms)
		if len(ids) == 0 {
			continue
		}
		s, e, active, ok := occurrence(w, now)
		if !ok {
			continue
		}
		status := MaintScheduled
		if active {
			status = MaintInProgress
		}
		out = append(out, publicMaint(w, status, s, e, ids))
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartsAt < out[j].StartsAt })
	if len(out) > publicMaintLimit {
		out = out[:publicMaintLimit]
	}
	return out
}

func publicMaint(w *store.Maintenance, status string, s, e time.Time, ids []int64) publicMaintenance {
	return publicMaintenance{
		ID: w.ID, Title: w.Name, Description: w.Announcement, Status: status,
		StartsAt: s.UnixMilli(), EndsAt: e.UnixMilli(), Kind: w.Kind, MonitorIDs: ids,
	}
}

func coveredOnPage(w *store.Maintenance, ms []pageMonitor) []int64 {
	ids := []int64{}
	for _, pm := range ms {
		if w.Covers(pm.m) {
			ids = append(ids, pm.m.ID)
		}
	}
	return ids
}

// maintPage is a status page that shows a window, with the monitors of the
// page that it covers.
type maintPage struct {
	page  *store.StatusPage
	ids   []int64
	names []string
}

// maintPages lists the status pages that show a window (w has its groups
// expanded).
func (h *Hub) maintPages(w *store.Maintenance) ([]maintPage, error) {
	pages, err := h.store.ListStatusPages()
	if err != nil {
		return nil, err
	}
	var out []maintPage
	for _, p := range pages {
		ms, err := h.pageMonitors(p)
		if err != nil {
			return nil, err
		}
		mp := maintPage{page: p, ids: []int64{}}
		for _, pm := range ms {
			if w.Covers(pm.m) {
				mp.ids = append(mp.ids, pm.m.ID)
				mp.names = append(mp.names, pm.m.Name)
			}
		}
		if len(mp.ids) > 0 {
			out = append(out, mp)
		}
	}
	return out, nil
}

// backgroundLinks are the links of a page's emails sent without a request:
// its custom domain, else the public URL setting.
func (h *Hub) backgroundLinks(p *store.StatusPage) (pageLinks, bool) {
	if p.Domain != "" {
		base := "https://" + p.Domain
		return pageLinks{page: base + "/", api: base}, true
	}
	u := strings.TrimRight(h.generalSettings().PublicURL, "/")
	if u == "" {
		return pageLinks{}, false
	}
	if !strings.Contains(u, "://") {
		u = "https://" + u
	}
	return pageLinks{page: u + "/status/" + p.Slug + "/", api: u}, true
}

// notifyMaint sends one event of a window (groups expanded) to every page
// that shows it. r is the API request that caused it, nil from the
// scheduler.
func (h *Hub) notifyMaint(r *http.Request, w *store.Maintenance, event string, s, e time.Time) {
	pages, err := h.maintPages(w)
	if err != nil {
		h.log.Error("maintenance pages", "maintenance", w.Name, "err", err)
		return
	}
	for _, mp := range pages {
		var links pageLinks
		if r != nil {
			links = h.linksFor(r, mp.page)
		} else if l, ok := h.backgroundLinks(mp.page); ok {
			links = l
		} else {
			h.log.Warn("maintenance email skipped: set the public URL in settings", "page", mp.page.Slug)
			continue
		}
		status := event
		if event == MaintReminder {
			status = MaintScheduled
		}
		h.maintenanceEvent(mp.page, links, event, publicMaint(w, status, s, e, mp.ids), mp.names, w)
	}
	h.dropPageCache()
}

// maintenanceEvent tells the people following a status page about a
// maintenance window: today the page's email subscribers.
func (h *Hub) maintenanceEvent(p *store.StatusPage, l pageLinks, event string, m publicMaintenance, names []string, w *store.Maintenance) int {
	ec := h.subscribeChannel(p)
	if ec == nil || !p.Published {
		return 0
	}
	subs, err := h.store.PageSubscribers(p.ID, true)
	if err != nil {
		h.log.Error("list subscribers", "page", p.Slug, "err", err)
		return 0
	}
	if len(subs) == 0 {
		return 0
	}
	now := time.Now()
	msgs := make([]outMail, len(subs))
	for i, s := range subs {
		msgs[i] = outMail{to: []string{s.Email}, msg: buildMaintEmail(p, event, m, names, w, s, l, now)}
	}
	h.sendBackground(ec, msgs, "maintenance "+event, p.Slug)
	return len(msgs)
}

// ---------- scheduler ----------

// maintNoticeTick sends the reminder, start and end emails of the windows
// that notify subscribers.
func (h *Hub) maintNoticeTick(now time.Time) {
	if now.Sub(h.maintNoticeAt) < maintNoticeEvery {
		return
	}
	h.maintNoticeAt = now
	for _, w := range h.maintSnapshot() {
		if notifiable(w) {
			h.maintNotices(w, now)
		}
	}
}

func (h *Hub) maintNotices(w *store.Maintenance, now time.Time) {
	mark := func(notice string, start int64) {
		if err := h.store.SetMaintenanceNotice(w.ID, notice, start); err != nil {
			h.log.Error("maintenance notice", "maintenance", w.Name, "err", err)
		}
		h.mu.Lock()
		switch notice {
		case store.NoticeRemind:
			w.NoticeRemind = start
		case store.NoticeStart:
			w.NoticeStart = start
		case store.NoticeEnd:
			w.NoticeEnd = start
		}
		h.mu.Unlock()
	}
	// The end of the occurrence whose start went out.
	if w.NoticeStart != 0 && w.NoticeEnd != w.NoticeStart {
		s := time.UnixMilli(w.NoticeStart)
		e := w.OccurrenceEnd(s)
		if as, _, ok := w.ActiveAt(now); !ok || !as.Equal(s) {
			if !now.After(e) { // shortened or disabled: it ended now
				e = now
			}
			mark(store.NoticeEnd, w.NoticeStart)
			if now.Sub(e) <= maintNoticeLate {
				h.notifyMaint(nil, w, MaintCompleted, s, e)
			}
		}
	}
	if s, e, ok := w.ActiveAt(now); ok {
		if w.NoticeStart != s.UnixMilli() {
			mark(store.NoticeStart, s.UnixMilli())
			if now.Sub(s) <= maintNoticeLate {
				h.notifyMaint(nil, w, MaintInProgress, s, e)
			}
		}
		return
	}
	if w.RemindMin > 0 {
		s, e, ok := w.NextAfter(now)
		if ok && s.Sub(now) <= time.Duration(w.RemindMin)*time.Minute && w.NoticeRemind != s.UnixMilli() {
			mark(store.NoticeRemind, s.UnixMilli())
			h.notifyMaint(nil, w, MaintReminder, s, e)
		}
	}
}

// ---------- API hooks ----------

func sameSchedule(a, b *store.Maintenance) bool {
	return a.Kind == b.Kind && a.StartAt == b.StartAt && a.EndAt == b.EndAt && a.StartTime == b.StartTime &&
		a.DurationMin == b.DurationMin && slices.Equal(a.Weekdays, b.Weekdays) && slices.Equal(a.MonthDays, b.MonthDays) &&
		a.Timezone == b.Timezone
}

// maintSaved emails what changed for subscribers after a window is created
// (old == nil) or updated.
func (h *Hub) maintSaved(r *http.Request, old, cur *store.Maintenance) {
	switch {
	case notifiable(cur) && (old == nil || !notifiable(old) || !sameSchedule(old, cur)):
		h.announceMaint(r, cur)
	case old != nil && notifiable(old) && !announced(cur):
		h.cancelMaint(r, old)
	}
}

// announceMaint emails a window that was just announced or rescheduled: the
// next occurrence, or the one in progress.
func (h *Hub) announceMaint(r *http.Request, w *store.Maintenance) {
	w = h.expandedMaint(w)
	now := time.Now()
	s, e, active, ok := occurrence(w, now)
	if !ok {
		return
	}
	if active {
		if w.NoticeStart == s.UnixMilli() {
			return // subscribers already know
		}
		if err := h.store.SetMaintenanceNotice(w.ID, store.NoticeStart, s.UnixMilli()); err != nil {
			h.log.Error("maintenance notice", "maintenance", w.Name, "err", err)
		}
		h.notifyMaint(r, w, MaintInProgress, s, e)
	} else {
		// No reminder right after the announcement.
		if w.RemindMin > 0 && s.Sub(now) <= time.Duration(w.RemindMin)*time.Minute {
			if err := h.store.SetMaintenanceNotice(w.ID, store.NoticeRemind, s.UnixMilli()); err != nil {
				h.log.Error("maintenance notice", "maintenance", w.Name, "err", err)
			}
		}
		h.notifyMaint(r, w, MaintScheduled, s, e)
	}
	_ = h.MaintenancesChanged() // pick up the notices
}

// cancelMaint emails that a window was deleted, disabled or hidden: ended
// early when it was in progress, else cancelled.
func (h *Hub) cancelMaint(r *http.Request, w *store.Maintenance) {
	w = h.expandedMaint(w)
	now := time.Now()
	s, _, active, ok := occurrence(w, now)
	if !ok {
		return
	}
	if active {
		if w.NoticeStart != s.UnixMilli() || w.NoticeEnd == w.NoticeStart {
			return
		}
		_ = h.store.SetMaintenanceNotice(w.ID, store.NoticeEnd, s.UnixMilli())
		h.notifyMaint(r, w, MaintCompleted, s, now)
		return
	}
	h.notifyMaint(r, w, MaintCancelled, s, w.OccurrenceEnd(s))
}

// ---------- email ----------

var maintBadges = map[string]string{
	MaintScheduled:  "SCHEDULED",
	MaintReminder:   "STARTS SOON",
	MaintInProgress: "IN PROGRESS",
	MaintCompleted:  "COMPLETED",
	MaintCancelled:  "CANCELLED",
}

var maintSubjects = map[string]string{
	MaintScheduled:  "Scheduled maintenance",
	MaintReminder:   "Maintenance starts soon",
	MaintInProgress: "Maintenance in progress",
	MaintCompleted:  "Maintenance completed",
	MaintCancelled:  "Maintenance cancelled",
}

// maintTime formats a time in the window's time zone, with UTC next to it.
func maintTime(t time.Time, loc *time.Location) string {
	if loc == time.UTC {
		return t.UTC().Format("Mon 2 Jan 2006 15:04 UTC")
	}
	return t.In(loc).Format("Mon 2 Jan 2006 15:04 MST") + " (" + t.UTC().Format("15:04 UTC") + ")"
}

// maintRepeat describes a recurring window, "" for a one-off.
func maintRepeat(w *store.Maintenance) string {
	var days string
	switch w.Kind {
	case store.MaintDaily:
		days = "every day"
	case store.MaintWeekly:
		names := []string{}
		for _, d := range []int{1, 2, 3, 4, 5, 6, 0} {
			if slices.Contains(w.Weekdays, d) {
				names = append(names, time.Weekday(d).String()[:3])
			}
		}
		days = "every " + strings.Join(names, ", ")
	case store.MaintMonthly:
		ds := make([]string, len(w.MonthDays))
		for i, d := range w.MonthDays {
			ds[i] = fmt.Sprint(d)
		}
		days = "monthly on day " + strings.Join(ds, ", ")
	default:
		return ""
	}
	return fmt.Sprintf("%s at %s %s, for %s", days, w.StartTime, w.Timezone, fmtDuration(time.Duration(w.DurationMin)*time.Minute))
}

func buildMaintEmail(p *store.StatusPage, event string, m publicMaintenance, names []string, w *store.Maintenance, s *store.PageSubscriber, l pageLinks, now time.Time) mailMsg {
	title := oneLine(p.Title)
	loc := w.Location()
	start, end := time.UnixMilli(m.StartsAt), time.UnixMilli(m.EndsAt)
	tone := toneInfo
	var lead string
	switch event {
	case MaintScheduled:
		lead = "Maintenance is planned on " + title + "."
	case MaintReminder:
		lead = "Maintenance on " + title + " starts in " + fmtDuration(start.Sub(now).Round(time.Minute)) + "."
		tone = toneWarn
	case MaintInProgress:
		lead = "Maintenance on " + title + " has started."
		tone = toneWarn
	case MaintCompleted:
		lead = "Maintenance on " + title + " is over."
		tone = toneUp
	case MaintCancelled:
		lead = "Maintenance planned on " + title + " is cancelled."
	}
	v := mailView{
		Preheader: clip(lead+" "+oneLine(m.Description), 140),
		Command:   "pingtxt maintenance " + title,
		Tone:      tone,
		Badge:     maintBadges[event],
		Title:     m.Title,
		Lead:      lead,
		Note:      m.Description,
		Rows:      []mailRow{{Key: "status page", Value: title, Href: l.page}},
		Action:    &mailLink{Label: "view status page", Href: l.page},
		Footer:    []string{"You get this email because you subscribed to " + title + "."},
		FootLinks: []mailLink{{Label: "unsubscribe", Href: l.unsubscribe(s.Token)}},
	}
	switch event {
	case MaintCompleted:
		v.Rows = append(v.Rows, mailRow{Key: "ended", Value: maintTime(end, loc), Tone: toneUp})
	case MaintCancelled:
		v.Rows = append(v.Rows, mailRow{Key: "was planned", Value: maintTime(start, loc)})
	default:
		v.Rows = append(v.Rows, mailRow{Key: "starts", Value: maintTime(start, loc)}, mailRow{Key: "ends", Value: maintTime(end, loc)})
	}
	if rep := maintRepeat(w); rep != "" && event != MaintCompleted {
		v.Rows = append(v.Rows, mailRow{Key: "repeats", Value: rep})
	}
	if len(names) > 0 {
		v.Rows = append(v.Rows, mailRow{Key: "affects", Value: clip(strings.Join(names, ", "), 300)})
	}
	subject := fmt.Sprintf("[%s] %s: %s", title, maintSubjects[event], oneLine(m.Title))
	return newMail(mailHeaders{to: s.Email, subject: subject, unsubscribe: l.oneClick(s.Token)}, v.renderText(""), v.renderHTML(nil), now)
}

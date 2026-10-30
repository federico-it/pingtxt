package hub

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// MaintenancesChanged reloads maintenance windows after an API change.
func (h *Hub) MaintenancesChanged() error {
	list, err := h.store.ListMaintenances()
	if err != nil {
		return err
	}
	h.mu.Lock()
	h.maints = list
	h.mu.Unlock()
	return nil
}

// activeMaintLocked returns the window covering a monitor at t, if any.
func (h *Hub) activeMaintLocked(monitorID int64, t time.Time) (*store.Maintenance, time.Time) {
	var best *store.Maintenance
	var bestEnd time.Time
	for _, w := range h.maints {
		if !w.Covers(monitorID) {
			continue
		}
		// With overlapping windows report the one that lasts longest.
		if _, end, ok := w.ActiveAt(t); ok && end.After(bestEnd) {
			best, bestEnd = w, end
		}
	}
	return best, bestEnd
}

// ActiveMaintenance returns the window covering a monitor now, if any.
func (h *Hub) ActiveMaintenance(monitorID int64) (*store.Maintenance, time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.activeMaintLocked(monitorID, time.Now())
}

// maintTick switches monitors in and out of maintenance at window bounds,
// without waiting for the next check result.
func (h *Hub) maintTick(now time.Time) {
	type change struct {
		m      *store.Monitor
		status string
		alert  string
		cause  string
	}
	var changes []change
	h.mu.Lock()
	for _, st := range h.monitors {
		if st.m.Paused {
			continue
		}
		w, _ := h.activeMaintLocked(st.m.ID, now)
		active := w != nil
		stale := !active && st.m.Status == StatusMaintenance // e.g. after a restart
		if active == st.inMaint && !stale {
			continue
		}
		st.inMaint = active
		status := StatusMaintenance
		if !active {
			status = st.real
			if status == "" {
				status = StatusPending
			}
		}
		c := change{m: st.m, status: status, alert: h.alertLocked(st)}
		if c.alert == StatusDown {
			c.cause = h.causesLocked(st)
		}
		if status != st.m.Status {
			st.m.Status = status
			st.m.StatusSince = now.UnixMilli()
		} else if c.alert == "" {
			continue
		}
		changes = append(changes, c)
	}
	h.mu.Unlock()

	at := now.UnixMilli()
	for _, c := range changes {
		_ = h.store.SetMonitorStatus(c.m.ID, c.status, at)
		h.log.Info("maintenance", "monitor", c.m.Name, "status", c.status)
		h.raise(c.m, c.alert, c.cause, at)
	}
}

func (h *Hub) causesLocked(st *monState) string {
	var causes []string
	for l, s := range st.locs {
		if s.down {
			causes = append(causes, h.locationNameLocked(l)+": "+s.lastMsg)
		}
	}
	slices.Sort(causes)
	return strings.Join(causes, "; ")
}

// ---------- API ----------

var maintKinds = []string{store.MaintOnce, store.MaintDaily, store.MaintWeekly, store.MaintMonthly}

func (h *Hub) validateMaintenance(m *store.Maintenance) error {
	m.Name = strings.TrimSpace(m.Name)
	m.Description = strings.TrimSpace(m.Description)
	if m.Name == "" {
		return errors.New("name is required")
	}
	if !slices.Contains(maintKinds, m.Kind) {
		return errors.New("invalid kind")
	}
	if m.MonitorIDs == nil {
		m.MonitorIDs = []int64{}
	}
	slices.Sort(m.MonitorIDs)
	m.MonitorIDs = slices.Compact(m.MonitorIDs)
	if m.AllMonitors {
		m.MonitorIDs = []int64{}
	} else {
		if len(m.MonitorIDs) == 0 {
			return errors.New("select at least one monitor")
		}
		for _, id := range m.MonitorIDs {
			if _, err := h.store.Monitor(id); err != nil {
				return errors.New("unknown monitor")
			}
		}
	}
	if m.Timezone == "" {
		m.Timezone = "UTC"
	}
	if _, err := time.LoadLocation(m.Timezone); err != nil {
		return errors.New("unknown time zone")
	}
	m.Weekdays, m.MonthDays = cleanDays(m.Weekdays, 0, 6), cleanDays(m.MonthDays, 1, 31)
	if m.Kind == store.MaintOnce {
		if m.StartAt <= 0 || m.EndAt <= m.StartAt {
			return errors.New("end must be after start")
		}
		m.StartTime, m.DurationMin = "", 0
		m.Weekdays, m.MonthDays = []int{}, []int{}
		return nil
	}
	if _, _, err := store.ParseClock(m.StartTime); err != nil {
		return err
	}
	if m.DurationMin <= 0 || m.DurationMin > store.MaxMaintDurationMin {
		return errors.New("duration must be between 1 minute and 7 days")
	}
	if m.StartAt < 0 || m.EndAt < 0 || (m.EndAt > 0 && m.EndAt <= m.StartAt) {
		return errors.New("invalid validity range")
	}
	switch m.Kind {
	case store.MaintDaily:
		m.Weekdays, m.MonthDays = []int{}, []int{}
	case store.MaintWeekly:
		if len(m.Weekdays) == 0 {
			return errors.New("select at least one weekday")
		}
		m.MonthDays = []int{}
	case store.MaintMonthly:
		if len(m.MonthDays) == 0 {
			return errors.New("select at least one day of the month")
		}
		m.Weekdays = []int{}
	}
	return nil
}

func cleanDays(in []int, lo, hi int) []int {
	out := []int{}
	for _, d := range in {
		if d >= lo && d <= hi {
			out = append(out, d)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

type maintView struct {
	*store.Maintenance
	Active    bool  `json:"active"`
	ActiveEnd int64 `json:"active_end,omitempty"`
	NextStart int64 `json:"next_start,omitempty"`
	NextEnd   int64 `json:"next_end,omitempty"`
}

func viewMaint(m *store.Maintenance, now time.Time) maintView {
	v := maintView{Maintenance: m}
	if _, end, ok := m.ActiveAt(now); ok {
		v.Active, v.ActiveEnd = true, end.UnixMilli()
	}
	if s, e, ok := m.NextAfter(now); ok {
		v.NextStart, v.NextEnd = s.UnixMilli(), e.UnixMilli()
	}
	return v
}

func (h *Hub) listMaintenances(w http.ResponseWriter, r *http.Request, _ *store.User) {
	list, err := h.store.ListMaintenances()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	now := time.Now()
	out := make([]maintView, 0, len(list))
	for _, m := range list {
		out = append(out, viewMaint(m, now))
	}
	writeJSON(w, 200, out)
}

func (h *Hub) createMaintenance(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var m store.Maintenance
	if readJSON(r, &m) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := h.validateMaintenance(&m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.CreateMaintenance(&m); err != nil {
		h.internalErr(w, err)
		return
	}
	h.afterMaintChange()
	writeJSON(w, 201, viewMaint(&m, time.Now()))
}

func (h *Hub) updateMaintenance(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	old, err := h.store.Maintenance(id)
	if err != nil {
		writeErr(w, 404, "maintenance not found")
		return
	}
	var m store.Maintenance
	if readJSON(r, &m) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	m.ID, m.CreatedAt = id, old.CreatedAt
	if err := h.validateMaintenance(&m); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	if err := h.store.UpdateMaintenance(&m); err != nil {
		h.internalErr(w, err)
		return
	}
	h.afterMaintChange()
	writeJSON(w, 200, viewMaint(&m, time.Now()))
}

func (h *Hub) deleteMaintenance(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if err := h.store.DeleteMaintenance(id); err != nil {
		h.internalErr(w, err)
		return
	}
	h.afterMaintChange()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// afterMaintChange reloads windows and applies them right away, so the
// response already reflects the new monitor statuses.
func (h *Hub) afterMaintChange() {
	if err := h.MaintenancesChanged(); err != nil {
		h.log.Error("reload maintenances", "err", err)
	}
	h.maintTick(time.Now())
}

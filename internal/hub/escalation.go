package hub

import (
	"context"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Alert escalation and reminders. Each notification channel can wait some
// minutes of downtime before its first alert (delay_min, so a second contact
// only hears about outages that last) and repeat the alert while the monitor
// stays down (repeat_min). What each channel received is stored per incident,
// so a restart neither repeats nor skips alerts. Nothing is sent while a
// maintenance window covers the monitor: due alerts go out when it ends.

const (
	escalationEvery = 15 * time.Second
	maxAlertMin     = 7 * 24 * 60 // longest delay or repeat interval: one week
	minRepeatMin    = 5           // shortest repeat interval
)

// alertState follows the alerts of a monitor's open incident.
type alertState struct {
	incident int64 // incident id (0 if it could not be stored)
	started  int64 // unix ms
	cause    string
	sent     map[int64]store.AlertSent // by notifier id
}

func newAlertState(a *store.IncidentAlerts) *alertState {
	return &alertState{incident: a.IncidentID, started: a.StartedAt, cause: a.Cause, sent: a.Sent}
}

// due reports whether the channel must be alerted at now (unix ms).
func (a *alertState) due(nt *store.Notifier, now int64) bool {
	s, ok := a.sent[nt.ID]
	if !ok {
		return now >= a.started+int64(nt.DelayMin)*60_000
	}
	return nt.RepeatMin > 0 && now >= s.At+int64(nt.RepeatMin)*60_000
}

// pendingAlert is an alert to deliver to one channel.
type pendingAlert struct {
	nt       *store.Notifier
	ev       Event
	incident int64
	sent     store.AlertSent
}

// dueAlertsLocked marks as sent and returns the alerts due for a monitor at
// now (unix ms) to the channels that want down alerts for it. Call with
// h.mu held.
func (h *Hub) dueAlertsLocked(st *monState, list []*store.Notifier, rt routing, now int64) []pendingAlert {
	a := st.alerts
	if a == nil || !st.incident || st.inMaint || st.m.Paused {
		return nil
	}
	var out []pendingAlert
	for _, nt := range list {
		if !rt.wants(nt, st.m, store.EventDown) || !a.due(nt, now) {
			continue
		}
		prev := a.sent[nt.ID]
		next := store.AlertSent{At: now, Count: prev.Count + 1}
		a.sent[nt.ID] = next
		out = append(out, pendingAlert{
			nt: nt, incident: a.incident, sent: next,
			ev: Event{
				MonitorID: st.m.ID, Monitor: st.m.Name, Target: eventTarget(st.m), Status: StatusDown,
				Cause: a.cause, At: now, DownSince: a.started, Reminder: prev.Count,
			},
		})
	}
	return out
}

// sendAlerts stores and delivers alerts, asynchronously.
func (h *Hub) sendAlerts(list []pendingAlert) {
	for _, p := range list {
		if p.incident > 0 {
			if err := h.store.RecordIncidentAlert(p.incident, p.nt.ID, p.sent); err != nil {
				h.log.Error("record alert", "err", err)
			}
		}
		h.notify.SendTo(p.nt, p.ev)
	}
}

// openAlerts starts following a new incident and sends the alerts due right
// away (channels without a delay).
func (h *Hub) openAlerts(m *store.Monitor, incident, at int64, cause string) {
	list, rt, err := h.loadRouting()
	if err != nil {
		h.log.Error("list notifiers", "err", err)
	}
	h.mu.Lock()
	var due []pendingAlert
	if st := h.monitors[m.ID]; st != nil {
		st.alerts = &alertState{incident: incident, started: at, cause: cause, sent: map[int64]store.AlertSent{}}
		due = h.dueAlertsLocked(st, list, rt, at)
	}
	h.mu.Unlock()
	h.sendAlerts(due)
}

// closeAlerts stops following the incident and tells the channels that were
// alerted about it, and want recoveries, that the monitor is back up.
func (h *Hub) closeAlerts(m *store.Monitor, at int64) {
	var a *alertState
	h.mu.Lock()
	if st := h.monitors[m.ID]; st != nil {
		a, st.alerts = st.alerts, nil
	}
	h.mu.Unlock()
	list, rt, err := h.loadRouting()
	if err != nil {
		h.log.Error("list notifiers", "err", err)
		return
	}
	ev := Event{MonitorID: m.ID, Monitor: m.Name, Target: eventTarget(m), Status: StatusUp, At: at}
	if a != nil {
		ev.DownSince = a.started
	}
	for _, nt := range list {
		if !rt.wants(nt, m, store.EventUp) {
			continue
		}
		if a != nil {
			if _, ok := a.sent[nt.ID]; !ok {
				continue // never told about this outage
			}
		}
		h.notify.SendTo(nt, ev)
	}
}

// loadAlerts restores the alerts of open incidents after a start. Incidents
// opened before alerts were recorded count as already sent to the channels
// whose delay has passed, so an upgrade does not resend them.
func (h *Hub) loadAlerts(now int64) map[int64]*alertState {
	open, err := h.store.OpenIncidentAlerts()
	if err != nil {
		h.log.Error("load open incidents", "err", err)
		return nil
	}
	var list []*store.Notifier
	var rt routing
	out := map[int64]*alertState{}
	for id, inc := range open {
		a := newAlertState(inc)
		if len(a.sent) == 0 {
			if list == nil {
				list, rt, _ = h.loadRouting()
			}
			m, _ := h.store.Monitor(id)
			for _, nt := range list {
				if rt.wants(nt, m, store.EventDown) && a.due(nt, now) {
					a.sent[nt.ID] = store.AlertSent{At: now, Count: 1}
				}
			}
		}
		out[id] = a
	}
	return out
}

// escalationLoop sends delayed alerts and reminders.
func (h *Hub) escalationLoop(ctx context.Context) {
	t := time.NewTicker(escalationEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.escalate(now)
		}
	}
}

func (h *Hub) escalate(now time.Time) {
	h.mu.Lock()
	open := false
	for _, st := range h.monitors {
		if st.alerts != nil && st.incident {
			open = true
			break
		}
	}
	h.mu.Unlock()
	if !open {
		return
	}
	list, rt, err := h.loadRouting()
	if err != nil {
		h.log.Error("list notifiers", "err", err)
		return
	}
	ms := now.UnixMilli()
	var due []pendingAlert
	h.mu.Lock()
	for _, st := range h.monitors {
		due = append(due, h.dueAlertsLocked(st, list, rt, ms)...)
	}
	h.mu.Unlock()
	h.sendAlerts(due)
}

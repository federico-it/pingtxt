package hub

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Agent offline alerts. When an agent stays disconnected longer than the
// grace period, channels with the agent_offline event get an "offline"
// event, and an "online" one when it connects again. Short drops (restarts,
// network blips) send nothing. The state of each disconnection is kept in
// the agents table, so a hub restart neither repeats nor loses an alert.
//
// Nothing is sent while the agent updates itself, when an admin kicked it
// (token rotated), when its alert is turned off, or while every monitor it
// checks is in maintenance (the alert goes out when the window ends).
//
// A monitor that runs only on agents offline past the grace period gets no
// results: it turns pending instead of keeping its last status.

// Agent event kind and statuses.
const (
	KindAgent    = "agent"
	AgentOffline = "offline"
	AgentOnline  = "online"
)

const (
	defaultOfflineGraceMin = 5
	maxOfflineGraceMin     = 1440
	// updateOfflineGrace is the grace period of an agent that was just sent
	// an update: it restarts and downloads, which can take a while.
	updateOfflineGrace = 10 * time.Minute
	agentOfflineEvery  = 15 * time.Second
)

// offlineGrace is how long an agent can be disconnected before it is
// reported offline.
func (s AgentUpdateSettings) offlineGrace() time.Duration {
	m := s.OfflineGraceMin
	if m <= 0 {
		m = defaultOfflineGraceMin
	}
	return time.Duration(m) * time.Minute
}

func (h *Hub) agentOfflineLoop(ctx context.Context) {
	t := time.NewTicker(agentOfflineEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			h.checkAgentsOffline(now)
		}
	}
}

// agentOfflineSince is when the disconnection of an offline agent counts
// from, or 0 when it does not count (never connected). Agents that were
// connected when the hub stopped count from the hub start, so they have the
// whole grace period to reconnect.
func agentOfflineSince(a *store.Agent, started int64) int64 {
	since := a.OfflineSince
	if since == 0 {
		if a.LastSeen == 0 {
			return 0
		}
		since = started
	}
	return max(since, started)
}

// checkAgentsOffline sends the alerts of agents offline past the grace
// period and turns the monitors that only they check pending.
func (h *Hub) checkAgentsOffline(now time.Time) {
	agents, err := h.store.ListAgents()
	if err != nil {
		h.log.Error("list agents", "err", err)
		return
	}
	grace := h.agentUpdateSettings().offlineGrace()
	at := now.UnixMilli()
	lost := map[string]bool{} // locations of agents offline past the grace period
	for _, a := range agents {
		if !h.agentLost(a, grace, now) {
			continue
		}
		lost[AgentLocation(a.ID)] = true
		if a.OfflineAlerted != store.AgentAlertNone || !a.AlertOffline || h.agentInMaintenance(a.ID) {
			continue
		}
		if err := h.store.SetAgentAlerted(a.ID, store.AgentAlertSent); err != nil {
			h.log.Error("agent offline", "err", err)
			continue
		}
		h.log.Warn("agent offline", "agent", a.Name)
		h.sendAgentEvent(agentEvent(a, AgentOffline, at))
	}
	h.markUnreachable(lost, at)
}

// agentLost reports whether the agent has been offline longer than the
// grace period (longer when it was just sent an update).
func (h *Hub) agentLost(a *store.Agent, grace time.Duration, now time.Time) bool {
	if h.AgentOnline(a.ID) {
		return false
	}
	since := agentOfflineSince(a, h.started)
	if since == 0 {
		return false
	}
	if h.agentUpdating(a.ID, now) {
		grace = max(grace, updateOfflineGrace)
	}
	return now.UnixMilli()-since >= grace.Milliseconds()
}

// lostAgent is an agent offline past the grace period, for the overview.
type lostAgent struct {
	ID           int64  `json:"id"`
	Name         string `json:"name"`
	OfflineSince int64  `json:"offline_since"`
}

// lostAgents lists the agents offline past the grace period.
func (h *Hub) lostAgents(agents []*store.Agent, now time.Time) []lostAgent {
	grace := h.agentUpdateSettings().offlineGrace()
	out := []lostAgent{}
	for _, a := range agents {
		if h.agentLost(a, grace, now) {
			out = append(out, lostAgent{ID: a.ID, Name: agentLabel(a), OfflineSince: cmp.Or(a.OfflineSince, a.LastSeen)})
		}
	}
	return out
}

// agentEvent is the event of an agent going offline or coming back.
func agentEvent(a *store.Agent, status string, at int64) Event {
	since := a.OfflineSince
	if since == 0 {
		since = a.LastSeen
	}
	e := Event{Kind: KindAgent, AgentID: a.ID, Agent: agentLabel(a), Target: a.Hostname, Status: status, At: at, DownSince: since}
	if status == AgentOffline && a.RemoteAddr != "" {
		e.Cause = "last connected from " + a.RemoteAddr
	}
	return e
}

// agentUpdating reports whether the agent was sent an update recently.
func (h *Hub) agentUpdating(id int64, now time.Time) bool {
	u := &h.updates
	u.mu.Lock()
	defer u.mu.Unlock()
	st := u.state[id]
	return st != nil && st.err == "" && now.Sub(st.started) < updateOfflineGrace
}

// agentInMaintenance reports whether every active monitor the agent checks
// is in a maintenance window. An agent with no monitors is not.
func (h *Hub) agentInMaintenance(id int64) bool {
	loc := AgentLocation(id)
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for _, st := range h.monitors {
		if st.m.Paused || !slices.Contains(h.locationsOf(st.m), loc) {
			continue
		}
		if !st.inMaint {
			return false
		}
		n++
	}
	return n > 0
}

// agentConnectedAlert sends the "online" event of an agent reported offline
// and clears its offline state. a is the agent as stored before it connected.
func (h *Hub) agentConnectedAlert(a *store.Agent) {
	if a.OfflineAlerted == store.AgentAlertSent && a.AlertOffline {
		h.log.Info("agent back online", "agent", a.Name)
		h.sendAgentEvent(agentEvent(a, AgentOnline, time.Now().UnixMilli()))
	}
	if a.OfflineSince != 0 || a.OfflineAlerted != store.AgentAlertNone {
		_ = h.store.SetAgentOnline(a.ID)
	}
}

// sendAgentEvent delivers an agent event to every enabled channel with the
// agent_offline event, whatever monitors the channel covers.
func (h *Hub) sendAgentEvent(e Event) {
	list, err := h.store.ListNotifiers()
	if err != nil {
		h.log.Error("list notifiers", "err", err)
		return
	}
	for _, nt := range list {
		if nt.Enabled && slices.Contains(nt.Events, store.EventAgentOffline) {
			h.notify.SendTo(nt, e)
		}
	}
}

// markUnreachable turns pending the monitors whose locations are all in lost.
// Monitors with an open incident stay down, so its reminders keep going.
func (h *Hub) markUnreachable(lost map[string]bool, at int64) {
	if len(lost) == 0 {
		return
	}
	var changed []*store.Monitor
	h.mu.Lock()
	for _, st := range h.monitors {
		m := st.m
		if m.Paused || m.Type == check.TypePush || st.incident || st.inMaint || m.Status == StatusPending {
			continue
		}
		if !allLost(h.locationsOf(m), lost) {
			continue
		}
		st.real = ""
		m.Status, m.StatusSince = StatusPending, at
		changed = append(changed, m)
	}
	h.mu.Unlock()
	for _, m := range changed {
		_ = h.store.SetMonitorStatus(m.ID, StatusPending, at)
		h.log.Info("status change", "monitor", m.Name, "to", StatusPending, "cause", "no location online")
	}
	if len(changed) > 0 {
		h.LiveActivitiesChanged()
	}
}

func allLost(locs []string, lost map[string]bool) bool {
	for _, l := range locs {
		if !lost[l] {
			return false
		}
	}
	return len(locs) > 0
}

// agentText is the text of an agent event.
func (e Event) agentText(prefix string) string {
	if e.Status == AgentOffline {
		s := fmt.Sprintf("%s[AGENT OFFLINE] %s is offline", prefix, e.Agent)
		if e.DownSince > 0 {
			s += " since " + time.UnixMilli(e.DownSince).UTC().Format("2006-01-02 15:04:05 UTC")
		}
		if e.Cause != "" {
			s += "\n> " + e.Cause
		}
		return s
	}
	s := fmt.Sprintf("%s[AGENT ONLINE] %s is back online", prefix, e.Agent)
	if d := e.downFor(); d != "" {
		s += " after " + d + " offline"
	}
	return s
}

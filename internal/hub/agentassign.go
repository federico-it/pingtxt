package hub

import (
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Reasons a monitor is left out when an agent is added to every compatible
// monitor. The web UI translates them.
const (
	skipNoIPv6      = "no_ipv6"      // IPv6-only (or IPv4 and IPv6) monitor, the agent has no IPv6
	skipIPv6Unknown = "ipv6_unknown" // IPv6 monitor, the agent has not reported IPv6 yet
	skipDocker      = "docker"       // checks a container on the probe's own host
	skipLoopback    = "loopback"     // target is localhost: another machine at each location
)

type assignMonitor struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Target string `json:"target"`
	Reason string `json:"reason,omitempty"`
}

type assignPlan struct {
	Compatible []assignMonitor `json:"compatible"`
	Skipped    []assignMonitor `json:"skipped"`
	// Already counts the monitors that check from this agent already.
	Already int `json:"already"`
}

// assignSkipReason tells why the location loc cannot be added to m, or ""
// when it can. Push monitors have no locations and are not asked about.
func (h *Hub) assignSkipReason(m *store.Monitor, loc string) string {
	if m.Type == check.TypeDocker {
		return skipDocker
	}
	if loopbackTarget(m.Target) {
		return skipLoopback
	}
	if needsIPv6(m.IPFamily) {
		switch h.locationIPv6(loc) {
		case check.IPv6No:
			return skipNoIPv6
		case check.IPv6Unknown:
			// Saving the monitor would be allowed (older agents never
			// report), but a bulk add should not guess.
			return skipIPv6Unknown
		}
	}
	return ""
}

// loopbackTarget reports whether a target points at the probe's own machine.
func loopbackTarget(target string) bool {
	host := strings.TrimSpace(target)
	if u, err := url.Parse(host); err == nil && u.Scheme != "" && u.Host != "" {
		host = u.Hostname()
	} else if hh, _, err := net.SplitHostPort(host); err == nil {
		host = hh
	}
	host = strings.Trim(strings.ToLower(host), "[]")
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsUnspecified())
}

func (h *Hub) agentAssignPlan(agentID int64) (assignPlan, error) {
	loc := AgentLocation(agentID)
	plan := assignPlan{Compatible: []assignMonitor{}, Skipped: []assignMonitor{}}
	ms, err := h.store.ListMonitors()
	if err != nil {
		return plan, err
	}
	for _, m := range ms {
		if m.Type == check.TypePush {
			continue
		}
		if slices.Contains(m.Locations, loc) {
			plan.Already++
			continue
		}
		am := assignMonitor{ID: m.ID, Name: m.Name, Type: m.Type, Target: m.Target}
		if am.Reason = h.assignSkipReason(m, loc); am.Reason != "" {
			plan.Skipped = append(plan.Skipped, am)
		} else {
			plan.Compatible = append(plan.Compatible, am)
		}
	}
	return plan, nil
}

// agentMonitors previews which monitors an agent can be added to.
func (h *Hub) agentMonitors(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	if _, err := h.store.Agent(id); err != nil {
		writeErr(w, 404, "agent not found")
		return
	}
	plan, err := h.agentAssignPlan(id)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, plan)
}

// assignAgent adds an agent's location to the given monitors. Monitors that
// are not compatible (anymore) are skipped and returned with their reason.
func (h *Hub) assignAgent(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	var in struct {
		MonitorIDs []int64 `json:"monitor_ids"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if _, err := h.store.Agent(id); err != nil {
		writeErr(w, 404, "agent not found")
		return
	}
	loc := AgentLocation(id)
	added := 0
	skipped := []assignMonitor{}
	for _, mid := range slices.Compact(slices.Sorted(slices.Values(in.MonitorIDs))) {
		m, err := h.store.Monitor(mid)
		if err != nil || m.Type == check.TypePush || slices.Contains(m.Locations, loc) {
			continue
		}
		if reason := h.assignSkipReason(m, loc); reason != "" {
			skipped = append(skipped, assignMonitor{ID: m.ID, Name: m.Name, Type: m.Type, Target: m.Target, Reason: reason})
			continue
		}
		m.Locations = append(m.Locations, loc)
		slices.Sort(m.Locations)
		if err := h.store.UpdateMonitor(m); err != nil {
			h.internalErr(w, err)
			return
		}
		h.MonitorChanged(m.ID)
		added++
	}
	writeJSON(w, 200, map[string]any{"added": added, "skipped": skipped})
}

// agentMonitorCounts returns how many monitors check from each location.
func agentMonitorCounts(ms []*store.Monitor) map[string]int {
	out := map[string]int{}
	for _, m := range ms {
		if m.Type == check.TypePush {
			continue
		}
		for _, l := range m.Locations {
			out[l]++
		}
	}
	return out
}

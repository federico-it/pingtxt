package hub

import (
	"bytes"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Prometheus metrics at GET /metrics, in the text exposition format.
//
// Access needs an API token (any scope: Prometheus sends it with
// "authorization: credentials: ptk_..."), or a browser session. The
// "public metrics" setting lets anyone scrape without a token.
//
// The body is cached for a few seconds so frequent scrapes don't run the
// uptime queries every time.

const metricsCacheTTL = 10 * time.Second

type metricsCache struct {
	mu   sync.Mutex
	at   time.Time
	body []byte
}

// MetricsSettings configure the Prometheus endpoint.
type MetricsSettings struct {
	// Public serves /metrics without a token.
	Public bool `json:"public"`
}

func (h *Hub) metrics(w http.ResponseWriter, r *http.Request) {
	if !h.generalSettings().Metrics.Public {
		if _, _, err := h.authenticate(r); err != nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="pingtxt"`)
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
	}
	body, err := h.metricsBody(time.Now())
	if err != nil {
		h.internalErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write(body)
}

func (h *Hub) metricsBody(now time.Time) ([]byte, error) {
	c := &h.metricsCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.body != nil && now.Sub(c.at) < metricsCacheTTL {
		return c.body, nil
	}
	b, err := h.renderMetrics(now)
	if err != nil {
		return nil, err
	}
	c.at, c.body = now, b
	return b, nil
}

// monitorStatuses lists every status, so a state-set metric always has all series.
var monitorStatuses = []string{StatusUp, StatusDown, StatusDegraded, StatusPending, StatusPaused, StatusMaintenance}

var uptimeWindows = []struct {
	label string
	d     time.Duration
}{
	{"24h", 24 * time.Hour},
	{"7d", 7 * 24 * time.Hour},
	{"30d", 30 * 24 * time.Hour},
}

func (h *Hub) renderMetrics(now time.Time) ([]byte, error) {
	ms, err := h.store.ListMonitors()
	if err != nil {
		return nil, err
	}
	agents, err := h.store.ListAgents()
	if err != nil {
		return nil, err
	}
	expiries, err := h.store.ExpiryChecks(0)
	if err != nil {
		return nil, err
	}
	openIncident, err := h.store.OpenIncidentMonitors()
	if err != nil {
		return nil, err
	}

	// Location id -> display name, as in the UI.
	locName := map[string]string{LocalLocation: h.localName(), PushLocation: pushLocationName}
	online := map[int64]bool{}
	for _, a := range agents {
		n := a.Name
		if a.Location != "" {
			n = a.Location
		}
		locName[AgentLocation(a.ID)] = n
		online[a.ID] = h.AgentOnline(a.ID)
	}
	nameOf := func(loc string) string {
		if n, ok := locName[loc]; ok {
			return n
		}
		return loc
	}

	var e expo
	e.family("pingtxt_info", "gauge", "Pingtxt hub version.")
	e.sample("pingtxt_info", labels{"version", h.cfg.Version}, 1)

	// ---- monitors ----
	counts := map[string]int{}
	for _, m := range ms {
		counts[m.Status]++
	}
	e.family("pingtxt_monitors", "gauge", "Number of monitors by status.")
	for _, s := range monitorStatuses {
		e.sample("pingtxt_monitors", labels{"status", s}, float64(counts[s]))
	}

	e.family("pingtxt_monitor_up", "gauge", "1 when the monitor is up or degraded, 0 when down. Not set while pending, paused or in maintenance.")
	for _, m := range ms {
		switch m.Status {
		case StatusUp, StatusDegraded:
			e.sample("pingtxt_monitor_up", monLabels(m), 1)
		case StatusDown:
			e.sample("pingtxt_monitor_up", monLabels(m), 0)
		}
	}

	e.family("pingtxt_monitor_status", "gauge", "Current status of the monitor: 1 for the active status, 0 for the others.")
	for _, m := range ms {
		for _, s := range monitorStatuses {
			e.sample("pingtxt_monitor_status", append(monLabels(m), "status", s), b2f(m.Status == s))
		}
	}

	e.family("pingtxt_monitor_status_since_timestamp_seconds", "gauge", "When the monitor entered its current status.")
	for _, m := range ms {
		if m.StatusSince > 0 {
			e.sample("pingtxt_monitor_status_since_timestamp_seconds", monLabels(m), msToSec(m.StatusSince))
		}
	}

	e.family("pingtxt_monitor_incident_open", "gauge", "1 while the monitor has an open incident.")
	for _, m := range ms {
		e.sample("pingtxt_monitor_incident_open", monLabels(m), b2f(openIncident[m.ID]))
	}

	// Latest result per location.
	type locResult struct {
		m *store.Monitor
		r *store.ResultRow
	}
	var latest []locResult
	for _, m := range ms {
		rows, err := h.store.LatestPerLocation(m.ID)
		if err != nil {
			return nil, err
		}
		sort.Slice(rows, func(i, j int) bool { return rows[i].Location < rows[j].Location })
		for _, r := range rows {
			latest = append(latest, locResult{m, r})
		}
	}
	locLabels := func(x locResult) labels {
		return append(monLabels(x.m), "location", x.r.Location, "location_name", nameOf(x.r.Location))
	}
	e.family("pingtxt_check_up", "gauge", "Result of the last check from each location: 1 ok, 0 failed.")
	for _, x := range latest {
		e.sample("pingtxt_check_up", locLabels(x), b2f(x.r.OK))
	}
	e.family("pingtxt_check_latency_seconds", "gauge", "Response time of the last check from each location.")
	for _, x := range latest {
		e.sample("pingtxt_check_latency_seconds", locLabels(x), x.r.LatencyMs/1000)
	}
	e.family("pingtxt_check_phase_seconds", "gauge", "Time per phase of the last HTTP check from each location: dns, connect, tls, ttfb (wait for the first byte).")
	for _, x := range latest {
		if p := x.r.Timing; p != nil {
			for _, ph := range []struct {
				name string
				ms   float64
			}{{"dns", p.DNSMs}, {"connect", p.ConnectMs}, {"tls", p.TLSMs}, {"ttfb", p.TTFBMs}} {
				e.sample("pingtxt_check_phase_seconds", append(locLabels(x), "phase", ph.name), ph.ms/1000)
			}
		}
	}
	e.family("pingtxt_check_status_code", "gauge", "HTTP status code of the last check from each location (HTTP monitors).")
	for _, x := range latest {
		if x.r.StatusCode > 0 {
			e.sample("pingtxt_check_status_code", locLabels(x), float64(x.r.StatusCode))
		}
	}
	e.family("pingtxt_check_timestamp_seconds", "gauge", "When the last check from each location ran.")
	for _, x := range latest {
		e.sample("pingtxt_check_timestamp_seconds", locLabels(x), msToSec(x.r.TS))
	}

	// Uptime and average latency, maintenance windows left out.
	e.family("pingtxt_monitor_uptime_ratio", "gauge", "Share of checks taken while the monitor was up or degraded over the window (0-1), maintenance windows excluded.")
	var avg []struct {
		m      *store.Monitor
		window string
		v      float64
	}
	for _, m := range ms {
		for _, win := range uptimeWindows {
			up, lat, err := h.store.Uptime(m.ID, now.Add(-win.d).UnixMilli())
			if err != nil {
				return nil, err
			}
			if up < 0 {
				continue
			}
			e.sample("pingtxt_monitor_uptime_ratio", append(monLabels(m), "window", win.label), up)
			avg = append(avg, struct {
				m      *store.Monitor
				window string
				v      float64
			}{m, win.label, lat / 1000})
		}
	}
	e.family("pingtxt_monitor_latency_avg_seconds", "gauge", "Average response time of successful checks over the window.")
	for _, a := range avg {
		e.sample("pingtxt_monitor_latency_avg_seconds", append(monLabels(a.m), "window", a.window), a.v)
	}

	// ---- certificate and domain expiry ----
	byID := map[int64]*store.Monitor{}
	for _, m := range ms {
		byID[m.ID] = m
	}
	sort.Slice(expiries, func(i, j int) bool {
		if expiries[i].MonitorID != expiries[j].MonitorID {
			return expiries[i].MonitorID < expiries[j].MonitorID
		}
		return expiries[i].Kind < expiries[j].Kind
	})
	for _, kind := range []struct{ kind, name, help string }{
		{store.ExpiryCert, "pingtxt_cert_expiry", "TLS certificate"},
		{store.ExpiryDomain, "pingtxt_domain_expiry", "domain registration"},
	} {
		ts := kind.name + "_timestamp_seconds"
		days := kind.name + "_days"
		e.family(ts, "gauge", "When the "+kind.help+" expires.")
		e.family(days, "gauge", "Days left before the "+kind.help+" expires (negative once expired).")
		for _, x := range expiries {
			m := byID[x.MonitorID]
			if m == nil || x.Kind != kind.kind || x.ExpiresAt <= 0 {
				continue
			}
			l := append(monLabels(m), "subject", x.Subject)
			e.sample(ts, l, msToSec(x.ExpiresAt))
			e.sample(days, l, float64(x.ExpiresAt-now.UnixMilli())/float64(24*time.Hour/time.Millisecond))
		}
	}

	// ---- agents ----
	e.family("pingtxt_agents", "gauge", "Number of agents by connection state.")
	n := 0
	for _, a := range agents {
		if online[a.ID] {
			n++
		}
	}
	e.sample("pingtxt_agents", labels{"state", "connected"}, float64(n))
	e.sample("pingtxt_agents", labels{"state", "disconnected"}, float64(len(agents)-n))

	e.family("pingtxt_agent_connected", "gauge", "1 while the agent is connected to the hub.")
	for _, a := range agents {
		e.sample("pingtxt_agent_connected", agentLabels(a, locName), b2f(online[a.ID]))
	}
	e.family("pingtxt_agent_last_seen_timestamp_seconds", "gauge", "Last time the agent was seen.")
	for _, a := range agents {
		if a.LastSeen > 0 {
			e.sample("pingtxt_agent_last_seen_timestamp_seconds", agentLabels(a, locName), msToSec(a.LastSeen))
		}
	}

	e.family("pingtxt_incidents_open", "gauge", "Number of open incidents.")
	e.sample("pingtxt_incidents_open", nil, float64(len(openIncident)))

	return e.bytes(), nil
}

// labels is a flat list of name, value pairs.
type labels []string

func monLabels(m *store.Monitor) labels {
	return labels{"monitor_id", strconv.FormatInt(m.ID, 10), "monitor", m.Name, "type", m.Type}
}

func agentLabels(a *store.Agent, locName map[string]string) labels {
	return labels{"agent_id", strconv.FormatInt(a.ID, 10), "agent", a.Name, "location", AgentLocation(a.ID), "location_name", locName[AgentLocation(a.ID)], "version", a.Version}
}

func b2f(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

func msToSec(ms int64) float64 { return float64(ms) / 1000 }

// expo writes the Prometheus text format, where every sample of a family is
// grouped under its header: samples are buffered per family.
type expo struct {
	order []string
	fam   map[string]*bytes.Buffer
}

func (e *expo) family(name, typ, help string) {
	if e.fam == nil {
		e.fam = map[string]*bytes.Buffer{}
	}
	b := &bytes.Buffer{}
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	e.fam[name] = b
	e.order = append(e.order, name)
}

func (e *expo) sample(name string, l labels, v float64) {
	b := e.fam[name]
	b.WriteString(name)
	if len(l) > 0 {
		b.WriteByte('{')
		for i := 0; i+1 < len(l); i += 2 {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(l[i])
			b.WriteString(`="`)
			b.WriteString(escapeLabel(l[i+1]))
			b.WriteByte('"')
		}
		b.WriteByte('}')
	}
	b.WriteByte(' ')
	b.WriteString(formatFloat(v))
	b.WriteByte('\n')
}

func (e *expo) bytes() []byte {
	var out bytes.Buffer
	for _, n := range e.order {
		out.Write(e.fam[n].Bytes())
	}
	return out.Bytes()
}

var labelEscaper = strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)

func escapeLabel(s string) string { return labelEscaper.Replace(s) }

func formatFloat(v float64) string {
	switch {
	case math.IsNaN(v):
		return "NaN"
	case math.IsInf(v, 1):
		return "+Inf"
	case math.IsInf(v, -1):
		return "-Inf"
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}

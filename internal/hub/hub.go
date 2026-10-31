// Package hub contains the central server: scheduler, agent connections,
// status evaluation, notifications and the HTTP API.
package hub

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/expiry"
	"github.com/federico-it/pingtxt/internal/protocol"
	"github.com/federico-it/pingtxt/internal/store"
)

// LocalLocation is the id of the hub's built-in probe location.
const LocalLocation = "local"

// Monitor statuses.
const (
	StatusPending  = "pending"
	StatusUp       = "up"
	StatusDown     = "down"
	StatusDegraded = "degraded"
	StatusPaused   = "paused"
	// StatusMaintenance is shown while a maintenance window covers the
	// monitor: checks keep running but no incidents or alerts are raised.
	StatusMaintenance = "maintenance"
)

// Config holds startup settings. PublicURL, LocalName, RetentionDays and
// DisableLocal only seed the database on first start: afterwards they are
// edited from the web UI (settings page).
type Config struct {
	Addr          string
	DataDir       string
	RetentionDays int
	PublicURL     string
	AgentDir      string
	LocalName     string
	Version       string
	DisableLocal  bool
	// DisableUpdateCheck turns the release check off on first start.
	DisableUpdateCheck bool
}

type locState struct {
	fails   int
	down    bool
	lastMsg string
}

type monState struct {
	m *store.Monitor
	// next is the time of the next probe. For push monitors it is the
	// deadline for the next heartbeat.
	next     time.Time
	locs     map[string]*locState
	lastPing time.Time // push monitors only

	real     string // status computed from the checks, ignoring maintenance
	inMaint  bool   // a maintenance window currently covers the monitor
	incident bool   // an incident is open (alerted as down)

	busy  bool      // a local check is queued or running
	stats *monStats // last 24h of results, for the dashboard
}

// Hub is the central server.
type Hub struct {
	cfg    Config
	store  *store.Store
	log    *slog.Logger
	notify *Notifier
	expiry expiryChecker

	mu       sync.Mutex
	agents   map[int64]*agentConn
	monitors map[int64]*monState
	maints   []*store.Maintenance
	// warmUntil is when the monitors were loaded: older results are added to
	// the dashboard stats from the database by warmStats.
	warmUntil int64

	localQueue chan check.Job
	skipped    atomic.Int64                 // local checks skipped: previous still running or pool full
	writer     atomic.Pointer[resultWriter] // batches result inserts while Run is active

	smu     sync.RWMutex
	general GeneralSettings
	auth    AuthSettings

	oidc       oidcDiscovery
	states     oauthStates
	oauth      oauthPending // OAuth consent requests and codes of MCP clients
	challenges challenges
	limits     loginLimits // brute force protection, see ratelimit.go
	httpClient *http.Client

	pages     statusPages
	subLimits subscribeLimits // status page subscriptions, see pageincidents.go

	metricsCache metricsCache
	updates      agentUpdates
	releases     releaseChecker

	backupMu sync.Mutex // one snapshot at a time
	importMu sync.Mutex // one configuration import at a time
}

// New creates a hub.
func New(cfg Config, st *store.Store, log *slog.Logger) *Hub {
	h := &Hub{
		cfg:        cfg,
		store:      st,
		log:        log,
		notify:     NewNotifier(st, log),
		agents:     map[int64]*agentConn{},
		monitors:   map[int64]*monState{},
		localQueue: make(chan check.Job, localQueueSize),
	}
	h.general, h.auth = defaultGeneral(cfg), defaultAuth()
	h.httpClient = &http.Client{Timeout: 15 * time.Second}
	h.releases.wake = make(chan struct{}, 1)
	h.expiry.domains = expiry.NewDomains(h.httpClient)
	h.expiry.cache = map[string]domainCacheEntry{}
	if err := h.loadSettings(); err != nil {
		log.Error("load settings", "err", err)
	}
	h.reloadPageDomains()
	return h
}

// AgentLocation returns the location id for an agent.
func AgentLocation(id int64) string { return "agent:" + strconv.FormatInt(id, 10) }

func parseAgentLocation(loc string) (int64, bool) {
	s, ok := strings.CutPrefix(loc, "agent:")
	if !ok {
		return 0, false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	return id, err == nil
}

// Run starts the scheduler and maintenance loops until ctx is done.
func (h *Hub) Run(ctx context.Context) error {
	if err := h.reloadAll(); err != nil {
		return err
	}
	w := newResultWriter(h.store, h.log)
	h.writer.Store(w)
	go w.run()
	defer func() {
		// Stop the writer after the scheduler, flushing what is queued.
		h.writer.Store(nil)
		w.stop()
	}()
	go h.warmStats()
	for range localWorkers {
		go h.localWorker(ctx)
	}
	go h.maintenance(ctx)
	go h.expiryLoop(ctx)
	go h.backupLoop(ctx)
	go h.releaseLoop(ctx)
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case now := <-t.C:
			h.tick(ctx, now)
		}
	}
}

func (h *Hub) reloadAll() error {
	ms, err := h.store.ListMonitors()
	if err != nil {
		return err
	}
	if err := h.MaintenancesChanged(); err != nil {
		return err
	}
	open := map[int64]bool{}
	for _, m := range ms {
		open[m.ID], _ = h.store.HasOpenIncident(m.ID)
	}
	now := time.Now()
	h.mu.Lock()
	defer h.mu.Unlock()
	h.warmUntil = now.UnixMilli()
	for _, m := range ms {
		next := now.Add(firstRunOffset(m))
		if m.Type == check.TypePush {
			// Give jobs a full period after a restart before calling them late.
			next = now.Add(pushWindow(m))
		}
		h.monitors[m.ID] = &monState{m: m, next: next, locs: map[string]*locState{}, incident: open[m.ID], stats: &monStats{since: h.warmUntil}}
	}
	return nil
}

// MonitorChanged reloads one monitor after an API change (or removes it).
func (h *Hub) MonitorChanged(id int64) {
	m, err := h.store.Monitor(id)
	h.mu.Lock()
	defer h.mu.Unlock()
	if err != nil {
		delete(h.monitors, id)
		return
	}
	next := time.Now()
	if m.Type == check.TypePush {
		next = next.Add(pushWindow(m))
	}
	if st, ok := h.monitors[id]; ok {
		st.m = m
		st.next = next
		// Drop states of locations no longer assigned.
		keep := map[string]bool{}
		for _, l := range h.locationsOf(m) {
			keep[l] = true
		}
		for l := range st.locs {
			if !keep[l] {
				delete(st.locs, l)
			}
		}
		if m.Paused {
			st.locs = map[string]*locState{}
			st.real = ""
			st.incident = false // pausing resolves open incidents
		}
		// Re-evaluate maintenance on the next tick (pause/resume resets status).
		st.inMaint = false
		return
	}
	h.monitors[id] = &monState{m: m, next: next, locs: map[string]*locState{}, stats: &monStats{}}
}

func (h *Hub) locationsOf(m *store.Monitor) []string {
	if m.Type == check.TypePush {
		return []string{PushLocation}
	}
	if len(m.Locations) == 0 {
		return []string{LocalLocation}
	}
	return m.Locations
}

func jobFor(m *store.Monitor) check.Job {
	return check.Job{
		ID:             fmt.Sprintf("%d-%d", m.ID, time.Now().UnixNano()),
		MonitorID:      m.ID,
		Type:           m.Type,
		Target:         m.Target,
		TimeoutMs:      m.TimeoutMs,
		Method:         m.Method,
		ExpectedStatus: m.ExpectedStatus,
		Keyword:        m.Keyword,
		KeywordInvert:  m.KeywordInvert,
		DNSRecord:      m.DNSRecord,
		DNSServer:      m.DNSServer,
		DNSExpected:    m.DNSExpected,
		IgnoreTLS:      m.IgnoreTLS,
		Headers:        m.Headers,
		Body:           m.Body,
		Assertions:     m.Assertions,
	}
}

func (h *Hub) tick(ctx context.Context, now time.Time) {
	h.maintTick(now)
	type dispatch struct {
		loc string
		job check.Job
	}
	var work []dispatch
	var missed []check.Result
	h.mu.Lock()
	for _, st := range h.monitors {
		if st.m.Paused || now.Before(st.next) {
			continue
		}
		if st.m.Type == check.TypePush {
			missed = append(missed, missedHeartbeat(st, now))
			continue
		}
		iv := time.Duration(max(st.m.IntervalSec, 10)) * time.Second
		// Keep the monitor on its own slot instead of drifting by the tick
		// delay; if it fell behind (paused, skipped), restart from now.
		st.next = st.next.Add(iv)
		if !st.next.After(now) {
			st.next = now.Add(iv)
		}
		job := jobFor(st.m)
		for _, loc := range h.locationsOf(st.m) {
			if loc == LocalLocation {
				if st.busy {
					// The previous local check has not finished yet: skip
					// this round instead of piling up checks.
					h.skipped.Add(1)
					continue
				}
				st.busy = true
			}
			work = append(work, dispatch{loc, job})
		}
	}
	h.mu.Unlock()

	for _, r := range missed {
		h.HandleResult(PushLocation, r)
	}
	for _, w := range work {
		if w.loc == LocalLocation {
			if !h.localEnabled() {
				h.setIdle(w.job.MonitorID)
				continue
			}
			select {
			case h.localQueue <- w.job:
			default:
				// All workers are busy and the queue is full.
				h.skipped.Add(1)
				h.setIdle(w.job.MonitorID)
			}
			continue
		}
		id, ok := parseAgentLocation(w.loc)
		if !ok {
			continue
		}
		h.mu.Lock()
		ac := h.agents[id]
		h.mu.Unlock()
		if ac == nil {
			continue // offline: skipped, it does not count toward status
		}
		job := w.job
		ac.send(protocol.Message{Type: protocol.TypeJob, Job: &job})
	}
}

// localWorker runs checks for the hub's built-in location. A fixed pool of
// workers bounds the number of checks (and goroutines) in flight.
func (h *Hub) localWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-h.localQueue:
			r := check.Run(ctx, job)
			if ctx.Err() == nil {
				h.HandleResult(LocalLocation, r)
			}
			h.setIdle(job.MonitorID)
		}
	}
}

func (h *Hub) setIdle(id int64) {
	h.mu.Lock()
	if st := h.monitors[id]; st != nil {
		st.busy = false
	}
	h.mu.Unlock()
}

// HandleResult stores a result and re-evaluates the monitor status.
func (h *Hub) HandleResult(loc string, r check.Result) {
	defPct := h.generalSettings().DegradedPct
	h.mu.Lock()
	st, ok := h.monitors[r.MonitorID]
	if !ok || st.m.Paused {
		h.mu.Unlock()
		return
	}
	allowed := false
	for _, l := range h.locationsOf(st.m) {
		if l == loc {
			allowed = true
		}
	}
	if !allowed {
		h.mu.Unlock()
		return
	}
	m := st.m
	// An agent built before response rules existed ignores them and would
	// report the monitor up: fail instead, so the problem is visible.
	if r.OK && len(m.Assertions) > 0 && r.Asserted < len(m.Assertions) && loc != LocalLocation {
		r.OK = false
		r.Message = "this agent cannot check response rules: update it"
	}
	pct := m.DegradedPct
	if pct == 0 {
		pct = defPct
	}
	ls := st.locs[loc]
	if ls == nil {
		ls = &locState{}
		st.locs[loc] = ls
	}
	ls.lastMsg = r.Message
	if r.OK {
		ls.fails = 0
		ls.down = false
	} else {
		ls.fails++
		if ls.fails >= max(m.Retries, 1) {
			ls.down = true
		}
	}
	// Evaluate over locations that are currently reachable.
	n, down := 0, 0
	var causes []string
	for l, s := range st.locs {
		if l != LocalLocation {
			if id, ok := parseAgentLocation(l); ok && h.agents[id] == nil {
				continue
			}
		}
		n++
		if s.down {
			down++
			causes = append(causes, h.locationNameLocked(l)+": "+s.lastMsg)
		}
	}
	status := classify(n, down, pct)
	st.real = status
	// Uptime is computed from the status kept with each result. Locations
	// that have not answered yet since the start count as not failing, so a
	// failing agent that happens to answer first does not count as downtime.
	pending := 0
	for _, l := range h.locationsOf(m) {
		if st.locs[l] != nil {
			continue
		}
		if id, ok := parseAgentLocation(l); ok && h.agents[id] == nil {
			continue
		}
		pending++
	}
	state := resultState(classify(n+pending, down, pct))
	inMaint := st.inMaint
	display := status
	if inMaint {
		display = StatusMaintenance
	}
	prev := m.Status
	changed := display != prev
	if changed {
		m.Status = display
		m.StatusSince = r.At
	}
	since := m.StatusSince
	name := m.Name
	alert := h.alertLocked(st)
	row := &store.ResultRow{
		MonitorID: r.MonitorID, Location: loc, TS: r.At, OK: r.OK, LatencyMs: r.LatencyMs,
		StatusCode: r.StatusCode, Message: r.Message, CertExpiry: r.CertExpiry, Maint: inMaint,
		MonState: state,
	}
	if st.stats != nil {
		st.stats.add(row)
	}
	h.mu.Unlock()

	if w := h.writer.Load(); w != nil {
		w.add(row)
	} else if err := h.store.InsertResult(row); err != nil {
		h.log.Error("insert result", "err", err)
	}
	if changed {
		_ = h.store.SetMonitorStatus(m.ID, display, since)
		h.log.Info("status change", "monitor", name, "from", prev, "to", display)
	}
	h.raise(m, alert, strings.Join(causes, "; "), r.At)
}

// resultState maps a monitor status to the value stored with each result, which
// uptime is computed from.
func resultState(status string) int {
	switch status {
	case StatusDown:
		return store.MonDown
	case StatusDegraded:
		return store.MonDegraded
	}
	return store.MonUp
}

// Share of failing locations that marks a monitor degraded.
const (
	defaultDegradedPct = 35
	maxDegradedPct     = 50 // above half of the locations the monitor is down
)

// classify gives a monitor's status from how many of its n reachable
// locations are failing: down when more than half fail, degraded when at
// least pct percent fail, up otherwise.
func classify(n, down, pct int) string {
	switch {
	case down == 0:
		return StatusUp
	case down*2 > n:
		return StatusDown
	case down*100 >= pct*n:
		return StatusDegraded
	}
	return StatusUp
}

// alertLocked decides whether an incident must be opened ("down") or
// resolved ("up") given the monitor's real status. Nothing is raised during
// maintenance; pending alerts fire once the window ends. Call with h.mu held.
func (h *Hub) alertLocked(st *monState) string {
	if st.inMaint || st.m.Paused {
		return ""
	}
	switch {
	case st.real == StatusDown && !st.incident:
		st.incident = true
		return StatusDown
	case st.real != "" && st.real != StatusDown && st.incident:
		st.incident = false
		return StatusUp
	}
	return ""
}

// raise opens or resolves the incident and sends notifications.
func (h *Hub) raise(m *store.Monitor, alert, cause string, at int64) {
	switch alert {
	case StatusDown:
		_ = h.store.OpenIncident(m.ID, at, cause)
		h.notify.Send(Event{MonitorID: m.ID, Monitor: m.Name, Target: eventTarget(m), Status: StatusDown, Cause: cause, At: at})
	case StatusUp:
		_ = h.store.ResolveIncident(m.ID, at)
		h.notify.Send(Event{MonitorID: m.ID, Monitor: m.Name, Target: eventTarget(m), Status: StatusUp, At: at})
	}
}

func (h *Hub) locationNameLocked(loc string) string {
	if loc == LocalLocation {
		return h.localName()
	}
	if loc == PushLocation {
		return pushLocationName
	}
	if id, ok := parseAgentLocation(loc); ok {
		if ac := h.agents[id]; ac != nil {
			return ac.label()
		}
	}
	return loc
}

// AgentOnline reports whether an agent is connected.
func (h *Hub) AgentOnline(id int64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.agents[id] != nil
}

// maintenance summarizes raw results into hourly rollups and deletes data
// past its retention: raw results after RawRetentionDays, hourly rollups
// after RetentionDays.
func (h *Hub) maintenance(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		h.compactResults(ctx, time.Now())
		h.cleanOAuth(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (h *Hub) compactResults(ctx context.Context, now time.Time) {
	g := h.generalSettings()
	t0 := time.Now()
	if n, err := h.store.RollupResults(now, ctx.Done()); err != nil {
		h.log.Error("roll up results", "err", err)
	} else if n > 1 {
		h.log.Info("rolled up results", "hours", n, "took", time.Since(t0).Round(time.Millisecond))
	}
	day := 24 * time.Hour
	if n, err := h.store.PruneResults(now.Add(-time.Duration(g.RawRetentionDays) * day).UnixMilli()); err != nil {
		h.log.Error("prune results", "err", err)
	} else if n > 0 {
		h.log.Info("pruned raw results", "rows", n)
	}
	if n, err := h.store.PruneRollups(now.Add(-time.Duration(g.RetentionDays) * day).UnixMilli()); err != nil {
		h.log.Error("prune rollups", "err", err)
	} else if n > 0 {
		h.log.Info("pruned hourly rollups", "rows", n)
	}
}

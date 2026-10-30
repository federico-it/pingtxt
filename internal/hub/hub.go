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
}

type locState struct {
	fails   int
	down    bool
	lastMsg string
}

type monState struct {
	m    *store.Monitor
	next time.Time
	locs map[string]*locState
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
	localSem chan struct{}

	smu     sync.RWMutex
	general GeneralSettings
	auth    AuthSettings

	oidc       oidcDiscovery
	states     oauthStates
	httpClient *http.Client
}

// New creates a hub.
func New(cfg Config, st *store.Store, log *slog.Logger) *Hub {
	h := &Hub{
		cfg:      cfg,
		store:    st,
		log:      log,
		notify:   NewNotifier(st, log),
		agents:   map[int64]*agentConn{},
		monitors: map[int64]*monState{},
		localSem: make(chan struct{}, 64),
	}
	h.general, h.auth = defaultGeneral(cfg), defaultAuth()
	h.httpClient = &http.Client{Timeout: 15 * time.Second}
	h.expiry.domains = expiry.NewDomains(h.httpClient)
	h.expiry.cache = map[string]domainCacheEntry{}
	if err := h.loadSettings(); err != nil {
		log.Error("load settings", "err", err)
	}
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
	go h.maintenance(ctx)
	go h.expiryLoop(ctx)
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
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, m := range ms {
		// Spread the first run over a few seconds to avoid a thundering herd.
		h.monitors[m.ID] = &monState{m: m, next: time.Now().Add(time.Duration(i%10) * 500 * time.Millisecond), locs: map[string]*locState{}}
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
	if st, ok := h.monitors[id]; ok {
		st.m = m
		st.next = time.Now()
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
		}
		return
	}
	h.monitors[id] = &monState{m: m, next: time.Now(), locs: map[string]*locState{}}
}

func (h *Hub) locationsOf(m *store.Monitor) []string {
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
	}
}

func (h *Hub) tick(ctx context.Context, now time.Time) {
	type dispatch struct {
		loc string
		job check.Job
	}
	var work []dispatch
	h.mu.Lock()
	for _, st := range h.monitors {
		if st.m.Paused || now.Before(st.next) {
			continue
		}
		iv := time.Duration(max(st.m.IntervalSec, 10)) * time.Second
		st.next = now.Add(iv)
		job := jobFor(st.m)
		for _, loc := range h.locationsOf(st.m) {
			work = append(work, dispatch{loc, job})
		}
	}
	h.mu.Unlock()

	for _, w := range work {
		if w.loc == LocalLocation {
			if !h.localEnabled() {
				continue
			}
			go h.runLocal(ctx, w.job)
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

func (h *Hub) runLocal(ctx context.Context, job check.Job) {
	select {
	case h.localSem <- struct{}{}:
	case <-ctx.Done():
		return
	}
	defer func() { <-h.localSem }()
	r := check.Run(ctx, job)
	h.HandleResult(LocalLocation, r)
}

// HandleResult stores a result and re-evaluates the monitor status.
func (h *Hub) HandleResult(loc string, r check.Result) {
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
	status := StatusUp
	switch {
	case down > 0 && down*2 > n:
		status = StatusDown
	case down > 0:
		status = StatusDegraded
	}
	prev := m.Status
	changed := status != prev
	if changed {
		m.Status = status
		m.StatusSince = r.At
	}
	since := m.StatusSince
	name := m.Name
	h.mu.Unlock()

	if err := h.store.InsertResult(&store.ResultRow{
		MonitorID: r.MonitorID, Location: loc, TS: r.At, OK: r.OK, LatencyMs: r.LatencyMs,
		StatusCode: r.StatusCode, Message: r.Message, CertExpiry: r.CertExpiry,
	}); err != nil {
		h.log.Error("insert result", "err", err)
	}
	if !changed {
		return
	}
	_ = h.store.SetMonitorStatus(m.ID, status, since)
	h.log.Info("status change", "monitor", name, "from", prev, "to", status)
	cause := strings.Join(causes, "; ")
	switch {
	case status == StatusDown && prev != StatusDown:
		_ = h.store.OpenIncident(m.ID, r.At, cause)
		h.notify.Send(Event{MonitorID: m.ID, Monitor: name, Target: m.Target, Status: StatusDown, Cause: cause, At: r.At})
	case prev == StatusDown && status != StatusDown:
		_ = h.store.ResolveIncident(m.ID, r.At)
		h.notify.Send(Event{MonitorID: m.ID, Monitor: name, Target: m.Target, Status: StatusUp, At: r.At})
	}
}

func (h *Hub) locationNameLocked(loc string) string {
	if loc == LocalLocation {
		return h.localName()
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

func (h *Hub) maintenance(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		days := h.generalSettings().RetentionDays
		if days <= 0 {
			days = 30
		}
		cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour).UnixMilli()
		if n, err := h.store.PruneResults(cutoff); err != nil {
			h.log.Error("prune results", "err", err)
		} else if n > 0 {
			h.log.Info("pruned old results", "rows", n)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

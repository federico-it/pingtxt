package hub

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/expiry"
	"github.com/federico-it/pingtxt/internal/store"
)

// How often expiry dates are looked up again.
const (
	certCheckEvery   = 6 * time.Hour
	domainCheckEvery = 12 * time.Hour
	expiryRetryEvery = time.Hour // after a failed lookup
	domainCacheTTL   = time.Hour // shared by monitors on the same domain
	expiryTimeout    = 20 * time.Second
)

// expiryChecker owns the lookups and serialises check passes.
type expiryChecker struct {
	run     sync.Mutex // one pass at a time
	domains *expiry.Domains

	mu    sync.Mutex
	cache map[string]domainCacheEntry
}

type domainCacheEntry struct {
	info *expiry.DomainInfo
	err  error
	at   time.Time
}

type expiryTarget struct {
	kind    string
	subject string // cert: host[:port], domain: registrable domain
	host    string
	port    string
}

// expiryTargets lists the expiry checks that apply to a monitor.
func expiryTargets(m *store.Monitor) []expiryTarget {
	host, port := expiry.Host(m.Target)
	if host == "" {
		return nil
	}
	var out []expiryTarget
	isHTTPS := (m.Type == check.TypeHTTP || m.Type == check.TypeKeyword) && strings.HasPrefix(strings.ToLower(m.Target), "https://")
	if m.CertExpiryAlert && isHTTPS {
		subject := host
		if port != "" && port != "443" {
			subject += ":" + port
		}
		out = append(out, expiryTarget{kind: store.ExpiryCert, subject: subject, host: host, port: port})
	}
	if m.DomainExpiryAlert {
		if d, err := expiry.RegistrableDomain(host); err == nil {
			out = append(out, expiryTarget{kind: store.ExpiryDomain, subject: d})
		}
	}
	return out
}

func (h *Hub) expiryLoop(ctx context.Context) {
	select { // let agents connect and the first checks run
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	t := time.NewTicker(time.Minute)
	defer t.Stop()
	for {
		if h.expiry.run.TryLock() {
			h.checkExpiries(ctx, 0, false)
			h.expiry.run.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// CheckExpiryNow looks up the expiry dates of one monitor right away.
func (h *Hub) CheckExpiryNow(ctx context.Context, monitorID int64) {
	h.expiry.run.Lock()
	defer h.expiry.run.Unlock()
	h.checkExpiries(ctx, monitorID, true)
}

// checkExpiries runs the due lookups (all of them when force) of one monitor,
// or of every active monitor when monitorID is 0. The caller holds expiry.run.
func (h *Hub) checkExpiries(ctx context.Context, monitorID int64, force bool) {
	var monitors []*store.Monitor
	if monitorID > 0 {
		m, err := h.store.Monitor(monitorID)
		if err != nil {
			return
		}
		monitors = []*store.Monitor{m}
	} else {
		var err error
		if monitors, err = h.store.ListMonitors(); err != nil {
			h.log.Error("expiry: list monitors", "err", err)
			return
		}
	}
	rows, err := h.store.ExpiryChecks(0)
	if err != nil {
		h.log.Error("expiry: list checks", "err", err)
		return
	}
	type key struct {
		id   int64
		kind string
	}
	existing := map[key]*store.ExpiryCheck{}
	for _, r := range rows {
		existing[key{r.MonitorID, r.Kind}] = r
	}
	sent := map[string]bool{} // one notification per subject and level per pass

	for _, m := range monitors {
		if ctx.Err() != nil {
			return
		}
		targets := expiryTargets(m)
		for _, kind := range []string{store.ExpiryCert, store.ExpiryDomain} {
			found := false
			for _, t := range targets {
				found = found || t.kind == kind
			}
			if _, ok := existing[key{m.ID, kind}]; ok && !found {
				_ = h.store.DeleteExpiryCheck(m.ID, kind)
				delete(existing, key{m.ID, kind})
			}
		}
		if m.Paused && !force {
			continue
		}
		for _, t := range targets {
			row := existing[key{m.ID, t.kind}]
			if row == nil || row.Subject != t.subject {
				row = &store.ExpiryCheck{MonitorID: m.ID, Kind: t.kind, Subject: t.subject, AlertedDays: -1}
				existing[key{m.ID, t.kind}] = row
			}
			if !force && !expiryDue(row, time.Now()) {
				continue
			}
			h.lookupExpiry(ctx, t, row, force)
			ev := h.evaluateExpiry(m, row, time.Now())
			if err := h.store.SaveExpiryCheck(row); err != nil {
				h.log.Error("expiry: save", "err", err)
				continue
			}
			if ev == nil {
				continue
			}
			// Several monitors often share a domain: notify once.
			id := t.kind + "|" + t.subject + "|" + ev.Status
			if ev.DaysLeft != nil && ev.Status == ExpiryExpiring {
				id += "|" + strconv.Itoa(row.AlertedDays)
			}
			dup := sent[id]
			if !dup && ev.Status != ExpiryRenewed {
				for k, other := range existing {
					if k.id != m.ID && k.kind == t.kind && other.Subject == t.subject && other.AlertedDays == row.AlertedDays && other.CheckedAt > time.Now().Add(-domainCheckEvery).UnixMilli() {
						dup = true
						break
					}
				}
			}
			sent[id] = true
			if !dup {
				h.log.Info("expiry alert", "monitor", m.Name, "kind", t.kind, "subject", t.subject, "status", ev.Status)
				h.notify.Send(*ev)
			}
		}
	}
}

func expiryDue(row *store.ExpiryCheck, now time.Time) bool {
	if row.CheckedAt == 0 {
		return true
	}
	every := certCheckEvery
	if row.Kind == store.ExpiryDomain {
		every = domainCheckEvery
	}
	if row.Error != "" && row.ExpiresAt == 0 {
		every = expiryRetryEvery
	}
	return now.Sub(time.UnixMilli(row.CheckedAt)) >= every
}

// lookupExpiry updates row with a fresh lookup. On failure the last known
// expiry date is kept and the error recorded.
func (h *Hub) lookupExpiry(ctx context.Context, t expiryTarget, row *store.ExpiryCheck, fresh bool) {
	ctx, cancel := context.WithTimeout(ctx, expiryTimeout)
	defer cancel()
	row.CheckedAt = time.Now().UnixMilli()
	switch t.kind {
	case store.ExpiryCert:
		info, err := expiry.Cert(ctx, t.host, t.port, 15*time.Second)
		if err != nil {
			row.Error = check.CleanErr(err)
			return
		}
		row.ExpiresAt = info.NotAfter.UnixMilli()
		row.Issuer = info.Issuer
		row.Source = "tls"
		row.Error = info.VerifyErr
	case store.ExpiryDomain:
		info, err := h.lookupDomain(ctx, t.subject, fresh)
		if err != nil {
			row.Error = check.CleanErr(err)
			return
		}
		row.ExpiresAt = info.ExpiresAt.UnixMilli()
		row.Issuer = info.Registrar
		row.Source = info.Source
		row.Error = ""
	}
}

func (h *Hub) lookupDomain(ctx context.Context, domain string, fresh bool) (*expiry.DomainInfo, error) {
	e := &h.expiry
	e.mu.Lock()
	c, ok := e.cache[domain]
	e.mu.Unlock()
	if ok && !fresh && time.Since(c.at) < domainCacheTTL {
		return c.info, c.err
	}
	info, err := e.domains.Lookup(ctx, domain)
	if errors.Is(err, context.Canceled) {
		return nil, err
	}
	e.mu.Lock()
	e.cache[domain] = domainCacheEntry{info: info, err: err, at: time.Now()}
	e.mu.Unlock()
	return info, err
}

// evaluateExpiry updates the alert state of row and returns the event to
// send, if any.
func (h *Hub) evaluateExpiry(m *store.Monitor, row *store.ExpiryCheck, now time.Time) *Event {
	if row.ExpiresAt == 0 {
		return nil
	}
	exp := h.generalSettings().Expiry
	thresholds, kind := exp.CertDays, KindCert
	if row.Kind == store.ExpiryDomain {
		thresholds, kind = exp.DomainDays, KindDomain
	}
	days := daysLeft(row.ExpiresAt, now)
	status, level := expiryAlert(thresholds, days, row.AlertedDays)
	row.AlertedDays = level
	if status == "" {
		return nil
	}
	return &Event{
		Kind: kind, MonitorID: m.ID, Monitor: m.Name, Target: m.Target, Status: status,
		At: now.UnixMilli(), Subject: row.Subject, ExpiresAt: row.ExpiresAt, DaysLeft: &days,
	}
}

// daysLeft is the number of whole days until expiresAt (negative once expired).
func daysLeft(expiresAt int64, now time.Time) int {
	d := time.UnixMilli(expiresAt).Sub(now)
	if d < 0 {
		return -int((-d + 24*time.Hour - 1) / (24 * time.Hour))
	}
	return int(d / (24 * time.Hour))
}

// expiryAlert decides whether to notify. thresholds are in days, largest
// first; alerted is the level of the last alert (-1 none, 0 expired).
// It returns the event status ("" for none) and the new alert level.
func expiryAlert(thresholds []int, days, alerted int) (string, int) {
	if len(thresholds) == 0 {
		return "", -1 // alerts turned off
	}
	level := -1
	if days < 0 {
		level = 0
	} else {
		for _, t := range thresholds {
			if days <= t {
				level = t // thresholds are sorted: keeps the smallest match
			}
		}
	}
	switch {
	case level == -1 && alerted != -1:
		return ExpiryRenewed, -1
	case level == -1:
		return "", -1
	case alerted == -1 || level < alerted:
		if level == 0 {
			return ExpiryExpired, 0
		}
		return ExpiryExpiring, level
	}
	// Same level, or renewed but still within a threshold: no news.
	return "", level
}

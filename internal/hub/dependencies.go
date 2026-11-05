package hub

import (
	"errors"
	"fmt"
	"time"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Monitor dependencies: a monitor can depend on others (store.Monitor.DependsOn),
// e.g. every site on a server depends on the ping of that server. While a
// monitor it depends on is down, a failing monitor is "blocked": it opens no
// incident and sends no alert, so one outage gives one alert. Its status
// stays what its checks say (down), so uptime and status pages are unchanged.
//
// Checks of different monitors do not run at the same time, so the alert of
// a monitor that fails first waits for a fresh check of the monitors it
// depends on, and the alert of a monitor still failing when its parent
// recovers waits for one fresh check of its own.

// maxDependencies is how many monitors one monitor can depend on.
const maxDependencies = 20

// depHoldMax caps how long a down alert waits for the checks above.
const depHoldMax = 10 * time.Minute

// depHold is a down alert waiting for fresh checks of the monitors in wait.
type depHold struct {
	wait  []int64
	since time.Time
	until time.Time
}

// checkDependencies validates the monitors m depends on: they must exist,
// and m cannot end up depending on itself. nil keeps the stored ones.
func (h *Hub) checkDependencies(m *store.Monitor) error {
	if m.DependsOn == nil {
		return nil
	}
	ids := cleanIDs(m.DependsOn)
	if len(ids) > maxDependencies {
		return fmt.Errorf("a monitor can depend on at most %d monitors", maxDependencies)
	}
	if len(ids) == 0 {
		m.DependsOn = []int64{}
		return nil
	}
	all, err := h.store.MonitorDependencies()
	if err != nil {
		return err
	}
	for _, p := range ids {
		if p == m.ID {
			return errors.New("a monitor cannot depend on itself")
		}
		if _, err := h.store.Monitor(p); err != nil {
			return fmt.Errorf("depends_on: monitor %d not found", p)
		}
	}
	if m.ID > 0 {
		all[m.ID] = ids
		if dependsOn(all, ids, m.ID) {
			return errors.New("depends_on: these dependencies form a loop")
		}
	}
	m.DependsOn = ids
	return nil
}

// dependsOn reports whether target is reachable from the monitors in from.
func dependsOn(deps map[int64][]int64, from []int64, target int64) bool {
	seen := map[int64]bool{}
	stack := append([]int64{}, from...)
	for len(stack) > 0 {
		id := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if id == target {
			return true
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		stack = append(stack, deps[id]...)
	}
	return false
}

// parentDownLocked gives the monitor blocking st: the first monitor it
// depends on that is down, or the one blocking that monitor in turn. 0 when
// none is down.
func (h *Hub) parentDownLocked(st *monState) int64 {
	for _, p := range st.m.DependsOn {
		ps := h.monitors[p]
		if ps == nil || ps.m.Paused || ps.real != StatusDown {
			continue
		}
		if ps.blockedBy != 0 {
			return ps.blockedBy
		}
		return p
	}
	return 0
}

// depsLocked updates whether st is blocked and reports whether its down
// alert must wait (blocked, or waiting for fresh checks). fresh is true when
// st just got a result. Call with h.mu held.
func (h *Hub) depsLocked(st *monState, now time.Time, fresh bool) bool {
	if len(st.m.DependsOn) == 0 || st.real != StatusDown {
		st.blockedBy, st.blockedSince, st.hold = 0, 0, nil
		return false
	}
	if b := h.parentDownLocked(st); b != 0 {
		if st.blockedBy == 0 {
			st.blockedSince = now.UnixMilli()
		}
		st.blockedBy, st.hold = b, nil
		return true
	}
	if st.incident {
		st.blockedBy, st.blockedSince, st.hold = 0, 0, nil
		return false
	}
	switch {
	case st.blockedBy != 0:
		// The parent is back: alert only if this monitor still fails at
		// its next check (this one, when it just answered).
		st.blockedBy, st.blockedSince = 0, 0
		if fresh {
			return false
		}
		st.hold = h.newHoldLocked([]int64{st.m.ID}, now)
	case st.hold == nil:
		// Failing first: give the parents a chance to fail too.
		st.hold = h.newHoldLocked(st.m.DependsOn, now)
	}
	if st.hold == nil || h.holdDoneLocked(st.hold, now) {
		st.hold = nil
		return false
	}
	return true
}

// newHoldLocked starts waiting for fresh checks of ids and moves them to
// now. Push monitors cannot be checked on demand: nobody waits for them.
// nil when there is nothing to wait for.
func (h *Hub) newHoldLocked(ids []int64, now time.Time) *depHold {
	hd := &depHold{since: now}
	var wait time.Duration
	for _, id := range ids {
		ws := h.monitors[id]
		if ws == nil || ws.m.Paused || ws.m.Type == check.TypePush {
			continue
		}
		hd.wait = append(hd.wait, id)
		ws.next = now
		// Retries: a parent needs that many failed checks to be down.
		need := time.Duration(max(ws.m.IntervalSec, 10)*max(ws.m.Retries-1, 0))*time.Second +
			time.Duration(ws.m.TimeoutMs)*time.Millisecond + 15*time.Second
		wait = max(wait, need)
	}
	if len(hd.wait) == 0 {
		return nil
	}
	hd.until = now.Add(min(wait, depHoldMax))
	return hd
}

// holdDoneLocked reports whether a held alert can go out: every monitor it
// waits for answered since, and those still failing are not about to be
// down, or the wait is over.
func (h *Hub) holdDoneLocked(hd *depHold, now time.Time) bool {
	if !now.Before(hd.until) {
		return true
	}
	for _, id := range hd.wait {
		ws := h.monitors[id]
		if ws == nil || ws.m.Paused {
			continue
		}
		if !ws.lastAt.After(hd.since) {
			return false
		}
		if ws.real == StatusDown {
			continue
		}
		for _, ls := range ws.locs {
			if ls.fails > 0 {
				return false // failing, not down yet: wait for its retries
			}
		}
	}
	return true
}

// depTick opens the incidents of monitors whose held alert can go out, and
// keeps blocked monitors up to date when the monitors they depend on change.
func (h *Hub) depTick(now time.Time) {
	type change struct {
		m     *store.Monitor
		alert string
		cause string
	}
	var changes []change
	h.mu.Lock()
	for _, st := range h.monitors {
		if st.m.Paused || len(st.m.DependsOn) == 0 || (st.real != StatusDown && st.blockedBy == 0 && st.hold == nil) {
			continue
		}
		alert := h.alertLocked(st, now, false)
		if alert == "" {
			continue
		}
		c := change{m: st.m, alert: alert}
		if alert == StatusDown {
			c.cause = h.causesLocked(st)
		}
		changes = append(changes, c)
	}
	h.mu.Unlock()
	at := now.UnixMilli()
	for _, c := range changes {
		h.raise(c.m, c.alert, c.cause, at)
	}
}

// blockedInfo is the monitor blocking another one, in the API.
type blockedInfo struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Since int64  `json:"since"`
}

// blockedBy gives the monitor blocking id, or nil.
func (h *Hub) blockedBy(id int64) *blockedInfo {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.monitors[id]
	if st == nil || st.blockedBy == 0 {
		return nil
	}
	b := &blockedInfo{ID: st.blockedBy, Since: st.blockedSince}
	if ps := h.monitors[st.blockedBy]; ps != nil {
		b.Name = ps.m.Name
	}
	return b
}

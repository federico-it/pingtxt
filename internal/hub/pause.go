package hub

import (
	"errors"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// maxPause is the longest timed pause.
const maxPause = 365 * 24 * time.Hour

// pauseLength is how long a pause lasts, in the body of
// /api/monitors/{id}/pause and of the bulk "pause" action: minutes from now
// or until a time (unix ms). Neither = until someone resumes the monitor.
type pauseLength struct {
	Minutes int   `json:"minutes"`
	Until   int64 `json:"until"`
}

// until returns when the pause ends (unix ms, 0 = never).
func (p pauseLength) until(now time.Time) (int64, error) {
	switch {
	case p.Minutes != 0 && p.Until != 0:
		return 0, errors.New("send minutes or until, not both")
	case p.Minutes < 0 || time.Duration(p.Minutes)*time.Minute > maxPause:
		return 0, errors.New("minutes must be between 1 and 525600 (365 days)")
	case p.Minutes > 0:
		return now.Add(time.Duration(p.Minutes) * time.Minute).UnixMilli(), nil
	case p.Until == 0:
		return 0, nil
	case p.Until <= now.UnixMilli():
		return 0, errors.New("until must be in the future")
	case p.Until > now.Add(maxPause).UnixMilli():
		return 0, errors.New("until must be within 365 days")
	}
	return p.Until, nil
}

// pauseTick resumes the monitors whose timed pause is over.
func (h *Hub) pauseTick(now time.Time) {
	ms := now.UnixMilli()
	var due []int64
	h.mu.Lock()
	for id, st := range h.monitors {
		if st.m.Paused && st.m.PausedUntil > 0 && st.m.PausedUntil <= ms {
			due = append(due, id)
		}
	}
	h.mu.Unlock()
	for _, id := range due {
		// Read it again: someone may have paused it again or resumed it.
		m, err := h.store.Monitor(id)
		if err != nil || !m.Paused || m.PausedUntil == 0 || m.PausedUntil > ms {
			if err == nil {
				h.MonitorChanged(id)
			}
			continue
		}
		if err := h.store.PauseMonitor(id, false, 0); err != nil {
			h.log.Error("resume paused monitor", "monitor", id, "err", err)
			continue
		}
		h.MonitorChanged(id)
		h.log.Info("monitor resumed: pause over", "monitor", id)
		h.AuditSystem(store.AuditSystem, "monitor.resume", "monitor", id, m.Name, map[string]any{"paused_until": m.PausedUntil})
	}
}

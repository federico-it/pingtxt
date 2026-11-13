package hub

import (
	"errors"
	"net/http"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Incident snooze. Snoozing an open incident ("not now, remind me in 30
// minutes") stops its reminders and its escalation to channels that wait
// before alerting until the snooze ends. Then, if the monitor is still
// down and nobody acknowledged the incident, every channel that got the
// alert hears it again (a STILL DOWN reminder, also on channels without
// reminders) and the escalation goes on. Ending the snooze early lets
// reminders and escalation run again at once, without the extra reminder.

const (
	minSnoozeMin = 5
	maxSnoozeMin = 24 * 60 // one day
)

var errSnoozeRange = errors.New("minutes must be between 5 and 1440")

// SnoozeIncident snoozes an open incident for u for some minutes, or ends
// the snooze (minutes 0), and returns the incident. Snoozing a snoozed
// incident again replaces the snooze.
func (h *Hub) SnoozeIncident(id int64, u *store.User, minutes int, now time.Time) (*store.Incident, error) {
	if minutes != 0 && (minutes < minSnoozeMin || minutes > maxSnoozeMin) {
		return nil, errSnoozeRange
	}
	inc, err := h.store.Incident(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errIncidentNotFound
	} else if err != nil {
		return nil, err
	}
	if inc.ResolvedAt > 0 {
		return nil, errIncidentResolved
	}
	var until int64
	email := ""
	if minutes > 0 {
		until, email = now.Add(time.Duration(minutes)*time.Minute).UnixMilli(), u.Email
	} else if inc.SnoozedUntil <= now.UnixMilli() {
		return inc, nil // not snoozed: nothing changes
	}
	ok, err := h.store.SnoozeIncident(id, until, email)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errIncidentResolved // resolved meanwhile
	}
	inc.SnoozedUntil, inc.SnoozedBy = until, email

	h.mu.Lock()
	if st := h.monitors[inc.MonitorID]; st != nil && st.alerts != nil && st.alerts.incident == id {
		st.alerts.snoozeUntil, st.alerts.snoozeBy = until, email
	}
	h.mu.Unlock()
	return inc, nil
}

// ---------- API ----------

func (h *Hub) snoozeIncident(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Minutes int `json:"minutes"`
	}
	if err := readJSON(r, &body); err != nil {
		writeErr(w, 400, "invalid json")
		return
	}
	if body.Minutes == 0 {
		writeErr(w, 400, errSnoozeRange.Error())
		return
	}
	h.snoozeIncidentAPI(w, r, u, body.Minutes)
}

func (h *Hub) unsnoozeIncident(w http.ResponseWriter, r *http.Request, u *store.User) {
	h.snoozeIncidentAPI(w, r, u, 0)
}

func (h *Hub) snoozeIncidentAPI(w http.ResponseWriter, r *http.Request, u *store.User, minutes int) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	inc, err := h.SnoozeIncident(id, u, minutes, time.Now())
	switch {
	case errors.Is(err, errSnoozeRange):
		writeErr(w, 400, err.Error())
		return
	case errors.Is(err, errIncidentNotFound):
		writeErr(w, 404, err.Error())
		return
	case errors.Is(err, errIncidentResolved):
		writeErr(w, 409, err.Error())
		return
	case err != nil:
		h.internalErr(w, err)
		return
	}
	h.markDiagnosed([]*store.Incident{inc})
	writeJSON(w, 200, inc)
}

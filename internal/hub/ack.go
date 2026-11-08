package hub

import (
	"errors"
	"net/http"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Incident acknowledgement. Someone who takes an open incident ("I'm on
// it") stops its reminders and its escalation to channels that wait before
// alerting; everyone sees who is on it. The channels that got the down
// alert, and picked the "ack" event, hear about it. Taking the
// acknowledgement back lets reminders and escalation run again. The
// recovery still goes to every channel that got the alert.

var (
	errIncidentNotFound = errors.New("incident not found")
	errIncidentResolved = errors.New("the incident is resolved")
)

// AckIncident acknowledges an open incident for u (ack true) or takes the
// acknowledgement back (ack false), and returns the incident. Acknowledging
// an incident someone else acknowledged takes it over.
func (h *Hub) AckIncident(id int64, u *store.User, ack bool) (*store.Incident, error) {
	inc, err := h.store.Incident(id)
	if errors.Is(err, store.ErrNotFound) {
		return nil, errIncidentNotFound
	} else if err != nil {
		return nil, err
	}
	if inc.ResolvedAt > 0 {
		return nil, errIncidentResolved
	}
	if ack == (inc.AckedAt > 0) && (!ack || inc.AckedByID == u.ID) {
		return inc, nil // nothing changes
	}
	now := time.Now().UnixMilli()
	var userID int64
	email := ""
	if ack {
		userID, email = u.ID, u.Email
	}
	ok, err := h.store.AckIncident(id, userID, email, now)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errIncidentResolved // resolved meanwhile
	}
	if !ack {
		now = 0
	}
	inc.AckedAt, inc.AckedBy, inc.AckedByID = now, email, userID

	// Follow it in memory, and tell the channels that got the alert.
	var told []int64
	var ev Event
	h.mu.Lock()
	st := h.monitors[inc.MonitorID]
	if st != nil && st.alerts != nil && st.alerts.incident == id {
		a := st.alerts
		a.ackAt, a.ackBy = now, email
		if ack {
			for nid := range a.sent {
				told = append(told, nid)
			}
			ev = Event{
				Kind: KindAck, MonitorID: st.m.ID, Monitor: st.m.Name, Target: eventTarget(st.m),
				Status: StatusAcked, Cause: a.cause, At: now, DownSince: a.started,
				IncidentID: id, AckBy: email,
			}
		}
	}
	h.mu.Unlock()
	if len(told) > 0 {
		h.sendAck(ev, inc.MonitorID, told)
	}
	h.LiveActivitiesChanged()
	return inc, nil
}

// sendAck delivers an acknowledgement to the channels (ids) that got the
// down alert and want the event.
func (h *Hub) sendAck(ev Event, monitorID int64, channels []int64) {
	list, rt, err := h.loadRouting()
	if err != nil {
		h.log.Error("list notifiers", "err", err)
		return
	}
	m, err := h.store.Monitor(monitorID)
	if err != nil {
		return
	}
	alerted := map[int64]bool{}
	for _, id := range channels {
		alerted[id] = true
	}
	for _, nt := range list {
		if alerted[nt.ID] && rt.wants(nt, m, store.EventAck) {
			h.notify.SendTo(nt, ev)
		}
	}
}

// openIncidentOf gives the open incident of a monitor as the dashboard
// shows it, or nil.
func (h *Hub) openIncidentOf(id int64) *openIncident {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := h.monitors[id]
	if st == nil || !st.incident || st.alerts == nil || st.alerts.incident == 0 {
		return nil
	}
	a := st.alerts
	return &openIncident{ID: a.incident, StartedAt: a.started, AckedAt: a.ackAt, AckedBy: a.ackBy}
}

// openIncident is the open incident of a monitor in its summary.
type openIncident struct {
	ID        int64  `json:"id"`
	StartedAt int64  `json:"started_at"`
	AckedAt   int64  `json:"acked_at"`
	AckedBy   string `json:"acked_by"`
}

// ---------- API ----------

func (h *Hub) ackIncident(w http.ResponseWriter, r *http.Request, u *store.User) {
	h.ackIncidentAPI(w, r, u, true)
}

func (h *Hub) unackIncident(w http.ResponseWriter, r *http.Request, u *store.User) {
	h.ackIncidentAPI(w, r, u, false)
}

func (h *Hub) ackIncidentAPI(w http.ResponseWriter, r *http.Request, u *store.User, ack bool) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	inc, err := h.AckIncident(id, u, ack)
	switch {
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

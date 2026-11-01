package hub

import (
	"errors"
	"slices"

	"github.com/federico-it/pingtxt/internal/store"
)

// Notification routing: which channels hear about which monitors, and for
// which events. A channel has a scope (every monitor, or the monitors of
// some groups and tags) and the events it gets for them. A monitor can be
// linked to a channel one by one with events of its own: the link wins over
// the scope, so it adds a monitor, changes its events, or (with no events)
// leaves it out.

// routing holds the one-by-one links, by monitor and channel id.
type routing map[int64]map[int64][]string

func newRouting(links []store.ChannelLink) routing {
	r := routing{}
	for _, l := range links {
		if r[l.MonitorID] == nil {
			r[l.MonitorID] = map[int64][]string{}
		}
		r[l.MonitorID][l.NotifierID] = l.Events
	}
	return r
}

// wants reports whether the channel is to be sent event (a store.Event*
// name) about the monitor.
func (r routing) wants(nt *store.Notifier, m *store.Monitor, event string) bool {
	if !nt.Enabled || m == nil {
		return false
	}
	if events, ok := r[m.ID][nt.ID]; ok {
		return slices.Contains(events, event)
	}
	return nt.Covers(m) && slices.Contains(nt.Events, event)
}

// loadRouting reads the channels and their links.
func (h *Hub) loadRouting() ([]*store.Notifier, routing, error) {
	list, err := h.store.ListNotifiers()
	if err != nil {
		return nil, nil, err
	}
	links, err := h.store.ListChannelLinks()
	if err != nil {
		return nil, nil, err
	}
	return list, newRouting(links), nil
}

// notifyEvent is the routing name of an event.
func (e Event) notifyEvent() string {
	switch e.Kind {
	case KindCert:
		return store.EventCertExpiry
	case KindDomain:
		return store.EventDomainExpiry
	case KindDegraded:
		return store.EventDegraded
	}
	if e.Status == StatusDown {
		return store.EventDown
	}
	return store.EventUp
}

// sendEvent delivers the event to every channel that wants it for at least
// one of the monitors (several monitors can share a certificate or domain).
func (h *Hub) sendEvent(e Event, monitors ...*store.Monitor) {
	list, rt, err := h.loadRouting()
	if err != nil {
		h.log.Error("list notifiers", "err", err)
		return
	}
	name := e.notifyEvent()
	for _, nt := range list {
		if slices.ContainsFunc(monitors, func(m *store.Monitor) bool { return rt.wants(nt, m, name) }) {
			h.notify.SendTo(nt, e)
		}
	}
}

// ---------- API ----------

// notifierBody is a channel as the API receives it. Pointers tell a field
// left out (create: defaults, update: unchanged) from an empty one.
type notifierBody struct {
	store.Notifier
	AllMonitors *bool                `json:"all_monitors"`
	GroupIDs    *[]int64             `json:"group_ids"`
	TagIDs      *[]int64             `json:"tag_ids"`
	Events      *[]string            `json:"events"`
	Monitors    *[]store.ChannelLink `json:"monitors"`
}

// apply fills the scope of n from the body: from cur (update) or the
// defaults (create, cur nil) for the fields left out. A new channel covers
// every monitor with the default events, as channels always did.
func (b *notifierBody) apply(cur *store.Notifier) *store.Notifier {
	n := b.Notifier
	n.AllMonitors, n.GroupIDs, n.TagIDs, n.Events = true, []int64{}, []int64{}, slices.Clone(store.DefaultNotifyEvents)
	if cur != nil {
		n.AllMonitors, n.GroupIDs, n.TagIDs, n.Events = cur.AllMonitors, cur.GroupIDs, cur.TagIDs, cur.Events
	}
	if b.AllMonitors != nil {
		n.AllMonitors = *b.AllMonitors
	}
	if b.GroupIDs != nil {
		n.GroupIDs = *b.GroupIDs
	}
	if b.TagIDs != nil {
		n.TagIDs = *b.TagIDs
	}
	if b.Events != nil {
		n.Events = *b.Events
	}
	n.Monitors = nil
	if b.Monitors != nil {
		n.Monitors = *b.Monitors
		if n.Monitors == nil {
			n.Monitors = []store.ChannelLink{}
		}
	}
	return &n
}

// validateScope cleans the scope and links of a channel.
func (h *Hub) validateScope(n *store.Notifier) error {
	n.Events = store.CleanEvents(n.Events)
	n.GroupIDs, n.TagIDs = cleanIDs(n.GroupIDs), cleanIDs(n.TagIDs)
	if n.AllMonitors {
		n.GroupIDs, n.TagIDs = []int64{}, []int64{}
	}
	if err := h.checkGroupsTags(n.GroupIDs, n.TagIDs); err != nil {
		return err
	}
	if n.Monitors == nil {
		return nil
	}
	ms, err := h.store.ListMonitors()
	if err != nil {
		return err
	}
	out := []store.ChannelLink{}
	for _, l := range n.Monitors {
		if !slices.ContainsFunc(ms, func(m *store.Monitor) bool { return m.ID == l.MonitorID }) {
			return errors.New("unknown monitor")
		}
		if slices.ContainsFunc(out, func(o store.ChannelLink) bool { return o.MonitorID == l.MonitorID }) {
			continue
		}
		out = append(out, store.ChannelLink{MonitorID: l.MonitorID, Events: store.CleanEvents(l.Events)})
	}
	n.Monitors = out
	return nil
}

// validateMonitorLinks cleans the channel links of a monitor (nil = keep).
func (h *Hub) validateMonitorLinks(m *store.Monitor) error {
	if m.Notifiers == nil {
		return nil
	}
	list, err := h.store.ListNotifiers()
	if err != nil {
		return err
	}
	out := []store.ChannelLink{}
	for _, l := range m.Notifiers {
		if !slices.ContainsFunc(list, func(n *store.Notifier) bool { return n.ID == l.NotifierID }) {
			return errors.New("unknown notification channel")
		}
		if slices.ContainsFunc(out, func(o store.ChannelLink) bool { return o.NotifierID == l.NotifierID }) {
			continue
		}
		out = append(out, store.ChannelLink{NotifierID: l.NotifierID, Events: store.CleanEvents(l.Events)})
	}
	m.Notifiers = out
	return nil
}

// withMonitors fills the one-by-one links of each channel.
func (h *Hub) withMonitors(list ...*store.Notifier) error {
	links, err := h.store.ListChannelLinks()
	if err != nil {
		return err
	}
	for _, n := range list {
		n.Monitors = []store.ChannelLink{}
		for _, l := range links {
			if l.NotifierID == n.ID {
				n.Monitors = append(n.Monitors, store.ChannelLink{MonitorID: l.MonitorID, Events: l.Events})
			}
		}
	}
	return nil
}

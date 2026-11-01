package store

import (
	"encoding/json"
	"slices"
)

// Notification events a channel can be sent for a monitor.
const (
	EventDown         = "down"          // incident opened, and its reminders
	EventUp           = "up"            // incident resolved
	EventDegraded     = "degraded"      // some locations fail, and when that ends
	EventCertExpiry   = "cert_expiry"   // TLS certificate expiring, expired or renewed
	EventDomainExpiry = "domain_expiry" // domain registration expiring, expired or renewed
)

// NotifyEvents lists every event, in display order.
var NotifyEvents = []string{EventDown, EventUp, EventDegraded, EventCertExpiry, EventDomainExpiry}

// DefaultNotifyEvents are the events a channel gets unless told otherwise:
// everything that was sent before channels could be picked per monitor.
var DefaultNotifyEvents = []string{EventDown, EventUp, EventCertExpiry, EventDomainExpiry}

// ChannelLink ties a notification channel to a monitor with the events the
// channel gets for it. It overrides the channel's own scope: a link with no
// events keeps a monitor out of a channel that covers every monitor.
// Monitors list their links by NotifierID, channels by MonitorID.
type ChannelLink struct {
	MonitorID  int64    `json:"monitor_id,omitempty"`
	NotifierID int64    `json:"notifier_id,omitempty"`
	Events     []string `json:"events"`
}

// CleanEvents keeps the known events of list, once each, in display order.
func CleanEvents(list []string) []string {
	out := []string{}
	for _, e := range NotifyEvents {
		if slices.Contains(list, e) {
			out = append(out, e)
		}
	}
	return out
}

// Covers reports whether the channel's scope includes the monitor: every
// monitor, or the monitors of its groups and tags.
func (n *Notifier) Covers(m *Monitor) bool {
	if n.AllMonitors {
		return true
	}
	if m.GroupID != 0 && slices.Contains(n.GroupIDs, m.GroupID) {
		return true
	}
	for _, t := range m.TagIDs {
		if slices.Contains(n.TagIDs, t) {
			return true
		}
	}
	return false
}

// ListChannelLinks returns every link between a monitor and a channel.
func (s *Store) ListChannelLinks() ([]ChannelLink, error) {
	return s.channelLinks(`SELECT monitor_id, notifier_id, events FROM monitor_notifiers ORDER BY monitor_id, notifier_id`)
}

func (s *Store) channelLinks(q string, args ...any) ([]ChannelLink, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ChannelLink{}
	for rows.Next() {
		var l ChannelLink
		var ev string
		if err := rows.Scan(&l.MonitorID, &l.NotifierID, &ev); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(ev), &l.Events)
		l.Events = CleanEvents(l.Events)
		out = append(out, l)
	}
	return out, rows.Err()
}

// monitorLinks returns the links of one monitor, or of all of them (id 0),
// by monitor id, each with only its NotifierID set.
func (s *Store) monitorLinks(id int64) (map[int64][]ChannelLink, error) {
	var list []ChannelLink
	var err error
	if id > 0 {
		list, err = s.channelLinks(`SELECT monitor_id, notifier_id, events FROM monitor_notifiers WHERE monitor_id = ? ORDER BY notifier_id`, id)
	} else {
		list, err = s.ListChannelLinks()
	}
	if err != nil {
		return nil, err
	}
	out := map[int64][]ChannelLink{}
	for _, l := range list {
		out[l.MonitorID] = append(out[l.MonitorID], ChannelLink{NotifierID: l.NotifierID, Events: l.Events})
	}
	return out, nil
}

// setMonitorLinks replaces the channel links of a monitor.
func setMonitorLinks(tx *tx, monitorID int64, links []ChannelLink) error {
	if _, err := tx.Exec(`DELETE FROM monitor_notifiers WHERE monitor_id = ?`, monitorID); err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO monitor_notifiers (monitor_id, notifier_id, events) VALUES (?, ?, ?)`, monitorID, l.NotifierID, jsonIDs(CleanEvents(l.Events))); err != nil {
			return err
		}
	}
	return nil
}

// SetChannelMonitors replaces the monitor links of a channel.
func (s *Store) SetChannelMonitors(notifierID int64, links []ChannelLink) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM monitor_notifiers WHERE notifier_id = ?`, notifierID); err != nil {
		return err
	}
	for _, l := range links {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO monitor_notifiers (monitor_id, notifier_id, events) VALUES (?, ?, ?)`, l.MonitorID, notifierID, jsonIDs(CleanEvents(l.Events))); err != nil {
			return err
		}
	}
	return tx.Commit()
}

package store

import (
	"strings"
	"testing"
)

// Channels stored without a scope (as before migration 26) cover every
// monitor with the events they always got.
func TestNotifierScopeDefaults(t *testing.T) {
	s := openTest(t)
	if _, err := s.db.Exec(`INSERT INTO notifiers (name, type, config, enabled, created_at) VALUES ('old', 'webhook', '{}', 1, 1)`); err != nil {
		t.Fatal(err)
	}
	list, err := s.ListNotifiers()
	if err != nil {
		t.Fatal(err)
	}
	n := list[0]
	if !n.AllMonitors || strings.Join(n.Events, ",") != "down,up,cert_expiry,domain_expiry" || len(n.GroupIDs) != 0 {
		t.Fatalf("defaults: %+v", n)
	}
	m := &Monitor{Name: "m", Type: "http", Target: "http://x", Notifiers: []ChannelLink{{NotifierID: n.ID, Events: []string{"up", "down", "x"}}}}
	if err := s.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	got, err := s.Monitor(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Notifiers) != 1 || strings.Join(got.Notifiers[0].Events, ",") != "down,up" {
		t.Fatalf("links: %+v", got.Notifiers)
	}
	// Deleting the channel removes its links.
	if err := s.DeleteNotifier(n.ID); err != nil {
		t.Fatal(err)
	}
	if l, _ := s.ListChannelLinks(); len(l) != 0 {
		t.Fatalf("links after delete: %+v", l)
	}
}

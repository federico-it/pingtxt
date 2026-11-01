package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestRoutingWants(t *testing.T) {
	a := &store.Monitor{ID: 1, GroupID: 7}
	b := &store.Monitor{ID: 2, TagIDs: []int64{3}}
	c := &store.Monitor{ID: 3}
	all := &store.Notifier{ID: 10, Enabled: true, AllMonitors: true, Events: store.DefaultNotifyEvents}
	grp := &store.Notifier{ID: 11, Enabled: true, GroupIDs: []int64{7}, Events: []string{store.EventDown}}
	tag := &store.Notifier{ID: 12, Enabled: true, TagIDs: []int64{3}, Events: []string{store.EventDegraded}}
	none := &store.Notifier{ID: 13, Enabled: true, Events: store.DefaultNotifyEvents}
	off := &store.Notifier{ID: 14, AllMonitors: true, Events: store.DefaultNotifyEvents}
	rt := newRouting([]store.ChannelLink{
		{MonitorID: 1, NotifierID: 10},                                  // a left out of "all"
		{MonitorID: 3, NotifierID: 13, Events: []string{store.EventUp}}, // c added to "none", recoveries only
		{MonitorID: 3, NotifierID: 14, Events: []string{store.EventDown}},
	})
	cases := []struct {
		nt    *store.Notifier
		m     *store.Monitor
		event string
		want  bool
	}{
		{all, a, store.EventDown, false},
		{all, b, store.EventDown, true},
		{all, b, store.EventDegraded, false}, // not in the default events
		{all, c, store.EventCertExpiry, true},
		{grp, a, store.EventDown, true},
		{grp, a, store.EventUp, false},
		{grp, b, store.EventDown, false},
		{tag, b, store.EventDegraded, true},
		{tag, a, store.EventDegraded, false},
		{none, a, store.EventDown, false},
		{none, c, store.EventUp, true},
		{none, c, store.EventDown, false},
		{off, c, store.EventDown, false}, // disabled channels get nothing
	}
	for i, tc := range cases {
		if got := rt.wants(tc.nt, tc.m, tc.event); got != tc.want {
			t.Errorf("case %d: channel %d monitor %d %s: got %v", i, tc.nt.ID, tc.m.ID, tc.event, got)
		}
	}
}

func TestDegradedChange(t *testing.T) {
	for _, tc := range []struct{ prev, cur, want string }{
		{StatusUp, StatusDegraded, StatusDegraded},
		{StatusDown, StatusDegraded, StatusDegraded},
		{StatusDegraded, StatusUp, StatusUp},
		{StatusDegraded, StatusDown, ""}, // the down alert says it
		{"pending", StatusDegraded, ""},
		{StatusMaintenance, StatusUp, ""},
		{StatusDegraded, StatusMaintenance, ""},
	} {
		if got := degradedChange(tc.prev, tc.cur); got != tc.want {
			t.Errorf("%s -> %s: got %q, want %q", tc.prev, tc.cur, got, tc.want)
		}
	}
}

// TestChannelRouting sends incidents of two monitors to channels with
// different scopes and links.
func TestChannelRouting(t *testing.T) {
	h, st := newTestHub(t)
	sink := newAlertSink(t)
	g := &store.Group{Name: "web"}
	must(t, st.CreateGroup(g))
	a := &store.Monitor{Name: "a", Type: "http", Target: "http://a", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}, GroupID: g.ID}
	b := &store.Monitor{Name: "b", Type: "http", Target: "http://b", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	must(t, st.CreateMonitor(a))
	must(t, st.CreateMonitor(b))
	ch := func(name string, all bool, groups []int64, events []string) *store.Notifier {
		n := &store.Notifier{Name: name, Type: "webhook", Enabled: true, AllMonitors: all, GroupIDs: groups, Events: events,
			Config: map[string]string{"url": sink.srv.URL + "/?ch=" + name}}
		must(t, st.CreateNotifier(n))
		return n
	}
	ch("all", true, nil, store.DefaultNotifyEvents)
	ch("grp", false, []int64{g.ID}, []string{store.EventDown})
	linked := ch("linked", false, nil, store.DefaultNotifyEvents)
	excl := ch("excl", true, nil, store.DefaultNotifyEvents)
	deg := ch("deg", true, nil, []string{store.EventDegraded})
	must(t, st.SetChannelMonitors(linked.ID, []store.ChannelLink{{MonitorID: b.ID, Events: []string{store.EventDown, store.EventUp}}}))
	must(t, st.SetChannelMonitors(excl.ID, []store.ChannelLink{{MonitorID: a.ID}}))
	_ = deg
	h.MonitorChanged(a.ID)
	h.MonitorChanged(b.ID)

	h.HandleResult(LocalLocation, result(a.ID, false))
	if got := summary(sink.wait(t, 2)); got != "all:down grp:down" {
		t.Fatalf("a down: %s", got)
	}
	h.HandleResult(LocalLocation, result(b.ID, false))
	if got := summary(sink.wait(t, 5)); got != "all:down all:down excl:down grp:down linked:down" {
		t.Fatalf("b down: %s", got)
	}
	h.HandleResult(LocalLocation, result(a.ID, true))
	h.HandleResult(LocalLocation, result(b.ID, true))
	// Recoveries go to the channels that got the alert and want them.
	if got := summary(sink.wait(t, 9)); got != "all:down all:down all:up all:up excl:down excl:up grp:down linked:down linked:up" {
		t.Fatalf("recoveries: %s", got)
	}

	// Degraded events only reach the channels that asked for them.
	h.sendEvent(Event{Kind: KindDegraded, MonitorID: a.ID, Monitor: "a", Target: "http://a", Status: StatusDegraded}, a)
	got := sink.wait(t, 10)
	if len(got["deg"]) != 1 || got["deg"][0].Kind != KindDegraded {
		t.Fatalf("degraded: %s", summary(got))
	}
}

func TestChannelLinksAPI(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
	cookie := login(t, h, st)
	do := func(method, path, body string, out any) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if out != nil {
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s %s: %s", method, path, b)
			}
		}
		return res.StatusCode
	}

	var m1, m2 store.Monitor
	do("POST", "/api/monitors", `{"name":"a","type":"http","target":"http://a"}`, &m1)
	do("POST", "/api/monitors", `{"name":"b","type":"http","target":"http://b"}`, &m2)

	// A channel created without a scope covers every monitor with the default events.
	var n store.Notifier
	if c := do("POST", "/api/notifiers", `{"name":"hook","type":"webhook","enabled":true,"config":{"url":"https://example.com"}}`, &n); c != 201 {
		t.Fatalf("create: %d", c)
	}
	if !n.AllMonitors || strings.Join(n.Events, ",") != "down,up,cert_expiry,domain_expiry" || n.Monitors == nil {
		t.Fatalf("defaults: %+v", n)
	}

	// Scope and links from the channel side; unknown events are dropped.
	body := `{"name":"hook","type":"webhook","enabled":true,"config":{"url":"https://example.com"},"all_monitors":false,"events":["down","nope","degraded"],
		"monitors":[{"monitor_id":` + itoa(m1.ID) + `,"events":["up"]},{"monitor_id":` + itoa(m1.ID) + `,"events":[]}]}`
	if c := do("PUT", "/api/notifiers/"+itoa(n.ID), body, &n); c != 200 {
		t.Fatalf("update: %d", c)
	}
	if n.AllMonitors || strings.Join(n.Events, ",") != "down,degraded" || len(n.Monitors) != 1 || n.Monitors[0].MonitorID != m1.ID {
		t.Fatalf("scope: %+v", n)
	}
	if c := do("PUT", "/api/notifiers/"+itoa(n.ID), `{"name":"hook","type":"webhook","config":{"url":"https://x"},"monitors":[{"monitor_id":999}]}`, nil); c != 400 {
		t.Fatalf("unknown monitor: want 400, got %d", c)
	}

	// The same link seen from the monitor.
	var got store.Monitor
	do("GET", "/api/monitors/"+itoa(m1.ID), "", &got)
	if len(got.Notifiers) != 1 || got.Notifiers[0].NotifierID != n.ID || strings.Join(got.Notifiers[0].Events, ",") != "up" {
		t.Fatalf("monitor links: %+v", got.Notifiers)
	}
	// Saving a monitor without notifiers keeps them; an empty list removes them.
	do("PUT", "/api/monitors/"+itoa(m1.ID), `{"name":"a2","type":"http","target":"http://a"}`, &got)
	if len(got.Notifiers) != 1 {
		t.Fatalf("kept: %+v", got.Notifiers)
	}
	do("PUT", "/api/monitors/"+itoa(m2.ID), `{"name":"b","type":"http","target":"http://b","notifiers":[{"notifier_id":`+itoa(n.ID)+`,"events":["down","up"]}]}`, &got)
	do("PUT", "/api/monitors/"+itoa(m1.ID), `{"name":"a","type":"http","target":"http://a","notifiers":[]}`, &got)
	var list []store.Notifier
	do("GET", "/api/notifiers", "", &list)
	if len(list) != 1 || len(list[0].Monitors) != 1 || list[0].Monitors[0].MonitorID != m2.ID {
		t.Fatalf("channel links: %+v", list)
	}
	if c := do("PUT", "/api/monitors/"+itoa(m1.ID), `{"name":"a","type":"http","target":"http://a","notifiers":[{"notifier_id":999}]}`, nil); c != 400 {
		t.Fatalf("unknown channel: want 400, got %d", c)
	}

	// Saving the channel without monitors keeps its links and scope.
	do("PUT", "/api/notifiers/"+itoa(n.ID), `{"name":"hook2","type":"webhook","enabled":true,"config":{"url":"https://example.com"}}`, &n)
	if n.AllMonitors || len(n.Monitors) != 1 || strings.Join(n.Events, ",") != "down,degraded" {
		t.Fatalf("kept on channel save: %+v", n)
	}
	// Deleting the monitor removes its links.
	do("DELETE", "/api/monitors/"+itoa(m2.ID), "", nil)
	do("GET", "/api/notifiers", "", &list)
	if len(list[0].Monitors) != 0 {
		t.Fatalf("links after delete: %+v", list[0].Monitors)
	}
}

func TestChannelLinksExportImport(t *testing.T) {
	h, st := newTestHub(t)
	g := &store.Group{Name: "web"}
	must(t, st.CreateGroup(g))
	scoped := &store.Notifier{Name: "scoped", Type: "webhook", Enabled: true, GroupIDs: []int64{g.ID}, Events: []string{store.EventDown},
		Config: map[string]string{"url": "https://example.com/a"}}
	must(t, st.CreateNotifier(scoped))
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://a", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation},
		Notifiers: []store.ChannelLink{{NotifierID: scoped.ID, Events: []string{store.EventUp}}}}
	must(t, st.CreateMonitor(m))
	doc, err := h.ExportConfig(true)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(doc)

	h2, st2 := newTestHub(t)
	var in ConfigDoc
	must(t, json.Unmarshal(b, &in))
	if _, err := h2.ImportConfig(&in, ImportOptions{Replace: true}); err != nil {
		t.Fatal(err)
	}
	ns, _ := st2.ListNotifiers()
	ms, _ := st2.ListMonitors()
	gs, _ := st2.ListGroups()
	if len(ns) != 1 || ns[0].AllMonitors || len(ns[0].GroupIDs) != 1 || ns[0].GroupIDs[0] != gs[0].ID || strings.Join(ns[0].Events, ",") != "down" {
		t.Fatalf("imported channel: %+v", ns)
	}
	if len(ms) != 1 || len(ms[0].Notifiers) != 1 || ms[0].Notifiers[0].NotifierID != ns[0].ID || strings.Join(ms[0].Notifiers[0].Events, ",") != "up" {
		t.Fatalf("imported links: %+v", ms[0].Notifiers)
	}

	// Files from before channel scopes: every monitor, default events.
	old := `{"format":"pingtxt-config","version":1,"notifiers":[{"id":5,"name":"old","type":"webhook","enabled":true,"config":{"url":"https://example.com"}}]}`
	var od ConfigDoc
	must(t, json.Unmarshal([]byte(old), &od))
	od.Format, od.Version = configFormat, configVersion
	if _, err := h2.ImportConfig(&od, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	ns, _ = st2.ListNotifiers()
	for _, n := range ns {
		if n.Name == "old" && (!n.AllMonitors || strings.Join(n.Events, ",") != "down,up,cert_expiry,domain_expiry") {
			t.Fatalf("old file: %+v", n)
		}
	}
}

package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"

	"github.com/federico-it/pingtxt/internal/protocol"
	"github.com/federico-it/pingtxt/internal/store"
)

// offlineAgent creates an agent that disconnected at since.
func offlineAgent(t *testing.T, st *store.Store, name string, since int64) *store.Agent {
	t.Helper()
	a, err := st.CreateAgent(name, "", "hash-"+name)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAgentOffline(a.ID, since, store.AgentAlertNone); err != nil {
		t.Fatal(err)
	}
	a, _ = st.Agent(a.ID)
	return a
}

func TestAgentOfflineAlert(t *testing.T) {
	h, st := newTestHub(t)
	sink := newAlertSink(t)
	sink.channel(t, st, "all", 0, 0)
	// A channel without the event gets nothing.
	quiet := &store.Notifier{Name: "quiet", Type: "webhook", Enabled: true, AllMonitors: true, Events: []string{store.EventDown},
		Config: map[string]string{"url": sink.srv.URL + "/?ch=quiet"}}
	if err := st.CreateNotifier(quiet); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.started = now.Add(-time.Hour).UnixMilli()
	a := offlineAgent(t, st, "milano", now.Add(-2*time.Minute).UnixMilli())

	// Within the grace period: nothing.
	h.checkAgentsOffline(now)
	// Past it: one alert, and only one.
	h.checkAgentsOffline(now.Add(4 * time.Minute))
	h.checkAgentsOffline(now.Add(5 * time.Minute))
	got := sink.wait(t, 1)
	if len(got["all"]) != 1 || len(got["quiet"]) != 0 {
		t.Fatalf("got %v", summary(got))
	}
	e := got["all"][0]
	if e.Kind != KindAgent || e.Status != AgentOffline || e.AgentID != a.ID || e.Agent != "milano" || e.DownSince != a.OfflineSince {
		t.Fatalf("event: %+v", e)
	}
	if !e.Alert() || !strings.Contains(e.Text(), "[AGENT OFFLINE] milano") {
		t.Fatalf("text: %q", e.Text())
	}
	a, _ = st.Agent(a.ID)
	if a.OfflineAlerted != store.AgentAlertSent {
		t.Fatalf("alerted: %d", a.OfflineAlerted)
	}

	// Back online: "online" with how long it was offline, state cleared.
	sink.reset()
	h.agentConnectedAlert(a)
	got = sink.wait(t, 1)
	if e := got["all"][0]; e.Status != AgentOnline || !strings.Contains(e.Text(), "back online after") {
		t.Fatalf("online: %+v %q", e, e.Text())
	}
	a, _ = st.Agent(a.ID)
	if a.OfflineSince != 0 || a.OfflineAlerted != store.AgentAlertNone {
		t.Fatalf("state not cleared: %+v", a)
	}
}

func TestAgentOfflineNoAlert(t *testing.T) {
	h, st := newTestHub(t)
	sink := newAlertSink(t)
	sink.channel(t, st, "all", 0, 0)
	now := time.Now()
	// The hub just started: agents disconnected before count from now on.
	h.started = now.Add(-time.Minute).UnixMilli()
	old := now.Add(-30 * time.Minute).UnixMilli()

	// Alert turned off.
	off := offlineAgent(t, st, "off", old)
	must(t, st.SetAgentAlertOffline(off.ID, false))
	// Kicked by an admin, or offline from before the upgrade.
	muted := offlineAgent(t, st, "muted", old)
	must(t, st.SetAgentAlerted(muted.ID, store.AgentAlertMuted))
	// Never connected.
	if _, err := st.CreateAgent("new", "", "hash-new"); err != nil {
		t.Fatal(err)
	}
	// Just sent an update: 10 minutes of grace.
	upd := offlineAgent(t, st, "upd", old)
	h.updates.state = map[int64]*agentUpdate{upd.ID: {target: "v9", started: now}}
	// Every monitor it checks is in maintenance.
	maint := offlineAgent(t, st, "maint", old)
	m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{AgentLocation(maint.ID)}}
	must(t, st.CreateMonitor(m))
	h.MonitorChanged(m.ID)
	h.monitors[m.ID].inMaint = true
	offlineAgent(t, st, "plain", old)

	agents := func(got map[string][]Event) string {
		var out []string
		for _, e := range got["all"] {
			out = append(out, e.Agent)
		}
		return strings.Join(out, ",")
	}
	h.checkAgentsOffline(now)
	if got := sink.wait(t, 0); len(got["all"]) != 0 {
		t.Fatalf("right after the start: %s", agents(got))
	}
	h.checkAgentsOffline(now.Add(4*time.Minute + time.Second))
	if got := sink.wait(t, 1); agents(got) != "plain" {
		t.Fatalf("after the grace period: %s", agents(got))
	}
	sink.reset()
	h.checkAgentsOffline(now.Add(9*time.Minute + time.Second))
	if got := sink.wait(t, 1); agents(got) != "upd" {
		t.Fatalf("after the update grace period: %s", agents(got))
	}
	// The maintenance ends: its alert goes out.
	sink.reset()
	h.monitors[m.ID].inMaint = false
	h.checkAgentsOffline(now.Add(10 * time.Minute))
	if got := sink.wait(t, 1); agents(got) != "maint" {
		t.Fatalf("after the maintenance: %s", agents(got))
	}
}

func TestMonitorOnOfflineAgentsTurnsPending(t *testing.T) {
	h, st := newTestHub(t)
	now := time.Now()
	h.started = now.Add(-time.Hour).UnixMilli()
	a := offlineAgent(t, st, "a", now.Add(-10*time.Minute).UnixMilli())
	b, _ := st.CreateAgent("b", "", "hash-b")
	h.agents[b.ID] = &agentConn{agent: b, done: make(chan struct{})}

	only := &store.Monitor{Name: "only", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{AgentLocation(a.ID)}}
	mixed := &store.Monitor{Name: "mixed", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{AgentLocation(a.ID), AgentLocation(b.ID)}}
	local := &store.Monitor{Name: "local", Type: "http", Target: "http://x", IntervalSec: 60}
	for _, m := range []*store.Monitor{only, mixed, local} {
		must(t, st.CreateMonitor(m))
		must(t, st.SetMonitorStatus(m.ID, StatusUp, now.UnixMilli()))
		h.MonitorChanged(m.ID)
	}
	h.checkAgentsOffline(now)
	if s := status(t, st, only.ID); s != StatusPending {
		t.Fatalf("only on the offline agent: want pending, got %s", s)
	}
	for _, m := range []*store.Monitor{mixed, local} {
		if s := status(t, st, m.ID); s != StatusUp {
			t.Fatalf("%s: want up, got %s", m.Name, s)
		}
	}
	// The agent comes back: the next result sets the status again.
	h.agents[a.ID] = &agentConn{agent: a, done: make(chan struct{})}
	h.HandleResult(AgentLocation(a.ID), result(only.ID, true))
	if s := status(t, st, only.ID); s != StatusUp {
		t.Fatalf("back: want up, got %s", s)
	}
}

// TestAgentDisconnectRecordsOffline runs a real agent connection.
func TestAgentDisconnectRecordsOffline(t *testing.T) {
	h, st := newTestHub(t)
	token, hash := NewToken("pt_")
	a, err := st.CreateAgent("ws", "", hash)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()

	connect := func() *websocket.Conn {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/agent/ws", &websocket.DialOptions{HTTPHeader: http.Header{"Authorization": {"Bearer " + token}}})
		if err != nil {
			t.Fatal(err)
		}
		must(t, wsjson.Write(ctx, c, protocol.Message{Type: protocol.TypeHello, Hello: &protocol.Hello{Version: "dev", Hostname: "box"}}))
		var welcome protocol.Message
		must(t, wsjson.Read(ctx, c, &welcome))
		return c
	}
	waitFor := func(what string, ok func(*store.Agent) bool) {
		t.Helper()
		for range 200 {
			if cur, _ := st.Agent(a.ID); ok(cur) {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	c := connect()
	waitFor("online", func(a *store.Agent) bool { return h.AgentOnline(a.ID) })
	c.Close(websocket.StatusNormalClosure, "")
	waitFor("offline_since", func(a *store.Agent) bool { return a.OfflineSince > 0 && a.OfflineAlerted == store.AgentAlertNone })

	// Reconnecting clears it; an admin kick mutes the alert.
	c = connect()
	waitFor("cleared", func(a *store.Agent) bool { return a.OfflineSince == 0 && h.AgentOnline(a.ID) })
	h.DisconnectAgent(a.ID)
	waitFor("muted", func(a *store.Agent) bool { return a.OfflineSince > 0 && a.OfflineAlerted == store.AgentAlertMuted })
	c.CloseNow()
}

func TestAgentEventChannels(t *testing.T) {
	e := Event{Kind: KindAgent, AgentID: 3, Agent: "milano · aruba", Target: "box", Status: AgentOffline, At: 1_000_000, DownSince: 400_000, Cause: "last connected from 1.2.3.4"}
	if e.notifyEvent() != store.EventAgentOffline {
		t.Fatal(e.notifyEvent())
	}
	if s := emailSubject(e); s != "[pingtxt] agent milano · aruba is offline" {
		t.Fatal(s)
	}
	v := eventMailView(e, "https://hub.example")
	if v.Badge != "AGENT OFFLINE" || v.Action == nil || v.Action.Href != "https://hub.example/agents/" {
		t.Fatalf("mail: %+v", v)
	}
	p := appMessage(e)
	if p.Priority != "high" || p.Title != "Agent milano · aruba is offline" || p.Data["agent_id"] != "3" || p.CollapseID != "agent-3" {
		t.Fatalf("push: %+v", p)
	}
	e.Status = AgentOnline
	if p := appMessage(e); p.Priority != "normal" || !strings.Contains(p.Body, "After 10m offline") {
		t.Fatalf("push online: %+v", p)
	}
	if e.Alert() {
		t.Fatal("online is not an alert")
	}
}

func TestMonitorLinksDropAgentEvent(t *testing.T) {
	if got := store.CleanLinkEvents([]string{"agent_offline", "down"}); len(got) != 1 || got[0] != "down" {
		t.Fatal(got)
	}
}

func (s *alertSink) reset() {
	s.mu.Lock()
	s.got = map[string][]Event{}
	s.mu.Unlock()
}

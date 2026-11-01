package hub

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestLiveActivity(t *testing.T) {
	h, st := newTestHub(t)
	relay := newFakeRelay(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	signInApp(t, st, "admin@b.co", "pma_admin")
	signInApp(t, st, "v@b.co", "pma_viewer")
	decode[pushSettingsView](t, admin.do("PUT", "/api/settings/push", map[string]any{"enabled": true, "relay_url": relay.srv.URL}))
	cfg := decode[map[string]any](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/config", nil))
	if cfg["live_activity"] != true {
		t.Fatalf("config: %+v", cfg)
	}

	tokA, tokV, startA, startV := strings.Repeat("a1", 32), strings.Repeat("b2", 32), strings.Repeat("c3", 32), strings.Repeat("d4", 32)
	// Tokens need a phone registered first, and the app's OAuth sign-in.
	if res := bearerDo(t, srv.URL, "pma_admin", "PUT", "/api/mobile/live-activity/token", map[string]string{"token": tokA}); res.StatusCode != 409 {
		t.Fatalf("token before the phone: %d", res.StatusCode)
	}
	if res := admin.do("PUT", "/api/mobile/live-activity/token", map[string]string{"token": tokA}); res.StatusCode != 403 {
		t.Fatalf("token with a session: %d", res.StatusCode)
	}
	decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_admin", "POST", "/api/mobile/devices", map[string]string{"device": "prd_admin"}))
	decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/mobile/devices", map[string]string{"device": "prd_viewer"}))
	bad := []struct {
		path string
		body map[string]string
	}{
		{"/api/mobile/live-activity/token", map[string]string{"token": "not hex at all!!"}},
		{"/api/mobile/live-activity/token", map[string]string{"token": "abcd"}},
		{"/api/mobile/live-activity/start-token", map[string]string{"token": startA}},
		{"/api/mobile/live-activity/start-token", map[string]string{"token": startA, "hub_url": "ftp://x"}},
	}
	for _, b := range bad {
		if res := bearerDo(t, srv.URL, "pma_admin", "PUT", b.path, b.body); res.StatusCode != 400 {
			t.Fatalf("%s %v: %d", b.path, b.body, res.StatusCode)
		}
	}
	for _, p := range []struct{ tok, path string }{
		{"pma_admin", "/api/mobile/live-activity/start-token"}, {"pma_viewer", "/api/mobile/live-activity/start-token"},
	} {
		start := startA
		if p.tok == "pma_viewer" {
			start = startV
		}
		if res := bearerDo(t, srv.URL, p.tok, "PUT", p.path, map[string]string{"token": start, "hub_url": "https://hub.example.com"}); res.StatusCode != 200 {
			t.Fatalf("start token: %d", res.StatusCode)
		}
	}

	m := &store.Monitor{Name: "api", Type: "http", Target: "https://x"}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	t0 := time.Now()
	h.syncLiveActivities(t.Context(), t0) // startup: nothing open, nothing sent
	if len(relay.las) != 0 {
		t.Fatalf("startup sent %+v", relay.las)
	}

	// A monitor goes down: both phones have no activity, it starts on both.
	opened := t0.Add(-time.Minute).UnixMilli()
	if _, err := st.OpenIncident(m.ID, opened, "HTTP 503"); err != nil {
		t.Fatal(err)
	}
	h.syncLiveActivities(t.Context(), t0.Add(time.Second))
	if len(relay.las) != 1 {
		t.Fatalf("pushes: %+v", relay.las)
	}
	s := relay.las[0]
	if s.Event != "start" || len(s.Targets) != 2 || s.AttributesType != "OutageActivityAttributes" ||
		string(s.Attributes) != `{"hub":"https://hub.example.com"}` || s.Title != "api is down" || s.Body != "HTTP 503" ||
		s.Priority != "high" || s.StaleDate != t0.Add(time.Second+laStale).Unix() {
		t.Fatalf("start: %+v", s)
	}
	var state laState
	if err := json.Unmarshal(s.ContentState, &state); err != nil {
		t.Fatal(err)
	}
	if state.Count != 1 || len(state.Outages) != 1 || state.Outages[0].MonitorName != "api" || state.Outages[0].MonitorID != m.ID ||
		state.Outages[0].Since != float64(opened)/1000-978307200 || state.Outages[0].Cause != "HTTP 503" {
		t.Fatalf("state: %+v", state)
	}
	if !strings.Contains(string(s.ContentState), `"monitorID":`) || !strings.Contains(string(s.ContentState), `"monitorName":`) {
		t.Fatalf("keys must match the app: %s", s.ContentState)
	}

	// The admin's phone sends the token of the started activity; the
	// viewer's has not yet. A second monitor fails: update for the admin,
	// no second start for the viewer so soon.
	if res := bearerDo(t, srv.URL, "pma_admin", "PUT", "/api/mobile/live-activity/token", map[string]string{"token": tokA}); res.StatusCode != 200 {
		t.Fatalf("token: %d", res.StatusCode)
	}
	m2 := &store.Monitor{Name: "web", Type: "http", Target: "https://y"}
	st.CreateMonitor(m2)
	st.OpenIncident(m2.ID, t0.UnixMilli(), "timeout")
	h.syncLiveActivities(t.Context(), t0.Add(2*time.Second))
	if len(relay.las) != 2 || relay.las[1].Event != "update" || len(relay.las[1].Targets) != 1 || relay.las[1].Targets[0].Token != tokA ||
		relay.las[1].Targets[0].Device != "prd_admin" || relay.las[1].Title != "" || relay.las[1].Attributes != nil {
		t.Fatalf("update: %+v", relay.las[1:])
	}

	// Nothing changed: nothing sent, until the refresh is due (normal priority).
	h.syncLiveActivities(t.Context(), t0.Add(time.Minute))
	if len(relay.las) != 2 {
		t.Fatalf("sent without a change: %+v", relay.las[2:])
	}
	if res := bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/live-activity/token", map[string]string{"token": tokV}); res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	relay.laStat[tokV] = "gone" // dismissed on the viewer's phone
	h.syncLiveActivities(t.Context(), t0.Add(2*time.Second+laRefresh))
	if len(relay.las) != 3 || relay.las[2].Event != "update" || relay.las[2].Priority != "normal" || len(relay.las[2].Targets) != 2 {
		t.Fatalf("refresh: %+v", relay.las[2:])
	}
	if d, _ := st.PushDeviceByGrant(grantOf(t, st, "pma_viewer")); d.LAToken != "" || d.LAStartToken != startV {
		t.Fatalf("gone activity token kept: %+v", d)
	}

	// Everything back up: end, removed right away, and the token forgotten.
	st.ResolveIncident(m.ID, time.Now().UnixMilli())
	st.ResolveIncident(m2.ID, time.Now().UnixMilli())
	end := t0.Add(3*time.Second + laRefresh)
	h.syncLiveActivities(t.Context(), end)
	if len(relay.las) != 4 || relay.las[3].Event != "end" || relay.las[3].DismissalDate != end.Unix() || len(relay.las[3].Targets) != 1 ||
		string(relay.las[3].ContentState) != `{"outages":[],"count":0}` {
		t.Fatalf("end: %+v", relay.las[3:])
	}
	if d, _ := st.PushDeviceByGrant(grantOf(t, st, "pma_admin")); d.LAToken != "" || d.LAStartToken != startA {
		t.Fatalf("token after end: %+v", d)
	}

	// Next outage: the remote start again, past the restart wait; a bad
	// handle deletes the device.
	relay.laStat[startV] = "device_gone"
	st.OpenIncident(m.ID, time.Now().UnixMilli(), "again")
	h.syncLiveActivities(t.Context(), end.Add(laRestartAfter))
	if len(relay.las) != 5 || relay.las[4].Event != "start" || len(relay.las[4].Targets) != 2 {
		t.Fatalf("restart: %+v", relay.las[4:])
	}
	if list := decode[[]store.PushDevice](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/devices", nil)); len(list) != 0 {
		t.Fatalf("device_gone kept: %+v", list)
	}

	// The app can clear its tokens.
	for _, p := range []string{"/api/mobile/live-activity/token", "/api/mobile/live-activity/start-token"} {
		if res := bearerDo(t, srv.URL, "pma_admin", "DELETE", p, nil); res.StatusCode != 200 {
			t.Fatalf("delete %s: %d", p, res.StatusCode)
		}
	}
	if list, _ := st.LiveActivityDevices(); len(list) != 0 {
		t.Fatalf("tokens left: %+v", list)
	}
}

func grantOf(t *testing.T, st *store.Store, token string) int64 {
	t.Helper()
	g, err := st.OAuthGrantByAccess(HashToken(token))
	if err != nil {
		t.Fatal(err)
	}
	return g.ID
}

func TestLiveActivityAlert(t *testing.T) {
	open := []*store.Incident{{MonitorName: "a"}, {MonitorName: "b"}, {MonitorName: "c"}, {MonitorName: "d"}, {MonitorName: "e"}}
	title, body := laAlert(open)
	if title != "5 monitors are down" || body != "a, b, c, +2" {
		t.Fatalf("%q %q", title, body)
	}
	if st := buildLAState(open); st.Count != 5 || len(st.Outages) != 3 {
		t.Fatalf("%+v", st)
	}
}

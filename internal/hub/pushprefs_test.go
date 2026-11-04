package hub

import (
	"net/http/httptest"
	"slices"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestPushPrefs(t *testing.T) {
	h, st := newTestHub(t)
	relay := newFakeRelay(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	signInApp(t, st, "v@b.co", "pma_viewer")
	// The admin's app sign-in can change data.
	u, _ := st.UserByEmail("admin@b.co")
	far := time.Now().Add(time.Hour).UnixMilli()
	if err := st.CreateOAuthGrant(&store.OAuthGrant{UserID: u.ID, ClientID: iosClientID, Scope: ScopeWrite, AccessHash: HashToken("pma_admin"), AccessExpires: far, RefreshHash: HashToken("pma_adminr"), RefreshExpires: far}); err != nil {
		t.Fatal(err)
	}
	decode[pushSettingsView](t, admin.do("PUT", "/api/settings/push", map[string]any{"enabled": true, "relay_url": relay.srv.URL}))
	adminDev := decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_admin", "POST", "/api/mobile/devices", map[string]string{"device": "prd_admin"}))["id"]
	viewerDev := decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/mobile/devices", map[string]string{"device": "prd_viewer"}))["id"]
	ch := decode[store.Notifier](t, admin.do("POST", "/api/notifiers", map[string]any{"name": "phones", "type": "app", "enabled": true, "events": store.NotifyEvents}))
	grp := decode[store.Group](t, admin.do("POST", "/api/groups", map[string]any{"name": "prod"}))
	sub := decode[store.Group](t, admin.do("POST", "/api/groups", map[string]any{"name": "eu", "parent_id": grp.ID}))
	m1 := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{"name": "a", "type": "http", "target": "https://a.example", "group_id": sub.ID, "paused": true}))
	m2 := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{"name": "b", "type": "http", "target": "https://b.example", "paused": true}))

	// Defaults: everything, not muted, and the caller's phone is marked.
	d := decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/devices/current", nil))
	if d.ID != viewerDev || !d.Current || !d.AllMonitors || len(d.Events) != len(store.NotifyEvents) || d.MutedUntil != 0 {
		t.Fatalf("defaults: %+v", d)
	}
	list := decode[[]mobileDevice](t, admin.do("GET", "/api/mobile/devices", nil))
	if len(list) != 1 || list[0].Current {
		t.Fatalf("admin list from the web: %+v", list)
	}
	// Someone else's phone is not found.
	if res := bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/devices/"+itoa(adminDev), map[string]any{"events": []string{}}); res.StatusCode != 404 {
		t.Fatalf("other phone: %d", res.StatusCode)
	}
	if res := admin.do("GET", "/api/mobile/devices/current", nil); res.StatusCode != 404 {
		t.Fatalf("current from the web: %d", res.StatusCode)
	}

	send := func(e Event) []pushMessage {
		t.Helper()
		relay.mu.Lock()
		relay.pushes = nil
		relay.mu.Unlock()
		e.At = time.Now().UnixMilli()
		if err := h.notify.Deliver(t.Context(), &ch, e); err != nil {
			t.Fatal(err)
		}
		relay.mu.Lock()
		defer relay.mu.Unlock()
		return slices.Clone(relay.pushes)
	}
	devices := func(ps []pushMessage) []string {
		out := []string{}
		for _, p := range ps {
			out = append(out, p.Devices...)
		}
		slices.Sort(out)
		return out
	}
	down1 := Event{MonitorID: m1.ID, Monitor: "a", Status: StatusDown}
	down2 := Event{MonitorID: m2.ID, Monitor: "b", Status: StatusDown}

	// Read-only phones get the read-only category.
	ps := send(down1)
	if len(ps) != 2 {
		t.Fatalf("pushes: %+v", ps)
	}
	for _, p := range ps {
		want := CategoryMonitorDown
		if p.Devices[0] == "prd_viewer" {
			want = CategoryMonitorDownReadOnly
		}
		if p.Category != want {
			t.Fatalf("category of %v: %s", p.Devices, p.Category)
		}
	}
	if ps := send(Event{MonitorID: m1.ID, Monitor: "a", Status: StatusUp}); len(ps) != 1 || ps[0].Category != CategoryMonitorUp {
		t.Fatalf("up goes in one push: %+v", ps)
	}

	// The viewer picks the "prod" group (subgroups included) and down only.
	d = decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/devices/current", map[string]any{"events": []string{"down", "nope"}, "all_monitors": false, "group_ids": []int64{grp.ID}}))
	if d.AllMonitors || !slices.Equal(d.Events, []string{"down"}) || !slices.Equal(d.GroupIDs, []int64{grp.ID}) || d.Name != "Phone" {
		t.Fatalf("saved: %+v", d)
	}
	if got := devices(send(down1)); !slices.Equal(got, []string{"prd_admin", "prd_viewer"}) {
		t.Fatalf("group: %v", got)
	}
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin"}) {
		t.Fatalf("outside the group: %v", got)
	}
	if got := devices(send(Event{MonitorID: m1.ID, Monitor: "a", Status: StatusUp})); !slices.Equal(got, []string{"prd_admin"}) {
		t.Fatalf("event left out: %v", got)
	}
	// A PUT keeps what it leaves out.
	d = decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/devices/"+itoa(viewerDev), map[string]any{"monitor_ids": []int64{m2.ID}, "name": "Pixel"}))
	if d.Name != "Pixel" || !slices.Equal(d.Events, []string{"down"}) || !slices.Equal(d.GroupIDs, []int64{grp.ID}) {
		t.Fatalf("partial put: %+v", d)
	}
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin", "prd_viewer"}) {
		t.Fatalf("one by one: %v", got)
	}
	if res := bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/devices/current", map[string]any{"monitor_ids": []int64{9999}}); res.StatusCode != 400 {
		t.Fatalf("unknown monitor: %d", res.StatusCode)
	}

	// Muting one monitor on the viewer's phone, from a notification.
	d = decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/mobile/devices/current/mute", map[string]any{"monitor_id": m1.ID, "minutes": 60}))
	if len(d.MutedMonitors) != 1 || d.MutedMonitors[0].Until < time.Now().Add(59*time.Minute).UnixMilli() {
		t.Fatalf("mute monitor: %+v", d)
	}
	if got := devices(send(down1)); !slices.Equal(got, []string{"prd_admin"}) {
		t.Fatalf("muted monitor: %v", got)
	}
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin", "prd_viewer"}) {
		t.Fatalf("other monitor while one is muted: %v", got)
	}
	d = decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "DELETE", "/api/mobile/devices/current/mute?monitor_id="+itoa(m1.ID), nil))
	if len(d.MutedMonitors) != 0 {
		t.Fatalf("unmute monitor: %+v", d)
	}

	// Muting the whole phone, until turned back on.
	d = decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/mobile/devices/current/mute", nil))
	if d.MutedUntil != store.MuteForever {
		t.Fatalf("mute phone: %+v", d)
	}
	if got := devices(send(down1)); !slices.Equal(got, []string{"prd_admin"}) {
		t.Fatalf("muted phone: %v", got)
	}
	// Tests still reach it.
	if res := admin.do("POST", "/api/notifiers/"+itoa(ch.ID)+"/test", nil); res.StatusCode != 200 {
		t.Fatalf("test: %d", res.StatusCode)
	}
	// Every phone muted: nothing to send is not an error.
	bearerDo(t, srv.URL, "pma_admin", "POST", "/api/mobile/devices/current/mute", map[string]any{"minutes": 5})
	if got := send(down1); len(got) != 0 {
		t.Fatalf("all muted: %+v", got)
	}
	// An ended mute reads as not muted.
	if err := st.UpdatePushDevice(u.ID, &store.PushDevice{ID: adminDev, Name: "x", PushPrefs: func() store.PushPrefs {
		p := store.DefaultPushPrefs()
		p.MutedUntil = time.Now().Add(-time.Minute).UnixMilli()
		return p
	}()}); err != nil {
		t.Fatal(err)
	}
	if d := decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_admin", "GET", "/api/mobile/devices/current", nil)); d.MutedUntil != 0 {
		t.Fatalf("ended mute: %+v", d)
	}
	if res := bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/mobile/devices/current/mute", map[string]any{"minutes": -3}); res.StatusCode != 400 {
		t.Fatalf("bad minutes: %d", res.StatusCode)
	}
	d = decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "DELETE", "/api/mobile/devices/current/mute", nil))
	if d.MutedUntil != 0 {
		t.Fatalf("unmute: %+v", d)
	}

	// Agent events follow the events only.
	bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/devices/current", map[string]any{"events": []string{"agent_offline"}})
	if got := devices(send(Event{Kind: KindAgent, AgentID: 1, Agent: "x", Status: AgentOffline})); !slices.Equal(got, []string{"prd_admin", "prd_viewer"}) {
		t.Fatalf("agent: %v", got)
	}

	// Account preferences: the phone follows them again.
	d = decode[mobileDevice](t, bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/devices/current", map[string]any{"follow_account": true}))
	if !d.FollowAccount {
		t.Fatalf("follow: %+v", d)
	}
	acc := decode[store.AccountPushPrefs](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/preferences", nil))
	if !acc.AllMonitors || len(acc.Events) != len(store.NotifyEvents) || acc.PrimaryDeviceID != 0 {
		t.Fatalf("account defaults: %+v", acc)
	}
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin", "prd_viewer"}) {
		t.Fatalf("account defaults: %v", got)
	}
	acc = decode[store.AccountPushPrefs](t, bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/preferences", map[string]any{"events": []string{"up"}}))
	if !slices.Equal(acc.Events, []string{"up"}) || !acc.AllMonitors {
		t.Fatalf("account put: %+v", acc)
	}
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin"}) {
		t.Fatalf("account events: %v", got)
	}
	bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/preferences", map[string]any{"events": store.NotifyEvents})

	// A second phone; an account mute silences both, even one with its own events.
	signInApp(t, st, "v@b.co", "pma_viewer2")
	decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_viewer2", "POST", "/api/mobile/devices", map[string]string{"device": "prd_ipad"}))
	bearerDo(t, srv.URL, "pma_viewer2", "PUT", "/api/mobile/devices/current", map[string]any{"events": []string{"down"}})
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin", "prd_ipad", "prd_viewer"}) {
		t.Fatalf("two phones: %v", got)
	}
	acc = decode[store.AccountPushPrefs](t, bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/mobile/preferences/mute", map[string]any{"monitor_id": m2.ID, "minutes": 30}))
	if len(acc.MutedMonitors) != 1 {
		t.Fatalf("account mute: %+v", acc)
	}
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin"}) {
		t.Fatalf("account monitor mute: %v", got)
	}
	bearerDo(t, srv.URL, "pma_viewer", "DELETE", "/api/mobile/preferences/mute?monitor_id="+itoa(m2.ID), nil)

	// Only the phone, not the tablet.
	if res := bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/preferences", map[string]any{"primary_device_id": adminDev}); res.StatusCode != 400 {
		t.Fatalf("someone else's phone as primary: %d", res.StatusCode)
	}
	bearerDo(t, srv.URL, "pma_viewer", "PUT", "/api/mobile/preferences", map[string]any{"primary_device_id": viewerDev})
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin", "prd_viewer"}) {
		t.Fatalf("primary: %v", got)
	}
	// The primary phone signs out: every phone gets them again.
	if err := st.DeletePushDevice(userID(t, st, "v@b.co"), viewerDev); err != nil {
		t.Fatal(err)
	}
	if got := devices(send(down2)); !slices.Equal(got, []string{"prd_admin", "prd_ipad"}) {
		t.Fatalf("primary gone: %v", got)
	}
}

func userID(t *testing.T, st *store.Store, email string) int64 {
	t.Helper()
	u, err := st.UserByEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// fakeRelay mimics the push relay: one hub, answers per device from status.
type fakeRelay struct {
	mu       sync.Mutex
	srv      *httptest.Server
	hubs     int
	pushes   []pushMessage
	status   map[string]string // device handle -> result status (default sent)
	fcm      bool
	regToken string
}

func newFakeRelay(t *testing.T) *fakeRelay {
	f := &fakeRelay{status: map[string]string{}, fcm: true}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "POST /v1/hubs":
			if f.regToken != "" && r.Header.Get("Authorization") != "Bearer "+f.regToken {
				w.WriteHeader(401)
				json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized", "message": "registration token required"})
				return
			}
			f.hubs++
			w.WriteHeader(201)
			json.NewEncoder(w).Encode(map[string]string{"hub_id": "hub" + string(rune('0'+f.hubs)), "hub_token": "prh_tok" + string(rune('0'+f.hubs))})
		case "GET /v1/hubs/me":
			json.NewEncoder(w).Encode(map[string]any{"hub_id": "hub1", "fcm": f.fcm})
		case "POST /v1/push":
			if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer prh_") {
				w.WriteHeader(401)
				return
			}
			var m pushMessage
			json.NewDecoder(r.Body).Decode(&m)
			f.pushes = append(f.pushes, m)
			res := []pushResult{}
			for _, d := range m.Devices {
				st := f.status[d]
				if st == "" {
					st = "sent"
				}
				res = append(res, pushResult{Device: d, Status: st})
			}
			json.NewEncoder(w).Encode(map[string]any{"results": res})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	return f
}

// signInApp gives a user an OAuth sign-in of the iOS app and returns its access token.
func signInApp(t *testing.T, st *store.Store, email, token string) {
	t.Helper()
	u, err := st.UserByEmail(email)
	if err != nil {
		t.Fatal(err)
	}
	far := time.Now().Add(time.Hour).UnixMilli()
	if err := st.CreateOAuthGrant(&store.OAuthGrant{UserID: u.ID, ClientID: iosClientID, Scope: ScopeRead, AccessHash: HashToken(token), AccessExpires: far, RefreshHash: HashToken(token + "r"), RefreshExpires: far}); err != nil {
		t.Fatal(err)
	}
}

func TestAppPush(t *testing.T) {
	h, st := newTestHub(t)
	h.notify.pushRetry = []time.Duration{time.Millisecond}
	relay := newFakeRelay(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	signInApp(t, st, "admin@b.co", "pma_admin")
	signInApp(t, st, "v@b.co", "pma_viewer")

	// Off by default: the app is told so and cannot enroll.
	cfg := decode[map[string]any](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/config", nil))
	if cfg["enabled"] != false || cfg["hub_id"] != "" {
		t.Fatalf("config while off: %+v", cfg)
	}
	if res := bearerDo(t, srv.URL, "pma_admin", "POST", "/api/mobile/devices", map[string]string{"device": "prd_a"}); res.StatusCode != 409 {
		t.Fatalf("enroll while off: %d", res.StatusCode)
	}

	// Turning it on registers the hub with the relay, once.
	if res := admin.do("PUT", "/api/settings/push", map[string]any{"enabled": true, "relay_url": "ftp://x"}); res.StatusCode != 400 {
		t.Fatalf("bad relay url: %d", res.StatusCode)
	}
	relay.regToken = "letmein"
	if res := admin.do("PUT", "/api/settings/push", map[string]any{"enabled": true, "relay_url": relay.srv.URL}); res.StatusCode != 502 {
		t.Fatalf("registration without token: %d", res.StatusCode)
	}
	v := decode[pushSettingsView](t, admin.do("PUT", "/api/settings/push", map[string]any{"enabled": true, "relay_url": relay.srv.URL + "/", "registration_token": "letmein"}))
	if !v.Enabled || !v.Registered || v.HubID != "hub1" || v.RelayURL != relay.srv.URL {
		t.Fatalf("settings: %+v", v)
	}
	decode[pushSettingsView](t, admin.do("PUT", "/api/settings/push", map[string]any{"enabled": true, "relay_url": relay.srv.URL}))
	if relay.hubs != 1 {
		t.Fatalf("registered %d times", relay.hubs)
	}
	raw := admin.do("GET", "/api/settings/push", nil)
	var body map[string]any
	json.NewDecoder(raw.Body).Decode(&body)
	if _, leak := body["hub_token"]; leak {
		t.Fatal("hub token returned")
	}
	if res := admin.do("POST", "/api/settings/push/test", nil); res.StatusCode != 200 {
		t.Fatalf("test: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, "pma_admin", "GET", "/api/settings/push", nil); res.StatusCode != 403 {
		t.Fatalf("settings with a token: %d", res.StatusCode)
	}
	cfg = decode[map[string]any](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/config", nil))
	if cfg["enabled"] != true || cfg["hub_id"] != "hub1" || cfg["relay_url"] != relay.srv.URL {
		t.Fatalf("config: %+v", cfg)
	}

	// Phones enroll with OAuth tokens only, read-only users included. A new
	// handle of the same sign-in replaces the old one.
	if res := admin.do("POST", "/api/mobile/devices", map[string]string{"device": "prd_a"}); res.StatusCode != 403 {
		t.Fatalf("enroll with a session: %d", res.StatusCode)
	}
	if res := bearerDo(t, srv.URL, "pma_admin", "POST", "/api/mobile/devices", map[string]string{"device": "fcm-token"}); res.StatusCode != 400 {
		t.Fatalf("raw fcm token: %d", res.StatusCode)
	}
	first := decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_admin", "POST", "/api/mobile/devices", map[string]string{"device": "prd_old", "name": "iPhone"}))
	again := decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_admin", "POST", "/api/mobile/devices", map[string]string{"device": "prd_admin", "name": "Admin's iPhone"}))
	if first["id"] == 0 || first["id"] != again["id"] {
		t.Fatalf("ids: %v %v", first, again)
	}
	decode[map[string]int64](t, bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/mobile/devices", map[string]string{"device": "prd_viewer"}))
	list := decode[[]store.PushDevice](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/devices", nil))
	if len(list) != 1 || list[0].Name != "Phone" {
		t.Fatalf("viewer devices: %+v", list)
	}
	grants := decode[[]map[string]any](t, admin.do("GET", "/api/oauth/grants", nil))
	if len(grants) != 1 || grants[0]["push_device"] != "Admin's iPhone" {
		t.Fatalf("grants: %+v", grants)
	}

	// The app channel needs a valid user list.
	if res := admin.do("POST", "/api/notifiers", map[string]any{"name": "x", "type": "app", "config": map[string]string{"users": "nope"}}); res.StatusCode != 400 {
		t.Fatalf("bad users: %d", res.StatusCode)
	}
	all := decode[store.Notifier](t, admin.do("POST", "/api/notifiers", map[string]any{"name": "phones", "type": "app", "enabled": true}))

	// A test goes to every phone; a gone handle is deleted.
	relay.status["prd_viewer"] = "gone"
	if res := admin.do("POST", "/api/notifiers/"+itoa(all.ID)+"/test", nil); res.StatusCode != 200 {
		t.Fatalf("test channel: %d", res.StatusCode)
	}
	if len(relay.pushes) != 1 || len(relay.pushes[0].Devices) != 2 || !strings.HasPrefix(relay.pushes[0].Title, "[test] ") || relay.pushes[0].Priority != "high" {
		t.Fatalf("pushes: %+v", relay.pushes)
	}
	if list := decode[[]store.PushDevice](t, bearerDo(t, srv.URL, "pma_viewer", "GET", "/api/mobile/devices", nil)); len(list) != 0 {
		t.Fatalf("gone device kept: %+v", list)
	}

	// A channel for one user only: the viewer has no phone left.
	only := decode[store.Notifier](t, admin.do("POST", "/api/notifiers", map[string]any{"name": "v", "type": "app", "enabled": true, "config": map[string]string{"users": "V@b.co"}}))
	if res := admin.do("POST", "/api/notifiers/"+itoa(only.ID)+"/test", nil); res.StatusCode != 409 {
		t.Fatalf("no phones: %d", res.StatusCode)
	}

	// Failed deliveries are retried.
	relay.status["prd_admin"] = "failed"
	err := h.notify.Deliver(t.Context(), &all, Event{MonitorID: 7, Monitor: "api", Target: "https://x", Status: StatusUp, At: time.Now().UnixMilli()})
	if err == nil || len(relay.pushes) != 3 {
		t.Fatalf("retry: %v, %d pushes", err, len(relay.pushes))
	}
	delete(relay.status, "prd_admin")

	// Removing a phone, or signing the app out, stops the notifications.
	if res := bearerDo(t, srv.URL, "pma_viewer", "DELETE", "/api/mobile/devices/"+itoa(again["id"]), nil); res.StatusCode != 404 {
		t.Fatalf("delete someone else's phone: %d", res.StatusCode)
	}
	g := grants[0]["id"].(float64)
	if res := admin.do("DELETE", "/api/oauth/grants/"+itoa(int64(g)), nil); res.StatusCode != 200 {
		t.Fatalf("revoke: %d", res.StatusCode)
	}
	if n, _ := st.CountPushDevices(); n != 0 {
		t.Fatalf("devices after sign out: %d", n)
	}

	// Another relay: a new registration, old handles are dropped.
	signInApp(t, st, "admin@b.co", "pma_admin2")
	bearerDo(t, srv.URL, "pma_admin2", "POST", "/api/mobile/devices", map[string]string{"device": "prd_x"})
	other := newFakeRelay(t)
	v = decode[pushSettingsView](t, admin.do("PUT", "/api/settings/push", map[string]any{"enabled": true, "relay_url": other.srv.URL}))
	if other.hubs != 1 || v.Devices != 0 {
		t.Fatalf("relay change: %+v", v)
	}
}


func TestAppMessage(t *testing.T) {
	at := time.Now().UnixMilli()
	down := appMessage(Event{MonitorID: 42, Monitor: "api", Target: "https://api", Status: StatusDown, Cause: "HTTP 503", At: at, DownSince: at - 5*60_000, Reminder: 1})
	if down.Title != "api is still down" || down.Priority != "high" || down.TTL != 3600 || down.CollapseID != "monitor-42" ||
		down.Data["monitor_id"] != "42" || down.Data["status"] != "down" || down.Body != "https://api\nHTTP 503\nDown for 5m" {
		t.Fatalf("down: %+v", down)
	}
	up := appMessage(Event{MonitorID: 42, Monitor: "api", Target: "https://api", Status: StatusUp, At: at, DownSince: at - 5*60_000})
	if up.Title != "api is back up" || up.Priority != "normal" || up.CollapseID != "monitor-42" || up.Body != "https://api · after 5m down" {
		t.Fatalf("up: %+v", up)
	}
	days := 3
	cert := appMessage(Event{Kind: KindCert, MonitorID: 42, Monitor: "api", Target: "https://api", Status: ExpiryExpiring, Subject: "api.example.com", ExpiresAt: at, DaysLeft: &days})
	if !strings.HasPrefix(cert.Title, "api.example.com: TLS certificate expires in 3 days") || cert.TTL != 86400 || cert.CollapseID != "cert_expiry-42" ||
		cert.ThreadID != "monitor-42" || cert.Body != "monitor api (https://api)" || cert.Data["type"] != KindCert {
		t.Fatalf("cert: %+v", cert)
	}
	long := appMessage(Event{Monitor: strings.Repeat("é", 300), Status: StatusDown})
	if len(long.Title) > maxPushTitle || !strings.HasSuffix(long.Title, "é") {
		t.Fatalf("title: %d bytes", len(long.Title))
	}
}

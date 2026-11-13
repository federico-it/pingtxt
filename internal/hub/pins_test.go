package hub

import (
	"net/http/httptest"
	"slices"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestPins(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	admin.do("POST", "/api/users", map[string]any{"email": "v@b.co", "password": "password123", "role": "viewer"})
	signInApp(t, st, "v@b.co", "pma_viewer") // read-only user, read-only token
	m1 := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{"name": "a", "type": "http", "target": "https://a.example", "paused": true}))
	m2 := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{"name": "b", "type": "http", "target": "https://b.example", "paused": true}))
	m3 := decode[store.Monitor](t, admin.do("POST", "/api/monitors", map[string]any{"name": "c", "type": "http", "target": "https://c.example", "paused": true}))

	pins := func(p MonitorPins) []int64 { return p.MonitorIDs }
	if got := pins(decode[MonitorPins](t, admin.do("GET", "/api/account/pins", nil))); got == nil || len(got) != 0 {
		t.Fatalf("empty: %v", got)
	}

	// PUT keeps the order and drops unknown and repeated ids.
	got := pins(decode[MonitorPins](t, admin.do("PUT", "/api/account/pins", map[string]any{"monitor_ids": []int64{m2.ID, 999, m1.ID, m2.ID}})))
	if !slices.Equal(got, []int64{m2.ID, m1.ID}) {
		t.Fatalf("put: %v", got)
	}
	// A body without the field changes nothing.
	if got := pins(decode[MonitorPins](t, admin.do("PUT", "/api/account/pins", map[string]any{}))); !slices.Equal(got, []int64{m2.ID, m1.ID}) {
		t.Fatalf("empty put: %v", got)
	}
	// Pin adds at the end, twice is a no-op; unpin removes.
	admin.do("POST", "/api/account/pins/"+itoa(m3.ID), nil)
	got = pins(decode[MonitorPins](t, admin.do("POST", "/api/account/pins/"+itoa(m3.ID), nil)))
	if !slices.Equal(got, []int64{m2.ID, m1.ID, m3.ID}) {
		t.Fatalf("pin: %v", got)
	}
	got = pins(decode[MonitorPins](t, admin.do("DELETE", "/api/account/pins/"+itoa(m1.ID), nil)))
	if !slices.Equal(got, []int64{m2.ID, m3.ID}) {
		t.Fatalf("unpin: %v", got)
	}
	if res := admin.do("POST", "/api/account/pins/999", nil); res.StatusCode != 404 {
		t.Fatalf("unknown monitor: %d", res.StatusCode)
	}
	if res := admin.do("POST", "/api/account/pins/x", nil); res.StatusCode != 400 {
		t.Fatalf("bad id: %d", res.StatusCode)
	}

	// Pins are personal: a read-only user with a read-only token has their own.
	got = pins(decode[MonitorPins](t, bearerDo(t, srv.URL, "pma_viewer", "POST", "/api/account/pins/"+itoa(m1.ID), nil)))
	if !slices.Equal(got, []int64{m1.ID}) {
		t.Fatalf("viewer pin: %v", got)
	}
	if got := pins(decode[MonitorPins](t, admin.do("GET", "/api/account/pins", nil))); !slices.Equal(got, []int64{m2.ID, m3.ID}) {
		t.Fatalf("admin after viewer: %v", got)
	}

	// Deleted monitors drop out of the list.
	admin.do("DELETE", "/api/monitors/"+itoa(m2.ID), nil)
	if got := pins(decode[MonitorPins](t, admin.do("GET", "/api/account/pins", nil))); !slices.Equal(got, []int64{m3.ID}) {
		t.Fatalf("after delete: %v", got)
	}
}

package hub

import (
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestOnboarding(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})

	steps := func(v onboardingView) map[string]bool {
		m := map[string]bool{}
		for _, s := range v.Steps {
			m[s.ID] = s.Done
		}
		return m
	}
	v := decode[onboardingView](t, admin.do("GET", "/api/onboarding", nil))
	if v.Dismissed || v.Complete || v.Done != 0 || v.Total != len(v.Steps) {
		t.Fatalf("fresh hub: %+v", v)
	}
	// Scheduled backups are a step with SQLite only.
	if _, ok := steps(v)[stepBackups]; ok != h.FileBackups() {
		t.Fatalf("backups step with %s: %+v", h.store.Driver(), v.Steps)
	}

	admin.do("POST", "/api/monitors", map[string]any{"name": "a", "type": "http", "target": "https://a.example", "paused": true})
	admin.do("PUT", "/api/settings/general", map[string]any{"public_url": "https://hub.example"})
	v = decode[onboardingView](t, admin.do("GET", "/api/onboarding", nil))
	if s := steps(v); !s[stepMonitor] || !s[stepPublicURL] || s[stepChannel] || v.Done != 2 {
		t.Fatalf("after monitor + url: %+v", v)
	}

	// Hiding it is stored; an empty PUT keeps it hidden.
	if v = decode[onboardingView](t, admin.do("PUT", "/api/onboarding", map[string]any{"dismissed": true})); !v.Dismissed {
		t.Fatalf("dismiss: %+v", v)
	}
	if v = decode[onboardingView](t, admin.do("PUT", "/api/onboarding", map[string]any{})); !v.Dismissed {
		t.Fatalf("empty put: %+v", v)
	}
	if v = decode[onboardingView](t, admin.do("PUT", "/api/onboarding", map[string]any{"dismissed": false})); v.Dismissed {
		t.Fatalf("show again: %+v", v)
	}
}

package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func fakeReleases(t *testing.T, body string) *atomic.Int32 {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	old := latestReleaseURL
	latestReleaseURL = srv.URL
	t.Cleanup(func() { latestReleaseURL = old })
	return &hits
}

func TestUpdateCheck(t *testing.T) {
	fakeReleases(t, `{"tag_name":"v0.2.0","name":"pingtxt 0.2.0","html_url":"https://github.com/federico-it/pingtxt/releases/tag/v0.2.0","published_at":"2026-10-30T10:00:00Z"}`)
	h, _ := newTestHub(t)

	h.cfg.Version = "v0.1.0"
	h.checkLatestRelease(context.Background())
	v := h.versionStatus()
	if !v.CheckEnabled || !v.UpdateAvailable || v.Latest == nil || v.Latest.Version != "v0.2.0" || v.Error != "" {
		t.Fatalf("v0.1.0: %+v", v)
	}
	if v.Latest.URL != "https://github.com/federico-it/pingtxt/releases/tag/v0.2.0" || v.Latest.PublishedAt == 0 {
		t.Fatalf("latest: %+v", v.Latest)
	}

	// Docker images report the version without the v.
	for _, cur := range []string{"0.2.0", "v0.2.0", "v0.3.0"} {
		h.cfg.Version = cur
		if v := h.versionStatus(); v.UpdateAvailable {
			t.Errorf("%s: update available", cur)
		}
	}

	// Development and branch builds never check.
	for _, cur := range []string{"dev", "", "main-abc1234"} {
		h.cfg.Version = cur
		if v := h.versionStatus(); v.CheckEnabled || v.UpdateAvailable || v.Latest != nil {
			t.Errorf("%q: %+v", cur, v)
		}
	}

	// Turned off in the settings.
	h.cfg.Version = "v0.1.0"
	h.smu.Lock()
	h.general.UpdateCheck.Disabled = true
	h.smu.Unlock()
	if v := h.versionStatus(); v.CheckEnabled || !v.CheckDisabled || v.UpdateAvailable {
		t.Errorf("disabled: %+v", v)
	}
}

func TestUpdateCheckRejectsOddReleases(t *testing.T) {
	h, _ := newTestHub(t)
	h.cfg.Version = "v0.1.0"

	fakeReleases(t, `{"tag_name":"v0.2.0-rc1","prerelease":true}`)
	h.checkLatestRelease(context.Background())
	if v := h.versionStatus(); v.Error == "" || v.UpdateAvailable {
		t.Errorf("prerelease: %+v", v)
	}

	// A link outside the repository is replaced by the tag page.
	fakeReleases(t, `{"tag_name":"v0.2.0","html_url":"javascript:alert(1)"}`)
	h.checkLatestRelease(context.Background())
	v := h.versionStatus()
	if v.Error != "" || v.Latest == nil || v.Latest.URL != "https://github.com/federico-it/pingtxt/releases/tag/v0.2.0" {
		t.Errorf("link: %+v", v)
	}
}

func TestUpdateCheckSettingSeed(t *testing.T) {
	g := defaultGeneral(Config{DisableUpdateCheck: true})
	if !g.UpdateCheck.Disabled {
		t.Fatal("PINGTXT_DISABLE_UPDATE_CHECK not seeded")
	}
}

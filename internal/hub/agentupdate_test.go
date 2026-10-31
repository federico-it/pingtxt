package hub

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/protocol"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestNewerThan(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v1.2.0", "v1.1.9", true},
		{"v1.1.9", "v1.2.0", false},
		{"v1.2.0", "v1.2.0", false},
		{"v2.0.0-rc1", "v1.9.9", true},
		{"main-abc1234", "v1.0.0", false},
		{"v1.0.0", "dev", false},
	}
	for _, c := range cases {
		if got := newerThan(c.a, c.b); got != c.want {
			t.Errorf("newerThan(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func testConn(id int64, hl protocol.Hello) *agentConn {
	return &agentConn{agent: &store.Agent{ID: id, Name: "a"}, hello: hl, out: make(chan protocol.Message, 8), done: make(chan struct{})}
}

func TestOfferUpdate(t *testing.T) {
	h, _ := newTestHub(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pingtxt-agent-linux-amd64"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.cfg.AgentDir = dir
	h.cfg.Version = "v1.1.0"

	old := protocol.Hello{Version: "v1.0.0", OS: "linux", Arch: "amd64", SelfUpdate: true}
	ac := testConn(1, old)
	if err := h.offerUpdate(ac, false); err != nil {
		t.Fatal(err)
	}
	m := <-ac.out
	if m.Type != protocol.TypeUpdate || m.Update.Version != "v1.1.0" || m.Update.Path != "/download/pingtxt-agent-linux-amd64" ||
		len(m.Update.SHA256) != 64 {
		t.Fatalf("unexpected update message %+v", m.Update)
	}
	// Second automatic offer while in progress is skipped.
	if err := h.offerUpdate(ac, false); err == nil || !strings.Contains(err.Error(), "in progress") {
		t.Fatalf("want in progress, got %v", err)
	}
	if v := h.agentUpdateView(&store.Agent{ID: 1, Version: "v1.0.0"}); v.UpdateState != "updating" || !v.UpdateAvailable {
		t.Fatalf("view %+v", v)
	}
	// A reported failure blocks automatic retries but not manual ones.
	h.updateFailed(1, protocol.UpdateStatus{Version: "v1.0.0", Error: "boom"})
	if v := h.agentUpdateView(&store.Agent{ID: 1, Version: "v1.0.0"}); v.UpdateState != "failed" || v.UpdateError != "boom" {
		t.Fatalf("view %+v", v)
	}
	if err := h.offerUpdate(ac, false); err == nil {
		t.Fatal("automatic retry right after a failure")
	}
	if err := h.offerUpdate(ac, true); err != nil {
		t.Fatalf("manual retry: %v", err)
	}
	// Reconnecting with the target version clears the state.
	h.agentConnected(testConn(1, protocol.Hello{Version: "v1.1.0", OS: "linux", Arch: "amd64", SelfUpdate: true}))
	if v := h.agentUpdateView(&store.Agent{ID: 1, Version: "v1.1.0"}); v.UpdateState != "" || v.UpdateAvailable {
		t.Fatalf("view after update %+v", v)
	}

	for name, c := range map[string]struct {
		hl      protocol.Hello
		version string
		err     string
	}{
		"dev hub":     {old, "dev", "development build"},
		"up to date":  {protocol.Hello{Version: "v1.1.0", SelfUpdate: true}, "v1.1.0", "up to date"},
		"container":   {protocol.Hello{Version: "v1.0.0", NoUpdateReason: protocol.NoUpdateContainer}, "v1.1.0", "container"},
		"old agent":   {protocol.Hello{Version: "v0.1.0"}, "v1.1.0", "too old"},
		"newer agent": {protocol.Hello{Version: "v2.0.0", OS: "linux", Arch: "amd64", SelfUpdate: true}, "v1.1.0", "newer"},
		"no binary":   {protocol.Hello{Version: "v1.0.0", OS: "darwin", Arch: "arm64", SelfUpdate: true}, "v1.1.0", "does not bundle"},
	} {
		h.cfg.Version = c.version
		err := h.offerUpdate(testConn(2, c.hl), false)
		if err == nil || !strings.Contains(err.Error(), c.err) {
			t.Errorf("%s: want %q, got %v", name, c.err, err)
		}
	}
}

func TestAutoUpdateSetting(t *testing.T) {
	h, _ := newTestHub(t)
	if !h.agentUpdateSettings().AutoUpdate {
		t.Fatal("auto update should default to on")
	}
	if err := h.setAgentUpdateSettings(AgentUpdateSettings{AutoUpdate: false}); err != nil {
		t.Fatal(err)
	}
	h.updates.settings = nil // force a reload from the database
	if h.agentUpdateSettings().AutoUpdate {
		t.Fatal("setting not persisted")
	}
}

func TestUpdateRetrySoon(t *testing.T) {
	h, _ := newTestHub(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "pingtxt-agent-linux-amd64"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	h.cfg.AgentDir = dir
	h.cfg.Version = "v1.1.0"
	ac := testConn(1, protocol.Hello{Version: "v1.0.0", OS: "linux", Arch: "amd64", SelfUpdate: true})

	if err := h.offerUpdate(ac, true); err != nil {
		t.Fatal(err)
	}
	<-ac.out
	for name, st := range map[string]protocol.UpdateStatus{
		"retry_soon flag":    {Version: "v1.0.0", Error: "release v1.1.0 has no SHA256SUMS yet", RetrySoon: true},
		"old agent's string": {Version: "v1.0.0", Error: "release v1.1.0 publishes no checksum for pingtxt-agent-linux-amd64, update refused"},
	} {
		h.updateFailed(1, st)
		if err := h.offerUpdate(ac, false); err == nil {
			t.Fatalf("%s: automatic retry right after the failure", name)
		}
		h.updates.mu.Lock()
		h.updates.state[1].started = time.Now().Add(-updateRetrySoon - time.Second)
		h.updates.mu.Unlock()
		if err := h.offerUpdate(ac, false); err != nil {
			t.Fatalf("%s: no automatic retry after %s: %v", name, updateRetrySoon, err)
		}
		<-ac.out
	}

	// Other failures wait hours.
	h.updateFailed(1, protocol.UpdateStatus{Version: "v1.0.0", Error: "checksum mismatch"})
	h.updates.mu.Lock()
	h.updates.state[1].started = time.Now().Add(-updateRetrySoon - time.Second)
	h.updates.mu.Unlock()
	if err := h.offerUpdate(ac, false); err == nil {
		t.Fatal("automatic retry minutes after a hard failure")
	}
}

func TestReleasePublished(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1.1.0/SHA256SUMS":
			w.WriteHeader(http.StatusOK)
		case "/v1.3.0/SHA256SUMS":
			http.Error(w, "boom", http.StatusBadGateway)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	old := releaseDownloadURL
	releaseDownloadURL = srv.URL + "/"
	defer func() { releaseDownloadURL = old }()

	ctx := context.Background()
	for _, c := range []struct {
		version string
		waited  time.Duration
		want    bool
	}{
		{"v1.1.0", 0, true},               // published
		{"v1.2.0", 0, false},              // still uploading
		{"v1.2.0", releaseSumsWait, true}, // waited long enough
		{"v1.3.0", 0, true},               // GitHub error: the agents decide
		{"main-abc1234", 0, true},         // not a release
		{"v1.2.0-rc1", 0, true},           // not a release
	} {
		h.cfg.Version = c.version
		if got := h.releasePublished(ctx, c.waited); got != c.want {
			t.Errorf("%s after %s: got %v, want %v", c.version, c.waited, got, c.want)
		}
	}
}

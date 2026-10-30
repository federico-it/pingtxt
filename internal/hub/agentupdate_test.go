package hub

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

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

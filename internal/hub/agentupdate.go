package hub

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/protocol"
	"github.com/federico-it/pingtxt/internal/store"
)

// Agent self-update: the hub bundles agent binaries built with its own
// version (see Dockerfile). When an agent with another version connects, the
// hub sends it the download path and the SHA-256 of the matching binary; the
// agent downloads it from the hub, checks the digest and the version, swaps
// the file and restarts itself. Docker agents cannot do this and are updated
// by pulling a new image.

const keyAgentUpdates = "agent_updates"

// AgentUpdateSettings control automatic agent updates.
type AgentUpdateSettings struct {
	// AutoUpdate updates agents to the hub version as soon as they connect.
	AutoUpdate bool `json:"auto_update"`
}

// Retry delays for automatic updates. A manual update ignores them.
const (
	updateTimeout    = 5 * time.Minute // an update with no news after this is considered lost
	updateRetryAfter = 6 * time.Hour   // after a failure, wait before trying the same version again
)

type agentUpdate struct {
	target  string
	started time.Time
	err     string
}

type agentUpdates struct {
	mu       sync.Mutex
	settings *AgentUpdateSettings // nil until loaded
	state    map[int64]*agentUpdate
	sums     map[string]binarySum // by file path
}

type binarySum struct {
	size  int64
	mtime time.Time
	sum   string
}

func (h *Hub) agentUpdateSettings() AgentUpdateSettings {
	u := &h.updates
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.settings == nil {
		s := AgentUpdateSettings{AutoUpdate: true}
		if err := h.store.GetSetting(keyAgentUpdates, &s); err != nil && !errors.Is(err, store.ErrNotFound) {
			h.log.Error("load agent update settings", "err", err)
		}
		u.settings = &s
	}
	return *u.settings
}

func (h *Hub) setAgentUpdateSettings(s AgentUpdateSettings) error {
	if err := h.store.SetSetting(keyAgentUpdates, s); err != nil {
		return err
	}
	h.updates.mu.Lock()
	h.updates.settings = &s
	h.updates.mu.Unlock()
	return nil
}

// updatableVersion reports whether the hub runs a build agents can update to.
func updatableVersion(v string) bool { return v != "" && v != "dev" }

// semver parses "v1.2.3" (prerelease and build suffixes ignored).
func semver(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(v, "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// newerThan is true when a and b are both semantic versions and a > b.
func newerThan(a, b string) bool {
	x, ok1 := semver(a)
	y, ok2 := semver(b)
	if !ok1 || !ok2 {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return x[i] > y[i]
		}
	}
	return false
}

func agentBinaryName(goos, arch string) string {
	n := "pingtxt-agent-" + goos + "-" + arch
	if goos == "windows" {
		n += ".exe"
	}
	return n
}

// binarySHA256 returns the hex digest of a bundled agent binary, cached
// until the file changes.
func (h *Hub) binarySHA256(name string) (string, error) {
	if h.cfg.AgentDir == "" || !agentFileRe.MatchString(name) || strings.HasSuffix(name, ".sha256") {
		return "", os.ErrNotExist
	}
	p := filepath.Join(h.cfg.AgentDir, name)
	st, err := os.Stat(p)
	if err != nil {
		return "", err
	}
	u := &h.updates
	u.mu.Lock()
	c, ok := u.sums[p]
	u.mu.Unlock()
	if ok && c.size == st.Size() && c.mtime.Equal(st.ModTime()) {
		return c.sum, nil
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	hs := sha256.New()
	if _, err := io.Copy(hs, f); err != nil {
		return "", err
	}
	sum := hex.EncodeToString(hs.Sum(nil))
	u.mu.Lock()
	if u.sums == nil {
		u.sums = map[string]binarySum{}
	}
	u.sums[p] = binarySum{size: st.Size(), mtime: st.ModTime(), sum: sum}
	u.mu.Unlock()
	return sum, nil
}

// offerUpdate asks a connected agent to update to the hub version. Automatic
// offers skip agents that are newer than the hub, are already updating, or
// failed with this version recently.
func (h *Hub) offerUpdate(ac *agentConn, manual bool) error {
	target := h.cfg.Version
	hl := ac.hello
	switch {
	case !updatableVersion(target):
		return errors.New("the hub is a development build: agents can only update to a released version")
	case hl.Version == target:
		return errors.New("agent is already up to date")
	case !hl.SelfUpdate:
		return errors.New(noUpdateText(hl))
	case !manual && newerThan(hl.Version, target):
		return errors.New("agent is newer than the hub")
	case !manual && hl.Version == "dev":
		return errors.New("agent is a development build")
	}
	name := agentBinaryName(hl.OS, hl.Arch)
	sum, err := h.binarySHA256(name)
	if err != nil {
		return fmt.Errorf("this hub does not bundle an agent for %s/%s", hl.OS, hl.Arch)
	}

	u := &h.updates
	u.mu.Lock()
	if u.state == nil {
		u.state = map[int64]*agentUpdate{}
	}
	if st := u.state[ac.agent.ID]; st != nil && st.target == target {
		busy := st.err == "" && time.Since(st.started) < updateTimeout
		if busy {
			u.mu.Unlock()
			return errors.New("update already in progress")
		}
		if !manual && st.err != "" && time.Since(st.started) < updateRetryAfter {
			u.mu.Unlock()
			return errors.New("last update failed: " + st.err)
		}
	}
	u.state[ac.agent.ID] = &agentUpdate{target: target, started: time.Now()}
	u.mu.Unlock()

	h.log.Info("updating agent", "agent", ac.agent.Name, "from", hl.Version, "to", target, "manual", manual)
	ac.send(protocol.Message{Type: protocol.TypeUpdate, Update: &protocol.Update{Version: target, Path: "/download/" + name, SHA256: sum}})
	return nil
}

func noUpdateText(hl protocol.Hello) string {
	switch hl.NoUpdateReason {
	case protocol.NoUpdateContainer:
		return "agent runs in a container: pull the new image (or use Watchtower)"
	case protocol.NoUpdateDisabled:
		return "self-update is turned off on the agent (-no-auto-update)"
	case protocol.NoUpdateNotWritable:
		return "the agent cannot write its own binary: re-run the install script"
	case protocol.NoUpdateUnsupported:
		return "self-update is not supported on this OS"
	case protocol.NoUpdateInsecure:
		return "the agent reaches the hub over plain http: use an https hub URL (or -insecure-update)"
	}
	return "agent version too old for self-update: re-run the install script once"
}

// agentConnected updates the update state when an agent (re)connects and
// starts an automatic update when needed.
func (h *Hub) agentConnected(ac *agentConn) {
	u := &h.updates
	u.mu.Lock()
	if st := u.state[ac.agent.ID]; st != nil && st.target == ac.hello.Version {
		delete(u.state, ac.agent.ID)
		h.log.Info("agent updated", "agent", ac.agent.Name, "version", ac.hello.Version)
	}
	u.mu.Unlock()
	if h.agentUpdateSettings().AutoUpdate {
		_ = h.offerUpdate(ac, false)
	}
}

// updateFailed records an error reported by the agent.
func (h *Hub) updateFailed(id int64, st protocol.UpdateStatus) {
	u := &h.updates
	u.mu.Lock()
	defer u.mu.Unlock()
	if s := u.state[id]; s != nil {
		s.err = st.Error
		if s.err == "" {
			s.err = "unknown error"
		}
	}
}

// updateView is the update info shown for an agent.
type updateView struct {
	UpdateAvailable bool   `json:"update_available"`
	SelfUpdate      bool   `json:"self_update"`
	NoUpdateReason  string `json:"no_update_reason,omitempty"`
	UpdateState     string `json:"update_state,omitempty"` // updating | failed
	UpdateError     string `json:"update_error,omitempty"`
}

func (h *Hub) agentUpdateView(a *store.Agent) updateView {
	v := updateView{UpdateAvailable: updatableVersion(h.cfg.Version) && a.Version != "" && a.Version != h.cfg.Version}
	h.mu.Lock()
	ac := h.agents[a.ID]
	h.mu.Unlock()
	if ac != nil {
		v.SelfUpdate = ac.hello.SelfUpdate
		if !v.SelfUpdate {
			v.NoUpdateReason = noUpdateText(ac.hello)
		}
	}
	u := &h.updates
	u.mu.Lock()
	defer u.mu.Unlock()
	if st := u.state[a.ID]; st != nil && st.target == h.cfg.Version {
		switch {
		case st.err != "":
			v.UpdateState, v.UpdateError = "failed", st.err
		case time.Since(st.started) < updateTimeout:
			v.UpdateState = "updating"
		default:
			v.UpdateState, v.UpdateError = "failed", "no answer from the agent"
		}
	}
	return v
}

// ---------- API ----------

func (h *Hub) putAgentUpdateSettings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in AgentUpdateSettings
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if err := h.setAgentUpdateSettings(in); err != nil {
		h.internalErr(w, err)
		return
	}
	if in.AutoUpdate {
		h.mu.Lock()
		conns := make([]*agentConn, 0, len(h.agents))
		for _, ac := range h.agents {
			conns = append(conns, ac)
		}
		h.mu.Unlock()
		for _, ac := range conns {
			_ = h.offerUpdate(ac, false)
		}
	}
	writeJSON(w, 200, in)
}

func (h *Hub) updateAgentNow(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return
	}
	h.mu.Lock()
	ac := h.agents[id]
	h.mu.Unlock()
	if ac == nil {
		writeErr(w, 409, "agent is offline")
		return
	}
	if err := h.offerUpdate(ac, true); err != nil {
		writeErr(w, 409, err.Error())
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

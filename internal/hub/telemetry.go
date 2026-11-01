package hub

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"sync"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

// Anonymous usage stats: when an admin opts in, the hub sends once an hour a
// random instance id, its version, how many monitors are active and how many
// agents are connected. Nothing else: no names, targets or addresses.
// Nothing is sent before an admin says yes, and PINGTXT_DISABLE_TELEMETRY
// turns it off for good (the opt-in question is not shown either).

const keyTelemetry = "telemetry"

// TelemetrySettings live in their own settings key, so a configuration
// import never copies the choice or the instance id to another hub.
type TelemetrySettings struct {
	Enabled bool `json:"enabled"`
	// Asked is true once an admin answered the opt-in question.
	Asked bool `json:"asked"`
	// InstanceID is random, made the first time stats are turned on and kept
	// for good, so the same hub is counted once even after off and on again.
	InstanceID string `json:"instance_id,omitempty"`
}

const (
	telemetryEvery  = time.Hour
	telemetryJitter = 5 * time.Minute
)

// telemetryURL is a variable so tests can point it at a fake server.
var telemetryURL = "https://telemetry.pingtxt.com/v1/report"

// telemetryReport is the whole body sent to the stats service.
type telemetryReport struct {
	ID       string `json:"id"`
	Version  string `json:"version"`
	Monitors int    `json:"monitors"` // monitors not paused
	Agents   int    `json:"agents"`   // agents connected now
}

type telemetry struct {
	mu     sync.Mutex
	sentAt time.Time
	err    string
	wake   chan struct{}
}

// telemetryForcedOff is true when the environment turns stats off, or for
// development builds, which never send.
func (h *Hub) telemetryForcedOff() bool {
	return h.cfg.DisableTelemetry || h.cfg.Version == "" || h.cfg.Version == "dev"
}

func (h *Hub) telemetrySettings() TelemetrySettings {
	var t TelemetrySettings
	if err := h.store.GetSetting(keyTelemetry, &t); err != nil && !errors.Is(err, store.ErrNotFound) {
		h.log.Warn("read telemetry settings", "err", err)
	}
	return t
}

func (h *Hub) telemetryReport(id string) telemetryReport {
	h.mu.Lock()
	defer h.mu.Unlock()
	r := telemetryReport{ID: id, Version: h.cfg.Version, Agents: len(h.agents)}
	for _, st := range h.monitors {
		if !st.m.Paused {
			r.Monitors++
		}
	}
	return r
}

func (h *Hub) wakeTelemetry() {
	select {
	case h.telemetry.wake <- struct{}{}:
	default:
	}
}

func jitter(d time.Duration) time.Duration {
	n, err := rand.Int(rand.Reader, big.NewInt(int64(d)))
	if err != nil {
		return 0
	}
	return time.Duration(n.Int64())
}

func (h *Hub) telemetryLoop(ctx context.Context) {
	// First report a few minutes after start, so restarts don't all line up.
	wait := time.Minute + jitter(telemetryJitter)
	for {
		select {
		case <-ctx.Done():
			return
		case <-h.telemetry.wake:
			// Just turned on: send soon (wake also fires when turned off).
			wait = 10 * time.Second
			continue
		case <-time.After(wait):
		}
		wait = telemetryEvery + jitter(telemetryJitter)
		t := h.telemetrySettings()
		if !t.Enabled || t.InstanceID == "" || h.telemetryForcedOff() {
			continue
		}
		h.sendTelemetry(ctx, h.telemetryReport(t.InstanceID))
	}
}

func (h *Hub) sendTelemetry(ctx context.Context, r telemetryReport) {
	err := h.postTelemetry(ctx, r)
	h.telemetry.mu.Lock()
	defer h.telemetry.mu.Unlock()
	if err != nil {
		// No retry queue: the next report an hour later replaces this one.
		h.telemetry.err = err.Error()
		h.log.Debug("usage stats", "err", err)
		return
	}
	h.telemetry.err = ""
	h.telemetry.sentAt = time.Now()
}

func (h *Hub) postTelemetry(ctx context.Context, r telemetryReport) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	body, _ := json.Marshal(r)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telemetryURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pingtxt")
	res, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	res.Body.Close()
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("stats service: %s", res.Status)
	}
	return nil
}

type telemetryView struct {
	Enabled   bool `json:"enabled"`
	Asked     bool `json:"asked"`
	ForcedOff bool `json:"forced_off"`
	// Report is exactly what the next report sends (null when off).
	Report *telemetryReport `json:"report"`
	SentAt int64            `json:"sent_at"`
	Error  string           `json:"error,omitempty"`
}

func (h *Hub) telemetryStatus() telemetryView {
	t := h.telemetrySettings()
	v := telemetryView{Enabled: t.Enabled, Asked: t.Asked, ForcedOff: h.telemetryForcedOff()}
	if v.ForcedOff {
		v.Enabled = false
		return v
	}
	if t.Enabled && t.InstanceID != "" {
		r := h.telemetryReport(t.InstanceID)
		v.Report = &r
	}
	h.telemetry.mu.Lock()
	defer h.telemetry.mu.Unlock()
	if !h.telemetry.sentAt.IsZero() && t.Enabled {
		v.SentAt = h.telemetry.sentAt.UnixMilli()
	}
	if t.Enabled {
		v.Error = h.telemetry.err
	}
	return v
}

func (h *Hub) getTelemetry(w http.ResponseWriter, _ *http.Request, _ *store.User) {
	writeJSON(w, 200, h.telemetryStatus())
}

// putTelemetry records an admin's answer: from the opt-in question or the
// switch in the settings.
func (h *Hub) putTelemetry(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var in struct {
		Enabled bool `json:"enabled"`
	}
	if readJSON(r, &in) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	if in.Enabled && h.telemetryForcedOff() {
		writeErr(w, 409, "usage stats are turned off on this hub (PINGTXT_DISABLE_TELEMETRY or a development build)")
		return
	}
	t := h.telemetrySettings()
	t.Asked = true
	if in.Enabled && t.InstanceID == "" {
		t.InstanceID = rand.Text()
	}
	t.Enabled = in.Enabled
	if err := h.store.SetSetting(keyTelemetry, t); err != nil {
		h.internalErr(w, err)
		return
	}
	h.telemetry.mu.Lock()
	h.telemetry.sentAt, h.telemetry.err = time.Time{}, ""
	h.telemetry.mu.Unlock()
	if in.Enabled {
		h.wakeTelemetry()
	}
	writeJSON(w, 200, h.telemetryStatus())
}

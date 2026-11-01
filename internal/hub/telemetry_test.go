package hub

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/federico-it/pingtxt/internal/store"
)

func putTelemetryReq(t *testing.T, h *Hub, enabled bool) (int, telemetryView) {
	t.Helper()
	body := `{"enabled":false}`
	if enabled {
		body = `{"enabled":true}`
	}
	w := httptest.NewRecorder()
	h.putTelemetry(w, httptest.NewRequest("PUT", "/api/telemetry", strings.NewReader(body)), nil)
	var v telemetryView
	_ = json.NewDecoder(w.Body).Decode(&v)
	return w.Code, v
}

func TestTelemetry(t *testing.T) {
	var got []telemetryReport
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var rep telemetryReport
		if err := json.NewDecoder(r.Body).Decode(&rep); err != nil {
			t.Error(err)
		}
		got = append(got, rep)
	}))
	defer srv.Close()
	old := telemetryURL
	telemetryURL = srv.URL
	defer func() { telemetryURL = old }()

	h, st := newTestHub(t)
	h.cfg.Version = "v0.4.0"

	// Not asked yet and off.
	if v := h.telemetryStatus(); v.Asked || v.Enabled || v.Report != nil {
		t.Fatalf("initial: %+v", v)
	}

	for i, paused := range []bool{false, false, true} {
		m := &store.Monitor{Name: "m", Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Paused: paused, Locations: []string{LocalLocation}}
		if err := st.CreateMonitor(m); err != nil {
			t.Fatal(i, err)
		}
		h.MonitorChanged(m.ID)
	}

	code, v := putTelemetryReq(t, h, true)
	if code != 200 || !v.Asked || !v.Enabled || v.Report == nil {
		t.Fatalf("on: %d %+v", code, v)
	}
	id := v.Report.ID
	if len(id) != 26 || v.Report.Monitors != 2 || v.Report.Agents != 0 || v.Report.Version != "v0.4.0" {
		t.Fatalf("report: %+v", v.Report)
	}

	h.sendTelemetry(context.Background(), *v.Report)
	if len(got) != 1 || got[0] != *v.Report {
		t.Fatalf("sent: %+v", got)
	}
	if v := h.telemetryStatus(); v.SentAt == 0 || v.Error != "" {
		t.Fatalf("after send: %+v", v)
	}

	// Off and on again keeps the same id.
	if _, v := putTelemetryReq(t, h, false); v.Enabled || !v.Asked || v.Report != nil {
		t.Fatalf("off: %+v", v)
	}
	if _, v := putTelemetryReq(t, h, true); v.Report == nil || v.Report.ID != id {
		t.Fatalf("on again: %+v", v)
	}

	// The environment and development builds turn it off for good.
	h.cfg.DisableTelemetry = true
	if code, _ := putTelemetryReq(t, h, true); code != 409 {
		t.Fatalf("forced off: %d", code)
	}
	if v := h.telemetryStatus(); !v.ForcedOff || v.Enabled || v.Report != nil {
		t.Fatalf("forced off: %+v", v)
	}
	h.cfg.DisableTelemetry, h.cfg.Version = false, "dev"
	if v := h.telemetryStatus(); !v.ForcedOff {
		t.Fatalf("dev: %+v", v)
	}
}

package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestValidateStatuspage(t *testing.T) {
	h, _ := newTestHub(t)
	cases := []struct {
		target, keyword    string
		ok                 bool
		wantTarget, wantKW string
	}{
		{"status.example.com", "", true, "https://status.example.com", ""},
		{"https://status.example.com/api/v2/status.json", " b1 ,a2,, b1", true, "https://status.example.com", "a2, b1"},
		{"ftp://status.example.com", "", false, "", ""},
		{"https://status.example.com", "a b", false, "", ""},
	}
	for _, c := range cases {
		m := &store.Monitor{Name: "x", Type: check.TypeStatuspage, Target: c.target, Keyword: c.keyword, Method: "POST", Body: "x",
			Headers: map[string]string{"A": "b"}, Assertions: []check.Assertion{{Path: "a", Op: "exists"}}}
		err := h.validateMonitor(m)
		if (err == nil) != c.ok {
			t.Errorf("%q %q: err = %v, want ok=%v", c.target, c.keyword, err, c.ok)
			continue
		}
		if c.ok && (m.Target != c.wantTarget || m.Keyword != c.wantKW || m.Method != "" || m.Body != "" || len(m.Headers) != 0 || len(m.Assertions) != 0) {
			t.Errorf("%q: got %+v", c.target, m)
		}
	}
}

// A status page with a minor problem makes the monitor degraded, not down.
func TestStatuspageDegraded(t *testing.T) {
	h, st := newTestHub(t)
	m := &store.Monitor{Name: "m", Type: check.TypeStatuspage, Target: "https://status.example.com", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	steps := []struct {
		ok, degraded bool
		want         string
	}{
		{true, false, StatusUp},
		{true, true, StatusDegraded},
		{false, false, StatusDown},
		{true, true, StatusDegraded},
		{true, false, StatusUp},
	}
	for i, s := range steps {
		r := result(m.ID, s.ok)
		r.Degraded = s.degraded
		r.Message = "Minor Service Outage"
		h.HandleResult(LocalLocation, r)
		if got := status(t, st, m.ID); got != s.want {
			t.Errorf("step %d: status %s, want %s", i, got, s.want)
		}
		if got := h.DegradedLocations(m.ID)[LocalLocation]; got != s.degraded {
			t.Errorf("step %d: location degraded = %v", i, got)
		}
	}
}

func TestDetectStatuspage(t *testing.T) {
	h, _ := newTestHub(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/summary.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"page":{"name":"Example"},"status":{"indicator":"minor","description":"Minor Service Outage"},
			"components":[{"id":"g","name":"Cloud","group":true,"status":"operational"},
			{"id":"a","name":"API","status":"operational"},{"id":"b","name":"AWS","group_id":"g","status":"partial_outage"}]}`))
	}))
	defer srv.Close()
	ask := func(target string) statuspageInfo {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/monitors/statuspage", strings.NewReader(`{"target":"`+target+`"}`))
		w := httptest.NewRecorder()
		h.detectStatuspage(w, r, nil)
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", target, w.Code, w.Body)
		}
		var info statuspageInfo
		if err := json.Unmarshal(w.Body.Bytes(), &info); err != nil {
			t.Fatal(err)
		}
		return info
	}
	info := ask(srv.URL + "/api/v2/status.json")
	if !info.Statuspage || info.URL != srv.URL || info.Name != "Example" || info.Indicator != "minor" || len(info.Components) != 2 ||
		info.Components[1] != (statuspageComponent{ID: "b", Name: "AWS", Group: "Cloud", Status: "partial_outage"}) {
		t.Errorf("page: %+v", info)
	}
	if info := ask(srv.URL + "/other"); info.Statuspage || info.Error == "" {
		t.Errorf("not a page: %+v", info)
	}
}

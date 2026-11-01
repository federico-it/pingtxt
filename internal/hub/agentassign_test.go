package hub

import (
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestLoopbackTarget(t *testing.T) {
	for target, want := range map[string]bool{
		"http://localhost:8080/health": true,
		"https://127.0.0.1":            true,
		"[::1]:5432":                   true,
		"localhost":                    true,
		"api.localhost:80":             true,
		"postgres://u:p@127.0.0.1/db":  true,
		"https://example.com":          false,
		"10.0.0.5:22":                  false,
		"example.com":                  false,
		"[2001:db8::1]:443":            false,
	} {
		if got := loopbackTarget(target); got != want {
			t.Errorf("loopbackTarget(%q) = %v, want %v", target, got, want)
		}
	}
}

func TestAssignAgentToCompatibleMonitors(t *testing.T) {
	h, st := newTestHub(t)
	noV6, _ := st.CreateAgent("ams", "Amsterdam", "h1")
	h.agents[noV6.ID] = &agentConn{agent: noV6, done: make(chan struct{})}
	h.agents[noV6.ID].ipv6.Store(check.IPv6No)
	loc := AgentLocation(noV6.ID)

	mk := func(name, typ, target string, fam int, locs ...string) *store.Monitor {
		if len(locs) == 0 {
			locs = []string{LocalLocation}
		}
		m := &store.Monitor{Name: name, Type: typ, Target: target, IntervalSec: 60, Retries: 1, IPFamily: fam, Locations: locs, Headers: map[string]string{}}
		if err := st.CreateMonitor(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	web := mk("web", "http", "https://example.com", ipEither)
	both := mk("both", "http", "https://example.com", check.IPBoth)
	v6 := mk("v6", "ping", "example.com", check.IPv6)
	v4 := mk("v4", "tcp", "example.com:22", check.IPv4)
	mk("docker", "docker", "nginx", ipEither)
	mk("lo", "http", "http://localhost:3000", ipEither)
	mk("push", "push", "", ipEither)
	mk("has", "http", "https://example.org", ipEither, LocalLocation, loc)

	req := func(method, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/api/agents/"+strconv.FormatInt(noV6.ID, 10)+"/monitors", strings.NewReader(body))
		r.SetPathValue("id", strconv.FormatInt(noV6.ID, 10))
		w := httptest.NewRecorder()
		if method == "GET" {
			h.agentMonitors(w, r, nil)
		} else {
			h.assignAgent(w, r, nil)
		}
		if w.Code != 200 {
			t.Fatalf("%s: %d %s", method, w.Code, w.Body)
		}
		return w
	}

	var plan assignPlan
	_ = json.Unmarshal(req("GET", "").Body.Bytes(), &plan)
	names := func(ms []assignMonitor) (out []string) {
		for _, m := range ms {
			out = append(out, m.Name+":"+m.Reason)
		}
		return out
	}
	if got := names(plan.Compatible); !slices.Equal(got, []string{"v4:", "web:"}) {
		t.Errorf("compatible %v", got)
	}
	if got := names(plan.Skipped); !slices.Equal(got, []string{"both:no_ipv6", "docker:docker", "lo:loopback", "v6:no_ipv6"}) {
		t.Errorf("skipped %v", got)
	}
	if plan.Already != 1 {
		t.Errorf("already %d", plan.Already)
	}

	// Incompatible ids sent anyway are skipped, not added.
	body := `{"monitor_ids":[` + strconv.FormatInt(web.ID, 10) + `,` + strconv.FormatInt(v4.ID, 10) + `,` + strconv.FormatInt(v6.ID, 10) + `,` + strconv.FormatInt(web.ID, 10) + `]}`
	var res struct {
		Added   int             `json:"added"`
		Skipped []assignMonitor `json:"skipped"`
	}
	_ = json.Unmarshal(req("POST", body).Body.Bytes(), &res)
	if res.Added != 2 || len(res.Skipped) != 1 || res.Skipped[0].ID != v6.ID {
		t.Fatalf("assign: %+v", res)
	}
	for _, m := range []*store.Monitor{web, v4} {
		got, _ := st.Monitor(m.ID)
		if !slices.Equal(got.Locations, []string{loc, LocalLocation}) {
			t.Errorf("%s locations %v", m.Name, got.Locations)
		}
	}
	if got, _ := st.Monitor(both.ID); slices.Contains(got.Locations, loc) {
		t.Errorf("both got the agent")
	}

	// Unknown IPv6: IPv6 monitors wait for the agent to report.
	h.agents[noV6.ID].ipv6.Store(check.IPv6Unknown)
	if r := h.assignSkipReason(v6, loc); r != skipIPv6Unknown {
		t.Errorf("unknown ipv6: %q", r)
	}
	h.agents[noV6.ID].ipv6.Store(check.IPv6Yes)
	if r := h.assignSkipReason(v6, loc); r != "" {
		t.Errorf("ipv6 yes: %q", r)
	}
}

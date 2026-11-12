package check

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestStatuspageBase(t *testing.T) {
	cases := map[string]string{
		"status.claude.com":                                 "https://status.claude.com",
		"https://status.claude.com/":                        "https://status.claude.com",
		"https://status.claude.com/api/v2/status.json":      "https://status.claude.com",
		"https://status.claude.com/api/v2/summary.json?x=1": "https://status.claude.com",
		"http://example.com/status/history#a":               "http://example.com/status",
	}
	for in, want := range cases {
		got, err := StatuspageBase(in)
		if err != nil || got != want {
			t.Errorf("StatuspageBase(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "ftp://x.com", "https://"} {
		if _, err := StatuspageBase(bad); err == nil {
			t.Errorf("StatuspageBase(%q): want error", bad)
		}
	}
}

func summary(indicator string, comps map[string]string, incidents ...statuspageIncident) *StatuspageSummary {
	s := &StatuspageSummary{}
	s.Status.Indicator = indicator
	s.Status.Description = "desc " + indicator
	for id, st := range comps {
		s.Components = append(s.Components, StatuspageComponent{ID: id, Name: "C-" + id, Status: st})
	}
	s.Incidents = incidents
	return s
}

func TestStatuspageEvaluatePage(t *testing.T) {
	cases := []struct {
		ind          string
		ok, degraded bool
	}{
		{"none", true, false}, {"minor", true, true}, {"major", false, false}, {"critical", false, false}, {"maintenance", true, false},
	}
	for _, c := range cases {
		r := summary(c.ind, nil).Evaluate(nil)
		if r.OK != c.ok || r.Degraded != c.degraded {
			t.Errorf("%s: ok=%v degraded=%v", c.ind, r.OK, r.Degraded)
		}
	}
	if r := summary("none", nil).Evaluate(nil); r.Message != "" {
		t.Errorf("operational page: message %q", r.Message)
	}
	in := statuspageIncident{Name: "Elevated errors", Status: "identified"}
	r := summary("minor", nil, in, statuspageIncident{Name: "Old", Status: "resolved"}).Evaluate(nil)
	if r.Message != "desc minor · Incident: Elevated errors (identified)" {
		t.Errorf("message %q", r.Message)
	}
}

func TestStatuspageEvaluateComponents(t *testing.T) {
	comps := map[string]string{"a": spOperational, "b": spPartial, "c": spMajor, "d": spMaintenance}
	inc := statuspageIncident{Name: "B broken", Status: "investigating"}
	inc.Components = append(inc.Components, struct {
		ID string `json:"id"`
	}{"b"})
	s := summary("major", comps, inc)

	if r := s.Evaluate([]string{"a"}); !r.OK || r.Degraded || r.Message != "" {
		t.Errorf("a: %+v", r)
	}
	if r := s.Evaluate([]string{"a", "b"}); !r.OK || !r.Degraded || r.Message != "C-b: partial outage · Incident: B broken (investigating)" {
		t.Errorf("a,b: %+v", r)
	}
	if r := s.Evaluate([]string{"b", "c"}); r.OK || r.Degraded {
		t.Errorf("b,c: %+v", r)
	}
	if r := s.Evaluate([]string{"d"}); !r.OK || r.Degraded || !strings.Contains(r.Message, "under maintenance") {
		t.Errorf("d: %+v", r)
	}
	if r := s.Evaluate([]string{"a", "gone"}); !r.OK || !strings.Contains(r.Message, "gone") {
		t.Errorf("a,gone: %+v", r)
	}
	if r := s.Evaluate([]string{"gone"}); r.OK {
		t.Errorf("gone: %+v", r)
	}
}

func TestRunStatuspage(t *testing.T) {
	fixture, err := os.ReadFile("testdata/statuspage_summary.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/summary.json":
			_, _ = w.Write(fixture)
		case "/other/api/v2/summary.json":
			_, _ = w.Write([]byte(`{"hello": "world"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	// The real page of status.claude.com with a minor outage of claude.ai
	// and Claude Cowork and the API operational.
	r := Run(context.Background(), Job{Type: TypeStatuspage, Target: srv.URL + "/api/v2/status.json"})
	if !r.OK || !r.Degraded || !strings.Contains(r.Message, "Microsoft 365 connector") {
		t.Errorf("page: %+v", r)
	}
	r = Run(context.Background(), Job{Type: TypeStatuspage, Target: srv.URL, Keyword: "k8w3r06qmzrp"})
	if !r.OK || r.Degraded || r.Message != "" {
		t.Errorf("API component: %+v", r)
	}
	r = Run(context.Background(), Job{Type: TypeStatuspage, Target: srv.URL, Keyword: "k8w3r06qmzrp, rwppv331jlwc"})
	if !r.OK || !r.Degraded || !strings.HasPrefix(r.Message, "claude.ai: partial outage") {
		t.Errorf("claude.ai component: %+v", r)
	}
	if r = Run(context.Background(), Job{Type: TypeStatuspage, Target: srv.URL + "/other"}); r.OK || !strings.Contains(r.Message, "not an Atlassian") {
		t.Errorf("not a page: %+v", r)
	}
	if r = Run(context.Background(), Job{Type: TypeStatuspage, Target: srv.URL + "/missing"}); r.OK || r.StatusCode != 404 {
		t.Errorf("404: %+v", r)
	}
}

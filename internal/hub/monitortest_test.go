package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

func TestMonitorTestAndBulk(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bad" {
			w.WriteHeader(500)
		}
	}))
	defer target.Close()
	cookie := login(t, h, st)
	do := func(method, path, body string, out any) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		if out != nil {
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s %s: %s", method, path, b)
			}
		}
		return res.StatusCode
	}
	type results struct {
		Results []testResult `json:"results"`
	}

	// An unsaved monitor is checked at its locations; offline agents are skipped.
	ag, err := st.CreateAgent("fra", "Frankfurt", "")
	if err != nil {
		t.Fatal(err)
	}
	var got results
	body := `{"type":"http","target":"` + target.URL + `","timeout_ms":2000,"locations":["local","agent:` + itoa(ag.ID) + `"]}`
	if c := do("POST", "/api/monitors/test", body, &got); c != 200 || len(got.Results) != 2 {
		t.Fatalf("test draft: %d %+v", c, got)
	}
	for _, r := range got.Results {
		if r.Location == LocalLocation && (!r.OK || r.StatusCode != 200 || r.Name != "hub") {
			t.Fatalf("local result: %+v", r)
		}
		if r.Location != LocalLocation && (!r.Skipped || r.OK) {
			t.Fatalf("offline agent: %+v", r)
		}
	}
	if c := do("POST", "/api/monitors/test", `{"type":"push"}`, nil); c != 400 {
		t.Fatalf("push draft: want 400, got %d", c)
	}

	// A saved monitor is checked without storing a result.
	var m store.Monitor
	if c := do("POST", "/api/monitors", `{"name":"bad","type":"http","target":"`+target.URL+`/bad","locations":["local"]}`, &m); c != 201 {
		t.Fatalf("create: %d", c)
	}
	got = results{}
	if c := do("POST", "/api/monitors/"+itoa(m.ID)+"/test", "", &got); c != 200 || len(got.Results) != 1 || got.Results[0].OK {
		t.Fatalf("test saved: %d %+v", c, got)
	}
	if rows, _ := st.RecentResults(m.ID, 10); len(rows) != 0 {
		t.Fatalf("test result was stored: %+v", rows)
	}

	// Bulk changes.
	var m2 store.Monitor
	do("POST", "/api/monitors", `{"name":"two","type":"tcp","target":"localhost:1"}`, &m2)
	var g store.Group
	do("POST", "/api/groups", `{"name":"web"}`, &g)
	ids := `[` + itoa(m.ID) + `,` + itoa(m2.ID) + `]`
	if c := do("POST", "/api/monitors/bulk", `{"ids":`+ids+`,"action":"pause"}`, nil); c != 200 {
		t.Fatalf("bulk pause: %d", c)
	}
	for _, id := range []int64{m.ID, m2.ID} {
		if mm, _ := st.Monitor(id); !mm.Paused {
			t.Fatalf("monitor %d not paused", id)
		}
	}
	if c := do("POST", "/api/monitors/bulk", `{"ids":`+ids+`,"action":"group","group_id":`+itoa(g.ID)+`}`, nil); c != 200 {
		t.Fatalf("bulk group: %d", c)
	}
	if mm, _ := st.Monitor(m2.ID); mm.GroupID != g.ID || !mm.Paused {
		t.Fatalf("bulk group: %+v", mm)
	}
	if c := do("POST", "/api/monitors/bulk", `{"ids":`+ids+`,"action":"group","group_id":999}`, nil); c != 400 {
		t.Fatalf("unknown group: want 400, got %d", c)
	}
	if c := do("POST", "/api/monitors/bulk", `{"ids":[999],"action":"pause"}`, nil); c != 400 {
		t.Fatalf("unknown monitor: want 400, got %d", c)
	}
	if c := do("POST", "/api/monitors/bulk", `{"ids":`+ids+`,"action":"explode"}`, nil); c != 400 {
		t.Fatalf("bad action: want 400, got %d", c)
	}
	if c := do("POST", "/api/monitors/bulk", `{"ids":`+ids+`,"action":"resume"}`, nil); c != 200 {
		t.Fatalf("bulk resume: %d", c)
	}
	if mm, _ := st.Monitor(m.ID); mm.Paused {
		t.Fatal("not resumed")
	}
	if c := do("POST", "/api/monitors/bulk", `{"ids":`+ids+`,"action":"delete"}`, nil); c != 200 {
		t.Fatalf("bulk delete: %d", c)
	}
	if ms, _ := st.ListMonitors(); len(ms) != 0 {
		t.Fatalf("left after delete: %d", len(ms))
	}
}

func TestTestChecksDeliver(t *testing.T) {
	var tc testChecks
	if tc.deliver(check.Result{JobID: "1-2"}) {
		t.Fatal("normal result taken as a test")
	}
	// A late answer to a finished test is still kept away from the monitor.
	if !tc.deliver(check.Result{JobID: testJobPrefix + "gone"}) {
		t.Fatal("late test result not recognized")
	}
	ch := tc.add(testJobPrefix + "x")
	if !tc.deliver(check.Result{JobID: testJobPrefix + "x", OK: true}) {
		t.Fatal("test result not delivered")
	}
	if r := <-ch; !r.OK {
		t.Fatal("wrong result")
	}
	tc.remove(testJobPrefix + "x")
}

func TestCheckAgentSupport(t *testing.T) {
	m := &store.Monitor{Assertions: []check.Assertion{{}}}
	if r := checkAgentSupport(m, "agent:1", check.IPAny, check.Result{OK: true}); r.OK {
		t.Fatal("old agent passed response rules")
	}
	if r := checkAgentSupport(m, LocalLocation, check.IPAny, check.Result{OK: true}); !r.OK {
		t.Fatal("local result changed")
	}
}

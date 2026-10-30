package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestGroupsAndTags(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}, "status/index.html": {Data: []byte("status")}}))
	defer srv.Close()
	cookie := login(t, h, st)
	do := func(method, path, body string, out any) int {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
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

	var g store.Group
	if c := do("POST", "/api/groups", `{"name":" Web "}`, &g); c != 201 || g.Name != "Web" {
		t.Fatalf("create group: %d %+v", c, g)
	}
	if c := do("POST", "/api/groups", `{"name":"web"}`, nil); c != 409 {
		t.Fatalf("duplicate group (case-insensitive): want 409, got %d", c)
	}
	var tag store.Tag
	if c := do("POST", "/api/tags", `{"name":"prod","color":"red"}`, &tag); c != 201 {
		t.Fatalf("create tag: %d", c)
	}
	if c := do("POST", "/api/tags", `{"name":"x","color":"chartreuse"}`, nil); c != 400 {
		t.Fatalf("bad color: want 400, got %d", c)
	}

	// Monitors carry their group and tags; unknown ones are refused.
	var m store.Monitor
	body := `{"name":"api","type":"http","target":"http://a","group_id":` + itoa(g.ID) + `,"tag_ids":[` + itoa(tag.ID) + `,` + itoa(tag.ID) + `]}`
	if c := do("POST", "/api/monitors", body, &m); c != 201 || m.GroupID != g.ID || len(m.TagIDs) != 1 {
		t.Fatalf("create monitor: %d %+v", c, m)
	}
	if c := do("POST", "/api/monitors", `{"name":"x","type":"http","target":"http://a","tag_ids":[999]}`, nil); c != 400 {
		t.Fatalf("unknown tag: want 400, got %d", c)
	}
	var push store.Monitor
	if c := do("POST", "/api/monitors", `{"name":"cron","type":"push","tag_ids":[`+itoa(tag.ID)+`]}`, &push); c != 201 || len(push.TagIDs) != 1 {
		t.Fatalf("push monitor keeps tags: %d %+v", c, push)
	}
	var other store.Monitor
	do("POST", "/api/monitors", `{"name":"db","type":"tcp","target":"db:5432"}`, &other)

	// Filters.
	var list []map[string]any
	do("GET", "/api/monitors?group="+itoa(g.ID), "", &list)
	if len(list) != 1 {
		t.Fatalf("group filter: want 1, got %d", len(list))
	}
	do("GET", "/api/monitors?group=none", "", &list)
	if len(list) != 2 {
		t.Fatalf("no-group filter: want 2, got %d", len(list))
	}
	do("GET", "/api/monitors?tag="+itoa(tag.ID), "", &list)
	if len(list) != 2 {
		t.Fatalf("tag filter: want 2, got %d", len(list))
	}

	// Set group members in bulk.
	if c := do("PUT", "/api/groups/"+itoa(g.ID)+"/monitors", `{"monitor_ids":[`+itoa(other.ID)+`]}`, &g); c != 200 || g.Monitors != 1 {
		t.Fatalf("set members: %d %+v", c, g)
	}
	if got, _ := st.Monitor(m.ID); got.GroupID != 0 {
		t.Fatal("monitor left out should leave the group")
	}

	// Maintenance by group covers members, by tag covers tagged monitors.
	now := time.Now()
	var mw store.Maintenance
	mb := `{"name":"w","kind":"once","group_ids":[` + itoa(g.ID) + `],"start_at":` + itoa(now.Add(-time.Minute).UnixMilli()) + `,"end_at":` + itoa(now.Add(time.Hour).UnixMilli()) + `,"enabled":true}`
	if c := do("POST", "/api/maintenances", mb, &mw); c != 201 {
		t.Fatalf("create maintenance: %d", c)
	}
	if s := status(t, st, other.ID); s != StatusMaintenance {
		t.Fatalf("group member: want maintenance, got %s", s)
	}
	if s := status(t, st, m.ID); s == StatusMaintenance {
		t.Fatal("non-member must not be in maintenance")
	}
	if c := do("POST", "/api/maintenances", `{"name":"w","kind":"once","start_at":1,"end_at":2}`, nil); c != 400 {
		t.Fatalf("empty selection: want 400, got %d", c)
	}

	// Moving a monitor into the group puts it in maintenance right away.
	do("PUT", "/api/groups/"+itoa(g.ID)+"/monitors", `{"monitor_ids":[`+itoa(other.ID)+`,`+itoa(m.ID)+`]}`, nil)
	if s := status(t, st, m.ID); s != StatusMaintenance {
		t.Fatalf("new member: want maintenance, got %s", s)
	}

	// Deleting the group ends its maintenance and keeps the monitors.
	if c := do("DELETE", "/api/groups/"+itoa(g.ID), "", nil); c != 200 {
		t.Fatalf("delete group: %d", c)
	}
	if s := status(t, st, m.ID); s == StatusMaintenance {
		t.Fatal("deleted group must not keep monitors in maintenance")
	}
	w, _ := st.Maintenance(mw.ID)
	if len(w.GroupIDs) != 0 {
		t.Fatalf("deleted group should be removed from windows, got %v", w.GroupIDs)
	}
	if got, err := st.Monitor(m.ID); err != nil || got.GroupID != 0 {
		t.Fatalf("monitor should survive without group: %v %+v", err, got)
	}

	// Deleting a tag removes it from monitors.
	do("DELETE", "/api/tags/"+itoa(tag.ID), "", nil)
	if got, _ := st.Monitor(push.ID); len(got.TagIDs) != 0 {
		t.Fatalf("tag should be gone, got %v", got.TagIDs)
	}
}

func TestStatusPageGroups(t *testing.T) {
	h, st := newTestHub(t)
	web := &store.Group{Name: "Web"}
	db := &store.Group{Name: "Databases"}
	for _, g := range []*store.Group{web, db} {
		if err := st.CreateGroup(g); err != nil {
			t.Fatal(err)
		}
	}
	mk := func(name string, group int64) *store.Monitor {
		m := &store.Monitor{Name: name, Type: "http", Target: "http://x", IntervalSec: 60, Retries: 1, Locations: []string{LocalLocation}, GroupID: group}
		if err := st.CreateMonitor(m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	solo := mk("solo", 0)
	site := mk("site", web.ID)
	mk("postgres", db.ID)
	mk("redis", db.ID)
	_ = mk("api", web.ID) // not on the page

	sp := &store.StatusPage{Slug: "s", Title: "S", Published: true, MonitorIDs: []int64{site.ID, solo.ID}, GroupIDs: []int64{db.ID}}
	if err := st.CreateStatusPage(sp); err != nil {
		t.Fatal(err)
	}
	got, _ := st.StatusPage(sp.ID)
	if len(got.GroupIDs) != 1 || got.GroupIDs[0] != db.ID {
		t.Fatalf("group ids not stored: %+v", got)
	}
	p, err := h.buildPublicPage(got)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, m := range p.Monitors {
		names = append(names, m.Group+"/"+m.Name)
	}
	want := "Web/site,/solo,Databases/postgres,Databases/redis"
	if strings.Join(names, ",") != want {
		t.Fatalf("sections: want %s, got %s", want, strings.Join(names, ","))
	}

	// A monitor added to the group later shows up without editing the page.
	mk("mysql", db.ID)
	p, _ = h.buildPublicPage(got)
	if len(p.Monitors) != 5 {
		t.Fatalf("new group member should appear, got %d monitors", len(p.Monitors))
	}
}

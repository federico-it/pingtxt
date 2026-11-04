package hub

import (
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestSubgroups(t *testing.T) {
	h, st := newTestHub(t)
	mk := func(name string, parent int64) *store.Group {
		g := &store.Group{Name: name, ParentID: parent}
		if err := st.CreateGroup(g); err != nil {
			t.Fatal(err)
		}
		return g
	}
	project := mk("project", 0)
	api := mk("api", project.ID)
	db := mk("db", api.ID)
	other := mk("other", 0)

	save := func(method string, id int64, body string) int {
		path := "/api/groups"
		if id != 0 {
			path += "/" + strconv.FormatInt(id, 10)
		}
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if id != 0 {
			r.SetPathValue("id", strconv.FormatInt(id, 10))
		}
		w := httptest.NewRecorder()
		h.saveGroup(w, r, nil)
		return w.Code
	}
	pid := func(id int64) string { return strconv.FormatInt(id, 10) }

	// A fourth level, a cycle and an unknown parent are refused.
	if c := save("POST", 0, `{"name":"deep","parent_id":`+pid(db.ID)+`}`); c != 400 {
		t.Errorf("fourth level: %d, want 400", c)
	}
	if c := save("PUT", project.ID, `{"parent_id":`+pid(db.ID)+`}`); c != 400 {
		t.Errorf("cycle: %d, want 400", c)
	}
	if c := save("PUT", other.ID, `{"parent_id":999}`); c != 400 {
		t.Errorf("unknown parent: %d, want 400", c)
	}
	// Moving a group with two levels below it under another top group: 4 levels.
	if c := save("PUT", project.ID, `{"parent_id":`+pid(other.ID)+`}`); c != 400 {
		t.Errorf("too deep after move: %d, want 400", c)
	}
	// PUT without parent_id keeps it (older clients).
	if c := save("PUT", api.ID, `{"name":"api","description":"x"}`); c != 200 {
		t.Fatalf("rename: %d", c)
	}
	if g, _ := st.Group(api.ID); g.ParentID != project.ID {
		t.Fatalf("parent lost on PUT without parent_id: %d", g.ParentID)
	}

	groups, _ := st.ListGroups()
	tree := store.NewGroupTree(groups)
	if got := tree.Expand([]int64{project.ID}); len(got) != 3 {
		t.Errorf("expand project = %v, want 3 groups", got)
	}
	if got := tree.Path(db.ID, " › "); got != "project › api › db" {
		t.Errorf("path = %q", got)
	}

	// A maintenance window on the top group covers a monitor two levels down.
	m := &store.Monitor{Name: "pg", Type: "tcp", Target: "db:5432", IntervalSec: 60, Retries: 1, GroupID: db.ID, Locations: []string{LocalLocation}, Headers: map[string]string{}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	w := &store.Maintenance{Name: "w", Kind: "once", StartAt: 1, EndAt: 2, Enabled: true, GroupIDs: []int64{project.ID}, MonitorIDs: []int64{}, TagIDs: []int64{}}
	if err := st.CreateMaintenance(w); err != nil {
		t.Fatal(err)
	}
	if err := h.MaintenancesChanged(); err != nil {
		t.Fatal(err)
	}
	h.mu.Lock()
	covered := h.maints[0].Covers(m)
	h.mu.Unlock()
	if !covered {
		t.Error("window on the parent group does not cover the subgroup's monitor")
	}
	if stored, _ := st.ListMaintenances(); len(stored[0].GroupIDs) != 1 {
		t.Errorf("stored window groups changed: %v", stored[0].GroupIDs)
	}

	// Deleting the middle group moves its subgroup up.
	if err := st.DeleteGroup(api.ID); err != nil {
		t.Fatal(err)
	}
	if g, _ := st.Group(db.ID); g.ParentID != project.ID {
		t.Errorf("db parent after deleting api = %d, want %d", g.ParentID, project.ID)
	}
}

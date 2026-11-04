package hub

import (
	"testing"
	"time"

	"github.com/federico-it/pingtxt/internal/store"
)

func depMonitor(t *testing.T, h *Hub, st *store.Store, name string, deps ...int64) *store.Monitor {
	t.Helper()
	m := &store.Monitor{Name: name, Type: "http", Target: "http://" + name, IntervalSec: 60, Retries: 1, TimeoutMs: 1000, Locations: []string{LocalLocation}, DependsOn: deps}
	if err := h.validateMonitor(m); err != nil {
		t.Fatal(err)
	}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	h.MonitorChanged(m.ID)
	return m
}

func openIncidents(t *testing.T, st *store.Store, id int64) int {
	t.Helper()
	inc, err := st.ListIncidents(id, 10)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, i := range inc {
		if i.ResolvedAt == 0 {
			n++
		}
	}
	return n
}

func TestDependencyBlocksAlerts(t *testing.T) {
	h, st := newTestHub(t)
	server := depMonitor(t, h, st, "server")
	site := depMonitor(t, h, st, "site", server.ID)
	app := depMonitor(t, h, st, "app", site.ID) // through site

	got, err := st.Monitor(app.ID)
	if err != nil || len(got.DependsOn) != 1 || got.DependsOn[0] != site.ID {
		t.Fatalf("depends_on not stored: %+v %v", got.DependsOn, err)
	}
	for _, m := range []*store.Monitor{server, site, app} {
		h.HandleResult(LocalLocation, result(m.ID, true))
	}

	// The server goes down first: one alert, the others are blocked.
	h.HandleResult(LocalLocation, result(server.ID, false))
	h.HandleResult(LocalLocation, result(site.ID, false))
	h.HandleResult(LocalLocation, result(app.ID, false))
	h.depTick(time.Now())
	if openIncidents(t, st, server.ID) != 1 {
		t.Fatal("the server should have an incident")
	}
	for _, m := range []*store.Monitor{site, app} {
		if n := openIncidents(t, st, m.ID); n != 0 {
			t.Fatalf("%s is blocked and should have no incident, got %d", m.Name, n)
		}
		if s := status(t, st, m.ID); s != StatusDown {
			t.Fatalf("%s keeps its own status, got %s", m.Name, s)
		}
		b := h.blockedBy(m.ID)
		if b == nil || b.ID != server.ID || b.Name != "server" {
			t.Fatalf("%s should be blocked by the server, got %+v", m.Name, b)
		}
	}

	// The server is back. The site recovers, the app still fails: only
	// the app alerts, after a fresh check of its own.
	h.HandleResult(LocalLocation, result(server.ID, true))
	h.depTick(time.Now())
	if openIncidents(t, st, app.ID) != 0 {
		t.Fatal("the app must wait for a fresh check before alerting")
	}
	h.HandleResult(LocalLocation, result(site.ID, true))
	h.HandleResult(LocalLocation, result(app.ID, false))
	if openIncidents(t, st, site.ID) != 0 {
		t.Fatal("the site recovered: no incident")
	}
	if openIncidents(t, st, app.ID) != 1 {
		t.Fatal("the app still fails with its parents up: it should alert")
	}
	if h.blockedBy(app.ID) != nil {
		t.Fatal("the app is no longer blocked")
	}
}

func TestDependencyChildFailsFirst(t *testing.T) {
	h, st := newTestHub(t)
	server := depMonitor(t, h, st, "server")
	site := depMonitor(t, h, st, "site", server.ID)
	h.HandleResult(LocalLocation, result(server.ID, true))
	h.HandleResult(LocalLocation, result(site.ID, true))

	// The site fails before the server is checked again: its alert waits.
	h.HandleResult(LocalLocation, result(site.ID, false))
	if openIncidents(t, st, site.ID) != 0 {
		t.Fatal("the alert should wait for a fresh check of the server")
	}
	h.mu.Lock()
	next := h.monitors[server.ID].next
	h.mu.Unlock()
	if time.Until(next) > time.Second {
		t.Fatal("the server should be checked right away")
	}
	// The server fails too: the site stays quiet.
	h.HandleResult(LocalLocation, result(server.ID, false))
	h.depTick(time.Now())
	if openIncidents(t, st, site.ID) != 0 || openIncidents(t, st, server.ID) != 1 {
		t.Fatal("only the server should alert")
	}

	// Another time the server answers fine: the site alerts.
	h2, st2 := newTestHub(t)
	server = depMonitor(t, h2, st2, "server")
	site = depMonitor(t, h2, st2, "site", server.ID)
	h2.HandleResult(LocalLocation, result(server.ID, true))
	h2.HandleResult(LocalLocation, result(site.ID, false))
	h2.depTick(time.Now())
	if openIncidents(t, st2, site.ID) != 0 {
		t.Fatal("still waiting for the server")
	}
	h2.HandleResult(LocalLocation, result(server.ID, true))
	h2.depTick(time.Now())
	if openIncidents(t, st2, site.ID) != 1 {
		t.Fatal("the server is up: the site should alert")
	}
}

func TestDependencyHoldExpires(t *testing.T) {
	h, st := newTestHub(t)
	server := depMonitor(t, h, st, "server")
	site := depMonitor(t, h, st, "site", server.ID)
	h.HandleResult(LocalLocation, result(site.ID, false))
	if openIncidents(t, st, site.ID) != 0 {
		t.Fatal("the alert should wait")
	}
	h.depTick(time.Now().Add(depHoldMax))
	if openIncidents(t, st, site.ID) != 1 {
		t.Fatal("the server never answered: the site alerts once the wait is over")
	}
}

func TestCheckDependencies(t *testing.T) {
	h, st := newTestHub(t)
	a := depMonitor(t, h, st, "a")
	b := depMonitor(t, h, st, "b", a.ID)
	c := depMonitor(t, h, st, "c", b.ID)

	a.DependsOn = []int64{c.ID}
	if err := h.checkDependencies(a); err == nil {
		t.Fatal("a loop must be refused")
	}
	a.DependsOn = []int64{a.ID}
	if err := h.checkDependencies(a); err == nil {
		t.Fatal("a monitor cannot depend on itself")
	}
	a.DependsOn = []int64{9999}
	if err := h.checkDependencies(a); err == nil {
		t.Fatal("unknown monitors must be refused")
	}
	c.DependsOn = []int64{a.ID, b.ID, a.ID}
	if err := h.checkDependencies(c); err != nil || len(c.DependsOn) != 2 {
		t.Fatalf("got %v %v", c.DependsOn, err)
	}
	c.DependsOn = nil
	if err := h.checkDependencies(c); err != nil || c.DependsOn != nil {
		t.Fatal("nil keeps the stored dependencies")
	}

	// Deleting a parent drops the dependency.
	if err := st.DeleteMonitor(a.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := st.Monitor(b.ID)
	if len(got.DependsOn) != 0 {
		t.Fatalf("dependency on a deleted monitor kept: %v", got.DependsOn)
	}
}

func TestDependenciesExportImport(t *testing.T) {
	h, st := newTestHub(t)
	server := depMonitor(t, h, st, "server")
	depMonitor(t, h, st, "site", server.ID)
	doc, err := h.ExportConfig(false)
	if err != nil {
		t.Fatal(err)
	}
	h2, st2 := newTestHub(t)
	depMonitor(t, h2, st2, "other") // shifts the ids
	if _, err := h2.ImportConfig(doc, ImportOptions{}); err != nil {
		t.Fatal(err)
	}
	ms, err := st2.ListMonitors()
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]*store.Monitor{}
	for _, m := range ms {
		ids[m.Name] = m
	}
	site, srv := ids["site"], ids["server"]
	if site == nil || srv == nil || len(site.DependsOn) != 1 || site.DependsOn[0] != srv.ID {
		t.Fatalf("dependency not imported: %+v", site)
	}
}

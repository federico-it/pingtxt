package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestStartingHandler(t *testing.T) {
	ui := fstest.MapFS{
		"starting.html": {Data: []byte("<html>starting page</html>")},
		"sw.js":         {Data: []byte("// sw")},
	}
	s := NewStarting(ui, "v0.3.0", "v0.2.1")
	s.SetPhase(PhaseRestore)

	get := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}

	w := get("GET", "/api/health")
	if w.Code != http.StatusServiceUnavailable || w.Header().Get(HubHeader) == "" || w.Header().Get("Retry-After") == "" {
		t.Fatalf("health: %d %v", w.Code, w.Header())
	}
	var h map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &h); err != nil {
		t.Fatal(err)
	}
	if h["ok"] != false || h["starting"] != true || h["phase"] != PhaseRestore || h["previous_version"] != "v0.2.1" || h["version"] != "v0.3.0" {
		t.Fatalf("health body: %v", h)
	}

	if w := get("POST", "/api/monitors"); w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), "starting") {
		t.Fatalf("api: %d %s", w.Code, w.Body)
	}
	for _, p := range []string{"/", "/monitors/view/?id=3", "/status/main/"} {
		w := get("GET", p)
		if w.Code != http.StatusServiceUnavailable || w.Body.String() != "<html>starting page</html>" || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
			t.Fatalf("%s: %d %q", p, w.Code, w.Body)
		}
	}
	if w := get("GET", "/sw.js"); w.Code != 200 || w.Body.String() != "// sw" {
		t.Fatalf("sw.js: %d %q", w.Code, w.Body)
	}

	// Without the built UI there is still a page that reloads itself.
	s = NewStarting(nil, "dev", "")
	if w := get("GET", "/"); !strings.Contains(w.Body.String(), "refresh") {
		t.Fatalf("fallback page: %q", w.Body)
	}
}

func TestSwapHandler(t *testing.T) {
	sw := NewSwapHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	w := httptest.NewRecorder()
	sw.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	sw.Set(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w = httptest.NewRecorder()
	sw.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 204 {
		t.Fatal(w.Code)
	}
}

func TestVersionFile(t *testing.T) {
	dir := t.TempDir()
	if v := PreviousVersion(dir); v != "" {
		t.Fatalf("empty dir: %q", v)
	}
	if err := RecordVersion(dir, "v1.2.3"); err != nil {
		t.Fatal(err)
	}
	if v := PreviousVersion(dir); v != "v1.2.3" {
		t.Fatalf("got %q", v)
	}
}

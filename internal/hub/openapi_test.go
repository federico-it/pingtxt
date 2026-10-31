package hub

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
)

type openAPIOps map[string]map[string]struct {
	OperationID string `json:"operationId"`
	Access      string `json:"x-pingtxt-access"`
}

func loadOpenAPIPaths(t *testing.T) openAPIOps {
	t.Helper()
	var doc struct {
		OpenAPI string     `json:"openapi"`
		Paths   openAPIOps `json:"paths"`
	}
	if err := json.Unmarshal(openAPIDoc, &doc); err != nil {
		t.Fatalf("openapi.json: %v", err)
	}
	if doc.OpenAPI != "3.1.0" {
		t.Fatalf("openapi version %q", doc.OpenAPI)
	}
	return doc.Paths
}

var pathParamRe = regexp.MustCompile(`\{[^}]+\}`)

// Every documented operation must reach a real route, and every /api route
// registered in routes() must be documented.
func TestOpenAPIMatchesRoutes(t *testing.T) {
	h, _ := newTestHub(t)
	mux := h.routes(fstest.MapFS{})
	paths := loadOpenAPIPaths(t)

	documented := map[string]bool{} // route patterns reached by the document
	ids := map[string]string{}
	for p, ops := range paths {
		for method, op := range ops {
			name := strings.ToUpper(method) + " " + p
			if op.OperationID == "" {
				t.Errorf("%s: missing operationId", name)
			} else if prev, dup := ids[op.OperationID]; dup {
				t.Errorf("%s: operationId %q also used by %s", name, op.OperationID, prev)
			}
			ids[op.OperationID] = name
			switch op.Access {
			case "public", "read", "write", "session", "admin":
			default:
				t.Errorf("%s: x-pingtxt-access %q", name, op.Access)
			}
			req := httptest.NewRequest(strings.ToUpper(method), pathParamRe.ReplaceAllString(p, "1"), nil)
			_, pattern := mux.Handler(req)
			if pattern == "" || pattern == "/" || pattern == "/api/" {
				t.Errorf("%s: no route (matched %q)", name, pattern)
				continue
			}
			documented[pattern] = true
		}
	}

	src, err := os.ReadFile("api.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range regexp.MustCompile(`mux\.HandleFunc\("((?:[A-Z]+ )?/api/[^"]+)"`).FindAllStringSubmatch(string(src), -1) {
		if !documented[m[1]] {
			t.Errorf("route %q is not documented in openapi.json", m[1])
		}
	}
}

func TestOpenAPIServed(t *testing.T) {
	h, _ := newTestHub(t)
	h.cfg.Version = "v9.9.9"
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()

	res, err := http.Get(srv.URL + "/api/openapi.json")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status %d", res.StatusCode)
	}
	var doc struct {
		Info    struct{ Version string } `json:"info"`
		Servers []struct{ URL string }   `json:"servers"`
		Paths   map[string]any           `json:"paths"`
	}
	if err := json.NewDecoder(res.Body).Decode(&doc); err != nil {
		t.Fatal(err)
	}
	if doc.Info.Version != "v9.9.9" || len(doc.Servers) != 1 || doc.Servers[0].URL != srv.URL || doc.Paths["/api/monitors"] == nil {
		t.Fatalf("unexpected document: version %q servers %v", doc.Info.Version, doc.Servers)
	}
	// The embedded copy is not changed by serving it.
	if v, _ := parsedOpenAPI(); v["info"].(map[string]any)["version"] != "dev" {
		t.Fatal("embedded document was modified")
	}

	c := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	res2, err := c.Get(srv.URL + "/api/docs")
	if err != nil {
		t.Fatal(err)
	}
	res2.Body.Close()
	if res2.StatusCode != http.StatusFound || res2.Header.Get("Location") != "/api-docs/" {
		t.Fatalf("/api/docs: %d %q", res2.StatusCode, res2.Header.Get("Location"))
	}
}

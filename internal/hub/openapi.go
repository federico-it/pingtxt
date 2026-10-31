package hub

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"sync"
)

// openapi.json documents the REST API (OpenAPI 3.1). It is served on
// /api/openapi.json and rendered by the Swagger UI page of the web app.
//
//go:embed openapi.json
var openAPIDoc []byte

var parsedOpenAPI = sync.OnceValues(func() (map[string]any, error) {
	var doc map[string]any
	err := json.Unmarshal(openAPIDoc, &doc)
	return doc, err
})

// openAPI serves the document with the version and URL of this hub.
func (h *Hub) openAPI(w http.ResponseWriter, r *http.Request) {
	doc, err := parsedOpenAPI()
	if err != nil {
		h.internalErr(w, err)
		return
	}
	out := make(map[string]any, len(doc))
	for k, v := range doc {
		out[k] = v
	}
	info := map[string]any{}
	if src, ok := doc["info"].(map[string]any); ok {
		for k, v := range src {
			info[k] = v
		}
	}
	if h.cfg.Version != "" {
		info["version"] = h.cfg.Version
	}
	out["info"] = info
	out["servers"] = []map[string]string{{"url": h.hubURL(r), "description": "This hub"}}
	writeJSON(w, 200, out)
}

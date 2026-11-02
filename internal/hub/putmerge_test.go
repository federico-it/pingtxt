package hub

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

// A PUT keeps the fields its body leaves out, so a client that does not know
// a newer field (an older app) does not reset it.
func TestPutKeepsMissingFields(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
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
		if out != nil && res.StatusCode < 300 {
			if err := json.Unmarshal(b, out); err != nil {
				t.Fatalf("%s %s: %s", method, path, b)
			}
		}
		return res.StatusCode
	}

	var tag store.Tag
	if c := do("POST", "/api/tags", `{"name":"prod","color":"red"}`, &tag); c != 201 {
		t.Fatalf("create tag: %d", c)
	}
	var m store.Monitor
	body := `{"name":"gw","type":"ping","target":"10.0.0.1","interval_sec":60,"timeout_ms":10000,"ping_count":5,"ping_loss_pct":40,"tag_ids":[` + itoa(tag.ID) + `]}`
	if c := do("POST", "/api/monitors", body, &m); c != 201 {
		t.Fatalf("create monitor: %d", c)
	}

	// An older client: knows neither ping fields nor tags.
	if c := do("PUT", "/api/monitors/"+itoa(m.ID), `{"name":"gateway","type":"ping","target":"10.0.0.1","interval_sec":60,"timeout_ms":10000}`, &m); c != 200 {
		t.Fatalf("update monitor: %d", c)
	}
	got, err := st.Monitor(m.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "gateway" || got.PingCount != 5 || got.PingLossPct != 40 || len(got.TagIDs) != 1 {
		t.Fatalf("fields not kept: %+v", got)
	}

	// Fields that are sent still replace the stored value, empty included.
	if c := do("PUT", "/api/monitors/"+itoa(m.ID), `{"ping_loss_pct":0,"tag_ids":[]}`, nil); c != 200 {
		t.Fatalf("update monitor: %d", c)
	}
	if got, _ = st.Monitor(m.ID); got.PingLossPct != 0 || len(got.TagIDs) != 0 || got.PingCount != 5 {
		t.Fatalf("fields not replaced: %+v", got)
	}
	// Maps are replaced as a whole, not merged.
	var web store.Monitor
	if c := do("POST", "/api/monitors", `{"name":"web","type":"http","target":"https://example.com","headers":{"A":"1","B":"2"}}`, &web); c != 201 {
		t.Fatalf("create http monitor: %d", c)
	}
	if c := do("PUT", "/api/monitors/"+itoa(web.ID), `{"headers":{"A":"3"}}`, nil); c != 200 {
		t.Fatalf("update http monitor: %d", c)
	}
	if got, _ = st.Monitor(web.ID); len(got.Headers) != 1 || got.Headers["A"] != "3" {
		t.Fatalf("headers: %v", got.Headers)
	}

	if c := do("PUT", "/api/monitors/"+itoa(m.ID), `[]`, nil); c != 400 {
		t.Fatalf("array body: %d", c)
	}
	if c := do("PUT", "/api/monitors/999", `{}`, nil); c != 404 {
		t.Fatalf("missing monitor: %d", c)
	}

	// Status pages and tags take the same path.
	var p store.StatusPage
	if c := do("POST", "/api/status-pages", `{"slug":"main","title":"Main","description":"All systems","published":true}`, &p); c != 201 {
		t.Fatalf("create page: %d", c)
	}
	if c := do("PUT", "/api/status-pages/"+itoa(p.ID), `{"title":"Status"}`, nil); c != 200 {
		t.Fatalf("update page: %d", c)
	}
	if gp, _ := st.StatusPage(p.ID); gp.Title != "Status" || gp.Description != "All systems" || !gp.Published || gp.Slug != "main" {
		t.Fatalf("page: %+v", gp)
	}
	if c := do("PUT", "/api/tags/"+itoa(tag.ID), `{"name":"production"}`, nil); c != 200 {
		t.Fatalf("update tag: %d", c)
	}
	if gt, _ := st.Tag(tag.ID); gt.Name != "production" || gt.Color != "red" {
		t.Fatalf("tag: %+v", gt)
	}
	if c := do("PUT", "/api/tags/999", `{"name":"x"}`, nil); c != 404 {
		t.Fatalf("missing tag: %d", c)
	}
}

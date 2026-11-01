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

func TestStatusPagePreview(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"index.html": {Data: []byte("admin")}}))
	defer srv.Close()
	cookie := login(t, h, st)
	m := &store.Monitor{Name: "api", Type: "http", Target: "http://x", IntervalSec: 60, Locations: []string{"local"}}
	if err := st.CreateMonitor(m); err != nil {
		t.Fatal(err)
	}
	body := `{"title":"  Draft  ","slug":"","monitor_ids":[` + itoa(m.ID) + `,999]}`
	req, _ := http.NewRequest("POST", srv.URL+"/api/status-pages/preview", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var got struct {
		Title    string
		Monitors []struct{ Name string }
	}
	if res.StatusCode != 200 || json.Unmarshal(b, &got) != nil || got.Title != "Draft" || len(got.Monitors) != 1 || got.Monitors[0].Name != "api" {
		t.Fatalf("preview: %d %s", res.StatusCode, b)
	}
	if pages, _ := st.ListStatusPages(); len(pages) != 0 {
		t.Fatalf("preview saved a page: %+v", pages)
	}
}

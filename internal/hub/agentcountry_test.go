package hub

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestAgentCountry(t *testing.T) {
	h, st := newTestHub(t)

	w := httptest.NewRecorder()
	h.createAgent(w, httptest.NewRequest("POST", "/api/agents", strings.NewReader(`{"name":"fra","location":"Frankfurt","country":"de"}`)), nil)
	if w.Code != 201 {
		t.Fatalf("create: %d %s", w.Code, w.Body)
	}
	var created struct {
		Agent struct {
			ID      int64  `json:"id"`
			Country string `json:"country"`
		} `json:"agent"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &created)
	if created.Agent.Country != "DE" {
		t.Fatalf("country = %q, want DE", created.Agent.Country)
	}
	id := strconv.FormatInt(created.Agent.ID, 10)

	put := func(body string) int {
		r := httptest.NewRequest("PUT", "/api/agents/"+id, strings.NewReader(body))
		r.SetPathValue("id", id)
		w := httptest.NewRecorder()
		h.updateAgent(w, r, nil)
		return w.Code
	}
	country := func() string {
		a, err := st.Agent(created.Agent.ID)
		if err != nil {
			t.Fatal(err)
		}
		return a.Country
	}

	// An older client that does not send country keeps it, and the location too.
	if c := put(`{"name":"fra-1"}`); c != 200 {
		t.Fatalf("put: %d", c)
	}
	if a, _ := st.Agent(created.Agent.ID); a.Country != "DE" || a.Location != "Frankfurt" || a.Name != "fra-1" {
		t.Fatalf("after put without country: %+v", a)
	}
	if c := put(`{"country":"Germany"}`); c != 400 {
		t.Fatalf("invalid country: %d, want 400", c)
	}
	if c := put(`{"country":" it "}`); c != 200 || country() != "IT" {
		t.Fatalf("set IT: %d %q", c, country())
	}
	if c := put(`{"country":""}`); c != 200 || country() != "" {
		t.Fatalf("clear: %d %q", c, country())
	}
}

func TestNormalizeCountry(t *testing.T) {
	for in, want := range map[string]string{"": "", "it": "IT", " DE ": "DE", "ITA": "!", "1T": "!", "é": "!"} {
		got, ok := normalizeCountry(in)
		if !ok {
			got = "!"
		}
		if got != want {
			t.Errorf("normalizeCountry(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLocalAgent(t *testing.T) {
	h, _ := newTestHub(t)
	put := func(body string) int {
		w := httptest.NewRecorder()
		h.updateLocalAgent(w, httptest.NewRequest("PUT", "/api/agents/local", strings.NewReader(body)), nil)
		return w.Code
	}
	enabled := h.generalSettings().LocalEnabled
	if c := put(`{"country":"it"}`); c != 200 {
		t.Fatalf("put: %d", c)
	}
	g := h.generalSettings()
	if g.LocalCountry != "IT" || g.LocalName != "hub" || g.LocalEnabled != enabled {
		t.Fatalf("after put: %+v", g)
	}
	if c := put(`{"country":"Italy"}`); c != 400 {
		t.Fatalf("invalid country: %d", c)
	}
	if v := h.localAgentView(0); v.LocationID != LocalLocation || v.Country != "IT" {
		t.Fatalf("view: %+v", v)
	}
}

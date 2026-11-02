package hub

import (
	"strings"
	"testing"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestValidatePageTheme(t *testing.T) {
	r := 8
	ok := store.PageTheme{
		Mode:        "dark",
		Light:       store.PageColors{Accent: " #2563EB "},
		Font:        "plex",
		Radius:      &r,
		Style:       "cards",
		HeaderLinks: []store.PageLink{{Label: " Site ", URL: "https://example.com"}, {}},
		FooterLinks: []store.PageLink{{Label: "Mail", URL: "mailto:help@example.com"}},
		FooterText:  "**Acme** [help](https://example.com)",
		CSS:         ".pt-banner { font-weight: 700 }",
	}
	if err := validatePageTheme(&ok); err != nil {
		t.Fatal(err)
	}
	if ok.Light.Accent != "#2563eb" || len(ok.HeaderLinks) != 1 || ok.HeaderLinks[0].Label != "Site" {
		t.Fatalf("not cleaned: %+v", ok)
	}

	big := 99
	bad := []store.PageTheme{
		{Mode: "sepia"},
		{Font: "comic"},
		{Light: store.PageColors{Background: "red"}},
		{Dark: store.PageColors{Text: "#fff"}},
		{Radius: &big},
		{HeaderLinks: []store.PageLink{{Label: "x", URL: "javascript:alert(1)"}}},
		{FooterLinks: []store.PageLink{{Label: "", URL: "https://example.com"}}},
		{CSS: "a{}</style><script>alert(1)</script>"},
		{CSS: strings.Repeat("a", maxThemeCSS+1)},
		{Tagline: strings.Repeat("a", maxTagline+1)},
	}
	for _, b := range bad {
		if err := validatePageTheme(&b); err == nil {
			t.Errorf("accepted %+v", b)
		}
	}
	many := store.PageTheme{}
	for i := 0; i <= maxHeaderLinks; i++ {
		many.HeaderLinks = append(many.HeaderLinks, store.PageLink{Label: "x", URL: "https://example.com"})
	}
	if validatePageTheme(&many) == nil {
		t.Error("accepted too many links")
	}
}

func TestPageThemeStored(t *testing.T) {
	_, st := newTestHub(t)
	r := 0
	p := &store.StatusPage{Slug: "a", Title: "A", Published: true, Theme: store.PageTheme{Font: "serif", Radius: &r, Light: store.PageColors{Accent: "#112233"}}}
	if err := st.CreateStatusPage(p); err != nil {
		t.Fatal(err)
	}
	got, err := st.StatusPage(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Theme.Font != "serif" || got.Theme.Radius == nil || *got.Theme.Radius != 0 || got.Theme.Light.Accent != "#112233" {
		t.Fatalf("theme not stored: %+v", got.Theme)
	}
	got.Theme = store.PageTheme{}
	if err := st.UpdateStatusPage(got); err != nil {
		t.Fatal(err)
	}
	if got, _ = st.StatusPage(p.ID); !got.Theme.IsZero() {
		t.Fatalf("theme not cleared: %+v", got.Theme)
	}
}

package hub

import (
	"errors"
	"fmt"

	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/federico-it/pingtxt/internal/store"
)

// Limits of the look of a status page.
const (
	maxThemeCSS     = 32 << 10
	maxThemeText    = 2000
	maxTagline      = 200
	maxNotice       = 500
	maxHeaderLinks  = 6
	maxFooterLinks  = 12
	maxLinkLabel    = 60
	maxThemeRadius  = 24
	maxThemeLinkURL = 2048
)

var hexColorRe = regexp.MustCompile(`^#[0-9a-f]{6}$`)

// themeChoices lists the allowed values of each option ("" = default).
var themeChoices = map[string][]string{
	"mode":         {"", "light", "dark"},
	"font":         {"", "plex", "figtree", "atkinson", "serif", "system"},
	"size":         {"", "s", "l"},
	"style":        {"", "solid", "cards"},
	"density":      {"", "compact", "airy"},
	"width":        {"", "wide"},
	"bars":         {"", "round", "thin"},
	"header_style": {"", "band"},
	"header_align": {"", "center"},
}

func checkChoice(name, v string) error {
	for _, c := range themeChoices[name] {
		if v == c {
			return nil
		}
	}
	return fmt.Errorf("theme %s must be one of %q", name, themeChoices[name][1:])
}

// validatePageTheme cleans t and checks every field. Colors must be
// #rrggbb, options come from fixed lists and links must be http(s) or
// mailto. The CSS is free (admins only) but cannot close its style tag.
func validatePageTheme(t *store.PageTheme) error {
	t.Mode = strings.TrimSpace(t.Mode)
	for name, v := range map[string]string{
		"mode": t.Mode, "font": t.Font, "size": t.Size, "style": t.Style, "density": t.Density,
		"width": t.Width, "bars": t.Bars, "header_style": t.HeaderStyle, "header_align": t.HeaderAlign,
	} {
		if err := checkChoice(name, v); err != nil {
			return err
		}
	}
	for _, c := range []*store.PageColors{&t.Light, &t.Dark} {
		for _, f := range []*string{&c.Background, &c.Surface, &c.Text, &c.Muted, &c.Border, &c.Accent, &c.Up, &c.Degraded, &c.Down} {
			*f = strings.ToLower(strings.TrimSpace(*f))
			if *f != "" && !hexColorRe.MatchString(*f) {
				return fmt.Errorf("theme color %q must be like #1a2b3c", *f)
			}
		}
	}
	if t.Radius != nil && (*t.Radius < 0 || *t.Radius > maxThemeRadius) {
		return fmt.Errorf("theme radius must be 0-%d", maxThemeRadius)
	}
	t.Tagline = strings.TrimSpace(t.Tagline)
	t.Notice = strings.TrimSpace(t.Notice)
	t.FooterText = strings.TrimSpace(t.FooterText)
	if utf8.RuneCountInString(t.Tagline) > maxTagline || utf8.RuneCountInString(t.Notice) > maxNotice ||
		utf8.RuneCountInString(t.FooterText) > maxThemeText {
		return errors.New("theme tagline, notice or footer text is too long")
	}
	var err error
	if t.HeaderLinks, err = cleanPageLinks(t.HeaderLinks, maxHeaderLinks); err != nil {
		return err
	}
	if t.FooterLinks, err = cleanPageLinks(t.FooterLinks, maxFooterLinks); err != nil {
		return err
	}
	t.CSS = strings.TrimSpace(t.CSS)
	if len(t.CSS) > maxThemeCSS {
		return fmt.Errorf("custom CSS must be at most %d KB", maxThemeCSS>>10)
	}
	if strings.Contains(strings.ToLower(t.CSS), "</style") {
		return errors.New("custom CSS cannot contain </style>")
	}
	return nil
}

func cleanPageLinks(in []store.PageLink, max int) ([]store.PageLink, error) {
	out := []store.PageLink{}
	for _, l := range in {
		l.Label = strings.TrimSpace(l.Label)
		l.URL = strings.TrimSpace(l.URL)
		if l.Label == "" && l.URL == "" {
			continue
		}
		if l.Label == "" || utf8.RuneCountInString(l.Label) > maxLinkLabel {
			return nil, fmt.Errorf("link text must be 1-%d characters", maxLinkLabel)
		}
		if safeLinkURL(l.URL) == "" {
			return nil, fmt.Errorf("link %q must be an http://, https:// or mailto: address", l.Label)
		}
		out = append(out, l)
	}
	if len(out) > max {
		return nil, fmt.Errorf("at most %d links", max)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

// safeLinkURL returns u when visitors can safely follow it: an absolute
// http(s) address or a mailto: address.
func safeLinkURL(u string) string {
	if strings.HasPrefix(strings.ToLower(u), "mailto:") {
		if len(u) > 320 || strings.ContainsAny(u, "<>\"' \n") || len(u) == len("mailto:") {
			return ""
		}
		return u
	}
	if len(u) > maxThemeLinkURL {
		return ""
	}
	return safeHeaderURL(u)
}

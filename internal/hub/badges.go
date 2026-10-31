package hub

import (
	"errors"
	"fmt"
	"html"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/federico-it/pingtxt/internal/store"
)

// Badges are small shields-style SVG images to embed in a README:
//
//	/api/badge/{slug}/status.svg               overall state of a status page
//	/api/badge/{slug}/{monitor}/status.svg     state of one monitor
//	/api/badge/{slug}/{monitor}/uptime.svg     uptime, ?period=24h|7d|30d|90d
//	/api/badge/{slug}/{monitor}/response.svg   average response time, same periods
//
// They need no login, so they only exist for published status pages and for
// the monitors those pages show: a badge never tells more than the page.
// The ".svg" suffix is optional. ?label= replaces the left text and
// ?style=flat-square drops the rounded corners.

const badgeCacheTTL = 60 * time.Second

type cachedBadge struct {
	at   time.Time
	code int
	svg  []byte
}

var badgePeriods = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
	"90d": 90 * 24 * time.Hour,
}

// Badge colors, as shields.io names them.
const (
	badgeGreen       = "#4c1"
	badgeYellowGreen = "#97ca00"
	badgeYellow      = "#dfb317"
	badgeOrange      = "#fe7d37"
	badgeRed         = "#e05d44"
	badgeBlue        = "#007ec6"
	badgeGrey        = "#9f9f9f"
)

type badge struct {
	label, value, color string
}

func (h *Hub) pageBadge(w http.ResponseWriter, r *http.Request) {
	h.serveBadge(w, r, "", r.PathValue("kind"))
}

func (h *Hub) monitorBadge(w http.ResponseWriter, r *http.Request) {
	h.serveBadge(w, r, r.PathValue("monitor"), r.PathValue("kind"))
}

func (h *Hub) serveBadge(w http.ResponseWriter, r *http.Request, monitor, kind string) {
	slug := strings.ToLower(r.PathValue("slug"))
	kind = strings.TrimSuffix(kind, ".svg")
	q := r.URL.Query()
	period := q.Get("period")
	label := q.Get("label")
	if utf8.RuneCountInString(label) > 60 {
		label = string([]rune(label)[:60])
	}
	square := q.Get("style") == "flat-square"
	key := strings.Join([]string{slug, monitor, kind, period, label, strconv.FormatBool(square)}, "\x00")

	h.pages.mu.RLock()
	c, ok := h.pages.badges[key]
	h.pages.mu.RUnlock()
	if !ok || time.Since(c.at) > badgeCacheTTL {
		b, code, err := h.buildBadge(slug, monitor, kind, period)
		if err != nil {
			h.internalErr(w, err)
			return
		}
		if label != "" {
			b.label = label
		}
		c = cachedBadge{at: time.Now(), code: code, svg: renderBadge(b, square)}
		h.pages.mu.Lock()
		if h.pages.badges == nil || len(h.pages.badges) >= 2000 {
			h.pages.badges = map[string]cachedBadge{}
		}
		h.pages.badges[key] = c
		h.pages.mu.Unlock()
	}
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", fmt.Sprintf("public, max-age=%d", int(badgeCacheTTL.Seconds())))
	// Opened directly, the image must not run anything.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'")
	w.WriteHeader(c.code)
	if r.Method != http.MethodHead {
		w.Write(c.svg)
	}
}

// buildBadge returns the badge content and the HTTP status. Unknown pages,
// drafts and monitors the page does not show all answer the same 404 badge.
func (h *Hub) buildBadge(slug, monitor, kind, period string) (badge, int, error) {
	notFound := badge{"pingtxt", "not found", badgeGrey}
	bad := func(msg string) (badge, int, error) { return badge{"pingtxt", msg, badgeGrey}, 400, nil }
	page, err := h.cachedPublicPage(slug)
	if errors.Is(err, store.ErrNotFound) {
		return notFound, 404, nil
	} else if err != nil {
		return badge{}, 0, err
	}
	if !page.Published {
		return notFound, 404, nil
	}
	if monitor == "" {
		if kind != "status" {
			return notFound, 404, nil
		}
		return pageStatusBadge(page), 200, nil
	}
	id, err := strconv.ParseInt(monitor, 10, 64)
	if err != nil {
		return notFound, 404, nil
	}
	var pm *publicMonitor
	for i := range page.Monitors {
		if page.Monitors[i].ID == id {
			pm = &page.Monitors[i]
			break
		}
	}
	if pm == nil {
		return notFound, 404, nil
	}
	switch kind {
	case "status":
		return monitorStatusBadge(pm), 200, nil
	case "uptime", "response":
	default:
		return notFound, 404, nil
	}
	if period == "" {
		period = map[string]string{"uptime": "30d", "response": "24h"}[kind]
	}
	d, ok := badgePeriods[period]
	if !ok {
		return bad("bad period")
	}
	up, avg, err := h.store.Uptime(id, time.Now().Add(-d).UnixMilli())
	if err != nil {
		return badge{}, 0, err
	}
	if kind == "uptime" {
		return uptimeBadge("uptime "+period, up), 200, nil
	}
	return responseBadge("response "+period, up, avg), 200, nil
}

func pageStatusBadge(p *publicPage) badge {
	b := badge{label: p.Title}
	switch p.Status {
	case PageOperational:
		b.value, b.color = "operational", badgeGreen
	case PageDegraded:
		b.value, b.color = "degraded", badgeYellow
	case PagePartialOutage:
		b.value, b.color = "partial outage", badgeOrange
	case PageMajorOutage:
		b.value, b.color = "major outage", badgeRed
	default:
		b.value, b.color = "unknown", badgeGrey
	}
	return b
}

func monitorStatusBadge(m *publicMonitor) badge {
	b := badge{label: m.Name, value: m.Status}
	switch m.Status {
	case StatusUp:
		b.color = badgeGreen
	case StatusDegraded:
		b.color = badgeYellow
	case StatusDown:
		b.color = badgeRed
	case StatusMaintenance:
		b.color = badgeBlue
	default:
		b.color = badgeGrey
	}
	return b
}

func uptimeBadge(label string, up float64) badge {
	if up < 0 {
		return badge{label, "no data", badgeGrey}
	}
	p := up * 100
	b := badge{label: label, value: formatUptime(p)}
	switch {
	case p >= 99.9:
		b.color = badgeGreen
	case p >= 99:
		b.color = badgeYellowGreen
	case p >= 95:
		b.color = badgeYellow
	case p >= 90:
		b.color = badgeOrange
	default:
		b.color = badgeRed
	}
	return b
}

// formatUptime keeps up to two decimals and never rounds up to 100%.
func formatUptime(p float64) string {
	if p >= 100 {
		return "100%"
	}
	s := strconv.FormatFloat(math.Floor(p*100)/100, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s + "%"
}

func responseBadge(label string, up, avg float64) badge {
	if up < 0 || avg <= 0 {
		return badge{label, "no data", badgeGrey}
	}
	b := badge{label: label}
	if avg >= 10000 {
		b.value = strconv.FormatFloat(avg/1000, 'f', 1, 64) + " s"
	} else {
		b.value = strconv.Itoa(int(math.Round(avg))) + " ms"
	}
	switch {
	case avg < 200:
		b.color = badgeGreen
	case avg < 500:
		b.color = badgeYellowGreen
	case avg < 1000:
		b.color = badgeYellow
	case avg < 2000:
		b.color = badgeOrange
	default:
		b.color = badgeRed
	}
	return b
}

// renderBadge draws a shields.io-like "flat" badge.
func renderBadge(b badge, square bool) []byte {
	if b.label == "" {
		b.label = "pingtxt"
	}
	lw := int(math.Ceil(textWidth(b.label))) + 10
	vw := int(math.Ceil(textWidth(b.value))) + 10
	w := lw + vw
	label, value := html.EscapeString(b.label), html.EscapeString(b.value)
	rx, gradient := "3", `<linearGradient id="s" x2="0" y2="100%"><stop offset="0" stop-color="#bbb" stop-opacity=".1"/><stop offset="1" stop-opacity=".1"/></linearGradient>`
	fill := `<rect width="%[1]d" height="20" fill="url(#s)"/>`
	if square {
		rx, gradient, fill = "0", "", ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="20" role="img" aria-label="%s: %s">`, w, label, value)
	fmt.Fprintf(&sb, `<title>%s: %s</title>%s`, label, value, gradient)
	fmt.Fprintf(&sb, `<clipPath id="r"><rect width="%d" height="20" rx="%s" fill="#fff"/></clipPath>`, w, rx)
	fmt.Fprintf(&sb, `<g clip-path="url(#r)"><rect width="%d" height="20" fill="#555"/><rect x="%d" width="%d" height="20" fill="%s"/>`, lw, lw, vw, b.color)
	if fill != "" {
		fmt.Fprintf(&sb, fill, w)
	}
	sb.WriteString(`</g><g fill="#fff" text-anchor="middle" font-family="Verdana,Geneva,DejaVu Sans,sans-serif" font-size="11">`)
	text := func(x float64, width int, s string) {
		tl := width - 10
		fmt.Fprintf(&sb, `<text x="%.1f" y="15" fill="#010101" fill-opacity=".3" textLength="%d" lengthAdjust="spacingAndGlyphs">%s</text>`, x, tl, s)
		fmt.Fprintf(&sb, `<text x="%.1f" y="14" textLength="%d" lengthAdjust="spacingAndGlyphs">%s</text>`, x, tl, s)
	}
	text(float64(lw)/2, lw, label)
	text(float64(lw)+float64(vw)/2, vw, value)
	sb.WriteString(`</g></svg>`)
	return []byte(sb.String())
}

// verdanaWidths are the advance widths of printable ASCII (from ' ') in
// Verdana at 11px, close enough to size the badge boxes.
var verdanaWidths = [...]float64{
	3.87, 4.33, 5.05, 9.0, 7.0, 11.84, 7.99, 2.95, 4.99, 4.99, 7.0, 9.0, 4.0, 4.99, 4.0, 4.99, // space to /
	7.0, 7.0, 7.0, 7.0, 7.0, 7.0, 7.0, 7.0, 7.0, 7.0, // 0-9
	4.99, 4.99, 9.0, 9.0, 9.0, 6.0, 11.0, // : to @
	7.52, 7.54, 7.68, 8.48, 6.96, 6.32, 8.53, 8.27, 4.63, 5.0, 7.62, 6.12, 9.27,
	8.23, 8.66, 6.63, 8.66, 7.65, 7.52, 6.78, 8.05, 7.52, 10.88, 7.54, 6.77, 7.54, // A-Z
	4.99, 4.99, 4.99, 9.0, 7.0, 7.0, // [ to `
	6.61, 6.85, 5.73, 6.85, 6.55, 3.87, 6.85, 6.96, 3.02, 3.79, 6.51, 3.02, 10.7,
	6.96, 6.68, 6.85, 6.85, 4.69, 5.73, 4.33, 6.96, 6.51, 8.98, 6.51, 6.51, 5.78, // a-z
	6.98, 4.99, 6.98, 9.0, // { to ~
}

func textWidth(s string) float64 {
	w := 0.0
	for _, r := range s {
		switch {
		case r >= ' ' && r <= '~':
			w += verdanaWidths[r-' ']
		case r >= 0x2E80: // CJK and other wide scripts
			w += 11
		default:
			w += 7
		}
	}
	return w
}

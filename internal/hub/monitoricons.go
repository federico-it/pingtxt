package hub

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/net/html"
	"golang.org/x/net/publicsuffix"

	"github.com/federico-it/pingtxt/internal/check"
	"github.com/federico-it/pingtxt/internal/store"
)

// Monitor icons. Monitor.Icon says where the icon comes from:
//
//   - "" (default): the favicon of the target's website, looked up by the
//     hub in the background (iconLoop) when the monitor is saved and again
//     when its target changes. Only public addresses are fetched.
//   - "none": no icon.
//   - "custom": an image uploaded to POST /api/monitors/{id}/icon.
//   - "dashboard:<name>": an icon of Dashboard Icons (dashboardicons.com,
//     github.com/homarr-labs/dashboard-icons), downloaded once with its
//     variant for dark themes when it has one.
//
// Images are stored in the database (an SVG cleaned like status page
// logos, or a PNG at most iconMaxSize pixels wide and high) and served on
// GET /api/monitors/{id}/icon, so the browser never contacts a third party.

const (
	IconAuto   = ""
	IconNone   = "none"
	IconCustom = "custom"
	iconDIPfx  = "dashboard:"

	iconMaxSize    = 128     // pixels, 4x the largest size the UI shows
	maxIconBytes   = 1 << 20 // upload or download
	maxIconPage    = 1 << 20 // HTML read to find the icon links
	iconRetryAfter = 7 * 24 * time.Hour
	diRetryAfter   = 24 * time.Hour
)

// dashboardIconsBase is where the Dashboard Icons files are downloaded
// from (tests replace it).
var dashboardIconsBase = "https://raw.githubusercontent.com/homarr-labs/dashboard-icons/main"

var diNameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,99}$`)

type monitorIcons struct {
	mu   sync.Mutex    // one icon write at a time
	wake chan struct{} // asks iconLoop for a pass

	cmu       sync.Mutex
	catalog   *diCatalog
	catalogAt time.Time
	catalogOK bool

	pmu      sync.Mutex
	previews map[string]iconImage
}

type iconImage struct {
	data  []byte
	ctype string
}

// iconsSoon asks the background loop to look for the icons that are
// missing (new monitors, changed targets).
func (h *Hub) iconsSoon() {
	select {
	case h.icons.wake <- struct{}{}:
	default:
	}
}

// iconLoop fetches missing icons: shortly after start, when asked, and
// once a day (favicons not found are looked for again after a week).
func (h *Hub) iconLoop(ctx context.Context) {
	t := time.NewTimer(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			t.Reset(24 * time.Hour)
		case <-h.icons.wake:
			// Saves often come in bursts (imports, bulk edits).
			select {
			case <-ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
		}
		h.iconPass(ctx)
	}
}

// iconPass fetches the icons of the monitors that need one.
func (h *Hub) iconPass(ctx context.Context) {
	ms, err := h.store.ListMonitors()
	if err != nil {
		h.log.Error("monitor icons", "err", err)
		return
	}
	refs, err := h.store.MonitorIconRefs()
	if err != nil {
		h.log.Error("monitor icons", "err", err)
		return
	}
	now := time.Now()
	found := map[string]*store.MonitorIcon{} // per page or Dashboard Icons name, within this pass
	for _, m := range ms {
		if ctx.Err() != nil {
			return
		}
		r := refs[m.ID]
		switch {
		case m.Icon == IconNone:
			if r != nil { // e.g. after an import
				_ = h.store.DeleteMonitorIcon(m.ID)
			}
		case m.Icon == IconAuto:
			page := faviconPage(m)
			if page == "" {
				if r != nil {
					_ = h.store.DeleteMonitorIcon(m.ID)
				}
				continue
			}
			if r != nil && r.Source == "favicon" && r.Ref == page &&
				(r.Image != nil || now.Sub(time.UnixMilli(r.CheckedAt)) < iconRetryAfter) {
				continue
			}
			ic := found["page "+page]
			if ic == nil {
				ic = h.lookupFavicon(ctx, page)
				found["page "+page] = ic
			}
			h.storeIconIf(m.ID, m.Icon, page, ic)
		case strings.HasPrefix(m.Icon, iconDIPfx):
			name := strings.TrimPrefix(m.Icon, iconDIPfx)
			if r != nil && r.Source == "dashboard" && r.Ref == name &&
				(r.Image != nil || now.Sub(time.UnixMilli(r.CheckedAt)) < diRetryAfter) {
				continue
			}
			ic := found["di "+name]
			if ic == nil {
				var err error
				if ic, err = h.dashboardIcon(ctx, name); err != nil {
					h.log.Warn("monitor icon", "monitor", m.ID, "icon", name, "err", err)
					ic = &store.MonitorIcon{Source: "dashboard", Ref: name}
				}
				found["di "+name] = ic
			}
			h.storeIconIf(m.ID, m.Icon, "", ic)
		}
	}
}

// lookupFavicon finds the favicon of a page; a result without Image when
// there is none.
func (h *Hub) lookupFavicon(ctx context.Context, page string) *store.MonitorIcon {
	c, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ic := &store.MonitorIcon{Source: "favicon", Ref: page}
	img, err := findFavicon(c, page)
	if err != nil {
		h.log.Debug("favicon", "page", page, "err", err)
		return ic
	}
	ic.Image, ic.ImageType = img.data, img.ctype
	return ic
}

// storeIconIf stores an icon found in the background, unless the monitor
// changed meanwhile.
func (h *Hub) storeIconIf(id int64, setting, page string, ic *store.MonitorIcon) {
	h.icons.mu.Lock()
	defer h.icons.mu.Unlock()
	m, err := h.store.Monitor(id)
	if err != nil || m.Icon != setting || (setting == IconAuto && faviconPage(m) != page) {
		return
	}
	c := *ic
	c.MonitorID, c.CheckedAt = id, 0
	if err := h.store.SetMonitorIcon(&c); err != nil {
		h.log.Error("monitor icon", "monitor", id, "err", err)
	}
}

// faviconPage is the web page whose favicon stands for the monitor's
// target: the site of a URL, or the website of a host's domain
// (smtp.example.com: https://example.com/). "" when there is none.
func faviconPage(m *store.Monitor) string {
	target := strings.TrimSpace(m.Target)
	switch m.Type {
	case check.TypeHTTP, check.TypeKeyword, check.TypeStatuspage, check.TypeMCP, check.TypeWS:
		u, err := url.Parse(target)
		if err != nil || u.Host == "" {
			return ""
		}
		scheme := u.Scheme
		switch scheme {
		case "ws":
			scheme = "http"
		case "wss":
			scheme = "https"
		case "http", "https":
		default:
			return ""
		}
		host := u.Hostname()
		if host == "" || net.ParseIP(host) != nil || !strings.Contains(host, ".") {
			return ""
		}
		return scheme + "://" + u.Host + "/"
	case check.TypeTCP, check.TypePing, check.TypeDNS, check.TypeGRPC, check.TypeSMTP, check.TypeSSH,
		check.TypeIMAP, check.TypePOP3:
		host := target
		if i := strings.Index(host, "://"); i >= 0 {
			if u, err := url.Parse(host); err == nil {
				host = u.Host
			}
		}
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		host = strings.TrimSuffix(strings.ToLower(host), ".")
		if host == "" || net.ParseIP(host) != nil || !strings.Contains(host, ".") || strings.ContainsAny(host, "/ @") {
			return ""
		}
		d, err := publicsuffix.EffectiveTLDPlusOne(host)
		if err != nil {
			return ""
		}
		return "https://" + d + "/"
	}
	return ""
}

// ---------- images ----------

// processIcon turns an uploaded or downloaded file into a stored icon.
func processIcon(data []byte) (iconImage, error) {
	if len(data) == 0 {
		return iconImage{}, errors.New("the icon file is empty")
	}
	if len(data) > maxIconBytes {
		return iconImage{}, fmt.Errorf("the icon is larger than %d MB", maxIconBytes>>20)
	}
	if looksLikeSVG(data) {
		svg, _, _, err := sanitizeSVG(data)
		if err != nil {
			return iconImage{}, err
		}
		return iconImage{data: svg, ctype: "image/svg+xml"}, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return iconImage{}, errors.New("the icon must be an SVG, PNG, JPEG, GIF, WebP, AVIF or ICO image")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || float64(cfg.Width)*float64(cfg.Height) > maxLogoPixels {
		return iconImage{}, errors.New("the icon image is too large (at most 40 megapixels)")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return iconImage{}, errors.New("the icon image could not be read")
	}
	img = fitIcon(img)
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		return iconImage{}, err
	}
	return iconImage{data: buf.Bytes(), ctype: "image/png"}, nil
}

// fitIcon scales img down (never up) to fit iconMaxSize x iconMaxSize.
func fitIcon(img image.Image) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := min(1, float64(iconMaxSize)/float64(w), float64(iconMaxSize)/float64(h))
	nw, nh := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	if nw == w && nh == h {
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	}
	return dst
}

// ---------- favicon ----------

type iconLink struct {
	href  string
	score int
}

// findFavicon looks for the icon of a website: the icons its home page
// links (an SVG first, then the largest), then /favicon.ico.
func findFavicon(ctx context.Context, page string) (iconImage, error) {
	client := publicHTTPClient()
	base, err := url.Parse(page)
	if err != nil {
		return iconImage{}, err
	}
	var links []iconLink
	if body, final, err := getLimited(ctx, client, page, "text/html,application/xhtml+xml", maxIconPage); err == nil {
		if final != nil {
			base = final
		}
		links = iconLinks(body)
	}
	slices.SortStableFunc(links, func(a, b iconLink) int { return b.score - a.score })
	var cands []string
	seen := map[string]bool{}
	for _, l := range links {
		u, err := base.Parse(l.href)
		if err != nil || seen[u.String()] {
			continue
		}
		seen[u.String()] = true
		cands = append(cands, u.String())
	}
	if fav := (&url.URL{Scheme: base.Scheme, Host: base.Host, Path: "/favicon.ico"}).String(); !seen[fav] {
		cands = append(cands, fav)
	}
	lastErr := errors.New("no icon found")
	for i, c := range cands {
		if i >= 5 {
			break
		}
		var data []byte
		if strings.HasPrefix(c, "data:") {
			data, err = decodeDataURL(c)
		} else {
			data, _, err = getLimited(ctx, client, c, "image/svg+xml,image/png,image/x-icon,image/*;q=0.8", maxIconBytes)
		}
		if err != nil {
			lastErr = err
			continue
		}
		img, err := processIcon(data)
		if err != nil {
			lastErr = err
			continue
		}
		return img, nil
	}
	return iconImage{}, lastErr
}

// getLimited downloads a URL, at most limit bytes, and returns the URL it
// ended on after redirects.
func getLimited(ctx context.Context, client *http.Client, raw, accept string, limit int64) ([]byte, *url.URL, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; pingtxt; +https://pingtxt.com)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("%s answered %s", raw, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, nil, err
	}
	if int64(len(data)) > limit {
		return nil, nil, fmt.Errorf("%s is larger than %d KB", raw, limit>>10)
	}
	return data, resp.Request.URL, nil
}

// iconLinks reads the icon links in the head of an HTML page, scored by
// how good they look: SVG first, then by size.
func iconLinks(body []byte) []iconLink {
	z := html.NewTokenizer(bytes.NewReader(body))
	var out []iconLink
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return out
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "head" {
				return out
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			name, more := z.TagName()
			if string(name) == "body" {
				return out
			}
			if string(name) != "link" || !more {
				continue
			}
			attrs := map[string]string{}
			for more {
				var k, v []byte
				k, v, more = z.TagAttr()
				attrs[string(k)] = string(v)
			}
			href := strings.TrimSpace(attrs["href"])
			if href == "" {
				continue
			}
			rel := strings.Fields(strings.ToLower(attrs["rel"]))
			isIcon := slices.Contains(rel, "icon")
			isApple := slices.Contains(rel, "apple-touch-icon") || slices.Contains(rel, "apple-touch-icon-precomposed")
			if !isIcon && !isApple {
				continue
			}
			typ := strings.ToLower(attrs["type"])
			lowHref := strings.ToLower(href)
			score := 0
			switch {
			case typ == "image/svg+xml" || strings.HasSuffix(strings.SplitN(lowHref, "?", 2)[0], ".svg") || strings.HasPrefix(lowHref, "data:image/svg+xml"):
				score = 1000
			default:
				size := 0
				for _, s := range strings.Fields(strings.ToLower(attrs["sizes"])) {
					if a, _, ok := strings.Cut(s, "x"); ok {
						if n, err := strconv.Atoi(a); err == nil && n > size {
							size = n
						}
					}
				}
				if size == 0 {
					size = 32
					if isApple {
						size = 180
					}
				}
				score = min(size, 512)
				if isApple {
					score -= 1 // same size: the plain icon is usually transparent
				}
			}
			out = append(out, iconLink{href: href, score: score})
		}
	}
}

// decodeDataURL reads an inline (data:) image.
func decodeDataURL(s string) ([]byte, error) {
	meta, data, ok := strings.Cut(strings.TrimPrefix(s, "data:"), ",")
	if !ok {
		return nil, errors.New("invalid data URL")
	}
	if strings.HasSuffix(meta, ";base64") {
		return base64.StdEncoding.DecodeString(strings.Map(dropSpace, data))
	}
	d, err := url.PathUnescape(data)
	return []byte(d), err
}

// ---------- Dashboard Icons ----------

// diIcon is one icon of the Dashboard Icons catalog.
type diIcon struct {
	Name       string   `json:"name"`
	Aliases    []string `json:"aliases"`
	Categories []string `json:"categories"`
	// Dark says whether the icon has a variant for dark themes.
	Dark bool `json:"dark"`
	// format is "svg" or "png"; light and dark the files for light and
	// dark themes.
	format, light, dark string
	key                 string // name and aliases, lowercase without separators
}

type diCatalog struct {
	icons  []*diIcon
	byName map[string]*diIcon
}

func diKey(s string) string {
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '-' || r == '_' || r == '.' {
			return -1
		}
		return r
	}, strings.ToLower(s))
}

// parseDICatalog reads metadata.json of Dashboard Icons. In "colors",
// "light" is the light colored file (for dark themes) and "dark" the dark
// one (for light themes).
func parseDICatalog(data []byte) (*diCatalog, error) {
	var raw map[string]struct {
		Base       string   `json:"base"`
		Aliases    []string `json:"aliases"`
		Categories []string `json:"categories"`
		Colors     *struct {
			Dark  string `json:"dark"`
			Light string `json:"light"`
		} `json:"colors"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("read the Dashboard Icons catalog: %w", err)
	}
	c := &diCatalog{byName: map[string]*diIcon{}}
	for name, r := range raw {
		if !diNameRe.MatchString(name) {
			continue
		}
		i := &diIcon{Name: name, Aliases: r.Aliases, Categories: r.Categories, format: "svg", light: name}
		if r.Base == "png" {
			i.format = "png"
		}
		if i.Aliases == nil {
			i.Aliases = []string{}
		}
		if i.Categories == nil {
			i.Categories = []string{}
		}
		if r.Colors != nil {
			if diNameRe.MatchString(r.Colors.Dark) {
				i.light = r.Colors.Dark
			}
			if diNameRe.MatchString(r.Colors.Light) && r.Colors.Light != i.light {
				i.dark = r.Colors.Light
				i.Dark = true
			}
		}
		i.key = diKey(name + " " + strings.Join(r.Aliases, " "))
		c.icons = append(c.icons, i)
		c.byName[name] = i
	}
	slices.SortFunc(c.icons, func(a, b *diIcon) int { return strings.Compare(a.Name, b.Name) })
	return c, nil
}

// dashboardCatalog returns the Dashboard Icons catalog, downloaded at most
// once a day (once every 5 minutes after a failure).
func (h *Hub) dashboardCatalog(ctx context.Context) (*diCatalog, error) {
	h.icons.cmu.Lock()
	defer h.icons.cmu.Unlock()
	age := time.Since(h.icons.catalogAt)
	if h.icons.catalog != nil && (age < 24*time.Hour || !h.icons.catalogOK && age < 5*time.Minute) {
		return h.icons.catalog, nil
	}
	if h.icons.catalog == nil && !h.icons.catalogOK && age < 5*time.Minute && !h.icons.catalogAt.IsZero() {
		return nil, errors.New("the Dashboard Icons catalog cannot be downloaded right now")
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	data, err := diGet(c, h.httpClient, "metadata.json", 16<<20)
	if err == nil {
		var cat *diCatalog
		if cat, err = parseDICatalog(data); err == nil {
			h.icons.catalog, h.icons.catalogAt, h.icons.catalogOK = cat, time.Now(), true
			return cat, nil
		}
	}
	h.icons.catalogAt, h.icons.catalogOK = time.Now(), false
	h.log.Warn("dashboard icons catalog", "err", err)
	if h.icons.catalog != nil {
		return h.icons.catalog, nil // keep using the last one
	}
	return nil, errors.New("the Dashboard Icons catalog cannot be downloaded right now")
}

func diGet(ctx context.Context, client *http.Client, path string, limit int64) ([]byte, error) {
	data, _, err := getLimited(ctx, client, dashboardIconsBase+"/"+path, "*/*", limit)
	return data, err
}

// search returns the icons matching q, best first: name, then name
// prefix, then name or aliases containing q.
func (c *diCatalog) search(q string, limit int) ([]*diIcon, int) {
	k := diKey(q)
	if k == "" {
		return c.icons[:min(limit, len(c.icons))], len(c.icons)
	}
	type hit struct {
		i    *diIcon
		rank int
	}
	var hits []hit
	for _, i := range c.icons {
		n := diKey(i.Name)
		switch {
		case n == k:
			hits = append(hits, hit{i, 0})
		case strings.HasPrefix(n, k):
			hits = append(hits, hit{i, 1})
		case strings.Contains(n, k):
			hits = append(hits, hit{i, 2})
		case strings.Contains(i.key, k):
			hits = append(hits, hit{i, 3})
		}
	}
	slices.SortStableFunc(hits, func(a, b hit) int {
		if a.rank != b.rank {
			return a.rank - b.rank
		}
		return len(a.i.Name) - len(b.i.Name)
	})
	out := make([]*diIcon, 0, min(limit, len(hits)))
	for _, h := range hits[:min(limit, len(hits))] {
		out = append(out, h.i)
	}
	return out, len(hits)
}

// diFile downloads one Dashboard Icons file and cleans it.
func (h *Hub) diFile(ctx context.Context, format, name string) (iconImage, error) {
	data, err := diGet(ctx, h.httpClient, format+"/"+name+"."+format, maxIconBytes)
	if err != nil {
		return iconImage{}, err
	}
	return processIcon(data)
}

// dashboardIcon downloads a Dashboard Icons icon, with its variant for
// dark themes.
func (h *Hub) dashboardIcon(ctx context.Context, name string) (*store.MonitorIcon, error) {
	if !diNameRe.MatchString(name) {
		return nil, fmt.Errorf("invalid Dashboard Icons name %q", name)
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	i := &diIcon{Name: name, format: "svg", light: name}
	if cat, err := h.dashboardCatalog(c); err == nil {
		known := cat.byName[name]
		if known == nil {
			return nil, fmt.Errorf("%q is not an icon of Dashboard Icons", name)
		}
		i = known
	}
	img, err := h.diFile(c, i.format, i.light)
	if err != nil && i.format == "svg" {
		img, err = h.diFile(c, "png", i.light)
	}
	if err != nil {
		return nil, fmt.Errorf("download the icon %q: %v", name, err)
	}
	ic := &store.MonitorIcon{Source: "dashboard", Ref: name, Image: img.data, ImageType: img.ctype}
	if i.dark != "" {
		if d, err := h.diFile(c, i.format, i.dark); err == nil {
			ic.Dark, ic.DarkType = d.data, d.ctype
		}
	}
	return ic, nil
}

// listDashboardIcons searches the Dashboard Icons catalog for the picker.
func (h *Hub) listDashboardIcons(w http.ResponseWriter, r *http.Request, _ *store.User) {
	cat, err := h.dashboardCatalog(r.Context())
	if err != nil {
		writeErr(w, 502, err.Error())
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 60
	}
	limit = min(limit, 300)
	icons, total := cat.search(r.URL.Query().Get("q"), limit)
	writeJSON(w, 200, map[string]any{"icons": icons, "total": total})
}

// dashboardIconPreview serves a Dashboard Icons file through the hub, for
// the previews of the picker.
func (h *Hub) dashboardIconPreview(w http.ResponseWriter, r *http.Request, _ *store.User) {
	name := r.PathValue("name")
	if !diNameRe.MatchString(name) {
		http.NotFound(w, r)
		return
	}
	dark := r.URL.Query().Get("theme") == "dark"
	key := name
	if dark {
		key += " dark"
	}
	h.icons.pmu.Lock()
	img, ok := h.icons.previews[key]
	h.icons.pmu.Unlock()
	if !ok {
		cat, err := h.dashboardCatalog(r.Context())
		if err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		i := cat.byName[name]
		if i == nil {
			http.NotFound(w, r)
			return
		}
		file := i.light
		if dark && i.dark != "" {
			file = i.dark
		}
		if img, err = h.diFile(r.Context(), i.format, file); err != nil {
			writeErr(w, 502, err.Error())
			return
		}
		h.icons.pmu.Lock()
		if h.icons.previews == nil || len(h.icons.previews) >= 2000 {
			h.icons.previews = map[string]iconImage{}
		}
		h.icons.previews[key] = img
		h.icons.pmu.Unlock()
	}
	serveIcon(w, r, img, "private, max-age=86400")
}

func serveIcon(w http.ResponseWriter, r *http.Request, img iconImage, cache string) {
	w.Header().Set("Content-Type", img.ctype)
	// Nothing in the file may run, even when it is opened directly.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", cache)
	ext := ".png"
	if img.ctype == "image/svg+xml" {
		ext = ".svg"
	}
	http.ServeContent(w, r, "icon"+ext, time.Time{}, bytes.NewReader(img.data))
}

// ---------- monitor API ----------

// validateIcon checks Monitor.Icon.
func validateIcon(m *store.Monitor) error {
	m.Icon = strings.TrimSpace(m.Icon)
	switch {
	case m.Icon == IconAuto, m.Icon == IconNone, m.Icon == IconCustom:
		return nil
	case strings.HasPrefix(m.Icon, iconDIPfx):
		m.Icon = iconDIPfx + strings.ToLower(strings.TrimSpace(strings.TrimPrefix(m.Icon, iconDIPfx)))
		if diNameRe.MatchString(strings.TrimPrefix(m.Icon, iconDIPfx)) {
			return nil
		}
	}
	return errors.New(`icon must be "" (favicon of the target), "none", "custom" or "dashboard:<name>"`)
}

// prepareIcon runs before a monitor is saved: it downloads a newly chosen
// Dashboard Icons icon, so a wrong name fails the request. prev is nil on
// create.
func (h *Hub) prepareIcon(ctx context.Context, m, prev *store.Monitor) (*store.MonitorIcon, error) {
	if m.Icon == IconCustom && (prev == nil || prev.Icon != IconCustom) {
		if prev == nil {
			m.Icon = IconAuto // e.g. a copy of a monitor: its image is not copied
			return nil, nil
		}
		return nil, errors.New(`upload the image to POST /api/monitors/{id}/icon to use a custom icon`)
	}
	name, ok := strings.CutPrefix(m.Icon, iconDIPfx)
	if !ok || (prev != nil && prev.Icon == m.Icon && prev.IconURL != "") {
		return nil, nil
	}
	return h.dashboardIcon(ctx, name)
}

// applyIcon runs after a monitor is saved with the result of prepareIcon.
func (h *Hub) applyIcon(m, prev *store.Monitor, ic *store.MonitorIcon) error {
	h.icons.mu.Lock()
	defer h.icons.mu.Unlock()
	switch {
	case ic != nil:
		c := *ic
		c.MonitorID = m.ID
		return h.store.SetMonitorIcon(&c)
	case m.Icon == IconNone:
		if prev != nil && prev.Icon != IconNone {
			return h.store.DeleteMonitorIcon(m.ID)
		}
	case m.Icon == IconAuto:
		// MonitorChanged then asks iconLoop to look for the favicon.
		if prev != nil && (prev.Icon != IconAuto || faviconPage(prev) != faviconPage(m)) {
			return h.store.DeleteMonitorIcon(m.ID)
		}
	}
	return nil
}

// iconState is the answer of the icon endpoints.
type iconState struct {
	Icon        string `json:"icon"`
	IconURL     string `json:"icon_url"`
	IconDarkURL string `json:"icon_dark_url"`
}

func (h *Hub) iconStateOf(id int64) (iconState, error) {
	m, err := h.store.Monitor(id)
	if err != nil {
		return iconState{}, err
	}
	return iconState{Icon: m.Icon, IconURL: m.IconURL, IconDarkURL: m.IconDarkURL}, nil
}

func (h *Hub) monitorForIcon(w http.ResponseWriter, r *http.Request) *store.Monitor {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return nil
	}
	m, err := h.store.Monitor(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "monitor not found")
		return nil
	} else if err != nil {
		h.internalErr(w, err)
		return nil
	}
	return m
}

// getMonitorIcon serves the icon image (?theme=dark: the variant for dark
// themes when there is one).
func (h *Hub) getMonitorIcon(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ic, err := h.store.MonitorIcon(id)
	if err != nil || len(ic.Image) == 0 {
		http.NotFound(w, r)
		return
	}
	img := iconImage{data: ic.Image, ctype: ic.ImageType}
	if r.URL.Query().Get("theme") == "dark" && len(ic.Dark) > 0 {
		img = iconImage{data: ic.Dark, ctype: ic.DarkType}
	}
	cache := "private, no-cache"
	if r.URL.Query().Get("v") == strconv.FormatInt(ic.CheckedAt, 10) {
		cache = "private, max-age=31536000, immutable" // the URL changes with the image
	}
	serveIcon(w, r, img, cache)
}

// uploadMonitorIcon sets a custom icon from the raw file in the body.
func (h *Hub) uploadMonitorIcon(w http.ResponseWriter, r *http.Request, _ *store.User) {
	m := h.monitorForIcon(w, r)
	if m == nil {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxIconBytes))
	if err != nil {
		writeErr(w, 413, fmt.Sprintf("the icon is larger than %d MB", maxIconBytes>>20))
		return
	}
	img, err := processIcon(data)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	h.icons.mu.Lock()
	err = h.store.SetMonitorIcon(&store.MonitorIcon{MonitorID: m.ID, Source: "custom", Image: img.data, ImageType: img.ctype})
	if err == nil {
		err = h.store.SetMonitorIconSetting(m.ID, IconCustom)
	}
	h.icons.mu.Unlock()
	h.iconChanged(w, m, err)
}

// detectMonitorIcon goes back to the favicon and looks for it now.
func (h *Hub) detectMonitorIcon(w http.ResponseWriter, r *http.Request, _ *store.User) {
	m := h.monitorForIcon(w, r)
	if m == nil {
		return
	}
	page := faviconPage(m)
	var ic *store.MonitorIcon
	if page != "" {
		ic = h.lookupFavicon(r.Context(), page)
	}
	h.icons.mu.Lock()
	err := h.store.SetMonitorIconSetting(m.ID, IconAuto)
	if err == nil {
		if ic != nil {
			c := *ic
			c.MonitorID = m.ID
			err = h.store.SetMonitorIcon(&c)
		} else {
			err = h.store.DeleteMonitorIcon(m.ID)
		}
	}
	h.icons.mu.Unlock()
	h.iconChanged(w, m, err)
}

// deleteMonitorIcon removes the icon (Icon = "none").
func (h *Hub) deleteMonitorIcon(w http.ResponseWriter, r *http.Request, _ *store.User) {
	m := h.monitorForIcon(w, r)
	if m == nil {
		return
	}
	h.icons.mu.Lock()
	err := h.store.SetMonitorIconSetting(m.ID, IconNone)
	if err == nil {
		err = h.store.DeleteMonitorIcon(m.ID)
	}
	h.icons.mu.Unlock()
	h.iconChanged(w, m, err)
}

func (h *Hub) iconChanged(w http.ResponseWriter, m *store.Monitor, err error) {
	if err != nil {
		h.internalErr(w, err)
		return
	}
	st, err := h.iconStateOf(m.ID)
	if err != nil {
		h.internalErr(w, err)
		return
	}
	writeJSON(w, 200, st)
}

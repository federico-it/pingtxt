package hub

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif" // decoders for uploaded logos
	_ "image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gen2brain/avif"
	"github.com/gen2brain/webp"
	xdraw "golang.org/x/image/draw"

	"github.com/federico-it/pingtxt/internal/store"
)

// Status page logos. An SVG is cleaned of anything active (scripts, event
// handlers, external references) and served as is. Raster images are
// scaled down to fit logoMaxW x logoMaxH and served as AVIF and WebP, with
// PNG for browsers that read neither. Everything is stored in the
// database, so backups carry it.

const (
	maxLogoBytes  = 4 << 20 // upload or download
	maxLogoSVG    = 512 << 10
	maxLogoPixels = 40e6 // decoded size limit (decompression bombs)
	logoMaxW      = 960  // 3x the widest the header shows
	logoMaxH      = 192  // 3x the header height
)

var errLogoFormat = errors.New("logo must be an SVG, PNG, JPEG, GIF, WebP or AVIF image")

// processLogo turns an uploaded file into the stored logo.
func processLogo(data []byte) (*store.PageLogo, error) {
	if len(data) == 0 {
		return nil, errors.New("the logo file is empty")
	}
	if len(data) > maxLogoBytes {
		return nil, fmt.Errorf("the logo is larger than %d MB", maxLogoBytes>>20)
	}
	if looksLikeSVG(data) {
		svg, w, h, err := sanitizeSVG(data)
		if err != nil {
			return nil, err
		}
		return &store.PageLogo{SVG: svg, Width: w, Height: h}, nil
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, errLogoFormat
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || float64(cfg.Width)*float64(cfg.Height) > maxLogoPixels {
		return nil, errors.New("the logo image is too large (at most 40 megapixels)")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, errLogoFormat
	}
	img = fitLogo(img)
	b := img.Bounds()
	l := &store.PageLogo{Width: b.Dx(), Height: b.Dy()}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		return nil, err
	}
	l.PNG = append([]byte(nil), buf.Bytes()...)
	// Logos are often flat colors, where lossless WebP is the smaller one.
	var lossy, lossless bytes.Buffer
	if err := webp.Encode(&lossy, img, webp.Options{Quality: 90, Method: 6}); err != nil {
		return nil, fmt.Errorf("encode webp: %w", err)
	}
	if err := webp.Encode(&lossless, img, webp.Options{Lossless: true, Method: 6}); err == nil && lossless.Len() < lossy.Len() {
		lossy = lossless
	}
	l.WebP = lossy.Bytes()
	buf.Reset()
	if err := avif.Encode(&buf, img, avif.Options{Quality: 80, QualityAlpha: 90, Speed: 6}); err != nil {
		return nil, fmt.Errorf("encode avif: %w", err)
	}
	l.AVIF = buf.Bytes()
	return l, nil
}

// fitLogo scales img down (never up) to fit logoMaxW x logoMaxH, and drops
// fully transparent borders first.
func fitLogo(img image.Image) image.Image {
	img = trimTransparent(img)
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	scale := min(1, float64(logoMaxW)/float64(w), float64(logoMaxH)/float64(h))
	nw, nh := max(1, int(float64(w)*scale+0.5)), max(1, int(float64(h)*scale+0.5))
	dst := image.NewNRGBA(image.Rect(0, 0, nw, nh))
	if nw == w && nh == h {
		draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
	}
	return dst
}

func trimTransparent(img image.Image) image.Image {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X-1, b.Min.Y-1
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if _, _, _, a := img.At(x, y).RGBA(); a > 0 {
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxX < minX { // fully transparent: keep as is
		return img
	}
	r := image.Rect(minX, minY, maxX+1, maxY+1)
	if r == b {
		return img
	}
	dst := image.NewNRGBA(image.Rect(0, 0, r.Dx(), r.Dy()))
	draw.Draw(dst, dst.Bounds(), &image.Uniform{color.Transparent}, image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), img, r.Min, draw.Src)
	return dst
}

// ---------- SVG ----------

func looksLikeSVG(data []byte) bool {
	head := data[:min(len(data), 4096)]
	head = bytes.TrimPrefix(head, []byte("\xef\xbb\xbf"))
	t := bytes.ToLower(bytes.TrimSpace(head))
	return (bytes.HasPrefix(t, []byte("<")) && bytes.Contains(t, []byte("<svg")))
}

// SVG elements and attributes kept in a logo; everything else is dropped.
var (
	svgElements = setOf("svg", "g", "path", "rect", "circle", "ellipse", "line", "polyline", "polygon",
		"text", "tspan", "defs", "lineargradient", "radialgradient", "stop", "clippath", "mask", "pattern",
		"use", "symbol", "title", "desc", "style", "image", "filter", "feblend", "fecolormatrix",
		"fecomposite", "fedropshadow", "feflood", "fegaussianblur", "femerge", "femergenode", "feoffset")
	// Attributes in no namespace are kept unless listed here or "on...".
	svgDropAttrs = setOf("href", "src", "action", "formaction")
	svgDataImage = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp|avif);base64,[A-Za-z0-9+/=\s]+$`)
	cssURL       = regexp.MustCompile(`(?i)url\(\s*(['"]?)([^)'"]*)['"]?\s*\)`)
	numRe        = regexp.MustCompile(`^\s*([0-9]*\.?[0-9]+)\s*(px)?\s*$`)
)

func setOf(s ...string) map[string]bool {
	m := make(map[string]bool, len(s))
	for _, v := range s {
		m[v] = true
	}
	return m
}

// cleanCSS removes what could load or run something from a style sheet or
// a style attribute: imports, external url()s and old IE expressions.
func cleanCSS(s string) string {
	low := strings.ToLower(s)
	if strings.Contains(low, "@import") || strings.Contains(low, "expression(") || strings.Contains(low, "javascript:") ||
		strings.Contains(low, "behavior") || strings.Contains(low, "-moz-binding") || strings.Contains(s, `\`) {
		return ""
	}
	return cssURL.ReplaceAllStringFunc(s, func(m string) string {
		if sub := cssURL.FindStringSubmatch(m); strings.HasPrefix(strings.TrimSpace(sub[2]), "#") {
			return m
		}
		return "none"
	})
}

// sanitizeSVG rewrites an SVG keeping only drawing elements and
// attributes. It returns the cleaned file and its aspect ratio.
func sanitizeSVG(data []byte) ([]byte, int, int, error) {
	// Token checks that tags match; RawToken below keeps the prefixes as written.
	check := xml.NewDecoder(bytes.NewReader(data))
	check.Strict = true // unknown entities fail instead of being expanded
	for {
		_, err := check.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, errors.New("the SVG file is not valid XML")
		}
	}
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = true
	var out bytes.Buffer
	skip := 0 // depth inside a dropped element
	var stack []string
	root := false
	w, h := 0, 0
	for {
		tok, err := dec.RawToken()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, 0, 0, errors.New("the SVG file is not valid XML")
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if skip > 0 || t.Name.Space != "" || !svgElements[name] || (len(stack) == 0 && (root || name != "svg")) {
				skip++
				continue
			}
			if !root {
				w, h = svgSize(t.Attr)
			}
			root = true
			out.WriteString("<" + t.Name.Local)
			hasViewBox := false
			for _, a := range t.Attr {
				k, v, ok := svgAttr(name, a)
				if !ok {
					continue
				}
				if k == "viewBox" {
					hasViewBox = true
				}
				out.WriteString(" " + k + `="`)
				xml.EscapeText(&out, []byte(v))
				out.WriteString(`"`)
			}
			if name == "svg" && len(stack) == 0 && !hasViewBox && w > 0 && h > 0 {
				fmt.Fprintf(&out, ` viewBox="0 0 %d %d"`, w, h)
			}
			out.WriteString(">")
			stack = append(stack, t.Name.Local)
		case xml.EndElement:
			if skip > 0 {
				skip--
				continue
			}
			if len(stack) == 0 {
				continue
			}
			out.WriteString("</" + stack[len(stack)-1] + ">")
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if skip > 0 || len(stack) == 0 {
				continue
			}
			parent := strings.ToLower(stack[len(stack)-1])
			text := string(t)
			switch parent {
			case "style":
				text = cleanCSS(text)
			case "text", "tspan", "title", "desc":
			default:
				text = strings.TrimSpace(text)
			}
			xml.EscapeText(&out, []byte(text))
		case xml.Directive:
			// <!DOCTYPE> with internal entities: refuse rather than guess.
			if bytes.Contains(bytes.ToUpper(t), []byte("ENTITY")) {
				return nil, 0, 0, errors.New("SVG files with entity declarations are not supported")
			}
		}
		// Comments, processing instructions and directives are dropped.
	}
	if !root {
		return nil, 0, 0, errLogoFormat
	}
	if out.Len() > maxLogoSVG {
		return nil, 0, 0, fmt.Errorf("the SVG logo is larger than %d KB once cleaned", maxLogoSVG>>10)
	}
	return out.Bytes(), w, h, nil
}

// svgAttr returns the attribute to write, or ok=false to drop it.
func svgAttr(elem string, a xml.Attr) (string, string, bool) {
	local := a.Name.Local
	low := strings.ToLower(local)
	v := a.Value
	if strings.Contains(strings.ToLower(strings.Map(dropSpace, v)), "javascript:") {
		return "", "", false
	}
	switch a.Name.Space {
	case "":
		if local == "xmlns" {
			return "xmlns", "http://www.w3.org/2000/svg", true
		}
		if strings.HasPrefix(low, "on") || svgDropAttrs[low] {
			// href: same rules as xlink:href below.
			if low == "href" {
				if hv, ok := svgHref(elem, v); ok {
					return "href", hv, true
				}
			}
			return "", "", false
		}
		if low == "style" {
			v = cleanCSS(v)
			if v == "" {
				return "", "", false
			}
		} else if strings.Contains(strings.ToLower(v), "url(") {
			v = cleanCSS(v)
		}
		return local, v, true
	case "xmlns":
		if local == "xlink" {
			return "xmlns:xlink", "http://www.w3.org/1999/xlink", true
		}
	case "xlink":
		if low == "href" {
			if hv, ok := svgHref(elem, v); ok {
				return "xlink:href", hv, true
			}
		}
	case "xml":
		if low == "space" {
			return "xml:space", v, true
		}
	}
	return "", "", false
}

// svgHref keeps links inside the document, and embedded raster images.
func svgHref(elem, v string) (string, bool) {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "#") {
		return v, true
	}
	if elem == "image" && svgDataImage.MatchString(v) {
		return v, true
	}
	return "", false
}

func dropSpace(r rune) rune {
	if r <= ' ' {
		return -1
	}
	return r
}

// svgSize reads the size of an SVG from its viewBox, else width/height.
func svgSize(attrs []xml.Attr) (int, int) {
	var vw, vh, aw, ah float64
	for _, a := range attrs {
		if a.Name.Space != "" {
			continue
		}
		switch strings.ToLower(a.Name.Local) {
		case "viewbox":
			f := strings.FieldsFunc(a.Value, func(r rune) bool { return r == ' ' || r == ',' || r == '\t' || r == '\n' })
			if len(f) == 4 {
				vw, _ = strconv.ParseFloat(f[2], 64)
				vh, _ = strconv.ParseFloat(f[3], 64)
			}
		case "width":
			if m := numRe.FindStringSubmatch(a.Value); m != nil {
				aw, _ = strconv.ParseFloat(m[1], 64)
			}
		case "height":
			if m := numRe.FindStringSubmatch(a.Value); m != nil {
				ah, _ = strconv.ParseFloat(m[1], 64)
			}
		}
	}
	if vw > 0 && vh > 0 {
		return int(vw + 0.5), int(vh + 0.5)
	}
	if aw > 0 && ah > 0 {
		return int(aw + 0.5), int(ah + 0.5)
	}
	return 0, 0
}

// ---------- download from a URL ----------

// logoDialControl refuses connections to the hub's own networks, so the
// URL import cannot be used to read internal services. Tests replace it.
var logoDialControl = func(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !publicIP(ip) {
		return errors.New("address not allowed")
	}
	return nil
}

var cgnat = &net.IPNet{IP: net.IP{100, 64, 0, 0}, Mask: net.CIDRMask(10, 32)}

func publicIP(ip net.IP) bool {
	if ip4 := ip.To4(); ip4 != nil {
		ip = ip4
	}
	return !(ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() || ip.IsMulticast() || cgnat.Contains(ip) ||
		ip.Equal(net.IPv4bcast))
}

// fetchLogo downloads an image from a public http(s) address.
func fetchLogo(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return nil, errors.New("logo URL must be an http:// or https:// address")
	}
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: logoDialControl}
	client := &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil, // a proxy would dial on our behalf, past the address check
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 15 * time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return errors.New("too many redirects")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return errors.New("redirect to a non-http address")
			}
			return nil
		},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "image/svg+xml,image/avif,image/webp,image/png,image/*;q=0.8")
	req.Header.Set("User-Agent", "pingtxt (status page logo)")
	resp, err := client.Do(req)
	if err != nil {
		if strings.Contains(err.Error(), "address not allowed") {
			return nil, errors.New("the logo URL points to a private or local address: upload the file instead")
		}
		return nil, fmt.Errorf("download the logo: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download the logo: the server answered %s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxLogoBytes+1))
	if err != nil {
		return nil, fmt.Errorf("download the logo: %v", err)
	}
	if len(data) > maxLogoBytes {
		return nil, fmt.Errorf("the logo is larger than %d MB", maxLogoBytes>>20)
	}
	return data, nil
}

// ---------- API ----------

// pageForLogo loads the page named in the path of an admin logo request.
func (h *Hub) pageForLogo(w http.ResponseWriter, r *http.Request) *store.StatusPage {
	id, ok := pathID(r)
	if !ok {
		writeErr(w, 400, "invalid id")
		return nil
	}
	p, err := h.store.StatusPage(id)
	if errors.Is(err, store.ErrNotFound) {
		writeErr(w, 404, "status page not found")
		return nil
	} else if err != nil {
		h.internalErr(w, err)
		return nil
	}
	return p
}

// uploadPageLogo sets the logo from the raw file in the request body.
func (h *Hub) uploadPageLogo(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageForLogo(w, r)
	if p == nil {
		return
	}
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLogoBytes))
	if err != nil {
		writeErr(w, 413, fmt.Sprintf("the logo is larger than %d MB", maxLogoBytes>>20))
		return
	}
	h.storePageLogo(w, p, data)
}

// importPageLogo sets the logo from an image the hub downloads.
func (h *Hub) importPageLogo(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var body struct {
		URL string `json:"url"`
	}
	if readJSON(r, &body) != nil {
		writeErr(w, 400, "invalid body")
		return
	}
	p := h.pageForLogo(w, r)
	if p == nil {
		return
	}
	data, err := fetchLogo(r.Context(), body.URL)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	h.storePageLogo(w, p, data)
}

func (h *Hub) storePageLogo(w http.ResponseWriter, p *store.StatusPage, data []byte) {
	l, err := processLogo(data)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	l.PageID = p.ID
	if err := h.store.SetPageLogo(l); err != nil {
		h.internalErr(w, err)
		return
	}
	h.reloadPageDomains() // drops the cached public payloads
	info, _ := h.store.PageLogoInfo(p.ID)
	writeJSON(w, 200, map[string]any{"logo": publicLogoOf(info)})
}

func (h *Hub) deletePageLogo(w http.ResponseWriter, r *http.Request, _ *store.User) {
	p := h.pageForLogo(w, r)
	if p == nil {
		return
	}
	if err := h.store.DeletePageLogo(p.ID); err != nil {
		h.internalErr(w, err)
		return
	}
	h.reloadPageDomains()
	writeJSON(w, 200, map[string]bool{"ok": true})
}

// publicLogo tells the page which logo files exist. The files are at
// <page JSON path>/logo/logo.{svg|avif|webp|png}?v=<v>.
type publicLogo struct {
	SVG    bool  `json:"svg"`
	Width  int   `json:"width"`
	Height int   `json:"height"`
	V      int64 `json:"v"`
}

func publicLogoOf(l *store.PageLogoInfo) *publicLogo {
	if l == nil {
		return nil
	}
	return &publicLogo{SVG: l.SVG, Width: l.Width, Height: l.Height, V: l.UpdatedAt}
}

func (h *Hub) publicPageLogo(w http.ResponseWriter, r *http.Request) {
	h.servePageLogo(w, r, strings.ToLower(r.PathValue("slug")), r.PathValue("file"), false)
}

// servePageLogo serves one format of a page logo, with the same access
// rules as the page itself.
func (h *Hub) servePageLogo(w http.ResponseWriter, r *http.Request, slug, file string, byDomain bool) {
	page, err := h.cachedPublicPage(slug)
	if err != nil || (!page.Published && (byDomain || h.currentUser(r) == nil)) || page.Logo == nil {
		http.NotFound(w, r)
		return
	}
	if !h.pageOpen(r, page, byDomain) {
		http.Error(w, "password required", http.StatusForbidden)
		return
	}
	l, err := h.store.PageLogo(page.id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	var data []byte
	var ctype string
	switch file {
	case "logo.svg":
		data, ctype = l.SVG, "image/svg+xml"
	case "logo.avif":
		data, ctype = l.AVIF, "image/avif"
	case "logo.webp":
		data, ctype = l.WebP, "image/webp"
	case "logo.png":
		data, ctype = l.PNG, "image/png"
	}
	if len(data) == 0 {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", ctype)
	// Nothing in the file may run, even when it is opened directly.
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; sandbox")
	if page.Published && page.passHash == "" {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		// The URL changes with the logo (?v=), so it can be kept for long.
		if r.URL.Query().Get("v") == strconv.FormatInt(l.UpdatedAt, 10) {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=300")
		}
	} else {
		w.Header().Set("Cache-Control", "private, no-cache")
	}
	http.ServeContent(w, r, file, time.UnixMilli(l.UpdatedAt), bytes.NewReader(data))
}

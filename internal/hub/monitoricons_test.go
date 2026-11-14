package hub

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestFaviconPage(t *testing.T) {
	for _, c := range []struct{ typ, target, want string }{
		{"http", "https://app.example.com/health?x=1", "https://app.example.com/"},
		{"keyword", "http://example.com:8080/a", "http://example.com:8080/"},
		{"websocket", "wss://ws.example.com/socket", "https://ws.example.com/"},
		{"statuspage", "https://status.openai.com", "https://status.openai.com/"},
		{"http", "http://192.168.1.10/", ""},
		{"http", "http://nas/", ""},
		{"tcp", "db.example.co.uk:5432", "https://example.co.uk/"},
		{"ping", "server1.example.com", "https://example.com/"},
		{"smtp", "smtps://smtp.gmail.com:465", "https://gmail.com/"},
		{"ping", "10.0.0.1", ""},
		{"ping", "[2001:db8::1]:22", ""},
		{"push", "", ""},
		{"postgres", "postgres://u:p@db.example.com/x", ""},
	} {
		if got := faviconPage(&store.Monitor{Type: c.typ, Target: c.target}); got != c.want {
			t.Errorf("%s %s: %q, want %q", c.typ, c.target, got, c.want)
		}
	}
}

func TestIconLinks(t *testing.T) {
	page := `<!doctype html><html><head>
<link rel="stylesheet" href="/a.css">
<link rel="shortcut icon" href="/favicon.ico">
<link rel="icon" type="image/png" sizes="16x16" href="/16.png">
<link rel="icon" type="image/png" sizes="192x192" href="/192.png">
<link rel="apple-touch-icon" href="/apple.png">
<link rel="icon" href="/logo.svg?v=2">
</head><body><link rel="icon" href="/late.png"></body></html>`
	links := iconLinks([]byte(page))
	if len(links) != 5 {
		t.Fatalf("links: %+v", links)
	}
	best := links[0]
	for _, l := range links {
		if l.score > best.score {
			best = l
		}
	}
	if best.href != "/logo.svg?v=2" {
		t.Fatalf("best %+v", best)
	}
}

// testICO makes an icon with a 2x2 32-bit bitmap and, when withPNG, a
// larger PNG image too.
func testICO(withPNG bool) []byte {
	dib := new(bytes.Buffer)
	hdr := make([]byte, 40)
	binary.LittleEndian.PutUint32(hdr[0:], 40)
	binary.LittleEndian.PutUint32(hdr[4:], 2)
	binary.LittleEndian.PutUint32(hdr[8:], 4) // image + mask
	binary.LittleEndian.PutUint16(hdr[12:], 1)
	binary.LittleEndian.PutUint16(hdr[14:], 32)
	dib.Write(hdr)
	// Bottom-up BGRA: bottom row red, top row blue with one transparent pixel.
	dib.Write([]byte{0, 0, 255, 255, 0, 0, 255, 255, 255, 0, 0, 255, 255, 0, 0, 0})
	dib.Write(make([]byte, 8)) // mask, 2 rows of 4 bytes
	images := [][]byte{dib.Bytes()}
	sizes := [][2]byte{{2, 2}}
	if withPNG {
		images = append(images, testPNG(64, 64))
		sizes = append(sizes, [2]byte{64, 64})
	}
	out := new(bytes.Buffer)
	binary.Write(out, binary.LittleEndian, []uint16{0, 1, uint16(len(images))})
	off := 6 + 16*len(images)
	for i, img := range images {
		e := make([]byte, 16)
		e[0], e[1] = sizes[i][0], sizes[i][1]
		binary.LittleEndian.PutUint16(e[6:], 32)
		binary.LittleEndian.PutUint32(e[8:], uint32(len(img)))
		binary.LittleEndian.PutUint32(e[12:], uint32(off))
		off += len(img)
		out.Write(e)
	}
	for _, img := range images {
		out.Write(img)
	}
	return out.Bytes()
}

func TestDecodeICO(t *testing.T) {
	img, format, err := image.Decode(bytes.NewReader(testICO(false)))
	if err != nil || format != "ico" {
		t.Fatal(format, err)
	}
	if img.Bounds().Dx() != 2 || img.Bounds().Dy() != 2 {
		t.Fatalf("size %v", img.Bounds())
	}
	if c := color.NRGBAModel.Convert(img.At(0, 1)).(color.NRGBA); c != (color.NRGBA{R: 255, A: 255}) {
		t.Errorf("bottom left %v", c)
	}
	if c := color.NRGBAModel.Convert(img.At(0, 0)).(color.NRGBA); c != (color.NRGBA{B: 255, A: 255}) {
		t.Errorf("top left %v", c)
	}
	if _, _, _, a := img.At(1, 0).RGBA(); a != 0 {
		t.Errorf("top right not transparent")
	}
	// The largest image wins, here a PNG.
	img, _, err = image.Decode(bytes.NewReader(testICO(true)))
	if err != nil || img.Bounds().Dx() != 64 {
		t.Fatalf("png entry: %v %v", img.Bounds(), err)
	}
	if _, _, err := image.Decode(bytes.NewReader([]byte("\x00\x00\x01\x00\x01\x00garbage"))); err == nil {
		t.Fatal("garbage decoded")
	}
}

func TestProcessIcon(t *testing.T) {
	img, err := processIcon(testPNG(512, 256))
	if err != nil || img.ctype != "image/png" {
		t.Fatal(err)
	}
	cfg, _, _ := image.DecodeConfig(bytes.NewReader(img.data))
	if cfg.Width != 128 || cfg.Height != 64 {
		t.Fatalf("size %dx%d", cfg.Width, cfg.Height)
	}
	img, err = processIcon([]byte(`<svg viewBox="0 0 10 10"><script>x()</script><circle r="5"/></svg>`))
	if err != nil || img.ctype != "image/svg+xml" || strings.Contains(string(img.data), "script") {
		t.Fatalf("svg: %v %s", err, img.data)
	}
	if _, err := processIcon(testICO(false)); err != nil {
		t.Fatalf("ico: %v", err)
	}
	if _, err := processIcon([]byte("<html>no</html>")); err == nil {
		t.Fatal("html accepted")
	}
}

func TestFindFavicon(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<html><head><link rel="icon" href="/missing.svg"><link rel="icon" sizes="64x64" href="/icon.png"></head></html>`))
	})
	mux.HandleFunc("/icon.png", func(w http.ResponseWriter, r *http.Request) { w.Write(testPNG(64, 64)) })
	mux.HandleFunc("/missing.svg", http.NotFound)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Local addresses are refused.
	if _, err := findFavicon(context.Background(), srv.URL+"/"); err == nil {
		t.Fatal("fetched a local address")
	}
	defer func(old func(string, string, syscall.RawConn) error) { logoDialControl = old }(logoDialControl)
	logoDialControl = func(string, string, syscall.RawConn) error { return nil }
	img, err := findFavicon(context.Background(), srv.URL+"/")
	if err != nil || img.ctype != "image/png" {
		t.Fatalf("link: %v", err)
	}

	// No links: /favicon.ico.
	ico := http.NewServeMux()
	ico.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/favicon.ico" {
			w.Write(testICO(false))
			return
		}
		w.Write([]byte(`<html><body>hi</body></html>`))
	})
	srv2 := httptest.NewServer(ico)
	defer srv2.Close()
	if img, err = findFavicon(context.Background(), srv2.URL+"/"); err != nil || img.ctype != "image/png" {
		t.Fatalf("favicon.ico: %v", err)
	}
}

// fakeDashboardIcons serves a small Dashboard Icons catalog.
func fakeDashboardIcons(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/metadata.json", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{
			"proxmox": {"base": "svg", "aliases": ["Virtualization Platform"], "colors": {"dark": "proxmox", "light": "proxmox-light"}},
			"proxmox-backup-server": {"base": "svg", "aliases": []},
			"home-assistant": {"base": "png", "aliases": ["hass"]},
			"bad name": {"base": "svg"}
		}`))
	})
	mux.HandleFunc("/svg/proxmox.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<svg viewBox="0 0 1 1"><rect width="1" height="1" fill="#000"/></svg>`))
	})
	mux.HandleFunc("/svg/proxmox-light.svg", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`<svg viewBox="0 0 1 1"><rect width="1" height="1" fill="#fff"/></svg>`))
	})
	mux.HandleFunc("/png/home-assistant.png", func(w http.ResponseWriter, r *http.Request) { w.Write(testPNG(300, 300)) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	old := dashboardIconsBase
	dashboardIconsBase = srv.URL
	t.Cleanup(func() { dashboardIconsBase = old })
}

func TestMonitorIconAPI(t *testing.T) {
	fakeDashboardIcons(t)
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{}))
	defer srv.Close()
	admin := newClient(t, srv.URL)
	admin.do("POST", "/api/setup", map[string]string{"email": "admin@b.co", "password": "password123"})
	raw := func(method, path, ctype string, body []byte) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		res, err := admin.c.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, b
	}

	// Search.
	got := decode[struct {
		Icons []diIcon `json:"icons"`
		Total int      `json:"total"`
	}](t, admin.do("GET", "/api/dashboard-icons?q=prox", nil))
	if got.Total != 2 || got.Icons[0].Name != "proxmox" || !got.Icons[0].Dark || got.Icons[1].Dark {
		t.Fatalf("search: %+v", got)
	}
	if got := decode[struct{ Total int }](t, admin.do("GET", "/api/dashboard-icons?q=hass", nil)); got.Total != 1 {
		t.Fatalf("alias search: %+v", got)
	}
	res, body := raw("GET", "/api/dashboard-icons/proxmox?theme=dark", "", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), "#fff") || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("preview: %d %s", res.StatusCode, body)
	}
	if res, _ = raw("GET", "/api/dashboard-icons/nope", "", nil); res.StatusCode != 404 {
		t.Fatalf("unknown preview: %d", res.StatusCode)
	}

	// A Dashboard Icons icon on create, with its dark variant.
	res = admin.do("POST", "/api/monitors", map[string]any{"name": "pve", "type": "ping", "target": "10.0.0.2", "icon": "dashboard:Proxmox"})
	m := decode[store.Monitor](t, res)
	if res.StatusCode != 201 || m.Icon != "dashboard:proxmox" || m.IconURL == "" || m.IconDarkURL == "" {
		t.Fatalf("create: %d %+v", res.StatusCode, m)
	}
	res, body = raw("GET", m.IconURL, "", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), "#000") || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("icon: %d %s %v", res.StatusCode, body, res.Header)
	}
	if _, body = raw("GET", m.IconDarkURL, "", nil); !strings.Contains(string(body), "#fff") {
		t.Fatalf("dark icon: %s", body)
	}
	if res := admin.do("POST", "/api/monitors", map[string]any{"name": "x", "type": "ping", "target": "10.0.0.3", "icon": "dashboard:nope"}); res.StatusCode != 400 {
		t.Fatalf("unknown icon: %d", res.StatusCode)
	}
	if res := admin.do("POST", "/api/monitors", map[string]any{"name": "x", "type": "ping", "target": "10.0.0.3", "icon": "emoji:x"}); res.StatusCode != 400 {
		t.Fatalf("invalid icon: %d", res.StatusCode)
	}
	id := itoa(m.ID)

	// A PUT without icon keeps it; "custom" needs an upload first.
	m = decode[store.Monitor](t, admin.do("PUT", "/api/monitors/"+id, map[string]any{"name": "pve 1"}))
	if m.Icon != "dashboard:proxmox" || m.IconURL == "" {
		t.Fatalf("put kept: %+v", m)
	}
	if res := admin.do("PUT", "/api/monitors/"+id, map[string]any{"icon": "custom"}); res.StatusCode != 400 {
		t.Fatalf("custom without upload: %d", res.StatusCode)
	}

	// Upload (raw body only), then the PUT keeps "custom".
	if res, _ = raw("POST", "/api/monitors/"+id+"/icon", "image/png", testPNG(40, 40)); res.StatusCode != 415 {
		t.Fatalf("form upload: %d", res.StatusCode)
	}
	res, body = raw("POST", "/api/monitors/"+id+"/icon", "application/octet-stream", testICO(true))
	if res.StatusCode != 200 || !strings.Contains(string(body), `"icon":"custom"`) || !strings.Contains(string(body), `"icon_dark_url":""`) {
		t.Fatalf("upload: %d %s", res.StatusCode, body)
	}
	m = decode[store.Monitor](t, admin.do("PUT", "/api/monitors/"+id, map[string]any{"icon": "custom", "name": "pve"}))
	if m.Icon != "custom" || m.IconURL == "" {
		t.Fatalf("custom kept: %+v", m)
	}
	if res, _ = raw("GET", m.IconURL, "", nil); res.Header.Get("Content-Type") != "image/png" {
		t.Fatalf("custom icon type %q", res.Header.Get("Content-Type"))
	}

	// The export carries the uploaded image; the code form leaves it out.
	doc, err := h.ExportConfig(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Monitors) != 1 || len(doc.Monitors[0].IconImage) == 0 || doc.Monitors[0].IconURL != "" {
		t.Fatalf("export: %+v", doc.Monitors)
	}

	// Remove: "none".
	res, body = raw("DELETE", "/api/monitors/"+id+"/icon", "", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"icon":"none","icon_url":""`) {
		t.Fatalf("delete: %d %s", res.StatusCode, body)
	}
	if res, _ = raw("GET", "/api/monitors/"+id+"/icon", "", nil); res.StatusCode != 404 {
		t.Fatalf("icon after delete: %d", res.StatusCode)
	}

	// Back to automatic: a ping on an IP has no website.
	res, body = raw("POST", "/api/monitors/"+id+"/icon/favicon", "", nil)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"icon":"","icon_url":""`) {
		t.Fatalf("favicon: %d %s", res.StatusCode, body)
	}

	// Icons chosen without the API (imports, presets) come from iconPass.
	if err := st.SetMonitorIconSetting(m.ID, "dashboard:home-assistant"); err != nil {
		t.Fatal(err)
	}
	h.iconPass(context.Background())
	m2, _ := st.Monitor(m.ID)
	if m2.IconURL == "" || m2.IconDarkURL != "" {
		t.Fatalf("pass: %+v", m2)
	}
	ic, _ := st.MonitorIcon(m.ID)
	if cfg, _, _ := image.DecodeConfig(bytes.NewReader(ic.Image)); cfg.Width != 128 {
		t.Fatalf("png icon %d wide", cfg.Width)
	}

	// Read-only users see icons but cannot change them.
	viewer := newClient(t, srv.URL)
	admin.do("POST", "/api/users", map[string]string{"email": "v@b.co", "password": "password123", "role": "viewer"})
	viewer.do("POST", "/api/auth/login", map[string]string{"email": "v@b.co", "password": "password123"})
	if res := viewer.do("GET", m2.IconURL, nil); res.StatusCode != 200 {
		t.Fatalf("viewer get: %d", res.StatusCode)
	}
	if res := viewer.do("DELETE", "/api/monitors/"+id+"/icon", nil); res.StatusCode != 403 {
		t.Fatalf("viewer delete: %d", res.StatusCode)
	}
}

// TestMonitorIconsLive fetches real favicons and Dashboard Icons:
// PINGTXT_LIVE=1 go test ./internal/hub -run TestMonitorIconsLive -v
func TestMonitorIconsLive(t *testing.T) {
	if os.Getenv("PINGTXT_LIVE") == "" {
		t.Skip("set PINGTXT_LIVE=1")
	}
	for _, page := range []string{"https://github.com/", "https://www.proxmox.com/", "https://status.openai.com/", "https://www.cloudflare.com/"} {
		img, err := findFavicon(context.Background(), page)
		t.Logf("%s: %s %d bytes %v", page, img.ctype, len(img.data), err)
		if err != nil {
			t.Error(err)
		}
	}
	h, _ := newTestHub(t)
	for _, name := range []string{"proxmox", "github", "home-assistant", "nextcloud"} {
		ic, err := h.dashboardIcon(context.Background(), name)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s: %s %d bytes, dark %d bytes", name, ic.ImageType, len(ic.Image), len(ic.Dark))
	}
	cat, err := h.dashboardCatalog(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	icons, total := cat.search("home assistant", 5)
	t.Logf("%d icons, home assistant: %d, first %s", len(cat.icons), total, icons[0].Name)
}

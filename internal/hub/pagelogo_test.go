package hub

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"syscall"
	"testing"
	"testing/fstest"

	"github.com/federico-it/pingtxt/internal/store"
)

func TestSanitizeSVG(t *testing.T) {
	in := `<?xml version="1.0"?>
<!-- made with a tool -->
<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" xmlns:inkscape="x" width="200" height="50" onload="alert(1)">
  <script>alert(1)</script>
  <inkscape:namedview foo="bar"/>
  <style>@import url(https://evil.test/x.css); .a{fill:red}</style>
  <defs><linearGradient id="g"><stop offset="0" stop-color="#fff"/></linearGradient></defs>
  <a href="javascript:alert(1)"><rect width="10" height="10"/></a>
  <rect width="10" height="10" fill="url(#g)" style="background:url(https://evil.test/t.png)"/>
  <use xlink:href="https://evil.test/s.svg#x"/><use href="#g"/>
  <image href="https://evil.test/track.png"/>
  <foreignObject><div xmlns="http://www.w3.org/1999/xhtml">hi</div></foreignObject>
  <text x="0" y="10" inkscape:label="t">Acme &amp; co</text>
</svg>`
	out, w, h, err := sanitizeSVG([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, bad := range []string{"script", "alert", "onload", "evil.test", "inkscape", "foreignObject", "<a", "@import", "<!--", "<?xml"} {
		if strings.Contains(s, bad) {
			t.Errorf("kept %q: %s", bad, s)
		}
	}
	for _, good := range []string{`viewBox="0 0 200 50"`, `fill="url(#g)"`, `<use href="#g">`, "Acme &amp; co", "<linearGradient", `xmlns="http://www.w3.org/2000/svg"`} {
		if !strings.Contains(s, good) {
			t.Errorf("lost %q: %s", good, s)
		}
	}
	if w != 200 || h != 50 {
		t.Errorf("size %dx%d", w, h)
	}

	for _, bad := range []string{
		`<!DOCTYPE svg [<!ENTITY x "y">]><svg>&x;</svg>`,
		`<html><svg></svg></html>`,
		`<svg><rect></svg>`,
	} {
		if _, _, _, err := sanitizeSVG([]byte(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func testPNG(w, h int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 10; x < w-10; x++ { // transparent 10px borders left and right
			img.Set(x, y, color.NRGBA{R: uint8(x), G: 100, B: 200, A: 255})
		}
	}
	var b bytes.Buffer
	png.Encode(&b, img)
	return b.Bytes()
}

func TestProcessLogoRaster(t *testing.T) {
	l, err := processLogo(testPNG(2020, 400))
	if err != nil {
		t.Fatal(err)
	}
	if l.Width != 960 || l.Height != 192 {
		t.Errorf("size %dx%d, want 960x192 (trimmed to 2000x400, then scaled)", l.Width, l.Height)
	}
	if !bytes.HasPrefix(l.PNG, []byte("\x89PNG")) || !bytes.HasPrefix(l.WebP[8:], []byte("WEBP")) || !bytes.Contains(l.AVIF[:32], []byte("avif")) {
		t.Fatal("missing or wrong formats")
	}
	for _, f := range [][]byte{l.PNG, l.WebP, l.AVIF} {
		if _, _, err := image.DecodeConfig(bytes.NewReader(f)); err != nil {
			t.Errorf("output does not decode: %v", err)
		}
	}
	if _, err := processLogo([]byte("not an image")); err == nil {
		t.Error("accepted garbage")
	}
}

func TestPageLogoAPI(t *testing.T) {
	h, st := newTestHub(t)
	srv := httptest.NewServer(h.Handler(fstest.MapFS{"status/index.html": {Data: []byte("status page")}}))
	defer srv.Close()
	admin := login(t, h, st)
	do := func(method, path, ctype string, body []byte, host string, cookies ...*http.Cookie) (*http.Response, []byte) {
		t.Helper()
		req, _ := http.NewRequest(method, srv.URL+path, bytes.NewReader(body))
		if ctype != "" {
			req.Header.Set("Content-Type", ctype)
		}
		if host != "" {
			req.Host = host
		}
		for _, c := range cookies {
			req.AddCookie(c)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res, b
	}

	// The name link must be http(s).
	res, body := do("POST", "/api/status-pages", "application/json", []byte(`{"slug":"acme","title":"Acme","published":true,"header_url":"javascript:alert(1)"}`), "", admin)
	if res.StatusCode != 400 {
		t.Fatalf("javascript link: %d %s", res.StatusCode, body)
	}
	res, body = do("POST", "/api/status-pages", "application/json", []byte(`{"slug":"acme","title":"Acme","published":true,"domain":"status.acme.test","header_url":" https://acme.test/ "}`), "", admin)
	if res.StatusCode != 201 {
		t.Fatalf("create: %d %s", res.StatusCode, body)
	}
	var page store.StatusPage
	json.Unmarshal(body, &page)
	if page.HeaderURL != "https://acme.test/" {
		t.Fatalf("header_url %q", page.HeaderURL)
	}
	id := itoa(page.ID)

	// Uploads need the raw file content type (no cross-site forms).
	if res, _ = do("POST", "/api/status-pages/"+id+"/logo", "image/png", testPNG(100, 40), "", admin); res.StatusCode != 415 {
		t.Fatalf("form upload: %d", res.StatusCode)
	}
	res, body = do("POST", "/api/status-pages/"+id+"/logo", "application/octet-stream", testPNG(100, 40), "", admin)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"svg":false`) {
		t.Fatalf("upload: %d %s", res.StatusCode, body)
	}

	res, body = do("GET", "/api/public/status/acme", "", nil, "")
	var pub struct {
		HeaderURL string `json:"header_url"`
		Logo      *publicLogo
	}
	json.Unmarshal(body, &pub)
	if pub.HeaderURL != "https://acme.test/" || pub.Logo == nil || pub.Logo.Width != 80 || pub.Logo.Height != 40 {
		t.Fatalf("public page: %s", body)
	}
	v := itoa(pub.Logo.V)
	for file, ctype := range map[string]string{"logo.avif": "image/avif", "logo.webp": "image/webp", "logo.png": "image/png"} {
		res, _ = do("GET", "/api/public/status/acme/logo/"+file+"?v="+v, "", nil, "")
		if res.StatusCode != 200 || res.Header.Get("Content-Type") != ctype || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
			t.Errorf("%s: %d %v", file, res.StatusCode, res.Header)
		}
	}
	if res, _ = do("GET", "/api/public/status/acme/logo/logo.svg", "", nil, ""); res.StatusCode != 404 {
		t.Errorf("svg of a raster logo: %d", res.StatusCode)
	}
	if res, _ = do("GET", "/api/public/status/logo/logo.png", "", nil, "status.acme.test"); res.StatusCode != 200 {
		t.Errorf("custom domain: %d", res.StatusCode)
	}

	// An SVG replaces it and is served with a sandbox.
	res, body = do("POST", "/api/status-pages/"+id+"/logo", "application/octet-stream", []byte(`<svg viewBox="0 0 30 10" onclick="x()"><rect width="30" height="10"/></svg>`), "", admin)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"svg":true`) {
		t.Fatalf("svg upload: %d %s", res.StatusCode, body)
	}
	res, body = do("GET", "/api/public/status/acme/logo/logo.svg", "", nil, "")
	if res.StatusCode != 200 || strings.Contains(string(body), "onclick") || !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("svg: %d %s", res.StatusCode, body)
	}

	// Private addresses cannot be imported.
	local := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.Write(testPNG(20, 20)) }))
	defer local.Close()
	res, body = do("POST", "/api/status-pages/"+id+"/logo/url", "application/json", []byte(`{"url":"`+local.URL+`"}`), "", admin)
	if res.StatusCode != 400 || !strings.Contains(string(body), "private or local") {
		t.Fatalf("local import: %d %s", res.StatusCode, body)
	}
	defer func(old func(string, string, syscall.RawConn) error) { logoDialControl = old }(logoDialControl)
	logoDialControl = func(string, string, syscall.RawConn) error { return nil }
	res, body = do("POST", "/api/status-pages/"+id+"/logo/url", "application/json", []byte(`{"url":"`+local.URL+`/logo.png"}`), "", admin)
	if res.StatusCode != 200 || !strings.Contains(string(body), `"svg":false`) {
		t.Fatalf("import: %d %s", res.StatusCode, body)
	}

	// The config export carries the logo.
	doc, err := h.ExportConfig(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.StatusPages) != 1 || !bytes.HasPrefix(doc.StatusPages[0].Logo, []byte("\x89PNG")) {
		t.Fatal("export without logo")
	}

	if res, _ = do("DELETE", "/api/status-pages/"+id+"/logo", "", nil, "", admin); res.StatusCode != 200 {
		t.Fatalf("delete: %d", res.StatusCode)
	}
	if res, _ = do("GET", "/api/public/status/acme/logo/logo.png", "", nil, ""); res.StatusCode != 404 {
		t.Fatalf("after delete: %d", res.StatusCode)
	}
}

package hub

import (
	"bytes"
	"image"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/federico-it/pingtxt/internal/store"
)

// The variants of a Dashboard Icons icon are not always where the catalog
// says: for "apple" its "dark" file (the one for light themes) is the white
// logo, so the light theme showed white on white. The hub looks at the
// files instead and keeps the darker one for light themes.

// iconSwapMargin is how much brighter the light theme image must be than
// the dark theme one before the two are swapped, so that colored logos
// with two similar variants are left alone.
const iconSwapMargin = 0.25

// orderIconVariants returns the image for light themes and the one for
// dark themes: the darker of the two first.
func orderIconVariants(light, dark iconImage) (iconImage, iconImage) {
	if len(light.data) == 0 || len(dark.data) == 0 {
		return light, dark
	}
	l, okL := iconLuminance(light)
	d, okD := iconLuminance(dark)
	if okL && okD && l-d > iconSwapMargin {
		return dark, light
	}
	return light, dark
}

// fixIconVariants swaps the images of a stored icon when the one for light
// themes is the light one. It says whether it changed anything.
func fixIconVariants(ic *store.MonitorIcon) bool {
	if len(ic.Image) == 0 || len(ic.Dark) == 0 {
		return false
	}
	l, d := iconImage{data: ic.Image, ctype: ic.ImageType}, iconImage{data: ic.Dark, ctype: ic.DarkType}
	nl, nd := orderIconVariants(l, d)
	if bytes.Equal(nl.data, l.data) {
		return false
	}
	ic.Image, ic.ImageType, ic.Dark, ic.DarkType = nl.data, nl.ctype, nd.data, nd.ctype
	return true
}

// iconLuminance estimates how light an icon is, from 0 (black) to 1
// (white): the mean relative luminance of the visible pixels of a PNG, or
// of the colors an SVG paints with. ok is false when it cannot tell.
func iconLuminance(img iconImage) (float64, bool) {
	if img.ctype == "image/svg+xml" || looksLikeSVG(img.data) {
		return svgLuminance(img.data)
	}
	m, _, err := image.Decode(bytes.NewReader(img.data))
	if err != nil {
		return 0, false
	}
	b := m.Bounds()
	var sum, weight float64
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			r, g, bl, a := m.At(x, y).RGBA()
			if a == 0 {
				continue
			}
			// Colors are premultiplied by alpha.
			fa := float64(a)
			lum := relLuminance(float64(r)/fa, float64(g)/fa, float64(bl)/fa)
			sum += lum * fa
			weight += fa
		}
	}
	if weight == 0 {
		return 0, false
	}
	return sum / weight, true
}

var (
	svgPaintRe = regexp.MustCompile(`(?i)\b(fill|stroke|stop-color)\s*(?:=\s*["']|:)\s*([^"';}]+)`)
	svgShapeRe = regexp.MustCompile(`(?i)<(path|rect|circle|ellipse|polygon|polyline|line|text|use)\b`)
)

// svgLuminance averages the colors an SVG paints with. Shapes paint black
// when nothing sets their fill, so an SVG without any fill counts black.
func svgLuminance(data []byte) (float64, bool) {
	var sum float64
	n := 0
	fill := false
	for _, m := range svgPaintRe.FindAllSubmatch(data, -1) {
		c, ok := parseCSSColor(string(m[2]))
		if !ok {
			continue
		}
		if strings.EqualFold(string(m[1]), "fill") {
			fill = true
		}
		sum += c
		n++
	}
	if !fill && svgShapeRe.Match(data) {
		n++ // default black fill
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

var cssNamedColors = map[string][3]float64{
	"black": {0, 0, 0}, "white": {255, 255, 255}, "gray": {128, 128, 128}, "grey": {128, 128, 128},
	"silver": {192, 192, 192}, "red": {255, 0, 0}, "green": {0, 128, 0}, "blue": {0, 0, 255},
	"yellow": {255, 255, 0}, "orange": {255, 165, 0}, "purple": {128, 0, 128}, "navy": {0, 0, 128},
	"whitesmoke": {245, 245, 245}, "snow": {255, 250, 250}, "ghostwhite": {248, 248, 255},
}

// parseCSSColor reads a CSS color (hex, rgb(a), a few names) and returns
// its relative luminance; false for none, transparent, currentColor,
// gradients (url(...)) and anything else it does not know.
func parseCSSColor(s string) (float64, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimSpace(strings.TrimSuffix(s, "!important"))
	if strings.HasPrefix(s, "#") {
		h := s[1:]
		switch len(h) {
		case 3, 4:
			h = string([]byte{h[0], h[0], h[1], h[1], h[2], h[2]})
		case 6, 8:
			h = h[:6]
		default:
			return 0, false
		}
		v, err := strconv.ParseUint(h, 16, 32)
		if err != nil {
			return 0, false
		}
		return relLuminance(float64(v>>16&0xff)/255, float64(v>>8&0xff)/255, float64(v&0xff)/255), true
	}
	if rest, ok := strings.CutPrefix(s, "rgb"); ok {
		rest = strings.TrimPrefix(rest, "a")
		rest = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(rest), "("), ")")
		parts := strings.FieldsFunc(rest, func(r rune) bool { return r == ',' || r == ' ' || r == '/' })
		if len(parts) < 3 {
			return 0, false
		}
		var c [3]float64
		for i := range 3 {
			p := parts[i]
			pct := strings.HasSuffix(p, "%")
			v, err := strconv.ParseFloat(strings.TrimSuffix(p, "%"), 64)
			if err != nil {
				return 0, false
			}
			if pct {
				v = v * 255 / 100
			}
			c[i] = math.Max(0, math.Min(255, v)) / 255
		}
		return relLuminance(c[0], c[1], c[2]), true
	}
	if c, ok := cssNamedColors[s]; ok {
		return relLuminance(c[0]/255, c[1]/255, c[2]/255), true
	}
	return 0, false
}

// relLuminance is the WCAG relative luminance of an sRGB color (0-1 each).
func relLuminance(r, g, b float64) float64 {
	lin := func(c float64) float64 {
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(r) + 0.7152*lin(g) + 0.0722*lin(b)
}

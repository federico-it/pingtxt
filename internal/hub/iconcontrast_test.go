package hub

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestIconLuminance(t *testing.T) {
	svg := func(s string) iconImage { return iconImage{data: []byte(s), ctype: "image/svg+xml"} }
	pngOf := func(c color.Color) iconImage {
		m := image.NewNRGBA(image.Rect(0, 0, 4, 4))
		for x := range 2 { // half transparent pixels, which do not count
			for y := range 4 {
				m.Set(x, y, c)
			}
		}
		var b bytes.Buffer
		png.Encode(&b, m)
		return iconImage{data: b.Bytes(), ctype: "image/png"}
	}
	for _, c := range []struct {
		name string
		img  iconImage
		min  float64
		max  float64
	}{
		{"no fill: black", svg(`<svg><path d="M0 0"/></svg>`), 0, 0.01},
		{"white", svg(`<svg><path fill="#fff" d="M0 0"/></svg>`), 0.99, 1},
		{"style white", svg(`<svg><path style="fill:white" d="M0 0"/></svg>`), 0.99, 1},
		{"rgb", svg(`<svg><path fill="rgb(255, 255, 255)" d="M0 0"/></svg>`), 0.99, 1},
		{"black and white", svg(`<svg><rect fill="#000000"/><path fill="#FFFFFF"/></svg>`), 0.4, 0.6},
		{"png white", pngOf(color.White), 0.99, 1},
		{"png black", pngOf(color.Black), 0, 0.01},
	} {
		l, ok := iconLuminance(c.img)
		if !ok || l < c.min || l > c.max {
			t.Errorf("%s: %v %v", c.name, l, ok)
		}
	}
	if _, ok := iconLuminance(svg(`<svg><linearGradient><stop stop-color="currentColor"/></linearGradient></svg>`)); ok {
		t.Error("unknown colors only: want not ok")
	}
}

func TestOrderIconVariants(t *testing.T) {
	black := iconImage{data: []byte(`<svg><path d="M0 0"/></svg>`), ctype: "image/svg+xml"}
	white := iconImage{data: []byte(`<svg><path fill="#fff" d="M0 0"/></svg>`), ctype: "image/svg+xml"}
	red := iconImage{data: []byte(`<svg><path fill="#e00" d="M0 0"/></svg>`), ctype: "image/svg+xml"}
	if l, d := orderIconVariants(white, black); string(l.data) != string(black.data) || string(d.data) != string(white.data) {
		t.Fatal("white for light themes not swapped")
	}
	if l, _ := orderIconVariants(black, white); string(l.data) != string(black.data) {
		t.Fatal("right order swapped")
	}
	if l, _ := orderIconVariants(red, black); string(l.data) != string(red.data) {
		t.Fatal("similar variants swapped")
	}
	if l, d := orderIconVariants(white, iconImage{}); string(l.data) != string(white.data) || d.data != nil {
		t.Fatal("single image changed")
	}
}

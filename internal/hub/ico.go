package hub

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
)

// A decoder for Windows icons (.ico), the format of most /favicon.ico
// files. An icon holds several images; the decoder returns the largest.
// Each one is a PNG file or a bitmap without its file header (1, 4, 8, 24
// or 32 bits per pixel, bottom-up, followed by a 1-bit transparency mask).

func init() {
	image.RegisterFormat("ico", "\x00\x00\x01\x00", decodeICO, decodeICOConfig)
}

var errICO = errors.New("invalid icon file")

const maxICOBytes = 4 << 20

type icoEntry struct {
	w, h   int
	bpp    int
	size   uint32
	offset uint32
}

// icoBest reads the directory and picks the largest image, then the one
// with the most colors.
func icoBest(data []byte) (icoEntry, error) {
	if len(data) < 6 || binary.LittleEndian.Uint16(data[0:]) != 0 || binary.LittleEndian.Uint16(data[2:]) != 1 {
		return icoEntry{}, errICO
	}
	n := int(binary.LittleEndian.Uint16(data[4:]))
	if n == 0 || len(data) < 6+16*n {
		return icoEntry{}, errICO
	}
	var best icoEntry
	for i := range n {
		d := data[6+16*i:]
		e := icoEntry{w: int(d[0]), h: int(d[1]), bpp: int(binary.LittleEndian.Uint16(d[6:])),
			size: binary.LittleEndian.Uint32(d[8:]), offset: binary.LittleEndian.Uint32(d[12:])}
		if e.w == 0 {
			e.w = 256
		}
		if e.h == 0 {
			e.h = 256
		}
		if uint64(e.offset)+uint64(e.size) > uint64(len(data)) || e.size < 8 {
			continue
		}
		if e.w*e.h > best.w*best.h || (e.w*e.h == best.w*best.h && e.bpp > best.bpp) {
			best = e
		}
	}
	if best.size == 0 {
		return icoEntry{}, errICO
	}
	return best, nil
}

func readICO(r io.Reader) ([]byte, icoEntry, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxICOBytes+1))
	if err != nil {
		return nil, icoEntry{}, err
	}
	if len(data) > maxICOBytes {
		return nil, icoEntry{}, errICO
	}
	e, err := icoBest(data)
	return data, e, err
}

var pngMagic = []byte("\x89PNG\r\n\x1a\n")

func decodeICOConfig(r io.Reader) (image.Config, error) {
	data, e, err := readICO(r)
	if err != nil {
		return image.Config{}, err
	}
	sub := data[e.offset : e.offset+e.size]
	if bytes.HasPrefix(sub, pngMagic) {
		return png.DecodeConfig(bytes.NewReader(sub))
	}
	return image.Config{ColorModel: color.NRGBAModel, Width: e.w, Height: e.h}, nil
}

func decodeICO(r io.Reader) (image.Image, error) {
	data, e, err := readICO(r)
	if err != nil {
		return nil, err
	}
	sub := data[e.offset : e.offset+e.size]
	if bytes.HasPrefix(sub, pngMagic) {
		return png.Decode(bytes.NewReader(sub))
	}
	return decodeDIB(sub)
}

// decodeDIB decodes the bitmap of an icon: a BITMAPINFOHEADER whose height
// counts the image and its mask, the palette, the pixels and the mask.
func decodeDIB(b []byte) (image.Image, error) {
	if len(b) < 40 {
		return nil, errICO
	}
	hdr := int(binary.LittleEndian.Uint32(b[0:]))
	w := int(int32(binary.LittleEndian.Uint32(b[4:])))
	h := int(int32(binary.LittleEndian.Uint32(b[8:]))) / 2
	bpp := int(binary.LittleEndian.Uint16(b[14:]))
	compression := binary.LittleEndian.Uint32(b[16:])
	colors := int(binary.LittleEndian.Uint32(b[32:]))
	if hdr < 40 || hdr > len(b) || w <= 0 || h <= 0 || w > 1024 || h > 1024 || (compression != 0 && compression != 3) {
		return nil, errICO
	}
	var palette []color.NRGBA
	pos := hdr
	if compression == 3 && hdr == 40 {
		pos += 12 // BI_BITFIELDS masks after the header
	}
	switch bpp {
	case 1, 4, 8:
		if colors == 0 || colors > 1<<bpp {
			colors = 1 << bpp
		}
		if pos+4*colors > len(b) {
			return nil, errICO
		}
		for i := range colors {
			p := b[pos+4*i:]
			palette = append(palette, color.NRGBA{R: p[2], G: p[1], B: p[0], A: 255})
		}
		pos += 4 * colors
	case 24, 32:
	default:
		return nil, errICO
	}
	stride := ((w*bpp + 31) / 32) * 4
	maskStride := ((w + 31) / 32) * 4
	if pos+stride*h > len(b) {
		return nil, errICO
	}
	pix := b[pos : pos+stride*h]
	var mask []byte
	if m := pos + stride*h; m+maskStride*h <= len(b) {
		mask = b[m : m+maskStride*h]
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := range h {
		row := pix[(h-1-y)*stride:]
		for x := range w {
			var c color.NRGBA
			switch bpp {
			case 32:
				p := row[4*x:]
				c = color.NRGBA{R: p[2], G: p[1], B: p[0], A: p[3]}
				anyAlpha = anyAlpha || p[3] != 0
			case 24:
				p := row[3*x:]
				c = color.NRGBA{R: p[2], G: p[1], B: p[0], A: 255}
			default:
				bit := x * bpp
				v := int(row[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				if v < len(palette) {
					c = palette[v]
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	// 32-bit pixels may carry their own alpha; otherwise the mask makes
	// pixels transparent.
	if bpp == 32 && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 255
		}
	}
	if mask != nil && (bpp != 32 || !anyAlpha) {
		for y := range h {
			row := mask[(h-1-y)*maskStride:]
			for x := range w {
				if row[x/8]>>(7-x%8)&1 == 1 {
					img.SetNRGBA(x, y, color.NRGBA{})
				}
			}
		}
	}
	return img, nil
}

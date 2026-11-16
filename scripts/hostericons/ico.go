package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
)

// decodeICO reads the largest frame of an .ico file. A frame is a PNG or a
// headerless bitmap whose height counts the colour rows and the 1-bit
// transparency mask after them.
func decodeICO(body []byte) (image.Image, error) {
	if len(body) < 6 {
		return nil, errors.New("ico: too short")
	}
	count := int(binary.LittleEndian.Uint16(body[4:6]))
	var frame []byte
	best := -1
	for i := range count {
		e := 6 + i*16
		if e+16 > len(body) {
			break
		}
		w, h := int(body[e]), int(body[e+1])
		if w == 0 {
			w = 256
		}
		if h == 0 {
			h = 256
		}
		bpp := int(binary.LittleEndian.Uint16(body[e+6 : e+8]))
		n := int(binary.LittleEndian.Uint32(body[e+8 : e+12]))
		off := int(binary.LittleEndian.Uint32(body[e+12 : e+16]))
		if n == 0 || off+n > len(body) {
			continue
		}
		if rank := w*h*64 + bpp; rank > best {
			best, frame = rank, body[off:off+n]
		}
	}
	if frame == nil {
		return nil, errors.New("ico: no readable frame")
	}
	if bytes.HasPrefix(frame, []byte("\x89PNG")) {
		return png.Decode(bytes.NewReader(frame))
	}
	return decodeDIB(frame)
}

func decodeDIB(b []byte) (image.Image, error) {
	if len(b) < 40 {
		return nil, errors.New("ico: bitmap header too short")
	}
	le := binary.LittleEndian
	headerSize := int(le.Uint32(b[0:4]))
	w := int(int32(le.Uint32(b[4:8])))
	h := int(int32(le.Uint32(b[8:12]))) / 2
	bpp := int(le.Uint16(b[14:16]))
	if le.Uint32(b[16:20]) != 0 || w <= 0 || h <= 0 || w > 256 || h > 256 {
		return nil, errors.New("ico: unsupported bitmap")
	}
	switch bpp {
	case 1, 4, 8, 24, 32:
	default:
		return nil, errors.New("ico: unsupported bit depth")
	}
	var palette []color.NRGBA
	at := headerSize
	if bpp <= 8 {
		n := int(le.Uint32(b[32:36]))
		if n == 0 {
			n = 1 << bpp
		}
		if at+4*n > len(b) {
			return nil, errors.New("ico: palette cut short")
		}
		for i := range n {
			p := b[at+4*i:]
			palette = append(palette, color.NRGBA{p[2], p[1], p[0], 255})
		}
		at += 4 * n
	}
	stride := (w*bpp + 31) / 32 * 4
	maskStride := (w + 31) / 32 * 4
	mask := at + stride*h
	if mask > len(b) {
		return nil, errors.New("ico: pixels cut short")
	}
	hasMask := mask+maskStride*h <= len(b)

	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	anyAlpha := false
	for y := range h {
		row := b[at+(h-1-y)*stride:]
		for x := range w {
			var c color.NRGBA
			switch bpp {
			case 32:
				c = color.NRGBA{row[4*x+2], row[4*x+1], row[4*x], row[4*x+3]}
				anyAlpha = anyAlpha || c.A != 0
			case 24:
				c = color.NRGBA{row[3*x+2], row[3*x+1], row[3*x], 255}
			default:
				bit := x * bpp
				idx := int(row[bit/8]>>(8-bpp-bit%8)) & (1<<bpp - 1)
				if idx < len(palette) {
					c = palette[idx]
				}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	// A 32-bit frame carries its own alpha; the others, and old 32-bit ones
	// that leave it at zero, take it from the mask.
	if anyAlpha || !hasMask {
		return img, nil
	}
	for y := range h {
		row := b[mask+(h-1-y)*maskStride:]
		for x := range w {
			c := img.NRGBAAt(x, y)
			c.A = 255
			if row[x/8]&(0x80>>(x%8)) != 0 {
				c.A = 0
			}
			img.SetNRGBA(x, y, c)
		}
	}
	return img, nil
}

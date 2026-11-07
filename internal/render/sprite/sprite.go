// Package sprite draws glyphs procedurally, cell by cell, for characters that
// fonts render badly: box drawing (U+2500-U+257F), block elements
// (U+2580-U+259F), braille patterns (U+2800-U+28FF) and the Powerline
// symbols U+E0B0-U+E0B7. Geometry is derived from the cell size with integer
// arithmetic, so neighbouring cells join without gaps at any cell size.
//
// The package is pure Go (image only) and knows nothing about the renderer
// that calls it.
package sprite

import (
	"image"
	"image/color"
)

// Handles reports whether Draw renders r.
func Handles(r rune) bool {
	return r >= 0x2500 && r <= 0x259F ||
		r >= 0x2800 && r <= 0x28FF ||
		r >= 0xE0B0 && r <= 0xE0B7
}

// Draw paints the glyph for r in fg inside cell of dst, composited over the
// pixels already there, and reports whether r is handled. Nothing outside
// cell (or outside dst) is touched, and nothing is drawn for an unhandled
// rune. cell may be any size, for example two cells wide for a wide rune.
//
// fg is expected to be opaque: strokes that overlap within a glyph are
// composited once per pixel only when fg is opaque.
func Draw(dst *image.RGBA, r rune, cell image.Rectangle, fg color.RGBA) bool {
	if !Handles(r) {
		return false
	}
	cell = cell.Canon()
	if cell.Empty() {
		return true
	}
	c := canvas{
		dst:  dst,
		clip: cell.Intersect(dst.Bounds()),
		ox:   cell.Min.X,
		oy:   cell.Min.Y,
		w:    cell.Dx(),
		h:    cell.Dy(),
		fg:   fg,
	}
	switch {
	case r <= 0x257F:
		c.box(r)
	case r <= 0x259F:
		c.block(r)
	case r <= 0x28FF && r >= 0x2800:
		c.braille(r)
	default:
		c.powerline(r)
	}
	return true
}

// canvas is one cell of a destination image. Coordinates passed to its
// methods are relative to the cell's top-left corner.
type canvas struct {
	dst    *image.RGBA
	clip   image.Rectangle // cell intersected with the image bounds
	ox, oy int             // cell origin in dst
	w, h   int             // cell size
	fg     color.RGBA
}

// set composites fg at coverage cov (0-255) over the pixel at (x, y), if it
// lies inside the cell.
func (c *canvas) set(x, y int, cov uint32) {
	p := image.Pt(c.ox+x, c.oy+y)
	if !p.In(c.clip) {
		return
	}
	i := c.dst.PixOffset(p.X, p.Y)
	px := c.dst.Pix[i : i+4 : i+4]
	inv := 255 - scale(uint32(c.fg.A), cov)
	px[0] = uint8(min(255, scale(uint32(c.fg.R), cov)+scale(uint32(px[0]), inv)))
	px[1] = uint8(min(255, scale(uint32(c.fg.G), cov)+scale(uint32(px[1]), inv)))
	px[2] = uint8(min(255, scale(uint32(c.fg.B), cov)+scale(uint32(px[2]), inv)))
	px[3] = uint8(min(255, scale(uint32(c.fg.A), cov)+scale(uint32(px[3]), inv)))
}

// scale returns v*a/255 rounded to nearest.
func scale(v, a uint32) uint32 { return (v*a + 127) / 255 }

// fill paints the half-open rectangle [x0,x1) x [y0,y1) solid.
func (c *canvas) fill(x0, y0, x1, y1 int) {
	c.fillCov(x0, y0, x1, y1, 255)
}

// fillCov is fill at partial coverage.
func (c *canvas) fillCov(x0, y0, x1, y1 int, cov uint32) {
	x0, y0 = max(x0, 0), max(y0, 0)
	x1, y1 = min(x1, c.w), min(y1, c.h)
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c.set(x, y, cov)
		}
	}
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

package emoji

import (
	"bytes"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"slices"
	"sync"

	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font/sfnt"
)

// maxBitmapSide bounds the PNG dimensions accepted from a font, so that a
// hostile font cannot make the decoder allocate gigabytes.
const maxBitmapSide = 4096

// Backend draws a colour emoji for a grapheme cluster. Both the COLR/CPAL
// outline font (Font) and the CBLC/CBDT bitmap font (Bitmap) implement it.
type Backend interface {
	// Image renders cluster as a side x side image, or reports false if
	// the font has no glyph for it. The image is cached and shared:
	// callers must not modify it.
	Image(cluster string, side int) (*image.RGBA, bool)
}

// Bitmap is a parsed CBLC/CBDT colour bitmap font such as Apple Color Emoji
// (one 96 ppem strike, PNG images) or Noto Color Emoji. It is safe for
// concurrent use.
type Bitmap struct {
	glyphMap
	bt *bitmapTables

	mu    sync.Mutex
	cache map[renderKey]*image.RGBA
}

var _ Backend = (*Font)(nil)
var _ Backend = (*Bitmap)(nil)
var _ wholeSequencer = (*Font)(nil)
var _ wholeSequencer = (*Bitmap)(nil)

// ParseBitmap parses a CBLC/CBDT PNG font. The font needs a cmap and, for
// multi-codepoint sequences, GSUB ligatures (ccmp or liga); the glyph
// outlines are not used.
func ParseBitmap(data []byte) (*Bitmap, error) {
	sf, err := sfnt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("emoji: parse font: %w", err)
	}
	t, err := readTables(data)
	if err != nil {
		return nil, err
	}
	bt, err := parseBitmaps(t)
	if err != nil {
		return nil, err
	}
	ligs, err := parseLigatures(t)
	if err != nil {
		return nil, err
	}
	return &Bitmap{
		glyphMap: glyphMap{sf: sf, ligs: ligs},
		bt:       bt,
		cache:    make(map[renderKey]*image.RGBA),
	}, nil
}

// Image renders cluster as a side x side image, or reports false if b has
// no bitmap for it. When the font lacks the whole sequence the glyph of its
// first rune is used. The image is cached and shared: callers must not
// modify it.
func (b *Bitmap) Image(cluster string, side int) (*image.RGBA, bool) {
	return b.image(cluster, side, false)
}

// imageExact is Image without the first-rune fallback (see wholeSequencer).
func (b *Bitmap) imageExact(cluster string, side int) (*image.RGBA, bool) {
	return b.image(cluster, side, true)
}

// image implements Image and imageExact. A glyph that the cmap or a GSUB
// ligature names but for which no strike has a bitmap is not drawn: the
// result is false, never an empty image, so that a Chain moves on to its
// next font.
func (b *Bitmap) image(cluster string, side int, exact bool) (*image.RGBA, bool) {
	if side <= 0 {
		return nil, false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	resolve := b.resolve
	if exact {
		resolve = b.resolveExact
	}
	gid, ok := resolve(cluster, b.hasBitmap)
	if !ok {
		return nil, false
	}
	key := renderKey{gid, side}
	if img, ok := b.cache[key]; ok {
		return img, true
	}
	img, err := b.render(gid, side)
	if err != nil {
		return nil, false
	}
	b.cache[key] = img
	return img, true
}

// hasBitmap reports whether any strike has a bitmap for gid.
func (b *Bitmap) hasBitmap(gid uint16) bool {
	for i := range b.bt.strikes {
		if _, ok, err := b.bt.glyph(&b.bt.strikes[i], gid); ok && err == nil {
			return true
		}
	}
	return false
}

// strikeOrder lists the strikes to try for a side-pixel image: the smallest
// ppem that is at least side (so that it is only scaled down), else the
// largest; then the remaining strikes, nearest larger ones first, in case
// the preferred strike lacks the glyph (Apple's 48 ppem strike is
// incomplete).
func (bt *bitmapTables) strikeOrder(side int) []*strike {
	var larger, smaller []*strike
	for i := range bt.strikes {
		if s := &bt.strikes[i]; s.ppem >= side {
			larger = append(larger, s)
		} else {
			smaller = append(smaller, s)
		}
	}
	slices.SortStableFunc(larger, func(a, b *strike) int { return a.ppem - b.ppem })
	slices.SortStableFunc(smaller, func(a, b *strike) int { return b.ppem - a.ppem })
	return append(larger, smaller...)
}

// render decodes the PNG of gid from the best strike that has it and scales
// it to fit a side x side image, keeping its aspect ratio and centring it.
// The caller holds b.mu.
func (b *Bitmap) render(gid uint16, side int) (*image.RGBA, error) {
	for _, s := range b.bt.strikeOrder(side) {
		g, ok, err := b.bt.glyph(s, gid)
		if !ok || err != nil {
			continue
		}
		src, err := decodeBitmap(g.png)
		if err != nil {
			return nil, err
		}
		return fitSquare(src, side), nil
	}
	return nil, fmt.Errorf("%w: no usable bitmap for glyph %d", ErrBitmap, gid)
}

// decodeBitmap decodes PNG data, refusing absurd sizes before allocating.
func decodeBitmap(data []byte) (image.Image, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: PNG: %v", ErrBitmap, err)
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > maxBitmapSide || cfg.Height > maxBitmapSide {
		return nil, bitmapError("PNG size %dx%d out of range", cfg.Width, cfg.Height)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("%w: PNG: %v", ErrBitmap, err)
	}
	return img, nil
}

// fitSquare scales src, keeping its aspect ratio, to the largest size that
// fits a side x side image and centres it. Scaling filters premultiplied
// colour, so semi-transparent edges do not pick up the colour of the
// transparent pixels around them.
func fitSquare(src image.Image, side int) *image.RGBA {
	sb := src.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, side, side))
	long := max(sb.Dx(), sb.Dy())
	w := max(1, (sb.Dx()*side+long/2)/long)
	h := max(1, (sb.Dy()*side+long/2)/long)
	r := image.Rect(0, 0, w, h).Add(image.Pt((side-w)/2, (side-h)/2))
	if r.Size() == sb.Size() {
		draw.Draw(dst, r, src, sb.Min, draw.Src)
	} else {
		xdraw.CatmullRom.Scale(dst, r, src, sb, xdraw.Src, nil)
	}
	return dst
}

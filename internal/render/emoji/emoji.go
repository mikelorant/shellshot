// Package emoji draws colour emoji from a COLR version 0 / CPAL font such as
// Twemoji Mozilla. It is pure Go (no cgo): golang.org/x/image/font/sfnt
// supplies the glyph outlines and cmap, and this package reads the colour
// layers (COLR, CPAL) and the sequence ligatures (GSUB) from the font file
// itself, because x/image can draw monochrome outlines only.
//
// A Font looks up a whole grapheme cluster (a ZWJ family, a flag, a skin
// tone, a keycap, a variation-selector sequence or a single emoji) and
// renders it as a square RGBA image of any pixel size, cached per size.
package emoji

import (
	"fmt"
	"image"
	"image/draw"
	"sync"

	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

const (
	variationSelector15 = '\uFE0E' // text presentation
	variationSelector16 = '\uFE0F' // emoji presentation
)

type renderKey struct {
	glyph uint16
	side  int
}

// Font is a parsed colour emoji font. It is safe for concurrent use.
type Font struct {
	sf     *sfnt.Font
	layers map[uint16][]layer
	ligs   ligatures

	unitsPerEm int // side of the square emoji box, in font units
	top        int // y of the box's top edge in font units (above the baseline)

	mu    sync.Mutex
	buf   sfnt.Buffer
	cache map[renderKey]*image.RGBA
}

// Parse parses a COLR v0 + CPAL font. Fonts with other colour technologies
// (COLRv1, CBDT, sbix, SVG) are rejected.
func Parse(data []byte) (*Font, error) {
	sf, err := sfnt.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("emoji: parse font: %w", err)
	}
	t, err := readTables(data)
	if err != nil {
		return nil, err
	}
	layers, err := parseColor(t)
	if err != nil {
		return nil, err
	}
	ligs, err := parseLigatures(t)
	if err != nil {
		return nil, err
	}
	f := &Font{
		sf:         sf,
		layers:     layers,
		ligs:       ligs,
		unitsPerEm: int(sf.UnitsPerEm()),
		cache:      make(map[renderKey]*image.RGBA),
	}
	f.top = typoAscender(t, f.unitsPerEm)
	return f, nil
}

// typoAscender returns the OS/2 typographic ascender, which Twemoji sets to
// the top of its square emoji box, or the em size when the table is absent.
func typoAscender(t tables, unitsPerEm int) int {
	os2 := reader{b: t["OS/2"]}
	if asc := int(int16(os2.u16(68))); os2.err == nil {
		return asc
	}
	return unitsPerEm
}

// Image renders cluster as a side x side image, or reports false if f has
// no glyph for it. When the font lacks the whole sequence the glyph of its
// first rune is used. The image is cached and shared: callers must not
// modify it.
func (f *Font) Image(cluster string, side int) (*image.RGBA, bool) {
	if side <= 0 {
		return nil, false
	}
	gid, ok := f.glyphFor(cluster)
	if !ok {
		return nil, false
	}
	key := renderKey{gid, side}
	f.mu.Lock()
	defer f.mu.Unlock()
	if img, ok := f.cache[key]; ok {
		return img, true
	}
	img, err := f.render(gid, side)
	if err != nil {
		return nil, false
	}
	f.cache[key] = img
	return img, true
}

// glyphFor resolves cluster to a layered glyph: the whole sequence first
// (with and without variation selectors, since fonts disagree on whether a
// sequence includes them), then the first rune alone.
func (f *Font) glyphFor(cluster string) (uint16, bool) {
	runes := []rune(cluster)
	if len(runes) == 0 {
		return 0, false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, seq := range sequenceVariants(runes) {
		if gid, ok := f.lookup(seq); ok {
			return gid, true
		}
	}
	return f.lookup(runes[:1])
}

// lookup maps seq to a layered glyph through cmap and, for several runes,
// the GSUB ligatures. The caller holds f.mu.
func (f *Font) lookup(seq []rune) (uint16, bool) {
	gids := make([]uint16, len(seq))
	for i, r := range seq {
		g, err := f.sf.GlyphIndex(&f.buf, r)
		if err != nil || g == 0 {
			return 0, false
		}
		gids[i] = uint16(g)
	}
	gid := gids[0]
	if len(gids) > 1 {
		gid = f.ligs[seqKey(gids)]
	}
	_, ok := f.layers[gid]
	return gid, ok && gid != 0
}

// sequenceVariants lists the spellings of runes to try, most specific first:
// as given, without variation selectors, and with an emoji variation
// selector after the first rune (the form keycap sequences use).
func sequenceVariants(runes []rune) [][]rune {
	out := [][]rune{runes}
	var bare []rune
	for _, r := range runes {
		if r != variationSelector15 && r != variationSelector16 {
			bare = append(bare, r)
		}
	}
	if len(bare) != len(runes) {
		out = append(out, bare)
	}
	if len(bare) > 1 {
		out = append(out, append([]rune{bare[0], variationSelector16}, bare[1:]...))
	}
	return out
}

// render rasterises the layers of gid into a side x side image. The emoji
// box (unitsPerEm square, top edge at f.top) is scaled to fill the image.
// The caller holds f.mu.
func (f *Font) render(gid uint16, side int) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, side, side))
	var rast vector.Rasterizer
	// Load outlines at an em of side pixels, so that the emoji box is side
	// pixels tall and its top edge is f.top scaled into pixels below y=0.
	ppem := fixed.I(side)
	top := float32(f.top) * float32(side) / float32(f.unitsPerEm)
	for _, l := range f.layers[gid] {
		segs, err := f.sf.LoadGlyph(&f.buf, sfnt.GlyphIndex(l.glyph), ppem, nil)
		if err != nil {
			return nil, fmt.Errorf("emoji: layer glyph %d: %w", l.glyph, err)
		}
		rast.Reset(side, side)
		rast.DrawOp = draw.Over
		for _, s := range segs {
			pt := func(i int) (x, y float32) {
				return float32(s.Args[i].X) / 64, float32(s.Args[i].Y)/64 + top
			}
			switch s.Op {
			case sfnt.SegmentOpMoveTo:
				rast.MoveTo(pt(0))
			case sfnt.SegmentOpLineTo:
				rast.LineTo(pt(0))
			case sfnt.SegmentOpQuadTo:
				x1, y1 := pt(0)
				x2, y2 := pt(1)
				rast.QuadTo(x1, y1, x2, y2)
			case sfnt.SegmentOpCubeTo:
				x1, y1 := pt(0)
				x2, y2 := pt(1)
				x3, y3 := pt(2)
				rast.CubeTo(x1, y1, x2, y2, x3, y3)
			}
		}
		rast.Draw(img, img.Bounds(), image.NewUniform(l.color), image.Point{})
	}
	return img, nil
}

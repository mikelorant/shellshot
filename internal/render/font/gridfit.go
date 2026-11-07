package font

import (
	"image"
	"image/draw"
	"math"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
	"golang.org/x/image/vector"
)

// overshootEm is the largest round-glyph overshoot (as a fraction of the em)
// that grid fitting flattens onto the x-height, cap-height or baseline.
const overshootEm = 0.015

// knot maps a height above the baseline (px, y up) in the unfitted outline to
// its fitted height.
type knot struct{ from, to float64 }

// gridFace draws a font like opentype.Face but first fits glyph heights to
// whole pixel rows. x/image does not implement TrueType or CFF hinting (its
// font.HintingVertical/Full options leave outlines untouched), so without
// this the baseline, x-height and cap-height edges of every glyph land at an
// arbitrary fraction of a pixel row and render as a soft grey row.
//
// The fit is a piecewise-linear remap of outline y coordinates: the baseline
// stays at 0, the x-height and cap-height (and the descender depth of "p")
// are rounded to whole pixels, and round-glyph overshoot beside those zones
// is flattened onto them, like the vertical "light" mode of FreeType's
// auto-hinter. x coordinates, advances and metrics are not changed.
type gridFace struct {
	xfont.Face // the unfitted face: metrics, advances and kerning
	font       *opentype.Font
	scale      fixed.Int26_6
	knots      []knot // ascending in from; heights outside shift by the end knots

	buf  sfnt.Buffer
	rast vector.Rasterizer
	mask image.Alpha
}

// newGridFace wraps plain, which must be a HintingNone opentype face of font
// at sizePx, with grid fitting. It returns plain unchanged if the size is too
// small for the zones to be separate or the font lacks the needed metrics.
func newGridFace(plain xfont.Face, font *opentype.Font, sizePx float64) xfont.Face {
	g := &gridFace{Face: plain, font: font, scale: fixed.Int26_6(0.5 + sizePx*64)}
	m, err := font.Metrics(&g.buf, g.scale, xfont.HintingNone)
	if err != nil {
		return plain
	}
	xh, capH := float64(m.XHeight)/64, float64(m.CapHeight)/64
	if xh < 2 || capH < xh+1 {
		return plain
	}
	over := overshootEm * sizePx
	XH, CAP := math.Round(xh), math.Round(capH)
	g.knots = []knot{{-over, 0}, {0, 0}, {xh, XH}, {xh + over, XH}, {capH, CAP}, {capH + over, CAP}}
	// The descender zone: its depth is that of "p".
	if idx, err := font.GlyphIndex(&g.buf, 'p'); err == nil && idx != 0 {
		if b, _, err := font.GlyphBounds(&g.buf, idx, g.scale, xfont.HintingNone); err == nil {
			if depth := float64(b.Max.Y) / 64; depth > over+1 {
				g.knots = append([]knot{{-depth, -math.Round(depth)}}, g.knots...)
			}
		}
	}
	return g
}

// fitY returns y, a y-down outline coordinate, with its height fitted.
func (g *gridFace) fitY(y fixed.Int26_6) fixed.Int26_6 {
	up := -float64(y) / 64
	k := g.knots
	var fitted float64
	switch {
	case up <= k[0].from:
		fitted = up + k[0].to - k[0].from
	case up >= k[len(k)-1].from:
		fitted = up + k[len(k)-1].to - k[len(k)-1].from
	default:
		i := 1
		for up > k[i].from {
			i++
		}
		a, b := k[i-1], k[i]
		fitted = a.to + (up-a.from)*(b.to-a.to)/(b.from-a.from)
	}
	return fixed.Int26_6(math.Round(-fitted * 64))
}

// outline returns the fitted outline and advance of r. ok is false if the
// font has no glyph for r or loading it fails.
func (g *gridFace) outline(r rune) (segs sfnt.Segments, advance fixed.Int26_6, ok bool) {
	idx, err := g.font.GlyphIndex(&g.buf, r)
	if err != nil || idx == 0 {
		return nil, 0, false
	}
	// GlyphAdvance first: LoadGlyph's segments are invalidated by buffer reuse.
	if advance, err = g.font.GlyphAdvance(&g.buf, idx, g.scale, xfont.HintingNone); err != nil {
		return nil, 0, false
	}
	if segs, err = g.font.LoadGlyph(&g.buf, idx, g.scale, nil); err != nil {
		return nil, 0, false
	}
	for i := range segs {
		for j := range segs[i].Args {
			segs[i].Args[j].Y = g.fitY(segs[i].Args[j].Y)
		}
	}
	return segs, advance, true
}

// GlyphBounds satisfies the font.Face interface.
func (g *gridFace) GlyphBounds(r rune) (bounds fixed.Rectangle26_6, advance fixed.Int26_6, ok bool) {
	segs, advance, ok := g.outline(r)
	if !ok {
		return g.Face.GlyphBounds(r)
	}
	return segs.Bounds(), advance, true
}

// Glyph satisfies the font.Face interface. It rasterises like
// opentype.Face.Glyph, with sub-pixel x placement from dot.X.
func (g *gridFace) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	segs, advance, ok := g.outline(r)
	if !ok {
		return g.Face.Glyph(dot, r)
	}
	b := segs.Bounds().Add(dot)
	dr = image.Rect(b.Min.X.Floor(), b.Min.Y.Floor(), b.Max.X.Ceil(), b.Max.Y.Ceil())
	width, height := dr.Dx(), dr.Dy()
	if width < 0 || height < 0 {
		return image.Rectangle{}, nil, image.Point{}, 0, false
	}
	biasX := dot.X - fixed.Int26_6(dr.Min.X<<6)
	biasY := dot.Y - fixed.Int26_6(dr.Min.Y<<6)

	n := width * height
	if cap(g.mask.Pix) < n {
		g.mask.Pix = make([]uint8, 2*n)
	}
	g.mask.Pix = g.mask.Pix[:n]
	g.mask.Stride = width
	g.mask.Rect = image.Rect(0, 0, width, height)

	px := func(a fixed.Point26_6) (float32, float32) {
		return float32(a.X+biasX) / 64, float32(a.Y+biasY) / 64
	}
	g.rast.Reset(width, height)
	g.rast.DrawOp = draw.Src
	for _, s := range segs {
		switch s.Op {
		case sfnt.SegmentOpMoveTo:
			g.rast.MoveTo(px(s.Args[0]))
		case sfnt.SegmentOpLineTo:
			g.rast.LineTo(px(s.Args[0]))
		case sfnt.SegmentOpQuadTo:
			x1, y1 := px(s.Args[0])
			x2, y2 := px(s.Args[1])
			g.rast.QuadTo(x1, y1, x2, y2)
		case sfnt.SegmentOpCubeTo:
			x1, y1 := px(s.Args[0])
			x2, y2 := px(s.Args[1])
			x3, y3 := px(s.Args[2])
			g.rast.CubeTo(x1, y1, x2, y2, x3, y3)
		}
	}
	g.rast.Draw(&g.mask, g.mask.Bounds(), image.Opaque, image.Point{})
	return dr, &g.mask, g.mask.Rect.Min, advance, true
}

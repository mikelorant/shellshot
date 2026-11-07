package font

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Metrics is the terminal cell geometry in whole pixels.
type Metrics struct {
	CellWidth  int // advance of 'M', rounded, then adjusted
	CellHeight int // line height (ascent + descent + gap), rounded, then adjusted
	Ascent     int // baseline offset from cell top
	Descent    int // CellHeight - Ascent

	// UnderlineOffset is how many pixel rows below the baseline the font puts
	// the top of an underline (at least 1).
	UnderlineOffset int

	// FaceWidth is the unrounded advance of 'M' in pixels. Glyphs are placed
	// relative to it, see GlyphOffsetX.
	FaceWidth float64

	faceHeight float64 // unrounded line height
	// faceY is the offset from the cell bottom to the bottom of the face's
	// line box: the baseline's distance from the bottom minus the face's.
	faceY float64
}

// Metrics computes cell metrics for an em of sizePx from the regular face,
// following Ghostty (src/font/Metrics.zig) so exports can match it:
//
//   - the cell width is the advance of 'M' rounded to the nearest pixel (a
//     glyph is never more than half a pixel from its design pitch, where
//     rounding up would loosen 8.4 px to 9 px), the cell height the line
//     height rounded; the em is not changed;
//   - the baseline is the face's baseline offset by the rounding, rounded;
//   - the font's line gap is split above and below the text.
//
// Metrics.Adjust then applies Ghostty's adjust-cell-width/height.
func (s *Set) Metrics(sizePx float64) (Metrics, error) {
	if !(sizePx > 0) {
		return Metrics{}, fmt.Errorf("font: invalid size %v", sizePx)
	}
	adv, err := s.advanceEm()
	if err != nil {
		return Metrics{}, err
	}
	face, err := s.Face(false, false, sizePx)
	if err != nil {
		return Metrics{}, err
	}
	// Face methods are not goroutine-safe; serialise with the cache lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	fm := face.Metrics()
	descent, height := float64(fm.Descent)/64, float64(fm.Height)/64
	// Half of any line gap goes below the text.
	faceBaseline := (height-float64(fm.Ascent)/64-descent)/2 + descent
	cellH := math.Max(1, math.Round(height))
	cellBaseline := math.Round(faceBaseline - (cellH-height)/2) // from the cell bottom
	// post.underlinePosition is negative below the baseline, in font units.
	f := s.fonts[styleRegular]
	underline := -float64(f.PostTable().UnderlinePosition) / float64(f.UnitsPerEm()) * sizePx
	return Metrics{
		CellWidth:       max(1, int(math.Round(adv*sizePx))),
		CellHeight:      int(cellH),
		Ascent:          int(cellH - cellBaseline),
		Descent:         int(cellBaseline),
		UnderlineOffset: max(1, int(math.Round(underline))),
		FaceWidth:       adv * sizePx,
		faceHeight:      height,
		faceY:           cellBaseline - faceBaseline,
	}, nil
}

// advanceEm returns the advance of 'M' in the regular font, in ems, read from
// the font in units so the 1/64 px rounding of face advances is avoided.
func (s *Set) advanceEm() (float64, error) {
	f := s.fonts[styleRegular]
	var buf sfnt.Buffer
	idx, err := f.GlyphIndex(&buf, 'M')
	if err != nil || idx == 0 {
		return 0, fmt.Errorf("font: regular face has no 'M' glyph")
	}
	units := fixed.I(int(f.UnitsPerEm()))
	adv, err := f.GlyphAdvance(&buf, idx, units, xfont.HintingNone)
	if err != nil || adv <= 0 {
		return 0, fmt.Errorf("font: regular face 'M' advance: %v", err)
	}
	return float64(adv) / float64(units), nil
}

// CellAdjust changes a cell dimension like Ghostty's adjust-cell-width and
// adjust-cell-height: by a number of pixels or by a percentage of the
// font-derived size. The zero value changes nothing.
type CellAdjust struct {
	kind adjustKind
	px   int
	pct  float64 // percentage change
}

type adjustKind uint8

const (
	adjustNone adjustKind = iota
	adjustPixels
	adjustPercent
)

// ParseCellAdjust parses an integer pixel delta ("2", "-1") or a percentage
// ("20%", "-7%") with Ghostty's rules: a percentage of -100% or less gives a
// zero multiplier (the cell is then clamped to one pixel).
func ParseCellAdjust(s string) (CellAdjust, error) {
	if s == "" {
		return CellAdjust{}, fmt.Errorf("empty value")
	}
	if num, ok := strings.CutSuffix(s, "%"); ok {
		p, err := strconv.ParseFloat(num, 64)
		if err != nil || math.IsNaN(p) || math.IsInf(p, 0) {
			return CellAdjust{}, fmt.Errorf("invalid percentage %q", s)
		}
		return CellAdjust{kind: adjustPercent, pct: p}, nil
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil {
		return CellAdjust{}, fmt.Errorf("invalid adjustment %q (want pixels like -1 or a percentage like -7%%)", s)
	}
	return CellAdjust{kind: adjustPixels, px: int(n)}, nil
}

// apply returns v adjusted, at least 0.
func (a CellAdjust) apply(v int) int {
	switch a.kind {
	case adjustPixels:
		return max(0, v+a.px)
	case adjustPercent:
		return int(math.Round(float64(v) * max(0, 1+a.pct/100)))
	}
	return v
}

// String formats a like the flag value it was parsed from.
func (a CellAdjust) String() string {
	switch a.kind {
	case adjustPixels:
		return strconv.Itoa(a.px)
	case adjustPercent:
		return strconv.FormatFloat(a.pct, 'f', -1, 64) + "%"
	}
	return "0"
}

// CheckAdjust rejects adjustments that leave a cell dimension under one pixel
// or over ten times the font-derived size. Ghostty clamps instead (to one
// pixel); an export that asked for such a cell is almost certainly a mistake.
func (m Metrics) CheckAdjust(width, height CellAdjust) error {
	for _, d := range []struct {
		name string
		base int
		adj  CellAdjust
	}{{"width", m.CellWidth, width}, {"height", m.CellHeight, height}} {
		if got := d.adj.apply(d.base); got < 1 || got > 10*d.base {
			return fmt.Errorf("cell %s %s on %d px would give %d px (must be 1 to %d)", d.name, d.adj, d.base, got, 10*d.base)
		}
	}
	return nil
}

// Adjust returns m with the cell width and height adjusted, each clamped to
// at least one pixel. Changing the height moves the baseline so the text stays
// centred, as Ghostty does: half of the change goes above and half below, and
// an odd pixel goes to the edge on which the face sits furthest from centre.
func (m Metrics) Adjust(width, height CellAdjust) Metrics {
	m.CellWidth = max(1, width.apply(m.CellWidth))
	newH := max(1, height.apply(m.CellHeight))
	if newH == m.CellHeight {
		return m
	}
	half := float64(newH-m.CellHeight) / 2
	top, bottom := math.Floor(half), math.Ceil(half)
	if m.faceY-(float64(m.CellHeight)-m.faceHeight)/2 > 0 {
		top, bottom = bottom, top
	}
	m.CellHeight = newH
	m.Ascent = max(0, m.Ascent+int(top))
	m.Descent = max(0, m.Descent+int(bottom))
	m.faceY += bottom
	return m
}

// GlyphOffsetX is how far, in pixels, a glyph is moved right from the cell
// origin, as Ghostty does: centred when the cell is wider than the face's
// advance (FaceWidth). When the cell is narrower only the fractional part of
// the centring offset is kept, so glyphs stay anchored at the cell's left
// edge and overflow its right edge instead of being cut or shifted.
func (m Metrics) GlyphOffsetX() float64 {
	dx := (float64(m.CellWidth) - m.FaceWidth) / 2
	if dx < 0 {
		dx -= math.Trunc(dx)
	}
	return dx
}

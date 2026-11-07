package font

import (
	"fmt"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Outline is the vector outline of one glyph.
type Outline struct {
	// Segments are the contour segments in font units, y growing downwards
	// from the baseline (so ink above it is negative), with the glyph origin
	// at x 0. The slice is the
	// caller's.
	Segments sfnt.Segments
	// UnitsPerEm is the number of font units in an em.
	UnitsPerEm int
	// Advance is the glyph's advance in font units.
	Advance int
	// Primary reports that the glyph is from the primary family, whose
	// outlines are used at the em size as they are. A fallback glyph comes
	// from a font of another design and needs fitting to its cells (see
	// FitFace).
	Primary bool
}

// Covers reports whether some font of the Set (the primary family in the
// style, then the fallbacks) has a glyph for r.
func (s *Set) Covers(r rune, bold, italic bool) bool {
	return s.fontIndexFor(r, bold, italic) >= 0
}

// Outline returns the outline of the glyph that draws r in the style, from
// the font FaceFor would choose. ok is false when no font has r. The outline
// is not grid fitted: it is the font's own design, for vector output.
func (s *Set) Outline(r rune, bold, italic bool) (o Outline, ok bool, err error) {
	idx := s.fontIndexFor(r, bold, italic)
	if idx < 0 {
		return Outline{}, false, nil
	}
	f, name := s.fallbackOrPrimary(idx)
	var buf sfnt.Buffer
	gi, err := f.GlyphIndex(&buf, r)
	if err != nil {
		return Outline{}, false, fmt.Errorf("font: %s glyph for %U: %w", name, r, err)
	}
	upem := int(f.UnitsPerEm())
	ppem := fixed.I(upem) // one font unit per pixel: coordinates come out in font units
	segs, err := f.LoadGlyph(&buf, gi, ppem, nil)
	if err != nil {
		return Outline{}, false, fmt.Errorf("font: %s outline of %U: %w", name, r, err)
	}
	adv, err := f.GlyphAdvance(&buf, gi, ppem, xfont.HintingNone)
	if err != nil {
		return Outline{}, false, fmt.Errorf("font: %s advance of %U: %w", name, r, err)
	}
	return Outline{
		Segments:   append(sfnt.Segments(nil), segs...),
		UnitsPerEm: upem,
		Advance:    adv.Round(),
		Primary:    idx < int(styleCount),
	}, true, nil
}

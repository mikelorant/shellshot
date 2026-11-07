package font

import (
	"fmt"
	"math"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Metrics is the terminal cell geometry in whole pixels.
type Metrics struct {
	CellWidth  int     // advance of 'M' at EmPx, a whole number of pixels
	CellHeight int     // the font's line height, rounded
	Ascent     int     // baseline offset from cell top
	Descent    int     // CellHeight - Ascent
	EmPx       float64 // em size to draw with so that 'M' advances exactly CellWidth

	// UnderlineOffset is how many pixel rows below the baseline the font puts
	// the top of an underline (at least 1).
	UnderlineOffset int
}

// Metrics computes cell metrics for a requested em size of sizePx from the
// regular face.
//
// Pitch policy: a glyph is drawn at a whole-pixel cell origin, so the cell
// width must be a whole number of pixels, and the cell must equal the glyph
// advance or tracking is off (rounding 8.4px up to 9px loosens it by 7%, and
// the error grows with every column). Metrics rounds the advance to the
// nearest whole pixel and returns the em size, EmPx, whose advance is exactly
// that: EmPx differs from sizePx by at most half a pixel of advance (about 4%
// at 14px). Faces for the cell grid must be created at EmPx.
//
// Line policy: CellHeight is the font's line height (ascent + descent + line
// gap) at EmPx rounded to a pixel, and the baseline sits Ascent pixels down:
// the font's ascent plus half of the rounding, so leading is shared equally
// above and below. Terminals lay rows out edge to edge; any extra leading is a
// renderer/padding decision.
func (s *Set) Metrics(sizePx float64) (Metrics, error) {
	if !(sizePx > 0) {
		return Metrics{}, fmt.Errorf("font: invalid size %v", sizePx)
	}
	em, err := s.pitchEm(sizePx)
	if err != nil {
		return Metrics{}, err
	}
	face, err := s.Face(false, false, em.px)
	if err != nil {
		return Metrics{}, err
	}
	// Face methods are not goroutine-safe; serialise with the cache lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	fm := face.Metrics()
	asc, desc := float64(fm.Ascent)/64, float64(fm.Descent)/64
	height := max(1, int(math.Round(float64(fm.Height)/64)))
	ascent := int(math.Round(asc + (float64(height)-asc-desc)/2))
	// post.underlinePosition is negative below the baseline, in font units.
	underline := -float64(s.fonts[styleRegular].PostTable().UnderlinePosition) / float64(s.fonts[styleRegular].UnitsPerEm()) * em.px
	return Metrics{
		CellWidth:       em.cellWidth,
		CellHeight:      height,
		Ascent:          ascent,
		Descent:         height - ascent,
		EmPx:            em.px,
		UnderlineOffset: max(1, int(math.Round(underline))),
	}, nil
}

// emFit is a whole-pixel cell width and the em size that produces it.
type emFit struct {
	cellWidth int
	px        float64
}

// pitchEm rounds the advance of 'M' in the regular font at sizePx to whole
// pixels and returns it with the em size at which 'M' advances exactly that.
// It scales by the design ratio advance/em, read from the font in units, so
// the 1/64 px rounding of face advances does not leak into EmPx.
func (s *Set) pitchEm(sizePx float64) (emFit, error) {
	f := s.fonts[styleRegular]
	var buf sfnt.Buffer
	idx, err := f.GlyphIndex(&buf, 'M')
	if err != nil || idx == 0 {
		return emFit{}, fmt.Errorf("font: regular face has no 'M' glyph")
	}
	units := fixed.I(int(f.UnitsPerEm()))
	adv, err := f.GlyphAdvance(&buf, idx, units, xfont.HintingNone)
	if err != nil || adv <= 0 {
		return emFit{}, fmt.Errorf("font: regular face 'M' advance: %v", err)
	}
	ratio := float64(adv) / float64(units) // advance in ems
	cellWidth := max(1, int(math.Round(sizePx*ratio)))
	return emFit{cellWidth: cellWidth, px: float64(cellWidth) / ratio}, nil
}

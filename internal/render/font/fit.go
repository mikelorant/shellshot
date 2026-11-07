package font

import (
	"fmt"
	"math"

	xfont "golang.org/x/image/font"
)

// sizeSteps is the number of quantisation steps per pixel of fitted face
// size. Fitted sizes are rounded to 1/sizeSteps px so that near-identical
// fits share one cached face.
const sizeSteps = 64

// FitFace returns the face that draws r, like FaceFor, but scales fallback
// glyphs to fit the cells they occupy. basePx is the primary em size, cellW
// and cellH the cell size in pixels, and cells the number of cells the rune
// spans (1, or 2 for a wide rune).
//
// A rune the primary family has is returned unscaled at basePx. For a
// fallback rune the face size is basePx scaled so that the glyph's advance
// equals cells*cellW, then reduced if needed so the glyph's ink box fits
// within cells*cellW by cellH. The size is quantised down to 1/64 px and the face
// cached per (font, size), so equal fits return the same face. ok is false
// when no font has r.
func (s *Set) FitFace(r rune, bold, italic bool, basePx float64, cellW, cellH, cells int) (face xfont.Face, ok bool, err error) {
	if !(basePx > 0) {
		return nil, false, fmt.Errorf("font: invalid size %v", basePx)
	}
	if cellW <= 0 || cellH <= 0 || cells <= 0 {
		return nil, false, fmt.Errorf("font: invalid cell fit %dx%d, %d cells", cellW, cellH, cells)
	}
	idx := s.fontIndexFor(r, bold, italic)
	if idx < 0 {
		return nil, false, nil
	}
	base, err := s.face(idx, basePx)
	if err != nil {
		return nil, false, err
	}
	if idx < int(styleCount) {
		return base, true, nil
	}
	budgetW := float64(cells * cellW)

	// Face methods are not goroutine-safe; serialise with the cache lock.
	s.mu.Lock()
	bounds, adv, found := base.GlyphBounds(r)
	s.mu.Unlock()
	if !found {
		return base, true, nil
	}
	scale := math.Inf(1)
	for _, c := range []struct{ have, budget float64 }{
		{float64(adv) / 64, budgetW},
		{float64(bounds.Max.X-bounds.Min.X) / 64, budgetW},
		{float64(bounds.Max.Y-bounds.Min.Y) / 64, float64(cellH)},
	} {
		if c.have > 0 {
			scale = math.Min(scale, c.budget/c.have)
		}
	}
	if math.IsInf(scale, 1) {
		return base, true, nil
	}
	size := math.Floor(basePx*scale*sizeSteps) / sizeSteps
	if !(size > 0) {
		return base, true, nil
	}
	face, err = s.face(idx, size)
	if err != nil {
		return nil, false, err
	}
	return face, true, nil
}

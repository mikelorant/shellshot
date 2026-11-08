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
// within cells*cellW by cellH. The exception is a Nerd Font icon (see
// IsIcon), which is sized by height only: at basePx, reduced if needed so its
// ink is no taller than cellH (see IconScale), and so it may be wider than
// the cells. The size is quantised down to 1/64 px and the face
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
	if s.isIcon(idx, r) {
		scale := IconScale(float64(bounds.Max.Y-bounds.Min.Y)/64, float64(cellH))
		if scale >= 1 {
			return base, true, nil
		}
		return s.quantisedFace(idx, basePx*scale, base)
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
	return s.quantisedFace(idx, basePx*scale, base)
}

// quantisedFace returns the face of font idx at sizePx rounded down to
// 1/sizeSteps px, or base if that rounds to zero.
func (s *Set) quantisedFace(idx int, sizePx float64, base xfont.Face) (xfont.Face, bool, error) {
	size := math.Floor(sizePx*sizeSteps) / sizeSteps
	if !(size > 0) {
		return base, true, nil
	}
	face, err := s.face(idx, size)
	if err != nil {
		return nil, false, err
	}
	return face, true, nil
}

// IconScale is the factor, at most 1, by which a Nerd Font icon's native size
// (the font size, one em) is reduced so that its ink, inkH pixels tall at that
// size, is no taller than cellH. The width of the icon and of the cell play no
// part: an icon wider than its cell overflows it.
func IconScale(inkH, cellH float64) float64 {
	if inkH > cellH {
		return cellH / inkH
	}
	return 1
}

// IsIcon reports whether the glyph that draws r in the style is a Nerd Font
// icon: from the embedded Symbols Nerd Font Mono, with r in a private-use
// range. Such glyphs are sized by height (see FitFace) and placed at the left
// edge of their cell. Glyphs of the same font outside the private-use ranges,
// and those of any other fallback font, are fitted to their cells.
func (s *Set) IsIcon(r rune, bold, italic bool) bool {
	return s.isIcon(s.fontIndexFor(r, bold, italic), r)
}

func (s *Set) isIcon(idx int, r rune) bool {
	return s.nerdIcons && idx == int(styleCount) && isPrivateUse(r)
}

// isPrivateUse reports whether r is in the BMP private use area or one of the
// two supplementary private use planes.
func isPrivateUse(r rune) bool {
	return r >= 0xE000 && r <= 0xF8FF || r >= 0xF0000 && r <= 0xFFFFD || r >= 0x100000 && r <= 0x10FFFD
}

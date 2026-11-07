package font

import (
	"fmt"

	"golang.org/x/image/math/fixed"
)

// Metrics is the terminal cell geometry in whole pixels.
type Metrics struct {
	CellWidth  int // advance of 'M', rounded up
	CellHeight int // Ascent + Descent
	Ascent     int // baseline offset from cell top, rounded up
	Descent    int // baseline offset to cell bottom, rounded up
}

// Metrics computes cell metrics at sizePx from the regular face.
//
// Line-gap policy: the font's line gap is ignored, so CellHeight is exactly
// Ascent+Descent (each rounded up to whole pixels). Terminals lay rows out
// edge to edge; any extra leading is a renderer/padding decision.
func (s *Set) Metrics(sizePx float64) (Metrics, error) {
	face, err := s.Face(false, false, sizePx)
	if err != nil {
		return Metrics{}, err
	}
	// Face methods are not goroutine-safe; serialise with the cache lock.
	s.mu.Lock()
	defer s.mu.Unlock()
	adv, ok := face.GlyphAdvance('M')
	if !ok {
		return Metrics{}, fmt.Errorf("font: regular face has no 'M' glyph")
	}
	fm := face.Metrics()
	m := Metrics{
		CellWidth: ceilPx(adv),
		Ascent:    ceilPx(fm.Ascent),
		Descent:   ceilPx(fm.Descent),
	}
	m.CellHeight = m.Ascent + m.Descent
	return m, nil
}

func ceilPx(v fixed.Int26_6) int { return v.Ceil() }

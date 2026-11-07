package raster

import (
	"image"
	"image/color"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// cellStyle is the resolved appearance of one cell. Extension point: spans
// (colours, bold, italic, faint, inverse, ...) will be resolved here.
type cellStyle struct {
	fg           color.RGBA
	bold, italic bool
}

// resolveCellStyle maps a snapshot style to a cellStyle. For now every cell
// is drawn with the default style, so st is ignored.
func (r *Renderer) resolveCellStyle(st snapshot.Style) cellStyle {
	_ = st
	return cellStyle{fg: r.theme.Foreground}
}

// fallback is the hook for runes the primary font lacks (alternate fonts,
// tofu boxes, ...). It reports whether it drew c. Until a later task adds
// fallback fonts it draws nothing, so such runes are left blank.
func (r *Renderer) fallback(img *image.RGBA, c rune, col, row int, st cellStyle) bool {
	return false
}

// drawLine draws the runes of s.Lines[row]. A rune is placed at the column
// s.Columns reports; a wide rune draws its glyph once at its first column.
// A zero-width rune (combining mark) overlays the previous cell-occupying
// rune: font marks have zero advance and are designed to be drawn at the pen
// position after their base glyph, so the pen is advanced by the base
// glyph's advance first.
func (r *Renderer) drawLine(img *image.RGBA, s snapshot.Snapshot, row int) {
	cols := s.Columns(row)
	base := 0                                   // column of the last rune that occupies cells
	baseAdvance := fixed.I(r.metrics.CellWidth) // pen advance of that rune's glyph
	st := r.resolveCellStyle(snapshot.Style{})
	d := &xfont.Drawer{Dst: img, Src: image.NewUniform(st.fg), Face: r.regular}
	i := 0
	for _, c := range s.Lines[row] {
		col := cols[i]
		mark := s.CellWidth(row, i) == 0
		if mark {
			col = base
		}
		i++
		if col >= s.Cols {
			continue
		}
		adv, ok := r.regular.GlyphAdvance(c)
		if !mark {
			base = col
			baseAdvance = fixed.I(r.metrics.CellWidth)
			if ok {
				baseAdvance = adv
			}
		}
		if !hasInk(c) {
			continue
		}
		if !ok {
			r.fallback(img, c, col, row, st)
			continue
		}
		d.Dot = r.baselineDot(col, row)
		if mark {
			d.Dot.X += baseAdvance
		}
		d.DrawString(string(c))
	}
}

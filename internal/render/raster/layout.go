package raster

import (
	"image"
	"unicode"

	"golang.org/x/image/math/fixed"
)

// cellOrigin returns the top-left pixel of the cell at (col, row).
func (r *Renderer) cellOrigin(col, row int) image.Point {
	return image.Pt(r.pad+col*r.metrics.CellWidth, r.pad+row*r.metrics.CellHeight)
}

// baselineDot returns the glyph origin (left edge, baseline) for the cell at
// (col, row): the baseline sits Ascent pixels below the cell top.
func (r *Renderer) baselineDot(col, row int) fixed.Point26_6 {
	o := r.cellOrigin(col, row)
	return fixed.P(o.X, o.Y+r.metrics.Ascent)
}

// hasInk reports whether a rune can produce visible glyph pixels. Spaces and
// control runes are skipped without consulting the font.
func hasInk(c rune) bool {
	return c > ' ' && !unicode.IsSpace(c) && !unicode.IsControl(c)
}

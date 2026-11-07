package raster

import (
	"image"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/michaellorant/shellshot/internal/render/sprite"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// cell is one rune of a line placed on the grid with its final style. A
// zero-width rune (mark) shares the column and width of the rune before it.
type cell struct {
	c        rune
	row, col int
	width    int // cells covered, clipped to the screen
	st       cellStyle
	mark     bool
	// advance is the pen advance of the glyph that anchors this cell: its
	// own for a normal rune, the base rune's for a mark.
	advance fixed.Int26_6
}

// fallback is the hook for runes the primary font lacks (alternate fonts,
// tofu boxes, ...). It reports whether it drew c. Until a later task adds
// fallback fonts it draws nothing, so such runes are left blank.
func (r *Renderer) fallback(img *image.RGBA, c cell) bool {
	return false
}

// drawLine draws row of s in two passes so a cell's background never covers
// a neighbour's overhanging glyph: all backgrounds first, then glyphs and
// decorations.
func (r *Renderer) drawLine(img *image.RGBA, s snapshot.Snapshot, row int, cur cursorBox) {
	cells := r.rowCells(s, row, cur)
	for _, c := range cells {
		if !c.mark && c.st.bg != r.theme.Background {
			o := r.cellOrigin(c.col, c.row)
			fillRect(img, image.Rect(o.X, o.Y, o.X+c.width*r.metrics.CellWidth, o.Y+r.metrics.CellHeight), c.st.bg)
		}
	}
	d := &xfont.Drawer{Dst: img}
	for _, c := range cells {
		r.drawGlyph(img, d, c)
		if !c.mark {
			r.drawDecorations(img, c)
		}
	}
}

// rowCells places the runes of s.Lines[row] on the grid, resolving each
// rune's span style (spans index runes; columns come from s.Columns) and
// applying the block cursor. Runes starting at or beyond the last column are
// dropped.
func (r *Renderer) rowCells(s snapshot.Snapshot, row int, cur cursorBox) []cell {
	cols := s.Columns(row)
	walk := spanWalker{spans: spansOf(s, row)}
	cells := make([]cell, 0, len(cols)+1)
	base := cell{width: 1}
	covered := false // whether a cell sits under the block cursor
	i := 0
	for _, c := range s.Lines[row] {
		cl := cell{c: c, row: row, col: cols[i], width: s.CellWidth(row, i), st: r.resolveCellStyle(walk.at(i))}
		i++
		if cl.mark = cl.width == 0; cl.mark {
			cl.col, cl.width, cl.advance = base.col, base.width, base.advance
		} else {
			cl.width = min(cl.width, s.Cols-cl.col)
			cl.advance = r.glyphAdvance(cl)
			base = cl
		}
		if cl.col >= s.Cols {
			continue
		}
		if r.blockCursorAt(cur, row, cl.col) {
			cl.st.fg, cl.st.bg = r.theme.Background, r.theme.Cursor
			covered = true
		}
		cells = append(cells, cl)
	}
	if r.blockCursorAt(cur, row, cur.col) && !covered {
		st := cellStyle{fg: r.theme.Background, bg: r.theme.Cursor}
		cells = append(cells, cell{c: ' ', row: row, col: cur.col, width: 1, st: st})
	}
	return cells
}

// glyphAdvance returns the pen advance of c's glyph, or one cell if the face
// has none.
func (r *Renderer) glyphAdvance(c cell) fixed.Int26_6 {
	if adv, ok := r.face(c.st).GlyphAdvance(c.c); ok {
		return adv
	}
	return fixed.I(r.metrics.CellWidth)
}

// drawGlyph draws c's glyph in its face and foreground, or procedurally via
// the sprite package for box, block, braille and powerline runes. A wide rune draws its
// glyph once at its first column. Font marks have zero advance and are
// designed to be drawn at the pen position after their base glyph, so a
// mark's origin is moved right by the base glyph's advance.
func (r *Renderer) drawGlyph(img *image.RGBA, d *xfont.Drawer, c cell) {
	if !hasInk(c.c) {
		return
	}
	if !c.mark {
		o := r.cellOrigin(c.col, c.row)
		cellRect := image.Rect(o.X, o.Y, o.X+c.width*r.metrics.CellWidth, o.Y+r.metrics.CellHeight)
		if sprite.Draw(img, c.c, cellRect, c.st.fg) {
			return // procedurally drawn: box, block, braille, powerline
		}
	}
	face := r.face(c.st)
	if _, ok := face.GlyphAdvance(c.c); !ok {
		r.fallback(img, c)
		return
	}
	d.Face = face
	d.Src = image.NewUniform(c.st.fg)
	d.Dot = r.baselineDot(c.col, c.row)
	if c.mark {
		d.Dot.X += c.advance
	}
	d.DrawString(string(c.c))
}

// spansOf returns row's spans, treating a missing row as unstyled.
func spansOf(s snapshot.Snapshot, row int) []snapshot.Span {
	if row < len(s.Spans) {
		return s.Spans[row]
	}
	return nil
}

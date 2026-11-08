package raster

import (
	"image"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"

	"github.com/mikelorant/shellshot/internal/render/sprite"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// cell is one grapheme cluster of a line placed on the grid with its final
// style: the base rune that takes the cell's width, and the zero-width runes
// that follow it in the same cell (combining marks, variation selectors,
// joiners and the runes they join, skin tones, keycap and tag runes).
type cell struct {
	c        rune   // base rune
	marks    []rune // zero-width runes after c, in the same cell
	row, col int
	width    int // cells covered, clipped to the screen
	st       cellStyle
	// advance is the pen advance of the base glyph: where font marks
	// (zero advance) are drawn after it.
	advance fixed.Int26_6
}

// cluster returns the whole grapheme cluster of c as a string.
func (c cell) cluster() string {
	return string(c.c) + string(c.marks)
}

// fallback draws c with a fallback font when the primary face lacks its
// glyph (Nerd Font icons, ...). It reports whether it drew c. The fallback
// face is scaled to fit the c.width cells c occupies (see Fonts.FitFace) and
// its ink box is centred in that cell span, horizontally and vertically, on
// whole pixels. The result is still clipped to the cell rect as a safety
// net. A Nerd Font icon (see Fonts.IsIcon) is the exception: it is sized by
// height, its glyph origin sits on the left edge of the cell (so the ink
// starts at the glyph's own left bearing) and it is not clipped to the cell,
// so it can overflow into the cells to the right; it is drawn after every
// background of the row, so it lies over the background of those cells, and
// is clipped only to the image. A rune no font has is left blank.
func (r *Renderer) fallback(img *image.RGBA, c cell) bool {
	face, ok, err := r.fonts.FitFace(c.c, c.st.bold, c.st.italic, r.sizePx, r.metrics.CellWidth, r.metrics.CellHeight, c.width)
	if err != nil || !ok {
		return false
	}
	o := r.cellOrigin(c.col, c.row)
	cellRect := image.Rect(o.X, o.Y, o.X+c.width*r.metrics.CellWidth, o.Y+r.metrics.CellHeight)
	dot := r.baselineDot(c.col, c.row)
	icon := r.fonts.IsIcon(c.c, c.st.bold, c.st.italic)
	dst := img.SubImage(cellRect).(*image.RGBA)
	if icon {
		dst = img
	}
	if bounds, _, ok := face.GlyphBounds(c.c); ok {
		// bounds are relative to the dot, y growing downwards (top is negative).
		inkMidX := (bounds.Min.X + bounds.Max.X) / 2
		inkMidY := (bounds.Min.Y + bounds.Max.Y) / 2
		dot.X = fixed.I((cellRect.Min.X+cellRect.Max.X)/2) - inkMidX
		dot.Y = fixed.I((cellRect.Min.Y+cellRect.Max.Y)/2) - inkMidY
		dot.X = fixed.I(dot.X.Round())
		dot.Y = fixed.I(dot.Y.Round())
		if icon {
			dot.X = fixed.I(cellRect.Min.X)
		}
	}
	d := &xfont.Drawer{
		Dst:  dst,
		Src:  image.NewUniform(c.st.fg),
		Face: r.contrastFace(face, c.st.fg, c.st.bg),
		Dot:  dot,
	}
	d.DrawString(string(c.c))
	return true
}

// drawLine draws row of s in two passes so a cell's background never covers
// a neighbour's overhanging glyph: all backgrounds first, then glyphs and
// decorations.
func (r *Renderer) drawLine(img *image.RGBA, s snapshot.Snapshot, row int, cur cursorBox) {
	cells := r.rowCells(s, row, cur)
	for _, c := range cells {
		if c.st.bg != r.theme.Background {
			o := r.cellOrigin(c.col, c.row)
			fillRect(img, image.Rect(o.X, o.Y, o.X+c.width*r.metrics.CellWidth, o.Y+r.metrics.CellHeight), c.st.bg)
		}
	}
	d := &xfont.Drawer{Dst: img}
	for _, c := range cells {
		r.drawGlyph(img, d, c)
		r.drawDecorations(img, c)
	}
}

// rowCells places the grapheme clusters of s.Lines[row] on the grid,
// resolving each cluster's span style (spans index runes; columns come from
// s.Columns) and applying the block cursor. A zero-width rune joins the
// cluster before it (a leading one starts a cluster of its own). Clusters
// starting at or beyond the last column are dropped.
func (r *Renderer) rowCells(s snapshot.Snapshot, row int, cur cursorBox) []cell {
	cols := s.Columns(row)
	walk := spanWalker{spans: spansOf(s, row)}
	cells := make([]cell, 0, len(cols)+1)
	dropped := false // whether the current cluster starts off screen
	i := -1          // rune index
	for _, c := range s.Lines[row] {
		i++
		w := s.CellWidth(row, i)
		if w == 0 && len(cells) > 0 {
			if !dropped {
				cells[len(cells)-1].marks = append(cells[len(cells)-1].marks, c)
			}
			continue
		}
		cl := cell{c: c, row: row, col: cols[i], width: max(w, 1), st: r.resolveCellStyle(walk.at(i))}
		cl.width = min(cl.width, s.Cols-cl.col)
		if dropped = cl.col >= s.Cols; dropped {
			continue
		}
		cl.advance = r.glyphAdvance(cl)
		cells = append(cells, cl)
	}
	covered := false // whether a cell sits under the block cursor
	for i := range cells {
		if r.blockCursorAt(cur, row, cells[i].col) {
			cells[i].st.fg, cells[i].st.bg = r.theme.Background, r.theme.Cursor
			covered = true
		}
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

// drawGlyph draws c: procedurally via the sprite package for box, block,
// braille and powerline runes, as a colour emoji for an emoji sequence, and
// otherwise as font glyphs in the face and foreground of c. A wide cluster
// draws its glyph once, at its first column. Font marks have zero advance
// and are designed to be drawn at the pen position after their base glyph,
// so a mark's origin is moved right by the base glyph's advance.
//
// Fonts are tried in this order: sprites; for a sequence that asks for emoji
// presentation (see emojiSequence) the colour emoji font; the primary face;
// the fallback chain (see fallback); and finally, for a base rune no text
// font has, the colour emoji font.
func (r *Renderer) drawGlyph(img *image.RGBA, d *xfont.Drawer, c cell) {
	if c.st.hidden {
		return
	}
	if len(c.marks) == 0 && hasInk(c.c) {
		o := r.cellOrigin(c.col, c.row)
		cellRect := image.Rect(o.X, o.Y, o.X+c.width*r.metrics.CellWidth, o.Y+r.metrics.CellHeight)
		if sprite.Draw(img, c.c, cellRect, c.st.fg) {
			return // procedurally drawn: box, block, braille, powerline
		}
	}
	if emojiSequence(c) && r.drawEmoji(img, c) {
		return
	}
	if hasInk(c.c) {
		if _, ok := r.face(c.st).GlyphAdvance(c.c); ok {
			d.Face = r.contrastFace(r.face(c.st), c.st.fg, c.st.bg)
			d.Src = image.NewUniform(c.st.fg)
			d.Dot = r.baselineDot(c.col, c.row)
			d.Dot.X += r.glyphDX
			d.DrawString(string(c.c))
		} else if !r.fallback(img, c) && !r.drawEmoji(img, c) {
			return // no font has the base rune: leave the cell blank
		}
	}
	for _, m := range c.marks {
		r.drawMark(img, d, c, m)
	}
}

// drawMark draws the zero-width rune m of cluster c at the pen position after
// the base glyph, or through the fallback chain if the primary face lacks it.
// Runes that only steer shaping (variation selectors, joiners, tags) have no
// glyph of their own and are skipped.
func (r *Renderer) drawMark(img *image.RGBA, d *xfont.Drawer, c cell, m rune) {
	if !hasInk(m) || isFormatRune(m) {
		return
	}
	face := r.face(c.st)
	if _, ok := face.GlyphAdvance(m); !ok {
		mc := c
		mc.c, mc.marks = m, nil
		r.fallback(img, mc)
		return
	}
	d.Face = r.contrastFace(face, c.st.fg, c.st.bg)
	d.Src = image.NewUniform(c.st.fg)
	d.Dot = r.baselineDot(c.col, c.row)
	d.Dot.X += r.glyphDX + c.advance
	d.DrawString(string(m))
}

// spansOf returns row's spans, treating a missing row as unstyled.
func spansOf(s snapshot.Snapshot, row int) []snapshot.Span {
	if row < len(s.Spans) {
		return s.Spans[row]
	}
	return nil
}

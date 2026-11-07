package raster

import (
	"image"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// CursorStyle selects how the cursor is drawn.
type CursorStyle uint8

const (
	// CursorBlock fills the cell with the theme cursor colour and draws the
	// glyph under it in the theme background colour. It is the default.
	CursorBlock CursorStyle = iota
	// CursorBar draws a vertical bar at the left edge of the cell.
	CursorBar
	// CursorUnderline draws a horizontal bar along the bottom of the cell.
	CursorUnderline
	// CursorNone draws no cursor.
	CursorNone
)

// cursorBox is where the cursor is drawn: the cell (or, over a wide rune, the
// pair of cells) starting at column col of row. ok is false when no cursor is
// drawn (hidden, CursorNone, or outside the screen).
type cursorBox struct {
	row, col, width int
	ok              bool
}

// cursorCell locates the cursor in s. A cursor on the second half of a wide
// rune snaps to that rune's first column.
func (r *Renderer) cursorCell(s snapshot.Snapshot) cursorBox {
	c := s.Cursor
	if !c.Visible || r.cursor == CursorNone || c.X < 0 || c.Y < 0 || c.X >= s.Cols || c.Y >= s.Rows {
		return cursorBox{}
	}
	for i, col := range s.Columns(c.Y) {
		if w := s.CellWidth(c.Y, i); w > 0 && col <= c.X && c.X < col+w {
			return cursorBox{row: c.Y, col: col, width: w, ok: true}
		}
	}
	return cursorBox{row: c.Y, col: c.X, width: 1, ok: true}
}

// drawCursorShape draws the bar and underline cursors over the finished
// image. The block cursor is applied while the row is drawn instead.
func (r *Renderer) drawCursorShape(img *image.RGBA, b cursorBox) {
	if !b.ok {
		return
	}
	o := r.cellOrigin(b.col, b.row)
	w, h := b.width*r.metrics.CellWidth, r.metrics.CellHeight
	switch r.cursor {
	case CursorBar:
		fillRect(img, image.Rect(o.X, o.Y, o.X+r.barWidth(), o.Y+h), r.theme.Cursor)
	case CursorUnderline:
		fillRect(img, image.Rect(o.X, o.Y+h-r.thickness(), o.X+w, o.Y+h), r.theme.Cursor)
	}
}

// blockCursorAt reports whether the block cursor sits at column col of row.
func (r *Renderer) blockCursorAt(b cursorBox, row, col int) bool {
	return b.ok && r.cursor == CursorBlock && b.row == row && b.col == col
}

// barWidth is the width in pixels of the bar cursor.
func (r *Renderer) barWidth() int { return max(1, r.metrics.CellWidth/8) }

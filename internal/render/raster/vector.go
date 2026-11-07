package raster

import (
	"image/color"

	"github.com/mikelorant/shellshot/internal/render/theme"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// Layout is the cell geometry and colours of a Renderer, in final pixels
// (Scale and the cell adjustments applied). Exporters that draw vectors, such
// as SVG, take their grid from it so they place everything exactly where the
// raster output does.
type Layout struct {
	Theme  theme.Theme
	Cursor CursorStyle
	// FontSize is the em size in pixels (the --font-size times Scale).
	FontSize float64
	// Scale and Pad are the scale factor and the border around the grid in
	// pixels (already multiplied by Scale).
	Scale, Pad int
	// CellWidth, CellHeight and Ascent are the cell size in pixels and the
	// distance from the top of a cell to its text baseline.
	CellWidth, CellHeight, Ascent int
	// GlyphOffsetX is how far, in pixels, glyphs move right of the cell
	// origin (see font.Metrics.GlyphOffsetX).
	GlyphOffsetX float64
	// Thickness is the stroke height of underlines, strikethroughs and the
	// underline cursor; UnderlineTop and StrikeTop are the offsets from the
	// cell top of the first row of the underline and strikethrough.
	Thickness, UnderlineTop, StrikeTop int
	// BarWidth is the width of the bar cursor.
	BarWidth int
}

// Layout returns the geometry r draws with.
func (r *Renderer) Layout() Layout {
	return Layout{
		Theme:        r.theme,
		Cursor:       r.cursor,
		FontSize:     r.sizePx,
		Scale:        r.scale,
		Pad:          r.pad,
		CellWidth:    r.metrics.CellWidth,
		CellHeight:   r.metrics.CellHeight,
		Ascent:       r.metrics.Ascent,
		GlyphOffsetX: float64(r.glyphDX) / 64,
		Thickness:    r.thickness(),
		UnderlineTop: r.underlineTop(),
		StrikeTop:    r.strikeTop(),
		BarWidth:     r.barWidth(),
	}
}

// RowCell is one grapheme cluster of a row placed on the grid with its final
// colours, as Render draws it.
type RowCell struct {
	// Base is the rune that takes the cell's width; Marks are the zero-width
	// runes after it in the same cell.
	Base  rune
	Marks []rune
	// Col is the first column of the cell and Width the columns it covers.
	Col, Width int
	// Fg and Bg are the resolved colours: inverse, faint and invisible
	// applied, and the block cursor's swap if the cursor sits on the cell.
	Fg, Bg                                  color.RGBA
	Bold, Italic, Underline, Strike, Hidden bool
	// EmojiSequence reports that the cluster asks for emoji presentation.
	EmojiSequence bool
}

// Cluster returns the whole grapheme cluster as a string.
func (c RowCell) Cluster() string { return string(c.Base) + string(c.Marks) }

// RowCells places the clusters of row of s on the grid exactly as Render does,
// including the block cursor when the renderer's cursor is CursorBlock: the
// cell under it has the cursor colours, and a blank cell is added where the
// cursor stands beyond the text.
func (r *Renderer) RowCells(s snapshot.Snapshot, row int) []RowCell {
	cells := r.rowCells(s, row, r.cursorCell(s))
	out := make([]RowCell, len(cells))
	for i, c := range cells {
		out[i] = RowCell{
			Base: c.c, Marks: c.marks, Col: c.col, Width: c.width,
			Fg: c.st.fg, Bg: c.st.bg,
			Bold: c.st.bold, Italic: c.st.italic, Underline: c.st.underline, Strike: c.st.strike, Hidden: c.st.hidden,
			EmojiSequence: emojiSequence(c),
		}
	}
	return out
}

// CursorCell reports where Render draws the cursor of s: the first column of
// the cell under it and how many columns it covers. ok is false when no
// cursor is drawn.
func (r *Renderer) CursorCell(s snapshot.Snapshot) (row, col, width int, ok bool) {
	b := r.cursorCell(s)
	return b.row, b.col, b.width, b.ok
}

// DrawnMarks returns the marks of c that Render draws as glyphs: it skips the
// runes that only steer shaping (variation selectors, joiners, tags) and
// those without ink.
func (c RowCell) DrawnMarks() []rune {
	var out []rune
	for _, m := range c.Marks {
		if hasInk(m) && !isFormatRune(m) {
			out = append(out, m)
		}
	}
	return out
}

// HasInk reports whether a rune can produce visible glyph pixels: spaces and
// control runes cannot.
func HasInk(c rune) bool { return hasInk(c) }

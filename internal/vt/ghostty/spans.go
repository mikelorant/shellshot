package ghostty

import (
	"fmt"
	"unicode/utf8"

	libghostty "go.mitchellh.com/libghostty"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// rowLayout is the rune-index layout of one trimmed row: its styled runs and
// the runes that do not occupy exactly one cell.
type rowLayout struct {
	spans  []snapshot.Span
	widths []snapshot.Wide
}

// cellInfo describes one terminal cell: how many runes it adds to the line,
// the cells its first rune occupies, and its style.
type cellInfo struct {
	runes int
	width uint8
	style snapshot.Style
}

// rowLayout returns the spans and widths of the current row as rune-index
// entries into the row's trimmed line, which is limit runes long.
//
// Indexes are computed from the cells so they stay aligned with the text
// AppendText produces: a wide character's spacer cell adds no rune, an empty
// cell counts as one space, and a cell holding a multi-codepoint cluster
// counts one rune per codepoint. Entries are clipped at limit, so styling and
// widths past the end of the trimmed text are dropped.
func (t *terminal) rowLayout(limit int) (rowLayout, error) {
	var layout rowLayout
	if limit == 0 {
		return layout, nil
	}
	if err := t.rows.Cells(t.cells); err != nil {
		return rowLayout{}, fmt.Errorf("load cells: %w", err)
	}

	pos := 0
	for pos < limit && t.cells.Next() {
		cell, err := t.readCell()
		if err != nil {
			return rowLayout{}, err
		}
		layout.spans = appendSpan(layout.spans, pos, min(pos+cell.runes, limit), cell.style)
		layout.widths = appendWides(layout.widths, pos, limit, cell)
		pos += cell.runes
	}
	return layout, nil
}

// readCell reads the current cell. A spacer cell (the second half of a wide
// character, or the leftover cell where a wide character did not fit at the
// end of a row) has no runes and no style.
func (t *terminal) readCell() (cellInfo, error) {
	raw, err := t.cells.Raw()
	if err != nil {
		return cellInfo{}, fmt.Errorf("raw cell: %w", err)
	}
	wide, err := raw.Wide()
	if err != nil {
		return cellInfo{}, fmt.Errorf("cell width: %w", err)
	}
	if wide == libghostty.CellWideSpacerTail || wide == libghostty.CellWideSpacerHead {
		return cellInfo{}, nil
	}
	runes, err := t.cellRunes()
	if err != nil {
		return cellInfo{}, err
	}
	style, err := t.cellStyle(raw)
	if err != nil {
		return cellInfo{}, err
	}
	width := uint8(1)
	if wide == libghostty.CellWideWide {
		width = 2
	}
	return cellInfo{runes: runes, width: width, style: style}, nil
}

// cellRunes returns how many runes the current non-spacer cell contributes to
// the line: 1 for an empty cell, otherwise the number of codepoints in its
// text.
func (t *terminal) cellRunes() (int, error) {
	var err error
	t.cellBuf, err = t.cells.AppendGraphemes(t.cellBuf[:0])
	if err != nil {
		return 0, fmt.Errorf("cell text: %w", err)
	}
	return max(1, utf8.RuneCount(t.cellBuf)), nil
}

// appendWides records the runes of cell, which starts at rune index pos, that
// are not one cell wide: the first rune takes the cell's width and every
// further codepoint of a cluster is zero-width. Runes at or past limit are
// dropped.
func appendWides(wides []snapshot.Wide, pos, limit int, cell cellInfo) []snapshot.Wide {
	if cell.runes == 0 {
		return wides
	}
	if cell.width != 1 && pos < limit {
		wides = append(wides, snapshot.Wide{Rune: pos, Cells: cell.width})
	}
	for i := pos + 1; i < pos+cell.runes && i < limit; i++ {
		wides = append(wides, snapshot.Wide{Rune: i, Cells: 0})
	}
	return wides
}

// cellStyle returns the style of the current cell. A cell with no text and
// only a background colour (left by erase operations) carries that colour in
// its content rather than in a style.
func (t *terminal) cellStyle(raw *libghostty.Cell) (snapshot.Style, error) {
	tag, err := raw.ContentTag()
	if err != nil {
		return snapshot.Style{}, fmt.Errorf("cell content: %w", err)
	}
	switch tag {
	case libghostty.CellContentBgColorPalette:
		index, err := raw.ColorPalette()
		if err != nil {
			return snapshot.Style{}, fmt.Errorf("cell background: %w", err)
		}
		return snapshot.Style{Bg: snapshot.Color{Kind: snapshot.ColorPalette, Index: index}}, nil
	case libghostty.CellContentBgColorRGB:
		rgb, err := raw.ColorRGB()
		if err != nil {
			return snapshot.Style{}, fmt.Errorf("cell background: %w", err)
		}
		return snapshot.Style{Bg: rgbColor(rgb)}, nil
	}

	styled, err := raw.HasStyling()
	if err != nil {
		return snapshot.Style{}, fmt.Errorf("cell styling: %w", err)
	}
	if !styled {
		return snapshot.Style{}, nil
	}
	style, err := t.cells.Style()
	if err != nil {
		return snapshot.Style{}, fmt.Errorf("cell style: %w", err)
	}
	return convertStyle(style), nil
}

// convertStyle maps a libghostty style to a snapshot.Style. The underline
// colour and overline have no snapshot equivalent and are dropped.
func convertStyle(s *libghostty.Style) snapshot.Style {
	attrs := snapshot.Attr(0)
	for _, a := range [...]struct {
		on   bool
		attr snapshot.Attr
	}{
		{s.Bold(), snapshot.Bold},
		{s.Faint(), snapshot.Faint},
		{s.Italic(), snapshot.Italic},
		{s.Underline() != libghostty.UnderlineNone, snapshot.Underline},
		{s.Blink(), snapshot.Blink},
		{s.Inverse(), snapshot.Inverse},
		{s.Invisible(), snapshot.Invisible},
		{s.Strikethrough(), snapshot.Strike},
	} {
		if a.on {
			attrs |= a.attr
		}
	}
	return snapshot.Style{Fg: convertColor(s.FgColor()), Bg: convertColor(s.BgColor()), Attrs: attrs}
}

func convertColor(c libghostty.StyleColor) snapshot.Color {
	switch c.Tag {
	case libghostty.StyleColorPalette:
		return snapshot.Color{Kind: snapshot.ColorPalette, Index: c.Palette}
	case libghostty.StyleColorRGB:
		return rgbColor(c.RGB)
	}
	return snapshot.Color{}
}

func rgbColor(c libghostty.ColorRGB) snapshot.Color {
	return snapshot.Color{Kind: snapshot.ColorRGB, R: c.R, G: c.G, B: c.B}
}

// appendSpan adds [start, end) with the given style to spans, merging it into
// the previous span when they touch and share a style. Empty ranges and the
// zero style add nothing.
func appendSpan(spans []snapshot.Span, start, end int, style snapshot.Style) []snapshot.Span {
	if start >= end || style.IsZero() {
		return spans
	}
	if n := len(spans); n > 0 && spans[n-1].End == start && spans[n-1].Style == style {
		spans[n-1].End = end
		return spans
	}
	return append(spans, snapshot.Span{Start: start, End: end, Style: style})
}

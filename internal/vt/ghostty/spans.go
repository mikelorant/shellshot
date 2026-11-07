package ghostty

import (
	"fmt"
	"unicode/utf8"

	libghostty "go.mitchellh.com/libghostty"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

// rowLayout is the rune-index layout of one row: its styled runs, the runes
// that do not occupy exactly one cell, and how many runes of the row's text to
// keep.
type rowLayout struct {
	spans  []snapshot.Span
	widths []snapshot.Wide
	keep   int
}

// cellInfo describes one terminal cell: how many runes it adds to the line,
// the cells its first rune occupies, and its style.
type cellInfo struct {
	runes int
	width uint8
	style snapshot.Style
}

// rowLayout returns the spans and widths of the current row as rune-index
// entries into the row's kept text, and the number of runes keep of the row's
// text that are kept. trimmed is the rune count of the text with every
// trailing space removed; keep is trimmed, or more when blank cells after it
// render something (see rendersOnBlank), in which case it ends after the last
// such cell.
//
// Indexes are computed from the cells so they stay aligned with the text
// AppendText produces: a wide character's spacer cell adds no rune, an empty
// cell counts as one space, and a cell holding a multi-codepoint cluster
// counts one rune per codepoint. Entries are clipped at keep, so styling and
// widths past the end of the kept text are dropped.
func (t *terminal) rowLayout(trimmed int) (rowLayout, error) {
	if err := t.rows.Cells(t.cells); err != nil {
		return rowLayout{}, fmt.Errorf("load cells: %w", err)
	}

	var layout rowLayout
	pos, visibleEnd := 0, 0
	for t.cells.Next() {
		cell, err := t.readCell()
		if err != nil {
			return rowLayout{}, err
		}
		layout.spans = appendSpan(layout.spans, pos, pos+cell.runes, cell.style)
		layout.widths = appendWides(layout.widths, pos, cell)
		pos += cell.runes
		if cell.runes > 0 && rendersOnBlank(cell.style) {
			visibleEnd = pos
		}
	}

	layout.keep = max(trimmed, visibleEnd)
	layout.spans = clipSpans(layout.spans, layout.keep)
	layout.widths = clipWides(layout.widths, layout.keep)
	return layout, nil
}

// rendersOnBlank reports whether a blank cell with style s looks different
// from an unstyled one: it has a background colour, or it is inverted,
// underlined or struck through. A foreground colour, bold, italic, faint and
// blink only change how a glyph looks, so they show nothing on a blank.
func rendersOnBlank(s snapshot.Style) bool {
	return s.Bg.Kind != snapshot.ColorDefault ||
		s.Attrs&(snapshot.Inverse|snapshot.Underline|snapshot.Strike) != 0
}

// clipSpans drops the parts of spans at or past limit.
func clipSpans(spans []snapshot.Span, limit int) []snapshot.Span {
	for len(spans) > 0 && spans[len(spans)-1].Start >= limit {
		spans = spans[:len(spans)-1]
	}
	if n := len(spans); n > 0 && spans[n-1].End > limit {
		spans[n-1].End = limit
	}
	if len(spans) == 0 {
		return nil
	}
	return spans
}

// clipWides drops the entries at or past limit.
func clipWides(wides []snapshot.Wide, limit int) []snapshot.Wide {
	for len(wides) > 0 && wides[len(wides)-1].Rune >= limit {
		wides = wides[:len(wides)-1]
	}
	if len(wides) == 0 {
		return nil
	}
	return wides
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
// further codepoint of a cluster is zero-width.
func appendWides(wides []snapshot.Wide, pos int, cell cellInfo) []snapshot.Wide {
	if cell.runes == 0 {
		return wides
	}
	if cell.width != 1 {
		wides = append(wides, snapshot.Wide{Rune: pos, Cells: cell.width})
	}
	for i := pos + 1; i < pos+cell.runes; i++ {
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

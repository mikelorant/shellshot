package ghostty

import (
	"fmt"
	"unicode/utf8"

	libghostty "go.mitchellh.com/libghostty"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// rowSpans returns the styled runs of the current row as rune-index ranges
// into the row's trimmed line, which is limit runes long.
//
// Indexes are computed from the cells so they stay aligned with the text
// AppendText produces: a wide character's spacer cell adds no rune, an empty
// cell counts as one space, and a cell holding a multi-codepoint cluster
// counts one rune per codepoint. Spans are clipped at limit, so styling past
// the end of the trimmed text is dropped.
func (t *terminal) rowSpans(limit int) ([]snapshot.Span, error) {
	if limit == 0 {
		return nil, nil
	}
	if err := t.rows.Cells(t.cells); err != nil {
		return nil, fmt.Errorf("load cells: %w", err)
	}

	var spans []snapshot.Span
	pos := 0
	for pos < limit && t.cells.Next() {
		raw, err := t.cells.Raw()
		if err != nil {
			return nil, fmt.Errorf("raw cell: %w", err)
		}
		runes, err := t.cellRunes(raw)
		if err != nil {
			return nil, err
		}
		if runes == 0 {
			continue
		}
		style, err := t.cellStyle(raw)
		if err != nil {
			return nil, err
		}
		end := min(pos+runes, limit)
		spans = appendSpan(spans, pos, end, style)
		pos += runes
	}
	return spans, nil
}

// cellRunes returns how many runes the current cell contributes to the line:
// 0 for a wide character's spacer, 1 for an empty cell, otherwise the number
// of codepoints in its text.
func (t *terminal) cellRunes(raw *libghostty.Cell) (int, error) {
	wide, err := raw.Wide()
	if err != nil {
		return 0, fmt.Errorf("cell width: %w", err)
	}
	if wide == libghostty.CellWideSpacerTail || wide == libghostty.CellWideSpacerHead {
		return 0, nil
	}
	t.cellBuf, err = t.cells.AppendGraphemes(t.cellBuf[:0])
	if err != nil {
		return 0, fmt.Errorf("cell text: %w", err)
	}
	return max(1, utf8.RuneCount(t.cellBuf)), nil
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

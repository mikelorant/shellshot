package replay

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

const (
	ansiHome       = "\x1b[H"
	ansiEraseLine  = "\x1b[K" // erase from the cursor to the end of the line
	ansiEraseBelow = "\x1b[J" // erase from the cursor to the end of the screen
	ansiReset      = "\x1b[0m"
	ansiShowCursor = "\x1b[?25h"
	ansiHideCursor = "\x1b[?25l"
	// ansiSyncBegin and ansiSyncEnd bracket a synchronized update (DEC mode
	// 2026): a terminal that supports it holds its display until the end, so
	// it never paints a half-drawn frame. Others ignore the sequences.
	ansiSyncBegin = "\x1b[?2026h"
	ansiSyncEnd   = "\x1b[?2026l"
)

// RenderOptions selects how Render formats a snapshot.
type RenderOptions struct {
	// Clear prepares output for a terminal: draw the frame over the previous
	// one from the top-left corner, join lines with "\r\n" and position the
	// cursor.
	Clear bool
	// Color emits each styled run as SGR escapes. When false, styles are
	// ignored and the output is plain text.
	Color bool
}

// Render returns the text to write to a terminal to display s.
//
// With o.Clear set, the output is one synchronized update (see ansiSyncBegin)
// that hides the cursor, homes it, and draws the lines joined by "\r\n" (safe
// for raw-mode terminals). The screen is never cleared: each line is
// overwritten and the rest of it erased (unless the text already fills the
// row), and the last row is erased together with everything below it, so no
// blank screen is painted between two frames. The update ends by placing the
// cursor at s.Cursor (1-based in CSI) and showing or hiding it per
// s.Cursor.Visible.
//
// Without o.Clear, the output is the lines each followed by "\n", suitable for
// pipes and files. Trailing spaces are left out of a line that is written
// without colour, including those Lines keeps for a background or other style.
//
// With o.Color set, each span of s.Spans is written as "ESC[<SGR>m", the
// span's text and "ESC[0m", so a style never outlives its run, line or frame.
// Span ranges are rune indexes into the line. Render never fails on a span
// that breaks the snapshot invariants: it clamps ranges to the line, drops
// empty ones and zero styles, and trims a span that overlaps the previous one.
func Render(s snapshot.Snapshot, o RenderOptions) string {
	var b strings.Builder
	// line writes row y and returns its width in cells.
	line := func(y int) int {
		text := s.Lines[y]
		if o.Color && y < len(s.Spans) {
			writeStyledLine(&b, text, s.Spans[y])
		} else {
			// Without colour, spaces that are only there to carry a style show nothing.
			text = strings.TrimRight(text, " ")
			b.WriteString(text)
		}
		return cellWidth(s, y, text)
	}
	if !o.Clear {
		for y := range s.Lines {
			line(y)
			b.WriteByte('\n')
		}
		return b.String()
	}
	b.WriteString(ansiSyncBegin + ansiHideCursor + ansiHome)
	for y := range s.Lines {
		if y > 0 {
			b.WriteString("\r\n")
		}
		if y == len(s.Lines)-1 {
			// Erase before drawing: after a row-wide line the cursor waits
			// past the last column, where an erase would remove the last cell.
			b.WriteString(ansiEraseBelow)
		}
		w := line(y)
		if y < len(s.Lines)-1 && (s.Cols <= 0 || w < s.Cols) {
			b.WriteString(ansiEraseLine)
		}
	}
	fmt.Fprintf(&b, "\x1b[%d;%dH", s.Cursor.Y+1, s.Cursor.X+1)
	if s.Cursor.Visible {
		b.WriteString(ansiShowCursor)
	}
	b.WriteString(ansiSyncEnd)
	return b.String()
}

// cellWidth returns the number of terminal cells text, a prefix of s.Lines[y],
// occupies.
func cellWidth(s snapshot.Snapshot, y int, text string) int {
	n := utf8.RuneCountInString(text)
	if y >= len(s.Widths) {
		return n
	}
	for _, w := range s.Widths[y] {
		if w.Rune < n {
			n += int(w.Cells) - 1
		}
	}
	return n
}

// writeStyledLine writes line to b with spans applied as SGR runs.
func writeStyledLine(b *strings.Builder, line string, spans []snapshot.Span) {
	if len(spans) == 0 {
		b.WriteString(line)
		return
	}
	// offsets[i] is the byte offset of rune i; the last entry is len(line).
	offsets := make([]int, 0, len(line)+1)
	for i := range line {
		offsets = append(offsets, i)
	}
	offsets = append(offsets, len(line))
	runes := len(offsets) - 1

	pos := 0 // rune index written so far
	for _, sp := range spans {
		start, end := max(sp.Start, pos), min(sp.End, runes)
		sgr := sp.Style.SGR()
		if start >= end || sgr == "" {
			continue
		}
		b.WriteString(line[offsets[pos]:offsets[start]])
		b.WriteString("\x1b[" + sgr + "m")
		b.WriteString(line[offsets[start]:offsets[end]])
		b.WriteString(ansiReset)
		pos = end
	}
	b.WriteString(line[offsets[pos]:])
}

package replay

import (
	"fmt"
	"strings"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

const (
	// ansiHomeClear moves the cursor to the top-left and erases the screen.
	ansiHomeClear  = "\x1b[H\x1b[2J"
	ansiReset      = "\x1b[0m"
	ansiShowCursor = "\x1b[?25h"
	ansiHideCursor = "\x1b[?25l"
)

// RenderOptions selects how Render formats a snapshot.
type RenderOptions struct {
	// Clear prepares output for a terminal: clear the screen, join lines with
	// "\r\n" and position the cursor.
	Clear bool
	// Color emits each styled run as SGR escapes. When false, styles are
	// ignored and the output is plain text.
	Color bool
}

// Render returns the text to write to a terminal to display s.
//
// With o.Clear set, the output starts with cursor-home and clear-screen, joins
// lines with "\r\n" (safe for raw-mode terminals), then places the cursor at
// s.Cursor (1-based in CSI) and shows or hides it per s.Cursor.Visible.
//
// Without o.Clear, the output is the lines each followed by "\n", suitable for
// pipes and files.
//
// With o.Color set, each span of s.Spans is written as "ESC[<SGR>m", the
// span's text and "ESC[0m", so a style never outlives its run, line or frame.
// Span ranges are rune indexes into the line. Render never fails on a span
// that breaks the snapshot invariants: it clamps ranges to the line, drops
// empty ones and zero styles, and trims a span that overlaps the previous one.
func Render(s snapshot.Snapshot, o RenderOptions) string {
	var b strings.Builder
	line := func(y int) {
		if o.Color && y < len(s.Spans) {
			writeStyledLine(&b, s.Lines[y], s.Spans[y])
		} else {
			b.WriteString(s.Lines[y])
		}
	}
	if !o.Clear {
		for y := range s.Lines {
			line(y)
			b.WriteByte('\n')
		}
		return b.String()
	}
	b.WriteString(ansiHomeClear)
	for y := range s.Lines {
		if y > 0 {
			b.WriteString("\r\n")
		}
		line(y)
	}
	fmt.Fprintf(&b, "\x1b[%d;%dH", s.Cursor.Y+1, s.Cursor.X+1)
	if s.Cursor.Visible {
		b.WriteString(ansiShowCursor)
	} else {
		b.WriteString(ansiHideCursor)
	}
	return b.String()
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

package replay

import (
	"fmt"
	"strings"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

const (
	// ansiHomeClear moves the cursor to the top-left and erases the screen.
	ansiHomeClear  = "\x1b[H\x1b[2J"
	ansiShowCursor = "\x1b[?25h"
	ansiHideCursor = "\x1b[?25l"
)

// Render returns the text to write to a terminal to display s.
//
// With clear set, the output starts with cursor-home and clear-screen, joins
// lines with "\r\n" (safe for raw-mode terminals), then places the cursor at
// s.Cursor (1-based in CSI) and shows or hides it per s.Cursor.Visible.
//
// Without clear, the output is the lines each followed by "\n", suitable for
// pipes and files.
func Render(s snapshot.Snapshot, clear bool) string {
	var b strings.Builder
	if !clear {
		for _, l := range s.Lines {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		return b.String()
	}
	b.WriteString(ansiHomeClear)
	b.WriteString(strings.Join(s.Lines, "\r\n"))
	fmt.Fprintf(&b, "\x1b[%d;%dH", s.Cursor.Y+1, s.Cursor.X+1)
	if s.Cursor.Visible {
		b.WriteString(ansiShowCursor)
	} else {
		b.WriteString(ansiHideCursor)
	}
	return b.String()
}

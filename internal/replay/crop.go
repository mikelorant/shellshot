package replay

import (
	"strings"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

// Crop returns the part of s that fits in a viewport of cols x rows cells, as
// a snapshot of that size. A zero or negative cols or rows leaves that
// dimension alone, and s itself is returned when it already fits.
//
// Columns are cut at the right edge. Rows are cut like a scrolling terminal
// would: the window ends on the last row that shows anything (text, a styled
// cell, or the visible cursor), so the newest output stays in view and rows
// above scroll away. A cursor outside the window is hidden. The input is not
// modified.
func Crop(s snapshot.Snapshot, cols, rows int) snapshot.Snapshot {
	if rows <= 0 || rows >= len(s.Lines) {
		rows = len(s.Lines)
	}
	if cols <= 0 || cols >= s.Cols {
		cols = s.Cols
	}
	if rows == len(s.Lines) && cols == s.Cols {
		return s
	}
	top := 0
	if rows < len(s.Lines) {
		top = min(max(lastShownRow(s)-rows+1, 0), len(s.Lines)-rows)
	}
	out := s
	out.Cols, out.Rows = cols, rows
	out.Lines = append([]string(nil), s.Lines[top:top+rows]...)
	out.Spans = window(s.Spans, top, rows)
	out.Widths = window(s.Widths, top, rows)
	if cols < s.Cols {
		for y := range out.Lines {
			cutLine(&out, y, cols)
		}
	}
	out.Cursor.Y -= top
	if out.Cursor.Y < 0 || out.Cursor.Y >= rows {
		out.Cursor.Y = min(max(out.Cursor.Y, 0), rows-1)
		out.Cursor.Visible = false
	}
	out.Cursor.X = min(out.Cursor.X, cols-1)
	return out
}

// window returns a copy of n entries of x from top on, fewer if x is shorter
// (Spans and Widths may be nil or shorter than Lines).
func window[T any](x []T, top, n int) []T {
	if top >= len(x) {
		return nil
	}
	return append([]T(nil), x[top:min(top+n, len(x))]...)
}

// lastShownRow returns the last row of s that shows text, a styled cell or the
// visible cursor, or 0 if there is none.
func lastShownRow(s snapshot.Snapshot) int {
	last := 0
	if s.Cursor.Visible {
		last = min(max(s.Cursor.Y, 0), len(s.Lines)-1)
	}
	for y := len(s.Lines) - 1; y > last; y-- {
		if strings.TrimSpace(s.Lines[y]) != "" || y < len(s.Spans) && len(s.Spans[y]) > 0 {
			return y
		}
	}
	return last
}

// cutLine shortens row y of s (a snapshot whose slices are private to the
// caller) to the runes that lie wholly within cols cells.
func cutLine(s *snapshot.Snapshot, y, cols int) {
	starts := s.Columns(y)
	keep := len(starts)
	for i, c := range starts {
		if c+s.CellWidth(y, i) > cols {
			keep = i
			break
		}
	}
	if keep == len(starts) {
		return
	}
	runes := []rune(s.Lines[y])
	s.Lines[y] = string(runes[:keep])
	if y < len(s.Spans) {
		var spans []snapshot.Span
		for _, sp := range s.Spans[y] {
			if sp.Start < keep {
				sp.End = min(sp.End, keep)
				spans = append(spans, sp)
			}
		}
		s.Spans[y] = spans
	}
	if y < len(s.Widths) {
		var wides []snapshot.Wide
		for _, w := range s.Widths[y] {
			if w.Rune < keep {
				wides = append(wides, w)
			}
		}
		s.Widths[y] = wides
	}
}

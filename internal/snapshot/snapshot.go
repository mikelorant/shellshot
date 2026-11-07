// Package snapshot defines the immutable, renderer-agnostic view of a
// terminal screen at a single moment. It has no dependencies so every
// other package can share it.
package snapshot

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// Snapshot is a copy of the visible screen. It never aliases terminal memory.
type Snapshot struct {
	// Cols and Rows are the screen dimensions in cells.
	Cols, Rows int
	// Lines holds exactly Rows entries of plain text, right-trimmed.
	Lines []string
	// Spans holds styling for Lines: either nil/empty (everything unstyled)
	// or exactly Rows entries, one slice per line. Each line's spans cover
	// only non-zero styles, are sorted by Start and do not overlap.
	Spans [][]Span
	// Widths records, sparsely, the runes of Lines that do not occupy exactly
	// one terminal cell: either nil/empty (every rune is one cell) or exactly
	// Rows entries, one slice per line. See Wide.
	Widths [][]Wide
	// Cursor is the cursor position (0-based) and visibility.
	Cursor Cursor
	// Title is the window title last set by the program, if any.
	Title string
}

// Wide marks one rune of a line that does not occupy exactly one cell.
// Rune is a rune index into the line (not a byte index and not a cell
// column); Cells is the number of terminal cells the rune occupies: 2 for a
// wide rune, 0 for a zero-width rune such as a combining mark. Runes without
// an entry occupy exactly one cell.
type Wide struct {
	Rune  int
	Cells uint8
}

// Cursor describes the cursor state.
type Cursor struct {
	X, Y    int
	Visible bool
}

// Frame is a Snapshot stamped with the time since the session started.
type Frame struct {
	At       time.Duration
	Snapshot Snapshot
}

// Text returns the screen as a single string, lines joined by "\n".
func (s Snapshot) Text() string {
	n := 0
	for _, l := range s.Lines {
		n += len(l) + 1
	}
	b := make([]byte, 0, n)
	for i, l := range s.Lines {
		if i > 0 {
			b = append(b, '\n')
		}
		b = append(b, l...)
	}
	return string(b)
}

// IsBlank reports whether the screen shows nothing: no text other than
// spaces and no styled cells. The cursor and the title are not content, so a
// fresh screen with only a cursor is blank.
func (s Snapshot) IsBlank() bool {
	for _, l := range s.Lines {
		if strings.TrimSpace(l) != "" {
			return false
		}
	}
	for _, spans := range s.Spans {
		if len(spans) > 0 {
			return false
		}
	}
	return true
}

// Equal reports whether two snapshots show the same screen content.
func (s Snapshot) Equal(o Snapshot) bool {
	if s.Cols != o.Cols || s.Rows != o.Rows || s.Cursor != o.Cursor || s.Title != o.Title || len(s.Lines) != len(o.Lines) {
		return false
	}
	for i := range s.Lines {
		if s.Lines[i] != o.Lines[i] {
			return false
		}
	}
	n := max(len(s.Spans), len(o.Spans))
	for i := 0; i < n; i++ {
		if !spansEqual(spansAt(s.Spans, i), spansAt(o.Spans, i)) {
			return false
		}
	}
	n = max(len(s.Widths), len(o.Widths))
	for i := 0; i < n; i++ {
		if !widesEqual(widesAt(s.Widths, i), widesAt(o.Widths, i)) {
			return false
		}
	}
	return true
}

// widesAt returns line i's width entries, treating a missing line as all
// single-cell.
func widesAt(widths [][]Wide, i int) []Wide {
	if i < len(widths) {
		return widths[i]
	}
	return nil
}

func widesEqual(a, b []Wide) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Columns returns, for each rune of Lines[row], the cell column where that
// rune starts (a prefix sum of the runes' widths). Spans and Widths index
// runes, so Columns converts a rune index to a cell column. It returns nil
// if row is out of range.
func (s Snapshot) Columns(row int) []int {
	if row < 0 || row >= len(s.Lines) {
		return nil
	}
	cols := make([]int, 0, len(s.Lines[row]))
	wides := widesAt(s.Widths, row)
	col, next := 0, 0
	idx := 0
	for range s.Lines[row] {
		cols = append(cols, col)
		w := 1
		for next < len(wides) && wides[next].Rune < idx {
			next++
		}
		if next < len(wides) && wides[next].Rune == idx {
			w = int(wides[next].Cells)
		}
		col += w
		idx++
	}
	return cols
}

// CellWidth returns the number of cells the rune at runeIdx of Lines[row]
// occupies: the Widths entry if there is one, otherwise 1.
func (s Snapshot) CellWidth(row, runeIdx int) int {
	wides := widesAt(s.Widths, row)
	i := sort.Search(len(wides), func(i int) bool { return wides[i].Rune >= runeIdx })
	if i < len(wides) && wides[i].Rune == runeIdx {
		return int(wides[i].Cells)
	}
	return 1
}

// spansAt returns line i's spans, treating a missing line as unstyled.
func spansAt(spans [][]Span, i int) []Span {
	if i < len(spans) {
		return spans[i]
	}
	return nil
}

func spansEqual(a, b []Span) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// Validate checks the snapshot's invariants: Lines has Rows entries; Spans is
// empty or has Rows entries; and each line's spans are non-empty ranges
// within the line's rune count, with non-zero styles, sorted and not
// overlapping. Widths is empty or has Rows entries, and each line's entries
// have rune indexes within the line's rune count, sorted and unique, with
// Cells of 0 or 2.
func (s Snapshot) Validate() error {
	if len(s.Lines) != s.Rows {
		return fmt.Errorf("snapshot: %d lines for %d rows", len(s.Lines), s.Rows)
	}
	if err := s.validateSpans(); err != nil {
		return err
	}
	return s.validateWidths()
}

func (s Snapshot) validateSpans() error {
	if len(s.Spans) == 0 {
		return nil
	}
	if len(s.Spans) != s.Rows {
		return fmt.Errorf("snapshot: %d span lines for %d rows", len(s.Spans), s.Rows)
	}
	for y, spans := range s.Spans {
		runes := utf8.RuneCountInString(s.Lines[y])
		prevEnd := 0
		for i, sp := range spans {
			switch {
			case sp.Start >= sp.End:
				return fmt.Errorf("snapshot: line %d span %d: empty or inverted range [%d,%d)", y, i, sp.Start, sp.End)
			case sp.Start < 0 || sp.End > runes:
				return fmt.Errorf("snapshot: line %d span %d: range [%d,%d) outside %d runes", y, i, sp.Start, sp.End, runes)
			case sp.Start < prevEnd:
				return fmt.Errorf("snapshot: line %d span %d: unsorted or overlapping at %d", y, i, sp.Start)
			case sp.Style.IsZero():
				return fmt.Errorf("snapshot: line %d span %d: zero style", y, i)
			}
			prevEnd = sp.End
		}
	}
	return nil
}

func (s Snapshot) validateWidths() error {
	if len(s.Widths) == 0 {
		return nil
	}
	if len(s.Widths) != s.Rows {
		return fmt.Errorf("snapshot: %d width lines for %d rows", len(s.Widths), s.Rows)
	}
	for y, wides := range s.Widths {
		runes := utf8.RuneCountInString(s.Lines[y])
		prev := -1
		for i, w := range wides {
			switch {
			case w.Rune < 0 || w.Rune >= runes:
				return fmt.Errorf("snapshot: line %d width %d: rune %d outside %d runes", y, i, w.Rune, runes)
			case w.Rune <= prev:
				return fmt.Errorf("snapshot: line %d width %d: unsorted or duplicate rune %d", y, i, w.Rune)
			case w.Cells != 0 && w.Cells != 2:
				return fmt.Errorf("snapshot: line %d width %d: %d cells (want 0 or 2)", y, i, w.Cells)
			}
			prev = w.Rune
		}
	}
	return nil
}

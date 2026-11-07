// Package snapshot defines the immutable, renderer-agnostic view of a
// terminal screen at a single moment. It has no dependencies so every
// other package can share it.
package snapshot

import (
	"fmt"
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
	// Cursor is the cursor position (0-based) and visibility.
	Cursor Cursor
	// Title is the window title last set by the program, if any.
	Title string
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
	return true
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
// overlapping.
func (s Snapshot) Validate() error {
	if len(s.Lines) != s.Rows {
		return fmt.Errorf("snapshot: %d lines for %d rows", len(s.Lines), s.Rows)
	}
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

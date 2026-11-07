// Package snapshot defines the immutable, renderer-agnostic view of a
// terminal screen at a single moment. It has no dependencies so every
// other package can share it.
package snapshot

import "time"

// Snapshot is a copy of the visible screen. It never aliases terminal memory.
type Snapshot struct {
	// Cols and Rows are the screen dimensions in cells.
	Cols, Rows int
	// Lines holds exactly Rows entries of plain text, right-trimmed.
	Lines []string
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
	return true
}

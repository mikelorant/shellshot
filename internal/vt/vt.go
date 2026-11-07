// Package vt defines the headless virtual terminal abstraction. Concrete
// engines live in sub-packages (see vt/ghostty) so the rest of the program
// never imports cgo and can be tested with fakes.
package vt

import "github.com/mikelorant/shellshot/internal/snapshot"

// Terminal is a headless terminal emulator. Implementations are NOT safe for
// concurrent use; callers must serialise access.
type Terminal interface {
	// Write feeds raw program output (VT sequences) to the terminal.
	Write(p []byte) (int, error)
	// Resize changes the screen size in cells.
	Resize(cols, rows int) error
	// Snapshot copies the current visible screen.
	Snapshot() (snapshot.Snapshot, error)
	// Close releases native resources.
	Close() error
}

// Factory creates a Terminal of the given size. Session code depends on a
// Factory so tests can inject fakes.
type Factory func(cols, rows int) (Terminal, error)

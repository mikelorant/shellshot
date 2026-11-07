// Package tty provides terminal detection, size queries and raw mode for
// files attached to a terminal.
package tty

import (
	"os"
	"sync"

	"golang.org/x/term"
)

// IsTerminal reports whether f is attached to a terminal.
func IsTerminal(f *os.File) bool {
	return term.IsTerminal(int(f.Fd()))
}

// Size returns the width and height of the terminal attached to f, in
// character cells. It returns an error if f is not a terminal.
func Size(f *os.File) (cols, rows int, err error) {
	return term.GetSize(int(f.Fd()))
}

// MakeRaw puts the terminal attached to f into raw mode and returns a
// function that restores the previous state. The restore function is safe to
// call multiple times; only the first call has an effect, and later calls
// return the result of the first. It returns an error if f is not a terminal.
func MakeRaw(f *os.File) (restore func() error, err error) {
	fd := int(f.Fd())
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, err
	}
	var once sync.Once
	var restoreErr error
	return func() error {
		once.Do(func() { restoreErr = term.Restore(fd, state) })
		return restoreErr
	}, nil
}

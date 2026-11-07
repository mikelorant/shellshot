package cli

import (
	"io"
	"os"

	"github.com/mikelorant/shellshot/internal/tty"
)

// isTerminal reports whether w is an *os.File attached to a character
// device, such as a terminal.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

// terminalSize returns the size in cells of the terminal behind w, or 0, 0 if
// w is not a terminal or its size is unknown.
func terminalSize(w io.Writer) (cols, rows int) {
	f, ok := w.(*os.File)
	if !ok {
		return 0, 0
	}
	cols, rows, err := tty.Size(f)
	if err != nil {
		return 0, 0
	}
	return cols, rows
}

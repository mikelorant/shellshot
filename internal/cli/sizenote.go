package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// cellSize is a size in terminal cells.
type cellSize struct{ Cols, Rows int }

// sizeMismatchNote returns the one-line warning for a live display whose
// terminal is real while the recorded terminal is virtual, or "" when there is
// nothing to say: the sizes are equal, or the real size is unknown (not a
// terminal). shellshot copies the recorded output to the real terminal as it
// is, so a different size makes the live display look wrong; the recording
// does not depend on it.
func sizeMismatchNote(real, virtual cellSize) string {
	if real.Cols <= 0 || real.Rows <= 0 || real == virtual {
		return ""
	}
	if real.Cols < virtual.Cols || real.Rows < virtual.Rows {
		return fmt.Sprintf("shellshot: your terminal is %dx%d, smaller than the recording (%dx%d), "+
			"so the live display will look wrong; the recording itself is correct",
			real.Cols, real.Rows, virtual.Cols, virtual.Rows)
	}
	return fmt.Sprintf("shellshot: your terminal is %dx%d but the recording is %dx%d, "+
		"so the live display can look wrong; the recording itself is correct",
		real.Cols, real.Rows, virtual.Cols, virtual.Rows)
}

// warnSizeMismatch prints the sizeMismatchNote for the terminal on cmd's
// stdout and the recorded size cols x rows to cmd's stderr. It prints nothing
// when stdout is not a terminal.
func (ro recordOptions) warnSizeMismatch(cmd *cobra.Command, cols, rows int) {
	probe := ro.stdoutSize
	if probe == nil {
		probe = terminalSize
	}
	realCols, realRows := probe(cmd.OutOrStdout())
	if note := sizeMismatchNote(cellSize{realCols, realRows}, cellSize{cols, rows}); note != "" {
		fmt.Fprintln(cmd.ErrOrStderr(), note)
	}
}

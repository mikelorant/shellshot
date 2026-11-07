package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// ExitError asks Run to exit with Code, the exit status of the command that
// record ran. A command returns it only after its normal output has
// been written. Run does not print it as an error.
type ExitError struct {
	Code int
}

// Error implements error.
func (e ExitError) Error() string {
	return fmt.Sprintf("command exited with %d", e.Code)
}

// addIgnoreExitStatusFlag registers --ignore-exit-status on cmd, binding it to
// ignore.
func addIgnoreExitStatusFlag(cmd *cobra.Command, ignore *bool) {
	cmd.Flags().BoolVar(ignore, "ignore-exit-status", false,
		"exit 0 even if the command exits non-zero")
}

// reportExit tells the user about a non-zero exit code on w and returns the
// ExitError that makes shellshot exit with the same code, or nil when code is
// 0 or ignore is set.
func reportExit(w io.Writer, code int, ignore bool) error {
	if code == 0 {
		return nil
	}
	fmt.Fprintf(w, "shellshot: command exited with %d\n", code)
	if ignore {
		return nil
	}
	return ExitError{Code: code}
}

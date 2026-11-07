// Package cli wires the cobra command tree to the domain packages.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// Run executes the shellshot CLI with args (excluding the program name),
// writing normal output to stdout and errors to stderr. It returns the
// process exit code: 0 on success, 1 on any error, or the exit status of the
// command that record ran (see ExitError).
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	return runWithStdin(ctx, args, os.Stdin, stdout, stderr)
}

// runWithStdin is Run with an injectable standard input.
func runWithStdin(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	cmd := newRootCmd()
	cmd.SetIn(stdin)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
		var exit ExitError
		if errors.As(err, &exit) {
			return exit.Code
		}
		fmt.Fprintf(stderr, "shellshot: %v\n", err)
		return 1
	}
	return 0
}

// newRootCmd builds the root command and registers every subcommand.
// Each subcommand lives in its own file and is added with one line below.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "shellshot",
		Short:         "Record and replay terminal screen snapshots",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newExportCmd(),
		newInfoCmd(),
		newRecordCmd(),
		newReplayCmd(),
		newVersionCmd(),
	)
	return root
}

// Package cli wires the cobra command tree to the domain packages.
package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

// Run executes the shellshot CLI with args (excluding the program name),
// writing normal output to stdout and errors to stderr. It returns the
// process exit code: 0 on success, 1 on any error.
func Run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	cmd := newRootCmd()
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs(args)
	if err := cmd.ExecuteContext(ctx); err != nil {
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
		newRecordCmd(),
		newSnapCmd(),
		newVersionCmd(),
	)
	return root
}

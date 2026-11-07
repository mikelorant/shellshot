package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// lastFrameSink keeps only the most recent frame it receives.
type lastFrameSink struct {
	frame snapshot.Frame
}

// Frame implements session.Sink.
func (s *lastFrameSink) Frame(f snapshot.Frame) error {
	s.frame = f
	return nil
}

// newSnapCmd builds "shellshot snap", which runs a command and prints its
// final screen as plain text.
func newSnapCmd() *cobra.Command {
	var opts captureOptions
	cmd := &cobra.Command{
		Use:   "snap [--cols N] [--rows N] [--interval d] -- command [args...]",
		Short: "Run a command and print its final screen as text",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Command = args
			return runSnap(cmd, opts)
		},
	}
	cmd.Args = cobra.MinimumNArgs(1)
	addCaptureFlags(cmd, &opts)
	return cmd
}

// runSnap captures opts.Command and writes the last screen to cmd's stdout.
// A non-zero exit code of the command is reported on stderr, not as an error.
func runSnap(cmd *cobra.Command, opts captureOptions) error {
	var sink lastFrameSink
	code, err := capture(cmd.Context(), opts, &sink)
	if err != nil {
		return err
	}
	if err := writeScreen(cmd.OutOrStdout(), sink.frame.Snapshot); err != nil {
		return err
	}
	if code != 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "shellshot: command exited with %d\n", code)
	}
	return nil
}

// writeScreen prints the snapshot text followed by a newline.
func writeScreen(w io.Writer, s snapshot.Snapshot) error {
	_, err := fmt.Fprintln(w, s.Text())
	return err
}

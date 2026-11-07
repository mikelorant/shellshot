package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/replay"
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
// final screen as text.
func newSnapCmd() *cobra.Command {
	var (
		opts       captureOptions
		colorMode  string
		ignoreExit bool
	)
	cmd := &cobra.Command{
		Use:   "snap [--cols N] [--rows N] [--fps N] [--color auto|always|never] [--ignore-exit-status] -- command [args...]",
		Short: "Run a command and print its final screen as text",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Command = args
			return runSnap(cmd, opts, colorMode, ignoreExit)
		},
	}
	cmd.Args = cobra.MinimumNArgs(1)
	addCaptureFlags(cmd, &opts)
	addColorFlag(cmd, &colorMode)
	addIgnoreExitStatusFlag(cmd, &ignoreExit)
	return cmd
}

// runSnap captures opts.Command and writes the last screen to cmd's stdout.
// A non-zero exit code of the command is reported on stderr after the screen
// is written and returned as an ExitError, unless ignoreExit is set.
func runSnap(cmd *cobra.Command, opts captureOptions, colorMode string, ignoreExit bool) error {
	color, err := resolveColor(colorMode, isTerminal(cmd.OutOrStdout()), os.Getenv("NO_COLOR"))
	if err != nil {
		return fmt.Errorf("snap: %w", err)
	}
	var sink lastFrameSink
	code, err := capture(cmd.Context(), opts, &sink)
	if err != nil {
		return err
	}
	if err := writeScreen(cmd.OutOrStdout(), sink.frame.Snapshot, color); err != nil {
		return err
	}
	return reportExit(cmd.ErrOrStderr(), code, ignoreExit)
}

// writeScreen prints the screen, one newline-terminated row per line, without
// the blank rows after the last non-empty one. With color, styled runs are
// written as SGR escapes.
func writeScreen(w io.Writer, s snapshot.Snapshot, color bool) error {
	n := len(s.Lines)
	for n > 0 && s.Lines[n-1] == "" {
		n--
	}
	s.Lines = s.Lines[:n]
	_, err := io.WriteString(w, replay.Render(s, replay.RenderOptions{Color: color}))
	return err
}

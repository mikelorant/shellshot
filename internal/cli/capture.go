package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/pty"
	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/vt/ghostty"
)

// captureOptions describes one command run that is sampled into frames. It is
// shared by the commands that run a program (snap, record).
type captureOptions struct {
	Cols, Rows int
	Interval   time.Duration
	// Command is the program followed by its arguments; it must not be empty.
	Command []string
}

// addCaptureFlags registers --cols, --rows and --interval on cmd, binding
// them to o, and makes cmd require a command to run. Flag parsing stops at the
// first positional argument, so the child's own flags (as in
// "shellshot snap -- ls -l") are never interpreted by shellshot.
// The caller must set o.Command from the positional args in its RunE.
func addCaptureFlags(cmd *cobra.Command, o *captureOptions) {
	cmd.Flags().SetInterspersed(false)
	cmd.Args = cobra.MinimumNArgs(1)
	cmd.Flags().IntVar(&o.Cols, "cols", session.DefaultCols, "terminal width in cells")
	cmd.Flags().IntVar(&o.Rows, "rows", session.DefaultRows, "terminal height in cells")
	cmd.Flags().DurationVar(&o.Interval, "interval", session.DefaultInterval, "screen sampling interval")
}

// capture runs o.Command in a PTY, feeds its output to a libghostty terminal
// and sends the resulting frames to sink. It returns the command's exit code;
// a non-zero exit code is not an error.
func capture(ctx context.Context, o captureOptions, sink session.Sink) (exitCode int, err error) {
	if len(o.Command) == 0 {
		return -1, fmt.Errorf("no command given")
	}
	proc, err := pty.Start(ctx, pty.Config{
		Command: o.Command[0],
		Args:    o.Command[1:],
		Cols:    o.Cols,
		Rows:    o.Rows,
	})
	if err != nil {
		return -1, err
	}
	defer func() { _ = proc.Close() }()

	cfg := session.Config{Interval: o.Interval, Cols: o.Cols, Rows: o.Rows}
	return session.Run(ctx, proc, ghostty.New, cfg, sink)
}

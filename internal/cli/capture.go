package cli

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/live"
	"github.com/michaellorant/shellshot/internal/pty"
	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/vt/ghostty"
)

// eot is the end-of-transmission byte (Ctrl-D) a terminal sends for EOF.
const eot = 0x04

// captureOptions describes one command run that is sampled into frames. It is
// used by record.
type captureOptions struct {
	Cols, Rows int
	Interval   time.Duration
	// Command is the program followed by its arguments; it must not be empty.
	Command []string
	// Stdin, when non-nil, is pumped to the process as user input.
	Stdin io.Reader
	// EOTOnStdinEOF sends Ctrl-D (EOT) to the process when Stdin reaches EOF,
	// so a shell fed from a pipe exits as if a user had pressed Ctrl-D. Set it
	// only when Stdin is not a terminal.
	EOTOnStdinEOF bool
	// Live, when non-nil, shows the process output as it is produced.
	Live io.Writer
}

// addCaptureFlags registers --cols, --rows and --interval on cmd, binding
// them to o, with explicit --cols and --rows defaults. Flag parsing stops at
// the first positional argument, so the child's own flags (as in "shellshot
// record -- ls -l") are never interpreted by shellshot. The caller decides how
// many positional arguments are required and must set o.Command from them in
// its RunE.
func addCaptureFlags(cmd *cobra.Command, o *captureOptions, cols, rows int) {
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().IntVar(&o.Cols, "cols", cols, "terminal width in cells")
	cmd.Flags().IntVar(&o.Rows, "rows", rows, "terminal height in cells")
	cmd.Flags().DurationVar(&o.Interval, "interval", session.DefaultInterval, "screen sampling interval")
}

// capture runs o.Command in a PTY, feeds its output to a libghostty terminal
// and sends the resulting frames to sink. It returns the command's exit code;
// a non-zero exit code is not an error.
//
// If o.Live is set, the process output is also written to it as it arrives.
// If o.Stdin is set, it is pumped to the process until the capture ends; a
// blocked read of o.Stdin cannot be interrupted and lingers (see live.Pump).
// With o.EOTOnStdinEOF, the end of o.Stdin is forwarded as Ctrl-D.
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

	var p live.Process = proc
	if o.Live != nil {
		p = live.Tee(p, o.Live)
	}
	if o.Stdin != nil {
		pumpCtx, stop := context.WithCancel(ctx)
		defer stop()
		var pumpOpts live.PumpOptions
		if o.EOTOnStdinEOF {
			pumpOpts.OnEOF = []byte{eot}
		}
		live.PumpWith(pumpCtx, o.Stdin, p, pumpOpts)
	}

	cfg := session.Config{Interval: o.Interval, Cols: o.Cols, Rows: o.Rows}
	return session.Run(ctx, p, ghostty.New, cfg, sink)
}

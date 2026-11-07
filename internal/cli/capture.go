package cli

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/live"
	"github.com/michaellorant/shellshot/internal/pty"
	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/snapshot"
	"github.com/michaellorant/shellshot/internal/vt/ghostty"
)

// eot is the end-of-transmission byte (Ctrl-D) a terminal sends for EOF.
const eot = 0x04

// tickFPS sets the session ticker rate used by record. record discards the
// frames the session samples; the ticker only drives flushing of the event
// log, so it is slow to keep the wasted snapshots few.
const tickFPS = 4

// captureOptions describes one command run recorded as an event log. It is
// used by record.
type captureOptions struct {
	Cols, Rows int
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
	// Events, when non-nil, receives the session's events and is flushed by
	// its ticker.
	Events *binSink
	// RecordInput also logs the bytes forwarded to the process as input
	// events. It requires Events, whose writer must allow input.
	RecordInput bool
}

// addCaptureFlags registers --cols and --rows on cmd, binding them to o, with
// explicit defaults. Flag parsing stops at
// the first positional argument, so the child's own flags (as in "shellshot
// record -- ls -l") are never interpreted by shellshot. The caller decides how
// many positional arguments are required and must set o.Command from them in
// its RunE.
func addCaptureFlags(cmd *cobra.Command, o *captureOptions, cols, rows int) {
	cmd.Flags().SetInterspersed(false)
	cmd.Flags().IntVar(&o.Cols, "cols", cols, "terminal width in cells")
	cmd.Flags().IntVar(&o.Rows, "rows", rows, "terminal height in cells")
	// The sampling rate (--fps) belongs to replay and export, not record.
}

// capture runs o.Command in a PTY and drives a session on it, logging the
// session events to o.Events. It returns the command's exit code; a non-zero
// exit code is not an error.
//
// If o.Live is set, the process output is also written to it as it arrives.
// If o.Stdin is set, it is pumped to the process until the capture ends; a
// blocked read of o.Stdin cannot be interrupted and lingers (see live.Pump).
// With o.EOTOnStdinEOF, the end of o.Stdin is forwarded as Ctrl-D (without
// the terminal echoing it as ^D), unless o.Stdin was empty.
func capture(ctx context.Context, o captureOptions) (exitCode int, err error) {
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
	if o.Events != nil && o.RecordInput {
		p = inputRecorder{Process: p, sink: o.Events}
	}
	if o.Stdin != nil {
		pumpCtx, stop := context.WithCancel(ctx)
		defer stop()
		var pumpOpts live.PumpOptions
		if o.EOTOnStdinEOF {
			pumpOpts.OnEOF = []byte{eot}
			// Best effort: if echo cannot be switched off, the EOT is
			// still sent so the child terminates, at the cost of a
			// visible ^D.
			pumpOpts.BeforeOnEOF = func() { _ = proc.DisableEOTEcho() }
		}
		live.PumpWith(pumpCtx, o.Stdin, p, pumpOpts)
	}

	cfg := session.Config{FPS: tickFPS, Cols: o.Cols, Rows: o.Rows}
	if o.Events != nil {
		o.Events.configure(&cfg)
	}
	return session.Run(ctx, p, ghostty.New, cfg, session.SinkFunc(discardFrame))
}

// discardFrame ignores a sampled frame: record logs raw events, not frames.
func discardFrame(snapshot.Frame) error { return nil }

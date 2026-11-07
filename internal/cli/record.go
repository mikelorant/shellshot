package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/recording"
)

// defaultRecordingFile is the output file of "shellshot record" without -o.
const defaultRecordingFile = "recording.shot"

// newRecordCmd builds "shellshot record", which runs a command and saves its
// screen frames to a recording file.
func newRecordCmd() *cobra.Command {
	var opts captureOptions
	var output string
	cmd := &cobra.Command{
		Use:   "record [-o file] [--cols N] [--rows N] [--interval d] -- command [args...]",
		Short: "Run a command and record its screen to a file",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Command = args
			return runRecord(cmd, opts, output)
		},
	}
	addCaptureFlags(cmd, &opts)
	cmd.Flags().StringVarP(&output, "output", "o", defaultRecordingFile, "recording file to write")
	return cmd
}

// runRecord captures opts.Command into the file at output. A non-zero exit
// code of the command is reported on stderr, not as an error.
func runRecord(cmd *cobra.Command, opts captureOptions, output string) (err error) {
	f, err := os.Create(output)
	if err != nil {
		return fmt.Errorf("create recording: %w", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close recording %s: %w", output, cerr)
		}
	}()

	sink, err := newRecordingSink(f, recording.Header{
		Version: recording.Version,
		Cols:    opts.Cols,
		Rows:    opts.Rows,
		Command: opts.Command,
		Started: time.Now(),
	})
	if err != nil {
		return fmt.Errorf("write recording %s: %w", output, err)
	}
	code, err := capture(cmd.Context(), opts, sink)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "recorded %d frames to %s\n", sink.frames, output)
	if code != 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "shellshot: command exited with %d\n", code)
	}
	return nil
}

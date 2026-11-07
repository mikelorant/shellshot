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

// newRecordCmd builds "shellshot record", an interactive recorder: it runs a
// shell or command on a live passthrough terminal and saves its screen frames
// to a recording file.
func newRecordCmd() *cobra.Command {
	var opts captureOptions
	var output, script string
	cmd := &cobra.Command{
		Use:   "record [-o file] [-c command] [--cols N] [--rows N] [--interval d] [-- command [args...]]",
		Short: "Record an interactive shell or command to a file",
		Long: "Record runs $SHELL (or /bin/sh), or the command given with -c or after --, " +
			"on a live terminal and records its screen. Recording ends when the command exits.",
		RunE: func(cmd *cobra.Command, args []string) error {
			command, err := resolveCommand(args, script, shellFromEnv(os.Getenv))
			if err != nil {
				return err
			}
			opts.Command = command
			opts.Cols, opts.Rows = resolveSize(opts.Cols, opts.Rows, cmd.OutOrStdout())
			return runRecord(cmd, opts, output)
		},
	}
	addCaptureFlagsWithSize(cmd, &opts, 0, 0)
	cmd.Flags().StringVarP(&output, "output", "o", defaultRecordingFile, "recording file to write")
	cmd.Flags().StringVarP(&script, "command", "c", "", "shell command string to run with $SHELL -c")
	return cmd
}

// runRecord captures opts.Command, shown live on cmd's stdout and fed from its
// stdin, into the file at output. A non-zero exit code of the command is
// reported on stderr, not as an error.
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
	code, err := captureLive(cmd.Context(), opts, cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr(), sink)
	if err != nil {
		return err
	}
	fmt.Fprintf(cmd.ErrOrStderr(), "recorded %d frames to %s\n", sink.frames, output)
	if code != 0 {
		fmt.Fprintf(cmd.ErrOrStderr(), "shellshot: command exited with %d\n", code)
	}
	return nil
}

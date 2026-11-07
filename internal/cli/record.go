package cli

import (
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/recording/bin"
)

// defaultRecordingFile is the output file of "shellshot record" without -o.
const defaultRecordingFile = "recording.shot"

// inputNotice is printed to stderr when --record-input is set.
const inputNotice = "shellshot: recording keyboard input"

// recordOptions holds the flags of "shellshot record" other than the shared
// capture flags.
type recordOptions struct {
	Output      string
	IgnoreExit  bool
	RecordInput bool
	Coalesce    time.Duration
	Codec       string
}

// newRecordCmd builds "shellshot record", an interactive recorder: it runs a
// shell or command on a live passthrough terminal and saves the terminal
// output, with its timing, to a binary recording file.
func newRecordCmd() *cobra.Command {
	var opts captureOptions
	var ro recordOptions
	var script string
	cmd := &cobra.Command{
		Use:   "record [-o file] [-c command] [--cols N] [--rows N] [--record-input] [--coalesce DURATION] [--codec none|flate|zstd] [--ignore-exit-status] [-- command [args...]]",
		Short: "Record an interactive shell or command to a file",
		Long: "Record runs $SHELL (or /bin/sh), or the command given with -c or after --, " +
			"on a live terminal and records its output with timing. Recording ends when the command exits.\n\n" +
			"Keystrokes are not recorded unless --record-input is set.",
		RunE: func(cmd *cobra.Command, args []string) error {
			command, err := resolveCommand(args, script, shellFromEnv(os.Getenv))
			if err != nil {
				return err
			}
			opts.Command = command
			opts.Cols, opts.Rows = resolveSize(opts.Cols, opts.Rows, cmd.OutOrStdout())
			return runRecord(cmd, opts, ro)
		},
	}
	addCaptureFlags(cmd, &opts, 0, 0)
	cmd.Flags().StringVarP(&ro.Output, "output", "o", defaultRecordingFile, "recording file to write")
	addIgnoreExitStatusFlag(cmd, &ro.IgnoreExit)
	cmd.Flags().StringVarP(&script, "command", "c", "", "shell command string to run with $SHELL -c")
	cmd.Flags().BoolVar(&ro.RecordInput, "record-input", false, "also record the keystrokes sent to the command")
	cmd.Flags().DurationVar(&ro.Coalesce, "coalesce", 0, "merge output events closer together than this (0 keeps every read separate)")
	cmd.Flags().StringVar(&ro.Codec, "codec", codecZstd, "block compression: none, flate or zstd")
	return cmd
}

// runRecord captures opts.Command, shown live on cmd's stdout and fed from its
// stdin, into the file ro.Output. A non-zero exit code of the command is
// reported on stderr after the recording is written and returned as an
// ExitError, unless ro.IgnoreExit is set.
//
// The file is written as the command runs, so a crash leaves a recording that
// is readable up to its last flushed block. The recording is finished and the
// file closed on every path once it has been created.
func runRecord(cmd *cobra.Command, opts captureOptions, ro recordOptions) error {
	wopts, err := writerOptions(ro.Codec, ro.Coalesce)
	if err != nil {
		return err
	}
	id, err := newSessionID()
	if err != nil {
		return err
	}
	errOut := cmd.ErrOrStderr()
	if ro.RecordInput {
		fmt.Fprintln(errOut, inputNotice)
	}

	f, err := os.Create(ro.Output)
	if err != nil {
		return fmt.Errorf("create recording: %w", err)
	}
	w, err := bin.NewWriter(f, buildHead(headInputs{
		Cols:        opts.Cols,
		Rows:        opts.Rows,
		Argv:        opts.Command,
		Started:     time.Now(),
		Getenv:      os.Getenv,
		SessionID:   id,
		RecordInput: ro.RecordInput,
	}), wopts)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("write recording %s: %w", ro.Output, err)
	}
	sink := newBinSink(w, time.Now)
	opts.Events, opts.RecordInput = sink, ro.RecordInput

	code, runErr := captureLive(cmd.Context(), opts, cmd.InOrStdin(), cmd.OutOrStdout(), errOut)
	closeErr := sink.close()
	if cerr := f.Close(); closeErr == nil {
		closeErr = cerr
	}
	if runErr != nil {
		return runErr
	}
	if closeErr != nil {
		return fmt.Errorf("write recording %s: %w", ro.Output, closeErr)
	}
	st := sink.stats()
	fmt.Fprintf(errOut, "recorded %d events (%.1f KB) to %s\n", st.Events, float64(st.BytesWritten)/1024, ro.Output)
	return reportExit(errOut, code, ro.IgnoreExit)
}

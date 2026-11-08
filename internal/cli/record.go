package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/recording/bin"
)

// defaultRecordingPattern is how the usage text describes the recording file
// of "shellshot record" without -o.
const defaultRecordingPattern = "shellshot-YYYYMMDD-HHMMSS.shot"

// inputNotice is printed to stderr when --record-input is set.
const inputNotice = "shellshot: recording keyboard input"

// recordOptions holds the flags of "shellshot record" other than the shared
// capture flags.
type recordOptions struct {
	// Output is the -o file; empty means a new timestamped file.
	Output      string
	IgnoreExit  bool
	RecordInput bool
	Coalesce    time.Duration
	Codec       string

	// now is the clock that names a default recording; outDir is where it is
	// created. Tests set them; the command uses time.Now and ".".
	now    func() time.Time
	outDir string
	// stderrTerminal tells whether stderr is a terminal and how wide it is
	// (see stderrTerminal). Tests set it; the command leaves it nil.
	stderrTerminal func(io.Writer) (width func() int, ok bool)
	// stdoutSize returns the size of the terminal behind a writer, or 0, 0
	// when it is not one (see terminalSize). Tests set it; the command leaves
	// it nil.
	stdoutSize func(io.Writer) (cols, rows int)
}

// createRecording creates the recording file and returns it with its path.
// An explicit path is created or truncated. With none, a new file
// shellshot-YYYYMMDD-HHMMSS.shot is created in ro.outDir from the local time
// now; if that name exists, -2, -3, ... is added before the extension, so a
// default recording never replaces another file. generated reports the second
// case.
func createRecording(ro recordOptions, explicit string) (f *os.File, path string, generated bool, err error) {
	if explicit != "" {
		f, err = os.Create(explicit)
		return f, explicit, false, err
	}
	stamp := ro.now().Format("20060102-150405")
	for n := 1; ; n++ {
		name := "shellshot-" + stamp + ".shot"
		if n > 1 {
			name = fmt.Sprintf("shellshot-%s-%d.shot", stamp, n)
		}
		path = filepath.Join(ro.outDir, name)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if !errors.Is(err, os.ErrExist) {
			return f, path, true, err
		}
	}
}

// newRecordCmd builds "shellshot record", an interactive recorder: it runs a
// shell or command on a live passthrough terminal and saves the terminal
// output, with its timing, to a binary recording file.
func newRecordCmd() *cobra.Command {
	return newRecordCmdWith(recordOptions{now: time.Now, outDir: "."})
}

// newRecordCmdWith is newRecordCmd with the clock and directory of ro, whose
// other fields are set from the flags.
func newRecordCmdWith(ro recordOptions) *cobra.Command {
	var opts captureOptions
	var commandString string
	var so scriptOptions
	cmd := &cobra.Command{
		Use:   "record [-o file] [-c command] [--cols N] [--rows N] [--record-input] [--coalesce DURATION] [--codec none|flate|zstd] [--ignore-exit-status] [-- command [args...]]\n       shellshot record --script FILE [--list] [--watch] [-o file] [--cols N] [--rows N] [--record-input]",
		Short: "Record an interactive shell or command to a file",
		Long: "Record runs $SHELL (or /bin/sh), or the command given with -c or after --, " +
			"on a live terminal and records its output with timing. Recording ends when the command exits.\n\n" +
			"Keystrokes are not recorded unless --record-input is set.\n\n" +
			"With --script FILE the session is run by a script (see docs/scripting.md) instead of by you. " +
			"--list prints what the script would do and runs nothing. " +
			"-o, --cols and --rows given on the command line win over the script's output and size.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if scriptRequested(cmd) {
				if err := validateScriptFlags(cmd, so, commandString, args); err != nil {
					return err
				}
				return runRecordScript(cmd, so, opts, ro)
			}
			command, err := resolveCommand(args, commandString, shellFromEnv(os.Getenv))
			if err != nil {
				return err
			}
			opts.Command = command
			opts.Cols, opts.Rows = resolveSize(opts.Cols, opts.Rows, cmd.OutOrStdout())
			if f := cmd.Flags(); f.Changed("cols") || f.Changed("rows") {
				ro.warnSizeMismatch(cmd, opts.Cols, opts.Rows)
			}
			return runRecord(cmd, opts, ro)
		},
	}
	addCaptureFlags(cmd, &opts, 0, 0)
	cmd.Flags().StringVarP(&ro.Output, "output", "o", "", "recording file (default "+defaultRecordingPattern+")")
	addIgnoreExitStatusFlag(cmd, &ro.IgnoreExit)
	cmd.Flags().StringVarP(&commandString, "command", "c", "", "shell command string to run with $SHELL -c")
	cmd.Flags().BoolVar(&ro.RecordInput, "record-input", false, "also record the keystrokes sent to the command")
	cmd.Flags().DurationVar(&ro.Coalesce, "coalesce", 0, "merge output events closer together than this (0 keeps every read separate)")
	cmd.Flags().StringVar(&ro.Codec, "codec", codecZstd, "block compression: none, flate or zstd")
	addScriptFlags(cmd, &so)
	return cmd
}

// runRecord captures opts.Command, shown live on cmd's stdout and fed from its
// stdin, into the file ro.Output, or a new timestamped file when that is empty
// (see createRecording). A non-zero exit code of the command is
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

	f, path, generated, err := createRecording(ro, ro.Output)
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
		if generated {
			_ = os.Remove(path)
		}
		return fmt.Errorf("write recording %s: %w", path, err)
	}
	sink := newBinSink(w, time.Now)
	opts.Events, opts.RecordInput = sink, ro.RecordInput

	code, runErr := captureLive(cmd.Context(), opts, cmd.InOrStdin(), cmd.OutOrStdout(), errOut)
	closeErr := sink.close()
	if cerr := f.Close(); closeErr == nil {
		closeErr = cerr
	}
	if runErr != nil {
		if generated && sink.stats().Events == 0 {
			_ = os.Remove(path)
		}
		return runErr
	}
	if closeErr != nil {
		return fmt.Errorf("write recording %s: %w", path, closeErr)
	}
	st := sink.stats()
	fmt.Fprintf(errOut, "recorded %d events (%.1f KB) to %s\n", st.Events, float64(st.BytesWritten)/1024, path)
	return reportExit(errOut, code, ro.IgnoreExit)
}

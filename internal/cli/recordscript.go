package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/live"
	"github.com/mikelorant/shellshot/internal/pty"
	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/script"
	"github.com/mikelorant/shellshot/internal/script/run"
	"github.com/mikelorant/shellshot/internal/session"
	"github.com/mikelorant/shellshot/internal/vt/ghostty"
)

// scriptOptions are the flags of "record" that belong to --script.
type scriptOptions struct {
	File  string
	List  bool
	Watch bool
}

// addScriptFlags registers --script, --list and --watch on cmd.
func addScriptFlags(cmd *cobra.Command, so *scriptOptions) {
	f := cmd.Flags()
	f.StringVar(&so.File, "script", "", "run the script `FILE` and record it instead of an interactive shell")
	f.BoolVar(&so.List, "list", false, "with --script: print the plan and its estimated duration, run nothing")
	f.BoolVar(&so.Watch, "watch", false, "with --script: show the terminal output on stdout while the script runs")
}

// scriptRequested reports whether any of the script flags was used.
func scriptRequested(cmd *cobra.Command) bool {
	f := cmd.Flags()
	return f.Changed("script") || f.Changed("list") || f.Changed("watch")
}

// validateScriptFlags checks the flag combinations of a script run:
// --script replaces the command, and --list and --watch need --script.
func validateScriptFlags(cmd *cobra.Command, so scriptOptions, commandString string, argv []string) error {
	f := cmd.Flags()
	switch {
	case !f.Changed("script"):
		if so.List {
			return errors.New("--list requires --script")
		}
		return errors.New("--watch requires --script")
	case so.File == "":
		return errors.New("--script needs a file")
	case commandString != "" || len(argv) > 0:
		return errors.New("--script cannot be combined with -c or a command after --: put the command in the script with shell and run")
	case so.List && so.Watch:
		return errors.New("--list runs nothing, so --watch has no effect")
	}
	return nil
}

// runRecordScript implements "record --script FILE" and "--list".
func runRecordScript(cmd *cobra.Command, so scriptOptions, opts captureOptions, ro recordOptions) error {
	errOut := cmd.ErrOrStderr()
	plan, err := loadPlan(so.File, errOut)
	if err != nil {
		return err
	}
	if so.List {
		_, err := io.WriteString(cmd.OutOrStdout(), plan.Format())
		return err
	}
	wopts, err := writerOptions(ro.Codec, ro.Coalesce)
	if err != nil {
		return err
	}
	f := cmd.Flags()
	termCols, termRows := resolveSize(0, 0, cmd.OutOrStdout())
	set := resolveScriptSettings(plan.Process, scriptFlags{
		Output: ro.Output, OutputChanged: f.Changed("output"),
		Cols: opts.Cols, ColsChanged: f.Changed("cols"),
		Rows: opts.Rows, RowsChanged: f.Changed("rows"),
	}, os.Environ(), termCols, termRows)
	if so.Watch {
		ro.warnSizeMismatch(cmd, set.Cols, set.Rows)
	}
	return runScript(cmd, so, ro, wopts, plan, set)
}

// loadPlan reads, checks and lowers the script at path. Diagnostics go to
// errOut, one "file:line:col: message (hint)" per line, and make it return an
// ExitError with exitScriptSyntax.
func loadPlan(path string, errOut io.Writer) (*run.Plan, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read script: %w", err)
	}
	s, diags := script.Check(path, src)
	if len(diags) > 0 {
		for _, d := range diags {
			fmt.Fprintln(errOut, d.Format(path))
		}
		return nil, ExitError{Code: exitScriptSyntax}
	}
	plan, err := run.Lower(s)
	if err != nil {
		fmt.Fprintln(errOut, err)
		return nil, ExitError{Code: exitScriptSyntax}
	}
	return plan, nil
}

// scriptFlags are the command-line values that can override the script, with
// whether the user set them.
type scriptFlags struct {
	Output                   string
	OutputChanged            bool
	Cols, Rows               int
	ColsChanged, RowsChanged bool
}

// scriptSettings is what a script run uses: the script's own settings, as
// overridden by explicit flags, over the defaults. An empty Output means a new
// timestamped file (see createRecording).
type scriptSettings struct {
	Argv       []string
	Env        []string // the complete environment of the child
	Dir        string
	Cols, Rows int
	Output     string
}

// resolveScriptSettings applies the precedence "explicit flag, then script,
// then default". environ is the environment of shellshot; termCols and
// termRows are the default size.
func resolveScriptSettings(p run.Process, f scriptFlags, environ []string, termCols, termRows int) scriptSettings {
	set := scriptSettings{
		Argv: p.Shell,
		Env:  mergeEnv(environ, p.Env),
		Dir:  p.Cwd,
		Cols: termCols,
		Rows: termRows,
	}
	if len(set.Argv) == 0 {
		shell := lookupEnv(environ, "SHELL")
		if shell == "" {
			shell = fallbackShell
		}
		set.Argv = []string{shell}
	}
	if p.Cols > 0 {
		set.Cols, set.Rows = p.Cols, p.Rows
	}
	if f.ColsChanged && f.Cols > 0 {
		set.Cols = f.Cols
	}
	if f.RowsChanged && f.Rows > 0 {
		set.Rows = f.Rows
	}
	if p.Output != "" {
		set.Output = p.Output
	}
	if f.OutputChanged {
		set.Output = f.Output
	}
	return set
}

// mergeEnv returns base with the KEY=VALUE entries of set applied in order;
// a later entry replaces an earlier one with the same key.
func mergeEnv(base, set []string) []string {
	out := make([]string, 0, len(base)+len(set))
	out = append(out, base...)
	for _, kv := range set {
		key, _, _ := strings.Cut(kv, "=")
		out = deleteEnvKey(out, key)
		out = append(out, kv)
	}
	return out
}

// deleteEnvKey returns env without the entries for key.
func deleteEnvKey(env []string, key string) []string {
	out := env[:0]
	for _, kv := range env {
		if k, _, _ := strings.Cut(kv, "="); k != key {
			out = append(out, kv)
		}
	}
	return out
}

// lookupEnv returns the value of key in env, "" if unset. The last entry wins.
func lookupEnv(env []string, key string) string {
	v := ""
	for _, kv := range env {
		if k, val, ok := strings.Cut(kv, "="); ok && k == key {
			v = val
		}
	}
	return v
}

// trackedProcess notes whether the process was closed before it had exited,
// which tells a shell that left on its own from one that shellshot had to
// kill after the script ended.
type trackedProcess struct {
	*pty.Process
	waited atomic.Bool
	killed atomic.Bool
}

// Wait implements live.Process.
func (t *trackedProcess) Wait() (int, error) {
	code, err := t.Process.Wait()
	t.waited.Store(true)
	return code, err
}

// Close implements live.Process.
func (t *trackedProcess) Close() error {
	if !t.waited.Load() {
		t.killed.Store(true)
	}
	return t.Process.Close()
}

// runScript runs plan on a pty and records it to set.Output. The recording is
// finished and kept on every path once it has been created.
func runScript(cmd *cobra.Command, so scriptOptions, ro recordOptions, wopts bin.WriterOptions, plan *run.Plan, set scriptSettings) error {
	errOut := cmd.ErrOrStderr()
	ctx := cmd.Context()

	id, err := newSessionID()
	if err != nil {
		return err
	}
	if ro.RecordInput {
		fmt.Fprintln(errOut, inputNotice)
	}
	// The progress line must be erased before anything else is written to
	// errOut: stopProgress is called as soon as the session has ended, and
	// again on every earlier return.
	observe, stopProgress := ro.startScriptProgress(errOut, so.Watch, plan)
	defer stopProgress()

	holder := &termHolder{}
	encoder := &lazyKeys{holder: holder}
	defer encoder.close()
	exec, err := run.New(plan, run.Deps{Keys: encoder, File: so.File, OnProgress: observe})
	if err != nil {
		return fmt.Errorf("prepare script: %w", err)
	}
	drv := newScriptDriver(exec)

	proc, err := pty.Start(ctx, pty.Config{
		Command: set.Argv[0],
		Args:    set.Argv[1:],
		Env:     set.Env,
		Dir:     set.Dir,
		Cols:    set.Cols,
		Rows:    set.Rows,
	})
	if err != nil {
		return fmt.Errorf("start %s: %w", set.Argv[0], err)
	}
	tracked := &trackedProcess{Process: proc}
	defer func() { _ = tracked.Close() }()

	f, path, _, err := createRecording(ro, set.Output)
	if err != nil {
		return fmt.Errorf("create recording: %w", err)
	}
	set.Output = path
	wopts.Meta = map[string]string{"script": filepath.Base(so.File)}
	w, err := bin.NewWriter(f, buildHead(headInputs{
		Cols:        set.Cols,
		Rows:        set.Rows,
		Argv:        set.Argv,
		Started:     time.Now(),
		Getenv:      func(k string) string { return lookupEnv(set.Env, k) },
		SessionID:   id,
		RecordInput: ro.RecordInput,
	}), wopts)
	if err != nil {
		_ = f.Close()
		return fmt.Errorf("write recording %s: %w", set.Output, err)
	}
	sink := newBinSink(w, time.Now)

	var p live.Process = tracked
	if so.Watch {
		p = live.Tee(p, cmd.OutOrStdout())
	}
	if ro.RecordInput {
		p = inputRecorder{Process: p, sink: sink}
	}
	cfg := session.Config{
		FPS:           tickFPS,
		Cols:          set.Cols,
		Rows:          set.Rows,
		Driver:        drv,
		ShutdownGrace: run.ExitGrace,
	}
	sink.configure(&cfg)

	status, runErr := session.Run(ctx, p, holder.factory(ghostty.New), cfg, session.SinkFunc(discardFrame))
	stopProgress()
	closedByUs := tracked.killed.Load()
	closeErr := sink.close()
	if cerr := f.Close(); closeErr == nil {
		closeErr = cerr
	}
	if runErr == nil && closeErr != nil {
		return fmt.Errorf("write recording %s: %w", set.Output, closeErr)
	}
	if runErr == nil {
		st := sink.stats()
		fmt.Fprintf(errOut, "recorded %d events (%.1f KB) to %s\n", st.Events, float64(st.BytesWritten)/1024, set.Output)
	}
	return reportScriptResult(errOut, scriptResult{
		Path:       set.Output,
		Err:        runErr,
		Status:     status,
		ClosedByUs: closedByUs,
		IgnoreExit: ro.IgnoreExit,
		Screen:     drv.lastScreen(),
	})
}

// scriptResult is how a script run ended.
type scriptResult struct {
	// Path is the recording.
	Path string
	// Err is the error of session.Run; nil when the script completed.
	Err error
	// Status is the exit status of the child when Err is nil.
	Status int
	// ClosedByUs is true when the child had to be closed by shellshot after
	// the script ended.
	ClosedByUs bool
	IgnoreExit bool
	// Screen is the last screen the script looked at.
	Screen string
}

// reportScriptResult tells the user how the run ended on w and returns the
// error that makes shellshot exit with the code of the table in
// docs/scripting.md: nil for 0, an ExitError for the codes that are not
// errors of shellshot itself, or a plain error for exit code 1.
func reportScriptResult(w io.Writer, r scriptResult) error {
	if r.Err == nil {
		if r.ClosedByUs {
			fmt.Fprintln(w, "shellshot: child did not exit; closed by shellshot")
			return nil
		}
		return reportExit(w, r.Status, r.IgnoreExit)
	}
	var se *run.ScriptError
	switch {
	case errors.As(r.Err, &se):
		printFinalScreen(w, r.Screen)
		fmt.Fprintf(w, "shellshot: script failed: %v; partial recording kept at %s\n", se, r.Path)
		return ExitError{Code: se.ExitCode}
	case errors.Is(r.Err, context.Canceled), errors.Is(r.Err, context.DeadlineExceeded):
		fmt.Fprintf(w, "shellshot: cancelled; partial recording kept at %s\n", r.Path)
		return ExitError{Code: exitCancelled}
	}
	return fmt.Errorf("script failed: %w; partial recording kept at %s", r.Err, r.Path)
}

// printFinalScreen writes the screen text, each row after "| ", under a title.
func printFinalScreen(w io.Writer, screen string) {
	if screen == "" {
		return
	}
	fmt.Fprintln(w, "shellshot: final screen:")
	for _, row := range strings.Split(screen, "\n") {
		fmt.Fprintf(w, "| %s\n", row)
	}
}

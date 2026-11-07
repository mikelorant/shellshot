package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/script"
	"github.com/michaellorant/shellshot/internal/script/run"
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
	_, _, _ = so, wopts, set
	return errors.New("running a script is not implemented yet; use --list")
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
// overridden by explicit flags, over the defaults.
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
		Argv:   p.Shell,
		Env:    mergeEnv(environ, p.Env),
		Dir:    p.Cwd,
		Cols:   termCols,
		Rows:   termRows,
		Output: defaultRecordingFile,
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

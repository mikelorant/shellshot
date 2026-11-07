package cli

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/tty"
)

// fallbackShell runs when no command is given and $SHELL is unset.
const fallbackShell = "/bin/sh"

// interactiveHint is printed before the terminal goes raw.
const interactiveHint = "shellshot: recording — exit the shell to finish"

// resolveCommand returns the argv to record: argv itself, "shell -c script"
// when script is set, or the bare shell. argv and script are exclusive.
func resolveCommand(argv []string, script, shell string) ([]string, error) {
	switch {
	case script != "" && len(argv) > 0:
		return nil, fmt.Errorf("-c and a command after -- are mutually exclusive")
	case script != "":
		return []string{shell, "-c", script}, nil
	case len(argv) > 0:
		return argv, nil
	default:
		return []string{shell}, nil
	}
}

// shellFromEnv returns the value of SHELL via getenv, or fallbackShell.
func shellFromEnv(getenv func(string) string) string {
	if shell := getenv("SHELL"); shell != "" {
		return shell
	}
	return fallbackShell
}

// resolveSize replaces a zero cols or rows with the size of out when out is a
// terminal, or with the session default otherwise.
func resolveSize(cols, rows int, out io.Writer) (int, int) {
	termCols, termRows := session.DefaultCols, session.DefaultRows
	if f, ok := out.(*os.File); ok && tty.IsTerminal(f) {
		if c, r, err := tty.Size(f); err == nil && c > 0 && r > 0 {
			termCols, termRows = c, r
		}
	}
	if cols == 0 {
		cols = termCols
	}
	if rows == 0 {
		rows = termRows
	}
	return cols, rows
}

// stdinTerminal returns r as a terminal file, or nil if r is not one.
func stdinTerminal(r io.Reader) *os.File {
	if f, ok := r.(*os.File); ok && tty.IsTerminal(f) {
		return f
	}
	return nil
}

// captureLive is capture with the process shown on out and fed from in. When
// in is a terminal it is put in raw mode for the duration of the capture and
// always restored before captureLive returns, including on error. When in is
// not a terminal (a pipe or file), its EOF is forwarded to the process as
// Ctrl-D so the shell exits instead of waiting forever for more input.
func captureLive(ctx context.Context, opts captureOptions, in io.Reader, out, errOut io.Writer, sink session.Sink) (code int, err error) {
	opts.Stdin, opts.Live = in, out
	term := stdinTerminal(in)
	opts.EOTOnStdinEOF = term == nil
	if term != nil {
		fmt.Fprintln(errOut, interactiveHint)
		restore, rawErr := tty.MakeRaw(term)
		if rawErr != nil {
			return -1, fmt.Errorf("enter raw mode: %w", rawErr)
		}
		defer func() {
			if rerr := restore(); rerr != nil && err == nil {
				code, err = -1, fmt.Errorf("restore terminal: %w", rerr)
			}
		}()
	}
	return capture(ctx, opts, sink)
}

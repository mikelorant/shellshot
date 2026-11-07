// Package pty runs a command attached to a pseudo-terminal.
package pty

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

const (
	defaultCols = 80
	defaultRows = 24
	maxDim      = 65535 // winsize fields are uint16
)

// Config describes the command to run and the terminal it runs in.
type Config struct {
	Command string   // program to execute (looked up in PATH)
	Args    []string // arguments, not including the program name
	Env     []string // KEY=VALUE entries; empty means os.Environ()
	Dir     string   // working directory; empty means the current directory
	Cols    int      // terminal width; 0 means 80
	Rows    int      // terminal height; 0 means 24
}

// size validates the dimensions, applying defaults for zero values.
func (c Config) size() (cols, rows int, err error) {
	cols, rows = c.Cols, c.Rows
	if cols == 0 {
		cols = defaultCols
	}
	if rows == 0 {
		rows = defaultRows
	}
	if err := validateSize(cols, rows); err != nil {
		return 0, 0, err
	}
	return cols, rows, nil
}

// environ returns the child environment, adding TERM=xterm-256color when unset.
func (c Config) environ() []string {
	env := c.Env
	if len(env) == 0 {
		env = os.Environ()
	}
	out := make([]string, len(env), len(env)+1)
	copy(out, env)
	for _, kv := range out {
		if strings.HasPrefix(kv, "TERM=") {
			return out
		}
	}
	return append(out, "TERM=xterm-256color")
}

func validateSize(cols, rows int) error {
	if cols <= 0 || rows <= 0 || cols > maxDim || rows > maxDim {
		return fmt.Errorf("pty: invalid size %dx%d", cols, rows)
	}
	return nil
}

var errNoCommand = errors.New("pty: command is required")

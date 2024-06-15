package pseudo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/mikelorant/shellshot/internal/terminal"

	"github.com/creack/pty"
)

type Pseudo struct {
	ptmx *os.File
	opts terminal.Options
}

var errStart = errors.New("pseudo terminal not started")

const (
	defaultCmd  = "sh"
	defaultCols = 80
	defaultRows = 24
)

func New() *Pseudo {
	opts := terminal.Options{
		Cmd:  defaultCmd,
		Cols: defaultCols,
		Rows: defaultRows,
	}

	return &Pseudo{
		opts: opts,
	}
}

func (ps *Pseudo) Start(opts ...func(*terminal.Options)) error {
	for _, o := range opts {
		o(&ps.opts)
	}

	cmd := exec.Command(ps.opts.Cmd)

	size := &pty.Winsize{
		Cols: uint16(ps.opts.Cols),
		Rows: uint16(ps.opts.Rows),
	}

	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return fmt.Errorf("unable to start pseudo terminal: %w", err)
	}

	ps.ptmx = ptmx

	return nil
}

func (ps *Pseudo) Write(p []byte) (int, error) {
	if ps.ptmx == nil {
		return 0, errStart
	}

	return ps.ptmx.Write(p)
}

func (ps *Pseudo) Read(p []byte) (n int, err error) {
	if ps.ptmx == nil {
		return 0, errStart
	}

	return ps.ptmx.Read(p)
}

func (ps *Pseudo) Close() error {
	if ps.ptmx == nil {
		return nil
	}

	return ps.ptmx.Close()
}

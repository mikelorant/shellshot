package pseudo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type Pseudo struct {
	ptmx *os.File
	opts Options
}

var errStart = errors.New("pseudo terminal not started")

const (
	defaultCmd  = "sh"
	defaultCols = 80
	defaultRows = 24
)

func New(options ...func(*Options)) *Pseudo {
	opts := Options{
		Cmd:  defaultCmd,
		Cols: defaultCols,
		Rows: defaultRows,
	}

	for _, o := range options {
		o(&opts)
	}

	return &Pseudo{
		opts: opts,
	}
}

func (ps *Pseudo) Start() error {
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

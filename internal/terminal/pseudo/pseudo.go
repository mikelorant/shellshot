package pseudo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type Terminal struct {
	ptmx *os.File
	opts Options
}

var errStart = errors.New("pseudo terminal not started")

const (
	defaultCmd  = "sh"
	defaultCols = 80
	defaultRows = 24
)

func New(options ...func(*Options)) *Terminal {
	opts := Options{
		cmd:  defaultCmd,
		cols: defaultCols,
		rows: defaultRows,
	}

	for _, o := range options {
		o(&opts)
	}

	return &Terminal{
		opts: opts,
	}
}

func (t *Terminal) Start() error {
	cmd := exec.Command(t.opts.cmd)

	size := &pty.Winsize{
		Cols: uint16(t.opts.cols),
		Rows: uint16(t.opts.rows),
	}

	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return fmt.Errorf("unable to start pseudo terminal: %w", err)
	}

	t.ptmx = ptmx

	return nil
}

func (t *Terminal) Write(p []byte) (int, error) {
	if t.ptmx == nil {
		return 0, errStart
	}

	return t.ptmx.Write(p)
}

func (t *Terminal) Read(p []byte) (int, error) {
	if t.ptmx == nil {
		return 0, errStart
	}

	return t.ptmx.Read(p)
}

func (t *Terminal) Close() error {
	if t.ptmx == nil {
		return nil
	}

	return t.ptmx.Close()
}

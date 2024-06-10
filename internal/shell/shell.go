package shell

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/creack/pty"
)

type Shell struct {
	ptmx    *os.File
	shell   string
	winsize *pty.Winsize
}

var errStart = errors.New("pseudo terminal multiplexer not started")

const (
	defaultShell = "sh"
	defaultCols  = 80
	defaultRows  = 24
)

func New() *Shell {
	size := &pty.Winsize{
		Cols: defaultCols,
		Rows: defaultRows,
	}

	return &Shell{
		shell:   defaultShell,
		winsize: size,
	}
}

func (s *Shell) Start(opts ...func(*Shell)) (*os.File, error) {
	for _, o := range opts {
		o(s)
	}

	cmd := exec.Command(s.shell)

	ptmx, err := pty.StartWithSize(cmd, s.winsize)
	if err != nil {
		return nil, fmt.Errorf("unable to start shell: %w", err)
	}

	s.ptmx = ptmx

	return ptmx, nil
}

func (s *Shell) WriteString(str string) error {
	return s.Write([]byte(str))
}

func (s *Shell) Write(bs []byte) error {
	if s.ptmx == nil {
		return errStart
	}

	_, err := s.ptmx.Write(bs)
	if err != nil {
		return fmt.Errorf("unable to write to ptmx: %w", err)
	}

	return nil
}

func WithShell(shell string) func(*Shell) {
	return func(s *Shell) {
		s.shell = shell
	}
}

func WithCols(cols int) func(*Shell) {
	return func(s *Shell) {
		s.winsize.Cols = uint16(cols)
	}
}

func WithRows(rows int) func(*Shell) {
	return func(s *Shell) {
		s.winsize.Rows = uint16(rows)
	}
}

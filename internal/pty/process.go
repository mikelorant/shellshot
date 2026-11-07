package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"syscall"

	creackpty "github.com/creack/pty"
)

// Process is a command running inside a PTY. Reading from it returns the
// terminal output; Read returns io.EOF once the child side has closed.
// Writing to it sends input to the child's stdin.
type Process struct {
	ptmx *os.File
	cmd  *exec.Cmd

	done     chan struct{} // closed once cmd.Wait has returned
	exitCode int
	waitErr  error

	closeOnce sync.Once
	closeErr  error
}

var _ io.ReadWriter = (*Process)(nil)

// Start launches cfg.Command in a new PTY. Cancelling ctx kills the process.
func Start(ctx context.Context, cfg Config) (*Process, error) {
	if cfg.Command == "" {
		return nil, errNoCommand
	}
	cols, rows, err := cfg.size()
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Env = cfg.environ()
	cmd.Dir = cfg.Dir

	ptmx, err := creackpty.StartWithSize(cmd, &creackpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
	if err != nil {
		return nil, fmt.Errorf("pty: start %q: %w", cfg.Command, err)
	}

	p := &Process{ptmx: ptmx, cmd: cmd, done: make(chan struct{})}
	go p.reap()
	go func() {
		select {
		case <-ctx.Done():
			p.kill()
		case <-p.done:
		}
	}()
	return p, nil
}

// reap waits for the child to exit and records its exit status.
func (p *Process) reap() {
	defer close(p.done)
	err := p.cmd.Wait()
	if err == nil {
		return
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		p.exitCode, p.waitErr = -1, err
		return
	}
	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		p.exitCode = 128 + int(ws.Signal())
		return
	}
	p.exitCode = exitErr.ExitCode()
}

// kill terminates the child's process group (the child is a session leader,
// so its pid is the group id), if it has not already exited.
func (p *Process) kill() {
	select {
	case <-p.done:
		return
	default:
	}
	_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	_ = p.cmd.Process.Kill()
}

// Read reads PTY output. It returns io.EOF when the child side is closed.
func (p *Process) Read(b []byte) (int, error) {
	n, err := p.ptmx.Read(b)
	if errors.Is(err, syscall.EIO) {
		err = io.EOF
	}
	return n, err
}

// Write sends b to the child's stdin through the PTY master. After Close it
// returns an error wrapping os.ErrClosed.
func (p *Process) Write(b []byte) (int, error) {
	n, err := p.ptmx.Write(b)
	if err != nil {
		return n, fmt.Errorf("pty: write: %w", err)
	}
	return n, nil
}

// Resize changes the terminal size seen by the child.
func (p *Process) Resize(cols, rows int) error {
	if err := validateSize(cols, rows); err != nil {
		return err
	}
	if err := creackpty.Setsize(p.ptmx, &creackpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		return fmt.Errorf("pty: resize: %w", err)
	}
	return nil
}

// Wait blocks until the process exits and returns its exit code. A non-zero
// exit is not an error. A process killed by a signal reports 128+signal.
func (p *Process) Wait() (int, error) {
	<-p.done
	return p.exitCode, p.waitErr
}

// Close kills the process if still running, waits for it, and closes the PTY.
// It is safe to call more than once.
func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		p.kill()
		<-p.done
		if err := p.ptmx.Close(); err != nil {
			p.closeErr = fmt.Errorf("pty: close: %w", err)
		}
	})
	return p.closeErr
}

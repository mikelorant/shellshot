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
	"time"

	creackpty "github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// drainPollInterval is how often reap checks whether the master has been
// drained before it releases the slave.
const drainPollInterval = time.Millisecond

// Process is a command running inside a PTY. Reading from it returns the
// terminal output; Read returns io.EOF once the child side has closed.
// Writing to it sends input to the child's stdin.
type Process struct {
	ptmx *os.File
	// slave is our own copy of the terminal side. It is held open until the
	// child has exited and the master has been drained (see reap), so that the
	// child's exit is never the last close of the slave while output is unread.
	slave *os.File
	cmd   *exec.Cmd

	done     chan struct{} // closed once cmd.Wait has returned
	closing  chan struct{} // closed by Close: stop waiting for the master to drain
	released chan struct{} // closed once reap has closed slave
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

	ptmx, slave, err := openPTY()
	if err != nil {
		return nil, fmt.Errorf("pty: start %q: %w", cfg.Command, err)
	}
	if err := creackpty.Setsize(ptmx, &creackpty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}); err != nil {
		_ = ptmx.Close()
		_ = slave.Close()
		return nil, fmt.Errorf("pty: start %q: %w", cfg.Command, err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = slave, slave, slave
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true}
	if err := cmd.Start(); err != nil {
		_ = ptmx.Close()
		_ = slave.Close()
		return nil, fmt.Errorf("pty: start %q: %w", cfg.Command, err)
	}

	p := &Process{
		ptmx: ptmx, slave: slave, cmd: cmd,
		done: make(chan struct{}), closing: make(chan struct{}), released: make(chan struct{}),
	}
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

// openAttempts is how many times openPTY tries to allocate a pty.
const openAttempts = 5

// openPTY allocates a pty. On macOS, opening many ptys at once occasionally
// fails with a spurious ioctl error that the next attempt does not repeat.
func openPTY() (ptmx, slave *os.File, err error) {
	for range openAttempts {
		if ptmx, slave, err = creackpty.Open(); err == nil {
			return ptmx, slave, nil
		}
	}
	return nil, nil, err
}

// reap waits for the child to exit, records its exit status and then releases
// the slave.
//
// The parent keeps its own slave open while the child runs. Otherwise the
// child's exit would be the last close of the slave, and macOS then discards
// output the master has not read yet (after waiting a while for it to drain),
// losing the end of a fast child's output. Once the child has exited the slave
// is closed as soon as the master holds no unread output, or on Close.
func (p *Process) reap() {
	defer close(p.released)
	p.wait()
	for p.hasUnread() {
		select {
		case <-p.closing:
			_ = p.slave.Close()
			return
		case <-time.After(drainPollInterval):
		}
	}
	_ = p.slave.Close()
}

// wait runs cmd.Wait, records the exit status and closes done.
func (p *Process) wait() {
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

// hasUnread reports whether the master holds output that has not been read.
// It reports false if it cannot tell.
func (p *Process) hasUnread() bool {
	rc, err := p.ptmx.SyscallConn()
	if err != nil {
		return false
	}
	readable := false
	_ = rc.Control(func(fd uintptr) {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		n, err := unix.Poll(fds, 0)
		readable = err == nil && n > 0 && fds[0].Revents&unix.POLLIN != 0
	})
	return readable
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

// DisableEOTEcho stops the terminal from echoing a Ctrl-D (EOT) byte written
// afterwards, so it does not appear in the output as "^D". On macOS this
// switches off all echo; on Linux only the ^X notation for control
// characters. It is a no-op on other platforms and is not reversed.
func (p *Process) DisableEOTEcho() error {
	if err := disableEOTEcho(p.ptmx); err != nil {
		return fmt.Errorf("pty: disable EOT echo: %w", err)
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
//
// The master is closed before waiting: a child that exited with output nobody
// read is held in its exit until the master is read or closed.
func (p *Process) Close() error {
	p.closeOnce.Do(func() {
		p.kill()
		close(p.closing)
		if err := p.ptmx.Close(); err != nil {
			p.closeErr = fmt.Errorf("pty: close: %w", err)
		}
		<-p.released
	})
	return p.closeErr
}

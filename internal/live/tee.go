// Package live makes a process interactive: it mirrors process output to a
// live writer and pumps user input into the process. It depends only on io
// interfaces.
package live

import (
	"errors"
	"io"
)

// Process is a running program with a bidirectional stream. It is satisfied
// by *pty.Process and is a superset of session.Process.
type Process interface {
	io.ReadWriter
	// Resize changes the terminal size seen by the child.
	Resize(cols, rows int) error
	// Wait blocks until the process exits and returns its exit code.
	Wait() (int, error)
	// Close kills the process if still running and releases it.
	Close() error
}

// ErrNoForeground is returned by ShellInForeground when the process cannot
// report its foreground process group.
var ErrNoForeground = errors.New("the process cannot report its foreground process group")

// ForegroundReporter is implemented by a Process that can tell whether it is
// the foreground process group of its terminal; *pty.Process does. It is
// optional, so a wrapper around a Process must forward it with
// ShellInForeground or the wrapped process loses the capability.
type ForegroundReporter interface {
	ShellInForeground() (bool, error)
}

// ShellInForeground asks p whether it is the foreground process group of its
// terminal. It returns ErrNoForeground when p is not a ForegroundReporter.
func ShellInForeground(p Process) (bool, error) {
	r, ok := p.(ForegroundReporter)
	if !ok {
		return false, ErrNoForeground
	}
	return r.ShellInForeground()
}

// Tee returns a Process whose Read delegates to p.Read and copies the bytes
// read to out before returning them, so out shows output in real time. All
// other methods delegate to p.
//
// A failing out never breaks the recording: after the first write error to
// out, Tee stops writing to out and keeps returning p's data unchanged. The
// error is not exposed.
//
// Read must not be called concurrently, which matches how a session reads a
// process.
func Tee(p Process, out io.Writer) Process {
	return &teeProcess{Process: p, out: out}
}

type teeProcess struct {
	Process
	out       io.Writer
	outFailed bool
}

func (t *teeProcess) Read(b []byte) (int, error) {
	n, err := t.Process.Read(b)
	if n > 0 && !t.outFailed {
		if _, werr := t.out.Write(b[:n]); werr != nil {
			t.outFailed = true
		}
	}
	return n, err
}

// ShellInForeground implements ForegroundReporter by asking the wrapped
// process.
func (t *teeProcess) ShellInForeground() (bool, error) {
	return ShellInForeground(t.Process)
}

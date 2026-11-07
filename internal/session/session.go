// Package session runs a program in a pseudo-terminal, feeds its output to a
// headless virtual terminal and emits snapshot frames to a Sink. It depends
// only on small interfaces (Process, vt.Terminal), never on pty or cgo.
package session

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
	"github.com/michaellorant/shellshot/internal/vt"
)

// readBufferSize is the size of the buffer used for each read from the process.
const readBufferSize = 32 * 1024

// chunkQueueLen is how many output chunks may be queued between the reader
// goroutine and the main loop.
const chunkQueueLen = 16

// Run drives proc until it exits and returns its exit code.
//
// A single goroutine reads proc and forwards copies of the output over a
// channel. The calling goroutine is the sole owner of the terminal (vt
// terminals are not safe for concurrent use): it writes the chunks to the
// terminal and, on every tick, snapshots it.
//
// Frames sent to sink:
//   - An initial frame at t=0, before any output has been processed.
//   - A frame, stamped with the time since Run started, on each tick at which
//     the screen differs from the previously emitted frame.
//   - A final frame once the output ends (EOF), after all remaining output has
//     been written to the terminal. The final frame is ALWAYS emitted exactly
//     once, even if it equals the previous frame, so a recording always ends
//     with the terminal's final state.
//
// Ticks arrive cfg.Period() apart (60 per second by default). A tick only
// snapshots the terminal if output was written since the previous snapshot,
// so an idle session does no work however high the rate is.
//
// After the final frame Run waits for the process and returns its exit code;
// a non-zero exit code is not an error.
//
// If cfg.Events is set it also receives the raw output: each chunk is passed
// to Events.Output, stamped with the time since Run started at which the read
// returned, before the chunk is written to the terminal. After the process has
// been waited for, Events.Exit receives the exit code. Nothing is sent while
// the process is idle. An Events error aborts the run like a sink error.
//
// Config.OnStart receives the start time once, before the process is read, and
// Config.OnTick is called on every tick before sampling (see Config).
//
// If cfg.Driver is set (see Driver) the process must also implement
// io.Writer. Run polls the driver on its own goroutine at the start, after
// output and at the driver's deadlines, carries out its writes, marks, spans
// and shutdown, and keeps running until the driver is done and the process
// has exited, with Events.Exit logged once at the end. A driver error, or a
// cancelled ctx, ends the driver's open spans in the event log first.
//
// If ctx is cancelled Run closes proc and returns an error wrapping ctx.Err().
// A sink error, terminal error or non-EOF read error also aborts the run with
// a wrapped error. On every path Run closes the terminal and proc and does not
// return until its goroutines have finished. The exit code is -1 when err is
// non-nil. Errors from closing proc are ignored.
func Run(ctx context.Context, proc Process, newTerm vt.Factory, cfg Config, sink Sink) (exitCode int, err error) {
	cfg = cfg.withDefaults()
	defer func() { _ = proc.Close() }()

	if cfg.FPS > MaxFPS {
		return -1, fmt.Errorf("%w: %d (maximum %d)", ErrFPS, cfg.FPS, MaxFPS)
	}
	var drv *driverState
	if cfg.Driver != nil {
		w, ok := proc.(io.Writer)
		if !ok {
			return -1, ErrDriverNeedsWriter
		}
		drv = newDriverState(cfg.Driver, w)
	}
	if err := ctx.Err(); err != nil {
		return -1, cancelError(err)
	}
	term, err := newTerm(cfg.Cols, cfg.Rows)
	if err != nil {
		return -1, fmt.Errorf("session: create terminal: %w", err)
	}
	defer func() {
		if cerr := term.Close(); cerr != nil && err == nil {
			exitCode, err = -1, fmt.Errorf("session: close terminal: %w", cerr)
		}
	}()
	if err := proc.Resize(cfg.Cols, cfg.Rows); err != nil {
		return -1, fmt.Errorf("session: resize process: %w", err)
	}

	s := &session{proc: proc, term: term, sink: sink, cfg: cfg, drv: drv}
	exitCode, err = s.run(ctx)
	if err != nil {
		return -1, err
	}
	return exitCode, nil
}

// session holds the state of one Run call. All fields are owned by the
// goroutine running Run.
type session struct {
	proc Process
	term vt.Terminal
	sink Sink
	cfg  Config

	start time.Time
	last  snapshot.Snapshot
	dirty bool // terminal written since the last snapshot

	in      <-chan chunk // output chunks; nil once the reader has finished with a driver
	readErr error        // set by the reader before it closes the chunk channel
	waited  <-chan waitResult
	drv     *driverState // nil without Config.Driver
}

// waitResult is the outcome of Process.Wait.
type waitResult struct {
	code int
	err  error
}

// run is the main loop. It returns once the process has exited or the run
// has been aborted, and joins every goroutine it started.
//
// With a Driver the loop also serves the driver's timers and the process
// exit: it does not end when the output ends but once the driver is done and
// the process has exited (see driver.go). Without one it is as it always was.
func (s *session) run(ctx context.Context) (int, error) {
	s.start = s.cfg.Clock() // set before the reader starts: it stamps chunks with it
	if s.cfg.OnStart != nil {
		s.cfg.OnStart(s.start)
	}
	chunks := make(chan chunk, chunkQueueLen)
	s.in = chunks
	stop := make(chan struct{})
	go func() {
		defer close(chunks)
		s.readErr = pump(s.proc, chunks, stop, s.elapsed)
	}()

	defer func() {
		close(stop)
		_ = s.proc.Close() // unblocks a pending Read and Wait
		for range chunks { // returns once the reader has exited
		}
		if s.waited != nil {
			<-s.waited
		}
		if s.drv != nil {
			s.drv.deadline.cancel()
			s.drv.grace.cancel()
		}
	}()

	ticks, stopTicker := s.cfg.Ticker(s.cfg.Period())
	defer stopTicker()

	snap, err := s.term.Snapshot()
	if err != nil {
		return 0, fmt.Errorf("session: snapshot: %w", err)
	}
	if err := s.emit(0, snap); err != nil {
		return 0, err
	}

	// These stay nil (never ready) without a driver.
	var again <-chan struct{}
	if s.drv != nil {
		again = s.drv.again
		if err := s.driverPoll(); err != nil {
			return 0, err
		}
	}

loop:
	for {
		var deadline, grace <-chan time.Time
		if s.drv != nil {
			if s.drv.finished() {
				break loop
			}
			deadline, grace = s.drv.deadline.armed(), s.drv.grace.armed()
		}
		select {
		case <-ctx.Done():
			return 0, s.cancelled(ctx.Err())
		case c, ok := <-s.in:
			if !ok {
				if s.drv == nil {
					break loop
				}
				if err := s.handleEOF(); err != nil {
					return 0, err
				}
				continue
			}
			if err := s.handleChunk(c); err != nil {
				return 0, err
			}
			if s.drv != nil {
				if err := s.driverPoll(); err != nil {
					return 0, err
				}
			}
		case <-ticks:
			if s.cfg.OnTick != nil {
				if err := s.cfg.OnTick(s.cfg.Clock()); err != nil {
					return 0, fmt.Errorf("session: tick: %w", err)
				}
			}
			if err := s.sample(); err != nil {
				return 0, err
			}
		case <-deadline:
			s.drv.deadline.cancel()
			if err := s.driverPoll(); err != nil {
				return 0, err
			}
		case <-again:
			if err := s.driverPoll(); err != nil {
				return 0, err
			}
		case <-grace:
			s.closeOnGrace()
		case r := <-s.waited:
			if err := s.handleExit(r); err != nil {
				return 0, err
			}
		}
	}

	// The reader has finished and every chunk has been written to the terminal.
	if ctx.Err() != nil {
		return 0, s.cancelled(ctx.Err())
	}
	if s.drv == nil && s.readErr != nil {
		return 0, fmt.Errorf("session: read process output: %w", s.readErr)
	}
	snap, err = s.term.Snapshot()
	if err != nil {
		return 0, fmt.Errorf("session: snapshot: %w", err)
	}
	if err := s.emit(s.elapsed(), snap); err != nil {
		return 0, err
	}

	if s.drv != nil {
		if s.cfg.Events != nil {
			if err := s.cfg.Events.Exit(s.elapsed(), s.drv.exitCode); err != nil {
				return 0, fmt.Errorf("session: events: %w", err)
			}
		}
		return s.drv.exitCode, nil
	}

	s.startWait()
	select {
	case <-ctx.Done():
		return 0, cancelError(ctx.Err())
	case r := <-s.waited:
		s.waited = nil
		if r.err != nil {
			return 0, fmt.Errorf("session: wait for process: %w", r.err)
		}
		if s.cfg.Events != nil {
			if err := s.cfg.Events.Exit(s.elapsed(), r.code); err != nil {
				return 0, fmt.Errorf("session: events: %w", err)
			}
		}
		return r.code, nil
	}
}

// handleChunk logs a chunk of output and writes it to the terminal.
func (s *session) handleChunk(c chunk) error {
	if s.cfg.Events != nil {
		if err := s.cfg.Events.Output(c.at, c.b); err != nil {
			return fmt.Errorf("session: events: %w", err)
		}
	}
	if _, err := s.term.Write(c.b); err != nil {
		return fmt.Errorf("session: write terminal: %w", err)
	}
	s.dirty = true
	if s.drv != nil {
		s.drv.textValid = false
	}
	return nil
}

// startWait waits for the process on a goroutine of its own (Wait does not
// touch the terminal) and delivers the result on s.waited.
func (s *session) startWait() {
	ch := make(chan waitResult, 1)
	s.waited = ch
	go func() {
		code, err := s.proc.Wait()
		ch <- waitResult{code, err}
	}()
}

// sample takes a snapshot and emits it if the screen changed since the last
// emitted frame.
func (s *session) sample() error {
	if !s.dirty {
		return nil
	}
	s.dirty = false
	snap, err := s.term.Snapshot()
	if err != nil {
		return fmt.Errorf("session: snapshot: %w", err)
	}
	if snap.Equal(s.last) {
		return nil
	}
	return s.emit(s.elapsed(), snap)
}

// emit sends a frame to the sink and records it as the last emitted one.
func (s *session) emit(at time.Duration, snap snapshot.Snapshot) error {
	if err := s.sink.Frame(snapshot.Frame{At: at, Snapshot: snap}); err != nil {
		return fmt.Errorf("session: sink: %w", err)
	}
	s.last = snap
	s.dirty = false
	return nil
}

// elapsed returns the time since the session started.
func (s *session) elapsed() time.Duration { return s.cfg.Clock().Sub(s.start) }

// cancelError wraps a context error so errors.Is(err, ctx.Err()) holds.
func cancelError(err error) error { return fmt.Errorf("session: %w", err) }

// chunk is one read from the process, stamped with the time since the session
// started at which the read returned.
type chunk struct {
	at time.Duration
	b  []byte
}

// pump copies r to out until EOF, a read error, or stop is closed. Each chunk
// is a fresh copy, so the receiver may keep it, and is stamped with elapsed()
// as soon as Read returns. io.EOF is not an error.
func pump(r io.Reader, out chan<- chunk, stop <-chan struct{}, elapsed func() time.Duration) error {
	buf := make([]byte, readBufferSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			c := chunk{at: elapsed(), b: append([]byte(nil), buf[:n]...)}
			select {
			case out <- c:
			case <-stop:
				return nil
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

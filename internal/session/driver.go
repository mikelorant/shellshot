package session

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// Driver scripts a session (see Config.Driver). It is a non-blocking state
// machine that Run polls on its own goroutine, the only goroutine that
// touches the terminal; no method may block.
//
// Run polls the driver once at the start, after the output that has arrived
// (a burst of queued chunks counts as one) and at the Deadline of the last
// Step. All times are offsets from the start of Run, the origin of every event
// time. A Poll never has a time before the previous one.
type Driver interface {
	// Poll advances the driver at time now with the current screen and returns
	// what the session must do. After an error, Run stops injecting input,
	// ends the open spans, logs the exit of the process (which it kills) and
	// returns the error wrapped.
	Poll(now time.Duration, scr Screen) (Step, error)
	// ChildExited reports, once, that the process has exited with status and
	// that all its output has been written to the terminal.
	ChildExited(now time.Duration, status int)
	// OpenSpans returns the span-end operations for the spans that are open,
	// innermost first. Run logs them when the run fails or is cancelled so the
	// recording stays well formed.
	OpenSpans() []SpanOp
}

// Screen is the terminal as the driver sees it.
type Screen interface {
	// Text returns the visible rows (the alternate screen when active), each
	// right-trimmed, joined by "\n", with trailing blank rows removed. The
	// terminal is only snapshotted when the screen changed since the last call
	// that needed one, so a driver that does not call Text costs nothing.
	Text() string
}

// SpanOp is a span boundary for Events.Span. The session never looks inside
// it, so it needs no dependency on the recording format: the driver and the
// EventSink agree on the dynamic type (the CLI uses event.Span).
type SpanOp interface{}

// Step is what the session must do after one Poll, in this order: Write, Mark,
// Span, Shutdown.
type Step struct {
	// Write is sent to the process now. A write error aborts the run.
	Write []byte
	// Mark, if not empty, is logged with Events.Mark now.
	Mark string
	// Span, if not nil, is logged with Events.Span now.
	Span SpanOp
	// Shutdown asks the session to end the child: it sends EOF (Ctrl-D, 0x04)
	// and, if the child is still running after Config.ShutdownGrace, closes
	// the process.
	Shutdown bool
	// Deadline is the time at which Poll must be called again if nothing else
	// happens. A Deadline not after the time of the Poll means more work is
	// ready and Poll is called again at once (at most maxConsecutivePolls
	// times in a row before the loop looks at other events). A Step that does
	// nothing and has no later Deadline is not polled again until output
	// arrives or the child exits.
	Deadline time.Duration
	// Done means the script is over. Poll is not called again. The session
	// waits for the child to exit (ShutdownGrace after Shutdown or after
	// Done, whichever came first, then it closes the process), writes the
	// final frame and ends as it does without a driver.
	Done bool
}

// maxConsecutivePolls bounds the polls made back to back for Steps whose
// Deadline is not after their time, so the loop still serves ctx, ticks and
// output.
const maxConsecutivePolls = 64

// eot is Ctrl-D, end of transmission.
const eot = 0x04

// timer is a one-shot timer made with Config.Ticker, so tests drive it.
type timer struct {
	c    <-chan time.Time
	stop func()
}

func (t *timer) cancel() {
	if t.stop != nil {
		t.stop()
	}
	t.c, t.stop = nil, nil
}

// armed returns the timer channel, nil (never ready) when not armed.
func (t *timer) armed() <-chan time.Time { return t.c }

// driverState is the session's state for Config.Driver. It is nil without a
// driver. All fields are owned by the goroutine running Run.
type driverState struct {
	d Driver
	w io.Writer

	again    chan struct{} // yields to the loop between bursts of polls
	deadline timer         // next Poll
	armedFor time.Duration // the deadline of the armed timer
	grace    timer         // closes the process if it ignores the shutdown

	done     bool // Step.Done seen
	exited   bool // Process.Wait returned; ChildExited was called
	exitCode int
	closed   bool // the session closed the process itself

	text      string // cached Screen.Text
	textValid bool   // the terminal is unchanged since text was taken
	screenErr error  // snapshot error met inside Screen.Text
}

func newDriverState(d Driver, w io.Writer) *driverState {
	return &driverState{d: d, w: w, again: make(chan struct{}, 1)}
}

// finished reports that the loop has nothing more to wait for.
func (d *driverState) finished() bool { return d.done && d.exited }

// screen is the Screen handed to Driver.Poll.
type screen struct{ s *session }

// Text implements Screen. A snapshot error is kept and reported by Run after
// Poll returns.
func (v screen) Text() string {
	d := v.s.drv
	if d.textValid {
		return d.text
	}
	snap, err := v.s.term.Snapshot()
	if err != nil {
		if d.screenErr == nil {
			d.screenErr = fmt.Errorf("session: snapshot: %w", err)
		}
		return ""
	}
	d.text, d.textValid = strings.TrimRight(snap.Text(), "\n"), true
	return d.text
}

// armTimer (re)arms t to fire after d.
func (s *session) armTimer(t *timer, d time.Duration) {
	t.cancel()
	t.c, t.stop = s.cfg.Ticker(max(d, time.Nanosecond))
}

// driverPoll polls the driver, and acts on its Steps, until it has a
// Deadline in the future, is done, or has made maxConsecutivePolls polls.
// Output that has already arrived is processed first, so the driver sees a
// current screen and events are logged in order.
func (s *session) driverPoll() error {
	d := s.drv
	if d.done {
		return nil
	}
	if err := s.drainPending(); err != nil {
		return err
	}
	for range maxConsecutivePolls {
		now := s.elapsed()
		step, err := d.d.Poll(now, screen{s})
		if d.screenErr != nil {
			return d.screenErr
		}
		if err != nil {
			return s.failDriver(err)
		}
		if err := s.act(step); err != nil {
			return err
		}
		if step.Done {
			d.done = true
			d.deadline.cancel()
			if !d.exited && d.grace.armed() == nil {
				s.armTimer(&d.grace, s.cfg.ShutdownGrace)
			}
			return nil
		}
		acts := len(step.Write) > 0 || step.Mark != "" || step.Span != nil || step.Shutdown
		if step.Deadline > now {
			if d.deadline.armed() == nil || d.armedFor != step.Deadline {
				s.armTimer(&d.deadline, step.Deadline-now)
				d.armedFor = step.Deadline
			}
			return nil
		}
		if !acts {
			return nil // waiting for output or the child's exit
		}
		d.deadline.cancel()
	}
	select {
	case d.again <- struct{}{}:
	default:
	}
	return nil
}

// act carries out a Step.
func (s *session) act(step Step) error {
	d := s.drv
	if len(step.Write) > 0 {
		if _, err := d.w.Write(step.Write); err != nil {
			return fmt.Errorf("session: write to process: %w", err)
		}
	}
	if step.Mark != "" && s.cfg.Events != nil {
		if err := s.cfg.Events.Mark(s.elapsed(), step.Mark); err != nil {
			return fmt.Errorf("session: events: %w", err)
		}
	}
	if step.Span != nil && s.cfg.Events != nil {
		if err := s.cfg.Events.Span(s.elapsed(), step.Span); err != nil {
			return fmt.Errorf("session: events: %w", err)
		}
	}
	if step.Shutdown && !d.exited {
		// Best effort: a child that is gone, or a pty that refuses, is dealt
		// with by the grace timer.
		_, _ = d.w.Write([]byte{eot})
		if d.grace.armed() == nil {
			s.armTimer(&d.grace, s.cfg.ShutdownGrace)
		}
	}
	return nil
}

// drainPending processes the output chunks that are already queued (at most
// the queue length), without blocking.
func (s *session) drainPending() error {
	for range chunkQueueLen {
		if s.in == nil {
			return nil
		}
		select {
		case c, ok := <-s.in:
			if !ok {
				return s.handleEOF()
			}
			if err := s.handleChunk(c); err != nil {
				return err
			}
		default:
			return nil
		}
	}
	return nil
}

// handleEOF is called when the reader has finished and every chunk has been
// written to the terminal. With a driver the session carries on: it waits for
// the process and tells the driver.
func (s *session) handleEOF() error {
	s.in = nil
	if s.readErr != nil && !s.drv.closed {
		return fmt.Errorf("session: read process output: %w", s.readErr)
	}
	s.startWait()
	return nil
}

// handleExit is called with the result of Process.Wait when a driver is set.
func (s *session) handleExit(r waitResult) error {
	s.waited = nil
	if r.err != nil {
		return fmt.Errorf("session: wait for process: %w", r.err)
	}
	d := s.drv
	d.exited, d.exitCode = true, r.code
	d.grace.cancel()
	d.d.ChildExited(s.elapsed(), r.code)
	return s.driverPoll()
}

// closeOnGrace closes a process that ignored the shutdown.
func (s *session) closeOnGrace() {
	d := s.drv
	d.grace.cancel()
	if !d.exited {
		d.closed = true
		_ = s.proc.Close()
	}
}

// closeSpans logs the span ends of the spans the driver has open. Errors are
// ignored: the run is already ending, and the writer keeps its first error.
func (s *session) closeSpans() {
	if s.drv == nil || s.cfg.Events == nil {
		return
	}
	for _, op := range s.drv.d.OpenSpans() {
		_ = s.cfg.Events.Span(s.elapsed(), op)
	}
}

// cancelled ends the run for a cancelled context.
func (s *session) cancelled(err error) error {
	s.closeSpans()
	return cancelError(err)
}

// failDriver ends the run after Driver.Poll failed: the open spans are ended,
// the process is killed and its exit logged, so the recording is complete.
func (s *session) failDriver(pollErr error) error {
	d := s.drv
	s.closeSpans()
	d.closed = true
	_ = s.proc.Close()
	code := -1
	if d.exited {
		code = d.exitCode
	} else {
		var r waitResult
		if s.waited != nil {
			r = <-s.waited
			s.waited = nil
		} else {
			r.code, r.err = s.proc.Wait()
		}
		if r.err == nil {
			code = r.code
		}
	}
	err := fmt.Errorf("session: driver: %w", pollErr)
	if s.cfg.Events != nil {
		if eerr := s.cfg.Events.Exit(s.elapsed(), code); eerr != nil {
			err = errors.Join(err, fmt.Errorf("session: events: %w", eerr))
		}
	}
	return err
}

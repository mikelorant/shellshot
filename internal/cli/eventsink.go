package cli

import (
	"fmt"
	"sync"
	"time"

	"github.com/mikelorant/shellshot/internal/live"
	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/session"
)

// binSink adapts a *bin.Writer to session.EventSink and also records the
// keystrokes forwarded to the child.
//
// bin.Writer is not safe for concurrent use, and its events come from two
// goroutines: the session's (output, resize, exit and the tick that flushes
// on time) and the input pump's. A mutex serialises them, so the writer needs
// no goroutine of its own and an idle session still writes nothing.
//
// Event times are offsets from one origin, the start of the session, which
// Start receives from session.Config.OnStart.
type binSink struct {
	w   *bin.Writer
	now func() time.Time // the session clock

	mu      sync.Mutex
	origin  time.Time
	started bool
}

// newBinSink returns a sink that writes to w. now is the clock the session
// runs on (see configure).
func newBinSink(w *bin.Writer, now func() time.Time) *binSink {
	return &binSink{w: w, now: now}
}

// configure points cfg at the sink: its events, clock, start hook and the tick
// that flushes pending events.
func (s *binSink) configure(cfg *session.Config) {
	cfg.Events = s
	cfg.Clock = s.now
	cfg.OnStart = s.start
	cfg.OnTick = s.tick
}

// start records the origin of event times.
func (s *binSink) start(origin time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.origin, s.started = origin, true
}

// tick flushes the pending block if it is due. It writes nothing when no
// event is pending.
func (s *binSink) tick(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Tick(now)
}

// Output implements session.EventSink. bin.Writer copies p.
func (s *binSink) Output(at time.Duration, p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Output(at, p)
}

// Resize implements session.EventSink. The cell pixel size is unknown.
func (s *binSink) Resize(at time.Duration, cols, rows int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Resize(at, cols, rows, 0, 0)
}

// Mark implements session.EventSink.
func (s *binSink) Mark(at time.Duration, label string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Mark(at, label)
}

// Span implements session.EventSink. The session treats a span operation as
// opaque; for this sink it must be an event.Span, which the writer validates.
func (s *binSink) Span(at time.Duration, op session.SpanOp) error {
	sp, ok := op.(event.Span)
	if !ok {
		return fmt.Errorf("span operation is %T, want event.Span", op)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Span(at, sp)
}

// Exit implements session.EventSink.
func (s *binSink) Exit(at time.Duration, status int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Exit(at, status)
}

// input records keystrokes p forwarded to the child, stamped with the time
// since the session started (0 before it has). The writer is only valid for
// input if its head sets head.FlagInputRecorded.
//
// bin.Writer copies p, so the input pump may reuse its buffer at once.
func (s *binSink) input(p []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var at time.Duration
	if s.started {
		at = s.now().Sub(s.origin)
	}
	return s.w.Input(at, p)
}

// close flushes the pending events and finishes the recording.
func (s *binSink) close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Close()
}

// stats returns the writer counters.
func (s *binSink) stats() bin.Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Stats()
}

// inputRecorder is a live.Process that logs every write to the process as an
// input event before forwarding it.
//
// A recording failure does not stop the input: the writer keeps its first
// error, which the next output event, tick or close reports and which aborts
// the recording.
type inputRecorder struct {
	live.Process
	sink *binSink
}

// Write implements io.Writer.
func (r inputRecorder) Write(p []byte) (int, error) {
	_ = r.sink.input(p)
	return r.Process.Write(p)
}

package session

import (
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Sink receives the frames produced by a session, in order. Frame is always
// called from the goroutine that called Run, so implementations need no
// locking for session calls. Returning an error aborts the session.
type Sink interface {
	Frame(snapshot.Frame) error
}

// SinkFunc adapts an ordinary function to the Sink interface.
type SinkFunc func(snapshot.Frame) error

// Frame calls f(frame).
func (f SinkFunc) Frame(frame snapshot.Frame) error { return f(frame) }

// EventSink receives the raw event stream of a session. It is optional (see
// Config.Events). Output and Exit are always called from the goroutine that
// called Run, so implementations need no locking for session calls. Returning
// an error aborts the session. Times are offsets from the start of Run.
//
// Output is called for every chunk read from the process, in order, before the
// chunk is written to the terminal. The sink must not retain p after the call
// returns. Exit is called once, after the final frame, with the process exit
// code. Nothing is called while the process is idle.
//
// Mark and Span are called only when Config.Driver asks for them, on the same
// goroutine and with offsets from the same origin as Output. Mark records a
// marker with a UTF-8 label. Span records a span boundary; op is exactly the
// value the driver put in Step.Span (see SpanOp), so the sink must know its
// dynamic type and return an error for any other.
//
// Resize is part of the interface for event writers (such as the binary
// recording writer's adapter) but is not called by session today.
type EventSink interface {
	Output(at time.Duration, p []byte) error
	Resize(at time.Duration, cols, rows int) error
	Mark(at time.Duration, label string) error
	Span(at time.Duration, op SpanOp) error
	Exit(at time.Duration, status int) error
}

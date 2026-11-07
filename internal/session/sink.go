package session

import "github.com/michaellorant/shellshot/internal/snapshot"

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

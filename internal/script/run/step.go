package run

import (
	"math/rand/v2"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/script/keys"
)

// Keys turns key presses and typed text into the bytes sent to the child. The
// session adapter implements it over the terminal, so the bytes follow the
// terminal's current modes; the Executor therefore asks for each unit at the
// moment it sends it.
type Keys interface {
	// Encode returns the bytes of one press of chord.
	Encode(chord keys.Chord) ([]byte, error)
	// EncodeText returns the bytes of typing s; a newline is sent as Enter.
	EncodeText(s string) ([]byte, error)
}

// Screen is the headless terminal as the Executor sees it.
type Screen interface {
	// Text returns the visible rows (the alternate screen when active), each
	// right-trimmed, joined by "\n", with trailing blank rows removed.
	Text() string
}

// Deps is what the Executor needs from its host.
type Deps struct {
	Keys Keys
	// Rand is the jitter source. When nil the Executor uses a PCG seeded by
	// the plan's seed, so one (plan, screen sequence) gives one timeline.
	Rand *rand.Rand
	// File is the script path used in error messages; it may be empty.
	File string
}

// SpanOp is a SPAN begin or end event for the host to record, with the hints
// the plan resolved.
type SpanOp struct {
	Event event.Span
}

// Step is what the host must do after one Poll. At most one of Write, Mark,
// Span and Shutdown is set per Step.
type Step struct {
	// Write is bytes to send to the child now.
	Write []byte
	// Mark is the label of a MARK event to record now.
	Mark string
	// Span is a SPAN event to record now.
	Span *SpanOp
	// Shutdown asks the host to end the child: send EOF (Ctrl-D) to a shell,
	// close the PTY master for any other program. The Executor then waits for
	// ChildExited until the grace time is over and carries on. When Done is
	// set and the child still runs, the host kills it.
	Shutdown bool
	// Deadline is the absolute time at which the host must call Poll again if
	// nothing else happens (output, ChildExited). It is never before the now
	// of the Poll. A Deadline equal to now means more work is ready: call
	// Poll again at once.
	Deadline time.Duration
	// Done is set when the script has finished. It can arrive together with
	// the final Span.
	Done bool
}

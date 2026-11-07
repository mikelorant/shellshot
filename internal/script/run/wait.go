package run

import (
	"fmt"
	"strings"
	"time"
)

// advanceWait runs a wait. The pattern is tested against the current screen
// text on every Poll, never against output that has scrolled away or against
// the bytes written; a wait that already matches finishes at once.
//
// The echo trap: the terminal echoes what the script types, so a wait issued
// right after "type" (or after a "run" with "settle off") can match the echoed
// command line instead of the command's output, and pass too early. The
// Executor does nothing about it, on purpose: it cannot tell echo from
// output. The script must anchor the pattern on line start (/^BUILD OK$/, as
// the echoed line starts with the prompt) or print the sentinel through a
// split literal (echo DO"NE").
//
// cur.start is when the wait began. A wait with Assert, or a timeout of 0,
// tests once. A wait idle finishes when the screen has been quiet for
// op.Quiet, and fails at the timeout like a run's settle.
func (e *Executor) advanceWait(op OpWait, now time.Duration) (Step, bool, *ScriptError) {
	limit := e.cur.start + op.Timeout
	if op.Kind == WaitIdle {
		return e.advanceQuiet(op.Meta, "wait idle", op.Quiet, limit, now, false)
	}
	if e.matches(op) {
		return Step{}, true, nil
	}
	what := fmt.Sprintf("%q", op.Pattern)
	if op.Kind == WaitRegex {
		what = "/" + op.Pattern + "/"
	}
	if e.exited {
		return Step{}, false, e.fail(op.Meta, ChildExited, "child exited (status %d) before match: waiting for %s", e.exitStatus, what)
	}
	if op.Assert || op.Timeout <= 0 {
		return Step{}, false, e.fail(op.Meta, WaitTimeout, "assertion failed: %s is not on the screen", what)
	}
	if now >= limit {
		return Step{}, false, e.fail(op.Meta, WaitTimeout, "wait timed out after %s waiting for %s", timeoutText(op.Timeout), what)
	}
	return Step{Deadline: limit}, false, nil
}

// matches tests the wait pattern against the current screen text.
func (e *Executor) matches(op OpWait) bool {
	if op.Kind == WaitRegex {
		return e.res[e.pc].MatchString(e.screen.text)
	}
	return strings.Contains(e.screen.text, op.Pattern)
}

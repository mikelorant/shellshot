package run

import (
	"fmt"
	"strings"
	"time"

	"github.com/mikelorant/shellshot/internal/script"
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
// op.Quiet, and fails at the timeout like a run's settle. A wait done
// finishes when the command in the foreground has ended (see advanceDone).
func (e *Executor) advanceWait(op OpWait, now time.Duration) (Step, bool, *ScriptError) {
	limit := e.cur.start + op.Timeout
	switch op.Kind {
	case WaitIdle:
		return e.advanceQuiet(op.Meta, "wait idle", op.Quiet, limit, now, false)
	case WaitDone:
		return e.advanceDone(op, limit, now)
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

// DonePollInterval is how often a wait done asks to be polled. The foreground
// process group changes without any output, so nothing else would wake the
// host up.
const DonePollInterval = 50 * time.Millisecond

// advanceDone runs a wait done. The shell must be the foreground process group
// at every Poll for script.DoneDebounce; a Poll that finds a command in the
// foreground starts the count again. The shell is briefly in the foreground
// right after Enter, before it starts the command, and between the parts of
// a chain, which is why one look is not enough. The first Poll that finds the
// shell in the foreground starts the count, so even a wait done issued at the
// prompt takes the debounce.
//
// The op uses cur.flag for "the shell was in the foreground at the last Poll"
// and cur.next for the time the count started. It fails at the timeout like
// other waits.
func (e *Executor) advanceDone(op OpWait, limit, now time.Duration) (Step, bool, *ScriptError) {
	if op.Timeout < script.DoneDebounce {
		return Step{}, false, e.fail(op.Meta, WaitTimeout, "wait done needs a timeout of at least %s, not %s", script.DoneDebounce, timeoutText(op.Timeout))
	}
	if e.exited {
		return Step{}, false, e.fail(op.Meta, ChildExited, "child exited (status %d) before the command in the foreground finished", e.exitStatus)
	}
	if e.scr == nil {
		return Step{}, false, e.fail(op.Meta, ForegroundQuery, "cannot tell whether a command is running: no terminal")
	}
	fg, err := e.scr.ShellForeground()
	if err != nil {
		return Step{}, false, e.fail(op.Meta, ForegroundQuery, "cannot tell whether a command is running: %v", err)
	}
	switch {
	case !fg:
		e.cur.flag = false
	case !e.cur.flag:
		e.cur.flag, e.cur.next = true, now
	}
	doneAt := e.cur.next + script.DoneDebounce
	if e.cur.flag && doneAt <= now && doneAt <= limit {
		return Step{}, true, nil
	}
	if now >= limit {
		return Step{}, false, e.fail(op.Meta, WaitTimeout, "wait done timed out after %s: a program is still running in the foreground", timeoutText(op.Timeout))
	}
	deadline := min(now+DonePollInterval, limit)
	if e.cur.flag {
		deadline = min(deadline, doneAt)
	}
	return Step{Deadline: deadline}, false, nil
}

package run

import (
	"time"
)

// screenTracker follows the screen text across Polls. The screen counts as
// changed at the Poll that first sees different text; the host polls after
// every output chunk, so that is the time of the output.
type screenTracker struct {
	text      string
	changedAt time.Duration
	seen      bool
}

func (t *screenTracker) observe(now time.Duration, text string) {
	if !t.seen || text != t.text {
		t.text, t.changedAt, t.seen = text, now, true
	}
}

// advanceSettle runs a run's settle and the final drain of the exit
// sequence. Settle is "the screen text has not changed for Quiet", counted
// from the later of the last change and the start of the op, and it fails the
// script if that is not reached within Timeout of the start. The drain
// carries on at its limit instead of failing. Off skips the wait.
func (e *Executor) advanceSettle(op OpSettle, now time.Duration) (Step, bool, *ScriptError) {
	if op.Off {
		return Step{}, true, nil
	}
	return e.advanceQuiet(op.Meta, "run", op.Quiet, e.cur.start+op.Timeout, now, op.Drain)
}

// advanceQuiet waits until the screen has been quiet for quiet. limit is the
// latest time quiet may be reached; past it the step fails (what names the
// step in the message), or, for the drain, finishes.
func (e *Executor) advanceQuiet(m Meta, what string, quiet, limit, now time.Duration, drain bool) (Step, bool, *ScriptError) {
	quietAt := max(e.screen.changedAt, e.cur.start) + quiet
	if quietAt <= now && quietAt <= limit {
		return Step{}, true, nil
	}
	if now >= limit {
		if drain {
			return Step{}, true, nil
		}
		timeout := limit - e.cur.start
		advice := `for continuous output use "settle off" and "wait PATTERN TIMEOUT", or raise "timeout"`
		if what != "run" {
			advice = `wait for a pattern instead, or raise "timeout"`
		}
		return Step{}, false, e.fail(m, SettleTimeout, "%s did not settle: the screen was still changing after %s (timeout); %s", what, timeoutText(timeout), advice)
	}
	return Step{Deadline: min(quietAt, limit)}, false, nil
}

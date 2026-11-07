package run

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"regexp"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
)

// Executor runs a Plan as a non-blocking state machine
// (docs/research/script-format.md §7 and §8.2). It has no goroutines, no
// clock and no I/O: the host calls Poll with the time since the recording
// started and the current screen, performs what the returned Step says, and
// calls Poll again at Step.Deadline or when the screen changes. Equal plans,
// seeds and (time, screen) sequences give equal Steps.
//
// An Executor is not safe for concurrent use; the session calls it from its
// single loop.
type Executor struct {
	ops  []Op
	keys Keys
	rng  *rand.Rand
	file string
	res  map[int]*regexp.Regexp // compiled regex of each OpWait, by op index

	pc      int     // index of the current op
	entered bool    // cur is set up for ops[pc]
	cur     opState // state of the current op
	last    time.Duration

	screen screenTracker

	exited     bool
	exitStatus int

	open []uint64 // IDs of the open spans, outermost first
	err  *ScriptError
	done bool
}

// opState is the progress of the current op. Which fields an op uses is
// described with the op's advance method.
type opState struct {
	start time.Duration // when the op became current
	next  time.Duration // when the next unit is due
	units []string      // OpType: the units still to type
	n, i  int           // units in total and units sent
	flag  bool
}

// New prepares an Executor for p. It fails on a plan the Executor cannot run:
// a regex that does not compile, an unknown op, or no Keys.
func New(p *Plan, deps Deps) (*Executor, error) {
	if p == nil {
		return nil, errors.New("run: nil plan")
	}
	if deps.Keys == nil {
		return nil, errors.New("run: Deps.Keys is nil")
	}
	e := &Executor{ops: p.Ops, keys: deps.Keys, rng: deps.Rand, file: deps.File, res: map[int]*regexp.Regexp{}}
	if e.rng == nil {
		seed := uint64(p.Process.Seed)
		e.rng = rand.New(rand.NewPCG(seed, seed^0x5348454c4c53484f))
	}
	for i, op := range p.Ops {
		switch op := op.(type) {
		case OpType, OpPress, OpSleep, OpLinger, OpMark, OpSettle, OpExit, OpSpanBegin, OpSpanEnd:
		case OpWait:
			switch op.Kind {
			case WaitLiteral, WaitIdle:
			case WaitRegex:
				re, err := regexp.Compile("(?m)" + op.Pattern)
				if err != nil {
					return nil, fmt.Errorf("run: %v: wait pattern: %w", op.Pos, err)
				}
				e.res[i] = re
			default:
				return nil, fmt.Errorf("run: %v: unknown wait kind %d", op.Pos, op.Kind)
			}
		default:
			return nil, fmt.Errorf("run: op %d: unknown op %T", i, op)
		}
	}
	return e, nil
}

// ChildExited tells the Executor that the child ended with status at now. A
// later type or press fails the script; a wait is tested once more against the
// final screen and then fails; sleep, mark, spans, settle and the exit
// sequence carry on. Only the first call counts.
func (e *Executor) ChildExited(now time.Duration, status int) {
	if e.exited {
		return
	}
	e.exited, e.exitStatus = true, status
	e.last = max(e.last, now)
}

// OpenSpans returns the SPAN end events of the spans that are open, innermost
// first. After a failure the host records them before the EXIT event.
func (e *Executor) OpenSpans() []SpanOp {
	out := make([]SpanOp, 0, len(e.open))
	for i := len(e.open) - 1; i >= 0; i-- {
		out = append(out, SpanOp{Event: event.Span{Op: event.SpanEnd, ID: e.open[i]}})
	}
	return out
}

// Poll advances the script at time now (which must not go backwards; an
// earlier value is treated as the latest time seen) with the current screen.
// It returns the one thing to do now, or the time to call again. After an
// error every Poll returns the same error; after Done every Poll returns
// Done.
func (e *Executor) Poll(now time.Duration, scr Screen) (Step, error) {
	now = max(now, e.last)
	e.last = now
	if e.err != nil {
		return Step{Deadline: now}, e.err
	}
	text := ""
	if scr != nil {
		text = scr.Text()
	}
	e.screen.observe(now, text)
	for {
		if e.pc >= len(e.ops) {
			e.done = true
			return Step{Done: true, Deadline: now}, nil
		}
		if !e.entered {
			e.cur = opState{start: now}
			e.entered = true
		}
		st, finished, err := e.advance(now)
		if err != nil {
			e.err = err
			return Step{Deadline: now}, err
		}
		if finished {
			e.pc++
			e.entered = false
			st.Deadline = now
			st.Done = e.pc >= len(e.ops)
		}
		if !finished || st.Write != nil || st.Mark != "" || st.Span != nil || st.Shutdown || st.Done {
			if st.Done {
				e.done = true
			}
			return st, nil
		}
	}
}

// advance runs the current op at now. It returns the Step to hand out, and
// whether the op is finished; an unfinished op sets the Deadline.
func (e *Executor) advance(now time.Duration) (Step, bool, *ScriptError) {
	op := e.ops[e.pc]
	switch op := op.(type) {
	case OpType:
		return e.advanceType(op, now)
	case OpPress:
		return e.advancePress(op, now)
	case OpWait:
		return e.advanceWait(op, now)
	case OpSettle:
		return e.advanceSettle(op, now)
	case OpSleep:
		return e.pause(op.Duration, now)
	case OpLinger:
		return e.pause(op.Duration, now)
	case OpMark:
		return Step{Mark: op.Label}, true, nil
	case OpSpanBegin:
		e.open = append(e.open, op.ID)
		ev := op.Event()
		return Step{Span: &SpanOp{Event: ev}}, true, nil
	case OpSpanEnd:
		if n := len(e.open); n > 0 && e.open[n-1] == op.ID {
			e.open = e.open[:n-1]
		} else {
			return Step{}, false, e.fail(op.Meta, Internal, "span %d ends but is not the innermost open span", op.ID)
		}
		ev := op.Event()
		return Step{Span: &SpanOp{Event: ev}}, true, nil
	case OpExit:
		return e.advanceExit(op, now)
	}
	return Step{}, false, e.fail(e.ops[e.pc].OpMeta(), Internal, "unknown op %T", op)
}

// pause waits d from the start of the op (sleep and linger).
func (e *Executor) pause(d, now time.Duration) (Step, bool, *ScriptError) {
	if until := e.cur.start + d; now < until {
		return Step{Deadline: until}, false, nil
	}
	return Step{}, true, nil
}

// advanceExit shuts the child down unless it is gone, then waits for its exit
// for the grace time. The op uses cur.flag for "Shutdown was handed out".
func (e *Executor) advanceExit(op OpExit, now time.Duration) (Step, bool, *ScriptError) {
	if e.exited {
		return Step{}, true, nil
	}
	if !e.cur.flag {
		e.cur.flag = true
		return Step{Shutdown: true, Deadline: e.cur.start + op.Grace}, false, nil
	}
	if until := e.cur.start + op.Grace; now < until {
		return Step{Deadline: until}, false, nil
	}
	return Step{}, true, nil
}

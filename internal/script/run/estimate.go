package run

import (
	"math"
	"time"
	"unicode/utf8"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/script"
)

// Estimate is the expected real-time length of a Plan, from the plan alone.
type Estimate struct {
	// Min is the time the plan takes when every wait matches at once: typing,
	// presses and sleeps, lingers, and the quiet time of each settle. Typing
	// delays use the low end of the jitter range.
	Min time.Duration
	// Max is the time when every wait and settle runs to its timeout (a
	// wait or settle that reaches it fails the script, so this is an upper
	// bound), and the exit sequence uses its full grace. Typing delays use the
	// high end of the jitter range.
	Max time.Duration
	// Waits counts the OpWait ops that can block: every wait except an
	// assertion (a pattern with timeout 0s).
	Waits int
	// Scenes lists the scenes in begin order with their own estimates. Time
	// in a hidden block inside a scene counts in that scene.
	Scenes []SceneEstimate
}

// SceneEstimate is the estimate of one scene span.
type SceneEstimate struct {
	ID       uint64
	Name     string
	Min, Max time.Duration
}

// Estimate computes the expected length of p. It is a pure function of the
// plan. (A package-level Estimate function cannot share its name with the
// type, so the entry point is a method.)
func (p *Plan) Estimate() Estimate {
	var e Estimate
	scene := -1 // index in e.Scenes of the open scene, or -1
	for _, op := range p.Ops {
		if b, ok := op.(OpSpanBegin); ok && b.Type == event.SpanScene {
			e.Scenes = append(e.Scenes, SceneEstimate{ID: b.ID, Name: b.Name})
			scene = len(e.Scenes) - 1
			continue
		}
		if end, ok := op.(OpSpanEnd); ok && scene >= 0 && e.Scenes[scene].ID == end.ID {
			scene = -1
			continue
		}
		lo, hi := opTime(op)
		e.Min += lo
		e.Max += hi
		if scene >= 0 {
			e.Scenes[scene].Min += lo
			e.Scenes[scene].Max += hi
		}
		if w, ok := op.(OpWait); ok && !w.Assert {
			e.Waits++
		}
	}
	return e
}

// opTime is the shortest and longest time op takes.
func opTime(op Op) (lo, hi time.Duration) {
	switch op := op.(type) {
	case OpType:
		n := utf8.RuneCountInString(op.Text)
		return typingTime(max(n-1, 0), op.PerChar, op.Jitter)
	case OpPress:
		n := op.Repeat - 1
		if op.Gap {
			n++
		}
		return typingTime(n, op.Eff.Typing, op.Eff.Jitter)
	case OpWait:
		switch {
		case op.Assert:
			return 0, 0
		case op.Kind == WaitIdle:
			return op.Quiet, max(op.Timeout, op.Quiet)
		case op.Kind == WaitDone:
			return script.DoneDebounce, max(op.Timeout, script.DoneDebounce)
		}
		return 0, op.Timeout
	case OpSettle:
		if op.Off {
			return 0, 0
		}
		return op.Quiet, max(op.Timeout, op.Quiet)
	case OpSleep:
		return op.Duration, op.Duration
	case OpLinger:
		return op.Duration, op.Duration
	case OpExit:
		return 0, op.Grace
	}
	return 0, 0
}

// typingTime is the time of n delays of d, each drawn from d*(1±jitter%).
func typingTime(n int, d time.Duration, jitter float64) (lo, hi time.Duration) {
	total := float64(d) * float64(n)
	lo = time.Duration(math.Round(total * (1 - jitter/100)))
	hi = time.Duration(math.Round(total * (1 + jitter/100)))
	return lo, hi
}

package run

// Progress is how far a script run has come, as seen by Deps.OnProgress.
type Progress struct {
	// Done is the number of steps completed, 0 to Total.
	Done int
	// Total is the number of steps of the plan (see Plan.Steps).
	Total int
	// Scope names where the current op sits: "NAME" for a scene, "hidden"
	// for a hidden block, "NAME/hidden" for a hidden block in a scene, and
	// "" outside any block and in the implicit exit sequence.
	Scope string
}

// Steps returns the number of steps the plan plays, which is the Total of
// Progress. A step is one step line or block of the script: a run, type,
// press, wait, sleep or mark, whatever number of ops it was lowered to (a
// run is a type, a press, a settle and maybe a linger). Scene and hidden
// blocks are not steps, but the steps inside them are. The implicit exit
// sequence at the end of every plan is not counted, so a plan whose script is
// empty has no steps.
func (p *Plan) Steps() int {
	done, _ := stepsDone(p.Ops)
	return done[len(p.Ops)]
}

// stepsDone returns, for each op index i, the number of steps completed while
// ops[i] is current; the entry for len(ops) is the total. A step is a run of
// consecutive ops with the same source position, apart from the span ops and
// the ops of the exit sequence, which have no step.
func stepsDone(ops []Op) (done []int, total int) {
	done = make([]int, len(ops)+1)
	begun := 0
	inStep := false
	var cur Meta
	for i, op := range ops {
		m := op.OpMeta()
		if !isStepOp(op, m) {
			inStep = false
			done[i] = begun
			continue
		}
		if !inStep || m.Pos != cur.Pos {
			begun++
			inStep, cur = true, m
		}
		done[i] = begun - 1
	}
	done[len(ops)] = begun
	return done, begun
}

func isStepOp(op Op, m Meta) bool {
	switch op.(type) {
	case OpSpanBegin, OpSpanEnd:
		return false
	}
	return m.Pos.Line != 0
}

// progressTracker tells an observer, once per change, where the executor is.
// A nil tracker does nothing.
type progressTracker struct {
	fn     func(Progress)
	done   []int
	total  int
	scopes []string // scope of each op, "" for the ops of no step
	last   Progress
	sent   bool
}

// newProgressTracker returns a tracker for ops that calls fn, or nil when fn
// is nil.
func newProgressTracker(ops []Op, fn func(Progress)) *progressTracker {
	if fn == nil {
		return nil
	}
	done, total := stepsDone(ops)
	scopes := make([]string, len(ops))
	for i, op := range ops {
		if m := op.OpMeta(); m.Pos.Line != 0 {
			scopes[i] = scopeName(m)
		}
	}
	return &progressTracker{fn: fn, done: done, total: total, scopes: scopes}
}

// at reports the position of the op at index pc (len(ops) after the last
// one) if it differs from the one reported last.
func (t *progressTracker) at(pc int) {
	if t == nil {
		return
	}
	p := Progress{Done: t.done[pc], Total: t.total}
	if pc < len(t.scopes) {
		p.Scope = t.scopes[pc]
	}
	if t.sent && p == t.last {
		return
	}
	t.last, t.sent = p, true
	t.fn(p)
}

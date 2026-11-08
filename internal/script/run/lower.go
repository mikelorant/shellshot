package run

import (
	"errors"
	"fmt"
	"strings"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/script"
	"github.com/mikelorant/shellshot/internal/script/keys"
)

// Lower converts a validated script into a Plan. It resolves every scope: each
// op carries the settings in effect at its position, SPAN begin and end are
// explicit ops, and the implicit hidden span of the end-of-script exit
// sequence is always appended. Lower does not read the environment, so equal
// scripts give equal plans.
//
// The script must have passed the validator. Lower still returns an error,
// naming the position, for a value it cannot read or a structure it cannot
// lower, rather than guess.
func Lower(s *script.Script) (*Plan, error) {
	if s == nil {
		return nil, errors.New("run: nil script")
	}
	l := &lowerer{file: s.File, plan: &Plan{Process: Process{Seed: DefaultSeed}}}
	for _, st := range s.Settings {
		switch {
		case isHint(st.Name):
			if err := applyHint(&l.preamble, st); err != nil {
				return nil, l.errorf(st.Pos, "%v", err)
			}
		case isBehaviour(st.Name):
		default:
			if err := applyProcess(&l.plan.Process, st); err != nil {
				return nil, l.errorf(st.Pos, "%v", err)
			}
		}
	}
	sc := defaultSettings()
	if err := l.items(s.Settings, s.Items, &sc, span{}); err != nil {
		return nil, err
	}
	l.exitSpan(sc)
	return l.plan, nil
}

// span is where the lowerer is: the enclosing scene, if any, and whether it
// is inside a hidden block.
type span struct {
	scene   string
	sceneID uint64
	hidden  bool
}

type lowerer struct {
	file     string
	plan     *Plan
	preamble event.SpanHints // script-wide hint defaults
	lastID   uint64
}

func (l *lowerer) errorf(pos script.Pos, format string, a ...any) error {
	return fmt.Errorf("%s:%v: %s", l.file, pos, fmt.Sprintf(format, a...))
}

func (l *lowerer) emit(op Op) { l.plan.Ops = append(l.plan.Ops, op) }

func (l *lowerer) meta(pos script.Pos, in span, sc Settings) Meta {
	return Meta{Pos: pos, Scene: in.scene, Hidden: in.hidden, Eff: sc}
}

// items lowers one list of settings and items with the settings in sc, which
// it changes as the settings of the list take effect. A setting applies before
// the item at its At index.
func (l *lowerer) items(settings []script.Setting, items []script.Item, sc *Settings, in span) error {
	next := 0
	applyUpTo := func(at int) error {
		for ; next < len(settings) && settings[next].At <= at; next++ {
			if err := l.setting(settings[next], sc, in); err != nil {
				return err
			}
		}
		return nil
	}
	for i, it := range items {
		if err := applyUpTo(i); err != nil {
			return err
		}
		var err error
		switch it := it.(type) {
		case script.Step:
			err = l.step(it, *sc, in)
		case script.Block:
			err = l.block(it, *sc, in)
		default:
			err = l.errorf(it.ItemPos(), "unknown item %T", it)
		}
		if err != nil {
			return err
		}
	}
	return applyUpTo(len(items))
}

// setting applies one behaviour setting; the other classes are handled by the
// caller that owns them, and are only checked for legality here.
func (l *lowerer) setting(st script.Setting, sc *Settings, in span) error {
	switch {
	case isBehaviour(st.Name):
		if err := sc.apply(st); err != nil {
			return l.errorf(st.Pos, "%v", err)
		}
	case isHint(st.Name):
		if in.hidden {
			return l.errorf(st.Pos, "%q is not allowed in a hidden block", st.Name)
		}
	default:
		if in.scene != "" || in.hidden {
			return l.errorf(st.Pos, "%q is not allowed inside a block", st.Name)
		}
	}
	return nil
}

func (l *lowerer) block(b script.Block, outer Settings, in span) error {
	if in.hidden || in.scene != "" && b.Kind != script.BlockHidden {
		return l.errorf(b.Pos, "%s cannot be nested here", b.Kind)
	}
	l.lastID++
	id := l.lastID
	begin := OpSpanBegin{ID: id}
	inner := in
	switch b.Kind {
	case script.BlockScene:
		begin.Type, begin.Name = event.SpanScene, b.Name
		begin.Hints = l.preamble
		for _, st := range b.Settings {
			if isHint(st.Name) {
				if err := applyHint(&begin.Hints, st); err != nil {
					return l.errorf(st.Pos, "%v", err)
				}
			}
		}
		inner.scene, inner.sceneID = b.Name, id
	case script.BlockHidden:
		begin.Type, begin.Name = event.SpanHidden, b.Name
		begin.Parent = in.sceneID
		inner.hidden = true
	default:
		return l.errorf(b.Pos, "unknown block %q", b.Kind)
	}
	begin.Meta = l.meta(b.Pos, inner, outer)
	if err := begin.Event().Validate(); err != nil {
		return l.errorf(b.Pos, "%v", err)
	}
	l.emit(begin)

	sc := outer
	if err := l.items(b.Settings, b.Items, &sc, inner); err != nil {
		return err
	}
	l.emit(OpSpanEnd{Meta: l.meta(b.End, inner, sc), ID: id})
	return nil
}

func (l *lowerer) step(st script.Step, sc Settings, in span) error {
	m := l.meta(st.Pos, in, sc)
	switch st.Kind {
	case script.StepRun:
		text := st.Text
		if st.Form == script.FormLines || st.Form == script.FormParagraphs {
			// A run block's text already ends with the final Enter; the
			// plan presses it as its own op.
			text = strings.TrimSuffix(text, "\n")
		}
		l.emit(OpType{Meta: m, Text: text, PerChar: sc.Typing, Jitter: sc.Jitter})
		l.emit(OpPress{Meta: m, Chord: keys.Chord{Key: keys.KeyEnter}, Repeat: 1, Gap: text != ""})
		quiet := sc.Settle
		if sc.SettleOff {
			quiet = 0
		}
		l.emit(OpSettle{Meta: m, Quiet: quiet, Timeout: sc.Timeout, Off: sc.SettleOff})
		l.linger(m)
	case script.StepType:
		l.emit(OpType{Meta: m, Text: st.Text, PerChar: sc.Typing, Jitter: sc.Jitter})
		l.linger(m)
	case script.StepPress:
		if len(st.Keys) == 0 {
			return l.errorf(st.Pos, "press without keys")
		}
		for i, k := range st.Keys {
			l.emit(OpPress{Meta: m, Chord: k.Chord, Repeat: max(k.Count, 1), Gap: i > 0})
		}
		l.linger(m)
	case script.StepWait:
		op := OpWait{Meta: m, Timeout: sc.Timeout}
		switch {
		case st.Idle:
			op.Kind = WaitIdle
			op.Quiet = sc.quiet()
			if st.HasDuration && st.Duration > 0 {
				op.Quiet = st.Duration
			}
		case st.Done:
			op.Kind = WaitDone
			if st.HasDuration {
				op.Timeout = st.Duration
			}
		case st.Pattern != nil:
			op.Kind, op.Pattern = WaitLiteral, st.Pattern.Source
			if st.Pattern.Regex {
				op.Kind = WaitRegex
			}
			if st.HasDuration {
				op.Timeout = st.Duration
				op.Assert = st.Duration == 0
			}
		default:
			return l.errorf(st.Pos, "wait without a pattern")
		}
		l.emit(op)
		l.linger(m)
	case script.StepSleep:
		l.emit(OpSleep{Meta: m, Duration: st.Duration})
	case script.StepMark:
		l.emit(OpMark{Meta: m, Label: st.Label})
	default:
		return l.errorf(st.Pos, "unknown step %q", st.Kind)
	}
	return nil
}

// linger emits the pause that follows a step, if the linger setting asks for one.
func (l *lowerer) linger(m Meta) {
	if m.Eff.Linger > 0 {
		l.emit(OpLinger{Meta: m, Duration: m.Eff.Linger})
	}
}

// exitSpan appends the implicit hidden span of the exit sequence
// (docs/research/script-format.md §7.4): a final drain that never fails, the
// shutdown with its grace time, and the span end. sc is the setting state at
// the end of the script.
func (l *lowerer) exitSpan(sc Settings) {
	l.lastID++
	id := l.lastID
	m := Meta{Hidden: true, Eff: sc}
	l.emit(OpSpanBegin{Meta: m, ID: id, Type: event.SpanHidden})
	l.emit(OpSettle{Meta: m, Quiet: sc.quiet(), Timeout: DrainTimeout, Drain: true})
	l.emit(OpExit{Meta: m, Grace: ExitGrace})
	l.emit(OpSpanEnd{Meta: m, ID: id})
}

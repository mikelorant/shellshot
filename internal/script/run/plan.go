// Package run turns a validated script AST into a Plan: a flat list of ops
// with every setting scope already resolved. The package is pure Go (no cgo,
// no pty, no cobra) so that the plan can be built, listed and estimated
// before anything is spawned. The design is docs/research/script-format.md §8.
package run

import (
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/script"
	"github.com/mikelorant/shellshot/internal/script/keys"
)

// Defaults of the behaviour settings and the fixed times of the end-of-script
// exit sequence (docs/research/script-format.md §3.2 and §7.4).
const (
	DefaultTyping  = 50 * time.Millisecond
	DefaultTimeout = 15 * time.Second
	DefaultSettle  = 200 * time.Millisecond
	DefaultSeed    = 1

	// DrainTimeout caps the final drain of the exit sequence.
	DrainTimeout = 2 * time.Second
	// ExitGrace is how long the exit sequence waits for the child to exit
	// before it kills the child.
	ExitGrace = 2 * time.Second
)

// Plan is a script lowered for execution. It is a value: two lowerings of the
// same AST are reflect.DeepEqual.
type Plan struct {
	Process Process
	// Ops are the steps in execution order. They always end with the exit
	// sequence: a final drain and the exit, outside any span.
	Ops []Op
}

// Process is the process class of settings (docs/research/script-format.md
// §3.2). A zero or nil field means the script did not set it; the caller
// applies the defaults that need the environment (the terminal size, $SHELL,
// the CLI flags), so lowering stays free of the environment.
type Process struct {
	Cols, Rows int      // 0 when no "size" was given
	Shell      []string // argv [PATH, ARGS...] of the "shell" setting; nil when none
	Env        []string // "KEY=VALUE" in script order; a later entry for a key wins
	Cwd        string
	Output     string
	Seed       int64 // DefaultSeed when no "seed" was given
}

// Settings are the behaviour settings in effect at an op.
type Settings struct {
	Typing    time.Duration // delay between typed units
	Jitter    float64       // percent, 0 to 100
	Timeout   time.Duration
	Settle    time.Duration // quiet time; meaningful only when SettleOff is false
	SettleOff bool
	Linger    time.Duration
}

// Meta is what every op knows about where it came from.
type Meta struct {
	Pos script.Pos // the source position of the step (or block); zero for the exit sequence
	// Scene is the name of the enclosing scene, or "".
	Scene string
	// Hidden is true inside a hidden span.
	Hidden bool
	// Eff holds the settings in effect at this op.
	Eff Settings
}

// OpMeta returns m, which makes every op that embeds Meta an Op.
func (m Meta) OpMeta() Meta { return m }

// Op is one executable step of a Plan. The implementations are the Op*
// types of this file, all used as values.
type Op interface {
	OpMeta() Meta
}

// OpType types Text as one stream of units: each rune, and each "\n" as
// Enter, with PerChar between consecutive units (jittered by Jitter percent).
// It is the whole text of one run or type step, even a text block. PerChar
// and Jitter equal Eff.Typing and Eff.Jitter.
type OpType struct {
	Meta
	Text    string
	PerChar time.Duration
	Jitter  float64
}

// OpPress presses Chord Repeat times (at least 1), with Eff.Typing between
// consecutive presses. A "press up up enter" step is three OpPress ops with
// no OpLinger between them.
type OpPress struct {
	Meta
	Chord  keys.Chord
	Repeat int
	// Gap is true when a typing delay comes before the first press of this
	// op: it follows another unit of the same step (the earlier chord of a
	// press step, or the text of a run).
	Gap bool
}

// WaitKind says what an OpWait waits for.
type WaitKind uint8

const (
	WaitLiteral WaitKind = iota + 1 // the screen contains Pattern
	WaitRegex                       // the screen matches Pattern (RE2, multiline)
	WaitIdle                        // the screen has had no output for Quiet
	WaitDone                        // the shell is the terminal's foreground process group, steadily for script.DoneDebounce
)

// OpWait blocks until the screen matches. A wait that does not match within
// Timeout fails the script. With Assert the screen is checked once and the
// script fails at once if it does not match ("wait "x" 0s"); Timeout is 0.
// A WaitDone is never an assertion and its Timeout is at least
// script.DoneDebounce.
type OpWait struct {
	Meta
	Kind    WaitKind
	Pattern string        // the substring, or the regex without slashes; empty for WaitIdle and WaitDone
	Quiet   time.Duration // WaitIdle only
	Timeout time.Duration
	Assert  bool
}

// OpSettle waits until the screen has had no output for Quiet. Normally it
// fails the script if that does not happen within Timeout, counted from the
// start of the op. With Off there is no wait at all (and nothing can fail).
// With Drain it is the final drain of the exit sequence, which carries on when
// Timeout is reached.
type OpSettle struct {
	Meta
	Quiet   time.Duration
	Timeout time.Duration
	Off     bool
	Drain   bool
}

// OpSleep pauses.
type OpSleep struct {
	Meta
	Duration time.Duration
}

// OpLinger pauses after a run, type, press or wait step so the viewer can
// read the screen. It is emitted only when the linger setting is above zero,
// once per step.
type OpLinger struct {
	Meta
	Duration time.Duration
}

// OpMark writes a MARK event.
type OpMark struct {
	Meta
	Label string
}

// OpSpanBegin writes the SPAN begin event of a scene or hidden block. IDs
// start at 1 in begin order; Parent is the ID of the enclosing scene or 0.
// Hints are resolved (inheritance applied); a hidden span has none.
type OpSpanBegin struct {
	Meta
	ID     uint64
	Type   event.SpanType // event.SpanScene or event.SpanHidden
	Name   string         // scene name; optional for hidden
	Parent uint64
	Hints  event.SpanHints
}

// Event returns the SPAN event value this op writes.
func (o OpSpanBegin) Event() event.Span {
	return event.Span{Op: event.SpanBegin, ID: o.ID, Type: o.Type, Parent: o.Parent, Name: o.Name, Hints: o.Hints}
}

// OpSpanEnd writes the SPAN end event of the span with the given ID.
type OpSpanEnd struct {
	Meta
	ID uint64
}

// Event returns the SPAN event value this op writes.
func (o OpSpanEnd) Event() event.Span {
	return event.Span{Op: event.SpanEnd, ID: o.ID}
}

// OpExit ends the recording of the terminal and shuts the child down
// (docs/research/script-format.md §7.4). If the child still runs, a shell gets
// EOF (Ctrl-D) and any other program gets its PTY closed; after Grace the
// child is killed. A child that had to be closed is noted, not a failure.
// Nothing the child prints from the start of the op on is recorded, except
// its exit status.
type OpExit struct {
	Meta
	Grace time.Duration
}

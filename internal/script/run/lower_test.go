package run

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/script"
	"github.com/michaellorant/shellshot/internal/script/keys"
)

// lowerSrc parses and lowers src; a parse diagnostic or lowering error fails
// the test.
func lowerSrc(t testing.TB, src string) *Plan {
	t.Helper()
	s, diags := script.ParseString("t.script", src)
	if len(diags) > 0 {
		t.Fatalf("parse: %v", diags)
	}
	p, err := Lower(s)
	if err != nil {
		t.Fatalf("Lower: %v", err)
	}
	return p
}

// kinds names the ops in order, e.g. "type press settle".
func kinds(ops []Op) string {
	var names []string
	for _, op := range ops {
		switch op.(type) {
		case OpType:
			names = append(names, "type")
		case OpPress:
			names = append(names, "press")
		case OpWait:
			names = append(names, "wait")
		case OpSettle:
			names = append(names, "settle")
		case OpSleep:
			names = append(names, "sleep")
		case OpLinger:
			names = append(names, "linger")
		case OpMark:
			names = append(names, "mark")
		case OpSpanBegin:
			names = append(names, "begin")
		case OpSpanEnd:
			names = append(names, "end")
		case OpExit:
			names = append(names, "exit")
		default:
			names = append(names, "?")
		}
	}
	return strings.Join(names, " ")
}

const exitKinds = "begin settle exit end"

// body returns the ops before the implicit exit span, and fails if the plan
// does not end with it.
func body(t *testing.T, p *Plan) []Op {
	t.Helper()
	n := len(p.Ops)
	if n < 4 || kinds(p.Ops[n-4:]) != exitKinds {
		t.Fatalf("plan does not end with the exit span: %s", kinds(p.Ops))
	}
	return p.Ops[:n-4]
}

func TestRunIsTypeEnterSettle(t *testing.T) {
	p := lowerSrc(t, "run echo hi\n")
	ops := body(t, p)
	if got := kinds(ops); got != "type press settle" {
		t.Fatalf("ops = %s", got)
	}
	ty := ops[0].(OpType)
	if ty.Text != "echo hi" || ty.PerChar != 50*time.Millisecond || ty.Jitter != 0 {
		t.Errorf("type = %+v", ty)
	}
	enter := ops[1].(OpPress)
	if enter.Chord != (keys.Chord{Key: keys.KeyEnter}) || enter.Repeat != 1 || !enter.Gap {
		t.Errorf("enter = %+v", enter)
	}
	st := ops[2].(OpSettle)
	if st.Quiet != 200*time.Millisecond || st.Timeout != 15*time.Second || st.Off || st.Drain {
		t.Errorf("settle = %+v", st)
	}
	for i, op := range ops {
		if m := op.OpMeta(); m.Pos.Line != 1 || m.Pos.Col != 1 || m.Scene != "" || m.Hidden {
			t.Errorf("op %d meta = %+v", i, m)
		}
	}
}

func TestRunSettleOff(t *testing.T) {
	p := lowerSrc(t, "settle off\ntimeout 5s\nrun make\n")
	st := body(t, p)[2].(OpSettle)
	if !st.Off || st.Quiet != 0 || st.Timeout != 5*time.Second {
		t.Errorf("settle = %+v", st)
	}
}

func TestRunBlockPressesEnterOnce(t *testing.T) {
	p := lowerSrc(t, "run lines {\n  a\n  b\n}\nrun paragraphs {\n  one\n  two\n}\n")
	ops := body(t, p)
	if got := kinds(ops); got != "type press settle type press settle" {
		t.Fatalf("ops = %s", got)
	}
	if got := ops[0].(OpType).Text; got != "a\nb" {
		t.Errorf("lines text = %q", got)
	}
	if got := ops[3].(OpType).Text; got != "one two" {
		t.Errorf("paragraphs text = %q", got)
	}
}

func TestRunQuotedKeepsItsEnters(t *testing.T) {
	p := lowerSrc(t, "run \"a\\n\"\n")
	ops := body(t, p)
	if got := ops[0].(OpType).Text; got != "a\n" {
		t.Errorf("text = %q, want the quoted text unchanged", got)
	}
}

func TestTextBlockIsOneTypeOpAndOneLinger(t *testing.T) {
	src := "linger 1s\ntype lines {\n  a\n  b\n  c\n}\ntype paragraphs {\n  x\n  y\n\n  z\n}\n"
	ops := body(t, lowerSrc(t, src))
	if got := kinds(ops); got != "type linger type linger" {
		t.Fatalf("ops = %s", got)
	}
	if got := ops[0].(OpType).Text; got != "a\nb\nc" {
		t.Errorf("lines text = %q", got)
	}
	if got := ops[2].(OpType).Text; got != "x y\n\nz" {
		t.Errorf("paragraphs text = %q", got)
	}
}

func TestLingerRules(t *testing.T) {
	src := `linger 1s
run a
type b
press enter up
wait "x"
wait idle
sleep 1s
mark m
`
	got := kinds(body(t, lowerSrc(t, src)))
	want := "type press settle linger " + // run
		"type linger " + // type
		"press press linger " + // press: once per step
		"wait linger wait linger " + // wait, wait idle
		"sleep mark" // no linger
	if got != want {
		t.Errorf("ops\n got %s\nwant %s", got, want)
	}
}

func TestLingerZeroEmitsNothing(t *testing.T) {
	if got := kinds(body(t, lowerSrc(t, "type a\n"))); got != "type" {
		t.Errorf("ops = %s", got)
	}
}

func TestLingerOpCarriesDuration(t *testing.T) {
	ops := body(t, lowerSrc(t, "linger 800ms\ntype a\n"))
	if l := ops[1].(OpLinger); l.Duration != 800*time.Millisecond {
		t.Errorf("linger = %+v", l)
	}
}

func TestScopedSettingsRevertAtBrace(t *testing.T) {
	src := `typing 40ms
linger 1s
jitter 10%
scene a {
  typing 70ms
  linger 2s
  jitter 30%
  type x
}
type y
scene b {
  type z
  typing 5ms
  type w
}
type v
`
	ops := body(t, lowerSrc(t, src))
	var typed []OpType
	var lingers []time.Duration
	for _, op := range ops {
		switch op := op.(type) {
		case OpType:
			typed = append(typed, op)
		case OpLinger:
			lingers = append(lingers, op.Duration)
		}
	}
	wantTyped := []struct {
		text   string
		typing time.Duration
		jitter float64
	}{
		{"x", 70 * time.Millisecond, 30},
		{"y", 40 * time.Millisecond, 10},
		{"z", 40 * time.Millisecond, 10},
		{"w", 5 * time.Millisecond, 10},
		{"v", 40 * time.Millisecond, 10},
	}
	if len(typed) != len(wantTyped) {
		t.Fatalf("got %d type ops, want %d", len(typed), len(wantTyped))
	}
	for i, w := range wantTyped {
		g := typed[i]
		if g.Text != w.text || g.PerChar != w.typing || g.Jitter != w.jitter ||
			g.Eff.Typing != w.typing || g.Eff.Jitter != w.jitter {
			t.Errorf("type %q = %+v (eff %+v), want typing %v jitter %v", w.text, g, g.Eff, w.typing, w.jitter)
		}
	}
	wantLingers := []time.Duration{2 * time.Second, time.Second, time.Second, time.Second, time.Second}
	if !reflect.DeepEqual(lingers, wantLingers) {
		t.Errorf("lingers = %v, want %v", lingers, wantLingers)
	}
}

func TestSettingTakesEffectFromItsLine(t *testing.T) {
	ops := body(t, lowerSrc(t, "type a\ntyping 10ms\ntype b\ntyping 20ms\ntype c\n"))
	var got []time.Duration
	for _, op := range ops {
		got = append(got, op.(OpType).PerChar)
	}
	want := []time.Duration{50 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("typing = %v, want %v", got, want)
	}
}

func TestSettingMidBlockAppliesToLaterStepsOnly(t *testing.T) {
	ops := body(t, lowerSrc(t, "scene a {\n  type a\n  timeout 3s\n  wait \"x\"\n}\nwait \"y\"\n"))
	var timeouts []time.Duration
	for _, op := range ops {
		if w, ok := op.(OpWait); ok {
			timeouts = append(timeouts, w.Timeout)
		}
	}
	want := []time.Duration{3 * time.Second, 15 * time.Second}
	if !reflect.DeepEqual(timeouts, want) {
		t.Errorf("timeouts = %v, want %v", timeouts, want)
	}
}

func TestSettleAndTimeoutScope(t *testing.T) {
	ops := body(t, lowerSrc(t, "settle 300ms\nscene a {\n  settle off\n  timeout 1s\n  run x\n}\nrun y\n"))
	var settles []OpSettle
	for _, op := range ops {
		if s, ok := op.(OpSettle); ok {
			settles = append(settles, s)
		}
	}
	if len(settles) != 2 {
		t.Fatalf("settles = %v", settles)
	}
	if !settles[0].Off || settles[0].Timeout != time.Second {
		t.Errorf("scene settle = %+v", settles[0])
	}
	if settles[1].Off || settles[1].Quiet != 300*time.Millisecond || settles[1].Timeout != 15*time.Second {
		t.Errorf("top-level settle = %+v", settles[1])
	}
}

func TestWaitVariants(t *testing.T) {
	src := `timeout 7s
settle 300ms
wait "lit"
wait /re+/ 2s
wait "now" 0s
wait /now/ 0s
wait idle
wait idle 1s
settle off
wait idle
`
	var waits []OpWait
	for _, op := range body(t, lowerSrc(t, src)) {
		if w, ok := op.(OpWait); ok {
			waits = append(waits, w)
		}
	}
	want := []OpWait{
		{Kind: WaitLiteral, Pattern: "lit", Timeout: 7 * time.Second},
		{Kind: WaitRegex, Pattern: "re+", Timeout: 2 * time.Second},
		{Kind: WaitLiteral, Pattern: "now", Assert: true},
		{Kind: WaitRegex, Pattern: "now", Assert: true},
		{Kind: WaitIdle, Quiet: 300 * time.Millisecond, Timeout: 7 * time.Second},
		{Kind: WaitIdle, Quiet: time.Second, Timeout: 7 * time.Second},
		{Kind: WaitIdle, Quiet: 200 * time.Millisecond, Timeout: 7 * time.Second},
	}
	if len(waits) != len(want) {
		t.Fatalf("got %d waits, want %d", len(waits), len(want))
	}
	for i := range waits {
		waits[i].Meta = Meta{}
		if waits[i] != want[i] {
			t.Errorf("wait %d = %+v, want %+v", i, waits[i], want[i])
		}
	}
}

func TestPressRepeatsAndGaps(t *testing.T) {
	ops := body(t, lowerSrc(t, "press ctrl+c\npress up up enter\npress down x3\n"))
	var got []OpPress
	for _, op := range ops {
		p := op.(OpPress)
		p.Meta = Meta{}
		got = append(got, p)
	}
	up := keys.Chord{Key: keys.KeyUp}
	enter := keys.Chord{Key: keys.KeyEnter}
	want := []OpPress{
		{Chord: keys.Chord{Key: keys.KeyC, Mods: keys.ModCtrl}, Repeat: 1},
		{Chord: up, Repeat: 1},
		{Chord: up, Repeat: 1, Gap: true},
		{Chord: enter, Repeat: 1, Gap: true},
		{Chord: keys.Chord{Key: keys.KeyDown}, Repeat: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("presses\n got %+v\nwant %+v", got, want)
	}
}

func TestSleepAndMark(t *testing.T) {
	ops := body(t, lowerSrc(t, "sleep 1500ms\nmark done\n"))
	if s := ops[0].(OpSleep); s.Duration != 1500*time.Millisecond {
		t.Errorf("sleep = %+v", s)
	}
	if m := ops[1].(OpMark); m.Label != "done" {
		t.Errorf("mark = %+v", m)
	}
}

func TestSpanIDsAndNesting(t *testing.T) {
	src := `hidden {
  run a
}
scene one {
  type b
  hidden {
    type c
  }
  type d
}
scene two {
  type e
}
hidden {
  type f
}
`
	p := lowerSrc(t, src)
	type span struct {
		id, parent uint64
		typ        event.SpanType
		name       string
	}
	var begins []span
	var ends []uint64
	depth, maxDepth := 0, 0
	for _, op := range p.Ops {
		switch op := op.(type) {
		case OpSpanBegin:
			begins = append(begins, span{op.ID, op.Parent, op.Type, op.Name})
			depth++
			maxDepth = max(maxDepth, depth)
		case OpSpanEnd:
			ends = append(ends, op.ID)
			depth--
		}
	}
	want := []span{
		{1, 0, event.SpanHidden, ""},
		{2, 0, event.SpanScene, "one"},
		{3, 2, event.SpanHidden, ""},
		{4, 0, event.SpanScene, "two"},
		{5, 0, event.SpanHidden, ""},
		{6, 0, event.SpanHidden, ""}, // the implicit exit span
	}
	if !reflect.DeepEqual(begins, want) {
		t.Errorf("begins\n got %+v\nwant %+v", begins, want)
	}
	if want := []uint64{1, 3, 2, 4, 5, 6}; !reflect.DeepEqual(ends, want) {
		t.Errorf("ends = %v, want %v", ends, want)
	}
	if depth != 0 || maxDepth != 2 {
		t.Errorf("depth %d max %d, want 0 and 2", depth, maxDepth)
	}
}

func TestSpanOpsPlacement(t *testing.T) {
	p := lowerSrc(t, "linger 1s\nscene a {\n  type x\n}\n")
	// The last step lingers inside the span.
	want := "begin type linger end " + exitKinds
	if got := kinds(p.Ops); got != want {
		t.Errorf("ops = %s, want %s", got, want)
	}
	begin, end := p.Ops[0].(OpSpanBegin), p.Ops[3].(OpSpanEnd)
	if begin.Pos.Line != 2 || begin.Pos.Col != 1 || end.Pos.Line != 4 || end.Pos.Col != 1 {
		t.Errorf("begin %v end %v", begin.Pos, end.Pos)
	}
}

func TestSceneNameOnOps(t *testing.T) {
	p := lowerSrc(t, "type top\nscene a {\n  type in\n  hidden {\n    type h\n  }\n}\nhidden {\n  type t\n}\n")
	type where struct {
		scene  string
		hidden bool
	}
	got := map[string]where{}
	for _, op := range p.Ops {
		if ty, ok := op.(OpType); ok {
			got[ty.Text] = where{ty.Scene, ty.Hidden}
		}
	}
	want := map[string]where{
		"top": {"", false},
		"in":  {"a", false},
		"h":   {"a", true},
		"t":   {"", true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
	n := len(p.Ops)
	for _, op := range p.Ops[n-4:] {
		if m := op.OpMeta(); m.Scene != "" || !m.Hidden {
			t.Errorf("exit op meta = %+v", m)
		}
	}
}

func TestHintInheritance(t *testing.T) {
	src := `speed 4x
hold 1s
scene plain {
  type a
}
scene own {
  speed 8x
  idle 2s
  hidden {
    type h
  }
  type b
}
hidden {
  type c
}
`
	p := lowerSrc(t, src)
	var begins []OpSpanBegin
	for _, op := range p.Ops {
		if b, ok := op.(OpSpanBegin); ok {
			begins = append(begins, b)
		}
	}
	want := []event.SpanHints{
		{Speed: 4000, Hold: time.Second},                        // plain: preamble
		{Speed: 8000, Idle: 2 * time.Second, Hold: time.Second}, // own: scene overrides, inherits hold
		{}, // hidden in scene: none
		{}, // top-level hidden: none
		{}, // implicit exit span: none
	}
	if len(begins) != len(want) {
		t.Fatalf("got %d spans", len(begins))
	}
	for i, b := range begins {
		if b.Hints != want[i] {
			t.Errorf("span %d (%q) hints = %+v, want %+v", b.ID, b.Name, b.Hints, want[i])
		}
		if err := b.Event().Validate(); err != nil {
			t.Errorf("span %d event invalid: %v", b.ID, err)
		}
	}
}

func TestHintAfterStepsInSceneStillApplies(t *testing.T) {
	p := lowerSrc(t, "scene a {\n  type x\n  speed 2x\n}\n")
	if got := p.Ops[0].(OpSpanBegin).Hints.Speed; got != 2000 {
		t.Errorf("speed = %d, want 2000", got)
	}
}

func TestImplicitExitSpanExactlyOnce(t *testing.T) {
	for _, src := range []string{
		"",
		"# only a comment\n",
		"size 80x24\n",
		"mark a\n",
		"scene a {\n  type x\n}\n",
		"hidden {\n  type x\n}\nhidden {\n  type y\n}\n",
		"linger 1s\nrun x\n",
	} {
		p := lowerSrc(t, src)
		n := len(p.Ops)
		if n < 4 || kinds(p.Ops[n-4:]) != exitKinds {
			t.Errorf("%q: ops = %s", src, kinds(p.Ops))
			continue
		}
		if c := strings.Count(kinds(p.Ops), "exit"); c != 1 {
			t.Errorf("%q: %d exit ops", src, c)
		}
		begin, end := p.Ops[n-4].(OpSpanBegin), p.Ops[n-1].(OpSpanEnd)
		if begin.Type != event.SpanHidden || begin.Name != "" || begin.Parent != 0 || begin.Hints != (event.SpanHints{}) || end.ID != begin.ID {
			t.Errorf("%q: exit span %+v / %+v", src, begin, end)
		}
		if drain := p.Ops[n-3].(OpSettle); !drain.Drain || drain.Timeout != 2*time.Second || drain.Off {
			t.Errorf("%q: drain = %+v", src, drain)
		}
		if exit := p.Ops[n-2].(OpExit); exit.Grace != 2*time.Second {
			t.Errorf("%q: exit = %+v", src, exit)
		}
		if begin.Pos != (script.Pos{}) {
			t.Errorf("%q: implicit span has a source position %v", src, begin.Pos)
		}
	}
}

func TestExitDrainUsesSettleAtEndOfScript(t *testing.T) {
	p := lowerSrc(t, "settle 500ms\nscene a {\n  settle 1s\n  type x\n}\n")
	n := len(p.Ops)
	if got := p.Ops[n-3].(OpSettle).Quiet; got != 500*time.Millisecond {
		t.Errorf("drain quiet = %v, want 500ms", got)
	}
	p = lowerSrc(t, "settle off\ntype x\n")
	n = len(p.Ops)
	if d := p.Ops[n-3].(OpSettle); d.Quiet != 200*time.Millisecond || d.Off {
		t.Errorf("drain with settle off = %+v", d)
	}
}

func TestProcessSettings(t *testing.T) {
	src := `size 100x28
shell /bin/bash --noprofile "--rcfile=/my dir/rc"
env A=1
env PS1="$ "
env A=2
cwd /tmp/x
output out.shot
seed 42
`
	p := lowerSrc(t, src)
	want := Process{
		Cols: 100, Rows: 28,
		Shell:  []string{"/bin/bash", "--noprofile", "--rcfile=/my dir/rc"},
		Env:    []string{"A=1", "PS1=$ ", "A=2"},
		Cwd:    "/tmp/x",
		Output: "out.shot",
		Seed:   42,
	}
	if !reflect.DeepEqual(p.Process, want) {
		t.Errorf("process\n got %+v\nwant %+v", p.Process, want)
	}
}

func TestProcessDefaults(t *testing.T) {
	p := lowerSrc(t, "type a\n")
	if want := (Process{Seed: 1}); !reflect.DeepEqual(p.Process, want) {
		t.Errorf("process = %+v, want %+v", p.Process, want)
	}
}

func TestLowerIsDeterministic(t *testing.T) {
	src := "size 80x24\nseed 3\njitter 25%\nscene a {\n  speed 2x\n  run lines {\n    a\n    b\n  }\n  wait /x/ 1s\n}\nhidden {\n  press up x2\n}\n"
	a, b := lowerSrc(t, src), lowerSrc(t, src)
	if !reflect.DeepEqual(a, b) {
		t.Error("two lowerings of the same script differ")
	}
}

func TestLowerErrors(t *testing.T) {
	if _, err := Lower(nil); err == nil {
		t.Error("Lower(nil) succeeded")
	}
	for name, src := range map[string]string{
		"bad duration":  "typing 5\ntype a\n",
		"bad jitter":    "jitter 200%\ntype a\n",
		"bad speed":     "speed 0x\ntype a\n",
		"huge idle":     "idle 25h\ntype a\n",
		"bad size":      "size 0x5\ntype a\n",
		"bad seed":      "seed -1\ntype a\n",
		"nested scene":  "scene a {\n  scene b {\n    type x\n  }\n}\n",
		"hidden hint":   "hidden {\n  speed 2x\n  type x\n}\n",
		"process block": "scene a {\n  size 1x1\n  type x\n}\n",
	} {
		s, diags := script.ParseString("t.script", src)
		if len(diags) > 0 {
			t.Errorf("%s: parse: %v", name, diags)
			continue
		}
		_, err := Lower(s)
		if err == nil {
			t.Errorf("%s: Lower succeeded", name)
		} else if !strings.HasPrefix(err.Error(), "t.script:") {
			t.Errorf("%s: error %q does not name the position", name, err)
		}
	}
}

package run

import (
	"errors"
	"math/rand/v2"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/script"
	"github.com/mikelorant/shellshot/internal/script/keys"
)

// textScreen is a fixed screen.
type textScreen string

func (s textScreen) Text() string { return string(s) }

func (s textScreen) ShellForeground() (bool, error) {
	return false, errors.New("textScreen has no terminal line")
}

func testMeta(scene string) Meta {
	return Meta{Pos: script.Pos{Line: 3, Col: 5}, Scene: scene}
}

func mustExec(t testing.TB, k Keys, ops ...Op) *Executor {
	t.Helper()
	if k == nil {
		k = fakeKeys{}
	}
	ex, err := New(&Plan{Process: Process{Seed: DefaultSeed}, Ops: ops}, Deps{Keys: k, File: "t.script"})
	if err != nil {
		t.Fatal(err)
	}
	return ex
}

// writes returns the write events as text.
func writes(events []Event) []string {
	var out []string
	for _, e := range events {
		if e.Kind == "write" {
			out = append(out, e.String())
		}
	}
	return out
}

func TestTypingSendsOneUnitPerStepAtItsTime(t *testing.T) {
	ex := mustExec(t, nil, OpType{Meta: testMeta(""), Text: "abc", PerChar: 50 * time.Millisecond})
	want := []struct {
		now      time.Duration
		write    string
		deadline time.Duration
	}{
		{0, "a", 50 * time.Millisecond},
		{50 * time.Millisecond, "b", 100 * time.Millisecond},
		{100 * time.Millisecond, "c", 100 * time.Millisecond},
	}
	for i, w := range want {
		step, err := ex.Poll(w.now, textScreen(""))
		if err != nil {
			t.Fatal(err)
		}
		if string(step.Write) != w.write || step.Deadline != w.deadline {
			t.Errorf("poll %d: got write %q deadline %v, want %q %v", i, step.Write, step.Deadline, w.write, w.deadline)
		}
	}
	step, _ := ex.Poll(100*time.Millisecond, textScreen(""))
	if !step.Done {
		t.Errorf("last poll: %+v, want Done", step)
	}
}

func TestTypingWaitsForTheScheduledTime(t *testing.T) {
	ex := mustExec(t, nil, OpType{Meta: testMeta(""), Text: "ab", PerChar: 50 * time.Millisecond})
	ex.Poll(0, textScreen(""))
	step, _ := ex.Poll(20*time.Millisecond, textScreen(""))
	if step.Write != nil || step.Deadline != 50*time.Millisecond {
		t.Errorf("early poll: %+v, want no write and deadline 50ms", step)
	}
}

func TestLatePollDelaysTheRestInsteadOfBursting(t *testing.T) {
	ex := mustExec(t, nil, OpType{Meta: testMeta(""), Text: "abc", PerChar: 50 * time.Millisecond})
	ex.Poll(0, textScreen(""))
	step, _ := ex.Poll(500*time.Millisecond, textScreen(""))
	if string(step.Write) != "b" || step.Deadline != 550*time.Millisecond {
		t.Errorf("late poll: %+v, want write b and deadline 550ms", step)
	}
}

func TestTypingWithoutDelayWritesAllAtOnce(t *testing.T) {
	ex := mustExec(t, nil, OpType{Meta: testMeta(""), Text: "ab\ncd"})
	step, _ := ex.Poll(0, textScreen(""))
	if string(step.Write) != "ab\rcd" {
		t.Errorf("write %q, want ab\\rcd", step.Write)
	}
}

func TestEnterIsOneUnitAndCRLFIsOne(t *testing.T) {
	got := typeUnits("a\r\nb\né")
	want := []string{"a", "\r\n", "b", "\n", "é"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("units %q, want %q", got, want)
	}
}

func TestJitterIsSeededAndBounded(t *testing.T) {
	plan := lowerSrc(t, "seed 7\njitter 50%\ntyping 40ms\ntype abcdefghijklmnopqrstuvwxyz\n")
	run := func(seed int64) []Event {
		p := *plan
		p.Process.Seed = seed
		ev, err := runWorld(t, &p, newFakeShell(), Deps{})
		if err != nil {
			t.Fatal(err)
		}
		return ev
	}
	a, b, c := run(7), run(7), run(8)
	if !reflect.DeepEqual(a, b) {
		t.Error("same seed gave different timelines")
	}
	if reflect.DeepEqual(a, c) {
		t.Error("different seeds gave the same timeline")
	}
	var at []time.Duration
	for _, e := range a {
		if e.Kind == "write" {
			at = append(at, e.At)
		}
	}
	if len(at) != 26 {
		t.Fatalf("%d writes, want 26", len(at))
	}
	for i := 1; i < len(at); i++ {
		if d := at[i] - at[i-1]; d < 20*time.Millisecond || d > 60*time.Millisecond {
			t.Errorf("delay %v outside 40ms±50%%", d)
		}
	}
}

func TestJitterZeroIsExact(t *testing.T) {
	plan := lowerSrc(t, "typing 40ms\ntype abc\n")
	ev, _ := runWorld(t, plan, newFakeShell(), Deps{})
	got := writes(ev)
	want := []string{`t=+0s write "a"`, `t=+40ms write "b"`, `t=+80ms write "c"`}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestDepsRandOverridesSeed(t *testing.T) {
	plan := lowerSrc(t, "jitter 50%\ntype abcdef\n")
	mk := func(seed uint64) []Event {
		ev, _ := runWorld(t, plan, newFakeShell(), Deps{Rand: rand.New(rand.NewPCG(seed, 1))})
		return ev
	}
	if !reflect.DeepEqual(mk(1), mk(1)) || reflect.DeepEqual(mk(1), mk(2)) {
		t.Error("Deps.Rand is not used for jitter")
	}
}

func TestPressRepeatPacing(t *testing.T) {
	typing := Settings{Typing: 10 * time.Millisecond}
	m := testMeta("")
	m.Eff = typing
	up := keys.Chord{Key: keys.KeyUp}
	ex := mustExec(t, nil, OpPress{Meta: m, Chord: up, Repeat: 3, Gap: true})
	var at []time.Duration
	for now := time.Duration(0); ; {
		step, err := ex.Poll(now, textScreen(""))
		if err != nil {
			t.Fatal(err)
		}
		if step.Write != nil {
			at = append(at, now)
		}
		if step.Done {
			break
		}
		now = step.Deadline
	}
	want := []time.Duration{10 * time.Millisecond, 20 * time.Millisecond, 30 * time.Millisecond}
	if !reflect.DeepEqual(at, want) {
		t.Errorf("presses at %v, want %v (the gap comes first)", at, want)
	}
}

func TestSleepAndLingerPause(t *testing.T) {
	ex := mustExec(t, nil,
		OpSleep{Meta: testMeta(""), Duration: time.Second},
		OpLinger{Meta: testMeta(""), Duration: 2 * time.Second},
		OpMark{Meta: testMeta(""), Label: "m"})
	step, _ := ex.Poll(0, textScreen(""))
	if step.Deadline != time.Second || step.Write != nil {
		t.Fatalf("sleep: %+v", step)
	}
	step, _ = ex.Poll(time.Second, textScreen(""))
	if step.Deadline != 3*time.Second {
		t.Fatalf("linger starts when the sleep ends: %+v", step)
	}
	step, _ = ex.Poll(3*time.Second, textScreen(""))
	if step.Mark != "m" || !step.Done {
		t.Fatalf("mark: %+v", step)
	}
}

func TestLingerOncePerTextBlock(t *testing.T) {
	plan := lowerSrc(t, "linger 1s\ntype lines {\n  a\n  b\n  c\n}\n")
	ev, err := runWorld(t, plan, newFakeShell(), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	// 5 units (a CR b CR c) at 50ms, then one linger of 1s before the exit span.
	var last, begin time.Duration
	for _, e := range ev {
		if e.Kind == "write" {
			last = e.At
		}
		if e.Kind == "span-begin" {
			begin = e.At
		}
	}
	if last != 200*time.Millisecond || begin != last+time.Second {
		t.Errorf("last write %v, exit span at %v; want one 1s linger after the block", last, begin)
	}
}

func TestSpansInOrderWithImplicitEndSpanLast(t *testing.T) {
	plan := lowerSrc(t, "scene s {\n  hidden {\n    mark a\n  }\n  mark b\n}\n")
	ev, err := runWorld(t, plan, newFakeShell(), Deps{})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range ev {
		got = append(got, e.Kind+" "+e.Data)
	}
	want := []string{
		"span-begin scene:s id=1 parent=0", "span-begin hidden id=2 parent=1", "mark a", "span-end id=2",
		"mark b", "span-end id=1", "span-begin hidden id=3 parent=0", "shutdown ", "child-exit 0", "span-end id=3", "done ",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("events\n%q\nwant\n%q", got, want)
	}
}

func TestKeyEncodeErrors(t *testing.T) {
	m := testMeta("demo")
	for _, tc := range []struct {
		name string
		op   Op
		want string
	}{
		{"press", OpPress{Meta: m, Chord: keys.Chord{Key: keys.KeyF5}, Repeat: 1}, `cannot press f5`},
		{"text", OpType{Meta: m, Text: "x!y", PerChar: time.Millisecond}, `cannot type "!"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ex := mustExec(t, fakeKeys{failOn: map[string]string{"press": "f5", "text": "!"}[tc.name]}, tc.op)
			var err error
			for now := time.Duration(0); err == nil; now += time.Millisecond {
				_, err = ex.Poll(now, textScreen(""))
				if now > time.Second {
					t.Fatal("no error")
				}
			}
			var se *ScriptError
			if !errors.As(err, &se) || se.Kind != KeyEncode || se.ExitCode != 1 {
				t.Fatalf("error %#v, want KeyEncode with exit code 1", err)
			}
			if !strings.HasPrefix(se.Error(), `t.script:3:5: scene "demo": `+tc.want) {
				t.Errorf("message %q", se.Error())
			}
		})
	}
}

func TestErrorIsStickyAndSpansCloseAfterFailure(t *testing.T) {
	plan := lowerSrc(t, "scene s {\n  hidden {\n    press f5\n  }\n}\n")
	ex, _ := New(plan, Deps{Keys: fakeKeys{failOn: "f5"}})
	var err error
	for now := time.Duration(0); err == nil; now += time.Second {
		_, err = ex.Poll(now, textScreen(""))
	}
	_, err2 := ex.Poll(time.Hour, textScreen(""))
	if err2 != err {
		t.Errorf("second error %v, want the same error", err2)
	}
	open := ex.OpenSpans()
	if len(open) != 2 || open[0].Event.ID != 2 || open[1].Event.ID != 1 {
		t.Errorf("open spans %+v, want the ends of 2 then 1", open)
	}
}

func TestPollAfterDoneStaysDone(t *testing.T) {
	ex := mustExec(t, nil, OpMark{Meta: testMeta(""), Label: "x"})
	step, _ := ex.Poll(0, textScreen(""))
	if step.Mark != "x" || !step.Done {
		t.Fatalf("first: %+v", step)
	}
	for i := range 3 {
		step, err := ex.Poll(time.Duration(i)*time.Second, textScreen(""))
		if err != nil || !step.Done || step.Mark != "" || step.Write != nil || step.Span != nil {
			t.Errorf("poll after done: %+v, %v", step, err)
		}
	}
}

func TestPollWithoutNewInformationRepeatsTheDeadline(t *testing.T) {
	ex := mustExec(t, nil, OpSleep{Meta: testMeta(""), Duration: 5 * time.Second})
	first, _ := ex.Poll(0, textScreen("x"))
	for _, now := range []time.Duration{time.Second, time.Second, 3 * time.Second} {
		step, _ := ex.Poll(now, textScreen("x"))
		if !reflect.DeepEqual(step, first) {
			t.Errorf("poll at %v: %+v, want %+v", now, step, first)
		}
	}
}

func TestPollTimeNeverGoesBackwards(t *testing.T) {
	ex := mustExec(t, nil, OpSleep{Meta: testMeta(""), Duration: 5 * time.Second})
	ex.Poll(2*time.Second, textScreen(""))
	step, _ := ex.Poll(time.Second, textScreen(""))
	if step.Deadline != 7*time.Second {
		t.Errorf("deadline %v, want 7s (the sleep began at 2s)", step.Deadline)
	}
}

func TestNewRejectsWhatItCannotRun(t *testing.T) {
	if _, err := New(&Plan{}, Deps{}); err == nil {
		t.Error("nil Keys accepted")
	}
	if _, err := New(nil, Deps{Keys: fakeKeys{}}); err == nil {
		t.Error("nil plan accepted")
	}
	bad := &Plan{Ops: []Op{OpWait{Meta: testMeta(""), Kind: WaitRegex, Pattern: "(", Timeout: time.Second}}}
	if _, err := New(bad, Deps{Keys: fakeKeys{}}); err == nil || !strings.Contains(err.Error(), "3:5") {
		t.Errorf("bad regex: %v", err)
	}
}

func TestDeadlineNeverInThePastOnExamples(t *testing.T) {
	// runWorld fails the test on a Deadline before now; run every example.
	for _, name := range []string{"ex1_hello", "ex2_git_scenes", "ex3_vim_tui", "ex4_long_build", "ex5_ci_smoke"} {
		src := readScript(t, "testdata/lower/"+name+".script")
		if _, err := runWorld(t, lowerSrc(t, src), newFakeShell(), Deps{}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

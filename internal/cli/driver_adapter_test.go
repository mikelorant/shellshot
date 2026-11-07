package cli

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/script/keys"
	"github.com/mikelorant/shellshot/internal/script/run"
	"github.com/mikelorant/shellshot/internal/vt/ghostty"
)

// fakeExecutor returns canned steps and records what it was given.
type fakeExecutor struct {
	steps   []run.Step
	err     error
	polls   []time.Duration
	screens []string
	exited  []int
	open    []run.SpanOp
}

func (f *fakeExecutor) Poll(now time.Duration, scr run.Screen) (run.Step, error) {
	f.polls = append(f.polls, now)
	f.screens = append(f.screens, scr.Text())
	if f.err != nil {
		return run.Step{}, f.err
	}
	st := f.steps[0]
	f.steps = f.steps[1:]
	return st, nil
}

func (f *fakeExecutor) ChildExited(_ time.Duration, status int) { f.exited = append(f.exited, status) }
func (f *fakeExecutor) OpenSpans() []run.SpanOp                 { return f.open }

type fakeScreen string

// startedDriver is a scriptDriver that has already released its executor.
func startedDriver(exec scriptExecutor) *scriptDriver {
	d := newScriptDriver(exec)
	d.started = true
	return d
}

func (s fakeScreen) Text() string { return string(s) }

func TestScriptDriverConvertsSteps(t *testing.T) {
	begin := event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanScene, Name: "intro"}
	fake := &fakeExecutor{steps: []run.Step{
		{Write: []byte("ls\r"), Deadline: 5 * time.Millisecond},
		{Mark: "here", Deadline: 6 * time.Millisecond},
		{Span: &run.SpanOp{Event: begin}, Deadline: 7 * time.Millisecond},
		{Shutdown: true, Deadline: 2 * time.Second},
		{Done: true, Deadline: 3 * time.Second},
	}}
	d := startedDriver(fake)
	want := []struct {
		write    string
		mark     string
		span     any
		shutdown bool
		deadline time.Duration
		done     bool
	}{
		{write: "ls\r", deadline: 5 * time.Millisecond},
		{mark: "here", deadline: 6 * time.Millisecond},
		{span: begin, deadline: 7 * time.Millisecond},
		{shutdown: true, deadline: 2 * time.Second},
		{deadline: 3 * time.Second, done: true},
	}
	for i, w := range want {
		now := time.Duration(i) * time.Millisecond
		got, err := d.Poll(now, fakeScreen("screen"))
		if err != nil {
			t.Fatalf("poll %d: %v", i, err)
		}
		if string(got.Write) != w.write || got.Mark != w.mark || got.Shutdown != w.shutdown ||
			got.Deadline != w.deadline || got.Done != w.done {
			t.Errorf("poll %d = %+v, want %+v", i, got, w)
		}
		if !reflect.DeepEqual(got.Span, w.span) {
			t.Errorf("poll %d Span = %#v, want %#v", i, got.Span, w.span)
		}
		if w.span == nil && got.Span != nil {
			t.Errorf("poll %d: Span = %#v, want a nil interface", i, got.Span)
		}
	}
	if want := []time.Duration{0, time.Millisecond, 2 * time.Millisecond, 3 * time.Millisecond, 4 * time.Millisecond}; !reflect.DeepEqual(fake.polls, want) {
		t.Errorf("executor polled at %v, want %v", fake.polls, want)
	}
}

// TestScriptDriverSpanIsEventSpan checks the type the binSink expects.
func TestScriptDriverSpanIsEventSpan(t *testing.T) {
	fake := &fakeExecutor{steps: []run.Step{{Span: &run.SpanOp{Event: event.Span{Op: event.SpanEnd, ID: 3}}}}}
	st, err := startedDriver(fake).Poll(0, fakeScreen(""))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Span.(event.Span); !ok {
		t.Errorf("Span is %T, want event.Span", st.Span)
	}
}

func TestScriptDriverPassesErrorsAndScreen(t *testing.T) {
	scriptErr := &run.ScriptError{Kind: run.WaitTimeout, Message: "boom", ExitCode: 3}
	fake := &fakeExecutor{err: scriptErr}
	d := startedDriver(fake)
	_, err := d.Poll(time.Second, fakeScreen("$ ls\nfile"))
	var got *run.ScriptError
	if !errors.As(err, &got) || got != scriptErr {
		t.Fatalf("Poll error = %v, want the executor's *ScriptError", err)
	}
	if d.lastScreen() != "$ ls\nfile" {
		t.Errorf("lastScreen = %q, want the screen of the failing poll", d.lastScreen())
	}
}

func TestScriptDriverForwardsChildExitAndOpenSpans(t *testing.T) {
	end2 := event.Span{Op: event.SpanEnd, ID: 2}
	end1 := event.Span{Op: event.SpanEnd, ID: 1}
	fake := &fakeExecutor{open: []run.SpanOp{{Event: end2}, {Event: end1}}}
	d := startedDriver(fake)
	d.ChildExited(time.Second, 7)
	if !reflect.DeepEqual(fake.exited, []int{7}) {
		t.Errorf("ChildExited statuses = %v, want [7]", fake.exited)
	}
	ops := d.OpenSpans()
	if len(ops) != 2 || ops[0] != any(end2) || ops[1] != any(end1) {
		t.Errorf("OpenSpans = %#v, want [%#v %#v] in order", ops, end2, end1)
	}
}

func TestLazyKeysNeedTheTerminal(t *testing.T) {
	h := &termHolder{}
	k := &lazyKeys{holder: h}
	defer k.close()
	if _, err := k.EncodeText("x"); err == nil {
		t.Error("EncodeText before the terminal exists succeeded, want an error")
	}
}

func TestLazyKeysEncodeWithTheCapturedTerminal(t *testing.T) {
	h := &termHolder{}
	term, err := h.factory(ghostty.New)(40, 5)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()
	if h.term != term {
		t.Fatal("termHolder did not keep the terminal made by the factory")
	}
	k := &lazyKeys{holder: h}
	defer k.close()

	chord, err := keys.Parse("up")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := k.Encode(chord); err != nil || string(got) != "\x1b[A" {
		t.Errorf("up = %q, %v, want ESC [ A", got, err)
	}
	// The encoder follows the terminal's modes at the moment of the press.
	if _, err := term.Write([]byte("\x1b[?1h")); err != nil {
		t.Fatal(err)
	}
	if got, err := k.Encode(chord); err != nil || string(got) != "\x1bOA" {
		t.Errorf("up with application cursor keys = %q, %v, want ESC O A", got, err)
	}
	if got, err := k.EncodeText("a\nb"); err != nil || string(got) != "a\rb" {
		t.Errorf("EncodeText = %q, %v, want a CR b", got, err)
	}
}

// poll is one call of Poll in TestScriptDriverStart.
type poll struct {
	at     time.Duration
	screen string
	// wantHeld is the Deadline of a poll that must not reach the executor;
	// 0 means the executor is polled.
	wantHeld time.Duration
}

func TestScriptDriverStart(t *testing.T) {
	ms := time.Millisecond
	tests := []struct {
		name      string
		polls     []poll
		wantStart time.Duration // the time of the first executor poll
	}{
		{
			name: "prompt at 300ms: starts 150ms after it",
			polls: []poll{
				{at: 0, screen: "", wantHeld: time.Second},
				{at: 300 * ms, screen: "$", wantHeld: 450 * ms},
				{at: 450 * ms, screen: "$"},
			},
			wantStart: 450 * ms,
		},
		{
			name: "a prompt that is redrawn restarts the quiet time",
			polls: []poll{
				{at: 10 * ms, screen: "$", wantHeld: 160 * ms},
				{at: 100 * ms, screen: "$ e", wantHeld: 250 * ms},
				{at: 250 * ms, screen: "$ e"},
			},
			wantStart: 250 * ms,
		},
		{
			name: "silent program: starts at the 1s cap",
			polls: []poll{
				{at: 0, screen: "", wantHeld: time.Second},
				{at: time.Second, screen: ""},
			},
			wantStart: time.Second,
		},
		{
			name: "output that keeps changing: starts at the 1s cap",
			polls: []poll{
				{at: 0, screen: "a", wantHeld: 150 * ms},
				{at: 100 * ms, screen: "ab", wantHeld: 250 * ms},
				{at: 200 * ms, screen: "abc", wantHeld: 350 * ms},
				{at: 900 * ms, screen: "abcd", wantHeld: time.Second},
				{at: time.Second, screen: "abcde"},
			},
			wantStart: time.Second,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeExecutor{steps: []run.Step{{Write: []byte("x"), Deadline: time.Hour}}}
			d := newScriptDriver(fake)
			for i, p := range tt.polls {
				st, err := d.Poll(p.at, fakeScreen(p.screen))
				if err != nil {
					t.Fatal(err)
				}
				if p.wantHeld != 0 {
					if len(fake.polls) != 0 || st.Deadline != p.wantHeld || len(st.Write) != 0 || st.Done {
						t.Errorf("poll %d at %v: step %+v, executor polls %v; want held with deadline %v", i, p.at, st, fake.polls, p.wantHeld)
					}
					continue
				}
				if len(fake.polls) != 1 || fake.polls[0] != p.at || string(st.Write) != "x" {
					t.Errorf("poll %d at %v: step %+v, executor polls %v; want the executor polled once", i, p.at, st, fake.polls)
				}
			}
			if len(fake.polls) != 1 || fake.polls[0] != tt.wantStart {
				t.Errorf("executor first polled at %v, want %v", fake.polls, tt.wantStart)
			}
		})
	}
}

func TestScriptDriverStartedPassesEveryPollThrough(t *testing.T) {
	fake := &fakeExecutor{steps: []run.Step{{Deadline: 1}, {Deadline: 2}, {Deadline: 3}}}
	d := newScriptDriver(fake)
	for i, at := range []time.Duration{time.Second, 1100 * time.Millisecond, 1200 * time.Millisecond} {
		if _, err := d.Poll(at, fakeScreen("")); err != nil {
			t.Fatal(err)
		}
		if len(fake.polls) != i+1 {
			t.Errorf("after poll %d the executor has %d polls, want %d", i, len(fake.polls), i+1)
		}
	}
}

func TestScriptDriverKeepsScreenWhileHeld(t *testing.T) {
	d := newScriptDriver(&fakeExecutor{})
	if _, err := d.Poll(0, fakeScreen("$ ")); err != nil {
		t.Fatal(err)
	}
	if d.lastScreen() != "$ " {
		t.Errorf("lastScreen = %q, want the screen seen while held", d.lastScreen())
	}
}

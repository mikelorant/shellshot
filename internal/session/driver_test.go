package session

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/snapshot"
	"github.com/mikelorant/shellshot/internal/vt"
)

// fakeDriver is a Driver whose Poll is a function of the call number, the time
// and the screen. It records every call.
type fakeDriver struct {
	mu       sync.Mutex
	fn       func(n int, now time.Duration, scr Screen) (Step, error)
	calls    []time.Duration
	exited   []exitCall
	open     []SpanOp
	openCall int
}

type exitCall struct {
	at     time.Duration
	status int
}

func (d *fakeDriver) Poll(now time.Duration, scr Screen) (Step, error) {
	d.mu.Lock()
	n := len(d.calls)
	d.calls = append(d.calls, now)
	d.mu.Unlock()
	return d.fn(n, now, scr)
}

func (d *fakeDriver) ChildExited(now time.Duration, status int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.exited = append(d.exited, exitCall{now, status})
}

func (d *fakeDriver) OpenSpans() []SpanOp {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.openCall++
	return append([]SpanOp(nil), d.open...)
}

func (d *fakeDriver) polls() []time.Duration {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]time.Duration(nil), d.calls...)
}

func (d *fakeDriver) exits() []exitCall {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]exitCall(nil), d.exited...)
}

// steps returns a driver that hands out the steps in order and then waits
// forever (no work, a Deadline far in the future).
func steps(list ...Step) *fakeDriver {
	return &fakeDriver{fn: func(n int, _ time.Duration, _ Screen) (Step, error) { return stepAt(list, n), nil }}
}

func stepAt(list []Step, n int) Step {
	if n < len(list) {
		return list[n]
	}
	return Step{Deadline: time.Hour}
}

// startDriverRun starts a run with a driver and events.
func startDriverRun(t *testing.T, d Driver, mutate func(*harness)) (*harness, *fakeEvents) {
	t.Helper()
	ev := &fakeEvents{}
	h := startRun(t, 0, nil, func(h *harness) {
		h.events = ev
		h.driver = d
		h.term.onWrite = ev.written
		h.proc.onWrite = func(p []byte) { ev.procWrote(p) }
		if mutate != nil {
			mutate(h)
		}
	})
	h.nextFrame()
	return h, ev
}

func (e *fakeEvents) procWrote(p []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq = append(e.seq, fmt.Sprintf("send:%q", p))
}

// eventually waits for cond, which is checked every millisecond.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testTimeout)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func (h *harness) timerAt(i int) *fakeTimer {
	h.t.Helper()
	eventually(h.t, fmt.Sprintf("timer %d", i), func() bool {
		h.tmu.Lock()
		defer h.tmu.Unlock()
		return len(h.timers) > i
	})
	h.tmu.Lock()
	defer h.tmu.Unlock()
	return h.timers[i]
}

func (h *harness) timerDurations() []time.Duration {
	h.tmu.Lock()
	defer h.tmu.Unlock()
	var out []time.Duration
	for _, t := range h.timers {
		out = append(out, t.d)
	}
	return out
}

// fire delivers the timer's tick.
func (t *fakeTimer) fire() { t.c <- time.Time{} }

// polled waits until the driver has been polled n times and the loop has
// finished acting on the last Step.
func (h *harness) polled(d *fakeDriver, n int) {
	h.t.Helper()
	eventually(h.t, fmt.Sprintf("%d polls", n), func() bool { return len(d.polls()) >= n })
	h.settle()
}

func (h *harness) exitChild(code int) {
	h.t.Helper()
	h.proc.exit = code
	close(h.proc.data)
	h.proc.Close()
}

func wantSeq(t *testing.T, ev *fakeEvents, want ...string) {
	t.Helper()
	if got := ev.sequence(); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("sequence = %q\n want %q", got, want)
	}
}

func (p *fakeProc) sent() []string {
	p.wmu.Lock()
	defer p.wmu.Unlock()
	var out []string
	for _, w := range p.writes {
		out = append(out, string(w))
	}
	return out
}

func TestDriverNilChangesNothing(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	h.output("a")
	h.tick()
	h.nextFrame()
	h.cancel()
	h.wait()
	if n := h.tickers.Load(); n != 1 {
		t.Errorf("Ticker called %d times, want 1 (the sampler only)", n)
	}
}

// readOnlyProc hides the Write method of fakeProc.
type readOnlyProc struct{ p *fakeProc }

func (r readOnlyProc) Read(b []byte) (int, error)  { return r.p.Read(b) }
func (r readOnlyProc) Resize(cols, rows int) error { return r.p.Resize(cols, rows) }
func (r readOnlyProc) Wait() (int, error)          { return r.p.Wait() }
func (r readOnlyProc) Close() error                { return r.p.Close() }

func TestDriverRequiresWritableProcess(t *testing.T) {
	cfg := Config{Driver: steps()}
	factory := func(cols, rows int) (vt.Terminal, error) { return &fakeTerm{written: make(chan struct{}, 1)}, nil }
	code, err := Run(context.Background(), readOnlyProc{newFakeProc()}, factory, cfg, SinkFunc(func(snapshot.Frame) error { return nil }))
	if !errors.Is(err, ErrDriverNeedsWriter) || code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, ErrDriverNeedsWriter)", code, err)
	}
}

func TestDriverIsPolledAtStartAndWritesFollowItsTiming(t *testing.T) {
	d := steps(
		Step{Write: []byte("a"), Deadline: 50 * time.Millisecond},
		Step{Write: []byte("b"), Deadline: 120 * time.Millisecond},
		Step{Write: []byte("c"), Deadline: time.Hour},
	)
	h, ev := startDriverRun(t, d, nil)
	h.polled(d, 1)
	if got := h.proc.sent(); fmt.Sprint(got) != "[a]" {
		t.Fatalf("sent after the first poll = %q, want [a]", got)
	}

	h.clock.advance(50 * time.Millisecond)
	h.timerAt(0).fire()
	h.polled(d, 2)
	h.clock.advance(70 * time.Millisecond)
	h.timerAt(1).fire()
	h.polled(d, 3)

	if got := h.proc.sent(); fmt.Sprint(got) != "[a b c]" {
		t.Errorf("sent = %q, want [a b c]", got)
	}
	wantPolls := []time.Duration{0, 50 * time.Millisecond, 120 * time.Millisecond}
	if got := d.polls(); fmt.Sprint(got) != fmt.Sprint(wantPolls) {
		t.Errorf("poll times = %v, want %v", got, wantPolls)
	}
	wantTimers := []time.Duration{50 * time.Millisecond, 70 * time.Millisecond, time.Hour - 120*time.Millisecond}
	h.timerAt(2)
	if got := h.timerDurations(); fmt.Sprint(got) != fmt.Sprint(wantTimers) {
		t.Errorf("timer durations = %v, want %v", got, wantTimers)
	}
	if !h.timerAt(0).stopped.Load() {
		t.Error("a fired timer was not released")
	}
	wantSeq(t, ev, `send:"a"`, `send:"b"`, `send:"c"`)
	h.cancel()
	h.wait()
}

func TestDriverMarksAndSpansAreOrderedBetweenOutput(t *testing.T) {
	d := &fakeDriver{fn: func(n int, now time.Duration, scr Screen) (Step, error) {
		switch n {
		case 0:
			return Step{Deadline: time.Hour}, nil
		case 1: // after output "x"
			return Step{Span: "begin1", Mark: "m", Deadline: now}, nil
		case 2: // the Step above asked for another poll at once
			return Step{Span: "end1", Deadline: time.Hour}, nil
		}
		return Step{Deadline: time.Hour}, nil
	}}
	h, ev := startDriverRun(t, d, nil)
	// Run emits the first frame before its first poll: without this wait, "x" can arrive in
	// that gap and be written ahead of poll 0, which shifts every later poll by one chunk.
	h.polled(d, 1)
	h.clock.advance(100 * time.Millisecond)
	h.output("x")
	h.settle() // the poll that follows the chunk is done before the clock moves
	h.clock.advance(200 * time.Millisecond)
	h.output("y")
	h.settle()

	wantSeq(t, ev, "event:x", "write:x", "mark:m", "span:begin1", "span:end1", "event:y", "write:y")
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.marks) != 1 || ev.marks[0].at != 100*time.Millisecond {
		t.Errorf("marks = %+v, want one at 100ms (same origin as output)", ev.marks)
	}
	if len(ev.spans) != 2 || ev.spans[0].at != 100*time.Millisecond || ev.spans[1].at != 100*time.Millisecond {
		t.Errorf("spans = %+v, want both at 100ms", ev.spans)
	}
	if len(ev.outs) != 2 || ev.outs[1].at != 300*time.Millisecond {
		t.Errorf("outs = %+v", ev.outs)
	}
}

func TestDriverWithoutEventsIgnoresMarksAndSpans(t *testing.T) {
	d := steps(Step{Mark: "m", Span: "s", Write: []byte("w"), Deadline: time.Hour})
	h := startRun(t, 0, nil, func(h *harness) { h.driver = d })
	h.nextFrame()
	h.polled(d, 1)
	if got := h.proc.sent(); fmt.Sprint(got) != "[w]" {
		t.Errorf("sent = %q", got)
	}
	h.cancel()
	h.wait()
}

func TestDriverIdleDoesNotSpinOrLog(t *testing.T) {
	d := steps(Step{Deadline: 10 * time.Second})
	h, ev := startDriverRun(t, d, nil)
	h.polled(d, 1)
	for range 5 {
		h.tick()
	}
	h.settle()
	if n := len(d.polls()); n != 1 {
		t.Errorf("driver polled %d times while idle, want 1", n)
	}
	if got := ev.sequence(); len(got) != 0 {
		t.Errorf("events while idle = %q, want none", got)
	}
	if n := h.term.snapshots.Load(); n != 1 {
		t.Errorf("snapshots = %d, want 1 (the initial frame)", n)
	}
	if got := h.timerDurations(); fmt.Sprint(got) != "[10s]" {
		t.Errorf("timers = %v, want one for the deadline", got)
	}
	h.cancel()
	h.wait()
}

func TestDriverSameDeadlineKeepsItsTimer(t *testing.T) {
	d := &fakeDriver{fn: func(n int, now time.Duration, _ Screen) (Step, error) {
		return Step{Deadline: 5 * time.Second}, nil
	}}
	h, _ := startDriverRun(t, d, nil)
	h.output("a")
	h.output("b")
	h.settle()
	if n := len(d.polls()); n != 3 {
		t.Errorf("polls = %d, want 3 (start and each chunk)", n)
	}
	if got := h.timerDurations(); len(got) != 1 {
		t.Errorf("timers = %v, want the one made for the unchanged deadline", got)
	}
	h.cancel()
	h.wait()
}

func TestDriverEarlyTimerIsRearmedNotSpun(t *testing.T) {
	d := &fakeDriver{fn: func(n int, now time.Duration, _ Screen) (Step, error) {
		return Step{Deadline: time.Second}, nil
	}}
	h, _ := startDriverRun(t, d, nil)
	h.polled(d, 1)
	h.clock.advance(400 * time.Millisecond)
	h.timerAt(0).fire() // the clock is behind the deadline: the driver asks again
	h.polled(d, 2)
	if got := h.timerDurations(); fmt.Sprint(got) != "[1s 600ms]" {
		t.Errorf("timers = %v, want [1s 600ms]", got)
	}
	h.cancel()
	h.wait()
}

func TestDriverImmediateStepsAreBoundedThenYield(t *testing.T) {
	d := &fakeDriver{fn: func(n int, now time.Duration, _ Screen) (Step, error) {
		return Step{Write: []byte("w"), Deadline: now}, nil // always more work
	}}
	var seen []int
	var mu sync.Mutex
	h, _ := startDriverRun(t, d, func(h *harness) {
		h.onTick = func(time.Time) error {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, len(d.polls()))
			return nil
		}
	})
	h.tick() // accepted only because the loop yields between bursts
	h.tick()
	mu.Lock()
	got := append([]int(nil), seen...)
	mu.Unlock()
	if len(got) == 0 {
		t.Fatal("no tick was handled")
	}
	for _, n := range got {
		if n == 0 || n%maxConsecutivePolls != 0 {
			t.Errorf("driver had %d polls when a tick ran, want a positive multiple of %d", n, maxConsecutivePolls)
		}
	}
	h.cancel()
	if r := h.wait(); !errors.Is(r.err, context.Canceled) {
		t.Errorf("Run error = %v, want cancellation to be served", r.err)
	}
}

func TestDriverNoWorkAndPastDeadlineWaitsForEvents(t *testing.T) {
	d := steps(Step{}, Step{Deadline: time.Hour}) // nothing to do, no time to wait for
	h, _ := startDriverRun(t, d, nil)
	h.polled(d, 1)
	h.tick()
	h.settle()
	if n := len(d.polls()); n != 1 {
		t.Fatalf("polls = %d, want 1 (no spinning on an idle Step)", n)
	}
	h.output("a") // output wakes the driver again
	h.settle()    // the poll that follows the chunk is done before it is counted
	if n := len(d.polls()); n != 2 {
		t.Errorf("polls after output = %d, want 2", n)
	}
	h.cancel()
	h.wait()
}

func TestDriverScreenSnapshotsAreLazyAndCached(t *testing.T) {
	var texts []string
	d := &fakeDriver{fn: func(n int, now time.Duration, scr Screen) (Step, error) {
		if n >= 1 { // the first poll never looks at the screen
			texts = append(texts, scr.Text(), scr.Text())
		}
		return Step{Deadline: now + time.Second}, nil
	}}
	h, _ := startDriverRun(t, d, nil)
	h.polled(d, 1)
	if n := h.term.snapshots.Load(); n != 1 {
		t.Fatalf("snapshots = %d after a poll that did not read the screen, want 1 (initial)", n)
	}

	h.clock.advance(time.Second)
	h.output("a") // poll 2 reads the screen twice: one snapshot
	h.settle()    // the tick samples the dirty terminal for the frame: one more
	h.nextFrame()
	if n := h.term.snapshots.Load(); n != 3 {
		t.Fatalf("snapshots = %d after output, a poll reading twice and a sample, want 3", n)
	}

	h.clock.advance(time.Second)
	h.timerAt(1).fire() // a deadline poll with an unchanged terminal: cached
	h.polled(d, 3)
	if n := h.term.snapshots.Load(); n != 3 {
		t.Errorf("snapshots = %d after a poll with no change, want 3 (cached)", n)
	}
	h.output("b")
	h.settle()
	h.nextFrame()
	if n := h.term.snapshots.Load(); n != 5 {
		t.Errorf("snapshots = %d after more output, want 5 (one for the poll, one for the sample)", n)
	}
	h.cancel()
	h.wait()
	want := []string{"a", "a", "a", "a", "ab", "ab"}
	if fmt.Sprint(texts) != fmt.Sprint(want) {
		t.Errorf("screen texts = %q, want %q", texts, want)
	}
}

func TestDriverShutdownSendsEOFThenClosesAfterGrace(t *testing.T) {
	d := &fakeDriver{fn: func(n int, now time.Duration, _ Screen) (Step, error) {
		switch n {
		case 0:
			return Step{Shutdown: true, Deadline: 5 * time.Second}, nil
		case 1: // after ChildExited
			return Step{Done: true, Deadline: now}, nil
		}
		return Step{Done: true}, nil
	}}
	h, ev := startDriverRun(t, d, func(h *harness) { h.grace = 3 * time.Second; h.proc.exit = 143 })
	h.polled(d, 1)
	if got := h.proc.sent(); fmt.Sprint(got) != "[\x04]" {
		t.Fatalf("sent = %q, want Ctrl-D", got)
	}
	grace := h.timerAt(0)
	if grace.d != 3*time.Second {
		t.Errorf("grace timer = %v, want 3s", grace.d)
	}
	if h.proc.isClosed() {
		t.Fatal("process closed before the grace time was over")
	}

	h.clock.advance(3 * time.Second)
	grace.fire()
	r := h.wait()
	if r.err != nil || r.code != 143 {
		t.Fatalf("Run = (%d, %v), want (143, nil)", r.code, r.err)
	}
	if !h.proc.isClosed() {
		t.Error("process not closed after the grace time")
	}
	if got := d.exits(); len(got) != 1 || got[0].status != 143 || got[0].at != 3*time.Second {
		t.Errorf("ChildExited calls = %+v, want one (3s, 143)", got)
	}
	last := h.nextFrame()
	if last.At != 3*time.Second {
		t.Errorf("final frame at %v, want 3s", last.At)
	}
	wantSeq(t, ev, `send:"\x04"`, "exit")
	h.assertClean()
}

func TestDriverShutdownChildExitsWithinGrace(t *testing.T) {
	d := &fakeDriver{fn: func(n int, now time.Duration, _ Screen) (Step, error) {
		switch n {
		case 0:
			return Step{Shutdown: true, Deadline: 5 * time.Second}, nil
		case 1:
			return Step{Done: true, Deadline: now}, nil
		}
		return Step{Done: true}, nil
	}}
	h, ev := startDriverRun(t, d, nil)
	h.polled(d, 1)
	grace := h.timerAt(0)
	if grace.d != DefaultShutdownGrace {
		t.Errorf("grace timer = %v, want the default %v", grace.d, DefaultShutdownGrace)
	}

	h.clock.advance(300 * time.Millisecond)
	h.output("logout")
	h.exitChild(5)
	r := h.wait()
	if r.err != nil || r.code != 5 {
		t.Fatalf("Run = (%d, %v), want (5, nil)", r.code, r.err)
	}
	if !grace.stopped.Load() {
		t.Error("grace timer still armed after the child exited")
	}
	if got := d.exits(); len(got) != 1 || got[0].status != 5 || got[0].at != 300*time.Millisecond {
		t.Errorf("ChildExited calls = %+v, want one (300ms, 5)", got)
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.exits) != 1 || ev.exits[0].status != 5 {
		t.Errorf("exit events = %+v, want one with status 5", ev.exits)
	}
}

func TestDriverDoneWaitsForChildThenEndsAsWithoutDriver(t *testing.T) {
	d := steps(Step{Done: true})
	h, ev := startDriverRun(t, d, nil)
	h.polled(d, 1)
	if h.proc.isClosed() {
		t.Fatal("process closed as soon as the driver was done")
	}
	select {
	case r := <-h.result:
		t.Fatalf("Run returned %+v while the child still runs", r)
	default:
	}

	h.clock.advance(time.Second)
	h.output("bye")
	h.tick()
	h.nextFrame() // sampled frame
	h.clock.advance(time.Second)
	h.exitChild(0)

	f := h.nextFrame() // the final frame
	if f.At != 2*time.Second || f.Snapshot.Lines[0] != "bye" {
		t.Errorf("final frame = %+v", f)
	}
	r := h.wait()
	if r.err != nil || r.code != 0 {
		t.Errorf("Run = (%d, %v)", r.code, r.err)
	}
	if n := len(d.polls()); n != 1 {
		t.Errorf("polls = %d after Done, want 1", n)
	}
	if got := d.exits(); len(got) != 1 {
		t.Errorf("ChildExited calls = %+v, want exactly one", got)
	}
	wantSeq(t, ev, "event:bye", "write:bye", "exit")
	h.noFrame()
	h.assertClean()
}

func TestDriverDoneClosesChildAfterGrace(t *testing.T) {
	d := steps(Step{Done: true})
	h, _ := startDriverRun(t, d, func(h *harness) { h.grace = time.Second })
	h.polled(d, 1)
	grace := h.timerAt(0)
	if grace.d != time.Second || h.proc.isClosed() {
		t.Fatalf("grace timer %v, closed %v", grace.d, h.proc.isClosed())
	}
	grace.fire()
	r := h.wait()
	if r.err != nil {
		t.Errorf("Run error = %v; closing the child ourselves is not an error", r.err)
	}
	if got := d.exits(); len(got) != 1 {
		t.Errorf("ChildExited calls = %+v, want exactly one", got)
	}
	h.assertClean()
}

func TestDriverChildExitedBeforeDoneKeepsPolling(t *testing.T) {
	d := &fakeDriver{}
	d.fn = func(n int, now time.Duration, _ Screen) (Step, error) {
		switch {
		case n == 0:
			return Step{Deadline: time.Hour}, nil
		case len(d.exits()) == 1 && now < time.Second:
			return Step{Deadline: time.Second}, nil // a linger after the child is gone
		case now >= time.Second:
			return Step{Done: true}, nil
		}
		return Step{Deadline: time.Hour}, nil
	}
	h, _ := startDriverRun(t, d, nil)
	h.exitChild(2)
	eventually(t, "ChildExited", func() bool { return len(d.exits()) == 1 })
	h.polled(d, 2)
	select {
	case r := <-h.result:
		t.Fatalf("Run returned %+v before the driver was done", r)
	default:
	}
	h.clock.advance(time.Second)
	h.timerAt(1).fire() // timer 0 was the initial deadline, replaced by the linger
	r := h.wait()
	if r.err != nil || r.code != 2 {
		t.Errorf("Run = (%d, %v), want (2, nil)", r.code, r.err)
	}
	if got := d.exits(); len(got) != 1 {
		t.Errorf("ChildExited calls = %+v, want exactly one", got)
	}
}

var errDriver = errors.New("wait timed out")

func TestDriverPollErrorClosesSpansLogsExitAndReturnsError(t *testing.T) {
	d := &fakeDriver{fn: func(n int, now time.Duration, _ Screen) (Step, error) {
		switch n {
		case 0:
			return Step{Span: "begin1", Write: []byte("go"), Deadline: time.Hour}, nil
		case 1:
			return Step{}, errDriver
		}
		return Step{Write: []byte("late")}, nil
	}}
	d.open = []SpanOp{"end2", "end1"}
	h, ev := startDriverRun(t, d, func(h *harness) { h.proc.exit = 137 })
	h.polled(d, 1)
	h.clock.advance(time.Second)
	h.output("x")

	r := h.wait()
	if !errors.Is(r.err, errDriver) || r.code != -1 {
		t.Fatalf("Run = (%d, %v), want (-1, errDriver)", r.code, r.err)
	}
	wantSeq(t, ev, `send:"go"`, "span:begin1", "event:x", "write:x", "span:end2", "span:end1", "exit")
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.exits) != 1 || ev.exits[0].status != 137 || ev.exits[0].at != time.Second {
		t.Errorf("exit events = %+v, want one (1s, 137)", ev.exits)
	}
	if got := h.proc.sent(); fmt.Sprint(got) != "[go]" {
		t.Errorf("sent = %q, want nothing after the error", got)
	}
	h.assertClean()
}

func TestDriverPollErrorAfterChildExitedReportsItsStatus(t *testing.T) {
	d := &fakeDriver{}
	d.fn = func(n int, now time.Duration, _ Screen) (Step, error) {
		if len(d.exits()) > 0 {
			return Step{}, errDriver // "child exited before match"
		}
		return Step{Deadline: time.Hour}, nil
	}
	d.open = []SpanOp{"end1"}
	h, ev := startDriverRun(t, d, nil)
	h.exitChild(4)
	r := h.wait()
	if !errors.Is(r.err, errDriver) {
		t.Fatalf("Run error = %v, want errDriver", r.err)
	}
	wantSeq(t, ev, "span:end1", "exit")
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.exits) != 1 || ev.exits[0].status != 4 {
		t.Errorf("exit events = %+v, want one with the child's status 4", ev.exits)
	}
	h.assertClean()
}

func TestDriverCancelClosesOpenSpansWithoutExit(t *testing.T) {
	d := steps(Step{Span: "begin1", Deadline: time.Hour})
	d.open = []SpanOp{"end1"}
	h, ev := startDriverRun(t, d, nil)
	h.polled(d, 1)
	h.clock.advance(time.Second)
	h.cancel()
	r := h.wait()
	if !errors.Is(r.err, context.Canceled) || r.code != -1 {
		t.Fatalf("Run = (%d, %v), want cancellation", r.code, r.err)
	}
	wantSeq(t, ev, "span:begin1", "span:end1")
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if ev.spans[1].at != time.Second {
		t.Errorf("span end at %v, want 1s", ev.spans[1].at)
	}
	h.assertClean()
}

func TestDriverWriteErrorAborts(t *testing.T) {
	werr := errors.New("input/output error")
	d := steps(Step{Write: []byte("a"), Deadline: time.Hour})
	h, _ := startDriverRun(t, d, func(h *harness) { h.proc.writeErr = werr })
	r := h.wait()
	if !errors.Is(r.err, werr) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want the write error", r.code, r.err)
	}
	h.assertClean()
}

func TestDriverEventsMarkErrorAborts(t *testing.T) {
	merr := errors.New("disk full")
	d := steps(Step{Mark: "m", Deadline: time.Hour})
	h, _ := startDriverRun(t, d, func(h *harness) { h.events.(*fakeEvents).markErr = merr })
	r := h.wait()
	if !errors.Is(r.err, merr) {
		t.Errorf("Run error = %v, want the events error", r.err)
	}
	h.assertClean()
}

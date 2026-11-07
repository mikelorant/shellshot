package session

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeEvents records every EventSink call, and the terminal writes, in one
// shared sequence.
type fakeEvents struct {
	mu      sync.Mutex
	seq     []string
	outs    []eventOutput
	exits   []eventExit
	resizes int
	failOn  int // 1-based Output call that fails; 0 never
	failErr error
	exitErr error
}

type eventOutput struct {
	at time.Duration
	p  []byte // retained deliberately to check chunks are not reused
}

type eventExit struct {
	at     time.Duration
	status int
}

func (e *fakeEvents) Output(at time.Duration, p []byte) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq = append(e.seq, "event:"+string(p))
	e.outs = append(e.outs, eventOutput{at, p})
	if e.failOn > 0 && len(e.outs) == e.failOn {
		return e.failErr
	}
	return nil
}

func (e *fakeEvents) Resize(time.Duration, int, int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.resizes++
	return nil
}

func (e *fakeEvents) Exit(at time.Duration, status int) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq = append(e.seq, "exit")
	e.exits = append(e.exits, eventExit{at, status})
	return e.exitErr
}

func (e *fakeEvents) written(p []byte) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.seq = append(e.seq, "write:"+string(p))
}

func (e *fakeEvents) sequence() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.seq...)
}

// startEventRun starts a run whose terminal writes are logged to ev.
func startEventRun(t *testing.T, ev *fakeEvents) *harness {
	t.Helper()
	return startRun(t, 0, nil, func(h *harness) {
		h.events = ev
		h.term.onWrite = ev.written
	})
}

func TestRunEventsOutputStampedWhenReadReturns(t *testing.T) {
	ev := &fakeEvents{}
	h := startEventRun(t, ev)
	h.nextFrame()

	h.clock.advance(150 * time.Millisecond)
	h.output("a")
	h.clock.advance(time.Second)
	h.output("b")
	h.clock.advance(250 * time.Millisecond)
	h.output("c")

	ev.mu.Lock()
	defer ev.mu.Unlock()
	want := []time.Duration{150 * time.Millisecond, 1150 * time.Millisecond, 1400 * time.Millisecond}
	if len(ev.outs) != len(want) {
		t.Fatalf("got %d output events, want %d", len(ev.outs), len(want))
	}
	for i, o := range ev.outs {
		if o.at != want[i] {
			t.Errorf("event %d at = %v, want %v", i, o.at, want[i])
		}
	}
	for i, p := range []string{"a", "b", "c"} {
		if string(ev.outs[i].p) != p {
			t.Errorf("event %d data = %q after later chunks, want %q", i, ev.outs[i].p, p)
		}
	}
}

func TestRunEventsStampIsReadTimeNotProcessingTime(t *testing.T) {
	ev := &fakeEvents{}
	h := startEventRun(t, ev)
	h.nextFrame()

	h.clock.advance(time.Second)
	h.output("a")
	h.clock.advance(time.Hour) // time passing after the read must not matter
	h.tick()
	h.settle()

	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.outs) != 1 || ev.outs[0].at != time.Second {
		t.Errorf("events = %+v, want one at 1s", ev.outs)
	}
}

func TestRunEventsLogPrecedesTerminalWrite(t *testing.T) {
	ev := &fakeEvents{}
	h := startEventRun(t, ev)
	h.nextFrame()

	h.output("a")
	h.output("b")

	want := []string{"event:a", "write:a", "event:b", "write:b"}
	got := ev.sequence()
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("sequence = %v, want %v", got, want)
	}
}

func TestRunEventsOutputErrorAbortsBeforeWrite(t *testing.T) {
	evErr := errors.New("log full")
	ev := &fakeEvents{failOn: 1, failErr: evErr}
	h := startEventRun(t, ev)
	h.nextFrame()

	h.send(h.proc.data, []byte("a"))
	r := h.wait()
	if !errors.Is(r.err, evErr) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, events error)", r.code, r.err)
	}
	if got := ev.sequence(); fmt.Sprint(got) != "[event:a]" {
		t.Errorf("sequence = %v, want only the failed event", got)
	}
	h.assertClean()
}

func TestRunEventsExitCarriesExitCodeAfterFinalFrame(t *testing.T) {
	ev := &fakeEvents{}
	h := startEventRun(t, ev)
	h.nextFrame()

	h.clock.advance(time.Second)
	h.output("bye")
	h.clock.advance(time.Second)
	h.proc.exit = 3
	close(h.proc.data)
	h.proc.Close()

	h.nextFrame() // final frame
	r := h.wait()
	if r.err != nil || r.code != 3 {
		t.Fatalf("Run = (%d, %v), want (3, nil)", r.code, r.err)
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if len(ev.exits) != 1 || ev.exits[0] != (eventExit{2 * time.Second, 3}) {
		t.Errorf("exits = %+v, want one {2s 3}", ev.exits)
	}
	if n := len(ev.seq); n != 3 || ev.seq[n-1] != "exit" {
		t.Errorf("sequence = %v, want exit last", ev.seq)
	}
}

func TestRunEventsExitErrorAborts(t *testing.T) {
	evErr := errors.New("close failed")
	ev := &fakeEvents{exitErr: evErr}
	h := startEventRun(t, ev)
	h.nextFrame()
	close(h.proc.data)
	h.proc.exitWith(0)
	r := h.wait()
	if !errors.Is(r.err, evErr) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, events error)", r.code, r.err)
	}
}

func TestRunEventsIdleEmitsNothing(t *testing.T) {
	ev := &fakeEvents{}
	h := startEventRun(t, ev)
	h.nextFrame()

	for i := 0; i < 5; i++ {
		h.clock.advance(time.Second)
		h.tick()
	}
	h.settle()
	if got := ev.sequence(); len(got) != 0 {
		t.Errorf("idle session produced events: %v", got)
	}
	ev.mu.Lock()
	defer ev.mu.Unlock()
	if ev.resizes != 0 {
		t.Errorf("Resize called %d times, want 0", ev.resizes)
	}
}

func TestRunEventsDeliveredOncePerChunk(t *testing.T) {
	ev := &fakeEvents{}
	h := startEventRun(t, ev)
	h.nextFrame()
	h.output("a")
	h.tick()
	h.settle()
	h.output("b")
	h.tick()
	h.settle()

	if got := ev.sequence(); fmt.Sprint(got) != "[event:a write:a event:b write:b]" {
		t.Errorf("sequence = %v", got)
	}
}

func TestRunNilEventsStillRuns(t *testing.T) {
	h := startRun(t, 0, nil, nil)
	h.nextFrame()
	h.output("x")
	close(h.proc.data)
	h.proc.exitWith(4)
	h.nextFrame()
	if r := h.wait(); r.err != nil || r.code != 4 {
		t.Errorf("Run = (%d, %v), want (4, nil)", r.code, r.err)
	}
}

func TestRunOnStartReceivesOriginBeforeOutput(t *testing.T) {
	starts := make(chan time.Time, 4)
	h := startRun(t, 0, nil, func(h *harness) {
		h.onStart = func(at time.Time) { starts <- at }
	})
	h.nextFrame()
	h.clock.advance(time.Second)
	h.output("a")
	close(starts)
	var got []time.Time
	for at := range starts {
		got = append(got, at)
	}
	if len(got) != 1 || !got[0].Equal(time.Unix(1000, 0)) {
		t.Errorf("OnStart calls = %v, want one at the clock origin", got)
	}
}

func TestRunOnTickCalledEveryTickWithClock(t *testing.T) {
	ticks := make(chan time.Time, 4)
	h := startRun(t, 0, nil, func(h *harness) {
		h.onTick = func(now time.Time) error { ticks <- now; return nil }
	})
	h.nextFrame()
	for i, want := range []time.Time{time.Unix(1001, 0), time.Unix(1002, 0)} {
		h.clock.advance(time.Second)
		h.tick()
		select {
		case got := <-ticks:
			if !got.Equal(want) {
				t.Errorf("OnTick %d = %v, want %v", i, got, want)
			}
		case <-time.After(testTimeout):
			t.Fatalf("OnTick %d not called", i)
		}
	}
}

func TestRunOnTickErrorAborts(t *testing.T) {
	tickErr := errors.New("flush failed")
	h := startRun(t, 0, nil, func(h *harness) {
		h.onTick = func(time.Time) error { return tickErr }
	})
	h.nextFrame()
	h.tick()
	r := h.wait()
	if !errors.Is(r.err, tickErr) || r.code != -1 {
		t.Errorf("Run = (%d, %v), want (-1, tick error)", r.code, r.err)
	}
	h.assertClean()
}

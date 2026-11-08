package run

import (
	"errors"
	"testing"
	"time"
)

// busySpan is a time when a command runs in the foreground: from, until to.
type busySpan struct{ from, to time.Duration }

// fgWorld is a screenSeq whose shell is in the foreground except during the
// busy spans. A non-nil err makes the foreground query fail.
type fgWorld struct {
	screenSeq
	busy []busySpan
	err  error
}

func (w *fgWorld) ShellForeground() (bool, error) {
	for _, b := range w.busy {
		if b.from <= w.now && w.now < b.to {
			return false, w.err
		}
	}
	return true, w.err
}

func doneOp(timeout time.Duration) OpWait {
	return OpWait{Meta: testMeta("s"), Kind: WaitDone, Timeout: timeout}
}

func TestWaitDoneEndsAfterTheDebounceOfSteadyForeground(t *testing.T) {
	tests := []struct {
		name    string
		busy    []busySpan
		timeout time.Duration
		wantAt  time.Duration
	}{
		{"shell already in the foreground still waits the debounce", nil, 10 * time.Second, ms(250)},
		{"command ends", []busySpan{{0, time.Second}}, 10 * time.Second, ms(1250)},
		{"command comes back during the debounce, as in a && chain",
			[]busySpan{{0, time.Second}, {ms(1100), 2 * time.Second}}, 10 * time.Second, ms(2250)},
		{"command comes back after the debounce is over",
			[]busySpan{{0, time.Second}, {ms(1300), 2 * time.Second}}, 10 * time.Second, ms(1250)},
		{"debounce ends exactly at the timeout", []busySpan{{0, ms(750)}}, time.Second, time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, se := runOps(t, &fgWorld{busy: tt.busy}, doneOp(tt.timeout), OpMark{Meta: testMeta(""), Label: "m"})
			if se != nil {
				t.Fatal(se)
			}
			if m, _ := lastEvent(ev, "mark"); m.At != tt.wantAt {
				t.Errorf("wait done ended at %v, want %v", m.At, tt.wantAt)
			}
		})
	}
}

func TestWaitDoneFailures(t *testing.T) {
	exitCode := 0
	tests := []struct {
		name     string
		world    *fgWorld
		timeout  time.Duration
		wantKind ErrorKind
		wantExit int
		wantMsg  string
		wantAt   time.Duration
	}{
		{
			name: "a program is still running at the timeout", world: &fgWorld{busy: []busySpan{{0, time.Hour}}},
			timeout: 3 * time.Minute, wantKind: WaitTimeout, wantExit: 3, wantAt: 3 * time.Minute,
			wantMsg: `demo.script:3:5: scene "s": wait done timed out after 3m0s: a program is still running in the foreground`,
		},
		{
			name: "the command ends too late for the debounce to finish", world: &fgWorld{busy: []busySpan{{0, ms(800)}}},
			timeout: time.Second, wantKind: WaitTimeout, wantExit: 3, wantAt: time.Second,
			wantMsg: `demo.script:3:5: scene "s": wait done timed out after 1s: a program is still running in the foreground`,
		},
		{
			name: "a timeout shorter than the debounce", world: &fgWorld{},
			timeout: ms(100), wantKind: WaitTimeout, wantExit: 3, wantAt: 0,
			wantMsg: `demo.script:3:5: scene "s": wait done needs a timeout of at least 250ms, not 100ms`,
		},
		{
			name: "the foreground cannot be queried", world: &fgWorld{busy: []busySpan{{0, time.Hour}}, err: errors.New("bad file descriptor")},
			timeout: 10 * time.Second, wantKind: ForegroundQuery, wantExit: 1, wantAt: 0,
			wantMsg: `demo.script:3:5: scene "s": cannot tell whether a command is running: bad file descriptor`,
		},
		{
			name: "the shell exits while the command runs", world: &fgWorld{busy: []busySpan{{0, time.Hour}}, screenSeq: screenSeq{exit: &exitCode, exitAt: ms(500)}},
			timeout: 10 * time.Second, wantKind: ChildExited, wantExit: 3, wantAt: ms(500),
			wantMsg: `demo.script:3:5: scene "s": child exited (status 0) before the command in the foreground finished`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ev, se := runOps(t, tt.world, doneOp(tt.timeout))
			if se == nil {
				t.Fatal("wait done passed, want a failure")
			}
			if se.Kind != tt.wantKind || se.ExitCode != tt.wantExit || se.Error() != tt.wantMsg {
				t.Errorf("got %v (%v, exit %d)\nwant %s (%v, exit %d)", se, se.Kind, se.ExitCode, tt.wantMsg, tt.wantKind, tt.wantExit)
			}
			if e, _ := lastEvent(ev, "error"); e.At != tt.wantAt {
				t.Errorf("failed at %v, want %v", e.At, tt.wantAt)
			}
		})
	}
}

// termScreen is a Screen whose foreground the test sets.
type termScreen struct {
	textScreen
	foreground bool
}

func (s termScreen) ShellForeground() (bool, error) { return s.foreground, nil }

func TestWaitDoneAsksToBePolledSoonButNeverAtOnce(t *testing.T) {
	ex := mustExec(t, fakeKeys{}, doneOp(time.Minute))
	tests := []struct {
		now          time.Duration
		foreground   bool
		wantDeadline time.Duration
	}{
		{0, false, DonePollInterval},
		{ms(50), true, ms(100)},   // the count starts at 50ms
		{ms(100), true, ms(150)},  // the next look is due before the end of the debounce
		{ms(280), true, ms(300)},  // the debounce ends at 300ms
		{ms(290), false, ms(340)}, // the command is back: the count starts again later
	}
	for _, tt := range tests {
		step, err := ex.Poll(tt.now, termScreen{foreground: tt.foreground})
		if err != nil || step.Done || step.Deadline != tt.wantDeadline {
			t.Fatalf("Poll(%v, foreground=%v) = %+v, %v; want a Deadline of %v", tt.now, tt.foreground, step, err, tt.wantDeadline)
		}
	}
}

func TestWaitDoneLimitsTheDeadlineToTheTimeout(t *testing.T) {
	ex := mustExec(t, fakeKeys{}, doneOp(ms(300)))
	_, _ = ex.Poll(0, termScreen{})
	if step, err := ex.Poll(ms(280), termScreen{}); err != nil || step.Deadline != ms(300) {
		t.Fatalf("Poll = %+v, %v; want a Deadline of 300ms", step, err)
	}
}

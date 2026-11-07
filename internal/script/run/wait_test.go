package run

import (
	"errors"
	"testing"
	"time"
)

func ms(n int) time.Duration { return time.Duration(n) * time.Millisecond }

// runOps drives hand-built ops against a screen sequence and returns the
// timeline and the script error, if any.
func runOps(t *testing.T, w world, ops ...Op) ([]Event, *ScriptError) {
	t.Helper()
	ev, err := runWorld(t, &Plan{Process: Process{Seed: 1}, Ops: ops}, w, Deps{File: "demo.script"})
	if err == nil {
		return ev, nil
	}
	var se *ScriptError
	if !errors.As(err, &se) {
		t.Fatalf("error %v is not a *ScriptError", err)
	}
	return ev, se
}

func lastEvent(ev []Event, kind string) (Event, bool) {
	for i := len(ev) - 1; i >= 0; i-- {
		if ev[i].Kind == kind {
			return ev[i], true
		}
	}
	return Event{}, false
}

func TestWaitLiteralMatchesWhenTheTextAppears(t *testing.T) {
	w := &screenSeq{changes: []seqChange{{ms(300), "building"}, {ms(900), "building\nall passed"}}}
	ev, se := runOps(t, w,
		OpWait{Meta: testMeta("s"), Kind: WaitLiteral, Pattern: "passed", Timeout: 5 * time.Second},
		OpMark{Meta: testMeta("s"), Label: "after"})
	if se != nil {
		t.Fatal(se)
	}
	if m, _ := lastEvent(ev, "mark"); m.At != ms(900) {
		t.Errorf("wait ended at %v, want 900ms", m.At)
	}
}

func TestWaitAlreadyMatchingReturnsAtOnce(t *testing.T) {
	w := &screenSeq{text: "ready"}
	ev, se := runOps(t, w,
		OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "ready", Timeout: 5 * time.Second},
		OpMark{Meta: testMeta(""), Label: "m"})
	if m, _ := lastEvent(ev, "mark"); se != nil || m.At != 0 {
		t.Errorf("mark at %v, error %v; want 0, nil", m.At, se)
	}
}

func TestWaitLiteralMatchesAcrossRows(t *testing.T) {
	w := &screenSeq{text: "one\ntwo"}
	if _, se := runOps(t, w, OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "one\ntwo", Assert: true}); se != nil {
		t.Error(se)
	}
}

func TestWaitRegexIsMultiline(t *testing.T) {
	for _, tc := range []struct {
		pattern, screen string
		match           bool
	}{
		{`^\$$`, "output\n$", true},
		{`^\$$`, "$ echo $", false}, // the echoed command line does not start a bare prompt
		{`^done$`, "x\ndone\ny", true},
		{`v[0-9]+\.[0-9]+`, "app v1.20", true},
		{`^pass`, "all pass", false},
	} {
		_, se := runOps(t, &screenSeq{text: tc.screen}, OpWait{Meta: testMeta(""), Kind: WaitRegex, Pattern: tc.pattern, Assert: true})
		if (se == nil) != tc.match {
			t.Errorf("/%s/ on %q: error %v, want match=%v", tc.pattern, tc.screen, se, tc.match)
		}
	}
}

func TestWaitTimeoutError(t *testing.T) {
	for _, tc := range []struct {
		name string
		op   OpWait
		want string
	}{
		{"literal", OpWait{Kind: WaitLiteral, Pattern: "passed", Timeout: 15 * time.Second},
			`demo.script:3:5: scene "build": wait timed out after 15s waiting for "passed"`},
		{"regex", OpWait{Kind: WaitRegex, Pattern: `^ok$`, Timeout: 1500 * time.Millisecond},
			`demo.script:3:5: scene "build": wait timed out after 1.5s waiting for /^ok$/`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.op.Meta = testMeta("build")
			ev, se := runOps(t, &screenSeq{text: "no"}, tc.op)
			if se == nil {
				t.Fatal("wait did not fail")
			}
			if se.Kind != WaitTimeout || se.ExitCode != 3 || se.Error() != tc.want {
				t.Errorf("got %v kind %v exit %d\nwant %s", se, se.Kind, se.ExitCode, tc.want)
			}
			if e, _ := lastEvent(ev, "error"); e.At != tc.op.Timeout {
				t.Errorf("failed at %v, want the timeout %v", e.At, tc.op.Timeout)
			}
		})
	}
}

func TestWaitMatchingAtTheLastMomentPasses(t *testing.T) {
	w := &screenSeq{changes: []seqChange{{time.Second, "ok"}}}
	if _, se := runOps(t, w, OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "ok", Timeout: time.Second}); se != nil {
		t.Errorf("text arriving exactly at the timeout failed: %v", se)
	}
}

func TestAssertionChecksOnceAndFailsAtOnce(t *testing.T) {
	w := &screenSeq{changes: []seqChange{{ms(10), "passed"}}}
	ev, se := runOps(t, w, OpWait{Meta: testMeta("check"), Kind: WaitLiteral, Pattern: "passed", Assert: true})
	if se == nil || se.Kind != WaitTimeout || se.ExitCode != 3 {
		t.Fatalf("got %v, want a WaitTimeout failure", se)
	}
	want := `demo.script:3:5: scene "check": assertion failed: "passed" is not on the screen`
	if se.Error() != want {
		t.Errorf("message %q, want %q", se.Error(), want)
	}
	if e, _ := lastEvent(ev, "error"); e.At != 0 {
		t.Errorf("assertion failed at %v, want 0", e.At)
	}
	if _, se := runOps(t, &screenSeq{text: "passed"}, OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "passed", Assert: true}); se != nil {
		t.Errorf("matching assertion failed: %v", se)
	}
}

// The echo trap: the terminal echoes typed text, so an unanchored wait can
// pass on the echoed command line. The Executor does not guard against it;
// anchoring does.
func TestEchoTrapIsAScriptConcernAnchoringAvoidsIt(t *testing.T) {
	echoed := "$ echo BUILD-OK"
	if _, se := runOps(t, &screenSeq{text: echoed}, OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "BUILD-OK", Assert: true}); se != nil {
		t.Errorf("an unanchored literal should match the echoed line: %v", se)
	}
	if _, se := runOps(t, &screenSeq{text: echoed}, OpWait{Meta: testMeta(""), Kind: WaitRegex, Pattern: `^BUILD-OK$`, Assert: true}); se == nil {
		t.Error("an anchored regex must not match the echoed line")
	}
	if _, se := runOps(t, &screenSeq{text: "$ echo BUILD-O\"K\"\nBUILD-OK"}, OpWait{Meta: testMeta(""), Kind: WaitRegex, Pattern: `^BUILD-OK$`, Assert: true}); se != nil {
		t.Errorf("the output line should match: %v", se)
	}
}

func TestWaitIdleSucceedsAfterQuietAndRestartsOnChange(t *testing.T) {
	w := &screenSeq{changes: []seqChange{{ms(100), "a"}, {ms(400), "ab"}}}
	ev, se := runOps(t, w,
		OpWait{Meta: testMeta(""), Kind: WaitIdle, Quiet: ms(500), Timeout: 5 * time.Second},
		OpMark{Meta: testMeta(""), Label: "m"})
	if se != nil {
		t.Fatal(se)
	}
	if m, _ := lastEvent(ev, "mark"); m.At != ms(900) {
		t.Errorf("idle ended at %v, want 900ms (500ms after the change at 400ms)", m.At)
	}
}

func TestWaitIdleTimeoutIsASettleFailure(t *testing.T) {
	var changes []seqChange
	for i := 1; i < 100; i++ {
		changes = append(changes, seqChange{ms(i * 100), string(rune('a' + i%26))})
	}
	_, se := runOps(t, &screenSeq{changes: changes},
		OpWait{Meta: testMeta("s"), Kind: WaitIdle, Quiet: ms(300), Timeout: 2 * time.Second})
	want := `demo.script:3:5: scene "s": wait idle did not settle: the screen was still changing after 2s (timeout); wait for a pattern instead, or raise "timeout"`
	if se == nil || se.Kind != SettleTimeout || se.ExitCode != 3 || se.Error() != want {
		t.Errorf("got %v\nwant %s", se, want)
	}
}

func settleOp(m Meta, quiet, timeout time.Duration) OpSettle {
	return OpSettle{Meta: m, Quiet: quiet, Timeout: timeout}
}

func TestSettleSucceedsOnceTheScreenIsQuiet(t *testing.T) {
	w := &screenSeq{changes: []seqChange{{ms(50), "a"}, {ms(150), "ab"}}}
	ev, se := runOps(t, w, settleOp(testMeta(""), ms(200), 5*time.Second), OpMark{Meta: testMeta(""), Label: "m"})
	if m, _ := lastEvent(ev, "mark"); se != nil || m.At != ms(350) {
		t.Errorf("settled at %v (error %v), want 350ms", m.At, se)
	}
}

func TestSilentLongCommandSettlesAtOnce(t *testing.T) {
	ev, se := runOps(t, &screenSeq{text: "$ sleep 30"}, settleOp(testMeta(""), ms(200), 15*time.Second), OpMark{Meta: testMeta(""), Label: "m"})
	if m, _ := lastEvent(ev, "mark"); se != nil || m.At != ms(200) {
		t.Errorf("settled at %v (error %v), want 200ms", m.At, se)
	}
}

func TestSettleTimeoutFailsWithTheDocumentedMessage(t *testing.T) {
	var changes []seqChange
	for i := 1; i < 400; i++ {
		changes = append(changes, seqChange{ms(i * 100), string(rune('a' + i%26))})
	}
	ev, se := runOps(t, &screenSeq{changes: changes}, settleOp(Meta{Scene: "build", Pos: testMeta("").Pos}, ms(200), 15*time.Second))
	want := `demo.script:3:5: scene "build": run did not settle: the screen was still changing after 15s (timeout); ` +
		`for continuous output use "settle off" and "wait PATTERN TIMEOUT", or raise "timeout"`
	if se == nil || se.Kind != SettleTimeout || se.ExitCode != 3 || se.Error() != want {
		t.Fatalf("got %v\nwant %s", se, want)
	}
	if e, _ := lastEvent(ev, "error"); e.At != 15*time.Second {
		t.Errorf("failed at %v, want 15s", e.At)
	}
}

func TestSettleErrorNamesHiddenAndTopLevel(t *testing.T) {
	busy := func() world {
		var changes []seqChange
		for i := 1; i < 100; i++ {
			changes = append(changes, seqChange{ms(i * 100), string(rune('a' + i%26))})
		}
		return &screenSeq{changes: changes}
	}
	m := testMeta("")
	_, se := runOps(t, busy(), settleOp(m, ms(200), time.Second))
	if want := `demo.script:3:5: run did not settle`; se == nil || se.Error()[:len(want)] != want {
		t.Errorf("top level: %v", se)
	}
	m.Hidden = true
	_, se = runOps(t, busy(), settleOp(m, ms(200), time.Second))
	if want := `demo.script:3:5: hidden: run did not settle`; se == nil || se.Error()[:len(want)] != want {
		t.Errorf("hidden: %v", se)
	}
}

func TestSettleOffIsSkipped(t *testing.T) {
	ev, se := runOps(t, &screenSeq{text: "x"}, OpSettle{Meta: testMeta(""), Off: true, Timeout: time.Second}, OpMark{Meta: testMeta(""), Label: "m"})
	if m, _ := lastEvent(ev, "mark"); se != nil || m.At != 0 {
		t.Errorf("settle off took %v (error %v)", m.At, se)
	}
}

func TestSettleDeadlinesFollowTheScreen(t *testing.T) {
	ex := mustExec(t, nil, settleOp(testMeta(""), ms(200), 10*time.Second))
	step, _ := ex.Poll(0, textScreen("a"))
	if step.Deadline != ms(200) {
		t.Errorf("deadline %v, want 200ms", step.Deadline)
	}
	step, _ = ex.Poll(ms(150), textScreen("ab"))
	if step.Deadline != ms(350) {
		t.Errorf("deadline after a change %v, want 350ms", step.Deadline)
	}
	same, _ := ex.Poll(ms(200), textScreen("ab"))
	if same.Deadline != ms(350) {
		t.Errorf("deadline with no new information %v, want 350ms", same.Deadline)
	}
}

func TestSettleDeadlineIsCappedByTheTimeout(t *testing.T) {
	ex := mustExec(t, nil, settleOp(testMeta(""), ms(200), ms(250)))
	ex.Poll(0, textScreen("a"))
	step, _ := ex.Poll(ms(100), textScreen("ab"))
	if step.Deadline != ms(250) {
		t.Errorf("deadline %v, want the timeout 250ms", step.Deadline)
	}
}

func TestDrainCarriesOnAtItsCap(t *testing.T) {
	var changes []seqChange
	for i := 1; i < 100; i++ {
		changes = append(changes, seqChange{ms(i * 100), string(rune('a' + i%26))})
	}
	m := Meta{Hidden: true}
	ev, se := runOps(t, &screenSeq{changes: changes},
		OpSettle{Meta: m, Quiet: ms(200), Timeout: DrainTimeout, Drain: true}, OpMark{Meta: m, Label: "m"})
	if mk, _ := lastEvent(ev, "mark"); se != nil || mk.At != DrainTimeout {
		t.Errorf("drain ended at %v (error %v), want %v and no error", mk.At, se, DrainTimeout)
	}
}

func TestChildExitedDuringWaitFails(t *testing.T) {
	st := 2
	w := &screenSeq{text: "bye", exit: &st, exitAt: ms(400)}
	ev, se := runOps(t, w, OpWait{Meta: testMeta("s"), Kind: WaitLiteral, Pattern: "never", Timeout: 15 * time.Second})
	want := `demo.script:3:5: scene "s": child exited (status 2) before match: waiting for "never"`
	if se == nil || se.Kind != ChildExited || se.ExitCode != 3 || se.Error() != want {
		t.Fatalf("got %v\nwant %s", se, want)
	}
	if e, _ := lastEvent(ev, "error"); e.At != ms(400) {
		t.Errorf("failed at %v, want 400ms (at the exit, not the timeout)", e.At)
	}
}

func TestChildExitedWaitStillPassesOnTheFinalScreen(t *testing.T) {
	st := 0
	w := &screenSeq{changes: []seqChange{{ms(100), "goodbye"}}, exit: &st, exitAt: ms(100)}
	_, se := runOps(t, w, OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "goodbye", Timeout: 15 * time.Second})
	if se != nil {
		t.Errorf("a wait matching the final screen failed: %v", se)
	}
}

func TestChildExitedBlocksTypeAndPressButNotSleepMarkSettle(t *testing.T) {
	ex := mustExec(t, nil,
		OpSleep{Meta: testMeta(""), Duration: ms(10)},
		settleOp(testMeta(""), ms(10), time.Second),
		OpMark{Meta: testMeta(""), Label: "m"},
		OpType{Meta: testMeta("s"), Text: "hello world", PerChar: ms(5)})
	ex.ChildExited(0, 7)
	var err error
	var marks int
	for now := time.Duration(0); err == nil && now < time.Second; now += ms(5) {
		var step Step
		step, err = ex.Poll(now, textScreen(""))
		if step.Mark != "" {
			marks++
		}
	}
	var se *ScriptError
	if !errors.As(err, &se) || se.Kind != ChildExited || se.ExitCode != 3 {
		t.Fatalf("got %v, want ChildExited", err)
	}
	if marks != 1 {
		t.Errorf("mark ran %d times, want 1", marks)
	}
	want := `t.script:3:5: scene "s": child exited (status 7) before the script could type "hello world"`
	if se.Error() != want {
		t.Errorf("message %q\nwant %q", se.Error(), want)
	}
}

func TestExitSequenceShutsDownAndToleratesExit(t *testing.T) {
	m := Meta{Hidden: true}
	// The child exits 100ms after the shutdown request.
	st := 0
	ev, se := runOps(t, &screenSeq{exit: &st, exitAt: ms(100)}, OpExit{Meta: m, Grace: ExitGrace}, OpMark{Meta: m, Label: "m"})
	if mk, _ := lastEvent(ev, "mark"); se != nil || mk.At != ms(100) {
		t.Errorf("exit sequence took until %v (error %v), want 100ms", mk.At, se)
	}
	// A child that is already gone gets no shutdown.
	ex := mustExec(t, nil, OpExit{Meta: m, Grace: ExitGrace})
	ex.ChildExited(0, 1)
	step, _ := ex.Poll(0, textScreen(""))
	if step.Shutdown || !step.Done {
		t.Errorf("step %+v, want done without shutdown", step)
	}
}

func TestExitGraceEndsTheScriptWithTheChildStillRunning(t *testing.T) {
	m := Meta{Hidden: true}
	ev, se := runOps(t, &screenSeq{}, OpExit{Meta: m, Grace: ExitGrace}, OpMark{Meta: m, Label: "m"})
	if mk, _ := lastEvent(ev, "mark"); se != nil || mk.At != ExitGrace {
		t.Errorf("grace ended at %v (error %v), want %v", mk.At, se, ExitGrace)
	}
}

func TestExecutorNeverSchedulesInThePastWithWaits(t *testing.T) {
	// A hostile mix of waits, settles and screens; runWorld checks Deadline >= now.
	st := 0
	w := &screenSeq{changes: []seqChange{{ms(7), "a"}, {ms(13), "b"}, {ms(900), "c"}}, exit: &st, exitAt: 2 * time.Second}
	runOps(t, w,
		OpWait{Meta: testMeta(""), Kind: WaitIdle, Quiet: ms(100), Timeout: time.Second},
		settleOp(testMeta(""), ms(50), time.Second),
		OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "c", Timeout: 5 * time.Second},
		OpWait{Meta: testMeta(""), Kind: WaitLiteral, Pattern: "z", Timeout: 5 * time.Second})
}

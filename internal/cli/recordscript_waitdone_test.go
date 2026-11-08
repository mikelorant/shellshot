package cli

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// doneShells are the shells the wait done tests run in; a shell that is not
// installed is skipped. The shells start without startup files.
func doneShells(t *testing.T) map[string]string {
	t.Helper()
	shells := map[string]string{"sh": "/bin/sh"}
	if path, err := exec.LookPath("bash"); err == nil {
		shells["bash"] = path + " --norc"
	}
	return shells
}

// The first wait holds the script until the shell has drawn its prompt (see
// TestRecordScriptSettleTimeoutFails). "END""ED" prints ENDED but echoes
// differently, so ENDED in the recording is the output of the command and
// nothing else. Without wait done, settle off would type the next command at
// once, and the terminal would echo NEXT before ENDED.
func TestRecordScriptWaitDoneHoldsTheScriptUntilTheCommandEnds(t *testing.T) {
	t.Parallel()
	for name, shell := range doneShells(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := runScriptCLIWith(t, context.Background(), scriptPreludeFor(shell)+`
wait "$" 20s
settle off
run sleep 2; echo END""ED
wait done 20s
run echo NEXT
`)
			if r.code != 0 {
				t.Fatalf("exit code = %d, want 0\nstderr: %s", r.code, r.stderr)
			}
			s := readShot(t, r.shot)
			out := s.out()
			ended, typed := strings.Index(out, "ENDED"), strings.Index(out, "NEXT")
			if ended < 0 || typed < 0 {
				t.Fatalf("OUT lacks the output of the first command or the second command:\n%q", out)
			}
			if typed < ended {
				t.Errorf("the second command was typed before the first one ended:\n%q", out)
			}
			if got := s.exitStatus(t); got != 0 {
				t.Errorf("EXIT status = %d, want 0", got)
			}
			if r.took > 25*time.Second {
				t.Errorf("took %v, want a few seconds", r.took)
			}
		})
	}
}

func TestRecordScriptWaitDoneTimeoutKeepsRecording(t *testing.T) {
	t.Parallel()
	for name, shell := range doneShells(t) {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			r := runScriptCLIWith(t, context.Background(), scriptPreludeFor(shell)+`
wait "$" 20s
timeout 2s
settle off
run sleep 30
wait done
`)
			if r.code != 3 {
				t.Fatalf("exit code = %d, want 3\nstderr: %s", r.code, r.stderr)
			}
			if r.took > 20*time.Second {
				t.Errorf("took %v, want about 2 s: the running command must not be waited for", r.took)
			}
			for _, want := range []string{
				"wait done timed out after 2s: a program is still running in the foreground",
				"; partial recording kept at " + r.shot,
			} {
				if !strings.Contains(r.stderr, want) {
					t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
				}
			}
			s := readShot(t, r.shot)
			if !strings.Contains(s.out(), "sleep 30") {
				t.Errorf("OUT = %q, want the echo of the command", s.out())
			}
			_ = s.exitStatus(t) // the child was closed and its exit recorded
			if code, stdout, _ := runCLI(t, "info", r.shot); code != 0 || !strings.Contains(stdout, "status        complete") {
				t.Errorf("info exit code = %d, want 0 and a complete recording:\n%s", code, stdout)
			}
		})
	}
}

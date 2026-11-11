package cli

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/frames"
)

// byeShell is a shell that reads one line at a time and prints BYE-MARKER when
// the terminal sends it end of file (Ctrl-D at an empty line). It stands in
// for a shell that prints something while it exits ("exit" for bash, "^D" for
// others).
const byeShell = `shell /bin/sh -c "while read l; do $l; done; echo BYE-MARKER"` + "\n"

// finalScreen replays the recording at path and returns the text of its last
// frame: the screen an export ends on.
func finalScreen(t *testing.T, path string) string {
	t.Helper()
	it, _, closeAll, err := openFrames(path, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer closeAll()
	all, err := frames.Collect(it)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("recording has no frames")
	}
	return all[len(all)-1].Snapshot.Text()
}

// What the shell prints while the script exits it is not recorded, but the
// last step's output is, and the recording still ends with an EXIT event.
func TestRecordScriptDoesNotRecordTheExit(t *testing.T) {
	t.Parallel()
	r := runScriptCLIWith(t, context.Background(), "size 60x8\ntyping 5ms\n"+byeShell+"run echo hello\n", "--record-input")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	s := readShot(t, r.shot)
	if s.err != nil && s.err.Error() != "EOF" {
		t.Errorf("recording ended with %v", s.err)
	}
	if strings.Contains(s.out(), "BYE-MARKER") {
		t.Errorf("OUT has what the shell printed while it exited:\n%q", s.out())
	}
	if strings.Contains(s.in(), "\x04") {
		t.Errorf("IN has the Ctrl-D of the exit: %q", s.in())
	}
	if !strings.Contains(s.in(), "echo hello") {
		t.Errorf("IN = %q, want the typed command (input is recorded until the exit)", s.in())
	}
	screen := finalScreen(t, r.shot)
	if !strings.Contains(screen, "hello") || strings.Contains(screen, "BYE-MARKER") {
		t.Errorf("final screen should end on the last step's output, not on the exit:\n%s", screen)
	}
	if got := s.exitStatus(t); got != 0 {
		t.Errorf("EXIT status = %d, want 0", got)
	}
	if s.count(event.KindExit) != 1 {
		t.Errorf("%d EXIT events, want 1", s.count(event.KindExit))
	}
	if len(s.spans) != 0 {
		t.Errorf("spans = %v, want none: the exit is not a span", spanNames(s.spans))
	}

	code, stdout, stderr := runCLI(t, "info", r.shot)
	if code != 0 || !strings.Contains(stdout, "status        complete") || strings.Contains(stdout, "hidden") {
		t.Errorf("info exit code = %d, stderr %q, stdout:\n%s\nwant a complete recording with no hidden span", code, stderr, stdout)
	}
}

// The same with a real shell, which prints "exit" when it reads Ctrl-D at an
// empty prompt.
func TestRecordScriptBashExitIsNotOnTheLastFrame(t *testing.T) {
	t.Parallel()
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	r := runScriptCLIWith(t, context.Background(), "env PS1=\"$ \"\nsize 60x8\ntyping 5ms\nshell "+bash+" --norc --noprofile -i\nwait \"$\" 20s\nrun echo hello\n")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	s := readShot(t, r.shot)
	if strings.Contains(s.out(), "exit") {
		t.Errorf("OUT has the exit that bash printed:\n%q", s.out())
	}
	screen := finalScreen(t, r.shot)
	if !strings.Contains(screen, "hello") || strings.Contains(screen, "exit") {
		t.Errorf("final screen should end on the last step's output:\n%s", screen)
	}
	_ = s.exitStatus(t)
}

// A script with a hidden block of its own still has that span, and only that
// one.
func TestRecordScriptWrittenHiddenBlockIsTheOnlySpan(t *testing.T) {
	t.Parallel()
	r := runScriptCLIWith(t, context.Background(), "size 60x8\ntyping 5ms\n"+byeShell+"hidden {\n  run echo tidy-up\n}\nrun echo hello\n")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	s := readShot(t, r.shot)
	if got := spanNames(s.spans); len(got) != 1 || got[0] != "hidden" || s.spans[0].Open {
		t.Errorf("spans = %v, want one closed hidden span", got)
	}
	code, stdout, stderr := runCLI(t, "info", r.shot)
	if code != 0 || !strings.Contains(stdout, "hidden        1 span, ") {
		t.Errorf("info exit code = %d, stderr %q, stdout:\n%s\nwant one hidden span", code, stderr, stdout)
	}
}

// A recording made before the exit was left out of the recording has the
// shell's exit text in a trailing hidden span. A replay of it is what it
// always was: the span is counted as cut, and the screen it left is the last
// frame.
func TestReplayOfRecordingWithImplicitExitSpanIsUnchanged(t *testing.T) {
	t.Parallel()
	path := spanRecording(t, func(w *bin.Writer) {
		out(t, w, 100, "$ ")
		out(t, w, 200, "echo hi\r\nhi\r\n$ ")
		span(t, w, 300, spanBegin(1, event.SpanHidden, 0, "", event.SpanHints{}))
		out(t, w, 400, "exit\r\n")
		span(t, w, 500, spanEnd(1))
		must(t, w.Exit(ms(600), 0))
	})
	run := func(args ...string) (stdout, stderr string) {
		t.Helper()
		code, stdout, stderr := runReplayCmd(t, append([]string{path, "--speed", "1000", "--fps", "10", "--clear", "--keep-leading-blank"}, args...)...)
		if code != 0 {
			t.Fatalf("replay %v: exit %d, stderr %q", args, code, stderr)
		}
		return stdout, stderr
	}
	cut, stderr := run()
	if n := strings.Count(cut, frameStart); n != 4 {
		t.Errorf("default replay drew %d frames, want 4", n)
	}
	if !strings.Contains(cut, "$ exit") {
		t.Errorf("the default replay does not end on the exit text: %q", cut)
	}
	if !strings.Contains(stderr, "hidden       1 span cut, ") {
		t.Errorf("summary lacks the cut span:\n%s", stderr)
	}
	shown, _ := run("--show-hidden")
	if n := strings.Count(shown, frameStart); n != 4 {
		t.Errorf("--show-hidden drew %d frames, want 4", n)
	}
}

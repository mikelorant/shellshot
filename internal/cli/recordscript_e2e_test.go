package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/recording/spans"
)

// scriptPrelude makes the shell a plain /bin/sh with a short prompt, so the
// scripts below do not depend on the user's shell or its startup files.
const scriptPrelude = "shell /bin/sh\nenv PS1=\"$ \"\nsize 60x8\ntyping 5ms\n"

// scriptRun is the result of runScriptCLI.
type scriptRun struct {
	code           int
	stdout, stderr string
	shot           string // the recording path
	took           time.Duration
}

// runScriptCLI writes src (after scriptPrelude) to a file, runs "record
// --script" on it with extra args and the recording at a temporary -o path, and
// fails the test if it takes more than 30 s.
func runScriptCLI(t *testing.T, src string, args ...string) scriptRun {
	t.Helper()
	return runScriptCLIContext(t, context.Background(), src, args...)
}

func runScriptCLIContext(t *testing.T, parent context.Context, src string, args ...string) scriptRun {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.script")
	if err := os.WriteFile(file, []byte(scriptPrelude+src), 0o644); err != nil {
		t.Fatal(err)
	}
	r := scriptRun{shot: filepath.Join(dir, "demo.shot")}
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	var out, errOut bytes.Buffer
	cli := append([]string{"record", "--script", file, "-o", r.shot}, args...)
	start := time.Now()
	r.code = runWithStdin(ctx, cli, strings.NewReader(""), &out, &errOut)
	r.took = time.Since(start)
	r.stdout, r.stderr = out.String(), errOut.String()
	return r
}

// shot is a recording read back with its meta and spans.
type shot struct {
	recorded
	meta  map[string]string
	spans []spans.Span
}

func readShot(t *testing.T, path string) shot {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recording: %v", err)
	}
	s := shot{recorded: readRecording(t, data)}
	r, err := bin.NewReader(bytes.NewReader(data), bin.ReaderOptions{CopyData: true})
	if err != nil {
		t.Fatal(err)
	}
	s.meta = r.Meta()
	r2, err := bin.NewReader(bytes.NewReader(data), bin.ReaderOptions{CopyData: true})
	if err != nil {
		t.Fatal(err)
	}
	if s.spans, err = spans.Scan(r2); err != nil {
		t.Fatalf("spans.Scan: %v", err)
	}
	return s
}

// exitStatus returns the status of the EXIT event, or fails the test.
func (s shot) exitStatus(t *testing.T) int64 {
	t.Helper()
	for _, e := range s.events {
		if e.Kind == event.KindExit {
			status, err := event.ParseExit(e.Data)
			if err != nil {
				t.Fatal(err)
			}
			return status
		}
	}
	t.Fatal("recording has no EXIT event")
	return 0
}

// spanNames lists the spans as "scene:NAME" and "hidden", in begin order.
func spanNames(sp []spans.Span) []string {
	var out []string
	for _, s := range sp {
		if s.Type == event.SpanScene {
			out = append(out, "scene:"+s.Name)
		} else {
			out = append(out, "hidden")
		}
	}
	return out
}

func TestRecordScriptEndToEnd(t *testing.T) {
	t.Parallel()
	r := runScriptCLI(t, `
scene hello {
  run echo hello
  type echo ke
  press y
  press enter
  wait /^key$/ 5s
  mark checked
}

hidden {
  run echo tidy-up
}
`)
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want nothing without --watch", r.stdout)
	}
	if !strings.Contains(r.stderr, "recorded ") {
		t.Errorf("stderr = %q, want the recorded summary", r.stderr)
	}
	s := readShot(t, r.shot)
	if s.err != nil && s.err.Error() != "EOF" {
		t.Errorf("recording ended with %v", s.err)
	}
	if want := []string{"scene:hello", "hidden", "hidden"}; !reflect.DeepEqual(spanNames(s.spans), want) {
		t.Errorf("spans = %v, want %v (the last is the implicit one)", spanNames(s.spans), want)
	}
	for _, sp := range s.spans {
		if sp.Open {
			t.Errorf("span %d %q is open", sp.ID, sp.Name)
		}
	}
	for _, want := range []string{"hello", "key", "tidy-up"} {
		if !strings.Contains(s.out(), want) {
			t.Errorf("OUT lacks %q:\n%q", want, s.out())
		}
	}
	if got := s.exitStatus(t); got != 0 {
		t.Errorf("EXIT status = %d, want 0", got)
	}
	if s.head.Cols != 60 || s.head.Rows != 8 {
		t.Errorf("size = %dx%d, want 60x8", s.head.Cols, s.head.Rows)
	}
	if !reflect.DeepEqual(s.head.Argv, []string{"/bin/sh"}) {
		t.Errorf("argv = %q, want [/bin/sh]", s.head.Argv)
	}
	if s.meta["script"] != "demo.script" {
		t.Errorf("meta script = %q, want demo.script", s.meta["script"])
	}
	if s.count(event.KindMark) != 1 || s.count(event.KindIn) != 0 {
		t.Errorf("MARK = %d, IN = %d, want 1 and 0 (input is not recorded by default)", s.count(event.KindMark), s.count(event.KindIn))
	}

	code, stdout, stderr := runCLI(t, "info", r.shot)
	if code != 0 || !strings.Contains(stdout, "status        complete") || !strings.Contains(stdout, "1 hello") {
		t.Errorf("info exit code = %d, stderr %q, stdout:\n%s\nwant status complete and scene hello", code, stderr, stdout)
	}
}

func TestRecordScriptNamedHiddenBlock(t *testing.T) {
	t.Parallel()
	r := runScriptCLI(t, `
hidden setup {
  run echo preparing
}

scene hello {
  run echo shown
}
`)
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	s := readShot(t, r.shot)
	var named []string
	for _, sp := range s.spans {
		if sp.Type == event.SpanHidden && sp.Name != "" {
			named = append(named, sp.Name)
		}
	}
	if want := []string{"setup"}; !reflect.DeepEqual(named, want) {
		t.Errorf("named hidden spans = %v, want %v (the implicit exit span has no name)", named, want)
	}
	code, stdout, stderr := runCLI(t, "info", r.shot)
	if code != 0 || stderr != "" {
		t.Fatalf("info exit code = %d, stderr %q", code, stderr)
	}
	if !strings.Contains(stdout, "hidden        2 spans, ") || !strings.Contains(stdout, "\n  1 setup  0:") {
		t.Errorf("info lacks the hidden span named setup:\n%s", stdout)
	}
}

func TestRecordScriptExportsWithSpans(t *testing.T) {
	t.Parallel()
	r := runScriptCLI(t, `
scene a {
  speed 4x
  run echo first
}
scene b {
  run echo second
}
`)
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.stderr)
	}
	png := filepath.Join(filepath.Dir(r.shot), "out.png")
	if code, _, stderr := runCLI(t, "export", r.shot, "-o", png); code != 0 {
		t.Fatalf("export exit code = %d: %s", code, stderr)
	}
	if fi, err := os.Stat(png); err != nil || fi.Size() == 0 {
		t.Errorf("export produced no image: %v", err)
	}
}

func TestRecordScriptWatch(t *testing.T) {
	t.Parallel()
	src := "run echo watched-output\n"
	if r := runScriptCLI(t, src); strings.Contains(r.stdout, "watched-output") {
		t.Errorf("without --watch stdout = %q, want nothing", r.stdout)
	}
	r := runScriptCLI(t, src, "--watch")
	if r.code != 0 || !strings.Contains(r.stdout, "watched-output") {
		t.Errorf("with --watch: exit code %d, stdout = %q, want the terminal output", r.code, r.stdout)
	}
}

func TestRecordScriptFailedWaitKeepsRecording(t *testing.T) {
	t.Parallel()
	r := runScriptCLI(t, `
scene probe {
  run echo hi
  wait "never-on-screen" 1s
}
`)
	if r.code != 3 {
		t.Fatalf("exit code = %d, want 3\nstderr: %s", r.code, r.stderr)
	}
	if r.took > 5*time.Second {
		t.Errorf("took %v, want about 2 s", r.took)
	}
	for _, want := range []string{
		`demo.script:`,
		`scene "probe": wait timed out after 1s waiting for "never-on-screen"`,
		"shellshot: script failed: ",
		"; partial recording kept at " + r.shot,
		"shellshot: final screen:",
		"| hi",
	} {
		if !strings.Contains(r.stderr, want) {
			t.Errorf("stderr lacks %q:\n%s", want, r.stderr)
		}
	}
	s := readShot(t, r.shot)
	if !strings.Contains(s.out(), "hi") {
		t.Errorf("OUT = %q, want hi", s.out())
	}
	if want := []string{"scene:probe"}; !reflect.DeepEqual(spanNames(s.spans), want) {
		t.Errorf("spans = %v, want %v", spanNames(s.spans), want)
	}
	if len(s.spans) == 1 && s.spans[0].Open {
		t.Error("the failed scene was left open")
	}
	_ = s.exitStatus(t)
	if code, stdout, _ := runCLI(t, "info", r.shot); code != 0 || !strings.Contains(stdout, "status        complete") {
		t.Errorf("info exit code = %d, want 0 and a complete recording:\n%s", code, stdout)
	}
	// The kept recording replays and exports like any other.
	if code, _, stderr := runCLI(t, "export", r.shot, "-o", filepath.Join(filepath.Dir(r.shot), "f.png")); code != 0 {
		t.Errorf("export of the partial recording: exit code %d: %s", code, stderr)
	}
}

func TestRecordScriptSettleTimeoutFails(t *testing.T) {
	t.Parallel()
	r := runScriptCLI(t, `
timeout 1s
scene busy {
  run i=0; while :; do i=$((i+1)); echo tick$i; sleep 0.05; done
}
`)
	if r.code != 3 {
		t.Fatalf("exit code = %d, want 3\nstderr: %s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, `scene "busy": run did not settle`) {
		t.Errorf("stderr = %s, want a settle failure naming the scene", r.stderr)
	}
	s := readShot(t, r.shot)
	if !strings.Contains(s.out(), "tick") {
		t.Errorf("OUT = %q, want the output of the loop", s.out())
	}
}

func TestRecordScriptChildExitsEarlyFails(t *testing.T) {
	t.Parallel()
	r := runScriptCLI(t, "run exit\nrun echo too-late\n")
	if r.code != 3 || !strings.Contains(r.stderr, "child exited") {
		t.Errorf("exit code = %d, stderr = %s, want 3 and a child exited failure", r.code, r.stderr)
	}
}

func TestRecordScriptChildStatus(t *testing.T) {
	t.Parallel()
	src := "run exit 7\n"
	r := runScriptCLI(t, src)
	if r.code != 7 || !strings.Contains(r.stderr, "command exited with 7") {
		t.Errorf("exit code = %d, stderr = %s, want the child's status 7", r.code, r.stderr)
	}
	if got := readShot(t, r.shot).exitStatus(t); got != 7 {
		t.Errorf("EXIT status = %d, want 7", got)
	}
	if r := runScriptCLI(t, src, "--ignore-exit-status"); r.code != 0 {
		t.Errorf("with --ignore-exit-status: exit code = %d, want 0", r.code)
	}
}

func TestRecordScriptCannotStartProgram(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "x.script")
	if err := os.WriteFile(file, []byte("shell /nonexistent/shell\nrun echo hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "x.shot")
	code, _, stderr := runCLI(t, "record", "--script", file, "-o", out)
	if code != 1 || !strings.Contains(stderr, "/nonexistent/shell") {
		t.Errorf("exit code = %d, stderr = %q, want 1 naming the program", code, stderr)
	}
	if _, err := os.Stat(out); err == nil {
		t.Error("a recording was created for a program that did not start")
	}
}

func TestRecordScriptCancelClosesSpans(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(1500*time.Millisecond, cancel)
	r := runScriptCLIContext(t, ctx, `
scene waiting {
  run echo started
  wait "never-on-screen" 20s
}
`)
	if r.code != 130 {
		t.Fatalf("exit code = %d, want 130\nstderr: %s", r.code, r.stderr)
	}
	if r.took > 8*time.Second {
		t.Errorf("took %v after cancel, want a prompt exit", r.took)
	}
	if !strings.Contains(r.stderr, "cancelled; partial recording kept at "+r.shot) {
		t.Errorf("stderr = %s", r.stderr)
	}
	s := readShot(t, r.shot)
	if want := []string{"scene:waiting"}; !reflect.DeepEqual(spanNames(s.spans), want) {
		t.Fatalf("spans = %v, want %v", spanNames(s.spans), want)
	}
	if s.spans[0].Open {
		t.Error("the scene was not closed on cancel")
	}
	if !strings.Contains(s.out(), "started") {
		t.Errorf("OUT = %q, want started", s.out())
	}
	if s.count(event.KindExit) != 0 {
		t.Error("a cancelled run logged an EXIT event")
	}
	if code, stdout, _ := runCLI(t, "info", r.shot); code != 0 || !strings.Contains(stdout, "status        interrupted") {
		t.Errorf("info exit code = %d, want 0 and an interrupted recording:\n%s", code, stdout)
	}
}

func TestRecordScriptRecordInput(t *testing.T) {
	t.Parallel()
	r := runScriptCLI(t, "type echo ab\npress enter\n", "--record-input")
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, inputNotice) {
		t.Errorf("stderr = %q, want the input notice", r.stderr)
	}
	s := readShot(t, r.shot)
	if !strings.Contains(s.in(), "echo ab\r") {
		t.Errorf("IN = %q, want the scripted keystrokes", s.in())
	}
	if s.head.Flags&head.FlagInputRecorded == 0 {
		t.Error("head does not say input was recorded")
	}
}

// timeline is the events of a recording without their times: the kind and
// data of everything but the output, whose chunking depends on timing.
func (s shot) timeline() []string {
	var out []string
	for _, e := range s.events {
		if e.Kind == event.KindOut {
			continue
		}
		out = append(out, fmt.Sprintf("%d:%q", e.Kind, e.Data))
	}
	return out
}

func TestRecordScriptIsDeterministic(t *testing.T) {
	t.Parallel()
	const src = `
seed 42
jitter 40%
scene one {
  run echo one
  press up x2
  press ctrl+a
  mark middle
}
scene two {
  type echo two
  press enter
}
`
	a := readShot(t, runScriptCLI(t, src, "--record-input").shot)
	b := readShot(t, runScriptCLI(t, src, "--record-input").shot)
	if !reflect.DeepEqual(a.timeline(), b.timeline()) {
		t.Errorf("timelines differ between two runs:\n%v\n%v", a.timeline(), b.timeline())
	}
	if len(a.timeline()) < 10 {
		t.Errorf("timeline has only %d events: %v", len(a.timeline()), a.timeline())
	}
}

func TestRecordScriptFlagsBeatScript(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// The script names its output by absolute path, so the test does not need
	// t.Chdir, which cannot be used in a parallel test.
	file := filepath.Join(dir, "p.script")
	src := scriptPrelude + "output " + filepath.Join(dir, "from-script.shot") + "\nrun echo hi\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}

	// The script's output and size apply without flags.
	if code, _, stderr := runCLI(t, "record", "--script", file); code != 0 {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}
	s := readShot(t, filepath.Join(dir, "from-script.shot"))
	if s.head.Cols != 60 || s.head.Rows != 8 {
		t.Errorf("script size: got %dx%d, want 60x8", s.head.Cols, s.head.Rows)
	}

	// Explicit flags win.
	flagged := filepath.Join(dir, "from-flag.shot")
	if code, _, stderr := runCLI(t, "record", "--script", file, "-o", flagged, "--cols", "50"); code != 0 {
		t.Fatalf("exit code = %d: %s", code, stderr)
	}
	s = readShot(t, flagged)
	if s.head.Cols != 50 || s.head.Rows != 8 {
		t.Errorf("flag size: got %dx%d, want 50x8 (--cols over size, rows from the script)", s.head.Cols, s.head.Rows)
	}
	if err := os.Remove(filepath.Join(dir, "from-script.shot")); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := runCLI(t, "record", "--script", file, "-o", flagged); code != 0 {
		t.Fatal("rerun failed")
	}
	if _, err := os.Stat(filepath.Join(dir, "from-script.shot")); err == nil {
		t.Error("-o did not beat the output setting of the script")
	}
}

func TestRecordScriptEnvAndCwd(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	r := runScriptCLI(t, fmt.Sprintf(`
env GREETING=howdy
cwd %s
scene env {
  run echo v=$GREETING
  wait "v=howdy" 5s
  run pwd -P
  wait %q 5s
}
`, dir, filepath.Base(dir)))
	if r.code != 0 {
		t.Fatalf("exit code = %d\n%s", r.code, r.stderr)
	}
}

// TestRecordScriptClosesChildThatIgnoresEOF checks the end of a script whose
// program does not leave on Ctrl-D: shellshot closes it after the grace time,
// says so, and does not count it as a failure of the child.
func TestRecordScriptClosesChildThatIgnoresEOF(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "x.script")
	if err := os.WriteFile(file, []byte("shell /bin/sleep 30\nsleep 100ms\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "x.shot")
	start := time.Now()
	code, _, stderr := runCLI(t, "record", "--script", file, "-o", out)
	if code != 0 || !strings.Contains(stderr, "child did not exit; closed by shellshot") {
		t.Errorf("exit code = %d, stderr = %q, want 0 and the closed note", code, stderr)
	}
	if took := time.Since(start); took > 8*time.Second {
		t.Errorf("took %v, want about the 2 s grace time", took)
	}
	s := readShot(t, out)
	if want := []string{"hidden"}; !reflect.DeepEqual(spanNames(s.spans), want) || s.spans[0].Open {
		t.Errorf("spans = %v, want one closed hidden span", spanNames(s.spans))
	}
}

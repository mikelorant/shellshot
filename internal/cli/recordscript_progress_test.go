package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// runScriptOnFakeTerminal runs "record --script" on src (after scriptPrelude)
// with a stderr that reports itself as a terminal of the given width, and
// returns the stderr text and the error of the command.
func runScriptOnFakeTerminal(t *testing.T, width int, src string, args ...string) (string, error) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.script")
	if err := os.WriteFile(file, []byte(scriptPrelude+src), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeTerminal := func(io.Writer) (func() int, bool) { return func() int { return width }, true }
	var stdout, stderr bytes.Buffer
	root := &cobra.Command{Use: "shellshot", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newRecordCmdWith(recordOptions{now: time.Now, outDir: dir, stderrTerminal: fakeTerminal}))
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"record", "--script", file, "-o", filepath.Join(dir, "demo.shot")}, args...))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := root.ExecuteContext(ctx)
	return stderr.String(), err
}

const progressScript = "run echo one\nscene demo {\n  run echo two\n}\nmark done\n"

// progressFrames returns the lines that were drawn on the progress line, in
// order, and the text written after the last time the line was erased.
func progressFrames(t *testing.T, stderr string) (frames []string, after string) {
	t.Helper()
	const erase = "\x1b[K"
	last := strings.LastIndex(stderr, erase)
	if last < 0 {
		t.Fatalf("stderr has no erase-to-end-of-line: %q", stderr)
	}
	for _, m := range regexp.MustCompile(`\r([^\r\x1b]*)\x1b\[K`).FindAllStringSubmatch(stderr, -1) {
		frames = append(frames, m[1])
	}
	return frames, stderr[last+len(erase):]
}

func TestRecordScriptDrawsProgressOnATerminal(t *testing.T) {
	stderr, err := runScriptOnFakeTerminal(t, 60, progressScript)
	if err != nil {
		t.Fatalf("record: %v\n%q", err, stderr)
	}
	frames, after := progressFrames(t, stderr)
	if len(frames) < 4 {
		t.Fatalf("frames = %q, want at least one per completed step and the first", frames)
	}
	if first := frames[0]; !strings.Contains(first, "0/3") || strings.Contains(first, "=") {
		t.Errorf("first frame = %q, want 0/3 and an empty bar", first)
	}
	last := frames[len(frames)-2] // the final erase is the last match
	if !strings.Contains(last, "3/3") || !strings.Contains(last, "["+strings.Repeat("=", 20)+"]") {
		t.Errorf("last frame = %q, want 3/3 and a full bar", last)
	}
	if !strings.Contains(strings.Join(frames, "\n"), "2/3  demo") {
		t.Errorf("frames = %q, want the scene name while it runs", frames)
	}
	for _, f := range frames {
		if len(f) > 59 {
			t.Errorf("frame %q is wider than the terminal", f)
		}
	}
	if !strings.HasPrefix(after, "recorded ") || strings.Count(after, "\n") != 1 {
		t.Errorf("after the last erase: %q, want only the summary line", after)
	}
}

func TestRecordScriptClearsProgressBeforeAScriptFailure(t *testing.T) {
	stderr, err := runScriptOnFakeTerminal(t, 60, "run echo one\nwait \"never\" 0s\n")
	var exit ExitError
	if !errors.As(err, &exit) {
		t.Fatalf("err = %v, want an ExitError for the failed script", err)
	}
	frames, after := progressFrames(t, stderr)
	if len(frames) == 0 || !strings.Contains(frames[0], "0/2") {
		t.Fatalf("frames = %q, want the progress line to have been drawn", frames)
	}
	if !strings.HasPrefix(after, "shellshot: ") || !strings.Contains(after, "script failed") {
		t.Errorf("after the last erase: %q, want the failure message on its own line", after)
	}
}

func TestRecordScriptWithWatchDrawsNoProgress(t *testing.T) {
	stderr, err := runScriptOnFakeTerminal(t, 60, progressScript, "--watch")
	if err != nil {
		t.Fatalf("record: %v\n%q", err, stderr)
	}
	if strings.ContainsAny(stderr, "\r\x1b") {
		t.Errorf("stderr has terminal control bytes with --watch: %q", stderr)
	}
}

func TestRecordScriptDrawsNoProgressWhenStderrIsNotATerminal(t *testing.T) {
	r := runScriptCLI(t, progressScript)
	if r.code != 0 {
		t.Fatalf("exit %d\n%s", r.code, r.stderr)
	}
	if strings.ContainsAny(r.stderr, "\r\x1b") || strings.Contains(r.stderr, "0/3") {
		t.Errorf("stderr has progress bytes on a non-terminal: %q", r.stderr)
	}
	if !strings.HasPrefix(r.stderr, "recorded ") {
		t.Errorf("stderr = %q, want only the summary", r.stderr)
	}
}

package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording"
	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

func TestRecordingSinkRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	started := time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)
	sink, err := newRecordingSink(&buf, recording.Header{
		Version: recording.Version, Cols: 10, Rows: 2, Command: []string{"ls", "-l"}, Started: started,
	})
	if err != nil {
		t.Fatalf("newRecordingSink: %v", err)
	}
	want := []snapshot.Frame{
		{At: 0, Snapshot: snapshot.Snapshot{Cols: 10, Rows: 2, Lines: []string{"a", ""}}},
		{At: 150 * time.Millisecond, Snapshot: snapshot.Snapshot{
			Cols: 10, Rows: 2, Lines: []string{"a", "b"},
			Cursor: snapshot.Cursor{X: 1, Y: 1, Visible: true}, Title: "t",
		}},
	}
	for _, f := range want {
		if err := sink.Frame(f); err != nil {
			t.Fatalf("Frame: %v", err)
		}
	}
	if sink.frames != len(want) {
		t.Errorf("frames = %d, want %d", sink.frames, len(want))
	}

	r, err := recording.NewReader(&buf)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	h := r.Header()
	if h.Cols != 10 || h.Rows != 2 || strings.Join(h.Command, " ") != "ls -l" || !h.Started.Equal(started) {
		t.Errorf("header = %+v", h)
	}
	for i, w := range want {
		got, err := r.Next()
		if err != nil {
			t.Fatalf("Next %d: %v", i, err)
		}
		if got.At != w.At || !got.Snapshot.Equal(w.Snapshot) {
			t.Errorf("frame %d = %+v, want %+v", i, got, w)
		}
	}
	if _, err := r.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("Next after last frame = %v, want io.EOF", err)
	}
}

func TestRecordingSinkRejectsWrongLineCount(t *testing.T) {
	sink, err := newRecordingSink(io.Discard, recording.Header{Cols: 10, Rows: 2})
	if err != nil {
		t.Fatalf("newRecordingSink: %v", err)
	}
	f := snapshot.Frame{Snapshot: snapshot.Snapshot{Lines: []string{"only one"}}}
	if err := sink.Frame(f); err == nil {
		t.Fatal("Frame with wrong line count succeeded, want error")
	}
	if sink.frames != 0 {
		t.Errorf("frames = %d, want 0", sink.frames)
	}
}

func TestRecordFlagDefaults(t *testing.T) {
	cmd := newRecordCmd()
	for name, want := range map[string]string{
		"output":   "recording.shot",
		"command":  "",
		"cols":     "0",
		"rows":     "0",
		"interval": session.DefaultInterval.String(),
	} {
		if got := cmd.Flags().Lookup(name).DefValue; got != want {
			t.Errorf("--%s default = %q, want %q", name, got, want)
		}
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("missing -o shorthand")
	}
	if cmd.Flags().ShorthandLookup("c") == nil {
		t.Error("missing -c shorthand")
	}
}

func TestRecordRejectsCommandStringWithArgv(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", filepath.Join(t.TempDir(), "x.shot"), "-c", "true", "--", "ls"}
	code := runWithStdin(context.Background(), args, strings.NewReader(""), &stdout, &stderr)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "mutually exclusive") {
		t.Errorf("stderr = %q, want mutually exclusive error", stderr.String())
	}
}

func TestRecordWritesRecording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.shot")
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5", "--", "sh", "-c", `printf 'hello\nworld'`}
	if code := runWithStdin(context.Background(), args, strings.NewReader(""), &stdout, &stderr); code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stderr.String(), "frames to "+path) {
		t.Errorf("stderr = %q, want recorded summary", stderr.String())
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open recording: %v", err)
	}
	defer f.Close()
	r, err := recording.NewReader(f)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if h := r.Header(); h.Cols != 40 || h.Rows != 5 || h.Command[0] != "sh" {
		t.Errorf("header = %+v", h)
	}
	last := lastRecordedFrame(t, r)
	if len(last.Snapshot.Lines) < 2 || last.Snapshot.Lines[0] != "hello" || last.Snapshot.Lines[1] != "world" {
		t.Errorf("last frame lines = %q, want hello and world first", last.Snapshot.Lines)
	}
}

func TestRecordForwardsStdinAndShowsOutputLive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.shot")
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "6", "-c", "read x; echo got:$x"}
	code := runWithStdin(context.Background(), args, strings.NewReader("hi\n"), &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "got:hi") {
		t.Errorf("live stdout = %q, want it to contain got:hi", stdout.String())
	}
	if strings.Contains(stderr.String(), "recording —") {
		t.Errorf("stderr = %q, want no interactive hint for non-terminal stdin", stderr.String())
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open recording: %v", err)
	}
	defer f.Close()
	r, err := recording.NewReader(f)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if h := r.Header(); h.Cols != 40 || h.Rows != 6 || len(h.Command) != 3 || h.Command[1] != "-c" {
		t.Errorf("header = %+v", h)
	}
	if text := lastRecordedFrame(t, r).Snapshot.Text(); !strings.Contains(text, "got:hi") {
		t.Errorf("last frame = %q, want it to contain got:hi", text)
	}
}

// recordTimeout bounds record runs fed from a finite stdin, so a regression
// that hangs fails fast.
const recordTimeout = 10 * time.Second

// runRecordWithTimeout runs args with stdin and fails the test if Run does not
// return within recordTimeout.
func runRecordWithTimeout(t *testing.T, args []string, stdin string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- runWithStdin(ctx, args, strings.NewReader(stdin), &out, &errOut) }()
	select {
	case code = <-done:
		return code, out.String(), errOut.String()
	case <-time.After(recordTimeout):
		cancel()
		t.Fatalf("record did not finish within %v after stdin EOF", recordTimeout)
		return 0, "", ""
	}
}

func TestRecordPipedStdinEOFExitsDefaultShell(t *testing.T) {
	path := filepath.Join(t.TempDir(), "eof.shot")
	t.Setenv("SHELL", "/bin/sh")
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5"}
	code, _, stderr := runRecordWithTimeout(t, args, "echo hi\n")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	assertLastFrameContains(t, path, "hi")
}

func TestRecordPipedStdinEOFEndsCat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cat.shot")
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5", "-c", "cat"}
	code, _, stderr := runRecordWithTimeout(t, args, "hi\n")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
	assertLastFrameContains(t, path, "hi")
}

// assertLastFrameContains fails unless the last frame of the recording at path
// contains want.
func assertLastFrameContains(t *testing.T, path, want string) {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open recording: %v", err)
	}
	defer f.Close()
	r, err := recording.NewReader(f)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	if text := lastRecordedFrame(t, r).Snapshot.Text(); !strings.Contains(text, want) {
		t.Errorf("last frame = %q, want it to contain %q", text, want)
	}
}

// lastRecordedFrame reads r to the end and returns the last frame.
func lastRecordedFrame(t *testing.T, r *recording.Reader) snapshot.Frame {
	t.Helper()
	var last snapshot.Frame
	n := 0
	for {
		fr, err := r.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next: %v", err)
		}
		last = fr
		n++
	}
	if n == 0 {
		t.Fatal("recording has no frames")
	}
	return last
}

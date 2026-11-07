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
	if got := cmd.Flags().Lookup("output").DefValue; got != "recording.shot" {
		t.Errorf("--output default = %q, want recording.shot", got)
	}
	if cmd.Flags().ShorthandLookup("o") == nil {
		t.Error("missing -o shorthand")
	}
}

func TestRecordRequiresCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"record"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestRecordWritesRecording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "x.shot")
	var stdout, stderr bytes.Buffer
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5", "--", "sh", "-c", `printf 'hello\nworld'`}
	if code := Run(context.Background(), args, &stdout, &stderr); code != 0 {
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
	if len(last.Snapshot.Lines) < 2 || last.Snapshot.Lines[0] != "hello" || last.Snapshot.Lines[1] != "world" {
		t.Errorf("last frame lines = %q, want hello and world first", last.Snapshot.Lines)
	}
}

package frames_test

import (
	"bytes"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
	"github.com/michaellorant/shellshot/internal/recording/frames"
	"github.com/michaellorant/shellshot/internal/snapshot"
	"github.com/michaellorant/shellshot/internal/vt/ghostty"
)

// TestReplayThroughGhostty writes a real binary recording, replays it through
// libghostty and checks the styled text and the mid-stream resize.
func TestReplayThroughGhostty(t *testing.T) {
	var buf bytes.Buffer
	now := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	w, err := bin.NewWriter(&buf, head.Head{Cols: 20, Rows: 4}, bin.WriterOptions{Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Output(30*time.Millisecond, []byte("\x1b[31mred\x1b[0m\r\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Resize(250*time.Millisecond, 30, 6, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Output(260*time.Millisecond, []byte("plain")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := bin.NewReader(bytes.NewReader(buf.Bytes()), bin.ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	it, err := frames.New(r, frames.Options{Factory: ghostty.New})
	if err != nil {
		t.Fatal(err)
	}
	got, err := frames.Collect(it)
	if err != nil {
		t.Fatal(err)
	}

	// t=0 blank, 100ms "red", final at 260ms after the resize and "plain".
	if len(got) != 3 {
		t.Fatalf("got %d frames, want 3", len(got))
	}
	if got[0].At != 0 || got[0].Snapshot.Cols != 20 || got[0].Snapshot.Rows != 4 || got[0].Snapshot.Lines[0] != "" {
		t.Errorf("frame 0 = %+v", got[0])
	}

	f1 := got[1]
	if f1.At != 100*time.Millisecond || f1.Snapshot.Lines[0] != "red" || f1.Snapshot.Cols != 20 {
		t.Fatalf("frame 1 = at %v lines %q", f1.At, f1.Snapshot.Lines)
	}
	if err := f1.Snapshot.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(f1.Snapshot.Spans) == 0 || len(f1.Snapshot.Spans[0]) != 1 {
		t.Fatalf("spans = %+v", f1.Snapshot.Spans)
	}
	sp := f1.Snapshot.Spans[0][0]
	want := snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}
	if sp.Start != 0 || sp.End != 3 || sp.Style.Fg != want {
		t.Errorf("span = %+v, want red over [0,3)", sp)
	}

	f2 := got[2]
	if f2.At != 260*time.Millisecond || f2.Snapshot.Cols != 30 || f2.Snapshot.Rows != 6 {
		t.Fatalf("frame 2 = at %v %dx%d", f2.At, f2.Snapshot.Cols, f2.Snapshot.Rows)
	}
	if f2.Snapshot.Lines[0] != "red" || f2.Snapshot.Lines[1] != "plain" {
		t.Errorf("frame 2 lines = %q", f2.Snapshot.Lines)
	}
}

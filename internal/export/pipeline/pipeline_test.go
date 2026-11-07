package pipeline

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"strings"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// sliceSource yields frames, then failAfter's error (or io.EOF).
type sliceSource struct {
	frames []snapshot.Frame
	err    error // returned instead of io.EOF once frames run out
	n      int
}

func (s *sliceSource) Next() (snapshot.Frame, error) {
	if s.n >= len(s.frames) {
		if s.err != nil {
			return snapshot.Frame{}, s.err
		}
		return snapshot.Frame{}, io.EOF
	}
	s.n++
	return s.frames[s.n-1], nil
}

// fakeRenderer fills the image with a grey level derived from the first
// line's first byte, and counts renders.
type fakeRenderer struct {
	renders int
	failAt  int // 1-based render number that fails; 0 never
	err     error
}

func (f *fakeRenderer) Size(cols, rows int) (int, int) { return cols * 2, rows * 2 }

func (f *fakeRenderer) Render(s snapshot.Snapshot) (*image.RGBA, error) {
	f.renders++
	if f.failAt != 0 && f.renders == f.failAt {
		return nil, f.err
	}
	img := image.NewRGBA(image.Rect(0, 0, s.Cols*2, s.Rows*2))
	g := s.Lines[0][0]
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = g, g, g, 0xff
	}
	return img, nil
}

func frame(at time.Duration, text string) snapshot.Frame {
	return snapshot.Frame{At: at, Snapshot: snapshot.Snapshot{Cols: 4, Rows: 1, Lines: []string{text}}}
}

func decode(t *testing.T, b []byte) kettek.APNG {
	t.Helper()
	a, err := kettek.DecodeAll(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return a
}

func delays(a kettek.APNG) []time.Duration {
	var out []time.Duration
	for _, f := range a.Frames {
		out = append(out, time.Duration(f.DelayNumerator)*time.Second/time.Duration(f.DelayDenominator))
	}
	return out
}

func equalDurations(a, b []time.Duration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestExportMergesEqualFramesAndSetsDelays(t *testing.T) {
	t.Parallel()
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"),
		frame(100*time.Millisecond, "a"), // equal: merged with the first
		frame(300*time.Millisecond, "b"),
		frame(700*time.Millisecond, "c"),
	}}
	r := &fakeRenderer{}
	var buf bytes.Buffer
	var progress [][2]int
	err := Export(context.Background(), src, 4, 1, r, &buf, Options{
		Timeline: timeline.Options{Hold: time.Second},
		Loops:    3,
		Progress: func(done, total int) { progress = append(progress, [2]int{done, total}) },
	})
	if err != nil {
		t.Fatal(err)
	}
	a := decode(t, buf.Bytes())
	want := []time.Duration{300 * time.Millisecond, 400 * time.Millisecond, time.Second}
	if got := delays(a); !equalDurations(got, want) {
		t.Errorf("delays = %v, want %v", got, want)
	}
	if a.LoopCount != 3 {
		t.Errorf("LoopCount = %d, want 3", a.LoopCount)
	}
	if r.renders != 3 {
		t.Errorf("renders = %d, want 3", r.renders)
	}
	if b := a.Frames[0].Image.Bounds(); b.Dx() != 8 || b.Dy() != 2 {
		// first frame is the full canvas: 4 cols x 2px, 1 row x 2px
		t.Errorf("first frame bounds = %v, want 8x2", b)
	}
	wantProgress := [][2]int{{1, 3}, {2, 3}, {3, 3}}
	if len(progress) != len(wantProgress) {
		t.Fatalf("progress = %v, want %v", progress, wantProgress)
	}
	for i := range wantProgress {
		if progress[i] != wantProgress[i] {
			t.Errorf("progress = %v, want %v", progress, wantProgress)
			break
		}
	}
}

func TestExportTimelineOptionsApply(t *testing.T) {
	t.Parallel()
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"),
		frame(10*time.Second, "b"),
	}}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, &fakeRenderer{}, &buf, Options{
		Timeline: timeline.Options{IdleLimit: 2 * time.Second, Speed: 2, Hold: time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, time.Second}
	if got := delays(decode(t, buf.Bytes())); !equalDurations(got, want) {
		t.Errorf("delays = %v, want %v", got, want)
	}
}

// Segments reach timeline.Build: a hidden stretch costs no time and no
// frames, and a scene segment scales its own part of the recording.
func TestExportSegmentsCutHiddenAndScaleScenes(t *testing.T) {
	t.Parallel()
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"),
		frame(time.Second, "b"),   // hidden
		frame(2*time.Second, "c"), // hidden
		frame(3*time.Second, "c"), // forced frame at the end of the hidden span
		frame(4*time.Second, "d"), // scene, speed 2
		frame(5*time.Second, "e"), // last frame
	}}
	r := &fakeRenderer{}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, r, &buf, Options{
		Timeline: timeline.Options{
			Hold: time.Second,
			Segments: []timeline.Segment{
				{From: time.Second, To: 3 * time.Second, Skip: true},
				{From: 3 * time.Second, To: 5 * time.Second, Speed: 2, Hold: 500 * time.Millisecond},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{time.Second, 500 * time.Millisecond, time.Second, time.Second}
	got := delays(decode(t, buf.Bytes()))
	if !equalDurations(got, want) {
		t.Errorf("delays = %v, want %v", got, want)
	}
	if r.renders != 4 {
		t.Errorf("renders = %d, want 4 (b is never rendered)", r.renders)
	}
}

func TestExportRendersOnceWhenMinDelayFoldLeavesEqualSteps(t *testing.T) {
	t.Parallel()
	// a, b (10ms, folded into a), a: the folded timeline is a, a, which must
	// render once and encode as one frame.
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"),
		frame(500*time.Millisecond, "b"),
		frame(510*time.Millisecond, "a"),
	}}
	r := &fakeRenderer{}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, r, &buf, Options{
		Timeline: timeline.Options{Hold: time.Second, MinDelay: 20 * time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.renders != 1 {
		t.Errorf("renders = %d, want 1", r.renders)
	}
	a := decode(t, buf.Bytes())
	want := []time.Duration{1510 * time.Millisecond}
	if got := delays(a); !equalDurations(got, want) {
		t.Errorf("delays = %v, want %v", got, want)
	}
}

func TestExportSplitsDelayLongerThanOneFrame(t *testing.T) {
	t.Parallel()
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, &fakeRenderer{}, &buf, Options{
		Timeline: timeline.Options{Hold: 70 * time.Second},
	})
	if err != nil {
		t.Fatal(err)
	}
	var total time.Duration
	for _, d := range delays(decode(t, buf.Bytes())) {
		total += d
	}
	if total != 70*time.Second {
		t.Errorf("total delay = %v, want 70s", total)
	}
}

func TestExportCancelledBeforeStartWritesNothing(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	var buf bytes.Buffer
	err := Export(ctx, src, 4, 1, &fakeRenderer{}, &buf, Options{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes after cancellation", buf.Len())
	}
}

func TestExportCancelledBetweenFrames(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"), frame(time.Second, "b"), frame(2*time.Second, "c"),
	}}
	r := &fakeRenderer{}
	var buf bytes.Buffer
	err := Export(ctx, src, 4, 1, r, &buf, Options{
		Progress: func(done, total int) { cancel() },
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if !strings.Contains(err.Error(), "step 1 of 3") {
		t.Errorf("err = %q, want it to name step 1 of 3", err)
	}
	if r.renders != 1 {
		t.Errorf("renders = %d, want 1 (stopped after the first step)", r.renders)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes after cancellation", buf.Len())
	}
}

func TestExportWrapsSourceError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}, err: boom}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, &fakeRenderer{}, &buf, Options{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap boom", err)
	}
	if !strings.Contains(err.Error(), "read frame 2") {
		t.Errorf("err = %q, want it to name frame 2", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes on failure", buf.Len())
	}
}

func TestExportWrapsRenderError(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}}
	var buf bytes.Buffer
	err := Export(context.Background(), src, 4, 1, &fakeRenderer{failAt: 2, err: boom}, &buf, Options{})
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap boom", err)
	}
	if !strings.Contains(err.Error(), "render step 1 of 2") {
		t.Errorf("err = %q, want it to name step 1 of 2", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes on failure", buf.Len())
	}
}

func TestExportRejectsEmptySource(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	err := Export(context.Background(), &sliceSource{}, 4, 1, &fakeRenderer{}, &buf, Options{})
	if err == nil || !strings.Contains(err.Error(), "no frames") {
		t.Fatalf("err = %v, want a no-frames error", err)
	}
}

func TestExportRejectsInvalidTimeline(t *testing.T) {
	t.Parallel()
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	err := Export(context.Background(), src, 4, 1, &fakeRenderer{}, io.Discard, Options{
		Timeline: timeline.Options{Speed: -1},
	})
	if err == nil || !strings.Contains(err.Error(), "speed") {
		t.Fatalf("err = %v, want an invalid-speed error", err)
	}
}

// wrongSizeRenderer reports one size and renders another.
type wrongSizeRenderer struct{ fakeRenderer }

func (wrongSizeRenderer) Size(cols, rows int) (int, int) { return 10, 10 }

func TestExportRejectsRendererSizeMismatch(t *testing.T) {
	t.Parallel()
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	err := Export(context.Background(), src, 4, 1, &wrongSizeRenderer{}, io.Discard, Options{})
	if err == nil || !strings.Contains(err.Error(), "encode step 0 of 1") {
		t.Fatalf("err = %v, want an encode error for step 0", err)
	}
}

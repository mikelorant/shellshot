package pipeline

import (
	"bytes"
	"context"
	"errors"
	"image"
	"io"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/export/timeline"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// recordedFrame is one AddFrame or AddFrameAuto call on a fakeSink.
type recordedFrame struct {
	img   *image.RGBA
	delay time.Duration
	auto  bool
}

// fakeSink records the frames it is given and, in Close, writes "fake" to
// its writer. It is a plain Sink.
type fakeSink struct {
	w        io.Writer
	width    int
	height   int
	frames   []recordedFrame
	closes   int
	addErr   error // returned by the failAt-th AddFrame
	failAt   int   // 1-based; 0 never
	closeErr error
}

func (f *fakeSink) AddFrame(img *image.RGBA, delay time.Duration) error {
	f.frames = append(f.frames, recordedFrame{img: img, delay: delay})
	if f.failAt != 0 && len(f.frames) == f.failAt {
		return f.addErr
	}
	return nil
}

func (f *fakeSink) Close() error {
	f.closes++
	f.w.Write([]byte("fake"))
	return f.closeErr
}

// limitedSink is a fakeSink with a MaxDelay.
type limitedSink struct {
	*fakeSink
	max time.Duration
}

func (l limitedSink) MaxDelay() time.Duration { return l.max }

// autoSink is a fakeSink that also implements AutoFrame.
type autoSink struct{ *fakeSink }

func (a autoSink) AddFrameAuto(img image.Image, delay time.Duration) error {
	a.fakeSink.frames = append(a.fakeSink.frames, recordedFrame{img: img.(*image.RGBA), delay: delay, auto: true})
	return nil
}

// factoryFor returns a SinkFactory that creates sinks with wrap and records
// the inner fakeSink in *got.
func factoryFor(got **fakeSink, wrap func(*fakeSink) Sink) SinkFactory {
	return func(w io.Writer, width, height int) (Sink, error) {
		f := &fakeSink{w: w, width: width, height: height}
		*got = f
		return wrap(f), nil
	}
}

func plain(f *fakeSink) Sink { return f }

func exportWith(t *testing.T, src Source, r Renderer, w io.Writer, hold time.Duration, factory SinkFactory) error {
	t.Helper()
	return Export(context.Background(), src, 4, 1, r, w, Options{
		Timeline: timeline.Options{Hold: hold},
		NewSink:  factory,
	})
}

func frameDelays(f *fakeSink) []time.Duration {
	var out []time.Duration
	for _, fr := range f.frames {
		out = append(out, fr.delay)
	}
	return out
}

func sameDurations(a, b []time.Duration) bool {
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

func TestExportUsesNewSinkAndPassesSize(t *testing.T) {
	var sink *fakeSink
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}}
	var buf bytes.Buffer
	if err := exportWith(t, src, &fakeRenderer{}, &buf, time.Second, factoryFor(&sink, plain)); err != nil {
		t.Fatal(err)
	}
	if sink.width != 8 || sink.height != 2 {
		t.Errorf("sink size = %dx%d, want 8x2", sink.width, sink.height)
	}
	if want := []time.Duration{time.Second, time.Second}; !sameDurations(frameDelays(sink), want) {
		t.Errorf("delays = %v, want %v", frameDelays(sink), want)
	}
	if sink.closes != 1 || buf.String() != "fake" {
		t.Errorf("closes = %d, output = %q; want 1 and %q", sink.closes, buf.String(), "fake")
	}
}

func TestExportSplitsLongHoldAtMaxDelay(t *testing.T) {
	var sink *fakeSink
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	wrap := func(f *fakeSink) Sink { return limitedSink{f, 10 * time.Second} }
	if err := exportWith(t, src, &fakeRenderer{}, io.Discard, 25*time.Second, factoryFor(&sink, wrap)); err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{10 * time.Second, 10 * time.Second, 5 * time.Second}
	if !sameDurations(frameDelays(sink), want) {
		t.Errorf("delays = %v, want %v", frameDelays(sink), want)
	}
	if sink.frames[0].img != sink.frames[2].img {
		t.Error("split frames should share one rendered image")
	}
}

func TestExportDelayEqualToMaxDelayIsNotSplit(t *testing.T) {
	var sink *fakeSink
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	wrap := func(f *fakeSink) Sink { return limitedSink{f, 10 * time.Second} }
	if err := exportWith(t, src, &fakeRenderer{}, io.Discard, 10*time.Second, factoryFor(&sink, wrap)); err != nil {
		t.Fatal(err)
	}
	if want := []time.Duration{10 * time.Second}; !sameDurations(frameDelays(sink), want) {
		t.Errorf("delays = %v, want %v", frameDelays(sink), want)
	}
}

func TestExportZeroMaxDelayIsUnlimited(t *testing.T) {
	for name, wrap := range map[string]func(*fakeSink) Sink{
		"MaxDelay 0":          func(f *fakeSink) Sink { return limitedSink{f, 0} },
		"no DelayLimiter":     plain,
		"AutoFrame, no limit": func(f *fakeSink) Sink { return autoSink{f} },
	} {
		t.Run(name, func(t *testing.T) {
			var sink *fakeSink
			src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
			if err := exportWith(t, src, &fakeRenderer{}, io.Discard, 70*time.Hour, factoryFor(&sink, wrap)); err != nil {
				t.Fatal(err)
			}
			if want := []time.Duration{70 * time.Hour}; !sameDurations(frameDelays(sink), want) {
				t.Errorf("delays = %v, want %v", frameDelays(sink), want)
			}
		})
	}
}

func TestExportPrefersAddFrameAuto(t *testing.T) {
	var sink *fakeSink
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}}
	if err := exportWith(t, src, &fakeRenderer{}, io.Discard, time.Second, factoryFor(&sink, func(f *fakeSink) Sink { return autoSink{f} })); err != nil {
		t.Fatal(err)
	}
	if len(sink.frames) != 2 {
		t.Fatalf("got %d frames, want 2", len(sink.frames))
	}
	for i, fr := range sink.frames {
		if !fr.auto {
			t.Errorf("frame %d added with AddFrame, want AddFrameAuto", i)
		}
	}
}

func TestExportUsesAddFrameWithoutAutoFrame(t *testing.T) {
	var sink *fakeSink
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	if err := exportWith(t, src, &fakeRenderer{}, io.Discard, time.Second, factoryFor(&sink, plain)); err != nil {
		t.Fatal(err)
	}
	if len(sink.frames) != 1 || sink.frames[0].auto {
		t.Errorf("frames = %+v, want one plain AddFrame", sink.frames)
	}
}

func TestExportClosesSinkOnceAndWritesNothingOnFailure(t *testing.T) {
	boom := errors.New("boom")
	cases := map[string]struct {
		src  Source
		r    Renderer
		fail int
	}{
		"source error": {src: &sliceSource{frames: []snapshot.Frame{frame(0, "a")}, err: boom}, r: &fakeRenderer{}},
		"render error": {
			src: &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}},
			r:   &fakeRenderer{failAt: 2, err: boom},
		},
		"AddFrame error": {src: &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}}, r: &fakeRenderer{}, fail: 2},
	}
	for name, tt := range cases {
		t.Run(name, func(t *testing.T) {
			var sink *fakeSink
			var buf bytes.Buffer
			factory := func(w io.Writer, width, height int) (Sink, error) {
				sink = &fakeSink{w: w, failAt: tt.fail, addErr: boom}
				return sink, nil
			}
			err := exportWith(t, tt.src, tt.r, &buf, time.Second, factory)
			if !errors.Is(err, boom) {
				t.Fatalf("err = %v, want it to wrap boom", err)
			}
			if buf.Len() != 0 {
				t.Errorf("wrote %q on failure", buf.String())
			}
			// A source error happens before the sink exists.
			if name != "source error" && (sink == nil || sink.closes != 1) {
				t.Errorf("sink = %+v, want Close called once", sink)
			}
		})
	}
}

func TestExportClosesSinkOnceOnCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	var sink *fakeSink
	var buf bytes.Buffer
	r := &cancelRenderer{cancel: cancel}
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a"), frame(time.Second, "b")}}
	err := Export(ctx, src, 4, 1, r, &buf, Options{
		Timeline: timeline.Options{Hold: time.Second},
		NewSink:  factoryFor(&sink, plain),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if sink.closes != 1 || buf.Len() != 0 {
		t.Errorf("closes = %d, output %q; want 1 and none", sink.closes, buf.String())
	}
}

// cancelRenderer cancels its context on the first render.
type cancelRenderer struct {
	fakeRenderer
	cancel context.CancelFunc
}

func (c *cancelRenderer) Render(s snapshot.Snapshot) (*image.RGBA, error) {
	c.cancel()
	return c.fakeRenderer.Render(s)
}

func TestExportReportsCloseError(t *testing.T) {
	boom := errors.New("boom")
	factory := func(w io.Writer, width, height int) (Sink, error) {
		return &fakeSink{w: w, closeErr: boom}, nil
	}
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	err := exportWith(t, src, &fakeRenderer{}, io.Discard, time.Second, factory)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap boom", err)
	}
}

func TestExportReportsSinkFactoryError(t *testing.T) {
	boom := errors.New("boom")
	factory := func(io.Writer, int, int) (Sink, error) { return nil, boom }
	src := &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}
	var buf bytes.Buffer
	err := exportWith(t, src, &fakeRenderer{}, &buf, time.Second, factory)
	if !errors.Is(err, boom) || buf.Len() != 0 {
		t.Errorf("err = %v, output %d bytes; want boom and none", err, buf.Len())
	}
}

func TestAPNGSinkLimitsDelayTo65535Milliseconds(t *testing.T) {
	s, err := APNGSinkFactory(0, 0)(io.Discard, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.(DelayLimiter).MaxDelay(); got != 65535*time.Millisecond {
		t.Errorf("MaxDelay = %v, want 65.535s", got)
	}
	if _, ok := s.(AutoFrame); !ok {
		t.Error("APNG sink should implement AutoFrame")
	}
}

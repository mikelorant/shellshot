package gif_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"io"
	"math/rand"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/gif"
	"github.com/mikelorant/shellshot/internal/export/pipeline"
	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// alternating returns n 2x2 frames that differ from their neighbours, so the
// sink cannot merge them.
func alternating(n int) []*image.RGBA {
	frames := make([]*image.RGBA, n)
	for i := range frames {
		frames[i] = solid(2, 2, color.RGBA{uint8(i % 2 * 100), 50, 0, 255})
	}
	return frames
}

func sumCentiseconds(delays []int) int {
	total := 0
	for _, d := range delays {
		total += d
	}
	return total
}

func TestDelaysRoundToNearestCentisecond(t *testing.T) {
	for _, c := range []struct {
		ms   int
		want int
	}{{20, 2}, {24, 2}, {25, 3}, {26, 3}, {100, 10}, {1234, 123}, {1235, 124}} {
		g := decodeGIF(t, encodeFrames(t, gif.Options{}, alternating(2), []time.Duration{time.Duration(c.ms) * time.Millisecond, time.Second}))
		if g.Delay[0] != c.want {
			t.Errorf("%d ms: %d cs, want %d", c.ms, g.Delay[0], c.want)
		}
	}
}

func TestDelaysBelowTwoCentisecondsAreClamped(t *testing.T) {
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, alternating(4),
		[]time.Duration{0, time.Millisecond, 10 * time.Millisecond, 19 * time.Millisecond}))
	for i, d := range g.Delay {
		if d != 2 {
			t.Errorf("frame %d: %d cs, want 2", i, d)
		}
	}
}

func TestRoundingErrorIsCarriedToTheNextFrame(t *testing.T) {
	// 25 ms is 2.5 cs: alternate 3 and 2 so ten frames last exactly 250 ms.
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, alternating(10), evenDelays(10, 25*time.Millisecond)))
	if got := sumCentiseconds(g.Delay); got != 25 {
		t.Errorf("ten 25 ms frames last %d cs, want 25 (delays %v)", got, g.Delay)
	}
	// 1/30 s is 3.33 cs: without the carry 300 frames would last 900 cs.
	g = decodeGIF(t, encodeFrames(t, gif.Options{}, alternating(300), evenDelays(300, time.Second/30)))
	if got := sumCentiseconds(g.Delay); got < 999 || got > 1001 {
		t.Errorf("300 frames of 1/30 s last %d cs, want 1000 within 1", got)
	}
}

func TestTotalDurationIsKeptWithinOneCentisecond(t *testing.T) {
	for seed := int64(0); seed < 20; seed++ {
		rng := rand.New(rand.NewSource(seed))
		n := 1 + rng.Intn(60)
		delays := make([]time.Duration, n)
		var total time.Duration
		for i := range delays {
			delays[i] = 20*time.Millisecond + time.Duration(rng.Int63n(int64(3*time.Second)))
			total += delays[i]
		}
		g := decodeGIF(t, encodeFrames(t, gif.Options{}, alternating(n), delays))
		got := time.Duration(sumCentiseconds(g.Delay)) * 10 * time.Millisecond
		if diff := got - total; diff < -10*time.Millisecond || diff > 10*time.Millisecond {
			t.Errorf("seed %d: %v encoded as %v", seed, total, got)
		}
	}
}

func TestMergedFramesSumTheirDelaysBeforeRounding(t *testing.T) {
	a := solid(2, 2, color.RGBA{A: 255})
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, []*image.RGBA{a, a, a}, evenDelays(3, 15*time.Millisecond)))
	if len(g.Image) != 1 || g.Delay[0] != 5 {
		t.Errorf("%d frames, delays %v; want one frame of 5 cs (45 ms)", len(g.Image), g.Delay)
	}
}

func TestMaxDelayIsTheLongestGIFDelay(t *testing.T) {
	s, err := gif.NewSink(io.Discard, 1, 1, gif.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := s.MaxDelay(), 65535*10*time.Millisecond; got != want {
		t.Errorf("MaxDelay %v, want %v", got, want)
	}
}

func TestHoldsLongerThanTheLongestDelaySplitIntoTransparentFrames(t *testing.T) {
	img := solid(3, 3, color.RGBA{9, 9, 9, 255})
	var buf bytes.Buffer
	s, err := gif.NewSink(&buf, 3, 3, gif.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := s.AddFrame(img, s.MaxDelay()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	g := decodeGIF(t, buf.Bytes())
	if len(g.Image) != 3 {
		t.Fatalf("%d frames, want 3", len(g.Image))
	}
	for i, f := range g.Image {
		if g.Delay[i] != 65535 {
			t.Errorf("frame %d: delay %d cs, want 65535", i, g.Delay[i])
		}
		if i > 0 && f.Bounds().Size() != image.Pt(1, 1) {
			t.Errorf("frame %d covers %v, want one pixel", i, f.Bounds())
		}
	}
	for i, shown := range referenceCompositor(g) {
		if p, bad := firstDifference(shown, img); bad {
			t.Errorf("frame %d differs at %v", i, p)
		}
	}
}

// greyRenderer draws a 2x2-pixel-per-cell image whose grey level is the first
// byte of the first line.
type greyRenderer struct{}

func (greyRenderer) Size(cols, rows int) (int, int) { return cols * 2, rows * 2 }

func (greyRenderer) Render(s snapshot.Snapshot) (*image.RGBA, error) {
	img := image.NewRGBA(image.Rect(0, 0, s.Cols*2, s.Rows*2))
	g := s.Lines[0][0]
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = g, g, g, 0xff
	}
	return img, nil
}

type sliceSource struct {
	frames []snapshot.Frame
	n      int
}

func (s *sliceSource) Next() (snapshot.Frame, error) {
	if s.n >= len(s.frames) {
		return snapshot.Frame{}, io.EOF
	}
	s.n++
	return s.frames[s.n-1], nil
}

func TestPipelineSplitsLongHoldsAtMaxDelay(t *testing.T) {
	at := func(d time.Duration, text string) snapshot.Frame {
		return snapshot.Frame{At: d, Snapshot: snapshot.Snapshot{Cols: 4, Rows: 1, Lines: []string{text}}}
	}
	src := &sliceSource{frames: []snapshot.Frame{at(0, "a"), at(2000*time.Second, "b")}}
	var buf bytes.Buffer
	err := pipeline.Export(context.Background(), src, 4, 1, greyRenderer{}, &buf, pipeline.Options{
		Timeline: timeline.Options{Hold: 1000 * time.Second},
		NewSink:  gif.SinkFactory(gif.Options{}),
	})
	if err != nil {
		t.Fatal(err)
	}
	g := decodeGIF(t, buf.Bytes())
	for i, d := range g.Delay {
		if d < 2 || d > 65535 {
			t.Errorf("frame %d: delay %d cs out of range", i, d)
		}
	}
	if got := sumCentiseconds(g.Delay); got < 299999 || got > 300001 {
		t.Errorf("total %d cs, want 300000 (3000 s) within 1", got)
	}
	shown := referenceCompositor(g)
	first, last := shown[0], shown[len(shown)-1]
	if first.RGBAAt(0, 0).R != 'a' || last.RGBAAt(0, 0).R != 'b' {
		t.Errorf("first frame grey %d, last %d; want %d and %d", first.RGBAAt(0, 0).R, last.RGBAAt(0, 0).R, 'a', 'b')
	}
	if len(g.Image) < 5 {
		t.Errorf("%d frames; the 2000 s and 1000 s holds need at least 5", len(g.Image))
	}
}

func TestLoopCountMirrorsTheAPNGPlayCount(t *testing.T) {
	// Loops is the number of plays, 0 forever; image/gif LoopCount is 0 for
	// forever, -1 for once, and n for n repeats after the first play.
	for _, c := range []struct {
		loops, want int
	}{{0, 0}, {1, -1}, {2, 1}, {3, 2}, {65536, 65535}} {
		g := decodeGIF(t, encodeFrames(t, gif.Options{Loops: c.loops}, alternating(2), evenDelays(2, time.Second)))
		if g.LoopCount != c.want {
			t.Errorf("Loops %d: LoopCount %d, want %d", c.loops, g.LoopCount, c.want)
		}
	}
}

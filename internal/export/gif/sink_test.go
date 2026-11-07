package gif_test

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	stdgif "image/gif"
	"math"
	"math/rand"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/gif"
)

func TestExactPaletteRoundTripsPixelExact(t *testing.T) {
	for _, colours := range []int{1, 2, 17, 200, 254, 255} {
		rng := rand.New(rand.NewSource(int64(colours)))
		pool := colourPool(rng, colours)
		frames := evolve(rng, 40, 30, 12, pool)
		// Paint every colour so the palette really has `colours` entries.
		for i, c := range pool {
			frames[0].SetRGBA(i%40, i/40, c)
		}
		g := decodeGIF(t, encodeFrames(t, gif.Options{}, frames, evenDelays(len(frames), 50*time.Millisecond)))
		assertPlayback(t, g, frames, colours)
	}
}

// assertPlayback checks that playing g shows every input frame exactly, where
// each run of equal inputs is one GIF frame.
func assertPlayback(t *testing.T, g *stdgif.GIF, frames []*image.RGBA, id any) {
	t.Helper()
	shown := referenceCompositor(g)
	j := 0
	for i, f := range frames {
		if i > 0 && !samePixels(f, frames[i-1]) {
			j++
		}
		if j >= len(shown) {
			t.Fatalf("%v: ran out of GIF frames at input %d", id, i)
		}
		if p, bad := firstDifference(shown[j], f); bad {
			t.Fatalf("%v: input %d differs from GIF frame %d at %v", id, i, j, p)
		}
	}
	if j != len(shown)-1 {
		t.Errorf("%v: %d GIF frames for %d distinct runs", id, len(shown), j+1)
	}
}

func TestEveryFrameIsRecompositedFromDeltas(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	pool := colourPool(rng, 100)
	frames := evolve(rng, 64, 48, 40, pool)
	// A distinct delay per frame lets us find which input each GIF frame is.
	delays := make([]time.Duration, len(frames))
	for i := range delays {
		delays[i] = 20 * time.Millisecond
	}
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, frames, delays))
	shown := referenceCompositor(g)
	// Replay the input, merging equal neighbours as the sink does.
	var want []*image.RGBA
	for i, f := range frames {
		if i == 0 || !samePixels(f, frames[i-1]) {
			want = append(want, f)
		}
	}
	if len(shown) != len(want) {
		t.Fatalf("%d GIF frames, want %d distinct inputs", len(shown), len(want))
	}
	for i := range want {
		if p, bad := firstDifference(shown[i], want[i]); bad {
			t.Fatalf("frame %d differs at %v", i, p)
		}
	}
	for i, f := range g.Image {
		if g.Disposal[i] != stdgif.DisposalNone {
			t.Errorf("frame %d: disposal %d, want 1 (do not dispose)", i, g.Disposal[i])
		}
		if i == 0 && f.Bounds() != image.Rect(0, 0, 64, 48) {
			t.Errorf("first frame covers %v, want the whole canvas", f.Bounds())
		}
	}
}

func samePixels(a, b *image.RGBA) bool {
	_, bad := firstDifference(a, b)
	return !bad
}

func TestDeltaFrameCoversOnlyTheChangedRectangle(t *testing.T) {
	a := solid(30, 20, color.RGBA{10, 20, 30, 255})
	b := clone(a)
	b.SetRGBA(5, 4, color.RGBA{200, 0, 0, 255})
	b.SetRGBA(12, 9, color.RGBA{0, 200, 0, 255})
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, []*image.RGBA{a, b}, evenDelays(2, 100*time.Millisecond)))
	if len(g.Image) != 2 {
		t.Fatalf("%d frames, want 2", len(g.Image))
	}
	f := g.Image[1]
	if want := image.Rect(5, 4, 13, 10); f.Bounds() != want {
		t.Fatalf("delta rect %v, want %v", f.Bounds(), want)
	}
	transparent := 0
	for y := f.Bounds().Min.Y; y < f.Bounds().Max.Y; y++ {
		for x := f.Bounds().Min.X; x < f.Bounds().Max.X; x++ {
			if _, _, _, a := f.At(x, y).RGBA(); a == 0 {
				transparent++
			}
		}
	}
	if want := 8*6 - 2; transparent != want {
		t.Errorf("%d transparent pixels in the delta, want %d", transparent, want)
	}
	if g.Config.Width != 30 || g.Config.Height != 20 {
		t.Errorf("canvas %dx%d, want 30x20", g.Config.Width, g.Config.Height)
	}
}

func TestIdenticalFramesMergeDelays(t *testing.T) {
	a := solid(8, 8, color.RGBA{1, 2, 3, 255})
	b := clone(a)
	b.SetRGBA(0, 0, color.RGBA{9, 9, 9, 255})
	frames := []*image.RGBA{a, a, clone(a), b, b}
	delays := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 300 * time.Millisecond, 400 * time.Millisecond, 500 * time.Millisecond}
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, frames, delays))
	if len(g.Image) != 2 {
		t.Fatalf("%d frames, want 2", len(g.Image))
	}
	if g.Delay[0] != 60 || g.Delay[1] != 90 {
		t.Errorf("delays %v cs, want [60 90]", g.Delay)
	}
}

func TestQuantisedFrameChangeInvisibleAfterMappingIsMerged(t *testing.T) {
	// Two colours of a gradient too close to differ in the palette: the second
	// frame changes the RGBA pixels but not the GIF indices.
	grad := gradient(300, 200, 0)
	near := clone(grad)
	c := near.RGBAAt(150, 100)
	c.B ^= 1
	near.SetRGBA(150, 100, c)
	g := decodeGIF(t, encodeFrames(t, gif.Options{Colors: 8}, []*image.RGBA{grad, near}, evenDelays(2, 100*time.Millisecond)))
	if len(g.Image) != 1 || g.Delay[0] != 20 {
		t.Errorf("%d frames with delays %v, want 1 frame of 20 cs", len(g.Image), g.Delay)
	}
}

func TestQuantisedPaletteStaysWithinMeasuredError(t *testing.T) {
	frames := []*image.RGBA{gradient(240, 180, 0), gradient(240, 180, 40)}
	distinct := countColours(frames)
	if distinct <= 255 {
		t.Fatalf("test input has only %d colours", distinct)
	}
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, frames, evenDelays(2, 100*time.Millisecond)))
	if n := len(g.Config.ColorModel.(color.Palette)); n > 256 {
		t.Fatalf("palette of %d entries", n)
	}
	shown := referenceCompositor(g)
	mean, worst := colourError(shown[len(shown)-1], frames[1])
	t.Logf("%d colours -> 255: mean error %.2f, worst %.0f per channel", distinct, mean, worst)
	if mean > 6 || worst > 40 {
		t.Errorf("mean error %.2f, worst %.0f; want at most 6 and 40", mean, worst)
	}
}

func TestColorsOptionLimitsThePalette(t *testing.T) {
	frames := []*image.RGBA{gradient(100, 80, 0)}
	for _, n := range []int{2, 3, 16, 100} {
		g := decodeGIF(t, encodeFrames(t, gif.Options{Colors: n}, frames, evenDelays(1, time.Second)))
		seen := map[color.Color]bool{}
		for _, p := range g.Image[0].Pix {
			seen[g.Image[0].Palette[p]] = true
		}
		if len(seen) > n {
			t.Errorf("Colors %d: %d colours used", n, len(seen))
		}
	}
}

func TestDitherChangesQuantisedOutputAndIsIgnoredForExactPalettes(t *testing.T) {
	grad := []*image.RGBA{gradient(120, 90, 0)}
	plain := encodeFrames(t, gif.Options{Colors: 8}, grad, evenDelays(1, time.Second))
	dithered := encodeFrames(t, gif.Options{Colors: 8, Dither: true}, grad, evenDelays(1, time.Second))
	if string(plain) == string(dithered) {
		t.Error("dither left a quantised GIF unchanged")
	}
	g := decodeGIF(t, dithered)
	mean, _ := colourError(referenceCompositor(g)[0], grad[0])
	t.Logf("8 colours dithered: mean error %.1f", mean)

	rng := rand.New(rand.NewSource(3))
	exact := evolve(rng, 20, 20, 5, colourPool(rng, 30))
	d := evenDelays(len(exact), 40*time.Millisecond)
	if string(encodeFrames(t, gif.Options{}, exact, d)) != string(encodeFrames(t, gif.Options{Dither: true}, exact, d)) {
		t.Error("dither changed an exact palette")
	}
}

func TestDitheredFramesRecomposite(t *testing.T) {
	frames := []*image.RGBA{gradient(100, 80, 0), gradient(100, 80, 25), gradient(100, 80, 60)}
	g := decodeGIF(t, encodeFrames(t, gif.Options{Colors: 16, Dither: true}, frames, evenDelays(3, 50*time.Millisecond)))
	shown := referenceCompositor(g)
	for i := range frames {
		mean, _ := colourError(shown[i], frames[i])
		if mean > 40 {
			t.Errorf("frame %d: mean error %.1f with 16 dithered colours", i, mean)
		}
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	rng := rand.New(rand.NewSource(11))
	exact := evolve(rng, 50, 40, 20, colourPool(rng, 120))
	many := []*image.RGBA{gradient(120, 90, 0), gradient(120, 90, 30)}
	for name, frames := range map[string][]*image.RGBA{"exact": exact, "quantised": many} {
		for _, o := range []gif.Options{{}, {Dither: true}} {
			d := evenDelays(len(frames), 70*time.Millisecond)
			first := encodeFrames(t, o, frames, d)
			for range 4 {
				if string(encodeFrames(t, o, frames, d)) != string(first) {
					t.Fatalf("%s %+v: two encodes differ", name, o)
				}
			}
		}
	}
}

func TestOnePixelAndLargeCanvases(t *testing.T) {
	one := []*image.RGBA{solid(1, 1, color.RGBA{4, 5, 6, 255}), solid(1, 1, color.RGBA{7, 8, 9, 255})}
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, one, evenDelays(2, 100*time.Millisecond)))
	shown := referenceCompositor(g)
	for i := range one {
		if p, bad := firstDifference(shown[i], one[i]); bad {
			t.Errorf("1x1 frame %d differs at %v", i, p)
		}
	}

	big := []*image.RGBA{gradient(1600, 1200, 0), gradient(1600, 1200, 9)}
	g = decodeGIF(t, encodeFrames(t, gif.Options{}, big, evenDelays(2, 100*time.Millisecond)))
	if g.Config.Width != 1600 || g.Config.Height != 1200 {
		t.Errorf("canvas %dx%d", g.Config.Width, g.Config.Height)
	}
	shown = referenceCompositor(g)
	mean, _ := colourError(shown[len(shown)-1], big[1])
	if mean > 6 {
		t.Errorf("large canvas mean error %.2f", mean)
	}
}

func TestSourceImageWithOffsetOrigin(t *testing.T) {
	// The image is a sub-image of a bigger one: its Rect.Min is not (0,0).
	base := gradient(60, 40, 0)
	sub := base.SubImage(image.Rect(10, 5, 40, 25)).(*image.RGBA)
	other := clone(base)
	other.SetRGBA(20, 10, color.RGBA{255, 255, 255, 255})
	sub2 := other.SubImage(image.Rect(10, 5, 40, 25)).(*image.RGBA)
	g := decodeGIF(t, encodeFrames(t, gif.Options{}, []*image.RGBA{sub, sub2}, evenDelays(2, 50*time.Millisecond)))
	shown := referenceCompositor(g)
	if shown[1].At(10, 5) != sub2.At(20, 10) {
		t.Errorf("pixel (10,5) is %v, want %v", shown[1].At(10, 5), sub2.At(20, 10))
	}
}

func TestRandomSequencesMatchTheReferenceCompositor(t *testing.T) {
	for seed := int64(0); seed < 30; seed++ {
		rng := rand.New(rand.NewSource(seed))
		w, h := 1+rng.Intn(70), 1+rng.Intn(50)
		pool := colourPool(rng, 1+rng.Intn(255))
		frames := evolve(rng, w, h, 1+rng.Intn(25), pool)
		delays := make([]time.Duration, len(frames))
		for i := range delays {
			delays[i] = time.Duration(rng.Intn(500)) * time.Millisecond
		}
		g := decodeGIF(t, encodeFrames(t, gif.Options{}, frames, delays))
		assertPlayback(t, g, frames, seed)
	}
}

func TestSinkErrors(t *testing.T) {
	var sink discard
	for _, c := range []struct {
		name string
		w, h int
		o    gif.Options
	}{
		{"zero width", 0, 10, gif.Options{}},
		{"negative height", 10, -1, gif.Options{}},
		{"too wide", 65536, 10, gif.Options{}},
		{"one colour", 10, 10, gif.Options{Colors: 1}},
		{"256 colours", 10, 10, gif.Options{Colors: 256}},
		{"negative colours", 10, 10, gif.Options{Colors: -3}},
		{"negative loops", 10, 10, gif.Options{Loops: -1}},
		{"too many loops", 10, 10, gif.Options{Loops: 65537}},
	} {
		if _, err := gif.NewSink(sink, c.w, c.h, c.o); err == nil {
			t.Errorf("%s: no error", c.name)
		}
	}

	s, err := gif.NewSink(sink, 4, 4, gif.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddFrame(solid(5, 4, color.RGBA{A: 255}), time.Second); !errors.Is(err, gif.ErrFrameSize) {
		t.Errorf("wrong size: %v, want ErrFrameSize", err)
	}
	if err := s.AddFrame(solid(4, 4, color.RGBA{A: 255}), -time.Second); !errors.Is(err, gif.ErrDelay) {
		t.Errorf("negative delay: %v, want ErrDelay", err)
	}
	if err := s.Close(); !errors.Is(err, gif.ErrNoFrames) {
		t.Errorf("Close without frames: %v, want ErrNoFrames", err)
	}
	if err := s.Close(); !errors.Is(err, gif.ErrNoFrames) {
		t.Errorf("second Close: %v, want the first result", err)
	}
	if err := s.AddFrame(solid(4, 4, color.RGBA{A: 255}), time.Second); !errors.Is(err, gif.ErrClosed) {
		t.Errorf("AddFrame after Close: %v, want ErrClosed", err)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestNothingIsWrittenBeforeClose(t *testing.T) {
	var w countingWriter
	s, err := gif.NewSink(&w, 4, 4, gif.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := s.AddFrame(solid(4, 4, color.RGBA{A: 255}), time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if w.n != 0 {
		t.Errorf("%d bytes written before Close", w.n)
	}
	if err := s.Close(); err != nil || w.n == 0 {
		t.Errorf("Close: %v, %d bytes", err, w.n)
	}
}

type countingWriter struct{ n int }

func (c *countingWriter) Write(p []byte) (int, error) { c.n += len(p); return len(p), nil }

// solid, clone, gradient and the error helpers build and compare test images.

func solid(w, h int, c color.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
	}
	return img
}

func clone(src *image.RGBA) *image.RGBA {
	dst := image.NewRGBA(src.Rect)
	copy(dst.Pix, src.Pix)
	return dst
}

// gradient is a smooth w x h colour ramp with thousands of distinct colours;
// shift moves it so successive frames differ.
func gradient(w, h, shift int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			img.SetRGBA(x, y, color.RGBA{
				R: uint8((x*255/w + shift) % 256),
				G: uint8(y * 255 / h),
				B: uint8(((x+y)*255/(w+h) + 2*shift) % 256),
				A: 255,
			})
		}
	}
	return img
}

func countColours(frames []*image.RGBA) int {
	seen := map[[3]uint8]bool{}
	for _, f := range frames {
		for i := 0; i < len(f.Pix); i += 4 {
			seen[[3]uint8{f.Pix[i], f.Pix[i+1], f.Pix[i+2]}] = true
		}
	}
	return len(seen)
}

// colourError returns the mean and largest per-channel difference between
// got and want.
func colourError(got, want *image.RGBA) (mean, worst float64) {
	var sum float64
	for i := 0; i < len(want.Pix); i += 4 {
		for c := range 3 {
			d := math.Abs(float64(got.Pix[i+c]) - float64(want.Pix[i+c]))
			sum += d
			worst = max(worst, d)
		}
	}
	return sum / float64(len(want.Pix)/4*3), worst
}

func TestFramesCountsTheFramesWritten(t *testing.T) {
	var buf bytes.Buffer
	s, err := gif.NewSink(&buf, 8, 8, gif.Options{})
	if err != nil {
		t.Fatal(err)
	}
	a, b := image.NewRGBA(image.Rect(0, 0, 8, 8)), image.NewRGBA(image.Rect(0, 0, 8, 8))
	b.Pix[0], b.Pix[3] = 0xff, 0xff
	for _, img := range []*image.RGBA{a, a, b, b, a} {
		if err := s.AddFrame(img, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if n := s.Frames(); n != 0 {
		t.Errorf("Frames before Close = %d, want 0", n)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	g, err := stdgif.DecodeAll(&buf)
	if err != nil {
		t.Fatal(err)
	}
	// Five images, three pictures.
	if n := s.Frames(); n != 3 || n != len(g.Image) {
		t.Errorf("Frames = %d, file holds %d, want 3", n, len(g.Image))
	}
}

package webp

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"math/rand"
	"testing"
	"time"
)

// toNRGBA converts img the way the Sink does, for comparing pixels.
func toNRGBA(img *image.RGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	draw.Draw(out, out.Bounds(), img, img.Bounds().Min, draw.Src)
	return out
}

func newSink(t testing.TB, w, h int, o Options) (*Sink, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	s, err := NewSink(&buf, w, h, o)
	if err != nil {
		t.Fatal(err)
	}
	return s, &buf
}

// terminalLike returns an image of a few flat colours with noisy blocks,
// like a rendered screen.
func terminalLike(rng *rand.Rand, w, h int, alpha bool) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	palette := []color.RGBA{{0, 0, 0, 255}, {255, 255, 255, 255}, {200, 30, 30, 255}, {30, 200, 90, 255}}
	if alpha {
		palette = append(palette, color.RGBA{}, color.RGBA{40, 20, 10, 128})
	}
	draw.Draw(img, img.Bounds(), image.NewUniform(palette[0]), image.Point{}, draw.Src)
	for i := 0; i < 1+w*h/40; i++ {
		randomBlock(rng, img, palette)
	}
	return img
}

// randomBlock fills a random small rectangle of img with random colours from
// palette, a flat one or per-pixel noise.
func randomBlock(rng *rand.Rand, img *image.RGBA, palette []color.RGBA) {
	b := img.Bounds()
	w, h := 1+rng.Intn(min(b.Dx(), 9)), 1+rng.Intn(min(b.Dy(), 9))
	x, y := rng.Intn(b.Dx()-w+1), rng.Intn(b.Dy()-h+1)
	flat := palette[rng.Intn(len(palette))]
	noisy := rng.Intn(2) == 0
	for py := y; py < y+h; py++ {
		for px := x; px < x+w; px++ {
			c := flat
			if noisy {
				c = palette[rng.Intn(len(palette))]
			}
			img.SetRGBA(px, py, c)
		}
	}
}

// sequence builds count frames where each differs from the previous by a few
// random small blocks (and is sometimes identical to it).
func sequence(rng *rand.Rand, w, h, count int, alpha bool) []*image.RGBA {
	palette := []color.RGBA{{0, 0, 0, 255}, {255, 255, 255, 255}, {200, 30, 30, 255}, {30, 200, 90, 255}, {7, 9, 11, 255}}
	if alpha {
		palette = append(palette, color.RGBA{}, color.RGBA{40, 20, 10, 128})
	}
	frames := []*image.RGBA{terminalLike(rng, w, h, alpha)}
	for i := 1; i < count; i++ {
		next := image.NewRGBA(frames[i-1].Rect)
		copy(next.Pix, frames[i-1].Pix)
		if rng.Intn(5) != 0 {
			for j := rng.Intn(3); j >= 0; j-- {
				randomBlock(rng, next, palette)
			}
		}
		frames = append(frames, next)
	}
	return frames
}

func TestRoundTripRandomDeltas(t *testing.T) {
	sizes := [][2]int{{1, 1}, {1, 7}, {2, 2}, {3, 5}, {16, 9}, {33, 17}, {64, 40}}
	for _, alpha := range []bool{false, true} {
		for _, sz := range sizes {
			t.Run(fmt.Sprintf("%dx%d/alpha=%v", sz[0], sz[1], alpha), func(t *testing.T) {
				rng := rand.New(rand.NewSource(int64(sz[0]*1000 + sz[1])))
				frames := sequence(rng, sz[0], sz[1], 12, alpha)
				s, buf := newSink(t, sz[0], sz[1], Options{})
				for _, f := range frames {
					if err := s.AddFrame(f, 40*time.Millisecond); err != nil {
						t.Fatal(err)
					}
				}
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
				file, err := parseWebP(buf.Bytes())
				if err != nil {
					t.Fatal(err)
				}
				if file.width != sz[0] || file.height != sz[1] {
					t.Errorf("canvas %dx%d, want %dx%d", file.width, file.height, sz[0], sz[1])
				}
				decoded, err := decodeFrames(file)
				if err != nil {
					t.Fatal(err)
				}
				// Distinct consecutive inputs map to one output frame each;
				// identical ones are merged into the previous frame.
				k := 0
				for i, f := range frames {
					if i > 0 && bytes.Equal(f.Pix, frames[i-1].Pix) {
						continue
					}
					if k >= len(decoded) {
						t.Fatalf("input frame %d has no output frame (%d written)", i, len(decoded))
					}
					if !bytes.Equal(decoded[k].Pix, toNRGBA(f).Pix) {
						t.Errorf("input frame %d, output frame %d: pixels differ", i, k)
					}
					k++
				}
				if k != len(decoded) {
					t.Errorf("%d output frames, want %d", len(decoded), k)
				}
			})
		}
	}
}

func TestFramesAreEvenAlignedNoBlendDeltas(t *testing.T) {
	rng := rand.New(rand.NewSource(7))
	w, h := 40, 30
	s, buf := newSink(t, w, h, Options{})
	prev := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(prev, prev.Bounds(), image.Black, image.Point{}, draw.Src)
	if err := s.AddFrame(prev, 50*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	// Change single pixels at odd coordinates.
	var want []image.Rectangle
	for i := 0; i < 20; i++ {
		x, y := rng.Intn(w), rng.Intn(h)
		next := image.NewRGBA(prev.Rect)
		copy(next.Pix, prev.Pix)
		c := color.RGBA{uint8(i + 1), 100, 200, 255}
		next.SetRGBA(x, y, c)
		if err := s.AddFrame(next, 50*time.Millisecond); err != nil {
			t.Fatal(err)
		}
		want = append(want, image.Rect(x&^1, y&^1, x+1, y+1))
		prev = next
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := parseWebP(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if got := len(file.frames); got != 21 {
		t.Fatalf("%d frames, want 21", got)
	}
	first := file.frames[0]
	if first.x != 0 || first.y != 0 || first.w != w || first.h != h {
		t.Errorf("first frame %+v does not cover the canvas", first)
	}
	for i, fr := range file.frames {
		if fr.x%2 != 0 || fr.y%2 != 0 {
			t.Errorf("frame %d starts at odd offset (%d,%d)", i, fr.x, fr.y)
		}
		if fr.flags != 0x02 {
			t.Errorf("frame %d flags %#x, want 0x02 (do not blend, dispose none)", i, fr.flags)
		}
		if i > 0 {
			got := image.Rect(fr.x, fr.y, fr.x+fr.w, fr.y+fr.h)
			if got != want[i-1] {
				t.Errorf("frame %d rectangle %v, want %v", i, got, want[i-1])
			}
		}
	}
}

func TestIdenticalFramesMerge(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	a := terminalLike(rng, 20, 10, false)
	b := image.NewRGBA(a.Rect)
	copy(b.Pix, a.Pix)
	b.SetRGBA(3, 3, color.RGBA{1, 2, 3, 255})
	s, buf := newSink(t, 20, 10, Options{})
	for _, step := range []struct {
		img   *image.RGBA
		delay time.Duration
	}{{a, 100 * time.Millisecond}, {a, 200 * time.Millisecond}, {a, 300 * time.Millisecond}, {b, 50 * time.Millisecond}, {b, 25 * time.Millisecond}} {
		if err := s.AddFrame(step.img, step.delay); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := parseWebP(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(file.frames) != 2 {
		t.Fatalf("%d frames, want 2", len(file.frames))
	}
	if got := file.frames[0].delayMillis; got != 600 {
		t.Errorf("first delay %d ms, want 600", got)
	}
	if got := file.frames[1].delayMillis; got != 75 {
		t.Errorf("second delay %d ms, want 75", got)
	}
}

func TestMergeOverflowAddsOnePixelFrame(t *testing.T) {
	rng := rand.New(rand.NewSource(2))
	a := terminalLike(rng, 9, 9, false)
	s, buf := newSink(t, 9, 9, Options{})
	for i := 0; i < 3; i++ {
		if err := s.AddFrame(a, s.MaxDelay()); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := parseWebP(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if len(file.frames) != 3 {
		t.Fatalf("%d frames, want 3 (each full-length delay needs its own)", len(file.frames))
	}
	for i, fr := range file.frames {
		if fr.delayMillis != 1<<24-1 {
			t.Errorf("frame %d delay %d, want 16777215", i, fr.delayMillis)
		}
		if i > 0 && (fr.x != 0 || fr.y != 0 || fr.w != 1 || fr.h != 1) {
			t.Errorf("frame %d is %+v, want a 1x1 frame at the origin", i, fr)
		}
	}
	decoded, err := decodeFrames(file)
	if err != nil {
		t.Fatal(err)
	}
	for i, d := range decoded {
		if !bytes.Equal(d.Pix, toNRGBA(a).Pix) {
			t.Errorf("frame %d differs from input", i)
		}
	}
}

func TestDelays(t *testing.T) {
	tests := []struct {
		delay time.Duration
		want  uint32
	}{
		{0, 10},
		{time.Millisecond, 10},
		{9*time.Millisecond + 400*time.Microsecond, 10},
		{10 * time.Millisecond, 10},
		{20 * time.Millisecond, 20},
		{20*time.Millisecond + 499*time.Microsecond, 20},
		{20*time.Millisecond + 500*time.Microsecond, 21},
		{1500 * time.Millisecond, 1500},
		{(1<<24 - 1) * time.Millisecond, 1<<24 - 1},
	}
	for _, tt := range tests {
		t.Run(tt.delay.String(), func(t *testing.T) {
			s, buf := newSink(t, 2, 2, Options{})
			if err := s.AddFrame(image.NewRGBA(image.Rect(0, 0, 2, 2)), tt.delay); err != nil {
				t.Fatal(err)
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			file, err := parseWebP(buf.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if got := file.frames[0].delayMillis; got != tt.want {
				t.Errorf("delay %d ms, want %d", got, tt.want)
			}
		})
	}
}

func TestMaxDelayIs24BitMillis(t *testing.T) {
	s, _ := newSink(t, 1, 1, Options{})
	if got, want := s.MaxDelay(), 16777215*time.Millisecond; got != want {
		t.Errorf("MaxDelay %v, want %v", got, want)
	}
}

func TestDelayOutOfRange(t *testing.T) {
	for _, d := range []time.Duration{-1, (1<<24-1)*time.Millisecond + time.Millisecond} {
		s, _ := newSink(t, 2, 2, Options{})
		err := s.AddFrame(image.NewRGBA(image.Rect(0, 0, 2, 2)), d)
		if !errors.Is(err, ErrDelay) {
			t.Errorf("delay %v: error %v, want ErrDelay", d, err)
		}
	}
}

func TestLoopCount(t *testing.T) {
	for _, loops := range []int{0, 1, 3, 65535} {
		s, buf := newSink(t, 2, 2, Options{Loops: loops})
		if err := s.AddFrame(image.NewRGBA(image.Rect(0, 0, 2, 2)), time.Second); err != nil {
			t.Fatal(err)
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		file, err := parseWebP(buf.Bytes())
		if err != nil {
			t.Fatal(err)
		}
		if int(file.loops) != loops {
			t.Errorf("loops %d, want %d", file.loops, loops)
		}
		if file.background != [4]byte{} {
			t.Errorf("background %v, want transparent black", file.background)
		}
	}
}

func TestAlphaFlagOnlyWhenNeeded(t *testing.T) {
	rng := rand.New(rand.NewSource(3))
	opaque := terminalLike(rng, 12, 12, false)
	clear := image.NewRGBA(opaque.Rect)
	copy(clear.Pix, opaque.Pix)
	clear.SetRGBA(5, 5, color.RGBA{})

	for _, tt := range []struct {
		name   string
		frames []*image.RGBA
		alpha  bool
	}{
		{"opaque", []*image.RGBA{opaque}, false},
		{"transparent pixel in a later delta", []*image.RGBA{opaque, clear}, true},
		{"transparent first frame", []*image.RGBA{clear}, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s, buf := newSink(t, 12, 12, Options{})
			for _, f := range tt.frames {
				if err := s.AddFrame(f, time.Second); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
			file, err := parseWebP(buf.Bytes())
			if err != nil {
				t.Fatal(err)
			}
			if got := file.vp8xFlags&vp8xFlagAlpha != 0; got != tt.alpha {
				t.Errorf("alpha flag %v, want %v", got, tt.alpha)
			}
			if file.vp8xFlags&vp8xFlagAnimation == 0 {
				t.Error("animation flag not set")
			}
			if file.vp8xFlags&^(vp8xFlagAlpha|vp8xFlagAnimation) != 0 {
				t.Errorf("unexpected VP8X flags %#x", file.vp8xFlags)
			}
		})
	}
}

func TestDeterministic(t *testing.T) {
	encode := func() []byte {
		rng := rand.New(rand.NewSource(11))
		s, buf := newSink(t, 48, 30, Options{Loops: 2})
		for _, f := range sequence(rng, 48, 30, 10, true) {
			if err := s.AddFrame(f, 30*time.Millisecond); err != nil {
				t.Fatal(err)
			}
		}
		if err := s.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	first := encode()
	for i := 0; i < 3; i++ {
		if !bytes.Equal(first, encode()) {
			t.Fatal("output differs between runs")
		}
	}
}

func TestAddFrameDoesNotRetainOrModifyInput(t *testing.T) {
	rng := rand.New(rand.NewSource(5))
	a := terminalLike(rng, 10, 10, false)
	orig := append([]uint8(nil), a.Pix...)
	s, buf := newSink(t, 10, 10, Options{})
	if err := s.AddFrame(a, time.Second); err != nil {
		t.Fatal(err)
	}
	for i := range a.Pix {
		a.Pix[i] = 0
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := parseWebP(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeFrames(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded[0].Pix, toNRGBA(&image.RGBA{Pix: orig, Stride: a.Stride, Rect: a.Rect}).Pix) {
		t.Error("frame changed after the caller overwrote its image")
	}
}

func TestAcceptsImageWithNonZeroOrigin(t *testing.T) {
	rng := rand.New(rand.NewSource(6))
	a := terminalLike(rng, 8, 6, false)
	shifted := a.SubImage(a.Rect).(*image.RGBA)
	big := image.NewRGBA(image.Rect(0, 0, 20, 20))
	draw.Draw(big, image.Rect(4, 4, 12, 10), a, image.Point{}, draw.Src)
	shifted = big.SubImage(image.Rect(4, 4, 12, 10)).(*image.RGBA)
	s, buf := newSink(t, 8, 6, Options{})
	if err := s.AddFrame(shifted, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := parseWebP(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := decodeFrames(file)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(decoded[0].Pix, toNRGBA(a).Pix) {
		t.Error("pixels differ")
	}
}

func TestNewSinkValidation(t *testing.T) {
	tests := []struct {
		name string
		w, h int
		o    Options
		ok   bool
	}{
		{"min", 1, 1, Options{}, true},
		{"max canvas", 16384, 1, Options{}, true},
		{"zero width", 0, 5, Options{}, false},
		{"negative height", 5, -1, Options{}, false},
		{"too wide", 16385, 1, Options{}, false},
		{"too tall", 1, 16385, Options{}, false},
		{"negative loops", 5, 5, Options{Loops: -1}, false},
		{"too many loops", 5, 5, Options{Loops: 65536}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewSink(&bytes.Buffer{}, tt.w, tt.h, tt.o)
			if (err == nil) != tt.ok {
				t.Errorf("error %v, want ok=%v", err, tt.ok)
			}
		})
	}
}

func TestFrameSizeMismatch(t *testing.T) {
	s, _ := newSink(t, 4, 4, Options{})
	err := s.AddFrame(image.NewRGBA(image.Rect(0, 0, 4, 5)), time.Second)
	if !errors.Is(err, ErrFrameSize) {
		t.Errorf("error %v, want ErrFrameSize", err)
	}
}

func TestCloseWithoutFrames(t *testing.T) {
	s, buf := newSink(t, 4, 4, Options{})
	if err := s.Close(); !errors.Is(err, ErrNoFrames) {
		t.Errorf("Close error %v, want ErrNoFrames", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes without frames", buf.Len())
	}
}

func TestCloseIdempotentAndBlocksAddFrame(t *testing.T) {
	s, buf := newSink(t, 4, 4, Options{})
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if err := s.AddFrame(img, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	n := buf.Len()
	if err := s.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
	if buf.Len() != n {
		t.Error("second Close wrote again")
	}
	if err := s.AddFrame(img, time.Second); !errors.Is(err, ErrClosed) {
		t.Errorf("AddFrame after Close: %v, want ErrClosed", err)
	}
}

type failWriter struct{ err error }

func (f failWriter) Write([]byte) (int, error) { return 0, f.err }

func TestCloseReportsWriteError(t *testing.T) {
	boom := errors.New("boom")
	s, err := NewSink(failWriter{boom}, 4, 4, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddFrame(image.NewRGBA(image.Rect(0, 0, 4, 4)), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); !errors.Is(err, boom) {
		t.Errorf("Close error %v, want boom", err)
	}
	if err := s.Close(); !errors.Is(err, boom) {
		t.Errorf("second Close error %v, want boom", err)
	}
}

func TestNothingWrittenBeforeClose(t *testing.T) {
	s, buf := newSink(t, 4, 4, Options{})
	if err := s.AddFrame(image.NewRGBA(image.Rect(0, 0, 4, 4)), time.Second); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes before Close", buf.Len())
	}
}

func TestSinkFactory(t *testing.T) {
	var buf bytes.Buffer
	sink, err := SinkFactory(4)(&buf, 6, 6)
	if err != nil {
		t.Fatal(err)
	}
	if err := sink.AddFrame(image.NewRGBA(image.Rect(0, 0, 6, 6)), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := sink.Close(); err != nil {
		t.Fatal(err)
	}
	file, err := parseWebP(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if file.loops != 4 {
		t.Errorf("loops %d, want 4", file.loops)
	}
	if _, err := SinkFactory(-1)(&buf, 6, 6); err == nil {
		t.Error("negative loops accepted")
	}
}

package apng_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"strings"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/mikelorant/shellshot/internal/export/apng"
)

type chunk struct {
	typ  string
	data []byte
}

// walkChunks splits an APNG/PNG into chunks, verifying the signature, bounds
// and every CRC.
func walkChunks(t *testing.T, b []byte) []chunk {
	t.Helper()
	const sig = "\x89PNG\r\n\x1a\n"
	if !strings.HasPrefix(string(b), sig) {
		t.Fatal("missing PNG signature")
	}
	b = b[len(sig):]
	var chunks []chunk
	for len(b) > 0 {
		if len(b) < 12 {
			t.Fatalf("truncated chunk header, %d bytes left", len(b))
		}
		n := int(binary.BigEndian.Uint32(b[:4]))
		if len(b) < 12+n {
			t.Fatalf("truncated chunk %q", b[4:8])
		}
		typ := string(b[4:8])
		data := b[8 : 8+n]
		want := binary.BigEndian.Uint32(b[8+n : 12+n])
		if got := crc32.ChecksumIEEE(b[4 : 8+n]); got != want {
			t.Fatalf("chunk %q: CRC %08x, want %08x", typ, got, want)
		}
		chunks = append(chunks, chunk{typ, data})
		b = b[12+n:]
	}
	return chunks
}

func solid(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func noisy(w, h int, seed int64) *image.NRGBA {
	r := rand.New(rand.NewSource(seed))
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	r.Read(img.Pix)
	return img
}

func sameNRGBA(t *testing.T, label string, got, want image.Image) {
	t.Helper()
	if got.Bounds().Size() != want.Bounds().Size() {
		t.Fatalf("%s: size %v, want %v", label, got.Bounds().Size(), want.Bounds().Size())
	}
	gb, wb := got.Bounds(), want.Bounds()
	for y := 0; y < wb.Dy(); y++ {
		for x := 0; x < wb.Dx(); x++ {
			g := color.NRGBAModel.Convert(got.At(gb.Min.X+x, gb.Min.Y+y))
			w := color.NRGBAModel.Convert(want.At(wb.Min.X+x, wb.Min.Y+y))
			if g != w {
				t.Fatalf("%s: pixel (%d,%d) = %v, want %v", label, x, y, g, w)
			}
		}
	}
}

func encode(t *testing.T, w, h int, opts apng.Options, frames []image.Image, delays []time.Duration) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, w, h, opts)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames {
		if err := enc.AddFrame(f, delays[i]); err != nil {
			t.Fatalf("AddFrame %d: %v", i, err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestSingleFrameDecodesAsPNG(t *testing.T) {
	t.Parallel()
	src := noisy(17, 9, 1)
	out := encode(t, 17, 9, apng.Options{}, []image.Image{src}, []time.Duration{100 * time.Millisecond})

	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	sameNRGBA(t, "default image", got, src)
}

func TestThreeFramesRoundTrip(t *testing.T) {
	t.Parallel()
	const w, h = 300, 200 // noisy frames are large enough to span several IDAT chunks
	frames := []image.Image{
		noisy(w, h, 1),
		solid(w, h, color.NRGBA{R: 10, G: 200, B: 30, A: 255}),
		noisy(w, h, 3),
	}
	delays := []time.Duration{40 * time.Millisecond, 1500 * time.Millisecond, 65535 * time.Millisecond}
	out := encode(t, w, h, apng.Options{Loops: 3}, frames, delays)

	chunks := walkChunks(t, out)

	// Chunk order and sequence numbers.
	var order []string
	var seqs []uint32
	for _, c := range chunks {
		order = append(order, c.typ)
		switch c.typ {
		case "fcTL", "fdAT":
			seqs = append(seqs, binary.BigEndian.Uint32(c.data[:4]))
		}
	}
	if order[0] != "IHDR" || order[1] != "acTL" || order[len(order)-1] != "IEND" {
		t.Fatalf("chunk order = %v", order)
	}
	for i, s := range seqs {
		if s != uint32(i) {
			t.Fatalf("sequence numbers = %v, want 0,1,2,...", seqs)
		}
	}
	if h := chunks[0].data; len(h) != 13 || h[8] != 8 || h[9] != 6 || h[12] != 0 {
		t.Fatalf("IHDR = %x, want 8-bit colour type 6, no interlace", h)
	}
	var fcTLs, fdATs, idats int
	for _, typ := range order {
		switch typ {
		case "fcTL":
			fcTLs++
		case "fdAT":
			fdATs++
		case "IDAT":
			idats++
		}
	}
	if fcTLs != 3 || fdATs != 2 || idats < 1 {
		t.Fatalf("got %d fcTL, %d fdAT, %d IDAT in %v", fcTLs, fdATs, idats, order)
	}
	if order[2] != "fcTL" || order[3] != "IDAT" {
		t.Fatalf("first frame must be fcTL then IDAT: %v", order)
	}

	// Independent decoder.
	a, err := kettek.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("kettek DecodeAll: %v", err)
	}
	if len(a.Frames) != 3 {
		t.Fatalf("frames = %d, want 3", len(a.Frames))
	}
	if a.LoopCount != 3 {
		t.Errorf("LoopCount = %d, want 3", a.LoopCount)
	}
	for i, f := range a.Frames {
		if f.IsDefault {
			t.Errorf("frame %d is a hidden default image", i)
		}
		if f.DelayDenominator != 1000 {
			t.Errorf("frame %d: delay_den = %d, want 1000", i, f.DelayDenominator)
		}
		if want := uint16(delays[i] / time.Millisecond); f.DelayNumerator != want {
			t.Errorf("frame %d: delay_num = %d, want %d", i, f.DelayNumerator, want)
		}
		if f.XOffset != 0 || f.YOffset != 0 || f.BlendOp != kettek.BLEND_OP_SOURCE || f.DisposeOp != kettek.DISPOSE_OP_NONE {
			t.Errorf("frame %d: offsets/ops = %d,%d blend %d dispose %d", i, f.XOffset, f.YOffset, f.BlendOp, f.DisposeOp)
		}
		sameNRGBA(t, "frame "+string(rune('0'+i)), f.Image, frames[i])
	}
}

func TestInfiniteLoopByDefault(t *testing.T) {
	t.Parallel()
	out := encode(t, 2, 2, apng.Options{}, []image.Image{solid(2, 2, color.NRGBA{A: 255})}, []time.Duration{0})
	for _, c := range walkChunks(t, out) {
		if c.typ == "acTL" {
			if n, plays := binary.BigEndian.Uint32(c.data[:4]), binary.BigEndian.Uint32(c.data[4:]); n != 1 || plays != 0 {
				t.Fatalf("acTL = frames %d plays %d, want 1, 0", n, plays)
			}
			return
		}
	}
	t.Fatal("no acTL chunk")
}

func TestAcceptsNonNRGBAAndOffsetImages(t *testing.T) {
	t.Parallel()
	big := image.NewRGBA(image.Rect(0, 0, 20, 20))
	for i := range big.Pix {
		big.Pix[i] = byte(i * 7)
	}
	// Premultiplied-valid pixels only: force alpha to opaque.
	for i := 3; i < len(big.Pix); i += 4 {
		big.Pix[i] = 255
	}
	sub := big.SubImage(image.Rect(5, 6, 15, 14)) // 10x8 at non-zero origin
	other := image.NewGray(image.Rect(0, 0, 10, 8))
	for i := range other.Pix {
		other.Pix[i] = byte(i)
	}
	out := encode(t, 10, 8, apng.Options{Compression: png.BestSpeed},
		[]image.Image{sub, other}, []time.Duration{time.Second, time.Second})
	a, err := kettek.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	sameNRGBA(t, "sub", a.Frames[0].Image, sub)
	sameNRGBA(t, "gray", a.Frames[1].Image, other)
}

func TestDelayRounding(t *testing.T) {
	t.Parallel()
	out := encode(t, 1, 1, apng.Options{}, []image.Image{solid(1, 1, color.NRGBA{A: 255}), solid(1, 1, color.NRGBA{A: 255})},
		[]time.Duration{1499 * time.Microsecond, 1500 * time.Microsecond})
	var nums []uint16
	for _, c := range walkChunks(t, out) {
		if c.typ == "fcTL" {
			nums = append(nums, binary.BigEndian.Uint16(c.data[20:22]))
		}
	}
	if len(nums) != 2 || nums[0] != 1 || nums[1] != 2 {
		t.Fatalf("delay_num = %v, want [1 2]", nums)
	}
}

func TestZeroFramesError(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, 4, 4, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); !errors.Is(err, apng.ErrNoFrames) {
		t.Fatalf("Close = %v, want ErrNoFrames", err)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %d bytes for an empty animation", buf.Len())
	}
}

func TestWrongSizeFrameError(t *testing.T) {
	t.Parallel()
	enc, err := apng.NewEncoder(&bytes.Buffer{}, 4, 4, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.AddFrame(solid(5, 4, color.NRGBA{}), 0); !errors.Is(err, apng.ErrFrameSize) {
		t.Fatalf("AddFrame wrong width = %v, want ErrFrameSize", err)
	}
	if err := enc.AddFrame(solid(4, 3, color.NRGBA{}), 0); !errors.Is(err, apng.ErrFrameSize) {
		t.Fatalf("AddFrame wrong height = %v, want ErrFrameSize", err)
	}
	if err := enc.Close(); !errors.Is(err, apng.ErrNoFrames) {
		t.Fatalf("rejected frames must not be counted: Close = %v", err)
	}
}

func TestDelayOutOfRange(t *testing.T) {
	t.Parallel()
	enc, err := apng.NewEncoder(&bytes.Buffer{}, 2, 2, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	img := solid(2, 2, color.NRGBA{})
	for _, d := range []time.Duration{-time.Millisecond, 65536 * time.Millisecond, 65535*time.Millisecond + time.Millisecond/2 + 1} {
		if err := enc.AddFrame(img, d); !errors.Is(err, apng.ErrDelay) {
			t.Errorf("AddFrame(%v) = %v, want ErrDelay", d, err)
		}
	}
	if err := enc.AddFrame(img, 65535*time.Millisecond); err != nil {
		t.Errorf("AddFrame(65535ms) = %v, want nil", err)
	}
}

func TestCloseIdempotent(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, 2, 2, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.AddFrame(solid(2, 2, color.NRGBA{A: 255}), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	n := buf.Len()
	if err := enc.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if buf.Len() != n {
		t.Fatalf("second Close wrote %d more bytes", buf.Len()-n)
	}
	if err := enc.AddFrame(solid(2, 2, color.NRGBA{}), 0); !errors.Is(err, apng.ErrClosed) {
		t.Fatalf("AddFrame after Close = %v, want ErrClosed", err)
	}
}

func TestNewEncoderValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		w, h, loops int
	}{{0, 1, 0}, {1, 0, 0}, {-1, 1, 0}, {1, 1, -1}} {
		if _, err := apng.NewEncoder(&bytes.Buffer{}, tc.w, tc.h, apng.Options{Loops: tc.loops}); err == nil {
			t.Errorf("NewEncoder(%d, %d, loops %d) succeeded, want error", tc.w, tc.h, tc.loops)
		}
	}
}

type failWriter struct{ after int }

func (f *failWriter) Write(p []byte) (int, error) {
	if f.after <= 0 {
		return 0, errors.New("disk full")
	}
	f.after--
	return len(p), nil
}

func TestCloseReportsWriteError(t *testing.T) {
	t.Parallel()
	enc, err := apng.NewEncoder(&failWriter{}, 2, 2, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.AddFrame(solid(2, 2, color.NRGBA{A: 255}), 0); err != nil {
		t.Fatal(err)
	}
	err1 := enc.Close()
	if err1 == nil {
		t.Fatal("Close succeeded on failing writer")
	}
	if err2 := enc.Close(); err2 != err1 {
		t.Fatalf("second Close = %v, want %v", err2, err1)
	}
}

// composite is a reference APNG compositor over decoded frames. It
// implements the spec for BLEND_OP_SOURCE only.
func composite(t *testing.T, a kettek.APNG, w, h int) []*image.NRGBA {
	t.Helper()
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	var out []*image.NRGBA
	for i, f := range a.Frames {
		if f.BlendOp != kettek.BLEND_OP_SOURCE || f.DisposeOp != kettek.DISPOSE_OP_NONE {
			t.Fatalf("frame %d: unexpected ops blend %d dispose %d", i, f.BlendOp, f.DisposeOp)
		}
		b := f.Image.Bounds()
		r := image.Rect(f.XOffset, f.YOffset, f.XOffset+b.Dx(), f.YOffset+b.Dy())
		if !r.In(canvas.Bounds()) {
			t.Fatalf("frame %d: rect %v outside canvas", i, r)
		}
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				canvas.Set(x, y, f.Image.At(b.Min.X+x-r.Min.X, b.Min.Y+y-r.Min.Y))
			}
		}
		snapshot := image.NewNRGBA(canvas.Bounds())
		copy(snapshot.Pix, canvas.Pix)
		out = append(out, snapshot)
	}
	return out
}

func samePix(t *testing.T, label string, got, want *image.NRGBA) {
	t.Helper()
	if !bytes.Equal(got.Pix, want.Pix) {
		t.Fatalf("%s: pixels differ", label)
	}
}

func fcTLs(t *testing.T, out []byte) []chunk {
	t.Helper()
	var fc []chunk
	for _, c := range walkChunks(t, out) {
		if c.typ == "fcTL" {
			fc = append(fc, c)
		}
	}
	return fc
}

func TestFrameOpValues(t *testing.T) {
	t.Parallel()
	if apng.BlendSource != 0 || apng.BlendOver != 1 {
		t.Errorf("BlendSource/BlendOver = %d/%d, want 0/1", apng.BlendSource, apng.BlendOver)
	}
	if apng.DisposeNone != 0 || apng.DisposeBackground != 1 || apng.DisposePrevious != 2 {
		t.Errorf("Dispose ops = %d/%d/%d, want 0/1/2", apng.DisposeNone, apng.DisposeBackground, apng.DisposePrevious)
	}
}

func TestAddFrameRectWritesOffsetsAndOps(t *testing.T) {
	t.Parallel()
	const w, h = 40, 30
	first := noisy(w, h, 1)
	second := noisy(w, h, 2)
	rect := image.Rect(7, 5, 19, 22)
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, w, h, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.AddFrame(first, 10*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if err := enc.AddFrameRect(second, rect, 20*time.Millisecond, apng.BlendOver, apng.DisposeBackground); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	fc := fcTLs(t, buf.Bytes())
	d := fc[1].data
	got := []uint32{
		binary.BigEndian.Uint32(d[4:8]), binary.BigEndian.Uint32(d[8:12]),
		binary.BigEndian.Uint32(d[12:16]), binary.BigEndian.Uint32(d[16:20]),
	}
	want := []uint32{12, 17, 7, 5}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("fcTL w,h,x,y = %v, want %v", got, want)
		}
	}
	if d[24] != 1 || d[25] != 1 {
		t.Errorf("dispose/blend = %d/%d, want 1/1", d[24], d[25])
	}

	a, err := kettek.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	f := a.Frames[1]
	if f.XOffset != 7 || f.YOffset != 5 || f.Image.Bounds().Dx() != 12 || f.Image.Bounds().Dy() != 17 {
		t.Fatalf("decoded frame 1 at (%d,%d) size %v", f.XOffset, f.YOffset, f.Image.Bounds())
	}
	// The sub-rectangle holds second's pixels under rect.
	for y := 0; y < 17; y++ {
		for x := 0; x < 12; x++ {
			g := color.NRGBAModel.Convert(f.Image.At(f.Image.Bounds().Min.X+x, f.Image.Bounds().Min.Y+y))
			if w := second.NRGBAAt(rect.Min.X+x, rect.Min.Y+y); g != w {
				t.Fatalf("pixel (%d,%d) = %v, want %v", x, y, g, w)
			}
		}
	}
}

func TestAddFrameRectErrors(t *testing.T) {
	t.Parallel()
	const w, h = 10, 8
	img := solid(w, h, color.NRGBA{A: 255})
	full := image.Rect(0, 0, w, h)
	newEnc := func(withFirst bool) *apng.Encoder {
		enc, err := apng.NewEncoder(&bytes.Buffer{}, w, h, apng.Options{})
		if err != nil {
			t.Fatal(err)
		}
		if withFirst {
			if err := enc.AddFrame(img, 0); err != nil {
				t.Fatal(err)
			}
		}
		return enc
	}
	for _, r := range []image.Rectangle{
		image.Rect(5, 5, 11, 8),    // right edge outside
		image.Rect(5, 5, 10, 9),    // bottom edge outside
		image.Rect(-1, 0, 4, 4),    // left edge outside
		image.Rect(0, -1, 4, 4),    // top edge outside
		image.Rect(20, 20, 25, 25), // fully outside
		image.Rect(3, 3, 3, 6),     // empty
		{},
	} {
		if err := newEnc(true).AddFrameRect(img, r, 0, apng.BlendSource, apng.DisposeNone); !errors.Is(err, apng.ErrFrameRect) {
			t.Errorf("rect %v: err = %v, want ErrFrameRect", r, err)
		}
	}
	// First frame must cover the canvas.
	if err := newEnc(false).AddFrameRect(img, image.Rect(0, 0, 5, 5), 0, apng.BlendSource, apng.DisposeNone); !errors.Is(err, apng.ErrFrameRect) {
		t.Errorf("partial first frame: err = %v, want ErrFrameRect", err)
	}
	if err := newEnc(false).AddFrameRect(img, full, 0, apng.BlendSource, apng.DisposeNone); err != nil {
		t.Errorf("full first frame: %v", err)
	}
	if err := newEnc(true).AddFrameRect(img, image.Rect(1, 1, 3, 3), 0, 2, apng.DisposeNone); !errors.Is(err, apng.ErrFrameOp) {
		t.Errorf("bad blend op: err = %v, want ErrFrameOp", err)
	}
	if err := newEnc(true).AddFrameRect(img, image.Rect(1, 1, 3, 3), 0, apng.BlendSource, 3); !errors.Is(err, apng.ErrFrameOp) {
		t.Errorf("bad dispose op: err = %v, want ErrFrameOp", err)
	}
	if err := newEnc(true).AddFrameRect(solid(w+1, h, color.NRGBA{}), image.Rect(1, 1, 3, 3), 0, apng.BlendSource, apng.DisposeNone); !errors.Is(err, apng.ErrFrameSize) {
		t.Errorf("wrong size: err = %v, want ErrFrameSize", err)
	}
	if err := newEnc(true).AddFrameRect(img, image.Rect(1, 1, 3, 3), -1, apng.BlendSource, apng.DisposeNone); !errors.Is(err, apng.ErrDelay) {
		t.Errorf("bad delay: err = %v, want ErrDelay", err)
	}
}

// mutate returns a copy of img with a random small rectangle filled with
// random pixels (random alpha, including fully transparent), and with
// probability 1/4 no change.
func mutate(r *rand.Rand, img *image.NRGBA) *image.NRGBA {
	out := image.NewNRGBA(img.Bounds())
	copy(out.Pix, img.Pix)
	if r.Intn(4) == 0 {
		return out
	}
	b := img.Bounds()
	rw, rh := 1+r.Intn(b.Dx()/3), 1+r.Intn(b.Dy()/3)
	x0, y0 := r.Intn(b.Dx()-rw+1), r.Intn(b.Dy()-rh+1)
	for y := y0; y < y0+rh; y++ {
		for x := x0; x < x0+rw; x++ {
			c := color.NRGBA{R: uint8(r.Intn(256)), G: uint8(r.Intn(256)), B: uint8(r.Intn(256)), A: uint8(r.Intn(256))}
			switch r.Intn(4) {
			case 0:
				c.A = 0
			case 1:
				c.A = 255
			}
			out.SetNRGBA(x, y, c)
		}
	}
	return out
}

func TestAddFrameAutoRecomposesExactly(t *testing.T) {
	t.Parallel()
	for seed := int64(1); seed <= 8; seed++ {
		r := rand.New(rand.NewSource(seed))
		w, h := 20+r.Intn(40), 15+r.Intn(30)
		var buf bytes.Buffer
		enc, err := apng.NewEncoder(&buf, w, h, apng.Options{Compression: png.BestSpeed})
		if err != nil {
			t.Fatal(err)
		}
		cur := noisy(w, h, seed)
		// shown[i] is the canvas expected after each emitted (non-merged) frame.
		var shown []*image.NRGBA
		var delays []time.Duration
		for i := 0; i < 30; i++ {
			if i > 0 {
				cur = mutate(r, cur)
			}
			d := time.Duration(1+r.Intn(200)) * time.Millisecond
			if err := enc.AddFrameAuto(cur, d); err != nil {
				t.Fatalf("seed %d frame %d: %v", seed, i, err)
			}
			if n := len(shown); n > 0 && bytes.Equal(shown[n-1].Pix, cur.Pix) {
				delays[n-1] += d
				continue
			}
			shown = append(shown, cur)
			delays = append(delays, d)
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
		a, err := kettek.DecodeAll(bytes.NewReader(buf.Bytes()))
		if err != nil {
			t.Fatalf("seed %d: %v", seed, err)
		}
		if len(a.Frames) != len(shown) {
			t.Fatalf("seed %d: %d frames, want %d", seed, len(a.Frames), len(shown))
		}
		for i, c := range composite(t, a, w, h) {
			samePix(t, fmt.Sprintf("seed %d frame %d", seed, i), c, shown[i])
			f := a.Frames[i]
			if got := time.Duration(f.DelayNumerator) * time.Millisecond; got != delays[i] {
				t.Fatalf("seed %d frame %d: delay %v, want %v", seed, i, got, delays[i])
			}
		}
	}
}

func TestAddFrameAutoMergesIdenticalFrames(t *testing.T) {
	t.Parallel()
	const w, h = 16, 16
	a, b := noisy(w, h, 1), noisy(w, h, 2)
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, w, h, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, step := range []struct {
		img *image.NRGBA
		d   time.Duration
	}{{a, 100 * time.Millisecond}, {a, 250 * time.Millisecond}, {a, 50 * time.Millisecond}, {b, 70 * time.Millisecond}, {b, 30 * time.Millisecond}} {
		if err := enc.AddFrameAuto(step.img, step.d); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	fc := fcTLs(t, buf.Bytes())
	if len(fc) != 2 {
		t.Fatalf("%d frames, want 2", len(fc))
	}
	for i, want := range []uint16{400, 100} {
		if got := binary.BigEndian.Uint16(fc[i].data[20:22]); got != want {
			t.Errorf("frame %d delay %d ms, want %d", i, got, want)
		}
	}
	for _, c := range walkChunks(t, buf.Bytes()) {
		if c.typ == "acTL" && binary.BigEndian.Uint32(c.data[:4]) != 2 {
			t.Errorf("acTL num_frames = %d, want 2", binary.BigEndian.Uint32(c.data[:4]))
		}
	}
}

func TestAddFrameAutoMergeOverflowAddsFrame(t *testing.T) {
	t.Parallel()
	const w, h = 8, 8
	img := noisy(w, h, 1)
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, w, h, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := enc.AddFrameAuto(img, 40000*time.Millisecond); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	a, err := kettek.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if len(a.Frames) != 3 {
		t.Fatalf("%d frames, want 3 (40 s cannot merge into 40 s)", len(a.Frames))
	}
	for i, c := range composite(t, a, w, h) {
		samePix(t, fmt.Sprintf("overflow frame %d", i), c, img)
	}
}

func TestAddFrameAutoDeltaRectIsMinimal(t *testing.T) {
	t.Parallel()
	const w, h = 64, 48
	base := noisy(w, h, 5)
	// A known two-pixel change.
	next := image.NewNRGBA(base.Bounds())
	copy(next.Pix, base.Pix)
	next.SetNRGBA(10, 20, color.NRGBA{R: 1, G: 2, B: 3, A: 4})
	next.SetNRGBA(30, 25, color.NRGBA{R: 5, G: 6, B: 7, A: 8})
	var buf bytes.Buffer
	enc, _ := apng.NewEncoder(&buf, w, h, apng.Options{})
	for _, img := range []*image.NRGBA{base, next} {
		if err := enc.AddFrameAuto(img, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	fc := fcTLs(t, buf.Bytes())
	d := fc[1].data
	got := []uint32{
		binary.BigEndian.Uint32(d[4:8]), binary.BigEndian.Uint32(d[8:12]),
		binary.BigEndian.Uint32(d[12:16]), binary.BigEndian.Uint32(d[16:20]),
	}
	want := []uint32{21, 6, 10, 20}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("delta fcTL w,h,x,y = %v, want %v", got, want)
		}
	}
	if d[24] != 0 || d[25] != 0 {
		t.Errorf("dispose/blend = %d/%d, want 0/0", d[24], d[25])
	}
}

func TestAddFrameAutoFullChangeUsesFullRect(t *testing.T) {
	t.Parallel()
	const w, h = 24, 18
	var buf bytes.Buffer
	enc, _ := apng.NewEncoder(&buf, w, h, apng.Options{})
	imgs := []*image.NRGBA{noisy(w, h, 1), noisy(w, h, 2)}
	for _, img := range imgs {
		if err := enc.AddFrameAuto(img, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	for i, c := range fcTLs(t, buf.Bytes()) {
		d := c.data
		if binary.BigEndian.Uint32(d[4:8]) != w || binary.BigEndian.Uint32(d[8:12]) != h ||
			binary.BigEndian.Uint32(d[12:16]) != 0 || binary.BigEndian.Uint32(d[16:20]) != 0 {
			t.Errorf("frame %d fcTL = %x, want full canvas at (0,0)", i, d[4:20])
		}
	}
}

func TestAddFrameAutoSmallerThanFullFrames(t *testing.T) {
	t.Parallel()
	const w, h, n = 200, 120, 20
	r := rand.New(rand.NewSource(3))
	frames := []*image.NRGBA{noisy(w, h, 9)}
	for i := 1; i < n; i++ {
		// Change a single 6x6 block per frame.
		next := image.NewNRGBA(frames[i-1].Bounds())
		copy(next.Pix, frames[i-1].Pix)
		x0, y0 := r.Intn(w-6), r.Intn(h-6)
		for y := y0; y < y0+6; y++ {
			for x := x0; x < x0+6; x++ {
				next.SetNRGBA(x, y, color.NRGBA{R: uint8(r.Intn(256)), G: uint8(r.Intn(256)), B: uint8(r.Intn(256)), A: 255})
			}
		}
		frames = append(frames, next)
	}
	size := func(add func(*apng.Encoder, *image.NRGBA) error) int {
		var buf bytes.Buffer
		enc, _ := apng.NewEncoder(&buf, w, h, apng.Options{Compression: png.BestSpeed})
		for _, f := range frames {
			if err := add(enc, f); err != nil {
				t.Fatal(err)
			}
		}
		if err := enc.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Len()
	}
	full := size(func(e *apng.Encoder, f *image.NRGBA) error { return e.AddFrame(f, time.Second) })
	delta := size(func(e *apng.Encoder, f *image.NRGBA) error { return e.AddFrameAuto(f, time.Second) })
	t.Logf("full %d bytes, delta %d bytes", full, delta)
	// Full frames cost ~n noisy canvases; delta costs one canvas plus tiny blocks.
	if delta*5 > full {
		t.Fatalf("delta output %d bytes is not at least 5x smaller than full frames %d", delta, full)
	}
}

func TestAddFrameAutoAfterDisposeOps(t *testing.T) {
	t.Parallel()
	const w, h = 12, 10
	red := solid(w, h, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	enc, _ := apng.NewEncoder(&buf, w, h, apng.Options{})
	if err := enc.AddFrame(red, time.Second); err != nil {
		t.Fatal(err)
	}
	// Frame 1 paints blue on a rect and is disposed back to the red canvas.
	blue := solid(w, h, color.NRGBA{B: 255, A: 255})
	if err := enc.AddFrameRect(blue, image.Rect(2, 2, 6, 6), time.Second, apng.BlendSource, apng.DisposePrevious); err != nil {
		t.Fatal(err)
	}
	// Auto frame equal to the all-red canvas: differs from what frame 1
	// showed, so it needs a real frame; the canvas it starts from is red.
	if err := enc.AddFrameAuto(red, time.Second); err != nil {
		t.Fatal(err)
	}
	// Frame 3 changes one pixel; frame 4 repeats it and merges.
	green := image.NewNRGBA(red.Bounds())
	copy(green.Pix, red.Pix)
	green.SetNRGBA(9, 9, color.NRGBA{G: 255, A: 255})
	if err := enc.AddFrameAuto(green, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := enc.AddFrameAuto(green, time.Second); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	fc := fcTLs(t, buf.Bytes())
	if len(fc) != 4 {
		t.Fatalf("%d frames, want 4", len(fc))
	}
	if got := binary.BigEndian.Uint16(fc[3].data[20:22]); got != 2000 {
		t.Errorf("merged delay %d ms, want 2000", got)
	}
	// Frame 2 is the all-red canvas, sent as a 1x1 delta.
	if w2, h2 := binary.BigEndian.Uint32(fc[2].data[4:8]), binary.BigEndian.Uint32(fc[2].data[8:12]); w2 != 1 || h2 != 1 {
		t.Errorf("frame 2 is %dx%d, want 1x1", w2, h2)
	}
	a, err := kettek.DecodeAll(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	// Reference compositor honouring dispose ops: kettek's frames are
	// patches, so replay them by hand.
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	want := []*image.NRGBA{red, blue, red, green}
	for i, f := range a.Frames {
		b := f.Image.Bounds()
		r := image.Rect(f.XOffset, f.YOffset, f.XOffset+b.Dx(), f.YOffset+b.Dy())
		before := image.NewNRGBA(canvas.Bounds())
		copy(before.Pix, canvas.Pix)
		for y := r.Min.Y; y < r.Max.Y; y++ {
			for x := r.Min.X; x < r.Max.X; x++ {
				canvas.Set(x, y, f.Image.At(b.Min.X+x-r.Min.X, b.Min.Y+y-r.Min.Y))
			}
		}
		if i == 1 {
			// Frame 1 shows blue in its rect over red.
			if canvas.NRGBAAt(3, 3) != (color.NRGBA{B: 255, A: 255}) || canvas.NRGBAAt(0, 0) != (color.NRGBA{R: 255, A: 255}) {
				t.Fatal("frame 1 composite wrong")
			}
		} else {
			samePix(t, fmt.Sprintf("frame %d", i), canvas, want[i])
		}
		if f.DisposeOp == kettek.DISPOSE_OP_PREVIOUS {
			canvas = before
		}
	}
}

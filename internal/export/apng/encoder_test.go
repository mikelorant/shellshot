package apng_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"math/rand"
	"strings"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/michaellorant/shellshot/internal/export/apng"
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
	src := noisy(17, 9, 1)
	out := encode(t, 17, 9, apng.Options{}, []image.Image{src}, []time.Duration{100 * time.Millisecond})

	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	sameNRGBA(t, "default image", got, src)
}

func TestThreeFramesRoundTrip(t *testing.T) {
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

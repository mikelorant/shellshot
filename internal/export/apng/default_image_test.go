package apng_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"
	"time"

	kettek "github.com/kettek/apng"

	"github.com/mikelorant/shellshot/internal/export/apng"
)

// encodeWithDefault writes frames through AddFrameAuto with img as the default
// image, which is set after the frames when afterFrames is true.
func encodeWithDefault(t *testing.T, w, h int, img image.Image, frames []image.Image, afterFrames bool) []byte {
	t.Helper()
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, w, h, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	set := func() {
		if err := enc.SetDefaultImage(img); err != nil {
			t.Fatalf("SetDefaultImage: %v", err)
		}
	}
	if !afterFrames {
		set()
	}
	for i, f := range frames {
		if err := enc.AddFrameAuto(f, time.Duration(i+1)*100*time.Millisecond); err != nil {
			t.Fatalf("AddFrameAuto %d: %v", i, err)
		}
	}
	if afterFrames {
		set()
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// A walk of the chunk sequence as the APNG specification fixes it for a file
// whose default image is not part of the animation.
func TestDefaultImageChunkSequence(t *testing.T) {
	t.Parallel()
	poster := noisy(20, 10, 99)
	var frames []image.Image
	for i := range 4 {
		frames = append(frames, noisy(20, 10, int64(i)))
	}
	for _, afterFrames := range []bool{false, true} {
		out := encodeWithDefault(t, 20, 10, poster, frames, afterFrames)
		chunks := walkChunks(t, out)

		var types []string
		for _, c := range chunks {
			types = append(types, c.typ)
		}
		want := []string{"IHDR", "acTL", "IDAT", "fcTL", "fdAT", "fcTL", "fdAT", "fcTL", "fdAT", "fcTL", "fdAT", "IEND"}
		if len(types) != len(want) {
			t.Fatalf("chunks = %v, want %v", types, want)
		}
		for i := range want {
			if types[i] != want[i] {
				t.Fatalf("chunks = %v, want %v", types, want)
			}
		}
		if n := binary.BigEndian.Uint32(chunks[1].data[:4]); n != 4 {
			t.Errorf("acTL num_frames = %d, want 4 (the default image is not a frame)", n)
		}
		// fcTL and fdAT share one sequence that starts at 0 with the first
		// animation frame's fcTL and has no gaps.
		var seq uint32
		for _, c := range chunks[3:] {
			if c.typ != "fcTL" && c.typ != "fdAT" {
				continue
			}
			if got := binary.BigEndian.Uint32(c.data[:4]); got != seq {
				t.Fatalf("%s sequence number = %d, want %d", c.typ, got, seq)
			}
			seq++
		}
		if seq != 8 {
			t.Errorf("%d sequenced chunks, want 8", seq)
		}
	}
}

func TestDefaultImageDecodesAsPNGAndAnimates(t *testing.T) {
	t.Parallel()
	poster := solid(20, 10, color.NRGBA{R: 200, A: 255})
	f0, f1 := noisy(20, 10, 1), noisy(20, 10, 2)
	out := encodeWithDefault(t, 20, 10, poster, []image.Image{f0, f1}, false)

	got, err := png.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	sameNRGBA(t, "png.Decode", got, poster)

	a, err := kettek.DecodeAll(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("kettek DecodeAll: %v", err)
	}
	if len(a.Frames) != 3 || !a.Frames[0].IsDefault || a.Frames[1].IsDefault || a.Frames[2].IsDefault {
		t.Fatalf("kettek frames = %d (default flags %v %v %v), want the default image and 2 animation frames",
			len(a.Frames), a.Frames[0].IsDefault, a.Frames[1].IsDefault, a.Frames[2].IsDefault)
	}
	sameNRGBA(t, "kettek default image", a.Frames[0].Image, poster)
	sameNRGBA(t, "animation frame 0", a.Frames[1].Image, f0)
	sameNRGBA(t, "animation frame 1", a.Frames[2].Image, f1)
	if d := a.Frames[1].DelayNumerator; d != 100 {
		t.Errorf("first animation frame delay = %d/%d, want 100 ms", d, a.Frames[1].DelayDenominator)
	}
}

// Without a default image the file is as before: the first frame is the IDAT
// image, with its fcTL in front.
func TestNoDefaultImageKeepsFirstFrameAsIDAT(t *testing.T) {
	t.Parallel()
	out := encode(t, 8, 8, apng.Options{}, []image.Image{noisy(8, 8, 1), noisy(8, 8, 2)}, []time.Duration{time.Second, time.Second})
	var types []string
	for _, c := range walkChunks(t, out) {
		types = append(types, c.typ)
	}
	want := []string{"IHDR", "acTL", "fcTL", "IDAT", "fcTL", "fdAT", "IEND"}
	if len(types) != len(want) {
		t.Fatalf("chunks = %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Fatalf("chunks = %v, want %v", types, want)
		}
	}
}

func TestSetDefaultImageErrors(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, 8, 8, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if err := enc.SetDefaultImage(noisy(7, 8, 1)); !errors.Is(err, apng.ErrFrameSize) {
		t.Errorf("wrong size: err = %v, want ErrFrameSize", err)
	}
	if err := enc.SetDefaultImage(noisy(8, 8, 1)); err != nil {
		t.Fatal(err)
	}
	// A default image alone is not an animation.
	if err := enc.Close(); !errors.Is(err, apng.ErrNoFrames) {
		t.Errorf("Close without frames: err = %v, want ErrNoFrames", err)
	}
	if err := enc.SetDefaultImage(noisy(8, 8, 1)); !errors.Is(err, apng.ErrClosed) {
		t.Errorf("after Close: err = %v, want ErrClosed", err)
	}
}

func TestSetDefaultImageTwiceReplaces(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, 8, 8, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	second := noisy(8, 8, 2)
	for _, img := range []image.Image{noisy(8, 8, 1), second} {
		if err := enc.SetDefaultImage(img); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.AddFrameAuto(noisy(8, 8, 3), time.Second); err != nil {
		t.Fatal(err)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	sameNRGBA(t, "default image", got, second)
}

func TestFramesCountsTheFramesWritten(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	enc, err := apng.NewEncoder(&buf, 8, 8, apng.Options{})
	if err != nil {
		t.Fatal(err)
	}
	same, other := noisy(8, 8, 1), noisy(8, 8, 2)
	for _, img := range []image.Image{same, same, other, other, same} {
		if err := enc.AddFrameAuto(img, time.Second); err != nil {
			t.Fatal(err)
		}
	}
	if err := enc.SetDefaultImage(other); err != nil {
		t.Fatal(err)
	}
	if n := enc.Frames(); n != 0 {
		t.Errorf("Frames before Close = %d, want 0", n)
	}
	if err := enc.Close(); err != nil {
		t.Fatal(err)
	}
	// Five images, three pictures; the default image is not a frame.
	if n := enc.Frames(); n != 3 {
		t.Errorf("Frames = %d, want 3", n)
	}
	if n := binary.BigEndian.Uint32(walkChunks(t, buf.Bytes())[1].data[:4]); n != 3 {
		t.Errorf("acTL num_frames = %d, want 3", n)
	}
}

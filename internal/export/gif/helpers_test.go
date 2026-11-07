package gif_test

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	stdgif "image/gif"
	"math/rand"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/export/gif"
)

// encodeFrames encodes frames, each shown for the matching delay, through a
// Sink and returns the GIF bytes.
func encodeFrames(t testing.TB, o gif.Options, frames []*image.RGBA, delays []time.Duration) []byte {
	t.Helper()
	b := frames[0].Bounds()
	var buf bytes.Buffer
	s, err := gif.NewSink(&buf, b.Dx(), b.Dy(), o)
	if err != nil {
		t.Fatal(err)
	}
	for i, f := range frames {
		if err := s.AddFrame(f, delays[i]); err != nil {
			t.Fatalf("AddFrame %d: %v", i, err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// evenDelays returns n delays of d.
func evenDelays(n int, d time.Duration) []time.Duration {
	out := make([]time.Duration, n)
	for i := range out {
		out[i] = d
	}
	return out
}

// referenceCompositor plays a decoded GIF the way a viewer does, following
// each frame's disposal method, and returns the canvas as each frame shows
// it. A frame is one decoded step, so merged frames are not repeated.
func referenceCompositor(g *stdgif.GIF) []*image.RGBA {
	canvasRect := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	canvas := image.NewRGBA(canvasRect)
	var shown []*image.RGBA
	for i, f := range g.Image {
		var before *image.RGBA
		if g.Disposal[i] == stdgif.DisposalPrevious {
			before = image.NewRGBA(canvasRect)
			copy(before.Pix, canvas.Pix)
		}
		draw.Draw(canvas, f.Bounds(), f, f.Bounds().Min, draw.Over)
		snap := image.NewRGBA(canvasRect)
		copy(snap.Pix, canvas.Pix)
		shown = append(shown, snap)
		switch g.Disposal[i] {
		case stdgif.DisposalBackground:
			draw.Draw(canvas, f.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case stdgif.DisposalPrevious:
			copy(canvas.Pix, before.Pix)
		}
	}
	return shown
}

func decodeGIF(t testing.TB, data []byte) *stdgif.GIF {
	t.Helper()
	g, err := stdgif.DecodeAll(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	return g
}

// firstDifference returns the first pixel where got and want differ in colour,
// or false.
func firstDifference(got, want *image.RGBA) (image.Point, bool) {
	b := want.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			g, w := got.RGBAAt(x, y), want.RGBAAt(x, y)
			if g.R != w.R || g.G != w.G || g.B != w.B || g.A != 0xff {
				return image.Pt(x, y), true
			}
		}
	}
	return image.Point{}, false
}

// colourPool returns n distinct opaque colours.
func colourPool(rng *rand.Rand, n int) []color.RGBA {
	seen := map[color.RGBA]bool{}
	var pool []color.RGBA
	for len(pool) < n {
		c := color.RGBA{uint8(rng.Intn(256)), uint8(rng.Intn(256)), uint8(rng.Intn(256)), 255}
		if !seen[c] {
			seen[c] = true
			pool = append(pool, c)
		}
	}
	return pool
}

// evolve returns n frames of w x h pixels: the first filled with pool[0],
// each next one the previous with up to three random rectangles painted from
// the pool.
func evolve(rng *rand.Rand, w, h, n int, pool []color.RGBA) []*image.RGBA {
	var frames []*image.RGBA
	cur := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(cur, cur.Rect, image.NewUniform(pool[0]), image.Point{}, draw.Src)
	for range n {
		next := image.NewRGBA(cur.Rect)
		copy(next.Pix, cur.Pix)
		for range rng.Intn(4) {
			x0, y0 := rng.Intn(w), rng.Intn(h)
			r := image.Rect(x0, y0, x0+1+rng.Intn(w-x0), y0+1+rng.Intn(h-y0))
			draw.Draw(next, r, image.NewUniform(pool[rng.Intn(len(pool))]), image.Point{}, draw.Src)
		}
		frames = append(frames, next)
		cur = next
	}
	return frames
}

// Package gif writes animated GIF files using image/gif for the container and
// LZW, and its own palette and delta-frame logic.
//
// A Sink buffers the animation and writes the file in Close, because the one
// global palette depends on every frame:
//
//   - AddFrame keeps only the pixels that changed since the previous frame
//     (their bounding rectangle), so memory is the size of the changes, not of
//     every frame, and counts the colours of the changed pixels.
//   - If the animation has at most Options.Colors distinct colours they are
//     the global palette, verbatim: the GIF is lossless and Options.Dither is
//     ignored. Otherwise the palette comes from a popularity-weighted median
//     cut of all the colours, and each pixel takes the nearest palette colour
//     (or the error-diffused one with Options.Dither).
//   - Palette index 0 is transparent. Every frame after the first covers only
//     the rectangle of pixels whose palette index changed, sets the others to
//     index 0 and uses disposal 1 (do not dispose), so the canvas shows the
//     previous frame through them. A frame that changes nothing is merged into
//     the previous frame's delay.
//   - Delays are whole centiseconds, at least 2 (browsers play shorter ones at
//     100 ms), rounded to nearest with the rounding error carried to the next
//     frame so the total duration is kept within 1 cs.
package gif

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	stdgif "image/gif"
	"io"
	"time"
)

var (
	// ErrNoFrames is returned by Close when no frame was added.
	ErrNoFrames = errors.New("gif: no frames")
	// ErrFrameSize is wrapped by AddFrame errors when the image size differs
	// from the canvas size.
	ErrFrameSize = errors.New("gif: frame size does not match canvas")
	// ErrDelay is wrapped by AddFrame errors for a negative delay.
	ErrDelay = errors.New("gif: negative frame delay")
	// ErrClosed is returned by AddFrame after Close.
	ErrClosed = errors.New("gif: sink closed")
)

const (
	// MaxColors is the largest Options.Colors: 255 colours plus the
	// transparent entry fill the 256 entries of a GIF palette.
	MaxColors = 255
	// maxDimension is the largest canvas side: a GIF stores it in 16 bits.
	maxDimension = 65535
	// maxLoops is the largest play count; the NETSCAPE loop extension stores
	// the repeats in 16 bits.
	maxLoops = 65536

	centisecond    = 10 * time.Millisecond
	minCentisecond = 2
	maxCentisecond = 65535
	maxDelay       = maxCentisecond * centisecond
)

// Options configures a Sink.
type Options struct {
	// Colors is the largest number of picture colours in the palette, 2 to
	// 255; the transparent entry is one more. 0 means 255.
	Colors int
	// Dither diffuses the colour error of a Floyd-Steinberg dither over the
	// changed pixels when the animation has more than Colors colours. It is
	// ignored for an animation that fits the palette. A dither makes the
	// changed rectangles bigger and the file larger.
	Dither bool
	// Loops is the number of times the animation plays; 0 means forever, 1
	// plays once. It is the same as the APNG option. GIF stores repeats, so
	// the file's loop count is Loops-1 (no loop extension for 1).
	Loops int
}

// patch is the part of a frame that differs from the previous frame.
type patch struct {
	// rect is the bounding rectangle of the changed pixels, in canvas
	// coordinates; empty if nothing changed (the patch only carries delay).
	rect image.Rectangle
	// pix holds the pixels of rect, row by row, as 0xRRGGBB.
	pix   []uint32
	delay time.Duration
}

// Sink writes an animated GIF to an io.Writer. It buffers the animation and
// writes the file in Close. It is not safe for concurrent use.
type Sink struct {
	w             io.Writer
	width, height int
	colors        int
	dither        bool
	loops         int

	patches []patch
	// last is the RGBA canvas as the added frames leave it, to diff the next
	// frame against; it is nil before the first frame.
	last *image.RGBA
	// hist counts the pixels of every patch by colour.
	hist map[uint32]int

	// written is the number of frames Close wrote.
	written  int
	closed   bool
	closeErr error
}

// NewSink returns a Sink for a width x height canvas that writes to w.
func NewSink(w io.Writer, width, height int, o Options) (*Sink, error) {
	if width <= 0 || height <= 0 || width > maxDimension || height > maxDimension {
		return nil, fmt.Errorf("gif: invalid canvas size %dx%d (each side must be 1 to %d)", width, height, maxDimension)
	}
	colors := o.Colors
	if colors == 0 {
		colors = MaxColors
	}
	if colors < 2 || colors > MaxColors {
		return nil, fmt.Errorf("gif: invalid colour count %d (want 2 to %d)", o.Colors, MaxColors)
	}
	if o.Loops < 0 || o.Loops > maxLoops {
		return nil, fmt.Errorf("gif: invalid loop count %d (want 0 to %d)", o.Loops, maxLoops)
	}
	return &Sink{
		w: w, width: width, height: height,
		colors: colors, dither: o.Dither, loops: o.Loops,
		hist: make(map[uint32]int),
	}, nil
}

// MaxDelay is the longest delay one GIF frame can carry, 65535 cs. The
// pipeline splits a longer hold into several frames.
func (s *Sink) MaxDelay() time.Duration { return maxDelay }

// AddFrame appends img, shown for delay. img must be exactly width x height
// pixels (its origin may be non-zero) and opaque: the alpha channel is
// ignored. A frame equal to the previous one adds delay to it.
func (s *Sink) AddFrame(img *image.RGBA, delay time.Duration) error {
	if s.closed {
		return ErrClosed
	}
	if delay < 0 {
		return fmt.Errorf("%w: %v", ErrDelay, delay)
	}
	if size := img.Bounds().Size(); size.X != s.width || size.Y != s.height {
		return fmt.Errorf("%w: got %dx%d, want %dx%d", ErrFrameSize, size.X, size.Y, s.width, s.height)
	}
	if s.last == nil {
		s.last = image.NewRGBA(image.Rect(0, 0, s.width, s.height))
		s.addPatch(img, s.last.Rect, delay)
		copyRGBA(s.last, img, s.last.Rect)
		return nil
	}
	rect := changedRect(img, s.last)
	if rect.Empty() {
		s.holdPatch(delay)
		return nil
	}
	s.addPatch(img, rect, delay)
	copyRGBA(s.last, img, rect)
	return nil
}

// addPatch appends the pixels of rect in img as a patch and counts their
// colours.
func (s *Sink) addPatch(img *image.RGBA, rect image.Rectangle, delay time.Duration) {
	pix := make([]uint32, 0, rect.Dx()*rect.Dy())
	origin := img.Rect.Min
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		row := img.Pix[img.PixOffset(origin.X+rect.Min.X, origin.Y+y):][:4*rect.Dx()]
		for i := 0; i < len(row); i += 4 {
			c := uint32(row[i])<<16 | uint32(row[i+1])<<8 | uint32(row[i+2])
			pix = append(pix, c)
			s.hist[c]++
		}
	}
	s.patches = append(s.patches, patch{rect: rect, pix: pix, delay: delay})
}

// Frames returns the number of frames Close wrote: patches that changed no
// palette entry are merged into the frame before. It is 0 before Close and
// after a Close that failed.
func (s *Sink) Frames() int { return s.written }

// Close builds the palette, encodes the frames and writes the GIF. It may be
// called again; it then returns the first result.
func (s *Sink) Close() error {
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.closeErr = s.write()
	s.patches, s.last, s.hist = nil, nil, nil
	return s.closeErr
}

func (s *Sink) write() error {
	if len(s.patches) == 0 {
		return ErrNoFrames
	}
	pal := buildPalette(s.hist, s.colors)
	frames, delays := s.encodeFrames(pal)
	g := &stdgif.GIF{
		Image:     make([]*image.Paletted, len(frames)),
		Delay:     centiseconds(delays),
		Disposal:  make([]byte, len(frames)),
		LoopCount: s.loopCount(),
		Config:    image.Config{ColorModel: pal.colors, Width: s.width, Height: s.height},
	}
	for i, f := range frames {
		g.Image[i] = f
		g.Disposal[i] = stdgif.DisposalNone
	}
	if err := stdgif.EncodeAll(s.w, g); err != nil {
		return err
	}
	s.written = len(frames)
	return nil
}

// loopCount maps the play count to image/gif's LoopCount.
func (s *Sink) loopCount() int {
	if s.loops == 0 {
		return 0
	}
	if s.loops == 1 {
		return -1
	}
	return s.loops - 1
}

// encodeFrames turns the patches into paletted frames and their delays: the
// first covers the canvas, the others only the pixels whose index changed.
func (s *Sink) encodeFrames(pal palette) ([]*image.Paletted, []time.Duration) {
	var (
		frames []*image.Paletted
		delays []time.Duration
		canvas = make([]uint8, s.width*s.height) // the palette indices shown
		m      = newMapper(pal)
	)
	// hold gives delay to the last frame, or to a new one-pixel transparent
	// frame if the last would pass the longest delay.
	hold := func(delay time.Duration) {
		if n := len(delays); delays[n-1]+delay <= maxDelay {
			delays[n-1] += delay
			return
		}
		frames = append(frames, image.NewPaletted(image.Rect(0, 0, 1, 1), pal.colors))
		delays = append(delays, delay)
	}
	for i, p := range s.patches {
		if p.rect.Empty() {
			hold(p.delay)
			continue
		}
		idx := m.index(p.pix, p.rect.Dx(), s.dither)
		if i == 0 {
			f := image.NewPaletted(p.rect, pal.colors)
			copy(f.Pix, idx)
			copy(canvas, idx)
			frames, delays = append(frames, f), append(delays, p.delay)
			continue
		}
		changed := changedIndexRect(canvas, s.width, idx, p.rect)
		if changed.Empty() {
			hold(p.delay)
			continue
		}
		f := image.NewPaletted(changed, pal.colors)
		w := p.rect.Dx()
		for y := changed.Min.Y; y < changed.Max.Y; y++ {
			src := idx[(y-p.rect.Min.Y)*w+changed.Min.X-p.rect.Min.X:][:changed.Dx()]
			dst := canvas[y*s.width+changed.Min.X:][:changed.Dx()]
			out := f.Pix[f.PixOffset(changed.Min.X, y):][:changed.Dx()]
			for x, v := range src {
				if v != dst[x] {
					out[x], dst[x] = v, v // else the transparent index 0
				}
			}
		}
		frames, delays = append(frames, f), append(delays, p.delay)
	}
	return frames, delays
}

// holdPatch adds delay to the last patch, or appends an empty patch that
// carries it if that would pass the longest delay.
func (s *Sink) holdPatch(delay time.Duration) {
	if last := &s.patches[len(s.patches)-1]; last.delay+delay <= maxDelay {
		last.delay += delay
		return
	}
	s.patches = append(s.patches, patch{delay: delay})
}

// centiseconds converts frame delays to GIF delays: whole centiseconds,
// rounded to nearest with the rounding error carried into the next frame, and
// limited to 2..65535 (the limit is applied after the carry, so it does not
// change it).
func centiseconds(delays []time.Duration) []int {
	out := make([]int, len(delays))
	var carry time.Duration
	for i, d := range delays {
		want := d + carry
		cs := int((want + centisecond/2) / centisecond) // want >= -5 ms: carry is at most 5 ms either way
		carry = want - time.Duration(cs)*centisecond
		out[i] = max(minCentisecond, min(maxCentisecond, cs))
	}
	return out
}

// changedRect returns the bounding rectangle of the pixels that differ
// between img and last, whose origin is (0,0).
func changedRect(img, last *image.RGBA) image.Rectangle {
	w, h := last.Rect.Dx(), last.Rect.Dy()
	origin := img.Rect.Min
	rect := image.Rectangle{Min: image.Pt(w, h)}
	found := false
	for y := range h {
		a := img.Pix[img.PixOffset(origin.X, origin.Y+y):][:4*w]
		b := last.Pix[last.PixOffset(0, y):][:4*w]
		if bytes.Equal(a, b) {
			continue
		}
		x0 := 0
		for x0 < w && pixelEqual(a, b, x0) {
			x0++
		}
		x1 := w
		for x1 > x0 && pixelEqual(a, b, x1-1) {
			x1--
		}
		if !found {
			rect.Min.Y, found = y, true
		}
		rect.Min.X, rect.Max.X = min(rect.Min.X, x0), max(rect.Max.X, x1)
		rect.Max.Y = y + 1
	}
	if !found {
		return image.Rectangle{}
	}
	return rect
}

// pixelEqual reports whether pixel x of the rows a and b has the same colour;
// alpha is ignored.
func pixelEqual(a, b []byte, x int) bool {
	return a[4*x] == b[4*x] && a[4*x+1] == b[4*x+1] && a[4*x+2] == b[4*x+2]
}

// changedIndexRect returns the bounding rectangle of the pixels in rect where
// idx (the indices of rect, row by row) differs from canvas (the indices of
// the whole canvas, width wide).
func changedIndexRect(canvas []uint8, width int, idx []uint8, rect image.Rectangle) image.Rectangle {
	w := rect.Dx()
	out := image.Rectangle{Min: rect.Max, Max: rect.Min}
	found := false
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		a := idx[(y-rect.Min.Y)*w:][:w]
		b := canvas[y*width+rect.Min.X:][:w]
		if bytes.Equal(a, b) {
			continue
		}
		x0 := 0
		for a[x0] == b[x0] {
			x0++
		}
		x1 := w
		for a[x1-1] == b[x1-1] {
			x1--
		}
		if !found {
			out.Min.Y, found = y, true
		}
		out.Min.X, out.Max.X = min(out.Min.X, rect.Min.X+x0), max(out.Max.X, rect.Min.X+x1)
		out.Max.Y = y + 1
	}
	if !found {
		return image.Rectangle{}
	}
	return out
}

// copyRGBA copies the pixels of rect from src (rect relative to src.Rect.Min)
// to dst, whose origin is (0,0).
func copyRGBA(dst, src *image.RGBA, rect image.Rectangle) {
	origin := src.Rect.Min
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		copy(dst.Pix[dst.PixOffset(rect.Min.X, y):][:4*rect.Dx()], src.Pix[src.PixOffset(origin.X+rect.Min.X, origin.Y+y):])
	}
}

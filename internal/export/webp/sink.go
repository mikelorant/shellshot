package webp

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"time"

	"github.com/mikelorant/shellshot/internal/export/pipeline"
)

var (
	// ErrDelay is wrapped by AddFrame errors for a negative delay or one
	// longer than MaxDelay.
	ErrDelay = errors.New("webp: frame delay out of range")
	// ErrFrameSize is wrapped by AddFrame errors when the image size differs
	// from the canvas size.
	ErrFrameSize = errors.New("webp: frame size does not match canvas")
	// ErrNoFrames is returned by Close when no frame was added.
	ErrNoFrames = errors.New("webp: no frames")
	// ErrClosed is returned by AddFrame after Close.
	ErrClosed = errors.New("webp: sink closed")
	// ErrTooLarge is returned by Close when the file would exceed the 4 GiB
	// RIFF limit.
	ErrTooLarge = errors.New("webp: file larger than 4 GiB")
)

const (
	// maxDelay is the longest delay of one frame, the largest 24-bit
	// millisecond count.
	maxDelay = (1<<24 - 1) * time.Millisecond
	// minDelay is the shortest delay written. Players disagree about delays
	// below about 10 ms, so shorter ones are raised to it.
	minDelay = 10 * time.Millisecond
	// maxLoops is the largest loop count of the ANIM chunk (16 bits).
	maxLoops = math.MaxUint16
)

// Options configures a Sink.
type Options struct {
	// Loops is the number of times the animation plays; 0 means forever.
	// It must be at most 65535.
	Loops int
}

type frame struct {
	rect        image.Rectangle
	delayMillis uint32
	vp8l        []byte
}

// Sink writes an animated lossless WebP to an io.Writer. It buffers all
// frames and writes the file in Close; see the package documentation. It
// implements pipeline.Sink and pipeline.DelayLimiter. It is not safe for
// concurrent use.
type Sink struct {
	w      io.Writer
	width  int
	height int
	loops  uint16

	frames []frame
	alpha  bool // some frame has a pixel that is not opaque

	// shown is the canvas as the frames so far leave it; cur is the scratch
	// image the next frame is converted into. They swap after a frame
	// that changed the canvas.
	shown, cur *image.NRGBA

	// written is the number of frames Close wrote.
	written  int
	closed   bool
	closeErr error
}

var (
	_ pipeline.Sink         = (*Sink)(nil)
	_ pipeline.DelayLimiter = (*Sink)(nil)
	_ pipeline.FrameCounter = (*Sink)(nil)
)

// NewSink returns a Sink for a width×height canvas that writes to w. The
// canvas must be at most 16384 pixels in each direction.
func NewSink(w io.Writer, width, height int, o Options) (*Sink, error) {
	if width <= 0 || height <= 0 || width > maxFrameDimension || height > maxFrameDimension {
		return nil, fmt.Errorf("webp: invalid canvas size %dx%d (each side must be 1 to %d)", width, height, maxFrameDimension)
	}
	if o.Loops < 0 || o.Loops > maxLoops {
		return nil, fmt.Errorf("webp: invalid loop count %d (must be 0 to %d)", o.Loops, maxLoops)
	}
	canvas := image.Rect(0, 0, width, height)
	return &Sink{
		w: w, width: width, height: height, loops: uint16(o.Loops),
		shown: image.NewNRGBA(canvas), cur: image.NewNRGBA(canvas),
	}, nil
}

// SinkFactory returns the pipeline.SinkFactory of a WebP that plays loops
// times (0 for forever).
func SinkFactory(loops int) pipeline.SinkFactory {
	return func(w io.Writer, width, height int) (pipeline.Sink, error) {
		s, err := NewSink(w, width, height, Options{Loops: loops})
		if err != nil {
			return nil, err
		}
		return s, nil
	}
}

// MaxDelay is the longest delay one frame can carry, 16777215 ms.
func (s *Sink) MaxDelay() time.Duration { return maxDelay }

// AddFrame appends img, a width×height image, as the next state of the
// animation, shown for delay. The delay is rounded to the nearest
// millisecond and raised to 10 ms if shorter; it must be within 0 to
// MaxDelay, otherwise the error wraps ErrDelay. The first frame is the whole
// canvas; a later one is the bounding rectangle of the changed pixels,
// widened to start at even coordinates. If nothing changed, delay is added to
// the previous frame instead (unless that would exceed MaxDelay, in which
// case a 1×1 frame is added). img is not retained.
func (s *Sink) AddFrame(img *image.RGBA, delay time.Duration) error {
	if s.closed {
		return ErrClosed
	}
	if delay < 0 || delay > maxDelay {
		return fmt.Errorf("%w: %v", ErrDelay, delay)
	}
	if size := img.Bounds().Size(); size.X != s.width || size.Y != s.height {
		return fmt.Errorf("%w: got %dx%d, want %dx%d", ErrFrameSize, size.X, size.Y, s.width, s.height)
	}
	millis := uint32((max(delay, minDelay) + time.Millisecond/2) / time.Millisecond)

	canvas := s.cur.Bounds()
	draw.Draw(s.cur, canvas, img, img.Bounds().Min, draw.Src)

	rect := canvas
	if len(s.frames) > 0 {
		rect = changedRect(s.cur, s.shown)
		if rect.Empty() {
			last := &s.frames[len(s.frames)-1]
			if sum := last.delayMillis + millis; sum <= uint32(maxDelay/time.Millisecond) {
				last.delayMillis = sum
				return nil
			}
			// A frame is still needed to carry the delay: re-send one pixel.
			rect = image.Rect(0, 0, 1, 1)
		} else {
			// ANMF stores offsets divided by two.
			rect.Min.X &^= 1
			rect.Min.Y &^= 1
		}
	}
	region := s.cur.SubImage(rect).(*image.NRGBA)
	vp8l, err := encodeVP8L(region)
	if err != nil {
		return err
	}
	s.alpha = s.alpha || !region.Opaque()
	s.frames = append(s.frames, frame{rect: rect, delayMillis: millis, vp8l: vp8l})
	s.shown, s.cur = s.cur, s.shown
	return nil
}

// changedRect returns the bounding rectangle of the pixels that differ
// between a and b, which must have equal bounds starting at the origin. It is
// empty if there are none.
func changedRect(a, b *image.NRGBA) image.Rectangle {
	bounds := a.Bounds()
	w := bounds.Dx()
	rect := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		ra := a.Pix[a.PixOffset(0, y):][:4*w]
		rb := b.Pix[b.PixOffset(0, y):][:4*w]
		if bytes.Equal(ra, rb) {
			continue
		}
		first := 0
		for bytes.Equal(ra[4*first:4*first+4], rb[4*first:4*first+4]) {
			first++
		}
		last := w - 1
		for bytes.Equal(ra[4*last:4*last+4], rb[4*last:4*last+4]) {
			last--
		}
		rect.Min.X = min(rect.Min.X, first)
		rect.Max.X = max(rect.Max.X, last+1)
		rect.Min.Y = min(rect.Min.Y, y)
		rect.Max.Y = y + 1
	}
	if rect.Empty() {
		return image.Rectangle{}
	}
	return rect
}

// Frames returns the number of frames Close wrote, after identical images
// were merged into one frame. It is 0 before Close and after a Close that
// failed.
func (s *Sink) Frames() int { return s.written }

// Close writes the WebP to the underlying writer. It returns ErrNoFrames if
// no frame was added. Close is idempotent: later calls return the first
// call's result without writing again.
func (s *Sink) Close() error {
	if s.closed {
		return s.closeErr
	}
	s.closed = true
	s.closeErr = s.write()
	if s.closeErr == nil {
		s.written = len(s.frames)
	}
	s.frames, s.shown, s.cur = nil, nil, nil
	return s.closeErr
}

func (s *Sink) write() error {
	if len(s.frames) == 0 {
		return ErrNoFrames
	}
	// The RIFF size counts everything after its own field.
	riffSize := int64(len(fourCCWEBP)) +
		chunkHeaderSize + vp8xPayloadSize +
		chunkHeaderSize + animPayloadSize
	for _, f := range s.frames {
		riffSize += int64(frameChunkSize(len(f.vp8l)))
	}
	if riffSize > math.MaxUint32 {
		return ErrTooLarge
	}

	bw := bufio.NewWriter(s.w)
	rw := &riffWriter{w: bw}
	rw.header(fourCCRIFF, int(riffSize))
	rw.write([]byte(fourCCWEBP))
	rw.vp8x(s.width, s.height, s.alpha)
	rw.anim(s.loops)
	for _, f := range s.frames {
		rw.frame(f.rect.Min.X, f.rect.Min.Y, f.rect.Dx(), f.rect.Dy(), f.delayMillis, f.vp8l)
	}
	if rw.err != nil {
		return rw.err
	}
	return bw.Flush()
}

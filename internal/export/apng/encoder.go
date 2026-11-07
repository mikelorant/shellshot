package apng

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"math"
	"time"
)

var (
	// ErrDelay is wrapped by AddFrame errors for delays outside
	// 0..65535 ms.
	ErrDelay = errors.New("apng: frame delay out of range")
	// ErrNoFrames is returned by Close when no frame was added.
	ErrNoFrames = errors.New("apng: no frames")
	// ErrFrameSize is wrapped by AddFrame errors when the image size differs
	// from the canvas size.
	ErrFrameSize = errors.New("apng: frame size does not match canvas")
	// ErrFrameRect is wrapped by AddFrameRect errors when the rectangle is
	// empty, outside the canvas, or does not cover the canvas in the first
	// frame.
	ErrFrameRect = errors.New("apng: invalid frame rectangle")
	// ErrFrameOp is wrapped by AddFrameRect errors for an unknown blend or
	// dispose operation.
	ErrFrameOp = errors.New("apng: invalid frame operation")
	// ErrClosed is returned by AddFrame after Close.
	ErrClosed = errors.New("apng: encoder closed")
)

// delayDenominator is the fcTL delay_den: delays are whole milliseconds.
const delayDenominator = 1000

const maxDelay = math.MaxUint16 * time.Millisecond

// Options configures an Encoder.
type Options struct {
	// Loops is the number of times the animation plays; 0 means forever.
	Loops int
	// Compression is the image/png compression level used for every frame.
	// The zero value (png.DefaultCompression) is treated as
	// png.BestCompression.
	Compression png.CompressionLevel
}

type frame struct {
	rect        image.Rectangle
	delayMillis uint16
	blend       BlendOp
	dispose     DisposeOp
	idat        []byte
}

// Encoder writes an APNG to an io.Writer. It buffers all frames and writes
// the file in Close; see the package documentation. It is not safe for
// concurrent use.
type Encoder struct {
	w      io.Writer
	width  int
	height int
	loops  uint32
	level  png.CompressionLevel

	frames []frame
	// defaultIDAT is the compressed default image set by SetDefaultImage,
	// nil if there is none.
	defaultIDAT []byte
	// written is the number of animation frames Close wrote.
	written  int
	closed   bool
	closeErr error

	// base is the canvas a decoder starts the next frame from: the previous
	// frame rendered, then disposed. shown is the canvas as the previous
	// frame displays it. Both are allocated by the first frame; BlendOver
	// is tracked with integer rounding that may differ from a decoder's by
	// one unit per channel.
	base  *image.NRGBA
	shown *image.NRGBA
}

// NewEncoder returns an Encoder for a width×height canvas that writes to w.
func NewEncoder(w io.Writer, width, height int, opts Options) (*Encoder, error) {
	if width <= 0 || height <= 0 || width > math.MaxInt32 || height > math.MaxInt32 {
		return nil, fmt.Errorf("apng: invalid canvas size %dx%d", width, height)
	}
	if opts.Loops < 0 || uint64(opts.Loops) > math.MaxUint32 {
		return nil, fmt.Errorf("apng: invalid loop count %d", opts.Loops)
	}
	level := opts.Compression
	if level == png.DefaultCompression {
		level = png.BestCompression
	}
	return &Encoder{w: w, width: width, height: height, loops: uint32(opts.Loops), level: level}, nil
}

// AddFrame appends a full-canvas frame shown for delay. img must be exactly
// width×height pixels (its origin may be non-zero). The delay is rounded to
// the nearest millisecond and must be within 0..65535 ms, otherwise the
// returned error wraps ErrDelay. The frame uses BlendSource and DisposeNone.
func (e *Encoder) AddFrame(img image.Image, delay time.Duration) error {
	return e.AddFrameRect(img, image.Rect(0, 0, e.width, e.height), delay, BlendSource, DisposeNone)
}

// SetDefaultImage sets the default image: the picture a viewer without
// animation support shows, written as the IDAT image that is not part of the
// animation. Without one, the first frame is both the default image and the
// first animation frame, and that is what such a viewer shows. The default
// image is not counted in the frame count of the animation, and costs as much
// as a full-canvas frame. img must be exactly width×height pixels (its origin
// may be non-zero). Calling it again replaces the image. Errors wrap
// ErrFrameSize or are ErrClosed.
func (e *Encoder) SetDefaultImage(img image.Image) error {
	if e.closed {
		return ErrClosed
	}
	if size := img.Bounds().Size(); size.X != e.width || size.Y != e.height {
		return fmt.Errorf("%w: default image got %dx%d, want %dx%d", ErrFrameSize, size.X, size.Y, e.width, e.height)
	}
	region := image.NewNRGBA(image.Rect(0, 0, e.width, e.height))
	draw.Draw(region, region.Bounds(), img, img.Bounds().Min, draw.Src)
	idat, err := encodeFrameIDAT(region, e.width, e.height, e.level)
	if err != nil {
		return err
	}
	e.defaultIDAT = idat
	return nil
}

// Frames returns the number of animation frames Close wrote, after
// AddFrameAuto merged identical images into one frame; the default image is
// not one of them. It is 0 before Close and after a Close that failed.
func (e *Encoder) Frames() int { return e.written }

// AddFrameRect appends a frame that updates only rect, shown for delay. img
// is the full canvas-size image (width×height pixels, origin may be
// non-zero); only the part of it under rect is encoded. rect is in canvas
// coordinates, with (0,0) the top-left of img. The first frame must cover
// the whole canvas. Errors wrap ErrFrameSize, ErrFrameRect, ErrFrameOp or
// ErrDelay as appropriate.
func (e *Encoder) AddFrameRect(img image.Image, rect image.Rectangle, delay time.Duration, blend BlendOp, dispose DisposeOp) error {
	millis, err := e.checkFrame(img, delay)
	if err != nil {
		return err
	}
	canvas := image.Rect(0, 0, e.width, e.height)
	if rect.Empty() || !rect.In(canvas) {
		return fmt.Errorf("%w: %v in canvas %v", ErrFrameRect, rect, canvas)
	}
	if len(e.frames) == 0 && rect != canvas {
		return fmt.Errorf("%w: first frame must cover the canvas %v, got %v", ErrFrameRect, canvas, rect)
	}
	if blend != BlendSource && blend != BlendOver {
		return fmt.Errorf("%w: blend op %d", ErrFrameOp, blend)
	}
	if dispose != DisposeNone && dispose != DisposeBackground && dispose != DisposePrevious {
		return fmt.Errorf("%w: dispose op %d", ErrFrameOp, dispose)
	}
	region := image.NewNRGBA(rect)
	draw.Draw(region, rect, img, img.Bounds().Min.Add(rect.Min), draw.Src)
	return e.addRegion(region, millis, blend, dispose)
}

// AddFrameAuto appends img, a full canvas-size image, as the next state of
// the animation, shown for delay. It diffs img against the canvas as the
// previous frames leave it and encodes only the bounding rectangle of the
// changed pixels, using BlendSource and DisposeNone. If nothing changed, no
// frame is added and delay is added to the previous frame instead (unless
// that would exceed 65535 ms, in which case a 1×1 frame is added). The first
// frame covers the whole canvas. Errors are as for AddFrame.
func (e *Encoder) AddFrameAuto(img image.Image, delay time.Duration) error {
	millis, err := e.checkFrame(img, delay)
	if err != nil {
		return err
	}
	canvas := image.Rect(0, 0, e.width, e.height)
	cur := image.NewNRGBA(canvas)
	draw.Draw(cur, canvas, img, img.Bounds().Min, draw.Src)
	if len(e.frames) == 0 {
		return e.addRegion(cur, millis, BlendSource, DisposeNone)
	}
	if bytes.Equal(cur.Pix, e.shown.Pix) {
		last := &e.frames[len(e.frames)-1]
		if sum := uint32(last.delayMillis) + uint32(millis); sum <= math.MaxUint16 {
			last.delayMillis = uint16(sum)
			return nil
		}
	}
	rect := changedRect(cur, e.base)
	if rect.Empty() {
		// Equal to the canvas but a frame is still needed to carry the
		// delay: re-send one pixel.
		rect = image.Rect(0, 0, 1, 1)
	}
	return e.addRegion(cur.SubImage(rect).(*image.NRGBA), millis, BlendSource, DisposeNone)
}

// checkFrame validates the encoder state, delay and image size, and returns
// the delay in whole milliseconds.
func (e *Encoder) checkFrame(img image.Image, delay time.Duration) (uint16, error) {
	if e.closed {
		return 0, ErrClosed
	}
	if delay < 0 || delay > maxDelay {
		return 0, fmt.Errorf("%w: %v", ErrDelay, delay)
	}
	if size := img.Bounds().Size(); size.X != e.width || size.Y != e.height {
		return 0, fmt.Errorf("%w: got %dx%d, want %dx%d", ErrFrameSize, size.X, size.Y, e.width, e.height)
	}
	millis := (delay + time.Millisecond/2) / time.Millisecond
	if millis > math.MaxUint16 {
		return 0, fmt.Errorf("%w: %v", ErrDelay, delay)
	}
	return uint16(millis), nil
}

// addRegion encodes region (whose bounds are its canvas rectangle) as the
// next frame and updates the tracked canvas.
func (e *Encoder) addRegion(region *image.NRGBA, millis uint16, blend BlendOp, dispose DisposeOp) error {
	rect := region.Bounds()
	idat, err := encodeFrameIDAT(region, rect.Dx(), rect.Dy(), e.level)
	if err != nil {
		return err
	}
	e.compose(region, blend, dispose)
	e.frames = append(e.frames, frame{rect: rect, delayMillis: millis, blend: blend, dispose: dispose, idat: idat})
	return nil
}

// compose renders region onto the tracked canvas the way a decoder does.
func (e *Encoder) compose(region *image.NRGBA, blend BlendOp, dispose DisposeOp) {
	canvas := image.Rect(0, 0, e.width, e.height)
	if e.base == nil {
		e.base = image.NewNRGBA(canvas)
		e.shown = image.NewNRGBA(canvas)
	}
	rect := region.Bounds()
	var before *image.NRGBA
	if dispose == DisposePrevious {
		before = image.NewNRGBA(rect)
		draw.Draw(before, rect, e.base, rect.Min, draw.Src)
	}
	if blend == BlendSource {
		draw.Draw(e.base, rect, region, rect.Min, draw.Src)
	} else {
		blendOver(e.base, region)
	}
	copy(e.shown.Pix, e.base.Pix)
	switch dispose {
	case DisposeBackground:
		draw.Draw(e.base, rect, image.Transparent, image.Point{}, draw.Src)
	case DisposePrevious:
		draw.Draw(e.base, rect, before, rect.Min, draw.Src)
	}
}

// blendOver composites src over the same coordinates of dst, both
// non-premultiplied.
func blendOver(dst, src *image.NRGBA) {
	b := src.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		s := src.Pix[src.PixOffset(b.Min.X, y):][:4*b.Dx()]
		d := dst.Pix[dst.PixOffset(b.Min.X, y):][:4*b.Dx()]
		for i := 0; i < len(s); i += 4 {
			sa := uint32(s[i+3])
			switch sa {
			case 0:
				continue
			case 255:
				copy(d[i:i+4], s[i:i+4])
				continue
			}
			da := uint32(d[i+3])
			outA := sa*255 + da*(255-sa) // 255 × the result alpha
			for c := 0; c < 3; c++ {
				d[i+c] = uint8((uint32(s[i+c])*sa*255 + uint32(d[i+c])*da*(255-sa) + outA/2) / outA)
			}
			d[i+3] = uint8((outA + 127) / 255)
		}
	}
}

// changedRect returns the bounding rectangle of the pixels that differ
// between a and b, which must have equal bounds. It is empty if there are
// none.
func changedRect(a, b *image.NRGBA) image.Rectangle {
	bounds := a.Bounds()
	w := bounds.Dx()
	rect := image.Rectangle{Min: bounds.Max, Max: bounds.Min}
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		ra := a.Pix[a.PixOffset(bounds.Min.X, y):][:4*w]
		rb := b.Pix[b.PixOffset(bounds.Min.X, y):][:4*w]
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
		rect.Min.X = min(rect.Min.X, bounds.Min.X+first)
		rect.Max.X = max(rect.Max.X, bounds.Min.X+last+1)
		rect.Min.Y = min(rect.Min.Y, y)
		rect.Max.Y = y + 1
	}
	if rect.Empty() {
		return image.Rectangle{}
	}
	return rect
}

// Close writes the APNG to the underlying writer, ending with IEND. It
// returns ErrNoFrames if no frame was added. Close is idempotent: later calls
// return the first call's result without writing again.
func (e *Encoder) Close() error {
	if e.closed {
		return e.closeErr
	}
	e.closed = true
	e.closeErr = e.write()
	if e.closeErr == nil {
		e.written = len(e.frames)
	}
	e.frames, e.base, e.shown, e.defaultIDAT = nil, nil, nil, nil
	return e.closeErr
}

func (e *Encoder) write() error {
	if len(e.frames) == 0 {
		return ErrNoFrames
	}
	bw := bufio.NewWriter(e.w)
	cw := &chunkWriter{w: bw}
	cw.writeRaw([]byte(pngSignature))
	cw.writeChunk(chunkIHDR, ihdrData(e.width, e.height))
	cw.writeChunk(chunkACTL, actlData(uint32(len(e.frames)), e.loops))

	// A default image is the IDAT image with no fcTL before it, so every
	// animation frame, the first too, is fdAT.
	if e.defaultIDAT != nil {
		cw.writeChunk(chunkIDAT, e.defaultIDAT)
	}
	// Sequence numbers are shared by fcTL and fdAT chunks, counting from 0.
	var seq uint32
	for i, f := range e.frames {
		cw.writeChunk(chunkFCTL, fctlData(seq, f.rect, f.delayMillis, f.dispose, f.blend))
		seq++
		if i == 0 && e.defaultIDAT == nil {
			cw.writeChunk(chunkIDAT, f.idat)
			continue
		}
		cw.writeChunk(chunkFDAT, seqData(seq), f.idat)
		seq++
	}
	cw.writeChunk(chunkIEND)
	if cw.err != nil {
		return cw.err
	}
	return bw.Flush()
}

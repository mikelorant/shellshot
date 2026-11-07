package apng

import (
	"bufio"
	"errors"
	"fmt"
	"image"
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
	delayMillis uint16
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

	frames   []frame
	closed   bool
	closeErr error
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
// returned error wraps ErrDelay.
func (e *Encoder) AddFrame(img image.Image, delay time.Duration) error {
	if e.closed {
		return ErrClosed
	}
	if delay < 0 || delay > maxDelay {
		return fmt.Errorf("%w: %v", ErrDelay, delay)
	}
	if size := img.Bounds().Size(); size.X != e.width || size.Y != e.height {
		return fmt.Errorf("%w: got %dx%d, want %dx%d", ErrFrameSize, size.X, size.Y, e.width, e.height)
	}
	millis := (delay + time.Millisecond/2) / time.Millisecond
	if millis > math.MaxUint16 {
		return fmt.Errorf("%w: %v", ErrDelay, delay)
	}
	idat, err := encodeFrameIDAT(img, e.width, e.height, e.level)
	if err != nil {
		return err
	}
	e.frames = append(e.frames, frame{delayMillis: uint16(millis), idat: idat})
	return nil
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
	e.frames = nil
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

	// Sequence numbers are shared by fcTL and fdAT chunks, counting from 0.
	var seq uint32
	for i, f := range e.frames {
		cw.writeChunk(chunkFCTL, fctlData(seq, e.width, e.height, f.delayMillis))
		seq++
		if i == 0 {
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

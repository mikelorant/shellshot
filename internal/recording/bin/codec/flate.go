package codec

import (
	"bytes"
	"compress/flate"
	"errors"
	"fmt"
	"io"
	"sync"
)

var errNegativeMaxLen = errors.New("codec: negative maxLen")

// FlateOptions configures a FlateCodec.
type FlateOptions struct {
	// Level is a compress/flate level: flate.HuffmanOnly (-2),
	// flate.DefaultCompression (-1), or 0 (flate.NoCompression) to 9.
	// Note the zero value is flate.NoCompression; use DefaultFlateOptions
	// for the default level.
	Level int
}

// DefaultFlateOptions returns options using flate.DefaultCompression.
func DefaultFlateOptions() FlateOptions {
	return FlateOptions{Level: flate.DefaultCompression}
}

// FlateCodec is raw DEFLATE via compress/flate, with optional preset
// dictionary.
//
// Pooling: compress/flate writers bake the preset dictionary in at creation
// and Reset restores that same dictionary, so a writer cannot move between
// dictionaries. Only the no-dictionary writers are pooled; a call with a
// dictionary allocates a fresh writer (roughly 1 MiB of state). Readers accept
// a dictionary on Reset, so readers are pooled for all calls.
type FlateCodec struct {
	level   int
	writers sync.Pool // *flateWriter, no-dict only
	readers sync.Pool // *flateReader
}

// NewFlateCodec validates opts and returns a FlateCodec.
func NewFlateCodec(opts FlateOptions) (*FlateCodec, error) {
	if _, err := flate.NewWriter(io.Discard, opts.Level); err != nil {
		return nil, fmt.Errorf("codec: invalid flate level %d: %w", opts.Level, err)
	}
	return &FlateCodec{level: opts.Level}, nil
}

// ID returns Flate.
func (*FlateCodec) ID() ID { return Flate }

// appendWriter is an io.Writer that appends to buf.
type appendWriter struct{ buf []byte }

func (a *appendWriter) Write(p []byte) (int, error) {
	a.buf = append(a.buf, p...)
	return len(p), nil
}

type flateWriter struct {
	w    *flate.Writer
	sink appendWriter
}

// Compress appends the DEFLATE encoding of src to dst.
func (c *FlateCodec) Compress(dst, src, dict []byte) ([]byte, error) {
	var fw *flateWriter
	if len(dict) == 0 {
		if v, ok := c.writers.Get().(*flateWriter); ok {
			fw = v
			fw.w.Reset(&fw.sink)
		}
	}
	if fw == nil {
		fw = &flateWriter{}
		var err error
		if len(dict) == 0 {
			fw.w, err = flate.NewWriter(&fw.sink, c.level)
		} else {
			fw.w, err = flate.NewWriterDict(&fw.sink, c.level, dict)
		}
		if err != nil {
			return dst, err
		}
	}
	fw.sink.buf = dst
	_, err := fw.w.Write(src)
	if err == nil {
		err = fw.w.Close()
	}
	out := fw.sink.buf
	fw.sink.buf = nil
	if len(dict) == 0 {
		c.writers.Put(fw)
	}
	if err != nil {
		return dst, err
	}
	return out, nil
}

type flateReader struct {
	r   io.ReadCloser
	src bytes.Reader
}

// Decompress appends the inflated src to dst, producing at most maxLen bytes.
//
// A wrong dictionary is not reliably detected: DEFLATE carries no dictionary
// checksum, so the result is either an ErrCorrupt error (a back-reference
// reaches before the available history) or silently different bytes. Callers
// that need to detect this must checksum the decompressed output.
func (c *FlateCodec) Decompress(dst, src, dict []byte, maxLen int) ([]byte, error) {
	if maxLen < 0 {
		return dst, errNegativeMaxLen
	}
	fr, _ := c.readers.Get().(*flateReader)
	if fr == nil {
		fr = &flateReader{}
		fr.src.Reset(src)
		fr.r = flate.NewReaderDict(&fr.src, dict)
	} else {
		fr.src.Reset(src)
		if err := fr.r.(flate.Resetter).Reset(&fr.src, dict); err != nil {
			return dst, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
	}
	out, err := readCapped(dst, fr.r, maxLen)
	if err == nil && fr.src.Len() > 0 {
		err = fmt.Errorf("%w: %d trailing bytes", ErrCorrupt, fr.src.Len())
	}
	fr.src.Reset(nil)
	c.readers.Put(fr)
	if err != nil {
		return dst, err
	}
	return out, nil
}

// readCapped appends r's output to dst until EOF, returning ErrTooLarge as
// soon as more than maxLen bytes are available. Capacity grows geometrically
// but never beyond len(dst)+maxLen+1, so allocation is bounded by the cap.
func readCapped(dst []byte, r io.Reader, maxLen int) ([]byte, error) {
	start := len(dst)
	limit := start + maxLen + 1 // reading this many proves overflow
	if limit < start {          // int overflow
		limit = int(^uint(0) >> 1)
	}
	buf := dst
	for {
		if len(buf) == cap(buf) {
			newCap := 2*cap(buf) + 512
			if newCap > limit || newCap < 0 {
				newCap = limit
			}
			grown := make([]byte, len(buf), newCap)
			copy(grown, buf)
			buf = grown
		}
		end := min(cap(buf), limit)
		n, err := r.Read(buf[len(buf):end])
		buf = buf[:len(buf)+n]
		if len(buf)-start > maxLen {
			return dst, ErrTooLarge
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF):
			return buf, nil
		default:
			return dst, fmt.Errorf("%w: %v", ErrCorrupt, err)
		}
	}
}

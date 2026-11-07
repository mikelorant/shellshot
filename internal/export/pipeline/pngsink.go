package pipeline

import (
	"image"
	"image/png"
	"io"
	"time"

	"github.com/mikelorant/shellshot/internal/export/apng"
)

// maxPNGDelay is the longest delay one PNG frame can carry.
const maxPNGDelay = 65535 * time.Millisecond

// PNGSink is the Sink that writes an animated PNG. It adapts
// *apng.Encoder, which does not know this package.
type PNGSink struct {
	enc *apng.Encoder
}

// PNGSinkFactory returns the SinkFactory for a PNG that plays loops times
// (0 for forever) with PNG compression level compression.
func PNGSinkFactory(loops int, compression png.CompressionLevel) SinkFactory {
	return func(w io.Writer, width, height int) (Sink, error) {
		enc, err := apng.NewEncoder(w, width, height, apng.Options{Loops: loops, Compression: compression})
		if err != nil {
			return nil, err
		}
		return &PNGSink{enc: enc}, nil
	}
}

var (
	_ Sink         = (*PNGSink)(nil)
	_ DelayLimiter = (*PNGSink)(nil)
	_ AutoFrame    = (*PNGSink)(nil)
	_ PosterSetter = (*PNGSink)(nil)
	_ FrameCounter = (*PNGSink)(nil)
)

// AddFrame adds img as a full frame.
func (s *PNGSink) AddFrame(img *image.RGBA, delay time.Duration) error {
	return s.enc.AddFrame(img, delay)
}

// AddFrameAuto adds img as a delta frame holding only the pixels that
// changed since the previous frame. Export prefers it over AddFrame.
func (s *PNGSink) AddFrameAuto(img image.Image, delay time.Duration) error {
	return s.enc.AddFrameAuto(img, delay)
}

// SetPoster makes img the default image of the PNG: the picture viewers
// without animation support show. The animation itself is unchanged.
func (s *PNGSink) SetPoster(img image.Image) error { return s.enc.SetDefaultImage(img) }

// Frames is the number of animation frames in the file Close wrote; the
// default image is not one of them.
func (s *PNGSink) Frames() int { return s.enc.Frames() }

// MaxDelay is the longest delay one PNG frame can carry, 65535 ms.
func (s *PNGSink) MaxDelay() time.Duration { return maxPNGDelay }

// Close writes the PNG.
func (s *PNGSink) Close() error { return s.enc.Close() }

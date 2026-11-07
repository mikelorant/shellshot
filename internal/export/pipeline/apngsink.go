package pipeline

import (
	"image"
	"image/png"
	"io"
	"time"

	"github.com/mikelorant/shellshot/internal/export/apng"
)

// maxAPNGDelay is the longest delay one APNG frame can carry.
const maxAPNGDelay = 65535 * time.Millisecond

// APNGSink is the Sink that writes an animated PNG. It adapts
// *apng.Encoder, which does not know this package.
type APNGSink struct {
	enc *apng.Encoder
}

// APNGSinkFactory returns the SinkFactory for an APNG that plays loops times
// (0 for forever) with PNG compression level compression.
func APNGSinkFactory(loops int, compression png.CompressionLevel) SinkFactory {
	return func(w io.Writer, width, height int) (Sink, error) {
		enc, err := apng.NewEncoder(w, width, height, apng.Options{Loops: loops, Compression: compression})
		if err != nil {
			return nil, err
		}
		return &APNGSink{enc: enc}, nil
	}
}

var (
	_ Sink         = (*APNGSink)(nil)
	_ DelayLimiter = (*APNGSink)(nil)
	_ AutoFrame    = (*APNGSink)(nil)
	_ PosterSetter = (*APNGSink)(nil)
	_ FrameCounter = (*APNGSink)(nil)
)

// AddFrame adds img as a full frame.
func (s *APNGSink) AddFrame(img *image.RGBA, delay time.Duration) error {
	return s.enc.AddFrame(img, delay)
}

// AddFrameAuto adds img as a delta frame holding only the pixels that
// changed since the previous frame. Export prefers it over AddFrame.
func (s *APNGSink) AddFrameAuto(img image.Image, delay time.Duration) error {
	return s.enc.AddFrameAuto(img, delay)
}

// SetPoster makes img the default image of the APNG: the picture viewers
// without animation support show. The animation itself is unchanged.
func (s *APNGSink) SetPoster(img image.Image) error { return s.enc.SetDefaultImage(img) }

// Frames is the number of animation frames in the file Close wrote; the
// default image is not one of them.
func (s *APNGSink) Frames() int { return s.enc.Frames() }

// MaxDelay is the longest delay one APNG frame can carry, 65535 ms.
func (s *APNGSink) MaxDelay() time.Duration { return maxAPNGDelay }

// Close writes the APNG.
func (s *APNGSink) Close() error { return s.enc.Close() }

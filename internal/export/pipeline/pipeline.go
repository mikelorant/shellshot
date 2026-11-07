// Package pipeline turns a stream of timestamped snapshot frames into an
// APNG: it reads every frame, applies the timeline policy, renders each
// distinct screen and feeds the result to the APNG encoder. It is pure Go
// (no cgo, no cobra); the frame source and the renderer are small interfaces
// defined here, so any recording format or renderer can plug in.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"time"

	"github.com/michaellorant/shellshot/internal/export/apng"
	"github.com/michaellorant/shellshot/internal/export/timeline"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Source yields recorded frames in order. Next returns io.EOF after the last
// frame. *frames.Iterator satisfies it.
type Source interface {
	Next() (snapshot.Frame, error)
}

// Renderer draws snapshots. *raster.Renderer satisfies it.
type Renderer interface {
	// Size returns the image size in pixels that Render produces for a
	// cols x rows screen.
	Size(cols, rows int) (width, height int)
	// Render draws s into a new image of the size reported by Size.
	Render(s snapshot.Snapshot) (*image.RGBA, error)
}

// Options configures Export.
type Options struct {
	// Timeline sets the idle cap, speed, final hold and minimum delay.
	Timeline timeline.Options
	// Loops is the APNG play count; 0 means forever.
	Loops int
	// Compression is the PNG compression level for every APNG frame.
	Compression png.CompressionLevel
	// Progress, if not nil, is called after each timeline step has been
	// encoded, with the number of steps done and the total number of steps.
	Progress func(done, total int)
}

// maxFrameDelay is the longest delay one APNG frame can carry.
const maxFrameDelay = 65535 * time.Millisecond

// Export reads all frames from src, renders them with r at the cols x rows
// screen size and writes an APNG to w. Consecutive equal screens are
// rendered once and merged into one longer frame. Cancelling ctx stops the
// export between frames; errors name the frame or step that failed. Nothing
// is written to w unless the whole animation encodes.
func Export(ctx context.Context, src Source, cols, rows int, r Renderer, w io.Writer, o Options) error {
	var frames []snapshot.Frame
	for i := 0; ; i++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("export: read frame %d: %w", i, err)
		}
		f, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return fmt.Errorf("export: read frame %d: %w", i, err)
		}
		frames = append(frames, f)
	}
	steps, err := timeline.Build(frames, o.Timeline)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	if len(steps) == 0 {
		return errors.New("export: recording has no frames")
	}
	frames = nil // free the raw frames; steps hold what is needed

	width, height := r.Size(cols, rows)
	enc, err := apng.NewEncoder(w, width, height, apng.Options{Loops: o.Loops, Compression: o.Compression})
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	var (
		prev    snapshot.Snapshot
		prevImg *image.RGBA
	)
	for i, s := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("export: step %d of %d: %w", i, len(steps), err)
		}
		if prevImg == nil || !s.Snapshot.Equal(prev) {
			if prevImg, err = r.Render(s.Snapshot); err != nil {
				return fmt.Errorf("export: render step %d of %d: %w", i, len(steps), err)
			}
			prev = s.Snapshot
		}
		if err := addFrame(enc, prevImg, s.Delay); err != nil {
			return fmt.Errorf("export: encode step %d of %d: %w", i, len(steps), err)
		}
		if o.Progress != nil {
			o.Progress(i+1, len(steps))
		}
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("export: write: %w", err)
	}
	return nil
}

// addFrame adds img for delay, splitting delays longer than one APNG frame
// can carry into repeats of the same image (which the encoder merges where
// it can).
func addFrame(enc *apng.Encoder, img *image.RGBA, delay time.Duration) error {
	for {
		d := min(delay, maxFrameDelay)
		if err := enc.AddFrameAuto(img, d); err != nil {
			return err
		}
		delay -= d
		if delay <= 0 {
			return nil
		}
	}
}

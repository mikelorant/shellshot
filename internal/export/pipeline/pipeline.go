// Package pipeline turns a stream of timestamped snapshot frames into an
// animation: it reads every frame, applies the timeline policy, renders each
// distinct screen and feeds the result to a Sink (an APNG encoder unless
// Options.NewSink says otherwise). It is pure Go (no cgo, no cobra); the
// frame source, the renderer and the sink are small interfaces defined here,
// so any recording format, renderer or raster output format can plug in.
package pipeline

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"time"

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

// Sink receives the rendered frames of one animation and writes the encoded
// result to the writer it was created with. Export drives it from a single
// goroutine: AddFrame once per timeline step (more than once for a step
// longer than the sink's MaxDelay), then Close exactly once.
//
// A Sink writes to its writer only from Close, so a failed export leaves
// nothing behind; Export also silences the writer it gives the sink when the
// export fails, so a Close after a failure writes nothing.
//
// A Sink may additionally implement DelayLimiter and AutoFrame.
type Sink interface {
	// AddFrame appends img, shown for delay. Export never modifies img, and
	// passes the same image again for the next step if the screen did not
	// change. delay never exceeds MaxDelay when the sink is a DelayLimiter.
	AddFrame(img *image.RGBA, delay time.Duration) error
	// Close finishes the animation and writes it out. Export calls it once,
	// also after an error (then with a writer that discards, so the sink can
	// release resources); a Close error on a successful export fails it.
	Close() error
}

// SinkFactory creates the Sink for an animation of width x height pixels
// that writes to w.
type SinkFactory func(w io.Writer, width, height int) (Sink, error)

// DelayLimiter is implemented by a Sink whose format has a longest frame
// delay. Export splits a longer hold into several AddFrame calls of the same
// image, each at most MaxDelay. A MaxDelay of 0 means unlimited, as for a
// Sink that does not implement DelayLimiter.
type DelayLimiter interface {
	MaxDelay() time.Duration
}

// AutoFrame is implemented by a Sink that can store only what changed
// between frames (the APNG sink). Export calls AddFrameAuto instead of
// AddFrame on such a sink; the contract of AddFrame applies.
type AutoFrame interface {
	AddFrameAuto(img image.Image, delay time.Duration) error
}

// Options configures Export.
type Options struct {
	// Timeline sets the idle cap, speed, final hold and minimum delay.
	Timeline timeline.Options
	// Loops is the APNG play count; 0 means forever. It only applies when
	// NewSink is nil.
	Loops int
	// Compression is the PNG compression level for every APNG frame. It only
	// applies when NewSink is nil.
	Compression png.CompressionLevel
	// NewSink creates the output format's Sink. If nil, Export writes an
	// APNG built from Loops and Compression.
	NewSink SinkFactory
	// Progress, if not nil, is called after each timeline step has been
	// encoded, with the number of steps done and the total number of steps.
	Progress func(done, total int)
}

// Export reads all frames from src, renders them with r at the cols x rows
// screen size and writes the animation to w through the Options.NewSink sink
// (an APNG by default). Consecutive equal screens are rendered once and
// merged into one longer frame. Cancelling ctx stops the export between
// frames; errors name the frame or step that failed. Nothing is written to w
// unless the whole animation encodes.
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
	newSink := o.NewSink
	if newSink == nil {
		newSink = APNGSinkFactory(o.Loops, o.Compression)
	}
	out := &gatedWriter{w: w}
	sink, err := newSink(out, width, height)
	if err != nil {
		return fmt.Errorf("export: %w", err)
	}
	if err := encodeSteps(ctx, steps, r, sink, o.Progress); err != nil {
		out.silence()
		sink.Close() // release the sink's resources; the error is the one already found
		return err
	}
	if err := sink.Close(); err != nil {
		return fmt.Errorf("export: write: %w", err)
	}
	return nil
}

// gatedWriter passes writes through to w until silence is called, then
// discards them.
type gatedWriter struct {
	w        io.Writer
	silenced bool
}

func (g *gatedWriter) Write(p []byte) (int, error) {
	if g.silenced {
		return len(p), nil
	}
	return g.w.Write(p)
}

func (g *gatedWriter) silence() { g.silenced = true }

// encodeSteps renders each step (once per run of equal screens) and adds it
// to sink.
func encodeSteps(ctx context.Context, steps []timeline.Step, r Renderer, sink Sink, progress func(done, total int)) error {
	var (
		err     error
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
		if err := addFrame(sink, prevImg, s.Delay); err != nil {
			return fmt.Errorf("export: encode step %d of %d: %w", i, len(steps), err)
		}
		if progress != nil {
			progress(i+1, len(steps))
		}
	}
	return nil
}

// addFrame adds img for delay, splitting a delay longer than the sink's
// MaxDelay into repeats of the same image (which the APNG sink merges where
// it can). It prefers AddFrameAuto when the sink has it.
func addFrame(sink Sink, img *image.RGBA, delay time.Duration) error {
	var limit time.Duration
	if l, ok := sink.(DelayLimiter); ok {
		limit = l.MaxDelay()
	}
	add := sink.AddFrame
	if a, ok := sink.(AutoFrame); ok {
		add = func(img *image.RGBA, d time.Duration) error { return a.AddFrameAuto(img, d) }
	}
	for {
		d := delay
		if limit > 0 {
			d = min(delay, limit)
		}
		if err := add(img, d); err != nil {
			return err
		}
		delay -= d
		if delay <= 0 {
			return nil
		}
	}
}

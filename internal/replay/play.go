// Package replay plays recorded frames to an io.Writer with their original
// timing.
package replay

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// ErrInvalidSpeed is returned by Play when Options.Speed is negative, NaN or
// infinite.
var ErrInvalidSpeed = errors.New("replay: speed must be greater than zero")

// Source yields frames in order. Next returns io.EOF after the last frame.
// *frames.Iterator satisfies Source.
type Source interface {
	Next() (snapshot.Frame, error)
}

// Options configures Play.
type Options struct {
	// Speed scales playback: 2 plays twice as fast. Zero means 1; negative,
	// NaN and infinite values are rejected with ErrInvalidSpeed.
	Speed float64
	// IdleLimit caps the wait between two frames before Speed is applied.
	// Zero means no cap.
	IdleLimit time.Duration
	// TrimLeadingBlank drops a blank first frame, whatever its length, so
	// playback opens on the first output. A lone frame is never dropped.
	TrimLeadingBlank bool
	// Clear draws each frame over the previous one from the top-left corner
	// instead of appending lines; use it when w is a terminal. See Render.
	Clear bool
	// Cols and Rows, when positive, are the size of the terminal behind w.
	// Frames larger than that are cropped to fit (see Crop); a frame taller or
	// wider than the terminal would scroll or wrap on every redraw. Only used
	// with Clear.
	Cols, Rows int
	// Color renders style spans as SGR escapes; when false, output is plain
	// text. See Render.
	Color bool
	// Sleep waits for d or until ctx is done, returning ctx's error in the
	// latter case. Nil means a timer-based wait. Tests inject their own.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Play reads frames from src and writes each rendered frame to w, waiting
// between frames for the difference in their At times, capped at IdleLimit and
// divided by Speed (the timeline package applies this policy, as for export).
// The first frame is shown immediately. Frames whose At does not advance are
// shown without waiting. Consecutive frames with equal snapshots are shown
// once (a frames.Iterator never yields them). Play reads all of src before it shows anything, so a
// recording is held in memory while it plays. It returns nil once src reports
// io.EOF, and otherwise an error wrapping the cause (context, source or write
// failure); frames read before a source failure are still played.
func Play(ctx context.Context, src Source, w io.Writer, opts Options) error {
	speed := opts.Speed
	if speed == 0 {
		speed = 1
	}
	if speed < 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return fmt.Errorf("%w (got %v)", ErrInvalidSpeed, opts.Speed)
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepTimer
	}

	var frames []snapshot.Frame
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("replay: %w", err)
		}
		f, err := src.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			readErr = fmt.Errorf("replay: read frame: %w", err)
			break
		}
		frames = append(frames, f)
	}
	steps, err := timeline.Build(frames, timeline.Options{
		Speed:            speed,
		IdleLimit:        opts.IdleLimit,
		TrimLeadingBlank: opts.TrimLeadingBlank,
	})
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}

	for _, s := range steps {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("replay: %w", err)
		}
		snap := s.Snapshot
		if opts.Clear {
			snap = Crop(snap, opts.Cols, opts.Rows)
		}
		// One write per frame, so the terminal receives it in one piece.
		if _, err := io.WriteString(w, Render(snap, RenderOptions{Clear: opts.Clear, Color: opts.Color})); err != nil {
			return fmt.Errorf("replay: write frame: %w", err)
		}
		if s.Delay > 0 {
			if err := sleep(ctx, s.Delay); err != nil {
				return fmt.Errorf("replay: wait: %w", err)
			}
		}
	}
	return readErr
}

// sleepTimer waits for d or until ctx is done.
func sleepTimer(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

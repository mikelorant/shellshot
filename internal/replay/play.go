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
	// Clear clears the screen and homes the cursor before each frame; use it
	// when w is a terminal. See Render.
	Clear bool
	// Color renders style spans as SGR escapes; when false, output is plain
	// text. See Render.
	Color bool
	// Sleep waits for d or until ctx is done, returning ctx's error in the
	// latter case. Nil means a timer-based wait. Tests inject their own.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Play reads frames from src and writes each rendered frame to w, waiting
// between frames for the difference in their At times divided by Speed.
// The first frame is shown immediately. Frames whose At does not advance
// are shown without waiting. Play returns nil once src reports io.EOF, and
// otherwise an error wrapping the cause (context, source or write failure).
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

	var prev time.Duration
	for first := true; ; first = false {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("replay: %w", err)
		}
		f, err := src.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("replay: read frame: %w", err)
		}
		if !first && f.At > prev {
			d := time.Duration(float64(f.At-prev) / speed)
			if err := sleep(ctx, d); err != nil {
				return fmt.Errorf("replay: wait: %w", err)
			}
		}
		prev = f.At
		if _, err := io.WriteString(w, Render(f.Snapshot, RenderOptions{Clear: opts.Clear, Color: opts.Color})); err != nil {
			return fmt.Errorf("replay: write frame: %w", err)
		}
	}
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

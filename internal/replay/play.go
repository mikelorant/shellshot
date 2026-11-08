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
	// Segments give ranges of the recording clock their own speed and idle
	// limit and cut hidden ranges out, exactly as for export; see
	// timeline.Options.Segments. Nil plays the recording as it is.
	Segments []timeline.Segment
	// Color renders style spans as SGR escapes; when false, output is plain
	// text. See Render.
	Color bool
	// Sleep waits for d or until ctx is done, returning ctx's error in the
	// latter case. Nil means a timer-based wait. Tests inject their own.
	Sleep func(ctx context.Context, d time.Duration) error
}

// Stats describes how a playback went. PlayWithStats fills it in whatever the
// outcome.
type Stats struct {
	// Steps is the number of frames the timeline planned to show, and Shown
	// how many of them were drawn.
	Steps, Shown int
	// Played is the wall time from the first frame to the end of playback, on
	// the monotonic clock. Planned is the sum of the step delays (what
	// playback takes at the chosen speed and idle limit), and Recorded is the
	// time of the last frame in the recording.
	Played, Planned, Recorded time.Duration
	// IdleCapped is the number of pauses between frames that IdleLimit cut.
	// Time inside a hidden range does not count towards a pause.
	IdleCapped int
	// HiddenCut is the number of hidden ranges (Skip segments) cut out of the
	// recording and HiddenTime their total length on the recording clock.
	HiddenCut  int
	HiddenTime time.Duration
	// Cropped reports that a frame was cut to fit Options.Cols x Rows.
	Cropped bool
	// Interrupted reports that ctx was done before playback ended.
	Interrupted bool
}

// Play reads frames from src and writes each rendered frame to w, waiting
// between frames for the difference in their At times, capped at IdleLimit and
// divided by Speed (the timeline package applies this policy, as for export,
// together with Options.Segments).
// The first frame is shown immediately. Frames whose At does not advance are
// shown without waiting. Consecutive frames with equal snapshots are shown
// once (a frames.Iterator never yields them). Play reads all of src before it shows anything, so a
// recording is held in memory while it plays. It returns nil once src reports
// io.EOF, and otherwise an error wrapping the cause (context, source or write
// failure); frames read before a source failure are still played.
//
// With Options.Clear, Play leaves the terminal usable however playback ends,
// including a cancelled ctx: see PlayWithStats.
func Play(ctx context.Context, src Source, w io.Writer, opts Options) error {
	_, err := PlayWithStats(ctx, src, w, opts)
	return err
}

// PlayWithStats is Play that also returns how playback went. With Options.Clear
// and at least one frame drawn, it ends by ending any synchronized update,
// showing the cursor, resetting attributes and moving to a new line below the
// last drawn row, so whatever is written next starts on a clear line. This
// happens when playback is cancelled or fails as well as when it completes.
func PlayWithStats(ctx context.Context, src Source, w io.Writer, opts Options) (stats Stats, err error) {
	speed := opts.Speed
	if speed == 0 {
		speed = 1
	}
	if speed < 0 || math.IsNaN(speed) || math.IsInf(speed, 0) {
		return stats, fmt.Errorf("%w (got %v)", ErrInvalidSpeed, opts.Speed)
	}
	sleep := opts.Sleep
	if sleep == nil {
		sleep = sleepTimer
	}
	defer func() {
		stats.Interrupted = ctx.Err() != nil && errors.Is(err, ctx.Err())
	}()

	var frames []snapshot.Frame
	var readErr error
	for {
		if err := ctx.Err(); err != nil {
			return stats, fmt.Errorf("replay: %w", err)
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
	if n := len(frames); n > 0 {
		stats.Recorded = frames[n-1].At
	}
	stats.IdleCapped = countIdleCapped(frames, opts.Segments, opts.IdleLimit)
	stats.HiddenCut, stats.HiddenTime = hiddenCut(opts.Segments, stats.Recorded)
	steps, err := timeline.Build(frames, timeline.Options{
		Speed:            speed,
		IdleLimit:        opts.IdleLimit,
		TrimLeadingBlank: opts.TrimLeadingBlank,
		Segments:         opts.Segments,
	})
	if err != nil {
		return stats, fmt.Errorf("replay: %w", err)
	}
	stats.Steps = len(steps)
	for _, s := range steps {
		stats.Planned += s.Delay
	}

	start := time.Now()
	lastRows := 0 // rows of the last frame drawn
	defer func() {
		stats.Played = time.Since(start)
		if opts.Clear && stats.Shown > 0 {
			// Best effort: the writer may be what failed.
			io.WriteString(w, restoreTerminal(lastRows))
		}
	}()
	for _, s := range steps {
		if err := ctx.Err(); err != nil {
			return stats, fmt.Errorf("replay: %w", err)
		}
		snap := s.Snapshot
		if opts.Clear {
			full := snap
			snap = Crop(full, opts.Cols, opts.Rows)
			if snap.Cols != full.Cols || len(snap.Lines) != len(full.Lines) {
				stats.Cropped = true
			}
		}
		// One write per frame, so the terminal receives it in one piece.
		if _, err := io.WriteString(w, Render(snap, RenderOptions{Clear: opts.Clear, Color: opts.Color})); err != nil {
			return stats, fmt.Errorf("replay: write frame: %w", err)
		}
		stats.Shown++
		lastRows = len(snap.Lines)
		if s.Delay > 0 {
			if err := sleep(ctx, s.Delay); err != nil {
				return stats, fmt.Errorf("replay: wait: %w", err)
			}
		}
	}
	return stats, readErr
}

// restoreTerminal returns the text that ends playback in place: it ends a
// synchronized update, shows the cursor, resets attributes and moves to the
// start of a new line below the last drawn row (rows rows from the top).
func restoreTerminal(rows int) string {
	return ansiSyncEnd + ansiShowCursor + ansiReset + fmt.Sprintf("\x1b[%d;1H\r\n", max(rows, 1))
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

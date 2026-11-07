// Package timeline turns timestamped snapshot frames into a sequence of
// steps, each a snapshot plus how long to show it. It applies the idle cap,
// speed, final hold, de-duplication and minimum-delay policy that animated
// exporters share. It is pure: it imports only internal/snapshot and the
// standard library.
//
// # Segments
//
// Options.Segments gives time ranges their own speed, idle cap and hold, and
// can cut ranges out of the output. Rules, in addition to those on Build:
//
//   - A gap between two frames is split at segment boundaries. Each piece is
//     divided by its own segment's speed (a pause at the start of scene 2 is
//     not scaled by scene 1's speed). Time outside every segment uses the
//     global Options.
//   - The idle cap applies once to the whole stretch, not once per piece. The
//     budget is the smallest non-zero limit among the pieces that are not
//     skipped (a segment's IdleLimit replaces the global one inside it), and
//     it is spent in time order. So a 3s pause in each of two adjacent
//     scenes with a 2s limit shows as 2s of idle (all of it from the first
//     piece), not 4s.
//   - Skip pieces add no time and consume no idle budget. Frames stamped
//     inside a Skip segment are dropped before de-duplication. Time those
//     frames would have shown after the skipped range (only possible if no
//     frame exists at the segment's To) is added to the last emitted step.
//     Contract for the frames stage: the screen after a skipped span is only
//     shown if a frame exists at or after To, so the caller must force a
//     frame at every Skip boundary (frames.Options.Boundaries).
//   - Segment Hold is added (not substituted) to the last emitted step when
//     the first frame at or after To arrives, or, for segments reaching past
//     the last frame, at the end. It adds to Options.Hold when the recording
//     ends inside the segment. It lands on the last step shown, so a Hold on
//     a segment that ends inside a trailing Skip lengthens the last visible
//     step. A segment that starts after the last frame adds nothing. Holds
//     before the first emitted step are dropped.
//   - Options.Hold goes to the last emitted step even when the final frame is
//     itself inside a Skip segment. If every frame is skipped the result is
//     empty.
//   - Hold is added before de-duplication and min-delay folding, so a
//     hold-lengthened step is not folded unless it is still below MinDelay.
//
// With no segments, Build behaves exactly as without this feature. The sum of
// the returned delays equals the sum of the per-gap stretch delays plus
// Options.Hold plus the applied segment holds, less anything dropped as
// stated above.
package timeline

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

// Options controls how frame timestamps become step delays.
type Options struct {
	// Speed divides every inter-frame delay. Zero means 1 (real time);
	// negative, NaN and infinite values are errors.
	Speed float64
	// IdleLimit caps each inter-frame delay before Speed is applied.
	// Zero means no cap.
	IdleLimit time.Duration
	// Hold is how long the final frame stays on screen. It is used as given:
	// it is neither capped by IdleLimit nor divided by Speed. Zero is allowed
	// (the caller applies any default).
	Hold time.Duration
	// MinDelay is the shortest delay a step may have; shorter steps are
	// folded into their predecessor (see Build). Zero disables folding.
	MinDelay time.Duration
	// Segments overrides the policy for ranges of the recording clock; see
	// Segment and the "Segments" section of the package comment. It must be
	// sorted and non-overlapping (FlattenSegments builds such a list from
	// nested ranges). Nil or empty means the policy above applies everywhere.
	//
	// Precedence between flags and hints is the caller's job: to make an
	// explicit flag beat a segment hint, clear that field (Speed, IdleLimit
	// or Hold) on the segments, keeping Skip.
	Segments []Segment
}

// Step is a snapshot to display for Delay.
type Step struct {
	Snapshot snapshot.Snapshot
	Delay    time.Duration
}

// Build converts frames into steps. Frame i is shown from frames[i].At until
// frames[i+1].At; the last frame is shown for Options.Hold. Empty input
// yields an empty result and no error.
//
// Rules, applied in this order:
//
//  1. Raw delay is next.At - this.At; timestamps that go backwards count as
//     a delay of 0.
//  2. The raw delay is capped at IdleLimit (if non-zero), then divided by
//     Speed and rounded to the nearest nanosecond. The last frame's delay is
//     Hold, unmodified.
//  3. Consecutive frames with Equal snapshots are merged into one step that
//     keeps the first snapshot and the sum of the delays.
//  4. Min-delay folding: scanning forward, any step whose delay is below
//     MinDelay is dropped and its delay added to the preceding kept step, so
//     that step stays on screen longer and the short intermediate state is
//     not shown. The first and the last step are never dropped, so the
//     start and final screens always appear (and may be shorter than
//     MinDelay).
//
// Dedup and folding only move time between steps, so the sum of the returned
// delays equals the sum of the per-frame delays from rule 2.
func Build(frames []snapshot.Frame, o Options) ([]Step, error) {
	speed := o.Speed
	if speed == 0 {
		speed = 1
	}
	if math.IsNaN(speed) || math.IsInf(speed, 0) || speed < 0 {
		return nil, fmt.Errorf("timeline: invalid speed %v", o.Speed)
	}
	if o.IdleLimit < 0 || o.Hold < 0 || o.MinDelay < 0 {
		return nil, errors.New("timeline: idle limit, hold and min delay must not be negative")
	}
	if err := validateSegments(o.Segments); err != nil {
		return nil, err
	}
	if len(frames) == 0 {
		return nil, nil
	}
	segs := o.Segments

	// Rules 1-3 and the segment rules: delays, dropped frames, holds, dedup.
	steps := make([]Step, 0, len(frames))
	addToLast := func(d time.Duration) {
		if n := len(steps); n > 0 {
			steps[n-1].Delay += d
		}
	}
	var pieces []piece
	nextHold := 0 // first segment whose end has not been reached yet
	for i, f := range frames {
		for nextHold < len(segs) && f.At >= segs[nextHold].To {
			addToLast(segs[nextHold].Hold)
			nextHold++
		}
		var delay time.Duration
		if i == len(frames)-1 {
			delay = o.Hold
		} else if next := frames[i+1].At; next > f.At {
			pieces = gapPieces(pieces[:0], segs, speed, o.IdleLimit, f.At, next)
			scaled := math.Round(stretchDelay(pieces))
			if scaled >= math.MaxInt64 {
				return nil, fmt.Errorf("timeline: delay %v at speed %v overflows", next-f.At, o.Speed)
			}
			delay = time.Duration(scaled)
		}
		if segmentSkips(segs, f.At) {
			addToLast(delay)
			continue
		}
		if n := len(steps); n > 0 && steps[n-1].Snapshot.Equal(f.Snapshot) {
			steps[n-1].Delay += delay
			continue
		}
		steps = append(steps, Step{Snapshot: f.Snapshot, Delay: delay})
	}
	// Segments that end after the last frame but start at or before it.
	lastAt := frames[len(frames)-1].At
	for ; nextHold < len(segs) && segs[nextHold].From <= lastAt; nextHold++ {
		addToLast(segs[nextHold].Hold)
	}
	if len(steps) == 0 {
		return nil, nil
	}

	// Rule 4: fold short steps into the previous kept step.
	if o.MinDelay > 0 {
		kept := steps[:1]
		for i := 1; i < len(steps); i++ {
			s := steps[i]
			if s.Delay < o.MinDelay && i != len(steps)-1 {
				kept[len(kept)-1].Delay += s.Delay
				continue
			}
			kept = append(kept, s)
		}
		steps = kept
	}
	return steps, nil
}

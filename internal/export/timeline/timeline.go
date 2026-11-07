// Package timeline turns timestamped snapshot frames into a sequence of
// steps, each a snapshot plus how long to show it. It applies the idle cap,
// speed, final hold, de-duplication and minimum-delay policy that animated
// exporters share. It is pure: it imports only internal/snapshot and the
// standard library.
package timeline

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
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
	if len(frames) == 0 {
		return nil, nil
	}

	// Rules 1-3: delays and dedup.
	steps := make([]Step, 0, len(frames))
	for i, f := range frames {
		var delay time.Duration
		if i == len(frames)-1 {
			delay = o.Hold
		} else {
			raw := max(frames[i+1].At-f.At, 0)
			if o.IdleLimit > 0 {
				raw = min(raw, o.IdleLimit)
			}
			scaled := math.Round(float64(raw) / speed)
			if scaled >= math.MaxInt64 {
				return nil, fmt.Errorf("timeline: delay %v at speed %v overflows", raw, o.Speed)
			}
			delay = time.Duration(scaled)
		}
		if n := len(steps); n > 0 && steps[n-1].Snapshot.Equal(f.Snapshot) {
			steps[n-1].Delay += delay
			continue
		}
		steps = append(steps, Step{Snapshot: f.Snapshot, Delay: delay})
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

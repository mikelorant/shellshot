package timeline

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Segment overrides the timeline policy for the recording-clock range
// [From, To). Segments let one export give different scenes their own speed,
// idle cap and hold, and cut hidden spans out of the output entirely.
//
// Zero Speed and zero IdleLimit inherit the corresponding Options value
// (Speed 0 there means 1). A non-zero IdleLimit replaces the global one for
// the segment; it is not combined with it (a segment may raise the cap as
// well as lower it).
type Segment struct {
	// From and To bound the segment on the recording clock: From is
	// inclusive, To exclusive. Both are non-negative and From < To.
	From, To time.Duration
	// Speed divides the delays inside the segment. Zero inherits.
	// Negative, NaN and infinite values are errors.
	Speed float64
	// IdleLimit caps the idle stretch inside the segment (see Options.Segments
	// for how caps combine across a boundary). Zero inherits.
	IdleLimit time.Duration
	// Hold is extra time added to the last step shown at or before To. It
	// is neither capped nor divided, and it is added on top of Options.Hold
	// when the segment ends at the recording's last frame. Zero adds nothing.
	Hold time.Duration
	// Skip removes the range from the output: it contributes no time and
	// frames timestamped inside it are not emitted. Hold on a Skip segment
	// still applies (it lands on the last step shown before To).
	Skip bool
}

// validateSegments checks that segs is sorted, non-overlapping (adjacent
// segments are fine) and that every segment is well formed.
func validateSegments(segs []Segment) error {
	for i, s := range segs {
		switch {
		case s.From < 0 || s.To < 0:
			return fmt.Errorf("timeline: segment %d [%v, %v): negative bound", i, s.From, s.To)
		case s.From >= s.To:
			return fmt.Errorf("timeline: segment %d [%v, %v): From must be before To", i, s.From, s.To)
		case math.IsNaN(s.Speed) || math.IsInf(s.Speed, 0) || s.Speed < 0:
			return fmt.Errorf("timeline: segment %d: invalid speed %v", i, s.Speed)
		case s.IdleLimit < 0 || s.Hold < 0:
			return fmt.Errorf("timeline: segment %d: idle limit and hold must not be negative", i)
		}
		if i == 0 {
			continue
		}
		prev := segs[i-1]
		if s.From < prev.From {
			return fmt.Errorf("timeline: segment %d [%v, %v) is not sorted: it starts before segment %d", i, s.From, s.To, i-1)
		}
		if s.From < prev.To {
			return fmt.Errorf("timeline: segment %d [%v, %v) overlaps segment %d [%v, %v)", i, s.From, s.To, i-1, prev.From, prev.To)
		}
	}
	return nil
}

// segmentIndex returns the index of the segment containing t, or -1.
func segmentIndex(segs []Segment, t time.Duration) int {
	i := sort.Search(len(segs), func(k int) bool { return segs[k].To > t })
	if i < len(segs) && segs[i].From <= t {
		return i
	}
	return -1
}

// piece is the part of an inter-frame gap that falls in one segment (or in
// no segment, which uses the global policy).
type piece struct {
	raw   time.Duration
	speed float64
	limit time.Duration // 0 = no cap
	skip  bool
}

// gapPieces splits the gap [a, b), a < b, at segment boundaries, in time
// order, appending to dst. globalSpeed must already be resolved (non-zero).
func gapPieces(dst []piece, segs []Segment, globalSpeed float64, globalLimit, a, b time.Duration) []piece {
	i := sort.Search(len(segs), func(k int) bool { return segs[k].To > a })
	for cur := a; cur < b; {
		if i < len(segs) && segs[i].From <= cur {
			s := segs[i]
			i++
			end := min(s.To, b)
			p := piece{raw: end - cur, speed: globalSpeed, limit: globalLimit, skip: s.Skip}
			if s.Speed != 0 {
				p.speed = s.Speed
			}
			if s.IdleLimit != 0 {
				p.limit = s.IdleLimit
			}
			dst = append(dst, p)
			cur = end
			continue
		}
		end := b
		if i < len(segs) {
			end = min(b, segs[i].From)
		}
		dst = append(dst, piece{raw: end - cur, speed: globalSpeed, limit: globalLimit})
		cur = end
	}
	return dst
}

// stretchDelay turns pieces into one scaled delay, in nanoseconds before
// rounding. The idle cap applies to the whole stretch: the budget is the
// smallest non-zero limit among the non-skipped pieces, spent in time order,
// so a pause that crosses a boundary is capped once, not once per piece.
// Skipped pieces neither cost budget nor add time.
func stretchDelay(pieces []piece) float64 {
	var limit time.Duration // 0 = unlimited
	for _, p := range pieces {
		if !p.skip && p.limit > 0 && (limit == 0 || p.limit < limit) {
			limit = p.limit
		}
	}
	remaining := limit
	var total float64
	for _, p := range pieces {
		if p.skip {
			continue
		}
		take := p.raw
		if limit > 0 {
			take = min(take, remaining)
			remaining -= take
		}
		total += float64(take) / p.speed
	}
	return total
}

// segmentSkips reports whether t lies inside a Skip segment.
func segmentSkips(segs []Segment, t time.Duration) bool {
	i := segmentIndex(segs, t)
	return i >= 0 && segs[i].Skip
}

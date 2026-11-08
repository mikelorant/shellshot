package replay

import (
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// countIdleCapped returns the number of pauses between consecutive frames
// that an idle limit cuts. The limit is limit, or the IdleLimit of the segment
// the pause starts in. Time inside a Skip segment is not part of a pause, as it
// is not played. Without segments this counts gaps longer than limit.
func countIdleCapped(frames []snapshot.Frame, segs []timeline.Segment, limit time.Duration) int {
	n := 0
	for i := 1; i < len(frames); i++ {
		from, to := frames[i-1].At, frames[i].At
		pause := to - from
		lim := limit
		for _, s := range segs {
			if s.Skip {
				pause -= max(0, min(s.To, to)-max(s.From, from))
			} else if s.IdleLimit > 0 && s.From <= from && from < s.To {
				lim = s.IdleLimit
			}
		}
		if lim > 0 && pause > lim {
			n++
		}
	}
	return n
}

// hiddenCut returns how many Skip segments start by recorded, and how much of
// the recording clock up to recorded they cover.
func hiddenCut(segs []timeline.Segment, recorded time.Duration) (count int, total time.Duration) {
	for _, s := range segs {
		if s.Skip && s.From < recorded {
			count++
			total += min(s.To, recorded) - s.From
		}
	}
	return count, total
}

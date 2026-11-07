// Package spanplan turns the scenes and hidden spans of a recording (see
// package spans) into what an export needs: timeline segments that apply the
// scene hints and cut out hidden spans, and the recording times at which the
// frames stage must emit a forced frame.
//
// It is pure: it imports only the span, event and timeline packages.
//
// A recording without spans yields an empty Plan, which leaves the export
// exactly as it would be without this package.
package spanplan

import (
	"slices"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/spans"
)

// Options says which parts of the spans to leave out of the plan.
type Options struct {
	// IgnoreHints drops every scene hint (--no-hints). Hidden spans are still
	// cut out.
	IgnoreHints bool
	// ShowHidden renders hidden spans like any other time (--show-hidden).
	ShowHidden bool
	// FlagSpeed, FlagIdleLimit and FlagHold report that the matching export
	// flag was given explicitly. It then applies to the whole recording and
	// beats the hint of the same name, so that hint is dropped from the
	// segments.
	FlagSpeed, FlagIdleLimit, FlagHold bool
}

// Plan is the export view of a recording's spans.
type Plan struct {
	// Segments is sorted and non-overlapping, for timeline.Options.Segments.
	Segments []timeline.Segment
	// Boundaries is the sorted, de-duplicated begin and end time of every span
	// that has a segment, for frames.Options.Boundaries. The screen at those
	// times is forced into the frame stream so the first frame after a hidden
	// span shows the post-span screen and a scene hold lands on the scene's
	// last screen.
	Boundaries []time.Duration
}

// Build plans found, the spans of one recording in begin order.
//
// A scene becomes a segment carrying its speed, idle and hold hints (speed
// hints are milli-x). A hidden span becomes a Skip segment; it carries no
// hints. A span that would change nothing (a scene without usable hints, a
// hidden span under ShowHidden, an empty span) contributes neither a segment
// nor boundaries. Nested spans are flattened with timeline.FlattenSegments.
func Build(found []spans.Span, o Options) (Plan, error) {
	var (
		ranges     []timeline.Segment
		boundaries []time.Duration
	)
	for _, s := range found {
		if s.End <= s.Start {
			continue
		}
		seg := timeline.Segment{From: s.Start, To: s.End}
		switch s.Type {
		case event.SpanHidden:
			if o.ShowHidden {
				continue
			}
			seg.Skip = true
		case event.SpanScene:
			if o.IgnoreHints {
				continue
			}
			if !o.FlagSpeed && s.Hints.Speed > 0 {
				seg.Speed = float64(s.Hints.Speed) / 1000
			}
			if !o.FlagIdleLimit {
				seg.IdleLimit = s.Hints.Idle
			}
			if !o.FlagHold {
				seg.Hold = s.Hints.Hold
			}
			if seg.Speed == 0 && seg.IdleLimit == 0 && seg.Hold == 0 {
				continue
			}
		default:
			continue
		}
		ranges = append(ranges, seg)
		boundaries = append(boundaries, s.Start, s.End)
	}
	segments, err := timeline.FlattenSegments(ranges)
	if err != nil {
		return Plan{}, err
	}
	return Plan{Segments: segments, Boundaries: sortedUnique(boundaries)}, nil
}

func sortedUnique(ts []time.Duration) []time.Duration {
	if len(ts) == 0 {
		return nil
	}
	slices.Sort(ts)
	return slices.Compact(ts)
}

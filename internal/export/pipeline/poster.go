package pipeline

import (
	"fmt"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
)

// PosterKind says which step is a poster.
type PosterKind uint8

const (
	// PosterNone writes no poster. The zero value.
	PosterNone PosterKind = iota
	// PosterFirst selects the first step.
	PosterFirst
	// PosterLast selects the last step.
	PosterLast
	// PosterAt selects the step on screen at Poster.At.
	PosterAt
)

// Poster selects the step that Export renders as the sink's still image. The
// zero value selects none.
type Poster struct {
	Kind PosterKind
	// At is the time on the output clock for PosterAt: the sum of the step
	// delays, after speed, idle limit, hold and the leading blank trim, like
	// the time of svg's --frame.
	At time.Duration
}

// ParsePoster parses the --poster values "none", "first", "last" and a
// duration such as "12s" or "800ms".
func ParsePoster(s string) (Poster, error) {
	switch s {
	case "none":
		return Poster{}, nil
	case "first":
		return Poster{Kind: PosterFirst}, nil
	case "last":
		return Poster{Kind: PosterLast}, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return Poster{}, fmt.Errorf("invalid poster %q (want none, first, last or a time such as 12s)", s)
	}
	return Poster{Kind: PosterAt, At: d}, nil
}

// Step returns the index of the step p selects in the non-empty steps: the
// first, the last, or the step on screen at p.At (the last step if p.At is
// past the end). PosterNone selects the last step.
func (p Poster) Step(steps []timeline.Step) int {
	switch p.Kind {
	case PosterFirst:
		return 0
	case PosterAt:
		var end time.Duration
		for i, st := range steps {
			if end += st.Delay; p.At < end {
				return i
			}
		}
	}
	return len(steps) - 1
}

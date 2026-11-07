// Package spans lists the scenes and hidden stretches of a recording from its
// SPAN events (event.KindSpan).
package spans

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
)

// Span is one scene or hidden stretch.
type Span struct {
	ID     uint64
	Type   event.SpanType
	Name   string
	Parent uint64
	// Depth is 0 for a top-level span and 1 for a span inside a scene.
	Depth int
	Hints event.SpanHints
	// Start and End are times since the recording start. End is the time of the
	// last event of the recording when Open is true.
	Start, End time.Duration
	// Open is true when the span has no end event: the recorder was killed or
	// the file is torn.
	Open bool
}

// Duration returns End - Start.
func (s Span) Duration() time.Duration { return s.End - s.Start }

// Source is the part of *bin.Reader that Scan uses.
type Source interface {
	Next() (bin.Event, error)
}

// Scan reads src to its end and returns the spans in begin order.
//
// It applies the rules of docs/research/script-scenes.md §4.4. These SPAN
// events are ignored, never an error: an unknown version, op or type; a
// begin with an id seen before; an end for an id that is not open; a scene
// begin while a scene or hidden span is open or with a parent; a hidden begin
// while a hidden span is open or whose parent is not the open scene; and the
// reserved beat type. Ending a scene also ends a hidden span still open inside
// it, at the same time. A span without an end is returned with Open set and
// ends at the time of the last event read.
//
// Scan returns the spans found so far together with any error from src other
// than io.EOF (for example bin.ErrTruncated for a torn tail, which is normal
// for a killed recorder), or an error wrapping event.ErrMalformedSpan for a
// SPAN payload that is truncated or out of range.
func Scan(src Source) ([]Span, error) {
	var (
		out  []Span
		seen = map[uint64]int{} // id -> index in out
		last time.Duration
		rerr error
	)
	open := func(id uint64) (int, bool) {
		i, ok := seen[id]
		return i, ok && out[i].Open
	}
	var scene, hidden = -1, -1 // indices of the open scene and hidden span
	closeAt := func(i int, at time.Duration) {
		out[i].End, out[i].Open = at, false
		switch i {
		case scene:
			scene = -1
		case hidden:
			hidden = -1
		}
	}

	for {
		e, err := src.Next()
		if err != nil {
			if err != io.EOF {
				rerr = err
			}
			break
		}
		last = e.At
		if e.Kind != event.KindSpan {
			continue
		}
		s, err := event.ParseSpan(e.Data)
		if errors.Is(err, event.ErrUnknownSpan) {
			continue
		}
		if err != nil {
			rerr = fmt.Errorf("span event at %v: %w", e.At, err)
			break
		}

		if s.Op == event.SpanEnd {
			i, ok := open(s.ID)
			if !ok {
				continue
			}
			if i == scene && hidden >= 0 {
				closeAt(hidden, e.At)
			}
			closeAt(i, e.At)
			continue
		}

		if _, dup := seen[s.ID]; dup {
			continue
		}
		depth := 0
		switch s.Type {
		case event.SpanScene:
			if s.Parent != 0 || scene >= 0 || hidden >= 0 {
				continue
			}
		case event.SpanHidden:
			if hidden >= 0 {
				continue
			}
			if s.Parent != 0 {
				if scene < 0 || out[scene].ID != s.Parent {
					continue
				}
				depth = 1
			}
		default:
			continue
		}
		out = append(out, Span{
			ID: s.ID, Type: s.Type, Name: s.Name, Parent: s.Parent,
			Depth: depth, Hints: s.Hints, Start: e.At, Open: true,
		})
		i := len(out) - 1
		seen[s.ID] = i
		if s.Type == event.SpanScene {
			scene = i
		} else {
			hidden = i
		}
	}

	for i := range out {
		if out[i].Open {
			out[i].End = last
		}
	}
	return out, rerr
}

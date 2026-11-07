// Package frames replays a binary recording's event log through a headless
// terminal and yields the screen as snapshot frames.
//
// The package is pure Go: it depends on the vt interface (not on a concrete
// engine) and on a Source defined here, which *bin.Reader satisfies. Callers
// inject the engine through Options.Factory.
//
// # Sampling
//
// Frames are taken on a fixed grid whose period is one second divided by
// Options.FPS (60 frames per second by default, a period of about 16.667ms). The first frame is the
// blank screen at t=0. Events are grouped by the grid boundary at or after their
// timestamp (an event at exactly a boundary belongs to that boundary). When a
// group is complete the terminal is snapshotted, and the frame is emitted,
// stamped with the boundary, only if the screen differs from the last emitted
// frame. Boundaries with no events are never visited: a five minute idle gap
// costs one step, not three thousand.
//
// Unless Options.OmitFinal is set, the last group of events is instead emitted
// once, always, stamped with the time of the last event, so a recording ends
// with the terminal's final state (this mirrors session.Run).
//
// # Boundaries
//
// Options.Boundaries lists recording times at which a frame is forced, for
// example the begin and end of a hidden span, so the screen state at exactly
// that time reaches the consumer even when nothing was written near it. For
// each boundary t, in ascending order, the iterator emits one frame stamped
// exactly t whose snapshot has every event with a timestamp at or before t
// applied and none after. The rules are:
//
//   - The frame is forced: it is emitted even when its screen equals the
//     previously emitted one. (The grid rule above still decides the other
//     frames, which are compared against the latest emitted frame, forced or
//     not.)
//   - A boundary on a grid tick that closes a group of events is that group's
//     frame: one frame is emitted, stamped at the tick, and forced. A boundary
//     inside a group splits it: the forced frame comes first and the group's
//     own grid frame follows if the screen changed again.
//   - Frames never go backwards in time; forced frames interleave with grid
//     frames in timestamp order.
//   - Work stays proportional to the number of events and boundaries. A
//     forced frame on a clean terminal reuses the last snapshot and calls the
//     terminal not at all, so a five minute idle gap with three boundaries
//     costs three frames and no extra Snapshot calls.
//   - A boundary at 0 is satisfied by the initial frame, unless an event at
//     time 0 changed the screen, in which case a second frame at 0 follows.
//   - A boundary before the first event shows the screen as it was
//     (blank). A boundary at or after the last event is emitted after the
//     final frame (which is stamped at the last event, see above) and shows
//     the final screen; a boundary exactly at the last event is the final
//     frame itself. With OmitFinal, boundaries after the last event are still
//     emitted, interleaved with the last group's grid frame in time order.
//
// # Size changes
//
// RESIZE events resize the terminal, so the Snapshot inside a Frame carries its
// own Cols and Rows and they may differ from frame to frame. Consumers (the
// pipeline and exporters) must not assume the size of the first frame holds for
// the whole stream. Frames are only emitted at sample boundaries, never in the
// middle of a group, so a resize and the output that follows it within one
// Interval appear together.
//
// # Ownership
//
// An Iterator owns its terminal and is not safe for concurrent use.
package frames

import (
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/snapshot"
	"github.com/mikelorant/shellshot/internal/vt"
)

// DefaultFPS is the sampling rate used when Options.FPS is not positive. It
// matches the live recorder's default.
const DefaultFPS = 60

// ErrClosed is returned by Next after Close.
var ErrClosed = errors.New("frames: iterator closed")

// Source supplies the recording head and its events in order. *bin.Reader
// satisfies it. Next returns io.EOF at the end; Event.Data need only stay valid
// until the following Next call.
type Source interface {
	Head() head.Head
	Next() (bin.Event, error)
}

// Options configures an Iterator.
type Options struct {
	// FPS is the sampling rate in frames per second; the grid period is one
	// second divided by FPS, rounded to the nearest microsecond (and never
	// below one microsecond). Zero or negative selects DefaultFPS.
	FPS int
	// Factory creates the terminal. It is required.
	Factory vt.Factory
	// OmitFinal disables the always-emitted final frame; the last group is then
	// sampled like any other (emitted only if the screen changed, stamped with
	// its grid boundary). The zero value keeps the final frame.
	OmitFinal bool
	// Boundaries are recording times at which a frame is forced; see the
	// package documentation. They may be unsorted and contain duplicates; the
	// iterator sorts and deduplicates a copy. A negative time is an error from
	// New. Empty leaves the frame stream unchanged.
	Boundaries []time.Duration
}

// Mark is a MARK event: a labelled point in the recording.
type Mark struct {
	At    time.Duration
	Label string
}

// Iterator turns an event Source into snapshot frames. Create one with New.
type Iterator struct {
	src      Source
	term     vt.Terminal
	interval time.Duration
	omitFin  bool

	bounds  []time.Duration // sorted, deduplicated forced-frame times
	nextB   int             // index of the first boundary not yet handled
	finalOK bool            // the final frame has been emitted (or is omitted)
	eof     bool            // the source is exhausted; Next is draining finish

	first bool              // the t=0 frame has not been returned yet
	prev  snapshot.Snapshot // last emitted snapshot; equals the screen unless dirty
	dirty bool              // the terminal changed since prev was taken

	open   bool          // a group of events is being accumulated
	groupB time.Duration // grid boundary of the open group
	lastB  time.Duration // boundary of the last sampled group
	lastAt time.Duration // timestamp of the last applied event (monotonic)

	hasPend bool
	pend    bin.Event // deferred event; Data aliases pendBuf
	pendBuf []byte

	marks []Mark
	err   error // sticky; io.EOF once finished
}

// New creates the terminal at the head's size and returns an Iterator
// positioned before the first frame.
func New(src Source, o Options) (*Iterator, error) {
	if o.Factory == nil {
		return nil, errors.New("frames: Options.Factory is required")
	}
	bounds := slices.Clone(o.Boundaries)
	for _, b := range bounds {
		if b < 0 {
			return nil, fmt.Errorf("frames: negative boundary %v", b)
		}
	}
	slices.Sort(bounds)
	bounds = slices.Compact(bounds)
	h := src.Head()
	term, err := o.Factory(h.Cols, h.Rows)
	if err != nil {
		return nil, fmt.Errorf("frames: create terminal: %w", err)
	}
	snap, err := term.Snapshot()
	if err != nil {
		_ = term.Close()
		return nil, fmt.Errorf("frames: initial snapshot: %w", err)
	}
	it := &Iterator{
		src:      src,
		term:     term,
		interval: period(o.FPS),
		omitFin:  o.OmitFinal,
		bounds:   bounds,
		first:    true,
		prev:     snap,
	}
	return it, nil
}

// period returns the grid period for fps, which is DefaultFPS when not
// positive. It rounds like session.Config.Period.
func period(fps int) time.Duration {
	if fps <= 0 {
		fps = DefaultFPS
	}
	return max(time.Duration(float64(time.Second)/float64(fps)).Round(time.Microsecond), time.Microsecond)
}

// Marks returns the MARK events read so far, in order. It grows as Next
// consumes the log; after Next returns io.EOF it holds every mark.
func (it *Iterator) Marks() []Mark { return slices.Clone(it.marks) }

// Next returns the next frame, or io.EOF after the last one. Any other error is
// sticky. The terminal is closed when Next returns io.EOF or an error.
func (it *Iterator) Next() (snapshot.Frame, error) {
	if it.err != nil {
		return snapshot.Frame{}, it.err
	}
	if it.first {
		it.first = false
		return snapshot.Frame{At: 0, Snapshot: it.prev}, nil
	}
	if it.eof {
		return it.finish()
	}
	for {
		ev, err := it.read()
		if err == io.EOF {
			it.eof = true
			return it.finish()
		}
		if err != nil {
			return snapshot.Frame{}, it.fail(err)
		}
		at := max(ev.At, it.lastAt)
		b := it.boundary(at)
		closeGroup := it.open && b != it.groupB
		// Events at or before a boundary are applied before its frame, so a
		// boundary is only due once an event strictly after it arrives.
		bt, due := it.pendingBoundary()
		due = due && bt < at
		if closeGroup || due {
			it.deferEvent(ev, at)
			var (
				f    snapshot.Frame
				emit bool
			)
			switch {
			case closeGroup && due && bt == it.groupB:
				it.nextB++
				f, emit, err = it.sample(it.groupB, true)
			case due && (!closeGroup || bt < it.groupB):
				f, emit, err = it.sampleBoundary(bt)
			default:
				f, emit, err = it.sample(it.groupB, false)
			}
			if err != nil {
				return snapshot.Frame{}, it.fail(err)
			}
			if emit {
				return f, nil
			}
			continue
		}
		if !it.open {
			it.open, it.groupB = true, b
		}
		it.lastAt = at
		if err := it.apply(ev, at); err != nil {
			return snapshot.Frame{}, it.fail(err)
		}
	}
}

// Close releases the terminal. It is idempotent and safe after io.EOF.
func (it *Iterator) Close() error {
	if it.err == nil {
		it.err = ErrClosed
	}
	return it.closeTerm()
}

// Collect drains it into a slice and closes it.
func Collect(it *Iterator) ([]snapshot.Frame, error) {
	defer it.Close()
	var out []snapshot.Frame
	for {
		f, err := it.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return out, err
		}
		out = append(out, f)
	}
}

// read returns the deferred event if there is one, else the next source event.
func (it *Iterator) read() (bin.Event, error) {
	if it.hasPend {
		it.hasPend = false
		return it.pend, nil
	}
	return it.src.Next()
}

// deferEvent keeps ev (with its clamped time) for the next call to read, copying
// Data because the source may reuse it.
func (it *Iterator) deferEvent(ev bin.Event, at time.Duration) {
	it.pendBuf = append(it.pendBuf[:0], ev.Data...)
	ev.At, ev.Data = at, it.pendBuf
	it.pend, it.hasPend = ev, true
}

// boundary returns the grid boundary the event at time at is sampled on: the
// first multiple of the interval at or after at, and always after the last
// sampled boundary.
func (it *Iterator) boundary(at time.Duration) time.Duration {
	b := (at + it.interval - 1) / it.interval * it.interval
	if b < at { // overflow near MaxInt64
		b = at
	}
	return max(b, it.lastB+it.interval)
}

// apply feeds one event to the terminal.
func (it *Iterator) apply(ev bin.Event, at time.Duration) error {
	switch ev.Kind {
	case event.KindOut:
		if len(ev.Data) == 0 {
			return nil
		}
		if _, err := it.term.Write(ev.Data); err != nil {
			return fmt.Errorf("frames: write at %v: %w", at, err)
		}
		it.dirty = true
	case event.KindResize:
		p, err := event.ParseResize(ev.Data)
		if err != nil {
			return fmt.Errorf("frames: resize at %v: %w", at, err)
		}
		if p.Cols == 0 || p.Rows == 0 || p.Cols > math.MaxInt32 || p.Rows > math.MaxInt32 {
			return fmt.Errorf("frames: resize at %v: invalid size %dx%d", at, p.Cols, p.Rows)
		}
		if err := it.term.Resize(int(p.Cols), int(p.Rows)); err != nil {
			return fmt.Errorf("frames: resize at %v: %w", at, err)
		}
		it.dirty = true
	case event.KindMark:
		it.marks = append(it.marks, Mark{At: at, Label: string(ev.Data)})
	}
	// KindIn, KindExit and ancillary kinds do not affect rendering.
	return nil
}

// sample closes the open group. It snapshots the terminal if the group changed
// it, and reports whether a frame should be emitted: when the screen differs
// from the last emitted one, or always when force is set.
func (it *Iterator) sample(at time.Duration, force bool) (snapshot.Frame, bool, error) {
	it.open = false
	it.lastB = it.groupB
	snap, changed, err := it.refresh(at)
	if err != nil {
		return snapshot.Frame{}, false, err
	}
	if !changed && !force {
		return snapshot.Frame{}, false, nil
	}
	it.prev = snap
	return snapshot.Frame{At: at, Snapshot: snap}, true, nil
}

// refresh returns the current screen, taking a new snapshot only if the
// terminal changed since the last one, and reports whether it differs from the
// last emitted snapshot.
func (it *Iterator) refresh(at time.Duration) (snapshot.Snapshot, bool, error) {
	if !it.dirty {
		return it.prev, false, nil
	}
	snap, err := it.term.Snapshot()
	if err != nil {
		return snapshot.Snapshot{}, false, fmt.Errorf("frames: snapshot at %v: %w", at, err)
	}
	it.dirty = false
	return snap, !snap.Equal(it.prev), nil
}

// pendingBoundary returns the earliest boundary not yet handled.
func (it *Iterator) pendingBoundary() (time.Duration, bool) {
	if it.nextB >= len(it.bounds) {
		return 0, false
	}
	return it.bounds[it.nextB], true
}

// sampleBoundary handles the pending boundary at, which must be pending: it
// emits a forced frame stamped at with the current screen, without closing the
// open group. The initial frame already stands for a boundary at 0 while no
// event has touched the terminal, so none is emitted then.
func (it *Iterator) sampleBoundary(at time.Duration) (snapshot.Frame, bool, error) {
	it.nextB++
	if at == 0 && !it.dirty {
		return snapshot.Frame{}, false, nil
	}
	snap, _, err := it.refresh(at)
	if err != nil {
		return snapshot.Frame{}, false, err
	}
	it.prev = snap
	return snapshot.Frame{At: at, Snapshot: snap}, true, nil
}

// finish emits what remains once the log has ended, one frame per call: the
// final frame (unless OmitFinal), the last group (with OmitFinal) and the
// boundaries not yet reached, in time order. When nothing is left it closes the
// terminal and the call returns io.EOF.
func (it *Iterator) finish() (snapshot.Frame, error) {
	for {
		var (
			f    snapshot.Frame
			emit bool
			err  error
		)
		bt, has := it.pendingBoundary()
		switch {
		case !it.omitFin && !it.finalOK:
			it.finalOK = true
			if it.open {
				f, emit, err = it.sample(it.lastAt, true)
			} else {
				f, emit = snapshot.Frame{At: it.lastAt, Snapshot: it.prev}, true
			}
			if has && bt == it.lastAt { // the final frame is this boundary's frame
				it.nextB++
			}
		case it.omitFin && it.open && has && bt == it.groupB:
			it.nextB++
			f, emit, err = it.sample(it.groupB, true)
		case it.omitFin && it.open && (!has || it.groupB < bt):
			f, emit, err = it.sample(it.groupB, false)
		case has:
			f, emit, err = it.sampleBoundary(bt)
		default:
			emit = false
		}
		if err != nil {
			return snapshot.Frame{}, it.fail(err)
		}
		if it.nextB >= len(it.bounds) && (it.omitFin && !it.open || !it.omitFin && it.finalOK) {
			// Nothing remains: the following Next returns io.EOF.
			it.err = io.EOF
			if cerr := it.closeTerm(); cerr != nil {
				it.err = cerr
			}
			if !emit {
				return snapshot.Frame{}, it.err
			}
		}
		if emit {
			return f, nil
		}
	}
}

// fail records err as the sticky error and closes the terminal.
func (it *Iterator) fail(err error) error {
	it.err = err
	_ = it.closeTerm()
	return err
}

func (it *Iterator) closeTerm() error {
	if it.term == nil {
		return nil
	}
	t := it.term
	it.term = nil
	return t.Close()
}

// Package frames replays a binary recording's event log through a headless
// terminal and yields the screen as snapshot frames.
//
// The package is pure Go: it depends on the vt interface (not on a concrete
// engine) and on a Source defined here, which *bin.Reader satisfies. Callers
// inject the engine through Options.Factory.
//
// # Sampling
//
// Frames are taken on a fixed grid of Options.Interval. The first frame is the
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

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
	"github.com/michaellorant/shellshot/internal/snapshot"
	"github.com/michaellorant/shellshot/internal/vt"
)

// DefaultInterval is the sampling grid used when Options.Interval is not
// positive.
const DefaultInterval = 100 * time.Millisecond

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
	// Interval is the sampling grid. Zero or negative selects DefaultInterval.
	Interval time.Duration
	// Factory creates the terminal. It is required.
	Factory vt.Factory
	// OmitFinal disables the always-emitted final frame; the last group is then
	// sampled like any other (emitted only if the screen changed, stamped with
	// its grid boundary). The zero value keeps the final frame.
	OmitFinal bool
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
		interval: o.Interval,
		omitFin:  o.OmitFinal,
		first:    true,
		prev:     snap,
	}
	if it.interval <= 0 {
		it.interval = DefaultInterval
	}
	return it, nil
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
	for {
		ev, err := it.read()
		if err == io.EOF {
			return it.finish()
		}
		if err != nil {
			return snapshot.Frame{}, it.fail(err)
		}
		at := max(ev.At, it.lastAt)
		b := it.boundary(at)
		if it.open && b != it.groupB {
			it.deferEvent(ev, at)
			f, emit, err := it.sample(it.groupB, false)
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
	changed := false
	snap := it.prev
	if it.dirty {
		var err error
		if snap, err = it.term.Snapshot(); err != nil {
			return snapshot.Frame{}, false, fmt.Errorf("frames: snapshot at %v: %w", at, err)
		}
		it.dirty = false
		changed = !snap.Equal(it.prev)
	}
	if !changed && !force {
		return snapshot.Frame{}, false, nil
	}
	it.prev = snap
	return snapshot.Frame{At: at, Snapshot: snap}, true, nil
}

// finish handles the end of the log: it emits the last group (or just the
// final frame), then closes the terminal. The following Next returns io.EOF.
func (it *Iterator) finish() (snapshot.Frame, error) {
	var (
		f    snapshot.Frame
		emit bool
		err  error
	)
	switch {
	case it.open && it.omitFin:
		f, emit, err = it.sample(it.groupB, false)
	case it.open:
		f, emit, err = it.sample(it.lastAt, true)
	case !it.omitFin:
		f, emit = snapshot.Frame{At: it.lastAt, Snapshot: it.prev}, true
	}
	if err != nil {
		return snapshot.Frame{}, it.fail(err)
	}
	it.err = io.EOF
	if cerr := it.closeTerm(); cerr != nil {
		it.err = cerr
	}
	if !emit {
		return snapshot.Frame{}, it.err
	}
	return f, nil
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

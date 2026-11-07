package bin

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin/chunk"
	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

// Writer defaults.
const (
	DefaultFlushInterval = 2 * time.Second
	DefaultFlushBytes    = 64 << 10
)

// perEventCost is charged against FlushBytes on top of the payload length, so
// a flood of empty events still flushes. It is the smallest body footprint of
// one event.
const perEventCost = 3

// WriterOptions configures a Writer. The zero value writes zstd blocks with the
// default flush policy.
type WriterOptions struct {
	// Codec is the block codec. The zero value selects codec.Zstd; set
	// Uncompressed to store blocks with codec.None instead (the zero ID is
	// codec.None, so it cannot select itself). NewWriter fails if the codec is
	// not in Registry.
	Codec codec.ID
	// Uncompressed stores blocks with codec.None and overrides Codec.
	Uncompressed bool
	// Registry resolves Codec. Nil means codec.DefaultRegistry().
	Registry *codec.Registry

	// Dict is an optional preset dictionary. When set, NewWriter writes a DICT
	// chunk after HEAD, sets head.FeatureBlockDictionaries in the HEAD required
	// features, and every block references the dictionary.
	Dict []byte
	// DictID names Dict in the file. Zero means 1. Ignored without Dict.
	DictID uint64

	// Theme, if non-nil, is written as a them chunk after HEAD.
	Theme *head.Theme
	// Meta, if non-empty, is written as a meta chunk after HEAD.
	Meta map[string]string

	// FlushInterval is the longest a pending event waits before its block is
	// flushed, measured from the first pending event. Zero means
	// DefaultFlushInterval; negative disables time-based flushing.
	FlushInterval time.Duration
	// FlushBytes is the raw size at which a block is flushed: payload bytes plus
	// 3 per event. Zero means DefaultFlushBytes; negative disables size-based
	// flushing. Values above MaxFlushBytes are rejected.
	FlushBytes int
	// CoalesceTicks, when nonzero, merges adjacent OUT events whose ticks are
	// within this many ticks of the first of the run (see event.Coalescer) and
	// sets head.FlagCoalesced. Zero disables coalescing.
	CoalesceTicks uint64

	// SyncOnFlush calls Sync after every chunk write if the underlying writer
	// has a Sync() error method (such as *os.File).
	SyncOnFlush bool

	// Now is the clock used for the flush interval. Nil means time.Now. Only
	// elapsed time between two readings is used, so a monotonic reading
	// suffices. Event timestamps never come from this clock: callers pass them
	// to WriteEvent.
	Now func() time.Time
}

// Stats counts what a Writer has done.
type Stats struct {
	// Blocks is the number of EVNT chunks written.
	Blocks uint64
	// Events is the number of events in those blocks (after coalescing).
	Events uint64
	// RawBytes is the total decompressed body size of the written blocks.
	RawBytes uint64
	// CompressedBytes is the total compressed body size of the written blocks.
	CompressedBytes uint64
	// BytesWritten is the total number of bytes handed to the underlying
	// writer, including the magic and all chunk framing.
	BytesWritten uint64
	// ClampedTimestamps counts events whose time went backwards (or was
	// negative) and was clamped to the previous tick.
	ClampedTimestamps uint64
}

// Writer writes a recording to an io.Writer.
//
// A Writer starts no goroutines and reads the clock only when called. The
// owner drives time: call WriteEvent as events arrive and Tick from a timer
// (at least once per FlushInterval while events may be pending) so that a quiet
// session still reaches the disk. Tick writes nothing when no event is
// pending, so idle time costs zero bytes.
//
// A Writer is not safe for concurrent use: calls from different goroutines must
// be serialised by the owner. After the underlying writer fails, every later
// call returns that first error.
type Writer struct {
	w      io.Writer
	syncer interface{ Sync() error }
	sync   bool
	now    func() time.Time

	codec  codec.Codec
	dict   []byte
	dictID uint64
	enc    event.Encoder

	tickNS        uint64
	flushInterval time.Duration
	flushBytes    int
	allowInput    bool
	co            *event.Coalescer

	lastTick uint64
	evs      []event.Event
	arena    []byte
	cost     int       // raw size charged for pending events
	pending  bool      // any event accepted since the last flush
	since    time.Time // clock reading when the first pending event arrived

	stats  Stats
	err    error
	closed bool
}

// NewWriter writes the magic, the HEAD chunk and the optional DICT, them and
// meta chunks to w in one Write call (the single initial flush point), and
// returns a Writer for the events. If h.TickNS is zero head.DefaultTickNS is
// used. NewWriter sets head.FlagCoalesced and head.FeatureBlockDictionaries in
// the written HEAD when the options need them.
func NewWriter(w io.Writer, h head.Head, o WriterOptions) (*Writer, error) {
	reg := o.Registry
	if reg == nil {
		reg = codec.DefaultRegistry()
	}
	id := o.Codec
	switch {
	case o.Uncompressed:
		id = codec.None
	case id == codec.None:
		id = codec.Zstd
	}
	c, err := reg.Get(id)
	if err != nil {
		return nil, fmt.Errorf("bin: writer codec: %w", err)
	}
	if o.FlushBytes > MaxFlushBytes {
		return nil, fmt.Errorf("bin: FlushBytes %d exceeds %d", o.FlushBytes, MaxFlushBytes)
	}

	wr := &Writer{
		w:             w,
		sync:          o.SyncOnFlush,
		now:           o.Now,
		codec:         c,
		tickNS:        h.TickNS,
		flushInterval: o.FlushInterval,
		flushBytes:    o.FlushBytes,
		allowInput:    h.Flags&head.FlagInputRecorded != 0,
	}
	if wr.now == nil {
		wr.now = time.Now
	}
	if wr.tickNS == 0 {
		wr.tickNS = head.DefaultTickNS
	}
	if wr.flushInterval == 0 {
		wr.flushInterval = DefaultFlushInterval
	}
	if wr.flushBytes == 0 {
		wr.flushBytes = DefaultFlushBytes
	}
	if s, ok := w.(interface{ Sync() error }); ok {
		wr.syncer = s
	}
	if o.CoalesceTicks > 0 {
		wr.co = event.NewCoalescer(o.CoalesceTicks, 0)
		h.Flags |= head.FlagCoalesced
	}
	if len(o.Dict) > 0 {
		wr.dict = bytes.Clone(o.Dict)
		wr.dictID = o.DictID
		if wr.dictID == 0 {
			wr.dictID = 1
		}
		h.RequiredFeatures |= head.FeatureBlockDictionaries
	}

	var buf bytes.Buffer
	buf.WriteString(chunk.Magic)
	headPayload, err := h.Marshal()
	if err != nil {
		return nil, fmt.Errorf("bin: HEAD: %w", err)
	}
	if err := chunk.Write(&buf, chunk.TypeHead, headPayload); err != nil {
		return nil, err
	}
	if wr.dict != nil {
		p := appendDictPayload(nil, wr.dictID, id, wr.dict)
		if err := chunk.Write(&buf, chunk.TypeDict, p); err != nil {
			return nil, err
		}
	}
	if o.Theme != nil {
		if err := chunk.Write(&buf, chunk.TypeTheme, head.MarshalTheme(*o.Theme)); err != nil {
			return nil, err
		}
	}
	if len(o.Meta) > 0 {
		p, err := head.MarshalMeta(o.Meta)
		if err != nil {
			return nil, fmt.Errorf("bin: meta: %w", err)
		}
		if err := chunk.Write(&buf, chunk.TypeMeta, p); err != nil {
			return nil, err
		}
	}
	if err := wr.write(buf.Bytes()); err != nil {
		return nil, err
	}
	return wr, nil
}

// write sends p to the underlying writer in one call and syncs if configured.
// Failures are sticky.
func (w *Writer) write(p []byte) error {
	n, err := w.w.Write(p)
	w.stats.BytesWritten += uint64(n)
	if err == nil && n < len(p) {
		err = io.ErrShortWrite
	}
	if err == nil && w.sync && w.syncer != nil {
		err = w.syncer.Sync()
	}
	if err != nil {
		w.err = fmt.Errorf("bin: write: %w", err)
		return w.err
	}
	return nil
}

// toTicks converts at to ticks, clamping it to be monotonic.
func (w *Writer) toTicks(at time.Duration) uint64 {
	var ticks uint64
	if at > 0 {
		ticks = uint64(at) / w.tickNS
	}
	if at < 0 || ticks < w.lastTick {
		w.stats.ClampedTimestamps++
		return w.lastTick
	}
	return ticks
}

// WriteEvent records an event at time at since the recording start. at is
// converted to ticks; a time before the previous event's tick is clamped to
// that tick and counted in Stats.ClampedTimestamps. data is copied, so the
// caller may reuse it.
//
// It flushes the pending block first if FlushInterval has elapsed since its
// first event or if data would push the block past FlushBytes, and flushes after
// adding if the block reached FlushBytes.
func (w *Writer) WriteEvent(at time.Duration, kind event.Kind, data []byte) error {
	if w.err != nil {
		return w.err
	}
	if w.closed {
		return ErrClosed
	}
	if kind == event.KindIn && !w.allowInput {
		return ErrInputNotRecorded
	}
	if kind.IsReserved() {
		return fmt.Errorf("%w: reserved event kind %d", event.ErrUnsupported, uint8(kind))
	}
	if len(data) > MaxEventData {
		return fmt.Errorf("%w: %d bytes, max %d", ErrEventTooLarge, len(data), MaxEventData)
	}

	now := w.now()
	cost := len(data) + perEventCost
	if w.pending {
		due := w.flushInterval > 0 && now.Sub(w.since) >= w.flushInterval
		full := w.flushBytes > 0 && w.cost+cost > w.flushBytes
		if due || full {
			if err := w.flush(); err != nil {
				return err
			}
		}
	}
	if !w.pending {
		w.pending = true
		w.since = now
	}
	w.cost += cost

	e := event.Event{Tick: w.toTicks(at), Kind: kind}
	w.lastTick = e.Tick
	if w.co != nil {
		e.Data = data
		w.evs = append(w.evs, w.co.Add(e)...)
	} else {
		if len(data) > 0 {
			start := len(w.arena)
			w.arena = append(w.arena, data...)
			e.Data = w.arena[start:len(w.arena):len(w.arena)]
		}
		w.evs = append(w.evs, e)
	}

	if (w.flushBytes > 0 && w.cost >= w.flushBytes) || len(w.evs) >= event.DefaultMaxEvents {
		return w.flush()
	}
	return nil
}

// Output records raw PTY output p at time at.
func (w *Writer) Output(at time.Duration, p []byte) error {
	return w.WriteEvent(at, event.KindOut, p)
}

// Input records raw input p at time at. It returns ErrInputNotRecorded unless
// head.FlagInputRecorded was set in the head passed to NewWriter, so a
// recording made without the user's consent never contains keystrokes.
func (w *Writer) Input(at time.Duration, p []byte) error {
	return w.WriteEvent(at, event.KindIn, p)
}

// Resize records a terminal resize. Cell pixel sizes are 0 when unknown.
func (w *Writer) Resize(at time.Duration, cols, rows, cellW, cellH int) error {
	if cols < 0 || rows < 0 || cellW < 0 || cellH < 0 {
		return errors.New("bin: negative resize dimension")
	}
	p := event.ResizePayload{
		Cols: uint64(cols), Rows: uint64(rows),
		CellPxW: uint64(cellW), CellPxH: uint64(cellH),
	}
	return w.WriteEvent(at, event.KindResize, p.Append(nil))
}

// Mark records a marker with a UTF-8 label.
func (w *Writer) Mark(at time.Duration, label string) error {
	return w.WriteEvent(at, event.KindMark, []byte(label))
}

// Exit records the exit status of the recorded process.
func (w *Writer) Exit(at time.Duration, status int) error {
	return w.WriteEvent(at, event.KindExit, event.NewExit(0, int64(status)).Data)
}

// Tick flushes the pending block if FlushInterval has elapsed between the
// clock reading of its first event and now. The owner calls it from a timer; it
// reads no clock itself. With nothing pending it does nothing and writes no
// bytes.
func (w *Writer) Tick(now time.Time) error {
	if w.err != nil {
		return w.err
	}
	if w.closed || !w.pending {
		return nil
	}
	if w.flushInterval > 0 && now.Sub(w.since) >= w.flushInterval {
		return w.flush()
	}
	return nil
}

// Flush writes the pending events as one EVNT chunk, if there are any.
func (w *Writer) Flush() error {
	if w.err != nil {
		return w.err
	}
	if w.closed {
		return ErrClosed
	}
	return w.flush()
}

// flush writes the pending block. It writes nothing if no event is pending.
func (w *Writer) flush() error {
	if w.co != nil {
		w.evs = append(w.evs, w.co.Flush()...)
	}
	if len(w.evs) == 0 {
		w.reset()
		return nil
	}
	b := event.Block{
		StartTick: w.evs[0].Tick,
		EndTick:   w.evs[len(w.evs)-1].Tick,
		Events:    w.evs,
	}
	payload, err := w.enc.Encode(b, w.codec, w.dict, w.dictID)
	if err != nil {
		w.err = fmt.Errorf("bin: encode block: %w", err)
		return w.err
	}
	hdr, body, err := event.ReadHeader(payload)
	if err != nil {
		w.err = fmt.Errorf("bin: encode block: %w", err)
		return w.err
	}
	var buf bytes.Buffer
	buf.Grow(chunk.Overhead + len(payload))
	if err := chunk.Write(&buf, chunk.TypeEvent, payload); err != nil {
		w.err = err
		return err
	}
	n := uint64(len(w.evs))
	w.reset()
	if err := w.write(buf.Bytes()); err != nil {
		return err
	}
	w.stats.Blocks++
	w.stats.Events += n
	w.stats.RawBytes += hdr.RawLen
	w.stats.CompressedBytes += uint64(len(body))
	return nil
}

func (w *Writer) reset() {
	clear(w.evs)
	w.evs = w.evs[:0]
	w.arena = w.arena[:0]
	w.cost = 0
	w.pending = false
}

// Close flushes the pending block and finishes the recording. It does not
// close the underlying writer. Close returns the first error the Writer
// encountered, and is idempotent: later calls return the same result.
func (w *Writer) Close() error {
	if w.closed {
		return w.err
	}
	if w.err == nil {
		if err := w.flush(); err == nil {
			w.writeFooter()
		}
	}
	w.closed = true
	return w.err
}

// writeFooter is the hook where Close will append the indx footer chunk and
// the trailer once the index package exists. A recording without a footer is
// valid; readers scan instead. It writes nothing today.
func (w *Writer) writeFooter() {}

// Stats returns the counters accumulated so far.
func (w *Writer) Stats() Stats { return w.stats }

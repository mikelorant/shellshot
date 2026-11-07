package event

import (
	"errors"
	"fmt"

	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/varint"
)

// Block is the decoded content of one EVNT chunk.
//
// Invariants (checked by Encode and Decode): StartTick <= first event tick;
// event ticks are non-decreasing; EndTick is the tick of the last event, or
// equal to StartTick when there are no events.
type Block struct {
	StartTick, EndTick uint64
	Events             []Event
}

// Header is the uncompressed prefix of an EVNT payload.
type Header struct {
	Codec              codec.ID
	DictID             uint64 // 0 means no dictionary
	StartTick, EndTick uint64
	NEvents            uint64
	// RawLen is the length of the decompressed body.
	RawLen uint64
}

// Limits bound what Decode accepts from untrusted input. A zero field means
// its default.
type Limits struct {
	// MaxEvents caps n_events. Default 1<<20.
	MaxEvents uint64
	// MaxRawLen caps raw_len, the decompressed body size. Default 64 MiB.
	MaxRawLen uint64
	// MaxPayload caps the whole chunk payload (header plus compressed body).
	// Default 64 MiB.
	MaxPayload uint64
}

// Default limits applied for zero Limits fields.
const (
	DefaultMaxEvents  = 1 << 20
	DefaultMaxRawLen  = 64 << 20
	DefaultMaxPayload = 64 << 20
)

func (l Limits) withDefaults() Limits {
	if l.MaxEvents == 0 {
		l.MaxEvents = DefaultMaxEvents
	}
	if l.MaxRawLen == 0 {
		l.MaxRawLen = DefaultMaxRawLen
	}
	if l.MaxPayload == 0 {
		l.MaxPayload = DefaultMaxPayload
	}
	return l
}

// minEventBytes is the smallest body footprint of one event: a one-byte dt,
// the kind byte and a one-byte plen.
const minEventBytes = 3

// ReadHeader parses the uncompressed header of an EVNT payload and returns
// the remaining (compressed) body. It never decompresses and allocates
// nothing. It checks only StartTick <= EndTick; Decode does full validation.
func ReadHeader(payload []byte) (Header, []byte, error) {
	r := varint.NewReader(payload)
	var h Header
	var codecID uint64
	for _, f := range []struct {
		name string
		dst  *uint64
	}{
		{"codec", &codecID},
		{"dict_id", &h.DictID},
		{"start_tick", &h.StartTick},
		{"end_tick", &h.EndTick},
		{"n_events", &h.NEvents},
		{"raw_len", &h.RawLen},
	} {
		v, err := r.Uvarint()
		if err != nil {
			return Header{}, nil, fmt.Errorf("%w: header %s: %w", ErrCorrupt, f.name, err)
		}
		*f.dst = v
	}
	h.Codec = codec.ID(codecID)
	if h.StartTick > h.EndTick {
		return Header{}, nil, fmt.Errorf("%w: start_tick %d after end_tick %d", ErrCorrupt, h.StartTick, h.EndTick)
	}
	body, _ := r.Bytes(r.Remaining())
	return h, body, nil
}

// Encoder encodes blocks. It keeps scratch buffers between calls, so an
// Encoder must not be used concurrently. The zero value is ready to use.
type Encoder struct {
	raw []byte
}

// Encode returns the EVNT chunk payload for b, compressing the body with c
// and the optional preset dictionary dict. dictID is recorded in the header
// and must be 0 exactly when dict is empty. The returned slice is newly
// allocated.
//
// Encode fails with ErrInvalidBlock if ticks are inconsistent and with
// ErrUnsupported if an event has a reserved kind.
func (enc *Encoder) Encode(b Block, c codec.Codec, dict []byte, dictID uint64) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("%w: nil codec", ErrInvalidBlock)
	}
	if (dictID == 0) != (len(dict) == 0) {
		return nil, fmt.Errorf("%w: dict_id %d does not match dictionary of %d bytes", ErrInvalidBlock, dictID, len(dict))
	}
	if err := checkBlock(b); err != nil {
		return nil, err
	}

	raw := enc.raw[:0]
	prev := b.StartTick
	for _, e := range b.Events {
		raw = varint.AppendUvarint(raw, e.Tick-prev)
		prev = e.Tick
	}
	for _, e := range b.Events {
		raw = append(raw, byte(e.Kind))
	}
	for _, e := range b.Events {
		raw = varint.AppendUvarint(raw, uint64(len(e.Data)))
	}
	for _, e := range b.Events {
		raw = append(raw, e.Data...)
	}
	enc.raw = raw

	out := make([]byte, 0, 6*varint.MaxLen+len(raw)/2)
	out = varint.AppendUvarint(out, uint64(c.ID()))
	out = varint.AppendUvarint(out, dictID)
	out = varint.AppendUvarint(out, b.StartTick)
	out = varint.AppendUvarint(out, b.EndTick)
	out = varint.AppendUvarint(out, uint64(len(b.Events)))
	out = varint.AppendUvarint(out, uint64(len(raw)))
	out, err := c.Compress(out, raw, dict)
	if err != nil {
		return nil, fmt.Errorf("event: compress body: %w", err)
	}
	return out, nil
}

func checkBlock(b Block) error {
	if b.StartTick > b.EndTick {
		return fmt.Errorf("%w: start_tick %d after end_tick %d", ErrInvalidBlock, b.StartTick, b.EndTick)
	}
	last := b.StartTick
	for i, e := range b.Events {
		if e.Kind.IsReserved() {
			return fmt.Errorf("%w: event %d has reserved kind %d", ErrUnsupported, i, uint8(e.Kind))
		}
		if e.Tick < last {
			return fmt.Errorf("%w: event %d tick %d precedes %d", ErrInvalidBlock, i, e.Tick, last)
		}
		last = e.Tick
	}
	if last != b.EndTick {
		return fmt.Errorf("%w: end_tick %d is not the last event tick %d", ErrInvalidBlock, b.EndTick, last)
	}
	return nil
}

// Decode parses and validates an EVNT chunk payload.
//
// reg resolves the codec. dicts resolves a nonzero dict_id to the preset
// dictionary and may be nil when no dictionary is expected. lim bounds
// untrusted counts and sizes; every bound is checked before memory is sized
// from it, and the event slice is allocated only after the decompressed body
// has been verified to be long enough to hold that many events.
//
// Decoded Event.Data slices alias one shared backing array (with capacity
// clipped to their length); copy them to retain individual payloads cheaply.
// Events of ancillary kinds (>= 64) are returned as ordinary events. A
// reserved kind yields ErrUnsupported.
func Decode(payload []byte, reg *codec.Registry, dicts func(id uint64) ([]byte, bool), lim Limits) (Block, error) {
	lim = lim.withDefaults()
	if uint64(len(payload)) > lim.MaxPayload {
		return Block{}, fmt.Errorf("%w: payload of %d bytes", ErrLimit, len(payload))
	}
	h, body, err := ReadHeader(payload)
	if err != nil {
		return Block{}, err
	}
	if h.NEvents > lim.MaxEvents {
		return Block{}, fmt.Errorf("%w: %d events", ErrLimit, h.NEvents)
	}
	if h.RawLen > lim.MaxRawLen {
		return Block{}, fmt.Errorf("%w: raw_len %d", ErrLimit, h.RawLen)
	}
	if h.NEvents > h.RawLen/minEventBytes {
		return Block{}, fmt.Errorf("%w: %d events cannot fit in raw_len %d", ErrCorrupt, h.NEvents, h.RawLen)
	}
	if h.NEvents == 0 && (h.EndTick != h.StartTick || h.RawLen != 0) {
		return Block{}, fmt.Errorf("%w: empty block with end_tick %d, start_tick %d, raw_len %d", ErrCorrupt, h.EndTick, h.StartTick, h.RawLen)
	}

	if reg == nil {
		return Block{}, errors.New("event: nil codec registry")
	}
	c, err := reg.Get(h.Codec)
	if err != nil {
		return Block{}, err
	}
	var dict []byte
	if h.DictID != 0 {
		var ok bool
		if dicts != nil {
			dict, ok = dicts(h.DictID)
		}
		if !ok || len(dict) == 0 {
			return Block{}, fmt.Errorf("%w: id %d", ErrUnknownDict, h.DictID)
		}
	}

	// RawLen <= MaxRawLen <= max int on 64-bit platforms; on 32-bit the
	// default still fits, and a larger caller limit is the caller's concern.
	raw, err := c.Decompress(nil, body, dict, int(h.RawLen))
	if err != nil {
		return Block{}, fmt.Errorf("event: decompress body: %w", err)
	}
	if uint64(len(raw)) != h.RawLen {
		return Block{}, fmt.Errorf("%w: body is %d bytes, raw_len says %d", ErrCorrupt, len(raw), h.RawLen)
	}
	return parseBody(h, raw)
}

// parseBody splits the decompressed columns. len(raw) == h.RawLen and
// h.NEvents*minEventBytes <= len(raw) hold on entry, so allocations below are
// bounded by the bytes actually decompressed.
func parseBody(h Header, raw []byte) (Block, error) {
	n := int(h.NEvents)
	events := make([]Event, n)
	r := varint.NewReader(raw)

	tick := h.StartTick
	for i := range events {
		dt, err := r.Uvarint()
		if err != nil {
			return Block{}, fmt.Errorf("%w: dt[%d]: %w", ErrCorrupt, i, err)
		}
		if tick+dt < tick {
			return Block{}, fmt.Errorf("%w: tick overflow at event %d", ErrCorrupt, i)
		}
		tick += dt
		events[i].Tick = tick
	}
	if tick != h.EndTick {
		return Block{}, fmt.Errorf("%w: last event tick %d does not match end_tick %d", ErrCorrupt, tick, h.EndTick)
	}

	kinds, err := r.Bytes(n)
	if err != nil {
		return Block{}, fmt.Errorf("%w: kind column: %w", ErrCorrupt, err)
	}
	for i, k := range kinds {
		events[i].Kind = Kind(k)
		if events[i].Kind.IsReserved() {
			return Block{}, fmt.Errorf("%w: event %d has reserved kind %d", ErrUnsupported, i, k)
		}
	}

	// First pass validates the plen column and its sum; the second pass below
	// re-reads it to slice the payloads, so no per-event length array is
	// needed.
	var total uint64
	plenStart := len(raw) - r.Remaining()
	for i := 0; i < n; i++ {
		l, err := r.Uvarint()
		if err != nil {
			return Block{}, fmt.Errorf("%w: plen[%d]: %w", ErrCorrupt, i, err)
		}
		if l > uint64(r.Remaining()) {
			return Block{}, fmt.Errorf("%w: plen[%d]=%d exceeds body", ErrCorrupt, i, l)
		}
		total += l // each l <= len(raw) and n <= 1<<20, so no overflow in uint64
	}
	if total != uint64(r.Remaining()) {
		return Block{}, fmt.Errorf("%w: payload lengths sum to %d, %d bytes present", ErrCorrupt, total, r.Remaining())
	}
	payloads, _ := r.Bytes(r.Remaining())

	lr := varint.NewReader(raw[plenStart:])
	off := 0
	for i := range events {
		l, _ := lr.Uvarint() // validated in the first pass
		if l > 0 {
			events[i].Data = payloads[off : off+int(l) : off+int(l)]
			off += int(l)
		}
	}
	return Block{StartTick: h.StartTick, EndTick: h.EndTick, Events: events}, nil
}

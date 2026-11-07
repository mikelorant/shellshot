// Package event implements the EVNT chunk payload of the binary recording
// format: typed events, columnar block encoding and decoding, and the
// writer-side coalescer.
//
// Block payload layout (all integers are canonical uvarints):
//
//	codec | dict_id | start_tick | end_tick | n_events | raw_len | body
//
// The header is uncompressed so scanners can read time ranges with ReadHeader.
// The body is compressed with the named codec and, once decompressed, is
// raw_len bytes of columns:
//
//	dt[n]    uvarint  ticks since the previous event (first: since start_tick)
//	kind[n]  byte
//	plen[n]  uvarint  payload length
//	payload  concatenated payload bytes
package event

import (
	"errors"
	"fmt"

	"github.com/michaellorant/shellshot/internal/recording/bin/varint"
)

// Kind identifies the type of an event.
type Kind uint8

const (
	// KindOut is raw PTY output.
	KindOut Kind = 0
	// KindIn is raw input bytes (only recorded when enabled).
	KindIn Kind = 1
	// KindResize carries a ResizePayload.
	KindResize Kind = 2
	// KindMark carries a UTF-8 label.
	KindMark Kind = 3
	// KindExit carries a zigzag exit status.
	KindExit Kind = 4

	// FirstReservedKind is the first kind that is reserved and critical:
	// readers that meet kinds FirstReservedKind..LastReservedKind fail with
	// ErrUnsupported.
	FirstReservedKind Kind = 5
	// LastReservedKind is the last reserved critical kind.
	LastReservedKind Kind = 63
	// FirstAncillaryKind is the first ancillary kind. Ancillary events are
	// surfaced as raw events with their kind number kept; exporters ignore
	// them.
	FirstAncillaryKind Kind = 64
)

// String returns a short name for diagnostics.
func (k Kind) String() string {
	switch k {
	case KindOut:
		return "out"
	case KindIn:
		return "in"
	case KindResize:
		return "resize"
	case KindMark:
		return "mark"
	case KindExit:
		return "exit"
	case KindSpan:
		return "span"
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// IsReserved reports whether k is in the reserved critical range.
func (k Kind) IsReserved() bool {
	return k >= FirstReservedKind && k <= LastReservedKind
}

// IsAncillary reports whether k is an ancillary passthrough kind.
func (k Kind) IsAncillary() bool { return k >= FirstAncillaryKind }

// Event is one recorded event.
type Event struct {
	// Tick is the absolute time in ticks since recording start.
	Tick uint64
	Kind Kind
	// Data is the kind-specific payload. Decoded events have nil Data for an
	// empty payload.
	Data []byte
}

var (
	// ErrCorrupt means the payload is malformed or internally inconsistent.
	ErrCorrupt = errors.New("event: corrupt block")
	// ErrUnsupported means the block contains a reserved critical event kind.
	ErrUnsupported = errors.New("event: unsupported event kind")
	// ErrLimit means a count or length exceeds the configured Limits.
	ErrLimit = errors.New("event: block exceeds limit")
	// ErrUnknownDict means the block names a dictionary that was not supplied.
	ErrUnknownDict = errors.New("event: unknown dictionary")
	// ErrInvalidBlock means a Block passed to Encode is inconsistent.
	ErrInvalidBlock = errors.New("event: invalid block")
)

// ResizePayload is the payload of a KindResize event. Cell pixel sizes are 0
// when unknown.
type ResizePayload struct {
	Cols, Rows       uint64
	CellPxW, CellPxH uint64
}

// Append appends the wire form of p (four uvarints) to dst.
func (p ResizePayload) Append(dst []byte) []byte {
	dst = varint.AppendUvarint(dst, p.Cols)
	dst = varint.AppendUvarint(dst, p.Rows)
	dst = varint.AppendUvarint(dst, p.CellPxW)
	return varint.AppendUvarint(dst, p.CellPxH)
}

// ParseResize parses a KindResize payload. Trailing bytes are an error.
func ParseResize(data []byte) (ResizePayload, error) {
	r := varint.NewReader(data)
	var p ResizePayload
	for _, f := range []*uint64{&p.Cols, &p.Rows, &p.CellPxW, &p.CellPxH} {
		v, err := r.Uvarint()
		if err != nil {
			return ResizePayload{}, fmt.Errorf("%w: resize payload: %w", ErrCorrupt, err)
		}
		*f = v
	}
	if r.Remaining() != 0 {
		return ResizePayload{}, fmt.Errorf("%w: resize payload has trailing bytes", ErrCorrupt)
	}
	return p, nil
}

// ParseExit parses a KindExit payload (a zigzag varint exit status). Trailing
// bytes are an error.
func ParseExit(data []byte) (int64, error) {
	v, n, err := varint.Svarint(data)
	if err != nil {
		return 0, fmt.Errorf("%w: exit payload: %w", ErrCorrupt, err)
	}
	if n != len(data) {
		return 0, fmt.Errorf("%w: exit payload has trailing bytes", ErrCorrupt)
	}
	return v, nil
}

// NewResize returns a KindResize event at tick.
func NewResize(tick uint64, p ResizePayload) Event {
	return Event{Tick: tick, Kind: KindResize, Data: p.Append(nil)}
}

// NewExit returns a KindExit event at tick carrying status.
func NewExit(tick uint64, status int64) Event {
	return Event{Tick: tick, Kind: KindExit, Data: varint.AppendSvarint(nil, status)}
}

// NewMark returns a KindMark event at tick carrying label.
func NewMark(tick uint64, label string) Event {
	return Event{Tick: tick, Kind: KindMark, Data: []byte(label)}
}

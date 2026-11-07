package event

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/mikelorant/shellshot/internal/recording/bin/varint"
)

// KindSpan marks the beginning or end of a span of the recording: a scene or a
// hidden stretch of a script. It is an ancillary kind (FirstAncillaryKind), so
// readers that do not know it surface it as a raw event and exporters ignore
// it. Use ParseSpan to decode Event.Data and AppendSpan or NewSpan to build it.
//
// Payload layout (integers are uvarints, strings are uvarint length + UTF-8):
//
//	END:   version=1 | op=2 | id
//	BEGIN: version=1 | op=1 | id | type(u8) | parent_id | name_len | name |
//	       n_hints | n_hints x { tag | len | value }
//
// Decoding ignores bytes after the last known field, and hints with unknown
// tags, so later versions can append fields without breaking this decoder.
const KindSpan Kind = 64

// SpanVersion is the payload version written by AppendSpan and the only one
// ParseSpan understands.
const SpanVersion = 1

// SpanOp says whether a SPAN event opens or closes a span.
type SpanOp uint8

const (
	SpanBegin SpanOp = 1
	SpanEnd   SpanOp = 2
)

// SpanType classifies a span.
type SpanType uint8

const (
	// SpanScene is a named scene of a script.
	SpanScene SpanType = 1
	// SpanHidden is a stretch whose steps are recorded but not shown.
	SpanHidden SpanType = 2
	// SpanBeat is reserved; it decodes but no writer emits it yet.
	SpanBeat SpanType = 3
)

// Span limits. Encoders must stay inside them; ParseSpan rejects anything
// outside.
const (
	// MaxSpanName is the longest span name or caption in bytes.
	MaxSpanName = 255
	// MaxSpanSpeedMilli is the largest speed hint (1000x) in milli-x.
	MaxSpanSpeedMilli = 1_000_000
	// MaxSpanDuration is the largest idle or hold hint.
	MaxSpanDuration = 24 * time.Hour
	// MaxSpanHints is the most hint entries a BEGIN may carry.
	MaxSpanHints = 64
)

// Hint tags.
const (
	hintSpeed   = 1
	hintIdle    = 2
	hintHold    = 3
	hintCaption = 4
)

var (
	// ErrMalformedSpan means a SPAN payload or Span value is truncated,
	// out of range or otherwise invalid.
	ErrMalformedSpan = errors.New("event: malformed span")
	// ErrUnknownSpan means a SPAN payload is well framed but uses a version,
	// op or type this package does not know. Such events are ignorable, not
	// corrupt. It does not wrap ErrMalformedSpan.
	ErrUnknownSpan = errors.New("event: unknown span version, op or type")
)

// SpanHints are the resolved export hints of a span. The zero value of a field
// means the hint is absent. Idle and Hold are stored with microsecond
// resolution.
type SpanHints struct {
	// Speed is the playback speed in milli-x (8x is 8000).
	Speed uint64
	// Idle caps each gap inside the span.
	Idle time.Duration
	// Hold is extra time on the span's last screen.
	Hold time.Duration
	// Caption is reserved text.
	Caption string
}

// Span is the decoded payload of a KindSpan event. For SpanEnd only Op and ID
// are meaningful.
type Span struct {
	Op SpanOp
	// ID is 1-based and unique in a recording, in begin order.
	ID uint64
	// Type is the span type (SpanBegin only).
	Type SpanType
	// Parent is the ID of the enclosing scene, or 0 (SpanBegin only).
	Parent uint64
	// Name is the scene name; empty for hidden spans (SpanBegin only).
	Name  string
	Hints SpanHints
}

// Validate reports whether s can be encoded and decoded back unchanged. Errors
// wrap ErrMalformedSpan.
func (s Span) Validate() error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("%w: "+format, append([]any{ErrMalformedSpan}, a...)...)
	}
	if s.ID == 0 {
		return bad("id 0")
	}
	switch s.Op {
	case SpanEnd:
		return nil
	case SpanBegin:
	default:
		return bad("op %d", s.Op)
	}
	if s.Type < SpanScene || s.Type > SpanBeat {
		return bad("type %d", s.Type)
	}
	if err := validString(s.Name); err != nil {
		return bad("name: %v", err)
	}
	h := s.Hints
	if h.Speed > MaxSpanSpeedMilli {
		return bad("speed %d milli-x exceeds %d", h.Speed, MaxSpanSpeedMilli)
	}
	if h.Idle < 0 || h.Idle > MaxSpanDuration {
		return bad("idle %v out of range", h.Idle)
	}
	if h.Hold < 0 || h.Hold > MaxSpanDuration {
		return bad("hold %v out of range", h.Hold)
	}
	if err := validString(h.Caption); err != nil {
		return bad("caption: %v", err)
	}
	return nil
}

func validString(s string) error {
	if len(s) > MaxSpanName {
		return fmt.Errorf("%d bytes, max %d", len(s), MaxSpanName)
	}
	if !utf8.ValidString(s) {
		return errors.New("invalid UTF-8")
	}
	return nil
}

// AppendSpan appends the wire form of s to dst. It does not validate s; call
// Span.Validate first for untrusted values. Hints that are zero are omitted,
// and Idle and Hold are truncated to whole microseconds.
func AppendSpan(dst []byte, s Span) []byte {
	dst = append(dst, SpanVersion, byte(s.Op))
	dst = varint.AppendUvarint(dst, s.ID)
	if s.Op != SpanBegin {
		return dst
	}
	dst = append(dst, byte(s.Type))
	dst = varint.AppendUvarint(dst, s.Parent)
	dst = varint.AppendUvarint(dst, uint64(len(s.Name)))
	dst = append(dst, s.Name...)

	h := s.Hints
	idleUS, holdUS := uint64(max(h.Idle, 0)/time.Microsecond), uint64(max(h.Hold, 0)/time.Microsecond)
	var n uint64
	for _, present := range []bool{h.Speed != 0, idleUS != 0, holdUS != 0, h.Caption != ""} {
		if present {
			n++
		}
	}
	dst = varint.AppendUvarint(dst, n)
	appendNum := func(tag, v uint64) {
		dst = varint.AppendUvarint(dst, tag)
		dst = varint.AppendUvarint(dst, uint64(len(varint.AppendUvarint(nil, v))))
		dst = varint.AppendUvarint(dst, v)
	}
	if h.Speed != 0 {
		appendNum(hintSpeed, h.Speed)
	}
	if idleUS != 0 {
		appendNum(hintIdle, idleUS)
	}
	if holdUS != 0 {
		appendNum(hintHold, holdUS)
	}
	if h.Caption != "" {
		dst = varint.AppendUvarint(dst, hintCaption)
		dst = varint.AppendUvarint(dst, uint64(len(h.Caption)))
		dst = append(dst, h.Caption...)
	}
	return dst
}

// NewSpan returns a KindSpan event at tick carrying s.
func NewSpan(tick uint64, s Span) Event {
	return Event{Tick: tick, Kind: KindSpan, Data: AppendSpan(nil, s)}
}

// ParseSpan decodes a KindSpan payload. Bytes after the last known field and
// hints with unknown tags are ignored. It returns an error wrapping
// ErrUnknownSpan for an unknown version, op or type, and one wrapping
// ErrMalformedSpan for anything truncated or out of range.
func ParseSpan(p []byte) (Span, error) {
	r := varint.NewReader(p)
	bad := func(what string, err error) (Span, error) {
		if err == nil {
			return Span{}, fmt.Errorf("%w: %s", ErrMalformedSpan, what)
		}
		return Span{}, fmt.Errorf("%w: %s: %w", ErrMalformedSpan, what, err)
	}
	ver, err := r.Byte()
	if err != nil {
		return bad("version", err)
	}
	if ver != SpanVersion {
		return Span{}, fmt.Errorf("%w: version %d", ErrUnknownSpan, ver)
	}
	op, err := r.Byte()
	if err != nil {
		return bad("op", err)
	}
	var s Span
	s.Op = SpanOp(op)
	if s.Op != SpanBegin && s.Op != SpanEnd {
		return Span{}, fmt.Errorf("%w: op %d", ErrUnknownSpan, op)
	}
	if s.ID, err = r.Uvarint(); err != nil {
		return bad("id", err)
	}
	if s.ID == 0 {
		return bad("id 0", nil)
	}
	if s.Op == SpanEnd {
		return s, nil
	}

	typ, err := r.Byte()
	if err != nil {
		return bad("type", err)
	}
	if typ == 0 {
		return bad("type 0", nil)
	}
	if SpanType(typ) > SpanBeat {
		return Span{}, fmt.Errorf("%w: type %d", ErrUnknownSpan, typ)
	}
	s.Type = SpanType(typ)
	if s.Parent, err = r.Uvarint(); err != nil {
		return bad("parent", err)
	}
	if s.Name, err = readSpanString(r); err != nil {
		return bad("name", err)
	}
	nHints, err := r.UvarintMax(MaxSpanHints)
	if err != nil {
		return bad("hint count", err)
	}
	var seen [hintCaption + 1]bool
	for range nHints {
		tag, err := r.Uvarint()
		if err != nil {
			return bad("hint tag", err)
		}
		n, err := r.UvarintMax(uint64(r.Remaining()))
		if err != nil {
			return bad("hint length", err)
		}
		val, err := r.Bytes(int(n))
		if err != nil {
			return bad("hint value", err)
		}
		if tag < hintSpeed || tag > hintCaption {
			continue
		}
		if seen[tag] {
			return bad(fmt.Sprintf("duplicate hint %d", tag), nil)
		}
		seen[tag] = true
		if tag == hintCaption {
			if err := validString(string(val)); err != nil {
				return bad("caption", err)
			}
			s.Hints.Caption = string(val)
			continue
		}
		v, used, err := varint.Uvarint(val)
		if err != nil {
			return bad(fmt.Sprintf("hint %d value", tag), err)
		}
		if used != len(val) {
			return bad(fmt.Sprintf("hint %d value has trailing bytes", tag), nil)
		}
		switch tag {
		case hintSpeed:
			if v == 0 || v > MaxSpanSpeedMilli {
				return bad(fmt.Sprintf("speed %d out of range", v), nil)
			}
			s.Hints.Speed = v
		case hintIdle, hintHold:
			if v > uint64(MaxSpanDuration/time.Microsecond) {
				return bad(fmt.Sprintf("hint %d duration %dµs out of range", tag, v), nil)
			}
			d := time.Duration(v) * time.Microsecond
			if tag == hintIdle {
				s.Hints.Idle = d
			} else {
				s.Hints.Hold = d
			}
		}
	}
	return s, nil
}

// readSpanString reads a uvarint length followed by that many bytes of UTF-8.
func readSpanString(r *varint.Reader) (string, error) {
	n, err := r.UvarintMax(MaxSpanName)
	if err != nil {
		return "", err
	}
	b, err := r.Bytes(int(n))
	if err != nil {
		return "", err
	}
	if !utf8.Valid(b) {
		return "", errors.New("invalid UTF-8")
	}
	return string(b), nil
}

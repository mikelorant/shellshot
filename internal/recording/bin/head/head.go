// Package head encodes and decodes the payloads of the HEAD, them and meta
// chunks of the binary recording format.
//
// The HEAD payload is a sequence of tagged fields, each encoded as
// [tag uvarint][len uvarint][value]. Fields are written in ascending tag
// order. Decoders keep fields with unknown tags in Head.Unknown and refuse
// files whose RequiredFeatures contain bits this package does not know.
//
// All decoders are bounds-checked and apply fixed limits before sizing any
// allocation from untrusted input.
package head

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"

	"github.com/michaellorant/shellshot/internal/recording/bin/varint"
)

// Limits applied by both encoders and decoders.
const (
	// MaxStringLen is the longest string or unknown-field value in bytes.
	MaxStringLen = 64 << 10
	// MaxListLen is the largest number of argv entries, env entries, unknown
	// fields or meta pairs.
	MaxListLen = 4096
	// MaxDimension is the largest cols, rows and cell pixel size.
	MaxDimension = 65535
	// DefaultTickNS is the tick length used when HEAD has no tick field.
	DefaultTickNS = 100000
	// maxFieldLen bounds one field value: the engine field holds three
	// length-prefixed strings, the largest composite.
	maxFieldLen = 3 * (MaxStringLen + varint.MaxLen)
)

// Required feature bits.
const (
	// FeatureBlockDictionaries means EVNT blocks may reference DICT chunks.
	FeatureBlockDictionaries uint64 = 1 << 0
	// KnownFeatures is the set of RequiredFeatures bits this package supports.
	KnownFeatures = FeatureBlockDictionaries
)

// Flags bits. Bit 1 is unused: clean close is signalled by the footer index.
const (
	FlagInputRecorded uint64 = 1 << 0
	FlagCoalesced     uint64 = 1 << 2
)

// HEAD field tags.
const (
	tagVersion    = 1
	tagSize       = 2
	tagTick       = 3
	tagStart      = 4
	tagCellPx     = 5
	tagEngine     = 6
	tagEngineConf = 7
	tagTerm       = 8
	tagArgv       = 9
	tagCwd        = 10
	tagEnv        = 11
	tagTitle      = 12
	tagFont       = 13
	tagFlags      = 14
	tagHostname   = 15
	tagSessionID  = 16
	tagRecorder   = 17
	maxKnownTag   = tagRecorder
)

var (
	// ErrMalformed means the payload is truncated, has trailing bytes, a
	// repeated singleton field, a missing required field or a bad encoding.
	ErrMalformed = errors.New("head: malformed payload")
	// ErrUnsupported means the file requires features this package does not
	// implement.
	ErrUnsupported = errors.New("head: unsupported required feature")
	// ErrLimit means a value is outside the limits above.
	ErrLimit = errors.New("head: value exceeds limit")
)

// RawField is a HEAD field whose tag this package does not know.
type RawField struct {
	Tag   uint64
	Value []byte
}

// Engine identifies the terminal engine that produced the recording.
type Engine struct {
	Name       string
	Commit     string
	BuildFlags string
}

// EngineConfig holds engine limits that affect replay.
type EngineConfig struct {
	ScrollbackLimit   uint64
	KittyStorageLimit uint64
}

// FontHint carries rendering hints captured at record time.
type FontHint struct {
	Family     string
	SizePt     float32
	CellAspect float32
}

// Head is the decoded HEAD chunk payload.
type Head struct {
	FormatMinor      uint64
	RequiredFeatures uint64
	Cols, Rows       int
	// TickNS is the tick length in nanoseconds. Marshal writes DefaultTickNS
	// when it is zero.
	TickNS        uint64
	StartedUnixNS int64
	TZOffsetS     int64
	CellPxW       int
	CellPxH       int
	Engine        Engine
	EngineConfig  EngineConfig
	Term          string
	Argv          []string
	Cwd           string
	// Env holds the allowlisted environment as K=V strings.
	Env       []string
	Title     string
	Font      FontHint
	Flags     uint64
	Hostname  string
	SessionID [16]byte
	Recorder  string
	// Unknown holds fields with tags this package does not know, in encounter
	// order. Marshal writes them back in tag order.
	Unknown []RawField
}

type field struct {
	tag   uint64
	value []byte
}

// Marshal encodes h deterministically: fields in ascending tag order, zero
// values omitted except cols, rows and tick. It returns ErrLimit if h is
// outside the decoder limits and ErrMalformed if Unknown uses a known tag.
func (h Head) Marshal() ([]byte, error) {
	if err := h.validate(); err != nil {
		return nil, err
	}
	tick := h.TickNS
	if tick == 0 {
		tick = DefaultTickNS
	}
	var fs []field
	add := func(tag uint64, v []byte) { fs = append(fs, field{tag, v}) }
	str := func(tag uint64, s string) {
		if s != "" {
			add(tag, []byte(s))
		}
	}

	if h.FormatMinor != 0 || h.RequiredFeatures != 0 {
		add(tagVersion, uvarints(h.FormatMinor, h.RequiredFeatures))
	}
	add(tagSize, uvarints(uint64(h.Cols), uint64(h.Rows)))
	add(tagTick, uvarints(tick))
	if h.StartedUnixNS != 0 || h.TZOffsetS != 0 {
		v := varint.AppendSvarint(nil, h.StartedUnixNS)
		add(tagStart, varint.AppendSvarint(v, h.TZOffsetS))
	}
	if h.CellPxW != 0 || h.CellPxH != 0 {
		add(tagCellPx, uvarints(uint64(h.CellPxW), uint64(h.CellPxH)))
	}
	if h.Engine != (Engine{}) {
		var v []byte
		v = appendString(v, h.Engine.Name)
		v = appendString(v, h.Engine.Commit)
		v = appendString(v, h.Engine.BuildFlags)
		add(tagEngine, v)
	}
	if h.EngineConfig != (EngineConfig{}) {
		add(tagEngineConf, uvarints(h.EngineConfig.ScrollbackLimit, h.EngineConfig.KittyStorageLimit))
	}
	str(tagTerm, h.Term)
	for _, a := range h.Argv {
		add(tagArgv, []byte(a))
	}
	str(tagCwd, h.Cwd)
	for _, e := range h.Env {
		add(tagEnv, []byte(e))
	}
	str(tagTitle, h.Title)
	if h.Font.Family != "" || h.Font.SizePt != 0 || h.Font.CellAspect != 0 {
		v := appendString(nil, h.Font.Family)
		v = appendFloat32(v, h.Font.SizePt)
		v = appendFloat32(v, h.Font.CellAspect)
		add(tagFont, v)
	}
	if h.Flags != 0 {
		add(tagFlags, uvarints(h.Flags))
	}
	str(tagHostname, h.Hostname)
	if h.SessionID != ([16]byte{}) {
		add(tagSessionID, h.SessionID[:])
	}
	str(tagRecorder, h.Recorder)
	for _, u := range h.Unknown {
		add(u.Tag, u.Value)
	}
	sort.SliceStable(fs, func(i, j int) bool { return fs[i].tag < fs[j].tag })

	var out []byte
	for _, f := range fs {
		out = varint.AppendUvarint(out, f.tag)
		out = varint.AppendUvarint(out, uint64(len(f.value)))
		out = append(out, f.value...)
	}
	return out, nil
}

func (h Head) validate() error {
	if h.Cols < 1 || h.Cols > MaxDimension || h.Rows < 1 || h.Rows > MaxDimension {
		return fmt.Errorf("%w: size %dx%d outside 1..%d", ErrLimit, h.Cols, h.Rows, MaxDimension)
	}
	if h.CellPxW < 0 || h.CellPxW > MaxDimension || h.CellPxH < 0 || h.CellPxH > MaxDimension {
		return fmt.Errorf("%w: cell size %dx%d outside 0..%d", ErrLimit, h.CellPxW, h.CellPxH, MaxDimension)
	}
	if len(h.Argv) > MaxListLen || len(h.Env) > MaxListLen || len(h.Unknown) > MaxListLen {
		return fmt.Errorf("%w: more than %d list entries", ErrLimit, MaxListLen)
	}
	for _, s := range h.strings() {
		if len(s) > MaxStringLen {
			return fmt.Errorf("%w: string of %d bytes", ErrLimit, len(s))
		}
	}
	for _, u := range h.Unknown {
		if u.Tag >= 1 && u.Tag <= maxKnownTag {
			return fmt.Errorf("%w: unknown field uses known tag %d", ErrMalformed, u.Tag)
		}
		if len(u.Value) > MaxStringLen {
			return fmt.Errorf("%w: unknown field of %d bytes", ErrLimit, len(u.Value))
		}
	}
	return nil
}

func (h Head) strings() []string {
	ss := []string{
		h.Engine.Name, h.Engine.Commit, h.Engine.BuildFlags, h.Term, h.Cwd,
		h.Title, h.Font.Family, h.Hostname, h.Recorder,
	}
	ss = append(ss, h.Argv...)
	return append(ss, h.Env...)
}

// Unmarshal decodes a HEAD payload. Fields with unknown tags are kept in
// Unknown. It returns ErrUnsupported if RequiredFeatures has unknown bits,
// ErrLimit for values outside the limits and ErrMalformed for anything else
// invalid, including a missing size field.
func Unmarshal(b []byte) (Head, error) {
	h := Head{TickNS: DefaultTickNS}
	var seen [maxKnownTag + 1]bool
	r := varint.NewReader(b)
	for r.Remaining() > 0 {
		tag, err := r.Uvarint()
		if err != nil {
			return Head{}, malformed("field tag", err)
		}
		n, err := r.UvarintMax(maxFieldLen)
		if err != nil {
			return Head{}, wrap(fmt.Sprintf("tag %d length", tag), err)
		}
		value, err := r.Bytes(int(n))
		if err != nil {
			return Head{}, malformed(fmt.Sprintf("tag %d value", tag), err)
		}
		if tag >= 1 && tag <= maxKnownTag {
			repeated := tag == tagArgv || tag == tagEnv
			if seen[tag] && !repeated {
				return Head{}, fmt.Errorf("%w: tag %d repeated", ErrMalformed, tag)
			}
			seen[tag] = true
		}
		if err := h.decodeField(tag, value); err != nil {
			return Head{}, err
		}
	}
	if !seen[tagSize] {
		return Head{}, fmt.Errorf("%w: missing size field", ErrMalformed)
	}
	return h, nil
}

func (h *Head) decodeField(tag uint64, value []byte) error {
	r := varint.NewReader(value)
	var err error
	switch tag {
	case tagVersion:
		if h.FormatMinor, err = r.Uvarint(); err == nil {
			h.RequiredFeatures, err = r.Uvarint()
		}
		if err == nil && h.RequiredFeatures&^KnownFeatures != 0 {
			return fmt.Errorf("%w: features %#x", ErrUnsupported, h.RequiredFeatures&^KnownFeatures)
		}
	case tagSize:
		if h.Cols, err = readDimension(r, 1); err == nil {
			h.Rows, err = readDimension(r, 1)
		}
	case tagTick:
		if h.TickNS, err = r.Uvarint(); err == nil && h.TickNS == 0 {
			return fmt.Errorf("%w: tick_ns is zero", ErrLimit)
		}
	case tagStart:
		if h.StartedUnixNS, err = r.Svarint(); err == nil {
			h.TZOffsetS, err = r.Svarint()
		}
	case tagCellPx:
		if h.CellPxW, err = readDimension(r, 0); err == nil {
			h.CellPxH, err = readDimension(r, 0)
		}
	case tagEngine:
		if h.Engine.Name, err = readString(r); err == nil {
			if h.Engine.Commit, err = readString(r); err == nil {
				h.Engine.BuildFlags, err = readString(r)
			}
		}
	case tagEngineConf:
		if h.EngineConfig.ScrollbackLimit, err = r.Uvarint(); err == nil {
			h.EngineConfig.KittyStorageLimit, err = r.Uvarint()
		}
	case tagTerm:
		return rawString(&h.Term, value)
	case tagArgv:
		if len(h.Argv) >= MaxListLen {
			return fmt.Errorf("%w: more than %d argv entries", ErrLimit, MaxListLen)
		}
		var s string
		if err := rawString(&s, value); err != nil {
			return err
		}
		h.Argv = append(h.Argv, s)
		return nil
	case tagCwd:
		return rawString(&h.Cwd, value)
	case tagEnv:
		if len(h.Env) >= MaxListLen {
			return fmt.Errorf("%w: more than %d env entries", ErrLimit, MaxListLen)
		}
		var s string
		if err := rawString(&s, value); err != nil {
			return err
		}
		h.Env = append(h.Env, s)
		return nil
	case tagTitle:
		return rawString(&h.Title, value)
	case tagFont:
		if h.Font.Family, err = readString(r); err == nil {
			if h.Font.SizePt, err = readFloat32(r); err == nil {
				h.Font.CellAspect, err = readFloat32(r)
			}
		}
	case tagFlags:
		h.Flags, err = r.Uvarint()
	case tagHostname:
		return rawString(&h.Hostname, value)
	case tagSessionID:
		if len(value) != len(h.SessionID) {
			return fmt.Errorf("%w: session id of %d bytes", ErrMalformed, len(value))
		}
		copy(h.SessionID[:], value)
		return nil
	case tagRecorder:
		return rawString(&h.Recorder, value)
	default:
		if len(h.Unknown) >= MaxListLen {
			return fmt.Errorf("%w: more than %d unknown fields", ErrLimit, MaxListLen)
		}
		if len(value) > MaxStringLen {
			return fmt.Errorf("%w: unknown field of %d bytes", ErrLimit, len(value))
		}
		h.Unknown = append(h.Unknown, RawField{Tag: tag, Value: slices.Clone(value)})
		return nil
	}
	if err != nil {
		return wrap(fmt.Sprintf("tag %d", tag), err)
	}
	if r.Remaining() != 0 {
		return fmt.Errorf("%w: tag %d has %d trailing bytes", ErrMalformed, tag, r.Remaining())
	}
	return nil
}

func uvarints(vs ...uint64) []byte {
	var b []byte
	for _, v := range vs {
		b = varint.AppendUvarint(b, v)
	}
	return b
}

func appendString(dst []byte, s string) []byte {
	dst = varint.AppendUvarint(dst, uint64(len(s)))
	return append(dst, s...)
}

func appendFloat32(dst []byte, f float32) []byte {
	u := math.Float32bits(f)
	return append(dst, byte(u), byte(u>>8), byte(u>>16), byte(u>>24))
}

func readString(r *varint.Reader) (string, error) {
	n, err := r.UvarintMax(MaxStringLen)
	if err != nil {
		return "", err
	}
	b, err := r.Bytes(int(n))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func readFloat32(r *varint.Reader) (float32, error) {
	b, err := r.Bytes(4)
	if err != nil {
		return 0, err
	}
	return math.Float32frombits(uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24), nil
}

func readDimension(r *varint.Reader, min int) (int, error) {
	v, err := r.Uvarint()
	if err != nil {
		return 0, err
	}
	if v > MaxDimension || int(v) < min {
		return 0, fmt.Errorf("%w: dimension %d outside %d..%d", varint.ErrLimit, v, min, MaxDimension)
	}
	return int(v), nil
}

func rawString(dst *string, value []byte) error {
	if len(value) > MaxStringLen {
		return fmt.Errorf("%w: string of %d bytes", ErrLimit, len(value))
	}
	*dst = string(value)
	return nil
}

// wrap classifies a varint-level error as ErrLimit or ErrMalformed.
func wrap(what string, err error) error {
	if errors.Is(err, varint.ErrLimit) {
		return fmt.Errorf("%w: %s: %v", ErrLimit, what, err)
	}
	return malformed(what, err)
}

func malformed(what string, err error) error {
	return fmt.Errorf("%w: %s: %v", ErrMalformed, what, err)
}

// Package varint implements bounded, canonical LEB128 integer codecs over
// byte slices for the binary recording format.
//
// Unsigned values use base-128 little-endian groups with a continuation bit.
// Signed values use zigzag mapping onto the unsigned encoding. Decoders accept
// exactly one encoding per value: overlong forms such as 0x80 0x00 are
// rejected. Decoding never allocates and never reads past the input.
package varint

import "errors"

// MaxLen is the longest valid encoding of a uint64 in bytes.
const MaxLen = 10

var (
	// ErrTruncated means the input ended before the value was complete.
	ErrTruncated = errors.New("varint: truncated input")
	// ErrOverflow means the encoding is longer than MaxLen bytes or its value
	// does not fit in 64 bits.
	ErrOverflow = errors.New("varint: value overflows 64 bits")
	// ErrNonCanonical means the value was encoded with redundant trailing
	// zero groups.
	ErrNonCanonical = errors.New("varint: non-canonical encoding")
	// ErrLimit means a decoded value exceeds the caller-supplied maximum.
	ErrLimit = errors.New("varint: value exceeds limit")
)

// AppendUvarint appends the canonical encoding of v to dst.
func AppendUvarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

// AppendSvarint appends the zigzag encoding of v to dst.
func AppendSvarint(dst []byte, v int64) []byte {
	return AppendUvarint(dst, zigzag(v))
}

// Uvarint decodes an unsigned value from the start of b and returns it with
// the number of bytes consumed. On error it returns 0, 0 and one of
// ErrTruncated, ErrOverflow or ErrNonCanonical.
func Uvarint(b []byte) (v uint64, n int, err error) {
	var shift uint
	for i := 0; i < MaxLen; i++ {
		if i >= len(b) {
			return 0, 0, ErrTruncated
		}
		c := b[i]
		if i == MaxLen-1 && c > 1 {
			return 0, 0, ErrOverflow
		}
		v |= uint64(c&0x7f) << shift
		if c < 0x80 {
			if c == 0 && i > 0 {
				return 0, 0, ErrNonCanonical
			}
			return v, i + 1, nil
		}
		shift += 7
	}
	// The tenth byte had its continuation bit set, which implies c > 1.
	return 0, 0, ErrOverflow
}

// Svarint decodes a zigzag-encoded signed value from the start of b. Errors
// are those of Uvarint.
func Svarint(b []byte) (int64, int, error) {
	u, n, err := Uvarint(b)
	if err != nil {
		return 0, 0, err
	}
	return unzigzag(u), n, nil
}

func zigzag(v int64) uint64   { return uint64(v<<1) ^ uint64(v>>63) }
func unzigzag(u uint64) int64 { return int64(u>>1) ^ -int64(u&1) }

// Reader is a bounds-checked cursor over a byte slice. A failed read leaves
// the cursor unchanged. The zero value reads from an empty slice.
type Reader struct {
	buf []byte
	pos int
}

// NewReader returns a Reader positioned at the start of b. The Reader does not
// copy b.
func NewReader(b []byte) *Reader { return &Reader{buf: b} }

// Remaining reports the number of unread bytes.
func (r *Reader) Remaining() int { return len(r.buf) - r.pos }

// Uvarint reads an unsigned varint.
func (r *Reader) Uvarint() (uint64, error) {
	v, n, err := Uvarint(r.buf[r.pos:])
	if err != nil {
		return 0, err
	}
	r.pos += n
	return v, nil
}

// Svarint reads a zigzag signed varint.
func (r *Reader) Svarint() (int64, error) {
	v, n, err := Svarint(r.buf[r.pos:])
	if err != nil {
		return 0, err
	}
	r.pos += n
	return v, nil
}

// UvarintMax reads an unsigned varint and returns ErrLimit, without
// consuming it, if it exceeds max. Use it for lengths and counts read from
// untrusted input before sizing anything from them.
func (r *Reader) UvarintMax(max uint64) (uint64, error) {
	v, n, err := Uvarint(r.buf[r.pos:])
	if err != nil {
		return 0, err
	}
	if v > max {
		return 0, ErrLimit
	}
	r.pos += n
	return v, nil
}

// Byte reads one byte.
func (r *Reader) Byte() (byte, error) {
	if r.pos >= len(r.buf) {
		return 0, ErrTruncated
	}
	c := r.buf[r.pos]
	r.pos++
	return c, nil
}

// Bytes returns the next n bytes as a subslice of the underlying buffer
// without copying. It returns ErrTruncated if n is negative or exceeds
// Remaining.
func (r *Reader) Bytes(n int) ([]byte, error) {
	if n < 0 || n > r.Remaining() {
		return nil, ErrTruncated
	}
	out := r.buf[r.pos : r.pos+n : r.pos+n]
	r.pos += n
	return out, nil
}

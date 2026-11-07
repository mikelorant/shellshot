// Package chunk implements the PNG-style container framing of the binary
// recording format: an 8-byte magic followed by chunks
//
//	len u32 LE | type [4]byte | payload (len bytes) | crc u32 LE
//
// where crc is CRC-32C (Castagnoli) over type || payload, and an optional
// 8-byte footer trailer. See docs/research/binary-format.md section 3.2.
//
// The package knows nothing about chunk contents. It depends on the standard
// library only.
package chunk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
)

// Magic is the 8-byte file signature: "\x89SHOT\r\n\x1a". The high bit catches
// 7-bit stripping, CR LF catches newline translation and ^Z stops DOS `type`.
const Magic = "\x89SHOT\r\n\x1a"

const (
	// HeaderSize is the size of a chunk's len and type fields.
	HeaderSize = 8
	// CRCSize is the size of a chunk's trailing checksum.
	CRCSize = 4
	// Overhead is the number of framing bytes around a payload.
	Overhead = HeaderSize + CRCSize

	// MaxLen is the hard limit on a chunk payload, for writers and readers.
	MaxLen = 64 << 20
	// DefaultMaxLen is the payload cap a Reader applies when Options.MaxLen is 0.
	DefaultMaxLen = 16 << 20

	// TrailerSize is the size of the optional footer trailer.
	TrailerSize = 8
	// TrailerMagic ends the footer trailer.
	TrailerMagic = "SHTE"
)

var (
	// ErrBadMagic means the data does not start with Magic.
	ErrBadMagic = errors.New("chunk: bad magic")
	// ErrTruncated means the data ends inside the magic or inside a chunk (a
	// torn tail). It wraps io.ErrUnexpectedEOF.
	ErrTruncated = fmt.Errorf("chunk: truncated: %w", io.ErrUnexpectedEOF)
	// ErrCorrupt means a chunk's CRC-32C does not match its type and payload.
	ErrCorrupt = errors.New("chunk: crc mismatch")
	// ErrTooLarge means a chunk length exceeds the reader's cap (or MaxLen
	// when writing).
	ErrTooLarge = errors.New("chunk: length exceeds limit")
)

var castagnoli = crc32.MakeTable(crc32.Castagnoli)

// Type is a chunk's four-byte type code. As in PNG, bit 5 (0x20) of the first
// byte is the criticality bit: a lowercase first letter marks an ancillary
// chunk that readers may skip when unknown; an uppercase one marks a critical
// chunk that readers must understand or fail.
type Type [4]byte

// Known chunk types. The on-disk bytes are HEAD DICT EVNT them meta grid indx
// blob.
var (
	TypeHead  = Type{'H', 'E', 'A', 'D'} // critical: file header, exactly one, first
	TypeDict  = Type{'D', 'I', 'C', 'T'} // critical: compression dictionary
	TypeEvent = Type{'E', 'V', 'N', 'T'} // critical: event block
	TypeTheme = Type{'t', 'h', 'e', 'm'} // ancillary: theme
	TypeMeta  = Type{'m', 'e', 't', 'a'} // ancillary: user key/value strings
	TypeGrid  = Type{'g', 'r', 'i', 'd'} // ancillary: derived keyframe
	TypeIndex = Type{'i', 'n', 'd', 'x'} // ancillary: footer index
	TypeBlob  = Type{'b', 'l', 'o', 'b'} // ancillary: attachments (reserved)
)

// Ancillary reports whether a reader may skip the chunk when it does not
// understand the type (bit 5 of the first byte is set).
func (t Type) Ancillary() bool { return t[0]&0x20 != 0 }

// String returns the four type bytes as text, or a quoted form if any byte is
// not printable ASCII.
func (t Type) String() string {
	for _, c := range t {
		if c < 0x20 || c > 0x7e {
			return strconv.Quote(string(t[:]))
		}
	}
	return string(t[:])
}

// WriteMagic writes Magic to w.
func WriteMagic(w io.Writer) error {
	_, err := io.WriteString(w, Magic)
	return err
}

// ReadMagic reads and checks the magic from r. It returns io.EOF if r is
// empty, ErrTruncated if r ends inside the magic, and ErrBadMagic if the bytes
// differ. After success a Reader created on r reports offsets relative to the
// first byte after the magic; add len(Magic) for file offsets.
func ReadMagic(r io.Reader) error {
	var b [len(Magic)]byte
	_, err := io.ReadFull(r, b[:])
	switch {
	case err == io.EOF:
		return io.EOF
	case err == io.ErrUnexpectedEOF:
		return ErrTruncated
	case err != nil:
		return fmt.Errorf("chunk: read magic: %w", err)
	}
	if string(b[:]) != Magic {
		return ErrBadMagic
	}
	return nil
}

// Write writes one chunk to w with a single w.Write call, so that a process
// killed mid-write leaves at most one torn tail chunk and concurrent writers to
// an O_APPEND file are not interleaved within a chunk. It returns ErrTooLarge
// without writing if len(payload) > MaxLen.
func Write(w io.Writer, t Type, payload []byte) error {
	if len(payload) > MaxLen {
		return fmt.Errorf("%w: payload %d bytes, max %d", ErrTooLarge, len(payload), MaxLen)
	}
	buf := make([]byte, Overhead+len(payload))
	binary.LittleEndian.PutUint32(buf, uint32(len(payload)))
	copy(buf[4:], t[:])
	copy(buf[HeaderSize:], payload)
	sum := crc32.Update(0, castagnoli, buf[4:HeaderSize+len(payload)])
	binary.LittleEndian.PutUint32(buf[HeaderSize+len(payload):], sum)
	_, err := w.Write(buf)
	return err
}

// WriteTrailer writes the optional footer trailer: footerChunkTotalLen (the
// size of the whole footer chunk, Overhead + payload length) as u32 LE
// followed by TrailerMagic.
func WriteTrailer(w io.Writer, footerChunkTotalLen uint32) error {
	var b [TrailerSize]byte
	binary.LittleEndian.PutUint32(b[:], footerChunkTotalLen)
	copy(b[4:], TrailerMagic)
	_, err := w.Write(b[:])
	return err
}

// ParseTrailer decodes the last TrailerSize bytes of a file. It reports false
// if they do not end in TrailerMagic or the length is smaller than Overhead
// (no chunk is that short).
func ParseTrailer(b [TrailerSize]byte) (footerChunkTotalLen uint32, ok bool) {
	if string(b[4:]) != TrailerMagic {
		return 0, false
	}
	n := binary.LittleEndian.Uint32(b[:4])
	if n < Overhead {
		return 0, false
	}
	return n, true
}

// Options configures a Reader.
type Options struct {
	// MaxLen is the largest payload length the reader accepts. Zero means
	// DefaultMaxLen; values above the hard limit MaxLen are lowered to it.
	MaxLen uint32
}

// Reader reads chunks sequentially from an io.Reader positioned just after the
// magic. It is not safe for concurrent use. Errors are sticky: once Next
// returns an error, it keeps returning that error.
type Reader struct {
	r      io.Reader
	maxLen uint32
	buf    []byte // payload storage; reused between calls
	off    int64  // offset of the next chunk
	err    error
}

// NewReader returns a Reader reading chunks from r.
func NewReader(r io.Reader, opts Options) *Reader {
	m := opts.MaxLen
	if m == 0 {
		m = DefaultMaxLen
	}
	if m > MaxLen {
		m = MaxLen
	}
	return &Reader{r: r, maxLen: m}
}

// Offset returns the offset, counted from the first byte given to NewReader, of
// the next chunk to be read: the end of the last chunk Next returned
// successfully. It does not advance past a chunk that fails to read.
func (r *Reader) Offset() int64 { return r.off }

// LastGoodOffset returns the offset at which valid data ends. It equals
// Offset; after Next returns an error it is where a recovering writer should
// truncate the file to discard the torn or corrupt tail. Add len(Magic) for the
// file offset if the magic was read first.
func (r *Reader) LastGoodOffset() int64 { return r.off }

// minGrow is the first and smallest step by which the payload buffer grows.
const minGrow = 32 << 10

// Next returns the next chunk. The payload is only valid until the next call
// to Next, which reuses its backing array; use bytes.Clone (or slices.Clone) to
// keep it. A zero-length payload is returned as a non-nil empty slice.
//
// Next returns io.EOF only at a clean chunk boundary; ErrTruncated if the data
// ends inside a chunk; ErrCorrupt if the CRC does not match; and ErrTooLarge if
// the declared length exceeds the cap, before any payload is read or allocated.
// Other errors from the underlying reader are returned wrapped. Memory use
// grows with the bytes actually read, not with the declared length, so a lying
// length on a short input does not allocate the claimed size.
func (r *Reader) Next() (Type, []byte, error) {
	var t Type
	if r.err != nil {
		return t, nil, r.err
	}
	t, payload, err := r.next()
	if err != nil {
		r.err = err
		return Type{}, nil, err
	}
	return t, payload, nil
}

func (r *Reader) next() (Type, []byte, error) {
	var t Type
	var hdr [HeaderSize]byte
	if _, err := io.ReadFull(r.r, hdr[:]); err != nil {
		return t, nil, r.mapReadErr(err, true)
	}
	n := binary.LittleEndian.Uint32(hdr[:4])
	copy(t[:], hdr[4:])
	if n > r.maxLen {
		return t, nil, fmt.Errorf("%w: chunk %s declares %d bytes, cap %d", ErrTooLarge, t, n, r.maxLen)
	}
	size := int(n)
	have := 0
	for have < size {
		if have == len(r.buf) {
			grown := make([]byte, min(size, max(2*len(r.buf), minGrow)))
			copy(grown, r.buf[:have])
			r.buf = grown
		}
		m, err := io.ReadFull(r.r, r.buf[have:min(len(r.buf), size)])
		have += m
		if err != nil {
			return t, nil, r.mapReadErr(err, false)
		}
	}
	var tail [CRCSize]byte
	if _, err := io.ReadFull(r.r, tail[:]); err != nil {
		return t, nil, r.mapReadErr(err, false)
	}
	payload := r.buf[:size:size]
	sum := crc32.Update(0, castagnoli, t[:])
	sum = crc32.Update(sum, castagnoli, payload)
	if sum != binary.LittleEndian.Uint32(tail[:]) {
		return t, nil, fmt.Errorf("%w: chunk %s at offset %d", ErrCorrupt, t, r.off)
	}
	r.off += int64(Overhead + size)
	if payload == nil {
		payload = []byte{}
	}
	return t, payload, nil
}

// mapReadErr converts an io.ReadFull error. atBoundary is true when no byte of
// the chunk has been consumed yet, where io.EOF is a clean end.
func (r *Reader) mapReadErr(err error, atBoundary bool) error {
	switch {
	case err == io.EOF && atBoundary:
		return io.EOF
	case err == io.EOF, err == io.ErrUnexpectedEOF:
		return fmt.Errorf("%w at offset %d", ErrTruncated, r.off)
	default:
		return fmt.Errorf("chunk: read at offset %d: %w", r.off, err)
	}
}

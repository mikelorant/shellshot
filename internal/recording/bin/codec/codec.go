// Package codec defines the block compression interface used by the binary
// recording format, with a small registry and the none, flate (stdlib) and zstd
// (github.com/klauspost/compress) codecs. The wire ids are fixed by the
// format: 0 none, 1 flate, 2 zstd.
package codec

import "errors"

// ID is the codec identifier stored in the file (a uvarint on the wire).
type ID uint64

const (
	// None stores bytes unchanged.
	None ID = 0
	// Flate is raw DEFLATE (RFC 1951) from the standard library.
	Flate ID = 1
	// Zstd is zstd via github.com/klauspost/compress/zstd.
	Zstd ID = 2
)

// String returns a short name for diagnostics.
func (id ID) String() string {
	switch id {
	case None:
		return "none"
	case Flate:
		return "flate"
	case Zstd:
		return "zstd"
	default:
		return "unknown"
	}
}

var (
	// ErrTooLarge is returned by Decompress when the output would exceed maxLen.
	ErrTooLarge = errors.New("codec: decompressed data exceeds maxLen")
	// ErrCorrupt is returned by Decompress when the input is not a valid stream.
	ErrCorrupt = errors.New("codec: corrupt compressed data")
	// ErrUnknownCodec is returned by Registry.Get for an unregistered id.
	ErrUnknownCodec = errors.New("codec: unknown codec id")
	// ErrDuplicateCodec is returned by Registry.Register when the id is taken.
	ErrDuplicateCodec = errors.New("codec: codec id already registered")
)

// Codec compresses and decompresses independent blocks.
//
// Both methods append to dst (which may be nil) and return the extended slice,
// like strconv.AppendInt. dict is an optional preset dictionary (nil or empty
// means none); the same dict must be supplied to Decompress as to Compress.
// Implementations must be safe for concurrent use.
type Codec interface {
	ID() ID
	Compress(dst, src, dict []byte) ([]byte, error)
	// Decompress appends at most maxLen decompressed bytes to dst. If the
	// stream would produce more, it returns ErrTooLarge; it never buffers
	// more than about maxLen bytes beyond what dst already holds. A negative
	// maxLen is an error. Invalid input yields an error wrapping ErrCorrupt.
	Decompress(dst, src, dict []byte, maxLen int) ([]byte, error)
}

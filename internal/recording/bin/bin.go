// Package bin reads and writes the binary recording format described in
// docs/research/binary-format.md: a PNG-style chunk file holding a HEAD chunk,
// optional DICT, them and meta chunks, and a log of EVNT blocks of timestamped
// events.
//
// It composes the sub-packages chunk (framing and CRC), head (HEAD, them and
// meta payloads), codec (block compression) and event (columnar block
// encoding). Everything is pure Go.
//
// Crash safety: the Writer emits each chunk with a single Write call, so a
// process killed mid-write leaves at most one torn tail chunk. The Reader
// returns every event of the fully valid blocks and then reports ErrTruncated
// together with ValidBytes, the offset at which valid data ends.
//
// Idle costs nothing: the Writer writes no bytes unless events are pending, and
// the gap between two events lives in the later event's timestamp.
//
// Neither Writer nor Reader is safe for concurrent use.
package bin

import (
	"errors"
	"fmt"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin/chunk"
	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/varint"
)

var (
	// ErrTruncated means the data ends inside a chunk or the last chunk fails
	// its CRC: a torn tail left by a crash. Everything before it was valid. It
	// is the same value as chunk.ErrTruncated and wraps io.ErrUnexpectedEOF.
	ErrTruncated = chunk.ErrTruncated
	// ErrBadMagic means the data does not start with chunk.Magic.
	ErrBadMagic = chunk.ErrBadMagic
	// ErrCorrupt means a chunk that is followed by more data fails its CRC, or
	// a chunk with a valid CRC has malformed content.
	ErrCorrupt = errors.New("bin: corrupt recording")
	// ErrUnsupported means the file needs a feature this reader lacks: an
	// unknown critical chunk, a reserved event kind, an unregistered codec or
	// an unknown required feature.
	ErrUnsupported = errors.New("bin: unsupported recording")
	// ErrLimit means a chunk or block exceeds the configured reader limits.
	ErrLimit = errors.New("bin: recording exceeds limit")

	// ErrClosed is returned by Writer methods after Close.
	ErrClosed = errors.New("bin: writer closed")
	// ErrInputNotRecorded is returned by Writer.Input (and WriteEvent with
	// event.KindIn) when head.FlagInputRecorded is not set in the HEAD flags.
	ErrInputNotRecorded = errors.New("bin: input recording not enabled in head flags")
	// ErrEventTooLarge is returned by Writer.WriteEvent for a payload larger
	// than MaxEventData.
	ErrEventTooLarge = errors.New("bin: event payload too large")
	// ErrNotSeekable is returned by Reader.Seek on a Reader made by NewReader.
	ErrNotSeekable = errors.New("bin: reader is not seekable; use OpenAt")
)

// Event is one decoded event.
type Event struct {
	// At is the time since recording start, a multiple of the HEAD tick.
	At   time.Duration
	Kind event.Kind
	// Data is the kind-specific payload (nil when empty). Unless
	// ReaderOptions.CopyData is set it is only valid until the next call to
	// Reader.Next.
	Data []byte
}

// Limits on writer inputs, chosen so that every block a Writer produces fits
// the default reader limits.
const (
	// MaxFlushBytes is the largest accepted WriterOptions.FlushBytes.
	MaxFlushBytes = 8 << 20
	// MaxEventData is the largest payload WriteEvent accepts.
	MaxEventData = 8 << 20
)

// appendDictPayload appends a DICT chunk payload: dict_id uvarint, codec
// uvarint, the dictionary bytes.
func appendDictPayload(dst []byte, id uint64, c codec.ID, dict []byte) []byte {
	dst = varint.AppendUvarint(dst, id)
	dst = varint.AppendUvarint(dst, uint64(c))
	return append(dst, dict...)
}

func parseDictPayload(p []byte) (id uint64, c codec.ID, dict []byte, err error) {
	r := varint.NewReader(p)
	id, err = r.Uvarint()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("%w: dict id: %w", ErrCorrupt, err)
	}
	cid, err := r.Uvarint()
	if err != nil {
		return 0, 0, nil, fmt.Errorf("%w: dict codec: %w", ErrCorrupt, err)
	}
	dict, _ = r.Bytes(r.Remaining())
	if id == 0 || len(dict) == 0 {
		return 0, 0, nil, fmt.Errorf("%w: dictionary with id %d and %d bytes", ErrCorrupt, id, len(dict))
	}
	return id, codec.ID(cid), dict, nil
}

// ticksToDuration converts ticks of tickNS nanoseconds to a Duration,
// saturating at the largest Duration.
func ticksToDuration(ticks, tickNS uint64) time.Duration {
	if tickNS != 0 && ticks > uint64(1<<63-1)/tickNS {
		return time.Duration(1<<63 - 1)
	}
	return time.Duration(ticks * tickNS)
}

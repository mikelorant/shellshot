package bin

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"maps"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin/chunk"
	"github.com/michaellorant/shellshot/internal/recording/bin/codec"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
	"github.com/michaellorant/shellshot/internal/recording/bin/varint"
)

// ReaderOptions configures a Reader. The zero value is ready to use.
type ReaderOptions struct {
	// CopyData makes Next return Event.Data slices that stay valid after later
	// calls. Without it Data is only valid until the next call to Next.
	CopyData bool
	// Registry resolves block codecs. Nil means codec.DefaultRegistry().
	Registry *codec.Registry
	// MaxChunkLen caps a chunk payload. Zero means chunk.DefaultMaxLen.
	MaxChunkLen uint32
	// Limits bounds event counts and decompressed sizes of blocks. Zero fields
	// mean the defaults of package event.
	Limits event.Limits
}

// Reader reads a recording. Create one with NewReader for sequential access or
// OpenAt for sequential access plus Seek.
//
// Reading stops at the first problem: Next returns the events of all valid
// blocks before it and then one error that stays returned (until Seek):
//
//   - io.EOF at a clean end;
//   - ErrTruncated if the file ends inside a chunk or the last chunk fails its
//     CRC (a torn tail, as left by a crash); ValidBytes then gives the offset of
//     the end of the last good chunk;
//   - ErrCorrupt if a chunk that is followed by more data fails its CRC or is
//     malformed. A corrupted length field cannot be told from a torn tail, so
//     it may also surface as ErrTruncated;
//   - ErrUnsupported for an unknown critical chunk or a reserved event kind.
//
// A Reader is not safe for concurrent use.
type Reader struct {
	opts ReaderOptions
	reg  *codec.Registry

	hd     head.Head
	tickNS uint64
	theme  *head.Theme
	meta   map[string]string
	dicts  map[uint64][]byte

	br    *bufio.Reader
	cr    *chunk.Reader
	base  int64 // file offset that cr's offset 0 corresponds to
	valid int64 // file offset of the end of the last fully processed chunk
	// scanStart is the file offset where the preamble ends and the block table
	// scan begins.
	scanStart int64

	// first is the first EVNT chunk, read while looking for the end of the
	// preamble, waiting to be decoded.
	first *rawChunk
	// deferred is the error that ended the preamble; Next returns it once the
	// events before it are delivered.
	deferred error
	err      error // sticky

	block    []event.Event
	blockPos int
	minTick  uint64 // Next skips events before this tick (after Seek)
	lastTick uint64 // end tick of the last block decoded
	nBlocks  int

	ra    io.ReaderAt // nil for NewReader
	size  int64
	table *blockTable
}

type rawChunk struct {
	typ     chunk.Type
	payload []byte
	end     int64 // file offset just past the chunk
}

// NewReader reads the magic, the HEAD chunk and any DICT, them and meta chunks
// that precede the first event block from r. It fails with ErrBadMagic,
// ErrTruncated (including an empty r) or ErrCorrupt if the file start is
// unusable, and with ErrUnsupported or a wrapped head error for a HEAD this
// package cannot read. A torn or corrupt tail after HEAD does not fail
// NewReader; Next reports it after the valid events.
func NewReader(r io.Reader, o ReaderOptions) (*Reader, error) {
	return newReader(r, nil, 0, o)
}

// OpenAt is like NewReader for a file of size bytes and additionally supports
// Seek and Duration. It reads r with ReadAt only.
func OpenAt(r io.ReaderAt, size int64, o ReaderOptions) (*Reader, error) {
	if size < 0 {
		return nil, errors.New("bin: negative size")
	}
	return newReader(io.NewSectionReader(r, 0, size), r, size, o)
}

func newReader(src io.Reader, ra io.ReaderAt, size int64, o ReaderOptions) (*Reader, error) {
	rd := &Reader{opts: o, reg: o.Registry, dicts: map[uint64][]byte{}, ra: ra, size: size}
	if rd.reg == nil {
		rd.reg = codec.DefaultRegistry()
	}
	rd.resetAt(src, int64(len(chunk.Magic)))
	if err := chunk.ReadMagic(rd.br); err != nil {
		if err == io.EOF {
			return nil, fmt.Errorf("%w: empty input", ErrTruncated)
		}
		return nil, err
	}
	rd.valid = int64(len(chunk.Magic))
	if err := rd.readPreamble(); err != nil {
		return nil, err
	}
	rd.scanStart = rd.valid
	return rd, nil
}

// resetAt makes the chunk reader read chunks from src, whose first byte is at
// file offset off.
func (r *Reader) resetAt(src io.Reader, off int64) {
	r.br = bufio.NewReaderSize(src, 32<<10)
	r.cr = chunk.NewReader(r.br, chunk.Options{MaxLen: r.opts.MaxChunkLen})
	r.base = off
}

// readChunk returns the next chunk, mapping chunk errors to this package's.
func (r *Reader) readChunk() (*rawChunk, error) {
	t, payload, err := r.cr.Next()
	if err == nil {
		return &rawChunk{typ: t, payload: payload, end: r.base + r.cr.Offset()}, nil
	}
	switch {
	case err == io.EOF:
		return nil, io.EOF
	case errors.Is(err, chunk.ErrTruncated):
		return nil, fmt.Errorf("%w: valid data ends at offset %d", ErrTruncated, r.valid)
	case errors.Is(err, chunk.ErrCorrupt):
		// A bad CRC on the very last chunk is a torn write; anywhere else it
		// is corruption.
		if _, perr := r.br.Peek(1); perr == io.EOF {
			return nil, fmt.Errorf("%w: last chunk fails its CRC; valid data ends at offset %d", ErrTruncated, r.valid)
		}
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	case errors.Is(err, chunk.ErrTooLarge):
		return nil, fmt.Errorf("%w: %w", ErrCorrupt, err)
	}
	return nil, err
}

func (r *Reader) readPreamble() error {
	c, err := r.readChunk()
	if err != nil {
		if err == io.EOF {
			return fmt.Errorf("%w: no HEAD chunk", ErrTruncated)
		}
		return err
	}
	if c.typ != chunk.TypeHead {
		return fmt.Errorf("%w: first chunk is %s, want HEAD", ErrCorrupt, c.typ)
	}
	hd, err := head.Unmarshal(c.payload)
	if err != nil {
		return headError(err)
	}
	r.hd = hd
	r.tickNS = hd.TickNS
	r.valid = c.end

	for {
		c, err := r.readChunk()
		if err != nil {
			r.deferred = err
			return nil
		}
		switch c.typ {
		case chunk.TypeEvent:
			c.payload = bytes.Clone(c.payload)
			r.first = c
			return nil
		case chunk.TypeTheme:
			if r.theme == nil {
				// Ancillary chunks with bad content are dropped, not fatal.
				if t, err := head.UnmarshalTheme(c.payload); err == nil {
					r.theme = &t
				}
			}
		case chunk.TypeMeta:
			if r.meta == nil {
				if m, err := head.UnmarshalMeta(c.payload); err == nil {
					r.meta = m
				}
			}
		default:
			if err := r.handleOther(c); err != nil {
				r.deferred = err
				return nil
			}
		}
		r.valid = c.end
	}
}

// handleOther processes a chunk that is neither EVNT nor a preamble chunk.
func (r *Reader) handleOther(c *rawChunk) error {
	switch c.typ {
	case chunk.TypeHead:
		return fmt.Errorf("%w: second HEAD chunk", ErrCorrupt)
	case chunk.TypeDict:
		id, _, dict, err := parseDictPayload(c.payload)
		if err != nil {
			return err
		}
		if old, dup := r.dicts[id]; dup && !bytes.Equal(old, dict) {
			return fmt.Errorf("%w: dictionary %d redefined", ErrCorrupt, id)
		}
		r.dicts[id] = bytes.Clone(dict)
		return nil
	}
	if c.typ.Ancillary() {
		return nil
	}
	return fmt.Errorf("%w: critical chunk %s", ErrUnsupported, c.typ)
}

func headError(err error) error {
	switch {
	case errors.Is(err, head.ErrUnsupported):
		return fmt.Errorf("%w: %w", ErrUnsupported, err)
	case errors.Is(err, head.ErrLimit):
		return fmt.Errorf("%w: %w", ErrLimit, err)
	}
	return fmt.Errorf("%w: HEAD: %w", ErrCorrupt, err)
}

// Head returns the decoded HEAD chunk.
func (r *Reader) Head() head.Head { return r.hd }

// Theme returns the them chunk that preceded the first event, or nil if there
// was none.
func (r *Reader) Theme() *head.Theme {
	if r.theme == nil {
		return nil
	}
	t := *r.theme
	return &t
}

// Meta returns the meta key/value pairs that preceded the first event, or nil.
func (r *Reader) Meta() map[string]string { return maps.Clone(r.meta) }

// ValidBytes returns the file offset of the end of the last chunk that was
// read and validated. After Next returns ErrTruncated it is where a recovering
// writer should truncate the file.
func (r *Reader) ValidBytes() int64 { return r.valid }

// Next returns the next event in order across blocks. It returns io.EOF at a
// clean end and otherwise one of the errors listed on Reader, which then stay
// returned. Event.Data is valid until the next call unless
// ReaderOptions.CopyData is set.
func (r *Reader) Next() (Event, error) {
	for {
		for r.blockPos < len(r.block) {
			e := r.block[r.blockPos]
			r.blockPos++
			if e.Tick < r.minTick {
				continue
			}
			ev := Event{At: ticksToDuration(e.Tick, r.tickNS), Kind: e.Kind, Data: e.Data}
			if r.opts.CopyData && ev.Data != nil {
				ev.Data = bytes.Clone(ev.Data)
			}
			return ev, nil
		}
		if r.err != nil {
			return Event{}, r.err
		}
		if err := r.advance(); err != nil {
			r.err = err
			return Event{}, err
		}
	}
}

// advance decodes the next EVNT block into r.block, processing other chunks
// on the way.
func (r *Reader) advance() error {
	for {
		var c *rawChunk
		if r.first != nil {
			c, r.first = r.first, nil
		} else if r.deferred != nil {
			return r.deferred
		} else {
			var err error
			if c, err = r.readChunk(); err != nil {
				return err
			}
		}
		if c.typ != chunk.TypeEvent {
			if err := r.handleOther(c); err != nil {
				return err
			}
			r.valid = c.end
			continue
		}
		b, err := event.Decode(c.payload, r.reg, r.lookupDict, r.opts.Limits)
		if err != nil {
			return blockError(err)
		}
		if r.nBlocks > 0 && b.StartTick < r.lastTick {
			return fmt.Errorf("%w: block starts at tick %d before previous block end %d", ErrCorrupt, b.StartTick, r.lastTick)
		}
		r.nBlocks++
		r.lastTick = b.EndTick
		r.block, r.blockPos = b.Events, 0
		r.valid = c.end
		return nil
	}
}

func (r *Reader) lookupDict(id uint64) ([]byte, bool) {
	d, ok := r.dicts[id]
	return d, ok
}

func blockError(err error) error {
	switch {
	case errors.Is(err, event.ErrUnsupported), errors.Is(err, codec.ErrUnknownCodec):
		return fmt.Errorf("%w: %w", ErrUnsupported, err)
	case errors.Is(err, event.ErrLimit), errors.Is(err, codec.ErrTooLarge):
		return fmt.Errorf("%w: %w", ErrLimit, err)
	}
	return fmt.Errorf("%w: %w", ErrCorrupt, err)
}

// Duration returns the time of the last event of the recording. For a Reader
// made by OpenAt it builds the block table if needed (reading chunk headers
// only) and covers every block the table scan could reach. For a Reader made by
// NewReader it covers the blocks decoded so far.
func (r *Reader) Duration() (time.Duration, error) {
	if r.ra == nil {
		return ticksToDuration(r.lastTick, r.tickNS), nil
	}
	t, err := r.buildTable()
	if err != nil {
		return 0, err
	}
	var end uint64
	if n := len(t.blocks); n > 0 {
		end = t.blocks[n-1].endTick
	}
	return ticksToDuration(end, r.tickNS), nil
}

// blockTable lists the EVNT chunks of a file, found by reading chunk headers
// without decompressing.
type blockTable struct {
	blocks []blockEntry
	// end is the file offset where the scan stopped: the end of the file, a
	// torn tail, or an unknown critical chunk.
	end int64
}

type blockEntry struct {
	offset             int64 // file offset of the chunk
	startTick, endTick uint64
}

// maxHeaderScan is the most bytes of an EVNT payload the scan reads: six
// uvarints.
const maxHeaderScan = 6 * varint.MaxLen

func (r *Reader) buildTable() (*blockTable, error) {
	if r.table != nil {
		return r.table, nil
	}
	t := &blockTable{}
	off := r.scanStart
	var prevEnd uint64
	maxLen := int64(r.opts.MaxChunkLen)
	if maxLen == 0 {
		maxLen = chunk.DefaultMaxLen
	}
	for {
		var hdr [chunk.HeaderSize]byte
		n, err := r.ra.ReadAt(hdr[:], off)
		if n < len(hdr) {
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("bin: scan: %w", err)
			}
			break // end of file or torn chunk header
		}
		plen := int64(uint32(hdr[0]) | uint32(hdr[1])<<8 | uint32(hdr[2])<<16 | uint32(hdr[3])<<24)
		var typ chunk.Type
		copy(typ[:], hdr[4:])
		total := chunk.Overhead + plen
		if plen > maxLen || off+total > r.size {
			break
		}
		switch {
		case typ == chunk.TypeEvent:
			buf := make([]byte, min(plen, maxHeaderScan))
			if _, err := r.ra.ReadAt(buf, off+chunk.HeaderSize); err != nil && err != io.EOF {
				return nil, fmt.Errorf("bin: scan: %w", err)
			}
			h, _, err := event.ReadHeader(buf)
			if err != nil || h.StartTick < prevEnd {
				r.table = t
				t.end = off
				return t, nil
			}
			prevEnd = h.EndTick
			t.blocks = append(t.blocks, blockEntry{offset: off, startTick: h.StartTick, endTick: h.EndTick})
		case typ == chunk.TypeDict:
			sub := chunk.NewReader(io.NewSectionReader(r.ra, off, total), chunk.Options{MaxLen: r.opts.MaxChunkLen})
			_, payload, err := sub.Next()
			if err != nil {
				r.table = t
				t.end = off
				return t, nil
			}
			if id, _, dict, err := parseDictPayload(payload); err == nil {
				if _, dup := r.dicts[id]; !dup {
					r.dicts[id] = bytes.Clone(dict)
				}
			}
		case typ == chunk.TypeHead, !typ.Ancillary():
			r.table = t
			t.end = off
			return t, nil
		}
		off += total
	}
	t.end = off
	r.table = t
	return t, nil
}

// Seek positions the Reader so that Next returns events with At >= t, starting
// from the block that contains t. Only Readers made by OpenAt can seek; others
// return ErrNotSeekable. The first Seek builds a block table by reading chunk
// headers only, without decompressing or checksumming any block, so it is cheap
// on large files; a Seek past the last block positions at the end of the
// scanned region, where Next reports whatever ends the file (io.EOF, or
// ErrTruncated for a torn tail). Seek clears any earlier read error.
//
// Events are raw terminal output: replaying them to rebuild screen state must
// start at the beginning of the recording unless keyframes exist. Seek is for
// event tooling, such as listing marks after a time.
func (r *Reader) Seek(t time.Duration) error {
	if r.ra == nil {
		return ErrNotSeekable
	}
	tab, err := r.buildTable()
	if err != nil {
		return err
	}
	var want uint64
	if t > 0 {
		want = uint64(t) / r.tickNS
		if uint64(t)%r.tickNS != 0 {
			want++
		}
	}
	off := tab.end
	for _, b := range tab.blocks {
		if b.endTick >= want {
			off = b.offset
			break
		}
	}
	r.resetAt(io.NewSectionReader(r.ra, off, r.size-off), off)
	r.first, r.deferred, r.err = nil, nil, nil
	r.block, r.blockPos = nil, 0
	r.minTick = want
	r.valid = off
	r.nBlocks = 0
	return nil
}

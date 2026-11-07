package codec

import (
	"bytes"
	"errors"
	"fmt"
	"hash/crc32"
	"hash/maphash"
	"sync"

	"github.com/klauspost/compress/zstd"
)

// maxZstdDecodeMemory is the hard ceiling on the output of a single Decompress
// call, applied through zstd.WithDecoderMaxMemory and zstd.WithDecoderMaxWindow
// on top of the per-call maxLen. A larger maxLen is still accepted, but output
// beyond this ceiling fails with ErrTooLarge.
const maxZstdDecodeMemory = 256 << 20

// zstdMaxExpansionShift is log2 of the largest expansion a zstd frame can
// achieve: every block is at least 4 compressed bytes (3 header bytes plus one
// RLE byte) and decodes to at most 128 KiB, so output <= input<<15. Decompress
// uses it to size its output buffer from the input instead of trusting maxLen
// or a declared frame content size.
const zstdMaxExpansionShift = 15

// maxZstdDictCacheEntries bounds the per-dictionary encoder/decoder cache.
// When full the cache is dropped and rebuilt on demand.
const maxZstdDictCacheEntries = 16

// zstdDictMagic is the little-endian magic number that starts a dictionary in
// the zstd dictionary format (as produced by `zstd --train` or BuildZstdDict).
var zstdDictMagic = []byte{0x37, 0xA4, 0x30, 0xEC}

// ZstdOptions configures a ZstdCodec.
type ZstdOptions struct {
	// Level is the encoder level. The zero value selects zstd.SpeedDefault.
	Level zstd.EncoderLevel
}

// ZstdCodec is zstd via github.com/klauspost/compress/zstd. Every block is
// an independent zstd frame (EncodeAll/DecodeAll, no streaming state), so a
// block can be decoded on its own, in any order, and damage stays inside it.
//
// Frames carry the content size and a 32-bit content checksum, so bit flips
// are reported as ErrCorrupt. An empty input compresses to a small valid
// empty frame; an empty compressed input is invalid.
//
// Dictionaries: the dict argument is opaque bytes. If it starts with the zstd
// dictionary magic it is loaded as a formatted dictionary (its id is stored in
// each frame, so a wrong dictionary is detected and reported as ErrCorrupt).
// Otherwise it is raw content (for example from BuildDict) and is registered
// under an id derived from its CRC-32 and stored in each frame; a different
// raw dictionary has a different id and is likewise rejected. The encoder may
// decide not to use the dictionary for a particular block (for example very
// small inputs); such a frame decodes with any dictionary or none, so wrong
// dictionary detection is best effort and callers needing certainty must
// checksum the decompressed output.
//
// A ZstdCodec is safe for concurrent use. Encoders and decoders are expensive
// so one pair is cached per distinct dictionary content (up to 16 at a time)
// and shared by all callers; dictionary bytes are compared, never aliased, so
// callers may reuse or mutate their slice after a call returns.
type ZstdCodec struct {
	level zstd.EncoderLevel
	plain *zstdState // no dictionary

	mu    sync.Mutex
	dicts map[uint64]*zstdState
	seed  maphash.Seed
}

// zstdState holds the lazily built encoder and decoder for one dictionary.
type zstdState struct {
	dict []byte // private copy; nil for the no-dictionary state

	encOnce sync.Once
	enc     *zstd.Encoder
	encErr  error

	decOnce sync.Once
	dec     *zstd.Decoder
	decErr  error
}

// NewZstdCodec validates opts and returns a ZstdCodec.
func NewZstdCodec(opts ZstdOptions) (*ZstdCodec, error) {
	level := opts.Level
	if level == 0 {
		level = zstd.SpeedDefault
	}
	if _, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(level)); err != nil {
		return nil, fmt.Errorf("codec: invalid zstd level %d: %w", level, err)
	}
	return &ZstdCodec{
		level: level,
		plain: &zstdState{},
		dicts: make(map[uint64]*zstdState),
		seed:  maphash.MakeSeed(),
	}, nil
}

// ID returns Zstd.
func (*ZstdCodec) ID() ID { return Zstd }

// state returns the cached state for dict.
func (c *ZstdCodec) state(dict []byte) *zstdState {
	if len(dict) == 0 {
		return c.plain
	}
	h := maphash.Bytes(c.seed, dict)
	c.mu.Lock()
	defer c.mu.Unlock()
	if s, ok := c.dicts[h]; ok && bytes.Equal(s.dict, dict) {
		return s
	}
	if len(c.dicts) >= maxZstdDictCacheEntries {
		clear(c.dicts)
	}
	s := &zstdState{dict: bytes.Clone(dict)}
	c.dicts[h] = s
	return s
}

// zstdRawDictID derives the frame dictionary id for a raw-content dictionary.
// Ids at or above 1<<31 are free for local use in the zstd format.
func zstdRawDictID(content []byte) uint32 {
	return crc32.ChecksumIEEE(content) | 1<<31
}

func isFormattedZstdDict(dict []byte) bool {
	return bytes.HasPrefix(dict, zstdDictMagic)
}

func (s *zstdState) encoder(level zstd.EncoderLevel) (*zstd.Encoder, error) {
	s.encOnce.Do(func() {
		opts := []zstd.EOption{
			zstd.WithEncoderLevel(level),
			zstd.WithZeroFrames(true),
		}
		switch {
		case len(s.dict) == 0:
		case isFormattedZstdDict(s.dict):
			opts = append(opts, zstd.WithEncoderDict(s.dict))
		default:
			opts = append(opts, zstd.WithEncoderDictRaw(zstdRawDictID(s.dict), s.dict))
		}
		s.enc, s.encErr = zstd.NewWriter(nil, opts...)
		if s.encErr != nil {
			s.encErr = fmt.Errorf("codec: invalid zstd dictionary: %w", s.encErr)
		}
	})
	return s.enc, s.encErr
}

func (s *zstdState) decoder() (*zstd.Decoder, error) {
	s.decOnce.Do(func() {
		opts := []zstd.DOption{
			zstd.WithDecoderMaxMemory(maxZstdDecodeMemory),
			zstd.WithDecoderMaxWindow(maxZstdDecodeMemory),
			zstd.WithDecodeAllCapLimit(true),
		}
		switch {
		case len(s.dict) == 0:
		case isFormattedZstdDict(s.dict):
			opts = append(opts, zstd.WithDecoderDicts(s.dict))
		default:
			opts = append(opts, zstd.WithDecoderDictRaw(zstdRawDictID(s.dict), s.dict))
		}
		s.dec, s.decErr = zstd.NewReader(nil, opts...)
		if s.decErr != nil {
			s.decErr = fmt.Errorf("codec: invalid zstd dictionary: %w", s.decErr)
		}
	})
	return s.dec, s.decErr
}

// Compress appends one independent zstd frame encoding src to dst.
func (c *ZstdCodec) Compress(dst, src, dict []byte) (out []byte, err error) {
	enc, err := c.state(dict).encoder(c.level)
	if err != nil {
		return dst, err
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = dst, fmt.Errorf("codec: zstd encoder panic: %v", r)
		}
	}()
	return enc.EncodeAll(src, dst), nil
}

// Decompress appends the decoded src to dst, producing at most maxLen bytes.
//
// Memory is bounded three ways: a frame whose declared content size exceeds
// maxLen is rejected before any allocation; otherwise the output buffer is
// sized to the declared size (or maxLen+1 when the frame does not declare
// one) and the decoder is limited to that capacity
// (zstd.WithDecodeAllCapLimit). That size is further capped at len(src)<<15,
// the most a zstd frame can expand, so a tiny hostile frame with a huge
// maxLen or lying content size cannot force a large allocation. There is a
// hard ceiling of 256 MiB per call
// (zstd.WithDecoderMaxMemory); finally the result length is checked against
// maxLen explicitly. Any overflow returns ErrTooLarge.
//
// src must be one frame as produced by Compress (further concatenated frames
// are decoded only while they fit the capacity sized from the first). Empty,
// truncated, damaged or unknown-dictionary input returns an error wrapping
// ErrCorrupt.
func (c *ZstdCodec) Decompress(dst, src, dict []byte, maxLen int) (out []byte, err error) {
	if maxLen < 0 {
		return dst, errNegativeMaxLen
	}
	if len(src) == 0 {
		return dst, fmt.Errorf("%w: empty input", ErrCorrupt)
	}
	dec, err := c.state(dict).decoder()
	if err != nil {
		return dst, err
	}
	defer func() {
		if r := recover(); r != nil {
			out, err = dst, fmt.Errorf("%w: zstd decoder panic: %v", ErrCorrupt, r)
		}
	}()

	// Bytes of spare capacity the decoder may use. maxLen+1 proves overflow.
	want := min(maxLen, maxZstdDecodeMemory) + 1
	var h zstd.Header
	if err := h.Decode(src); err != nil {
		return dst, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if h.HasFCS {
		if h.FrameContentSize > uint64(maxLen) {
			return dst, fmt.Errorf("%w: frame declares %d bytes, maxLen %d", ErrTooLarge, h.FrameContentSize, maxLen)
		}
		want = min(want, int(h.FrameContentSize))
	}
	// Neither maxLen nor the declared size is trusted for allocation: a few
	// input bytes cannot expand past the format's maximum ratio.
	if len(src) <= want>>zstdMaxExpansionShift {
		want = len(src) << zstdMaxExpansionShift
	}
	start := len(dst)
	buf := dst
	if cap(buf)-len(buf) < want {
		buf = make([]byte, len(dst), len(dst)+want)
		copy(buf, dst)
	}
	res, err := dec.DecodeAll(src, buf)
	if err != nil {
		if errors.Is(err, zstd.ErrDecoderSizeExceeded) || errors.Is(err, zstd.ErrWindowSizeExceeded) {
			return dst, fmt.Errorf("%w: %v", ErrTooLarge, err)
		}
		return dst, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if len(res)-start > maxLen {
		return dst, ErrTooLarge
	}
	return res, nil
}

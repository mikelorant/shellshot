package emoji

import (
	"fmt"
	"image"
	"sync"
	"sync/atomic"

	"github.com/klauspost/compress/zstd"
)

// Set names a built-in chain of colour emoji fonts.
type Set int

const (
	// SetApple draws the Apple Color Emoji derivative first and falls back
	// to Noto Color Emoji for what Apple's lacks (Unicode 18 emoji and the
	// gender and medical symbols with U+FE0F). The artwork belongs to Apple
	// Inc. and is not open source.
	SetApple Set = iota
	// SetNoto draws Noto Color Emoji only (SIL OFL 1.1): the choice for
	// images you want to share without Apple's artwork in them.
	SetNoto
)

// String returns the set's flag value ("apple" or "noto").
func (s Set) String() string {
	switch s {
	case SetApple:
		return "apple"
	case SetNoto:
		return "noto"
	}
	return fmt.Sprintf("Set(%d)", int(s))
}

// ParseSet returns the Set named name ("apple" or "noto").
func ParseSet(name string) (Set, error) {
	for _, s := range []Set{SetApple, SetNoto} {
		if name == s.String() {
			return s, nil
		}
	}
	return 0, fmt.Errorf("unknown emoji set %q (want apple or noto)", name)
}

// Default returns the backend for the built-in emoji set. The result is
// shared by the whole process and safe for concurrent use.
//
// Nothing is decompressed or parsed here: each embedded font is inflated
// from its zstd form (Apple 13.0 MB -> 15.7 MB, 22.7 ms; Noto 9.7 MB ->
// 10.7 MB, 8.9 ms) the first time a cluster reaches it, so a session
// without emoji pays nothing. In SetApple the Noto font is only inflated if
// a cluster is missing from Apple's. The decompressed bytes are kept for the
// life of the process: the parsed font (the glyph tables and the CBDT
// bitmaps it decodes on demand) points into them, and re-inflating would
// cost more than the memory. Only the default fonts are held this way: a
// Backend from Load holds just the file you gave it.
func Default(set Set) (Backend, error) {
	switch set {
	case SetApple, SetNoto:
		defaults[set].once.Do(func() { defaults[set].chain = newDefault(set, AppleFontZstd, NotoFontZstd) })
		return defaults[set].chain, nil
	}
	return nil, fmt.Errorf("emoji: unknown set %d", int(set))
}

var defaults [SetNoto + 1]struct {
	once  sync.Once
	chain Chain
}

// newDefault builds the chain of lazy fonts of set from the compressed
// Apple and Noto fonts.
func newDefault(set Set, apple, noto func() []byte) Chain {
	appleFont := &lazyFont{name: "Apple Color Emoji", zstd: apple}
	notoFont := &lazyFont{name: "Noto Color Emoji", zstd: noto}
	if set == SetNoto {
		return Chain{notoFont}
	}
	return Chain{appleFont, notoFont}
}

// lazyFont is a Backend that inflates and parses a zstd-compressed font the
// first time it is asked for an image. If that fails it draws nothing and
// Err reports why. Safe for concurrent use.
type lazyFont struct {
	name string
	zstd func() []byte

	once   sync.Once
	loaded atomic.Bool // set when once has run, so that Err does not trigger a load
	b      Backend
	err    error
}

var _ Backend = (*lazyFont)(nil)
var _ wholeSequencer = (*lazyFont)(nil)

// inflations counts the fonts inflated, which tests use to check laziness.
var inflations atomic.Int32

func (l *lazyFont) load() Backend {
	l.once.Do(func() {
		defer l.loaded.Store(true)
		inflations.Add(1)
		data, err := inflate(l.zstd())
		if err == nil {
			l.b, err = Load(data)
		}
		if err != nil {
			l.err = fmt.Errorf("emoji: embedded %s font: %w", l.name, err)
		}
	})
	return l.b
}

// Err returns the error that stopped the font from loading, or nil if it
// loaded or has not been asked for an image yet (Err never loads the font).
func (l *lazyFont) Err() error {
	if !l.loaded.Load() {
		return nil
	}
	return l.err
}

// Image implements Backend.
func (l *lazyFont) Image(cluster string, side int) (*image.RGBA, bool) {
	if b := l.load(); b != nil {
		return b.Image(cluster, side)
	}
	return nil, false
}

func (l *lazyFont) imageExact(cluster string, side int) (*image.RGBA, bool) {
	if w, ok := l.load().(wholeSequencer); ok {
		return w.imageExact(cluster, side)
	}
	return nil, false
}

// inflate decompresses one zstd frame into a buffer of exactly its size.
func inflate(src []byte) ([]byte, error) {
	var h zstd.Header
	if err := h.Decode(src); err != nil {
		return nil, err
	}
	dec, err := zstd.NewReader(nil, zstd.WithDecoderConcurrency(1))
	if err != nil {
		return nil, err
	}
	defer dec.Close()
	return dec.DecodeAll(src, make([]byte, 0, h.FrameContentSize))
}

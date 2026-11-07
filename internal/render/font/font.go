// Package font provides monospace font sets and faces for rasterising
// terminal cells. It is pure Go (no cgo) and ships JetBrains Mono embedded,
// with a fallback chain (Symbols Nerd Font, a CJK subset, user fonts) for
// runes it lacks and a colour emoji font for emoji.
package font

import (
	"fmt"
	"image"
	"sync"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"

	"github.com/michaellorant/shellshot/internal/render/emoji"
)

// dpi is 72 so that a face size in points equals its size in pixels.
const dpi = 72

type style int

const (
	styleRegular style = iota
	styleBold
	styleItalic
	styleBoldItalic
	styleCount
)

func styleOf(bold, italic bool) style {
	switch {
	case bold && italic:
		return styleBoldItalic
	case bold:
		return styleBold
	case italic:
		return styleItalic
	}
	return styleRegular
}

// FallbackFont is a font consulted for runes the primary family lacks.
type FallbackFont struct {
	// Name identifies the font in error messages.
	Name string
	// Data is the TTF/OTF file contents. For a collection (TTC/OTC) the
	// first font is used.
	Data []byte
}

// faceKey identifies a cached face: font is the style index (0..styleCount-1)
// of a primary font, or styleCount+i for fallback i.
type faceKey struct {
	font   int
	sizePx float64
}

// Set is a regular/bold/italic/bold-italic family of parsed fonts plus an
// ordered chain of fallback fonts for runes the family lacks (the first font
// in the chain that has a rune draws it), and optionally a colour emoji font
// that is consulted last for text and first for emoji sequences. Faces and
// metrics are cached per (font, size). A Set is safe for concurrent use, but
// the returned faces are shared and, like all opentype faces, must not be
// used from multiple goroutines at once.
type Set struct {
	fonts         [styleCount]*opentype.Font
	fallbacks     []*opentype.Font
	fallbackNames []string
	emoji         *emoji.Font // nil without colour emoji

	mu    sync.Mutex
	faces map[faceKey]xfont.Face
}

// Default returns a new Set backed by the embedded JetBrains Mono fonts. Its
// fallback chain is the embedded Symbols Nerd Font Mono (icons), the embedded
// Noto Sans Mono CJK SC subset (hanzi, Hangul, kana), then extra, in order;
// embedded fonts come first so that user fonts never change the glyphs they
// cover. The embedded Twemoji Mozilla colour emoji font backs Emoji. Each
// call parses the fonts again and returns an independent Set (with its own
// caches); callers that render repeatedly should keep the Set.
func Default(extra ...FallbackFont) (*Set, error) {
	emojiFont, err := emoji.Parse(twemojiMozilla)
	if err != nil {
		return nil, fmt.Errorf("font: embedded colour emoji: %w", err)
	}
	chain := append([]FallbackFont{
		{Name: "Symbols Nerd Font Mono", Data: symbolsNerdFontMono},
		{Name: "Noto Sans Mono CJK SC", Data: notoSansMonoCJK},
	}, extra...)
	s, err := Load(jetBrainsMonoRegular, jetBrainsMonoBold, jetBrainsMonoItalic, jetBrainsMonoBoldItalic, chain...)
	if err != nil {
		return nil, err
	}
	s.emoji = emojiFont
	return s, nil
}

// Load parses TTF/OTF data for the four styles into a Set. Each fallback is
// parsed too; FaceFor tries them in the order given. The Set has no colour
// emoji font.
func Load(regular, bold, italic, boldItalic []byte, fallbacks ...FallbackFont) (*Set, error) {
	s := &Set{faces: make(map[faceKey]xfont.Face)}
	for i, d := range [styleCount][]byte{regular, bold, italic, boldItalic} {
		f, err := opentype.Parse(d)
		if err != nil {
			return nil, fmt.Errorf("font: parse %s: %w", styleNames[i], err)
		}
		s.fonts[i] = f
	}
	for _, fb := range fallbacks {
		coll, err := opentype.ParseCollection(fb.Data)
		if err != nil {
			return nil, fmt.Errorf("font: parse fallback %q: %w", fb.Name, err)
		}
		f, err := coll.Font(0)
		if err != nil {
			return nil, fmt.Errorf("font: parse fallback %q: %w", fb.Name, err)
		}
		s.fallbacks = append(s.fallbacks, f)
		s.fallbackNames = append(s.fallbackNames, fb.Name)
	}
	return s, nil
}

// Emoji renders cluster, a grapheme cluster (a base rune with its zero-width
// followers), as a side x side colour image from the colour emoji font. It
// uses the glyph of the whole sequence when the font has one and otherwise
// that of the first rune. ok is false when the Set has no colour emoji font
// or the font has no glyph. The image is shared: callers must not modify it.
func (s *Set) Emoji(cluster string, side int) (img *image.RGBA, ok bool) {
	if s.emoji == nil {
		return nil, false
	}
	return s.emoji.Image(cluster, side)
}

var styleNames = [styleCount]string{"regular", "bold", "italic", "bold italic"}

// Face returns the primary-family face for the given style at sizePx pixels
// (em size), creating and caching it on first use.
func (s *Set) Face(bold, italic bool, sizePx float64) (xfont.Face, error) {
	return s.face(int(styleOf(bold, italic)), sizePx)
}

// FaceFor returns the face that draws r: the primary face for the requested
// style if its font maps r to a glyph, otherwise the first fallback font that
// does, at the same sizePx. Fallback fonts have one style, so bold and italic
// requests use their only face. ok is false when no font has r.
//
// Fallback glyphs are not scaled to the primary cell width; use FitFace to
// draw them fitted to the cells they occupy.
func (s *Set) FaceFor(r rune, bold, italic bool, sizePx float64) (face xfont.Face, ok bool, err error) {
	if !(sizePx > 0) {
		return nil, false, fmt.Errorf("font: invalid size %v", sizePx)
	}
	idx := s.fontIndexFor(r, bold, italic)
	if idx < 0 {
		return nil, false, nil
	}
	face, err = s.face(idx, sizePx)
	if err != nil {
		return nil, false, err
	}
	return face, true, nil
}

// fontIndexFor returns the faceKey font index that draws r, or -1 if no font
// has it.
func (s *Set) fontIndexFor(r rune, bold, italic bool) int {
	st := int(styleOf(bold, italic))
	if hasGlyph(s.fonts[st], r) {
		return st
	}
	for i, f := range s.fallbacks {
		if hasGlyph(f, r) {
			return int(styleCount) + i
		}
	}
	return -1
}

func (s *Set) face(font int, sizePx float64) (xfont.Face, error) {
	if !(sizePx > 0) {
		return nil, fmt.Errorf("font: invalid size %v", sizePx)
	}
	k := faceKey{font, sizePx}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.faces[k]; ok {
		return f, nil
	}
	parsed, name := s.fallbackOrPrimary(font)
	f, err := opentype.NewFace(parsed, &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     dpi,
		Hinting: xfont.HintingNone,
	})
	if err != nil {
		return nil, fmt.Errorf("font: new %s face at %vpx: %w", name, sizePx, err)
	}
	s.faces[k] = f
	return f, nil
}

// fallbackOrPrimary returns the parsed font and display name for a faceKey
// font index.
func (s *Set) fallbackOrPrimary(font int) (*opentype.Font, string) {
	if font < int(styleCount) {
		return s.fonts[font], styleNames[font]
	}
	return s.fallbacks[font-int(styleCount)], s.fallbackNames[font-int(styleCount)]
}

// HasGlyph reports whether the regular font maps r to a real glyph.
func (s *Set) HasGlyph(r rune) bool {
	return hasGlyph(s.fonts[styleRegular], r)
}

func hasGlyph(f *opentype.Font, r rune) bool {
	var buf sfnt.Buffer
	idx, err := f.GlyphIndex(&buf, r)
	return err == nil && idx != 0
}

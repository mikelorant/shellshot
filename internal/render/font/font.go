// Package font provides monospace font sets and faces for rasterising
// terminal cells. It is pure Go (no cgo) and ships JetBrains Mono embedded.
package font

import (
	"fmt"
	"sync"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
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
	// Data is the TTF/OTF file contents.
	Data []byte
}

// faceKey identifies a cached face: font is the style index (0..styleCount-1)
// of a primary font, or styleCount+i for fallback i.
type faceKey struct {
	font   int
	sizePx float64
}

// Set is a regular/bold/italic/bold-italic family of parsed fonts plus an
// ordered list of fallback fonts for runes the family lacks. Faces and
// metrics are cached per (font, size). A Set is safe for concurrent use, but
// the returned faces are shared and, like all opentype faces, must not be
// used from multiple goroutines at once.
type Set struct {
	fonts         [styleCount]*opentype.Font
	fallbacks     []*opentype.Font
	fallbackNames []string

	mu    sync.Mutex
	faces map[faceKey]xfont.Face
}

// Default returns a new Set backed by the embedded JetBrains Mono fonts with
// the embedded Symbols Nerd Font Mono as a fallback for icon glyphs. Each
// call parses the fonts again and returns an independent Set (with its own
// caches); callers that render repeatedly should keep the Set.
func Default() (*Set, error) {
	return Load(jetBrainsMonoRegular, jetBrainsMonoBold, jetBrainsMonoItalic, jetBrainsMonoBoldItalic,
		FallbackFont{Name: "Symbols Nerd Font Mono", Data: symbolsNerdFontMono})
}

// Load parses TTF/OTF data for the four styles into a Set. Each fallback is
// parsed too; FaceFor tries them in the order given.
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
		f, err := opentype.Parse(fb.Data)
		if err != nil {
			return nil, fmt.Errorf("font: parse fallback %q: %w", fb.Name, err)
		}
		s.fallbacks = append(s.fallbacks, f)
		s.fallbackNames = append(s.fallbackNames, fb.Name)
	}
	return s, nil
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
// Fallback glyphs are not scaled to the primary cell width; callers should
// clip them to the cells the rune occupies.
func (s *Set) FaceFor(r rune, bold, italic bool, sizePx float64) (face xfont.Face, ok bool, err error) {
	if !(sizePx > 0) {
		return nil, false, fmt.Errorf("font: invalid size %v", sizePx)
	}
	st := int(styleOf(bold, italic))
	idx := -1
	if hasGlyph(s.fonts[st], r) {
		idx = st
	} else {
		for i, f := range s.fallbacks {
			if hasGlyph(f, r) {
				idx = int(styleCount) + i
				break
			}
		}
	}
	if idx < 0 {
		return nil, false, nil
	}
	face, err = s.face(idx, sizePx)
	if err != nil {
		return nil, false, err
	}
	return face, true, nil
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

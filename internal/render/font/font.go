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

type faceKey struct {
	style  style
	sizePx float64
}

// Set is a regular/bold/italic/bold-italic family of parsed fonts.
// Faces and metrics are cached per (style, size). A Set is safe for
// concurrent use, but the returned faces are shared and, like all
// opentype faces, must not be used from multiple goroutines at once.
type Set struct {
	fonts [styleCount]*opentype.Font

	mu    sync.Mutex
	faces map[faceKey]xfont.Face
}

// Default returns a new Set backed by the embedded JetBrains Mono fonts.
// Each call parses the fonts again and returns an independent Set (with its
// own caches); callers that render repeatedly should keep the Set.
func Default() (*Set, error) {
	return Load(jetBrainsMonoRegular, jetBrainsMonoBold, jetBrainsMonoItalic, jetBrainsMonoBoldItalic)
}

// Load parses TTF/OTF data for the four styles into a Set.
func Load(regular, bold, italic, boldItalic []byte) (*Set, error) {
	s := &Set{faces: make(map[faceKey]xfont.Face)}
	for i, d := range [styleCount][]byte{regular, bold, italic, boldItalic} {
		f, err := opentype.Parse(d)
		if err != nil {
			return nil, fmt.Errorf("font: parse %s: %w", styleNames[i], err)
		}
		s.fonts[i] = f
	}
	return s, nil
}

var styleNames = [styleCount]string{"regular", "bold", "italic", "bold italic"}

// Face returns the face for the given style at sizePx pixels (em size),
// creating and caching it on first use.
func (s *Set) Face(bold, italic bool, sizePx float64) (xfont.Face, error) {
	if !(sizePx > 0) {
		return nil, fmt.Errorf("font: invalid size %v", sizePx)
	}
	k := faceKey{styleOf(bold, italic), sizePx}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, ok := s.faces[k]; ok {
		return f, nil
	}
	f, err := opentype.NewFace(s.fonts[k.style], &opentype.FaceOptions{
		Size:    sizePx,
		DPI:     dpi,
		Hinting: xfont.HintingNone,
	})
	if err != nil {
		return nil, fmt.Errorf("font: new %s face at %vpx: %w", styleNames[k.style], sizePx, err)
	}
	s.faces[k] = f
	return f, nil
}

// HasGlyph reports whether the regular font maps r to a real glyph.
func (s *Set) HasGlyph(r rune) bool {
	var buf sfnt.Buffer
	idx, err := s.fonts[styleRegular].GlyphIndex(&buf, r)
	return err == nil && idx != 0
}

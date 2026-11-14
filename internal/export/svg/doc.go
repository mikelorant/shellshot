// Package svg writes terminal sessions as SVG: a static image of one screen,
// or an animation of the whole session. It works from snapshots, not from
// rendered frames, so the output is vector content (text or glyph outlines,
// rectangles, and colour emoji as text or embedded images) and not a picture of pixels. It is pure
// Go (no cgo, no cobra).
//
// Every cell is placed where the raster renderer places it: the cell grid,
// baseline, decorations, cursor shapes and box/block/braille geometry all come
// from internal/render/raster and internal/render/sprite through Geometry, so
// an SVG and the PNG of the same snapshot agree to within glyph anti-aliasing.
//
// An animation keeps, for each screen row, a vertical strip of the distinct
// versions of that row; a clipped nested <svg> shows one version at a time,
// and a CSS @keyframes list with steps(1, end) moves the strip at the step
// times of the timeline. Nothing in the output is a script or an external
// reference; the only URLs are #fragments and data: URIs, so it is valid
// where SVG is loaded as an image.
package svg

import (
	"fmt"
	"image"
	"time"

	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// TextMode says how glyphs are written.
type TextMode uint8

const (
	// TextText writes glyphs as <text> in a monospace font stack, each
	// character placed on its cell. It is small and selectable, but the
	// viewer's font draws the glyphs. The exception is a Nerd Font icon (a
	// private use rune a font of the set has): it is an outline, as in
	// TextPaths, because a viewer seldom has a Nerd Font.
	TextText TextMode = iota
	// TextPaths writes glyphs as outlines from the embedded JetBrains Mono
	// (and its fallback fonts), so the look does not depend on the viewer's
	// fonts.
	TextPaths
)

// ParseTextMode parses the --svg-text values "text" and "paths".
func ParseTextMode(s string) (TextMode, error) {
	switch s {
	case "text":
		return TextText, nil
	case "paths":
		return TextPaths, nil
	}
	return 0, fmt.Errorf("invalid text mode %q (want text or paths)", s)
}

func (m TextMode) String() string {
	if m == TextPaths {
		return "paths"
	}
	return "text"
}

// EmojiMode says how emoji are written.
type EmojiMode uint8

const (
	// EmojiText writes an emoji grapheme cluster as <text>, centred on its
	// cells like any other wide character, so the viewer's emoji font draws
	// it. It is small, but the artwork depends on the viewer. The emoji fonts
	// are not read. A cluster is an emoji by the same test as EmojiImage; in
	// TextPaths mode it is still <text>, because the outline fonts have no
	// colour glyphs.
	EmojiText EmojiMode = iota
	// EmojiImage embeds each distinct emoji as a PNG <image> rendered from
	// the emoji fonts of the Geometry, so the artwork is the same in every
	// viewer. A cluster no emoji font has falls back to the text mode's
	// treatment of the cell.
	EmojiImage
)

// ParseEmojiMode parses the --svg-emoji values "text" and "image".
func ParseEmojiMode(s string) (EmojiMode, error) {
	switch s {
	case "text":
		return EmojiText, nil
	case "image":
		return EmojiImage, nil
	}
	return 0, fmt.Errorf("invalid emoji mode %q (want text or image)", s)
}

func (m EmojiMode) String() string {
	if m == EmojiImage {
		return "image"
	}
	return "text"
}

// FrameKind says which step a static SVG shows.
type FrameKind uint8

const (
	// FrameNone selects no step: the output is the animation.
	FrameNone FrameKind = iota
	// FrameFirst selects the first step.
	FrameFirst
	// FrameLast selects the last step.
	FrameLast
	// FrameAt selects the step on screen at Frame.At on the output clock.
	FrameAt
)

// Frame selects the step of a static SVG. The zero value selects none.
type Frame struct {
	Kind FrameKind
	// At is the time on the output clock (after speed, idle limit and hold)
	// for FrameAt.
	At time.Duration
}

// ParseFrame parses the --frame values "first", "last" and a duration such
// as "1.5s" or "800ms".
func ParseFrame(s string) (Frame, error) {
	switch s {
	case "first":
		return Frame{Kind: FrameFirst}, nil
	case "last":
		return Frame{Kind: FrameLast}, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return Frame{}, fmt.Errorf("invalid frame %q (want first, last or a time such as 1.5s)", s)
	}
	return Frame{Kind: FrameAt, At: d}, nil
}

// Geometry is what the SVG writer needs from the raster renderer and its
// fonts. *raster.Renderer provides the first three methods and *font.Set the
// Assets.
type Geometry interface {
	// Layout returns the cell grid and colours.
	Layout() raster.Layout
	// RowCells places the clusters of a row, block cursor included.
	RowCells(s snapshot.Snapshot, row int) []raster.RowCell
	// CursorCell locates the cursor of s, if one is drawn.
	CursorCell(s snapshot.Snapshot) (row, col, width int, ok bool)
	Assets
}

// Assets are the font data glyphs and emoji are taken from.
type Assets interface {
	// Emoji renders a grapheme cluster as a side x side colour image.
	Emoji(cluster string, side int) (img *image.RGBA, ok bool)
	// Covers reports whether a font of the set has a glyph for r.
	Covers(r rune, bold, italic bool) bool
	// Outline returns the outline of the glyph for r.
	Outline(r rune, bold, italic bool) (font.Outline, bool, error)
}

// Options configures a Writer.
type Options struct {
	Geometry Geometry
	// Text selects text or outlines for glyphs.
	Text TextMode
	// Emoji selects <text> or embedded images for emoji.
	Emoji EmojiMode
	// Frame selects the step of a static SVG; FrameNone writes the animation.
	Frame Frame
	// Loops is the number of times the animation plays; 0 means forever.
	Loops int
}

// fontFamily is the font stack of text mode: the embedded face's name first,
// then common monospace fonts of each platform.
const fontFamily = "'JetBrains Mono', ui-monospace, Menlo, monospace"

// geometry joins a renderer and the fonts it draws with into a Geometry.
type geometry struct {
	*raster.Renderer
	*font.Set
}

// NewGeometry returns the Geometry of renderer r, whose fonts are f. The
// SVG then has the grid, colours and glyph coverage the renderer has.
func NewGeometry(r *raster.Renderer, f *font.Set) Geometry { return geometry{r, f} }

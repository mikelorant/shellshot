// Package theme maps snapshot colours to concrete RGBA values. Themes are an
// export-time choice: recordings store only palette indices and RGB values.
package theme

import (
	"image/color"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Role says whether a colour is used as a foreground or a background, which
// decides what snapshot.ColorDefault resolves to.
type Role uint8

const (
	// Fg resolves the default colour to the theme foreground.
	Fg Role = iota
	// Bg resolves the default colour to the theme background.
	Bg
)

// Theme is a named set of terminal colours. Palette holds palette indices
// 0–15; indices 16–255 are computed (see paletteColor).
type Theme struct {
	Name       string
	Foreground color.RGBA
	Background color.RGBA
	Cursor     color.RGBA
	Palette    [16]color.RGBA
}

// Resolve returns the concrete colour for c in the given role.
func (t Theme) Resolve(c snapshot.Color, role Role) color.RGBA {
	switch c.Kind {
	case snapshot.ColorPalette:
		return t.paletteColor(c.Index)
	case snapshot.ColorRGB:
		return color.RGBA{R: c.R, G: c.G, B: c.B, A: 0xff}
	}
	if role == Bg {
		return t.Background
	}
	return t.Foreground
}

// Colors returns the foreground and background used to draw a cell with
// style s. Inverse swaps them, then Faint blends the foreground halfway
// toward the background, then Invisible makes the foreground equal the
// background.
func (t Theme) Colors(s snapshot.Style) (fg, bg color.RGBA) {
	fg = t.Resolve(s.Fg, Fg)
	bg = t.Resolve(s.Bg, Bg)
	if s.Attrs&snapshot.Inverse != 0 {
		fg, bg = bg, fg
	}
	if s.Attrs&snapshot.Faint != 0 {
		fg = blend(fg, bg)
	}
	if s.Attrs&snapshot.Invisible != 0 {
		fg = bg
	}
	return fg, bg
}

// blend returns the midpoint of a and b, rounding up.
func blend(a, b color.RGBA) color.RGBA {
	mid := func(x, y uint8) uint8 { return uint8((uint16(x) + uint16(y) + 1) / 2) }
	return color.RGBA{R: mid(a.R, b.R), G: mid(a.G, b.G), B: mid(a.B, b.B), A: 0xff}
}

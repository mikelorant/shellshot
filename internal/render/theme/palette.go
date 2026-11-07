package theme

import (
	"image/color"
	"sort"
)

// cubeLevels are the channel values of the 6×6×6 colour cube (indices 16–231).
var cubeLevels = [6]uint8{0, 95, 135, 175, 215, 255}

// paletteColor returns palette entry i: the theme's own colour for 0–15, the
// colour cube for 16–231 and the grayscale ramp (8+10*n) for 232–255.
func (t Theme) paletteColor(i uint8) color.RGBA {
	switch {
	case i < 16:
		return t.Palette[i]
	case i < 232:
		n := int(i) - 16
		return color.RGBA{
			R: cubeLevels[n/36],
			G: cubeLevels[n/6%6],
			B: cubeLevels[n%6],
			A: 0xff,
		}
	}
	v := uint8(8 + 10*(int(i)-232))
	return color.RGBA{R: v, G: v, B: v, A: 0xff}
}

func rgb(hex uint32) color.RGBA {
	return color.RGBA{R: uint8(hex >> 16), G: uint8(hex >> 8), B: uint8(hex), A: 0xff}
}

// Default returns the built-in dark theme, named "dark". Its 16 colours
// follow the classic xterm/VGA-style layout: 0–7 normal black, red, green,
// yellow, blue, magenta, cyan, white; 8–15 their bright variants.
func Default() Theme {
	return Theme{
		Name:       "dark",
		Foreground: rgb(0xd4d4d4),
		Background: rgb(0x1e1e1e),
		Cursor:     rgb(0xaeafad),
		Palette: [16]color.RGBA{
			rgb(0x000000), rgb(0xcd3131), rgb(0x0dbc79), rgb(0xe5e510),
			rgb(0x2472c8), rgb(0xbc3fbc), rgb(0x11a8cd), rgb(0xe5e5e5),
			rgb(0x666666), rgb(0xf14c4c), rgb(0x23d18b), rgb(0xf5f543),
			rgb(0x3b8eea), rgb(0xd670d6), rgb(0x29b8db), rgb(0xffffff),
		},
	}
}

// Light returns the built-in light theme, named "light", with the same
// 16-colour layout as Default.
func Light() Theme {
	return Theme{
		Name:       "light",
		Foreground: rgb(0x383a42),
		Background: rgb(0xfafafa),
		Cursor:     rgb(0x526fff),
		Palette: [16]color.RGBA{
			rgb(0x000000), rgb(0xcd3131), rgb(0x00875a), rgb(0x949800),
			rgb(0x0451a5), rgb(0xbc05bc), rgb(0x0598bc), rgb(0x555555),
			rgb(0x666666), rgb(0xcd3131), rgb(0x14ce14), rgb(0xb5ba00),
			rgb(0x0451a5), rgb(0xbc05bc), rgb(0x0598bc), rgb(0xa5a5a5),
		},
	}
}

// builtins returns the built-in themes keyed by name.
func builtins() map[string]Theme {
	d, l := Default(), Light()
	return map[string]Theme{d.Name: d, l.Name: l}
}

// Lookup returns the built-in theme with the given name.
func Lookup(name string) (Theme, bool) {
	t, ok := builtins()[name]
	return t, ok
}

// Names returns the built-in theme names in sorted order.
func Names() []string {
	m := builtins()
	names := make([]string, 0, len(m))
	for n := range m {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

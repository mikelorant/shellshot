package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"image/color"
	"io"
	"strconv"
)

// ErrTheme is wrapped by every error Load returns.
var ErrTheme = errors.New("theme: invalid theme file")

// fileTheme is the JSON theme file format.
type fileTheme struct {
	Name       string   `json:"name"`
	Foreground *string  `json:"foreground"`
	Background *string  `json:"background"`
	Cursor     *string  `json:"cursor"`
	Palette    []string `json:"palette"`
}

// Load reads a JSON theme file of the form
//
//	{"name": "x", "foreground": "#rrggbb", "background": "#rrggbb",
//	 "cursor": "#rrggbb", "palette": [16 "#rrggbb" strings]}
//
// All fields are required. Unknown fields are rejected.
func Load(r io.Reader) (Theme, error) {
	var f fileTheme
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Theme{}, fmt.Errorf("%w: %v", ErrTheme, err)
	}
	if dec.More() {
		return Theme{}, fmt.Errorf("%w: trailing data after theme object", ErrTheme)
	}
	if f.Name == "" {
		return Theme{}, fmt.Errorf("%w: missing name", ErrTheme)
	}
	t := Theme{Name: f.Name}
	for _, field := range []struct {
		key string
		val *string
		dst *color.RGBA
	}{
		{"foreground", f.Foreground, &t.Foreground},
		{"background", f.Background, &t.Background},
		{"cursor", f.Cursor, &t.Cursor},
	} {
		if field.val == nil {
			return Theme{}, fmt.Errorf("%w: missing %s", ErrTheme, field.key)
		}
		c, err := parseHex(*field.val)
		if err != nil {
			return Theme{}, fmt.Errorf("%w: %s: %v", ErrTheme, field.key, err)
		}
		*field.dst = c
	}
	if len(f.Palette) != len(t.Palette) {
		return Theme{}, fmt.Errorf("%w: palette has %d colours, want %d",
			ErrTheme, len(f.Palette), len(t.Palette))
	}
	for i, s := range f.Palette {
		c, err := parseHex(s)
		if err != nil {
			return Theme{}, fmt.Errorf("%w: palette[%d]: %v", ErrTheme, i, err)
		}
		t.Palette[i] = c
	}
	return t, nil
}

// parseHex parses "#rrggbb".
func parseHex(s string) (color.RGBA, error) {
	if len(s) != 7 || s[0] != '#' {
		return color.RGBA{}, fmt.Errorf("colour %q is not of the form #rrggbb", s)
	}
	for _, r := range s[1:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F') {
			return color.RGBA{}, fmt.Errorf("colour %q is not of the form #rrggbb", s)
		}
	}
	v, err := strconv.ParseUint(s[1:], 16, 32)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("colour %q: %v", s, err)
	}
	return rgb(uint32(v)), nil
}

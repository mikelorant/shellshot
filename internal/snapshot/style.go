package snapshot

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrBadSGR is wrapped by every error ParseSGR returns.
var ErrBadSGR = errors.New("snapshot: bad SGR parameters")

// ColorKind says how a Color is specified.
type ColorKind uint8

const (
	// ColorDefault is the terminal's default colour (the zero value).
	ColorDefault ColorKind = iota
	// ColorPalette selects entry Index of the 256-colour palette.
	ColorPalette
	// ColorRGB is a 24-bit colour given by R, G and B.
	ColorRGB
)

// Color is a foreground or background colour. Fields not used by Kind
// must be zero so that Colors compare with ==.
type Color struct {
	Kind    ColorKind
	Index   uint8 // palette index, used when Kind == ColorPalette
	R, G, B uint8 // used when Kind == ColorRGB
}

// Attr is a set of text attributes.
type Attr uint16

// Text attribute flags.
const (
	Bold Attr = 1 << iota
	Faint
	Italic
	Underline
	Blink
	Inverse
	Invisible
	Strike
)

// attrCodes lists attributes with their SGR codes in encoding order.
var attrCodes = [...]struct {
	attr Attr
	code int
}{
	{Bold, 1}, {Faint, 2}, {Italic, 3}, {Underline, 4},
	{Blink, 5}, {Inverse, 7}, {Invisible, 8}, {Strike, 9},
}

// Style is the visual style of a run of cells. The zero Style is the
// terminal default. Style is comparable.
type Style struct {
	Fg, Bg Color
	Attrs  Attr
}

// IsZero reports whether s is the default style.
func (s Style) IsZero() bool { return s == Style{} }

// SGR returns the SGR parameter string for s, without the leading "ESC[" and
// trailing "m", e.g. "1;31" or "38;5;208". Order is deterministic: attributes
// (bold, faint, italic, underline, blink, inverse, invisible, strike), then
// foreground, then background. The zero Style yields "".
func (s Style) SGR() string {
	var p []string
	for _, a := range attrCodes {
		if s.Attrs&a.attr != 0 {
			p = append(p, strconv.Itoa(a.code))
		}
	}
	p = appendColor(p, s.Fg, 30, 90, 38)
	p = appendColor(p, s.Bg, 40, 100, 48)
	return strings.Join(p, ";")
}

// appendColor appends the SGR parameters for c. base and bright are the
// first codes of the 8 normal and 8 bright colours; ext is 38 or 48.
func appendColor(p []string, c Color, base, bright, ext int) []string {
	switch c.Kind {
	case ColorPalette:
		switch {
		case c.Index < 8:
			return append(p, strconv.Itoa(base+int(c.Index)))
		case c.Index < 16:
			return append(p, strconv.Itoa(bright+int(c.Index)-8))
		default:
			return append(p, strconv.Itoa(ext), "5", strconv.Itoa(int(c.Index)))
		}
	case ColorRGB:
		return append(p, strconv.Itoa(ext), "2",
			strconv.Itoa(int(c.R)), strconv.Itoa(int(c.G)), strconv.Itoa(int(c.B)))
	}
	return p
}

// ParseSGR is the inverse of Style.SGR. "" and "0" give the zero Style.
// Unknown or malformed parameters return an error wrapping ErrBadSGR.
func ParseSGR(s string) (Style, error) {
	if s == "" || s == "0" {
		return Style{}, nil
	}
	parts := strings.Split(s, ";")
	nums := make([]int, len(parts))
	for i, p := range parts {
		n, err := parseParam(p)
		if err != nil {
			return Style{}, fmt.Errorf("%w: %q: %v", ErrBadSGR, s, err)
		}
		nums[i] = n
	}
	var st Style
	for i := 0; i < len(nums); i++ {
		n := nums[i]
		if a, ok := attrForCode(n); ok {
			st.Attrs |= a
			continue
		}
		switch {
		case n >= 30 && n <= 37:
			st.Fg = Color{Kind: ColorPalette, Index: uint8(n - 30)}
		case n >= 90 && n <= 97:
			st.Fg = Color{Kind: ColorPalette, Index: uint8(n - 90 + 8)}
		case n >= 40 && n <= 47:
			st.Bg = Color{Kind: ColorPalette, Index: uint8(n - 40)}
		case n >= 100 && n <= 107:
			st.Bg = Color{Kind: ColorPalette, Index: uint8(n - 100 + 8)}
		case n == 38 || n == 48:
			c, used, err := parseExtended(nums[i+1:])
			if err != nil {
				return Style{}, fmt.Errorf("%w: %q: %v", ErrBadSGR, s, err)
			}
			if n == 38 {
				st.Fg = c
			} else {
				st.Bg = c
			}
			i += used
		default:
			return Style{}, fmt.Errorf("%w: %q: unknown parameter %d", ErrBadSGR, s, n)
		}
	}
	return st, nil
}

func attrForCode(n int) (Attr, bool) {
	for _, a := range attrCodes {
		if a.code == n {
			return a.attr, true
		}
	}
	return 0, false
}

// parseParam parses one decimal parameter made only of digits.
func parseParam(p string) (int, error) {
	if p == "" {
		return 0, errors.New("empty parameter")
	}
	for _, r := range p {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-numeric parameter %q", p)
		}
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		return 0, fmt.Errorf("parameter %q out of range", p)
	}
	return n, nil
}

// parseExtended parses the parameters following 38 or 48 ("5;n" or
// "2;r;g;b") and returns the colour and how many parameters it consumed.
func parseExtended(rest []int) (Color, int, error) {
	if len(rest) == 0 {
		return Color{}, 0, errors.New("missing extended colour mode")
	}
	switch rest[0] {
	case 5:
		if len(rest) < 2 {
			return Color{}, 0, errors.New("missing palette index")
		}
		if rest[1] > 255 {
			return Color{}, 0, fmt.Errorf("palette index %d out of range", rest[1])
		}
		return Color{Kind: ColorPalette, Index: uint8(rest[1])}, 2, nil
	case 2:
		if len(rest) < 4 {
			return Color{}, 0, errors.New("missing RGB components")
		}
		for _, v := range rest[1:4] {
			if v > 255 {
				return Color{}, 0, fmt.Errorf("RGB component %d out of range", v)
			}
		}
		return Color{Kind: ColorRGB, R: uint8(rest[1]), G: uint8(rest[2]), B: uint8(rest[3])}, 4, nil
	}
	return Color{}, 0, fmt.Errorf("unknown extended colour mode %d", rest[0])
}

// Span applies Style to the half-open range [Start, End) of rune indexes
// into a line string. Indexes count runes, not bytes or terminal cells.
type Span struct {
	Start, End int
	Style      Style
}

package svg

import (
	"encoding/xml"
	"image/color"
	"math"
	"strconv"
	"strings"
)

// num formats a coordinate: at most two decimals, no trailing zeros.
func num(v float64) string {
	s := strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64)
	if s == "-0" {
		return "0"
	}
	return s
}

// hex formats c as #rgb or #rrggbb. The alpha of c is ignored: themes are
// opaque.
func hex(c color.RGBA) string {
	const digits = "0123456789abcdef"
	if c.R>>4 == c.R&15 && c.G>>4 == c.G&15 && c.B>>4 == c.B&15 {
		return string([]byte{'#', digits[c.R&15], digits[c.G&15], digits[c.B&15]})
	}
	b := []byte{'#', 0, 0, 0, 0, 0, 0}
	for i, v := range [3]uint8{c.R, c.G, c.B} {
		b[1+2*i], b[2+2*i] = digits[v>>4], digits[v&15]
	}
	return string(b)
}

// escape writes s to sb as XML character data. encoding/xml replaces runes
// that XML cannot carry with U+FFFD.
func escape(sb *strings.Builder, s string) {
	xml.EscapeText(sb, []byte(s)) // a strings.Builder never fails to write
}

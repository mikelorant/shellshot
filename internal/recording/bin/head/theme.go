package head

import (
	"fmt"
	"sort"

	"github.com/michaellorant/shellshot/internal/recording/bin/varint"
)

// ThemeLen is the exact size of a them payload in bytes.
const ThemeLen = 3 + 3 + 3 + 1 + 3 + 3 + 256*3 + 1

// Color is an 8-bit RGB colour.
type Color struct{ R, G, B uint8 }

// Theme is the them chunk payload: the terminal colours in effect at record
// time.
//
// Layout (fixed size, ThemeLen bytes): FG, BG, Cursor (RGB each), HasCursor
// (0 or 1), SelectionFG, SelectionBG, Palette (256 RGB triples), CursorStyle.
type Theme struct {
	FG, BG                   Color
	Cursor                   Color
	HasCursor                bool
	SelectionFG, SelectionBG Color
	Palette                  [256]Color
	CursorStyle              byte
}

// MarshalTheme encodes t.
func MarshalTheme(t Theme) []byte {
	b := make([]byte, 0, ThemeLen)
	rgb := func(c Color) { b = append(b, c.R, c.G, c.B) }
	rgb(t.FG)
	rgb(t.BG)
	rgb(t.Cursor)
	if t.HasCursor {
		b = append(b, 1)
	} else {
		b = append(b, 0)
	}
	rgb(t.SelectionFG)
	rgb(t.SelectionBG)
	for _, c := range t.Palette {
		rgb(c)
	}
	return append(b, t.CursorStyle)
}

// UnmarshalTheme decodes a them payload. It returns ErrMalformed unless b is
// exactly ThemeLen bytes with a 0 or 1 has-cursor byte.
func UnmarshalTheme(b []byte) (Theme, error) {
	if len(b) != ThemeLen {
		return Theme{}, fmt.Errorf("%w: theme of %d bytes, want %d", ErrMalformed, len(b), ThemeLen)
	}
	r := varint.NewReader(b)
	var t Theme
	rgb := func() Color {
		v, _ := r.Bytes(3) // length checked above
		return Color{v[0], v[1], v[2]}
	}
	t.FG, t.BG, t.Cursor = rgb(), rgb(), rgb()
	hc, _ := r.Byte()
	if hc > 1 {
		return Theme{}, fmt.Errorf("%w: has-cursor byte %d", ErrMalformed, hc)
	}
	t.HasCursor = hc == 1
	t.SelectionFG, t.SelectionBG = rgb(), rgb()
	for i := range t.Palette {
		t.Palette[i] = rgb()
	}
	t.CursorStyle, _ = r.Byte()
	return t, nil
}

// Meta is the meta chunk payload: free-form user key/value strings.
//
// Layout: [count uvarint] then per pair [klen uvarint][key][vlen uvarint][value],
// with keys in ascending byte order.
type Meta map[string]string

// MarshalMeta encodes m with sorted keys. It returns ErrLimit if m has more
// than MaxListLen pairs or a key or value longer than MaxStringLen.
func MarshalMeta(m Meta) ([]byte, error) {
	if len(m) > MaxListLen {
		return nil, fmt.Errorf("%w: %d meta pairs", ErrLimit, len(m))
	}
	keys := make([]string, 0, len(m))
	for k, v := range m {
		if len(k) > MaxStringLen || len(v) > MaxStringLen {
			return nil, fmt.Errorf("%w: meta string of %d bytes", ErrLimit, max(len(k), len(v)))
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b := varint.AppendUvarint(nil, uint64(len(keys)))
	for _, k := range keys {
		b = appendString(b, k)
		b = appendString(b, m[k])
	}
	return b, nil
}

// UnmarshalMeta decodes a meta payload. Duplicate keys, trailing bytes and
// truncation are ErrMalformed; oversized counts and strings are ErrLimit.
func UnmarshalMeta(b []byte) (Meta, error) {
	r := varint.NewReader(b)
	n, err := r.UvarintMax(MaxListLen)
	if err != nil {
		return nil, wrap("meta count", err)
	}
	m := make(Meta, min(int(n), r.Remaining()/2))
	for range n {
		k, err := readString(r)
		if err != nil {
			return nil, wrap("meta key", err)
		}
		v, err := readString(r)
		if err != nil {
			return nil, wrap("meta value", err)
		}
		if _, dup := m[k]; dup {
			return nil, fmt.Errorf("%w: duplicate meta key %q", ErrMalformed, k)
		}
		m[k] = v
	}
	if r.Remaining() != 0 {
		return nil, fmt.Errorf("%w: %d trailing bytes", ErrMalformed, r.Remaining())
	}
	return m, nil
}

package emoji

import "fmt"

// Load parses a colour emoji font of either supported technology and
// returns the backend for it: a COLR v0 + CPAL font (Twemoji Mozilla) gives
// a *Font, a CBLC + CBDT PNG font (Apple Color Emoji, Noto Color Emoji)
// gives a *Bitmap. A font with both uses its COLR layers.
func Load(data []byte) (Backend, error) {
	t, err := readTables(data)
	if err != nil {
		return nil, err
	}
	switch {
	case t["COLR"] != nil && t["CPAL"] != nil:
		return Parse(data)
	case t["CBLC"] != nil && t["CBDT"] != nil:
		return ParseBitmap(data)
	}
	return nil, fmt.Errorf("emoji: no colour glyph tables (need COLR/CPAL or CBLC/CBDT)")
}

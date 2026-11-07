package raster

import (
	"image"
	"image/draw"
)

// drawEmoji draws the colour emoji for c's whole cluster, scaled to fit the
// cells c spans and centred in them on whole pixels. It reports whether the
// emoji font had a glyph. Colour emoji ignore the foreground colour.
func (r *Renderer) drawEmoji(img *image.RGBA, c cell) bool {
	w, h := c.width*r.metrics.CellWidth, r.metrics.CellHeight
	side := min(w, h)
	glyph, ok := r.fonts.Emoji(c.cluster(), side)
	if !ok {
		return false
	}
	o := r.cellOrigin(c.col, c.row)
	x, y := o.X+(w-side)/2, o.Y+(h-side)/2
	draw.Draw(img, image.Rect(x, y, x+side, y+side), glyph, image.Point{}, draw.Over)
	return true
}

// emojiSequence reports whether c asks for emoji presentation: it has a
// follower that only exists in emoji sequences (emoji variation selector,
// zero-width joiner, skin tone, keycap mark, regional indicator or tag
// character). U+FE0E, which asks for text presentation, does not count.
func emojiSequence(c cell) bool {
	for _, m := range c.marks {
		switch {
		case m == 0xFE0F, m == 0x200D, m == 0x20E3,
			0x1F3FB <= m && m <= 0x1F3FF, // skin tones
			0x1F1E6 <= m && m <= 0x1F1FF, // regional indicators
			0xE0020 <= m && m <= 0xE007F: // tags (subdivision flags)
			return true
		}
	}
	return false
}

// isFormatRune reports whether r only steers shaping and has no glyph of its
// own: variation selectors, zero-width space/joiners, word joiner and tags.
func isFormatRune(r rune) bool {
	return 0xFE00 <= r && r <= 0xFE0F || 0x200B <= r && r <= 0x200D || r == 0x2060 ||
		0xE0020 <= r && r <= 0xE007F || 0xE0100 <= r && r <= 0xE01EF
}

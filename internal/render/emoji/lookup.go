package emoji

import "golang.org/x/image/font/sfnt"

const (
	variationSelector15 = '\uFE0E' // text presentation
	variationSelector16 = '\uFE0F' // emoji presentation
)

// glyphMap resolves grapheme clusters to glyph ids with the font's cmap and
// GSUB ligatures. Both colour backends embed it. It is not safe for
// concurrent use (sfnt.Buffer): the owner serialises access.
type glyphMap struct {
	sf   *sfnt.Font
	ligs ligatures
	buf  sfnt.Buffer
}

// resolve maps cluster to a glyph for which has reports true: the whole
// sequence first (with and without variation selectors, since fonts
// disagree on whether a sequence includes them), then the first rune alone.
func (m *glyphMap) resolve(cluster string, has func(gid uint16) bool) (uint16, bool) {
	runes := []rune(cluster)
	if len(runes) == 0 {
		return 0, false
	}
	for _, seq := range sequenceVariants(runes) {
		if gid, ok := m.lookup(seq, has); ok {
			return gid, true
		}
	}
	return m.lookup(runes[:1], has)
}

// lookup maps seq to a glyph through cmap and, for several runes, the GSUB
// ligatures, and reports whether has accepts it.
func (m *glyphMap) lookup(seq []rune, has func(gid uint16) bool) (uint16, bool) {
	if len(seq) == 0 {
		return 0, false
	}
	gids := make([]uint16, len(seq))
	for i, r := range seq {
		g, err := m.sf.GlyphIndex(&m.buf, r)
		if err != nil || g == 0 {
			return 0, false
		}
		gids[i] = uint16(g)
	}
	gid := gids[0]
	if len(gids) > 1 {
		gid = m.ligs[seqKey(gids)]
	}
	return gid, gid != 0 && has(gid)
}

// sequenceVariants lists the spellings of runes to try, most specific first:
// as given, without variation selectors, and with an emoji variation
// selector after the first rune (the form keycap sequences use).
func sequenceVariants(runes []rune) [][]rune {
	out := [][]rune{runes}
	var bare []rune
	for _, r := range runes {
		if r != variationSelector15 && r != variationSelector16 {
			bare = append(bare, r)
		}
	}
	if len(bare) != len(runes) {
		out = append(out, bare)
	}
	if len(bare) > 1 {
		out = append(out, append([]rune{bare[0], variationSelector16}, bare[1:]...))
	}
	return out
}

package emoji

import (
	"fmt"
	"strings"
)

// ligatures maps a sequence of glyph ids (packed by seqKey) to the glyph
// that GSUB substitutes for it.
type ligatures map[string]uint16

// seqKey packs glyph ids into a map key.
func seqKey(gids []uint16) string {
	var b strings.Builder
	for _, g := range gids {
		b.WriteByte(byte(g >> 8))
		b.WriteByte(byte(g))
	}
	return b.String()
}

// parseLigatures collects the ligature substitutions (lookup type 4, also
// when wrapped in an extension lookup, type 7) of the "ccmp" and "liga"
// features. Emoji fonts use them to map a multi-codepoint sequence (a ZWJ
// family, a flag, a skin tone, a keycap) to a single glyph. Other GSUB
// lookups are ignored.
func parseLigatures(t tables) (ligatures, error) {
	gsub := t["GSUB"]
	if gsub == nil {
		return ligatures{}, nil
	}
	g := reader{b: gsub}
	featureList, lookupList := g.u16(6), g.u16(8)

	wanted := map[int]bool{}
	for i := range g.u16(featureList) {
		rec := featureList + 2 + 6*i
		if tag := g.tag(rec); tag != "ccmp" && tag != "liga" {
			continue
		}
		feature := featureList + g.u16(rec+4)
		for j := range g.u16(feature + 2) {
			wanted[g.u16(feature+4+2*j)] = true
		}
	}

	ligs := ligatures{}
	for i := range g.u16(lookupList) {
		if !wanted[i] {
			continue
		}
		lookup := lookupList + g.u16(lookupList+2+2*i)
		typ := g.u16(lookup)
		for j := range g.u16(lookup + 4) {
			sub := lookup + g.u16(lookup+6+2*j)
			if typ == 7 { // extension: format, wrapped type, 32-bit offset
				if g.u16(sub+2) != 4 {
					continue
				}
				sub += g.u32(sub + 4)
			} else if typ != 4 {
				continue
			}
			readLigatureSubtable(&g, sub, ligs)
		}
	}
	if g.err != nil {
		return nil, fmt.Errorf("emoji: GSUB: %w", g.err)
	}
	return ligs, nil
}

// readLigatureSubtable adds the ligatures of one LigatureSubstFormat1
// subtable at off. Within a first glyph the font's order is the priority
// order, so the first definition of a sequence wins.
func readLigatureSubtable(g *reader, off int, into ligatures) {
	if g.u16(off) != 1 {
		return
	}
	firsts := coverage(g, off+g.u16(off+2))
	for i := range g.u16(off + 4) {
		if i >= len(firsts) {
			return
		}
		set := off + g.u16(off+6+2*i)
		for j := range g.u16(set) {
			lig := set + g.u16(set+2+2*j)
			glyph, n := uint16(g.u16(lig)), g.u16(lig+2)
			seq := make([]uint16, 0, n)
			seq = append(seq, firsts[i])
			for k := 1; k < n; k++ {
				seq = append(seq, uint16(g.u16(lig+4+2*(k-1))))
			}
			if g.err != nil {
				return
			}
			if key := seqKey(seq); into[key] == 0 {
				into[key] = glyph
			}
		}
	}
}

// coverage returns the glyph ids of a Coverage table in coverage-index order.
func coverage(g *reader, off int) []uint16 {
	var gids []uint16
	switch g.u16(off) {
	case 1:
		for i := range g.u16(off + 2) {
			gids = append(gids, uint16(g.u16(off+4+2*i)))
		}
	case 2:
		for i := range g.u16(off + 2) {
			rec := off + 4 + 6*i
			start, end := g.u16(rec), g.u16(rec+2)
			for gid := start; gid <= end && g.err == nil; gid++ {
				gids = append(gids, uint16(gid))
			}
		}
	}
	return gids
}

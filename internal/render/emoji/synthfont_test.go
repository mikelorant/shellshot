package emoji

import (
	"encoding/binary"
	"slices"
	"testing"
)

// Glyph ids of the synthetic bitmap font.
const (
	gidSmiley = 1 // U+1F600
	gidMan    = 2 // U+1F468
	gidWoman  = 3 // U+1F469
	gidZWJ    = 4 // U+200D
	gidGirl   = 5 // U+1F467, in the cmap but without a bitmap
	gidVS16   = 6 // U+FE0F
	gidHeart  = 7 // U+2764
	gidFamily = 8 // GSUB ccmp: man ZWJ woman
	numGlyphs = 9
)

// buildSFNT assembles an sfnt file from tables, with the directory sorted
// by tag and every table 4-byte aligned, as x/image/font/sfnt requires.
func buildSFNT(tabs map[string][]byte) []byte {
	tags := make([]string, 0, len(tabs))
	for tag := range tabs {
		tags = append(tags, tag)
	}
	slices.Sort(tags)
	out := cat(be32(0x00010000), be(len(tags), 0, 0, 0))
	body := make([]byte, 0)
	start := 12 + 16*len(tags)
	for _, tag := range tags {
		b := tabs[tag]
		out = cat(out, []byte(tag), be32(0), be32(start+len(body)), be32(len(b)))
		body = append(body, b...)
		for len(body)%4 != 0 {
			body = append(body, 0)
		}
	}
	return append(out, body...)
}

// testCmap is a cmap with one format 12 subtable (platform 3, encoding 10).
func testCmap() []byte {
	runes := map[rune]int{
		0x200D: gidZWJ, 0x2764: gidHeart, 0xFE0F: gidVS16, 0x1F467: gidGirl,
		0x1F468: gidMan, 0x1F469: gidWoman, 0x1F600: gidSmiley,
	}
	order := make([]rune, 0, len(runes))
	for r := range runes {
		order = append(order, r)
	}
	slices.Sort(order)
	groups := make([]byte, 0)
	for _, r := range order {
		groups = cat(groups, be32(int(r)), be32(int(r)), be32(runes[r]))
	}
	sub := cat(be(12, 0), be32(16+len(groups)), be32(0), be32(len(order)), groups)
	return cat(be(0, 1, 3, 10), be32(12), sub)
}

// testGSUB has one ccmp lookup with a single ligature: man ZWJ woman -> family.
func testGSUB() []byte {
	return cat(
		be(1, 0, 0, 10, 24),                       // header: featureList@10, lookupList@24
		be(1), []byte("ccmp"), be(8), be(0, 1, 0), // FeatureList@10 and its feature: lookup 0
		be(1, 4), be(4, 0, 1, 8), // LookupList@24: type 4 lookup@28, subtable at +8
		be(1, 8, 1, 14),                    // LigatureSubst@36: coverage +8, one set at +14
		be(1, 1, gidMan),                   // coverage: man
		be(1, 4),                           // ligature set: one ligature at +4
		be(gidFamily, 3, gidZWJ, gidWoman), // ligature: family from man ZWJ woman
	)
}

// buildBitmapFont assembles a minimal sfnt with a cmap, a GSUB ligature and
// the given bitmap strikes. It has no outlines.
func buildBitmapFont(t testing.TB, strikes []testStrike) []byte {
	t.Helper()
	cblc, cbdt := buildBitmapTables(t, strikes)
	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5)
	binary.BigEndian.PutUint16(head[18:], 1000) // unitsPerEm
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[0:], 0x00010000)
	binary.BigEndian.PutUint16(hhea[34:], numGlyphs) // numberOfHMetrics
	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp[0:], 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:], numGlyphs)
	post := make([]byte, 32) // version 3.0: no glyph names
	binary.BigEndian.PutUint32(post[0:], 0x00030000)
	return buildSFNT(map[string][]byte{
		"head": head, "post": post, "hhea": hhea, "maxp": maxp,
		"hmtx": make([]byte, 4*numGlyphs),
		"loca": make([]byte, 2*(numGlyphs+1)), "glyf": make([]byte, 4),
		"cmap": testCmap(), "GSUB": testGSUB(), "CBLC": cblc, "CBDT": cbdt,
	})
}

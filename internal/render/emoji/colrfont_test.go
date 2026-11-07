package emoji

import (
	"encoding/binary"
	"testing"
)

// Glyph ids the synthetic COLR font adds to those of synthfont_test.go.
const (
	gidLeft     = 9  // outline: left half of the em square
	gidRight    = 10 // outline: right half
	gidTop      = 11 // outline: top half
	gidFull     = 12 // outline: the whole em square
	numColrGlyp = 13
)

// Palette indexes of the synthetic COLR font, and their colours.
const (
	palRed = iota
	palBlue
	palGreen
	palCyan
	palMagenta
)

// rectGlyph is a glyf entry for the rectangle x0,y0 - x1,y1 (font units).
func rectGlyph(x0, y0, x1, y1 int) []byte {
	return cat(
		be(1, x0, y0, x1, y1), // one contour and its bounding box
		be(3),                 // last point of the contour
		be(0),                 // no instructions
		[]byte{1, 1, 1, 1},    // on-curve points, 16-bit deltas
		be(x0, 0, x1-x0, 0),
		be(y0, y1-y0, 0, y0-y1),
	)
}

// buildColorFont assembles a COLR v0 font (1000 units per em, no OS/2 table,
// so the emoji box is the whole em square) with the cmap and GSUB ligature
// of the synthetic bitmap font. Colours (BGRA records in CPAL):
//
//	smiley  red left half over nothing, blue right half
//	man     all green
//	woman   magenta top half
//	heart   all blue with a red left half on top
//	family  all cyan (ligature man ZWJ woman)
//
// The girl is in the cmap but has no layers.
func buildColorFont(t testing.TB) []byte {
	t.Helper()
	outlines := [][]byte{
		gidLeft - 9:  rectGlyph(0, 0, 500, 1000),
		gidRight - 9: rectGlyph(500, 0, 1000, 1000),
		gidTop - 9:   rectGlyph(0, 500, 1000, 1000),
		gidFull - 9:  rectGlyph(0, 0, 1000, 1000),
	}
	var glyf []byte
	loca := be(0)
	for g := range numColrGlyp {
		if g >= gidLeft {
			glyf = append(glyf, outlines[g-gidLeft]...)
		}
		loca = append(loca, be(len(glyf)/2)...)
	}

	cpal := cat(be(0, 5, 1, 5), be32(14), be(0),
		[]byte{0, 0, 255, 255},   // red (B, G, R, A)
		[]byte{255, 0, 0, 255},   // blue
		[]byte{0, 255, 0, 255},   // green
		[]byte{255, 255, 0, 255}, // cyan
		[]byte{255, 0, 255, 255}, // magenta
	)
	bases := [][3]int{ // glyph, first layer, layer count
		{gidSmiley, 0, 2}, {gidMan, 2, 1}, {gidWoman, 3, 1}, {gidHeart, 4, 2}, {gidFamily, 6, 1},
	}
	layers := [][2]int{ // outline glyph, palette index
		{gidLeft, palRed}, {gidRight, palBlue},
		{gidFull, palGreen},
		{gidTop, palMagenta},
		{gidFull, palBlue}, {gidLeft, palRed},
		{gidFull, palCyan},
	}
	colr := cat(be(0, len(bases)), be32(14), be32(14+6*len(bases)), be(len(layers)))
	for _, b := range bases {
		colr = cat(colr, be(b[0], b[1], b[2]))
	}
	for _, l := range layers {
		colr = cat(colr, be(l[0], l[1]))
	}

	head := make([]byte, 54)
	binary.BigEndian.PutUint32(head[0:], 0x00010000)
	binary.BigEndian.PutUint32(head[12:], 0x5F0F3CF5)
	binary.BigEndian.PutUint16(head[18:], 1000) // unitsPerEm; loca format 0 (short)
	hhea := make([]byte, 36)
	binary.BigEndian.PutUint32(hhea[0:], 0x00010000)
	binary.BigEndian.PutUint16(hhea[34:], numColrGlyp)
	maxp := make([]byte, 32)
	binary.BigEndian.PutUint32(maxp[0:], 0x00010000)
	binary.BigEndian.PutUint16(maxp[4:], numColrGlyp)
	post := make([]byte, 32)
	binary.BigEndian.PutUint32(post[0:], 0x00030000)
	return buildSFNT(map[string][]byte{
		"head": head, "post": post, "hhea": hhea, "maxp": maxp,
		"hmtx": make([]byte, 4*numColrGlyp), "loca": loca, "glyf": glyf,
		"cmap": testCmap(), "GSUB": testGSUB(), "COLR": colr, "CPAL": cpal,
	})
}

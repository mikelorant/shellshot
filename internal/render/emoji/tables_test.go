package emoji

import (
	"encoding/binary"
	"strings"
	"testing"
)

// be packs values as big-endian 16-bit words.
func be(vals ...int) []byte {
	var b []byte
	for _, v := range vals {
		b = binary.BigEndian.AppendUint16(b, uint16(v))
	}
	return b
}

// be32 packs one big-endian 32-bit value.
func be32(v int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// A GSUB with one "ccmp" feature whose lookup is a type 7 extension around
// a type 4 ligature subtable with a format 2 coverage: glyphs 5, 7 -> 9.
func TestParseLigaturesExtensionAndRangeCoverage(t *testing.T) {
	gsub := cat(
		be(1, 0, 0, 10, 24), // header: featureList@10, lookupList@24
		// FeatureList@10: one record, then the feature table at +8.
		be(1), []byte("ccmp"), be(8),
		be(0, 1, 0), // feature@18: no params, lookup 0
		// LookupList@24: lookup@28 = extension, subtable@36.
		be(1, 4),
		be(7, 0, 1, 8),
		// extension@36: format 1, wraps type 4, subtable at +8 (@44).
		cat(be(1, 4), be32(8)),
		// ligature subtable@44: coverage at +8, one set at +18.
		be(1, 8, 1, 18),   // coverage@52 (+8), ligature set@62 (+18)
		be(2, 1, 5, 5, 0), // coverage@52: range 5..5
		be(1, 4),          // ligature set@62: one ligature at +4
		be(9, 2, 7),       // ligature: glyph 9 from 5, 7
	)
	ligs, err := parseLigatures(tables{"GSUB": gsub})
	if err != nil {
		t.Fatal(err)
	}
	if got := ligs[seqKey([]uint16{5, 7})]; got != 9 || len(ligs) != 1 {
		t.Errorf("ligatures = %v, want only {5 7} -> 9", ligs)
	}
}

func TestParseLigaturesTruncatedAndAbsent(t *testing.T) {
	if ligs, err := parseLigatures(tables{}); err != nil || len(ligs) != 0 {
		t.Errorf("no GSUB: %v, %v; want empty, nil", ligs, err)
	}
	if _, err := parseLigatures(tables{"GSUB": be(1, 0, 0, 10, 24, 1)}); err == nil {
		t.Error("truncated GSUB accepted")
	}
}

func colorTables(colr, cpal []byte) tables { return tables{"COLR": colr, "CPAL": cpal} }

// palette with one BGRA colour record.
var onePalette = cat(be(0, 1, 1, 1), be32(14), be(0), []byte{3, 2, 1, 255})

func TestParseColorErrors(t *testing.T) {
	// header: version, numBase=1, baseOff=14, layerOff=20, numLayers=1
	// (14 bytes), then one base glyph record (6 bytes) and one layer record.
	colr := func(version, palette int) []byte {
		return cat(be(version, 1), be32(14), be32(20), be(1), be(7, 0, 1), be(8, palette))
	}
	for name, tc := range map[string]struct {
		t    tables
		want string
	}{
		"missing tables": {tables{}, "no COLR"},
		"colrv1":         {colorTables(colr(1, 0), onePalette), "version 1"},
		"palette range":  {colorTables(colr(0, 5), onePalette), "out of range"},
		"truncated colr": {colorTables(colr(0, 0)[:16], onePalette), "COLR"},
		"truncated cpal": {colorTables(colr(0, 0), onePalette[:14]), "CPAL"},
		"no palette":     {colorTables(colr(0, 0), cat(be(0, 1, 0, 1), be32(12))), "no palette"},
	} {
		if _, err := parseColor(tc.t); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: error %v, want containing %q", name, err, tc.want)
		}
	}
}

func TestParseColorReadsLayersAndForeground(t *testing.T) {
	// glyph 7 has two layers: glyph 8 in palette colour 0, glyph 9 in the
	// foreground colour, which is dropped.
	colr := cat(be(0, 1), be32(14), be32(20), be(2), be(7, 0, 2), be(8, 0, 9, 0xFFFF))
	got, err := parseColor(colorTables(colr, onePalette))
	if err != nil {
		t.Fatal(err)
	}
	ls := got[7]
	if len(ls) != 1 || ls[0].glyph != 8 || ls[0].color.R != 1 || ls[0].color.G != 2 || ls[0].color.B != 3 || ls[0].color.A != 255 {
		t.Errorf("layers of glyph 7 = %+v, want one layer: glyph 8, colour (1,2,3,255)", ls)
	}
}

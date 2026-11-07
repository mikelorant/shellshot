package emoji

import (
	"bytes"
	"errors"
	"image/color"
	"testing"
)

var (
	red   = color.NRGBA{255, 0, 0, 255}
	green = color.NRGBA{0, 255, 0, 255}
	blue  = color.NRGBA{0, 0, 255, 255}
	white = color.NRGBA{255, 255, 255, 255}
)

// sampleStrikes has two strikes covering every index and image format:
// ppem 96 has format 1/17 for glyphs 1-3 (2 absent) and format 3/18 for
// glyphs 7-8; ppem 32 has format 1/19 for glyph 1 only.
func sampleStrikes(t testing.TB) []testStrike {
	return []testStrike{
		{ppem: 96, subs: []testSub{
			{first: 1, last: 3, indexFormat: 1, imageFormat: 17, png: map[uint16][]byte{
				1: solidPNG(t, 2, 2, red), 3: solidPNG(t, 3, 2, green),
			}},
			{first: 7, last: 8, indexFormat: 3, imageFormat: 18, png: map[uint16][]byte{
				7: solidPNG(t, 4, 4, blue), 8: solidPNG(t, 1, 1, white),
			}},
		}},
		{ppem: 32, subs: []testSub{
			{first: 1, last: 1, indexFormat: 1, imageFormat: 19, png: map[uint16][]byte{1: solidPNG(t, 2, 2, white)}},
		}},
	}
}

func sampleTables(t testing.TB) tables {
	cblc, cbdt := buildBitmapTables(t, sampleStrikes(t))
	return tables{"CBLC": cblc, "CBDT": cbdt}
}

func TestParseBitmapsStrikes(t *testing.T) {
	b, err := parseBitmaps(sampleTables(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(b.strikes) != 2 || b.strikes[0].ppem != 96 || b.strikes[1].ppem != 32 {
		t.Fatalf("strikes = %+v, want ppem 96 and 32", b.strikes)
	}
	if got := len(b.strikes[0].subs); got != 2 {
		t.Errorf("strike 96 has %d subtables, want 2", got)
	}
}

func TestBitmapGlyphIndexAndImageFormats(t *testing.T) {
	b, err := parseBitmaps(sampleTables(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name          string
		strike        int
		gid           uint16
		w, h, bearing int
		advance       int
	}{
		{"index 1, image 17", 0, 1, 2, 2, 1, 2},
		{"index 1, image 17, other size", 0, 3, 3, 2, 1, 3},
		{"index 3, image 18", 0, 7, 4, 4, 3, 4},
		{"index 3, image 18, last glyph", 0, 8, 1, 1, 0, 1},
		{"index 1, image 19 has no metrics", 1, 1, 0, 0, 0, 0},
	} {
		g, ok, err := b.glyph(&b.strikes[tc.strike], tc.gid)
		if err != nil || !ok {
			t.Errorf("%s: ok %v, err %v", tc.name, ok, err)
			continue
		}
		if g.width != tc.w || g.height != tc.h || g.bearingY != tc.bearing || g.advance != tc.advance {
			t.Errorf("%s: metrics w%d h%d bearingY%d adv%d, want w%d h%d bearingY%d adv%d",
				tc.name, g.width, g.height, g.bearingY, g.advance, tc.w, tc.h, tc.bearing, tc.advance)
		}
		if !bytes.HasPrefix(g.png, []byte("\x89PNG")) {
			t.Errorf("%s: data %q does not start with a PNG signature", tc.name, g.png[:min(4, len(g.png))])
		}
	}
}

func TestBitmapGlyphAbsent(t *testing.T) {
	b, err := parseBitmaps(sampleTables(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, gid := range []uint16{0, 2, 4, 6, 9, 0xFFFF} { // empty range, or outside every subtable
		if _, ok, err := b.glyph(&b.strikes[0], gid); ok || err != nil {
			t.Errorf("glyph %d: ok %v, err %v; want absent without error", gid, ok, err)
		}
	}
}

func TestParseBitmapsRejectsMissingTables(t *testing.T) {
	good := sampleTables(t)
	for name, tb := range map[string]tables{
		"none": {}, "no CBDT": {"CBLC": good["CBLC"]}, "no CBLC": {"CBDT": good["CBDT"]},
	} {
		if _, err := parseBitmaps(tb); !errors.Is(err, ErrBitmap) {
			t.Errorf("%s: err = %v, want ErrBitmap", name, err)
		}
	}
}

func TestParseBitmapsRejectsBadData(t *testing.T) {
	good := sampleTables(t)
	patch := func(table string, at int, v ...byte) tables {
		out := tables{"CBLC": bytes.Clone(good["CBLC"]), "CBDT": bytes.Clone(good["CBDT"])}
		copy(out[table][at:], v)
		return out
	}
	// The first BitmapSize record starts at 8; its subtable array follows
	// the two records and holds two 8-byte entries, then the first
	// subtable's IndexSubHeader.
	array := cblcHeaderSize + 2*bitmapSizeSize
	hdr := array + 16
	for name, tb := range map[string]tables{
		"CBLC version 1":        patch("CBLC", 1, 1),
		"CBDT version 9":        patch("CBDT", 1, 9),
		"huge strike count":     patch("CBLC", 4, 0xFF, 0xFF, 0xFF, 0xFF),
		"huge subtable count":   patch("CBLC", cblcHeaderSize+8, 0xFF, 0xFF, 0xFF, 0xFF),
		"array offset past end": patch("CBLC", cblcHeaderSize, 0x7F, 0xFF, 0xFF, 0xFF),
		"reversed glyph range":  patch("CBLC", array, 0, 9, 0, 1),
		"index format 2":        patch("CBLC", hdr, 0, 2),
		"image format 5":        patch("CBLC", hdr+2, 0, 5),
		"subtable past end":     patch("CBLC", array+4, 0x00, 0xFF, 0xFF, 0xFF),
	} {
		if _, err := parseBitmaps(tb); !errors.Is(err, ErrBitmap) {
			t.Errorf("%s: err = %v, want ErrBitmap", name, err)
		}
	}
}

func TestParseBitmapsTruncatedCBLC(t *testing.T) {
	good := sampleTables(t)
	for n := range len(good["CBLC"]) {
		_, err := parseBitmaps(tables{"CBLC": good["CBLC"][:n], "CBDT": good["CBDT"]})
		if !errors.Is(err, ErrBitmap) {
			t.Errorf("CBLC cut to %d of %d bytes: err = %v, want ErrBitmap", n, len(good["CBLC"]), err)
		}
	}
}

func TestBitmapGlyphTruncatedOrCorruptCBDT(t *testing.T) {
	good := sampleTables(t)
	for n := range len(good["CBDT"]) {
		b, err := parseBitmaps(tables{"CBLC": good["CBLC"], "CBDT": good["CBDT"][:n]})
		if n < 4 {
			if !errors.Is(err, ErrBitmap) {
				t.Errorf("CBDT cut to %d bytes: err = %v, want ErrBitmap", n, err)
			}
			continue
		}
		if err != nil {
			t.Fatalf("CBDT cut to %d bytes: %v", n, err)
		}
		// Glyph 1 of the last strike is the last record; any cut loses some of the data.
		if _, ok, err := b.glyph(&b.strikes[1], 1); ok || !errors.Is(err, ErrBitmap) {
			t.Errorf("CBDT cut to %d of %d bytes: last glyph ok %v, err %v; want ErrBitmap", n, len(good["CBDT"]), ok, err)
		}
	}

	// A record whose PNG length runs past its offset range.
	bad := bytes.Clone(good["CBDT"])
	b, _ := parseBitmaps(tables{"CBLC": good["CBLC"], "CBDT": bad})
	first := b.strikes[0].subs[0]
	rec := first.dataOffset // glyph 1's record: 5 metrics bytes, then the length
	copy(bad[rec+5:], []byte{0x7F, 0xFF, 0xFF, 0xFF})
	if _, ok, err := b.glyph(&b.strikes[0], 1); ok || !errors.Is(err, ErrBitmap) {
		t.Errorf("oversized PNG length: ok %v, err %v; want ErrBitmap", ok, err)
	}
}

func FuzzParseCBLC(f *testing.F) {
	good := sampleTables(f)
	f.Add(good["CBLC"], good["CBDT"])
	f.Add(good["CBLC"][:60], good["CBDT"])
	f.Add([]byte{0, 3, 0, 0, 0, 0, 0, 1}, []byte{0, 3, 0, 0})
	f.Fuzz(func(t *testing.T, cblc, cbdt []byte) {
		b, err := parseBitmaps(tables{"CBLC": cblc, "CBDT": cbdt})
		if err != nil {
			if !errors.Is(err, ErrBitmap) {
				t.Fatalf("parse error does not wrap ErrBitmap: %v", err)
			}
			return
		}
		for i := range b.strikes {
			for _, sub := range b.strikes[i].subs {
				for gid := int(sub.first); gid <= int(sub.last) && gid < int(sub.first)+64; gid++ {
					if _, _, err := b.glyph(&b.strikes[i], uint16(gid)); err != nil && !errors.Is(err, ErrBitmap) {
						t.Fatalf("glyph %d: error does not wrap ErrBitmap: %v", gid, err)
					}
				}
			}
		}
	})
}

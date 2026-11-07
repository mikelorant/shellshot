package emoji

import (
	"bytes"
	"image/color"
	"os"
	"sync"
	"testing"
)

const smiley = "\U0001F600"

// readAsset reads a font from the font package's assets directory, e.g.
// "jetbrains-mono/JetBrainsMono-Regular.ttf".
func readAsset(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../font/assets/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// loadColor parses the synthetic COLR font.
func loadColor(t testing.TB) *Font {
	t.Helper()
	f, err := Parse(buildColorFont(t))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseRejectsFontsWithoutColourTables(t *testing.T) {
	for name, data := range map[string][]byte{
		"monochrome font": readAsset(t, "jetbrains-mono/JetBrainsMono-Regular.ttf"),
		"bitmap font":     buildBitmapFont(t, testEmojiStrikes(t)),
		"empty":           nil,
		"garbage":         bytes.Repeat([]byte{0xAB}, 64),
	} {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: Parse succeeded", name)
		}
	}
}

func TestParseRejectsTruncatedFont(t *testing.T) {
	data := buildColorFont(t)
	for _, n := range []int{8, 100, len(data) - 40} {
		if _, err := Parse(data[:n]); err == nil {
			t.Errorf("Parse of first %d of %d bytes succeeded", n, len(data))
		}
	}
}

func TestReadTablesRejectsCollections(t *testing.T) {
	if _, err := readTables(append([]byte("ttcf"), make([]byte, 16)...)); err == nil {
		t.Error("collection accepted")
	}
}

func TestParsedFontHasLigaturesAndLayers(t *testing.T) {
	f := loadColor(t)
	if len(f.layers) != 5 || len(f.ligs) != 1 {
		t.Errorf("parsed %d layered glyphs and %d ligatures, want 5 and 1", len(f.layers), len(f.ligs))
	}
	if f.unitsPerEm != 1000 || f.top != 1000 {
		t.Errorf("emoji box = %d units, top %d; want 1000, 1000 (no OS/2 table)", f.unitsPerEm, f.top)
	}
}

var (
	redRGBA     = color.RGBA{255, 0, 0, 255}
	blueRGBA    = color.RGBA{0, 0, 255, 255}
	magentaRGBA = color.RGBA{255, 0, 255, 255}
)

// Each layer is drawn in its palette colour, bottom layer first, and the
// emoji box fills the image.
func TestImageDrawsLayersInOrder(t *testing.T) {
	f := loadColor(t)
	const side = 20
	type probe struct {
		x, y int
		want color.RGBA
	}
	for name, tc := range map[string]struct {
		cluster string
		probes  []probe
	}{
		"left red, right blue":       {smiley, []probe{{1, 1, redRGBA}, {1, 18, redRGBA}, {18, 1, blueRGBA}, {18, 18, blueRGBA}}},
		"one layer, all green":       {man, []probe{{0, 0, greenRGBA}, {19, 19, greenRGBA}, {10, 10, greenRGBA}}},
		"red layer above blue layer": {heartVS, []probe{{1, 10, redRGBA}, {18, 10, blueRGBA}}},
		"top half only":              {woman, []probe{{10, 1, magentaRGBA}, {10, 9, magentaRGBA}, {10, 11, color.RGBA{}}, {10, 18, color.RGBA{}}}},
	} {
		img, ok := f.Image(tc.cluster, side)
		if !ok {
			t.Errorf("%s: no glyph", name)
			continue
		}
		if img.Bounds().Dx() != side || img.Bounds().Dy() != side {
			t.Errorf("%s: bounds %v, want %dx%d", name, img.Bounds(), side, side)
		}
		for _, p := range tc.probes {
			if got := img.RGBAAt(p.x, p.y); got != p.want {
				t.Errorf("%s: pixel (%d,%d) = %v, want %v", name, p.x, p.y, got, p.want)
			}
		}
	}
}

// A sequence the font has a ligature for draws its own glyph, not that of its
// first rune.
func TestImageUsesSequenceLigatureNotFirstRune(t *testing.T) {
	f := loadColor(t)
	seq, ok1 := f.Image(manWoman, 16)
	first, ok2 := f.Image(man, 16)
	if !ok1 || !ok2 {
		t.Fatalf("lookup failed (%v, %v)", ok1, ok2)
	}
	if got := seq.RGBAAt(8, 8); got != (color.RGBA{0, 255, 255, 255}) {
		t.Errorf("family pixel = %v, want cyan", got)
	}
	if bytes.Equal(seq.Pix, first.Pix) {
		t.Error("sequence drew the same image as its first rune")
	}
}

// Fonts spell sequences with or without variation selectors; both resolve.
func TestImageVariationSelectorSpellings(t *testing.T) {
	f := loadColor(t)
	same := func(a, b string) {
		t.Helper()
		x, ok1 := f.Image(a, 32)
		y, ok2 := f.Image(b, 32)
		if !ok1 || !ok2 || !bytes.Equal(x.Pix, y.Pix) {
			t.Errorf("%q and %q: ok %v %v, equal %v; want the same glyph", a, b, ok1, ok2, ok1 && ok2 && bytes.Equal(x.Pix, y.Pix))
		}
	}
	same(heartVS, "\u2764")
	same(manWoman+"\uFE0F", manWoman)
	same(man+zwj+"\uFE0F"+woman, manWoman)
}

// A sequence of known emoji that the font has no ligature for falls back to
// its first rune.
func TestImageUnknownSequenceFallsBackToFirstRune(t *testing.T) {
	f := loadColor(t)
	want, _ := f.Image(man, 32)
	got, ok := f.Image(man+zwj+smiley+zwj+man, 32)
	if !ok || !bytes.Equal(got.Pix, want.Pix) {
		t.Errorf("unknown sequence: ok %v, same as first rune %v", ok, ok && bytes.Equal(got.Pix, want.Pix))
	}
}

func TestImageMissingGlyphs(t *testing.T) {
	f := loadColor(t)
	for name, c := range map[string]string{
		"empty": "", "not in cmap": "a", "cjk": "\u4E16",
		"in cmap, no layers": girl, "with VS16": girl + "\uFE0F", "joiner alone": zwj,
	} {
		if _, ok := f.Image(c, 32); ok {
			t.Errorf("%s %q: unexpectedly drawn", name, c)
		}
	}
	for _, side := range []int{0, -4} {
		if _, ok := f.Image(smiley, side); ok {
			t.Errorf("side %d drawn", side)
		}
	}
}

func TestImageIsCachedAndDeterministic(t *testing.T) {
	f := loadColor(t)
	a, _ := f.Image(manWoman, 40)
	b, _ := f.Image(manWoman, 40)
	if a != b {
		t.Error("second call did not return the cached image")
	}
	c, _ := loadColor(t).Image(manWoman, 40)
	if !bytes.Equal(a.Pix, c.Pix) {
		t.Error("a fresh font rendered different pixels")
	}
}

func TestImageConcurrent(t *testing.T) {
	f := loadColor(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, c := range []string{smiley, man, manWoman, heartVS} {
				if _, ok := f.Image(c, 16+i); !ok {
					t.Errorf("%q: not drawn", c)
				}
			}
		}()
	}
	wg.Wait()
}

// Font and Bitmap both find only whole sequences when asked to.
func TestBackendsImageExactSkipsFirstRuneFallback(t *testing.T) {
	for name, b := range map[string]wholeSequencer{"COLR": loadColor(t), "bitmap": loadBitmap(t)} {
		unknown := man + zwj + smiley + zwj + man
		if _, ok := b.imageExact(unknown, 8); ok {
			t.Errorf("%s: imageExact drew an unknown sequence", name)
		}
		if _, ok := b.imageExact(manWoman, 8); !ok {
			t.Errorf("%s: imageExact lacks the ligature", name)
		}
		if _, ok := b.imageExact(man, 8); !ok {
			t.Errorf("%s: imageExact lacks a single emoji", name)
		}
		if _, ok := b.(Backend).Image(unknown, 8); !ok {
			t.Errorf("%s: Image lost its first-rune fallback", name)
		}
	}
}

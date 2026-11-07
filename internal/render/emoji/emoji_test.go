package emoji

import (
	"bytes"
	"image"
	"image/color"
	"os"
	"sync"
	"testing"
)

const (
	smiley   = "\U0001F600"
	thumb    = "\U0001F44D"
	thumbMed = "\U0001F44D\U0001F3FD"
	family   = "\U0001F468\u200D\U0001F469\u200D\U0001F467"
	flagAU   = "\U0001F1E6\U0001F1FA"
	keycap   = "1\uFE0F\u20E3"
	rainbow  = "\U0001F3F3\uFE0F\u200D\U0001F308"
)

func readAsset(t testing.TB, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../font/assets/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func loadTwemoji(t testing.TB) *Font {
	t.Helper()
	f, err := Parse(readAsset(t, "TwemojiMozilla.ttf"))
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestParseRejectsFontsWithoutColourTables(t *testing.T) {
	for name, data := range map[string][]byte{
		"monochrome font": readAsset(t, "JetBrainsMono-Regular.ttf"),
		"empty":           nil,
		"garbage":         bytes.Repeat([]byte{0xAB}, 64),
	} {
		if _, err := Parse(data); err == nil {
			t.Errorf("%s: Parse succeeded", name)
		}
	}
}

func TestParseRejectsTruncatedFont(t *testing.T) {
	data := readAsset(t, "TwemojiMozilla.ttf")
	for _, n := range []int{8, 100, len(data) / 2} {
		if _, err := Parse(data[:n]); err == nil {
			t.Errorf("Parse of first %d bytes succeeded", n)
		}
	}
}

func TestReadTablesRejectsCollections(t *testing.T) {
	if _, err := readTables(append([]byte("ttcf"), make([]byte, 16)...)); err == nil {
		t.Error("collection accepted")
	}
}

func TestParsedFontHasLigaturesAndLayers(t *testing.T) {
	f := loadTwemoji(t)
	if len(f.layers) < 3000 || len(f.ligs) < 2000 {
		t.Errorf("parsed %d layered glyphs and %d ligatures, want thousands", len(f.layers), len(f.ligs))
	}
	if f.unitsPerEm != 512 || f.top != 448 {
		t.Errorf("emoji box = %d units, top %d; want 512, 448", f.unitsPerEm, f.top)
	}
}

// opaquePixels counts pixels with alpha > 0 and the number of distinct
// opaque colours.
func opaquePixels(img interface {
	At(x, y int) color.Color
}, side int) (n, colours int) {
	seen := map[color.Color]bool{}
	for y := range side {
		for x := range side {
			c := img.At(x, y)
			if _, _, _, a := c.RGBA(); a == 0xffff {
				seen[c] = true
			}
			if _, _, _, a := c.RGBA(); a != 0 {
				n++
			}
		}
	}
	return n, len(seen)
}

func TestImageRendersColourForEverySequenceKind(t *testing.T) {
	f := loadTwemoji(t)
	for name, cluster := range map[string]string{
		"emoji": smiley, "vs16": "\u2764\uFE0F", "text default": "\u2764", "tone": thumbMed,
		"zwj family": family, "flag": flagAU, "keycap": keycap, "zwj with vs16": rainbow,
	} {
		img, ok := f.Image(cluster, 64)
		if !ok {
			t.Errorf("%s %q: no glyph", name, cluster)
			continue
		}
		if img.Bounds().Dx() != 64 || img.Bounds().Dy() != 64 {
			t.Errorf("%s: bounds %v, want 64x64", name, img.Bounds())
		}
		if n, colours := opaquePixels(img, 64); n < 64*64/4 || colours < 1 {
			t.Errorf("%s: %d inked pixels, %d opaque colours; want a filled coloured glyph", name, n, colours)
		}
	}
}

// A sequence the font knows draws its own glyph, not that of its first rune.
func TestImageUsesSequenceLigatureNotFirstRune(t *testing.T) {
	f := loadTwemoji(t)
	for _, tc := range []struct{ name, seq, first string }{
		{"family", family, "\U0001F468"},
		{"flag", flagAU, "\U0001F1E6"},
		{"tone", thumbMed, thumb},
		{"rainbow flag", rainbow, "\U0001F3F3"},
	} {
		seq, ok1 := f.Image(tc.seq, 48)
		first, ok2 := f.Image(tc.first, 48)
		if !ok1 || !ok2 {
			t.Fatalf("%s: lookup failed (%v, %v)", tc.name, ok1, ok2)
		}
		if bytes.Equal(seq.Pix, first.Pix) {
			t.Errorf("%s: sequence drew the same image as its first rune", tc.name)
		}
	}
}

// Fonts spell sequences with or without variation selectors; both resolve.
func TestImageVariationSelectorSpellings(t *testing.T) {
	f := loadTwemoji(t)
	same := func(a, b string) {
		t.Helper()
		x, ok1 := f.Image(a, 32)
		y, ok2 := f.Image(b, 32)
		if !ok1 || !ok2 || !bytes.Equal(x.Pix, y.Pix) {
			t.Errorf("%q and %q: ok %v %v, equal %v; want the same glyph", a, b, ok1, ok2, ok1 && ok2 && bytes.Equal(x.Pix, y.Pix))
		}
	}
	same("\u2764\uFE0F", "\u2764")
	same("1\u20E3", keycap)
	same("\U0001F3F3\u200D\U0001F308", rainbow)
}

// A sequence of known emoji that the font has no ligature for falls back to
// its first rune.
func TestImageUnknownSequenceFallsBackToFirstRune(t *testing.T) {
	f := loadTwemoji(t)
	want, _ := f.Image(smiley, 32)
	got, ok := f.Image(smiley+"\u200D\U0001F600\u200D\U0001F600\u200D\U0001F600", 32)
	if !ok || !bytes.Equal(got.Pix, want.Pix) {
		t.Errorf("unknown sequence: ok %v, same as first rune %v", ok, ok && bytes.Equal(got.Pix, want.Pix))
	}
}

func TestImageMissingGlyphs(t *testing.T) {
	f := loadTwemoji(t)
	for _, c := range []string{"", "a", "\u4E16", "\U0001FAE8", "\U0010FFFE"} {
		if _, ok := f.Image(c, 32); ok {
			t.Errorf("%q: unexpectedly drawn", c)
		}
	}
	if _, ok := f.Image(smiley, 0); ok {
		t.Error("zero size drawn")
	}
}

// The Australian flag is mostly blue, the family has several hues.
func TestImageColours(t *testing.T) {
	f := loadTwemoji(t)
	img, _ := f.Image(flagAU, 64)
	blue := 0
	for i := 0; i < len(img.Pix); i += 4 {
		if r, b := img.Pix[i], img.Pix[i+2]; img.Pix[i+3] == 255 && b > 100 && r < 60 {
			blue++
		}
	}
	if blue < 64*64/5 {
		t.Errorf("flag has %d blue pixels, want a blue field", blue)
	}
}

// The emoji box maps onto the full image: ink reaches near all four edges of
// a face and nothing is clipped at the top.
func TestImageFillsItsSquare(t *testing.T) {
	f := loadTwemoji(t)
	img, _ := f.Image(smiley, 64)
	lo, hi := image.Pt(64, 64), image.Pt(-1, -1)
	for y := range 64 {
		for x := range 64 {
			if img.RGBAAt(x, y).A != 0 {
				lo = image.Pt(min(lo.X, x), min(lo.Y, y))
				hi = image.Pt(max(hi.X, x), max(hi.Y, y))
			}
		}
	}
	if lo.X > 2 || lo.Y > 2 || hi.X < 61 || hi.Y < 61 {
		t.Errorf("smiley ink spans %v-%v of a 64px square, want almost all of it", lo, hi)
	}
}

func TestImageIsCachedAndDeterministic(t *testing.T) {
	f := loadTwemoji(t)
	a, _ := f.Image(family, 40)
	b, _ := f.Image(family, 40)
	if a != b {
		t.Error("second call did not return the cached image")
	}
	c, _ := loadTwemoji(t).Image(family, 40)
	if !bytes.Equal(a.Pix, c.Pix) {
		t.Error("a fresh font rendered different pixels")
	}
}

func TestImageConcurrent(t *testing.T) {
	f := loadTwemoji(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, c := range []string{smiley, family, flagAU, keycap} {
				if _, ok := f.Image(c, 16+i); !ok {
					t.Errorf("%q: not drawn", c)
				}
			}
		}()
	}
	wg.Wait()
}

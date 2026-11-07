package emoji

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image/color"
	"sync"
	"testing"
)

var (
	cyan    = color.NRGBA{0, 255, 255, 255}
	magenta = color.NRGBA{255, 0, 255, 255}
	yellow  = color.NRGBA{255, 255, 0, 255}
	halfRed = color.NRGBA{255, 0, 0, 128}
)

const (
	man      = "\U0001F468"
	woman    = "\U0001F469"
	girl     = "\U0001F467"
	zwj      = "\u200D"
	heartVS  = "\u2764\uFE0F"
	manWoman = man + zwj + woman
)

// testEmojiStrikes is the bitmap content of the synthetic font: a 96 ppem
// strike with every glyph and a 32 ppem strike with only a yellow smiley.
func testEmojiStrikes(t testing.TB) []testStrike {
	return []testStrike{
		{ppem: 96, subs: []testSub{
			{first: gidSmiley, last: gidWoman, indexFormat: 1, imageFormat: 17, png: map[uint16][]byte{
				gidSmiley: solidPNG(t, 2, 2, red, green, blue, white), // one colour per quadrant
				gidMan:    solidPNG(t, 2, 2, green),
				gidWoman:  solidPNG(t, 4, 2, magenta), // not square
			}},
			{first: gidHeart, last: gidFamily, indexFormat: 3, imageFormat: 18, png: map[uint16][]byte{
				gidHeart:  solidPNG(t, 2, 2, halfRed),
				gidFamily: solidPNG(t, 2, 2, cyan),
			}},
		}},
		{ppem: 32, subs: []testSub{
			{first: gidSmiley, last: gidSmiley, indexFormat: 1, imageFormat: 17, png: map[uint16][]byte{
				gidSmiley: solidPNG(t, 2, 2, yellow),
			}},
		}},
	}
}

func loadBitmap(t testing.TB) *Bitmap {
	t.Helper()
	b, err := ParseBitmap(buildBitmapFont(t, testEmojiStrikes(t)))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// near reports whether the pixel at (x, y) of img is within tol of want in
// each channel (premultiplied RGBA).
func near(t *testing.T, img interface{ At(x, y int) color.Color }, x, y int, want color.RGBA, tol int) {
	t.Helper()
	got := color.RGBAModel.Convert(img.At(x, y)).(color.RGBA)
	d := func(a, b uint8) int { return max(int(a)-int(b), int(b)-int(a)) }
	if d(got.R, want.R) > tol || d(got.G, want.G) > tol || d(got.B, want.B) > tol || d(got.A, want.A) > tol {
		t.Errorf("pixel (%d,%d) = %v, want %v (±%d)", x, y, got, want, tol)
	}
}

func TestBitmapSingleRuneScalesAndKeepsColours(t *testing.T) {
	b := loadBitmap(t)
	img, ok := b.Image(smiley, 96)
	if !ok {
		t.Fatal("no glyph for the smiley")
	}
	if img.Bounds().Dx() != 96 || img.Bounds().Dy() != 96 {
		t.Fatalf("bounds %v, want 96x96", img.Bounds())
	}
	near(t, img, 20, 20, color.RGBA{255, 0, 0, 255}, 40)
	near(t, img, 76, 20, color.RGBA{0, 255, 0, 255}, 40)
	near(t, img, 20, 76, color.RGBA{0, 0, 255, 255}, 40)
	near(t, img, 76, 76, color.RGBA{255, 255, 255, 255}, 40)

	only96, err := ParseBitmap(buildBitmapFont(t, testEmojiStrikes(t)[:1]))
	if err != nil {
		t.Fatal(err)
	}
	small, _ := only96.Image(smiley, 8)
	near(t, small, 1, 1, color.RGBA{255, 0, 0, 255}, 40)
	near(t, small, 6, 6, color.RGBA{255, 255, 255, 255}, 40)
}

func TestBitmapCopiesWhenSizesMatch(t *testing.T) {
	b := loadBitmap(t)
	img, _ := b.Image(man, 2) // the 2x2 bitmap in a 2x2 image: exact
	for _, p := range [][2]int{{0, 0}, {1, 0}, {0, 1}, {1, 1}} {
		near(t, img, p[0], p[1], color.RGBA{0, 255, 0, 255}, 0)
	}
}

func TestBitmapKeepsAspectRatioAndCentres(t *testing.T) {
	b := loadBitmap(t)
	img, _ := b.Image(woman, 8) // 4x2 -> 8x4, rows 2..5
	near(t, img, 4, 0, color.RGBA{}, 0)
	near(t, img, 4, 1, color.RGBA{}, 0)
	near(t, img, 0, 2, color.RGBA{255, 0, 255, 255}, 8)
	near(t, img, 7, 5, color.RGBA{255, 0, 255, 255}, 8)
	near(t, img, 4, 6, color.RGBA{}, 0)
	near(t, img, 4, 7, color.RGBA{}, 0)
}

// The PNG is straight alpha; the image must be premultiplied.
func TestBitmapPremultipliesAlpha(t *testing.T) {
	b := loadBitmap(t)
	for _, side := range []int{2, 6} {
		img, ok := b.Image(heartVS, side)
		if !ok {
			t.Fatal("no heart")
		}
		for _, p := range [][2]int{{0, 0}, {side - 1, side - 1}, {side / 2, side / 2}} {
			near(t, img, p[0], p[1], color.RGBA{128, 0, 0, 128}, 3)
		}
	}
}

func TestBitmapLigatureAndVariationSelectors(t *testing.T) {
	b := loadBitmap(t)
	for name, cluster := range map[string]string{
		"ZWJ ligature":             manWoman,
		"ligature with VS16":       manWoman + "\uFE0F",
		"VS16 after the middle":    man + zwj + "\uFE0F" + woman,
		"ligature repeated lookup": manWoman,
	} {
		img, ok := b.Image(cluster, 4)
		if !ok {
			t.Errorf("%s: no glyph", name)
			continue
		}
		near(t, img, 2, 2, color.RGBA{0, 255, 255, 255}, 0)
	}
	// Variation selectors the font has no ligature for are stripped.
	plain, _ := b.Image("\u2764", 4)
	vs16, ok := b.Image(heartVS, 4)
	vs15, ok15 := b.Image("\u2764\uFE0E", 4)
	if !ok || !ok15 || !bytes.Equal(plain.Pix, vs16.Pix) || !bytes.Equal(plain.Pix, vs15.Pix) {
		t.Errorf("heart with and without variation selectors differ (ok %v %v)", ok, ok15)
	}
}

// A sequence the font has no ligature for draws its first rune.
func TestBitmapUnknownSequenceFallsBackToFirstRune(t *testing.T) {
	b := loadBitmap(t)
	img, ok := b.Image(man+zwj+smiley+zwj+man, 4)
	if !ok {
		t.Fatal("no fallback glyph")
	}
	near(t, img, 2, 2, color.RGBA{0, 255, 0, 255}, 0)
}

func TestBitmapMissingGlyphs(t *testing.T) {
	b := loadBitmap(t)
	for name, c := range map[string]string{
		"empty": "", "not in cmap": "a", "cjk": "\u4E16",
		"in cmap, no bitmap": girl, "joiner alone": zwj, "selector alone": "\uFE0F",
	} {
		if _, ok := b.Image(c, 16); ok {
			t.Errorf("%s %q: unexpectedly drawn", name, c)
		}
	}
	for _, side := range []int{0, -3} {
		if _, ok := b.Image(smiley, side); ok {
			t.Errorf("side %d drawn", side)
		}
	}
}

// The strike is the smallest ppem >= side, else the largest; a glyph the
// strike lacks comes from another strike.
func TestBitmapStrikeChoice(t *testing.T) {
	b := loadBitmap(t)
	yellowPx := color.RGBA{255, 255, 0, 255}
	for _, tc := range []struct {
		side   int
		pixel  [2]int
		want   color.RGBA
		strike string
	}{
		{20, [2]int{2, 2}, yellowPx, "32 for 20"},
		{32, [2]int{2, 2}, yellowPx, "32 for 32"},
		{33, [2]int{1, 1}, color.RGBA{255, 0, 0, 255}, "96 for 33"},
		{96, [2]int{1, 1}, color.RGBA{255, 0, 0, 255}, "96 for 96"},
		{300, [2]int{1, 1}, color.RGBA{255, 0, 0, 255}, "96 for 300 (largest)"},
	} {
		img, ok := b.Image(smiley, tc.side)
		if !ok {
			t.Fatalf("%s: no glyph", tc.strike)
		}
		near(t, img, tc.pixel[0], tc.pixel[1], tc.want, 40)
	}
	// Strike 32 has no man; side 20 still finds the 96 ppem bitmap.
	img, ok := b.Image(man, 20)
	if !ok {
		t.Fatal("man at side 20: no glyph")
	}
	near(t, img, 10, 10, color.RGBA{0, 255, 0, 255}, 0)
}

func TestBitmapStrikeOrder(t *testing.T) {
	cblc, cbdt := buildBitmapTables(t, []testStrike{{ppem: 109}, {ppem: 20}, {ppem: 64}, {ppem: 48}})
	bt, err := parseBitmaps(tables{"CBLC": cblc, "CBDT": cbdt})
	if err != nil {
		t.Fatal(err)
	}
	for side, want := range map[int][]int{
		10: {20, 48, 64, 109}, 48: {48, 64, 109, 20}, 50: {64, 109, 48, 20}, 500: {109, 64, 48, 20},
	} {
		var got []int
		for _, s := range bt.strikeOrder(side) {
			got = append(got, s.ppem)
		}
		if !equalInts(got, want) {
			t.Errorf("side %d: strike order %v, want %v", side, got, want)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestBitmapIsCachedAndDeterministic(t *testing.T) {
	b := loadBitmap(t)
	x, _ := b.Image(smiley, 40)
	y, _ := b.Image(smiley, 40)
	if x != y {
		t.Error("second call did not return the cached image")
	}
	if z, _ := b.Image(smiley, 41); z == x {
		t.Error("a different size returned the cached image")
	}
	if w, _ := loadBitmap(t).Image(smiley, 40); !bytes.Equal(x.Pix, w.Pix) {
		t.Error("a fresh font rendered different pixels")
	}
}

func TestBitmapConcurrent(t *testing.T) {
	b := loadBitmap(t)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for _, c := range []string{smiley, manWoman, heartVS, woman, girl} {
				_, ok := b.Image(c, 16+i%3)
				if want := c != girl; ok != want {
					t.Errorf("%q: ok %v, want %v", c, ok, want)
				}
			}
		}()
	}
	wg.Wait()
}

func TestParseBitmapRejectsBadFonts(t *testing.T) {
	good := buildBitmapFont(t, testEmojiStrikes(t))
	for _, n := range []int{0, 8, 100, len(good) / 2, len(good) - 1} {
		if _, err := ParseBitmap(good[:n]); err == nil {
			t.Errorf("font cut to %d bytes parsed", n)
		}
	}
	tabs, err := readTables(good)
	if err != nil {
		t.Fatal(err)
	}
	corrupt := bytes.Clone(good)
	i := bytes.Index(corrupt, tabs["CBLC"])
	corrupt[i+1] = 9 // CBLC major version
	if _, err := ParseBitmap(corrupt); !errors.Is(err, ErrBitmap) {
		t.Errorf("corrupt CBLC: err = %v, want ErrBitmap", err)
	}
	noBitmaps := buildSFNT(map[string][]byte{"head": tabs["head"], "hhea": tabs["hhea"], "maxp": tabs["maxp"],
		"hmtx": tabs["hmtx"], "post": tabs["post"], "loca": tabs["loca"], "glyf": tabs["glyf"], "cmap": tabs["cmap"]})
	if _, err := ParseBitmap(noBitmaps); !errors.Is(err, ErrBitmap) {
		t.Errorf("font without CBLC/CBDT: err = %v, want ErrBitmap", err)
	}
}

// A glyph whose data is not a PNG, or claims a huge size, is not drawn.
func TestBitmapBadPNGIsNotDrawn(t *testing.T) {
	garbage := bytes.Repeat([]byte{0xAB}, 32)
	huge := solidPNG(t, 2, 2, red)
	copy(huge[16:], []byte{0, 1, 0, 0})                                    // IHDR width 65536 ...
	binary.BigEndian.PutUint32(huge[29:], crc32.ChecksumIEEE(huge[12:29])) // ... with a valid checksum
	for name, data := range map[string][]byte{"garbage": garbage, "huge": huge} {
		strikes := []testStrike{{ppem: 96, subs: []testSub{
			{first: gidSmiley, last: gidSmiley, indexFormat: 1, imageFormat: 17, png: map[uint16][]byte{gidSmiley: data}},
		}}}
		b, err := ParseBitmap(buildBitmapFont(t, strikes))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, ok := b.Image(smiley, 8); ok {
			t.Errorf("%s: drawn", name)
		}
	}
}

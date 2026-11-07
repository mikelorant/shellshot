package emoji

import (
	"errors"
	"image"
	"image/color"
	"strings"
	"sync"
	"testing"
)

// bitmapFontWith parses a synthetic font whose only bitmaps are those of subs.
func bitmapFontWith(t testing.TB, ppem int, subs ...testSub) *Bitmap {
	t.Helper()
	b, err := ParseBitmap(buildBitmapFont(t, []testStrike{{ppem: ppem, subs: subs}}))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// onlyGirl draws the girl yellow and nothing else.
func onlyGirl(t testing.TB) *Bitmap {
	return bitmapFontWith(t, 96, testSub{first: gidGirl, last: gidGirl, indexFormat: 1, imageFormat: 17,
		png: map[uint16][]byte{gidGirl: solidPNG(t, 2, 2, yellow)}})
}

// onlyMan draws the man green and, through the font's man ZWJ woman
// ligature, nothing else: the family glyph exists but has no bitmap.
func onlyMan(t testing.TB) *Bitmap {
	return bitmapFontWith(t, 96, testSub{first: gidMan, last: gidMan, indexFormat: 1, imageFormat: 17,
		png: map[uint16][]byte{gidMan: solidPNG(t, 2, 2, green)}})
}

func centre(t *testing.T, img *image.RGBA) color.RGBA {
	t.Helper()
	if img == nil {
		t.Fatal("nil image")
	}
	return img.RGBAAt(img.Bounds().Dx()/2, img.Bounds().Dy()/2)
}

var (
	yellowRGBA = color.RGBA{255, 255, 0, 255}
	greenRGBA  = color.RGBA{0, 255, 0, 255}
	cyanRGBA   = color.RGBA{0, 255, 255, 255}
)

// The first backend that has the cluster draws it, in the order given.
func TestChainOrder(t *testing.T) {
	full, girlFont := loadBitmap(t), onlyGirl(t)
	got := func(c Chain, cluster string) color.RGBA {
		img, ok := c.Image(cluster, 4)
		if !ok {
			t.Fatalf("chain of %d drew nothing for %q", len(c), cluster)
		}
		return centre(t, img)
	}
	if g := got(Chain{full, girlFont}, man); g != greenRGBA {
		t.Errorf("man from the full font = %v", g)
	}
	// Only the girl font has the girl bitmap: the full font has the glyph in
	// its cmap without a bitmap and so passes it on, in either order.
	for _, c := range []Chain{{full, girlFont}, {girlFont, full}} {
		if g := got(c, girl); g != yellowRGBA {
			t.Errorf("girl = %v, want yellow from the font with her bitmap", g)
		}
	}
	// A cluster both have is drawn by the first.
	twice := Chain{bitmapFontWith(t, 96, testSub{first: gidMan, last: gidMan, indexFormat: 1, imageFormat: 17,
		png: map[uint16][]byte{gidMan: solidPNG(t, 2, 2, cyan)}}), full}
	if g := got(twice, man); g != cyanRGBA {
		t.Errorf("first backend did not win: %v", g)
	}
}

func TestChainDrawsNothingWhenNoMemberDoes(t *testing.T) {
	for name, c := range map[string]Chain{
		"empty":   nil,
		"one":     {loadBitmap(t)},
		"several": {loadBitmap(t), onlyGirl(t), onlyMan(t)},
	} {
		if img, ok := c.Image("\u4E16", 16); ok || img != nil {
			t.Errorf("%s: drew a hanzi", name)
		}
		if _, ok := c.Image(smiley, 0); ok {
			t.Errorf("%s: drew at side 0", name)
		}
	}
}

// A font that knows only the first emoji of a sequence does not shadow a
// later font that has the whole sequence, but still draws that first emoji
// when no font has the sequence.
func TestChainPrefersWholeSequenceOverFirstEmoji(t *testing.T) {
	full, manFont := loadBitmap(t), onlyMan(t)

	if img, ok := manFont.Image(manWoman, 4); !ok || centre(t, img) != greenRGBA {
		t.Fatal("the single-emoji font should degrade the family to the man")
	}
	for _, c := range []Chain{{manFont, full}, {full, manFont}, {Chain{manFont}, Chain{full}}} {
		img, ok := c.Image(manWoman, 4)
		if !ok || centre(t, img) != cyanRGBA {
			t.Errorf("chain %d: family not drawn whole (ok %v)", len(c), ok)
		}
	}

	// No font has man ZWJ smiley ZWJ man: the first one that can draws the man.
	img, ok := Chain{manFont, full}.Image(man+zwj+smiley+zwj+man, 4)
	if !ok || centre(t, img) != greenRGBA {
		t.Errorf("fallback to the first emoji failed (ok %v)", ok)
	}
}

// Chains nest, as when a user font is put in front of a built-in set.
func TestChainNests(t *testing.T) {
	full, girlFont := loadBitmap(t), onlyGirl(t)
	c := Chain{girlFont, Chain{onlyMan(t), full}}
	for cluster, want := range map[string]color.RGBA{girl: yellowRGBA, man: greenRGBA, manWoman: cyanRGBA} {
		img, ok := c.Image(cluster, 4)
		if !ok || centre(t, img) != want {
			t.Errorf("nested chain %q: ok %v, colour %v, want %v", cluster, ok, centre(t, img), want)
		}
	}
}

func TestChainConcurrent(t *testing.T) {
	c := Chain{onlyMan(t), loadBitmap(t)}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				if _, ok := c.Image(manWoman, 8); !ok {
					t.Error("no image")
				}
			}
		})
	}
	wg.Wait()
}

type errBackend struct{ err error }

func (errBackend) Image(string, int) (*image.RGBA, bool) { return nil, false }
func (e errBackend) Err() error                          { return e.err }

func TestLoadErrJoinsMembers(t *testing.T) {
	a, b := errors.New("apple broke"), errors.New("noto broke")
	if LoadErr(Chain{loadBitmap(t), Chain{errBackend{a}, errBackend{b}}}) == nil {
		t.Fatal("no error")
	}
	err := LoadErr(Chain{errBackend{a}, Chain{errBackend{b}}})
	if !errors.Is(err, a) || !errors.Is(err, b) {
		t.Errorf("LoadErr = %v, want both errors", err)
	}
	if err := LoadErr(Chain{loadBitmap(t), errBackend{}}); err != nil {
		t.Errorf("LoadErr without failures = %v", err)
	}
	if !strings.Contains(LoadErr(errBackend{a}).Error(), "apple") {
		t.Error("single backend error lost")
	}
}

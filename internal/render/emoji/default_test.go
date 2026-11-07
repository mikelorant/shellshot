package emoji

import (
	"bytes"
	"strings"
	"sync"
	"testing"

	"github.com/klauspost/compress/zstd"
)

// compress returns data as one zstd frame.
func compress(t testing.TB, data []byte) []byte {
	t.Helper()
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	return enc.EncodeAll(data, nil)
}

// syntheticDefault builds a default chain from the synthetic fonts: "apple"
// is the full font, "noto" has only the girl.
func syntheticDefault(t *testing.T, set Set) Chain {
	t.Helper()
	apple := compress(t, buildBitmapFont(t, testEmojiStrikes(t)))
	noto := compress(t, buildBitmapFont(t, []testStrike{{ppem: 96, subs: []testSub{
		{first: gidGirl, last: gidGirl, indexFormat: 1, imageFormat: 17, png: map[uint16][]byte{gidGirl: solidPNG(t, 2, 2, yellow)}},
	}}}))
	return newDefault(set, func() []byte { return apple }, func() []byte { return noto })
}

// Nothing is inflated until a cluster is asked for, and a font that is not
// needed is never inflated.
func TestDefaultInflatesLazily(t *testing.T) {
	before := inflations.Load()
	c := syntheticDefault(t, SetApple)
	if n := inflations.Load() - before; n != 0 {
		t.Fatalf("building the chain inflated %d fonts", n)
	}
	if err := LoadErr(c); err != nil {
		t.Fatalf("LoadErr before use = %v", err)
	}
	if n := inflations.Load() - before; n != 0 {
		t.Fatalf("LoadErr inflated %d fonts", n)
	}

	if _, ok := c.Image(smiley, 8); !ok {
		t.Fatal("smiley not drawn")
	}
	if n := inflations.Load() - before; n != 1 {
		t.Errorf("after one emoji the primary font is inflated %d times, want 1", n)
	}
	for range 3 {
		c.Image(smiley, 8)
		c.Image(man, 8)
	}
	if n := inflations.Load() - before; n != 1 {
		t.Errorf("the fallback font was inflated for clusters the primary has (%d inflations)", n)
	}

	// The girl has no bitmap in the primary font; now the fallback is inflated, once.
	if _, ok := c.Image(girl, 8); !ok {
		t.Fatal("girl not drawn by the fallback")
	}
	c.Image(girl, 8)
	if n := inflations.Load() - before; n != 2 {
		t.Errorf("%d inflations after the fallback was needed, want 2", n)
	}
}

func TestDefaultChainsPerSet(t *testing.T) {
	apple, noto := syntheticDefault(t, SetApple), syntheticDefault(t, SetNoto)
	if len(apple) != 2 || len(noto) != 1 {
		t.Fatalf("chain lengths apple %d, noto %d; want 2 and 1", len(apple), len(noto))
	}
	if _, ok := apple.Image(smiley, 8); !ok {
		t.Error("apple set does not draw the smiley")
	}
	// In the synthetic pair the noto font has only the girl.
	if _, ok := noto.Image(smiley, 8); ok {
		t.Error("noto-only set drew the smiley from the apple font")
	}
	if _, ok := noto.Image(girl, 8); !ok {
		t.Error("noto-only set lacks the girl")
	}
}

func TestDefaultErrorsAreReportedOnceAndDrawNothing(t *testing.T) {
	broken := []byte("not zstd")
	good := compress(t, buildBitmapFont(t, testEmojiStrikes(t)))
	c := newDefault(SetApple, func() []byte { return broken }, func() []byte { return good })

	if _, ok := c.Image(smiley, 8); !ok {
		t.Error("the chain should continue with the fallback font when the primary cannot load")
	}
	err := LoadErr(c)
	if err == nil || !strings.Contains(err.Error(), "Apple Color Emoji") {
		t.Fatalf("LoadErr = %v, want an error naming the Apple font", err)
	}
	before := inflations.Load()
	c.Image(man, 8)
	c.Image(man, 8)
	if n := inflations.Load() - before; n != 0 {
		t.Errorf("a failed font was inflated again %d times", n)
	}
	if err2 := LoadErr(c); err2 == nil || err2.Error() != err.Error() {
		t.Errorf("error changed: %v then %v", err, err2)
	}

	// A font that inflates but has no colour tables fails the same way.
	noColour := compress(t, readAsset(t, "jetbrains-mono/JetBrainsMono-Regular.ttf"))
	c = newDefault(SetNoto, func() []byte { return nil }, func() []byte { return noColour })
	if _, ok := c.Image(smiley, 8); ok {
		t.Error("drew with a font without colour tables")
	}
	if err := LoadErr(c); err == nil || !strings.Contains(err.Error(), "Noto Color Emoji") {
		t.Errorf("LoadErr = %v, want an error naming the Noto font", err)
	}
}

func TestDefaultConcurrentFirstUse(t *testing.T) {
	c := syntheticDefault(t, SetApple)
	before := inflations.Load()
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 20 {
				if _, ok := c.Image(smiley, 8); !ok {
					t.Error("smiley not drawn")
				}
			}
		})
	}
	wg.Wait()
	if n := inflations.Load() - before; n != 1 {
		t.Errorf("%d inflations from concurrent first use, want 1", n)
	}
}

func TestDefaultReturnsSharedChainPerSet(t *testing.T) {
	a1, err := Default(SetApple)
	if err != nil {
		t.Fatal(err)
	}
	a2, _ := Default(SetApple)
	n1, _ := Default(SetNoto)
	c1, c2 := a1.(Chain), a2.(Chain)
	if len(c1) != 2 || &c1[0] != &c2[0] {
		t.Error("Default(SetApple) is not one shared two-font chain")
	}
	if len(n1.(Chain)) != 1 {
		t.Errorf("Default(SetNoto) has %d fonts, want 1", len(n1.(Chain)))
	}
	if _, err := Default(Set(7)); err == nil {
		t.Error("unknown set accepted")
	}
}

func TestParseSet(t *testing.T) {
	for name, want := range map[string]Set{"apple": SetApple, "noto": SetNoto} {
		got, err := ParseSet(name)
		if err != nil || got != want || got.String() != name {
			t.Errorf("ParseSet(%q) = %v, %v", name, got, err)
		}
	}
	for _, bad := range []string{"", "Apple", "twemoji", "apple,noto"} {
		if _, err := ParseSet(bad); err == nil || !strings.Contains(err.Error(), "apple or noto") {
			t.Errorf("ParseSet(%q) err = %v", bad, err)
		}
	}
}

// The embedded fonts as the app uses them: Apple's cmap maps U+2640 FE0F,
// U+2642 FE0F and U+2695 FE0F to glyphs that have no bitmap, Noto draws
// them, and the Apple set must too.
func TestRealDefaultSetsDrawTheSymbolsApplesLacks(t *testing.T) {
	apple, err := Default(SetApple)
	if err != nil {
		t.Fatal(err)
	}
	noto, _ := Default(SetNoto)

	appleOnly := newLoaded(t, decompressedAppleFont(t))
	for _, cluster := range []string{"\u2640\uFE0F", "\u2642\uFE0F", "\u2695\uFE0F"} {
		if img, ok := appleOnly.Image(cluster, 32); ok || img != nil {
			t.Errorf("Apple alone drew %U: ok %v, image %v", []rune(cluster), ok, img != nil)
		}
		for name, b := range map[string]Backend{"apple set": apple, "noto set": noto} {
			img, ok := b.Image(cluster, 32)
			if !ok || !hasInk(img.Pix) {
				t.Errorf("%s: %U not drawn (ok %v)", name, []rune(cluster), ok)
			}
		}
	}
	// Apple's font also lacks some newer emoji, among them U+1FAF9, its skin tone variants and U+1FA8B.
	for _, cluster := range []string{"\U0001FAF9", "\U0001FAF9\U0001F3FD", "\U0001FA8B"} {
		if _, ok := appleOnly.Image(cluster, 32); ok {
			t.Errorf("Apple alone draws %U; pick another emoji for this test", []rune(cluster))
		}
		if img, ok := apple.Image(cluster, 32); !ok || !hasInk(img.Pix) {
			t.Errorf("apple set lacks %U", []rune(cluster))
		}
	}
	if err := LoadErr(apple); err != nil {
		t.Errorf("LoadErr(apple) = %v", err)
	}
	if err := LoadErr(noto); err != nil {
		t.Errorf("LoadErr(noto) = %v", err)
	}
}

func newLoaded(t *testing.T, data []byte) Backend {
	t.Helper()
	b, err := Load(data)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func hasInk(pix []byte) bool {
	return !bytes.Equal(pix, make([]byte, len(pix)))
}

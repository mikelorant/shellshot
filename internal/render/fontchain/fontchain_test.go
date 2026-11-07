package fontchain

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/mikelorant/shellshot/internal/render/emoji"
)

// colourEmojiFont writes the embedded Noto Color Emoji font to a temporary
// file and returns its path. It is a valid font that maps U+1F600, which no
// embedded text font has, so it shows whether a user font joined the chain.
func colourEmojiFont(t *testing.T) string {
	t.Helper()
	dec, err := zstd.NewReader(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer dec.Close()
	data, err := dec.DecodeAll(emoji.NotoFontZstd(), nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "ColourEmoji.ttf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func hasGlyph(t *testing.T, o Options, r rune) bool {
	t.Helper()
	set, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	_, ok, err := set.FaceFor(r, false, false, 14)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestNewEmbeddedChainDrawsCJKAndNerdIcons(t *testing.T) {
	for _, r := range []rune{'世', '한', 'こ', 0xF015} {
		if !hasGlyph(t, Options{}, r) {
			t.Errorf("default chain has no glyph for %U", r)
		}
	}
	if hasGlyph(t, Options{}, 0x1F600) {
		t.Error("default chain has a text glyph for an emoji")
	}
}

func TestNewAddsFontFiles(t *testing.T) {
	if !hasGlyph(t, Options{Files: []string{colourEmojiFont(t)}}, 0x1F600) {
		t.Error("the --fallback-font file did not join the chain")
	}
}

func TestNewFontFileErrors(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.ttf")
	if err := os.WriteFile(garbage, []byte("this is not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	empty := filepath.Join(dir, "empty.otf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing.ttf")
	for _, tc := range []struct {
		name  string
		files []string
		want  string
	}{
		{"missing file", []string{missing}, missing},
		{"directory", []string{dir}, dir},
		{"garbage", []string{garbage}, garbage + " is not a valid font"},
		{"empty", []string{empty}, empty + " is not a valid font"},
		{"good then bad", []string{colourEmojiFont(t), garbage}, garbage + " is not a valid font"},
	} {
		set, err := New(Options{Files: tc.files})
		if err == nil || set != nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: New = %v, %v; want an error containing %q", tc.name, set, err, tc.want)
		}
	}
}

func TestReadFilesSkipsBrokenFontsWhenNotStrict(t *testing.T) {
	garbage := filepath.Join(t.TempDir(), "garbage.ttf")
	if err := os.WriteFile(garbage, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	good := colourEmojiFont(t)
	fonts, err := readFiles([]string{"/no/such/font.ttf", garbage, good}, false)
	if err != nil || len(fonts) != 1 || fonts[0].Name != good {
		t.Errorf("readFiles = %d fonts, %v; want only %s", len(fonts), err, good)
	}
}

func touch(t *testing.T, root string, names ...string) {
	t.Helper()
	for _, n := range names {
		p := filepath.Join(root, n)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSystemFontsPicksFamiliesInPriorityOrder(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	touch(t, a,
		"Arial Unicode.ttf",
		"opentype/noto/NotoSansCJK-Bold.ttc",
		"opentype/noto/NotoSansCJK-Regular.ttc",
		"opentype/noto/NotoSansCJK-Black.ttc",
		"Hiragino Sans GB.ttc",
		"readme.txt",
		"Menlo.ttc",
		"Helvetica.ttf")
	touch(t, b, "AppleSDGothicNeo.ttc", "Songti.ttc")

	got := SystemFonts([]string{a, b, filepath.Join(a, "no-such-dir")})
	rel := func(p string) string {
		for _, root := range []string{a, b} {
			if r, err := filepath.Rel(root, p); err == nil && !strings.HasPrefix(r, "..") {
				return r
			}
		}
		return p
	}
	var names []string
	for _, p := range got {
		names = append(names, rel(p))
	}
	want := []string{
		"Hiragino Sans GB.ttc", "AppleSDGothicNeo.ttc", "Songti.ttc",
		"opentype/noto/NotoSansCJK-Regular.ttc", "Arial Unicode.ttf",
	}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("SystemFonts = %q\nwant %q", names, want)
	}
}

func TestSystemFontsNoneInstalled(t *testing.T) {
	dir := t.TempDir()
	touch(t, dir, "Helvetica.ttf", "Menlo.ttc")
	if got := SystemFonts([]string{dir}); len(got) != 0 {
		t.Errorf("SystemFonts = %q, want none", got)
	}
	if got := SystemFonts(nil); len(got) != 0 {
		t.Errorf("SystemFonts(nil) = %q, want none", got)
	}
}

func TestPreferredBreaksTiesDeterministically(t *testing.T) {
	if !preferred("/x/NotoSansCJK-Regular.ttc", "/x/NotoSansCJK-Bold.ttc") {
		t.Error("regular weight not preferred")
	}
	if !preferred("/a/F.ttc", "/b/F.ttc") || preferred("/b/F.ttc", "/a/F.ttc") {
		t.Error("equal names not ordered by path")
	}
	if !preferred("/x/Noto.ttc", "/x/NotoBold.ttc") {
		t.Error("shorter name not preferred")
	}
}

// --system-fonts must work on any machine, with or without matching fonts
// installed.
func TestNewWithSystemFonts(t *testing.T) {
	if _, err := New(Options{System: true}); err != nil {
		t.Fatal(err)
	}
}

func TestNewEmojiSets(t *testing.T) {
	for _, set := range []emoji.Set{emoji.SetApple, emoji.SetNoto} {
		fonts, err := New(Options{EmojiSet: set})
		if err != nil {
			t.Fatal(err)
		}
		for _, cluster := range []string{"\U0001F600", "\u2640\uFE0F", "\U0001FAF9"} {
			if img, ok := fonts.Emoji(cluster, 16); !ok || img == nil {
				t.Errorf("%v set: no emoji for %+q", set, cluster)
			}
		}
	}
	if _, err := New(Options{EmojiSet: emoji.Set(9)}); err == nil {
		t.Error("unknown emoji set accepted")
	}
}

// The two sets draw different artwork for the same emoji, and --emoji-font
// artwork wins over both.
func TestNewEmojiFontComesFirst(t *testing.T) {
	apple, err := New(Options{EmojiSet: emoji.SetApple})
	if err != nil {
		t.Fatal(err)
	}
	noto, err := New(Options{EmojiSet: emoji.SetNoto})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := apple.Emoji("\U0001F600", 32)
	n, _ := noto.Emoji("\U0001F600", 32)
	if bytes.Equal(a.Pix, n.Pix) {
		t.Error("apple and noto sets drew identical grins")
	}

	// The user font is Noto itself, so with the apple set behind it the
	// grin is Noto's.
	user, err := New(Options{EmojiSet: emoji.SetApple, EmojiFile: colourEmojiFont(t)})
	if err != nil {
		t.Fatal(err)
	}
	u, ok := user.Emoji("\U0001F600", 32)
	if !ok || !bytes.Equal(u.Pix, n.Pix) {
		t.Errorf("user font did not come first (ok %v)", ok)
	}
}

func TestNewEmojiFontErrors(t *testing.T) {
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.ttf")
	if err := os.WriteFile(garbage, []byte("this is not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	mono := "../font/assets/jetbrains-mono/JetBrainsMono-Regular.ttf"
	missing := filepath.Join(dir, "missing.ttf")
	for _, tc := range []struct{ name, file, want string }{
		{"missing", missing, missing},
		{"directory", dir, dir},
		{"garbage", garbage, garbage + " is not a usable colour emoji font"},
		{"monochrome font", mono, mono + " is not a usable colour emoji font (it needs COLR v0 + CPAL or CBLC + CBDT"},
	} {
		set, err := New(Options{EmojiFile: tc.file})
		if err == nil || set != nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: New = %v, %v; want an error containing %q", tc.name, set, err, tc.want)
		}
	}
}

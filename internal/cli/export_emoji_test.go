package cli

import (
	"bytes"
	"image"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/render/emoji"
	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/raster"
)

// The emoji of the recording below, in order, each two cells wide and
// followed by a space, so emoji i starts at column 3*i. The last two are
// ones that Apple's font cannot draw: U+2640 U+FE0F maps to a glyph without
// a bitmap, and the E18.0 monarch butterfly is missing from it.
var exportEmoji = []string{
	"\U0001F600", "\U0001F44D\U0001F3FD", "\u2764\uFE0F", "\U0001F468\u200D\U0001F469\u200D\U0001F467",
	"\U0001F1E6\U0001F1FA", "1\uFE0F\u20E3", "\U0001F3F3\uFE0F\u200D\U0001F308", "\u2640\uFE0F", "\U0001FACC",
}

const exportEmojiCols = 3*9 + 4 // nine emoji with a space after each, then 世界

func writeEmojiRecording(t *testing.T) string {
	t.Helper()
	data := buildRecording(t, head.Head{Cols: exportEmojiCols, Rows: 1}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(100*time.Millisecond, []byte(strings.Join(exportEmoji, " ")+" 世界")))
		must(t, w.Exit(200*time.Millisecond, 0))
	})
	return writeFile(t, "emoji-sheet.shot", data)
}

// exportLastFrame exports in with the given extra flags and returns the last
// frame.
func exportLastFrame(t *testing.T, in string, flags ...string) *image.RGBA {
	t.Helper()
	out := filepath.Join(t.TempDir(), "out.png")
	args := append([]string{in, "-o", out, "--hold", "1s", "--cursor", "none"}, flags...)
	if code, stderr := runExportCmd(t, args...); code != 0 {
		t.Fatalf("export %v: exit %d, stderr %q", flags, code, stderr)
	}
	a := decodeAPNG(t, out)
	return compose(t, a, len(a.Frames)-1)
}

// cellRect is the pixel rectangle of columns [col, col+cells) of row 0.
func cellRect(t *testing.T, col, cells int) image.Rectangle {
	t.Helper()
	fonts, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	r, err := raster.New(fonts, raster.Options{})
	if err != nil {
		t.Fatal(err)
	}
	w, h := r.Size(exportEmojiCols, 1)
	cellW := (w - 2*raster.DefaultPadding) / exportEmojiCols
	cellH := h - 2*raster.DefaultPadding
	return image.Rect(raster.DefaultPadding+col*cellW, raster.DefaultPadding, raster.DefaultPadding+(col+cells)*cellW, raster.DefaultPadding+cellH)
}

// inked counts the pixels of r that differ from the background at the
// corner of the image.
func inked(img *image.RGBA, r image.Rectangle) int {
	bg := img.RGBAAt(0, 0)
	n := 0
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.RGBAAt(x, y) != bg {
				n++
			}
		}
	}
	return n
}

func samePixelsIn(img *image.RGBA, other *image.RGBA, r image.Rectangle) bool {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if img.RGBAAt(x, y) != other.RGBAAt(x, y) {
				return false
			}
		}
	}
	return true
}

// notoFontFile writes the embedded Noto Color Emoji font to a temporary file.
func notoFontFile(t *testing.T) string {
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
	path := filepath.Join(t.TempDir(), "Noto.ttf")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// Both sets draw every emoji, including the ones missing from Apple's font
// (which the apple set takes from Noto), and differ in artwork; the text is
// the same.
func TestExportEmojiSets(t *testing.T) {
	in := writeEmojiRecording(t)
	apple := exportLastFrame(t, in, "--emoji-set", "apple")
	noto := exportLastFrame(t, in, "--emoji-set", "noto")
	deflt := exportLastFrame(t, in)

	if !samePixelsIn(apple, deflt, apple.Bounds()) {
		t.Error("the default emoji set is not apple")
	}
	for i, cluster := range exportEmoji {
		cells := cellRect(t, 3*i, 2)
		for name, img := range map[string]*image.RGBA{"apple": apple, "noto": noto} {
			if n := inked(img, cells); n < 60 {
				t.Errorf("%s set: %+q has %d inked pixels in its two cells", name, cluster, n)
			}
		}
		same := samePixelsIn(apple, noto, cells)
		if i < 7 && same {
			t.Errorf("%+q: apple and noto sets drew the same pixels", cluster)
		}
		// Apple's font cannot draw the last two: the apple set falls back to Noto's.
		if i >= 7 && !same {
			t.Errorf("%+q: the apple set did not fall back to Noto's artwork", cluster)
		}
	}
	if text := cellRect(t, 3*9, 4); !samePixelsIn(apple, noto, text) {
		t.Error("the emoji set changed the CJK text")
	}
}

// A user emoji font goes first: Noto given as --emoji-font over the apple
// set draws exactly what --emoji-set noto draws.
func TestExportEmojiFontComesFirst(t *testing.T) {
	in := writeEmojiRecording(t)
	noto := exportLastFrame(t, in, "--emoji-set", "noto")
	user := exportLastFrame(t, in, "--emoji-set", "apple", "--emoji-font", notoFontFile(t))
	if !samePixelsIn(user, noto, user.Bounds()) {
		t.Error("--emoji-font artwork was not used before the apple set")
	}
}

func TestExportEmojiFlagErrors(t *testing.T) {
	in := writeExportFixture(t)
	dir := t.TempDir()
	garbage := filepath.Join(dir, "garbage.ttf")
	if err := os.WriteFile(garbage, []byte("not a font"), 0o644); err != nil {
		t.Fatal(err)
	}
	mono := "../render/font/assets/jetbrains-mono/JetBrainsMono-Regular.ttf"
	missing := filepath.Join(dir, "missing.ttf")
	for _, tc := range []struct {
		name  string
		flags []string
		want  []string
	}{
		{"unknown set", []string{"--emoji-set", "twemoji"}, []string{"invalid --emoji-set", "twemoji", "apple or noto"}},
		{"empty set", []string{"--emoji-set", ""}, []string{"invalid --emoji-set", "apple or noto"}},
		{"missing font", []string{"--emoji-font", missing}, []string{"--emoji-font", missing}},
		{"directory", []string{"--emoji-font", dir}, []string{"--emoji-font", dir}},
		{"not a font", []string{"--emoji-font", garbage}, []string{"--emoji-font", garbage, "not a usable colour emoji font", "COLR v0 + CPAL or CBLC + CBDT"}},
		{"monochrome font", []string{"--emoji-font", mono}, []string{"--emoji-font", mono, "not a usable colour emoji font", "COLR v0 + CPAL or CBLC + CBDT"}},
	} {
		out := filepath.Join(dir, strings.ReplaceAll(tc.name, " ", "-")+".png")
		code, stderr := runExportCmd(t, append([]string{in, "-o", out}, tc.flags...)...)
		if code == 0 {
			t.Errorf("%s: export succeeded", tc.name)
		}
		for _, w := range tc.want {
			if !strings.Contains(stderr, w) {
				t.Errorf("%s: stderr %q lacks %q", tc.name, stderr, w)
			}
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("%s: output was written despite the error", tc.name)
		}
	}
}

func TestExportHelpExplainsEmojiSets(t *testing.T) {
	cmd := newExportCmd()
	usage := cmd.Flags().FlagUsages()
	for _, want := range []string{
		"--emoji-set", "apple or noto", "belongs to Apple Inc.", "licence-clean choice for exports you share",
		"--emoji-font", "COLR v0 or CBDT/CBLC",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("export flag help lacks %q:\n%s", want, usage)
		}
	}
	if f := cmd.Flags().Lookup("emoji-set"); f == nil || f.DefValue != "apple" {
		t.Errorf("--emoji-set default = %v, want apple", f)
	}
}

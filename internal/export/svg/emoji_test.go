package svg

import (
	"image"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// emojiCluster is a grapheme cluster and the cells a terminal gives it.
type emojiCluster struct {
	text  string
	cells int
}

// emojiClusters are the cluster shapes a terminal reports: a plain emoji, a
// skin tone, a ZWJ family, a flag, a keycap, a variation selector 16 emoji
// of one cell and of two.
var emojiClusters = []emojiCluster{
	{"\U0001F600", 2},
	{"\U0001F44D\U0001F3FD", 2},
	{"\U0001F468\u200D\U0001F469\u200D\U0001F467", 2},
	{"\U0001F1E6\U0001F1FA", 2},
	{"1\uFE0F\u20E3", 1},
	{"\u2764\uFE0F", 1},
	{"\u2764\uFE0F", 2},
	{"\U0001F3F3\uFE0F\u200D\U0001F308", 2},
}

// emojiRow returns a one-row snapshot that has each cluster followed by a
// space, and the first column of each cluster. The base rune of a cluster
// takes its cells; every other rune is a zero-width mark.
func emojiRow(t testing.TB, clusters []emojiCluster) (s snapshot.Snapshot, cols []int) {
	t.Helper()
	var (
		line  strings.Builder
		wides []snapshot.Wide
		runes int
		col   int
	)
	for _, c := range clusters {
		cols = append(cols, col)
		for i, r := range []rune(c.text) {
			switch {
			case i > 0:
				wides = append(wides, snapshot.Wide{Rune: runes, Cells: 0})
			case c.cells == 2:
				wides = append(wides, snapshot.Wide{Rune: runes, Cells: 2})
			}
			line.WriteRune(r)
			runes++
		}
		line.WriteByte(' ')
		runes++
		col += c.cells + 1
	}
	s = snapshot.Snapshot{
		Cols: col, Rows: 1, Lines: []string{strings.TrimRight(line.String(), " ")},
		Widths: [][]snapshot.Wide{wides},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	return s, cols
}

// countingGeometry counts the requests for emoji images.
type countingGeometry struct {
	Geometry
	emoji int
}

func (g *countingGeometry) Emoji(cluster string, side int) (img *image.RGBA, ok bool) {
	g.emoji++
	return g.Geometry.Emoji(cluster, side)
}

func TestParseEmojiMode(t *testing.T) {
	t.Parallel()
	if m, err := ParseEmojiMode("text"); err != nil || m != EmojiText || m.String() != "text" {
		t.Errorf("text: %v, %v", m, err)
	}
	if m, err := ParseEmojiMode("image"); err != nil || m != EmojiImage || m.String() != "image" {
		t.Errorf("image: %v, %v", m, err)
	}
	if _, err := ParseEmojiMode("png"); err == nil || !strings.Contains(err.Error(), "png") {
		t.Errorf("png: err = %v", err)
	}
	if (Options{}).Emoji != EmojiText {
		t.Error("the zero Options do not draw emoji as text")
	}
}

func TestEmojiTextDrawsEachClusterOnItsCells(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s, cols := emojiRow(t, emojiClusters)
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, s.Cols, 1, step(s, time.Second))
	if strings.Contains(doc, "<image") || strings.Contains(doc, "<defs") || strings.Contains(doc, "<use ") {
		t.Errorf("text emoji left images or shapes in the document:\n%s", normalise(doc))
	}
	for i, c := range emojiClusters {
		left := float64(lay.Pad + cols[i]*lay.CellWidth)
		var want string
		if c.cells == 2 {
			want = `<tspan x="` + num(left+float64(2*lay.CellWidth)/2) + `" text-anchor="middle">` + c.text + `</tspan>`
		} else {
			want = `<tspan x="` + num(left+lay.GlyphOffsetX) + `">` + c.text + `</tspan>`
		}
		if !strings.Contains(doc, want) {
			t.Errorf("cluster %d: document lacks %s\n%s", i, want, normalise(doc))
		}
	}
	if n := len(textRunRe.FindAllString(doc, -1)); n != len(emojiClusters) {
		t.Errorf("%d text runs, want one for each of the %d clusters", n, len(emojiClusters))
	}
}

func TestEmojiTextNeverReadsTheEmojiFonts(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s, _ := emojiRow(t, emojiClusters)
	for _, tc := range []struct {
		o         Options
		wantReads bool
	}{
		{Options{Emoji: EmojiText}, false},
		{Options{Emoji: EmojiText, Text: TextPaths}, false},
		{Options{Emoji: EmojiImage}, true},
	} {
		cg := &countingGeometry{Geometry: g}
		tc.o.Geometry = cg
		tc.o.Frame = Frame{Kind: FrameLast}
		write(t, tc.o, s.Cols, 1, step(s, time.Second))
		if (cg.emoji > 0) != tc.wantReads {
			t.Errorf("%+v: %d emoji image requests, want some: %v", tc.o.Emoji, cg.emoji, tc.wantReads)
		}
	}
}

// With --svg-text paths the outline fonts cannot draw an emoji, so text emoji
// stay <text> among the outlines of the other cells.
func TestEmojiTextInPathsModeStaysText(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := screen(t, 10, "a\U0001F600b")
	doc := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameLast}}, 10, 1, step(s, time.Second))
	if strings.Contains(doc, "<image") {
		t.Errorf("paths mode with text emoji has an image:\n%s", normalise(doc))
	}
	want := `<tspan x="` + num(float64(lay.Pad+lay.CellWidth)+float64(2*lay.CellWidth)/2) + `" text-anchor="middle">` + "\U0001F600" + `</tspan>`
	if !strings.Contains(doc, want) {
		t.Errorf("the emoji is not text on its cells (want %s):\n%s", want, normalise(doc))
	}
	if n := len(useRe.FindAllString(doc, -1)); n != 2 {
		t.Errorf("%d outlines, want 2 (a and b)", n)
	}
}

func TestEmojiImageDrawsClustersAsImages(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s, _ := emojiRow(t, emojiClusters[:4])
	doc := write(t, Options{Geometry: g, Emoji: EmojiImage, Frame: Frame{Kind: FrameLast}}, s.Cols, 1, step(s, time.Second))
	if n := strings.Count(doc, "<image "); n != 4 {
		t.Errorf("%d images, want 4", n)
	}
	if n := len(useRe.FindAllString(doc, -1)); n != 4 {
		t.Errorf("%d uses, want 4", n)
	}
	if strings.Contains(doc, "<text") {
		t.Errorf("image emoji left text behind:\n%s", normalise(doc))
	}
}

// Powerline sprites, Nerd Font icons, box drawing and plain text are decided
// before and apart from emoji, so the emoji mode changes nothing for them.
func TestEmojiModeLeavesSpritesIconsAndTextAlone(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 20,
		"\ue0b0\ue0b2 ┌─┐ █▄ ⣿ abc",
		"\uf015 \uf015\ufe0f \uf07b x",
	)
	for _, m := range []TextMode{TextText, TextPaths} {
		text := write(t, Options{Geometry: g, Text: m, Emoji: EmojiText, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
		asImage := write(t, Options{Geometry: g, Text: m, Emoji: EmojiImage, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
		if text != asImage {
			t.Errorf("%v: a screen without emoji differs between emoji modes\ntext:\n%s\nimage:\n%s", m, normalise(text), normalise(asImage))
		}
		if !strings.Contains(text, `href="#s`) || !strings.Contains(text, `href="#g`) {
			t.Errorf("%v: the screen drew no sprite or outline:\n%s", m, normalise(text))
		}
	}
}

func TestEmojiTextBesideSpritesAndIconsKeepsTheirShapes(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 20, "\ue0b0 \uf015\ufe0f ┌ \U0001F600 x")
	shapes := func(doc string) string {
		var out []string
		for _, u := range useRe.FindAllString(doc, -1) {
			if !strings.Contains(u, `href="#e`) {
				out = append(out, u)
			}
		}
		return strings.Join(out, "\n")
	}
	text := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, s.Cols, 1, step(s, time.Second))
	asImage := write(t, Options{Geometry: g, Emoji: EmojiImage, Frame: Frame{Kind: FrameLast}}, s.Cols, 1, step(s, time.Second))
	if got, want := shapes(text), shapes(asImage); got != want || got == "" {
		t.Errorf("sprites and icons differ between emoji modes\ntext:\n%s\nimage:\n%s", got, want)
	}
	if strings.Contains(text, "\uf015") {
		t.Error("the icon is text, not an outline")
	}
}

func TestEmojiTextInAnimation(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	steps := []timeline.Step{
		step(screen(t, 10, "go \U0001F600"), 300*time.Millisecond),
		step(screen(t, 10, "go \U0001F44D"), 300*time.Millisecond),
	}
	doc := write(t, Options{Geometry: g}, 10, 1, steps...)
	if strings.Contains(doc, "<image") {
		t.Errorf("animated text emoji has an image:\n%s", normalise(doc))
	}
	for _, e := range []string{"\U0001F600", "\U0001F44D"} {
		if !strings.Contains(doc, e) {
			t.Errorf("animation lacks %s as text", e)
		}
	}
	img := write(t, Options{Geometry: g, Emoji: EmojiImage}, 10, 1, steps...)
	if n := strings.Count(img, "<image "); n != 2 {
		t.Errorf("animated image emoji: %d images, want 2", n)
	}
}

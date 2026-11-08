package svg

import (
	"image"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// nerdIcons are private use runes the embedded Symbols Nerd Font Mono has:
// Font Awesome home and folder, Seti-UI go, Seti-UI folder, and a Material
// Design icon from plane 15.
var nerdIcons = []rune{0xF015, 0xF07B, 0xE62B, 0xE5FF, 0xF0001}

var (
	useRe      = regexp.MustCompile(`<use [^>]*/>`)
	glyphDefRe = regexp.MustCompile(`<path id="g\d+" d="[^"]*"/>`)
	textRunRe  = regexp.MustCompile(`<tspan[^>]*>[^<]*</tspan>`)
)

func TestIsPrivateUse(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		r    rune
		want bool
	}{
		{'a', false},
		{0xDFFF, false},
		{0xE000, true},
		{0xE0B0, true},
		{0xF8FF, true},
		{0xF900, false},
		{0xEFFFF, false},
		{0xF0000, true},
		{0xF0001, true},
		{0xFFFFD, true},
		{0xFFFFE, false},
		{0xFFFFF, false},
		{0x100000, true},
		{0x10FFFD, true},
		{0x10FFFE, false},
		{0x10FFFF, false},
		{0x1F600, false},
	} {
		if got := isPrivateUse(tc.r); got != tc.want {
			t.Errorf("isPrivateUse(%U) = %v, want %v", tc.r, got, tc.want)
		}
	}
}

func TestTextModeDrawsNerdIconsAsOutlines(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	x := func(col int) string { return num(float64(lay.Pad+col*lay.CellWidth) + lay.GlyphOffsetX) }
	for _, icon := range nerdIcons {
		t.Run(string(icon), func(t *testing.T) {
			if !g.Covers(icon, false, false) {
				t.Fatalf("%U is not in the embedded fonts: pick another test icon", icon)
			}
			s := screen(t, 20, "ab "+string(icon)+" cd")
			doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
			if strings.ContainsRune(doc, icon) {
				t.Errorf("%U is still in the document as text:\n%s", icon, doc)
			}
			uses := useRe.FindAllString(doc, -1)
			iconX := num(float64(lay.Pad + 3*lay.CellWidth))
			if len(uses) != 1 || !strings.Contains(uses[0], `href="#g0"`) || !strings.Contains(uses[0], ` x="`+iconX+`" y="`+itoaT(lay.Pad)+`"`) {
				t.Errorf("want one outline use of the icon at column 3 (x %s), got %q", iconX, uses)
			}
			// The words either side stay text, on their own cells; the
			// icon cuts the run so the blank before it is not carried over.
			want := `<text y="` + itoaT(lay.Pad+lay.Ascent) + `" xml:space="preserve">` +
				`<tspan x="` + x(0) + ` ` + x(1) + `">ab</tspan>` +
				`<tspan x="` + x(5) + ` ` + x(6) + `">cd</tspan></text>`
			if !strings.Contains(doc, want) {
				t.Errorf("text element differs\nwant: %s\ngot:\n%s", want, doc)
			}
		})
	}
}

func TestTextModeKeepsPrivateUseRunesWithoutAGlyphAsText(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	for _, missing := range []rune{0xF8FF, 0x10FFFE} {
		if g.Covers(missing, false, false) {
			t.Fatalf("%U is in the embedded fonts: pick another test rune", missing)
		}
		s := screen(t, 10, "a"+string(missing)+"b")
		doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 10, 1, step(s, time.Second))
		if !strings.ContainsRune(doc, missing) {
			t.Errorf("%U was dropped from the document:\n%s", missing, doc)
		}
		if n := len(useRe.FindAllString(doc, -1)); n != 0 {
			t.Errorf("%U: %d uses, want none", missing, n)
		}
	}
}

func TestTextModeKeepsPowerlineSprites(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 10, "\ue0b0\ue0b6")
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 10, 1, step(s, time.Second))
	if !strings.Contains(doc, `<use href="#s0"`) || !strings.Contains(doc, `<use href="#s1"`) {
		t.Errorf("powerline glyphs are not sprites:\n%s", doc)
	}
	if strings.Contains(doc, `href="#g`) || strings.Contains(doc, "<text") {
		t.Errorf("powerline glyphs became outlines or text:\n%s", doc)
	}
}

// TestTextModeIconsAreTheOutlinesOfPathsMode checks the icon cells of text
// mode against paths mode, which draws every cell as outlines: the same
// definitions and the same uses, whatever the colour, weight, width or marks.
func TestTextModeIconsAreTheOutlinesOfPathsMode(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 20,
		"\uf015 "+esc+"31m\uf07b"+reset+esc+"1m\ue62b"+reset+esc+"3;32m\uf015"+reset,
		"\uf015\ufe0f \ue5ff\U000f0001",
		"\uf015\uf07b",
	)
	// A width of 2 cells is what a terminal reports for an icon it treats as
	// wide; the screen helper only knows East Asian and emoji widths.
	s.Widths[2] = []snapshot.Wide{{Rune: 0, Cells: 2}}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	opts := func(m TextMode) Options {
		return Options{Geometry: g, Text: m, Frame: Frame{Kind: FrameLast}}
	}
	text := write(t, opts(TextText), s.Cols, s.Rows, step(s, time.Second))
	paths := write(t, opts(TextPaths), s.Cols, s.Rows, step(s, time.Second))
	if strings.Contains(text, "<text") {
		t.Errorf("an icon-only screen has text:\n%s", normalise(text))
	}
	if got, want := strings.Join(useRe.FindAllString(text, -1), "\n"), strings.Join(useRe.FindAllString(paths, -1), "\n"); got != want || got == "" {
		t.Errorf("uses differ from paths mode\ntext:\n%s\npaths:\n%s", got, want)
	}
	if got, want := strings.Join(glyphDefRe.FindAllString(text, -1), "\n"), strings.Join(glyphDefRe.FindAllString(paths, -1), "\n"); got != want || got == "" {
		t.Errorf("glyph definitions differ from paths mode")
	}
	// Colours and weights are carried by the uses and the glyph keys.
	if !strings.Contains(text, `fill="`+hex(g.Layout().Theme.Palette[1])+`"`) {
		t.Errorf("the red icon lost its colour:\n%s", normalise(text))
	}
}

func TestTextModeDropsAnIconsMarksLikePathsMode(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	// A variation selector is a mark the renderer does not draw; a combining
	// acute is drawn after the icon. Paths mode draws the same screen with
	// blanks for x and y, so its uses are those of the icons.
	s := screen(t, 10, "x\uf015\ufe0f y", "\uf015\u0301")
	blanked := screen(t, 10, " \uf015\ufe0f  ", "\uf015\u0301")
	text := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
	paths := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(blanked, time.Second))
	if got, want := useRe.FindAllString(text, -1), useRe.FindAllString(paths, -1); len(got) != 3 || strings.Join(got, "") != strings.Join(want, "") {
		t.Errorf("uses differ from paths mode\ntext:  %q\npaths: %q", got, want)
	}
	for _, r := range []rune{0xF015, 0xFE0F, 0x301} {
		if strings.ContainsRune(text, r) {
			t.Errorf("%U is in the document as text:\n%s", r, normalise(text))
		}
	}
	// Row 0 keeps x and y as text runs around the cut.
	if n := len(textRunRe.FindAllString(text, -1)); n != 2 {
		t.Errorf("%d text runs, want 2 (x and y)\n%s", n, normalise(text))
	}
}

// TestTextModeWithoutIconsHasNoOutlines checks that a screen with no private
// use rune gets no glyph outlines: TestStaticTextGolden and
// TestAnimatedGolden pin the rest of its bytes.
func TestTextModeWithoutIconsHasNoOutlines(t *testing.T) {
	t.Parallel()
	g := testGeometry(t, raster.Options{})
	s := withCursor(featureScreen(t), 3, 0)
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
	if strings.Contains(doc, `href="#g`) || strings.Contains(doc, `id="g`) {
		t.Errorf("a screen without icons has glyph outlines:\n%s", normalise(doc))
	}
}

func TestAnimatedTextModeDrawsIconsAsSharedOutlines(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	steps := []timeline.Step{
		step(screen(t, 10, "\uf015 home", ""), 300*time.Millisecond),
		step(screen(t, 10, "\uf015 home", "\uf07b dir"), 300*time.Millisecond),
		step(screen(t, 10, "\uf07b dir", "\uf015 home x"), 300*time.Millisecond),
	}
	text := write(t, Options{Geometry: g}, 10, 2, steps...)
	paths := write(t, Options{Geometry: g, Text: TextPaths}, 10, 2, steps...)
	if strings.ContainsRune(text, 0xF015) || strings.ContainsRune(text, 0xF07B) {
		t.Errorf("an icon is still text:\n%s", normalise(text))
	}
	// Two icons, each defined once however often a row shows it.
	if n := len(glyphDefRe.FindAllString(text, -1)); n != 2 {
		t.Errorf("%d glyph definitions of icons, want 2\n%s", n, normalise(text))
	}
	if n := strings.Count(text, `<use href="#g0"`) + strings.Count(text, `<use href="#g1"`); n != len(useRe.FindAllString(text, -1)) || n != 4 {
		t.Errorf("%d icon uses of %d, want 4 icons in the strips", n, len(useRe.FindAllString(text, -1)))
	}
	// The words stay text.
	for _, w := range []string{"home", "dir"} {
		if !strings.Contains(text, ">"+w+"</tspan>") {
			t.Errorf("%q is not text in the animation", w)
		}
	}
	// Same strips, same keyframes as paths mode, which has the same icon cells.
	if got, want := animationSkeleton(text), animationSkeleton(paths); got != want {
		t.Errorf("animation structure differs from paths mode\ntext:  %s\npaths: %s", got, want)
	}
}

// animationSkeleton is the CSS of doc: the keyframes and rules, which depend
// on the rows and step times and not on how glyphs are written.
func animationSkeleton(doc string) string {
	m := regexp.MustCompile(`<style>[^<]*</style>`).FindString(doc)
	return m
}

func TestTextModeIconPixelsMatchRasterRenderer(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	s := screen(t, 12,
		"\uf015 "+esc+"31m\uf07b"+reset+" "+esc+"1m\ue62b"+reset+" \U000f0001",
		esc+"44m\ue5ff\uf015"+reset+" "+esc+"32m\uf07b"+reset)
	ref, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
	if strings.Contains(doc, "<text") {
		t.Fatalf("an icon-only screen has text:\n%s", normalise(doc))
	}
	got := rasteriseSVG(t, doc, ref.Bounds().Dx(), ref.Bounds().Dy())
	mean, bad := pixelDiff(ref, got)
	blank := image.NewRGBA(ref.Bounds())
	for i := 0; i < len(blank.Pix); i += 4 {
		c := r.Layout().Theme.Background
		blank.Pix[i], blank.Pix[i+1], blank.Pix[i+2], blank.Pix[i+3] = c.R, c.G, c.B, 255
	}
	control, _ := pixelDiff(ref, blank)
	t.Logf("icons: mean error %.4f, %.2f%% bad pixels (an empty screen would score %.4f)", mean, bad*100, control)
	if mean*5 > control {
		t.Errorf("mean error %.4f is not far below that of an empty screen, %.4f", mean, control)
	}
	if mean > 0.02 || bad > 0.04 {
		t.Errorf("SVG icons differ from the raster render: mean error %.4f (limit 0.02), %.2f%% bad pixels (limit 4%%)", mean, bad*100)
	}
}

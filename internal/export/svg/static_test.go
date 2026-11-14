package svg

import (
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/render/theme"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

const (
	esc   = "\x1b["
	reset = esc + "0m"
)

// featureScreen has a row for each kind of cell the builder treats on its own.
func featureScreen(t testing.TB) snapshot.Snapshot {
	return screen(t, 24,
		"plain "+esc+"31mred"+reset+" "+esc+"1;32mbold"+reset+" "+esc+"3mitalic"+reset,
		esc+"2mfaint"+reset+" "+esc+"7minverse"+reset+" "+esc+"4munder"+reset+" "+esc+"9mstrike"+reset,
		esc+"44mblue "+esc+"42mgreen"+reset+" "+esc+"48;2;10;20;30mrgb"+reset,
		"世界 wide e\u0301 mark <&>\"'",
		"┌─┐ █▄░ ⣿ "+esc+"33m▀"+reset,
		"emoji 😀😀 ❤\ufe0f",
		esc+"4;31munder red"+reset+"  gap     far",
	)
}

func TestStaticTextGolden(t *testing.T) {
	t.Parallel()
	g := testGeometry(t, raster.Options{})
	s := withCursor(featureScreen(t), 3, 0)
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
	golden(t, "static_text.svg", normalise(doc))
}

// TestStaticTextEmojiImageGolden pins the output of --svg-emoji image, which
// is what the default drew before emoji became text.
func TestStaticTextEmojiImageGolden(t *testing.T) {
	t.Parallel()
	g := testGeometry(t, raster.Options{})
	s := withCursor(featureScreen(t), 3, 0)
	doc := write(t, Options{Geometry: g, Emoji: EmojiImage, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
	golden(t, "static_text_emoji_image.svg", normalise(doc))
}

func TestStaticPathsGolden(t *testing.T) {
	t.Parallel()
	g := testGeometry(t, raster.Options{})
	s := withCursor(screen(t, 12, "Hi "+esc+"1mBold"+reset, "世 e\u0301 ┌─", esc+"4munder"+reset), 2, 2)
	doc := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameFirst}}, s.Cols, s.Rows, step(s, time.Second))
	golden(t, "static_paths.svg", normalise(doc))
}

func TestStaticDocumentShape(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{FontSize: 14, Scale: 2, Padding: 4})
	lay := r.Layout()
	s := screen(t, 10, "hello")
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 10, 1, step(s, time.Second))
	w, h := 10*lay.CellWidth+2*lay.Pad, lay.CellHeight+2*lay.Pad
	for _, want := range []string{
		`viewBox="0 0 ` + itoaT(w) + ` ` + itoaT(h) + `"`,
		`width="` + num(float64(w)/2) + `" height="` + num(float64(h)/2) + `"`,
		`font-size="28"`,
		`<rect width="` + itoaT(w) + `" height="` + itoaT(h) + `" fill="` + hex(lay.Theme.Background) + `"/>`,
		`font-family="'JetBrains Mono', ui-monospace, Menlo, monospace"`,
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("document lacks %s:\n%s", want, doc)
		}
	}
	if strings.Contains(doc, "<style") || strings.Contains(doc, "@keyframes") {
		t.Error("a static document must not need CSS")
	}
}

func itoaT(n int) string { return num(float64(n)) }

func TestTextRunsPlaceEveryCharacterOnItsCell(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := screen(t, 20, "ab "+esc+"31mcd"+reset+"  e")
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	x := func(col int) string { return num(float64(lay.Pad+col*lay.CellWidth) + lay.GlyphOffsetX) }
	want := `<text y="` + itoaT(lay.Pad+lay.Ascent) + `" xml:space="preserve">` +
		`<tspan x="` + x(0) + ` ` + x(1) + `">ab</tspan>` +
		`<tspan x="` + x(3) + ` ` + x(4) + `" fill="` + hex(lay.Theme.Palette[1]) + `">cd</tspan>` +
		`<tspan x="` + x(7) + `">e</tspan></text>`
	// Runs split where the style changes; leading spaces and a space before
	// a run of another style are not part of any run.
	if !strings.Contains(doc, want) {
		t.Errorf("text element differs\nwant: %s\ngot:\n%s", want, doc)
	}
}

func TestInnerSpacesJoinARunUpToTheLimit(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	for _, tc := range []struct {
		line     string
		wantRuns int
	}{
		{"a b", 1},
		{"a" + strings.Repeat(" ", maxInnerSpaces) + "b", 1},
		{"a" + strings.Repeat(" ", maxInnerSpaces+1) + "b", 2},
	} {
		s := screen(t, 20, tc.line)
		doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
		if n := strings.Count(doc, "<tspan"); n != tc.wantRuns {
			t.Errorf("%q: %d runs, want %d\n%s", tc.line, n, tc.wantRuns, doc)
		}
	}
}

func TestWideAndCombinedClustersAreCentredOrAnchoredAlone(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := screen(t, 20, "世a\u0301b")
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	cx := lay.Pad + lay.CellWidth // centre of the 2-cell span at column 0
	if want := `<tspan x="` + itoaT(cx) + `" text-anchor="middle">世</tspan>`; !strings.Contains(doc, want) {
		t.Errorf("wide rune not centred on its two cells; want %s in\n%s", want, doc)
	}
	// a + combining acute is one tspan with a single x; b is its own run.
	if want := `>a` + "\u0301" + `</tspan>`; !strings.Contains(doc, want) {
		t.Errorf("combined cluster not kept together in one tspan:\n%s", doc)
	}
}

func TestBackgroundRunsMergeAdjacentCells(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := screen(t, 20, esc+"44m  "+esc+"42m "+reset+esc+"44m "+reset+" "+esc+"48;2;30;30;30m"+reset+esc+"41m世")
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	rect := func(col, cells int, c string) string {
		return `<rect x="` + itoaT(lay.Pad+col*lay.CellWidth) + `" y="` + itoaT(lay.Pad) + `" width="` + itoaT(cells*lay.CellWidth) +
			`" height="` + itoaT(lay.CellHeight) + `" fill="` + c + `"/>`
	}
	blue, green, red := hex(lay.Theme.Palette[4]), hex(lay.Theme.Palette[2]), hex(lay.Theme.Palette[1])
	for _, want := range []string{rect(0, 2, blue), rect(2, 1, green), rect(3, 1, blue), rect(5, 2, red)} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %s in\n%s", want, doc)
		}
	}
	if n := strings.Count(doc, `fill="`+hex(lay.Theme.Background)+`"`); n != 1 {
		t.Errorf("theme background used for %d elements, want only the page rect", n)
	}
}

func TestDecorationsAreRectanglesFromTheLayout(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := screen(t, 20, esc+"4;31mab"+esc+"9;32mcd"+reset)
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	under := `<rect x="` + itoaT(lay.Pad) + `" y="` + itoaT(lay.Pad+lay.UnderlineTop) + `" width="` + itoaT(2*lay.CellWidth) +
		`" height="` + itoaT(lay.Thickness) + `" fill="` + hex(lay.Theme.Palette[1]) + `"/>`
	strike := `<rect x="` + itoaT(lay.Pad+2*lay.CellWidth) + `" y="` + itoaT(lay.Pad+lay.StrikeTop) + `" width="` + itoaT(2*lay.CellWidth) +
		`" height="` + itoaT(lay.Thickness) + `" fill="` + hex(lay.Theme.Palette[2]) + `"/>`
	for _, want := range []string{under, strike} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %s in\n%s", want, doc)
		}
	}
}

func TestColoursAreResolvedLikeTheRaster(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{Theme: theme.Default()})
	lay := r.Layout()
	for name, sgr := range map[string]string{"faint": "2", "inverse": "7", "faint inverse": "2;7", "bright": "91", "256": "38;5;202", "rgb": "38;2;1;2;3", "invisible": "8"} {
		st, err := snapshot.ParseSGR(sgr)
		if err != nil {
			t.Fatal(err)
		}
		fg, bg := lay.Theme.Colors(st)
		s := screen(t, 10, esc+sgr+"mx"+reset)
		doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 10, 1, step(s, time.Second))
		if st.Attrs&snapshot.Invisible != 0 {
			if strings.Contains(doc, "<text") {
				t.Errorf("%s: invisible cell has text", name)
			}
			continue
		}
		if fg != lay.Theme.Foreground && !strings.Contains(doc, `<tspan x="`+num(float64(lay.Pad)+lay.GlyphOffsetX)+`" fill="`+hex(fg)+`"`) {
			t.Errorf("%s: text fill is not %s:\n%s", name, hex(fg), doc)
		}
		if bg != lay.Theme.Background && !strings.Contains(doc, `height="`+itoaT(lay.CellHeight)+`" fill="`+hex(bg)+`"/>`) {
			t.Errorf("%s: no background rect %s:\n%s", name, hex(bg), doc)
		}
	}
}

func TestBoldAndItalicAreFontAttributes(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 20, esc+"1mb"+reset+esc+"3mi"+reset+esc+"1;3mx"+reset)
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	for _, want := range []string{` font-weight="bold">b<`, ` font-style="italic">i<`, ` font-weight="bold" font-style="italic">x<`} {
		if !strings.Contains(doc, want) {
			t.Errorf("missing %q in\n%s", want, doc)
		}
	}
}

func TestCursorShapes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		style  raster.CursorStyle
		cursor snapshot.Cursor
		want   func(lay raster.Layout) string // "" for no cursor element
	}{
		{"block over text", raster.CursorBlock, snapshot.Cursor{X: 1, Visible: true}, func(l raster.Layout) string {
			return `<rect x="` + itoaT(l.Pad+l.CellWidth) + `" y="` + itoaT(l.Pad) + `" width="` + itoaT(l.CellWidth) + `" height="` + itoaT(l.CellHeight) + `" fill="` + hex(l.Theme.Cursor) + `"/>`
		}},
		{"block past the text", raster.CursorBlock, snapshot.Cursor{X: 5, Visible: true}, func(l raster.Layout) string {
			return `<rect x="` + itoaT(l.Pad+5*l.CellWidth) + `" y="` + itoaT(l.Pad) + `" width="` + itoaT(l.CellWidth) + `" height="` + itoaT(l.CellHeight) + `" fill="` + hex(l.Theme.Cursor) + `"/>`
		}},
		{"bar", raster.CursorBar, snapshot.Cursor{X: 1, Visible: true}, func(l raster.Layout) string {
			return `<rect x="` + itoaT(l.Pad+l.CellWidth) + `" y="` + itoaT(l.Pad) + `" width="` + itoaT(l.BarWidth) + `" height="` + itoaT(l.CellHeight) + `" fill="` + hex(l.Theme.Cursor) + `"/>`
		}},
		{"underline", raster.CursorUnderline, snapshot.Cursor{X: 1, Visible: true}, func(l raster.Layout) string {
			return `<rect x="` + itoaT(l.Pad+l.CellWidth) + `" y="` + itoaT(l.Pad+l.CellHeight-l.Thickness) + `" width="` + itoaT(l.CellWidth) + `" height="` + itoaT(l.Thickness) + `" fill="` + hex(l.Theme.Cursor) + `"/>`
		}},
		{"underline over a wide rune", raster.CursorUnderline, snapshot.Cursor{X: 1, Visible: true}, func(l raster.Layout) string {
			return `<rect x="` + itoaT(l.Pad) + `" y="` + itoaT(l.Pad+l.CellHeight-l.Thickness) + `" width="` + itoaT(2*l.CellWidth) + `" height="` + itoaT(l.Thickness) + `" fill="` + hex(l.Theme.Cursor) + `"/>`
		}},
		{"none", raster.CursorNone, snapshot.Cursor{X: 1, Visible: true}, nil},
		{"hidden", raster.CursorBlock, snapshot.Cursor{X: 1}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, g := newRenderer(t, raster.Options{Cursor: tc.style})
			lay := r.Layout()
			text := "abc"
			if strings.Contains(tc.name, "wide") {
				text = "世c"
			}
			s := screen(t, 20, text)
			s.Cursor = tc.cursor
			doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
			cursor := `fill="` + hex(lay.Theme.Cursor) + `"`
			if tc.want == nil {
				if strings.Contains(doc, cursor) {
					t.Errorf("unexpected cursor:\n%s", doc)
				}
				return
			}
			if want := tc.want(lay); !strings.Contains(doc, want) {
				t.Errorf("missing %s in\n%s", want, doc)
			}
			if strings.Count(doc, cursor) != 1 {
				t.Errorf("want exactly one cursor-coloured element:\n%s", doc)
			}
		})
	}
}

func TestBlockCursorRedrawsTheGlyphInTheBackgroundColour(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := withCursor(screen(t, 20, "abc"), 1, 0)
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	want := `fill="` + hex(lay.Theme.Background) + `">b</tspan>`
	if !strings.Contains(doc, want) {
		t.Errorf("glyph under the block cursor is not drawn in the background colour; want %s in\n%s", want, doc)
	}
}

func TestSpritesAreSharedShapes(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := screen(t, 20, "██ ▄ ░")
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	if n := strings.Count(doc, "<path id="); n != 3 {
		t.Errorf("%d sprite definitions, want 3 (█, ▄, ░)\n%s", n, doc)
	}
	full := `<path id="s0" d="M0 0h` + itoaT(lay.CellWidth) + `v` + itoaT(lay.CellHeight) + `h-` + itoaT(lay.CellWidth) + `z"/>`
	if !strings.Contains(doc, full) {
		t.Errorf("full block is not one rectangle: want %s in\n%s", full, doc)
	}
	if !strings.Contains(doc, `fill-opacity="0.251"`) {
		t.Errorf("light shade is not 25%% coverage:\n%s", doc)
	}
	if n := strings.Count(doc, `<use href="#s0"`); n != 2 {
		t.Errorf("█ used %d times, want 2", n)
	}
}

func TestEmojiImagesAreDefinedOnce(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	s := screen(t, 20, "😀😀 ❤\ufe0f")
	doc := write(t, Options{Geometry: g, Emoji: EmojiImage, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	if n := strings.Count(doc, "<image "); n != 2 {
		t.Errorf("%d <image> definitions, want 2 (😀 once, ❤️ once)", n)
	}
	if n := strings.Count(doc, `<use href="#e0"`); n != 2 {
		t.Errorf("😀 used %d times, want 2", n)
	}
	side := min(2*lay.CellWidth, lay.CellHeight)
	if want := `<image id="e0" width="` + itoaT(side) + `" height="` + itoaT(side) + `" href="data:image/png;base64,`; !strings.Contains(doc, want) {
		t.Errorf("missing %s", want)
	}
	if strings.Contains(doc, "😀") {
		t.Error("colour emoji left to the host font")
	}
}

func TestSpecialCharactersAreEscaped(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	s := screen(t, 20, `<a href="x">&amp; 'q'</a>`)
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	if strings.Contains(doc, `<a href`) || !strings.Contains(doc, "&lt;a") || !strings.Contains(doc, "&amp;amp;") {
		t.Errorf("text not escaped:\n%s", doc)
	}
}

func TestFrameSelection(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	steps := []timelineStep{
		{"one", 100 * time.Millisecond}, {"two", 200 * time.Millisecond}, {"three", 300 * time.Millisecond},
	}
	var ts = stepsOf(t, steps)
	for _, tc := range []struct {
		frame Frame
		want  string
	}{
		{Frame{Kind: FrameFirst}, "one"},
		{Frame{Kind: FrameLast}, "three"},
		{Frame{Kind: FrameAt, At: 0}, "one"},
		{Frame{Kind: FrameAt, At: 99 * time.Millisecond}, "one"},
		{Frame{Kind: FrameAt, At: 100 * time.Millisecond}, "two"},
		{Frame{Kind: FrameAt, At: 299 * time.Millisecond}, "two"},
		{Frame{Kind: FrameAt, At: 300 * time.Millisecond}, "three"},
		{Frame{Kind: FrameAt, At: time.Hour}, "three"},
	} {
		doc := write(t, Options{Geometry: g, Frame: tc.frame}, 10, 1, ts...)
		for _, name := range []string{"one", "two", "three"} {
			if has := strings.Contains(doc, ">"+name+"<"); has != (name == tc.want) {
				t.Errorf("frame %+v: contains %q = %v, want the frame %q only", tc.frame, name, has, tc.want)
			}
		}
	}
}

// A bar of styled blanks at the end of a line (a status line, an erase with a
// background colour) is one background rectangle over all of its cells.
func TestBackgroundRunCoversTrailingStyledBlanks(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	blue := snapshot.Style{Bg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 4}}
	s := snapshot.Snapshot{
		Cols: 20, Rows: 1,
		Lines: []string{"ab" + strings.Repeat(" ", 18)},
		Spans: [][]snapshot.Span{{{Start: 0, End: 20, Style: blue}}},
	}
	if err := s.Validate(); err != nil {
		t.Fatal(err)
	}
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(s, time.Second))
	want := `<rect x="` + itoaT(lay.Pad) + `" y="` + itoaT(lay.Pad) + `" width="` + itoaT(20*lay.CellWidth) +
		`" height="` + itoaT(lay.CellHeight) + `" fill="` + hex(lay.Theme.Palette[4]) + `"/>`
	if !strings.Contains(doc, want) {
		t.Errorf("missing %s in\n%s", want, doc)
	}
	if strings.Contains(doc, "ab ") {
		t.Errorf("trailing blanks were written as text:\n%s", doc)
	}
}

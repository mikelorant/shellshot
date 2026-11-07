package raster

import (
	"image"
	"reflect"
	"strings"
	"testing"

	xfont "golang.org/x/image/font"

	"github.com/michaellorant/shellshot/internal/render/font"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

const (
	grin     = "\U0001F600"
	thumb    = "\U0001F44D"
	thumbMed = "\U0001F44D\U0001F3FD"
	heartVS  = "\u2764\uFE0F"
	family   = "\U0001F468\u200D\U0001F469\u200D\U0001F467"
	flagAU   = "\U0001F1E6\U0001F1FA"
	keycap   = "1\uFE0F\u20E3"
	rainbow  = "\U0001F3F3\uFE0F\u200D\U0001F308"
	// femaleSign (with VS16) has a glyph without a bitmap in Apple's font;
	// monarch (E18.0) is missing from it. Noto draws both.
	femaleSign = "\u2640\uFE0F"
	monarch    = "\U0001FACC"
)

// clusterSnapshot lays out a row of 10 columns: lead blanks, then the
// cluster (a 2-cell base followed by zero-width runes, as libghostty
// reports it with grapheme clustering on), then a trailing "x".
func clusterSnapshot(lead int, cluster string, baseCells uint8) snapshot.Snapshot {
	n := len([]rune(cluster))
	s := plainSnapshot(10, spaces(lead)+cluster+"x")
	wides := []snapshot.Wide{{Rune: lead, Cells: baseCells}}
	for i := 1; i < n; i++ {
		wides = append(wides, snapshot.Wide{Rune: lead + i, Cells: 0})
	}
	s.Widths = [][]snapshot.Wide{wides}
	return s
}

func spaces(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func emojiCells(r *Renderer) image.Rectangle { return cellSpan(r, 3, 2) }

// A cluster draws once over exactly the span of its base rune: ZWJ families,
// flags, skin tones, keycaps and VS16 hearts put ink in both of their cells
// and nothing leaks into the neighbours, the padding or the line below.
func TestEmojiClusterOccupiesExactlyItsSpan(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	for name, cluster := range map[string]string{
		"single": grin, "vs16 heart": heartVS, "skin tone": thumbMed, "zwj family": family,
		"flag": flagAU, "keycap": keycap, "zwj rainbow flag": rainbow,
	} {
		s := clusterSnapshot(3, cluster, 2)
		s.Lines[0] = s.Lines[0][:len(s.Lines[0])-1] + " " // no trailing x: only the emoji inks
		img := mustRender(t, r, s)
		span := emojiCells(r)
		box := inkBox(r, img, span)
		if box.Empty() {
			t.Errorf("%s: no ink in its span", name)
			continue
		}
		outside := 0
		b := img.Bounds()
		for y := b.Min.Y; y < b.Max.Y; y++ {
			for x := b.Min.X; x < b.Max.X; x++ {
				if img.RGBAAt(x, y) != r.theme.Background && !image.Pt(x, y).In(span) {
					outside++
				}
			}
		}
		if outside != 0 {
			t.Errorf("%s: %d inked pixels outside its two cells", name, outside)
		}
		// Centred: equal margins left and right of the ink inside the span.
		if l, rr := box.Min.X-span.Min.X, span.Max.X-box.Max.X; l < 0 || rr < 0 || l-rr > 1 || rr-l > 1 {
			t.Errorf("%s: ink %v not centred in span %v", name, box, span)
		}
		if box.Dx() < span.Dx()*3/4 && name != "vs16 heart" && name != "keycap" {
			t.Errorf("%s: ink is %d px wide in a %d px span", name, box.Dx(), span.Dx())
		}
	}
}

// The text after a cluster lands in the column after the base's span.
func TestEmojiClusterNeighboursKeepTheirColumns(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, clusterSnapshot(3, family, 2))
	if inkInCell(r, img, 5, 0) == 0 {
		t.Error("the 'x' after the family is not in column 5")
	}
	for _, col := range []int{6, 7, 8, 9} {
		if inkInCell(r, img, col, 0) != 0 {
			t.Errorf("column %d has ink", col)
		}
	}
}

// A sequence is drawn as its own glyph, not as its first rune nor as one
// glyph per rune.
func TestEmojiClusterDrawsWholeSequence(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	whole := mustRender(t, r, clusterSnapshot(3, family, 2))
	first := mustRender(t, r, clusterSnapshot(3, "\U0001F468", 2))
	if string(whole.Pix) == string(first.Pix) {
		t.Error("family drew the same pixels as its first person")
	}
	tone := mustRender(t, r, clusterSnapshot(3, thumbMed, 2))
	plain := mustRender(t, r, clusterSnapshot(3, thumb, 2))
	if string(tone.Pix) == string(plain.Pix) {
		t.Error("skin tone sequence drew the plain thumb")
	}
}

// Colour emoji ignore the foreground colour and the font style.
func TestEmojiIgnoresForegroundAndStyle(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	plain := mustRender(t, r, clusterSnapshot(3, thumbMed, 2))
	s := clusterSnapshot(3, thumbMed, 2)
	s.Spans = [][]snapshot.Span{{{Start: 3, End: 5, Style: snapshot.Style{Fg: palette(1), Attrs: snapshot.Bold | snapshot.Italic}}}}
	if got := mustRender(t, r, s); string(got.Pix) != string(plain.Pix) {
		t.Error("foreground or style changed the emoji pixels")
	}
}

func TestEmojiInvisibleDrawsNothing(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := clusterSnapshot(3, family, 2)
	s.Spans = [][]snapshot.Span{{{Start: 3, End: 8, Style: snapshot.Style{Attrs: snapshot.Invisible}}}}
	img := mustRender(t, r, s)
	if box := inkBox(r, img, emojiCells(r)); !box.Empty() {
		t.Errorf("invisible emoji left ink %v", box)
	}
}

// A block cursor on an emoji fills the whole span and the emoji is drawn on it.
func TestEmojiOnBlockCursor(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := clusterSnapshot(3, grin, 2)
	s.Cursor = snapshot.Cursor{X: 3, Y: 0, Visible: true}
	img := mustRender(t, r, s)
	span := emojiCells(r)
	o := span.Min
	if got := img.RGBAAt(o.X, o.Y); got != r.theme.Cursor {
		t.Errorf("span corner = %v, want the cursor colour %v", got, r.theme.Cursor)
	}
	if got := img.RGBAAt(span.Max.X-1, span.Max.Y-1); got != r.theme.Cursor {
		t.Errorf("span far corner = %v, want the cursor colour", got)
	}
	yellow := 0
	for y := span.Min.Y; y < span.Max.Y; y++ {
		for x := span.Min.X; x < span.Max.X; x++ {
			if c := img.RGBAAt(x, y); c.R > 200 && c.G > 150 && c.B < 120 {
				yellow++
			}
		}
	}
	if yellow < 40 {
		t.Errorf("only %d yellow face pixels on the cursor", yellow)
	}
}

// A narrow cluster (one cell, as with grapheme clustering off) is scaled into
// its single cell.
func TestEmojiNarrowClusterStaysInItsCell(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(5, "  "+heartVS)
	s.Widths = [][]snapshot.Wide{{{Rune: 3, Cells: 0}}}
	img := mustRender(t, r, s)
	if inkInCell(r, img, 2, 0) == 0 {
		t.Error("no ink in the heart's cell")
	}
	if out := inkOutsideCells(r, img, 2); out != 0 {
		t.Errorf("%d inked pixels outside the heart's cell", out)
	}
}

// A wide emoji whose second cell would fall off the screen is clipped to the
// one cell left, not drawn past the canvas.
func TestEmojiClippedAtRightEdge(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(3, "  "+family)
	s.Widths = [][]snapshot.Wide{{{Rune: 2, Cells: 2}, {Rune: 3, Cells: 0}, {Rune: 4, Cells: 0}, {Rune: 5, Cells: 0}, {Rune: 6, Cells: 0}}}
	img := mustRender(t, r, s)
	if inkInCell(r, img, 2, 0) == 0 {
		t.Error("no ink in the last column")
	}
	if out := inkOutsideCells(r, img, 2); out != 0 {
		t.Errorf("%d inked pixels outside the last column", out)
	}
}

// The text fonts take a lone emoji rune before the colour font only if they
// have it; sequences that ask for emoji presentation go to colour first.
type recordingFonts struct {
	*font.Set
	fitted []rune
	emoji  []string
}

func (f *recordingFonts) FitFace(r rune, bold, italic bool, basePx float64, cellW, cellH, cells int) (xfont.Face, bool, error) {
	f.fitted = append(f.fitted, r)
	return f.Set.FitFace(r, bold, italic, basePx, cellW, cellH, cells)
}

func (f *recordingFonts) Emoji(cluster string, side int) (*image.RGBA, bool) {
	f.emoji = append(f.emoji, cluster)
	return f.Set.Emoji(cluster, side)
}

func TestEmojiLookupOrder(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		line       string
		widths     []snapshot.Wide
		wantFitted []rune
		wantEmoji  []string
	}{
		{"sequence goes to colour first", family + " ", wides2(5), nil, []string{family}},
		{"lone emoji tries text fonts first", grin, wides2(1), []rune(grin), []string{grin}},
		{"text presentation skips colour", "\u263a\ufe0e", []snapshot.Wide{{Rune: 1, Cells: 0}}, []rune("\u263a"), []string{"\u263a\ufe0e"}},
		{"hanzi never asks for colour", "世", wides2(1), []rune("世"), nil},
		{"primary glyph asks nobody", "a", nil, nil, nil},
	} {
		f := &recordingFonts{Set: set}
		r, err := New(f, Options{})
		if err != nil {
			t.Fatal(err)
		}
		s := plainSnapshot(8, tc.line)
		if tc.widths != nil {
			s.Widths = [][]snapshot.Wide{tc.widths}
		}
		mustRender(t, r, s)
		if !reflect.DeepEqual(f.fitted, tc.wantFitted) || !reflect.DeepEqual(f.emoji, tc.wantEmoji) {
			t.Errorf("%s: text lookups %q, emoji lookups %q; want %q, %q", tc.name, string(f.fitted), f.emoji, string(tc.wantFitted), tc.wantEmoji)
		}
	}
}

// failingEmoji fails the test when the renderer asks it for a cluster.
type failingEmoji struct {
	t     *testing.T
	calls int
}

func (f *failingEmoji) Image(cluster string, _ int) (*image.RGBA, bool) {
	f.calls++
	f.t.Errorf("emoji backend asked for %+q", cluster)
	return nil, false
}

// Text without emoji never reaches the colour emoji backend, so the built-in
// fonts (which inflate on first use) cost nothing for it.
func TestPlainTextNeverAsksForEmoji(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	backend := &failingEmoji{t: t}
	set.SetEmoji(backend)
	r, err := New(set, Options{})
	if err != nil {
		t.Fatal(err)
	}
	s := plainSnapshot(30, "plain ASCII $ % 世界 한글 \uf015 ─│┌ ▀▄ ⠿")
	s.Widths = [][]snapshot.Wide{wideAt(s.Lines[0], "世界한글")}
	mustRender(t, r, s)
	if backend.calls != 0 {
		t.Errorf("%d emoji lookups for text without emoji", backend.calls)
	}
}

// wideAt marks every rune of line that is in wide as two cells.
func wideAt(line, wide string) []snapshot.Wide {
	var out []snapshot.Wide
	for i, c := range []rune(line) {
		if strings.ContainsRune(wide, c) {
			out = append(out, snapshot.Wide{Rune: i, Cells: 2})
		}
	}
	return out
}

// wides2 reports a 2-cell base at rune 0 followed by zero-width runes up to n.
func wides2(n int) []snapshot.Wide {
	w := []snapshot.Wide{{Rune: 0, Cells: 2}}
	for i := 1; i < n; i++ {
		w = append(w, snapshot.Wide{Rune: i, Cells: 0})
	}
	return w
}

// rowCells groups zero-width runes into the cluster of the rune before them.
func TestRowCellsGroupsClusters(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(12, "a"+family+"e\u0301"+"b")
	s.Widths = [][]snapshot.Wide{append(append([]snapshot.Wide{{Rune: 1, Cells: 2}}, zeroWidths(2, 5)...),
		snapshot.Wide{Rune: 7, Cells: 0})}
	var got []struct {
		cluster  string
		col, wid int
	}
	for _, c := range r.rowCells(s, 0, cursorBox{}) {
		got = append(got, struct {
			cluster  string
			col, wid int
		}{c.cluster(), c.col, c.width})
	}
	want := []struct {
		cluster  string
		col, wid int
	}{{"a", 0, 1}, {family, 1, 2}, {"e\u0301", 3, 1}, {"b", 4, 1}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("clusters = %v, want %v", got, want)
	}
}

func zeroWidths(from, to int) []snapshot.Wide {
	var w []snapshot.Wide
	for i := from; i <= to; i++ {
		w = append(w, snapshot.Wide{Rune: i, Cells: 0})
	}
	return w
}

func TestEmojiSequencePredicate(t *testing.T) {
	for _, tc := range []struct {
		marks string
		want  bool
	}{
		{"", false}, {"\uFE0F", true}, {"\uFE0E", false}, {"\u0301", false}, {"\u200D\U0001F469", true},
		{"\U0001F3FD", true}, {"\u20E3", true}, {"\U0001F1FA", true}, {"\U000E0067", true},
	} {
		if got := emojiSequence(cell{c: 'x', marks: []rune(tc.marks)}); got != tc.want {
			t.Errorf("emojiSequence(%+q) = %v, want %v", tc.marks, got, tc.want)
		}
	}
}

// seg is a run of text whose first rune takes cells cells; the other runes
// are zero-width followers in the same cell, as libghostty reports a cluster.
type seg struct {
	text  string
	cells uint8
}

// narrow and wide are segments of one and two cells.
func narrow(text string) seg { return seg{text, 1} }
func wide(text string) seg   { return seg{text, 2} }

// layoutLine joins segs, one cluster each, into a line and its sparse
// Widths entries.
func layoutLine(segs ...seg) (string, []snapshot.Wide) {
	var line string
	var wides []snapshot.Wide
	i := 0 // rune index
	for _, sg := range segs {
		for k := range []rune(sg.text) {
			switch {
			case k > 0:
				wides = append(wides, snapshot.Wide{Rune: i, Cells: 0})
			case sg.cells != 1:
				wides = append(wides, snapshot.Wide{Rune: i, Cells: sg.cells})
			}
			i++
		}
		line += sg.text
	}
	return line, wides
}

// wideRunes returns one wide segment per rune of text.
func wideRunes(text string) []seg {
	var out []seg
	for _, c := range text {
		out = append(out, wide(string(c)))
	}
	return out
}

// The emoji sheet: emoji sequences from the Apple set, the ones Apple's font
// lacks (U+2640 FE0F has a glyph but no bitmap; the E18.0 monarch butterfly
// has no glyph at all) coming from Noto, CJK and mixed text, laid out as
// libghostty with grapheme clustering reports them.
func TestGoldenEmojiSheet(t *testing.T) {
	r, _ := newTestRenderer(t, Options{FontSize: 20})
	sp := narrow(" ")
	rows := [][]seg{
		{wide(grin), sp, wide(thumbMed), sp, wide(heartVS), sp, wide(family), sp, wide(flagAU), sp, wide(keycap), sp, wide(rainbow)},
		append([]seg{wide(femaleSign), sp, wide(monarch), sp}, append(wideRunes("世界"), append([]seg{sp}, append(wideRunes("한글"), append([]seg{sp}, wideRunes("こんにちは")...)...)...)...)...),
		{narrow("o"), narrow("k"), sp, wide(grin), sp, narrow("d"), narrow("o"), narrow("n"), narrow("e"), sp, wide(rainbow), sp, narrow("e"), narrow("n"), narrow("d")},
	}
	s := snapshot.Snapshot{Cols: 26, Rows: len(rows)}
	for _, row := range rows {
		line, wides := layoutLine(row...)
		s.Lines = append(s.Lines, line)
		s.Widths = append(s.Widths, wides)
	}
	compareGolden(t, "emoji_sheet.png", mustRender(t, r, s))
}

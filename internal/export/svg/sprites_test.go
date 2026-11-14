package svg

import (
	"fmt"
	"image"
	"math/rand"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/render/sprite"
)

// spriteRunes returns every rune the sprite package draws.
func spriteRunes() []rune {
	var out []rune
	for r := rune(0x2500); r <= 0x28FF; r++ {
		if sprite.Handles(r) {
			out = append(out, r)
		}
	}
	for r := rune(0xE0B0); r <= 0xE0B7; r++ {
		out = append(out, r)
	}
	return out
}

// coverage maps each pixel the rectangles ink to the number of rectangles
// that ink it; a merge must leave every inked pixel at exactly 1.
func coverage(rs []pixelRect) map[image.Point]int {
	m := map[image.Point]int{}
	for _, r := range rs {
		for y := r.y; y < r.y+r.h; y++ {
			for x := r.x; x < r.x+r.w; x++ {
				m[image.Pt(x, y)]++
			}
		}
	}
	return m
}

func TestMergeRects(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		in   []pixelRect
		want []pixelRect
	}{
		{"no rectangles", nil, nil},
		{"single rectangle", []pixelRect{{4, 9, 5, 1}}, []pixelRect{{4, 9, 5, 1}}},
		{"horizontal run", []pixelRect{{0, 9, 9, 1}, {9, 9, 9, 1}, {18, 9, 9, 1}}, []pixelRect{{0, 9, 27, 1}}},
		{"horizontal run in any order", []pixelRect{{18, 9, 9, 1}, {0, 9, 9, 1}, {9, 9, 9, 1}}, []pixelRect{{0, 9, 27, 1}}},
		{"vertical stack", []pixelRect{{4, 0, 1, 20}, {4, 20, 1, 20}, {4, 40, 1, 20}}, []pixelRect{{4, 0, 1, 60}}},
		{"gap between cells", []pixelRect{{0, 9, 9, 1}, {18, 9, 9, 1}}, []pixelRect{{0, 9, 9, 1}, {18, 9, 9, 1}}},
		{"different heights do not join", []pixelRect{{0, 9, 9, 1}, {9, 9, 9, 2}}, []pixelRect{{0, 9, 9, 1}, {9, 9, 9, 2}}},
		{"different rows do not join", []pixelRect{{0, 9, 9, 1}, {9, 10, 9, 1}}, []pixelRect{{0, 9, 9, 1}, {9, 10, 9, 1}}},
		{
			"top left corner then a line right",
			[]pixelRect{{4, 9, 5, 1}, {4, 10, 1, 10}, {9, 9, 9, 1}},
			[]pixelRect{{4, 9, 14, 1}, {4, 10, 1, 10}},
		},
		{
			"box of corners, edges and sides",
			[]pixelRect{
				{4, 9, 5, 1}, {4, 10, 1, 10}, {9, 9, 9, 1}, {18, 9, 5, 1}, {22, 10, 1, 10},
				{4, 20, 1, 20}, {22, 20, 1, 20},
				{4, 40, 1, 10}, {4, 49, 5, 1}, {9, 49, 9, 1}, {18, 49, 5, 1}, {22, 40, 1, 10},
			},
			[]pixelRect{{4, 9, 19, 1}, {4, 10, 1, 40}, {22, 10, 1, 40}, {4, 49, 19, 1}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := mergeRects(tc.in)
			if !slices.Equal(got, tc.want) {
				t.Errorf("mergeRects(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestMergeRectsKeepsThePixels checks, for every sprite at several cell
// sizes and for every glyph repeated and stacked in a grid of cells, that the
// merged rectangles ink the same pixels as the ones merged, each once.
func TestMergeRectsKeepsThePixels(t *testing.T) {
	t.Parallel()
	for _, size := range [][2]int{{9, 20}, {7, 15}, {18, 40}, {10, 21}, {5, 11}, {1, 1}} {
		w, h := size[0], size[1]
		for _, a := range []uint8{255, 64} {
			var all []pixelRect
			for i, r := range spriteRunes() {
				rects, ok := sprite.Rects(r, w, h)
				if !ok {
					t.Fatalf("%U: sprite.Rects not ok", r)
				}
				var cell []pixelRect
				for _, q := range rects {
					if q.A == a {
						cell = append(cell, pixelRect{q.X0, q.Y0, q.X1 - q.X0, q.Y1 - q.Y0})
					}
				}
				// Put each rune in a 2 x 2 block of cells and the blocks in a grid
				// that touches, so there are runs both ways.
				for dy := 0; dy < 2; dy++ {
					for dx := 0; dx < 2; dx++ {
						for _, c := range cell {
							c.x += (i%32*2 + dx) * w
							c.y += (i/32*2 + dy) * h
							all = append(all, c)
						}
					}
				}
				merged := mergeRects(cell)
				if !slices.Equal(coverageKeys(coverage(merged)), coverageKeys(coverage(cell))) || maxCover(coverage(merged)) > 1 {
					t.Fatalf("%U at %dx%d: merging one cell changed its pixels", r, w, h)
				}
			}
			merged := mergeRects(all)
			if got, want := coverageKeys(coverage(merged)), coverageKeys(coverage(all)); !slices.Equal(got, want) {
				t.Errorf("%dx%d level %d: merged grid inks %d pixels, want %d", w, h, a, len(got), len(want))
			}
			if maxCover(coverage(merged)) > 1 {
				t.Errorf("%dx%d level %d: merged rectangles overlap", w, h, a)
			}
			if len(merged) > len(all) {
				t.Errorf("%dx%d level %d: merging made %d rectangles from %d", w, h, a, len(merged), len(all))
			}
		}
	}
}

func coverageKeys(m map[image.Point]int) []image.Point {
	out := make([]image.Point, 0, len(m))
	for p := range m {
		out = append(out, p)
	}
	slices.SortFunc(out, func(a, b image.Point) int {
		if a.Y != b.Y {
			return a.Y - b.Y
		}
		return a.X - b.X
	})
	return out
}

func maxCover(m map[image.Point]int) int {
	var n int
	for _, c := range m {
		n = max(n, c)
	}
	return n
}

func TestRepeatedLineCellsAreOneRectangle(t *testing.T) {
	t.Parallel()
	r, g := newRenderer(t, raster.Options{})
	lay := r.Layout()
	doc := write(t, Options{Geometry: g, Frame: Frame{Kind: FrameLast}}, 20, 1, step(screen(t, 20, "──────────"), time.Second))
	if n := strings.Count(doc, "<path "); n != 1 {
		t.Errorf("%d paths for a run of line cells, want 1:\n%s", n, doc)
	}
	w := itoaT(10 * lay.CellWidth)
	if d := firstPathData(t, doc); !strings.HasSuffix(d, "h"+w+"v1h-"+w+"z") || strings.Count(d, "M") != 1 {
		t.Errorf("run of line cells is not one %s wide rectangle: d=%q", w, d)
	}
	if strings.Contains(doc, "<use ") || strings.Contains(doc, "<defs>") {
		t.Errorf("line cells are drawn by reference:\n%s", doc)
	}
}

// firstPathData returns the d attribute of the first <path> in doc.
func firstPathData(t *testing.T, doc string) string {
	t.Helper()
	_, rest, ok := strings.Cut(doc, `<path `)
	if !ok {
		t.Fatalf("no path in\n%s", doc)
	}
	_, rest, _ = strings.Cut(rest, `d="`)
	d, _, _ := strings.Cut(rest, `"`)
	return d
}

func TestInlineSpritesAreGroupedByColourAndLevel(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	b := NewBuilder(g, TextText, EmojiText)
	red, blue := esc+"31m", esc+"34m"
	s := screen(t, 20, "██"+red+"░█░"+reset+"█"+blue+"░░░"+reset+"▓")
	row, err := b.Row(s, 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	// One path for each colour and level: the default colour has full (██, █)
	// and 75% (▓), red has full and 25% (░░), blue has only 25% (░░░). Red's
	// two paths share a <g> that holds the fill; blue's one carries it.
	if n := strings.Count(row, "<path "); n != 5 {
		t.Errorf("%d paths, want 5 (one per colour and level):\n%s", n, row)
	}
	if n := strings.Count(row, "<g fill="); n != 1 {
		t.Errorf("%d groups, want 1 (red):\n%s", n, row)
	}
	if n := strings.Count(row, `fill-opacity="0.251"`); n != 2 {
		t.Errorf("%d 25%% paths, want 2 (red and blue):\n%s", n, row)
	}
	if n := strings.Count(row, `<path fill=`); n != 1 {
		t.Errorf("%d paths with their own fill, want 1 (blue):\n%s", n, row)
	}
}

func TestInlineSpriteRunsStayInDrawOrder(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	b := NewBuilder(g, TextPaths, EmojiText)
	row, err := b.Row(screen(t, 10, "──a──"), 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, el := range strings.SplitAfter(strings.ReplaceAll(row, "\n", ""), "/>") {
		switch {
		case strings.HasPrefix(el, "<path"):
			order = append(order, "path")
		case strings.HasPrefix(el, "<use"):
			order = append(order, "use")
		}
	}
	if want := []string{"path", "use", "path"}; !slices.Equal(order, want) {
		t.Errorf("draw order %v, want %v (a glyph between two runs of lines):\n%s", order, want, row)
	}
}

func TestComplexSpritesStayShared(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	b := NewBuilder(g, TextText, EmojiText)
	row, err := b.Row(screen(t, 10, "\ue0b0\ue0b0─\ue0b0"), 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(row, `<use href="#s0"`); n != 3 {
		t.Errorf("%d uses of the Powerline arrow, want 3:\n%s", n, row)
	}
	if n := strings.Count(row, "<path "); n != 1 {
		t.Errorf("%d inline paths, want 1 (the line):\n%s", n, row)
	}
	if n := strings.Count(b.Defs(), "<path id="); n != 1 {
		t.Errorf("%d shared sprites, want 1:\n%s", n, b.Defs())
	}
}

func TestBlankBrailleDrawsNothing(t *testing.T) {
	t.Parallel()
	_, g := newRenderer(t, raster.Options{})
	b := NewBuilder(g, TextText, EmojiText)
	row, err := b.Row(screen(t, 10, "\u2800\u2800"), 0, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(row, "<path") || strings.Contains(row, "<use") || b.Defs() != "" {
		t.Errorf("blank braille left markup:\n%s\n%s", row, b.Defs())
	}
}

// TestInlineSpritesMatchReferences rasterises each row of sprites two ways:
// the markup the exporter writes, and one <use> of a shared <path> per cell,
// which is what it wrote before sprites were merged. The pixels must be
// identical.
func TestInlineSpritesMatchReferences(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		o    raster.Options
	}{
		{"default", raster.Options{}},
		{"scale 2", raster.Options{Scale: 2, FontSize: 12}},
		{"odd size", raster.Options{FontSize: 13}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r, g := newRenderer(t, tc.o)
			lay := r.Layout()
			const cols = 40
			runes := spriteRunes()
			rng := rand.New(rand.NewSource(1))
			// Rows of consecutive runes, then rows of a repeated rune, then
			// rows of random runes, in colours that change every few cells.
			var lines [][]rune
			for i := 0; i < len(runes); i += cols {
				lines = append(lines, runes[i:min(i+cols, len(runes))])
			}
			for _, r := range []rune{'─', '│', '█', '░', '▒', '▓', '┼', '╬', '▀', '▄', '▌', '▐', '⣿', '\u2800', '\ue0b0', '╱'} {
				lines = append(lines, slices.Repeat([]rune{r}, cols))
			}
			for range 10 {
				line := make([]rune, cols)
				for i := range line {
					line[i] = runes[rng.Intn(len(runes))]
					if rng.Intn(4) == 0 {
						line[i] = ' '
					}
				}
				lines = append(lines, line)
			}
			colours := []string{"", esc + "31m", esc + "32m", esc + "1;34m", esc + "38;5;208m"}
			src := make([]string, len(lines))
			for i, line := range lines {
				var sb strings.Builder
				for j, c := range line {
					if j%3 == 0 || rng.Intn(5) == 0 {
						sb.WriteString(reset + colours[rng.Intn(len(colours))])
					}
					sb.WriteRune(c)
				}
				src[i] = sb.String() + reset
			}
			s := screen(t, cols, src...)
			w, h := cols*lay.CellWidth, len(lines)*lay.CellHeight
			b := NewBuilder(g, TextPaths, EmojiText)
			var merged, refs, defs strings.Builder
			for row := range lines {
				rowMarkup, err := b.Row(s, row, 0, row*lay.CellHeight)
				if err != nil {
					t.Fatal(err)
				}
				merged.WriteString(rowMarkup)
				for _, c := range g.RowCells(s, row) {
					rects, ok := sprite.Rects(c.Base, c.Width*lay.CellWidth, lay.CellHeight)
					if !ok || len(rects) == 0 || c.Hidden {
						continue
					}
					id := fmt.Sprintf("r%d_%d", row, c.Col)
					defs.WriteString(spriteMarkup(id, rects) + "\n")
					fmt.Fprintf(&refs, `<use href="#%s" x="%d" y="%d" fill="%s"/>`+"\n", id, c.Col*lay.CellWidth, row*lay.CellHeight, hex(c.Fg))
				}
			}
			wrap := func(defs, body string) string {
				return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + strconv.Itoa(w) + ` ` + strconv.Itoa(h) +
					`" width="` + strconv.Itoa(w) + `" height="` + strconv.Itoa(h) + `" fill="` + hex(lay.Theme.Foreground) + `">` + "\n" +
					defs + `<rect width="` + strconv.Itoa(w) + `" height="` + strconv.Itoa(h) + `" fill="` + hex(lay.Theme.Background) + `"/>` + "\n" + body + "</svg>\n"
			}
			got := rasteriseSVG(t, wrap(b.Defs(), merged.String()), w, h)
			want := rasteriseSVG(t, wrap("<defs>\n"+defs.String()+"</defs>\n", refs.String()), w, h)
			if n, first := countDifferent(got, want); n != 0 {
				t.Errorf("%d pixels differ between merged and referenced sprites, first at %v:\n got %v\nwant %v", n, first,
					got.RGBAAt(first.X, first.Y), want.RGBAAt(first.X, first.Y))
			}
			if ink, _ := countDifferent(want, rasteriseSVG(t, wrap("", ""), w, h)); ink < 1000 {
				t.Errorf("only %d inked pixels: the test is not drawing sprites", ink)
			}
		})
	}
}

// countDifferent returns how many pixels of a and b differ and where the first
// one is.
func countDifferent(a, b *image.RGBA) (n int, first image.Point) {
	for y := a.Rect.Min.Y; y < a.Rect.Max.Y; y++ {
		for x := a.Rect.Min.X; x < a.Rect.Max.X; x++ {
			if a.RGBAAt(x, y) != b.RGBAAt(x, y) {
				if n == 0 {
					first = image.Pt(x, y)
				}
				n++
			}
		}
	}
	return n, first
}

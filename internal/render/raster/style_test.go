package raster

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/mikelorant/shellshot/internal/render/theme"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

var (
	red      = snapshot.Color{Kind: snapshot.ColorRGB, R: 200, G: 0, B: 0}
	redRGBA  = color.RGBA{R: 200, G: 0, B: 0, A: 0xff}
	blueBg   = snapshot.Color{Kind: snapshot.ColorRGB, R: 0, G: 0, B: 200}
	blueRGBA = color.RGBA{R: 0, G: 0, B: 200, A: 0xff}
)

// styled returns a one-row snapshot of text with a single span over runes
// [start, end) carrying st.
func styled(cols int, text string, start, end int, st snapshot.Style) snapshot.Snapshot {
	s := plainSnapshot(cols, text)
	s.Spans = [][]snapshot.Span{{{Start: start, End: end, Style: st}}}
	return s
}

func mustRender(t *testing.T, r *Renderer, s snapshot.Snapshot) *image.RGBA {
	t.Helper()
	img, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

// cornerPixel is the top-left pixel of the cell, which no glyph touches.
func cornerPixel(r *Renderer, img *image.RGBA, col, row int) color.RGBA {
	o := r.cellOrigin(col, row)
	return img.RGBAAt(o.X, o.Y)
}

// cellPixels returns the pixels of the cell at (col, row).
func cellPixels(r *Renderer, img *image.RGBA, col, row, width int) []color.RGBA {
	o := r.cellOrigin(col, row)
	var px []color.RGBA
	for y := o.Y; y < o.Y+r.metrics.CellHeight; y++ {
		for x := o.X; x < o.X+width*r.metrics.CellWidth; x++ {
			px = append(px, img.RGBAAt(x, y))
		}
	}
	return px
}

// rowsWithColour returns the cell-relative y offsets of rows in which every
// pixel across width cells is c.
func rowsWithColour(r *Renderer, img *image.RGBA, col, row, width int, c color.RGBA) []int {
	o := r.cellOrigin(col, row)
	var ys []int
	for y := 0; y < r.metrics.CellHeight; y++ {
		full := true
		for x := 0; x < width*r.metrics.CellWidth; x++ {
			if img.RGBAAt(o.X+x, o.Y+y) != c {
				full = false
				break
			}
		}
		if full {
			ys = append(ys, y)
		}
	}
	return ys
}

// coverage says how far p lies from colour a toward colour b (0 = a,
// 1 = b) by comparing channel sums; anti-aliased glyph pixels are blends of
// the two.
func coverage(p, a, b color.RGBA) float64 {
	sum := func(c color.RGBA) float64 { return float64(c.R) + float64(c.G) + float64(c.B) }
	return (sum(p) - sum(a)) / (sum(b) - sum(a))
}

// countCovered counts pixels of the cell lying at least 0.6 of the way from a
// to b.
func countCovered(r *Renderer, img *image.RGBA, col, row int, a, b color.RGBA) int {
	n := 0
	for _, p := range cellPixels(r, img, col, row, 1) {
		if coverage(p, a, b) >= 0.6 {
			n++
		}
	}
	return n
}

func TestBackgroundSpanPaintsOnlyItsCells(t *testing.T) {
	r, m := newTestRenderer(t, Options{})
	img := mustRender(t, r, styled(4, "a b", 1, 2, snapshot.Style{Bg: blueBg}))
	o := r.cellOrigin(1, 0)
	if got := img.RGBAAt(o.X+m.CellWidth/2, o.Y+m.CellHeight/2); got != blueRGBA {
		t.Errorf("centre of styled cell = %v, want %v", got, blueRGBA)
	}
	for _, col := range []int{0, 2, 3} {
		if got := cornerPixel(r, img, col, 0); got != r.theme.Background {
			t.Errorf("cell %d corner = %v, want theme background", col, got)
		}
	}
	// The whole cell is painted, edge to edge.
	for i, p := range cellPixels(r, img, 1, 0, 1) {
		if p != blueRGBA {
			t.Fatalf("styled cell pixel %d = %v, want %v", i, p, blueRGBA)
		}
	}
}

func TestForegroundColourInk(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, styled(3, "MMM", 1, 2, snapshot.Style{Fg: red}))
	ink := 0
	for _, p := range cellPixels(r, img, 1, 0, 1) {
		if p == r.theme.Background {
			continue
		}
		ink++
		if p.R < p.G || p.G != p.B {
			t.Fatalf("ink pixel %v is not a blend of red over the background", p)
		}
	}
	if strong := countCovered(r, img, 1, 0, r.theme.Background, redRGBA); ink == 0 || strong*3 < ink {
		t.Errorf("only %d of %d ink pixels are mostly the fg colour", strong, ink)
	}
	// Neighbours keep the theme foreground.
	for _, p := range cellPixels(r, img, 0, 0, 1) {
		if p == redRGBA {
			t.Fatal("unstyled neighbour has red ink")
		}
	}
}

func TestInverseSwapsColours(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, styled(3, "MMM", 1, 2, snapshot.Style{Attrs: snapshot.Inverse}))
	if got := cornerPixel(r, img, 1, 0); got != r.theme.Foreground {
		t.Errorf("inverse cell bg = %v, want theme foreground %v", got, r.theme.Foreground)
	}
	if holes := countCovered(r, img, 1, 0, r.theme.Foreground, r.theme.Background); holes < 10 {
		t.Errorf("glyph drawn in background colour covers %d pixels, want >= 10", holes)
	}
}

func TestFaintDimsForeground(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, styled(2, "MM", 1, 2, snapshot.Style{Attrs: snapshot.Faint}))
	faint, _ := r.theme.Colors(snapshot.Style{Attrs: snapshot.Faint})
	bg := r.theme.Background
	if n := countCovered(r, img, 1, 0, bg, faint); n < 10 {
		t.Errorf("faint cell has %d pixels near the dimmed colour %v, want >= 10", n, faint)
	}
	if n := countCovered(r, img, 1, 0, bg, r.theme.Foreground); n != 0 {
		t.Errorf("faint cell has %d pixels near the full foreground", n)
	}
	if n := countCovered(r, img, 0, 0, bg, r.theme.Foreground); n < 10 {
		t.Errorf("unstyled cell has %d pixels near the foreground, want >= 10", n)
	}
}

func TestInvisibleDrawsNoInk(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, styled(3, "MMM", 0, 3, snapshot.Style{Attrs: snapshot.Invisible | snapshot.Underline | snapshot.Strike}))
	for col := 0; col < 3; col++ {
		if n := inkInCell(r, img, col, 0); n != 0 {
			t.Errorf("invisible cell %d has %d ink pixels", col, n)
		}
	}
}

func pixelDiff(a, b *image.RGBA) int {
	n := 0
	for i := 0; i < len(a.Pix); i += 4 {
		if !bytes.Equal(a.Pix[i:i+4], b.Pix[i:i+4]) {
			n++
		}
	}
	return n
}

func TestBoldAndItalicChangeGlyphs(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	regular := mustRender(t, r, plainSnapshot(1, "A"))
	for _, tc := range []struct {
		name string
		attr snapshot.Attr
	}{
		{"bold", snapshot.Bold},
		{"italic", snapshot.Italic},
		{"bold italic", snapshot.Bold | snapshot.Italic},
	} {
		t.Run(tc.name, func(t *testing.T) {
			img := mustRender(t, r, styled(1, "A", 0, 1, snapshot.Style{Attrs: tc.attr}))
			if d := pixelDiff(regular, img); d < 10 {
				t.Errorf("%s 'A' differs from regular in only %d pixels", tc.name, d)
			}
		})
	}
	bold := mustRender(t, r, styled(1, "A", 0, 1, snapshot.Style{Attrs: snapshot.Bold}))
	if inkInCell(r, bold, 0, 0) <= inkInCell(r, regular, 0, 0) {
		t.Error("bold 'A' has no more ink than regular")
	}
}

func TestUnderlineAndStrikeRows(t *testing.T) {
	r, m := newTestRenderer(t, Options{})
	fg := r.theme.Foreground
	th := max(1, m.CellHeight/14)
	for _, tc := range []struct {
		name string
		attr snapshot.Attr
		top  int
	}{
		{"underline", snapshot.Underline, r.underlineTop()},
		{"strike", snapshot.Strike, r.strikeTop()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Spaces keep glyph ink out of the way; the line spans 2 cells.
			img := mustRender(t, r, styled(3, "  ", 0, 2, snapshot.Style{Attrs: tc.attr}))
			var want []int
			for y := tc.top; y < tc.top+th; y++ {
				want = append(want, y)
			}
			for _, col := range []int{0, 1} {
				got := rowsWithColour(r, img, col, 0, 1, fg)
				if !equalInts(got, want) {
					t.Errorf("cell %d: decoration rows %v, want %v", col, got, want)
				}
			}
			if n := inkInCell(r, img, 2, 0); n != 0 {
				t.Errorf("cell after the span has %d ink pixels", n)
			}
		})
	}
	if top := r.underlineTop(); top <= m.Ascent || top+th > m.CellHeight {
		t.Errorf("underline rows %d..%d not between the baseline %d and the cell bottom %d", top, top+th, m.Ascent, m.CellHeight)
	}
	if st := r.strikeTop(); st <= 0 || st >= m.Ascent {
		t.Errorf("strike row %d not between cell top and baseline %d", st, m.Ascent)
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

func TestWideCellBackgroundCoversTwoCells(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := styled(5, "世a", 0, 1, snapshot.Style{Bg: blueBg})
	s.Widths = [][]snapshot.Wide{{{Rune: 0, Cells: 2}}}
	img := mustRender(t, r, s)
	for _, col := range []int{0, 1} {
		if got := cornerPixel(r, img, col, 0); got != blueRGBA {
			t.Errorf("cell %d corner = %v, want %v", col, got, blueRGBA)
		}
	}
	if got := cornerPixel(r, img, 2, 0); got != r.theme.Background {
		t.Errorf("cell 2 corner = %v, want background", got)
	}
}

func TestSpanAfterWideRuneUsesColumns(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	// Rune 1 ('a') sits in column 2 because 世 takes columns 0-1.
	s := styled(6, "世ab", 1, 2, snapshot.Style{Bg: blueBg})
	s.Widths = [][]snapshot.Wide{{{Rune: 0, Cells: 2}}}
	img := mustRender(t, r, s)
	want := map[int]color.RGBA{0: r.theme.Background, 1: r.theme.Background, 2: blueRGBA, 3: r.theme.Background}
	for col, c := range want {
		if got := cornerPixel(r, img, col, 0); got != c {
			t.Errorf("cell %d corner = %v, want %v", col, got, c)
		}
	}
}

func TestZeroWidthRuneDoesNotPaintBackground(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := styled(4, "e\u0301x", 1, 2, snapshot.Style{Bg: blueBg})
	s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 0}}}
	img := mustRender(t, r, s)
	for col := 0; col < 3; col++ {
		if got := cornerPixel(r, img, col, 0); got != r.theme.Background {
			t.Errorf("cell %d corner = %v, want background (mark has a style but paints nothing)", col, got)
		}
	}
}

func TestStyledRenderDeterministic(t *testing.T) {
	s := plainSnapshot(12, "bold italic", "red on blue")
	s.Spans = [][]snapshot.Span{
		{{Start: 0, End: 4, Style: snapshot.Style{Attrs: snapshot.Bold | snapshot.Underline}}, {Start: 5, End: 11, Style: snapshot.Style{Attrs: snapshot.Italic | snapshot.Strike}}},
		{{Start: 0, End: 3, Style: snapshot.Style{Fg: red, Bg: blueBg}}},
	}
	s.Cursor = snapshot.Cursor{X: 3, Y: 1, Visible: true}
	r1, _ := newTestRenderer(t, Options{Theme: theme.Light()})
	r2, _ := newTestRenderer(t, Options{Theme: theme.Light()})
	a, b, c := mustRender(t, r1, s), mustRender(t, r1, s), mustRender(t, r2, s)
	if !bytes.Equal(a.Pix, b.Pix) || !bytes.Equal(a.Pix, c.Pix) {
		t.Error("styled renders differ between calls or Renderers")
	}
}

func TestSpanWalker(t *testing.T) {
	a, b := snapshot.Style{Attrs: snapshot.Bold}, snapshot.Style{Attrs: snapshot.Italic}
	w := spanWalker{spans: []snapshot.Span{{Start: 1, End: 3, Style: a}, {Start: 3, End: 4, Style: b}, {Start: 6, End: 7, Style: a}}}
	want := []snapshot.Style{{}, a, a, b, {}, {}, a, {}, {}}
	for i, ws := range want {
		if got := w.at(i); got != ws {
			t.Errorf("at(%d) = %v, want %v", i, got, ws)
		}
	}
}

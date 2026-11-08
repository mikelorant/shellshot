package raster

import (
	"errors"
	"image"
	"math"
	"testing"

	xfont "golang.org/x/image/font"

	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// inkOutsideCells counts non-background pixels of the canvas that lie
// outside the given cells (all on row 0), padding included.
func inkOutsideCells(r *Renderer, img *image.RGBA, cols ...int) int {
	inside := func(x, y int) bool {
		for _, col := range cols {
			o := r.cellOrigin(col, 0)
			if image.Pt(x, y).In(image.Rect(o.X, o.Y, o.X+r.metrics.CellWidth, o.Y+r.metrics.CellHeight)) {
				return true
			}
		}
		return false
	}
	n := 0
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if img.RGBAAt(x, y) != r.theme.Background && !inside(x, y) {
				n++
			}
		}
	}
	return n
}

// A Nerd Font icon the primary font lacks is drawn by the fallback font
// starting at the left edge of its cell and overflowing to the right: nothing
// is inked left of its cell, and the ink reaches into the next cell.
func TestFallbackNerdGlyphOverflowsToTheRight(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	for _, c := range []rune{0xF015, 0xE725, 0xF0001} {
		img := mustRender(t, r, plainSnapshot(5, " "+string(c)+"  "))
		cell, next := cellSpan(r, 1, 1), cellSpan(r, 2, 1)
		if inkInCell(r, img, 1, 0) == 0 {
			t.Errorf("%U: no ink in its cell", c)
		}
		if inkInCell(r, img, 2, 0) == 0 {
			t.Errorf("%U: no overflow into the next cell", c)
		}
		if inkInCell(r, img, 0, 0) != 0 {
			t.Errorf("%U: ink left of its cell", c)
		}
		box := inkBox(r, img, cellSpan(r, 0, 5))
		if box.Min.X < cell.Min.X || box.Min.X > cell.Min.X+2 {
			t.Errorf("%U: ink starts at x %d, want within 2 px of the cell's left edge %d", c, box.Min.X, cell.Min.X)
		}
		if box.Max.X <= cell.Max.X || box.Max.X > next.Max.X+r.metrics.CellWidth {
			t.Errorf("%U: ink ends at x %d, cell ends at %d", c, box.Max.X, cell.Max.X)
		}
	}
}

// A wide icon cell keeps the left edge too: the icon is not centred in the
// two cells.
func TestFallbackWideNerdGlyphKeepsLeftEdge(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	narrow := mustRender(t, r, plainSnapshot(5, " \uf015  "))
	s := plainSnapshot(5, " \uf015 ")
	s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 2}}}
	wide := mustRender(t, r, s)
	if string(narrow.Pix) != string(wide.Pix) {
		t.Error("a wide icon cell draws differently from a narrow one")
	}
}

// The icon is painted over the background of the cell it overflows into, and
// that cell's background is otherwise left as it was.
func TestFallbackNerdGlyphOverflowsOverNeighbourBackground(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(5, " \uf015  ")
	s.Spans = [][]snapshot.Span{{{Start: 2, End: 3, Style: snapshot.Style{Bg: blueBg}}}}
	img := mustRender(t, r, s)
	next := cellSpan(r, 2, 1)
	bg := img.RGBAAt(next.Max.X-1, next.Max.Y-1)
	if bg == r.theme.Background {
		t.Fatal("neighbour background not painted")
	}
	over := 0
	for y := next.Min.Y; y < next.Max.Y; y++ {
		for x := next.Min.X; x < next.Max.X; x++ {
			if img.RGBAAt(x, y) != bg {
				over++
			}
		}
	}
	if over == 0 {
		t.Error("icon does not show over the next cell's background")
	}
}

// An icon in the last column overflows past the grid, as far as the padding
// and no further: it is clipped to the image, with no out-of-range write.
func TestFallbackNerdGlyphInLastColumnIsClippedToImage(t *testing.T) {
	r, _ := newTestRenderer(t, Options{Padding: 1})
	img := mustRender(t, r, plainSnapshot(3, "  \uf015"))
	w, h := r.Size(3, 1)
	if img.Bounds() != image.Rect(0, 0, w, h) {
		t.Fatalf("image bounds %v, want %dx%d", img.Bounds(), w, h)
	}
	if inkInCell(r, img, 2, 0) == 0 {
		t.Error("no ink in the last column")
	}
	if img.RGBAAt(w-1, h/2) == r.theme.Background {
		t.Error("the overflow does not reach the right edge of the image")
	}
}

// A CJK glyph, which is not an icon, is still fitted to the cells it occupies
// and clipped to them.
func TestFallbackCJKStillFitsItsCells(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(5, " \u4e16  ")
	s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 2}}}
	img := mustRender(t, r, s)
	span := cellSpan(r, 1, 2)
	box := inkBox(r, img, span)
	if box.Empty() || !box.In(span) {
		t.Errorf("ink box %v not within span %v", box, span)
	}
	if out := inkOutsideCells(r, img, 1, 2); out != 0 {
		t.Errorf("%d inked pixels outside the two cells", out)
	}
	if dx := box.Min.X + box.Max.X - span.Min.X - span.Max.X; dx < -3 || dx > 3 {
		t.Errorf("ink box %v not centred horizontally in %v", box, span)
	}
}

// The symbols font has one style: bold and italic icons are drawn exactly
// like regular ones (with their own colours, here none).
func TestFallbackBoldItalicUseRegularSymbols(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	want := mustRender(t, r, plainSnapshot(3, "\uf015\ue725"))
	for _, a := range []snapshot.Attr{snapshot.Bold, snapshot.Italic, snapshot.Bold | snapshot.Italic} {
		s := plainSnapshot(3, "\uf015\ue725")
		s.Spans = [][]snapshot.Span{{{Start: 0, End: 2, Style: snapshot.Style{Attrs: a}}}}
		got := mustRender(t, r, s)
		if string(got.Pix) != string(want.Pix) {
			t.Errorf("attrs %v: icons differ from the regular rendering", a)
		}
	}
}

// A rune no font has stays blank and does not panic.
func TestFallbackMissingEverywhereIsBlank(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, plainSnapshot(4, "\U0010FFFE\U0010FFFD "))
	if got := inkOutsideCells(r, img); got != 0 {
		t.Errorf("%d inked pixels for runes no font has", got)
	}
}

// Sprite-drawn runes never reach the fonts, whatever the fallback holds.
func TestFallbackSpriteStillFirst(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	f := &countingFonts{Set: set}
	r, err := New(f, Options{})
	if err != nil {
		t.Fatal(err)
	}
	mustRender(t, r, plainSnapshot(4, "─█\ue0b0 "))
	if f.faceForCalls != 0 {
		t.Errorf("FitFace called %d times for sprite runes", f.faceForCalls)
	}
	mustRender(t, r, plainSnapshot(4, "\uf015"))
	if f.faceForCalls != 1 {
		t.Errorf("FitFace called %d times for one icon, want 1", f.faceForCalls)
	}
}

type countingFonts struct {
	*font.Set
	faceForCalls int
}

func (f *countingFonts) FitFace(r rune, bold, italic bool, sizePx float64, cellW, cellH, cells int) (xfont.Face, bool, error) {
	f.faceForCalls++
	return f.Set.FitFace(r, bold, italic, sizePx, cellW, cellH, cells)
}

type failingFonts struct{ *font.Set }

func (failingFonts) FitFace(rune, bool, bool, float64, int, int, int) (xfont.Face, bool, error) {
	return nil, false, errors.New("boom")
}

// A fallback lookup error leaves the rune blank rather than failing the render.
func TestFallbackLookupErrorIsBlank(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(failingFonts{set}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	img := mustRender(t, r, plainSnapshot(3, "a\uf015"))
	if inkInCell(r, img, 0, 0) == 0 {
		t.Error("primary glyph a not drawn")
	}
	if inkInCell(r, img, 1, 0) != 0 {
		t.Error("icon drawn despite fallback error")
	}
}

func TestGoldenNerdSymbols(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(24,
		"\ue725 main  \uf015 home  \uf07b dir",
		"\ue0a0 branch (primary)  \ue0b0\ue0b1",
		"\U000f0001 \U000f02a2 \U000f1af0 md icons",
		"bold \uf015 italic \ue725 end")
	s.Spans = [][]snapshot.Span{nil, nil, nil, {
		{Start: 0, End: 6, Style: snapshot.Style{Attrs: snapshot.Bold}},
		{Start: 7, End: 14, Style: snapshot.Style{Attrs: snapshot.Italic}},
	}}
	compareGolden(t, "nerd_symbols.png", mustRender(t, r, s))
}

// inkBox returns the bounding box of non-background pixels inside rect.
func inkBox(r *Renderer, img *image.RGBA, rect image.Rectangle) image.Rectangle {
	var box image.Rectangle
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		for x := rect.Min.X; x < rect.Max.X; x++ {
			if img.RGBAAt(x, y) != r.theme.Background {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box
}

func cellSpan(r *Renderer, col, cells int) image.Rectangle {
	o := r.cellOrigin(col, 0)
	return image.Rect(o.X, o.Y, o.X+cells*r.metrics.CellWidth, o.Y+r.metrics.CellHeight)
}

// Nerd Font icons are sized by height: at the font size unless that is taller
// than the cell, centred vertically, never wider than the font size (one em)
// and never clipped to the cell, at several sizes and scales, for one and
// two cells.
func TestFallbackIconsAreSizedByHeight(t *testing.T) {
	for _, tc := range []struct {
		name  string
		opts  Options
		cells int
	}{
		{"14px", Options{FontSize: 14}, 1},
		{"20px", Options{FontSize: 20}, 1},
		{"scale2", Options{FontSize: 14, Scale: 2}, 1},
		{"14px wide", Options{FontSize: 14}, 2},
		{"scale2 wide", Options{FontSize: 14, Scale: 2}, 2},
	} {
		r, _ := newTestRenderer(t, tc.opts)
		em := int(math.Round(r.sizePx))
		for _, c := range []rune{0xF015, 0xE725} {
			s := plainSnapshot(6, " "+string(c)+"   ")
			if tc.cells == 2 {
				s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 2}}}
			}
			img := mustRender(t, r, s)
			span := cellSpan(r, 1, tc.cells)
			box := inkBox(r, img, cellSpan(r, 0, 6))
			if box.Empty() {
				t.Errorf("%s %U: no ink", tc.name, c)
				continue
			}
			if box.Dy() > r.metrics.CellHeight {
				t.Errorf("%s %U: ink height %d exceeds the cell height %d", tc.name, c, box.Dy(), r.metrics.CellHeight)
			}
			if box.Dx() > em+1 {
				t.Errorf("%s %U: ink width %d exceeds the font size %d", tc.name, c, box.Dx(), em)
			}
			if tc.cells == 1 && box.Dx() <= r.metrics.CellWidth {
				t.Errorf("%s %U: ink width %d does not exceed the %d px cell: icon was fitted to the cell", tc.name, c, box.Dx(), r.metrics.CellWidth)
			}
			// The ink starts at the glyph's own left bearing, up to a quarter em.
			if box.Min.X < span.Min.X || box.Min.X > span.Min.X+em/4 {
				t.Errorf("%s %U: ink starts at x %d, want near the cell's left edge %d", tc.name, c, box.Min.X, span.Min.X)
			}
			dy := box.Min.Y + box.Max.Y - span.Min.Y - span.Max.Y
			if dy < -3 || dy > 3 {
				t.Errorf("%s %U: ink box %v not centred vertically in %v", tc.name, c, box, span)
			}
		}
	}
}

// Fitted fallback drawing is deterministic.
func TestFallbackFitDeterministic(t *testing.T) {
	s := plainSnapshot(6, "\uf015 \ue725 \U000f0001")
	a, _ := newTestRenderer(t, Options{})
	b, _ := newTestRenderer(t, Options{})
	first := mustRender(t, a, s)
	if string(first.Pix) != string(mustRender(t, a, s).Pix) || string(first.Pix) != string(mustRender(t, b, s).Pix) {
		t.Error("fitted fallback rendering is not deterministic")
	}
}

// Primary glyphs are drawn where and how they always were: a line of text
// renders the same with or without an icon elsewhere on the row.
func TestFallbackFitLeavesPrimaryGlyphsAlone(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	plain := mustRender(t, r, plainSnapshot(6, "abc   "))
	mixed := mustRender(t, r, plainSnapshot(6, "abc \uf015 "))
	for col := 0; col < 3; col++ {
		if inkInCell(r, plain, col, 0) == 0 {
			t.Fatalf("cell %d has no ink", col)
		}
		span := cellSpan(r, col, 1)
		for y := span.Min.Y; y < span.Max.Y; y++ {
			for x := span.Min.X; x < span.Max.X; x++ {
				if plain.RGBAAt(x, y) != mixed.RGBAAt(x, y) {
					t.Fatalf("primary cell %d pixel (%d,%d) changed", col, x, y)
				}
			}
		}
	}
}

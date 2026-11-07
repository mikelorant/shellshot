package raster

import (
	"errors"
	"image"
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
// inside its own cell only, even though the icon is wider than the cell.
func TestFallbackNerdGlyphInksOnlyItsCell(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	for _, c := range []rune{0xF015, 0xE725, 0xF0001} {
		img := mustRender(t, r, plainSnapshot(5, " "+string(c)+"  "))
		if inkInCell(r, img, 1, 0) == 0 {
			t.Errorf("%U: no ink in its cell", c)
		}
		if out := inkOutsideCells(r, img, 1); out != 0 {
			t.Errorf("%U: %d inked pixels outside its cell", c, out)
		}
	}
}

// A wide rune is clipped to two cells, not one and not three.
func TestFallbackWideNerdGlyphClipsToTwoCells(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(5, " \uf015 ")
	s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 2}}}
	img := mustRender(t, r, s)
	if inkInCell(r, img, 1, 0) == 0 || inkInCell(r, img, 2, 0) == 0 {
		t.Errorf("wide icon ink = %d, %d cells; want both inked", inkInCell(r, img, 1, 0), inkInCell(r, img, 2, 0))
	}
	if out := inkOutsideCells(r, img, 1, 2); out != 0 {
		t.Errorf("%d inked pixels outside the two cells", out)
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

// Fallback icons are scaled to fit their cell: the ink covers most of the
// cell width, stays inside the cell span and is centred in it, at several
// sizes and scales, for one and two cells. (An icon designed to fill its
// advance legitimately inks the edge columns; TestFitFaceBoundsWithinBudget
// proves the fitted glyph is not cut off by the clip.)
func TestFallbackIconsFitTheirCells(t *testing.T) {
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
		for _, c := range []rune{0xF015, 0xE725} {
			s := plainSnapshot(5, " "+string(c)+"  ")
			if tc.cells == 2 {
				s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 2}}}
			}
			img := mustRender(t, r, s)
			span := cellSpan(r, 1, tc.cells)
			box := inkBox(r, img, span)
			if box.Empty() {
				t.Errorf("%s %U: no ink", tc.name, c)
				continue
			}
			if w := float64(box.Dx()) / float64(span.Dx()); w <= 0.6 {
				t.Errorf("%s %U: ink width %d is %.0f%% of the %d px span, want > 60%%", tc.name, c, box.Dx(), w*100, span.Dx())
			}
			if !box.In(span) {
				t.Errorf("%s %U: ink box %v exceeds cell span %v", tc.name, c, box, span)
			}
			cols := []int{1}
			if tc.cells == 2 {
				cols = append(cols, 2)
			}
			if out := inkOutsideCells(r, img, cols...); out != 0 {
				t.Errorf("%s %U: %d inked pixels outside its cells", tc.name, c, out)
			}
			// centred: the ink box midpoint is within a pixel of the span's.
			dx := box.Min.X + box.Max.X - span.Min.X - span.Max.X
			dy := box.Min.Y + box.Max.Y - span.Min.Y - span.Max.Y
			if dx < -3 || dx > 3 || dy < -3 || dy > 3 {
				t.Errorf("%s %U: ink box %v not centred in %v", tc.name, c, box, span)
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

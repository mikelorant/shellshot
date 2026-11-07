package raster

import (
	"errors"
	"image"
	"testing"

	xfont "golang.org/x/image/font"

	"github.com/michaellorant/shellshot/internal/render/font"
	"github.com/michaellorant/shellshot/internal/snapshot"
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
	img := mustRender(t, r, plainSnapshot(4, "\U0010FFFE世 "))
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
		t.Errorf("FaceFor called %d times for sprite runes", f.faceForCalls)
	}
	mustRender(t, r, plainSnapshot(4, "\uf015"))
	if f.faceForCalls != 1 {
		t.Errorf("FaceFor called %d times for one icon, want 1", f.faceForCalls)
	}
}

type countingFonts struct {
	*font.Set
	faceForCalls int
}

func (f *countingFonts) FaceFor(r rune, bold, italic bool, sizePx float64) (xfont.Face, bool, error) {
	f.faceForCalls++
	return f.Set.FaceFor(r, bold, italic, sizePx)
}

type failingFonts struct{ *font.Set }

func (failingFonts) FaceFor(rune, bool, bool, float64) (xfont.Face, bool, error) {
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

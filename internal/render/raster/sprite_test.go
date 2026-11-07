package raster

import (
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

// A box-drawing rune is drawn by the sprite package: a solid foreground
// line across the whole cell at the cell's middle rows, with nothing else,
// instead of the font's anti-aliased glyph.
func TestRenderBoxDrawingUsesSprite(t *testing.T) {
	r, m := newTestRenderer(t, Options{})
	img, err := r.Render(plainSnapshot(3, "a─█"))
	if err != nil {
		t.Fatal(err)
	}
	fg, bg := r.theme.Foreground, r.theme.Background
	o := r.cellOrigin(1, 0)
	th := max(1, m.CellHeight/14)
	y0 := (m.CellHeight - th) / 2
	for y := 0; y < m.CellHeight; y++ {
		for x := 0; x < m.CellWidth; x++ {
			want := bg
			if y >= y0 && y < y0+th {
				want = fg
			}
			if got := img.RGBAAt(o.X+x, o.Y+y); got != want {
				t.Fatalf("─ pixel (%d,%d) = %v, want %v", x, y, got, want)
			}
		}
	}
	// █ fills its cell solidly.
	if got := inkInCell(r, img, 2, 0); got != m.CellWidth*m.CellHeight {
		t.Errorf("█ inks %d pixels, want %d", got, m.CellWidth*m.CellHeight)
	}
}

// A wide rune that the sprite package handles fills its two cells.
func TestRenderWideSpriteRuneSpansTwoCells(t *testing.T) {
	r, m := newTestRenderer(t, Options{})
	s := plainSnapshot(3, "█ ")
	s.Widths = [][]snapshot.Wide{{{Rune: 0, Cells: 2}}}
	img, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	want := m.CellWidth * m.CellHeight
	if inkInCell(r, img, 0, 0) != want || inkInCell(r, img, 1, 0) != want || inkInCell(r, img, 2, 0) != 0 {
		t.Errorf("wide █ ink = %d, %d, %d; want %d, %d, 0", inkInCell(r, img, 0, 0), inkInCell(r, img, 1, 0), inkInCell(r, img, 2, 0), want, want)
	}
}

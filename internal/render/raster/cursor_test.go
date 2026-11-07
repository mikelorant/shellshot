package raster

import (
	"image"
	"testing"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

func withCursor(s snapshot.Snapshot, x, y int, visible bool) snapshot.Snapshot {
	s.Cursor = snapshot.Cursor{X: x, Y: y, Visible: visible}
	return s
}

func TestBlockCursorInvertsGlyph(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, withCursor(plainSnapshot(5, "MMMMM"), 2, 0, true))
	if got := cornerPixel(r, img, 2, 0); got != r.theme.Cursor {
		t.Errorf("cursor cell corner = %v, want %v", got, r.theme.Cursor)
	}
	if holes := countCovered(r, img, 2, 0, r.theme.Cursor, r.theme.Background); holes < 10 {
		t.Errorf("glyph under block cursor has %d background-coloured pixels, want >= 10", holes)
	}
	// Neighbours are untouched.
	plain := mustRender(t, r, plainSnapshot(5, "MMMMM"))
	for _, col := range []int{0, 1, 3, 4} {
		if !sameCell(r, plain, img, col, 0) {
			t.Errorf("cell %d changed by the cursor", col)
		}
	}
}

func sameCell(r *Renderer, a, b *image.RGBA, col, row int) bool {
	pa, pb := cellPixels(r, a, col, row, 1), cellPixels(r, b, col, row, 1)
	for i := range pa {
		if pa[i] != pb[i] {
			return false
		}
	}
	return true
}

func TestBlockCursorOverridesSpanColours(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := withCursor(styled(3, "MMM", 0, 3, snapshot.Style{Fg: red, Bg: blueBg}), 1, 0, true)
	img := mustRender(t, r, s)
	if got := cornerPixel(r, img, 1, 0); got != r.theme.Cursor {
		t.Errorf("cursor cell corner = %v, want cursor colour", got)
	}
	if got := cornerPixel(r, img, 0, 0); got != blueRGBA {
		t.Errorf("neighbour corner = %v, want span bg", got)
	}
}

func TestBlockCursorOnBlankAndWide(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img := mustRender(t, r, withCursor(plainSnapshot(5, "ab"), 4, 0, true))
	for i, p := range cellPixels(r, img, 4, 0, 1) {
		if p != r.theme.Cursor {
			t.Fatalf("blank cursor cell pixel %d = %v, want cursor colour", i, p)
		}
	}
	s := withCursor(plainSnapshot(5, "世a"), 1, 0, true) // second half of the wide rune
	s.Widths = [][]snapshot.Wide{{{Rune: 0, Cells: 2}}}
	img = mustRender(t, r, s)
	for _, col := range []int{0, 1} {
		if got := cornerPixel(r, img, col, 0); got != r.theme.Cursor {
			t.Errorf("cell %d corner = %v, want cursor colour over the wide rune", col, got)
		}
	}
	if got := cornerPixel(r, img, 2, 0); got != r.theme.Background {
		t.Errorf("cell 2 corner = %v, want background", got)
	}
}

func TestCursorBarAndUnderlineShapes(t *testing.T) {
	r, m := newTestRenderer(t, Options{Cursor: CursorBar})
	cur := r.theme.Cursor
	base := plainSnapshot(4, "MMMM")
	bar := mustRender(t, r, withCursor(base, 1, 0, true))
	o := r.cellOrigin(1, 0)
	bw := max(1, m.CellWidth/8)
	plain := mustRender(t, r, base)
	for y := 0; y < m.CellHeight; y++ {
		for x := 0; x < m.CellWidth; x++ {
			got, unchanged := bar.RGBAAt(o.X+x, o.Y+y), plain.RGBAAt(o.X+x, o.Y+y)
			if x < bw && got != cur {
				t.Fatalf("bar: pixel (%d,%d) = %v, want cursor colour", x, y, got)
			}
			if x >= bw && got != unchanged {
				t.Fatalf("bar: pixel (%d,%d) = %v, want unchanged %v", x, y, got, unchanged)
			}
		}
	}

	r, m = newTestRenderer(t, Options{Cursor: CursorUnderline})
	ul := mustRender(t, r, withCursor(base, 1, 0, true))
	th := max(1, m.CellHeight/14)
	want := []int{}
	for y := m.CellHeight - th; y < m.CellHeight; y++ {
		want = append(want, y)
	}
	if got := rowsWithColour(r, ul, 1, 0, 1, cur); !equalInts(got, want) {
		t.Errorf("underline cursor rows %v, want %v", got, want)
	}
	// The glyph stays in the normal foreground: the shape does not invert.
	if inkInCell(r, ul, 1, 0) < 10 {
		t.Error("underline cursor removed the glyph")
	}
	for _, col := range []int{0, 2, 3} {
		if rows := rowsWithColour(r, ul, col, 0, 1, cur); len(rows) != 0 {
			t.Errorf("cell %d has cursor rows %v", col, rows)
		}
	}
}

func TestCursorNoneHiddenAndOutOfRange(t *testing.T) {
	base := plainSnapshot(4, "MMMM", "MM")
	rBlock, _ := newTestRenderer(t, Options{})
	want := mustRender(t, rBlock, base)
	for _, tc := range []struct {
		name string
		opts Options
		cur  snapshot.Cursor
	}{
		{"none", Options{Cursor: CursorNone}, snapshot.Cursor{X: 1, Y: 0, Visible: true}},
		{"hidden", Options{}, snapshot.Cursor{X: 1, Y: 0, Visible: false}},
		{"right of canvas", Options{}, snapshot.Cursor{X: 4, Y: 0, Visible: true}},
		{"below canvas", Options{}, snapshot.Cursor{X: 0, Y: 2, Visible: true}},
		{"negative", Options{}, snapshot.Cursor{X: -1, Y: -1, Visible: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _ := newTestRenderer(t, tc.opts)
			s := base
			s.Cursor = tc.cur
			if d := pixelDiff(want, mustRender(t, r, s)); d != 0 {
				t.Errorf("%d pixels differ from the cursor-less render", d)
			}
		})
	}
	for _, style := range []CursorStyle{CursorBar, CursorUnderline} {
		r, _ := newTestRenderer(t, Options{Cursor: style})
		s := withCursor(base, 4, 0, true)
		if d := pixelDiff(want, mustRender(t, r, s)); d != 0 {
			t.Errorf("style %d: out-of-range cursor changed %d pixels", style, d)
		}
	}
}

func TestNewRejectsBadCursorStyle(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	if _, err := New(r.fonts, Options{Cursor: CursorNone + 1}); err == nil {
		t.Error("New accepted an unknown cursor style")
	}
}

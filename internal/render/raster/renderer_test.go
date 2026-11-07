package raster

import (
	"bytes"
	"image"
	"image/color"
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/theme"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

func newTestRenderer(t *testing.T, o Options) (*Renderer, font.Metrics) {
	t.Helper()
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	r, err := New(set, o)
	if err != nil {
		t.Fatal(err)
	}
	return r, r.metrics
}

func plainSnapshot(cols int, lines ...string) snapshot.Snapshot {
	return snapshot.Snapshot{Cols: cols, Rows: len(lines), Lines: lines}
}

// inkInCell counts pixels differing from the theme background inside the
// cell at (col, row).
func inkInCell(r *Renderer, img *image.RGBA, col, row int) int {
	o := r.cellOrigin(col, row)
	cell := image.Rect(o.X, o.Y, o.X+r.metrics.CellWidth, o.Y+r.metrics.CellHeight)
	n := 0
	for y := cell.Min.Y; y < cell.Max.Y; y++ {
		for x := cell.Min.X; x < cell.Max.X; x++ {
			if img.RGBAAt(x, y) != r.theme.Background {
				n++
			}
		}
	}
	return n
}

func TestSizeFromMetrics(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		opts       Options
		size       float64
		pad, scale int
		cols, rows int
	}{
		{"defaults", Options{}, 15, 8, 1, 80, 24},
		{"scale 2", Options{FontSize: 12, Padding: 5, Scale: 2}, 12, 10, 2, 7, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, err := set.Metrics(tc.size * float64(tc.scale))
			if err != nil {
				t.Fatal(err)
			}
			r, err := New(set, tc.opts)
			if err != nil {
				t.Fatal(err)
			}
			w, h := r.Size(tc.cols, tc.rows)
			if want := tc.cols*m.CellWidth + 2*tc.pad; w != want {
				t.Errorf("width = %d, want %d", w, want)
			}
			if want := tc.rows*m.CellHeight + 2*tc.pad; h != want {
				t.Errorf("height = %d, want %d", h, want)
			}
			img, err := r.Render(plainSnapshot(tc.cols, make([]string, tc.rows)...))
			if err != nil {
				t.Fatal(err)
			}
			if got := img.Bounds(); got != image.Rect(0, 0, w, h) {
				t.Errorf("image bounds = %v, want %dx%d", got, w, h)
			}
		})
	}
}

func TestNewRejectsBadOptions(t *testing.T) {
	set, _ := font.Default()
	for _, o := range []Options{{FontSize: -1}, {Padding: -1}, {Scale: -1}} {
		if _, err := New(set, o); err == nil {
			t.Errorf("New(%+v) succeeded, want error", o)
		}
	}
	if _, err := New(nil, Options{}); err == nil {
		t.Error("New(nil) succeeded, want error")
	}
}

func TestRenderHello(t *testing.T) {
	r, m := newTestRenderer(t, Options{})
	img, err := r.Render(plainSnapshot(10, "Hello", ""))
	if err != nil {
		t.Fatal(err)
	}
	bg := r.theme.Background
	// Padding and the unused second row are pure background.
	w, h := r.Size(10, 2)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			inFirstRow := y >= r.pad && y < r.pad+m.CellHeight
			inGrid := x >= r.pad && x < w-r.pad
			if inFirstRow && inGrid && x < r.pad+5*m.CellWidth {
				continue
			}
			if img.RGBAAt(x, y) != bg {
				t.Fatalf("pixel (%d,%d) = %v, want background %v", x, y, img.RGBAAt(x, y), bg)
			}
		}
	}
	for col := 0; col < 5; col++ {
		if n := inkInCell(r, img, col, 0); n < 10 {
			t.Errorf("cell %d has %d ink pixels, want >= 10", col, n)
		}
	}
	for col := 5; col < 10; col++ {
		if n := inkInCell(r, img, col, 0); n != 0 {
			t.Errorf("cell %d has %d ink pixels, want 0", col, n)
		}
	}
	// Full-coverage pixels use the foreground exactly.
	found := false
	for i := 0; i < len(img.Pix); i += 4 {
		if (color.RGBA{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]}) == r.theme.Foreground {
			found = true
			break
		}
	}
	if !found {
		t.Error("no pixel with the theme foreground colour")
	}
}

func TestRenderWideRunePositioning(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(6, "\U0010FFFEa")
	s.Widths = [][]snapshot.Wide{{{Rune: 0, Cells: 2}}}
	img, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	// No embedded font has a glyph for U+10FFFE, so the wide rune draws nothing and
	// the only ink is the 'a', which must start at column 2.
	for _, col := range []int{0, 1, 3, 4, 5} {
		if n := inkInCell(r, img, col, 0); n != 0 {
			t.Errorf("cell %d has %d ink pixels, want 0", col, n)
		}
	}
	if n := inkInCell(r, img, 2, 0); n < 10 {
		t.Errorf("cell 2 has %d ink pixels, want >= 10 (the 'a')", n)
	}

	// Same screen without width info puts the 'a' in column 1.
	img, err = r.Render(plainSnapshot(6, "\U0010FFFEa"))
	if err != nil {
		t.Fatal(err)
	}
	if inkInCell(r, img, 1, 0) == 0 || inkInCell(r, img, 2, 0) != 0 {
		t.Error("without Widths the 'a' should land in column 1")
	}
}

func TestRenderZeroWidthRuneOverlaysPreviousCell(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(4, "e\u0301x")
	s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 0}}}
	img, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := r.Render(plainSnapshot(4, "ex"))
	if err != nil {
		t.Fatal(err)
	}
	if inkInCell(r, img, 0, 0) <= inkInCell(r, plain, 0, 0) {
		t.Error("combining mark added no ink to the base cell")
	}
	if inkInCell(r, img, 1, 0) == 0 || inkInCell(r, img, 2, 0) != 0 {
		t.Error("the 'x' should follow in column 1")
	}
}

func TestRenderMissingGlyphDrawsNothing(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	img, err := r.Render(plainSnapshot(3, "\U0010FFFE"))
	if err != nil {
		t.Fatal(err)
	}
	if n := inkInCell(r, img, 0, 0); n != 0 {
		t.Errorf("missing glyph drew %d pixels", n)
	}
}

func TestRenderRejectsInvalidSnapshot(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	_, err := r.Render(snapshot.Snapshot{Cols: 5, Rows: 2, Lines: []string{"x"}})
	if err == nil || !strings.Contains(err.Error(), "raster:") {
		t.Fatalf("err = %v, want wrapped raster error", err)
	}
}

func TestRenderDeterministicAndFresh(t *testing.T) {
	r, _ := newTestRenderer(t, Options{Theme: theme.Light()})
	s := plainSnapshot(12, "Hello, world", "func main() {}")
	a, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	b, err := r.Render(s)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("Render returned the same image twice")
	}
	if !bytes.Equal(a.Pix, b.Pix) {
		t.Error("two renders of the same snapshot differ")
	}
	// A second Renderer over a fresh font Set gives the same pixels.
	r2, _ := newTestRenderer(t, Options{Theme: theme.Light()})
	c, _ := r2.Render(s)
	if !bytes.Equal(a.Pix, c.Pix) {
		t.Error("renders differ across independent Renderers")
	}
}

// Adjustments apply to the font-derived cell after Scale, like Ghostty's
// adjust-cell-width and adjust-cell-height.
func TestAdjustCellSize(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	width, _ := font.ParseCellAdjust("-7%")
	height, _ := font.ParseCellAdjust("3")
	plain, err := New(set, Options{FontSize: 12, Scale: 2})
	if err != nil {
		t.Fatal(err)
	}
	adj, err := New(set, Options{FontSize: 12, Scale: 2, AdjustCellWidth: width, AdjustCellHeight: height})
	if err != nil {
		t.Fatal(err)
	}
	// 12 px at scale 2 is 24 px: 14.4 x 31.68 rounds to 14x32; -7% gives 13,
	// and 3 more rows give 35.
	if m := plain.metrics; m.CellWidth != 14 || m.CellHeight != 32 {
		t.Fatalf("plain cell %dx%d, want 14x32", m.CellWidth, m.CellHeight)
	}
	if m := adj.metrics; m.CellWidth != 13 || m.CellHeight != 35 || m.Ascent+m.Descent != 35 {
		t.Errorf("adjusted cell %+v, want 13x35", m)
	}
	if w, h := adj.Size(80, 24); w != 80*13+2*16 || h != 24*35+2*16 {
		t.Errorf("Size(80, 24) = %dx%d", w, h)
	}
}

// Adjustments that leave a cell under 1 px or over ten times the font's size
// are errors, not silently clamped.
func TestAdjustCellSizeRejectsNonsense(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, width, height string }{
		{"width -100%", "-100%", "0"},
		{"width below one pixel", "-20", "0"},
		{"width over 10x", "1000%", "0"},
		{"height over 10x", "0", "5000"},
		{"height -100%", "0", "-100%"},
	} {
		w, _ := font.ParseCellAdjust(tc.width)
		h, _ := font.ParseCellAdjust(tc.height)
		if _, err := New(set, Options{FontSize: 12, AdjustCellWidth: w, AdjustCellHeight: h}); err == nil {
			t.Errorf("%s: New succeeded, want an error", tc.name)
		}
	}
}

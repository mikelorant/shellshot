package svg

import (
	"image"
	"math"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/raster"
)

var pathNumber = regexp.MustCompile(`-?\d+(?:\.\d+)?`)

// pathBox returns the box of the points of the path data of the glyph def d
// emitted for id.
func pathBox(t testing.TB, d *defs, id string) (minX, minY, maxX, maxY float64) {
	t.Helper()
	for _, item := range d.items {
		if !strings.HasPrefix(item, `<path id="`+id+`" d="`) {
			continue
		}
		data := item[strings.Index(item, ` d="`)+4 : strings.LastIndex(item, `"`)]
		nums := pathNumber.FindAllString(data, -1)
		minX, minY, maxX, maxY = math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)
		for i := 0; i+1 < len(nums); i += 2 {
			x, _ := strconv.ParseFloat(nums[i], 64)
			y, _ := strconv.ParseFloat(nums[i+1], 64)
			minX, maxX = math.Min(minX, x), math.Max(maxX, x)
			minY, maxY = math.Min(minY, y), math.Max(maxY, y)
		}
		return
	}
	t.Fatalf("no path %s in defs", id)
	return
}

// A Nerd Font icon outline is scaled by height (font.IconScale), starts at the
// left edge of its cell with its own left bearing and is centred vertically,
// whatever the number of cells and the cell width.
func TestIconOutlineIsSizedByHeightAndStartsAtTheLeftEdge(t *testing.T) {
	t.Parallel()
	fonts := testFonts(t)
	for _, tc := range []struct {
		name     string
		size     float64
		cellH    int // 0: the layout's own
		cells    int
		heightFt bool // the cell height limits the icon
	}{
		{"15px 1 cell", 15, 0, 1, false},
		{"15px 2 cells", 15, 0, 2, false},
		{"30px 1 cell", 30, 0, 1, false},
		{"30px 2 cells", 30, 0, 2, false},
		{"15px short cell", 15, 10, 1, true},
		{"30px short cell", 30, 20, 2, true},
	} {
		r, _ := newRenderer(t, raster.Options{FontSize: tc.size})
		l := r.Layout()
		if tc.cellH != 0 {
			l.CellHeight = tc.cellH
		}
		for _, ru := range []rune{0xF015, 0xE725, 0xF0001} {
			o, ok, err := fonts.Outline(ru, false, false)
			if err != nil || !ok || !o.Icon {
				t.Fatalf("%s %U: outline ok=%v icon=%v err=%v", tc.name, ru, ok, o.Icon, err)
			}
			nMinX, nMinY, nMaxX, nMaxY, _ := bounds(o.Segments)
			native := l.FontSize / float64(o.UnitsPerEm)
			want := font.IconScale((nMaxY-nMinY)*native, float64(l.CellHeight))
			if (want < 1) != tc.heightFt {
				t.Fatalf("%s %U: test expects the cell height to limit the icon: %v", tc.name, ru, tc.heightFt)
			}
			scale := native * want

			d := newDefs()
			gd, err := d.glyph(fonts, l, ru, false, false, tc.cells)
			if err != nil || !gd.ok {
				t.Fatal(gd, err)
			}
			minX, minY, maxX, maxY := pathBox(t, &d, gd.id)
			const eps = 0.01
			if got, want := maxY-minY, (nMaxY-nMinY)*scale; math.Abs(got-want) > eps {
				t.Errorf("%s %U: ink height %.3f, want %.3f", tc.name, ru, got, want)
			}
			if got, want := maxY-minY, float64(l.CellHeight); got > want+eps {
				t.Errorf("%s %U: ink height %.3f exceeds the cell height %v", tc.name, ru, got, want)
			}
			if got, want := maxX-minX, (nMaxX-nMinX)*scale; math.Abs(got-want) > eps {
				t.Errorf("%s %U: ink width %.3f, want %.3f", tc.name, ru, got, want)
			}
			if got, want := minX, nMinX*scale; math.Abs(got-want) > eps {
				t.Errorf("%s %U: ink starts at x %.3f, want the left bearing %.3f from the cell's left edge", tc.name, ru, got, want)
			}
			if mid := (minY + maxY) / 2; math.Abs(mid-float64(l.CellHeight)/2) > 0.5+eps {
				t.Errorf("%s %U: ink centre y %.3f, want the cell centre %.1f", tc.name, ru, mid, float64(l.CellHeight)/2)
			}
			if tc.cells == 1 && !tc.heightFt && maxX <= float64(l.CellWidth) && ru == 0xF015 {
				t.Errorf("%s %U: ink ends at x %.3f within the %d px cell: not overflowing", tc.name, ru, maxX, l.CellWidth)
			}
		}
	}
	// The 15 px home icon is about 15 x 13 px in a 9 px wide cell.
	r, _ := newRenderer(t, raster.Options{FontSize: 15})
	d := newDefs()
	gd, _ := d.glyph(fonts, r.Layout(), 0xF015, false, false, 1)
	minX, minY, maxX, maxY := pathBox(t, &d, gd.id)
	if w, h := math.Round(maxX-minX), math.Round(maxY-minY); w != 15 || h != 13 {
		t.Errorf("home icon at 15 px is %v x %v, want 15 x 13", w, h)
	}
}

// Glyphs that are not icons keep the fit-to-cells rule: scaled to the cells
// and centred horizontally in them.
func TestNonIconFallbackOutlineIsStillCentredInItsCells(t *testing.T) {
	t.Parallel()
	fonts := testFonts(t)
	r, _ := newRenderer(t, raster.Options{FontSize: 15})
	l := r.Layout()
	for _, cells := range []int{1, 2} {
		d := newDefs()
		gd, err := d.glyph(fonts, l, 0x4E16, false, false, cells)
		if err != nil || !gd.ok {
			t.Fatal(gd, err)
		}
		minX, minY, maxX, maxY := pathBox(t, &d, gd.id)
		budget := float64(cells * l.CellWidth)
		if maxX-minX > budget+0.01 || maxY-minY > float64(l.CellHeight)+0.01 {
			t.Errorf("%d cells: ink %.2f x %.2f exceeds %v x %d", cells, maxX-minX, maxY-minY, budget, l.CellHeight)
		}
		if mid := (minX + maxX) / 2; math.Abs(mid-budget/2) > 0.5+0.01 {
			t.Errorf("%d cells: ink centre x %.2f, want %v", cells, mid, budget/2)
		}
		if mid := (minY + maxY) / 2; math.Abs(mid-float64(l.CellHeight)/2) > 0.5+0.01 {
			t.Errorf("%d cells: ink centre y %.2f, want %v", cells, mid, float64(l.CellHeight)/2)
		}
	}
}

// inkBoxOf returns the box of pixels that differ from the background in the
// band of pixel rows [y0, y1) and columns [x0, x1).
func inkBoxOf(img *image.RGBA, bg [4]uint8, x0, y0, x1, y1 int) image.Rectangle {
	var box image.Rectangle
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			i := img.PixOffset(x, y)
			if [4]uint8{img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3]} != bg {
				box = box.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	return box
}

// Icons in the paths mode land where the raster renderer puts them: followed
// directly by text, over a coloured neighbour's background, with a wide cell,
// and in the last column; and nothing clips them.
func TestSVGIconsMatchRasterRenderer(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		o    raster.Options
	}{
		{"default", raster.Options{}},
		{"15px", raster.Options{FontSize: 15}},
		{"scale 2", raster.Options{Scale: 2, FontSize: 12}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, g := newRenderer(t, tc.o)
			s := screen(t, 24,
				"\uf015home \ue725main \U000f0001md",
				"\uf07b\ue0b6\ue62b "+esc+"44m\uf015"+reset+esc+"41m  "+reset+"x",
				"a\uf015  "+esc+"42mabc"+reset,
				"          \uf015\ue725   \uf015\uf015")
			ref, err := r.Render(s)
			if err != nil {
				t.Fatal(err)
			}
			doc := write(t, Options{Geometry: g, Text: TextPaths, Frame: Frame{Kind: FrameLast}}, s.Cols, s.Rows, step(s, time.Second))
			if strings.Contains(doc, "clip") {
				t.Error("the document clips")
			}
			got := rasteriseSVG(t, doc, ref.Bounds().Dx(), ref.Bounds().Dy())
			mean, bad := pixelDiff(ref, got)
			t.Logf("%s: mean error %.5f, %.3f%% bad pixels", tc.name, mean, bad*100)
			if mean > 0.01 || bad > 0.02 {
				t.Errorf("SVG differs from the raster render: mean error %.5f, %.3f%% bad pixels", mean, bad*100)
			}
			// The first icon's ink box agrees with the raster within a pixel.
			lay := r.Layout()
			bg := [4]uint8{lay.Theme.Background.R, lay.Theme.Background.G, lay.Theme.Background.B, 255}
			x0 := lay.Pad
			y0 := lay.Pad
			x1 := x0 + lay.CellWidth
			y1 := y0 + lay.CellHeight
			a := inkBoxOf(ref, bg, x0, y0, x1, y1)
			b := inkBoxOf(got, bg, x0, y0, x1, y1)
			if a.Empty() || b.Empty() {
				t.Fatalf("no ink in the first cell: raster %v, svg %v", a, b)
			}
			abs := func(n int) int { return max(n, -n) }
			if abs(a.Min.X-b.Min.X) > 1 || abs(a.Min.Y-b.Min.Y) > 1 || abs(a.Max.X-b.Max.X) > 1 || abs(a.Max.Y-b.Max.Y) > 1 {
				t.Errorf("first icon cell ink: raster %v, svg %v", a, b)
			}
		})
	}
}

package sprite

import (
	"image"
	"image/color"
	"testing"
)

var (
	white = color.RGBA{255, 255, 255, 255}
	black = color.RGBA{0, 0, 0, 255}
)

// testSizes are cell sizes (width, height), including odd ones.
var testSizes = []image.Point{{10, 20}, {9, 20}, {10, 21}, {7, 13}, {8, 16}, {14, 28}, {12, 24}}

const margin = 3

// shot is a black opaque canvas with one cell (of size w x h, offset by
// margin) that sprites are drawn into.
type shot struct {
	img  *image.RGBA
	cell image.Rectangle
}

func newShot(w, h int) *shot {
	img := image.NewRGBA(image.Rect(0, 0, w+2*margin, h+2*margin))
	for i := 0; i < len(img.Pix); i += 4 {
		img.Pix[i+3] = 255
	}
	return &shot{img, image.Rect(margin, margin, margin+w, margin+h)}
}

// drawn returns a fresh shot with r drawn in white.
func drawn(t *testing.T, r rune, w, h int) *shot {
	t.Helper()
	s := newShot(w, h)
	if !Draw(s.img, r, s.cell, white) {
		t.Fatalf("Draw(%U) = false", r)
	}
	return s
}

func (s *shot) w() int { return s.cell.Dx() }
func (s *shot) h() int { return s.cell.Dy() }

// on reports whether the cell pixel (x, y) is inked (pure white on black).
func (s *shot) on(x, y int) bool {
	return s.img.RGBAAt(s.cell.Min.X+x, s.cell.Min.Y+y).R > 0
}

// value returns the red channel at cell pixel (x, y).
func (s *shot) value(x, y int) int {
	return int(s.img.RGBAAt(s.cell.Min.X+x, s.cell.Min.Y+y).R)
}

// count returns the inked pixels in the cell-relative rectangle.
func (s *shot) count(x0, y0, x1, y1 int) int {
	n := 0
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			if s.on(x, y) {
				n++
			}
		}
	}
	return n
}

func (s *shot) total() int { return s.count(0, 0, s.w(), s.h()) }

// row and col return which pixels of row y or column x are inked.
func (s *shot) row(y int) []bool {
	out := make([]bool, s.w())
	for x := range out {
		out[x] = s.on(x, y)
	}
	return out
}

func (s *shot) col(x int) []bool {
	out := make([]bool, s.h())
	for y := range out {
		out[y] = s.on(x, y)
	}
	return out
}

// bandSet marks the positions covered by the bands of weight wt in an axis
// of length n.
func bandSet(n int, wt weight, t int) []bool {
	out := make([]bool, n)
	if wt == none {
		return out
	}
	for _, b := range bands(n, wt, t) {
		for i := b.a; i < b.b; i++ {
			out[i] = true
		}
	}
	return out
}

func equalBools(a, b []bool) bool {
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

func runs(v []bool) int {
	n := 0
	for i, b := range v {
		if b && (i == 0 || !v[i-1]) {
			n++
		}
	}
	return n
}

func TestHandles(t *testing.T) {
	for _, r := range []rune{0x2500, 0x257F, 0x2580, 0x259F, 0x2800, 0x28FF, 0xE0B0, 0xE0B7} {
		if !Handles(r) {
			t.Errorf("Handles(%U) = false", r)
		}
	}
	for _, r := range []rune{'a', ' ', 0x24FF, 0x25A0, 0x27FF, 0x2900, 0xE0AF, 0xE0B8} {
		if Handles(r) {
			t.Errorf("Handles(%U) = true", r)
		}
	}
}

func TestDrawUnhandledLeavesImage(t *testing.T) {
	s := newShot(10, 20)
	before := append([]byte(nil), s.img.Pix...)
	for _, r := range []rune{'a', 0x24FF, 0x25A0, 0x2900, 0xE0B8} {
		if Draw(s.img, r, s.cell, white) {
			t.Errorf("Draw(%U) = true", r)
		}
	}
	if string(before) != string(s.img.Pix) {
		t.Error("unhandled runes changed the image")
	}
}

func TestAllRunesDrawInsideCell(t *testing.T) {
	var all []rune
	for r := rune(0x2500); r <= 0x259F; r++ {
		all = append(all, r)
	}
	for r := rune(0x2800); r <= 0x28FF; r++ {
		all = append(all, r)
	}
	for r := rune(0xE0B0); r <= 0xE0B7; r++ {
		all = append(all, r)
	}
	for _, sz := range append(testSizes, image.Pt(20, 20), image.Pt(1, 1), image.Pt(2, 3)) {
		for _, r := range all {
			s := newShot(sz.X, sz.Y)
			if !Draw(s.img, r, s.cell, white) {
				t.Fatalf("Draw(%U) = false at %v", r, sz)
			}
			// nothing outside the cell
			for y := 0; y < s.img.Bounds().Dy(); y++ {
				for x := 0; x < s.img.Bounds().Dx(); x++ {
					if !(image.Pt(x, y).In(s.cell)) && s.img.RGBAAt(x, y) != black {
						t.Fatalf("%U at %v drew outside the cell at (%d,%d)", r, sz, x, y)
					}
				}
			}
			if r != 0x2800 && sz.X >= 7 && s.total() == 0 {
				t.Errorf("%U at %v drew nothing", r, sz)
			}
		}
	}
}

func TestDrawClipsToImage(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 10, 10))
	for _, r := range []rune{'─', '█', '⣿', '\ue0b0', '╭', '╳'} {
		Draw(img, r, image.Rect(-3, -5, 7, 15), white)
		Draw(img, r, image.Rect(5, 5, 25, 25), white)
		Draw(img, r, image.Rect(20, 20, 30, 40), white)
		Draw(img, r, image.Rectangle{}, white)
		Draw(img, r, image.Rect(7, 7, 3, 3), white) // non-canonical
	}
}

func TestDeterministicAndIdempotent(t *testing.T) {
	for _, r := range []rune{'┼', '╬', '╭', '╳', '█', '▚', '⣿', '\ue0b0', '\ue0b5', '┄'} {
		a, b := drawn(t, r, 10, 20), drawn(t, r, 10, 20)
		if string(a.img.Pix) != string(b.img.Pix) {
			t.Errorf("%U: two draws differ", r)
		}
		Draw(a.img, r, a.cell, white)
		if string(a.img.Pix) != string(b.img.Pix) {
			t.Errorf("%U: drawing twice differs from once", r)
		}
	}
}

func TestLightAndHeavyLines(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		th := lineThickness(h)
		for _, tc := range []struct {
			r     rune
			thick int
		}{{'─', th}, {'━', 2 * th}} {
			s := drawn(t, tc.r, w, h)
			y0 := (h - tc.thick) / 2
			for y := 0; y < h; y++ {
				want := y >= y0 && y < y0+tc.thick
				for x := 0; x < w; x++ {
					if s.on(x, y) != want {
						t.Fatalf("%c %v: pixel (%d,%d) = %v, want %v", tc.r, sz, x, y, s.on(x, y), want)
					}
				}
			}
			if s.count(0, 0, w, 1) != 0 || s.count(0, h-1, w, h) != 0 {
				t.Errorf("%c %v: ink in top or bottom row", tc.r, sz)
			}
		}
		for _, tc := range []struct {
			r     rune
			thick int
		}{{'│', th}, {'┃', 2 * th}} {
			s := drawn(t, tc.r, w, h)
			x0 := (w - tc.thick) / 2
			for x := 0; x < w; x++ {
				want := x >= x0 && x < x0+tc.thick
				for y := 0; y < h; y++ {
					if s.on(x, y) != want {
						t.Fatalf("%c %v: pixel (%d,%d) = %v, want %v", tc.r, sz, x, y, s.on(x, y), want)
					}
				}
			}
		}
	}
}

func TestCrossTouchesAllEdges(t *testing.T) {
	for _, sz := range testSizes {
		for _, r := range []rune{'┼', '╋', '╬', '╪', '╫', '╂', '┿'} {
			s := drawn(t, r, sz.X, sz.Y)
			w, h := sz.X, sz.Y
			if s.count(0, 0, w, 1) == 0 || s.count(0, h-1, w, h) == 0 || s.count(0, 0, 1, h) == 0 || s.count(w-1, 0, w, h) == 0 {
				t.Errorf("%c %v does not touch all four edges", r, sz)
			}
		}
		s := drawn(t, '┼', sz.X, sz.Y)
		if !s.on(sz.X/2, 0) && !s.on(sz.X/2-1, 0) || !s.on(0, sz.Y/2) && !s.on(0, sz.Y/2-1) {
			t.Errorf("┼ %v: centre of the edges is not inked", sz)
		}
	}
}

// arcArms maps the rounded corners to the edges they reach.
var arcArms = map[rune]arms{
	'╭': {down: light, right: light},
	'╮': {down: light, left: light},
	'╯': {up: light, left: light},
	'╰': {up: light, right: light},
}

// TestArmsReachEdgesExactly checks, for every glyph made of arms and for the
// arcs, that the ink on each cell edge is exactly the stroke of that arm.
// That is what makes neighbouring cells join without a gap or a step.
func TestArmsReachEdgesExactly(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		th := lineThickness(h)
		for r := rune(0x2500); r <= 0x257F; r++ {
			a := boxArms[r-0x2500]
			if arc, ok := arcArms[r]; ok {
				a = arc
			} else if a == (arms{}) {
				continue // dashes, diagonals
			}
			s := drawn(t, r, w, h)
			for _, e := range []struct {
				name string
				got  []bool
				want []bool
			}{
				{"top", s.row(0), bandSet(w, a.up, th)},
				{"bottom", s.row(h - 1), bandSet(w, a.down, th)},
				{"left", s.col(0), bandSet(h, a.left, th)},
				{"right", s.col(w - 1), bandSet(h, a.right, th)},
			} {
				if !equalBools(e.got, e.want) {
					t.Errorf("%c (%U) %v %s edge = %v, want %v", r, r, sz, e.name, e.got, e.want)
				}
			}
		}
	}
}

func TestCornerFilled(t *testing.T) {
	for _, sz := range testSizes {
		th := lineThickness(sz.Y)
		// ┌: the corner square where the two strokes meet is solid.
		s := drawn(t, '┌', sz.X, sz.Y)
		x0, y0 := (sz.X-th)/2, (sz.Y-th)/2
		if got := s.count(x0, y0, x0+th, y0+th); got != th*th {
			t.Errorf("┌ %v: corner has %d of %d pixels", sz, got, th*th)
		}
		if s.count(0, 0, x0, sz.Y) != 0 || s.count(0, 0, sz.X, y0) != 0 {
			t.Errorf("┌ %v: ink left of or above the corner", sz)
		}
	}
}

func TestDoubleLines(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		th := lineThickness(h)
		s := drawn(t, '═', w, h)
		if got := runs(s.col(w / 2)); got != 2 {
			t.Errorf("═ %v: %d lines, want 2", sz, got)
		}
		s = drawn(t, '║', w, h)
		if got := runs(s.row(h / 2)); got != 2 {
			t.Errorf("║ %v: %d lines, want 2", sz, got)
		}
		// ╬: both horizontal lines are interrupted by the verticals.
		s = drawn(t, '╬', w, h)
		vb := bands(w, double, th)
		hb := bands(h, double, th)
		if s.on((vb[0].b+vb[1].a)/2, hb[0].a) {
			t.Errorf("╬ %v: upper line crosses the gap between the verticals", sz)
		}
		if !s.on(0, hb[0].a) || !s.on(w-1, hb[1].a) {
			t.Errorf("╬ %v: lines do not reach the cell edges", sz)
		}
		// ╔: the outer and inner corners are solid and the gap between the
		// lines stays open.
		s = drawn(t, '╔', w, h)
		if !s.on(vb[0].a, hb[0].a) {
			t.Errorf("╔ %v: outer corner missing", sz)
		}
		if s.on(vb[0].b, hb[0].b) {
			t.Errorf("╔ %v: pixel between the lines inked", sz)
		}
		if !s.on(vb[1].a, hb[1].a) {
			t.Errorf("╔ %v: inner corner missing", sz)
		}
	}
}

func TestDashedLines(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		for _, tc := range []struct {
			r rune
			n int
		}{{'┄', 3}, {'┅', 3}, {'┈', 4}, {'┉', 4}, {'╌', 2}, {'╍', 2}} {
			if tc.n == 4 && w < 9 {
				continue
			}
			s := newShot(2*w, h)
			Draw(s.img, tc.r, image.Rect(margin, margin, margin+w, margin+h), white)
			Draw(s.img, tc.r, image.Rect(margin+w, margin, margin+2*w, margin+h), white)
			row := s.row((h-lineThickness(h))/2 - 0)
			if got := runs(row); got != 2*tc.n {
				t.Errorf("%c %v: %d dashes over two cells, want %d", tc.r, sz, got, 2*tc.n)
			}
		}
		for _, tc := range []struct {
			r rune
			n int
		}{{'┆', 3}, {'┊', 4}, {'╎', 2}, {'╏', 2}} {
			s := newShot(w, 2*h)
			Draw(s.img, tc.r, image.Rect(margin, margin, margin+w, margin+h), white)
			Draw(s.img, tc.r, image.Rect(margin, margin+h, margin+w, margin+2*h), white)
			if got := runs(s.col((w - lineThickness(h)) / 2)); got != 2*tc.n {
				t.Errorf("%c %v: %d dashes over two cells, want %d", tc.r, sz, got, 2*tc.n)
			}
		}
	}
}

func TestDiagonals(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		slash := drawn(t, '╱', w, h)
		back := drawn(t, '╲', w, h)
		both := drawn(t, '╳', w, h)
		if !slash.on(w-1, 0) || !slash.on(0, h-1) || slash.on(0, 0) || slash.on(w-1, h-1) {
			t.Errorf("╱ %v: wrong corners", sz)
		}
		if !back.on(0, 0) || !back.on(w-1, h-1) || back.on(w-1, 0) || back.on(0, h-1) {
			t.Errorf("╲ %v: wrong corners", sz)
		}
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				if both.on(x, y) != (slash.on(x, y) || back.on(x, y)) {
					t.Fatalf("╳ %v: not the union at (%d,%d)", sz, x, y)
				}
			}
			if runs(slash.row(y)) != 1 {
				t.Errorf("╱ %v: row %d has %d runs, want 1", sz, y, runs(slash.row(y)))
			}
		}
	}
}

func TestBlockHalvesAndQuadrants(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		if got := drawn(t, '█', w, h).total(); got != w*h {
			t.Errorf("█ %v covers %d of %d", sz, got, w*h)
		}
		ym, xm := (h+1)/2, (w+1)/2
		top := drawn(t, '▀', w, h)
		if got := top.count(0, 0, w, ym); got != w*ym || top.total() != w*ym {
			t.Errorf("▀ %v: top half has %d, total %d, want %d", sz, got, top.total(), w*ym)
		}
		bottom := drawn(t, '▄', w, h)
		if got := bottom.count(0, ym, w, h); got != w*(h-ym) || bottom.total() != got {
			t.Errorf("▄ %v: wrong coverage", sz)
		}
		left, right := drawn(t, '▌', w, h), drawn(t, '▐', w, h)
		if left.count(0, 0, xm, h) != xm*h || left.total() != xm*h || right.count(xm, 0, w, h) != (w-xm)*h || right.total() != (w-xm)*h {
			t.Errorf("▌▐ %v: wrong coverage", sz)
		}
		// Quadrants partition the cell.
		sum := make([]int, w*h)
		for _, r := range []rune{'▘', '▝', '▖', '▗'} {
			s := drawn(t, r, w, h)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if s.on(x, y) {
						sum[y*w+x]++
					}
				}
			}
		}
		for i, v := range sum {
			if v != 1 {
				t.Fatalf("quadrants %v: pixel %d covered %d times", sz, i, v)
			}
		}
		// Three-quadrant glyphs are the full cell minus the missing one.
		for r, missing := range map[rune]rune{'▙': '▝', '▛': '▗', '▜': '▖', '▟': '▘'} {
			s, m := drawn(t, r, w, h), drawn(t, missing, w, h)
			for y := 0; y < h; y++ {
				for x := 0; x < w; x++ {
					if s.on(x, y) == m.on(x, y) {
						t.Fatalf("%c %v: pixel (%d,%d) not complementary to %c", r, sz, x, y, missing)
					}
				}
			}
		}
		// ▚ is ▘ + ▗, ▞ is ▝ + ▖.
		for r, parts := range map[rune][2]rune{'▚': {'▘', '▗'}, '▞': {'▝', '▖'}} {
			s, p, q := drawn(t, r, w, h), drawn(t, parts[0], w, h), drawn(t, parts[1], w, h)
			if s.total() != p.total()+q.total() {
				t.Errorf("%c %v: coverage %d, want %d", r, sz, s.total(), p.total()+q.total())
			}
		}
	}
}

func TestBlockEighths(t *testing.T) {
	w, h := 8, 16
	for n := 1; n <= 8; n++ {
		lower := drawn(t, rune(0x2580+n), w, h)
		if got, want := lower.total(), w*h*n/8; got != want {
			t.Errorf("lower %d/8: %d pixels, want %d", n, got, want)
		}
		if n < 8 && lower.on(0, h-1-h*n/8) {
			t.Errorf("lower %d/8 reaches too high", n)
		}
	}
	for n := 7; n >= 1; n-- {
		left := drawn(t, rune(0x2590-n), w, h)
		if got, want := left.total(), w*h*n/8; got != want {
			t.Errorf("left %d/8: %d pixels, want %d", n, got, want)
		}
	}
	if got := drawn(t, '▔', w, h).total(); got != w*2 {
		t.Errorf("▔: %d pixels, want %d", got, w*2)
	}
	if got := drawn(t, '▕', w, h).total(); got != h {
		t.Errorf("▕: %d pixels, want %d", got, h)
	}
	// Every size gets at least one pixel for the thinnest eighth.
	for _, sz := range testSizes {
		for _, r := range []rune{'▁', '▏', '▔', '▕'} {
			if drawn(t, r, sz.X, sz.Y).total() == 0 {
				t.Errorf("%c %v is empty", r, sz)
			}
		}
	}
}

func TestShadesCoverage(t *testing.T) {
	for _, sz := range testSizes {
		for r, want := range map[rune]float64{'░': 0.25, '▒': 0.5, '▓': 0.75} {
			s := drawn(t, r, sz.X, sz.Y)
			sum := 0
			for y := 0; y < sz.Y; y++ {
				for x := 0; x < sz.X; x++ {
					sum += s.value(x, y)
				}
			}
			got := float64(sum) / float64(255*sz.X*sz.Y)
			if got < want-0.01 || got > want+0.01 {
				t.Errorf("%c %v: mean coverage %.3f, want %.2f", r, sz, got, want)
			}
		}
	}
}

func TestShadeBlendsOverExistingPixels(t *testing.T) {
	s := newShot(10, 20)
	bg := color.RGBA{200, 100, 0, 255}
	for y := 0; y < s.img.Bounds().Dy(); y++ {
		for x := 0; x < s.img.Bounds().Dx(); x++ {
			s.img.SetRGBA(x, y, bg)
		}
	}
	Draw(s.img, '▒', s.cell, color.RGBA{0, 0, 100, 255})
	got := s.img.RGBAAt(s.cell.Min.X+4, s.cell.Min.Y+9)
	want := color.RGBA{100, 50, 50, 255}
	for i, d := range []int{int(got.R) - int(want.R), int(got.G) - int(want.G), int(got.B) - int(want.B), int(got.A) - 255} {
		if d < -1 || d > 1 {
			t.Fatalf("blend = %v, want about %v (channel %d)", got, want, i)
		}
	}
	if s.img.RGBAAt(0, 0) != bg {
		t.Error("pixel outside the cell changed")
	}
}

// blobs returns the 4-connected inked components of s as bounding boxes.
func blobs(s *shot) []image.Rectangle {
	seen := make([]bool, s.w()*s.h())
	var out []image.Rectangle
	for y := 0; y < s.h(); y++ {
		for x := 0; x < s.w(); x++ {
			if !s.on(x, y) || seen[y*s.w()+x] {
				continue
			}
			bb := image.Rect(x, y, x+1, y+1)
			stack := []image.Point{{x, y}}
			seen[y*s.w()+x] = true
			for len(stack) > 0 {
				p := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				bb = bb.Union(image.Rect(p.X, p.Y, p.X+1, p.Y+1))
				for _, d := range []image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					q := p.Add(d)
					if q.X < 0 || q.Y < 0 || q.X >= s.w() || q.Y >= s.h() || !s.on(q.X, q.Y) || seen[q.Y*s.w()+q.X] {
						continue
					}
					seen[q.Y*s.w()+q.X] = true
					stack = append(stack, q)
				}
			}
			out = append(out, bb)
		}
	}
	return out
}

func TestBrailleSingleDotsLandInTheirSlot(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		for b, d := range brailleDots {
			s := drawn(t, rune(0x2800+1<<b), w, h)
			bb := blobs(s)
			if len(bb) != 1 {
				t.Fatalf("dot %d at %v: %d blobs, want 1", b+1, sz, len(bb))
			}
			slot := image.Rect(d[0]*w/2, d[1]*h/4, (d[0]+1)*w/2+1, ((d[1]+1)*h+3)/4)
			if !bb[0].In(slot) {
				t.Errorf("dot %d at %v: blob %v outside slot %v", b+1, sz, bb[0], slot)
			}
		}
	}
	// ⠁ is a single dot wholly in the top-left quarter-height / half-width.
	s := drawn(t, '⠁', 10, 20)
	if s.total() == 0 || s.count(5, 0, 10, 20) != 0 || s.count(0, 5, 10, 20) != 0 {
		t.Errorf("⠁: dot not confined to the top-left region")
	}
}

func TestBrailleDotCount(t *testing.T) {
	pop := func(v int) int {
		n := 0
		for ; v != 0; v &= v - 1 {
			n++
		}
		return n
	}
	for _, sz := range testSizes {
		for v := 0; v < 256; v++ {
			s := drawn(t, rune(0x2800+v), sz.X, sz.Y)
			if got := len(blobs(s)); got != pop(v) {
				t.Fatalf("%U at %v: %d dots, want %d", rune(0x2800+v), sz, got, pop(v))
			}
		}
	}
	if drawn(t, '⠀', 10, 20).total() != 0 {
		t.Error("blank braille inked pixels")
	}
	// The dot radius scales with the cell.
	small, big := drawn(t, '⠁', 10, 20).total(), drawn(t, '⠁', 20, 40).total()
	if big <= 2*small {
		t.Errorf("dot area %d at 10x20, %d at 20x40: radius does not scale", small, big)
	}
}

func TestPowerlineTriangle(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		s := drawn(t, '\ue0b0', w, h)
		for y := 0; y < h; y++ {
			if !s.on(0, y) {
				t.Fatalf("%v: left edge not full at row %d", sz, y)
			}
		}
		width := func(y int) int { return s.count(0, y, w, y+1) }
		for y := 1; y < h/2; y++ {
			if width(y) < width(y-1) {
				t.Errorf("%v: width shrinks towards the middle at row %d", sz, y)
			}
		}
		for y := 0; y < h; y++ {
			if width(y) != width(h-1-y) {
				t.Errorf("%v: rows %d and %d are not symmetric", sz, y, h-1-y)
			}
		}
		if width(0) >= w/2 || width(h/2) != w || !s.on(w-1, h/2) || s.on(w-1, 0) {
			t.Errorf("%v: triangle does not taper from %d to %d", sz, width(0), width(h/2))
		}
	}
}

func TestPowerlineMirroredAndOutline(t *testing.T) {
	for _, sz := range testSizes {
		for _, pair := range [][2]rune{{0xE0B0, 0xE0B2}, {0xE0B1, 0xE0B3}, {0xE0B4, 0xE0B6}, {0xE0B5, 0xE0B7}} {
			a, b := drawn(t, pair[0], sz.X, sz.Y), drawn(t, pair[1], sz.X, sz.Y)
			for y := 0; y < sz.Y; y++ {
				for x := 0; x < sz.X; x++ {
					if a.on(x, y) != b.on(sz.X-1-x, y) {
						t.Fatalf("%U/%U %v: not mirrored at (%d,%d)", pair[0], pair[1], sz, x, y)
					}
				}
			}
		}
		solid, line := drawn(t, 0xE0B0, sz.X, sz.Y), drawn(t, 0xE0B1, sz.X, sz.Y)
		if line.total() >= solid.total()/2 || line.total() == 0 {
			t.Errorf("%v: chevron has %d pixels, triangle %d", sz, line.total(), solid.total())
		}
		if !line.on(0, 0) || !line.on(0, sz.Y-1) || line.on(0, sz.Y/2) || !line.on(sz.X-1, sz.Y/2) {
			t.Errorf("%v: chevron corners wrong", sz)
		}
		disc, ring := drawn(t, 0xE0B4, sz.X, sz.Y), drawn(t, 0xE0B5, sz.X, sz.Y)
		if frac := float64(disc.total()) / float64(sz.X*sz.Y); frac < 0.7 || frac > 0.9 {
			t.Errorf("%v: half disc covers %.2f of the cell", sz, frac)
		}
		for y := 0; y < sz.Y; y++ {
			if !disc.on(0, y) {
				t.Fatalf("%v: half disc left edge not full at row %d", sz, y)
			}
		}
		if ring.total() == 0 || ring.total() >= disc.total()/2 || ring.on(1, sz.Y/2) && sz.Y >= 20 {
			t.Errorf("%v: ring has %d pixels, disc %d", sz, ring.total(), disc.total())
		}
		if !ring.on(sz.X-1, sz.Y/2) && !ring.on(sz.X-1, sz.Y/2-1) {
			t.Errorf("%v: ring does not reach the right edge", sz)
		}
	}
}

func TestAdjacentCellsJoinWithoutSeam(t *testing.T) {
	for _, sz := range testSizes {
		w, h := sz.X, sz.Y
		th := lineThickness(h)
		for _, r := range []rune{'─', '━', '═', '┼', '╋', '╪', '┬', '┴'} {
			s := newShot(2*w, h)
			Draw(s.img, r, image.Rect(margin, margin, margin+w, margin+h), white)
			Draw(s.img, r, image.Rect(margin+w, margin, margin+2*w, margin+h), white)
			a := boxArms[r-0x2500]
			for y, want := range bandSet(h, max(a.left, a.right), th) {
				if !want {
					continue
				}
				for x := 0; x < 2*w; x++ {
					if !s.on(x, y) {
						t.Fatalf("%c %v: gap at (%d,%d)", r, sz, x, y)
					}
				}
			}
		}
		for _, r := range []rune{'│', '┃', '║', '├', '┤'} {
			s := newShot(w, 2*h)
			Draw(s.img, r, image.Rect(margin, margin, margin+w, margin+h), white)
			Draw(s.img, r, image.Rect(margin, margin+h, margin+w, margin+2*h), white)
			a := boxArms[r-0x2500]
			for x, want := range bandSet(w, max(a.up, a.down), th) {
				if !want {
					continue
				}
				for y := 0; y < 2*h; y++ {
					if !s.on(x, y) {
						t.Fatalf("%c %v: gap at (%d,%d)", r, sz, x, y)
					}
				}
			}
		}
	}
}

func TestWideCell(t *testing.T) {
	const w, h = 10, 20
	cell := func() *shot {
		s := newShot(2*w, h)
		return s
	}
	s := cell()
	Draw(s.img, '█', s.cell, white)
	if s.total() != 2*w*h {
		t.Errorf("█ in a wide cell covers %d, want %d", s.total(), 2*w*h)
	}
	s = cell()
	Draw(s.img, '─', s.cell, white)
	y := (h - lineThickness(h)) / 2
	for x := 0; x < 2*w; x++ {
		if !s.on(x, y) {
			t.Fatalf("─ in a wide cell has a gap at x=%d", x)
		}
	}
	s = cell()
	Draw(s.img, '⠁', s.cell, white)
	bb := blobs(s)
	if len(bb) != 1 || !bb[0].In(image.Rect(0, 0, w, h/2)) {
		t.Errorf("⠁ in a wide cell: blobs %v, want one in the top-left", bb)
	}
	s = cell()
	Draw(s.img, 0xE0B0, s.cell, white)
	if !s.on(2*w-1, h/2) || s.on(2*w-1, 0) || s.count(0, 0, 1, h) != h {
		t.Error("powerline triangle does not span the wide cell")
	}
}

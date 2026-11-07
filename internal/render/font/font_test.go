package font

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

func mustDefault(t *testing.T) *Set {
	t.Helper()
	s, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	return s
}

func TestDefaultLoads(t *testing.T) {
	s := mustDefault(t)
	for _, st := range []struct{ bold, italic bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		if _, err := s.Face(st.bold, st.italic, 14); err != nil {
			t.Errorf("Face(bold=%v, italic=%v): %v", st.bold, st.italic, err)
		}
	}
}

func TestMetricsGolden(t *testing.T) {
	// JetBrains Mono: advance 0.6 em, ascent 1.02 em, descent 0.3 em, no gap.
	for _, tc := range []struct {
		size float64
		want Metrics
	}{
		// 8.4 px advance rounds to 8, so the em shrinks to 8/0.6.
		{14, Metrics{CellWidth: 8, CellHeight: 18, Ascent: 14, Descent: 4, EmPx: 8 / 0.6}},
		// 0.6 em is already a whole number of pixels: nothing changes.
		{15, Metrics{CellWidth: 9, CellHeight: 20, Ascent: 15, Descent: 5, EmPx: 15}},
		// 9.6 px advance rounds up to 10, so the em grows to 10/0.6.
		{16, Metrics{CellWidth: 10, CellHeight: 22, Ascent: 17, Descent: 5, EmPx: 10 / 0.6}},
	} {
		m, err := mustDefault(t).Metrics(tc.size)
		if err != nil {
			t.Fatal(err)
		}
		if m.CellWidth != tc.want.CellWidth || m.CellHeight != tc.want.CellHeight ||
			m.Ascent != tc.want.Ascent || m.Descent != tc.want.Descent || math.Abs(m.EmPx-tc.want.EmPx) > 1e-9 {
			t.Errorf("Metrics(%v) = %+v, want %+v", tc.size, m, tc.want)
		}
	}
}

// The cell width is the glyph advance: at EmPx an 'M' advances CellWidth to
// within the 1/64 px resolution of face advances, and EmPx stays within half
// a pixel of advance of the requested size.
func TestMetricsPitchMatchesAdvance(t *testing.T) {
	s := mustDefault(t)
	for size := 6.0; size <= 48; size += 0.5 {
		m, err := s.Metrics(size)
		if err != nil {
			t.Fatal(err)
		}
		face, _ := s.Face(false, false, m.EmPx)
		adv, _ := face.GlyphAdvance('M')
		if d := math.Abs(float64(adv)/64 - float64(m.CellWidth)); d > 0.05 {
			t.Errorf("Metrics(%v): advance %.3f px, cell width %d", size, float64(adv)/64, m.CellWidth)
		}
		if d := math.Abs(m.EmPx-size) * 0.6; d > 0.5+1e-9 && m.CellWidth > 1 {
			t.Errorf("Metrics(%v): EmPx %.3f moves the advance by %.2f px", size, m.EmPx, d)
		}
	}
}

func TestMetricsMonotonicAndStable(t *testing.T) {
	s := mustDefault(t)
	var prev Metrics
	for _, size := range []float64{8, 10, 12, 14, 16, 20, 24, 32, 48} {
		m, err := s.Metrics(size)
		if err != nil {
			t.Fatal(err)
		}
		again, _ := s.Metrics(size)
		if m != again {
			t.Errorf("Metrics(%v) unstable: %+v vs %+v", size, m, again)
		}
		if m.CellHeight != m.Ascent+m.Descent {
			t.Errorf("Metrics(%v): height %d != ascent+descent", size, m.CellHeight)
		}
		if m.CellWidth < prev.CellWidth || m.CellHeight < prev.CellHeight ||
			m.Ascent < prev.Ascent || m.Descent < prev.Descent {
			t.Errorf("Metrics(%v) = %+v not monotonic after %+v", size, m, prev)
		}
		prev = m
	}
	small, _ := s.Metrics(8)
	large, _ := s.Metrics(48)
	if large.CellWidth <= small.CellWidth || large.CellHeight <= small.CellHeight {
		t.Errorf("metrics did not grow: %+v -> %+v", small, large)
	}
}

func TestMetricsInvalidSize(t *testing.T) {
	s := mustDefault(t)
	for _, size := range []float64{0, -1} {
		if _, err := s.Metrics(size); err == nil {
			t.Errorf("Metrics(%v) succeeded, want error", size)
		}
	}
}

func TestFaceCached(t *testing.T) {
	s := mustDefault(t)
	a, _ := s.Face(true, false, 14)
	b, _ := s.Face(true, false, 14)
	c, _ := s.Face(true, false, 15)
	if a != b {
		t.Error("same (style,size) returned different faces")
	}
	if a == c {
		t.Error("different sizes returned same face")
	}
}

func renderA(t *testing.T, s *Set, bold bool) *image.Alpha {
	t.Helper()
	face, err := s.Face(bold, false, 32)
	if err != nil {
		t.Fatal(err)
	}
	dr, mask, maskp, _, ok := face.Glyph(fixed.P(0, 32), 'A')
	if !ok {
		t.Fatal("no glyph for A")
	}
	out := image.NewAlpha(dr)
	draw.DrawMask(out, dr, image.NewUniform(color.Alpha{A: 255}), image.Point{}, mask, maskp, draw.Src)
	return out
}

func TestBoldDiffersFromRegular(t *testing.T) {
	s := mustDefault(t)
	reg, bold := renderA(t, s, false), renderA(t, s, true)
	if bytes.Equal(reg.Pix, bold.Pix) {
		t.Error("bold 'A' is pixel-identical to regular 'A'")
	}
}

func TestHasGlyph(t *testing.T) {
	s := mustDefault(t)
	if !s.HasGlyph('A') {
		t.Error("HasGlyph('A') = false, want true")
	}
	if s.HasGlyph('世') {
		t.Error("HasGlyph('世') = true, want false for JetBrains Mono")
	}
}

func TestLoadGarbageErrors(t *testing.T) {
	good := jetBrainsMonoRegular
	garbage := []byte("not a font")
	if _, err := Load(garbage, good, good, good); err == nil {
		t.Error("Load with garbage regular succeeded")
	}
	if _, err := Load(good, good, good, garbage); err == nil {
		t.Error("Load with garbage bold italic succeeded")
	}
	if _, err := Load(nil, nil, nil, nil); err == nil {
		t.Error("Load with nil data succeeded")
	}
}

func TestLoadCustom(t *testing.T) {
	g := jetBrainsMonoRegular
	s, err := Load(g, g, g, g)
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasGlyph('A') {
		t.Error("custom set lacks 'A'")
	}
}

// nerdRunes are Nerd Font icons JetBrains Mono lacks: Devicons git branch,
// Font Awesome home, and a Material Design icon in the supplementary private
// use area. (JetBrains Mono itself draws the Powerline branch U+E0A0.)
var nerdRunes = []rune{0xE725, 0xF015, 0xF0001}

func TestFaceForNerdGlyphFallsBack(t *testing.T) {
	s := mustDefault(t)
	primary, err := s.Face(false, false, 14)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range nerdRunes {
		if s.HasGlyph(r) {
			t.Errorf("primary HasGlyph(%U) = true, want false", r)
		}
		if _, ok := primary.GlyphAdvance(r); ok {
			t.Errorf("primary face has glyph %U", r)
		}
		face, ok, err := s.FaceFor(r, false, false, 14)
		if err != nil || !ok {
			t.Fatalf("FaceFor(%U) = ok %v, err %v; want a fallback face", r, ok, err)
		}
		if face == primary {
			t.Errorf("FaceFor(%U) returned the primary face", r)
		}
		if _, ok := face.GlyphAdvance(r); !ok {
			t.Errorf("fallback face for %U has no glyph", r)
		}
	}
}

func TestFaceForPrefersPrimaryPerStyle(t *testing.T) {
	s := mustDefault(t)
	for _, st := range []struct{ bold, italic bool }{{false, false}, {true, false}, {false, true}, {true, true}} {
		want, _ := s.Face(st.bold, st.italic, 14)
		got, ok, err := s.FaceFor('A', st.bold, st.italic, 14)
		if err != nil || !ok || got != want {
			t.Errorf("FaceFor('A', bold=%v, italic=%v) = %v, %v, %v; want the primary face", st.bold, st.italic, got, ok, err)
		}
	}
}

// Runes the primary family already draws never reach the fallback.
func TestFaceForPowerlineBranchStaysOnPrimary(t *testing.T) {
	s := mustDefault(t)
	primary, _ := s.Face(false, false, 14)
	got, ok, err := s.FaceFor(0xE0A0, false, false, 14)
	if err != nil || !ok || got != primary {
		t.Errorf("FaceFor(U+E0A0) = %v, %v, %v; want the primary face", got, ok, err)
	}
}

func TestFaceForStyledRequestUsesRegularSymbols(t *testing.T) {
	s := mustDefault(t)
	reg, ok, err := s.FaceFor(0xF015, false, false, 14)
	if err != nil || !ok {
		t.Fatalf("regular: ok %v, err %v", ok, err)
	}
	for _, st := range []struct{ bold, italic bool }{{true, false}, {false, true}, {true, true}} {
		got, ok, err := s.FaceFor(0xF015, st.bold, st.italic, 14)
		if err != nil || !ok || got != reg {
			t.Errorf("FaceFor(bold=%v, italic=%v) = %v, %v, %v; want the regular symbols face", st.bold, st.italic, got, ok, err)
		}
	}
}

func TestFaceForMissingEverywhere(t *testing.T) {
	s := mustDefault(t)
	for _, r := range []rune{'世', 0x10FFFE} {
		face, ok, err := s.FaceFor(r, false, false, 14)
		if err != nil || ok || face != nil {
			t.Errorf("FaceFor(%U) = %v, %v, %v; want nil, false, nil", r, face, ok, err)
		}
	}
}

func TestFaceForCachedPerSize(t *testing.T) {
	s := mustDefault(t)
	a, _, _ := s.FaceFor(0xE725, false, false, 14)
	b, _, _ := s.FaceFor(0xF015, true, true, 14)
	c, _, _ := s.FaceFor(0xE725, false, false, 20)
	if a != b {
		t.Error("same fallback font and size returned different faces")
	}
	if a == c {
		t.Error("different sizes returned the same fallback face")
	}
}

func TestFaceForInvalidSize(t *testing.T) {
	s := mustDefault(t)
	for _, size := range []float64{0, -1} {
		if _, _, err := s.FaceFor('A', false, false, size); err == nil {
			t.Errorf("FaceFor size %v succeeded, want error", size)
		}
	}
}

func TestLoadWithoutFallbacks(t *testing.T) {
	g := jetBrainsMonoRegular
	s, err := Load(g, g, g, g)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.FaceFor(0xE725, false, false, 14); ok || err != nil {
		t.Errorf("FaceFor without fallbacks = ok %v, err %v; want false, nil", ok, err)
	}
}

func TestLoadFallbackGarbageAndChain(t *testing.T) {
	g := jetBrainsMonoRegular
	if _, err := Load(g, g, g, g, FallbackFont{Name: "bad", Data: []byte("nope")}); err == nil {
		t.Error("Load with garbage fallback succeeded")
	}
	// The symbols font, listed after another fallback that lacks the glyph,
	// is still reached.
	s, err := Load(g, g, g, g, FallbackFont{Name: "jb", Data: g}, FallbackFont{Name: "nerd", Data: symbolsNerdFontMono})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FaceFor(0xF015, false, false, 14); !ok {
		t.Error("second fallback not consulted")
	}
}

func TestMetricsUnaffectedByFallbacks(t *testing.T) {
	g := jetBrainsMonoRegular
	plain, _ := Load(g, g, g, g)
	withFB := mustDefault(t)
	a, _ := plain.Metrics(14)
	b, _ := withFB.Metrics(14)
	if a != b {
		t.Errorf("fallbacks changed metrics: %+v vs %+v", a, b)
	}
}

// FitFace scales a fallback glyph so its advance equals the cell span and
// its ink box lies within the span and the cell height.
func TestFitFaceBoundsWithinBudget(t *testing.T) {
	s := mustDefault(t)
	for _, basePx := range []float64{14, 20, 28} {
		m, err := s.Metrics(basePx)
		if err != nil {
			t.Fatal(err)
		}
		for _, cells := range []int{1, 2} {
			for _, r := range []rune{0xF015, 0xE725, 0xF0001} {
				face, ok, err := s.FitFace(r, false, false, basePx, m.CellWidth, m.CellHeight, cells)
				if err != nil || !ok {
					t.Fatalf("FitFace(%U): ok=%v err=%v", r, ok, err)
				}
				b, adv, ok := face.GlyphBounds(r)
				if !ok {
					t.Fatalf("%U: no bounds", r)
				}
				budget := fixed.I(cells * m.CellWidth)
				if adv > budget {
					t.Errorf("%v px %d cells %U: advance %v exceeds %v", basePx, cells, r, adv, budget)
				}
				if w := b.Max.X - b.Min.X; w > budget {
					t.Errorf("%v px %d cells %U: ink width %v exceeds %v", basePx, cells, r, w, budget)
				}
				if h := b.Max.Y - b.Min.Y; h > fixed.I(m.CellHeight) {
					t.Errorf("%v px %d cells %U: ink height %v exceeds cell height %d", basePx, cells, r, h, m.CellHeight)
				}
				// 1-cell icons are fitted by width: the advance fills the cell
				// to within the 1/64 px size quantisation.
				if cells == 1 && budget-adv > fixed.Int26_6(64)/8 {
					t.Errorf("%v px %U: advance %v is far below the %v cell", basePx, r, adv, budget)
				}
			}
		}
	}
}

func TestFitFaceCachesFaces(t *testing.T) {
	s := mustDefault(t)
	a, _, err := s.FitFace(0xF015, false, false, 14, 9, 20, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Bold/italic requests and other icons fitted to the same size share the face.
	b, _, _ := s.FitFace(0xF015, true, true, 14, 9, 20, 1)
	if a != b {
		t.Error("FitFace returned different faces for the same fit")
	}
	wide, _, _ := s.FitFace(0xF015, false, false, 14, 9, 20, 2)
	if a == wide {
		t.Error("1-cell and 2-cell fits share a face")
	}
}

// Runes the primary family has are returned unscaled, exactly like FaceFor.
func TestFitFacePrimaryRuneUnscaled(t *testing.T) {
	s := mustDefault(t)
	want, ok, err := s.FaceFor('a', false, false, 14)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	got, ok, err := s.FitFace('a', false, false, 14, 9, 20, 1)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	if got != want {
		t.Error("FitFace scaled or replaced a primary-font face")
	}
}

func TestFitFaceMissingAndInvalid(t *testing.T) {
	s := mustDefault(t)
	if _, ok, err := s.FitFace(0x10FFFE, false, false, 14, 9, 20, 1); ok || err != nil {
		t.Errorf("missing rune: ok=%v err=%v, want false, nil", ok, err)
	}
	for _, c := range [][4]int{{0, 20, 1, 14}, {9, 0, 1, 14}, {9, 20, 0, 14}, {9, 20, 1, 0}} {
		if _, _, err := s.FitFace(0xF015, false, false, float64(c[3]), c[0], c[1], c[2]); err == nil {
			t.Errorf("FitFace(%v): no error", c)
		}
	}
}

// Mixed regular/bold/italic text shares one cell grid, so every style of the
// family must advance the same for every printable ASCII glyph.
func TestStylesShareAdvance(t *testing.T) {
	s := mustDefault(t)
	for _, size := range []float64{14, 20} {
		ref, _ := s.Face(false, false, size)
		for _, st := range []struct{ bold, italic bool }{{true, false}, {false, true}, {true, true}} {
			f, err := s.Face(st.bold, st.italic, size)
			if err != nil {
				t.Fatal(err)
			}
			for r := rune('!'); r <= '~'; r++ {
				want, _ := ref.GlyphAdvance(r)
				if got, _ := f.GlyphAdvance(r); got != want {
					t.Fatalf("size %v bold=%v italic=%v: advance of %q = %v, regular %v", size, st.bold, st.italic, r, got, want)
				}
			}
		}
	}
}

// gridFit tests: the fitted outline has its x-height, cap height, baseline
// and descender edges on whole pixel rows, round overshoot flattened.

func TestGridFitSnapsHeights(t *testing.T) {
	s := mustDefault(t)
	for _, size := range []float64{11, 13.333, 15, 16.667, 20, 26.667} {
		f, err := s.Face(false, false, size)
		if err != nil {
			t.Fatal(err)
		}
		whole := func(name string, v fixed.Int26_6) {
			if v&63 != 0 {
				t.Errorf("size %v: %s = %v px, want a whole pixel", size, name, float64(v)/64)
			}
		}
		top := func(r rune) fixed.Int26_6 { b, _, _ := f.GlyphBounds(r); return b.Min.Y }
		bottom := func(r rune) fixed.Int26_6 { b, _, _ := f.GlyphBounds(r); return b.Max.Y }
		whole("top of x", top('x'))
		whole("top of o", top('o'))
		whole("top of H", top('H'))
		whole("bottom of x", bottom('x'))
		whole("bottom of o", bottom('o'))
		whole("bottom of p", bottom('p'))
		if top('o') != top('x') {
			t.Errorf("size %v: o overshoots x-height: %v vs %v", size, top('o'), top('x'))
		}
	}
}

// Grid fitting moves outlines vertically only: advances, and the x extent of
// a glyph, match the unfitted face.
func TestGridFitKeepsHorizontalMetrics(t *testing.T) {
	fitted := mustDefault(t)
	plain := mustDefault(t)
	plain.SetGridFit(false)
	a, _ := fitted.Face(false, false, 15)
	b, _ := plain.Face(false, false, 15)
	for r := rune('!'); r <= '~'; r++ {
		ba, adva, _ := a.GlyphBounds(r)
		bb, advb, _ := b.GlyphBounds(r)
		if adva != advb || ba.Min.X != bb.Min.X || ba.Max.X != bb.Max.X {
			t.Errorf("%q: fitted x extent/advance %v %v, plain %v %v", r, ba, adva, bb, advb)
		}
	}
}

// The fitted mask must not leak between glyphs: drawing the same glyph twice
// (with another between) yields identical coverage, and the top row of an
// 'x' is fully covered where the unfitted face leaves a partial row.
func TestGridFitMask(t *testing.T) {
	s := mustDefault(t)
	f, _ := s.Face(false, false, 15)
	coverage := func(r rune) []uint8 {
		_, mask, _, _, ok := f.Glyph(fixed.P(0, 20), r)
		if !ok {
			t.Fatalf("no glyph for %q", r)
		}
		a := mask.(*image.Alpha)
		return append([]uint8(nil), a.Pix...)
	}
	first := coverage('x')
	coverage('W')
	second := coverage('x')
	if !bytes.Equal(first, second) {
		t.Error("mask of x changed between calls")
	}
	dr, mask, _, _, _ := f.Glyph(fixed.P(0, 20), 'x')
	a := mask.(*image.Alpha)
	maxTop := uint8(0)
	for x := 0; x < dr.Dx(); x++ {
		maxTop = max(maxTop, a.Pix[x])
	}
	if maxTop < 200 {
		t.Errorf("top row of fitted x has max coverage %d, want a crisp edge", maxTop)
	}
	if _, ok := mustGlyphOK(f, 0x10FFFF); ok {
		t.Error("glyph reported for an unmapped rune")
	}
}

func mustGlyphOK(f xfont.Face, r rune) (image.Rectangle, bool) {
	dr, _, _, _, ok := f.Glyph(fixed.P(0, 20), r)
	return dr, ok
}

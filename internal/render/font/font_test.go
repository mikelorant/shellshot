package font

import (
	"bytes"
	"image"
	"image/color"
	"image/draw"
	"testing"

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

func TestMetricsGolden14px(t *testing.T) {
	m, err := mustDefault(t).Metrics(14)
	if err != nil {
		t.Fatal(err)
	}
	want := Metrics{CellWidth: 9, CellHeight: 20, Ascent: 15, Descent: 5}
	if m != want {
		t.Errorf("Metrics(14) = %+v, want %+v", m, want)
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

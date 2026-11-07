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

package font

import (
	"bytes"
	"encoding/binary"
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
	for _, r := range []rune{0x10FFFE, 0x1F600} {
		face, ok, err := s.FaceFor(r, false, false, 14)
		if err != nil || ok || face != nil {
			t.Errorf("FaceFor(%U) = %v, %v, %v; want nil, false, nil", r, face, ok, err)
		}
	}
}

// The embedded CJK subset covers hanzi, Hangul, kana and CJK punctuation.
func TestFaceForCJKFallsBack(t *testing.T) {
	s := mustDefault(t)
	for _, r := range []rune{'世', '界', '한', '글', 'こ', 'ん', 'カ', '、', '，'} {
		if s.HasGlyph(r) {
			t.Errorf("primary HasGlyph(%U) = true, want false", r)
		}
		face, ok, err := s.FaceFor(r, false, false, 14)
		if err != nil || !ok {
			t.Errorf("FaceFor(%U) = ok %v, err %v; want the CJK face", r, ok, err)
			continue
		}
		if _, ok := face.GlyphAdvance(r); !ok {
			t.Errorf("CJK face for %U has no glyph", r)
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

// The first font in the chain that has a rune draws it.
func TestFallbackChainOrder(t *testing.T) {
	g := jetBrainsMonoRegular
	// The symbols font as primary lacks 'A'; both fallbacks have it.
	s, err := Load(symbolsNerdFontMono, symbolsNerdFontMono, symbolsNerdFontMono, symbolsNerdFontMono,
		FallbackFont{Name: "regular", Data: g}, FallbackFont{Name: "bold", Data: jetBrainsMonoBold})
	if err != nil {
		t.Fatal(err)
	}
	if got := s.fontIndexFor('A', false, false); got != int(styleCount) {
		t.Fatalf("fontIndexFor('A') = %d, want the first fallback %d", got, styleCount)
	}
	first, _, _ := s.FaceFor('A', false, false, 14)
	want, _ := s.face(int(styleCount), 14)
	if first != want {
		t.Error("FaceFor('A') did not return the first fallback's face")
	}
}

// User fonts come after the embedded fonts, so they never change glyphs the
// embedded chain already draws, but they supply what it lacks.
func TestDefaultExtraFontsFollowEmbeddedChain(t *testing.T) {
	// The Twemoji font maps U+1F600 to an (outline-less) glyph, so it stands
	// in for a user font that has a rune nothing else has.
	s, err := Default(FallbackFont{Name: "user", Data: twemojiMozilla})
	if err != nil {
		t.Fatal(err)
	}
	const embedded = 2 // Nerd symbols, CJK subset
	if got := s.fontIndexFor(0xF015, false, false); got != int(styleCount) {
		t.Errorf("Nerd icon from font %d, want the embedded symbols font %d", got, styleCount)
	}
	if got := s.fontIndexFor('世', false, false); got != int(styleCount)+1 {
		t.Errorf("hanzi from font %d, want the embedded CJK font %d", got, styleCount+1)
	}
	if got := s.fontIndexFor(0x1F600, false, false); got != int(styleCount)+embedded {
		t.Errorf("rune only the user font has from font %d, want %d", got, int(styleCount)+embedded)
	}
}

// A TrueType collection is accepted as a fallback; its first font is used.
func TestLoadFallbackCollection(t *testing.T) {
	g := jetBrainsMonoRegular
	s, err := Load(g, g, g, g, FallbackFont{Name: "ttc", Data: wrapInCollection(t, notoSansMonoCJK)})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.FaceFor('世', false, false, 14); !ok {
		t.Error("collection member not consulted")
	}
}

// wrapInCollection returns a one-font TTC containing font.
func wrapInCollection(t *testing.T, font []byte) []byte {
	t.Helper()
	const header = 16 // "ttcf", version 1.0, numFonts 1, offset of font 0
	out := append(make([]byte, header), font...)
	copy(out, "ttcf")
	binary.BigEndian.PutUint32(out[4:], 0x00010000)
	binary.BigEndian.PutUint32(out[8:], 1)
	binary.BigEndian.PutUint32(out[12:], header)
	// Table offsets in a collection are relative to the start of the file.
	numTables := int(binary.BigEndian.Uint16(out[header+4:]))
	for i := range numTables {
		off := header + 12 + 16*i + 8
		binary.BigEndian.PutUint32(out[off:], binary.BigEndian.Uint32(out[off:])+header)
	}
	return out
}

func TestEmoji(t *testing.T) {
	s := mustDefault(t)
	for _, c := range []string{"\U0001F600", "\U0001F468\u200d\U0001F469\u200d\U0001F467", "\U0001F1E6\U0001F1FA", "1\ufe0f\u20e3"} {
		img, ok := s.Emoji(c, 32)
		if !ok || img.Bounds().Dx() != 32 || img.Bounds().Dy() != 32 {
			t.Errorf("Emoji(%q, 32) = %v, ok %v; want a 32x32 image", c, img != nil, ok)
		}
	}
	if _, ok := s.Emoji("世", 32); ok {
		t.Error("Emoji drew a hanzi")
	}
}

func TestEmojiAbsentWithLoad(t *testing.T) {
	g := jetBrainsMonoRegular
	s, _ := Load(g, g, g, g)
	if _, ok := s.Emoji("\U0001F600", 32); ok {
		t.Error("Load set has colour emoji")
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

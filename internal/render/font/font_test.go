package font

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"math"
	"testing"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/font/sfnt"
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
		size                              float64
		width, height, ascent, descent, u int
	}{
		{12, 7, 16, 12, 4, 2},  // 7.2 and 15.84 round
		{14, 8, 18, 14, 4, 2},  // 8.4 rounds down: tighter than design, not looser
		{15, 9, 20, 16, 4, 2},  // exact advance
		{16, 10, 21, 16, 5, 2}, // 9.6 rounds up
		{24, 14, 32, 25, 7, 4}, // 12pt on a 2x display in Ghostty
	} {
		m, err := mustDefault(t).Metrics(tc.size)
		if err != nil {
			t.Fatal(err)
		}
		if m.CellWidth != tc.width || m.CellHeight != tc.height || m.Ascent != tc.ascent || m.Descent != tc.descent || m.UnderlineOffset != tc.u {
			t.Errorf("Metrics(%v) = %+v, want %dx%d ascent %d descent %d underline %d", tc.size, m, tc.width, tc.height, tc.ascent, tc.descent, tc.u)
		}
		if math.Abs(m.FaceWidth-0.6*tc.size) > 1e-9 {
			t.Errorf("Metrics(%v).FaceWidth = %v, want %v", tc.size, m.FaceWidth, 0.6*tc.size)
		}
	}
}

func TestParseCellAdjust(t *testing.T) {
	for _, tc := range []struct {
		in         string
		v, want    int
		wantString string
	}{
		{"2", 14, 16, "2"},
		{"-1", 14, 13, "-1"},
		{"+3", 14, 17, "3"},
		{"-100", 14, 0, "-100"},   // pixels: not below zero
		{"-7%", 14, 13, "-7%"},    // 13.02
		{"-7%", 7, 7, "-7%"},      // 6.51 rounds up
		{"20%", 10, 12, "20%"},    // a percentage of the size, not of 100
		{"-150%", 10, 0, "-150%"}, // multiplier clamped at 0
		{"0%", 10, 10, "0%"},
	} {
		a, err := ParseCellAdjust(tc.in)
		if err != nil {
			t.Errorf("ParseCellAdjust(%q): %v", tc.in, err)
			continue
		}
		if got := a.apply(tc.v); got != tc.want {
			t.Errorf("%q applied to %d = %d, want %d", tc.in, tc.v, got, tc.want)
		}
		if a.String() != tc.wantString {
			t.Errorf("%q formats as %q, want %q", tc.in, a.String(), tc.wantString)
		}
	}
	for _, bad := range []string{"", "%", "x", "1.5", "5px", "--2", "NaN%", "1e999%"} {
		if _, err := ParseCellAdjust(bad); err == nil {
			t.Errorf("ParseCellAdjust(%q) succeeded, want error", bad)
		}
	}
	var zero CellAdjust
	if zero.apply(9) != 9 || zero.String() != "0" {
		t.Error("zero CellAdjust changes the size")
	}
}

// ghosttyCell transcribes Metrics.calc and the cell_width/cell_height
// modifiers of Ghostty (third_party/ghostty/src/font/Metrics.zig, pinned
// commit) from raw font numbers in pixels, with its own bottom-up baseline,
// so the arithmetic is independent of Set.Metrics. Returns cell width,
// height, baseline distance from the top.
func ghosttyCell(advance, ascent, descent, gap float64, wPct, hPct *float64, wPx, hPx int) (w, h, baselineFromTop int) {
	faceHeight := ascent + descent + gap
	cellW, cellH := math.Round(advance), math.Round(faceHeight)
	faceBaseline := gap/2 + descent // descent as a positive depth
	cellBaseline := math.Round(faceBaseline - (cellH-faceHeight)/2)
	faceY := cellBaseline - faceBaseline
	mod := func(v float64, pct *float64, px int) float64 {
		switch {
		case pct != nil:
			return math.Max(1, math.Round(v*math.Max(0, 1+*pct/100)))
		default:
			return math.Max(1, v+float64(px))
		}
	}
	newW, newH := mod(cellW, wPct, wPx), mod(cellH, hPct, hPx)
	if newH != cellH {
		diff := newH - cellH
		half := diff / 2
		var diffTop, diffBottom float64
		if faceY-(cellH-faceHeight)/2 > 0 {
			diffTop, diffBottom = math.Ceil(half), math.Floor(half)
		} else {
			diffTop, diffBottom = math.Floor(half), math.Ceil(half)
		}
		_ = diffTop
		cellBaseline += diffBottom
		cellH = newH
	}
	return int(newW), int(cellH), int(cellH - cellBaseline)
}

// Cell geometry must equal Ghostty's for the same font size, including the
// user's 12pt font with adjust-cell-width = -7% on a 1x and 2x display.
func TestCellGeometryMatchesGhostty(t *testing.T) {
	s := mustDefault(t)
	f := s.fonts[styleRegular]
	var buf sfnt.Buffer
	upm := fixed.I(int(f.UnitsPerEm()))
	m, err := f.Metrics(&buf, upm, xfont.HintingNone)
	if err != nil {
		t.Fatal(err)
	}
	g, _ := f.GlyphIndex(&buf, 'M')
	advUnits, _ := f.GlyphAdvance(&buf, g, upm, xfont.HintingNone)
	pct := func(v float64) *float64 { return &v }
	type adj struct {
		spec string
		pct  *float64
		px   int
	}
	adjusts := []adj{{"0", nil, 0}, {"-7%", pct(-7), 0}, {"2", nil, 2}, {"-1", nil, -1}, {"10%", pct(10), 0}, {"-20%", pct(-20), 0}}
	for _, size := range []float64{11, 12, 14, 15, 16, 20, 24, 28, 33.5} {
		scale := size / float64(upm>>6)
		advance := float64(advUnits>>6) * scale
		ascent, descent, height := float64(m.Ascent>>6)*scale, float64(m.Descent>>6)*scale, float64(m.Height>>6)*scale
		base, err := s.Metrics(size)
		if err != nil {
			t.Fatal(err)
		}
		for _, w := range adjusts {
			for _, h := range adjusts {
				wa, _ := ParseCellAdjust(w.spec)
				ha, _ := ParseCellAdjust(h.spec)
				got := base.Adjust(wa, ha)
				wantW, wantH, wantBase := ghosttyCell(advance, ascent, descent, height-ascent-descent, w.pct, h.pct, w.px, h.px)
				if got.CellWidth != wantW || got.CellHeight != wantH || got.Ascent != wantBase {
					t.Errorf("size %v width %s height %s: got %dx%d baseline %d, Ghostty %dx%d baseline %d",
						size, w.spec, h.spec, got.CellWidth, got.CellHeight, got.Ascent, wantW, wantH, wantBase)
				}
			}
		}
	}
}

func TestGlyphOffsetX(t *testing.T) {
	for _, tc := range []struct {
		cell int
		face float64
		want float64
	}{
		{9, 9, 0},
		{10, 9.6, 0.2},   // wider cell: centre
		{12, 9.6, 1.2},   // much wider: centre
		{8, 8.4, -0.2},   // narrower by under a pixel: shift by the fraction
		{13, 14.4, -0.7}, // narrower by 1.4: the integer part is dropped, left-anchored
		{7, 14.4, -0.7},  // dx = -3.7 -> -0.7
		{14, 14.4, -0.2}, // 12pt on a 2x display
	} {
		m := Metrics{CellWidth: tc.cell, FaceWidth: tc.face}
		if got := m.GlyphOffsetX(); math.Abs(got-tc.want) > 1e-9 {
			t.Errorf("cell %d face %v: offset %v, want %v", tc.cell, tc.face, got, tc.want)
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

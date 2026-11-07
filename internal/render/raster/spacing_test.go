package raster

import (
	"fmt"
	"image"
	"math"
	"strings"
	"testing"

	"github.com/michaellorant/shellshot/internal/render/font"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// spacingGlyphs are the glyphs whose ink is measured for centring.
const spacingGlyphs = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// TestSpacingAudit measures the cell geometry at several sizes and scales and
// logs one table row per combination (go test -run Spacing -v). It fails if
// the cell pitch is more than half a pixel from the glyph advance, if ink is
// off-centre in the cell by more than the rounding allows, or if glyphs or
// decorations leave the cell or are out of order.
//
// Columns: px = em in pixels; adv = advance of 'M'; err = (cell width - adv)
// / adv; lh = cell height / em against the font's design line height; dx =
// glyph offset in the cell; L/R = mean left/right bearing of ink in the cell
// after dx (px); base = baseline row; ul, strike, mid = rows relative to the
// baseline of the underline top, the strikethrough top and the cell middle
// (where box-drawing lines are); xh = x-height centre relative to the baseline.
func TestSpacingAudit(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	var tab strings.Builder
	tab.WriteString("\n size sc  px    cell   adv     err%   lh(design)   dx     base ul  strike mid  xh     L/R reg       L/R bold      L/R ital\n")
	for _, size := range []float64{12, 14, 15, 16, 20} {
		for _, scale := range []int{1, 2} {
			r, err := New(set, Options{FontSize: size, Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			m := r.metrics
			px := size * float64(scale)
			face, _ := set.Face(false, false, px)
			adv := m.FaceWidth
			pitchErr := (float64(m.CellWidth) - adv) / adv * 100
			if d := math.Abs(float64(m.CellWidth) - adv); d > 0.5+1e-6 {
				t.Errorf("size %v scale %d: cell width %d is %.2f px from the advance %.3f", size, scale, m.CellWidth, d, adv)
			}
			dx := m.GlyphOffsetX()
			design := float64(face.Metrics().Height) / 64 / px

			bearings := func(bold, italic bool) (l, rr float64) {
				f, _ := set.Face(bold, italic, px)
				n := 0
				for _, g := range spacingGlyphs {
					b, _, ok := f.GlyphBounds(g)
					if !ok {
						continue
					}
					l += float64(b.Min.X)/64 + dx
					rr += float64(m.CellWidth) - float64(b.Max.X)/64 - dx
					n++
				}
				return l / float64(n), rr / float64(n)
			}
			l0, r0 := bearings(false, false)
			l1, r1 := bearings(true, false)
			l2, r2 := bearings(false, true)
			// Rounding the cell moves the ink by at most half a pixel from
			// centre, plus the left-anchored overflow of a narrower cell.
			if d := math.Abs(l0 - r0); d > 1.0 {
				t.Errorf("size %v scale %d: regular ink off-centre by %.2f px (L %.2f, R %.2f)", size, scale, d, l0, r0)
			}

			xh := float64(r.regular.Metrics().XHeight) / 64
			ul, strike, th := r.underlineTop(), r.strikeTop(), r.thickness()
			// Top to bottom: strikethrough (x-height middle), baseline, a gap row, underline.
			if !(strike < m.Ascent && m.Ascent < ul && ul+th <= m.CellHeight) {
				t.Errorf("size %v scale %d: strike %d, baseline %d, underline %d+%d, cell height %d out of order", size, scale, strike, m.Ascent, ul, th, m.CellHeight)
			}
			// The deepest descender ('p') and the cap height fit the cell.
			if b, _, ok := face.GlyphBounds('p'); ok && b.Max.Y.Ceil() > m.Descent {
				t.Errorf("size %v scale %d: descender %d px deeper than descent %d", size, scale, b.Max.Y.Ceil(), m.Descent)
			}
			if b, _, ok := face.GlyphBounds('H'); ok && -b.Min.Y.Floor() > m.Ascent {
				t.Errorf("size %v scale %d: cap height %d above ascent %d", size, scale, -b.Min.Y.Floor(), m.Ascent)
			}
			tab.WriteString(fmt.Sprintf(" %-4v %d   %-5.1f %2dx%-3d %-7.3f %+-6.2f %.3f(%.2f) %+-.2f %-4d %-3d %-6d %-4d %-6.1f %+.2f/%+-.2f %+.2f/%+-.2f %+.2f/%+-.2f\n",
				size, scale, px, m.CellWidth, m.CellHeight, adv, pitchErr,
				float64(m.CellHeight)/px, design, dx, m.Ascent, ul-m.Ascent, strike-m.Ascent, m.CellHeight/2-m.Ascent, float64(m.Ascent)-xh/2-float64(m.Ascent), l0, r0, l1, r1, l2, r2))
		}
	}
	t.Log(tab.String())
}

// With a cell adjusted by --adjust-cell-width/-height, colour emoji, CJK
// fallback glyphs and combining marks are laid out on the adjusted grid: the
// emoji and the wide CJK rune are scaled to the adjusted two-cell span and
// ink nothing outside it, and the neighbouring column keeps its text.
func TestAdjustedCellsKeepClustersInTheirSpan(t *testing.T) {
	for _, adj := range []struct{ name, width, height string }{
		{"narrow", "-7%", "0"}, {"wide", "20%", "0"}, {"short", "0", "-2"}, {"narrow and tall", "-1", "3"},
	} {
		w, err := font.ParseCellAdjust(adj.width)
		if err != nil {
			t.Fatal(err)
		}
		h, err := font.ParseCellAdjust(adj.height)
		if err != nil {
			t.Fatal(err)
		}
		r, _ := newTestRenderer(t, Options{FontSize: 12, Scale: 2, AdjustCellWidth: w, AdjustCellHeight: h})
		for name, cluster := range map[string]string{"emoji": grin, "vs16 heart": heartVS, "cjk": "\u4e16"} {
			s := clusterSnapshot(3, cluster, 2)
			s.Lines[0] = s.Lines[0][:len(s.Lines[0])-1] + " "
			img := mustRender(t, r, s)
			span := emojiCells(r)
			if span.Dx() != 2*r.metrics.CellWidth {
				t.Fatalf("%s: span %v is not two adjusted cells of %d px", adj.name, span, r.metrics.CellWidth)
			}
			if inkBox(r, img, span).Empty() {
				t.Errorf("%s %s: no ink in its span", adj.name, name)
			}
			b := img.Bounds()
			for y := b.Min.Y; y < b.Max.Y; y++ {
				for x := b.Min.X; x < b.Max.X; x++ {
					if img.RGBAAt(x, y) != r.theme.Background && !image.Pt(x, y).In(span) {
						t.Fatalf("%s %s: ink at (%d,%d) outside the span %v", adj.name, name, x, y, span)
					}
				}
			}
		}
		// A text glyph is moved by the glyph offset and a combining mark
		// follows its base, so "e" + U+0301 inks only its own column.
		s := plainSnapshot(10, "   e\u0301     ")
		s.Widths = [][]snapshot.Wide{{{Rune: 4, Cells: 0}}}
		img := mustRender(t, r, s)
		if inkInCell(r, img, 3, 0) == 0 || inkInCell(r, img, 2, 0) != 0 || inkInCell(r, img, 4, 0) != 0 {
			t.Errorf("%s: e + combining acute is not confined to column 3", adj.name)
		}
	}
}

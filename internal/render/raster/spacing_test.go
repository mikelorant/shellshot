package raster

import (
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/michaellorant/shellshot/internal/render/font"
)

// spacingGlyphs are the glyphs whose ink is measured for centring.
const spacingGlyphs = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"

// TestSpacingAudit measures the cell geometry at several sizes and scales and
// logs one table row per combination (go test -run Spacing -v). It fails if
// the cell pitch differs from the drawn glyph advance, if ink is off-centre in
// the cell, if scale n is not exactly n times scale 1, or if glyphs or
// decorations leave the cell.
//
// Columns: req = requested em px; em = em px drawn; adv = advance of 'M' at
// the requested size and spacing = cell width - adv, the letter spacing the
// cell adds to the requested size (px); err = pitch error against the drawn
// advance (%); lh = cell height / em, against the font's design line height;
// L/R = mean left/right bearing of ink in the cell (px).
func TestSpacingAudit(t *testing.T) {
	set, err := font.Default()
	if err != nil {
		t.Fatal(err)
	}
	var tab strings.Builder
	tab.WriteString("\n req  sc  em      cell   adv     spacing  err%   lh(design)   base  ul  strike  mid  xh    L/R reg        L/R bold       L/R ital\n")
	for _, size := range []float64{12, 14, 15, 16, 20} {
		var one font.Metrics
		for _, scale := range []int{1, 2} {
			r, err := New(set, Options{FontSize: size, Scale: scale})
			if err != nil {
				t.Fatal(err)
			}
			m := r.metrics
			if scale == 1 {
				one = m
			} else if m.CellWidth != 2*one.CellWidth || m.CellHeight != 2*one.CellHeight || m.Ascent != 2*one.Ascent {
				t.Errorf("size %v: scale 2 cell %+v is not twice scale 1 %+v", size, m, one)
			}
			req, _ := set.Face(false, false, size*float64(scale))
			reqAdv, _ := req.GlyphAdvance('M')
			adv := float64(reqAdv) / 64
			drawn, _ := set.Face(false, false, m.EmPx)
			drawnAdv, _ := drawn.GlyphAdvance('M')
			pitchErr := (float64(m.CellWidth) - float64(drawnAdv)/64) / (float64(drawnAdv) / 64) * 100
			if math.Abs(pitchErr) > 0.5 {
				t.Errorf("size %v scale %d: cell width %d vs drawn advance %.3f (%.2f%%)", size, scale, m.CellWidth, float64(drawnAdv)/64, pitchErr)
			}
			lh := drawn.Metrics()
			design := float64(lh.Height) / 64 / m.EmPx

			bearings := func(bold, italic bool) (l, rr float64) {
				f, _ := set.Face(bold, italic, m.EmPx)
				n := 0
				for _, g := range spacingGlyphs {
					b, _, ok := f.GlyphBounds(g)
					if !ok {
						continue
					}
					l += float64(b.Min.X) / 64
					rr += float64(m.CellWidth) - float64(b.Max.X)/64
					n++
				}
				return l / float64(n), rr / float64(n)
			}
			l0, r0 := bearings(false, false)
			l1, r1 := bearings(true, false)
			l2, r2 := bearings(false, true)
			if d := math.Abs(l0 - r0); d > 0.5 {
				t.Errorf("size %v scale %d: regular ink off-centre by %.2f px (L %.2f, R %.2f)", size, scale, d, l0, r0)
			}

			xh := float64(r.regular.Metrics().XHeight) / 64
			mid := m.CellHeight / 2
			ul, strike, th := r.underlineTop(), r.strikeTop(), r.thickness()
			xhMid := float64(m.Ascent) - xh/2
			if !(strike < m.Ascent && ul+th <= m.CellHeight) {
				t.Errorf("size %v scale %d: strike %d, baseline %d, underline %d+%d, cell height %d out of order", size, scale, strike, m.Ascent, ul, th, m.CellHeight)
			}
			// The deepest descender ('p') and the cap height must fit the cell.
			if b, _, ok := drawn.GlyphBounds('p'); ok && b.Max.Y.Ceil() > m.Descent {
				t.Errorf("size %v scale %d: descender %d px deeper than descent %d", size, scale, b.Max.Y.Ceil(), m.Descent)
			}
			if b, _, ok := drawn.GlyphBounds('H'); ok && -b.Min.Y.Floor() > m.Ascent {
				t.Errorf("size %v scale %d: cap height %d above ascent %d", size, scale, -b.Min.Y.Floor(), m.Ascent)
			}
			tab.WriteString(fmt.Sprintf(" %-4v %d   %-7.3f %2dx%-3d %-7.3f %+-8.3f %+-6.2f %.3f(%.2f) %-5d %-3d %-7d %-4d %-5.1f %+.2f/%+-.2f %s %+.2f/%+-.2f %s %+.2f/%+-.2f\n",
				size, scale, m.EmPx, m.CellWidth, m.CellHeight, adv, float64(m.CellWidth)-adv, pitchErr,
				float64(m.CellHeight)/m.EmPx, design, m.Ascent, ul-m.Ascent, strike-m.Ascent, mid-m.Ascent, xhMid-float64(m.Ascent), l0, r0, "", l1, r1, "", l2, r2))
		}
	}
	t.Log(tab.String() + " (ul, strike, mid are rows relative to the baseline; xh is the x-height centre relative to the baseline)")
}

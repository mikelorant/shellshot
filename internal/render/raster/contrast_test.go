package raster

import (
	"image/color"
	"math"
	"testing"

	"github.com/michaellorant/shellshot/internal/render/theme"
)

func TestCoverageLUT(t *testing.T) {
	for name, lut := range map[string]*[256]uint8{"light on dark": lightOnDarkLUT, "dark on light": darkOnLightLUT} {
		if lut[0] != 0 || lut[255] != 255 {
			t.Errorf("%s: endpoints %d, %d", name, lut[0], lut[255])
		}
		for i := 1; i < 256; i++ {
			if lut[i] < lut[i-1] {
				t.Fatalf("%s: not monotonic at %d", name, i)
			}
		}
	}
	if lightOnDarkLUT[128] <= 128 || darkOnLightLUT[128] >= 128 {
		t.Errorf("midpoints %d (light on dark), %d (dark on light)", lightOnDarkLUT[128], darkOnLightLUT[128])
	}
}

// Perceived ink is the light-linear brightness added by the glyph pixels. The
// curve must move naive sRGB blending toward the light-linear ideal for both
// polarities of the default themes, without overshooting it.
func TestContrastMovesInkTowardLinearBlend(t *testing.T) {
	lin := func(v uint8) float64 {
		c := float64(v) / 255
		if c <= 0.04045 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	for _, th := range []theme.Theme{theme.Default(), theme.Light()} {
		fg, bg := th.Foreground, th.Background
		lut := lightOnDarkLUT
		if luma(fg) < luma(bg) {
			lut = darkOnLightLUT
		}
		var area, naive, curved float64
		for i := 0; i <= 255; i++ {
			c := float64(i) / 255
			area += c
			naive += blendedInk(lin, fg, bg, c)
			curved += blendedInk(lin, fg, bg, float64(lut[i])/255)
		}
		ideal := area * (lin(fg.G) - lin(bg.G))
		before, after := naive/ideal, curved/ideal
		if math.Abs(after-1) >= math.Abs(before-1) || math.Abs(after-1) > 0.2 {
			t.Errorf("%s: perceived ink %.2f naive, %.2f with contrast, want closer to 1", th.Name, before, after)
		}
	}
}

func blendedInk(lin func(uint8) float64, fg, bg color.RGBA, coverage float64) float64 {
	v := uint8(math.Round(float64(bg.G) + (float64(fg.G)-float64(bg.G))*coverage))
	return lin(v) - lin(bg.G)
}

func TestContrastFaceEqualLumaIsIdentity(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	c := color.RGBA{100, 100, 100, 255}
	if got := r.contrastFace(r.regular, c, c); got != r.regular {
		t.Error("equal luma colours wrapped the face")
	}
}

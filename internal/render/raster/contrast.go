package raster

import (
	"image"
	"image/color"
	"math"

	xfont "golang.org/x/image/font"
	"golang.org/x/image/math/fixed"
)

// Glyph coverage is blended into the background in sRGB space, as terminals
// do. That is not light-linear, so the same outline looks lighter than
// designed when it is light on dark (coverage 0.5 gives a pixel at 50% of the
// sRGB range, only 21% of the light) and heavier when it is dark on light.
// For the default themes this makes dark-theme text about 23% too thin and
// light-theme text about 22% too heavy by perceived ink. contrastFace nudges
// coverage with a power curve per polarity to reduce that error without
// leaving sRGB blending (a full correction would make dark-on-light text
// look anaemic next to other terminals): exponents below 1 raise partial
// coverage.
const (
	lightOnDarkExponent = 0.8
	darkOnLightExponent = 1.25
)

var (
	lightOnDarkLUT = coverageLUT(lightOnDarkExponent)
	darkOnLightLUT = coverageLUT(darkOnLightExponent)
)

func coverageLUT(exponent float64) *[256]uint8 {
	var lut [256]uint8
	for i := range lut {
		lut[i] = uint8(math.Round(255 * math.Pow(float64(i)/255, exponent)))
	}
	return &lut
}

// luma is the Rec. 709 luma of c's sRGB values, 0..255.
func luma(c color.RGBA) float64 {
	return 0.2126*float64(c.R) + 0.7152*float64(c.G) + 0.0722*float64(c.B)
}

// contrastFace wraps face so its glyph masks are corrected for drawing fg on
// bg. Colours of equal luma are left alone.
func (r *Renderer) contrastFace(face xfont.Face, fg, bg color.RGBA) xfont.Face {
	var lut *[256]uint8
	switch lf, lb := luma(fg), luma(bg); {
	case lf > lb:
		lut = lightOnDarkLUT
	case lf < lb:
		lut = darkOnLightLUT
	default:
		return face
	}
	return &contrastedFace{Face: face, lut: lut, scratch: &r.maskScratch}
}

type contrastedFace struct {
	xfont.Face
	lut     *[256]uint8
	scratch *image.Alpha
}

// Glyph satisfies the font.Face interface. The returned mask is valid until
// the next call, like the wrapped face's.
func (f *contrastedFace) Glyph(dot fixed.Point26_6, r rune) (dr image.Rectangle, mask image.Image, maskp image.Point, advance fixed.Int26_6, ok bool) {
	dr, mask, maskp, advance, ok = f.Face.Glyph(dot, r)
	a, isAlpha := mask.(*image.Alpha)
	if !ok || !isAlpha {
		return
	}
	s := f.scratch
	if cap(s.Pix) < len(a.Pix) {
		s.Pix = make([]uint8, 2*len(a.Pix))
	}
	s.Pix = s.Pix[:len(a.Pix)]
	s.Stride, s.Rect = a.Stride, a.Rect
	for i, v := range a.Pix {
		s.Pix[i] = f.lut[v]
	}
	return dr, s, maskp, advance, ok
}

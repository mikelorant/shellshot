package raster

import (
	"image"
	"image/color"
)

// thickness is the stroke width in pixels of underlines, strikethroughs and
// the cursor's underline: max(1, cellH/14).
func (r *Renderer) thickness() int {
	return max(1, r.metrics.CellHeight/14)
}

// underlineTop is the offset from the cell top of the underline's first
// pixel row. It leaves one stroke of space above the cell bottom, where the
// underline cursor is drawn.
func (r *Renderer) underlineTop() int {
	return r.metrics.CellHeight - 3*r.thickness()
}

// strikeTop is the offset from the cell top of the strikethrough's first
// pixel row: the stroke is centred on half the x-height above the baseline.
func (r *Renderer) strikeTop() int {
	xh := r.regular.Metrics().XHeight.Round()
	if xh <= 0 {
		xh = r.metrics.Ascent / 2
	}
	return r.metrics.Ascent - xh/2 - r.thickness()/2
}

// drawDecorations draws the cell's underline and strikethrough across its
// w columns in the cell's foreground colour.
func (r *Renderer) drawDecorations(img *image.RGBA, c cell) {
	o := r.cellOrigin(c.col, c.row)
	width := c.width * r.metrics.CellWidth
	if c.st.underline {
		fillRect(img, image.Rect(o.X, o.Y+r.underlineTop(), o.X+width, o.Y+r.underlineTop()+r.thickness()), c.st.fg)
	}
	if c.st.strike {
		fillRect(img, image.Rect(o.X, o.Y+r.strikeTop(), o.X+width, o.Y+r.strikeTop()+r.thickness()), c.st.fg)
	}
}

// fillRect paints rect (clipped to img) with the opaque colour c.
func fillRect(img *image.RGBA, rect image.Rectangle, c color.RGBA) {
	rect = rect.Intersect(img.Bounds())
	for y := rect.Min.Y; y < rect.Max.Y; y++ {
		i := img.PixOffset(rect.Min.X, y)
		for x := rect.Min.X; x < rect.Max.X; x++ {
			img.Pix[i], img.Pix[i+1], img.Pix[i+2], img.Pix[i+3] = c.R, c.G, c.B, c.A
			i += 4
		}
	}
}

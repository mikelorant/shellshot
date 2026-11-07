package sprite

import "math"

// powerline draws U+E0B0..U+E0B7:
//
//	E0B0  solid right-pointing triangle   E0B2  solid left-pointing triangle
//	E0B1  right-pointing chevron          E0B3  left-pointing chevron
//	E0B4  solid right half disc           E0B6  solid left half disc
//	E0B5  right half-disc outline         E0B7  left half-disc outline
//
// The left-hand forms mirror the right-hand ones. Shapes span the whole
// cell height and their flat side lies on the cell edge.
func (c *canvas) powerline(r rune) {
	mirror := r == 0xE0B2 || r == 0xE0B3 || r == 0xE0B6 || r == 0xE0B7
	outline := r&1 == 1
	round := r >= 0xE0B4
	t := lineThickness(c.h)
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			ux := x
			if mirror {
				ux = c.w - 1 - x
			}
			var on bool
			switch {
			case round:
				on = c.halfDisc(ux, y, outline, t)
			default:
				on = c.triangle(ux, y, outline, t)
			}
			if on {
				c.set(x, y, 255)
			}
		}
	}
}

// triangleEdge returns how many pixels of row y lie inside the triangle with
// base on the left edge and tip at the middle of the right edge. The extent
// is that of the row's edge nearest the middle, rounded up, so the base stays
// solid and the tip rows reach the right edge.
func (c *canvas) triangleEdge(y int) int {
	d := abs(2*y + 1 - c.h)
	return min(c.w, max(1, (c.w*(c.h-d+1)+c.h-1)/c.h))
}

// triangle reports whether pixel (x, y) is inked in the solid triangle, or in
// the chevron formed by its two slanted edges.
func (c *canvas) triangle(x, y int, outline bool, t int) bool {
	e := c.triangleEdge(y)
	if !outline {
		return x < e
	}
	// horizontal run of a stroke t thick along the slanted edge
	slope := math.Hypot(float64(c.w), float64(c.h)/2) / (float64(c.h) / 2)
	run := max(1, int(math.Round(float64(t)*slope)))
	if y == 0 || y == c.h-1 {
		run = e // the chevron starts at the cell's left edge
	}
	return x < e && x >= e-run
}

// halfDisc reports whether pixel (x, y) is inked in the half ellipse centred
// on the left edge, with horizontal radius w and vertical radius h/2, either
// solid or as a ring t thick. Pixel centres are tested in doubled integer
// coordinates.
func (c *canvas) halfDisc(x, y int, outline bool, t int) bool {
	dx, dy := int64(2*x+1), int64(2*y+1-c.h)
	a, b := int64(2*c.w), int64(c.h)
	if !insideEllipse(dx, dy, a, b, true) {
		return false
	}
	if !outline {
		return true
	}
	ai, bi := a-2*int64(t), b-2*int64(t)
	return !(ai > 0 && bi > 0 && insideEllipse(dx, dy, ai, bi, false))
}

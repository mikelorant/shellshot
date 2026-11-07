package sprite

import (
	"fmt"
	"strings"
)

// weight is the line style of one arm of a box-drawing glyph.
type weight uint8

const (
	none weight = iota
	light
	heavy
	double
)

// arms gives the line style reaching each cell edge from the cell centre.
type arms struct{ up, right, down, left weight }

// boxArms lists U+2500..U+257F as up,right,down,left weights per glyph
// (0 none, 1 light, 2 heavy, 3 double), 16 glyphs per row. "...." marks the
// glyphs that are not made of straight arms: dashes, arcs and diagonals.
var boxArms = parseArms(`
0101 0202 1010 2020 .... .... .... .... .... .... .... .... 0110 0210 0120 0220
0011 0012 0021 0022 1100 1200 2100 2200 1001 1002 2001 2002 1110 1210 2110 1120
2120 2210 1220 2220 1011 1012 2011 1021 2021 2012 1022 2022 0111 0112 0211 0212
0121 0122 0221 0222 1101 1102 1201 1202 2101 2102 2201 2202 1111 1112 1211 1212
2111 1121 2121 2112 2211 1122 1221 2212 1222 2122 2221 2222 .... .... .... ....
0303 3030 0310 0130 0330 0013 0031 0033 1300 3100 3300 1003 3001 3003 1310 3130
3330 1013 3031 3033 0313 0131 0333 1303 3101 3303 1313 3131 3333 .... .... ....
.... .... .... .... 0001 1000 0100 0010 0002 2000 0200 0020 0201 1020 0102 2010
`)

func parseArms(table string) [0x80]arms {
	var out [0x80]arms
	fields := strings.Fields(table)
	if len(fields) != len(out) {
		panic(fmt.Sprintf("sprite: box table has %d entries, want %d", len(fields), len(out)))
	}
	for i, f := range fields {
		if f == "...." {
			continue
		}
		out[i] = arms{weight(f[0] - '0'), weight(f[1] - '0'), weight(f[2] - '0'), weight(f[3] - '0')}
	}
	return out
}

// lineThickness is the light stroke width for a cell of height h.
func lineThickness(h int) int { return max(1, h/14) }

// span is a half-open pixel range along one axis.
type span struct{ a, b int }

// bands returns the strokes of weight wt centred in an axis of length n,
// where t is the light thickness: one band for light, one of 2t for heavy,
// two bands of t with a gap of t for double. With no weight it returns one
// empty band at the centre.
func bands(n int, wt weight, t int) []span {
	switch wt {
	case light:
		a := max(0, (n-t)/2)
		return []span{{a, a + t}}
	case heavy:
		a := max(0, (n-2*t)/2)
		return []span{{a, a + 2*t}}
	case double:
		a := max(0, (n-3*t)/2)
		return []span{{a, a + t}, {a + 2*t, a + 3*t}}
	}
	return []span{{n / 2, n / 2}}
}

// box draws U+2500..U+257F.
func (c *canvas) box(r rune) {
	t := lineThickness(c.h)
	switch {
	case r >= 0x2504 && r <= 0x250B:
		i := int(r - 0x2504)
		n := 3
		if i >= 4 {
			n = 4
		}
		c.dashed(n, i&1 != 0, i&2 != 0, t)
	case r >= 0x254C && r <= 0x254F:
		i := int(r - 0x254C)
		c.dashed(2, i&1 != 0, i&2 != 0, t)
	case r >= 0x256D && r <= 0x2570:
		// ╭ down+right, ╮ down+left, ╯ up+left, ╰ up+right
		i := int(r - 0x256D)
		c.arc(i == 0 || i == 3, i < 2, t)
	case r >= 0x2571 && r <= 0x2573:
		c.diagonals(r != 0x2572, r != 0x2571, t)
	default:
		c.lines(boxArms[r-0x2500], t)
	}
}

// lines draws the straight arms of a glyph. Each arm runs from the centre to
// its cell edge, so adjacent cells join. Where arms of different glyphs
// meet, the strokes are extended just far enough to fill the corner, and
// double lines turn into the nested corners of ╔ ╗ ╚ ╝ and the split lines of
// ╠ ╣ ╦ ╩ ╬.
func (c *canvas) lines(a arms, t int) {
	vx := bands(c.w, max(a.up, a.down), t) // x bands of the vertical strokes
	hy := bands(c.h, max(a.left, a.right), t)
	vx0, vx1 := vx[0].a, vx[len(vx)-1].b
	hy0, hy1 := hy[0].a, hy[len(hy)-1].b
	vert := a.up > none && a.down > none // vertical strokes pass through
	horiz := a.left > none && a.right > none
	vDouble := max(a.up, a.down) == double
	hDouble := max(a.left, a.right) == double

	for k, b := range bands(c.h, a.right, t) {
		x0 := vx0
		switch {
		case a.right != double || !vDouble:
			if a.left == none && vert {
				x0 = vx[0].b
			}
		case k == 0 && a.up > none, k == 1 && a.down > none:
			x0 = vx[1].a
		}
		if a.right != none {
			c.fill(x0, b.a, c.w, b.b)
		}
	}
	for k, b := range bands(c.h, a.left, t) {
		x1 := vx1
		switch {
		case a.left != double || !vDouble:
			if a.right == none && vert {
				x1 = vx[0].b
			}
		case k == 0 && a.up > none, k == 1 && a.down > none:
			x1 = vx[0].b
		}
		if a.left != none {
			c.fill(0, b.a, x1, b.b)
		}
	}
	for k, b := range bands(c.w, a.down, t) {
		y0 := hy0
		switch {
		case a.down != double || !hDouble:
			if a.up == none && horiz {
				y0 = hy[0].b
			}
		case k == 0 && a.left > none, k == 1 && a.right > none:
			y0 = hy[1].a
		}
		if a.down != none {
			c.fill(b.a, y0, b.b, c.h)
		}
	}
	for k, b := range bands(c.w, a.up, t) {
		y1 := hy1
		switch {
		case a.up != double || !hDouble:
			if a.down == none && horiz {
				y1 = hy[0].b
			}
		case k == 0 && a.left > none, k == 1 && a.right > none:
			y1 = hy[0].b
		}
		if a.up != none {
			c.fill(b.a, 0, b.b, y1)
		}
	}
}

// dashed draws a line of n dashes across the cell, centred on the other axis.
// Each of n equal segments loses a gap, split between its two ends, so the
// pattern continues unbroken into the neighbouring cell.
func (c *canvas) dashed(n int, isHeavy, vertical bool, t int) {
	th, length, across := t, c.w, c.h
	if isHeavy {
		th *= 2
	}
	if vertical {
		length, across = c.h, c.w
	}
	lo := max(0, (across-th)/2)
	for i := 0; i < n; i++ {
		s0, s1 := i*length/n, (i+1)*length/n
		gap := max(1, (s1-s0)/3)
		d0, d1 := s0+gap/2, s1-(gap-gap/2)
		if d1 <= d0 {
			d0, d1 = s0, s0+1
		}
		if vertical {
			c.fill(lo, d0, lo+th, d1)
		} else {
			c.fill(d0, lo, d1, lo+th)
		}
	}
}

// arc draws a rounded corner: a quarter ellipse about the cell corner on the
// right/down side that joins the middle of the two opposite edges. The
// stroke is the ring between two ellipses a half thickness either side of
// the centreline, tested at pixel centres in doubled integer coordinates, so
// where the arc meets an edge it matches the straight line of the
// neighbouring cell exactly.
func (c *canvas) arc(right, down bool, t int) {
	cx, cy := int64(2*max(0, (c.w-t)/2)+t), int64(2*max(0, (c.h-t)/2)+t) // doubled stroke centrelines
	var ex, ey int64                                                     // doubled corner the ellipse is centred on
	if right {
		ex = int64(2 * c.w)
	}
	if down {
		ey = int64(2 * c.h)
	}
	T := int64(t)
	ao, bo := abs64(ex-cx)+T, abs64(ey-cy)+T
	ai, bi := ao-2*T, bo-2*T
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			dx, dy := int64(2*x+1)-ex, int64(2*y+1)-ey
			if !insideEllipse(dx, dy, ao, bo, true) {
				continue
			}
			if ai > 0 && bi > 0 && insideEllipse(dx, dy, ai, bi, false) {
				continue
			}
			c.set(x, y, 255)
		}
	}
}

// insideEllipse reports whether (dx, dy) lies inside the origin-centred
// ellipse with semi-axes a and b; onEdge decides points exactly on it.
func insideEllipse(dx, dy, a, b int64, onEdge bool) bool {
	lhs, rhs := dx*dx*b*b+dy*dy*a*a, a*a*b*b
	return lhs < rhs || onEdge && lhs == rhs
}

func abs64(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}

// diagonals draws ╱ (slash), ╲ (backslash) or both as lines t thick between
// opposite cell corners: a pixel is inked when its centre is within t/2 of
// the line.
func (c *canvas) diagonals(slash, backslash bool, t int) {
	w, h := int64(c.w), int64(c.h)
	limit := int64(t) * int64(t) * (w*w + h*h)
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			px, py := int64(2*x+1), int64(2*y+1)
			n1 := px*h + py*w - 2*w*h // ╱: (w,0) to (0,h)
			n2 := px*h - py*w         // ╲: (0,0) to (w,h)
			if slash && n1*n1 <= limit || backslash && n2*n2 <= limit {
				c.set(x, y, 255)
			}
		}
	}
}

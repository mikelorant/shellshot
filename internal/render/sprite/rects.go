package sprite

import (
	"image"
	"image/color"
)

// Rect is a solid rectangle of a glyph: the pixels [X0,X1) x [Y0,Y1) of the
// cell, drawn at coverage A (1 to 255) of the foreground colour.
type Rect struct {
	X0, Y0, X1, Y1 int
	A              uint8
}

// Rects returns the glyph for r in a w x h pixel cell as rectangles, with the
// same pixels Draw inks, for vector output. The rectangles do not overlap and
// are ordered by top edge, then left edge. ok is false for a rune Draw does
// not handle. A glyph with no ink (a blank braille pattern) gives no
// rectangles and ok true.
func Rects(r rune, w, h int) (rects []Rect, ok bool) {
	if !Handles(r) {
		return nil, false
	}
	if w <= 0 || h <= 0 {
		return nil, true
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	Draw(img, r, img.Bounds(), color.RGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	alpha := func(x, y int) uint8 { return img.Pix[img.PixOffset(x, y)+3] }

	// open holds the rectangles that reach the previous row and may still
	// grow downwards.
	var open []Rect
	for y := 0; y < h; y++ {
		var runs []Rect
		for x := 0; x < w; {
			a := alpha(x, y)
			if a == 0 {
				x++
				continue
			}
			x1 := x + 1
			for x1 < w && alpha(x1, y) == a {
				x1++
			}
			runs = append(runs, Rect{X0: x, Y0: y, X1: x1, Y1: y + 1, A: a})
			x = x1
		}
		used := make([]bool, len(open))
		next := make([]Rect, 0, len(runs))
		for _, run := range runs {
			for i, o := range open {
				if !used[i] && o.X0 == run.X0 && o.X1 == run.X1 && o.A == run.A {
					run.Y0, used[i] = o.Y0, true // the same run one row up: grow it
					break
				}
			}
			next = append(next, run)
		}
		for i, o := range open {
			if !used[i] {
				rects = append(rects, o)
			}
		}
		open = next
	}
	rects = append(rects, open...)
	sortRects(rects)
	return rects, true
}

// sortRects orders rects by top edge, then left edge (insertion sort: glyphs
// have few rectangles).
func sortRects(rs []Rect) {
	for i := 1; i < len(rs); i++ {
		for j := i; j > 0 && (rs[j].Y0 < rs[j-1].Y0 || rs[j].Y0 == rs[j-1].Y0 && rs[j].X0 < rs[j-1].X0); j-- {
			rs[j], rs[j-1] = rs[j-1], rs[j]
		}
	}
}

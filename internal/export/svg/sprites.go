package svg

import (
	"cmp"
	"image/color"
	"slices"
	"strconv"
	"strings"
)

// maxInlineRects is the most rectangles a sprite can have and still be drawn
// inline. A glyph with more (the Powerline arrows and half circles, the
// diagonals, a few braille patterns) costs more as a copy in every cell than
// as a <use> of one shared <path>.
const maxInlineRects = 8

// pixelRect is a rectangle of whole pixels in the document.
type pixelRect struct{ x, y, w, h int }

// translated returns the rectangles of coverage level a of the inline sprite
// u, in the document.
func (u use) translated(a uint8) []pixelRect {
	var out []pixelRect
	for _, r := range u.rects {
		if r.A == a {
			out = append(out, pixelRect{int(u.x) + r.X0, int(u.y) + r.Y0, r.X1 - r.X0, r.Y1 - r.Y0})
		}
	}
	return out
}

// mergeRects returns rectangles that cover exactly the pixels of rs, in as
// few pieces as two greedy passes find: rectangles of one height that touch
// along a row join into one, then rectangles of one width that touch along a
// column. The pieces are ordered by top edge, then left edge. rs must not
// overlap.
func mergeRects(rs []pixelRect) []pixelRect {
	rs = slices.Clone(rs)
	slices.SortFunc(rs, func(a, b pixelRect) int {
		return cmp.Or(cmp.Compare(a.y, b.y), cmp.Compare(a.h, b.h), cmp.Compare(a.x, b.x))
	})
	rs = joinRects(rs, func(a, b pixelRect) bool {
		return a.y == b.y && a.h == b.h && a.x+a.w == b.x
	}, func(a, b pixelRect) pixelRect {
		a.w += b.w
		return a
	})
	slices.SortFunc(rs, func(a, b pixelRect) int {
		return cmp.Or(cmp.Compare(a.x, b.x), cmp.Compare(a.w, b.w), cmp.Compare(a.y, b.y))
	})
	rs = joinRects(rs, func(a, b pixelRect) bool {
		return a.x == b.x && a.w == b.w && a.y+a.h == b.y
	}, func(a, b pixelRect) pixelRect {
		a.h += b.h
		return a
	})
	slices.SortFunc(rs, func(a, b pixelRect) int {
		return cmp.Or(cmp.Compare(a.y, b.y), cmp.Compare(a.x, b.x))
	})
	return rs
}

// joinRects folds each rectangle of the sorted rs into the previous piece
// when touches says they meet.
func joinRects(rs []pixelRect, touches func(a, b pixelRect) bool, join func(a, b pixelRect) pixelRect) []pixelRect {
	var out []pixelRect
	for _, r := range rs {
		if n := len(out); n > 0 && touches(out[n-1], r) {
			out[n-1] = join(out[n-1], r)
			continue
		}
		out = append(out, r)
	}
	return out
}

// rectsPathData returns the path data that draws rs, one closed subpath each.
func rectsPathData(rs []pixelRect) string {
	var sb strings.Builder
	for _, r := range rs {
		w := strconv.Itoa(r.w)
		sb.WriteString("M" + strconv.Itoa(r.x) + " " + strconv.Itoa(r.y) + "h" + w + "v" + strconv.Itoa(r.h) + "h-" + w + "z")
	}
	return sb.String()
}

// fillOpacity returns the fill-opacity attribute of coverage level a, or ""
// for full coverage.
func fillOpacity(a uint8) string {
	if a == 255 {
		return ""
	}
	return ` fill-opacity="` + strconv.FormatFloat(float64(a)/255, 'f', 3, 64) + `"`
}

// writeInlineSprites writes the cells of one run of adjacent inline sprites as
// one <path> per colour and coverage level. The cells ink separate pixels, so
// drawing them in colour and level order instead of cell order changes
// nothing. Colours and levels keep the order they first appear in. Paths of a
// colour other than def share a <g> that holds the fill.
func writeInlineSprites(sb *strings.Builder, cells []use, def color.RGBA) {
	var colours []color.RGBA
	byColour := map[color.RGBA][]use{}
	for _, c := range cells {
		k := c.fg
		if _, ok := byColour[k]; !ok {
			colours = append(colours, k)
		}
		byColour[k] = append(byColour[k], c)
	}
	for _, k := range colours {
		cs := byColour[k]
		var levels []uint8
		for _, c := range cs {
			for _, r := range c.rects {
				if !slices.Contains(levels, r.A) {
					levels = append(levels, r.A)
				}
			}
		}
		var paths []string
		for _, a := range levels {
			var rs []pixelRect
			for _, c := range cs {
				rs = append(rs, c.translated(a)...)
			}
			paths = append(paths, "d=\""+rectsPathData(mergeRects(rs))+"\""+fillOpacity(a))
		}
		grouped := len(paths) > 1 && k != def
		if grouped {
			sb.WriteString(`<g fill="` + hex(k) + `">`)
		}
		for _, p := range paths {
			sb.WriteString("<path ")
			if !grouped && k != def {
				sb.WriteString(`fill="` + hex(k) + `" `)
			}
			sb.WriteString(p + "/>")
		}
		if grouped {
			sb.WriteString("</g>")
		}
	}
}

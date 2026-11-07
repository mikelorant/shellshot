package gif

import (
	"cmp"
	"image/color"
	"slices"
)

// palette is the global colour table. colors[0] is the transparent entry and
// colors[1:] the picture colours, which rgb repeats as channel values.
type palette struct {
	colors color.Palette
	rgb    [][3]int32
	// exact maps every colour of the animation to its index if the palette
	// holds all of them; it is nil for a quantised palette.
	exact map[uint32]uint8
}

// entry is a colour of the animation and the number of pixels that have it.
type entry struct {
	rgb uint32
	n   int
}

// buildPalette returns the palette for the colours in hist (colour to pixel
// count) with at most maxColors picture colours: hist itself if it fits,
// otherwise the weighted median cut of it.
func buildPalette(hist map[uint32]int, maxColors int) palette {
	entries := make([]entry, 0, len(hist))
	for c, n := range hist {
		entries = append(entries, entry{c, n})
	}
	// Map order is random: sort so the result depends only on the input.
	slices.SortFunc(entries, func(a, b entry) int { return cmp.Compare(a.rgb, b.rgb) })
	exact := len(entries) <= maxColors
	var chosen []entry
	if exact {
		chosen = entries
		slices.SortStableFunc(chosen, func(a, b entry) int { return cmp.Compare(b.n, a.n) })
	} else {
		chosen = medianCut(entries, maxColors)
	}
	p := palette{
		colors: make(color.Palette, 1, len(chosen)+1),
		rgb:    make([][3]int32, len(chosen)),
	}
	p.colors[0] = color.RGBA{}
	for i, e := range chosen {
		r, g, b := uint8(e.rgb>>16), uint8(e.rgb>>8), uint8(e.rgb)
		p.colors = append(p.colors, color.RGBA{r, g, b, 255})
		p.rgb[i] = [3]int32{int32(r), int32(g), int32(b)}
	}
	if exact {
		p.exact = make(map[uint32]uint8, len(chosen))
		for i, e := range chosen {
			p.exact[e.rgb] = uint8(i + 1)
		}
	}
	return p
}

// box is a run entries[lo:hi] of a median cut and the weighted squared error
// of its colours around their mean.
type box struct {
	lo, hi int
	sse    float64
}

// medianCut reduces entries, sorted by colour, to at most n colours by
// repeatedly splitting the box with the largest error at the weighted median
// of its widest channel. Each colour is the weighted mean of its box, most
// popular first.
func medianCut(entries []entry, n int) []entry {
	boxes := []box{{0, len(entries), boxError(entries)}}
	for len(boxes) < n {
		best := -1
		for i, b := range boxes {
			if b.hi-b.lo >= 2 && b.sse > 0 && (best < 0 || b.sse > boxes[best].sse) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		b := boxes[best]
		part := entries[b.lo:b.hi]
		shift := widestChannel(part)
		slices.SortStableFunc(part, func(x, y entry) int {
			return cmp.Compare(x.rgb>>shift&0xff, y.rgb>>shift&0xff)
		})
		total := 0
		for _, e := range part {
			total += e.n
		}
		mid, run := 0, 0
		for mid < len(part)-1 {
			run += part[mid].n
			mid++
			if 2*run >= total {
				break
			}
		}
		boxes[best] = box{b.lo, b.lo + mid, boxError(entries[b.lo : b.lo+mid])}
		boxes = append(boxes, box{b.lo + mid, b.hi, boxError(entries[b.lo+mid : b.hi])})
	}
	out := make([]entry, len(boxes))
	for i, b := range boxes {
		out[i] = boxMean(entries[b.lo:b.hi])
	}
	slices.SortStableFunc(out, func(a, b entry) int {
		if c := cmp.Compare(b.n, a.n); c != 0 {
			return c
		}
		return cmp.Compare(a.rgb, b.rgb)
	})
	return out
}

// boxMean returns the weighted mean colour of part and its total weight.
func boxMean(part []entry) entry {
	var sum [3]int
	total := 0
	for _, e := range part {
		sum[0] += int(e.rgb>>16&0xff) * e.n
		sum[1] += int(e.rgb>>8&0xff) * e.n
		sum[2] += int(e.rgb&0xff) * e.n
		total += e.n
	}
	var c uint32
	for _, s := range sum {
		c = c<<8 | uint32((s+total/2)/total)
	}
	return entry{c, total}
}

// boxError returns the weighted squared distance of the colours of part from
// their mean.
func boxError(part []entry) float64 {
	mean := boxMean(part).rgb
	mr, mg, mb := int(mean>>16&0xff), int(mean>>8&0xff), int(mean&0xff)
	var sse float64
	for _, e := range part {
		dr, dg, db := int(e.rgb>>16&0xff)-mr, int(e.rgb>>8&0xff)-mg, int(e.rgb&0xff)-mb
		sse += float64(e.n) * float64(dr*dr+dg*dg+db*db)
	}
	return sse
}

// widestChannel returns the bit shift (16, 8 or 0) of the channel of part
// with the widest range (green first on ties).
func widestChannel(part []entry) uint {
	lo, hi := [3]int{255, 255, 255}, [3]int{}
	for _, e := range part {
		for i, shift := range [3]uint{16, 8, 0} {
			v := int(e.rgb >> shift & 0xff)
			lo[i], hi[i] = min(lo[i], v), max(hi[i], v)
		}
	}
	rr, rg, rb := hi[0]-lo[0], hi[1]-lo[1], hi[2]-lo[2]
	switch {
	case rg >= rr && rg >= rb:
		return 8
	case rr >= rb:
		return 16
	default:
		return 0
	}
}

// mapper maps pixels to palette indices.
type mapper struct {
	pal   palette
	cache map[uint32]uint8
}

func newMapper(pal palette) *mapper {
	m := &mapper{pal: pal, cache: pal.exact}
	if m.cache == nil {
		m.cache = make(map[uint32]uint8)
	}
	return m
}

// index returns the palette index of every pixel of pix, a block width
// pixels wide. With dither (and a quantised palette) the colour error of a
// pixel is diffused to its neighbours.
func (m *mapper) index(pix []uint32, width int, dither bool) []uint8 {
	if dither && m.pal.exact == nil {
		return m.dither(pix, width)
	}
	out := make([]uint8, len(pix))
	var lastColor uint32
	var lastIndex uint8
	for i, c := range pix {
		if i > 0 && c == lastColor {
			out[i] = lastIndex
			continue
		}
		idx, ok := m.cache[c]
		if !ok {
			idx = m.nearest(int32(c>>16&0xff), int32(c>>8&0xff), int32(c&0xff))
			m.cache[c] = idx
		}
		out[i], lastColor, lastIndex = idx, c, idx
	}
	return out
}

// nearest returns the index of the palette colour closest to r, g, b; the
// first of equally close ones.
func (m *mapper) nearest(r, g, b int32) uint8 {
	best, bestDist := 0, int32(1<<31-1)
	for i, c := range m.pal.rgb {
		dr, dg, db := r-c[0], g-c[1], b-c[2]
		if d := dr*dr + dg*dg + db*db; d < bestDist {
			best, bestDist = i, d
		}
	}
	return uint8(best + 1)
}

// dither maps pix with Floyd-Steinberg error diffusion, scanning left to
// right. Errors are kept in sixteenths.
func (m *mapper) dither(pix []uint32, width int) []uint8 {
	out := make([]uint8, len(pix))
	cur := make([][3]int32, width+2) // index x+1 holds column x
	next := make([][3]int32, width+2)
	for y := 0; y*width < len(pix); y++ {
		for x := range width {
			c := pix[y*width+x]
			var adj [3]int32
			for ch, shift := range [3]uint{16, 8, 0} {
				v := int32(c>>shift&0xff) + floorDiv(cur[x+1][ch]+8, 16)
				adj[ch] = max(0, min(255, v))
			}
			idx := m.nearest(adj[0], adj[1], adj[2])
			out[y*width+x] = idx
			pc := m.pal.rgb[idx-1]
			for ch := range adj {
				e := adj[ch] - pc[ch]
				cur[x+2][ch] += e * 7
				next[x][ch] += e * 3
				next[x+1][ch] += e * 5
				next[x+2][ch] += e
			}
		}
		cur, next = next, cur
		clear(next)
	}
	return out
}

// floorDiv returns a/b rounded down, for b > 0.
func floorDiv(a, b int32) int32 {
	q := a / b
	if a%b < 0 {
		q--
	}
	return q
}

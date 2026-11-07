package sprite

// Shade coverages for ░ ▒ ▓: 25%, 50% and 75% of fg blended over the
// pixels already in the cell.
const (
	shadeLight  = 64
	shadeMedium = 128
	shadeDark   = 191
)

// eighths returns n/8 of an axis of length l in whole pixels, at least one
// pixel for n > 0.
func eighths(l, n int) int {
	return min(l, max(1, (l*n+4)/8))
}

// block draws U+2580..U+259F.
func (c *canvas) block(r rune) {
	w, h := c.w, c.h
	xm, ym := (w+1)/2, (h+1)/2 // the half boundaries
	switch {
	case r == 0x2580: // ▀
		c.fill(0, 0, w, ym)
	case r >= 0x2581 && r <= 0x2588: // ▁ .. █ lower n/8
		n := int(r - 0x2580)
		c.fill(0, min(h-1, (h*(8-n)+4)/8), w, h)
	case r >= 0x2589 && r <= 0x258F: // ▉ .. ▏ left (0x2590-r)/8
		c.fill(0, 0, eighths(w, int(0x2590-r)), h)
	case r == 0x2590: // ▐
		c.fill(xm, 0, w, h)
	case r >= 0x2591 && r <= 0x2593: // ░ ▒ ▓
		c.fillCov(0, 0, w, h, [...]uint32{shadeLight, shadeMedium, shadeDark}[r-0x2591])
	case r == 0x2594: // ▔
		c.fill(0, 0, w, eighths(h, 1))
	case r == 0x2595: // ▕
		c.fill(min(w-1, (w*7+4)/8), 0, w, h)
	default: // 0x2596..0x259F quadrants
		q := quadrants[r-0x2596]
		if q&upperLeft != 0 {
			c.fill(0, 0, xm, ym)
		}
		if q&upperRight != 0 {
			c.fill(xm, 0, w, ym)
		}
		if q&lowerLeft != 0 {
			c.fill(0, ym, xm, h)
		}
		if q&lowerRight != 0 {
			c.fill(xm, ym, w, h)
		}
	}
}

const (
	upperLeft = 1 << iota
	upperRight
	lowerLeft
	lowerRight
)

// quadrants lists the quadrants filled by U+2596..U+259F (▖ ▗ ▘ ▙ ▚ ▛ ▜ ▝ ▞ ▟).
var quadrants = [...]uint8{
	lowerLeft,
	lowerRight,
	upperLeft,
	upperLeft | lowerLeft | lowerRight,
	upperLeft | lowerRight,
	upperLeft | upperRight | lowerLeft,
	upperLeft | upperRight | lowerRight,
	upperRight,
	upperRight | lowerLeft,
	upperRight | lowerLeft | lowerRight,
}

package sprite

// brailleDots gives the (column, row) in the 2x4 dot grid of each pattern
// bit: bit 0 is dot 1 ... bit 7 is dot 8.
var brailleDots = [8][2]int{{0, 0}, {0, 1}, {0, 2}, {1, 0}, {1, 1}, {1, 2}, {0, 3}, {1, 3}}

// braille draws U+2800..U+28FF: a filled disc for each set dot, centred in
// its grid slot, with radius 0.32 of the smaller slot pitch. Geometry is in
// eighths of a pixel; a dot always inks at least the pixel under its centre.
func (c *canvas) braille(r rune) {
	bits := uint(r - 0x2800)
	pitchX, pitchY := 4*c.w, 2*c.h // slot size in eighths of a pixel
	rad := min(pitchX, pitchY) * 8 / 25
	for b, d := range brailleDots {
		if bits&(1<<uint(b)) == 0 {
			continue
		}
		cx, cy := (2*d[0]+1)*pitchX/2, (2*d[1]+1)*pitchY/2
		c.set(cx/8, cy/8, 255)
		for y := max(0, (cy-rad)/8); y <= min(c.h-1, (cy+rad)/8); y++ {
			for x := max(0, (cx-rad)/8); x <= min(c.w-1, (cx+rad)/8); x++ {
				dx, dy := 8*x+4-cx, 8*y+4-cy
				if dx*dx+dy*dy <= rad*rad {
					c.set(x, y, 255)
				}
			}
		}
	}
}

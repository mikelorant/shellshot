package emoji

// CBDT image formats with PNG data (all values big-endian):
//
//	17: smallGlyphMetrics (5 bytes: u8 height, u8 width, i8 bearingX,
//	    i8 bearingY, u8 advance), u32 dataLen, PNG bytes.
//	18: bigGlyphMetrics (8 bytes: height, width, horiBearingX, horiBearingY,
//	    horiAdvance, vertBearingX, vertBearingY, vertAdvance), u32 dataLen,
//	    PNG bytes.
//	19: u32 dataLen, PNG bytes; the metrics live in the index subtable
//	    (index formats 2 and 5, which are unsupported), so they read as 0.

// bitmapGlyph is one PNG bitmap with its horizontal metrics in pixels.
type bitmapGlyph struct {
	width, height      int
	bearingX, bearingY int
	advance            int
	png                []byte
}

func (r *reader) u8(off int) int {
	if off < 0 || off+1 > len(r.b) {
		r.err = errTruncated
		return 0
	}
	return int(r.b[off])
}

func (r *reader) i8(off int) int { return int(int8(r.u8(off))) }

// find returns the index subtable of s that covers gid.
func (s *strike) find(gid uint16) (indexSub, bool) {
	for _, sub := range s.subs {
		if sub.first <= gid && gid <= sub.last {
			return sub, true
		}
	}
	return indexSub{}, false
}

// glyph returns the bitmap of gid in strike s. ok is false when the strike
// has no bitmap for gid; err wraps ErrBitmap when the data is malformed.
func (b *bitmapTables) glyph(s *strike, gid uint16) (g bitmapGlyph, ok bool, err error) {
	sub, found := s.find(gid)
	if !found {
		return g, false, nil
	}
	i := int(gid - sub.first)
	c := reader{b: b.cblc}
	var start, end int
	if sub.format == 1 {
		start, end = c.u32(sub.offsets+4*i), c.u32(sub.offsets+4*i+4)
	} else {
		start, end = c.u16(sub.offsets+2*i), c.u16(sub.offsets+2*i+2)
	}
	if c.err != nil {
		return g, false, bitmapError("glyph %d offsets: %v", gid, c.err)
	}
	if end == start {
		return g, false, nil
	}
	from, to := sub.dataOffset+start, sub.dataOffset+end
	if start < 0 || end < start || from < 4 || to > len(b.cbdt) {
		return g, false, bitmapError("glyph %d data %d-%d lies outside CBDT", gid, from, to)
	}
	g, err = parseImage(sub.imageFormat, b.cbdt[from:to])
	if err != nil {
		return g, false, bitmapError("glyph %d: %v", gid, err)
	}
	return g, true, nil
}

// parseImage reads the metrics and PNG of one glyph record of the given
// image format.
func parseImage(format int, data []byte) (g bitmapGlyph, err error) {
	r := reader{b: data}
	lenAt := 0
	switch format {
	case 17:
		g.height, g.width, g.bearingX, g.bearingY, g.advance = r.u8(0), r.u8(1), r.i8(2), r.i8(3), r.u8(4)
		lenAt = 5
	case 18:
		g.height, g.width, g.bearingX, g.bearingY, g.advance = r.u8(0), r.u8(1), r.i8(2), r.i8(3), r.u8(4)
		lenAt = 8
	}
	n := r.u32(lenAt)
	if r.err != nil {
		return g, r.err
	}
	if n < 0 || n > len(data)-lenAt-4 {
		return g, errTruncated
	}
	g.png = data[lenAt+4 : lenAt+4+n]
	return g, nil
}

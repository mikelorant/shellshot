package emoji

import (
	"errors"
	"fmt"
)

// CBLC (colour bitmap location) and CBDT (colour bitmap data) hold the
// PNG strikes of a bitmap colour font such as Apple Color Emoji or Noto
// Color Emoji. The facts this package relies on, from the OpenType spec
// (all values big-endian):
//
//	CBLC header: u16 majorVersion (2 or 3), u16 minorVersion, u32 numSizes,
//	  then numSizes BitmapSize records of 48 bytes.
//	BitmapSize: u32 indexSubTableArrayOffset (from the start of CBLC),
//	  u32 indexTablesSize, u32 numberOfIndexSubTables, u32 colorRef,
//	  2 x 12-byte SbitLineMetrics (hori, vert), u16 startGlyphIndex,
//	  u16 endGlyphIndex, u8 ppemX, u8 ppemY (at +44, +45), u8 bitDepth,
//	  i8 flags.
//	IndexSubTableArray: per subtable 8 bytes: u16 firstGlyphIndex,
//	  u16 lastGlyphIndex, u32 additionalOffsetToIndexSubtable (from the start
//	  of the array).
//	IndexSubHeader: u16 indexFormat, u16 imageFormat, u32 imageDataOffset
//	  (into CBDT). Index format 1 continues with u32 offsets, format 3 with
//	  u16 offsets: last-first+2 of them, so that glyph first+i occupies
//	  CBDT bytes imageDataOffset+offset[i] up to imageDataOffset+offset[i+1];
//	  an empty range means the strike has no bitmap for that glyph.
//	CBDT header: u16 majorVersion (2 or 3), u16 minorVersion; image data
//	  follows (formats in cbdt.go).
//
// Index formats 2, 4 and 5 and the non-PNG image formats (1, 2, 5, 6, 7, 8,
// 9) are rejected, as no colour emoji font uses them with PNG data.

// ErrBitmap is wrapped by every error about malformed or unsupported CBLC
// and CBDT data.
var ErrBitmap = errors.New("emoji: bad CBLC/CBDT bitmap data")

const (
	cblcHeaderSize     = 8
	bitmapSizeSize     = 48
	bitmapSizePpemY    = 45
	subTableArrayEntry = 8
	indexSubHeaderSize = 8
)

// indexSub is one IndexSubTable: the glyphs first..last, whose bitmaps are
// located by an offset array in CBLC.
type indexSub struct {
	first, last uint16
	format      int // index format: 1 (u32 offsets) or 3 (u16 offsets)
	imageFormat int // CBDT image format: 17, 18 or 19
	dataOffset  int // position in CBDT of the data the offsets are relative to
	offsets     int // position in CBLC of the offset array
}

// strike is the set of bitmaps of one pixel size.
type strike struct {
	ppem int
	subs []indexSub
}

// bitmapTables is the parsed CBLC index and the CBDT data it points into.
type bitmapTables struct {
	cblc, cbdt []byte
	strikes    []strike
}

func bitmapError(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrBitmap, fmt.Sprintf(format, args...))
}

// parseBitmaps reads the strike and index subtable records of CBLC and
// checks that every offset array lies inside CBLC. Offsets into CBDT are
// checked when a glyph is looked up.
func parseBitmaps(t tables) (*bitmapTables, error) {
	cblc, cbdt := t["CBLC"], t["CBDT"]
	if cblc == nil || cbdt == nil {
		return nil, bitmapError("font has no CBLC/CBDT tables")
	}
	c := reader{b: cblc}
	if v := c.u16(0); c.err == nil && v != 2 && v != 3 {
		return nil, bitmapError("unsupported CBLC version %d", v)
	}
	d := reader{b: cbdt}
	if v := d.u16(0); d.err == nil && v != 2 && v != 3 {
		return nil, bitmapError("unsupported CBDT version %d", v)
	}
	numSizes := c.u32(4)
	d.u16(2) // CBDT minor version: only checks that the 4-byte header is present
	if c.err != nil || d.err != nil {
		return nil, bitmapError("header: %v", errTruncated)
	}
	if numSizes < 0 || numSizes > (len(cblc)-cblcHeaderSize)/bitmapSizeSize {
		return nil, bitmapError("%d BitmapSize records run past the table", numSizes)
	}

	b := &bitmapTables{cblc: cblc, cbdt: cbdt, strikes: make([]strike, 0, numSizes)}
	for i := range numSizes {
		rec := cblcHeaderSize + bitmapSizeSize*i
		arrayOff, numSubs := c.u32(rec), c.u32(rec+8)
		if numSubs < 0 || arrayOff < 0 || numSubs > len(cblc)/subTableArrayEntry ||
			arrayOff+subTableArrayEntry*numSubs > len(cblc) {
			return nil, bitmapError("strike %d: index subtable array lies outside the table", i)
		}
		s := strike{ppem: int(cblc[rec+bitmapSizePpemY])}
		for j := range numSubs {
			sub, err := readIndexSub(&c, arrayOff, j)
			if err != nil {
				return nil, bitmapError("strike %d subtable %d: %v", i, j, err)
			}
			s.subs = append(s.subs, sub)
		}
		b.strikes = append(b.strikes, s)
	}
	return b, nil
}

// readIndexSub reads entry j of the IndexSubTableArray at arrayOff and the
// IndexSubHeader it points to.
func readIndexSub(c *reader, arrayOff, j int) (indexSub, error) {
	entry := arrayOff + subTableArrayEntry*j
	sub := indexSub{first: uint16(c.u16(entry)), last: uint16(c.u16(entry + 2))}
	hdr := arrayOff + c.u32(entry+4)
	sub.format, sub.imageFormat = c.u16(hdr), c.u16(hdr+2)
	sub.dataOffset = c.u32(hdr + 4)
	sub.offsets = hdr + indexSubHeaderSize
	if c.err != nil {
		return sub, c.err
	}
	if sub.first > sub.last {
		return sub, fmt.Errorf("glyph range %d-%d is reversed", sub.first, sub.last)
	}
	width := 0
	switch sub.format {
	case 1:
		width = 4
	case 3:
		width = 2
	default:
		return sub, fmt.Errorf("unsupported index format %d", sub.format)
	}
	switch sub.imageFormat {
	case 17, 18, 19:
	default:
		return sub, fmt.Errorf("unsupported image format %d", sub.imageFormat)
	}
	if sub.offsets+width*(int(sub.last-sub.first)+2) > len(c.b) {
		return sub, fmt.Errorf("offset array runs past the table: %w", errTruncated)
	}
	return sub, nil
}

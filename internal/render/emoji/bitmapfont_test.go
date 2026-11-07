package emoji

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

// wbuf appends big-endian integers to a byte slice.
type wbuf []byte

func (b *wbuf) u8(vs ...int) {
	for _, v := range vs {
		*b = append(*b, byte(v))
	}
}

func (b *wbuf) u16(vs ...int) {
	for _, v := range vs {
		*b = binary.BigEndian.AppendUint16(*b, uint16(v))
	}
}

func (b *wbuf) u32(vs ...int) {
	for _, v := range vs {
		*b = binary.BigEndian.AppendUint32(*b, uint32(v))
	}
}

// testSub describes one index subtable: glyphs first..last with the PNG of
// each present glyph; the others get an empty offset range.
type testSub struct {
	first, last uint16
	indexFormat int // 1 or 3
	imageFormat int // 17, 18 or 19
	png         map[uint16][]byte
}

type testStrike struct {
	ppem int
	subs []testSub
}

// imageRecord builds the CBDT record of one glyph in the given image format.
func imageRecord(format, w, h int, data []byte) []byte {
	var r wbuf
	switch format {
	case 17:
		r.u8(h, w, 0, h-1, w)
	case 18:
		r.u8(h, w, 0, h-1, w, 0, h-1, h)
	}
	r.u32(len(data))
	return append(r, data...)
}

// buildBitmapTables assembles CBLC and CBDT tables (version 3.0) from the
// strike descriptions. Every PNG is recorded with its own decoded size (0x0 if it does not decode).
func buildBitmapTables(t testing.TB, strikes []testStrike) (cblc, cbdt []byte) {
	t.Helper()
	data := wbuf{0, 3, 0, 0} // CBDT header: version 3.0
	var tail wbuf            // arrays and subtables, after the BitmapSize records
	base := cblcHeaderSize + bitmapSizeSize*len(strikes)
	var records wbuf
	for _, s := range strikes {
		arrayOff := base + len(tail)
		array := make(wbuf, 0, 8*len(s.subs))
		var subs wbuf
		for _, sub := range s.subs {
			array.u16(int(sub.first), int(sub.last))
			array.u32(8*len(s.subs) + len(subs))

			start := len(data)
			var offsets wbuf
			put := func(off int) {
				if sub.indexFormat == 1 {
					offsets.u32(off)
				} else {
					offsets.u16(off)
				}
			}
			for g := sub.first; g <= sub.last; g++ {
				put(len(data) - start)
				if p, ok := sub.png[g]; ok {
					cfg, _ := png.DecodeConfig(bytes.NewReader(p)) // garbage test data records as 0x0
					data = append(data, imageRecord(sub.imageFormat, cfg.Width, cfg.Height, p)...)
				}
			}
			put(len(data) - start)
			subs.u16(sub.indexFormat, sub.imageFormat)
			subs.u32(start)
			subs = append(subs, offsets...)
			for len(subs)%4 != 0 {
				subs.u8(0)
			}
		}
		tail = append(append(tail, array...), subs...)

		records.u32(arrayOff, len(array)+len(subs), len(s.subs), 0)
		records = append(records, make([]byte, 24)...) // hori and vert line metrics
		records.u16(0, 0xFFFF)                         // start, end glyph (unused)
		records.u8(s.ppem, s.ppem, 32, 1)              // ppemX, ppemY, bitDepth, flags
	}
	head := wbuf{0, 3, 0, 0}
	head.u32(len(strikes))
	return append(append(head, records...), tail...), data
}

// solidPNG encodes a w x h PNG whose pixels, row by row, are cols (repeated
// when there are fewer colours than pixels).
func solidPNG(t testing.TB, w, h int, cols ...color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for i := range w * h {
		img.SetNRGBA(i%w, i/w, cols[i%len(cols)])
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

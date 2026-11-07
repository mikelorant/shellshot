package apng

import (
	"encoding/binary"
	"hash/crc32"
	"image"
	"io"
)

// pngSignature is the eight-byte PNG file signature.
const pngSignature = "\x89PNG\r\n\x1a\n"

const (
	chunkIHDR = "IHDR"
	chunkIDAT = "IDAT"
	chunkIEND = "IEND"
	chunkACTL = "acTL"
	chunkFCTL = "fcTL"
	chunkFDAT = "fdAT"
)

// BlendOp is the APNG blend_op of a frame: how its pixels combine with the
// canvas.
type BlendOp uint8

const (
	// BlendSource replaces the canvas region with the frame's pixels,
	// including alpha.
	BlendSource BlendOp = 0
	// BlendOver alpha-composites the frame over the canvas region.
	BlendOver BlendOp = 1
)

// DisposeOp is the APNG dispose_op of a frame: how the canvas region is
// treated after the frame's delay has elapsed.
type DisposeOp uint8

const (
	// DisposeNone leaves the canvas as is.
	DisposeNone DisposeOp = 0
	// DisposeBackground clears the frame's region to transparent black.
	DisposeBackground DisposeOp = 1
	// DisposePrevious restores the frame's region to its content before the
	// frame was rendered.
	DisposePrevious DisposeOp = 2
)

// chunkWriter writes PNG chunks to w and remembers the first write error.
type chunkWriter struct {
	w   io.Writer
	err error
}

// writeRaw writes p verbatim.
func (c *chunkWriter) writeRaw(p []byte) {
	if c.err != nil {
		return
	}
	_, c.err = c.w.Write(p)
}

// writeChunk writes one chunk: length, type, data (the concatenation of
// parts) and the CRC-32 of type plus data.
func (c *chunkWriter) writeChunk(typ string, parts ...[]byte) {
	if c.err != nil {
		return
	}
	length := 0
	for _, p := range parts {
		length += len(p)
	}
	var head [8]byte
	binary.BigEndian.PutUint32(head[:4], uint32(length))
	copy(head[4:], typ)
	c.writeRaw(head[:])

	crc := crc32.ChecksumIEEE([]byte(typ))
	for _, p := range parts {
		c.writeRaw(p)
		crc = crc32.Update(crc, crc32.IEEETable, p)
	}
	var tail [4]byte
	binary.BigEndian.PutUint32(tail[:], crc)
	c.writeRaw(tail[:])
}

// ihdrData returns the IHDR payload for an 8-bit RGBA, non-interlaced image.
func ihdrData(width, height int) []byte {
	b := make([]byte, 13)
	binary.BigEndian.PutUint32(b[0:4], uint32(width))
	binary.BigEndian.PutUint32(b[4:8], uint32(height))
	b[8] = 8  // bit depth
	b[9] = 6  // colour type: truecolour with alpha
	b[10] = 0 // compression method
	b[11] = 0 // filter method
	b[12] = 0 // no interlace
	return b
}

// actlData returns the acTL payload.
func actlData(numFrames, numPlays uint32) []byte {
	b := make([]byte, 8)
	binary.BigEndian.PutUint32(b[0:4], numFrames)
	binary.BigEndian.PutUint32(b[4:8], numPlays)
	return b
}

// fctlData returns the fcTL payload for the frame region rect.
func fctlData(seq uint32, rect image.Rectangle, delayNum uint16, dispose DisposeOp, blend BlendOp) []byte {
	b := make([]byte, 26)
	binary.BigEndian.PutUint32(b[0:4], seq)
	binary.BigEndian.PutUint32(b[4:8], uint32(rect.Dx()))
	binary.BigEndian.PutUint32(b[8:12], uint32(rect.Dy()))
	binary.BigEndian.PutUint32(b[12:16], uint32(rect.Min.X))
	binary.BigEndian.PutUint32(b[16:20], uint32(rect.Min.Y))
	binary.BigEndian.PutUint16(b[20:22], delayNum)
	binary.BigEndian.PutUint16(b[22:24], delayDenominator)
	b[24] = byte(dispose)
	b[25] = byte(blend)
	return b
}

// seqData returns the 4-byte sequence number prefix of an fdAT chunk.
func seqData(seq uint32) []byte {
	b := make([]byte, 4)
	binary.BigEndian.PutUint32(b, seq)
	return b
}

package webp

import (
	"encoding/binary"
	"io"
)

const (
	fourCCRIFF = "RIFF"
	fourCCWEBP = "WEBP"
	fourCCVP8X = "VP8X"
	fourCCANIM = "ANIM"
	fourCCANMF = "ANMF"
	fourCCVP8L = "VP8L"
)

// Sizes of the fixed parts of the file.
const (
	riffHeaderSize  = 12 // "RIFF", size, "WEBP"
	chunkHeaderSize = 8  // fourCC, size
	vp8xPayloadSize = 10
	animPayloadSize = 6
	anmfHeaderSize  = 16 // frame fields before the VP8L sub-chunk
)

// VP8X flag bits.
const (
	vp8xFlagAlpha     = 1 << 4
	vp8xFlagAnimation = 1 << 1
)

// ANMF flag bits: bit 1 is the blending method (1 = do not blend), bit 0 is
// the disposal method (0 = none).
const anmfFlagNoBlend = 1 << 1

// padded returns n rounded up to an even length, the size a chunk payload
// occupies in the file.
func padded(n int) int { return n + n&1 }

// frameChunkSize is the size in the file of the ANMF chunk (header, frame
// fields and VP8L sub-chunk included) for a VP8L bitstream of n bytes.
func frameChunkSize(n int) int {
	return chunkHeaderSize + anmfHeaderSize + chunkHeaderSize + padded(n)
}

// putUint24 stores v, which must be below 1<<24, little endian.
func putUint24(b []byte, v uint32) {
	b[0], b[1], b[2] = byte(v), byte(v>>8), byte(v>>16)
}

// riffWriter writes chunks to w and remembers the first write error.
type riffWriter struct {
	w   io.Writer
	err error
}

func (r *riffWriter) write(p []byte) {
	if r.err != nil {
		return
	}
	_, r.err = r.w.Write(p)
}

// header writes a chunk header: the fourCC and the payload size.
func (r *riffWriter) header(fourCC string, size int) {
	var h [chunkHeaderSize]byte
	copy(h[:], fourCC)
	binary.LittleEndian.PutUint32(h[4:], uint32(size))
	r.write(h[:])
}

// vp8x writes the VP8X chunk for a canvas of width x height pixels.
func (r *riffWriter) vp8x(width, height int, alpha bool) {
	var p [vp8xPayloadSize]byte
	p[0] = vp8xFlagAnimation
	if alpha {
		p[0] |= vp8xFlagAlpha
	}
	putUint24(p[4:], uint32(width-1))
	putUint24(p[7:], uint32(height-1))
	r.header(fourCCVP8X, vp8xPayloadSize)
	r.write(p[:])
}

// anim writes the ANIM chunk: a transparent black background colour and the
// loop count.
func (r *riffWriter) anim(loops uint16) {
	var p [animPayloadSize]byte
	binary.LittleEndian.PutUint16(p[4:], loops)
	r.header(fourCCANIM, animPayloadSize)
	r.write(p[:])
}

// frame writes an ANMF chunk holding the VP8L bitstream vp8l for the
// rectangle at (x, y) of size width x height, shown for delayMillis. x and y
// must be even.
func (r *riffWriter) frame(x, y, width, height int, delayMillis uint32, vp8l []byte) {
	var p [anmfHeaderSize + chunkHeaderSize]byte
	putUint24(p[0:], uint32(x/2))
	putUint24(p[3:], uint32(y/2))
	putUint24(p[6:], uint32(width-1))
	putUint24(p[9:], uint32(height-1))
	putUint24(p[12:], delayMillis)
	p[15] = anmfFlagNoBlend
	copy(p[16:], fourCCVP8L)
	binary.LittleEndian.PutUint32(p[20:], uint32(len(vp8l)))
	r.header(fourCCANMF, anmfHeaderSize+chunkHeaderSize+padded(len(vp8l)))
	r.write(p[:])
	r.write(vp8l)
	if len(vp8l)&1 != 0 {
		r.write([]byte{0})
	}
}

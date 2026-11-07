package apng

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

// encodeFrameIDAT encodes img as an 8-bit RGBA PNG using image/png and
// returns the concatenated IDAT payloads (one zlib stream).
func encodeFrameIDAT(img image.Image, width, height int, level png.CompressionLevel) ([]byte, error) {
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: level}
	if err := enc.Encode(&buf, toNRGBA(img, width, height)); err != nil {
		return nil, fmt.Errorf("apng: encode frame: %w", err)
	}
	return extractIDAT(buf.Bytes(), width, height)
}

// alwaysAlpha wraps an image so image/png's opacity probe (the Opaque method
// it looks for) reports false. Without it image/png writes fully opaque
// images as colour type 2 (RGB) and others as colour type 6 (RGBA), but all
// APNG frames must share the IHDR's colour type. extractIDAT verifies the
// resulting IHDR, so a change in image/png behaviour fails loudly.
type alwaysAlpha struct{ *image.NRGBA }

func (alwaysAlpha) Opaque() bool { return false }

// toNRGBA returns img converted to 8-bit non-premultiplied RGBA with origin
// (0,0), wrapped so image/png always chooses colour type 6.
func toNRGBA(img image.Image, width, height int) image.Image {
	dst := image.NewNRGBA(image.Rect(0, 0, width, height))
	draw.Draw(dst, dst.Bounds(), img, img.Bounds().Min, draw.Src)
	return alwaysAlpha{dst}
}

// extractIDAT walks the chunks of a PNG produced by image/png, checks that
// its IHDR matches the expected 8-bit RGBA non-interlaced layout, and returns
// the concatenated IDAT payloads.
func extractIDAT(pngData []byte, width, height int) ([]byte, error) {
	if !bytes.HasPrefix(pngData, []byte(pngSignature)) {
		return nil, errors.New("apng: frame encoder produced no PNG signature")
	}
	rest := pngData[len(pngSignature):]
	var (
		idat    []byte
		sawIHDR bool
		sawIEND bool
	)
	for len(rest) > 0 && !sawIEND {
		if len(rest) < 12 {
			return nil, errors.New("apng: truncated PNG chunk from frame encoder")
		}
		length := binary.BigEndian.Uint32(rest[:4])
		if uint64(length)+12 > uint64(len(rest)) {
			return nil, errors.New("apng: truncated PNG chunk from frame encoder")
		}
		typ := string(rest[4:8])
		data := rest[8 : 8+length]
		rest = rest[12+length:]

		switch typ {
		case chunkIHDR:
			if !bytes.Equal(data, ihdrData(width, height)) {
				return nil, errors.New("apng: frame encoder produced unexpected IHDR")
			}
			sawIHDR = true
		case chunkIDAT:
			idat = append(idat, data...)
		case chunkIEND:
			sawIEND = true
		}
	}
	if !sawIHDR || !sawIEND || len(idat) == 0 {
		return nil, errors.New("apng: incomplete PNG from frame encoder")
	}
	return idat, nil
}

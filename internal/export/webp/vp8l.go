package webp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"

	"github.com/HugoSmits86/nativewebp"
)

// maxFrameDimension is the largest width or height of a VP8L image (14 bits).
const maxFrameDimension = 1 << 14

// encodeVP8L returns the VP8L bitstream of img, a lossless encoding of its
// pixels. nativewebp writes only whole files, so the bitstream is taken out
// of the plain "RIFF size WEBP VP8L size bitstream" file it produces.
func encodeVP8L(img *image.NRGBA) (stream []byte, err error) {
	defer func() {
		if r := recover(); r != nil {
			stream, err = nil, fmt.Errorf("webp: encode frame: %v", r)
		}
	}()
	var file bytes.Buffer
	if err := nativewebp.Encode(&file, img, nil); err != nil {
		return nil, fmt.Errorf("webp: encode frame: %w", err)
	}
	return vp8lPayload(file.Bytes())
}

// vp8lPayload returns the payload of the VP8L chunk of a simple lossless
// WebP file.
func vp8lPayload(file []byte) ([]byte, error) {
	const start = riffHeaderSize + chunkHeaderSize
	if len(file) < start || string(file[8:12]) != fourCCWEBP || string(file[12:16]) != fourCCVP8L {
		return nil, errors.New("webp: encoder output is not a simple VP8L file")
	}
	n := int(binary.LittleEndian.Uint32(file[16:20]))
	if n > len(file)-start {
		return nil, errors.New("webp: encoder output is truncated")
	}
	return file[start : start+n], nil
}

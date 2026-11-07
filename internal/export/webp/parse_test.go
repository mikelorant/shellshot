package webp

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"

	xwebp "golang.org/x/image/webp"
)

// parsedFrame is one ANMF chunk read back from a file.
type parsedFrame struct {
	x, y, w, h  int
	delayMillis uint32
	flags       byte
	vp8l        []byte
}

// parsedFile is a file read back by parseWebP.
type parsedFile struct {
	vp8xFlags     byte
	width, height int
	background    [4]byte
	loops         uint16
	frames        []parsedFrame
}

func le24(b []byte) int { return int(b[0]) | int(b[1])<<8 | int(b[2])<<16 }

// parseWebP reads an animated WebP strictly: chunk sizes, even padding with
// zero bytes, chunk order and the RIFF size are all checked.
func parseWebP(b []byte) (parsedFile, error) {
	var f parsedFile
	if len(b) < 12 || string(b[:4]) != "RIFF" || string(b[8:12]) != "WEBP" {
		return f, fmt.Errorf("not a RIFF WEBP file")
	}
	if got := binary.LittleEndian.Uint32(b[4:]); int(got) != len(b)-8 {
		return f, fmt.Errorf("RIFF size %d, file has %d bytes after the size field", got, len(b)-8)
	}
	if len(b)%2 != 0 {
		return f, fmt.Errorf("odd file length %d", len(b))
	}
	rest := b[12:]
	var order []string
	for len(rest) > 0 {
		if len(rest) < 8 {
			return f, fmt.Errorf("truncated chunk header")
		}
		fourCC := string(rest[:4])
		size := int(binary.LittleEndian.Uint32(rest[4:]))
		total := 8 + size + size&1
		if total > len(rest) {
			return f, fmt.Errorf("%s chunk of %d bytes overruns the file", fourCC, size)
		}
		payload := rest[8 : 8+size]
		if size&1 == 1 && rest[8+size] != 0 {
			return f, fmt.Errorf("%s chunk padding is not zero", fourCC)
		}
		rest = rest[total:]
		order = append(order, fourCC)
		switch fourCC {
		case "VP8X":
			if size != 10 {
				return f, fmt.Errorf("VP8X size %d", size)
			}
			f.vp8xFlags = payload[0]
			if payload[1]|payload[2]|payload[3] != 0 {
				return f, fmt.Errorf("VP8X reserved bytes not zero")
			}
			f.width, f.height = le24(payload[4:])+1, le24(payload[7:])+1
		case "ANIM":
			if size != 6 {
				return f, fmt.Errorf("ANIM size %d", size)
			}
			copy(f.background[:], payload[:4])
			f.loops = binary.LittleEndian.Uint16(payload[4:])
		case "ANMF":
			if size < 16+8 {
				return f, fmt.Errorf("ANMF size %d", size)
			}
			fr := parsedFrame{
				x: 2 * le24(payload[0:]), y: 2 * le24(payload[3:]),
				w: le24(payload[6:]) + 1, h: le24(payload[9:]) + 1,
				delayMillis: uint32(le24(payload[12:])), flags: payload[15],
			}
			sub := payload[16:]
			if string(sub[:4]) != "VP8L" {
				return f, fmt.Errorf("ANMF holds %q, want VP8L", sub[:4])
			}
			n := int(binary.LittleEndian.Uint32(sub[4:]))
			if 8+n+n&1 != len(sub) {
				return f, fmt.Errorf("VP8L size %d does not fill the ANMF (%d bytes left)", n, len(sub)-8)
			}
			if n&1 == 1 && sub[8+n] != 0 {
				return f, fmt.Errorf("VP8L padding is not zero")
			}
			fr.vp8l = sub[8 : 8+n]
			f.frames = append(f.frames, fr)
		default:
			return f, fmt.Errorf("unexpected chunk %q", fourCC)
		}
	}
	if len(order) < 3 || order[0] != "VP8X" || order[1] != "ANIM" {
		return f, fmt.Errorf("chunk order %v, want VP8X ANIM ANMF...", order)
	}
	for _, c := range order[2:] {
		if c != "ANMF" {
			return f, fmt.Errorf("chunk order %v, want VP8X ANIM ANMF...", order)
		}
	}
	return f, nil
}

// standalone wraps a VP8L bitstream in a RIFF header so a still-image
// decoder can read it.
func standalone(vp8l []byte) []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	pad := len(vp8l) & 1
	binary.Write(&b, binary.LittleEndian, uint32(4+8+len(vp8l)+pad))
	b.WriteString("WEBPVP8L")
	binary.Write(&b, binary.LittleEndian, uint32(len(vp8l)))
	b.Write(vp8l)
	if pad == 1 {
		b.WriteByte(0)
	}
	return b.Bytes()
}

// decodeFrames recomposites the file the way a player does for frames that
// do not blend and are not disposed: each frame's pixels replace the canvas
// under its rectangle. It returns the canvas after every frame, decoding each
// frame payload as a standalone WebP.
func decodeFrames(f parsedFile) ([]*image.NRGBA, error) {
	canvas := image.NewNRGBA(image.Rect(0, 0, f.width, f.height))
	var out []*image.NRGBA
	for i, fr := range f.frames {
		img, err := xwebp.Decode(bytes.NewReader(standalone(fr.vp8l)))
		if err != nil {
			return nil, fmt.Errorf("frame %d: %w", i, err)
		}
		if got := img.Bounds().Size(); got != image.Pt(fr.w, fr.h) {
			return nil, fmt.Errorf("frame %d: bitstream is %v, ANMF says %dx%d", i, got, fr.w, fr.h)
		}
		rect := image.Rect(fr.x, fr.y, fr.x+fr.w, fr.y+fr.h)
		if !rect.In(canvas.Bounds()) {
			return nil, fmt.Errorf("frame %d: rectangle %v outside canvas", i, rect)
		}
		draw.Draw(canvas, rect, img, img.Bounds().Min, draw.Src)
		snap := image.NewNRGBA(canvas.Bounds())
		copy(snap.Pix, canvas.Pix)
		out = append(out, snap)
	}
	return out, nil
}

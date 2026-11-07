package emoji

import (
	"fmt"
	"image/color"
)

// layer is one coloured outline of a COLR v0 glyph.
type layer struct {
	glyph uint16 // glyph id of the outline
	color color.NRGBA
}

// foregroundPalette is the COLR palette index meaning "the text colour".
const foregroundPalette = 0xFFFF

// parseColor reads the COLR v0 base glyph and layer records and the first
// CPAL palette. It returns each layered glyph's layers in drawing order
// (bottom first). Layers that use the foreground colour are dropped: an
// emoji has no text colour.
func parseColor(t tables) (map[uint16][]layer, error) {
	colr, cpal := t["COLR"], t["CPAL"]
	if colr == nil || cpal == nil {
		return nil, fmt.Errorf("emoji: font has no COLR/CPAL tables")
	}
	c := reader{b: colr}
	if v := c.u16(0); v != 0 {
		return nil, fmt.Errorf("emoji: unsupported COLR version %d (only version 0)", v)
	}
	numBase, baseOff, layerOff, numLayers := c.u16(2), c.u32(4), c.u32(8), c.u16(12)

	p := reader{b: cpal}
	numEntries, numRecords, recordsOff := p.u16(2), p.u16(6), p.u32(8)
	if p.u16(4) < 1 || p.err != nil {
		return nil, fmt.Errorf("emoji: CPAL has no palette")
	}
	first := p.u16(12) // index of palette 0's first colour record
	palette := make([]color.NRGBA, numEntries)
	for i := range palette {
		if first+i >= numRecords {
			return nil, fmt.Errorf("emoji: CPAL palette runs past its colour records")
		}
		bgra := p.u32(recordsOff + 4*(first+i)) // colour records are stored B, G, R, A
		palette[i] = color.NRGBA{B: uint8(bgra >> 24), G: uint8(bgra >> 16), R: uint8(bgra >> 8), A: uint8(bgra)}
	}
	if p.err != nil {
		return nil, fmt.Errorf("emoji: CPAL: %w", p.err)
	}

	layers := make(map[uint16][]layer, numBase)
	for i := range numBase {
		o := baseOff + 6*i
		gid, firstLayer, n := uint16(c.u16(o)), c.u16(o+2), c.u16(o+4)
		ls := make([]layer, 0, n)
		for j := firstLayer; j < firstLayer+n; j++ {
			if j >= numLayers {
				return nil, fmt.Errorf("emoji: COLR glyph %d has a layer past the layer records", gid)
			}
			lo := layerOff + 4*j
			lg, pi := uint16(c.u16(lo)), c.u16(lo+2)
			switch {
			case pi == foregroundPalette:
			case pi >= len(palette):
				return nil, fmt.Errorf("emoji: COLR palette index %d out of range", pi)
			default:
				ls = append(ls, layer{glyph: lg, color: palette[pi]})
			}
		}
		layers[gid] = ls
	}
	if c.err != nil {
		return nil, fmt.Errorf("emoji: COLR: %w", c.err)
	}
	return layers, nil
}

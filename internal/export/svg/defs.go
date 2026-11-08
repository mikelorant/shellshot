package svg

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image/png"
	"math"
	"strconv"
	"strings"

	"golang.org/x/image/font/sfnt"

	"github.com/mikelorant/shellshot/internal/render/font"
	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/render/sprite"
)

// defs is the registry of the shared shapes a document defines once and
// references with <use>: sprite glyphs (s0, s1, ...), glyph outlines
// (g0, ...) and colour emoji images (e0, ...). Ids follow first use, so the
// output depends only on the order the builder is driven in.
type defs struct {
	items   []string // <defs> children in definition order
	sprites map[spriteKey]string
	glyphs  map[glyphKey]glyphDef
	emoji   map[emojiKey]string // "" for a cluster without an image
	nGlyphs int                 // glyph ids handed out
	nEmoji  int                 // emoji ids handed out
}

type spriteKey struct {
	r    rune
	w, h int
}

type glyphKey struct {
	r            rune
	bold, italic bool
	cells        int
}

type glyphDef struct {
	id      string
	advance float64 // pen advance in pixels
	primary bool
	ok      bool
}

type emojiKey struct {
	cluster string
	side    int
}

func newDefs() defs {
	return defs{
		sprites: make(map[spriteKey]string),
		glyphs:  make(map[glyphKey]glyphDef),
		emoji:   make(map[emojiKey]string),
	}
}

// markup returns the <defs> element, or "" when nothing was defined.
func (d *defs) markup() string {
	if len(d.items) == 0 {
		return ""
	}
	return "<defs>\n" + strings.Join(d.items, "\n") + "\n</defs>\n"
}

// sprite defines the glyph for r in a w x h cell and returns its id. ok is
// false for a rune sprite does not draw.
func (d *defs) sprite(r rune, w, h int) (id string, ok bool) {
	k := spriteKey{r, w, h}
	if id, ok := d.sprites[k]; ok {
		return id, true
	}
	rects, ok := sprite.Rects(r, w, h)
	if !ok {
		return "", false
	}
	id = "s" + strconv.Itoa(len(d.sprites))
	d.sprites[k] = id
	d.items = append(d.items, spriteMarkup(id, rects))
	return id, true
}

// spriteMarkup writes rects as one <path> per coverage level, in a <g> when
// there is more than one.
func spriteMarkup(id string, rects []sprite.Rect) string {
	var levels []uint8
	paths := map[uint8]*strings.Builder{}
	for _, r := range rects {
		p, ok := paths[r.A]
		if !ok {
			p = &strings.Builder{}
			paths[r.A] = p
			levels = append(levels, r.A)
		}
		w, h := strconv.Itoa(r.X1-r.X0), strconv.Itoa(r.Y1-r.Y0)
		fmt.Fprintf(p, "M%d %dh%sv%sh-%sz", r.X0, r.Y0, w, h, w)
	}
	path := func(a uint8, attr string) string {
		s := fmt.Sprintf(`<path%s d="%s"`, attr, paths[a].String())
		if a != 255 {
			s += fmt.Sprintf(` fill-opacity="%s"`, strconv.FormatFloat(float64(a)/255, 'f', 3, 64))
		}
		return s + "/>"
	}
	switch len(levels) {
	case 0:
		return fmt.Sprintf(`<path id="%s" d=""/>`, id)
	case 1:
		return path(levels[0], ` id="`+id+`"`)
	}
	var sb strings.Builder
	sb.WriteString(`<g id="` + id + `">`)
	for _, a := range levels {
		sb.WriteString(path(a, ""))
	}
	sb.WriteString("</g>")
	return sb.String()
}

// emojiImage defines the colour emoji image of cluster at side x side pixels
// and returns its id, or "" if no emoji font has the cluster.
func (d *defs) emojiImage(g Assets, cluster string, side int) string {
	k := emojiKey{cluster, side}
	if id, ok := d.emoji[k]; ok {
		return id
	}
	img, ok := g.Emoji(cluster, side)
	if !ok {
		d.emoji[k] = ""
		return ""
	}
	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, img); err != nil {
		d.emoji[k] = ""
		return ""
	}
	id := "e" + strconv.Itoa(d.nEmoji)
	d.nEmoji++
	d.emoji[k] = id
	d.items = append(d.items, fmt.Sprintf(`<image id="%s" width="%d" height="%d" href="data:image/png;base64,%s"/>`,
		id, side, side, base64.StdEncoding.EncodeToString(buf.Bytes())))
	return id
}

// glyph defines the outline of r as a path in the frame of a cell (origin at
// its top-left corner, y down) and returns it. A primary-family glyph sits
// where the raster renderer puts it: on the baseline, moved right by the
// layout's glyph offset. A fallback glyph is scaled and centred in the cells
// it spans, as font.FitFace and the raster fallback do.
//
// A Nerd Font icon (Outline.Icon) is the exception, again as in the raster
// renderer: it is scaled by font.IconScale (by height only, never above the
// font size), its origin is the left edge of the cell so the ink starts at
// the glyph's own left bearing and overflows to the right, and it is centred
// vertically. Nothing in the SVG clips a glyph to its cell, and glyphs are
// drawn after all backgrounds, so the overflow shows over the next cells.
// This applies wherever glyph is used, which is the "paths" text mode. A
// text mode that routes private-use icons to outlines must use glyph for
// them and so gets this placement without further work.
func (d *defs) glyph(a Assets, l raster.Layout, r rune, bold, italic bool, cells int) (glyphDef, error) {
	k := glyphKey{r, bold, italic, cells}
	if g, ok := d.glyphs[k]; ok {
		return g, nil
	}
	o, ok, err := a.Outline(r, bold, italic)
	if err != nil {
		return glyphDef{}, err
	}
	if !ok {
		d.glyphs[k] = glyphDef{}
		return glyphDef{}, nil
	}
	upem := float64(o.UnitsPerEm)
	scale := l.FontSize / upem
	ox, oy := l.GlyphOffsetX, float64(l.Ascent)
	if !o.Primary {
		minX, minY, maxX, maxY, any := bounds(o.Segments)
		switch {
		case any && o.Icon:
			scale *= font.IconScale((maxY-minY)*scale, float64(l.CellHeight))
			ox = 0
			oy = math.Round(math.FMA(-(minY+maxY)/2, scale, float64(l.CellHeight)/2))
		case any:
			budgetW := float64(cells * l.CellWidth)
			fit := math.Inf(1)
			for _, c := range [...]struct{ have, budget float64 }{
				{float64(o.Advance) * scale, budgetW},
				{(maxX - minX) * scale, budgetW},
				{(maxY - minY) * scale, float64(l.CellHeight)},
			} {
				if c.have > 0 {
					fit = math.Min(fit, c.budget/c.have)
				}
			}
			if !math.IsInf(fit, 1) {
				scale *= fit
			}
			ox = math.Round(math.FMA(-(minX+maxX)/2, scale, budgetW/2))
			oy = math.Round(math.FMA(-(minY+maxY)/2, scale, float64(l.CellHeight)/2))
		}
	}
	id := "g" + strconv.Itoa(d.nGlyphs)
	d.nGlyphs++
	g := glyphDef{id: id, advance: float64(o.Advance) * l.FontSize / upem, primary: o.Primary, ok: true}
	d.glyphs[k] = g
	d.items = append(d.items, fmt.Sprintf(`<path id="%s" d="%s"/>`, id, pathData(o.Segments, scale, ox, oy)))
	return g, nil
}

// bounds returns the box of all points of segs, in font units.
func bounds(segs sfnt.Segments) (minX, minY, maxX, maxY float64, any bool) {
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, s := range segs {
		for _, p := range s.Args[:argCount(s.Op)] {
			x, y := float64(p.X)/64, float64(p.Y)/64
			minX, maxX = math.Min(minX, x), math.Max(maxX, x)
			minY, maxY = math.Min(minY, y), math.Max(maxY, y)
			any = true
		}
	}
	return
}

func argCount(op sfnt.SegmentOp) int {
	switch op {
	case sfnt.SegmentOpMoveTo, sfnt.SegmentOpLineTo:
		return 1
	case sfnt.SegmentOpQuadTo:
		return 2
	}
	return 3
}

// pathData writes segs, in font units with y down from the baseline, as SVG
// path data: scaled by scale and moved so the glyph origin is at (ox, oy).
// Each scale-and-move is one explicit fused multiply-add: Go fuses x*y+z on
// arm64 but not on amd64, which changes the last bit and so, at a rounding
// tie, a printed digit. math.FMA gives every architecture the same bits.
func pathData(segs sfnt.Segments, scale, ox, oy float64) string {
	var sb strings.Builder
	open := false
	for _, s := range segs {
		if s.Op == sfnt.SegmentOpMoveTo && open {
			sb.WriteByte('z')
		}
		sb.WriteByte("MLQC"[int(s.Op)])
		for i, p := range s.Args[:argCount(s.Op)] {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(num(math.FMA(float64(p.X)/64, scale, ox)) + " " + num(math.FMA(float64(p.Y)/64, scale, oy)))
		}
		open = true
	}
	if open {
		sb.WriteByte('z')
	}
	return sb.String()
}

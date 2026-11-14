package svg

import (
	"image/color"
	"strconv"
	"strings"

	"github.com/mikelorant/shellshot/internal/render/raster"
	"github.com/mikelorant/shellshot/internal/render/sprite"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

// maxInnerSpaces is the longest stretch of blank cells a run of text spans
// without being split into two runs: a blank costs a coordinate in the run,
// a split costs another element.
const maxInnerSpaces = 3

// Builder turns screen rows into SVG elements. It keeps the shapes the rows
// share (sprites, glyph outlines, emoji images) in a registry that Defs
// writes out, so a document is built by calling Row for each row, then
// taking Defs. A Builder is not safe for concurrent use; its output depends
// only on the order of its calls.
type Builder struct {
	g         Geometry
	lay       raster.Layout
	mode      TextMode
	emojiMode EmojiMode
	defs      defs
}

// NewBuilder returns a Builder drawing with the geometry g, writing glyphs as
// mode says and emoji as emojiMode says.
func NewBuilder(g Geometry, mode TextMode, emojiMode EmojiMode) *Builder {
	return &Builder{g: g, lay: g.Layout(), mode: mode, emojiMode: emojiMode, defs: newDefs()}
}

// Defs returns the <defs> element with every shape the rows built so far
// reference, or "" if there are none.
func (b *Builder) Defs() string { return b.defs.markup() }

// use is a reference to a shared shape at a position.
type use struct {
	id   string
	x, y float64
	fg   color.RGBA
}

// Row returns the elements that draw row of s with the top-left corner of its
// first cell at (x0, y0): background rectangles, glyphs, then underlines and
// strikethroughs. The result ends in a newline and is empty for a blank row.
// The block cursor is part of the row it sits on; Cursor draws the others.
func (b *Builder) Row(s snapshot.Snapshot, row, x0, y0 int) (string, error) {
	cells := b.g.RowCells(s, row)
	var sb strings.Builder
	b.backgrounds(&sb, cells, x0, y0)
	if err := b.glyphs(&sb, cells, x0, y0); err != nil {
		return "", err
	}
	b.decorations(&sb, cells, x0, y0, func(c raster.RowCell) bool { return c.Underline }, b.lay.UnderlineTop)
	b.decorations(&sb, cells, x0, y0, func(c raster.RowCell) bool { return c.Strike }, b.lay.StrikeTop)
	if sb.Len() > 0 {
		sb.WriteByte('\n')
	}
	return sb.String(), nil
}

// Rect is a rectangle in pixels.
type Rect struct{ X, Y, W, H int }

// Cursor returns where the bar or underline cursor of s is drawn, with the
// top-left corner of the grid at (x0, y0). ok is false when there is no such
// cursor: it is hidden, the style is none, or the style is block, which Row
// draws as part of its cell.
func (b *Builder) Cursor(s snapshot.Snapshot, x0, y0 int) (r Rect, ok bool) {
	row, col, width, ok := b.g.CursorCell(s)
	if !ok {
		return Rect{}, false
	}
	l := b.lay
	x, y := x0+col*l.CellWidth, y0+row*l.CellHeight
	switch l.Cursor {
	case raster.CursorBar:
		return Rect{x, y, l.BarWidth, l.CellHeight}, true
	case raster.CursorUnderline:
		return Rect{x, y + l.CellHeight - l.Thickness, width * l.CellWidth, l.Thickness}, true
	}
	return Rect{}, false
}

// rectMarkup writes <rect> for r in fill colour c.
func rectMarkup(sb *strings.Builder, x, y, w, h int, c color.RGBA) {
	sb.WriteString(`<rect x="` + strconv.Itoa(x) + `" y="` + strconv.Itoa(y) +
		`" width="` + strconv.Itoa(w) + `" height="` + strconv.Itoa(h) + `" fill="` + hex(c) + `"/>`)
}

// backgrounds writes one rectangle for each run of adjacent cells with the
// same background that is not the theme background (the document paints that
// once).
func (b *Builder) backgrounds(sb *strings.Builder, cells []raster.RowCell, x0, y0 int) {
	l := b.lay
	for i := 0; i < len(cells); {
		c := cells[i]
		if c.Bg == l.Theme.Background {
			i++
			continue
		}
		end, j := c.Col+c.Width, i+1
		for j < len(cells) && cells[j].Bg == c.Bg && cells[j].Col == end {
			end += cells[j].Width
			j++
		}
		rectMarkup(sb, x0+c.Col*l.CellWidth, y0, (end-c.Col)*l.CellWidth, l.CellHeight, c.Bg)
		i = j
	}
}

// decorations writes one rectangle of the layout's stroke height, top pixels
// below the cell top, for each run of adjacent cells that has the decoration
// picked by has in the same colour.
func (b *Builder) decorations(sb *strings.Builder, cells []raster.RowCell, x0, y0 int, has func(raster.RowCell) bool, top int) {
	l := b.lay
	for i := 0; i < len(cells); {
		c := cells[i]
		if !has(c) {
			i++
			continue
		}
		end, j := c.Col+c.Width, i+1
		for j < len(cells) && has(cells[j]) && cells[j].Fg == c.Fg && cells[j].Col == end {
			end += cells[j].Width
			j++
		}
		rectMarkup(sb, x0+c.Col*l.CellWidth, y0+top, (end-c.Col)*l.CellWidth, l.Thickness, c.Fg)
		i = j
	}
}

// glyphs writes the shapes (sprites, emoji) and the text or outlines of the
// row's cells.
func (b *Builder) glyphs(sb *strings.Builder, cells []raster.RowCell, x0, y0 int) error {
	var (
		uses []use
		text = textRuns{fg: b.lay.Theme.Foreground}
	)
	l := b.lay
	for _, c := range cells {
		x := float64(x0 + c.Col*l.CellWidth)
		y := float64(y0)
		if c.Hidden || !raster.HasInk(c.Base) && len(c.DrawnMarks()) == 0 {
			text.blank(x + l.GlyphOffsetX)
			continue
		}
		if id, ok := b.sprite(c); ok {
			uses = append(uses, use{id, x, y, c.Fg})
			text.close()
			continue
		}
		if b.emojiMode == EmojiText && b.isEmoji(c) && !b.hasIcon(c) {
			text.add(c, x, l)
			continue
		}
		if id, side := b.emoji(c); id != "" {
			w := c.Width * l.CellWidth
			uses = append(uses, use{id, x + float64((w-side)/2), y + float64((l.CellHeight-side)/2), c.Fg})
			text.close()
			continue
		}
		if b.mode == TextPaths || b.iconOutline(c) {
			u, err := b.outlines(c, x, y)
			if err != nil {
				return err
			}
			uses = append(uses, u...)
			text.close()
			continue
		}
		text.add(c, x, l)
	}
	writeUses(sb, uses, l.Theme.Foreground)
	text.write(sb, y0+l.Ascent)
	return nil
}

// iconOutline reports whether text mode draws c as glyph outlines rather
// than <text>: its base rune is in a private use area (where Nerd Fonts keep
// their icons) and a font of the set has a glyph for it. A viewer without a
// Nerd Font would draw such a rune as a blank or a box, so the outline
// carries the icon. A private use rune no font has stays text, as does all
// other text, which the viewer can select and search.
func (b *Builder) iconOutline(c raster.RowCell) bool {
	return b.mode == TextText && b.hasIcon(c)
}

// hasIcon reports whether the base rune of c is a private use rune that a font
// of the set has a glyph for. Such a cell is an icon even if a variation
// selector follows, so text emoji mode leaves it to the outlines.
func (b *Builder) hasIcon(c raster.RowCell) bool {
	return isPrivateUse(c.Base) && b.g.Covers(c.Base, c.Bold, c.Italic)
}

// isPrivateUse reports whether r is in the Basic Multilingual Plane private
// use area (U+E000-U+F8FF) or in private use plane 15 or 16.
func isPrivateUse(r rune) bool {
	return r >= 0xE000 && r <= 0xF8FF || r >= 0xF0000 && r <= 0xFFFFD || r >= 0x100000 && r <= 0x10FFFD
}

// sprite returns the id of the procedural glyph that draws c, if it has one.
func (b *Builder) sprite(c raster.RowCell) (string, bool) {
	if len(c.Marks) != 0 || !raster.HasInk(c.Base) || !sprite.Handles(c.Base) {
		return "", false
	}
	return b.defs.sprite(c.Base, c.Width*b.lay.CellWidth, b.lay.CellHeight)
}

// isEmoji reports whether c may be an emoji. As in the raster renderer, a
// cluster that asks for emoji presentation or whose base rune no text font has
// is drawn as an emoji when an emoji font has it. In text emoji mode that
// cannot be asked of the emoji fonts, so the cluster is text.
func (b *Builder) isEmoji(c raster.RowCell) bool {
	return raster.HasInk(c.Base) && (c.EmojiSequence || !b.g.Covers(c.Base, c.Bold, c.Italic))
}

// emoji returns the id of the colour emoji image that draws c and its side in
// pixels, or "" if c is drawn as text or outlines (it is not an emoji, no
// emoji font has it, or emoji are text).
func (b *Builder) emoji(c raster.RowCell) (id string, side int) {
	if b.emojiMode != EmojiImage || !b.isEmoji(c) {
		return "", 0
	}
	side = min(c.Width*b.lay.CellWidth, b.lay.CellHeight)
	return b.defs.emojiImage(b.g, c.Cluster(), side), side
}

// outlines returns the references that draw c as glyph outlines: the base
// glyph, then each mark. A primary-family mark sits after the base glyph's
// advance; a fallback mark is centred in the cell.
func (b *Builder) outlines(c raster.RowCell, x, y float64) ([]use, error) {
	var out []use
	base, err := b.defs.glyph(b.g, b.lay, c.Base, c.Bold, c.Italic, c.Width)
	if err != nil {
		return nil, err
	}
	if raster.HasInk(c.Base) && base.ok {
		out = append(out, use{base.id, x, y, c.Fg})
	}
	advance := float64(b.lay.CellWidth)
	if base.ok && base.primary {
		advance = base.advance
	}
	for _, m := range c.DrawnMarks() {
		g, err := b.defs.glyph(b.g, b.lay, m, c.Bold, c.Italic, c.Width)
		if err != nil {
			return nil, err
		}
		if !g.ok {
			continue
		}
		mx := x
		if g.primary {
			mx += advance
		}
		out = append(out, use{g.id, mx, y, c.Fg})
	}
	return out, nil
}

// writeUses writes the references, grouping neighbours of one colour under a
// <g> with that fill. The theme foreground is the document's default fill.
func writeUses(sb *strings.Builder, uses []use, def color.RGBA) {
	for i := 0; i < len(uses); {
		j := i + 1
		for j < len(uses) && uses[j].fg == uses[i].fg {
			j++
		}
		group := uses[i:j]
		grouped := j-i > 1 && group[0].fg != def
		if grouped {
			sb.WriteString(`<g fill="` + hex(group[0].fg) + `">`)
		}
		for _, u := range group {
			sb.WriteString(`<use href="#` + u.id + `" x="` + num(u.x) + `" y="` + num(u.y) + `"`)
			if !grouped && u.fg != def {
				sb.WriteString(` fill="` + hex(u.fg) + `"`)
			}
			sb.WriteString("/>")
		}
		if grouped {
			sb.WriteString("</g>")
		}
		i = j
	}
}

// textStyle is what splits text into runs: the attributes of a <tspan>.
type textStyle struct {
	fg           color.RGBA
	bold, italic bool
}

// attrs returns the attributes of a <tspan> of this style, leaving out the
// default fill.
func (s textStyle) attrs(def color.RGBA) string {
	var out string
	if s.fg != def {
		out += ` fill="` + hex(s.fg) + `"`
	}
	if s.bold {
		out += ` font-weight="bold"`
	}
	if s.italic {
		out += ` font-style="italic"`
	}
	return out
}

// textRun is one <tspan>: characters and the x of each, or one cluster
// anchored at its x.
type textRun struct {
	style  textStyle
	xs     []float64
	text   strings.Builder
	middle bool // text-anchor middle
}

// textRuns collects the runs of a row.
type textRuns struct {
	fg      color.RGBA // the default fill, which a run leaves out
	runs    []*textRun
	cur     *textRun // open run of single characters, or nil
	pending []float64
}

// blank notes a blank cell at x: it joins the open run only if text of the
// same style follows soon enough.
func (t *textRuns) blank(x float64) {
	if t.cur != nil {
		t.pending = append(t.pending, x)
	}
}

// close ends the open run and drops the blanks after it.
func (t *textRuns) close() { t.cur, t.pending = nil, nil }

// add adds the text of cell c, whose left edge is at x. A single BMP rune of
// one cell goes into a run with an x for each character. Anything else
// (wide, combined or outside the BMP) is a run of its own, so the viewer's
// shaping places the marks and the x list never miscounts characters.
func (t *textRuns) add(c raster.RowCell, x float64, l raster.Layout) {
	st := textStyle{c.Fg, c.Bold, c.Italic}
	if len(c.Marks) != 0 || c.Width != 1 || c.Base > 0xFFFF {
		t.close()
		r := &textRun{style: st}
		r.text.WriteString(c.Cluster())
		if c.Width > 1 {
			r.xs = []float64{x + float64(c.Width*l.CellWidth)/2}
			r.middle = true
		} else {
			r.xs = []float64{x + l.GlyphOffsetX}
		}
		t.runs = append(t.runs, r)
		return
	}
	if t.cur == nil || t.cur.style != st || len(t.pending) > maxInnerSpaces {
		r := &textRun{style: st}
		t.runs = append(t.runs, r)
		t.cur = r
	} else {
		for _, px := range t.pending {
			t.cur.xs = append(t.cur.xs, px)
			t.cur.text.WriteByte(' ')
		}
	}
	t.pending = t.pending[:0]
	t.cur.xs = append(t.cur.xs, x+l.GlyphOffsetX)
	t.cur.text.WriteRune(c.Base)
}

// write writes the runs as one <text> element with its baseline at y.
func (t *textRuns) write(sb *strings.Builder, baseline int) {
	if len(t.runs) == 0 {
		return
	}
	sb.WriteString(`<text y="` + strconv.Itoa(baseline) + `" xml:space="preserve">`)
	for _, r := range t.runs {
		sb.WriteString(`<tspan x="`)
		for i, x := range r.xs {
			if i > 0 {
				sb.WriteByte(' ')
			}
			sb.WriteString(num(x))
		}
		sb.WriteByte('"')
		if r.middle {
			sb.WriteString(` text-anchor="middle"`)
		}
		sb.WriteString(r.style.attrs(t.fg))
		sb.WriteByte('>')
		escape(sb, r.text.String())
		sb.WriteString("</tspan>")
	}
	sb.WriteString("</text>")
}

package svg

import (
	"bytes"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/michaellorant/shellshot/internal/export/timeline"
	"github.com/michaellorant/shellshot/internal/render/raster"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// rowVariants are the distinct versions of one screen row over the session,
// in order of first appearance.
type rowVariants struct {
	index map[string]int // markup at the strip's origin -> variant
	first []int          // step where each variant first appears
	at    []int          // variant on screen at each step
}

func (w *Writer) writeAnimated(out *bytes.Buffer, cols, rows int, steps []timeline.Step) error {
	b := NewBuilder(w.o.Geometry, w.o.Text)
	lay := b.lay

	// Find the variants of every row.
	vars := make([]rowVariants, rows)
	for r := range vars {
		vars[r] = rowVariants{index: map[string]int{}, at: make([]int, len(steps))}
	}
	for i, st := range steps {
		for r := range vars {
			v := &vars[r]
			if i > 0 && w.rowUnchanged(b, steps[i-1].Snapshot, st.Snapshot, r) {
				v.at[i] = v.at[i-1]
				continue
			}
			m, err := b.Row(st.Snapshot, r, lay.Pad, 0)
			if err != nil {
				return fmt.Errorf("svg: step %d row %d: %w", i, r, err)
			}
			k, ok := v.index[m]
			if !ok {
				k = len(v.first)
				v.index[m] = k
				v.first = append(v.first, i)
			}
			v.at[i] = k
		}
	}

	// Time of each step as a percentage of the animation.
	starts := make([]time.Duration, len(steps))
	var sum time.Duration
	for i, st := range steps {
		starts[i] = sum
		sum += st.Delay
	}
	clk := newClock(starts, sum, w.o.Loops)

	var body strings.Builder
	anims := &animations{clock: clk}
	for r := range vars {
		v := &vars[r]
		top := lay.Pad + r*lay.CellHeight
		if len(v.first) == 1 {
			m, err := b.Row(steps[v.first[0]].Snapshot, r, lay.Pad, top)
			if err != nil {
				return fmt.Errorf("svg: row %d: %w", r, err)
			}
			body.WriteString(m)
			continue
		}
		var strip strings.Builder
		for k, i := range v.first {
			m, err := b.Row(steps[i].Snapshot, r, lay.Pad, k*lay.CellHeight)
			if err != nil {
				return fmt.Errorf("svg: step %d row %d: %w", i, r, err)
			}
			strip.WriteString(m)
		}
		class := anims.add(clk.frames(v.at, func(k int) string {
			return "transform:translateY(" + px(-k*lay.CellHeight) + ")"
		}))
		fmt.Fprintf(&body, `<svg y="%d" width="%d" height="%d"><g class="%s">%s</g></svg>`+"\n",
			top, cols*lay.CellWidth+2*lay.Pad, lay.CellHeight, class, strings.TrimSuffix(strip.String(), "\n"))
	}
	w.cursorElements(&body, b, anims, steps)

	var head strings.Builder
	open(&head, lay, cols, rows, anims.css(), b.Defs())
	out.WriteString(head.String())
	out.WriteString(body.String())
	out.WriteString("</svg>\n")
	return nil
}

// px formats a length in CSS pixels: "0" or "-36px".
func px(v int) string {
	if v == 0 {
		return "0"
	}
	return strconv.Itoa(v) + "px"
}

// rowUnchanged reports that row r draws the same in cur as in prev: the text,
// spans and widths are equal and the block cursor, which changes the cell it
// sits on, is on the row in neither.
func (w *Writer) rowUnchanged(b *Builder, prev, cur snapshot.Snapshot, r int) bool {
	if prev.Lines[r] != cur.Lines[r] || !slices.Equal(rowOf(prev.Spans, r), rowOf(cur.Spans, r)) ||
		!slices.Equal(rowOf(prev.Widths, r), rowOf(cur.Widths, r)) {
		return false
	}
	if b.lay.Cursor != raster.CursorBlock {
		return true
	}
	for _, s := range [...]snapshot.Snapshot{prev, cur} {
		if row, _, _, ok := b.g.CursorCell(s); ok && row == r {
			return false
		}
	}
	return true
}

// rowOf returns row r of per-row data that may be nil for a snapshot without
// styling.
func rowOf[T any](rows [][]T, r int) []T {
	if r < len(rows) {
		return rows[r]
	}
	return nil
}

// cursorElements writes the bar or underline cursor as rectangles moved by
// keyframes. An underline cursor over a wide rune is wider, so each width
// that occurs gets a rectangle of its own, shown only while it applies.
func (w *Writer) cursorElements(body *strings.Builder, b *Builder, anims *animations, steps []timeline.Step) {
	lay := b.lay
	rects := make([]Rect, len(steps))
	shown := make([]bool, len(steps))
	var widths []int // distinct rectangle widths, in order of first use
	for i, st := range steps {
		rects[i], shown[i] = b.Cursor(st.Snapshot, lay.Pad, lay.Pad)
		if shown[i] && !slices.Contains(widths, rects[i].W) {
			widths = append(widths, rects[i].W)
		}
	}
	for _, wd := range widths {
		first := slices.IndexFunc(rects, func(r Rect) bool { return r.W == wd }) // an unshown rect has W 0
		base := rects[first]
		// State per step: 0 hidden, else an index into the positions seen.
		type pos struct{ dx, dy int }
		var positions []pos
		at := make([]int, len(steps))
		for i, r := range rects {
			if !shown[i] || r.W != wd {
				continue
			}
			p := pos{r.X - base.X, r.Y - base.Y}
			k := slices.Index(positions, p)
			if k < 0 {
				k = len(positions)
				positions = append(positions, p)
			}
			at[i] = k + 1
		}
		class := anims.add(anims.clock.frames(at, func(k int) string {
			if k == 0 {
				return "transform:translate(0,0);visibility:hidden"
			}
			p := positions[k-1]
			return "transform:translate(" + px(p.dx) + "," + px(p.dy) + ");visibility:visible"
		}))
		hidden := ""
		if at[0] == 0 {
			hidden = ` visibility="hidden"`
		}
		fmt.Fprintf(body, `<rect class="%s" x="%d" y="%d" width="%d" height="%d" fill="%s"%s/>`+"\n",
			class, base.X, base.Y, base.W, base.H, hex(lay.Theme.Cursor), hidden)
	}
}
func total(steps []timeline.Step) time.Duration {
	var d time.Duration
	for _, st := range steps {
		d += st.Delay
	}
	return d
}

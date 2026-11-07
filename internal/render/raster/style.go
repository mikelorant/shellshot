package raster

import (
	"image/color"

	xfont "golang.org/x/image/font"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// cellStyle is the resolved appearance of one cell: concrete colours (after
// inverse, faint and invisible) and the attributes that change how it is
// drawn.
type cellStyle struct {
	fg, bg       color.RGBA
	bold, italic bool
	underline    bool
	strike       bool
}

// resolveCellStyle maps a snapshot style to a cellStyle.
func (r *Renderer) resolveCellStyle(st snapshot.Style) cellStyle {
	fg, bg := r.theme.Colors(st)
	return cellStyle{
		fg:        fg,
		bg:        bg,
		bold:      st.Attrs&snapshot.Bold != 0,
		italic:    st.Attrs&snapshot.Italic != 0,
		underline: st.Attrs&snapshot.Underline != 0,
		strike:    st.Attrs&snapshot.Strike != 0,
	}
}

// face returns the face for st's bold/italic combination.
func (r *Renderer) face(st cellStyle) xfont.Face {
	i := 0
	if st.bold {
		i |= 1
	}
	if st.italic {
		i |= 2
	}
	return r.faces[i]
}

// spanWalker looks up the style of consecutive rune indexes in a sorted,
// non-overlapping span list in O(spans + runes) overall.
type spanWalker struct {
	spans []snapshot.Span
	next  int
}

// at returns the style of rune index i. Calls must use non-decreasing i.
func (w *spanWalker) at(i int) snapshot.Style {
	for w.next < len(w.spans) && w.spans[w.next].End <= i {
		w.next++
	}
	if w.next < len(w.spans) && w.spans[w.next].Start <= i {
		return w.spans[w.next].Style
	}
	return snapshot.Style{}
}

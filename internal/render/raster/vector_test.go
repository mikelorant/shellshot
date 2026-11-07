package raster

import (
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

func TestLayoutMatchesTheRenderer(t *testing.T) {
	r, m := newTestRenderer(t, Options{FontSize: 13, Scale: 2, Padding: 3, Cursor: CursorBar})
	l := r.Layout()
	w, h := r.Size(7, 3)
	if gw, gh := 7*l.CellWidth+2*l.Pad, 3*l.CellHeight+2*l.Pad; gw != w || gh != h {
		t.Errorf("layout gives %dx%d, Size gives %dx%d", gw, gh, w, h)
	}
	if l.CellWidth != m.CellWidth || l.CellHeight != m.CellHeight || l.Ascent != m.Ascent {
		t.Errorf("layout cell %dx%d ascent %d, metrics %dx%d ascent %d", l.CellWidth, l.CellHeight, l.Ascent, m.CellWidth, m.CellHeight, m.Ascent)
	}
	if l.Scale != 2 || l.Pad != 6 || l.FontSize != 26 || l.Cursor != CursorBar || l.Theme != r.theme {
		t.Errorf("layout = %+v", l)
	}
	if l.Thickness != r.thickness() || l.UnderlineTop != r.underlineTop() || l.StrikeTop != r.strikeTop() || l.BarWidth != max(1, l.CellWidth/8) {
		t.Errorf("decoration geometry differs from the renderer's: %+v", l)
	}
}

func TestRowCellsAreWhatRenderDraws(t *testing.T) {
	r, _ := newTestRenderer(t, Options{})
	s := plainSnapshot(10, "a世e\u0301😀\u2764\ufe0f")
	s.Widths = [][]snapshot.Wide{{{Rune: 1, Cells: 2}, {Rune: 3, Cells: 0}, {Rune: 4, Cells: 2}, {Rune: 6, Cells: 0}}}
	s.Spans = [][]snapshot.Span{{{Start: 0, End: 1, Style: snapshot.Style{Attrs: snapshot.Bold | snapshot.Underline}}}}
	s.Cursor = snapshot.Cursor{X: 3, Visible: true}
	cells := r.RowCells(s, 0)
	type want struct {
		cluster  string
		col, w   int
		bold     bool
		emojiSeq bool
	}
	for i, w := range []want{{"a", 0, 1, true, false}, {"世", 1, 2, false, false}, {"e\u0301", 3, 1, false, false}, {"😀", 4, 2, false, false}, {"\u2764\ufe0f", 6, 1, false, true}} {
		if i >= len(cells) {
			t.Fatalf("only %d cells", len(cells))
		}
		c := cells[i]
		if c.Cluster() != w.cluster || c.Col != w.col || c.Width != w.w || c.Bold != w.bold || c.EmojiSequence != w.emojiSeq {
			t.Errorf("cell %d = %+v, want %+v", i, c, w)
		}
	}
	// The cursor at column 3 is a block over the combined cluster.
	if c := cells[2]; c.Bg != r.theme.Cursor || c.Fg != r.theme.Background {
		t.Errorf("cell under the block cursor has colours %v on %v", c.Fg, c.Bg)
	}
	if !cells[0].Underline || cells[1].Underline {
		t.Error("underline flag wrong")
	}
	if got := string(cells[4].DrawnMarks()); got != "" {
		t.Errorf("VS16 is a shaping mark, DrawnMarks = %q", got)
	}
	if got := string(cells[2].DrawnMarks()); got != "\u0301" {
		t.Errorf("DrawnMarks = %q, want the combining acute", got)
	}
	// A cursor past the text adds a blank cell with the cursor colours.
	s.Cursor = snapshot.Cursor{X: 9, Visible: true}
	cells = r.RowCells(s, 0)
	if last := cells[len(cells)-1]; last.Col != 9 || last.Base != ' ' || last.Bg != r.theme.Cursor {
		t.Errorf("cursor cell = %+v", last)
	}
}

func TestCursorCellSnapsToTheWideRune(t *testing.T) {
	r, _ := newTestRenderer(t, Options{Cursor: CursorUnderline})
	s := plainSnapshot(10, "ab世")
	s.Widths = [][]snapshot.Wide{{{Rune: 2, Cells: 2}}}
	s.Cursor = snapshot.Cursor{X: 3, Visible: true}
	if row, col, width, ok := r.CursorCell(s); !ok || row != 0 || col != 2 || width != 2 {
		t.Errorf("CursorCell = %d, %d, %d, %v; want 0, 2, 2, true", row, col, width, ok)
	}
	s.Cursor.Visible = false
	if _, _, _, ok := r.CursorCell(s); ok {
		t.Error("hidden cursor reported")
	}
}

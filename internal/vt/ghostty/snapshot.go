package ghostty

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Snapshot copies the visible screen: exactly Rows lines of plain text with
// trailing spaces removed, the styled spans of those lines, the cursor and the
// title. Spans is nil when no line has styling. The result owns all of its
// memory.
func (t *terminal) Snapshot() (snapshot.Snapshot, error) {
	if t.closed {
		return snapshot.Snapshot{}, ErrClosed
	}

	if err := t.state.Update(t.term); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: update render state: %w", err)
	}
	if err := t.state.RowIterator(t.rows); err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: iterate rows: %w", err)
	}

	lines := make([]string, 0, t.height)
	spans := make([][]snapshot.Span, 0, t.height)
	styled := false
	for t.rows.Next() {
		text, err := t.rows.AppendText(t.rowBuf[:0], t.cells)
		if err != nil {
			return snapshot.Snapshot{}, fmt.Errorf("ghostty: read row %d: %w", len(lines), err)
		}
		t.rowBuf = text
		// string() copies, so lines never alias rowBuf or native memory.
		line := strings.TrimRight(string(text), " ")
		lines = append(lines, line)

		rs, err := t.rowSpans(utf8.RuneCountInString(line))
		if err != nil {
			return snapshot.Snapshot{}, fmt.Errorf("ghostty: read row %d styles: %w", len(lines)-1, err)
		}
		spans = append(spans, rs)
		styled = styled || len(rs) > 0
	}
	if len(lines) != t.height {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: screen has %d rows, want %d", len(lines), t.height)
	}

	if !styled {
		spans = nil
	}

	cx, err := t.term.CursorX()
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: cursor x: %w", err)
	}
	cy, err := t.term.CursorY()
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: cursor y: %w", err)
	}
	visible, err := t.term.CursorVisible()
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: cursor visibility: %w", err)
	}
	title, err := t.term.Title()
	if err != nil {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: title: %w", err)
	}

	return snapshot.Snapshot{
		Cols:   t.cols,
		Rows:   t.height,
		Lines:  lines,
		Spans:  spans,
		Cursor: snapshot.Cursor{X: int(cx), Y: int(cy), Visible: visible},
		Title:  title,
	}, nil
}

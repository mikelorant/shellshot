package ghostty

import (
	"fmt"
	"strings"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Snapshot copies the visible screen: exactly Rows lines of plain text with
// trailing spaces removed, the cursor and the title. The result owns all of
// its memory.
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
	for t.rows.Next() {
		text, err := t.rows.AppendText(t.rowBuf[:0], t.cells)
		if err != nil {
			return snapshot.Snapshot{}, fmt.Errorf("ghostty: read row %d: %w", len(lines), err)
		}
		t.rowBuf = text
		// string() copies, so lines never alias rowBuf or native memory.
		lines = append(lines, strings.TrimRight(string(text), " "))
	}
	if len(lines) != t.height {
		return snapshot.Snapshot{}, fmt.Errorf("ghostty: screen has %d rows, want %d", len(lines), t.height)
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
		Cursor: snapshot.Cursor{X: int(cx), Y: int(cy), Visible: visible},
		Title:  title,
	}, nil
}

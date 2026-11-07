package recording

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Writer appends frames to a recording.
type Writer struct {
	enc        *json.Encoder
	cols, rows int
}

// NewWriter writes the header line to w and returns a Writer for frames.
// A zero h.Version is replaced by Version; any other version is rejected with
// ErrVersion, since Writer only produces the current format.
func NewWriter(w io.Writer, h Header) (*Writer, error) {
	if h.Version == 0 {
		h.Version = Version
	}
	if h.Version != Version {
		return nil, fmt.Errorf("%w: cannot write %d", ErrVersion, h.Version)
	}
	if err := h.validate(); err != nil {
		return nil, err
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	line := headerLine{
		Version: h.Version,
		Cols:    h.Cols,
		Rows:    h.Rows,
		Command: h.Command,
		Started: h.Started,
	}
	if line.Command == nil {
		line.Command = []string{}
	}
	if err := enc.Encode(line); err != nil {
		return nil, fmt.Errorf("recording: write header: %w", err)
	}
	return &Writer{enc: enc, cols: h.Cols, rows: h.Rows}, nil
}

// WriteFrame appends f as one line. Frame time is stored with millisecond
// precision; screen dimensions are taken from the header, not the frame.
// f must have exactly as many lines as the header has rows and must satisfy
// snapshot.Snapshot.Validate; otherwise the error wraps ErrMalformed.
func (w *Writer) WriteFrame(f snapshot.Frame) error {
	lines := f.Snapshot.Lines
	if len(lines) != w.rows {
		return fmt.Errorf("%w: frame has %d lines, want %d", ErrMalformed, len(lines), w.rows)
	}
	checked := f.Snapshot
	checked.Cols, checked.Rows = w.cols, w.rows
	if err := checked.Validate(); err != nil {
		return fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	err := w.enc.Encode(frameLine{
		T:     f.At.Milliseconds(),
		Lines: lines,
		CX:    f.Snapshot.Cursor.X,
		CY:    f.Snapshot.Cursor.Y,
		CV:    f.Snapshot.Cursor.Visible,
		Title: f.Snapshot.Title,
		Spans: encodeSpans(f.Snapshot.Spans),
	})
	if err != nil {
		return fmt.Errorf("recording: write frame: %w", err)
	}
	return nil
}

package recording

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Reader decodes a recording written by Writer.
type Reader struct {
	br     *bufio.Reader
	header Header
	line   int // number of lines consumed so far, for error messages
}

// NewReader reads and validates the header line from r.
// It returns an error wrapping ErrVersion for an unsupported version and
// ErrMalformed for any other invalid header.
func NewReader(r io.Reader) (*Reader, error) {
	rd := &Reader{br: bufio.NewReader(r)}
	data, err := rd.readLine()
	if errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: missing header", ErrMalformed)
	}
	if err != nil {
		return nil, fmt.Errorf("recording: read header: %w", err)
	}
	var hl headerLine
	if err := json.Unmarshal(data, &hl); err != nil {
		return nil, fmt.Errorf("%w: header: %w", ErrMalformed, err)
	}
	h := Header{
		Version: hl.Version,
		Cols:    hl.Cols,
		Rows:    hl.Rows,
		Command: hl.Command,
		Started: hl.Started,
	}
	if err := h.validate(); err != nil {
		return nil, err
	}
	rd.header = h
	return rd, nil
}

// Header returns the recording header.
func (r *Reader) Header() Header { return r.header }

// Next returns the next frame, or io.EOF after the last one. The frame's
// Cols and Rows are taken from the header.
func (r *Reader) Next() (snapshot.Frame, error) {
	data, err := r.readLine()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return snapshot.Frame{}, io.EOF
		}
		return snapshot.Frame{}, fmt.Errorf("recording: read frame: %w", err)
	}
	var fl frameLine
	if err := json.Unmarshal(data, &fl); err != nil {
		return snapshot.Frame{}, fmt.Errorf("%w: line %d: %w", ErrMalformed, r.line, err)
	}
	if fl.T < 0 {
		return snapshot.Frame{}, fmt.Errorf("%w: line %d: negative time %d", ErrMalformed, r.line, fl.T)
	}
	if len(fl.Lines) != r.header.Rows {
		return snapshot.Frame{}, fmt.Errorf("%w: line %d: %d lines, want %d", ErrMalformed, r.line, len(fl.Lines), r.header.Rows)
	}
	snap := snapshot.Snapshot{
		Cols:   r.header.Cols,
		Rows:   r.header.Rows,
		Lines:  fl.Lines,
		Cursor: snapshot.Cursor{X: fl.CX, Y: fl.CY, Visible: fl.CV},
		Title:  fl.Title,
	}
	if r.header.Version >= 2 {
		if fl.Spans != nil && len(fl.Spans) != r.header.Rows {
			return snapshot.Frame{}, fmt.Errorf("%w: line %d: %d span rows, want %d", ErrMalformed, r.line, len(fl.Spans), r.header.Rows)
		}
		snap.Spans = decodeSpans(fl.Spans)
	}
	if err := snap.Validate(); err != nil {
		return snapshot.Frame{}, fmt.Errorf("%w: line %d: %w", ErrMalformed, r.line, err)
	}
	return snapshot.Frame{At: time.Duration(fl.T) * time.Millisecond, Snapshot: snap}, nil
}

// readLine returns the next line without its line ending, or io.EOF when no
// data remains. A final line lacking a trailing newline is still returned.
func (r *Reader) readLine() ([]byte, error) {
	data, err := r.br.ReadBytes('\n')
	if err != nil && (!errors.Is(err, io.EOF) || len(data) == 0) {
		return nil, err
	}
	r.line++
	return bytes.TrimRight(data, "\r\n"), nil
}

package ghostty

import (
	"errors"
	"fmt"

	libghostty "go.mitchellh.com/libghostty"

	"github.com/mikelorant/shellshot/internal/vt"
)

// ErrClosed is returned by every Terminal method called after Close.
var ErrClosed = errors.New("ghostty: terminal is closed")

// Nominal cell size in pixels. libghostty only uses it for image protocols and
// size reports, which shellshot does not render.
const (
	cellWidthPx  = 8
	cellHeightPx = 16
)

// maxDimension is the largest size libghostty accepts (uint16 cells).
const maxDimension = 1<<16 - 1

var _ vt.Factory = New

// terminal is a vt.Terminal backed by a libghostty terminal. The render state,
// row iterator and cell cursor are allocated once and reused by every
// Snapshot call.
type terminal struct {
	term  *libghostty.Terminal
	state *libghostty.RenderState
	rows  *libghostty.RenderStateRowIterator
	cells *libghostty.RenderStateRowCells

	cols, height int
	rowBuf       []byte
	cellBuf      []byte
	closed       bool
}

// New creates a headless terminal of the given size in cells. It is a
// vt.Factory.
func New(cols, rows int) (vt.Terminal, error) {
	return newTerminal(cols, rows, nil)
}

// newTerminal is New with an optional reply sink; see NewAnswering.
func newTerminal(cols, rows int, reply func([]byte)) (vt.Terminal, error) {
	if err := checkSize(cols, rows); err != nil {
		return nil, err
	}

	t := &terminal{cols: cols, height: rows}

	opts := []libghostty.TerminalOption{
		libghostty.WithSize(uint16(cols), uint16(rows)),
		// Grapheme clustering (mode 2027) is on by default in modern terminals;
		// as the reset default it also survives RIS.
		libghostty.WithModeDefault(libghostty.ModeGraphemeCluster, true),
	}
	if reply != nil {
		opts = append(opts, t.replyOptions(reply)...)
	}

	var err error
	t.term, err = libghostty.NewTerminal(opts...)
	if err != nil {
		return nil, fmt.Errorf("ghostty: create terminal: %w", err)
	}
	if reply != nil {
		if err := t.setReplyColors(); err != nil {
			t.Close()
			return nil, err
		}
	}
	if t.state, err = libghostty.NewRenderState(); err != nil {
		t.Close()
		return nil, fmt.Errorf("ghostty: create render state: %w", err)
	}
	if t.rows, err = libghostty.NewRenderStateRowIterator(); err != nil {
		t.Close()
		return nil, fmt.Errorf("ghostty: create row iterator: %w", err)
	}
	if t.cells, err = libghostty.NewRenderStateRowCells(); err != nil {
		t.Close()
		return nil, fmt.Errorf("ghostty: create row cells: %w", err)
	}
	return t, nil
}

func checkSize(cols, rows int) error {
	if cols < 1 || cols > maxDimension || rows < 1 || rows > maxDimension {
		return fmt.Errorf("ghostty: invalid size %dx%d: each dimension must be 1..%d", cols, rows, maxDimension)
	}
	return nil
}

// Write feeds raw VT output to the terminal. It never fails on malformed
// input; libghostty skips what it cannot parse.
func (t *terminal) Write(p []byte) (int, error) {
	if t.closed {
		return 0, ErrClosed
	}
	t.term.VTWrite(p)
	return len(p), nil
}

// Resize changes the screen size in cells.
func (t *terminal) Resize(cols, rows int) error {
	if t.closed {
		return ErrClosed
	}
	if err := checkSize(cols, rows); err != nil {
		return err
	}
	if err := t.term.Resize(uint16(cols), uint16(rows), cellWidthPx, cellHeightPx); err != nil {
		return fmt.Errorf("ghostty: resize: %w", err)
	}
	t.cols, t.height = cols, rows
	return nil
}

// Close releases the native resources. It is safe to call more than once.
func (t *terminal) Close() error {
	if t.closed {
		return nil
	}
	t.closed = true
	if t.cells != nil {
		t.cells.Close()
	}
	if t.rows != nil {
		t.rows.Close()
	}
	if t.state != nil {
		t.state.Close()
	}
	if t.term != nil {
		t.term.Close()
	}
	return nil
}

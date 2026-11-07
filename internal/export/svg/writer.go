package svg

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/michaellorant/shellshot/internal/export/timeline"
	"github.com/michaellorant/shellshot/internal/render/raster"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// Writer writes timeline steps as an SVG file: the animation, or one step as
// a static image if Options.Frame selects one.
type Writer struct {
	o Options
}

// NewWriter returns a Writer for o.
func NewWriter(o Options) *Writer { return &Writer{o: o} }

// WriteSteps writes the document to w, which receives nothing if an error is
// returned. Every step must be a valid snapshot of cols x rows cells. A
// single step, or steps that last no time at all, give a static image.
func (w *Writer) WriteSteps(out io.Writer, cols, rows int, steps []timeline.Step) error {
	if w.o.Geometry == nil {
		return errors.New("svg: no geometry")
	}
	if len(steps) == 0 {
		return errors.New("svg: no steps")
	}
	for i, st := range steps {
		if err := st.Snapshot.Validate(); err != nil {
			return fmt.Errorf("svg: step %d: %w", i, err)
		}
		if st.Snapshot.Cols != cols || st.Snapshot.Rows != rows {
			return fmt.Errorf("svg: step %d is %dx%d cells, want %dx%d", i, st.Snapshot.Cols, st.Snapshot.Rows, cols, rows)
		}
	}
	var buf bytes.Buffer
	var err error
	if w.o.Frame.Kind != FrameNone {
		err = w.writeStatic(&buf, cols, rows, steps[PickStep(steps, w.o.Frame)].Snapshot)
	} else if total(steps) <= 0 || len(steps) == 1 {
		err = w.writeStatic(&buf, cols, rows, steps[len(steps)-1].Snapshot)
	} else {
		err = w.writeAnimated(&buf, cols, rows, steps)
	}
	if err != nil {
		return err
	}
	_, err = out.Write(buf.Bytes())
	return err
}

// PickStep returns the index of the step f selects: the first, the last, or
// the step on screen at f.At on the clock of the steps' delays (the last step
// if f.At is past the end).
func PickStep(steps []timeline.Step, f Frame) int {
	switch f.Kind {
	case FrameFirst:
		return 0
	case FrameAt:
		var end time.Duration
		for i, st := range steps {
			if end += st.Delay; f.At < end {
				return i
			}
		}
	}
	return len(steps) - 1
}

// open writes the start of the document: the <svg> element and its
// background, with style (CSS, or "") and defs between them.
func open(sb *strings.Builder, lay raster.Layout, cols, rows int, style, defs string) {
	w, h := cols*lay.CellWidth+2*lay.Pad, rows*lay.CellHeight+2*lay.Pad
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 ` + strconv.Itoa(w) + ` ` + strconv.Itoa(h) +
		`" width="` + num(float64(w)/float64(lay.Scale)) + `" height="` + num(float64(h)/float64(lay.Scale)) +
		`" font-family="` + fontFamily + `" font-size="` + num(lay.FontSize) + `" fill="` + hex(lay.Theme.Foreground) + "\">\n")
	if style != "" {
		sb.WriteString("<style>" + style + "</style>\n")
	}
	sb.WriteString(defs)
	sb.WriteString(`<rect width="` + strconv.Itoa(w) + `" height="` + strconv.Itoa(h) + `" fill="` + hex(lay.Theme.Background) + "\"/>\n")
}

func (w *Writer) writeStatic(out *bytes.Buffer, cols, rows int, s snapshot.Snapshot) error {
	b := NewBuilder(w.o.Geometry, w.o.Text)
	lay := b.lay
	var body strings.Builder
	for row := 0; row < rows; row++ {
		m, err := b.Row(s, row, lay.Pad, lay.Pad+row*lay.CellHeight)
		if err != nil {
			return fmt.Errorf("svg: row %d: %w", row, err)
		}
		body.WriteString(m)
	}
	if r, ok := b.Cursor(s, lay.Pad, lay.Pad); ok {
		rectMarkup(&body, r.X, r.Y, r.W, r.H, lay.Theme.Cursor)
		body.WriteByte('\n')
	}
	var head strings.Builder
	open(&head, lay, cols, rows, "", b.Defs())
	out.WriteString(head.String())
	out.WriteString(body.String())
	out.WriteString("</svg>\n")
	return nil
}

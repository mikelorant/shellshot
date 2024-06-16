package emulator

import (
	"bytes"

	"github.com/vito/midterm"
)

type Emulator struct {
	vt   *midterm.Terminal
	opts Options
}

const (
	defaultCols          = 80
	defaultRows          = 24
	defaultRaw           = true
	defaultCursorVisible = true
)

func New(options ...func(*Options)) *Emulator {
	opts := Options{
		Cols: defaultCols,
		Rows: defaultRows,
	}

	for _, o := range options {
		o(&opts)
	}

	return &Emulator{
		opts: opts,
	}
}

func (em *Emulator) Start() {
	term := midterm.NewTerminal(em.opts.Rows, em.opts.Cols)
	term.Raw = defaultRaw
	term.CursorVisible = defaultCursorVisible

	if em.opts.Output != nil {
		term.ForwardResponses = em.opts.Output
	}

	em.vt = term
}

func (em *Emulator) Render() string {
	var buf bytes.Buffer

	if err := em.vt.Render(&buf); err != nil {
		return ""
	}

	return buf.String()
}

func (em *Emulator) Write(p []byte) (int, error) {
	return em.vt.Write(p)
}

package emulator

import (
	"bytes"
	"fmt"
	"sync"

	"github.com/mikelorant/shellshot/internal/terminal"

	"github.com/vito/midterm"
)

type Emulator struct {
	vt     *midterm.Terminal
	opts   terminal.Options
	update chan bool
	buf    bytes.Buffer
	mu     sync.Mutex
}

const (
	defaultCols          = 80
	defaultRows          = 24
	defaultRaw           = true
	defaultCursorVisible = true
)

func New() *Emulator {
	opts := terminal.Options{
		Cols: defaultCols,
		Rows: defaultRows,
	}

	return &Emulator{
		opts:   opts,
		update: make(chan bool, 1),
	}
}

func (em *Emulator) Start(opts ...func(*terminal.Options)) error {
	for _, o := range opts {
		o(&em.opts)
	}

	term := midterm.NewTerminal(em.opts.Rows, em.opts.Cols)
	term.Raw = defaultRaw
	term.CursorVisible = defaultCursorVisible

	if em.opts.Output != nil {
		term.ForwardResponses = em.opts.Output
	}

	em.vt = term

	return nil
}

func (em *Emulator) Read(p []byte) (int, error) {
	<-em.update

	if err := em.vt.Render(&em.buf); err != nil {
		return 0, fmt.Errorf("unable to render: %w", err)
	}

	n, err := em.buf.Read(p)
	if err != nil {
		return n, fmt.Errorf("unable to read from buffer: %w", err)
	}

	if em.buf.Len() > 0 {
		em.Update()
	}

	return n, err
}

func (em *Emulator) Write(p []byte) (int, error) {
	em.Update()

	return em.vt.Write(p)
}

func (em *Emulator) Close() error {
	return nil
}

func (em *Emulator) Update() {
	em.mu.Lock()
	defer em.mu.Unlock()

	if len(em.update) == 0 {
		em.update <- true
	}
}

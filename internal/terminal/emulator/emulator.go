package emulator

import (
	"bytes"
	"sync"
	"time"

	"github.com/vito/midterm"
)

type Terminal struct {
	vt      *midterm.Terminal
	updates []time.Time
	cond    *sync.Cond
	opts    Options
}

const (
	defaultCols          = 80
	defaultRows          = 24
	defaultRaw           = true
	defaultCursorVisible = true
)

func New(options ...func(*Options)) *Terminal {
	opts := Options{
		cols: defaultCols,
		rows: defaultRows,
	}

	for _, o := range options {
		o(&opts)
	}

	var mu sync.Mutex
	cond := sync.NewCond(&mu)

	return &Terminal{
		cond: cond,
		opts: opts,
	}
}

func (t *Terminal) Start() *Terminal {
	term := midterm.NewTerminal(t.opts.rows, t.opts.cols)
	term.Raw = defaultRaw
	term.CursorVisible = defaultCursorVisible

	if t.opts.fwdResp != nil {
		term.ForwardResponses = t.opts.fwdResp
	}

	t.vt = term

	return t
}

func (t *Terminal) Render() string {
	var buf bytes.Buffer

	if err := t.vt.Render(&buf); err != nil {
		return ""
	}

	return buf.String()
}

func (t *Terminal) Write(p []byte) (int, error) {
	n, err := t.vt.Write(p)

	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	t.updates = append(t.updates, time.Now())
	t.cond.Broadcast()

	return n, err
}

func (t *Terminal) Wait() {
	t.cond.L.Lock()
	defer t.cond.L.Unlock()

	for len(t.updates) == 0 {
		t.cond.Wait()
	}

	t.updates = nil
}

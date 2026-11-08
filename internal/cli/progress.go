package cli

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mikelorant/shellshot/internal/script/run"
	"github.com/mikelorant/shellshot/internal/tty"
)

// showProgress decides whether "record --script" draws the progress line.
// It needs a terminal on stderr to redraw in place, and --watch has the
// recorded terminal write to the same terminal, which a redrawn line would
// tear up. (--list runs nothing and never gets this far.)
func showProgress(stderrIsTerminal, watch bool) bool {
	return stderrIsTerminal && !watch
}

// startScriptProgress starts the progress line of a script run on errOut when
// it is to be shown (see showProgress) and returns the observer to give the
// executor as Deps.OnProgress and the function that ends the line. Without a
// progress line the observer is nil and the function does nothing. The
// function may be called more than once.
func (ro recordOptions) startScriptProgress(errOut io.Writer, watch bool, plan *run.Plan) (observe func(run.Progress), stop func()) {
	probe := ro.stderrTerminal
	if probe == nil {
		probe = stderrTerminal
	}
	width, isTerminal := probe(errOut)
	if !showProgress(isTerminal, watch) {
		return nil, func() {}
	}
	p := newProgress(progressConfig{Out: errOut, Width: width})
	p.Update(0, plan.Steps(), "")
	return func(r run.Progress) { p.Update(r.Done, r.Total, r.Scope) }, p.Close
}

// stderrTerminal reports whether w is a terminal and, if so, a function that
// returns its current width in columns (80 when the size is unknown). The
// width is asked at every redraw so that the line follows a resize.
func stderrTerminal(w io.Writer) (width func() int, ok bool) {
	f, isFile := w.(*os.File)
	if !isFile || !tty.IsTerminal(f) {
		return nil, false
	}
	return func() int {
		if cols, _, err := tty.Size(f); err == nil && cols > 0 {
			return cols
		}
		return defaultProgressWidth
	}, true
}

const (
	defaultProgressWidth = 80
	maxBarCells          = 20 // inside the brackets
	minBarCells          = 10
	progressInterval     = time.Second // redraw while a long step runs
	carriageReturn       = "\r"
	eraseToEnd           = "\x1b[K"
)

// progress draws one line on a terminal that shows how far a script is:
//
//	[=======>            ]  7/23  create  12s
//
// the bar, the steps done of the total, the scene or hidden block of the
// current step when there is one, and the time since the progress started.
// The line is redrawn in place by Update, and once a second by a goroutine
// of its own so the time keeps running during a long step. Close erases the
// line, and must be called before anything else is written to the terminal.
//
// A progress is safe for concurrent use.
type progress struct {
	out   io.Writer
	width func() int
	now   func() time.Time
	start time.Time

	mu            sync.Mutex
	done, total   int
	label         string
	drawn, closed bool

	ticks  <-chan time.Time // redraw prompts
	ticker *time.Ticker     // owned when ticks was not given
	stop   chan struct{}
	wg     sync.WaitGroup
}

// progressConfig is what newProgress needs; only Out is required.
type progressConfig struct {
	Out   io.Writer
	Width func() int       // columns of the terminal; default 80
	Now   func() time.Time // default time.Now
	// Ticks prompts a redraw each time it delivers. When nil, progress
	// makes a ticker of its own that fires every second.
	Ticks <-chan time.Time
}

// newProgress returns a progress that draws nothing until the first Update.
func newProgress(cfg progressConfig) *progress {
	p := &progress{out: cfg.Out, width: cfg.Width, now: cfg.Now, ticks: cfg.Ticks, stop: make(chan struct{})}
	if p.width == nil {
		p.width = func() int { return defaultProgressWidth }
	}
	if p.now == nil {
		p.now = time.Now
	}
	if p.ticks == nil {
		p.ticker = time.NewTicker(progressInterval)
		p.ticks = p.ticker.C
	}
	p.start = p.now()
	p.wg.Add(1)
	go p.redrawOnTick()
	return p
}

func (p *progress) redrawOnTick() {
	defer p.wg.Done()
	for {
		select {
		case <-p.ticks:
			p.mu.Lock()
			p.draw()
			p.mu.Unlock()
		case <-p.stop:
			return
		}
	}
}

// Update sets the steps done of total and the label of the current step, and
// redraws. A total of 0 or less draws nothing. After Close it does nothing.
func (p *progress) Update(done, total int, label string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return
	}
	p.done, p.total, p.label = done, total, label
	p.draw()
}

// Close stops the redraws and erases the line if one was drawn. Later calls,
// and Updates, do nothing.
func (p *progress) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	if p.drawn {
		_, _ = io.WriteString(p.out, carriageReturn+eraseToEnd)
	}
	p.mu.Unlock()
	close(p.stop)
	p.wg.Wait()
	if p.ticker != nil {
		p.ticker.Stop()
	}
}

// draw writes the line. The caller holds p.mu.
func (p *progress) draw() {
	if p.closed || p.total <= 0 {
		return
	}
	line := renderProgress(p.width(), p.done, p.total, p.label, p.now().Sub(p.start))
	_, _ = io.WriteString(p.out, carriageReturn+line+eraseToEnd)
	p.drawn = true
}

// renderProgress returns the progress line for a terminal width columns wide.
// It is never wider than width-1 columns, so that it does not wrap, and it
// is plain ASCII. The bar shrinks first, to minBarCells; then the label is
// dropped; then the bar, then the time.
func renderProgress(width, done, total int, label string, elapsed time.Duration) string {
	limit := width - 1
	counter := fmt.Sprintf("%*d/%d", len(strconv.Itoa(total)), done, total)
	clock := formatElapsed(elapsed)
	label = printableASCII(label)

	var tails []string
	if label != "" {
		tails = append(tails, "  "+counter+"  "+label+"  "+clock)
	}
	tails = append(tails, "  "+counter+"  "+clock)
	for _, tail := range tails {
		if cells := min(limit-2-len(tail), maxBarCells); cells >= minBarCells {
			return "[" + bar(done, total, cells) + "]" + tail
		}
	}
	counter = strings.TrimSpace(counter)
	for _, s := range []string{counter + "  " + clock, counter} {
		if len(s) <= limit {
			return s
		}
	}
	return counter[:max(limit, 0)]
}

// bar draws done of total in cells: spaces for what is to do, "=" for what is
// done and ">" at its end. The bar is full only when done reaches total.
func bar(done, total, cells int) string {
	done = max(0, min(done, total))
	filled := done * cells / total
	switch filled {
	case 0:
		return strings.Repeat(" ", cells)
	case cells:
		return strings.Repeat("=", cells)
	}
	return strings.Repeat("=", filled-1) + ">" + strings.Repeat(" ", cells-filled)
}

// formatElapsed shows d in whole seconds as 12s, 1m05s or 1h02m03s.
func formatElapsed(d time.Duration) string {
	s := int(d.Seconds())
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", max(s, 0))
	case s < 3600:
		return fmt.Sprintf("%dm%02ds", s/60, s%60)
	}
	return fmt.Sprintf("%dh%02dm%02ds", s/3600, s/60%60, s%60)
}

// printableASCII returns s with every byte outside 0x20 to 0x7e replaced by
// "?", so that a script's names cannot send control sequences to the terminal
// and every byte is one column.
func printableASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			r = '?'
		}
		b.WriteRune(r)
	}
	return b.String()
}

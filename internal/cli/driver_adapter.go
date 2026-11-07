package cli

import (
	"errors"
	"time"

	"github.com/michaellorant/shellshot/internal/script/keys"
	"github.com/michaellorant/shellshot/internal/script/run"
	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/vt"
	"github.com/michaellorant/shellshot/internal/vt/ghostty"
)

// scriptExecutor is the part of *run.Executor that scriptDriver uses.
type scriptExecutor interface {
	Poll(now time.Duration, scr run.Screen) (run.Step, error)
	ChildExited(now time.Duration, status int)
	OpenSpans() []run.SpanOp
}

// A script starts once the child has drawn its first screen and left it
// alone for scriptStartQuiet, or scriptStartCap after the launch, whichever
// comes first. Typing earlier would race the shell's first prompt: the tty
// echoes the first characters before the prompt is drawn, and a line editor
// redraws the rest after it. Waiting never fails the script.
const (
	scriptStartQuiet = 150 * time.Millisecond
	scriptStartCap   = time.Second
)

// scriptDriver adapts a script executor to session.Driver. The two Step types
// differ only in how a span is carried: the session treats a span as opaque
// and the binSink, which records it, expects an event.Span.
//
// It holds the executor back until the child has settled (see
// scriptStartQuiet), and remembers the last screen it looked at, which is
// the screen of a script failure.
type scriptDriver struct {
	exec   scriptExecutor
	screen string

	started    bool
	seen       bool          // a screen was looked at before the start
	beforeText string        // the latest screen text before the start
	changedAt  time.Duration // when beforeText last changed
}

var _ session.Driver = (*scriptDriver)(nil)

func newScriptDriver(exec scriptExecutor) *scriptDriver { return &scriptDriver{exec: exec} }

// Poll implements session.Driver.
func (d *scriptDriver) Poll(now time.Duration, scr session.Screen) (session.Step, error) {
	watched := watchedScreen{scr: scr, last: &d.screen}
	if !d.started {
		if wait, hold := d.holdBack(now, watched.Text()); hold {
			return session.Step{Deadline: wait}, nil
		}
		d.started = true
	}
	st, err := d.exec.Poll(now, watched)
	if err != nil {
		return session.Step{}, err
	}
	out := session.Step{
		Write:    st.Write,
		Mark:     st.Mark,
		Shutdown: st.Shutdown,
		Deadline: st.Deadline,
		Done:     st.Done,
	}
	if st.Span != nil {
		out.Span = st.Span.Event
	}
	return out, nil
}

// holdBack decides, before the script has started, whether the executor must
// still wait. If so it returns the time to poll again.
func (d *scriptDriver) holdBack(now time.Duration, text string) (until time.Duration, hold bool) {
	if !d.seen || text != d.beforeText {
		d.changedAt = now
	}
	d.seen, d.beforeText = true, text
	if now >= scriptStartCap {
		return 0, false
	}
	if text == "" {
		return scriptStartCap, true
	}
	if quietAt := d.changedAt + scriptStartQuiet; now < quietAt {
		return min(quietAt, scriptStartCap), true
	}
	return 0, false
}

// ChildExited implements session.Driver.
func (d *scriptDriver) ChildExited(now time.Duration, status int) {
	d.exec.ChildExited(now, status)
}

// OpenSpans implements session.Driver.
func (d *scriptDriver) OpenSpans() []session.SpanOp {
	ops := d.exec.OpenSpans()
	out := make([]session.SpanOp, len(ops))
	for i, op := range ops {
		out[i] = op.Event
	}
	return out
}

// lastScreen returns the screen text the executor saw at its latest Poll.
func (d *scriptDriver) lastScreen() string { return d.screen }

// watchedScreen is a run.Screen that keeps the text it returns.
type watchedScreen struct {
	scr  session.Screen
	last *string
}

func (w watchedScreen) Text() string {
	*w.last = w.scr.Text()
	return *w.last
}

// termHolder captures the terminal that session.Run makes with the factory
// from factory, so the key encoder can read its modes. The terminal exists
// only inside Run, and both it and the encoder belong to the goroutine that
// runs Run, which is also the one that polls the driver.
type termHolder struct{ term vt.Terminal }

// factory returns a vt.Factory that makes terminals with inner and remembers
// the latest one.
func (h *termHolder) factory(inner vt.Factory) vt.Factory {
	return func(cols, rows int) (vt.Terminal, error) {
		t, err := inner(cols, rows)
		if err == nil {
			h.term = t
		}
		return t, err
	}
}

// lazyKeys is a run.Keys over a ghostty key encoder that is made when the
// first key is needed, because the terminal does not exist when the executor
// is built. It must be used only from the session goroutine.
type lazyKeys struct {
	holder *termHolder
	enc    *ghostty.KeyEncoder
}

var _ run.Keys = (*lazyKeys)(nil)

func (k *lazyKeys) encoder() (*ghostty.KeyEncoder, error) {
	if k.enc != nil {
		return k.enc, nil
	}
	if k.holder.term == nil {
		return nil, errors.New("key encoder used before the terminal exists")
	}
	enc, err := ghostty.NewKeyEncoder(k.holder.term)
	if err != nil {
		return nil, err
	}
	k.enc = enc
	return enc, nil
}

// Encode implements run.Keys.
func (k *lazyKeys) Encode(chord keys.Chord) ([]byte, error) {
	enc, err := k.encoder()
	if err != nil {
		return nil, err
	}
	return enc.Encode(chord)
}

// EncodeText implements run.Keys.
func (k *lazyKeys) EncodeText(s string) ([]byte, error) {
	enc, err := k.encoder()
	if err != nil {
		return nil, err
	}
	return enc.EncodeText(s)
}

// close releases the encoder, if one was made.
func (k *lazyKeys) close() {
	if k.enc != nil {
		k.enc.Close()
	}
}

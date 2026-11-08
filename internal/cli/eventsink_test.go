package cli

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/live"
	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/session"
)

// testClock is a settable clock.
type testClock struct{ t time.Time }

func (c *testClock) now() time.Time { return c.t }

// newTestSink returns a sink over an in-memory recording with the given head
// flags and options, driven by clk.
func newTestSink(t *testing.T, flags uint64, o bin.WriterOptions, clk *testClock) (*binSink, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	o.Now = clk.now
	w, err := bin.NewWriter(&buf, head.Head{Cols: 10, Rows: 2, Flags: flags}, o)
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	return newBinSink(w, clk.now), &buf
}

func TestBinSinkWritesEvents(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, buf := newTestSink(t, 0, bin.WriterOptions{Uncompressed: true}, clk)

	p := []byte("hello")
	if err := s.Output(10*time.Millisecond, p); err != nil {
		t.Fatal(err)
	}
	copy(p, "XXXXX") // the sink must not retain p
	if err := s.Resize(20*time.Millisecond, 30, 4); err != nil {
		t.Fatal(err)
	}
	if err := s.Exit(30*time.Millisecond, 5); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}

	rec := readRecording(t, buf.Bytes())
	if !errors.Is(rec.err, io.EOF) || len(rec.events) != 3 {
		t.Fatalf("read = %d events, %v", len(rec.events), rec.err)
	}
	if e := rec.events[0]; e.Kind != event.KindOut || string(e.Data) != "hello" || e.At != 10*time.Millisecond {
		t.Errorf("event 0 = %+v", e)
	}
	rp, err := event.ParseResize(rec.events[1].Data)
	if rec.events[1].Kind != event.KindResize || err != nil || rp.Cols != 30 || rp.Rows != 4 {
		t.Errorf("event 1 = %+v (%+v, %v)", rec.events[1], rp, err)
	}
	if st, err := event.ParseExit(rec.events[2].Data); rec.events[2].Kind != event.KindExit || err != nil || st != 5 {
		t.Errorf("event 2 = %+v (%d, %v)", rec.events[2], st, err)
	}
}

func TestBinSinkInputIsStampedFromSessionOrigin(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, buf := newTestSink(t, head.FlagInputRecorded, bin.WriterOptions{Uncompressed: true}, clk)

	if err := s.input([]byte("early")); err != nil { // before the session starts
		t.Fatal(err)
	}
	s.start(clk.t)
	clk.t = clk.t.Add(750 * time.Millisecond)
	if err := s.input([]byte("k")); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}

	rec := readRecording(t, buf.Bytes())
	if len(rec.events) != 2 || rec.events[0].At != 0 || rec.events[1].At != 750*time.Millisecond {
		t.Errorf("events = %+v, want IN at 0 and at 750ms", rec.events)
	}
}

func TestBinSinkInputSurvivesPumpBufferReuseWhileCoalescing(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, buf := newTestSink(t, head.FlagInputRecorded, bin.WriterOptions{Uncompressed: true, CoalesceTicks: 100}, clk)
	s.start(clk.t)

	keys := []byte("first")
	if err := s.input(keys); err != nil {
		t.Fatal(err)
	}
	copy(keys, "XXXXX") // the input pump reuses its buffer; bin.Writer owns the copy
	if err := s.close(); err != nil {
		t.Fatal(err)
	}
	if got := readRecording(t, buf.Bytes()).in(); got != "first" {
		t.Errorf("IN = %q, want first", got)
	}
}

func TestBinSinkInputRequiresInputFlag(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, _ := newTestSink(t, 0, bin.WriterOptions{Uncompressed: true}, clk)
	if err := s.input([]byte("secret")); !errors.Is(err, bin.ErrInputNotRecorded) {
		t.Errorf("input without flag = %v, want ErrInputNotRecorded", err)
	}
}

func TestBinSinkTickFlushesOnlyWhenDue(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, buf := newTestSink(t, 0, bin.WriterOptions{Uncompressed: true, FlushInterval: 2 * time.Second}, clk)
	headBytes := buf.Len()

	// Idle: ticks write nothing, however many and however late.
	for i := 0; i < 5; i++ {
		clk.t = clk.t.Add(10 * time.Second)
		if err := s.tick(clk.t); err != nil {
			t.Fatal(err)
		}
	}
	if buf.Len() != headBytes {
		t.Fatalf("idle ticks wrote %d bytes", buf.Len()-headBytes)
	}

	if err := s.Output(0, []byte("x")); err != nil {
		t.Fatal(err)
	}
	clk.t = clk.t.Add(time.Second)
	if err := s.tick(clk.t); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != headBytes {
		t.Fatal("tick before the flush interval wrote a block")
	}
	clk.t = clk.t.Add(1500 * time.Millisecond)
	if err := s.tick(clk.t); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == headBytes {
		t.Fatal("tick after the flush interval wrote nothing")
	}
	if got := readRecording(t, buf.Bytes()).out(); got != "x" {
		t.Errorf("flushed OUT = %q, want x", got)
	}
	if st := s.stats(); st.Events != 1 || st.BytesWritten != uint64(buf.Len()) {
		t.Errorf("stats = %+v, want 1 event and %d bytes", st, buf.Len())
	}
}

func TestBinSinkConfigure(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, _ := newTestSink(t, 0, bin.WriterOptions{Uncompressed: true}, clk)
	var cfg session.Config
	s.configure(&cfg)
	if cfg.Events != session.EventSink(s) || cfg.Clock == nil || cfg.OnStart == nil || cfg.OnTick == nil {
		t.Errorf("config = %+v, want events, clock and hooks set", cfg)
	}
	if got := cfg.Clock(); !got.Equal(clk.t) {
		t.Errorf("Clock() = %v, want the sink clock", got)
	}
}

// Compile-time check that the adapter satisfies the session's event sink,
// including the mark and span events a driver produces.
var _ session.EventSink = (*binSink)(nil)

func TestBinSinkWritesMarkAndSpan(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, buf := newTestSink(t, 0, bin.WriterOptions{Uncompressed: true}, clk)

	begin := event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanScene, Name: "build"}
	end := event.Span{Op: event.SpanEnd, ID: 1}
	if err := s.Output(10*time.Millisecond, []byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := s.Span(20*time.Millisecond, begin); err != nil {
		t.Fatal(err)
	}
	if err := s.Mark(30*time.Millisecond, "ready"); err != nil {
		t.Fatal(err)
	}
	if err := s.Span(40*time.Millisecond, end); err != nil {
		t.Fatal(err)
	}
	if err := s.close(); err != nil {
		t.Fatal(err)
	}

	rec := readRecording(t, buf.Bytes())
	if !errors.Is(rec.err, io.EOF) || len(rec.events) != 4 {
		t.Fatalf("read = %d events, %v", len(rec.events), rec.err)
	}
	if e := rec.events[1]; e.Kind != event.KindSpan || e.At != 20*time.Millisecond {
		t.Errorf("event 1 = %+v, want span at 20ms", e)
	} else if got, err := event.ParseSpan(e.Data); err != nil || got != begin {
		t.Errorf("span = %+v, %v, want %+v", got, err, begin)
	}
	if e := rec.events[2]; e.Kind != event.KindMark || string(e.Data) != "ready" || e.At != 30*time.Millisecond {
		t.Errorf("event 2 = %+v, want mark ready at 30ms", e)
	}
	if e := rec.events[3]; e.Kind != event.KindSpan {
		t.Errorf("event 3 = %+v, want span end", e)
	}
}

func TestBinSinkSpanRejectsOtherOperations(t *testing.T) {
	t.Parallel()
	clk := &testClock{t: time.Unix(1000, 0)}
	s, _ := newTestSink(t, 0, bin.WriterOptions{Uncompressed: true}, clk)
	if err := s.Span(0, "not a span"); err == nil {
		t.Error("Span(string) = nil, want an error")
	}
	if err := s.Span(0, event.Span{Op: event.SpanEnd}); !errors.Is(err, event.ErrMalformedSpan) {
		t.Errorf("Span(invalid) = %v, want ErrMalformedSpan", err)
	}
}

// foregroundProcess is a live.Process that reports its foreground state.
type foregroundProcess struct {
	live.Process
	foreground bool
}

func (p foregroundProcess) ShellInForeground() (bool, error) { return p.foreground, nil }

func TestInputRecorderForwardsShellInForeground(t *testing.T) {
	for _, foreground := range []bool{true, false} {
		r := inputRecorder{Process: foregroundProcess{foreground: foreground}}
		if got, err := r.ShellInForeground(); err != nil || got != foreground {
			t.Errorf("ShellInForeground = (%v, %v), want (%v, nil)", got, err, foreground)
		}
	}
}

func TestInputRecorderShellInForegroundWithoutReporterReturnsErrNoForeground(t *testing.T) {
	r := inputRecorder{Process: struct{ live.Process }{}}
	if got, err := r.ShellInForeground(); got || !errors.Is(err, session.ErrNoForeground) {
		t.Errorf("ShellInForeground = (%v, %v), want (false, session.ErrNoForeground)", got, err)
	}
}

// Every layer that record --script puts around the pty process must keep the
// foreground query that wait done needs.
var (
	_ live.ForegroundReporter = (*trackedProcess)(nil)
	_ live.ForegroundReporter = inputRecorder{}
)

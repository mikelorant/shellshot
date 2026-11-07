package cli

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/event"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
	"github.com/michaellorant/shellshot/internal/session"
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
	clk := &testClock{t: time.Unix(1000, 0)}
	s, _ := newTestSink(t, 0, bin.WriterOptions{Uncompressed: true}, clk)
	if err := s.input([]byte("secret")); !errors.Is(err, bin.ErrInputNotRecorded) {
		t.Errorf("input without flag = %v, want ErrInputNotRecorded", err)
	}
}

func TestBinSinkTickFlushesOnlyWhenDue(t *testing.T) {
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

package bin

import (
	"bytes"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
)

func spanOf(t *testing.T, e Event) event.Span {
	t.Helper()
	if e.Kind != event.KindSpan {
		t.Fatalf("kind = %v, want span", e.Kind)
	}
	s, err := event.ParseSpan(e.Data)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWriterSpanRoundTrip(t *testing.T) {
	t.Parallel()
	begin := event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanScene, Name: "install",
		Hints: event.SpanHints{Speed: 8000, Idle: 2 * time.Second, Hold: time.Second}}
	end := event.Span{Op: event.SpanEnd, ID: 1}

	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{})
	for _, err := range []error{
		w.Span(0, begin),
		w.Output(time.Millisecond, []byte("x")),
		w.Span(2*time.Millisecond, end),
		w.Close(),
	} {
		if err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewReader(&buf, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := drain(t, r)
	if err != io.EOF {
		t.Fatal(err)
	}
	if len(evs) != 3 || evs[1].Kind != event.KindOut {
		t.Fatalf("events = %+v", toWant(evs))
	}
	if got := spanOf(t, evs[0]); got != begin {
		t.Errorf("begin = %+v", got)
	}
	if got := spanOf(t, evs[2]); got != end || evs[2].At != 2*time.Millisecond {
		t.Errorf("end = %+v at %v", got, evs[2].At)
	}
}

func TestWriterSpanRejectsInvalid(t *testing.T) {
	t.Parallel()
	w := mustWriter(t, io.Discard, testHead(), WriterOptions{})
	err := w.Span(0, event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanScene, Name: string(make([]byte, 256))})
	if !errors.Is(err, event.ErrMalformedSpan) {
		t.Fatalf("err = %v", err)
	}
	if w.Stats().Events != 0 {
		t.Fatal("invalid span was written")
	}
}

// Spans must not be merged into, or reordered around, coalesced OUT runs.
func TestWriterSpanOrderWithCoalescing(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{CoalesceTicks: 1000}) // 100ms
	begin := event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanScene, Name: "a"}
	end := event.Span{Op: event.SpanEnd, ID: 1}
	steps := []func() error{
		func() error { return w.Output(0, []byte("a")) },
		func() error { return w.Output(time.Millisecond, []byte("b")) },
		func() error { return w.Span(2*time.Millisecond, begin) },
		func() error { return w.Output(3*time.Millisecond, []byte("c")) },
		func() error { return w.Output(4*time.Millisecond, []byte("d")) },
		func() error { return w.Span(5*time.Millisecond, end) },
		func() error { return w.Span(5*time.Millisecond, event.Span{Op: event.SpanEnd, ID: 2}) },
		func() error { return w.Output(6*time.Millisecond, []byte("e")) },
		w.Close,
	}
	for _, f := range steps {
		if err := f(); err != nil {
			t.Fatal(err)
		}
	}
	r, err := NewReader(&buf, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := drain(t, r)
	if err != io.EOF {
		t.Fatal(err)
	}
	var got []string
	for _, e := range evs {
		if e.Kind == event.KindSpan {
			s := spanOf(t, e)
			got = append(got, map[event.SpanOp]string{event.SpanBegin: "B", event.SpanEnd: "E"}[s.Op])
		} else {
			got = append(got, string(e.Data))
		}
	}
	want := []string{"ab", "B", "cd", "E", "E", "e"}
	if len(got) != len(want) {
		t.Fatalf("events = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("events = %v, want %v", got, want)
		}
	}
}

// A span written just before Close must reach the file, with and without
// coalescing, and a span alone in the pending block must flush on Close.
func TestWriterSpanFlushedOnClose(t *testing.T) {
	t.Parallel()
	for _, co := range []uint64{0, 1000} {
		rw := newRecordingWriter()
		w := mustWriter(t, rw, testHead(), WriterOptions{CoalesceTicks: co})
		headerWrites := len(rw.writes)
		if err := w.Span(0, event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanHidden}); err != nil {
			t.Fatal(err)
		}
		if len(rw.writes) != headerWrites {
			t.Fatalf("coalesce=%d: span flushed before Close; the policy is size/time/close", co)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if len(rw.writes) != headerWrites+1 {
			t.Fatalf("coalesce=%d: Close wrote %d chunks, want 1", co, len(rw.writes)-headerWrites)
		}
		r, err := NewReader(bytes.NewReader(rw.buf.Bytes()), ReaderOptions{})
		if err != nil {
			t.Fatal(err)
		}
		evs, _ := drain(t, r)
		if len(evs) != 1 || spanOf(t, evs[0]).Type != event.SpanHidden {
			t.Fatalf("coalesce=%d: events = %+v", co, toWant(evs))
		}
	}
}

func TestWriterSpanFlushBySizeAndTime(t *testing.T) {
	t.Parallel()
	clock := newFakeClock()
	rw := newRecordingWriter()
	w := mustWriter(t, rw, testHead(), WriterOptions{Now: clock.Now, FlushInterval: time.Second})
	n := len(rw.writes)
	if err := w.Span(0, event.Span{Op: event.SpanEnd, ID: 1}); err != nil {
		t.Fatal(err)
	}
	clock.Advance(time.Second)
	if err := w.Tick(clock.Now()); err != nil {
		t.Fatal(err)
	}
	if len(rw.writes) != n+1 {
		t.Fatalf("Tick did not flush a pending span")
	}
}

// A reader written before SPAN existed treats kind 64 as an opaque ancillary
// event: it must still see every other event, in order.
func TestSpansPassThroughOldReader(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	w := mustWriter(t, &buf, testHead(), WriterOptions{})
	if err := w.Output(0, []byte("a")); err != nil {
		t.Fatal(err)
	}
	if err := w.Span(time.Millisecond, event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanScene, Name: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := w.Mark(2*time.Millisecond, "m"); err != nil {
		t.Fatal(err)
	}
	if err := w.Span(3*time.Millisecond, event.Span{Op: event.SpanEnd, ID: 1}); err != nil {
		t.Fatal(err)
	}
	if err := w.Exit(4*time.Millisecond, 0); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(&buf, ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := drain(t, r)
	if err != io.EOF {
		t.Fatal(err)
	}
	var old []wantEvent // what a consumer that switches on the five known kinds sees
	for _, e := range evs {
		if e.Kind.IsAncillary() {
			continue
		}
		old = append(old, wantEvent{e.At, e.Kind, string(e.Data)})
	}
	want := []wantEvent{
		{0, event.KindOut, "a"},
		{2 * time.Millisecond, event.KindMark, "m"},
		{4 * time.Millisecond, event.KindExit, "\x00"},
	}
	if !equalWant(old, want) {
		t.Fatalf("old view = %+v", old)
	}
	if len(evs) != 5 {
		t.Fatalf("got %d events, want 5", len(evs))
	}
}

// A recorder killed mid-write leaves a torn tail; the SPAN begin in the
// intact blocks must still be readable.
func TestSpanBeginSurvivesTornTail(t *testing.T) {
	t.Parallel()
	rw := newRecordingWriter()
	w := mustWriter(t, rw, testHead(), WriterOptions{})
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(w.Span(0, event.Span{Op: event.SpanBegin, ID: 1, Type: event.SpanScene, Name: "s"}))
	must(w.Output(time.Millisecond, []byte("hello")))
	must(w.Flush())
	good := rw.buf.Len()
	must(w.Output(2*time.Millisecond, []byte("lost")))
	must(w.Span(3*time.Millisecond, event.Span{Op: event.SpanEnd, ID: 1}))
	must(w.Close())

	torn := rw.buf.Bytes()[:rw.buf.Len()-3]
	r, err := NewReader(bytes.NewReader(torn), ReaderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	evs, err := drain(t, r)
	if !errors.Is(err, ErrTruncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}
	if len(evs) != 2 || spanOf(t, evs[0]).Op != event.SpanBegin || r.ValidBytes() != int64(good) {
		t.Fatalf("events = %+v, valid = %d, want %d", toWant(evs), r.ValidBytes(), good)
	}
}

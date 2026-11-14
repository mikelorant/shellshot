package live_test

import (
	"bytes"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/live"
)

// chanWriter hands every Write to the test and can block the goroutine that
// writes until the test releases it.
type chanWriter struct {
	mu      sync.Mutex
	buf     bytes.Buffer
	written chan struct{} // receives a value after each Write
	gate    chan struct{} // if non-nil, Write waits for it to be closed
	entered chan struct{} // if non-nil, receives a value as a Write begins
	err     error
}

func newChanWriter() *chanWriter { return &chanWriter{written: make(chan struct{}, 1024)} }

func (w *chanWriter) Write(p []byte) (int, error) {
	if w.entered != nil {
		select {
		case w.entered <- struct{}{}:
		default:
		}
	}
	if w.gate != nil {
		<-w.gate
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.err != nil {
		w.written <- struct{}{}
		return 0, w.err
	}
	w.buf.Write(p)
	w.written <- struct{}{}
	return len(p), nil
}

func (w *chanWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func waitFor(t *testing.T, w *chanWriter, want string) {
	t.Helper()
	timeout := time.After(30 * time.Second)
	for w.String() != want {
		select {
		case <-w.written:
		case <-timeout:
			t.Fatalf("written = %q, want %q", w.String(), want)
		}
	}
}

func TestRepliesDeliversInOrder(t *testing.T) {
	w := newChanWriter()
	r := live.NewReplies(w)
	r.Send([]byte("one,"))
	r.Send([]byte("two,"))
	r.Send([]byte("three"))
	waitFor(t, w, "one,two,three")
	r.Close()
}

func TestRepliesSendDoesNotBlockOnAStuckWriter(t *testing.T) {
	w := newChanWriter()
	w.gate = make(chan struct{})
	r := live.NewReplies(w)

	sent := make(chan struct{})
	go func() {
		defer close(sent)
		for range 1000 {
			r.Send([]byte("0123456789"))
		}
	}()
	select {
	case <-sent:
	case <-time.After(30 * time.Second):
		t.Fatal("Send blocked while the writer was stuck")
	}

	close(w.gate)
	r.Close()
	if got := len(w.String()); got > live.MaxPendingReplies+10 {
		t.Errorf("delivered %d bytes, want at most the cap plus one reply in flight", got)
	}
}

func TestRepliesDropsBeyondTheCap(t *testing.T) {
	w := newChanWriter()
	w.gate = make(chan struct{})
	w.entered = make(chan struct{}, 1)
	r := live.NewReplies(w)
	r.Send([]byte("a")) // taken by the goroutine, which then blocks in Write
	select {
	case <-w.entered: // the goroutine holds "a" alone; what follows is only queued
	case <-time.After(30 * time.Second):
		t.Fatal("the writer never started its first Write")
	}
	for range live.MaxPendingReplies + 100 {
		r.Send([]byte("x"))
	}
	close(w.gate)
	r.Close()
	if got := w.String(); !strings.HasPrefix(got, "a") || len(got) > live.MaxPendingReplies+1 {
		t.Errorf("delivered %d bytes starting %q, want at most %d starting with a", len(got), got[:1], live.MaxPendingReplies+1)
	}
}

func TestRepliesStopsAfterWriteError(t *testing.T) {
	w := newChanWriter()
	w.err = errors.New("closed")
	r := live.NewReplies(w)
	r.Send([]byte("a"))
	<-w.written
	r.Send([]byte("b"))
	r.Close()
	if got := w.String(); got != "" {
		t.Errorf("written = %q, want nothing", got)
	}
}

func TestRepliesCloseIsIdempotentAndDropsLaterSends(t *testing.T) {
	w := newChanWriter()
	r := live.NewReplies(w)
	r.Close()
	r.Close()
	r.Send([]byte("late"))
	if got := w.String(); got != "" {
		t.Errorf("written = %q, want nothing", got)
	}
}

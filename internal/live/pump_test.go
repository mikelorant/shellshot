package live

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

const wait = 2 * time.Second

func receive(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err, ok := <-ch:
		if !ok {
			t.Fatal("channel closed without value")
		}
		return err
	case <-time.After(wait):
		t.Fatal("timed out waiting for Pump result")
		return nil
	}
}

func assertClosed(t *testing.T, ch <-chan error) {
	t.Helper()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel yielded a second value")
		}
	case <-time.After(wait):
		t.Fatal("channel not closed")
	}
}

func TestPumpForwardsInputAndReturnsNilOnEOF(t *testing.T) {
	var dst bytes.Buffer
	ch := Pump(context.Background(), strings.NewReader("typed input"), &dst)
	if err := receive(t, ch); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	assertClosed(t, ch)
	if dst.String() != "typed input" {
		t.Fatalf("dst = %q", dst.String())
	}
}

func TestPumpReturnsWriteError(t *testing.T) {
	ch := Pump(context.Background(), strings.NewReader("x"), &failingWriter{})
	if err := receive(t, ch); err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v; want boom", err)
	}
	assertClosed(t, ch)
}

func TestPumpReturnsReadError(t *testing.T) {
	want := errors.New("read failed")
	ch := Pump(context.Background(), errReader{want}, io.Discard)
	if err := receive(t, ch); !errors.Is(err, want) {
		t.Fatalf("err = %v; want %v", err, want)
	}
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

func TestPumpCancelReturnsPromptlyWhileReadBlocks(t *testing.T) {
	pr, pw := io.Pipe() // never written: Read blocks until closed
	ctx, cancel := context.WithCancel(context.Background())
	ch := Pump(ctx, pr, io.Discard)

	cancel()
	if err := receive(t, ch); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	assertClosed(t, ch)

	// Unblock the lingering goroutine so the test leaves nothing behind.
	pw.Close()
}

// writeRecorder records each Write call separately.
type writeRecorder struct{ writes []string }

func (w *writeRecorder) Write(b []byte) (int, error) {
	w.writes = append(w.writes, string(b))
	return len(b), nil
}

func TestPumpWithWritesOnEOFAfterInput(t *testing.T) {
	var dst writeRecorder
	ch := PumpWith(context.Background(), strings.NewReader("echo hi\n"), &dst, PumpOptions{OnEOF: []byte{0x04}})
	if err := receive(t, ch); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	assertClosed(t, ch)
	if got := strings.Join(dst.writes, "|"); got != "echo hi\n|\x04" {
		t.Fatalf("writes = %q; want input then one EOT", dst.writes)
	}
}

func TestPumpWithoutOnEOFWritesNothingExtra(t *testing.T) {
	var dst writeRecorder
	ch := PumpWith(context.Background(), strings.NewReader("x"), &dst, PumpOptions{})
	receive(t, ch)
	if len(dst.writes) != 1 || dst.writes[0] != "x" {
		t.Fatalf("writes = %q; want only the input", dst.writes)
	}
}

func TestPumpWithSkipsOnEOFOnReadError(t *testing.T) {
	var dst writeRecorder
	want := errors.New("read failed")
	ch := PumpWith(context.Background(), errReader{want}, &dst, PumpOptions{OnEOF: []byte{0x04}})
	if err := receive(t, ch); !errors.Is(err, want) {
		t.Fatalf("err = %v; want %v", err, want)
	}
	if len(dst.writes) != 0 {
		t.Fatalf("writes = %q; want none", dst.writes)
	}
}

// failOnWrite succeeds until its nth write (1-based), which fails with boom.
type failOnWrite struct{ n, calls int }

func (f *failOnWrite) Write(b []byte) (int, error) {
	f.calls++
	if f.calls == f.n {
		return 0, errors.New("boom")
	}
	return len(b), nil
}

func TestPumpWithReturnsOnEOFWriteError(t *testing.T) {
	ch := PumpWith(context.Background(), strings.NewReader("x"), &failOnWrite{n: 2}, PumpOptions{OnEOF: []byte{0x04}})
	if err := receive(t, ch); err == nil || err.Error() != "boom" {
		t.Fatalf("err = %v; want boom", err)
	}
}

func TestPumpWithSkipsOnEOFWhenNothingForwarded(t *testing.T) {
	var dst writeRecorder
	called := false
	opts := PumpOptions{OnEOF: []byte{0x04}, BeforeOnEOF: func() { called = true }}
	ch := PumpWith(context.Background(), strings.NewReader(""), &dst, opts)
	if err := receive(t, ch); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if len(dst.writes) != 0 {
		t.Fatalf("writes = %q; want none for empty input", dst.writes)
	}
	if called {
		t.Fatal("BeforeOnEOF called although no EOT was written")
	}
}

func TestPumpWithCallsBeforeOnEOFBetweenInputAndEOT(t *testing.T) {
	var dst writeRecorder
	opts := PumpOptions{OnEOF: []byte{0x04}, BeforeOnEOF: func() { dst.writes = append(dst.writes, "<before>") }}
	ch := PumpWith(context.Background(), strings.NewReader("x"), &dst, opts)
	if err := receive(t, ch); err != nil {
		t.Fatalf("err = %v; want nil", err)
	}
	if got := strings.Join(dst.writes, "|"); got != "x|<before>|\x04" {
		t.Fatalf("writes = %q; want input, hook, then EOT", dst.writes)
	}
}

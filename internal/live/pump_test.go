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

package pty

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func start(t *testing.T, ctx context.Context, cfg Config) *Process {
	t.Helper()
	p, err := Start(ctx, cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestStartOutputAndExitCode(t *testing.T) {
	p := start(t, context.Background(), Config{Command: "sh", Args: []string{"-c", "printf hi; exit 3"}})
	out, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(out) != "hi" {
		t.Errorf("output = %q, want %q", out, "hi")
	}
	code, err := p.Wait()
	if err != nil || code != 3 {
		t.Errorf("Wait = (%d, %v), want (3, nil)", code, err)
	}
}

func TestEchoZeroExit(t *testing.T) {
	p := start(t, context.Background(), Config{Command: "echo", Args: []string{"hello"}})
	out, err := io.ReadAll(p)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "hello" {
		t.Errorf("output = %q, want hello", got)
	}
	if code, err := p.Wait(); err != nil || code != 0 {
		t.Errorf("Wait = (%d, %v), want (0, nil)", code, err)
	}
}

func TestDefaultSizeAndTerm(t *testing.T) {
	p := start(t, context.Background(), Config{Command: "sh", Args: []string{"-c", "stty size; echo $TERM"}, Env: []string{"PATH=/usr/bin:/bin"}})
	out, _ := io.ReadAll(p)
	if got := strings.Fields(string(out)); len(got) != 3 || got[0] != "24" || got[1] != "80" || got[2] != "xterm-256color" {
		t.Errorf("output = %q, want 24 80 xterm-256color", out)
	}
}

func TestResize(t *testing.T) {
	p := start(t, context.Background(), Config{
		Command: "sh", Args: []string{"-c", "sleep 1; stty size"}, Cols: 100, Rows: 30,
	})
	if err := p.Resize(120, 40); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	out, _ := io.ReadAll(p)
	if got := strings.TrimSpace(string(out)); got != "40 120" {
		t.Errorf("stty size = %q, want %q", got, "40 120")
	}
}

func TestInvalidSize(t *testing.T) {
	if _, err := Start(context.Background(), Config{Command: "true", Cols: -1}); err == nil {
		t.Error("Start with negative cols: want error")
	}
	p := start(t, context.Background(), Config{Command: "sleep", Args: []string{"30"}})
	if err := p.Resize(0, 10); err == nil {
		t.Error("Resize(0, 10): want error")
	}
}

func TestStartErrors(t *testing.T) {
	if _, err := Start(context.Background(), Config{}); err == nil {
		t.Error("empty command: want error")
	}
	if _, err := Start(context.Background(), Config{Command: "/nonexistent/shellshot-cmd"}); err == nil {
		t.Error("missing binary: want error")
	}
}

func TestContextCancelKills(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	p := start(t, ctx, Config{Command: "sleep", Args: []string{"30"}})
	cancel()

	type result struct {
		code int
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		code, err := p.Wait()
		ch <- result{code, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil || r.code == 0 {
			t.Errorf("Wait = (%d, %v), want non-zero code and nil error", r.code, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("process not killed after context cancel")
	}
}

func TestCloseKillsAndIsIdempotent(t *testing.T) {
	p, err := Start(context.Background(), Config{Command: "sleep", Args: []string{"30"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	if code, err := p.Wait(); err != nil || code == 0 {
		t.Errorf("Wait after Close = (%d, %v), want non-zero code and nil error", code, err)
	}
}

func TestWriteReachesChild(t *testing.T) {
	p := start(t, context.Background(), Config{Command: "cat"})
	if n, err := p.Write([]byte("ping\n")); err != nil || n != 5 {
		t.Fatalf("Write = (%d, %v), want (5, nil)", n, err)
	}

	found := make(chan error, 1)
	go func() {
		var got []byte
		buf := make([]byte, 256)
		for !strings.Contains(string(got), "ping") {
			n, err := p.Read(buf)
			got = append(got, buf[:n]...)
			if err != nil {
				found <- err
				return
			}
		}
		found <- nil
	}()
	select {
	case err := <-found:
		if err != nil {
			t.Fatalf("Read before seeing ping: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for ping")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestWriteAfterCloseFails(t *testing.T) {
	p, err := Start(context.Background(), Config{Command: "cat"})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	n, err := p.Write([]byte("x"))
	if err == nil || !errors.Is(err, os.ErrClosed) || n != 0 {
		t.Errorf("Write after Close = (%d, %v), want (0, error wrapping os.ErrClosed)", n, err)
	}
}

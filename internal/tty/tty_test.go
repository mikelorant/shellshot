package tty

import (
	"os"
	"testing"

	"github.com/creack/pty"
	"golang.org/x/term"
)

func nonTerminalFiles(t *testing.T) map[string]*os.File {
	t.Helper()
	tmp, err := os.CreateTemp(t.TempDir(), "tty")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { tmp.Close() })
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return map[string]*os.File{"tempfile": tmp, "pipe": r}
}

func TestNonTerminal(t *testing.T) {
	for name, f := range nonTerminalFiles(t) {
		t.Run(name, func(t *testing.T) {
			if IsTerminal(f) {
				t.Error("IsTerminal = true, want false")
			}
			if _, _, err := Size(f); err == nil {
				t.Error("Size error = nil, want error")
			}
			restore, err := MakeRaw(f)
			if err == nil {
				t.Error("MakeRaw error = nil, want error")
			}
			if restore != nil {
				t.Error("MakeRaw restore != nil on error")
			}
		})
	}
}

func openPTY(t *testing.T) *os.File {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Skipf("cannot open pty: %v", err)
	}
	t.Cleanup(func() { tty.Close(); ptmx.Close() })
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 31, Cols: 97}); err != nil {
		t.Fatal(err)
	}
	return tty
}

func TestPTYIsTerminalAndSize(t *testing.T) {
	tty := openPTY(t)
	if !IsTerminal(tty) {
		t.Error("IsTerminal = false, want true")
	}
	cols, rows, err := Size(tty)
	if err != nil {
		t.Fatal(err)
	}
	if cols != 97 || rows != 31 {
		t.Errorf("Size = %dx%d, want 97x31", cols, rows)
	}
}

func TestPTYMakeRawRestore(t *testing.T) {
	tty := openPTY(t)
	fd := int(tty.Fd())
	before, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	restore, err := MakeRaw(tty)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	if *raw == *before {
		t.Error("terminal state unchanged after MakeRaw")
	}
	if err := restore(); err != nil {
		t.Fatalf("first restore: %v", err)
	}
	after, err := term.GetState(fd)
	if err != nil {
		t.Fatal(err)
	}
	if *after != *before {
		t.Error("terminal state not restored")
	}
	if err := restore(); err != nil {
		t.Fatalf("second restore: %v", err)
	}
}

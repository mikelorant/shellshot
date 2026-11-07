package ghostty_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/michaellorant/shellshot/internal/snapshot"
	"github.com/michaellorant/shellshot/internal/vt"
	"github.com/michaellorant/shellshot/internal/vt/ghostty"
)

func newTerminal(t *testing.T, cols, rows int) vt.Terminal {
	t.Helper()
	term, err := ghostty.New(cols, rows)
	if err != nil {
		t.Fatalf("New(%d, %d): %v", cols, rows, err)
	}
	t.Cleanup(func() { term.Close() })
	return term
}

func write(t *testing.T, term vt.Terminal, s string) {
	t.Helper()
	n, err := term.Write([]byte(s))
	if err != nil || n != len(s) {
		t.Fatalf("Write(%q) = %d, %v; want %d, nil", s, n, err, len(s))
	}
}

func snap(t *testing.T, term vt.Terminal) snapshot.Snapshot {
	t.Helper()
	s, err := term.Snapshot()
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return s
}

func wantLines(t *testing.T, s snapshot.Snapshot, rows int, nonEmpty ...string) {
	t.Helper()
	want := make([]string, rows)
	copy(want, nonEmpty)
	if s.Rows != rows || !reflect.DeepEqual(s.Lines, want) {
		t.Fatalf("Rows=%d Lines=%q; want Rows=%d Lines=%q", s.Rows, s.Lines, rows, want)
	}
}

func TestHelloWorld(t *testing.T) {
	term := newTerminal(t, 20, 4)
	write(t, term, "hello world")

	s := snap(t, term)
	wantLines(t, s, 4, "hello world")
	if s.Cols != 20 {
		t.Errorf("Cols = %d, want 20", s.Cols)
	}
	if want := (snapshot.Cursor{X: 11, Y: 0, Visible: true}); s.Cursor != want {
		t.Errorf("Cursor = %+v, want %+v", s.Cursor, want)
	}
	if s.Title != "" {
		t.Errorf("Title = %q, want empty", s.Title)
	}
}

func TestNewlineAndCursorMovement(t *testing.T) {
	term := newTerminal(t, 20, 4)

	write(t, term, "ab\r\ncd")
	s := snap(t, term)
	wantLines(t, s, 4, "ab", "cd")
	if s.Cursor.X != 2 || s.Cursor.Y != 1 {
		t.Errorf("cursor = (%d,%d), want (2,1)", s.Cursor.X, s.Cursor.Y)
	}

	// A bare line feed moves down without returning to column 0.
	write(t, term, "\nef")
	wantLines(t, snap(t, term), 4, "ab", "cd", "  ef")

	// CUP is 1-based: move to row 4, column 3, then overwrite.
	write(t, term, "\x1b[4;3HZ")
	s = snap(t, term)
	wantLines(t, s, 4, "ab", "cd", "  ef", "  Z")
	if s.Cursor.X != 3 || s.Cursor.Y != 3 {
		t.Errorf("cursor = (%d,%d), want (3,3)", s.Cursor.X, s.Cursor.Y)
	}
}

func TestScrollKeepsOnlyVisibleRows(t *testing.T) {
	term := newTerminal(t, 10, 3)
	write(t, term, "1\r\n2\r\n3\r\n4\r\n5")
	wantLines(t, snap(t, term), 3, "3", "4", "5")
}

func TestCursorVisibilityAndTitle(t *testing.T) {
	term := newTerminal(t, 20, 2)
	write(t, term, "\x1b[?25l\x1b]2;my title\x07")

	s := snap(t, term)
	if s.Cursor.Visible {
		t.Error("cursor should be hidden")
	}
	if s.Title != "my title" {
		t.Errorf("Title = %q, want %q", s.Title, "my title")
	}

	write(t, term, "\x1b[?25h")
	if !snap(t, term).Cursor.Visible {
		t.Error("cursor should be visible again")
	}
}

func TestSGRIsStrippedFromText(t *testing.T) {
	term := newTerminal(t, 30, 2)
	write(t, term, "\x1b[31mred\x1b[0m \x1b[1;44mbold\x1b[0m \x1b[38;2;1;2;3mtrue\x1b[0m")
	wantLines(t, snap(t, term), 2, "red bold true")
}

func TestTrailingSpacesAreTrimmed(t *testing.T) {
	term := newTerminal(t, 10, 2)
	write(t, term, "hi   \r\n  x   ")
	wantLines(t, snap(t, term), 2, "hi", "  x")
}

func TestResize(t *testing.T) {
	term := newTerminal(t, 20, 5)
	write(t, term, "one\r\ntwo")

	if err := term.Resize(10, 3); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	s := snap(t, term)
	wantLines(t, s, 3, "one", "two")
	if s.Cols != 10 {
		t.Errorf("Cols = %d, want 10", s.Cols)
	}

	if err := term.Resize(30, 6); err != nil {
		t.Fatalf("Resize grow: %v", err)
	}
	s = snap(t, term)
	wantLines(t, s, 6, "one", "two")
	if s.Cols != 30 {
		t.Errorf("Cols = %d, want 30", s.Cols)
	}
}

func TestInvalidSizes(t *testing.T) {
	for _, size := range [][2]int{{0, 5}, {5, 0}, {-1, 5}, {5, -1}, {70000, 5}, {5, 70000}} {
		if term, err := ghostty.New(size[0], size[1]); err == nil {
			term.Close()
			t.Errorf("New(%d, %d) succeeded, want error", size[0], size[1])
		}
	}

	term := newTerminal(t, 10, 3)
	if err := term.Resize(0, 3); err == nil {
		t.Error("Resize(0, 3) succeeded, want error")
	}
	wantLines(t, snap(t, term), 3)
}

func TestWideAndUnicodeCharacters(t *testing.T) {
	term := newTerminal(t, 20, 2)
	write(t, term, "a世界b é 😀")

	s := snap(t, term)
	wantLines(t, s, 2, "a世界b é 😀")
	// a(1) + 世界(4) + b(1) + space(1) + é(1) + space(1) + 😀(2) = 11
	if s.Cursor.X != 11 {
		t.Errorf("cursor X = %d, want 11", s.Cursor.X)
	}
}

func TestWideCharacterWrapsAtEdge(t *testing.T) {
	term := newTerminal(t, 5, 3)
	write(t, term, "abcd世")
	wantLines(t, snap(t, term), 3, "abcd", "世")
}

func TestSnapshotDoesNotAlias(t *testing.T) {
	term := newTerminal(t, 20, 3)
	write(t, term, "first")

	a := snap(t, term)
	a.Lines[0] = "mutated"
	b := snap(t, term)
	wantLines(t, b, 3, "first")

	write(t, term, "\r\nsecond")
	wantLines(t, b, 3, "first")
	wantLines(t, snap(t, term), 3, "first", "second")
}

func TestCloseIsIdempotentAndGuardsUse(t *testing.T) {
	term, err := ghostty.New(10, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := term.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	if _, err := term.Write([]byte("x")); !errors.Is(err, ghostty.ErrClosed) {
		t.Errorf("Write after Close error = %v, want ErrClosed", err)
	}
	if err := term.Resize(5, 5); !errors.Is(err, ghostty.ErrClosed) {
		t.Errorf("Resize after Close error = %v, want ErrClosed", err)
	}
	if _, err := term.Snapshot(); !errors.Is(err, ghostty.ErrClosed) {
		t.Errorf("Snapshot after Close error = %v, want ErrClosed", err)
	}
}

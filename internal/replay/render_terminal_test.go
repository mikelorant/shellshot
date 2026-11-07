package replay_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/replay"
	"github.com/mikelorant/shellshot/internal/snapshot"
	"github.com/mikelorant/shellshot/internal/vt"
	"github.com/mikelorant/shellshot/internal/vt/ghostty"
)

// TestRenderClearReproducesScreenOnTerminal feeds a series of screens to a
// libghostty terminal through Render (without ever clearing it) and checks that
// it shows exactly what a terminal fed the program output directly shows. This
// covers the overwrite drawing that replaced the clear-screen redraw: text that
// fills the row, wide runes, styled blanks, content that shrinks between frames
// and a hidden cursor.
func TestRenderClearReproducesScreenOnTerminal(t *testing.T) {
	const cols, rows = 20, 6
	source, err := ghostty.New(cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	viewer, err := ghostty.New(cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	defer viewer.Close()
	// Stale text on the viewer that the first frame must overwrite.
	if _, err := viewer.Write([]byte(strings.Repeat("stale text over there\r\n", rows))); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name   string
		output string
	}{
		{"rows full width", strings.Repeat(strings.Repeat("X", cols), rows-1) + "tail"},
		{"short styled content after a full screen", "\x1b[2J\x1b[H\x1b[31mred\x1b[0m plain\r\n\x1b[44m      \x1b[0m\r\n" + strings.Repeat("y", cols)},
		{"wide runes ending at the edge", "\x1b[2J\x1b[H" + strings.Repeat("世", cols/2) + "\r\nab\u0301c"},
		{"hidden cursor", "\x1b[?25l\x1b[2J\x1b[Hhidden"},
		{"cursor shown mid-screen", "\x1b[?25h\x1b[3;4Hhere"},
		{"empty screen", "\x1b[2J"},
	}
	for _, st := range steps {
		if _, err := source.Write([]byte(st.output)); err != nil {
			t.Fatal(err)
		}
		want, err := source.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := viewer.Write([]byte(replay.Render(want, replay.RenderOptions{Clear: true, Color: true}))); err != nil {
			t.Fatal(err)
		}
		got, err := viewer.Snapshot()
		if err != nil {
			t.Fatal(err)
		}
		if !got.Equal(want) {
			t.Errorf("%s: viewer shows\n%q\nwant\n%q\n(cursor got %+v want %+v)", st.name, got.Lines, want.Lines, got.Cursor, want.Cursor)
		}
	}
}

// TestPlayEndsWithUsableTerminal plays two frames with a hidden cursor onto a
// libghostty terminal that already shows a prompt line above, cancelling
// during the wait after the second, and checks the screen afterwards: the last
// frame is intact, the cursor is visible on its own line below the frame and
// there is room to print under it.
func TestPlayEndsWithUsableTerminal(t *testing.T) {
	const cols, rows = 20, 8
	term, err := ghostty.New(cols, rows)
	if err != nil {
		t.Fatal(err)
	}
	defer term.Close()

	frame := func(at time.Duration, lines ...string) snapshot.Frame {
		return snapshot.Frame{At: at, Snapshot: snapshot.Snapshot{Cols: cols, Rows: len(lines), Lines: lines}}
	}
	src := &frameList{frames: []snapshot.Frame{frame(0, "one", "uno"), frame(time.Second, "two", "dos"), frame(2*time.Second, "three", "tres")}}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	waits := 0
	_, err = replay.PlayWithStats(ctx, src, termWriter{t, term}, replay.Options{
		Clear: true,
		Sleep: func(ctx context.Context, _ time.Duration) error {
			if waits++; waits == 2 {
				cancel()
				return ctx.Err()
			}
			return nil
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Play() error = %v, want context.Canceled", err)
	}
	got, err := term.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got.Lines[0] != "two" || got.Lines[1] != "dos" {
		t.Errorf("screen = %q, want the last drawn frame intact", got.Lines)
	}
	if !got.Cursor.Visible || got.Cursor.X != 0 || got.Cursor.Y != 2 {
		t.Errorf("cursor = %+v, want it visible at the start of row 3, below the frame", got.Cursor)
	}
	// Room below: a summary line printed now lands under the frame.
	if _, err := term.Write([]byte("summary")); err != nil {
		t.Fatal(err)
	}
	if got, err = term.Snapshot(); err != nil {
		t.Fatal(err)
	}
	if got.Lines[0] != "two" || got.Lines[1] != "dos" || got.Lines[2] != "summary" {
		t.Errorf("screen after the summary = %q, want it under the frame", got.Lines)
	}
}

type frameList struct{ frames []snapshot.Frame }

func (l *frameList) Next() (snapshot.Frame, error) {
	if len(l.frames) == 0 {
		return snapshot.Frame{}, io.EOF
	}
	f := l.frames[0]
	l.frames = l.frames[1:]
	return f, nil
}

// termWriter feeds everything written to a terminal.
type termWriter struct {
	t    *testing.T
	term vt.Terminal
}

func (w termWriter) Write(p []byte) (int, error) {
	if _, err := w.term.Write(p); err != nil {
		w.t.Fatal(err)
	}
	return len(p), nil
}

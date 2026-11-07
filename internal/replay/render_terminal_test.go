package replay_test

import (
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/replay"
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

package ghostty_test

import (
	"bytes"
	"fmt"
	"image/color"
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/render/theme"
	"github.com/mikelorant/shellshot/internal/vt"
	"github.com/mikelorant/shellshot/internal/vt/ghostty"
)

// answering returns a terminal made by NewAnswering and what it has replied.
func answering(t *testing.T, cols, rows int) (vt.Terminal, *bytes.Buffer) {
	t.Helper()
	var got bytes.Buffer
	term, err := ghostty.NewAnswering(func(p []byte) { got.Write(p) })(cols, rows)
	if err != nil {
		t.Fatalf("NewAnswering: %v", err)
	}
	t.Cleanup(func() { term.Close() })
	return term, &got
}

func oscColor(code int, c [3]uint8, term string, index ...string) string {
	return fmt.Sprintf("\x1b]%d;"+strings.Join(index, "")+"rgb:%02x%02x/%02x%02x/%02x%02x%s", code, c[0], c[0], c[1], c[1], c[2], c[2], term)
}

func TestAnsweringReplies(t *testing.T) {
	th := theme.Default()
	rgb := func(c color.RGBA) [3]uint8 { return [3]uint8{c.R, c.G, c.B} }
	fg, bg, cur, red := rgb(th.Foreground), rgb(th.Background), rgb(th.Cursor), rgb(th.Palette[1])

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"cursor position", "abc\x1b[6n", "\x1b[1;4R"},
		{"cursor position after move", "\x1b[3;10H\x1b[6n", "\x1b[3;10R"},
		{"status report", "\x1b[5n", "\x1b[0n"},
		{"foreground ST", "\x1b]10;?\x1b\\", oscColor(10, fg, "\x1b\\")},
		{"foreground BEL", "\x1b]10;?\a", oscColor(10, fg, "\a")},
		{"background ST", "\x1b]11;?\x1b\\", oscColor(11, bg, "\x1b\\")},
		{"background BEL", "\x1b]11;?\a", oscColor(11, bg, "\a")},
		{"cursor colour", "\x1b]12;?\x1b\\", oscColor(12, cur, "\x1b\\")},
		{"palette", "\x1b]4;1;?\x1b\\", oscColor(4, red, "\x1b\\", "1;")},
		{"device attributes 1", "\x1b[c", "\x1b[?62;22c"},
		{"device attributes 2", "\x1b[>c", "\x1b[>1;0;0c"},
		{"xtversion", "\x1b[>0q", "\x1bP>|libghostty\x1b\\"},
		{"colour scheme", "\x1b[?996n", "\x1b[?997;1n"},
		{"text area pixels", "\x1b[14t", "\x1b[4;64;160t"},
		{"cell pixels", "\x1b[16t", "\x1b[6;16;8t"},
		{"text area cells", "\x1b[18t", "\x1b[8;4;20t"},
		{"mode report", "\x1b[?2004$p", "\x1b[?2004;2$y"},
		{"kitty keyboard flags", "\x1b[?u", "\x1b[?0u"},
		{"setter is not answered", "\x1b]11;rgb:ff/00/00\x1b\\", ""},
		{"enquiry is not answered", "\x05", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			term, got := answering(t, 20, 4)
			write(t, term, tc.input)
			if got.String() != tc.want {
				t.Errorf("replies = %q, want %q", got.String(), tc.want)
			}
		})
	}
}

func TestAnsweringDoesNotChangeSnapshot(t *testing.T) {
	const input = "\x1b[31mred\x1b[0m \x1b[48;5;21mblue\x1b[0m plain\r\n\x1b[6n"
	plain := newTerminal(t, 20, 4)
	write(t, plain, input)
	term, _ := answering(t, 20, 4)
	write(t, term, input)
	if got, want := snap(t, term), snap(t, plain); !got.Equal(want) {
		t.Errorf("snapshot with replies = %+v, want %+v", got, want)
	}
}

func TestPlainTerminalDoesNotAnswer(t *testing.T) {
	// ghostty.New has no reply sink: queries must be ignored without error.
	term := newTerminal(t, 20, 4)
	write(t, term, "\x1b[6n\x1b]11;?\x1b\\\x1b[c")
}

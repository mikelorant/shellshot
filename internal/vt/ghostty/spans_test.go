package ghostty_test

import (
	"reflect"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

func pal(i uint8) snapshot.Color { return snapshot.Color{Kind: snapshot.ColorPalette, Index: i} }

func rgb(r, g, b uint8) snapshot.Color {
	return snapshot.Color{Kind: snapshot.ColorRGB, R: r, G: g, B: b}
}

// wantSpans checks line y's spans and that the whole snapshot is valid.
func wantSpans(t *testing.T, s snapshot.Snapshot, y int, want ...snapshot.Span) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v (lines %q, spans %+v)", err, s.Lines, s.Spans)
	}
	var got []snapshot.Span
	if y < len(s.Spans) {
		got = s.Spans[y]
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("line %d (%q) spans = %+v, want %+v", y, s.Lines[y], got, want)
	}
}

func TestSpansPaletteForeground(t *testing.T) {
	term := newTerminal(t, 20, 2)
	write(t, term, "\x1b[31mred\x1b[0m plain")

	s := snap(t, term)
	wantLines(t, s, 2, "red plain")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 3, Style: snapshot.Style{Fg: pal(1)}})
	wantSpans(t, s, 1)
}

func TestSpansBoldUnderline(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab\x1b[1;4mcd\x1b[0mef")

	wantSpans(t, snap(t, term), 0,
		snapshot.Span{Start: 2, End: 4, Style: snapshot.Style{Attrs: snapshot.Bold | snapshot.Underline}})
}

func TestSpansAllAttributes(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[1;2;3;5;7;8;9mx\x1b[0m")

	want := snapshot.Bold | snapshot.Faint | snapshot.Italic | snapshot.Blink |
		snapshot.Inverse | snapshot.Invisible | snapshot.Strike
	wantSpans(t, snap(t, term), 0, snapshot.Span{Start: 0, End: 1, Style: snapshot.Style{Attrs: want}})
}

func TestSpansEveryUnderlineKindIsUnderline(t *testing.T) {
	for _, sgr := range []string{"4", "21", "4:1", "4:2", "4:3", "4:4", "4:5"} {
		term := newTerminal(t, 10, 1)
		write(t, term, "\x1b["+sgr+"mx")
		wantSpans(t, snap(t, term), 0,
			snapshot.Span{Start: 0, End: 1, Style: snapshot.Style{Attrs: snapshot.Underline}})
	}
}

func TestSpans256ColourForeground(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[38;5;208mhi\x1b[0m")

	wantSpans(t, snap(t, term), 0, snapshot.Span{Start: 0, End: 2, Style: snapshot.Style{Fg: pal(208)}})
}

func TestSpansBrightAndBackgroundPalette(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[91;44mhi\x1b[0m")

	wantSpans(t, snap(t, term), 0, snapshot.Span{Start: 0, End: 2, Style: snapshot.Style{Fg: pal(9), Bg: pal(4)}})
}

func TestSpansTruecolourForegroundAndBackground(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[38;2;10;20;30;48;2;1;2;3mhi\x1b[0m")

	wantSpans(t, snap(t, term), 0, snapshot.Span{
		Start: 0, End: 2,
		Style: snapshot.Style{Fg: rgb(10, 20, 30), Bg: rgb(1, 2, 3)},
	})
}

func TestSpansInverse(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "a\x1b[7mb\x1b[27mc")

	wantSpans(t, snap(t, term), 0, snapshot.Span{Start: 1, End: 2, Style: snapshot.Style{Attrs: snapshot.Inverse}})
}

func TestSpansWideCharacters(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[31m世界\x1b[0m!")

	s := snap(t, term)
	wantLines(t, s, 1, "世界!")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 2, Style: snapshot.Style{Fg: pal(1)}})
}

func TestSpansAfterWideCharacterStayAligned(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "世a\x1b[32mb\x1b[0m界")

	s := snap(t, term)
	wantLines(t, s, 1, "世ab界")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 3, Style: snapshot.Style{Fg: pal(2)}})
}

func TestSpansCombiningCharacterCountsItsRunes(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "e\u0301\x1b[32mx\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "e\u0301x")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 3, Style: snapshot.Style{Fg: pal(2)}})
}

func TestSpansAfterLeadingBlanks(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "   \x1b[32mx\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "   x")
	wantSpans(t, s, 0, snapshot.Span{Start: 3, End: 4, Style: snapshot.Style{Fg: pal(2)}})
}

func TestSpansBackgroundOnErasedBlanksInsideLine(t *testing.T) {
	term := newTerminal(t, 20, 1)
	// Erase two cells with a blue background, skip them, then print plain text.
	write(t, term, "ab\x1b[44m\x1b[2X\x1b[2C\x1b[0mz")

	s := snap(t, term)
	wantLines(t, s, 1, "ab  z")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 4, Style: snapshot.Style{Bg: pal(4)}})
}

func TestSpansStyleChangesMidLine(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[31mab\x1b[32mcd\x1b[1;32mef\x1b[0mgh")

	green := snapshot.Style{Fg: pal(2)}
	wantSpans(t, snap(t, term), 0,
		snapshot.Span{Start: 0, End: 2, Style: snapshot.Style{Fg: pal(1)}},
		snapshot.Span{Start: 2, End: 4, Style: green},
		snapshot.Span{Start: 4, End: 6, Style: snapshot.Style{Fg: pal(2), Attrs: snapshot.Bold}},
	)
}

func TestSpansAdjacentEqualStylesMerge(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[31ma\x1b[31mb\x1b[0m\x1b[31mc\x1b[0m")

	wantSpans(t, snap(t, term), 0, snapshot.Span{Start: 0, End: 3, Style: snapshot.Style{Fg: pal(1)}})
}

func TestSpansStyleDoesNotLeakAcrossLines(t *testing.T) {
	term := newTerminal(t, 20, 3)
	write(t, term, "\x1b[31mone\x1b[0m\r\ntwo\r\n\x1b[34mthree\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 3, "one", "two", "three")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 3, Style: snapshot.Style{Fg: pal(1)}})
	wantSpans(t, s, 1)
	wantSpans(t, s, 2, snapshot.Span{Start: 0, End: 5, Style: snapshot.Style{Fg: pal(4)}})
}

func TestSpansStyleContinuesAcrossLinesUntilReset(t *testing.T) {
	term := newTerminal(t, 20, 2)
	write(t, term, "\x1b[31mone\r\ntwo")

	s := snap(t, term)
	red := snapshot.Style{Fg: pal(1)}
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 3, Style: red})
	wantSpans(t, s, 1, snapshot.Span{Start: 0, End: 3, Style: red})
}

func TestSpansClippedToTrimmedLine(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[31mab   \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "ab")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 2, Style: snapshot.Style{Fg: pal(1)}})
}

func TestSpansNilWithoutStyles(t *testing.T) {
	term := newTerminal(t, 20, 3)
	write(t, term, "plain\r\ntext")

	s := snap(t, term)
	if s.Spans != nil {
		t.Errorf("Spans = %+v, want nil", s.Spans)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestSpansSurviveResizeAndRewrite(t *testing.T) {
	term := newTerminal(t, 20, 2)
	write(t, term, "\x1b[31mred\x1b[0m")
	wantSpans(t, snap(t, term), 0, snapshot.Span{Start: 0, End: 3, Style: snapshot.Style{Fg: pal(1)}})

	write(t, term, "\x1b[2J\x1b[Hok")
	s := snap(t, term)
	wantLines(t, s, 2, "ok")
	wantSpans(t, s, 0)
	if s.Spans != nil {
		t.Errorf("Spans = %+v, want nil after clear", s.Spans)
	}
}

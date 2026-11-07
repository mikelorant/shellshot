package ghostty_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

// wantFullRow checks that line y is exactly cols cells wide (every rune one
// cell) and that Columns agrees with the terminal's columns.
func wantFullRow(t *testing.T, s snapshot.Snapshot, y, cols int) {
	t.Helper()
	columns := s.Columns(y)
	if len(columns) != cols {
		t.Fatalf("line %d has %d runes, want %d", y, len(columns), cols)
	}
	for i, c := range columns {
		if c != i {
			t.Fatalf("line %d Columns[%d] = %d, want %d", y, i, c, i)
		}
	}
}

func blueBg() snapshot.Style { return snapshot.Style{Bg: pal(4)} }

func TestStyledBlanksAtLineEndAreKept(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab\x1b[44m   \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "ab   ")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 5, Style: blueBg()})
}

func TestStyledBlanksOnAnOtherwiseEmptyLineAreKept(t *testing.T) {
	term := newTerminal(t, 20, 2)
	write(t, term, "\x1b[44m   \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 2, "   ")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 3, Style: blueBg()})
	wantSpans(t, s, 1)
}

func TestStyledBlanksKeepOnlyUpToTheLastVisibleOne(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab\x1b[44m  \x1b[0m   ")

	s := snap(t, term)
	wantLines(t, s, 1, "ab  ")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 4, Style: blueBg()})
}

func TestStyledBlanksBeforeClearedCells(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab\x1b[44m  \x1b[0m\x1b[K")

	s := snap(t, term)
	wantLines(t, s, 1, "ab  ")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 4, Style: blueBg()})
}

func TestEraseToEndOfLineWithBackgroundFillsTheRow(t *testing.T) {
	term := newTerminal(t, 80, 2)
	write(t, term, "\x1b[44m\x1b[K\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 2, strings.Repeat(" ", 80))
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 80, Style: blueBg()})
	wantSpans(t, s, 1)
	wantFullRow(t, s, 0, 80)
}

func TestStatusBarWithTextAndEraseToEndOfLine(t *testing.T) {
	term := newTerminal(t, 80, 1)
	write(t, term, "\x1b[44;37m status bar \x1b[K\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, " status bar "+strings.Repeat(" ", 68))
	// The erased cells keep only the background colour, not the foreground.
	wantSpans(t, s, 0,
		snapshot.Span{Start: 0, End: 12, Style: snapshot.Style{Fg: pal(7), Bg: pal(4)}},
		snapshot.Span{Start: 12, End: 80, Style: blueBg()})
	wantFullRow(t, s, 0, 80)
}

func TestEraseWithBackgroundCellsAtLineEnd(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab\x1b[44m\x1b[3X\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "ab   ")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 5, Style: blueBg()})
}

func TestTruecolourBackgroundBlanksAtLineEnd(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[48;2;1;2;3m  \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "  ")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 2, Style: snapshot.Style{Bg: rgb(1, 2, 3)}})
}

func TestReverseVideoBarOfPrintedSpacesFillsTheRow(t *testing.T) {
	term := newTerminal(t, 80, 1)
	write(t, term, "\x1b[7m bar"+strings.Repeat(" ", 76)+"\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, " bar"+strings.Repeat(" ", 76))
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 80, Style: snapshot.Style{Attrs: snapshot.Inverse}})
	wantFullRow(t, s, 0, 80)
}

// Erasing clears cells to the current background colour only: the inverse
// attribute is not stored in an erased cell, so no terminal draws a bar there.
func TestEraseToEndOfLineDoesNotCarryInverse(t *testing.T) {
	term := newTerminal(t, 80, 1)
	write(t, term, "\x1b[7m bar\x1b[K\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, " bar")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 4, Style: snapshot.Style{Attrs: snapshot.Inverse}})
}

func TestInvertedBlanksAtLineEndAreKept(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "a\x1b[7m  \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "a  ")
	wantSpans(t, s, 0, snapshot.Span{Start: 1, End: 3, Style: snapshot.Style{Attrs: snapshot.Inverse}})
}

func TestFullScreenBackgroundFillsEveryRow(t *testing.T) {
	term := newTerminal(t, 80, 5)
	write(t, term, "\x1b[48;5;196m\x1b[2J\x1b[H")

	s := snap(t, term)
	full := strings.Repeat(" ", 80)
	wantLines(t, s, 5, full, full, full, full, full)
	for y := 0; y < 5; y++ {
		wantSpans(t, s, y, snapshot.Span{Start: 0, End: 80, Style: snapshot.Style{Bg: pal(196)}})
		wantFullRow(t, s, y, 80)
	}
}

func TestBackgroundSetBeforeScrollingFillsNewRows(t *testing.T) {
	term := newTerminal(t, 10, 3)
	write(t, term, "a\r\nb\r\nc\x1b[44m\r\n\r\n")

	s := snap(t, term)
	wantLines(t, s, 3, "c", strings.Repeat(" ", 10), strings.Repeat(" ", 10))
	wantSpans(t, s, 0)
	wantSpans(t, s, 1, snapshot.Span{Start: 0, End: 10, Style: blueBg()})
	wantSpans(t, s, 2, snapshot.Span{Start: 0, End: 10, Style: blueBg()})
}

func TestUnstyledTrailingSpacesAreStillTrimmed(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab    \x1b[0m  ")

	s := snap(t, term)
	wantLines(t, s, 1, "ab")
	wantSpans(t, s, 0)
	if s.Spans != nil {
		t.Errorf("Spans = %+v, want nil", s.Spans)
	}
}

func TestErasingTheBackgroundTrimsTheRowAgain(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab\x1b[44m\x1b[K\x1b[0m")
	write(t, term, "\x1b[2K\rcd")

	s := snap(t, term)
	wantLines(t, s, 1, "cd")
	if s.Spans != nil {
		t.Errorf("Spans = %+v, want nil", s.Spans)
	}
}

func TestTrailingUnderlinedAndStruckBlanksAreKept(t *testing.T) {
	term := newTerminal(t, 20, 3)
	write(t, term, "a\x1b[4m  \x1b[0m\r\nb\x1b[9m  \x1b[0m\r\nc\x1b[4;9m  \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 3, "a  ", "b  ", "c  ")
	wantSpans(t, s, 0, snapshot.Span{Start: 1, End: 3, Style: snapshot.Style{Attrs: snapshot.Underline}})
	wantSpans(t, s, 1, snapshot.Span{Start: 1, End: 3, Style: snapshot.Style{Attrs: snapshot.Strike}})
	wantSpans(t, s, 2, snapshot.Span{Start: 1, End: 3, Style: snapshot.Style{Attrs: snapshot.Underline | snapshot.Strike}})
}

func TestTrailingBlanksThatShowNothingAreTrimmed(t *testing.T) {
	for _, sgr := range []string{"31", "38;5;208", "38;2;1;2;3", "1", "2", "3", "5", "8", "1;3;31"} {
		term := newTerminal(t, 20, 1)
		write(t, term, "ab\x1b["+sgr+"m   \x1b[0m")

		s := snap(t, term)
		wantLines(t, s, 1, "ab")
		wantSpans(t, s, 0)
		if s.Spans != nil {
			t.Errorf("SGR %s: Spans = %+v, want nil", sgr, s.Spans)
		}
	}
}

func TestTrailingForegroundOnlyBlanksAfterStyledBlanksAreTrimmed(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "ab\x1b[44m  \x1b[0;31m   \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "ab  ")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 4, Style: blueBg()})
}

func TestWideCharacterBeforeStyledBlanks(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "a世\x1b[44m  \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "a世  ")
	wantSpans(t, s, 0, snapshot.Span{Start: 2, End: 4, Style: blueBg()})
	wantWidths(t, s, 0, snapshot.Wide{Rune: 1, Cells: 2})
	if got, want := s.Columns(0), []int{0, 1, 3, 4}; !reflect.DeepEqual(got, want) {
		t.Errorf("Columns = %v, want %v", got, want)
	}
}

func TestWideCharacterAtTheEndOfTheRow(t *testing.T) {
	term := newTerminal(t, 10, 2)
	write(t, term, "\x1b[44m12345678世\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 2, "12345678世")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 9, Style: blueBg()})
	wantWidths(t, s, 0, snapshot.Wide{Rune: 8, Cells: 2})
	if cols := s.Columns(0); cols[len(cols)-1] != 8 {
		t.Errorf("last rune starts at column %d, want 8", cols[len(cols)-1])
	}
}

func TestWideCharacterWrapLeavesStyledRowEndUnchanged(t *testing.T) {
	term := newTerminal(t, 10, 2)
	write(t, term, "\x1b[44m123456789世\x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 2, "123456789", "世")
	wantSpans(t, s, 0, snapshot.Span{Start: 0, End: 9, Style: blueBg()})
	wantSpans(t, s, 1, snapshot.Span{Start: 0, End: 1, Style: blueBg()})
}

func TestMixedLineOfTextAndStyledBlanks(t *testing.T) {
	term := newTerminal(t, 30, 1)
	write(t, term, "text\x1b[44m  \x1b[0m plain\x1b[41m  \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "text   plain  ")
	wantSpans(t, s, 0,
		snapshot.Span{Start: 4, End: 6, Style: blueBg()},
		snapshot.Span{Start: 12, End: 14, Style: snapshot.Style{Bg: pal(1)}})
}

func TestSixteenColourSwatchRow(t *testing.T) {
	term := newTerminal(t, 80, 1)
	var b strings.Builder
	for i := 0; i < 16; i++ {
		b.WriteString("\x1b[48;5;" + strconv.Itoa(i) + "m  ")
	}
	b.WriteString("\x1b[0m")
	write(t, term, b.String())

	s := snap(t, term)
	wantLines(t, s, 1, strings.Repeat(" ", 32))
	if got := len(s.Spans[0]); got != 16 {
		t.Fatalf("got %d spans, want 16: %+v", got, s.Spans[0])
	}
	for i, sp := range s.Spans[0] {
		want := snapshot.Span{Start: 2 * i, End: 2*i + 2, Style: snapshot.Style{Bg: pal(uint8(i))}}
		if sp != want {
			t.Errorf("span %d = %+v, want %+v", i, sp, want)
		}
	}
}

func TestEqualNoticesStyledTrailingBlanks(t *testing.T) {
	plain := newTerminal(t, 20, 1)
	write(t, plain, "ab")
	blue := newTerminal(t, 20, 1)
	write(t, blue, "ab\x1b[44m  ")
	red := newTerminal(t, 20, 1)
	write(t, red, "ab\x1b[41m  ")

	a, b, c := snap(t, plain), snap(t, blue), snap(t, red)
	if a.Equal(b) || b.Equal(a) {
		t.Error("snapshots with and without styled trailing blanks are Equal")
	}
	if b.Equal(c) {
		t.Error("snapshots with different trailing background colours are Equal")
	}
	if !b.Equal(snap(t, blue)) {
		t.Error("identical snapshots are not Equal")
	}
}

func TestSnapshotsWithStyledBlanksValidate(t *testing.T) {
	for _, input := range []string{
		"\x1b[44m\x1b[2J",
		"\x1b[7m\x1b[K",
		"世\x1b[44m世\x1b[K",
		"e\u0301\x1b[44m  ",
		"\x1b[4m   ",
		"\x1b[48;2;1;2;3m\x1b[2J\x1b[H世",
	} {
		term := newTerminal(t, 12, 3)
		write(t, term, input)
		if err := snap(t, term).Validate(); err != nil {
			t.Errorf("%q: Validate: %v", input, err)
		}
	}
}

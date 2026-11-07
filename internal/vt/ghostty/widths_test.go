package ghostty_test

import (
	"reflect"
	"testing"
	"unicode/utf8"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// wantWidths checks line y's width entries and that the whole snapshot is
// valid.
func wantWidths(t *testing.T, s snapshot.Snapshot, y int, want ...snapshot.Wide) {
	t.Helper()
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v (lines %q, widths %+v)", err, s.Lines, s.Widths)
	}
	var got []snapshot.Wide
	if y < len(s.Widths) {
		got = s.Widths[y]
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("line %d (%q) widths = %+v, want %+v", y, s.Lines[y], got, want)
	}
}

// wantCursorAtLineEnd checks that the cell columns the snapshot computes for
// line y end exactly where the terminal's own cursor stopped after the line
// was written. The line must not have wrapped.
func wantCursorAtLineEnd(t *testing.T, s snapshot.Snapshot, y int) {
	t.Helper()
	cols := s.Columns(y)
	last := utf8.RuneCountInString(s.Lines[y]) - 1
	end := cols[last] + s.CellWidth(y, last)
	if s.Cursor.Y != y || s.Cursor.X != end {
		t.Fatalf("line %d (%q): columns %v end at cell %d, terminal cursor at (%d,%d)",
			y, s.Lines[y], cols, end, s.Cursor.X, s.Cursor.Y)
	}
}

func TestWidthsNilForASCII(t *testing.T) {
	term := newTerminal(t, 20, 3)
	write(t, term, "plain\r\ntext \x1b[31mred\x1b[0m")

	s := snap(t, term)
	if s.Widths != nil {
		t.Errorf("Widths = %+v, want nil", s.Widths)
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestWidthsNilForNarrowNonASCII(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "café ñ ü")

	s := snap(t, term)
	wantLines(t, s, 1, "café ñ ü")
	if s.Widths != nil {
		t.Errorf("Widths = %+v, want nil", s.Widths)
	}
}

func TestWidthsWideCharacters(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "世界ab")

	s := snap(t, term)
	wantLines(t, s, 1, "世界ab")
	wantWidths(t, s, 0, snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 2})
	if got, want := s.Columns(0), []int{0, 2, 4, 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("Columns = %v, want %v", got, want)
	}
	wantCursorAtLineEnd(t, s, 0)
}

func TestWidthsWideCharacterBetweenNarrowOnes(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "a世b")

	s := snap(t, term)
	wantWidths(t, s, 0, snapshot.Wide{Rune: 1, Cells: 2})
	wantCursorAtLineEnd(t, s, 0)
}

func TestWidthsCombiningMark(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "e\u0301x")

	s := snap(t, term)
	wantLines(t, s, 1, "e\u0301x")
	wantWidths(t, s, 0, snapshot.Wide{Rune: 1, Cells: 0})
	if got, want := s.Columns(0), []int{0, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("Columns = %v, want %v", got, want)
	}
	wantCursorAtLineEnd(t, s, 0)
}

func TestWidthsCombiningMarkOnWideCharacter(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "世\u0301x")

	s := snap(t, term)
	wantWidths(t, s, 0, snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 0})
	wantCursorAtLineEnd(t, s, 0)
}

// With grapheme clustering (mode 2027) switched off by the program, a
// variation selector after a narrow base stays one cell wide.
func TestWidthsVariationSelectorWithoutGraphemeClustering(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[?2027l\u2764\ufe0fx")

	s := snap(t, term)
	wantLines(t, s, 1, "\u2764\ufe0fx")
	wantWidths(t, s, 0, snapshot.Wide{Rune: 1, Cells: 0})
	wantCursorAtLineEnd(t, s, 0)
}

// Grapheme clustering is on by default: VS16 turns the base into a wide emoji.
func TestWidthsVariationSelectorWithGraphemeClustering(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\u2764\ufe0fx")

	s := snap(t, term)
	wantWidths(t, s, 0, snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 0})
	wantCursorAtLineEnd(t, s, 0)
}

// With the mode off libghostty joins a ZWJ to the cell of the emoji before
// it, so each wide emoji cell holds its emoji and the joiner after it.
func TestWidthsZeroWidthJoinerSequenceWithoutGraphemeClustering(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[?2027l\U0001F468\u200d\U0001F469\u200d\U0001F467x")

	s := snap(t, term)
	wantLines(t, s, 1, "\U0001F468\u200d\U0001F469\u200d\U0001F467x")
	wantWidths(t, s, 0,
		snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 0},
		snapshot.Wide{Rune: 2, Cells: 2}, snapshot.Wide{Rune: 3, Cells: 0},
		snapshot.Wide{Rune: 4, Cells: 2})
	wantCursorAtLineEnd(t, s, 0)
}

// Without grapheme clustering the skin tone modifier is an emoji of its own,
// two cells wide.
func TestWidthsSkinToneModifierWithoutGraphemeClustering(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[?2027l\U0001F44D\U0001F3FDx")

	s := snap(t, term)
	wantLines(t, s, 1, "\U0001F44D\U0001F3FDx")
	wantWidths(t, s, 0, snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 2})
	wantCursorAtLineEnd(t, s, 0)
}

func TestWidthsSkinToneModifierWithGraphemeClustering(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\U0001F44D\U0001F3FDx")

	s := snap(t, term)
	wantWidths(t, s, 0, snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 0})
	wantCursorAtLineEnd(t, s, 0)
}

// Emoji sequences are single cells by default (mode 2027 is on): the base
// takes 2 cells and every follower 0, so a family, a flag and a keycap each
// span exactly two columns.
func TestWidthsEmojiSequencesAreOneCluster(t *testing.T) {
	const family = "\U0001F468\u200d\U0001F469\u200d\U0001F467"
	const flag = "\U0001F1E6\U0001F1FA"
	const keycap = "1\ufe0f\u20e3"
	term := newTerminal(t, 20, 1)
	write(t, term, family+flag+keycap+"x")

	s := snap(t, term)
	wantLines(t, s, 1, family+flag+keycap+"x")
	wantWidths(t, s, 0,
		snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 0}, snapshot.Wide{Rune: 2, Cells: 0},
		snapshot.Wide{Rune: 3, Cells: 0}, snapshot.Wide{Rune: 4, Cells: 0},
		snapshot.Wide{Rune: 5, Cells: 2}, snapshot.Wide{Rune: 6, Cells: 0},
		snapshot.Wide{Rune: 7, Cells: 2}, snapshot.Wide{Rune: 8, Cells: 0}, snapshot.Wide{Rune: 9, Cells: 0})
	if got, want := s.Columns(0)[10], 6; got != want {
		t.Errorf("x starts at column %d, want %d", got, want)
	}
	wantCursorAtLineEnd(t, s, 0)
}

// A full reset (RIS) keeps grapheme clustering on: it is the mode's default.
func TestWidthsGraphemeClusteringSurvivesReset(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[?2027l\x1bc\U0001F44D\U0001F3FDx")

	s := snap(t, term)
	wantWidths(t, s, 0, snapshot.Wide{Rune: 0, Cells: 2}, snapshot.Wide{Rune: 1, Cells: 0})
}

func TestWidthsWideCharacterWrapsAtRightEdge(t *testing.T) {
	term := newTerminal(t, 5, 3)
	// 世 needs two cells but only one is left, so it moves to the next row and
	// the leftover cell becomes a spacer that adds no rune.
	write(t, term, "abcd世e")

	s := snap(t, term)
	wantLines(t, s, 3, "abcd", "世e")
	wantWidths(t, s, 0)
	wantWidths(t, s, 1, snapshot.Wide{Rune: 0, Cells: 2})
	if len(s.Widths) != s.Rows {
		t.Errorf("len(Widths) = %d, want %d", len(s.Widths), s.Rows)
	}
	wantCursorAtLineEnd(t, s, 1)
}

func TestWidthsWideCharacterFillingLastCells(t *testing.T) {
	term := newTerminal(t, 5, 2)
	write(t, term, "abc世")

	s := snap(t, term)
	wantLines(t, s, 2, "abc世")
	wantWidths(t, s, 0, snapshot.Wide{Rune: 3, Cells: 2})
	if got, want := s.Columns(0), []int{0, 1, 2, 3}; !reflect.DeepEqual(got, want) {
		t.Errorf("Columns = %v, want %v", got, want)
	}
}

func TestWidthsOnlyOnLinesThatNeedThem(t *testing.T) {
	term := newTerminal(t, 20, 3)
	write(t, term, "one\r\n世two\r\nthree")

	s := snap(t, term)
	wantLines(t, s, 3, "one", "世two", "three")
	wantWidths(t, s, 0)
	wantWidths(t, s, 1, snapshot.Wide{Rune: 0, Cells: 2})
	wantWidths(t, s, 2)
}

func TestWidthsAlignWithSpansOnStyledWideCharacters(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "\x1b[31m世\x1b[0mab\x1b[32me\u0301\x1b[0m界!")

	s := snap(t, term)
	wantLines(t, s, 1, "世abe\u0301界!")
	wantSpans(t, s, 0,
		snapshot.Span{Start: 0, End: 1, Style: snapshot.Style{Fg: pal(1)}},
		snapshot.Span{Start: 3, End: 5, Style: snapshot.Style{Fg: pal(2)}})
	wantWidths(t, s, 0,
		snapshot.Wide{Rune: 0, Cells: 2},
		snapshot.Wide{Rune: 4, Cells: 0},
		snapshot.Wide{Rune: 5, Cells: 2})
	if got, want := s.Columns(0), []int{0, 2, 3, 4, 5, 5, 7}; !reflect.DeepEqual(got, want) {
		t.Errorf("Columns = %v, want %v", got, want)
	}
	wantCursorAtLineEnd(t, s, 0)
}

func TestWidthsNotReportedForTrailingBlanks(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "世\x1b[41m   \x1b[0m")

	s := snap(t, term)
	wantLines(t, s, 1, "世")
	wantWidths(t, s, 0, snapshot.Wide{Rune: 0, Cells: 2})
}

func TestWidthsClearedWhenWideTextIsOverwritten(t *testing.T) {
	term := newTerminal(t, 20, 1)
	write(t, term, "世界")
	if s := snap(t, term); s.Widths == nil {
		t.Fatal("Widths = nil, want entries for wide text")
	}

	write(t, term, "\x1b[2J\x1b[Hok")
	s := snap(t, term)
	wantLines(t, s, 1, "ok")
	if s.Widths != nil {
		t.Errorf("Widths = %+v, want nil after clear", s.Widths)
	}
}

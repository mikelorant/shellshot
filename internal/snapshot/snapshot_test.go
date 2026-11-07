package snapshot

import (
	"slices"
	"testing"
)

var red = Style{Fg: pal(1)}
var bold = Style{Attrs: Bold}

func TestSnapshotEqual(t *testing.T) {
	base := func() Snapshot {
		return Snapshot{Cols: 10, Rows: 2, Lines: []string{"ab", "cd"}, Title: "t"}
	}
	styled := func(sp ...Span) Snapshot {
		s := base()
		s.Spans = [][]Span{sp, nil}
		return s
	}
	tests := []struct {
		name string
		a, b Snapshot
		want bool
	}{
		{"identical", base(), base(), true},
		{"cols", base(), func() Snapshot { s := base(); s.Cols = 11; return s }(), false},
		{"line", base(), func() Snapshot { s := base(); s.Lines = []string{"ab", "xx"}; return s }(), false},
		{"title", base(), func() Snapshot { s := base(); s.Title = "u"; return s }(), false},
		{"cursor", base(), func() Snapshot { s := base(); s.Cursor.X = 1; return s }(), false},
		{"nil vs all-empty spans", base(), func() Snapshot { s := base(); s.Spans = [][]Span{nil, {}}; return s }(), true},
		{"nil vs zero-length spans", base(), func() Snapshot { s := base(); s.Spans = [][]Span{}; return s }(), true},
		{"nil vs styled", base(), styled(Span{0, 1, red}), false},
		{"same spans", styled(Span{0, 1, red}), styled(Span{0, 1, red}), true},
		{"different style", styled(Span{0, 1, red}), styled(Span{0, 1, bold}), false},
		{"different range", styled(Span{0, 1, red}), styled(Span{0, 2, red}), false},
		{"nil vs all-empty widths", base(), func() Snapshot { s := base(); s.Widths = [][]Wide{nil, {}}; return s }(), true},
		{"nil vs wide", base(), func() Snapshot { s := base(); s.Widths = [][]Wide{{{0, 2}}, nil}; return s }(), false},
		{"same widths", func() Snapshot { s := base(); s.Widths = [][]Wide{{{0, 2}}, nil}; return s }(), func() Snapshot { s := base(); s.Widths = [][]Wide{{{0, 2}}, nil}; return s }(), true},
		{"different cells", func() Snapshot { s := base(); s.Widths = [][]Wide{{{0, 2}}, nil}; return s }(), func() Snapshot { s := base(); s.Widths = [][]Wide{{{0, 0}}, nil}; return s }(), false},
		{"different rune", func() Snapshot { s := base(); s.Widths = [][]Wide{{{0, 2}}, nil}; return s }(), func() Snapshot { s := base(); s.Widths = [][]Wide{{{1, 2}}, nil}; return s }(), false},
		{"different count", styled(Span{0, 1, red}), styled(Span{0, 1, red}, Span{1, 2, bold}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.a.Equal(tt.b); got != tt.want {
				t.Errorf("a.Equal(b) = %v, want %v", got, tt.want)
			}
			if got := tt.b.Equal(tt.a); got != tt.want {
				t.Errorf("b.Equal(a) = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSnapshotValidate(t *testing.T) {
	mk := func(lines []string, spans [][]Span) Snapshot {
		return Snapshot{Cols: 10, Rows: 2, Lines: lines, Spans: spans}
	}
	two := []string{"héllo", "x"}
	tests := []struct {
		name    string
		s       Snapshot
		wantErr bool
	}{
		{"no spans", mk(two, nil), false},
		{"empty spans slice", mk(two, [][]Span{}), false},
		{"all-empty spans", mk(two, [][]Span{nil, nil}), false},
		{"valid spans", mk(two, [][]Span{{{0, 2, red}, {2, 5, bold}}, {{0, 1, red}}}), false},
		{"rune not byte index", mk(two, [][]Span{{{0, 5, red}}, nil}), false},
		{"too few lines", mk([]string{"a"}, nil), true},
		{"too many lines", mk([]string{"a", "b", "c"}, nil), true},
		{"span count mismatch", mk(two, [][]Span{nil}), true},
		{"end past runes", mk(two, [][]Span{{{0, 6, red}}, nil}), true},
		{"negative start", mk(two, [][]Span{{{-1, 2, red}}, nil}), true},
		{"start equals end", mk(two, [][]Span{{{2, 2, red}}, nil}), true},
		{"start after end", mk(two, [][]Span{{{3, 2, red}}, nil}), true},
		{"unsorted", mk(two, [][]Span{{{3, 4, red}, {0, 1, bold}}, nil}), true},
		{"overlap", mk(two, [][]Span{{{0, 3, red}, {2, 4, bold}}, nil}), true},
		{"second line out of range", mk(two, [][]Span{nil, {{0, 2, red}}}), true},
		{"zero style", mk(two, [][]Span{{{0, 1, Style{}}}, nil}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSnapshotValidateWidths(t *testing.T) {
	mk := func(widths [][]Wide) Snapshot {
		return Snapshot{Cols: 10, Rows: 2, Lines: []string{"世界ab", "e\u0301x"}, Widths: widths}
	}
	tests := []struct {
		name    string
		s       Snapshot
		wantErr bool
	}{
		{"no widths", mk(nil), false},
		{"empty widths slice", mk([][]Wide{}), false},
		{"all-empty widths", mk([][]Wide{nil, nil}), false},
		{"valid wide", mk([][]Wide{{{0, 2}, {1, 2}}, nil}), false},
		{"valid zero-width", mk([][]Wide{nil, {{1, 0}}}), false},
		{"zero-width at rune 0", mk([][]Wide{nil, {{0, 0}}}), false},
		{"count mismatch", mk([][]Wide{nil}), true},
		{"too many lines", mk([][]Wide{nil, nil, nil}), true},
		{"rune at line length", mk([][]Wide{{{4, 2}}, nil}), true},
		{"last rune", mk([][]Wide{{{3, 2}}, nil}), false},
		{"negative rune", mk([][]Wide{{{-1, 2}}, nil}), true},
		{"unsorted", mk([][]Wide{{{1, 2}, {0, 2}}, nil}), true},
		{"duplicate", mk([][]Wide{{{1, 2}, {1, 2}}, nil}), true},
		{"one cell", mk([][]Wide{{{0, 1}}, nil}), true},
		{"three cells", mk([][]Wide{{{0, 3}}, nil}), true},
		{"second line out of range", mk([][]Wide{nil, {{3, 2}}}), true},
		{"rune not byte index", mk([][]Wide{{{3, 2}}, nil}), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.s.Validate(); (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestSnapshotColumns(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		widths [][]Wide
		row    int
		want   []int
		widthN []int // expected CellWidth per rune of the row
	}{
		{"ascii no widths", []string{"abcd"}, nil, 0, []int{0, 1, 2, 3}, []int{1, 1, 1, 1}},
		{"empty line", []string{""}, nil, 0, []int{}, nil},
		{"cjk", []string{"世界ab"}, [][]Wide{{{0, 2}, {1, 2}}}, 0, []int{0, 2, 4, 5}, []int{2, 2, 1, 1}},
		{"combining mark", []string{"e\u0301x"}, [][]Wide{{{1, 0}}}, 0, []int{0, 1, 1}, []int{1, 0, 1}},
		{"zero-width first rune", []string{"\u0301x"}, [][]Wide{{{0, 0}}}, 0, []int{0, 0}, []int{0, 1}},
		{"mixed", []string{"a世e\u0301b"}, [][]Wide{{{1, 2}, {3, 0}}}, 0, []int{0, 1, 3, 4, 4}, []int{1, 2, 1, 0, 1}},
		{"second row uses its own widths", []string{"世", "ab"}, [][]Wide{{{0, 2}}, nil}, 1, []int{0, 1}, []int{1, 1}},
		{"second row wide", []string{"ab", "世a"}, [][]Wide{nil, {{0, 2}}}, 1, []int{0, 2}, []int{2, 1}},
		{"short widths slice", []string{"ab", "cd"}, [][]Wide{}, 1, []int{0, 1}, []int{1, 1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := Snapshot{Cols: 10, Rows: len(tt.lines), Lines: tt.lines, Widths: tt.widths}
			if err := s.Validate(); err != nil {
				t.Fatalf("Validate() = %v", err)
			}
			got := s.Columns(tt.row)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Columns(%d) = %v, want %v", tt.row, got, tt.want)
			}
			for i, w := range tt.widthN {
				if got := s.CellWidth(tt.row, i); got != w {
					t.Errorf("CellWidth(%d, %d) = %d, want %d", tt.row, i, got, w)
				}
			}
		})
	}
}

func TestSnapshotColumnsOutOfRange(t *testing.T) {
	s := Snapshot{Cols: 10, Rows: 1, Lines: []string{"ab"}}
	for _, row := range []int{-1, 1} {
		if got := s.Columns(row); got != nil {
			t.Errorf("Columns(%d) = %v, want nil", row, got)
		}
	}
	if got := s.CellWidth(5, 5); got != 1 {
		t.Errorf("CellWidth(5, 5) = %d, want 1", got)
	}
}

func TestSnapshotTextTrimsStyledTrailingSpaces(t *testing.T) {
	s := Snapshot{
		Rows:  3,
		Lines: []string{"ab   ", "", "c d"},
		Spans: [][]Span{{{Start: 2, End: 5, Style: Style{Bg: Color{Kind: ColorPalette, Index: 4}}}}, nil, nil},
	}
	if got, want := s.Text(), "ab\n\nc d"; got != want {
		t.Errorf("Text = %q, want %q", got, want)
	}
}

func TestSnapshotIsBlank(t *testing.T) {
	styled := [][]Span{{{Start: 0, End: 1, Style: Style{Attrs: Inverse}}}}
	tests := []struct {
		name string
		s    Snapshot
		want bool
	}{
		{"empty lines", Snapshot{Cols: 4, Rows: 2, Lines: []string{"", ""}}, true},
		{"spaces only", Snapshot{Cols: 4, Rows: 1, Lines: []string{"  "}}, true},
		{"cursor and title only", Snapshot{Cols: 4, Rows: 1, Lines: []string{""}, Cursor: Cursor{X: 1, Visible: true}, Title: "t"}, true},
		{"text", Snapshot{Cols: 4, Rows: 2, Lines: []string{"", "$"}}, false},
		{"styled blank cell", Snapshot{Cols: 4, Rows: 1, Lines: []string{" "}, Spans: styled}, false},
		{"empty span lists", Snapshot{Cols: 4, Rows: 1, Lines: []string{""}, Spans: [][]Span{nil}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.s.IsBlank(); got != tt.want {
				t.Errorf("IsBlank() = %v, want %v", got, tt.want)
			}
		})
	}
}

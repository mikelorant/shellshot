package snapshot

import "testing"

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

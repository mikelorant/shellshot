package replay

import (
	"reflect"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

func TestCrop(t *testing.T) {
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	tests := []struct {
		name       string
		snap       snapshot.Snapshot
		cols, rows int
		want       snapshot.Snapshot
	}{
		{
			name: "fits is unchanged",
			snap: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"ab", "cd"}},
			cols: 10, rows: 10,
			want: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"ab", "cd"}},
		},
		{
			name: "zero viewport is unchanged",
			snap: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"ab", "cd"}},
			want: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"ab", "cd"}},
		},
		{
			name: "rows follow the last row with text and drop rows above",
			snap: snapshot.Snapshot{Cols: 4, Rows: 6, Lines: []string{"a", "b", "c", "d", "", ""}},
			rows: 2,
			want: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"c", "d"}},
		},
		{
			name: "rows follow the visible cursor below the text",
			snap: snapshot.Snapshot{Cols: 4, Rows: 6, Lines: []string{"a", "b", "", "", "", ""}, Cursor: snapshot.Cursor{X: 1, Y: 4, Visible: true}},
			rows: 3,
			want: snapshot.Snapshot{Cols: 4, Rows: 3, Lines: []string{"", "", ""}, Cursor: snapshot.Cursor{X: 1, Y: 2, Visible: true}},
		},
		{
			name: "hidden cursor above the window is hidden",
			snap: snapshot.Snapshot{Cols: 4, Rows: 4, Lines: []string{"a", "b", "c", "d"}, Cursor: snapshot.Cursor{X: 0, Y: 0}},
			rows: 2,
			want: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"c", "d"}},
		},
		{
			name: "styled blanks count as shown",
			snap: snapshot.Snapshot{Cols: 4, Rows: 3, Lines: []string{"a", "b", "  "}, Spans: [][]snapshot.Span{nil, nil, {{Start: 0, End: 2, Style: red}}}},
			rows: 2,
			want: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"b", "  "}, Spans: [][]snapshot.Span{nil, {{Start: 0, End: 2, Style: red}}}},
		},
		{
			name: "columns cut text, spans, widths and cursor",
			snap: snapshot.Snapshot{
				Cols: 8, Rows: 1, Lines: []string{"abcdefgh"},
				Spans:  [][]snapshot.Span{{{Start: 1, End: 3, Style: red}, {Start: 5, End: 8, Style: red}}},
				Cursor: snapshot.Cursor{X: 7, Visible: true},
			},
			cols: 4,
			want: snapshot.Snapshot{
				Cols: 4, Rows: 1, Lines: []string{"abcd"},
				Spans:  [][]snapshot.Span{{{Start: 1, End: 3, Style: red}}},
				Cursor: snapshot.Cursor{X: 3, Visible: true},
			},
		},
		{
			name: "a wide rune across the edge is dropped",
			snap: snapshot.Snapshot{
				Cols: 6, Rows: 1, Lines: []string{"ab世cd"},
				Widths: [][]snapshot.Wide{{{Rune: 2, Cells: 2}}},
			},
			cols: 3,
			want: snapshot.Snapshot{Cols: 3, Rows: 1, Lines: []string{"ab"}, Widths: [][]snapshot.Wide{nil}},
		},
		{
			name: "short span list",
			snap: snapshot.Snapshot{Cols: 4, Rows: 3, Lines: []string{"a", "b", "c"}, Spans: [][]snapshot.Span{{{Start: 0, End: 1, Style: red}}}},
			rows: 2,
			want: snapshot.Snapshot{Cols: 4, Rows: 2, Lines: []string{"b", "c"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := cloneSnapshot(tt.snap)
			got := Crop(tt.snap, tt.cols, tt.rows)
			if !reflect.DeepEqual(normalise(got), normalise(tt.want)) {
				t.Errorf("Crop() = %+v, want %+v", got, tt.want)
			}
			if !reflect.DeepEqual(tt.snap, orig) {
				t.Errorf("Crop modified its input: %+v, was %+v", tt.snap, orig)
			}
		})
	}
}

// normalise turns empty slices into nil so DeepEqual ignores the difference.
func normalise(s snapshot.Snapshot) snapshot.Snapshot {
	for i := range s.Spans {
		if len(s.Spans[i]) == 0 {
			s.Spans[i] = nil
		}
	}
	for i := range s.Widths {
		if len(s.Widths[i]) == 0 {
			s.Widths[i] = nil
		}
	}
	if len(s.Spans) == 0 {
		s.Spans = nil
	}
	if len(s.Widths) == 0 {
		s.Widths = nil
	}
	allNil := func(x [][]snapshot.Wide) bool {
		for _, w := range x {
			if w != nil {
				return false
			}
		}
		return true
	}
	if allNil(s.Widths) {
		s.Widths = nil
	}
	return s
}

func cloneSnapshot(s snapshot.Snapshot) snapshot.Snapshot {
	s.Lines = append([]string(nil), s.Lines...)
	spans := make([][]snapshot.Span, len(s.Spans))
	for i, sp := range s.Spans {
		spans[i] = append([]snapshot.Span(nil), sp...)
	}
	if s.Spans == nil {
		spans = nil
	}
	s.Spans = spans
	widths := make([][]snapshot.Wide, len(s.Widths))
	for i, w := range s.Widths {
		widths[i] = append([]snapshot.Wide(nil), w...)
	}
	if s.Widths == nil {
		widths = nil
	}
	s.Widths = widths
	return s
}

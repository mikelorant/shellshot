package ghostty

import (
	"reflect"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

func TestAppendWidesThenClipWides(t *testing.T) {
	tests := []struct {
		name  string
		pos   int
		limit int
		cell  cellInfo
		want  []snapshot.Wide
	}{
		{"narrow", 2, 10, cellInfo{runes: 1, width: 1}, nil},
		{"spacer", 2, 10, cellInfo{}, nil},
		{"wide", 2, 10, cellInfo{runes: 1, width: 2}, []snapshot.Wide{{Rune: 2, Cells: 2}}},
		{"wide past limit", 4, 4, cellInfo{runes: 1, width: 2}, nil},
		{"cluster", 1, 10, cellInfo{runes: 3, width: 1}, []snapshot.Wide{{Rune: 2}, {Rune: 3}}},
		{"wide cluster clipped", 1, 3, cellInfo{runes: 3, width: 2}, []snapshot.Wide{{Rune: 1, Cells: 2}, {Rune: 2}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := clipWides(appendWides(nil, tt.pos, tt.cell), tt.limit)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clipWides(appendWides) = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestClipSpans(t *testing.T) {
	red := snapshot.Style{Bg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	tests := []struct {
		name  string
		spans []snapshot.Span
		limit int
		want  []snapshot.Span
	}{
		{"inside", []snapshot.Span{{Start: 0, End: 2, Style: red}}, 5, []snapshot.Span{{Start: 0, End: 2, Style: red}}},
		{"straddles", []snapshot.Span{{Start: 1, End: 6, Style: red}}, 4, []snapshot.Span{{Start: 1, End: 4, Style: red}}},
		{"past", []snapshot.Span{{Start: 0, End: 2, Style: red}, {Start: 4, End: 6, Style: red}}, 4, []snapshot.Span{{Start: 0, End: 2, Style: red}}},
		{"none", nil, 4, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := clipSpans(tt.spans, tt.limit); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("clipSpans = %+v, want %+v", got, tt.want)
			}
		})
	}
}

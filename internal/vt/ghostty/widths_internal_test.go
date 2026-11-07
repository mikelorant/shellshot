package ghostty

import (
	"reflect"
	"testing"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

func TestAppendWidesClipsAtLimit(t *testing.T) {
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
			got := appendWides(nil, tt.pos, tt.limit, tt.cell)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("appendWides = %+v, want %+v", got, tt.want)
			}
		})
	}
}

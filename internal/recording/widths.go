package recording

import (
	"encoding/json"
	"fmt"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// wideJSON is the on-disk form of snapshot.Wide: [runeIndex,cells].
type wideJSON snapshot.Wide

// MarshalJSON encodes the entry as a [runeIndex,cells] pair.
func (w wideJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal([]int{w.Rune, int(w.Cells)})
}

// UnmarshalJSON decodes a [runeIndex,cells] pair.
func (w *wideJSON) UnmarshalJSON(data []byte) error {
	var parts []json.RawMessage
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	if len(parts) != 2 {
		return fmt.Errorf("width has %d elements, want 2", len(parts))
	}
	if err := json.Unmarshal(parts[0], &w.Rune); err != nil {
		return fmt.Errorf("width rune: %w", err)
	}
	if err := json.Unmarshal(parts[1], &w.Cells); err != nil {
		return fmt.Errorf("width cells: %w", err)
	}
	return nil
}

// encodeWidths converts snapshot widths to their on-disk form. It returns nil
// when no line has any entry, so the "widths" key is omitted.
func encodeWidths(lines [][]snapshot.Wide) [][]wideJSON {
	present := false
	out := make([][]wideJSON, len(lines))
	for y, wides := range lines {
		row := make([]wideJSON, len(wides))
		for i, w := range wides {
			row[i] = wideJSON(w)
		}
		out[y] = row
		present = present || len(wides) > 0
	}
	if !present {
		return nil
	}
	return out
}

// decodeWidths converts on-disk widths to snapshot widths. A nil input (no
// "widths" key) yields nil.
func decodeWidths(lines [][]wideJSON) [][]snapshot.Wide {
	if lines == nil {
		return nil
	}
	out := make([][]snapshot.Wide, len(lines))
	for y, row := range lines {
		if len(row) == 0 {
			continue
		}
		wides := make([]snapshot.Wide, len(row))
		for i, w := range row {
			wides[i] = snapshot.Wide(w)
		}
		out[y] = wides
	}
	return out
}

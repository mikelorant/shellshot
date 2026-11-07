package recording

import (
	"encoding/json"
	"fmt"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

// spanJSON is the on-disk form of snapshot.Span: [start,end,"<SGR>"].
type spanJSON snapshot.Span

// MarshalJSON encodes the span as a [start,end,"<SGR>"] triple.
func (s spanJSON) MarshalJSON() ([]byte, error) {
	return json.Marshal([]any{s.Start, s.End, s.Style.SGR()})
}

// UnmarshalJSON decodes a [start,end,"<SGR>"] triple.
func (s *spanJSON) UnmarshalJSON(data []byte) error {
	var parts []json.RawMessage
	if err := json.Unmarshal(data, &parts); err != nil {
		return err
	}
	if len(parts) != 3 {
		return fmt.Errorf("span has %d elements, want 3", len(parts))
	}
	var sgr string
	if err := json.Unmarshal(parts[0], &s.Start); err != nil {
		return fmt.Errorf("span start: %w", err)
	}
	if err := json.Unmarshal(parts[1], &s.End); err != nil {
		return fmt.Errorf("span end: %w", err)
	}
	if err := json.Unmarshal(parts[2], &sgr); err != nil {
		return fmt.Errorf("span style: %w", err)
	}
	style, err := snapshot.ParseSGR(sgr)
	if err != nil {
		return err
	}
	s.Style = style
	return nil
}

// encodeSpans converts snapshot spans to their on-disk form. It returns nil
// when no line has any span, so the "spans" key is omitted.
func encodeSpans(lines [][]snapshot.Span) [][]spanJSON {
	styled := false
	out := make([][]spanJSON, len(lines))
	for y, spans := range lines {
		row := make([]spanJSON, len(spans))
		for i, sp := range spans {
			row[i] = spanJSON(sp)
		}
		out[y] = row
		styled = styled || len(spans) > 0
	}
	if !styled {
		return nil
	}
	return out
}

// decodeSpans converts on-disk spans to snapshot spans. A nil input (no
// "spans" key) yields nil.
func decodeSpans(lines [][]spanJSON) [][]snapshot.Span {
	if lines == nil {
		return nil
	}
	out := make([][]snapshot.Span, len(lines))
	for y, row := range lines {
		if len(row) == 0 {
			continue
		}
		spans := make([]snapshot.Span, len(row))
		for i, sp := range row {
			spans[i] = snapshot.Span(sp)
		}
		out[y] = spans
	}
	return out
}

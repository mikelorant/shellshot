package timeline

import (
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

// styled is a frame that has no text but one styled cell.
func styledBlank(at time.Duration) snapshot.Frame {
	return snapshot.Frame{At: at, Snapshot: snapshot.Snapshot{
		Cols: 10, Rows: 1, Lines: []string{" "},
		Spans: [][]snapshot.Span{{{Start: 0, End: 1, Style: snapshot.Style{Attrs: snapshot.Inverse}}}},
	}}
}

func TestBuildTrimLeadingBlank(t *testing.T) {
	t.Parallel()
	// "" is the label of a blank screen (see snap).
	tests := []struct {
		name   string
		frames []snapshot.Frame
		opts   Options
		want   []want
	}{
		{
			name:   "off by default",
			frames: mk(fr{"", 0}, fr{"a", 100 * ms}),
			opts:   Options{Hold: sec},
			want:   []want{{"", 100 * ms}, {"a", sec}},
		},
		{
			name:   "short blank first step is dropped and its time is not given to the next",
			frames: mk(fr{"", 0}, fr{"a", 100 * ms}, fr{"b", 400 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: sec},
			want:   []want{{"a", 300 * ms}, {"b", sec}},
		},
		{
			name:   "blank exactly at the limit is dropped",
			frames: mk(fr{"", 0}, fr{"a", 250 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: sec},
			want:   []want{{"a", sec}},
		},
		{
			name:   "blank longer than the limit stays",
			frames: mk(fr{"", 0}, fr{"a", 251 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: sec},
			want:   []want{{"", 251 * ms}, {"a", sec}},
		},
		{
			name:   "the limit applies to the delay after speed",
			frames: mk(fr{"", 0}, fr{"a", 400 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Speed: 2, Hold: sec},
			want:   []want{{"a", sec}},
		},
		{
			name:   "the limit applies to the delay after the idle cap",
			frames: mk(fr{"", 0}, fr{"a", 5 * sec}),
			opts:   Options{TrimLeadingBlank: 250 * ms, IdleLimit: 200 * ms, Hold: sec},
			want:   []want{{"a", sec}},
		},
		{
			name:   "merged equal blank frames count as one step",
			frames: mk(fr{"", 0}, fr{"", 100 * ms}, fr{"a", 200 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: sec},
			want:   []want{{"a", sec}},
		},
		{
			name:   "a lone blank step is kept",
			frames: mk(fr{"", 0}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: 100 * ms},
			want:   []want{{"", 100 * ms}},
		},
		{
			name:   "a lone blank step after merging is kept",
			frames: mk(fr{"", 0}, fr{"", 50 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: 100 * ms},
			want:   []want{{"", 150 * ms}},
		},
		{
			name:   "only a leading blank is trimmed",
			frames: mk(fr{"a", 0}, fr{"", 100 * ms}, fr{"b", 200 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: sec},
			want:   []want{{"a", 100 * ms}, {"", 100 * ms}, {"b", sec}},
		},
		{
			name:   "a second blank step is not trimmed",
			frames: mk(fr{"", 0}, fr{"a", 100 * ms}, fr{"", 200 * ms}, fr{"b", 300 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: sec},
			want:   []want{{"a", 100 * ms}, {"", 100 * ms}, {"b", sec}},
		},
		{
			name:   "the step after the trim is protected from min-delay folding",
			frames: mk(fr{"", 0}, fr{"a", 100 * ms}, fr{"b", 105 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, MinDelay: 20 * ms, Hold: sec},
			want:   []want{{"a", 5 * ms}, {"b", sec}},
		},
		{
			name:   "text on the screen is not blank",
			frames: mk(fr{"$", 0}, fr{"a", 100 * ms}),
			opts:   Options{TrimLeadingBlank: 250 * ms, Hold: sec},
			want:   []want{{"$", 100 * ms}, {"a", sec}},
		},
		{
			name:   "a hidden span covering the start trims the blank shown at its end",
			frames: mk(fr{"x", 0}, fr{"", 2 * sec}, fr{"a", 2*sec + 100*ms}),
			opts: Options{TrimLeadingBlank: 250 * ms, Hold: sec, Segments: []Segment{
				{From: 0, To: 2 * sec, Skip: true},
			}},
			want: []want{{"a", sec}},
		},
		{
			name:   "a hidden span covering the start leaves a visible first step alone",
			frames: mk(fr{"", 0}, fr{"x", 100 * ms}, fr{"a", 2 * sec}, fr{"b", 3 * sec}),
			opts: Options{TrimLeadingBlank: 250 * ms, Hold: sec, Segments: []Segment{
				{From: 0, To: 2 * sec, Skip: true},
			}},
			want: []want{{"a", sec}, {"b", sec}},
		},
		{
			name:   "a hidden span after the blank does not stop the trim",
			frames: mk(fr{"", 0}, fr{"a", 100 * ms}, fr{"x", 200 * ms}, fr{"b", 3 * sec}),
			opts: Options{TrimLeadingBlank: 250 * ms, Hold: sec, Segments: []Segment{
				{From: 150 * ms, To: 3 * sec, Skip: true},
			}},
			want: []want{{"a", 50 * ms}, {"b", sec}},
		},
		{
			name:   "a segment boundary inside the blank does not change the rule",
			frames: mk(fr{"", 0}, fr{"a", 400 * ms}),
			opts: Options{TrimLeadingBlank: 250 * ms, Hold: sec, Segments: []Segment{
				{From: 100 * ms, To: 5 * sec, Speed: 2},
			}},
			// 100ms at 1x + 300ms at 2x = 250ms: at the limit.
			want: []want{{"a", sec}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Build(tt.frames, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d steps %v, want %d %v", len(got), describe(got), len(tt.want), tt.want)
			}
			for i, w := range tt.want {
				if got[i].Snapshot.Lines[0] != w.label || got[i].Delay != w.delay {
					t.Errorf("step %d = %q/%v, want %q/%v (all: %v)", i, got[i].Snapshot.Lines[0], got[i].Delay, w.label, w.delay, describe(got))
				}
			}
		})
	}
}

func TestBuildTrimLeadingBlankKeepsStyledBlank(t *testing.T) {
	t.Parallel()
	frames := []snapshot.Frame{styledBlank(0), {At: 100 * ms, Snapshot: snap("a")}}
	got, err := Build(frames, Options{TrimLeadingBlank: 250 * ms, Hold: sec})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d steps, want 2: a screen with a styled cell is not blank", len(got))
	}
}

func TestBuildTrimLeadingBlankNegative(t *testing.T) {
	t.Parallel()
	if _, err := Build(mk(fr{"a", 0}), Options{TrimLeadingBlank: -1}); err == nil {
		t.Error("Build with a negative TrimLeadingBlank succeeded, want an error")
	}
}

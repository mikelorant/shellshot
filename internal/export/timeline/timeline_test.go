package timeline

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/snapshot"
)

const ms = time.Millisecond

// snap returns a distinct snapshot per label.
func snap(label string) snapshot.Snapshot {
	return snapshot.Snapshot{Cols: 10, Rows: 1, Lines: []string{label}}
}

// fr is a labelled frame timestamp; mk turns them into frames.
type fr struct {
	label string
	at    time.Duration
}

func mk(fs ...fr) []snapshot.Frame {
	out := make([]snapshot.Frame, len(fs))
	for i, f := range fs {
		out[i] = snapshot.Frame{At: f.at, Snapshot: snap(f.label)}
	}
	return out
}

type want struct {
	label string
	delay time.Duration
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name   string
		frames []snapshot.Frame
		opts   Options
		want   []want
	}{
		{name: "empty", frames: nil, opts: Options{Hold: time.Second}, want: nil},
		{
			name:   "single frame gets hold",
			frames: mk(fr{"a", 500 * ms}),
			opts:   Options{Hold: 2 * time.Second},
			want:   []want{{"a", 2 * time.Second}},
		},
		{
			name:   "single frame zero hold",
			frames: mk(fr{"a", 0}),
			want:   []want{{"a", 0}},
		},
		{
			name:   "delays are next minus this, last is hold",
			frames: mk(fr{"a", 0}, fr{"b", 100 * ms}, fr{"c", 350 * ms}),
			opts:   Options{Hold: 700 * ms},
			want:   []want{{"a", 100 * ms}, {"b", 250 * ms}, {"c", 700 * ms}},
		},
		{
			name:   "backwards timestamp counts as zero",
			frames: mk(fr{"a", 100 * ms}, fr{"b", 50 * ms}, fr{"c", 150 * ms}),
			opts:   Options{Hold: 10 * ms},
			want:   []want{{"a", 0}, {"b", 100 * ms}, {"c", 10 * ms}},
		},
		{
			name:   "idle cap applies to gaps but not hold",
			frames: mk(fr{"a", 0}, fr{"b", 10 * time.Second}, fr{"c", 10*time.Second + 300*ms}),
			opts:   Options{IdleLimit: 2 * time.Second, Hold: 5 * time.Second},
			want:   []want{{"a", 2 * time.Second}, {"b", 300 * ms}, {"c", 5 * time.Second}},
		},
		{
			name:   "speed 2x halves gaps but not hold",
			frames: mk(fr{"a", 0}, fr{"b", 400 * ms}, fr{"c", 600 * ms}),
			opts:   Options{Speed: 2, Hold: time.Second},
			want:   []want{{"a", 200 * ms}, {"b", 100 * ms}, {"c", time.Second}},
		},
		{
			name:   "speed 0.5 doubles gaps",
			frames: mk(fr{"a", 0}, fr{"b", 100 * ms}),
			opts:   Options{Speed: 0.5, Hold: 10 * ms},
			want:   []want{{"a", 200 * ms}, {"b", 10 * ms}},
		},
		{
			name:   "idle cap before speed",
			frames: mk(fr{"a", 0}, fr{"b", 10 * time.Second}),
			opts:   Options{Speed: 2, IdleLimit: 2 * time.Second, Hold: 0},
			want:   []want{{"a", time.Second}, {"b", 0}},
		},
		{
			name:   "speed rounds to nearest nanosecond",
			frames: mk(fr{"a", 0}, fr{"b", 10}),
			opts:   Options{Speed: 3},
			want:   []want{{"a", 3}, {"b", 0}},
		},
		{
			name:   "equal consecutive snapshots merge",
			frames: mk(fr{"a", 0}, fr{"a", 100 * ms}, fr{"b", 250 * ms}, fr{"b", 300 * ms}),
			opts:   Options{Hold: 50 * ms},
			want:   []want{{"a", 250 * ms}, {"b", 100 * ms}},
		},
		{
			name:   "equal non-consecutive snapshots stay separate",
			frames: mk(fr{"a", 0}, fr{"b", 100 * ms}, fr{"a", 200 * ms}),
			opts:   Options{Hold: 100 * ms},
			want:   []want{{"a", 100 * ms}, {"b", 100 * ms}, {"a", 100 * ms}},
		},
		{
			name:   "dedup merges trailing duplicate into hold",
			frames: mk(fr{"a", 0}, fr{"a", 100 * ms}),
			opts:   Options{Hold: time.Second},
			want:   []want{{"a", 1100 * ms}},
		},
		{
			name:   "min delay folds short step into previous",
			frames: mk(fr{"a", 0}, fr{"b", 100 * ms}, fr{"c", 105 * ms}, fr{"d", 205 * ms}),
			opts:   Options{MinDelay: 20 * ms, Hold: 100 * ms},
			want:   []want{{"a", 105 * ms}, {"c", 100 * ms}, {"d", 100 * ms}},
		},
		{
			name:   "min delay 5,5,100 protects first frame",
			frames: mk(fr{"a", 0}, fr{"b", 5 * ms}, fr{"c", 10 * ms}, fr{"d", 110 * ms}),
			opts:   Options{MinDelay: 20 * ms, Hold: 100 * ms},
			// a(5) kept as first; b(5) folds into a; c(100) stays; d is last.
			want: []want{{"a", 10 * ms}, {"c", 100 * ms}, {"d", 100 * ms}},
		},
		{
			name:   "min delay folds into previous after long step",
			frames: mk(fr{"a", 0}, fr{"b", 100 * ms}, fr{"c", 105 * ms}, fr{"d", 110 * ms}, fr{"e", 210 * ms}),
			opts:   Options{MinDelay: 20 * ms, Hold: 100 * ms},
			want:   []want{{"a", 110 * ms}, {"d", 100 * ms}, {"e", 100 * ms}},
		},
		{
			name:   "step exactly at min delay is kept",
			frames: mk(fr{"a", 0}, fr{"b", 20 * ms}, fr{"c", 40 * ms}),
			opts:   Options{MinDelay: 20 * ms, Hold: 20 * ms},
			want:   []want{{"a", 20 * ms}, {"b", 20 * ms}, {"c", 20 * ms}},
		},
		{
			name:   "min delay never drops last frame",
			frames: mk(fr{"a", 0}, fr{"b", 100 * ms}),
			opts:   Options{MinDelay: 20 * ms, Hold: 0},
			want:   []want{{"a", 100 * ms}, {"b", 0}},
		},
		{
			name:   "min delay never drops first frame",
			frames: mk(fr{"a", 0}, fr{"b", 1 * ms}, fr{"c", 2 * ms}, fr{"d", 200 * ms}),
			opts:   Options{MinDelay: 20 * ms, Hold: 50 * ms},
			want:   []want{{"a", 2 * ms}, {"c", 198 * ms}, {"d", 50 * ms}},
		},
		{
			name: "dedup happens before min delay folding",
			// a(10) + a(10) merge to 20 and survive MinDelay 20.
			frames: mk(fr{"a", 0}, fr{"a", 10 * ms}, fr{"b", 20 * ms}),
			opts:   Options{MinDelay: 20 * ms, Hold: 100 * ms},
			want:   []want{{"a", 20 * ms}, {"b", 100 * ms}},
		},
		{
			name:   "min delay applies after speed",
			frames: mk(fr{"a", 0}, fr{"b", 100 * ms}, fr{"c", 140 * ms}),
			opts:   Options{Speed: 4, MinDelay: 20 * ms, Hold: 100 * ms},
			// a=25ms, b=10ms (<20, folded into a), c=hold.
			want: []want{{"a", 35 * ms}, {"c", 100 * ms}},
		},
		{
			name:   "single short step with only a first and last",
			frames: mk(fr{"a", 0}, fr{"b", 5 * ms}),
			opts:   Options{MinDelay: 20 * ms},
			want:   []want{{"a", 5 * ms}, {"b", 0}},
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
					t.Errorf("step %d = %s/%v, want %s/%v (all: %v)", i, got[i].Snapshot.Lines[0], got[i].Delay, w.label, w.delay, describe(got))
				}
			}
		})
	}
}

func describe(steps []Step) []want {
	out := make([]want, len(steps))
	for i, s := range steps {
		out[i] = want{s.Snapshot.Lines[0], s.Delay}
	}
	return out
}

func TestBuildErrors(t *testing.T) {
	f := mk(fr{"a", 0}, fr{"b", 10 * ms})
	tests := []struct {
		name string
		opts Options
	}{
		{"negative speed", Options{Speed: -1}},
		{"NaN speed", Options{Speed: math.NaN()}},
		{"+Inf speed", Options{Speed: math.Inf(1)}},
		{"-Inf speed", Options{Speed: math.Inf(-1)}},
		{"negative idle limit", Options{IdleLimit: -1}},
		{"negative hold", Options{Hold: -1}},
		{"negative min delay", Options{MinDelay: -1}},
		{"speed overflow", Options{Speed: 1e-30}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Build(f, tt.opts); err == nil {
				t.Errorf("Build = %v, want error", got)
			}
		})
	}
	t.Run("errors even on empty input", func(t *testing.T) {
		if _, err := Build(nil, Options{Speed: -1}); err == nil {
			t.Error("want error")
		}
	})
}

// TestBuildTotalDuration checks that dedup and min-delay folding preserve
// the total: it equals the sum of capped and sped gaps plus the hold.
func TestBuildTotalDuration(t *testing.T) {
	ats := []time.Duration{0, 3 * ms, 8 * ms, 5 * ms, 400 * ms, 405 * ms, 9 * time.Second, 9*time.Second + 7*ms, 9*time.Second + 500*ms}
	labels := []string{"a", "a", "b", "c", "c", "d", "e", "f", "f"}
	var fs []fr
	for i := range ats {
		fs = append(fs, fr{labels[i], ats[i]})
	}
	frames := mk(fs...)
	for _, speed := range []float64{0, 1, 2, 3, 0.5} {
		for _, idle := range []time.Duration{0, 2 * time.Second, 50 * ms} {
			for _, minDelay := range []time.Duration{0, 20 * ms, time.Second} {
				o := Options{Speed: speed, IdleLimit: idle, Hold: 1234 * ms, MinDelay: minDelay}
				got, err := Build(frames, o)
				if err != nil {
					t.Fatal(err)
				}
				s := speed
				if s == 0 {
					s = 1
				}
				want := o.Hold
				for i := 0; i+1 < len(frames); i++ {
					raw := max(frames[i+1].At-frames[i].At, 0)
					if idle > 0 {
						raw = min(raw, idle)
					}
					want += time.Duration(math.Round(float64(raw) / s))
				}
				var total time.Duration
				for _, st := range got {
					total += st.Delay
				}
				if total != want {
					t.Errorf("%+v: total = %v, want %v", o, total, want)
				}
				if got[0].Snapshot.Lines[0] != "a" || got[len(got)-1].Snapshot.Lines[0] != "f" {
					t.Errorf("%+v: first/last = %v", o, describe(got))
				}
				for i := 1; i < len(got); i++ {
					if got[i].Snapshot.Equal(got[i-1].Snapshot) {
						t.Errorf("%+v: adjacent equal steps at %d", o, i)
					}
					if minDelay > 0 && got[i].Delay < minDelay && i != len(got)-1 {
						t.Errorf("%+v: step %d delay %v below min", o, i, got[i].Delay)
					}
				}
			}
		}
	}
}

func TestBuildDoesNotMutateInput(t *testing.T) {
	frames := mk(fr{"a", 0}, fr{"a", 5 * ms}, fr{"b", 10 * ms})
	before := slices.Clone(frames)
	if _, err := Build(frames, Options{MinDelay: time.Second}); err != nil {
		t.Fatal(err)
	}
	if !slices.EqualFunc(frames, before, func(a, b snapshot.Frame) bool {
		return a.At == b.At && a.Snapshot.Equal(b.Snapshot)
	}) {
		t.Error("input frames were modified")
	}
}

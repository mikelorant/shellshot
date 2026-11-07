package timeline

import (
	"math"
	"math/rand/v2"
	"slices"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/snapshot"
)

const sec = time.Second

func TestBuildSegments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		frames []snapshot.Frame
		opts   Options
		want   []want
	}{
		{
			name:   "gap is split at a boundary and each piece uses its own speed",
			frames: mk(fr{"a", 0}, fr{"b", 10 * sec}, fr{"c", 12 * sec}),
			opts: Options{Hold: sec, Segments: []Segment{
				{From: 5 * sec, To: 15 * sec, Speed: 2},
			}},
			// a: 5s at 1x + 5s at 2x = 7.5s; b: 2s at 2x = 1s.
			want: []want{{"a", 7500 * ms}, {"b", sec}, {"c", sec}},
		},
		{
			name:   "8x middle segment",
			frames: mk(fr{"a", 0}, fr{"b", sec}, fr{"c", 9 * sec}, fr{"d", 17 * sec}, fr{"e", 18 * sec}),
			opts: Options{Hold: 500 * ms, Segments: []Segment{
				{From: sec, To: 17 * sec, Speed: 8},
			}},
			want: []want{{"a", sec}, {"b", sec}, {"c", sec}, {"d", sec}, {"e", 500 * ms}},
		},
		{
			name:   "speed 0 inherits the global speed",
			frames: mk(fr{"a", 0}, fr{"b", 4 * sec}),
			opts: Options{Speed: 2, Segments: []Segment{
				{From: 0, To: 10 * sec, Hold: 0},
			}},
			want: []want{{"a", 2 * sec}, {"b", 0}},
		},
		{
			name:   "segment speed replaces the global speed",
			frames: mk(fr{"a", 0}, fr{"b", 4 * sec}),
			opts: Options{Speed: 2, Segments: []Segment{
				{From: 0, To: 10 * sec, Speed: 4},
			}},
			want: []want{{"a", sec}, {"b", 0}},
		},
		{
			name:   "segment idle limit lowers the global one",
			frames: mk(fr{"a", 0}, fr{"b", 4 * sec}),
			opts: Options{IdleLimit: 3 * sec, Segments: []Segment{
				{From: 0, To: 10 * sec, IdleLimit: sec},
			}},
			want: []want{{"a", sec}, {"b", 0}},
		},
		{
			name:   "segment idle limit raises the global one",
			frames: mk(fr{"a", 0}, fr{"b", 4 * sec}),
			opts: Options{IdleLimit: sec, Segments: []Segment{
				{From: 0, To: 10 * sec, IdleLimit: 3 * sec},
			}},
			want: []want{{"a", 3 * sec}, {"b", 0}},
		},
		{
			name:   "outside every segment the global idle limit applies",
			frames: mk(fr{"a", 0}, fr{"b", 8 * sec}),
			opts: Options{IdleLimit: sec, Segments: []Segment{
				{From: 20 * sec, To: 30 * sec, IdleLimit: 9 * sec},
			}},
			want: []want{{"a", sec}, {"b", 0}},
		},
		{
			// Two adjacent scenes, 2s limit each, a 3s pause in each: the
			// stretch is capped once (2s), not once per piece (4s).
			name:   "double idle cap at a boundary is a single cap",
			frames: mk(fr{"a", 0}, fr{"b", 6 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: 3 * sec, IdleLimit: 2 * sec},
				{From: 3 * sec, To: 10 * sec, IdleLimit: 2 * sec},
			}},
			want: []want{{"a", 2 * sec}, {"b", 0}},
		},
		{
			name:   "double idle cap with the global limit and an inheriting segment",
			frames: mk(fr{"a", 0}, fr{"b", 6 * sec}),
			opts: Options{IdleLimit: 2 * sec, Segments: []Segment{
				{From: 3 * sec, To: 10 * sec},
			}},
			want: []want{{"a", 2 * sec}, {"b", 0}},
		},
		{
			// Budget 2s (the smaller limit): 1s from the first piece at 1x,
			// then 1s from the second at 2x.
			name:   "idle budget is the smallest limit and is spent in order",
			frames: mk(fr{"a", 0}, fr{"b", 6 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: sec, IdleLimit: 2 * sec},
				{From: sec, To: 10 * sec, IdleLimit: 3 * sec, Speed: 2},
			}},
			want: []want{{"a", 1500 * ms}, {"b", 0}},
		},
		{
			name:   "frames inside a Skip segment are dropped and time is removed",
			frames: mk(fr{"a", 0}, fr{"x", sec}, fr{"y", 2 * sec}, fr{"b", 5 * sec}, fr{"c", 6 * sec}),
			opts: Options{Hold: 300 * ms, Segments: []Segment{
				{From: sec, To: 5 * sec, Skip: true},
			}},
			// b is the forced boundary frame carrying the post-span screen.
			want: []want{{"a", sec}, {"b", sec}, {"c", 300 * ms}},
		},
		{
			name:   "skipped time adds nothing even with idle cap and speed",
			frames: mk(fr{"a", 0}, fr{"b", 100 * sec}),
			opts: Options{IdleLimit: 10 * sec, Speed: 2, Segments: []Segment{
				{From: sec, To: 99 * sec, Skip: true},
			}},
			// 1s before + 1s after, 2s total, halved. The skip piece
			// consumes no idle budget.
			want: []want{{"a", sec}, {"b", 0}},
		},
		{
			name:   "no frame at the Skip end: post-skip time lands on the previous step",
			frames: mk(fr{"a", 0}, fr{"x", 2 * sec}, fr{"c", 7 * sec}),
			opts: Options{Segments: []Segment{
				{From: 2 * sec, To: 4 * sec, Skip: true},
			}},
			// x is dropped; its gap [2s,7s) is 2s skipped + 3s shown.
			want: []want{{"a", 5 * sec}, {"c", 0}},
		},
		{
			name:   "trailing Skip: the final hold goes to the last visible step",
			frames: mk(fr{"a", 0}, fr{"b", sec}, fr{"x", 3 * sec}),
			opts: Options{Hold: 700 * ms, Segments: []Segment{
				{From: 2 * sec, To: 10 * sec, Skip: true},
			}},
			want: []want{{"a", sec}, {"b", sec + 700*ms}},
		},
		{
			name:   "leading Skip",
			frames: mk(fr{"x", 0}, fr{"b", 2 * sec}, fr{"c", 3 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: 2 * sec, Skip: true},
			}},
			want: []want{{"b", sec}, {"c", 0}},
		},
		{
			name:   "every frame skipped yields nothing",
			frames: mk(fr{"a", 0}, fr{"b", sec}),
			opts: Options{Hold: sec, Segments: []Segment{
				{From: 0, To: 5 * sec, Skip: true},
			}},
			want: nil,
		},
		{
			name:   "Skip To is exclusive",
			frames: mk(fr{"a", 0}, fr{"b", sec}, fr{"c", 2 * sec}),
			opts: Options{Segments: []Segment{
				{From: sec, To: 2 * sec, Skip: true},
			}},
			want: []want{{"a", sec}, {"c", 0}},
		},
		{
			name:   "dedup across a Skip merges the screens before and after",
			frames: mk(fr{"a", 0}, fr{"x", sec}, fr{"a", 2 * sec}, fr{"b", 3 * sec}),
			opts: Options{Segments: []Segment{
				{From: sec, To: 2 * sec, Skip: true},
			}},
			want: []want{{"a", 2 * sec}, {"b", 0}},
		},
		{
			name:   "dedup across a segment boundary sums the pieces",
			frames: mk(fr{"a", 0}, fr{"a", sec}, fr{"b", 2 * sec}),
			opts: Options{Segments: []Segment{
				{From: sec, To: 3 * sec, Speed: 2},
			}},
			want: []want{{"a", 1500 * ms}, {"b", 0}},
		},
		{
			name:   "Hold is added to the last step at the segment end",
			frames: mk(fr{"a", 0}, fr{"b", sec}, fr{"c", 2 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: 1500 * ms, Hold: sec},
			}},
			want: []want{{"a", sec}, {"b", 2 * sec}, {"c", 0}},
		},
		{
			name:   "Hold on a segment that reaches the final frame adds to Options.Hold",
			frames: mk(fr{"a", 0}, fr{"b", sec}),
			opts: Options{Hold: 500 * ms, Segments: []Segment{
				{From: 0, To: 10 * sec, Hold: sec},
			}},
			want: []want{{"a", sec}, {"b", 1500 * ms}},
		},
		{
			name:   "Hold is neither divided by speed nor capped by idle",
			frames: mk(fr{"a", 0}, fr{"b", 8 * sec}, fr{"c", 9 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: 8500 * ms, Speed: 4, IdleLimit: sec, Hold: 5 * sec},
			}},
			// a: 8s capped to 1s, /4 = 250ms. b: 0.5s at 4x (125ms) plus 0.5s
			// at 1x, plus the 5s hold at the segment end.
			want: []want{{"a", 250 * ms}, {"b", 5*sec + 625*ms}, {"c", 0}},
		},
		{
			name:   "Hold on a middle segment leaves the other segments alone",
			frames: mk(fr{"a", 0}, fr{"b", 2 * sec}, fr{"c", 4 * sec}, fr{"d", 6 * sec}),
			opts: Options{Segments: []Segment{
				{From: 2 * sec, To: 4 * sec, Hold: sec},
			}},
			want: []want{{"a", 2 * sec}, {"b", 3 * sec}, {"c", 2 * sec}, {"d", 0}},
		},
		{
			name:   "Hold on a segment that ends in a trailing Skip lengthens the last visible step",
			frames: mk(fr{"a", 0}, fr{"b", sec}, fr{"x", 2 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: sec + 500*ms},
				{From: sec + 500*ms, To: 5 * sec, Skip: true, Hold: 2 * sec},
			}},
			want: []want{{"a", sec}, {"b", 2500 * ms}},
		},
		{
			name:   "segment starting after the last frame adds no hold",
			frames: mk(fr{"a", 0}, fr{"b", sec}),
			opts: Options{Segments: []Segment{
				{From: 5 * sec, To: 6 * sec, Hold: sec},
			}},
			want: []want{{"a", sec}, {"b", 0}},
		},
		{
			name:   "hold-lengthened step is not folded by MinDelay",
			frames: mk(fr{"a", 0}, fr{"b", 5 * ms}, fr{"c", 10 * ms}, fr{"d", sec}),
			opts: Options{MinDelay: 20 * ms, Segments: []Segment{
				{From: 0, To: 8 * ms, Hold: 100 * ms},
			}},
			want: []want{{"a", 5 * ms}, {"b", 105 * ms}, {"c", 990 * ms}, {"d", 0}},
		},
		{
			// b's delay is 10ms at 1x then 1ms at 10x = 11ms, under MinDelay,
			// so it folds into a even though it straddles the boundary.
			name:   "min-delay folding across a segment boundary",
			frames: mk(fr{"a", 0}, fr{"b", 990 * ms}, fr{"c", 1010 * ms}, fr{"d", 2 * sec}),
			opts: Options{MinDelay: 20 * ms, Segments: []Segment{
				{From: 0, To: sec, Speed: 1},
				{From: sec, To: 3 * sec, Speed: 10},
			}},
			want: []want{{"a", 1001 * ms}, {"c", 99 * ms}, {"d", 0}},
		},
		{
			name:   "backwards timestamps still count as zero",
			frames: mk(fr{"a", 5 * sec}, fr{"b", 2 * sec}, fr{"c", 4 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: 10 * sec, Speed: 2},
			}},
			want: []want{{"a", 0}, {"b", sec}, {"c", 0}},
		},
		{
			name:   "adjacent segments",
			frames: mk(fr{"a", 0}, fr{"b", 2 * sec}, fr{"c", 4 * sec}),
			opts: Options{Segments: []Segment{
				{From: 0, To: 2 * sec, Speed: 2},
				{From: 2 * sec, To: 4 * sec, Speed: 4},
			}},
			want: []want{{"a", sec}, {"b", 500 * ms}, {"c", 0}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Build(tt.frames, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(tt.want) == 0 {
				if len(got) != 0 {
					t.Fatalf("got %v, want no steps", describe(got))
				}
				return
			}
			if !slices.Equal(describe(got), tt.want) {
				t.Fatalf("got %v, want %v", describe(got), tt.want)
			}
		})
	}
}

func TestBuildSegmentsDoNotMutateInput(t *testing.T) {
	t.Parallel()
	frames := mk(fr{"a", 0}, fr{"x", sec}, fr{"b", 3 * sec})
	segs := []Segment{{From: sec, To: 2 * sec, Skip: true, Hold: sec}}
	beforeFrames, beforeSegs := slices.Clone(frames), slices.Clone(segs)
	if _, err := Build(frames, Options{Segments: segs}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(segs, beforeSegs) {
		t.Error("segments were modified")
	}
	if !slices.EqualFunc(frames, beforeFrames, func(a, b snapshot.Frame) bool {
		return a.At == b.At && a.Snapshot.Equal(b.Snapshot)
	}) {
		t.Error("frames were modified")
	}
}

func TestBuildSegmentsDeterministic(t *testing.T) {
	t.Parallel()
	frames := mk(fr{"a", 0}, fr{"b", sec}, fr{"x", 2 * sec}, fr{"c", 5 * sec}, fr{"c", 6 * sec}, fr{"d", 9 * sec})
	o := Options{Speed: 1.5, IdleLimit: 2 * sec, Hold: sec, MinDelay: 30 * ms, Segments: []Segment{
		{From: 0, To: 1500 * ms, Speed: 3, Hold: sec},
		{From: 2 * sec, To: 4 * sec, Skip: true},
		{From: 4 * sec, To: 7 * sec, IdleLimit: 500 * ms, Speed: 0.7},
	}}
	first, err := Build(frames, o)
	if err != nil {
		t.Fatal(err)
	}
	for range 5 {
		again, err := Build(frames, o)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.EqualFunc(first, again, func(a, b Step) bool {
			return a.Delay == b.Delay && a.Snapshot.Equal(b.Snapshot)
		}) {
			t.Fatalf("non-deterministic: %v vs %v", describe(first), describe(again))
		}
	}
}

func TestBuildSegmentValidation(t *testing.T) {
	t.Parallel()
	f := mk(fr{"a", 0}, fr{"b", sec})
	tests := []struct {
		name string
		segs []Segment
	}{
		{"overlap", []Segment{{From: 0, To: 5 * sec}, {From: 3 * sec, To: 8 * sec}}},
		{"nested", []Segment{{From: 0, To: 10 * sec}, {From: 2 * sec, To: 3 * sec}}},
		{"identical", []Segment{{From: 0, To: sec}, {From: 0, To: sec}}},
		{"unsorted", []Segment{{From: 5 * sec, To: 6 * sec}, {From: sec, To: 2 * sec}}},
		{"From equals To", []Segment{{From: sec, To: sec}}},
		{"From after To", []Segment{{From: 2 * sec, To: sec}}},
		{"negative From", []Segment{{From: -1, To: sec}}},
		{"negative To", []Segment{{From: -2, To: -1}}},
		{"negative speed", []Segment{{From: 0, To: sec, Speed: -1}}},
		{"NaN speed", []Segment{{From: 0, To: sec, Speed: math.NaN()}}},
		{"Inf speed", []Segment{{From: 0, To: sec, Speed: math.Inf(1)}}},
		{"negative idle", []Segment{{From: 0, To: sec, IdleLimit: -1}}},
		{"negative hold", []Segment{{From: 0, To: sec, Hold: -1}}},
		{"bad segment after a good one", []Segment{{From: 0, To: sec}, {From: sec, To: sec}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := Build(f, Options{Segments: tt.segs}); err == nil {
				t.Errorf("Build = %v, want error", got)
			}
		})
	}
	t.Run("errors even on empty input", func(t *testing.T) {
		if _, err := Build(nil, Options{Segments: invalidSegments}); err == nil {
			t.Error("want error")
		}
	})
	t.Run("adjacent segments are valid", func(t *testing.T) {
		segs := []Segment{{From: 0, To: sec}, {From: sec, To: 2 * sec}}
		if _, err := Build(f, Options{Segments: segs}); err != nil {
			t.Error(err)
		}
	})
}

var invalidSegments = []Segment{{From: 2, To: 1}}

// referenceBuild is the pre-segments Build, kept as an oracle for the
// "no segments means no behaviour change" property.
func referenceBuild(frames []snapshot.Frame, o Options) ([]Step, error) {
	speed := o.Speed
	if speed == 0 {
		speed = 1
	}
	if len(frames) == 0 {
		return nil, nil
	}
	steps := make([]Step, 0, len(frames))
	for i, f := range frames {
		var delay time.Duration
		if i == len(frames)-1 {
			delay = o.Hold
		} else {
			raw := max(frames[i+1].At-f.At, 0)
			if o.IdleLimit > 0 {
				raw = min(raw, o.IdleLimit)
			}
			delay = time.Duration(math.Round(float64(raw) / speed))
		}
		if n := len(steps); n > 0 && steps[n-1].Snapshot.Equal(f.Snapshot) {
			steps[n-1].Delay += delay
			continue
		}
		steps = append(steps, Step{Snapshot: f.Snapshot, Delay: delay})
	}
	if o.MinDelay > 0 {
		kept := steps[:1]
		for i := 1; i < len(steps); i++ {
			s := steps[i]
			if s.Delay < o.MinDelay && i != len(steps)-1 {
				kept[len(kept)-1].Delay += s.Delay
				continue
			}
			kept = append(kept, s)
		}
		steps = kept
	}
	return steps, nil
}

func randomFrames(r *rand.Rand, n int, labels int) []snapshot.Frame {
	frames := make([]snapshot.Frame, n)
	var at time.Duration
	for i := range frames {
		switch r.IntN(6) {
		case 0:
			at -= time.Duration(r.IntN(300)) * ms // backwards
			at = max(at, 0)
		case 1:
			at += time.Duration(r.IntN(20)) * sec
		default:
			at += time.Duration(r.IntN(400)) * ms
		}
		frames[i] = snapshot.Frame{At: at, Snapshot: snap(string(rune('a' + r.IntN(labels))))}
	}
	return frames
}

func sameSteps(a, b []Step) bool {
	return slices.EqualFunc(a, b, func(x, y Step) bool {
		return x.Delay == y.Delay && x.Snapshot.Equal(y.Snapshot)
	})
}

// TestBuildWithoutSegmentsMatchesReference compares the new Build against the
// old algorithm, for nil segments, empty segments and segments that change
// nothing (zero fields, no skip, no hold) however they split the timeline.
func TestBuildWithoutSegmentsMatchesReference(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(1, 2))
	for iter := range 400 {
		frames := randomFrames(r, 1+r.IntN(30), 1+r.IntN(5))
		o := Options{
			Speed:     []float64{0, 1, 2, 0.5, 3.3}[r.IntN(5)],
			IdleLimit: []time.Duration{0, 100 * ms, 2 * sec}[r.IntN(3)],
			Hold:      time.Duration(r.IntN(2000)) * ms,
			MinDelay:  []time.Duration{0, 20 * ms, 300 * ms}[r.IntN(3)],
		}
		want, err := referenceBuild(frames, o)
		if err != nil {
			t.Fatal(err)
		}
		var noops []Segment
		var at time.Duration
		for range r.IntN(6) {
			at += time.Duration(r.IntN(5000)) * ms
			end := at + time.Duration(1+r.IntN(5000))*ms
			noops = append(noops, Segment{From: at, To: end})
			at = end
		}
		for name, segs := range map[string][]Segment{"nil": nil, "empty": {}, "noop": noops} {
			oo := o
			oo.Segments = segs
			got, err := Build(frames, oo)
			if err != nil {
				t.Fatal(err)
			}
			if !sameSteps(got, want) {
				t.Fatalf("iter %d (%s) %+v: got %v, want %v", iter, name, o, describe(got), describe(want))
			}
		}
	}
}

// oracleTotal recomputes the expected total duration with a deliberately
// simple method: split each gap at every segment bound, look each part's
// segment up linearly, then spend the smallest limit as a budget.
func oracleTotal(frames []snapshot.Frame, o Options) time.Duration {
	speed := o.Speed
	if speed == 0 {
		speed = 1
	}
	segAt := func(t time.Duration) *Segment {
		for i := range o.Segments {
			if o.Segments[i].From <= t && t < o.Segments[i].To {
				return &o.Segments[i]
			}
		}
		return nil
	}
	total := o.Hold
	for i := 0; i+1 < len(frames); i++ {
		a, b := frames[i].At, frames[i+1].At
		if b <= a {
			continue
		}
		cuts := []time.Duration{a, b}
		for _, s := range o.Segments {
			for _, c := range []time.Duration{s.From, s.To} {
				if c > a && c < b {
					cuts = append(cuts, c)
				}
			}
		}
		slices.Sort(cuts)
		type part struct {
			raw   time.Duration
			speed float64
		}
		var parts []part
		var limit time.Duration
		for k := 0; k+1 < len(cuts); k++ {
			if cuts[k] == cuts[k+1] {
				continue
			}
			s := segAt(cuts[k])
			if s != nil && s.Skip {
				continue
			}
			p := part{cuts[k+1] - cuts[k], speed}
			l := o.IdleLimit
			if s != nil {
				if s.Speed != 0 {
					p.speed = s.Speed
				}
				if s.IdleLimit != 0 {
					l = s.IdleLimit
				}
			}
			if l != 0 && (limit == 0 || l < limit) {
				limit = l
			}
			parts = append(parts, p)
		}
		var sum float64
		left := limit
		for _, p := range parts {
			take := p.raw
			if limit != 0 {
				take = min(take, left)
				left -= take
			}
			sum += float64(take) / p.speed
		}
		total += time.Duration(math.Round(sum))
	}
	last := frames[len(frames)-1].At
	for _, s := range o.Segments {
		if s.From <= last {
			total += s.Hold
		}
	}
	return total
}

// TestBuildSegmentsRandomTotals checks, over random segments, that the total
// duration matches the oracle whatever dedup and min-delay folding do, that
// and that results are stable.
func TestBuildSegmentsRandomTotals(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(3, 4))
	for iter := range 500 {
		frames := randomFrames(r, 2+r.IntN(30), 1+r.IntN(6))
		// The first frame sits at 0, outside every segment, so no hold or
		// delay is ever dropped for lack of a preceding step.
		frames[0].At = 0
		var segs []Segment
		at := time.Duration(1+r.IntN(3000)) * ms
		for range r.IntN(6) {
			end := at + time.Duration(1+r.IntN(8000))*ms
			s := Segment{From: at, To: end}
			switch r.IntN(5) {
			case 0:
				s.Skip = true
			case 1:
				s.Speed = []float64{0.5, 2, 8}[r.IntN(3)]
			case 2:
				s.IdleLimit = []time.Duration{50 * ms, sec}[r.IntN(2)]
			}
			if r.IntN(3) == 0 {
				s.Hold = time.Duration(r.IntN(1500)) * ms
			}
			segs = append(segs, s)
			at = end + time.Duration(r.IntN(3))*sec
		}
		o := Options{
			Speed:     []float64{0, 2, 0.5}[r.IntN(3)],
			IdleLimit: []time.Duration{0, 200 * ms, 3 * sec}[r.IntN(3)],
			Hold:      time.Duration(r.IntN(1000)) * ms,
			MinDelay:  []time.Duration{0, 20 * ms, 500 * ms}[r.IntN(3)],
			Segments:  segs,
		}
		got, err := Build(frames, o)
		if err != nil {
			t.Fatalf("iter %d: %v", iter, err)
		}
		var total time.Duration
		for _, s := range got {
			total += s.Delay
		}
		if want := oracleTotal(frames, o); total != want {
			t.Fatalf("iter %d %+v\nframes %v\ntotal = %v, want %v", iter, o, frames, total, want)
		}
		again, _ := Build(frames, o)
		if !sameSteps(got, again) {
			t.Fatalf("iter %d: non-deterministic", iter)
		}
	}
}

func TestFlattenSegments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []Segment
		want []Segment
	}{
		{name: "empty", in: nil, want: nil},
		{
			name: "single range passes through",
			in:   []Segment{{From: sec, To: 4 * sec, Speed: 2, IdleLimit: sec, Hold: sec}},
			want: []Segment{{From: sec, To: 4 * sec, Speed: 2, IdleLimit: sec, Hold: sec}},
		},
		{
			name: "unsorted disjoint ranges are sorted",
			in:   []Segment{{From: 5 * sec, To: 6 * sec, Speed: 3}, {From: 0, To: sec, Speed: 2}},
			want: []Segment{{From: 0, To: sec, Speed: 2}, {From: 5 * sec, To: 6 * sec, Speed: 3}},
		},
		{
			name: "hidden inside a scene splits it and overrides it",
			in: []Segment{
				{From: 0, To: 10 * sec, Speed: 8, IdleLimit: 2 * sec, Hold: sec},
				{From: 4 * sec, To: 6 * sec, Skip: true},
			},
			want: []Segment{
				{From: 0, To: 4 * sec, Speed: 8, IdleLimit: 2 * sec},
				{From: 4 * sec, To: 6 * sec, Speed: 8, IdleLimit: 2 * sec, Skip: true},
				{From: 6 * sec, To: 10 * sec, Speed: 8, IdleLimit: 2 * sec, Hold: sec},
			},
		},
		{
			name: "input order does not matter",
			in: []Segment{
				{From: 4 * sec, To: 6 * sec, Skip: true},
				{From: 0, To: 10 * sec, Speed: 8, Hold: sec},
			},
			want: []Segment{
				{From: 0, To: 4 * sec, Speed: 8},
				{From: 4 * sec, To: 6 * sec, Speed: 8, Skip: true},
				{From: 6 * sec, To: 10 * sec, Speed: 8, Hold: sec},
			},
		},
		{
			name: "hidden at the end of a scene carries the scene hold",
			in: []Segment{
				{From: 0, To: 10 * sec, Hold: sec},
				{From: 7 * sec, To: 10 * sec, Skip: true},
			},
			want: []Segment{
				{From: 0, To: 7 * sec},
				{From: 7 * sec, To: 10 * sec, Skip: true, Hold: sec},
			},
		},
		{
			name: "hidden at the start of a scene",
			in: []Segment{
				{From: 0, To: 10 * sec, Speed: 2},
				{From: 0, To: 3 * sec, Skip: true},
			},
			want: []Segment{
				{From: 0, To: 3 * sec, Speed: 2, Skip: true},
				{From: 3 * sec, To: 10 * sec, Speed: 2},
			},
		},
		{
			name: "two hidden spans in one scene",
			in: []Segment{
				{From: 0, To: 10 * sec, Speed: 2},
				{From: 2 * sec, To: 3 * sec, Skip: true},
				{From: 6 * sec, To: 7 * sec, Skip: true},
			},
			want: []Segment{
				{From: 0, To: 2 * sec, Speed: 2},
				{From: 2 * sec, To: 3 * sec, Speed: 2, Skip: true},
				{From: 3 * sec, To: 6 * sec, Speed: 2},
				{From: 6 * sec, To: 7 * sec, Speed: 2, Skip: true},
				{From: 7 * sec, To: 10 * sec, Speed: 2},
			},
		},
		{
			name: "hidden between scenes keeps its own gap",
			in: []Segment{
				{From: 0, To: 2 * sec, Speed: 2},
				{From: 5 * sec, To: 7 * sec, Speed: 4},
				{From: 2 * sec, To: 3 * sec, Skip: true},
			},
			want: []Segment{
				{From: 0, To: 2 * sec, Speed: 2},
				{From: 2 * sec, To: 3 * sec, Skip: true},
				{From: 5 * sec, To: 7 * sec, Speed: 4},
			},
		},
		{
			name: "inner non-zero overrides, zero inherits",
			in: []Segment{
				{From: 0, To: 10 * sec, Speed: 2, IdleLimit: sec},
				{From: 3 * sec, To: 5 * sec, Speed: 8},
			},
			want: []Segment{
				{From: 0, To: 3 * sec, Speed: 2, IdleLimit: sec},
				{From: 3 * sec, To: 5 * sec, Speed: 8, IdleLimit: sec},
				{From: 5 * sec, To: 10 * sec, Speed: 2, IdleLimit: sec},
			},
		},
		{
			name: "equal ranges: the later one is the inner",
			in:   []Segment{{From: 0, To: 4 * sec, Speed: 2, IdleLimit: sec}, {From: 0, To: 4 * sec, Speed: 5}},
			want: []Segment{{From: 0, To: 4 * sec, Speed: 5, IdleLimit: sec}},
		},
		{
			name: "skip is sticky: an inner non-skip range cannot revive it",
			in:   []Segment{{From: 0, To: 10 * sec, Skip: true}, {From: 2 * sec, To: 3 * sec, Speed: 4}},
			want: []Segment{
				{From: 0, To: 2 * sec, Skip: true},
				{From: 2 * sec, To: 3 * sec, Speed: 4, Skip: true},
				{From: 3 * sec, To: 10 * sec, Skip: true},
			},
		},
		{
			name: "holds of ranges ending together add up",
			in: []Segment{
				{From: 0, To: 10 * sec, Hold: sec},
				{From: 6 * sec, To: 10 * sec, Hold: 2 * sec, Speed: 3},
			},
			want: []Segment{
				{From: 0, To: 6 * sec},
				{From: 6 * sec, To: 10 * sec, Speed: 3, Hold: 3 * sec},
			},
		},
		{
			name: "inner hold stays at the inner end",
			in: []Segment{
				{From: 0, To: 10 * sec},
				{From: 2 * sec, To: 4 * sec, Hold: sec, Speed: 3},
			},
			want: []Segment{
				{From: 0, To: 2 * sec},
				{From: 2 * sec, To: 4 * sec, Speed: 3, Hold: sec},
				{From: 4 * sec, To: 10 * sec},
			},
		},
		{
			name: "touching ranges with equal settings merge when no hold intervenes",
			in:   []Segment{{From: 0, To: 2 * sec, Speed: 2}, {From: 2 * sec, To: 5 * sec, Speed: 2, Hold: sec}},
			want: []Segment{{From: 0, To: 5 * sec, Speed: 2, Hold: sec}},
		},
		{
			name: "touching ranges stay apart when the first has a hold",
			in:   []Segment{{From: 0, To: 2 * sec, Speed: 2, Hold: sec}, {From: 2 * sec, To: 5 * sec, Speed: 2}},
			want: []Segment{{From: 0, To: 2 * sec, Speed: 2, Hold: sec}, {From: 2 * sec, To: 5 * sec, Speed: 2}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			before := slices.Clone(tt.in)
			got, err := FlattenSegments(tt.in)
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got  %v\nwant %v", got, tt.want)
			}
			if !slices.Equal(tt.in, before) {
				t.Error("input was modified")
			}
			if err := validateSegments(got); err != nil {
				t.Errorf("result is not a valid Options.Segments: %v", err)
			}
		})
	}
}

func TestFlattenSegmentsErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   []Segment
	}{
		{"partial overlap", []Segment{{From: 0, To: 5 * sec}, {From: 3 * sec, To: 8 * sec}}},
		{"partial overlap given inner first", []Segment{{From: 3 * sec, To: 8 * sec}, {From: 0, To: 5 * sec}}},
		{"From equals To", []Segment{{From: sec, To: sec}}},
		{"From after To", []Segment{{From: 2 * sec, To: sec}}},
		{"negative bound", []Segment{{From: -1, To: sec}}},
		{"negative speed", []Segment{{From: 0, To: sec, Speed: -2}}},
		{"NaN speed", []Segment{{From: 0, To: sec, Speed: math.NaN()}}},
		{"negative hold", []Segment{{From: 0, To: sec, Hold: -1}}},
		{"negative idle", []Segment{{From: 0, To: sec, IdleLimit: -1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, err := FlattenSegments(tt.in); err == nil {
				t.Errorf("got %v, want error", got)
			}
		})
	}
}

// randomNested appends a chain of properly nested ranges inside [from, to).
func randomNested(r *rand.Rand, from, to time.Duration, depth int, out *[]Segment) {
	if depth == 0 || to-from < 4*ms {
		return
	}
	a := from + time.Duration(r.Int64N(int64(to-from)-1))
	b := a + 1 + time.Duration(r.Int64N(int64(to-a)))
	s := Segment{From: a, To: b}
	switch r.IntN(4) {
	case 0:
		s.Skip = true
	case 1:
		s.Speed = []float64{0.5, 2, 8}[r.IntN(3)]
	case 2:
		s.IdleLimit = []time.Duration{sec, 3 * sec}[r.IntN(2)]
	}
	*out = append(*out, s)
	randomNested(r, a, b, depth-1, out)
}

func TestFlattenSegmentsRandomNesting(t *testing.T) {
	t.Parallel()
	r := rand.New(rand.NewPCG(5, 6))
	for iter := range 300 {
		var in []Segment
		var at time.Duration
		for range 1 + r.IntN(3) { // disjoint top-level scenes
			end := at + time.Duration(10+r.IntN(20000))*ms
			top := Segment{From: at, To: end, Speed: []float64{0, 2}[r.IntN(2)], Hold: time.Duration(r.IntN(3)) * sec}
			in = append(in, top)
			randomNested(r, top.From, top.To, 3, &in)
			at = end + time.Duration(r.IntN(3))*sec
		}
		r.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
		// The expected chain uses the same stable ordering as the
		// function, so equal ranges shuffled either way agree.
		got, err := FlattenSegments(in)
		if err != nil {
			t.Fatalf("iter %d: %v\nin %v", iter, err, in)
		}
		if err := validateSegments(got); err != nil {
			t.Fatalf("iter %d: invalid output: %v", iter, err)
		}
		ordered := slices.Clone(in)
		slices.SortStableFunc(ordered, func(a, b Segment) int {
			if a.From != b.From {
				return int(a.From - b.From)
			}
			return int(b.To - a.To)
		})
		for range 200 {
			tm := time.Duration(r.Int64N(int64(at) + int64(sec)))
			// Expected settings at tm: fold the covering chain outer to inner.
			var want Segment
			covered := false
			for _, s := range ordered {
				if s.From <= tm && tm < s.To {
					covered = true
					if s.Speed != 0 {
						want.Speed = s.Speed
					}
					if s.IdleLimit != 0 {
						want.IdleLimit = s.IdleLimit
					}
					want.Skip = want.Skip || s.Skip
				}
			}
			i := segmentIndex(got, tm)
			if (i >= 0) != covered {
				t.Fatalf("iter %d t=%v: covered = %v, want %v", iter, tm, i >= 0, covered)
			}
			if !covered {
				continue
			}
			g := got[i]
			if g.Speed != want.Speed || g.IdleLimit != want.IdleLimit || g.Skip != want.Skip {
				t.Fatalf("iter %d t=%v: got %+v, want %+v", iter, tm, g, want)
			}
		}
		// Holds are conserved: every range's hold appears exactly once.
		var inHold, outHold time.Duration
		for _, s := range in {
			inHold += s.Hold
		}
		for _, s := range got {
			outHold += s.Hold
		}
		if inHold != outHold {
			t.Fatalf("iter %d: hold in %v out %v", iter, inHold, outHold)
		}
	}
}

// TestFlattenedSegmentsBuild runs a scene with a hidden span end to end.
func TestFlattenedSegmentsBuild(t *testing.T) {
	t.Parallel()
	segs, err := FlattenSegments([]Segment{
		{From: 0, To: 10 * sec, Speed: 2, Hold: sec},
		{From: 2 * sec, To: 6 * sec, Skip: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// c is the frame forced at the end of the hidden span.
	frames := mk(fr{"a", 0}, fr{"h", 3 * sec}, fr{"c", 6 * sec}, fr{"d", 8 * sec}, fr{"e", 12 * sec})
	got, err := Build(frames, Options{Hold: 500 * ms, Segments: segs})
	if err != nil {
		t.Fatal(err)
	}
	// a: 2s visible at 2x = 1s. h dropped. c: 2s at 2x = 1s. d: 2s at 2x
	// plus 2s at 1x past the scene end = 3s, plus the scene's 1s hold; e
	// gets Options.Hold.
	want := []want{{"a", sec}, {"c", sec}, {"d", 4 * sec}, {"e", 500 * ms}}
	if !slices.Equal(describe(got), want) {
		t.Fatalf("got %v, want %v", describe(got), want)
	}
}

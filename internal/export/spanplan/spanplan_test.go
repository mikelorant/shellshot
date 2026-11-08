package spanplan

import (
	"reflect"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/recording/bin/event"
	"github.com/mikelorant/shellshot/internal/recording/spans"
)

const sec = time.Second

func scene(id uint64, from, to time.Duration, h event.SpanHints) spans.Span {
	return spans.Span{ID: id, Type: event.SpanScene, Name: "s", Hints: h, Start: from, End: to}
}

func hidden(id, parent uint64, from, to time.Duration) spans.Span {
	s := spans.Span{ID: id, Type: event.SpanHidden, Parent: parent, Start: from, End: to}
	if parent != 0 {
		s.Depth = 1
	}
	return s
}

var allHints = event.SpanHints{Speed: 8000, Idle: 500 * time.Millisecond, Hold: sec}

func TestBuildNoSpansIsEmpty(t *testing.T) {
	t.Parallel()
	for _, in := range [][]spans.Span{nil, {}} {
		p, err := Build(in, Options{})
		if err != nil {
			t.Fatal(err)
		}
		if p.Segments != nil || p.Boundaries != nil {
			t.Errorf("Build(%v) = %+v, want the zero Plan", in, p)
		}
	}
}

func TestBuildSceneHints(t *testing.T) {
	t.Parallel()
	p, err := Build([]spans.Span{scene(1, 2*sec, 5*sec, allHints)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{{From: 2 * sec, To: 5 * sec, Speed: 8, IdleLimit: 500 * time.Millisecond, Hold: sec}}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
	if wantB := []time.Duration{2 * sec, 5 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

func TestBuildSpeedMilliX(t *testing.T) {
	t.Parallel()
	p, err := Build([]spans.Span{scene(1, 0, sec, event.SpanHints{Speed: 1500})}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Segments) != 1 || p.Segments[0].Speed != 1.5 {
		t.Errorf("Segments = %+v, want one segment with speed 1.5", p.Segments)
	}
}

func TestBuildHiddenIsSkipWithBoundaries(t *testing.T) {
	t.Parallel()
	p, err := Build([]spans.Span{hidden(1, 0, sec, 3*sec)}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{{From: sec, To: 3 * sec, Skip: true}}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
	if wantB := []time.Duration{sec, 3 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

func TestBuildHiddenInsideSceneIsFlattened(t *testing.T) {
	t.Parallel()
	in := []spans.Span{
		scene(1, 0, 10*sec, event.SpanHints{Speed: 4000, Hold: sec}),
		hidden(2, 1, 3*sec, 5*sec),
	}
	p, err := Build(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{
		{From: 0, To: 3 * sec, Speed: 4},
		{From: 3 * sec, To: 5 * sec, Speed: 4, Skip: true},
		{From: 5 * sec, To: 10 * sec, Speed: 4, Hold: sec},
	}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
	if wantB := []time.Duration{0, 3 * sec, 5 * sec, 10 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

// A hidden span that ends together with its scene (the scene's end closed
// it) shares the end boundary.
func TestBuildSharedBoundariesAreListedOnce(t *testing.T) {
	t.Parallel()
	in := []spans.Span{
		scene(1, sec, 4*sec, event.SpanHints{Speed: 2000}),
		hidden(2, 1, 2*sec, 4*sec),
	}
	p, err := Build(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if wantB := []time.Duration{sec, 2 * sec, 4 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

func TestBuildExplicitFlagsDropTheirHint(t *testing.T) {
	t.Parallel()
	in := []spans.Span{scene(1, 0, 4*sec, allHints)}
	tests := []struct {
		name string
		opts Options
		want timeline.Segment
	}{
		{"speed", Options{FlagSpeed: true}, timeline.Segment{To: 4 * sec, IdleLimit: 500 * time.Millisecond, Hold: sec}},
		{"idle", Options{FlagIdleLimit: true}, timeline.Segment{To: 4 * sec, Speed: 8, Hold: sec}},
		{"hold", Options{FlagHold: true}, timeline.Segment{To: 4 * sec, Speed: 8, IdleLimit: 500 * time.Millisecond}},
	}
	for _, tt := range tests {
		p, err := Build(in, tt.opts)
		if err != nil {
			t.Fatal(err)
		}
		if want := []timeline.Segment{tt.want}; !reflect.DeepEqual(p.Segments, want) {
			t.Errorf("%s: Segments = %+v, want %+v", tt.name, p.Segments, want)
		}
	}
}

// With every flag explicit a scene has nothing left to say, and neither the
// segment nor the forced frames are planned. A hidden span still is.
func TestBuildSceneWithNoEffectIsDropped(t *testing.T) {
	t.Parallel()
	in := []spans.Span{scene(1, 0, 4*sec, allHints), hidden(2, 1, sec, 2*sec)}
	p, err := Build(in, Options{FlagSpeed: true, FlagIdleLimit: true, FlagHold: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{{From: sec, To: 2 * sec, Skip: true}}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
	if wantB := []time.Duration{sec, 2 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

func TestBuildNoHintsKeepsHidden(t *testing.T) {
	t.Parallel()
	in := []spans.Span{scene(1, 0, 6*sec, allHints), hidden(2, 1, 2*sec, 3*sec)}
	p, err := Build(in, Options{IgnoreHints: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{{From: 2 * sec, To: 3 * sec, Skip: true}}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
}

func TestBuildShowHiddenKeepsHints(t *testing.T) {
	t.Parallel()
	in := []spans.Span{scene(1, 0, 6*sec, event.SpanHints{Speed: 2000}), hidden(2, 1, 2*sec, 3*sec)}
	p, err := Build(in, Options{ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{{From: 0, To: 6 * sec, Speed: 2}}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
	if wantB := []time.Duration{0, 6 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

func TestBuildNoHintsAndShowHiddenPlanNothing(t *testing.T) {
	t.Parallel()
	in := []spans.Span{scene(1, 0, 6*sec, allHints), hidden(2, 1, 2*sec, 3*sec)}
	p, err := Build(in, Options{IgnoreHints: true, ShowHidden: true})
	if err != nil {
		t.Fatal(err)
	}
	if p.Segments != nil || p.Boundaries != nil {
		t.Errorf("plan = %+v, want the zero Plan", p)
	}
}

func TestBuildSkipsEmptySpansAndKeepsOpenOnes(t *testing.T) {
	t.Parallel()
	open := scene(2, 3*sec, 5*sec, event.SpanHints{Speed: 2000})
	open.Open = true
	in := []spans.Span{scene(1, sec, sec, allHints), open}
	p, err := Build(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{{From: 3 * sec, To: 5 * sec, Speed: 2}}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
	if wantB := []time.Duration{3 * sec, 5 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

func TestBuildTwoScenesAndHiddenBetween(t *testing.T) {
	t.Parallel()
	in := []spans.Span{
		scene(1, 0, 2*sec, event.SpanHints{Speed: 2000}),
		hidden(2, 0, 2*sec, 3*sec),
		scene(3, 3*sec, 5*sec, event.SpanHints{Speed: 4000}),
	}
	p, err := Build(in, Options{})
	if err != nil {
		t.Fatal(err)
	}
	want := []timeline.Segment{
		{From: 0, To: 2 * sec, Speed: 2},
		{From: 2 * sec, To: 3 * sec, Skip: true},
		{From: 3 * sec, To: 5 * sec, Speed: 4},
	}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("Segments = %+v, want %+v", p.Segments, want)
	}
	if wantB := []time.Duration{0, 2 * sec, 3 * sec, 5 * sec}; !reflect.DeepEqual(p.Boundaries, wantB) {
		t.Errorf("Boundaries = %v, want %v", p.Boundaries, wantB)
	}
}

func TestBuildSegmentsTable(t *testing.T) {
	t.Parallel()
	openHidden := hidden(1, 0, 4*sec, 6*sec)
	openHidden.Open = true
	hintScene := scene(1, 2*sec, 5*sec, allHints)
	tests := []struct {
		name  string
		found []spans.Span
		opts  Options
		want  []timeline.Segment
	}{
		{"no spans", nil, Options{}, nil},
		{"hidden at the start", []spans.Span{hidden(1, 0, 0, 2*sec)}, Options{},
			[]timeline.Segment{{From: 0, To: 2 * sec, Skip: true}}},
		{"hidden in the middle", []spans.Span{hidden(1, 0, 2*sec, 3*sec)}, Options{},
			[]timeline.Segment{{From: 2 * sec, To: 3 * sec, Skip: true}}},
		{"unclosed hidden runs to the end of the recording", []spans.Span{openHidden}, Options{},
			[]timeline.Segment{{From: 4 * sec, To: 6 * sec, Skip: true}}},
		{"scene hints", []spans.Span{hintScene}, Options{},
			[]timeline.Segment{{From: 2 * sec, To: 5 * sec, Speed: 8, IdleLimit: 500 * time.Millisecond, Hold: sec}}},
		{"no hints ignores the scene", []spans.Span{hintScene}, Options{IgnoreHints: true}, nil},
		{"no hints still cuts hidden", []spans.Span{hintScene, hidden(2, 1, 3*sec, 4*sec)}, Options{IgnoreHints: true},
			[]timeline.Segment{{From: 3 * sec, To: 4 * sec, Skip: true}}},
		{"speed flag replaces the speed hint", []spans.Span{hintScene}, Options{FlagSpeed: true},
			[]timeline.Segment{{From: 2 * sec, To: 5 * sec, IdleLimit: 500 * time.Millisecond, Hold: sec}}},
		{"all flags replace the hints", []spans.Span{hintScene}, Options{FlagSpeed: true, FlagIdleLimit: true, FlagHold: true}, nil},
		{"show hidden plays the hidden span", []spans.Span{hidden(1, 0, 2*sec, 3*sec)}, Options{ShowHidden: true}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p, err := Build(tt.found, tt.opts)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(p.Segments, tt.want) {
				t.Errorf("Segments = %+v, want %+v", p.Segments, tt.want)
			}
		})
	}
}

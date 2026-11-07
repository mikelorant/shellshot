package pipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

func TestReadStepsAppliesTimelineAndMergesEqualFrames(t *testing.T) {
	t.Parallel()
	src := &sliceSource{frames: []snapshot.Frame{
		frame(0, "a"), frame(100*time.Millisecond, "b"), frame(300*time.Millisecond, "b"), frame(400*time.Millisecond, "c"),
	}}
	steps, err := ReadSteps(context.Background(), src, timeline.Options{Hold: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	want := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, time.Second}
	if len(steps) != len(want) {
		t.Fatalf("got %d steps, want %d", len(steps), len(want))
	}
	for i, d := range want {
		if steps[i].Delay != d {
			t.Errorf("step %d delay = %v, want %v", i, steps[i].Delay, d)
		}
	}
}

func TestReadStepsErrors(t *testing.T) {
	t.Parallel()
	boom := errors.New("boom")
	for _, tc := range []struct {
		name string
		src  Source
		o    timeline.Options
		want string
	}{
		{"empty", &sliceSource{}, timeline.Options{}, "no frames"},
		{"source error", &sliceSource{frames: []snapshot.Frame{frame(0, "a")}, err: boom}, timeline.Options{}, "read frame 1"},
		{"bad speed", &sliceSource{frames: []snapshot.Frame{frame(0, "a")}}, timeline.Options{Speed: -1}, "speed"},
	} {
		if _, err := ReadSteps(context.Background(), tc.src, tc.o); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want it to contain %q", tc.name, err, tc.want)
		}
	}
}

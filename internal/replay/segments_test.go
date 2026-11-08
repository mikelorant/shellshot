package replay

import (
	"testing"
	"time"

	"github.com/mikelorant/shellshot/internal/export/timeline"
	"github.com/mikelorant/shellshot/internal/snapshot"
)

func framesAt(secs ...int) []snapshot.Frame {
	var fs []snapshot.Frame
	for _, s := range secs {
		fs = append(fs, snapshot.Frame{At: time.Duration(s) * time.Second})
	}
	return fs
}

func TestCountIdleCapped(t *testing.T) {
	t.Parallel()
	const sec = time.Second
	tests := []struct {
		name   string
		frames []snapshot.Frame
		segs   []timeline.Segment
		limit  time.Duration
		want   int
	}{
		{"no limit", framesAt(0, 10), nil, 0, 0},
		{"no segments counts long gaps", framesAt(0, 1, 6, 9), nil, 2 * sec, 2},
		{"a gap equal to the limit is not capped", framesAt(0, 2), nil, 2 * sec, 0},
		{"a hidden span is not a pause", framesAt(0, 1, 10), []timeline.Segment{{From: sec, To: 10 * sec, Skip: true}}, 2 * sec, 0},
		{"a pause beside a hidden span still counts", framesAt(0, 8), []timeline.Segment{{From: 5 * sec, To: 8 * sec, Skip: true}}, 2 * sec, 1},
		{"a scene limit replaces the global one", framesAt(0, 3, 9), []timeline.Segment{{From: 0, To: 20 * sec, IdleLimit: 5 * sec}}, 2 * sec, 1},
		{"a scene limit applies where the pause starts", framesAt(0, 3, 9), []timeline.Segment{{From: 3 * sec, To: 20 * sec, IdleLimit: 5 * sec}}, 2 * sec, 2},
	}
	for _, tt := range tests {
		if got := countIdleCapped(tt.frames, tt.segs, tt.limit); got != tt.want {
			t.Errorf("%s: countIdleCapped() = %d, want %d", tt.name, got, tt.want)
		}
	}
}

func TestHiddenCut(t *testing.T) {
	t.Parallel()
	const sec = time.Second
	segs := []timeline.Segment{
		{From: sec, To: 3 * sec, Skip: true},
		{From: 3 * sec, To: 4 * sec, Speed: 2},
		{From: 8 * sec, To: 12 * sec, Skip: true},
		{From: 20 * sec, To: 22 * sec, Skip: true},
	}
	count, total := hiddenCut(segs, 10*sec)
	if count != 2 || total != 4*sec { // [1s,3s) and [8s,10s): the last starts after the end
		t.Errorf("hiddenCut() = %d, %v, want 2, 4s", count, total)
	}
	if count, total := hiddenCut(nil, 10*sec); count != 0 || total != 0 {
		t.Errorf("hiddenCut(nil) = %d, %v, want 0, 0", count, total)
	}
}

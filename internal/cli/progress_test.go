package cli

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestShowProgress(t *testing.T) {
	tests := []struct {
		name                    string
		stderrIsTerminal, watch bool
		want                    bool
	}{
		{"terminal", true, false, true},
		{"terminal with --watch", true, true, false},
		{"stderr is a file or pipe", false, false, false},
		{"file with --watch", false, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := showProgress(tt.stderrIsTerminal, tt.watch); got != tt.want {
				t.Errorf("showProgress(%v, %v) = %v, want %v", tt.stderrIsTerminal, tt.watch, got, tt.want)
			}
		})
	}
}

func TestRenderProgressBarFill(t *testing.T) {
	tests := []struct {
		name        string
		done, total int
		label       string
		elapsed     time.Duration
		want        string
	}{
		{"nothing done", 0, 10, "", 0, "[" + strings.Repeat(" ", 20) + "]   0/10  0s"},
		{"part done with a label", 7, 23, "create", 12 * time.Second,
			"[=====>              ]   7/23  create  12s"},
		{"all done", 23, 23, "", 65 * time.Second, "[" + strings.Repeat("=", 20) + "]  23/23  1m05s"},
		{"rounds down, never full before the end", 99, 100, "", 0,
			"[" + strings.Repeat("=", 18) + ">" + " ]   99/100  0s"},
		{"one in a hundred shows no fill yet", 1, 100, "", 0, "[" + strings.Repeat(" ", 20) + "]    1/100  0s"},
		{"done above total is full", 30, 23, "", 0, "[" + strings.Repeat("=", 20) + "]  30/23  0s"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := renderProgress(80, tt.done, tt.total, tt.label, tt.elapsed); got != tt.want {
				t.Errorf("renderProgress =\n%q\nwant\n%q", got, tt.want)
			}
		})
	}
}

func TestRenderProgressNeverExceedsTheWidth(t *testing.T) {
	for _, label := range []string{"", "create", "a-very-long-scene-name-that-does-not-fit-anywhere-near"} {
		for width := 0; width <= 210; width++ {
			line := renderProgress(width, 7, 23, label, 3725*time.Second)
			if limit := max(width-1, 0); len(line) > limit {
				t.Fatalf("width %d label %q: %d columns %q", width, label, len(line), line)
			}
			for _, r := range line {
				if r < 0x20 || r > 0x7e {
					t.Fatalf("width %d: %q has non-printable-ASCII %q", width, line, r)
				}
			}
		}
	}
}

func TestRenderProgressAtTwentyColumnsKeepsTheCountAndTime(t *testing.T) {
	if got, want := renderProgress(20, 7, 23, "create", 12*time.Second), "7/23  12s"; got != want {
		t.Errorf("renderProgress = %q, want %q", got, want)
	}
}

func TestRenderProgressShowsTheBarAtCommonWidths(t *testing.T) {
	for _, width := range []int{40, 80, 200} {
		line := renderProgress(width, 7, 23, "create", 12*time.Second)
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, "]") || !strings.Contains(line, "7/23") {
			t.Errorf("width %d: %q lacks the bar or the count", width, line)
		}
	}
	if line := renderProgress(200, 7, 23, "create", 0); strings.Count(line, "=")+strings.Count(line, ">")+strings.Count(line, " ") > 100 {
		t.Errorf("bar grows with the terminal: %q", line)
	}
}

func TestRenderProgressShrinksTheBarThenDropsTheLabel(t *testing.T) {
	const label = "create"
	var sawShrunkWithLabel, sawBarWithoutLabel bool
	labelGone := false
	for width := 80; width >= 0; width-- {
		line := renderProgress(width, 7, 23, label, 12*time.Second)
		hasLabel := strings.Contains(line, label)
		hasBar := strings.HasPrefix(line, "[")
		if labelGone && hasLabel {
			t.Fatalf("width %d: label is back after it was dropped: %q", width, line)
		}
		if hasLabel && !hasBar {
			t.Fatalf("width %d: bar is gone while the label is shown: %q", width, line)
		}
		if hasLabel && hasBar && strings.Index(line, "]")-1 < maxBarCells {
			sawShrunkWithLabel = true
		}
		if !hasLabel && hasBar {
			sawBarWithoutLabel = true
		}
		labelGone = !hasLabel
	}
	if !sawShrunkWithLabel {
		t.Error("the bar never shrank while the label was still shown")
	}
	if !sawBarWithoutLabel {
		t.Error("the label was never dropped while the bar was still shown")
	}
}

func TestRenderProgressKeepsTheLabelAsPlainASCII(t *testing.T) {
	got := renderProgress(80, 1, 2, "a\x1b[2Jb\u00e9\n", 0)
	if strings.ContainsAny(got, "\x1b\n\r") || !strings.Contains(got, "a?[2Jb??") {
		t.Errorf("label not made plain: %q", got)
	}
}

func TestFormatElapsed(t *testing.T) {
	tests := map[time.Duration]string{
		-time.Second:                          "0s",
		0:                                     "0s",
		999 * time.Millisecond:                "0s",
		12*time.Second + 900*time.Millisecond: "12s",
		59 * time.Second:                      "59s",
		60 * time.Second:                      "1m00s",
		65 * time.Second:                      "1m05s",
		3599 * time.Second:                    "59m59s",
		3723 * time.Second:                    "1h02m03s",
	}
	for d, want := range tests {
		if got := formatElapsed(d); got != want {
			t.Errorf("formatElapsed(%v) = %q, want %q", d, got, want)
		}
	}
}

// syncBuffer is a strings.Builder that the redraw goroutine and the test can
// both use.
type syncBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// testProgress is a progress on a buffer, 40 columns wide, whose clock and
// redraw ticks the test controls.
func testProgress() (p *progress, out *syncBuffer, clock *atomic.Int64, ticks chan time.Time) {
	out = &syncBuffer{}
	clock = &atomic.Int64{}
	ticks = make(chan time.Time)
	p = newProgress(progressConfig{
		Out:   out,
		Width: func() int { return 40 },
		Now:   func() time.Time { return time.Unix(1000, 0).Add(time.Duration(clock.Load())) },
		Ticks: ticks,
	})
	return p, out, clock, ticks
}

func TestProgressUpdateRedrawsTheLineInPlace(t *testing.T) {
	p, out, _, _ := testProgress()
	p.Update(0, 4, "")
	p.Update(2, 4, "demo")
	p.Close()
	want := "\r" + renderProgress(40, 0, 4, "", 0) + "\x1b[K" +
		"\r" + renderProgress(40, 2, 4, "demo", 0) + "\x1b[K" +
		"\r\x1b[K"
	if got := out.String(); got != want {
		t.Errorf("output =\n%q\nwant\n%q", got, want)
	}
}

func TestProgressUpdateWithTheSameValuesDoesNotRedraw(t *testing.T) {
	p, out, _, _ := testProgress()
	p.Update(1, 4, "demo")
	p.Update(1, 4, "demo")
	if got, want := out.String(), "\r"+renderProgress(40, 1, 4, "demo", 0)+"\x1b[K"; got != want {
		t.Errorf("output = %q, want one frame %q", got, want)
	}
	p.Close()
}

func TestProgressCloseClearsOnlyWhatWasDrawn(t *testing.T) {
	p, out, _, _ := testProgress()
	p.Update(0, 0, "") // nothing to count: nothing drawn
	p.Close()
	if got := out.String(); got != "" {
		t.Errorf("output = %q, want none", got)
	}
}

func TestProgressAfterCloseDoesNothing(t *testing.T) {
	p, out, _, ticks := testProgress()
	p.Update(1, 2, "")
	p.Close()
	before := out.String()
	p.Update(2, 2, "late")
	p.Close()
	select {
	case ticks <- time.Time{}:
		t.Error("the redraw goroutine still receives ticks after Close")
	default:
	}
	if got := out.String(); got != before {
		t.Errorf("output changed after Close: %q -> %q", before, got)
	}
}

func TestProgressRedrawsWithTheElapsedTimeOnATick(t *testing.T) {
	p, out, clock, ticks := testProgress()
	p.Update(1, 4, "demo")
	clock.Store(int64(5 * time.Second))
	// The second send returns only when the goroutine has finished the
	// redraw for the first.
	ticks <- time.Time{}
	ticks <- time.Time{}
	p.Close()
	want := renderProgress(40, 1, 4, "demo", 5*time.Second)
	if got := out.String(); !strings.Contains(got, "\r"+want+"\x1b[K") {
		t.Errorf("output %q lacks the redraw %q", got, want)
	}
}

func TestProgressTickBeforeAnyUpdateDrawsNothing(t *testing.T) {
	p, out, _, ticks := testProgress()
	ticks <- time.Time{}
	ticks <- time.Time{}
	p.Close()
	if got := out.String(); got != "" {
		t.Errorf("output = %q, want none", got)
	}
}

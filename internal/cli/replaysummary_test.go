package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/replay"
)

func TestWriteReplaySummary(t *testing.T) {
	s := time.Second
	finished := replaySummary{
		stats:     replay.Stats{Steps: 74, Shown: 74, Played: 11900 * time.Millisecond, Planned: 11900 * time.Millisecond, Recorded: 27600 * time.Millisecond},
		speed:     1,
		idleLimit: 2 * s,
		path:      "pi.shot",
		fileBytes: 6656,
	}
	with := func(edit func(*replaySummary)) replaySummary {
		c := finished
		edit(&c)
		return c
	}
	tests := []struct {
		name string
		in   replaySummary
		want string
	}{
		{
			name: "finished",
			in:   finished,
			want: `replay finished
frames       74 shown (of 74)
time         11.9s played, 27.6s recorded (15.7s saved; speed 1x)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "interrupted",
			in: with(func(c *replaySummary) {
				c.stats = replay.Stats{Steps: 74, Shown: 30, Played: 12200 * time.Millisecond, Planned: 27400 * time.Millisecond, Recorded: 27600 * time.Millisecond, Interrupted: true}
			}),
			want: `replay stopped at 0:12 of 0:27 (interrupted)
frames       30 shown (of 74)
time         12.2s played, 27.6s recorded (0.2s saved; speed 1x)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "interrupted before the first frame",
			in: with(func(c *replaySummary) {
				c.stats = replay.Stats{Interrupted: true}
			}),
			want: `replay stopped before the first frame (interrupted)
frames       0 shown (of 0)
time         0.0s played, 0.0s recorded (speed 1x)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "truncated",
			in:   with(func(c *replaySummary) { c.incomplete = "truncated (4096 bytes valid)" }),
			want: `replay ended early: recording is truncated (4096 bytes valid)
frames       74 shown (of 74)
time         11.9s played, 27.6s recorded (15.7s saved; speed 1x)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "incomplete",
			in:   with(func(c *replaySummary) { c.incomplete = "incomplete (no exit event)" }),
			want: `replay ended early: recording is incomplete (no exit event)
frames       74 shown (of 74)
time         11.9s played, 27.6s recorded (15.7s saved; speed 1x)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "failed",
			in: with(func(c *replaySummary) {
				c.stats.Shown = 12
				c.err = errors.New("replay: write frame: broken pipe")
			}),
			want: `replay failed: write frame: broken pipe
frames       12 shown (of 74)
time         11.9s played, 27.6s recorded (15.7s saved; speed 1x)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "cropped",
			in: with(func(c *replaySummary) {
				c.stats.Cropped = true
				c.recCols, c.recRows, c.cols, c.rows = 182, 79, 120, 40
			}),
			want: `replay finished
frames       74 shown (of 74)
time         11.9s played, 27.6s recorded (15.7s saved; speed 1x)
screen       182x79 recorded, shown at 120x40 (cropped)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "idle capped and sped up",
			in: with(func(c *replaySummary) {
				c.speed = 2.5
				c.stats.IdleCapped = 3
				c.stats.Planned = 5 * s
				c.stats.Played = 5 * s
			}),
			want: `replay finished
frames       74 shown (of 74)
time         5.0s played, 27.6s recorded (22.6s saved; 3 pauses capped at 2s; speed 2.5x)
file         pi.shot, 6.5 KB
`,
		},
		{
			name: "one capped pause, nothing saved, file size unknown",
			in: with(func(c *replaySummary) {
				c.stats.IdleCapped = 1
				c.stats.Planned = c.stats.Recorded
				c.fileBytes = -1
			}),
			want: `replay finished
frames       74 shown (of 74)
time         11.9s played, 27.6s recorded (1 pause capped at 2s; speed 1x)
file         pi.shot
`,
		},
		{
			name: "zero frames",
			in:   with(func(c *replaySummary) { c.stats = replay.Stats{} }),
			want: `replay finished
frames       0 shown (of 0)
time         0.0s played, 0.0s recorded (speed 1x)
file         pi.shot, 6.5 KB
`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			writeReplaySummary(&out, tt.in)
			if out.String() != tt.want {
				t.Errorf("summary =\n%s\nwant\n%s", out.String(), tt.want)
			}
		})
	}
}

// The summary goes to stderr and only when the replay draws in place.
func TestReplaySummaryOnStderr(t *testing.T) {
	t.Parallel()
	data := simpleRecording(t)
	path := writeFile(t, "demo.shot", data)
	tests := []struct {
		name    string
		args    []string
		summary bool
	}{
		{"in place", []string{"--clear"}, true},
		{"piped", nil, false},
		{"in place, quiet", []string{"--clear", "-q"}, false},
		{"in place, --quiet", []string{"--clear", "--quiet"}, false},
		{"piped, quiet", []string{"-q"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runReplayCmd(t, append([]string{path, "--speed", "1000"}, tt.args...)...)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr)
			}
			if strings.Contains(stdout, "replay finished") {
				t.Errorf("stdout = %q, want no summary on stdout", stdout)
			}
			if !tt.summary {
				if stderr != "" {
					t.Errorf("stderr = %q, want empty", stderr)
				}
				return
			}
			for _, want := range []string{"replay finished\n", "frames       2 shown (of 2)\n", "recorded (", "speed 1000x)\n", "file         " + path + ", 0."} {
				if !strings.Contains(stderr, want) {
					t.Errorf("stderr = %q, want it to contain %q", stderr, want)
				}
			}
			// The terminal is restored and the summary starts on its own line.
			if !strings.HasSuffix(stdout, "\x1b[?2026l\x1b[?25h\x1b[0m\x1b[2;1H\r\n") {
				t.Errorf("stdout ends with %q, want the cursor restore below the frame", stdout[max(0, len(stdout)-40):])
			}
		})
	}
}

// A truncated or killed recording is described in the summary instead of the
// separate warning.
func TestReplaySummaryIncompleteRecording(t *testing.T) {
	t.Parallel()
	data, firstEnd := twoBlockRecording(t)
	torn := writeFile(t, "torn.shot", data[:len(data)-3])
	killed := writeFile(t, "killed.shot", interruptedRecording(t))
	for path, want := range map[string]string{
		torn:   "replay ended early: recording is truncated (" + itoa(firstEnd) + " bytes valid)\n",
		killed: "replay ended early: recording is incomplete (no exit event)\n",
	} {
		code, _, stderr := runReplayCmd(t, path, "--speed", "1000", "--clear")
		if code != 0 || !strings.HasPrefix(stderr, want) {
			t.Errorf("exit %d, stderr = %q, want it to start with %q", code, stderr, want)
		}
		if strings.Contains(stderr, "shellshot: recording is") {
			t.Errorf("stderr = %q, want the note only once, in the summary", stderr)
		}
	}
}

// Frames cropped to the terminal are reported with both sizes.
func TestReplaySummaryCropped(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "demo.shot", simpleRecording(t)) // 10x2
	var out, errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	if err := runReplay(cmd, path, 60, replay.Options{Speed: 1000, Clear: true, Cols: 4, Rows: 1}, false); err != nil {
		t.Fatalf("runReplay() error = %v", err)
	}
	if want := "screen       10x2 recorded, shown at 4x1 (cropped)\n"; !strings.Contains(errBuf.String(), want) {
		t.Errorf("stderr = %q, want it to contain %q", errBuf.String(), want)
	}
}

// Ctrl-C ends a replay normally: the summary says where it stopped, the
// terminal is restored, the exit code is 130 and no error line is printed.
func TestReplayCancelIsNormalExit(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "idle.shot", idleGapRecording(t))
	for _, args := range [][]string{{"--clear"}, {"--clear", "-q"}, nil} {
		ctx, cancel := context.WithCancel(t.Context())
		time.AfterFunc(150*time.Millisecond, cancel)
		var out, errBuf bytes.Buffer
		// A 1h idle limit keeps the replay waiting in the 5 minute gap.
		code := Run(ctx, append([]string{"replay", path, "--idle-limit", "1h"}, args...), &out, &errBuf)
		cancel()
		if code != 130 {
			t.Errorf("%v: exit code = %d, want 130", args, code)
		}
		if strings.Contains(errBuf.String(), "context canceled") || strings.Contains(errBuf.String(), "shellshot:") {
			t.Errorf("%v: stderr = %q, want no error line", args, errBuf.String())
		}
		inPlace := len(args) > 0
		if inPlace != strings.Contains(out.String(), "\x1b[?25h") {
			t.Errorf("%v: stdout = %q, cursor restore present = %v", args, out.String(), !inPlace)
		}
		wantSummary := len(args) == 1
		if got := strings.Contains(errBuf.String(), "(interrupted)\n"); got != wantSummary {
			t.Errorf("%v: stderr = %q, summary present = %v, want %v", args, errBuf.String(), got, wantSummary)
		}
		if wantSummary && !strings.HasPrefix(errBuf.String(), "replay stopped at 0:00 of 5:00 (interrupted)\nframes       1 shown (of 2)\n") {
			t.Errorf("%v: stderr = %q", args, errBuf.String())
		}
	}
}

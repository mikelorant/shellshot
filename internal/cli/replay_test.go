package cli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/mikelorant/shellshot/internal/recording/bin"
	"github.com/mikelorant/shellshot/internal/recording/bin/head"
	"github.com/mikelorant/shellshot/internal/replay"
)

func runReplayCmd(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(context.Background(), append([]string{"replay"}, args...), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// frameStart opens each frame drawn with --clear: a synchronized update.
const frameStart = "\x1b[?2026h"

func TestReplayColorFlag(t *testing.T) {
	path := writeFile(t, "styled.shot", styledRecording(t))
	t.Setenv("NO_COLOR", "")
	tests := []struct {
		color     string
		wantColor bool
	}{
		{"always", true},
		{"never", false},
		{"auto", false}, // stdout is a buffer, not a terminal
	}
	for _, tt := range tests {
		t.Run(tt.color, func(t *testing.T) {
			code, stdout, stderr := runReplayCmd(t, path, "--speed", "1000", "--color", tt.color)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr)
			}
			if got := strings.Contains(stdout, "\x1b[31mred\x1b[0m ok"); got != tt.wantColor {
				t.Errorf("stdout has the red span = %v, want %v: %q", got, tt.wantColor, stdout)
			}
			if !tt.wantColor && strings.Contains(stdout, "\x1b") {
				t.Errorf("stdout = %q, want no escape sequences", stdout)
			}
			if !strings.Contains(stdout, "red ok") && !tt.wantColor {
				t.Errorf("stdout = %q, want the text 'red ok'", stdout)
			}
		})
	}
}

func TestReplayPlaysFramesInOrder(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "demo.shot", simpleRecording(t))
	// The blank screen before the first output is dropped unless kept.
	for _, tt := range []struct {
		args []string
		want string
	}{
		{nil, "first\n\nfirst\nsecond\n"},
		{[]string{"--keep-leading-blank"}, "\n\nfirst\n\nfirst\nsecond\n"},
	} {
		code, stdout, stderr := runReplayCmd(t, append([]string{path, "--speed", "1000"}, tt.args...)...)
		if code != 0 {
			t.Fatalf("%v: exit code = %d, stderr = %q", tt.args, code, stderr)
		}
		if stdout != tt.want {
			t.Errorf("%v: stdout = %q, want %q", tt.args, stdout, tt.want)
		}
		if stderr != "" {
			t.Errorf("%v: stderr = %q, want empty", tt.args, stderr)
		}
	}
}

func TestReplayClearFlag(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "demo.shot", simpleRecording(t))
	code, stdout, stderr := runReplayCmd(t, path, "--speed", "1000", "--clear", "--keep-leading-blank")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if n := strings.Count(stdout, frameStart); n != 3 {
		t.Errorf("want 3 synchronized frames, got %d in %q", n, stdout)
	}
	if strings.Contains(stdout, "\x1b[2J") {
		t.Errorf("stdout clears the screen between frames: %q", stdout)
	}
	if strings.Index(stdout, "first") > strings.Index(stdout, "second") {
		t.Errorf("frames out of order: %q", stdout)
	}
}

// A recording larger than the terminal is cropped to it and the note says so;
// --no-crop is accepted. The terminal size is passed in directly because the
// test output is a buffer, not a terminal.
func TestReplayCropsToTerminal(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "demo.shot", simpleRecording(t)) // 10x2
	var out, errBuf bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetContext(t.Context())
	cmd.SetOut(&out)
	cmd.SetErr(&errBuf)
	if err := runReplay(cmd, path, 60, replay.Options{Speed: 1000, Clear: true, Cols: 4, Rows: 1}, true); err != nil {
		t.Fatalf("runReplay() error = %v", err)
	}
	if want := "shellshot: replay: recording is 10x2 but the terminal is 4x1: cropping (use --no-crop to disable)\n"; errBuf.String() != want {
		t.Errorf("stderr = %q, want %q", errBuf.String(), want)
	}
	if !strings.Contains(out.String(), "seco") || strings.Contains(out.String(), "secon") || strings.Contains(out.String(), "first") {
		t.Errorf("stdout = %q, want the last row cropped to 4 columns", out.String())
	}

	code, _, stderr := runReplayCmd(t, path, "--speed", "1000", "--clear", "--no-crop", "-q")
	if code != 0 || stderr != "" {
		t.Errorf("--no-crop: exit code = %d, stderr = %q", code, stderr)
	}
}

// --fps sets the sampling grid: two writes 20ms apart share one frame at
// 10 fps and get a frame each at 1000 fps.
func TestReplayFPSChangesFrameCount(t *testing.T) {
	t.Parallel()
	data := buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("a")))
		must(t, w.Output(30*time.Millisecond, []byte("b")))
	})
	path := writeFile(t, "fps.shot", data)
	for fps, want := range map[string]int{"10": 2, "1000": 3} { // blank + final, or blank + a + final
		_, stdout, stderr := runReplayCmd(t, path, "--speed", "1000", "--clear", "--keep-leading-blank", "--fps", fps)
		if n := strings.Count(stdout, frameStart); n != want {
			t.Errorf("--fps %s: %d frames, want %d (stderr %q)", fps, n, want, stderr)
		}
	}
}

// A five minute pause is one step of the frame iterator, not thousands, and
// replays at --speed in a fraction of the pause.
func TestReplayIdleGapIsInstant(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "idle.shot", idleGapRecording(t))
	start := time.Now()
	code, stdout, stderr := runReplayCmd(t, path, "--speed", "2000", "--clear")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("replay took %v, want well under the 5 minute gap", elapsed)
	}
	if n := strings.Count(stdout, frameStart); n < 2 || n > 4 {
		t.Errorf("%d frames for a recording with two bursts, want 2 to 4", n)
	}
	if !strings.Contains(stdout, "after") {
		t.Errorf("final frame missing: %q", stdout)
	}
}

func TestReplayHelpShowsIdleFlags(t *testing.T) {
	t.Parallel()
	code, stdout, stderr := runReplayCmd(t, "--help")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	for _, want := range []string{
		"--idle-limit duration",
		"longest pause between frames, before --speed (0 for no limit)",
		"(default 2s)",
		"--keep-leading-blank",
		"keep a blank screen at the start of the animation",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("replay --help lacks %q:\n%s", want, stdout)
		}
	}
}

// A long pause is capped by --idle-limit (2s by default) and the blank screen
// before the first output is dropped, using a fake sleeper so the test does
// not wait.
func TestReplayIdleLimitCapsPauses(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "idle.shot", idleGapRecording(t))
	sleeps := func(opts replay.Options) []time.Duration {
		var got []time.Duration
		if opts.Speed == 0 {
			opts.Speed = 1
		}
		opts.Sleep = func(_ context.Context, d time.Duration) error {
			got = append(got, d)
			return nil
		}
		cmd := &cobra.Command{}
		cmd.SetContext(t.Context())
		cmd.SetOut(&bytes.Buffer{})
		cmd.SetErr(&bytes.Buffer{})
		if err := runReplay(cmd, path, 60, opts, true); err != nil {
			t.Fatalf("runReplay() error = %v", err)
		}
		return got
	}
	if def := newReplayCmd().Flags().Lookup("idle-limit").DefValue; def != "2s" {
		t.Errorf("--idle-limit default = %q, want 2s", def)
	}
	// The recording is blank for 10ms, "before" for 5 minutes, then "after".
	for _, tt := range []struct {
		name string
		opts replay.Options
		want []time.Duration
	}{
		{"default", replay.Options{IdleLimit: 2 * time.Second, TrimLeadingBlank: true}, []time.Duration{2 * time.Second}},
		{"speed", replay.Options{IdleLimit: 2 * time.Second, TrimLeadingBlank: true, Speed: 4}, []time.Duration{500 * time.Millisecond}},
		{"no limit", replay.Options{TrimLeadingBlank: true}, []time.Duration{5 * time.Minute}},
		{"keep blank", replay.Options{IdleLimit: 2 * time.Second}, []time.Duration{10 * time.Millisecond, 2 * time.Second}},
	} {
		// Frame times sit on the 60 fps grid, so allow a frame of slack.
		got := sleeps(tt.opts)
		if len(got) != len(tt.want) {
			t.Errorf("%s: sleeps = %v, want %v", tt.name, got, tt.want)
			continue
		}
		for i := range got {
			if d := got[i] - tt.want[i]; d < -20*time.Millisecond || d > 20*time.Millisecond {
				t.Errorf("%s: sleeps = %v, want %v", tt.name, got, tt.want)
				break
			}
		}
	}
}

// The --idle-limit flag reaches Play: a 10ms limit replays the 5 minute pause
// at once, and a negative limit is refused.
func TestReplayIdleLimitFlag(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "idle.shot", idleGapRecording(t))
	start := time.Now()
	if code, _, stderr := runReplayCmd(t, path, "--idle-limit", "10ms"); code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("replay with --idle-limit 10ms took %v, want well under the 5 minute pause", elapsed)
	}
	code, _, stderr := runReplayCmd(t, path, "--idle-limit", "-1s")
	if code == 0 || !strings.Contains(stderr, "invalid --idle-limit") {
		t.Errorf("exit code = %d, stderr = %q, want an invalid --idle-limit error", code, stderr)
	}
}

func TestReplaySurvivesResize(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "resize.shot", resizingRecording(t))
	for _, clear := range []string{"--clear=false", "--clear=true"} {
		code, stdout, stderr := runReplayCmd(t, path, "--speed", "1000", clear)
		if code != 0 {
			t.Fatalf("%s: exit code = %d, stderr = %q", clear, code, stderr)
		}
		if !strings.Contains(stdout, "small bigger") {
			t.Errorf("%s: stdout = %q, want the final text 'small bigger'", clear, stdout)
		}
	}
}

func TestReplayTruncatedPlaysValidPrefix(t *testing.T) {
	t.Parallel()
	data, firstEnd := twoBlockRecording(t)
	path := writeFile(t, "torn.shot", data[:len(data)-3])
	code, stdout, stderr := runReplayCmd(t, path, "--speed", "1000")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 for a truncated recording; stderr %q", code, stderr)
	}
	if want := fmt.Sprintf("shellshot: recording is truncated (%d bytes valid)\n", firstEnd); stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if !strings.Contains(stdout, "one") || strings.Contains(stdout, "two") {
		t.Errorf("stdout = %q, want the first block only", stdout)
	}
}

// A recording with no EXIT event ends cleanly but its recorder died: replay
// plays everything, exits 0 and says so on stderr.
func TestReplayInterruptedWarns(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "killed.shot", interruptedRecording(t))
	code, stdout, stderr := runReplayCmd(t, path, "--speed", "1000")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr %q", code, stderr)
	}
	if want := "shellshot: recording is incomplete (no exit event; recorder was interrupted)\n"; stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if !strings.Contains(stdout, "one") || !strings.Contains(stdout, "two") {
		t.Errorf("stdout = %q, want both blocks", stdout)
	}
}

// A finished recording gets no note.
func TestReplayCompleteHasNoWarning(t *testing.T) {
	t.Parallel()
	path := writeFile(t, "done.shot", simpleRecording(t))
	if code, _, stderr := runReplayCmd(t, path, "--speed", "1000"); code != 0 || stderr != "" {
		t.Errorf("exit %d, stderr %q, want 0 and no output", code, stderr)
	}
}

func TestReplayCorruptIsFatal(t *testing.T) {
	t.Parallel()
	data, firstEnd := twoBlockRecording(t)
	bad := append([]byte(nil), data...)
	bad[firstEnd-10] ^= 0xff // inside the first block, which is followed by more data
	path := writeFile(t, "corrupt.shot", bad)
	code, _, stderr := runReplayCmd(t, path, "--speed", "1000")
	if code != 1 || !strings.Contains(stderr, "corrupt") {
		t.Errorf("exit code = %d, stderr = %q, want exit 1 with a corrupt error", code, stderr)
	}
}

func TestReplayErrors(t *testing.T) {
	t.Parallel()
	good := writeFile(t, "demo.shot", simpleRecording(t))
	json := writeFile(t, "old.jsonl", []byte(`{"version":1,"cols":10,"rows":2}`+"\n"))
	empty := writeFile(t, "empty.shot", nil)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing file", []string{"replay", filepath.Join(t.TempDir(), "nope.shot")}, "no such file"},
		{"json file", []string{"replay", json}, "not a shellshot recording (bad magic)"},
		{"empty file", []string{"replay", empty}, "truncated"},
		{"zero speed", []string{"replay", good, "--speed", "0"}, "speed must be greater than zero"},
		{"negative speed", []string{"replay", good, "--speed", "-2"}, "speed must be greater than zero"},
		{"fps zero", []string{"replay", good, "--fps", "0"}, "invalid --fps 0: must be between 1 and 1000"},
		{"fps negative", []string{"replay", good, "--fps", "-5"}, "invalid --fps -5"},
		{"fps too high", []string{"replay", good, "--fps", "1001"}, "invalid --fps 1001"},
		{"invalid color", []string{"replay", good, "--color", "sometimes"}, "invalid --color"},
		{"no file argument", []string{"replay"}, "accepts 1 arg"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), tt.args, &stdout, &stderr)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr.String(), tt.want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.want)
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

func TestIsTerminal(t *testing.T) {
	t.Parallel()
	if isTerminal(&bytes.Buffer{}) {
		t.Error("bytes.Buffer reported as terminal")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "plain"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Error("regular file reported as terminal")
	}
}

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

	"github.com/michaellorant/shellshot/internal/recording/bin"
	"github.com/michaellorant/shellshot/internal/recording/bin/head"
)

func runReplayCmd(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errBuf bytes.Buffer
	code = Run(context.Background(), append([]string{"replay"}, args...), &out, &errBuf)
	return code, out.String(), errBuf.String()
}

const clearSeq = "\x1b[H\x1b[2J"

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
	path := writeFile(t, "demo.shot", simpleRecording(t))
	code, stdout, stderr := runReplayCmd(t, path, "--speed", "1000")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	// Blank screen, "first", then both rows: three frames of two lines each.
	if want := "\n\nfirst\n\nfirst\nsecond\n"; stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestReplayClearFlag(t *testing.T) {
	path := writeFile(t, "demo.shot", simpleRecording(t))
	code, stdout, stderr := runReplayCmd(t, path, "--speed", "1000", "--clear")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if n := strings.Count(stdout, clearSeq); n != 3 {
		t.Errorf("want a clear before each of 3 frames, got %d in %q", n, stdout)
	}
	if strings.Index(stdout, "first") > strings.Index(stdout, "second") {
		t.Errorf("frames out of order: %q", stdout)
	}
}

// --fps sets the sampling grid: two writes 20ms apart share one frame at
// 10 fps and get a frame each at 1000 fps.
func TestReplayFPSChangesFrameCount(t *testing.T) {
	data := buildRecording(t, head.Head{Cols: 10, Rows: 2}, bin.WriterOptions{}, func(w *bin.Writer, _ *bytes.Buffer) {
		must(t, w.Output(10*time.Millisecond, []byte("a")))
		must(t, w.Output(30*time.Millisecond, []byte("b")))
	})
	path := writeFile(t, "fps.shot", data)
	for fps, want := range map[string]int{"10": 2, "1000": 3} { // blank + final, or blank + a + final
		_, stdout, stderr := runReplayCmd(t, path, "--speed", "1000", "--clear", "--fps", fps)
		if n := strings.Count(stdout, clearSeq); n != want {
			t.Errorf("--fps %s: %d frames, want %d (stderr %q)", fps, n, want, stderr)
		}
	}
}

// A five minute pause is one step of the frame iterator, not thousands, and
// replays at --speed in a fraction of the pause.
func TestReplayIdleGapIsInstant(t *testing.T) {
	path := writeFile(t, "idle.shot", idleGapRecording(t))
	start := time.Now()
	code, stdout, stderr := runReplayCmd(t, path, "--speed", "2000", "--clear")
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("replay took %v, want well under the 5 minute gap", elapsed)
	}
	if n := strings.Count(stdout, clearSeq); n < 3 || n > 5 {
		t.Errorf("%d frames for a recording with two bursts, want 3 to 5", n)
	}
	if !strings.Contains(stdout, "after") {
		t.Errorf("final frame missing: %q", stdout)
	}
}

func TestReplaySurvivesResize(t *testing.T) {
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
	path := writeFile(t, "done.shot", simpleRecording(t))
	if code, _, stderr := runReplayCmd(t, path, "--speed", "1000"); code != 0 || stderr != "" {
		t.Errorf("exit %d, stderr %q, want 0 and no output", code, stderr)
	}
}

func TestReplayCorruptIsFatal(t *testing.T) {
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

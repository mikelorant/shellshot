package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/recording"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

// writeFixtureRecording writes a two-frame recording and returns its path.
func writeFixtureRecording(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "demo.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := recording.NewWriter(f, recording.Header{Cols: 10, Rows: 2, Command: []string{"demo"}, Started: time.Unix(0, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	for _, fr := range []snapshot.Frame{
		{At: 0, Snapshot: snapshot.Snapshot{Cols: 10, Rows: 2, Lines: []string{"first", ""}}},
		{At: 500 * time.Millisecond, Snapshot: snapshot.Snapshot{Cols: 10, Rows: 2, Lines: []string{"second", ""}}},
	} {
		if err := w.WriteFrame(fr); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// writeStyledRecording writes a one-frame recording whose first three
// characters are red and returns its path.
func writeStyledRecording(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "styled.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w, err := recording.NewWriter(f, recording.Header{Cols: 10, Rows: 1, Command: []string{"demo"}, Started: time.Unix(0, 0).UTC()})
	if err != nil {
		t.Fatal(err)
	}
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	frame := snapshot.Frame{Snapshot: snapshot.Snapshot{
		Cols: 10, Rows: 1,
		Lines: []string{"red ok"},
		Spans: [][]snapshot.Span{{{Start: 0, End: 3, Style: red}}},
	}}
	if err := w.WriteFrame(frame); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReplayColorFlag(t *testing.T) {
	path := writeStyledRecording(t)
	t.Setenv("NO_COLOR", "")
	tests := []struct {
		color string
		want  string
	}{
		{"always", "\x1b[31mred\x1b[0m ok\n"},
		{"never", "red ok\n"},
		{"auto", "red ok\n"}, // stdout is a buffer, not a terminal
	}
	for _, tt := range tests {
		t.Run(tt.color, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"replay", path, "--color", tt.color}, &stdout, &stderr)
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if stdout.String() != tt.want {
				t.Errorf("stdout = %q, want %q", stdout.String(), tt.want)
			}
		})
	}
}

func TestReplayPlaysFramesInOrder(t *testing.T) {
	path := writeFixtureRecording(t)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"replay", path, "--speed", "1000"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if want := "first\n\nsecond\n\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestReplayClearFlag(t *testing.T) {
	path := writeFixtureRecording(t)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"replay", path, "--speed", "1000", "--clear"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	out := stdout.String()
	if strings.Count(out, "\x1b[H\x1b[2J") != 2 {
		t.Errorf("want clear before each of 2 frames, got %q", out)
	}
	if strings.Index(out, "first") > strings.Index(out, "second") {
		t.Errorf("frames out of order: %q", out)
	}
}

func TestReplayErrors(t *testing.T) {
	good := writeFixtureRecording(t)
	bad := filepath.Join(t.TempDir(), "bad.jsonl")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"missing file", []string{"replay", filepath.Join(t.TempDir(), "nope.jsonl")}, "no such file"},
		{"malformed file", []string{"replay", bad}, "malformed"},
		{"zero speed", []string{"replay", good, "--speed", "0"}, "speed must be greater than zero"},
		{"negative speed", []string{"replay", good, "--speed", "-2"}, "speed must be greater than zero"},
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

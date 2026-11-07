package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/michaellorant/shellshot/internal/session"
	"github.com/michaellorant/shellshot/internal/snapshot"
)

func TestLastFrameSinkKeepsLastFrame(t *testing.T) {
	var sink lastFrameSink
	for _, line := range []string{"one", "two"} {
		f := snapshot.Frame{Snapshot: snapshot.Snapshot{Lines: []string{line}}}
		if err := sink.Frame(f); err != nil {
			t.Fatalf("Frame: %v", err)
		}
	}
	if got := sink.frame.Snapshot.Text(); got != "two" {
		t.Errorf("last frame text = %q, want %q", got, "two")
	}
}

func TestWriteScreen(t *testing.T) {
	red := snapshot.Style{Fg: snapshot.Color{Kind: snapshot.ColorPalette, Index: 1}}
	styled := snapshot.Snapshot{
		Lines: []string{"red ok", "", ""},
		Spans: [][]snapshot.Span{{{Start: 0, End: 3, Style: red}}, nil, nil},
	}
	tests := []struct {
		name  string
		s     snapshot.Snapshot
		color bool
		want  string
	}{
		{"appends newline", snapshot.Snapshot{Lines: []string{"hello", "world"}}, false, "hello\nworld\n"},
		{"trims trailing blank rows", snapshot.Snapshot{Lines: []string{"a", "", "b", "", ""}}, false, "a\n\nb\n"},
		{"empty screen prints nothing", snapshot.Snapshot{Lines: []string{"", ""}}, false, ""},
		{"plain ignores spans", styled, false, "red ok\n"},
		{"colour writes SGR", styled, true, "\x1b[31mred\x1b[0m ok\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := writeScreen(&buf, tt.s, tt.color); err != nil {
				t.Fatalf("writeScreen: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Errorf("output = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCaptureFlagsParsing(t *testing.T) {
	var got captureOptions
	cmd := &cobra.Command{
		Use: "x",
		RunE: func(_ *cobra.Command, args []string) error {
			got.Command = args
			return nil
		},
	}
	addCaptureFlags(cmd, &got)
	cmd.SetArgs([]string{"--cols", "40", "--rows", "5", "--fps", "30", "--", "ls", "-l", "--cols", "9"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Cols != 40 || got.Rows != 5 || got.FPS != 30 {
		t.Errorf("options = %+v, want 40x5 at 30fps", got)
	}
	if want := "ls -l --cols 9"; strings.Join(got.Command, " ") != want {
		t.Errorf("command = %q, want %q", got.Command, want)
	}
}

func TestSnapFlagDefaults(t *testing.T) {
	cmd := newSnapCmd()
	for name, want := range map[string]string{
		"cols":  "80",
		"rows":  "24",
		"fps":   strconv.Itoa(session.DefaultFPS),
		"color": "auto",
	} {
		if got := cmd.Flags().Lookup(name).DefValue; got != want {
			t.Errorf("--%s default = %q, want %q", name, got, want)
		}
	}
}

func TestSnapRequiresCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"snap"}, &stdout, &stderr); code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
}

func TestSnapPrintsFinalScreen(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"snap", "--cols", "40", "--rows", "5", "--", "sh", "-c", `printf 'hello\nworld'`}
	code := Run(context.Background(), args, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr.String())
	}
	if want := "hello\nworld\n"; stdout.String() != want {
		t.Errorf("stdout = %q, want %q", stdout.String(), want)
	}
}

func TestSnapColorFlag(t *testing.T) {
	const script = `printf '\033[31mred\033[0m'`
	tests := []struct {
		name      string
		color     string
		wantSGR   bool
		wantError string
	}{
		{"always emits SGR", "always", true, ""},
		{"never is plain", "never", false, ""},
		{"auto is plain when piped", "auto", false, ""},
		{"invalid", "sometimes", false, "invalid --color"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			args := []string{"snap", "--color", tt.color, "--cols", "40", "--rows", "4", "--", "sh", "-c", script}
			code := Run(context.Background(), args, &stdout, &stderr)
			if tt.wantError != "" {
				if code != 1 || !strings.Contains(stderr.String(), tt.wantError) {
					t.Fatalf("exit code = %d, stderr = %q, want 1 and %q", code, stderr.String(), tt.wantError)
				}
				return
			}
			if code != 0 {
				t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
			}
			if tt.wantSGR {
				if want := "\x1b[31mred\x1b[0m\n"; stdout.String() != want {
					t.Errorf("stdout = %q, want %q", stdout.String(), want)
				}
			} else if want := "red\n"; stdout.String() != want {
				t.Errorf("stdout = %q, want %q", stdout.String(), want)
			}
		})
	}
}

func TestCaptureOptionsValidateFPS(t *testing.T) {
	for _, tc := range []struct {
		fps int
		ok  bool
	}{{-1, false}, {0, false}, {1, true}, {60, true}, {1000, true}, {1001, false}} {
		err := captureOptions{FPS: tc.fps}.validate()
		if (err == nil) != tc.ok {
			t.Errorf("validate(fps=%d) = %v, want ok=%v", tc.fps, err, tc.ok)
		}
	}
}

func TestRecordRejectsInvalidFPS(t *testing.T) {
	for _, fps := range []string{"0", "-5", "1001"} {
		out := filepath.Join(t.TempDir(), "x.shot")
		var stdout, stderr bytes.Buffer
		code := runWithStdin(context.Background(), []string{"record", "-o", out, "--fps", fps, "-c", "true"}, strings.NewReader(""), &stdout, &stderr)
		if code != 1 {
			t.Errorf("--fps %s: exit code = %d, want 1", fps, code)
		}
		if !strings.Contains(stderr.String(), "invalid --fps") {
			t.Errorf("--fps %s: stderr = %q, want invalid --fps", fps, stderr.String())
		}
		if _, err := os.Stat(out); err == nil {
			t.Errorf("--fps %s: recording file created", fps)
		}
	}
}

func TestRecordRejectsInterval(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := runWithStdin(context.Background(), []string{"record", "--interval", "50ms", "-c", "true"}, strings.NewReader(""), &stdout, &stderr)
	if code == 0 || !strings.Contains(stderr.String(), "unknown flag") {
		t.Errorf("code=%d stderr=%q, want unknown flag error", code, stderr.String())
	}
}

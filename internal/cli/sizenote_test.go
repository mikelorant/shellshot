package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func TestSizeMismatchNote(t *testing.T) {
	t.Parallel()
	const bigger = "shellshot: your terminal is 140x40 but the recording is 80x24, " +
		"so the live display can look wrong; the recording itself is correct"
	tests := []struct {
		name          string
		real, virtual cellSize
		want          string
	}{
		{"equal", cellSize{80, 24}, cellSize{80, 24}, ""},
		{"bigger in both", cellSize{140, 40}, cellSize{80, 24}, bigger},
		{"bigger in columns only", cellSize{100, 24}, cellSize{80, 24},
			"shellshot: your terminal is 100x24 but the recording is 80x24, " +
				"so the live display can look wrong; the recording itself is correct"},
		{"smaller in both", cellSize{60, 15}, cellSize{80, 24},
			"shellshot: your terminal is 60x15, smaller than the recording (80x24), " +
				"so the live display will look wrong; the recording itself is correct"},
		{"smaller in rows only", cellSize{80, 20}, cellSize{80, 24},
			"shellshot: your terminal is 80x20, smaller than the recording (80x24), " +
				"so the live display will look wrong; the recording itself is correct"},
		{"bigger in columns and smaller in rows", cellSize{140, 10}, cellSize{80, 24},
			"shellshot: your terminal is 140x10, smaller than the recording (80x24), " +
				"so the live display will look wrong; the recording itself is correct"},
		{"unknown real size", cellSize{}, cellSize{80, 24}, ""},
		{"unknown real rows", cellSize{140, 0}, cellSize{80, 24}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := sizeMismatchNote(tt.real, tt.virtual); got != tt.want {
				t.Errorf("sizeMismatchNote(%v, %v) =\n%q\nwant\n%q", tt.real, tt.virtual, got, tt.want)
			}
		})
	}
}

// sizeNoteScript is a script whose recorded terminal is 80x24.
const sizeNoteScript = "shell /bin/sh\nenv PS1=\"$ \"\nsize 80x24\ntyping 5ms\nrun echo hi\nwait /^hi$/ 5s\n"

// runRecordWithRealSize runs "shellshot record" with args, whose stdout reports
// a terminal of the given size (0x0 for no terminal), and returns its stderr.
func runRecordWithRealSize(t *testing.T, real cellSize, args ...string) string {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.script")
	if err := os.WriteFile(file, []byte(sizeNoteScript), 0o644); err != nil {
		t.Fatal(err)
	}
	fakeSize := func(io.Writer) (int, int) { return real.Cols, real.Rows }
	var stdout, stderr bytes.Buffer
	root := &cobra.Command{Use: "shellshot", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newRecordCmdWith(recordOptions{now: time.Now, outDir: dir, stdoutSize: fakeSize}))
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"record", "-o", filepath.Join(dir, "demo.shot")}, args...))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := root.ExecuteContext(ctx); err != nil {
		t.Fatalf("record: %v\n%q", err, stderr.String())
	}
	return strings.ReplaceAll(stderr.String(), file, "SCRIPT")
}

func TestRecordScriptWatchWarnsWhenTerminalSizeDiffers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.script")
	if err := os.WriteFile(file, []byte(sizeNoteScript), 0o644); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		real cellSize
		args []string
		want string // a fragment of the note; "" for no note
	}{
		{"bigger terminal", cellSize{140, 40}, []string{"--watch"}, "your terminal is 140x40 but the recording is 80x24"},
		{"smaller terminal", cellSize{60, 15}, []string{"--watch"}, "your terminal is 60x15, smaller than the recording (80x24)"},
		{"flag beats script size", cellSize{80, 24}, []string{"--watch", "--cols", "100"}, "your terminal is 80x24, smaller than the recording (100x24)"},
		{"equal sizes", cellSize{80, 24}, []string{"--watch"}, ""},
		{"stdout is not a terminal", cellSize{}, []string{"--watch"}, ""},
		{"without --watch", cellSize{140, 40}, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stderr := runRecordWithRealSize(t, tt.real, append([]string{"--script", file}, tt.args...)...)
			assertSizeNote(t, stderr, tt.want)
		})
	}
}

func TestRecordScriptListNeverWarns(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "demo.script")
	if err := os.WriteFile(file, []byte(sizeNoteScript), 0o644); err != nil {
		t.Fatal(err)
	}
	assertSizeNote(t, runRecordWithRealSize(t, cellSize{140, 40}, "--script", file, "--list"), "")
}

func TestRecordInteractiveWarnsOnlyForExplicitSize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		real cellSize
		args []string
		want string
	}{
		{"cols and rows differ", cellSize{140, 40}, []string{"--cols", "80", "--rows", "24"}, "your terminal is 140x40 but the recording is 80x24"},
		// The rows follow the real stdout, a buffer here, so they are the default 24.
		{"only cols given", cellSize{140, 40}, []string{"--cols", "80"}, "your terminal is 140x40 but the recording is 80x24"},
		{"explicit size equals the terminal", cellSize{80, 24}, []string{"--cols", "80", "--rows", "24"}, ""},
		{"size follows the terminal", cellSize{140, 40}, nil, ""},
		{"stdout is not a terminal", cellSize{}, []string{"--cols", "80", "--rows", "24"}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stderr := runRecordWithRealSize(t, tt.real, append(tt.args, "-c", "true")...)
			assertSizeNote(t, stderr, tt.want)
		})
	}
}

// assertSizeNote checks that stderr has exactly one size note containing want,
// or none when want is empty.
func assertSizeNote(t *testing.T, stderr, want string) {
	t.Helper()
	n := strings.Count(stderr, "your terminal is")
	if want == "" {
		if n != 0 {
			t.Errorf("stderr has a size note, want none: %q", stderr)
		}
		return
	}
	if n != 1 || !strings.Contains(stderr, want) {
		t.Errorf("stderr = %q, want one note containing %q", stderr, want)
	}
}

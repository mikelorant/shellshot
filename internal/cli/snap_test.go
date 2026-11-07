package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

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

func TestWriteScreenAppendsNewline(t *testing.T) {
	var buf bytes.Buffer
	s := snapshot.Snapshot{Lines: []string{"hello", "world"}}
	if err := writeScreen(&buf, s); err != nil {
		t.Fatalf("writeScreen: %v", err)
	}
	if got, want := buf.String(), "hello\nworld\n"; got != want {
		t.Errorf("output = %q, want %q", got, want)
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
	cmd.SetArgs([]string{"--cols", "40", "--rows", "5", "--interval", "50ms", "--", "ls", "-l", "--cols", "9"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if got.Cols != 40 || got.Rows != 5 || got.Interval != 50*time.Millisecond {
		t.Errorf("options = %+v, want 40x5 at 50ms", got)
	}
	if want := "ls -l --cols 9"; strings.Join(got.Command, " ") != want {
		t.Errorf("command = %q, want %q", got.Command, want)
	}
}

func TestSnapFlagDefaults(t *testing.T) {
	cmd := newSnapCmd()
	for name, want := range map[string]string{
		"cols":     "80",
		"rows":     "24",
		"interval": session.DefaultInterval.String(),
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
	lines := strings.Split(stdout.String(), "\n")
	if len(lines) < 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Errorf("stdout = %q, want first lines hello and world", stdout.String())
	}
	if !strings.HasSuffix(stdout.String(), "\n") {
		t.Errorf("stdout = %q, want trailing newline", stdout.String())
	}
}

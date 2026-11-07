package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestExitErrorMessageAndAs(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", ExitError{Code: 3})
	var exit ExitError
	if !errors.As(err, &exit) || exit.Code != 3 {
		t.Fatalf("errors.As = %+v, want code 3", exit)
	}
	if got := (ExitError{Code: 3}).Error(); got != "command exited with 3" {
		t.Errorf("Error() = %q", got)
	}
}

func TestRecordPassesThroughSignalExitCode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "signal.shot")
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5", "-c", "kill -TERM $$"}
	code, _, stderr := runRecordWithTimeout(t, args, "")
	if code != 128+15 {
		t.Fatalf("exit code = %d, want 143 (stderr %q)", code, stderr)
	}
}

func TestRecordPropagatesChildExitStatusAfterWritingRecording(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exit.shot")
	args := []string{"record", "-o", path, "--cols", "40", "--rows", "5", "-c", "echo before; exit 3"}
	code, _, stderr := runRecordWithTimeout(t, args, "")
	if code != 3 {
		t.Fatalf("exit code = %d, want 3 (stderr %q)", code, stderr)
	}
	if !strings.Contains(stderr, "recorded ") || !strings.Contains(stderr, "shellshot: command exited with 3") {
		t.Errorf("stderr = %q, want recorded line and exit note", stderr)
	}
	if strings.Contains(stderr, "shellshot: command exited with 3\nshellshot:") {
		t.Errorf("stderr = %q, exit note printed twice", stderr)
	}
	assertLastFrameContains(t, path, "before")
}

func TestRecordIgnoreExitStatusExitsZero(t *testing.T) {
	path := filepath.Join(t.TempDir(), "exit0.shot")
	args := []string{"record", "--ignore-exit-status", "-o", path, "--cols", "40", "--rows", "5", "-c", "exit 3"}
	code, _, stderr := runRecordWithTimeout(t, args, "")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr %q)", code, stderr)
	}
}

func TestRunOtherCommandsKeepExitCodeConvention(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"version"}, &stdout, &stderr); code != 0 {
		t.Errorf("version exit code = %d, want 0", code)
	}
	stderr.Reset()
	if code := Run(context.Background(), []string{"replay", filepath.Join(t.TempDir(), "missing.shot")}, &stdout, &stderr); code != 1 {
		t.Errorf("replay of missing file exit code = %d, want 1", code)
	}
	if !strings.HasPrefix(stderr.String(), "shellshot: ") {
		t.Errorf("stderr = %q, want error prefix", stderr.String())
	}
}

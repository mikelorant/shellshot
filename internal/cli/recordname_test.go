package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// fixedClock returns a clock that always reads 2026-11-08 09:30:45 local time.
func fixedClock() func() time.Time {
	at := time.Date(2026, time.November, 8, 9, 30, 45, 0, time.Local)
	return func() time.Time { return at }
}

// recordIn runs "record args..." with the default recording name made in dir at
// the fixed clock, and returns the error and stderr.
func recordIn(t *testing.T, dir string, now func() time.Time, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	root := &cobra.Command{Use: "shellshot", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newRecordCmdWith(recordOptions{now: now, outDir: dir}))
	root.SetIn(strings.NewReader(""))
	root.SetOut(&stdout)
	root.SetErr(&stderr)
	root.SetArgs(append([]string{"record"}, args...))
	err := root.ExecuteContext(context.Background())
	return stderr.String(), err
}

func shotNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names
}

func TestRecordDefaultNameIsTimestamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	stderr, err := recordIn(t, dir, fixedClock(), "-c", "echo hi")
	if err != nil {
		t.Fatalf("record: %v\n%s", err, stderr)
	}
	want := "shellshot-20261108-093045.shot"
	if got := shotNames(t, dir); !slices.Equal(got, []string{want}) {
		t.Fatalf("files = %v, want [%s]", got, want)
	}
	if !strings.Contains(stderr, " to "+filepath.Join(dir, want)+"\n") {
		t.Errorf("stderr = %q, want the recorded line to name %s", stderr, want)
	}
}

func TestRecordDefaultNameNeverOverwrites(t *testing.T) {
	t.Parallel()
	const first = "shellshot-20261108-093045.shot"
	tests := []struct {
		name     string
		existing []string
		want     string
	}{
		{"first taken", []string{first}, "shellshot-20261108-093045-2.shot"},
		{"first and second taken", []string{first, "shellshot-20261108-093045-2.shot"}, "shellshot-20261108-093045-3.shot"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			for _, name := range tt.existing {
				if err := os.WriteFile(filepath.Join(dir, name), []byte("keep"), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			stderr, err := recordIn(t, dir, fixedClock(), "-c", "echo hi")
			if err != nil {
				t.Fatalf("record: %v\n%s", err, stderr)
			}
			if !strings.Contains(stderr, " to "+filepath.Join(dir, tt.want)+"\n") {
				t.Errorf("stderr = %q, want the recorded line to name %s", stderr, tt.want)
			}
			for _, name := range tt.existing {
				data, err := os.ReadFile(filepath.Join(dir, name))
				if err != nil || string(data) != "keep" {
					t.Errorf("%s = %q, %v; want it untouched", name, data, err)
				}
			}
			info, err := os.Stat(filepath.Join(dir, tt.want))
			if err != nil || info.Size() == 0 {
				t.Errorf("%s: %v, want a recording", tt.want, err)
			}
		})
	}
}

func TestRecordTwiceMakesTwoFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for range 2 {
		if stderr, err := recordIn(t, dir, time.Now, "-c", "echo hi"); err != nil {
			t.Fatalf("record: %v\n%s", err, stderr)
		}
	}
	if got := shotNames(t, dir); len(got) != 2 || got[0] == got[1] {
		t.Errorf("files = %v, want two distinct recordings", got)
	}
}

func TestRecordExplicitOutputOverwrites(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "fixed.shot")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stderr, err := recordIn(t, dir, fixedClock(), "-o", path, "-c", "echo hi"); err != nil {
		t.Fatalf("record: %v\n%s", err, stderr)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) == "old" {
		t.Errorf("fixed.shot = %q, %v; want it replaced by the recording", data, err)
	}
	if got := shotNames(t, dir); !slices.Equal(got, []string{"fixed.shot"}) {
		t.Errorf("files = %v, want only fixed.shot", got)
	}
}

func TestRecordFailureLeavesNoDefaultFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	_, err := recordIn(t, dir, fixedClock(), "--", "/nonexistent/shellshot-test-command")
	if err == nil {
		t.Fatal("record of a missing command succeeded")
	}
	if got := shotNames(t, dir); len(got) != 0 {
		t.Errorf("files = %v, want none", got)
	}
}

func TestRecordBadFlagLeavesNoDefaultFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := recordIn(t, dir, fixedClock(), "--codec", "lz4", "-c", "true"); err == nil {
		t.Fatal("record with a bad codec succeeded")
	}
	if got := shotNames(t, dir); len(got) != 0 {
		t.Errorf("files = %v, want none", got)
	}
}

func TestRecordHelpShowsDefaultName(t *testing.T) {
	t.Parallel()
	cmd := newRecordCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if want := "recording file (default shellshot-YYYYMMDD-HHMMSS.shot)"; !strings.Contains(out.String(), want) {
		t.Errorf("help = %q, want %q", out.String(), want)
	}
}

func TestRecordScriptDefaultNameIsTimestamp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	file := filepath.Join(dir, "demo.script")
	if err := os.WriteFile(file, []byte(scriptPrelude+"run echo hi\nwait /^hi$/ 5s\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	root := &cobra.Command{Use: "shellshot", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newRecordCmdWith(recordOptions{now: fixedClock(), outDir: out}))
	var stderr bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&stderr)
	root.SetArgs([]string{"record", "--script", file})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("record --script: %v\n%s", err, stderr.String())
	}
	if got := shotNames(t, out); !slices.Equal(got, []string{"shellshot-20261108-093045.shot"}) {
		t.Errorf("files = %v, want the timestamped recording", got)
	}
}

func TestRecordScriptOutputSettingIsExplicit(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	shot := filepath.Join(dir, "chosen.shot")
	if err := os.WriteFile(shot, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "demo.script")
	src := scriptPrelude + "output \"" + shot + "\"\nrun echo hi\nwait /^hi$/ 5s\n"
	if err := os.WriteFile(file, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	out := t.TempDir()
	root := &cobra.Command{Use: "shellshot", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(newRecordCmdWith(recordOptions{now: fixedClock(), outDir: out}))
	var stderr bytes.Buffer
	root.SetIn(strings.NewReader(""))
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&stderr)
	root.SetArgs([]string{"record", "--script", file})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("record --script: %v\n%s", err, stderr.String())
	}
	if data, err := os.ReadFile(shot); err != nil || string(data) == "old" {
		t.Errorf("chosen.shot = %q, %v; want it replaced", data, err)
	}
	if got := shotNames(t, out); len(got) != 0 {
		t.Errorf("files = %v in the default directory, want none", got)
	}
}

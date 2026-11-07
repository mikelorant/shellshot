package cli

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/michaellorant/shellshot/internal/script"
	"github.com/michaellorant/shellshot/internal/script/run"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// runCLI runs shellshot with args and an empty stdin and returns the exit code
// and the two output streams.
func runCLI(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code = runWithStdin(context.Background(), args, strings.NewReader(""), &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestRecordScriptFlags(t *testing.T) {
	cmd := newRecordCmd()
	for name, want := range map[string]string{"script": "", "list": "false", "watch": "false"} {
		f := cmd.Flags().Lookup(name)
		if f == nil {
			t.Errorf("missing --%s", name)
			continue
		}
		if f.DefValue != want {
			t.Errorf("--%s default = %q, want %q", name, f.DefValue, want)
		}
	}
}

func TestRecordScriptFlagConflicts(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{"script with -c", []string{"--script", "x.script", "-c", "true"}, "put the command in the script with shell and run"},
		{"script with argv", []string{"--script", "x.script", "--", "ls"}, "put the command in the script with shell and run"},
		{"list without script", []string{"--list"}, "--list requires --script"},
		{"watch without script", []string{"--watch"}, "--watch requires --script"},
		{"empty script", []string{"--script", ""}, "--script needs a file"},
		{"list with watch", []string{"--script", "x.script", "--list", "--watch"}, "--list runs nothing"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			t.Chdir(dir)
			code, stdout, stderr := runCLI(t, append([]string{"record"}, tt.args...)...)
			if code != 1 {
				t.Errorf("exit code = %d, want 1", code)
			}
			if !strings.Contains(stderr, tt.want) {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("created files %v, want none", entries)
			}
		})
	}
}

func writeScript(t *testing.T, src string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "demo.script")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRecordScriptDiagnostics(t *testing.T) {
	path := writeScript(t, "shell /bin/sh\nsize 0x0\nrn echo hi\n")
	for _, extra := range [][]string{{"--list"}, {}} {
		t.Run(strings.Join(extra, " "), func(t *testing.T) {
			t.Chdir(t.TempDir())
			code, stdout, stderr := runCLI(t, append([]string{"record", "--script", path}, extra...)...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2 (syntax error)", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
			want := path + `:3:1: unknown command "rn" (did you mean "run"?)`
			if !strings.Contains(stderr, want) {
				t.Errorf("stderr = %q, want it to contain %q", stderr, want)
			}
			if entries, _ := os.ReadDir("."); len(entries) != 0 {
				t.Errorf("created files %v, want none", entries)
			}
		})
	}
}

func TestRecordScriptMissingFile(t *testing.T) {
	code, _, stderr := runCLI(t, "record", "--script", filepath.Join(t.TempDir(), "nope.script"))
	if code != 1 || !strings.Contains(stderr, "read script") {
		t.Errorf("exit code = %d, stderr = %q, want 1 and a read script error", code, stderr)
	}
}

// listCases returns the scripts whose --list output is a golden file: the
// five examples of the design and the scripts in contrib/examples/scripts.
func listCases(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pattern := range []string{"../script/testdata/ex*.script", "../../contrib/examples/scripts/*.script"} {
		m, err := filepath.Glob(pattern)
		if err != nil || len(m) == 0 {
			t.Fatalf("no scripts match %s (err %v)", pattern, err)
		}
		files = append(files, m...)
	}
	return files
}

func TestRecordScriptListGolden(t *testing.T) {
	var files []string
	for _, f := range listCases(t) {
		abs, err := filepath.Abs(f)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, abs)
	}
	goldenDir, err := filepath.Abs(filepath.Join("testdata", "list"))
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(t.TempDir())
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".script")
		t.Run(name, func(t *testing.T) {
			code, stdout, stderr := runCLI(t, "record", "--script", file, "--list")
			if code != 0 || stderr != "" {
				t.Fatalf("exit code = %d, stderr = %q, want 0 and no output", code, stderr)
			}
			golden := filepath.Join(goldenDir, name+".golden")
			if *update {
				if err := os.MkdirAll(filepath.Dir(golden), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(golden, []byte(stdout), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("read golden (run with -update): %v", err)
			}
			if stdout != string(want) {
				t.Errorf("--list output differs from %s (run with -update)\ngot:\n%s", golden, stdout)
			}
		})
	}
	if entries, _ := os.ReadDir("."); len(entries) != 0 {
		t.Errorf("--list created files %v", entries)
	}
}

func TestResolveScriptSettings(t *testing.T) {
	environ := []string{"HOME=/h", "SHELL=/bin/zsh", "PS1=old"}
	tests := []struct {
		name  string
		proc  run.Process
		flags scriptFlags
		want  scriptSettings
	}{
		{
			name: "defaults",
			want: scriptSettings{
				Argv: []string{"/bin/zsh"}, Env: environ, Cols: 100, Rows: 30, Output: "recording.shot",
			},
		},
		{
			name: "script settings",
			proc: run.Process{
				Cols: 60, Rows: 8, Shell: []string{"/bin/sh", "-l"}, Env: []string{"PS1=$ ", "A=1"},
				Cwd: "/tmp", Output: "demo.shot",
			},
			want: scriptSettings{
				Argv: []string{"/bin/sh", "-l"}, Env: []string{"HOME=/h", "SHELL=/bin/zsh", "PS1=$ ", "A=1"},
				Dir: "/tmp", Cols: 60, Rows: 8, Output: "demo.shot",
			},
		},
		{
			name: "flags beat the script",
			proc: run.Process{Cols: 60, Rows: 8, Output: "demo.shot"},
			flags: scriptFlags{
				Output: "x.shot", OutputChanged: true, Cols: 120, ColsChanged: true,
			},
			want: scriptSettings{
				Argv: []string{"/bin/zsh"}, Env: environ, Cols: 120, Rows: 8, Output: "x.shot",
			},
		},
		{
			name: "a flag left at its default does not beat the script",
			proc: run.Process{Cols: 60, Rows: 8, Output: "demo.shot"},
			flags: scriptFlags{
				Output: "recording.shot", Cols: 80, Rows: 24,
			},
			want: scriptSettings{
				Argv: []string{"/bin/zsh"}, Env: environ, Cols: 60, Rows: 8, Output: "demo.shot",
			},
		},
		{
			name: "rows flag beats a terminal-sized default",
			flags: scriptFlags{
				Rows: 10, RowsChanged: true,
			},
			want: scriptSettings{
				Argv: []string{"/bin/zsh"}, Env: environ, Cols: 100, Rows: 10, Output: "recording.shot",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveScriptSettings(tt.proc, tt.flags, environ, 100, 30)
			if !slices.Equal(got.Argv, tt.want.Argv) || !slices.Equal(got.Env, tt.want.Env) ||
				got.Dir != tt.want.Dir || got.Cols != tt.want.Cols || got.Rows != tt.want.Rows || got.Output != tt.want.Output {
				t.Errorf("got  %+v\nwant %+v", got, tt.want)
			}
		})
	}
}

func TestResolveScriptSettingsShellFallback(t *testing.T) {
	got := resolveScriptSettings(run.Process{}, scriptFlags{}, []string{"HOME=/h"}, 80, 24)
	if want := []string{"/bin/sh"}; !slices.Equal(got.Argv, want) {
		t.Errorf("Argv = %q, want %q", got.Argv, want)
	}
}

func TestMergeEnv(t *testing.T) {
	base := []string{"A=1", "B=2", "C=3"}
	got := mergeEnv(base, []string{"B=x", "D=4", "B=y", "E="})
	want := []string{"A=1", "C=3", "D=4", "B=y", "E="}
	if !slices.Equal(got, want) {
		t.Errorf("mergeEnv = %q, want %q", got, want)
	}
	if !slices.Equal(base, []string{"A=1", "B=2", "C=3"}) {
		t.Errorf("mergeEnv changed its base: %q", base)
	}
}

func TestReportScriptResult(t *testing.T) {
	waitErr := &run.ScriptError{
		Kind: run.WaitTimeout, File: "demo.script", Pos: script.Pos{Line: 12, Col: 3}, Scene: "build",
		Message: `wait timed out after 1s waiting for "never"`, ExitCode: run.ExitScriptFailure,
	}
	runtimeErr := &run.ScriptError{Kind: run.KeyEncode, File: "demo.script", Message: "cannot encode key", ExitCode: run.ExitRuntimeError}
	tests := []struct {
		name       string
		res        scriptResult
		wantCode   int // 0: nil error
		wantPlain  bool
		wantStderr []string
	}{
		{name: "success", res: scriptResult{Path: "r.shot"}},
		{
			name: "child failed", res: scriptResult{Path: "r.shot", Status: 5}, wantCode: 5,
			wantStderr: []string{"shellshot: command exited with 5\n"},
		},
		{
			name: "child failed, ignored", res: scriptResult{Path: "r.shot", Status: 5, IgnoreExit: true},
			wantStderr: []string{"shellshot: command exited with 5\n"},
		},
		{
			name: "child closed by shellshot", res: scriptResult{Path: "r.shot", Status: 137, ClosedByUs: true},
			wantStderr: []string{"shellshot: child did not exit; closed by shellshot\n"},
		},
		{
			name: "wait timeout", res: scriptResult{Path: "r.shot", Err: fmt.Errorf("session: driver: %w", waitErr), Screen: "$ echo hi\nhi"},
			wantCode: 3,
			wantStderr: []string{
				"shellshot: final screen:\n| $ echo hi\n| hi\n",
				`shellshot: script failed: demo.script:12:3: scene "build": wait timed out after 1s waiting for "never"; partial recording kept at r.shot` + "\n",
			},
		},
		{
			name: "script runtime error", res: scriptResult{Path: "r.shot", Err: runtimeErr}, wantCode: 1,
			wantStderr: []string{"shellshot: script failed: demo.script: cannot encode key; partial recording kept at r.shot\n"},
		},
		{
			name: "cancelled", res: scriptResult{Path: "r.shot", Err: fmt.Errorf("session: %w", context.Canceled)}, wantCode: 130,
			wantStderr: []string{"shellshot: cancelled; partial recording kept at r.shot\n"},
		},
		{
			name: "other error", res: scriptResult{Path: "r.shot", Err: errors.New("session: sink: disk full")}, wantPlain: true,
			wantStderr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var w bytes.Buffer
			err := reportScriptResult(&w, tt.res)
			var exit ExitError
			switch {
			case tt.wantPlain:
				if err == nil || errors.As(err, &exit) || !strings.Contains(err.Error(), "partial recording kept at r.shot") {
					t.Errorf("err = %v, want a plain error naming the kept recording", err)
				}
			case tt.wantCode == 0:
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
			default:
				if !errors.As(err, &exit) || exit.Code != tt.wantCode {
					t.Errorf("err = %v, want ExitError{%d}", err, tt.wantCode)
				}
			}
			if got, want := w.String(), strings.Join(tt.wantStderr, ""); got != want {
				t.Errorf("stderr = %q, want %q", got, want)
			}
		})
	}
}

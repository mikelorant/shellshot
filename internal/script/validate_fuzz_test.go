package script

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// FuzzCheck checks that Check terminates without panic on any input, gives
// the same diagnostics twice, keeps them ordered, positioned and within the
// limit, never validates a script the parser rejected, and that a script
// without diagnostics is still valid after printing and parsing again.
func FuzzCheck(f *testing.F) {
	for _, pattern := range []string{"testdata/*.script", "testdata/err/*.script", "testdata/validate/*.script"} {
		files, _ := filepath.Glob(pattern)
		for _, file := range files {
			if src, err := os.ReadFile(file); err == nil {
				f.Add(string(src))
			}
		}
	}
	for _, seed := range []string{
		"", "run a\nsize 1x1\n", "scene a {\n}\n", "scene a {\nscene a {\nrun x\n}\n}\n",
		"hidden {\nspeed 2x\nrun x\n}\n", "wait /(/\n", "wait /((a{999}){999}){999}/\n",
		"env A=\n", "shell /bin/sh \"\\x00\"\n", "run \"\\xff\"\n", "mark \"\\e\"\n",
		"size 99999999999999999999x1\nrun a\n", "speed 1e5x\njitter 1e2%\n", "output .shot\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		s, diags := Check("f", []byte(src))
		s2, diags2 := Check("f", []byte(src))
		if a, b := renderDiags(diags), renderDiags(diags2); !slices.Equal(a, b) || dumpAST(s, true) != dumpAST(s2, true) {
			t.Fatalf("checking twice differs: %q vs %q", a, b)
		}
		checkDiagnostics(t, src, diags)
		if parsed, parseDiags := Parse("f", []byte(src)); len(parseDiags) == 0 {
			if again := Validate(parsed); !slices.Equal(renderDiags(again), renderDiags(diags)) {
				t.Fatalf("Validate differs from Check: %q vs %q", renderDiags(again), renderDiags(diags))
			}
		} else if !slices.Equal(renderDiags(parseDiags), renderDiags(diags)) {
			t.Fatalf("Check differs from Parse after a parse error: %q vs %q", renderDiags(diags), renderDiags(parseDiags))
		}
		if len(diags) > 0 {
			return
		}
		out := Format(s)
		s3, diags3 := Check("f", []byte(out))
		if len(diags3) > 0 {
			t.Fatalf("printed script has diagnostics %q\n%s", renderDiags(diags3), out)
		}
		if Format(s3) != out {
			t.Fatal("print/check/print is not stable")
		}
	})
}

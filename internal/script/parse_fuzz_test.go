package script

import (
	"os"
	"path/filepath"
	"testing"
)

// FuzzParse checks that parsing any input terminates without panic, gives
// the same result twice, keeps its diagnostics in order and within the limit,
// and that an accepted script prints and parses again to the same AST.
func FuzzParse(f *testing.F) {
	files, _ := filepath.Glob("testdata/*.script")
	more, _ := filepath.Glob("testdata/err/*.script")
	for _, file := range append(files, more...) {
		if src, err := os.ReadFile(file); err == nil {
			f.Add(string(src))
		}
	}
	for _, seed := range []string{
		"", "}", "{", "scene", "scene a {", "hidden {\n}\n}", "run lines {", "press x3", "wait /a",
		"shell \"", "env =", "env A=", "sleep 1", "scene a {\nscene b {\nscene c {\n",
		"run a\r\nrun b\r\n", "mark \xff\n", "if x {\nrun a\n}\nrun b\n",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		s, diags := ParseString("f", src)
		s2, diags2 := ParseString("f", src)
		if a, b := renderDiags(diags), renderDiags(diags2); len(a) != len(b) || dumpAST(s, true) != dumpAST(s2, true) {
			t.Fatalf("parsing twice differs: %q vs %q", a, b)
		}
		checkDiagnostics(t, src, diags)
		out := Format(s)
		if len(diags) > 0 {
			return
		}
		checkRoundTrip(t, src)
		if Format(s) != out {
			t.Fatal("Format changed its result")
		}
	})
}

func checkDiagnostics(t *testing.T, src string, diags []Diagnostic) {
	t.Helper()
	if len(diags) > MaxDiagnostics {
		t.Fatalf("%d diagnostics", len(diags))
	}
	prev := 0
	for _, d := range diags {
		if d.Msg == "" || d.Pos.Line < 1 || d.Pos.Col < 1 || d.Pos.Offset < prev || d.Pos.Offset > len(src) {
			t.Fatalf("malformed or unordered diagnostic %+v after offset %d (%d bytes)", d, prev, len(src))
		}
		if want := posAt(src, d.Pos.Offset); want != d.Pos {
			t.Fatalf("diagnostic %+v: offset %d is at %+v", d, d.Pos.Offset, want)
		}
		prev = d.Pos.Offset
	}
}

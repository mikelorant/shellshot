package script

import (
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// checkGolden compares got with the file at path, or rewrites it with -update.
func checkGolden(t *testing.T, path, got string) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run go test -update to create it)", err)
	}
	if string(want) != got {
		t.Errorf("%s differs from the parser output (run go test -update to accept):\n--- got\n%s--- want\n%s", path, got, want)
	}
}

func scriptFiles(t *testing.T, pattern string) []string {
	t.Helper()
	files, err := filepath.Glob(pattern)
	if err != nil || len(files) == 0 {
		t.Fatalf("no files match %s (%v)", pattern, err)
	}
	return files
}

func TestParseGolden(t *testing.T) {
	for _, file := range scriptFiles(t, "testdata/*.script") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			s, diags := Parse(file, src)
			if len(diags) > 0 {
				t.Fatalf("diagnostics: %v", renderDiags(diags))
			}
			checkGolden(t, strings.TrimSuffix(file, ".script")+".ast.golden", dumpAST(s, true))
		})
	}
}

func TestParseErrorGolden(t *testing.T) {
	for _, file := range scriptFiles(t, "testdata/err/*.script") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			_, diags := Parse(file, src)
			if len(diags) == 0 {
				t.Fatal("no diagnostics")
			}
			var b strings.Builder
			for _, d := range diags {
				b.WriteString(d.Format(filepath.Base(file)) + "\n")
			}
			checkGolden(t, strings.TrimSuffix(file, ".script")+".diag.golden", b.String())
		})
	}
}

func TestParseAtMostTenDiagnostics(t *testing.T) {
	src := strings.Repeat("bogus\n", 25)
	_, diags := ParseString("x", src)
	if len(diags) != MaxDiagnostics {
		t.Fatalf("got %d diagnostics, want %d", len(diags), MaxDiagnostics)
	}
}

func TestParseNeverReturnsNil(t *testing.T) {
	s, diags := ParseString("f.script", "}")
	if s == nil || s.File != "f.script" || len(diags) != 1 {
		t.Fatalf("got %+v, %v", s, diags)
	}
}

func TestParseStructure(t *testing.T) {
	const src = "size 80x24\nscene a {\n  typing 1s\n  hidden {\n    run x\n  }\n  sleep 1s\n  linger 2s\n}\ncwd /tmp\n"
	s, diags := ParseString("", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	if len(s.Settings) != 2 || s.Settings[0].At != 0 || s.Settings[1].At != 1 || len(s.Items) != 1 {
		t.Fatalf("top level: %+v", s)
	}
	b, ok := s.Items[0].(Block)
	if !ok || b.Kind != BlockScene || b.Name != "a" || b.Pos != (Pos{2, 1, 11}) || b.End != (Pos{Line: 9, Col: 1, Offset: 81}) {
		t.Fatalf("block: %+v", s.Items[0])
	}
	if len(b.Settings) != 2 || b.Settings[0].At != 0 || b.Settings[1].At != 2 || len(b.Items) != 2 {
		t.Fatalf("block contents: %+v", b)
	}
	if h, ok := b.Items[0].(Block); !ok || h.Kind != BlockHidden || h.ItemPos().Line != 4 {
		t.Fatalf("hidden: %+v", b.Items[0])
	}
}

func TestParseHiddenName(t *testing.T) {
	const src = "hidden setup {\n  run x\n}\nhidden {\n  run y\n}\nscene a {\n  hidden  teardown  { # c\n    run z\n  }\n}\n"
	s, diags := ParseString("", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	a := s.Items[2].(Block)
	got := []string{s.Items[0].(Block).Name, s.Items[1].(Block).Name, a.Items[0].(Block).Name}
	if want := []string{"setup", "", "teardown"}; !slices.Equal(got, want) {
		t.Fatalf("hidden names %q, want %q", got, want)
	}
	if out := Format(s); out != "hidden setup {\n  run x\n}\n\nhidden {\n  run y\n}\n\nscene a {\n  hidden teardown {\n    run z\n  }\n}\n" {
		t.Fatalf("format:\n%s", out)
	}
}

func TestParseHiddenNameErrors(t *testing.T) {
	for _, tc := range []struct{ name, src, want string }{
		{"quoted", "hidden \"setup\" {\n  run x\n}\n", `1:8: a hidden name is written bare, not in quotes (write hidden setup {)`},
		{"two names", "hidden a b {\n  run x\n}\n", `1:10: expected "{" to open the hidden block (write the brace after a space on the same line)`},
		{"no brace", "hidden a\n  run x\n}\n", `1:9: expected "{" to open the hidden block (write the brace after a space on the same line)`},
		{"unterminated", "hidden a {\n  run x\n", `1:1: unterminated hidden "a": expected a line with only "}" before the end of the file`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, diags := ParseString("", tc.src)
			if got := renderDiags(diags); len(got) == 0 || got[0] != tc.want {
				t.Fatalf("got %q\nwant first %q", got, tc.want)
			}
		})
	}
}

func TestParseStepOperands(t *testing.T) {
	const src = "press ctrl+c up x3 shift+tab\nwait \"a\"\nwait /b\\/c/ 0s\nwait idle\nwait idle 1.5s\nsleep 2m\nmark \"a b\"\nrun \"x\\n\"  # c\n"
	s, diags := ParseString("", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	want := `Script
  Items
    Step press ctrl+c*1 up*3 shift+tab*1
    Step wait string "a"
    Step wait regex "b/c" 0s
    Step wait idle
    Step wait idle 1500ms
    Step sleep 2m
    Step mark "a b"
    Step run quoted "x\n"
`
	if got := dumpAST(s, false); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	if wait := s.Items[2].(Step); !wait.HasDuration || wait.Duration != 0 || !wait.Pattern.Regex {
		t.Fatalf("wait /re/ 0s: %+v", wait)
	}
	if wait := s.Items[1].(Step); wait.HasDuration {
		t.Fatalf("wait without duration has HasDuration")
	}
}

func TestParseSettingValues(t *testing.T) {
	const src = "shell /bin/bash --rcfile=\"my rc\" \"\" # c\nenv PS1=\"$ \"\nenv E=\nenv P=a=b\ncwd \"/my dir\"\nsettle off\n"
	s, diags := ParseString("", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	var values []string
	for _, st := range s.Settings {
		values = append(values, st.Value)
	}
	wantValues := []string{`/bin/bash --rcfile="my rc" ""`, "PS1=$ ", "E=", "P=a=b", "/my dir", "off"}
	if !reflect.DeepEqual(values, wantValues) {
		t.Fatalf("values %q, want %q", values, wantValues)
	}
	wantArgv := []string{"/bin/bash", "--rcfile=my rc", ""}
	if !reflect.DeepEqual(s.Settings[0].Argv, wantArgv) || s.Settings[1].Argv != nil {
		t.Fatalf("argv %q", s.Settings[0].Argv)
	}
}

func TestParseTextBlock(t *testing.T) {
	const src = "run lines {\n  a\n    b\n\n  c\n}\ntype paragraphs { # c\n  one\n  two\n\n\n  three\n}\n"
	s, diags := ParseString("", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	want := `Script
  Items
    Step run lines "a\n  b\n\nc\n"
    Step type paragraphs "one two\n\nthree"
`
	if got := dumpAST(s, false); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestParseDuration(t *testing.T) {
	ok := map[string]time.Duration{
		"0s": 0, "500ms": 500 * time.Millisecond, "1.5s": 1500 * time.Millisecond,
		"2m": 2 * time.Minute, "0.5m": 30 * time.Second, "10s": 10 * time.Second,
		"0.0000001s": 100, "1.0000000009s": time.Second, "007ms": 7 * time.Millisecond,
		"0.000001ms": 1, "153722867m": 153722867 * time.Minute,
	}
	for in, want := range ok {
		got, err := ParseDuration(in)
		if err != nil || got != want {
			t.Errorf("ParseDuration(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, in := range []string{"", "5", "s", "1h", "1.s", ".5s", "1..5s", "-1s", "+1s", "1e3s", "1 s", "5S", "1.2.3s", "9223372036854775807s", "153722868m"} {
		if got, err := ParseDuration(in); err == nil {
			t.Errorf("ParseDuration(%q) = %v, want an error", in, got)
		}
	}
}

func TestFormatDurationRoundTrip(t *testing.T) {
	for _, d := range []time.Duration{0, 1, 999, 1000, time.Millisecond, 1500 * time.Millisecond, 61 * time.Second, time.Minute, 90 * time.Minute, 1234567891, 1<<63 - 1} {
		got, err := ParseDuration(formatDuration(d))
		if err != nil || got != d {
			t.Errorf("%d -> %q -> %v, %v", d, formatDuration(d), got, err)
		}
	}
}

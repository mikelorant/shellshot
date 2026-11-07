package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateGolden(t *testing.T) {
	for _, file := range scriptFiles(t, "testdata/validate/*.script") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			_, diags := Check(file, src)
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

// TestCheckDocExamples checks that the five examples of the design document
// parse and validate without a diagnostic.
func TestCheckDocExamples(t *testing.T) {
	files := scriptFiles(t, "testdata/ex*.script")
	if len(files) != 5 {
		t.Fatalf("found %d example scripts, want 5", len(files))
	}
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			s, diags := Check(file, src)
			if len(diags) > 0 {
				t.Fatalf("diagnostics: %v", renderDiags(diags))
			}
			if s == nil || s.File != file || len(s.Items) == 0 {
				t.Fatalf("unexpected script %+v", s)
			}
		})
	}
}

func TestCheckStopsAfterParseErrors(t *testing.T) {
	// The parse error is reported; the invalid size is not.
	s, diags := Check("t.script", []byte("rn x\nsize 0x0\nrun a\n"))
	if s == nil || len(diags) != 1 || diags[0].Msg != `unknown command "rn"` {
		t.Fatalf("got %+v, %v", s, renderDiags(diags))
	}
}

func TestCheckValidates(t *testing.T) {
	s, diags := Check("t.script", []byte("run a\nsize 0x0\n"))
	want := `2:1: "size" must appear before the first step (first step: line 1)`
	if s == nil || len(diags) != 2 || diags[0].Error() != want || diags[1].Msg != `bad size "0x0"` {
		t.Fatalf("got %+v, %v", s, renderDiags(diags))
	}
}

func TestCheckCleanScript(t *testing.T) {
	s, diags := Check("t.script", []byte(""))
	if s == nil || len(diags) != 0 {
		t.Fatalf("got %+v, %v", s, renderDiags(diags))
	}
}

func TestCheckInvalidUTF8IsAParseError(t *testing.T) {
	_, diags := Check("t.script", []byte("run a\xff\n"))
	if len(diags) == 0 || diags[0].Msg != "invalid UTF-8" {
		t.Fatalf("got %v", renderDiags(diags))
	}
}

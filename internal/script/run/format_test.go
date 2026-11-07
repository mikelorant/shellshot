package run

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mikelorant/shellshot/internal/script"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// TestGoldenPlans lowers each testdata/lower/*.script (the five examples of
// docs/research/script-format.md §5, verbatim, and others) and compares the
// plan listing with the .plan.golden file next to it.
func TestGoldenPlans(t *testing.T) {
	files, err := filepath.Glob("testdata/lower/*.script")
	if err != nil || len(files) == 0 {
		t.Fatalf("no golden inputs: %v", err)
	}
	for _, file := range files {
		name := strings.TrimSuffix(filepath.Base(file), ".script")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			s, diags := script.Parse(filepath.Base(file), src)
			if len(diags) > 0 {
				t.Fatalf("parse: %v", diags)
			}
			p, err := Lower(s)
			if err != nil {
				t.Fatal(err)
			}
			got := p.Format()
			if again := mustLower(t, s).Format(); again != got {
				t.Error("Format is not deterministic")
			}
			golden := strings.TrimSuffix(file, ".script") + ".plan.golden"
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("listing differs from %s (run go test -update to accept):\n--- got\n%s--- want\n%s", golden, got, want)
			}
		})
	}
}

func mustLower(t *testing.T, s *script.Script) *Plan {
	t.Helper()
	p, err := Lower(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestFormatShowsScenesAndDurations(t *testing.T) {
	got := lowerSrc(t, "scene intro {\n  sleep 2s\n}\n").Format()
	for _, want := range []string{
		"scene   #1 intro: 2s to 2s",
		"total   2.2s to 6s, 0 waits, 1 scene",
		"span-begin #1 scene \"intro\"",
		"intro",
		"span-begin #2 hidden",
		"exit grace=2s",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("listing lacks %q:\n%s", want, got)
		}
	}
}

func TestFormatShellArgv(t *testing.T) {
	got := lowerSrc(t, "shell /bin/bash --noprofile \"--rcfile=/my dir/rc\" \"\"\n").Format()
	if want := `shell   /bin/bash --noprofile "--rcfile=/my dir/rc" ""`; !strings.Contains(got, want) {
		t.Errorf("listing lacks %q:\n%s", want, got)
	}
	if got := lowerSrc(t, "type a\n").Format(); !strings.Contains(got, "shell   (default)") {
		t.Errorf("default shell not shown:\n%s", got)
	}
}

func TestFormatTruncatesLongText(t *testing.T) {
	got := lowerSrc(t, "type "+strings.Repeat("x", 100)+"\n").Format()
	if !strings.Contains(got, strings.Repeat("x", 60)+`"... (100 runes)`) || strings.Contains(got, strings.Repeat("x", 61)) {
		t.Errorf("long text not cut:\n%s", got)
	}
}

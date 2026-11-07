package run

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTimelineGoldens runs each example plan and each case script against the
// fake shell on a fake clock and compares the timeline of what the host did
// with testdata/exec/NAME.timeline.golden (go test -update rewrites them).
func TestTimelineGoldens(t *testing.T) {
	var scripts []string
	for _, pat := range []string{"testdata/lower/*.script", "testdata/exec/*.script"} {
		m, err := filepath.Glob(pat)
		if err != nil {
			t.Fatal(err)
		}
		scripts = append(scripts, m...)
	}
	if len(scripts) < 10 {
		t.Fatalf("found only %d scripts", len(scripts))
	}
	for _, path := range scripts {
		name := strings.TrimSuffix(filepath.Base(path), ".script")
		t.Run(name, func(t *testing.T) {
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			plan := lowerSrc(t, string(src))
			events, _ := runWorld(t, plan, newFakeShell(), Deps{File: name + ".script"})
			got := timelineText(events)

			golden := filepath.Join("testdata", "exec", name+".timeline.golden")
			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(golden)
			if err != nil {
				t.Fatalf("%v (run go test -update to create it)", err)
			}
			if got != string(want) {
				t.Errorf("timeline differs from %s (run go test -update to accept):\n--- got\n%s--- want\n%s", golden, got, want)
			}
		})
	}
}

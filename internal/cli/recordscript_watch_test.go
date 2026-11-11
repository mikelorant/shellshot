package cli

import (
	"strings"
	"testing"
)

func TestRecordScriptWatchHidesQueriesFromDisplay(t *testing.T) {
	t.Parallel()
	helper := writeHelper(t, "printf 'before \\033]11;?\\033\\\\\\033[6n after\\n'\n")

	r := runScriptCLI(t, "run sh "+helper+"\nwait /before +after/ 10s\n", "--watch")
	if r.code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", r.code, r.stderr)
	}
	for _, q := range []string{"\x1b]11;?", "\x1b[6n"} {
		if strings.Contains(r.stdout, q) {
			t.Errorf("--watch output contains the query %q: %q", q, r.stdout)
		}
	}
	if !strings.Contains(r.stdout, "before ") || !strings.Contains(r.stdout, " after") {
		t.Errorf("--watch output = %q, want the text around the queries", r.stdout)
	}
}

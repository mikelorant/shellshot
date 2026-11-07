package script

import "testing"

func TestEditDistance(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"run", "run", 0},
		{"rn", "run", 1},
		{"rnu", "run", 2},
		{"", "abc", 3},
		{"kitten", "sitting", 3},
		{"é", "e", 1},
	} {
		if got := EditDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("EditDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestDidYouMean(t *testing.T) {
	commands := []string{"run", "type", "press", "wait", "sleep", "mark"}
	for _, tc := range []struct {
		word string
		want string
	}{
		{"rn", `did you mean "run"?`},
		{"tpye", `did you mean "type"?`},  // distance 2
		{"prss", `did you mean "press"?`}, // distance 1
		{"slep", `did you mean "sleep"?`}, // distance 1
		{"wate", `did you mean "wait"?`},  // distance 1
		{"markers", ""},                   // distance 3
		{"run", ""},                       // exact: nothing to suggest
		{"banana", ""},
		{"", ""},
	} {
		if got := DidYouMean(tc.word, commands); got != tc.want {
			t.Errorf("DidYouMean(%q) = %q, want %q", tc.word, got, tc.want)
		}
	}
	// The closest candidate wins; the earliest wins a tie.
	if got := DidYouMean("lines", []string{"line", "linen", "lined"}); got != `did you mean "line"?` {
		t.Errorf("closest = %q", got)
	}
	if got := DidYouMean("ab", []string{"ac", "ad"}); got != `did you mean "ac"?` {
		t.Errorf("tie = %q", got)
	}
}

func TestDiagnosticFormat(t *testing.T) {
	d := Diagnostic{Pos: Pos{Line: 12, Col: 3}, Msg: `unknown command "rn"`, Hint: `did you mean "run"?`}
	want := `demo.script:12:3: unknown command "rn" (did you mean "run"?)`
	if got := d.Format("demo.script"); got != want {
		t.Errorf("Format = %q, want %q", got, want)
	}
	if got := (Diagnostic{Pos: Pos{Line: 1, Col: 2}, Msg: "m"}).Format("f"); got != "f:1:2: m" {
		t.Errorf("no hint: %q", got)
	}
}

func TestDiagnosticsLimit(t *testing.T) {
	var ds Diagnostics
	for i := 1; i <= MaxDiagnostics+3; i++ {
		stored := ds.Add(Pos{Line: i, Col: 1}, "e", "")
		if stored != (i <= MaxDiagnostics) {
			t.Errorf("Add #%d stored = %v", i, stored)
		}
	}
	if len(ds.Items()) != MaxDiagnostics || ds.Dropped() != 3 {
		t.Errorf("items %d dropped %d", len(ds.Items()), ds.Dropped())
	}
	if ds.Items()[MaxDiagnostics-1].Pos.Line != MaxDiagnostics {
		t.Errorf("kept the wrong diagnostics: %v", ds.Items())
	}
}

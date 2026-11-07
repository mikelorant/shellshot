package script

import (
	"slices"
	"strings"
	"testing"
)

type validateCase struct {
	name string
	src  string
	want []string // "line:col: message (hint)" in order; nil means valid
}

// runValidateCases parses each source (which must parse cleanly), validates
// it and compares the diagnostics with the exact text.
func runValidateCases(t *testing.T, cases []validateCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, diags := ParseString("t.script", tc.src)
			if len(diags) > 0 {
				t.Fatalf("source does not parse: %v", renderDiags(diags))
			}
			got := renderDiags(Validate(s))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

func TestValidateSettingPlacement(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"preamble allows every class", "size 80x24\nshell /bin/sh\nenv A=1\ncwd /tmp\nseed 1\noutput a.shot\ntyping 1ms\njitter 1%\ntimeout 1s\nsettle off\nlinger 0s\nspeed 2x\nidle 1s\nhold 1s\nrun a\n", nil},
		{"process after step", "run a\nsize 80x24\n", []string{
			`2:1: "size" must appear before the first step (first step: line 1)`}},
		{"process after block names the first step", "\n\nhidden {\n  run a\n}\nshell /bin/sh\n", []string{
			`6:1: "shell" must appear before the first step (first step: line 3)`}},
		{"every process setting after a step", "run a\nsize 1x1\nshell /bin/sh\nenv A=1\ncwd /tmp\nseed 1\noutput a.shot\n", []string{
			`2:1: "size" must appear before the first step (first step: line 1)`,
			`3:1: "shell" must appear before the first step (first step: line 1)`,
			`4:1: "env" must appear before the first step (first step: line 1)`,
			`5:1: "cwd" must appear before the first step (first step: line 1)`,
			`6:1: "seed" must appear before the first step (first step: line 1)`,
			`7:1: "output" must appear before the first step (first step: line 1)`}},
		{"process in scene", "scene intro {\n  size 80x24\n  run a\n}\n", []string{
			`2:3: "size" must appear before the first step (first step: line 1) (a process setting cannot be set in scene "intro")`}},
		{"process in hidden", "hidden {\n  env A=1\n  run a\n}\n", []string{
			`2:3: "env" must appear before the first step (first step: line 1) (a process setting cannot be set in hidden)`}},
		{"process in hidden in scene", "scene s {\n  hidden {\n    cwd /tmp\n    run a\n  }\n}\n", []string{
			`3:5: "cwd" must appear before the first step (first step: line 1) (a process setting cannot be set in hidden)`}},
		{"hint at top level after step", "run a\nspeed 2x\nidle 1s\nhold 1s\n", []string{
			`2:1: "speed" must appear before the first step or inside a scene (first step: line 1)`,
			`3:1: "idle" must appear before the first step or inside a scene (first step: line 1)`,
			`4:1: "hold" must appear before the first step or inside a scene (first step: line 1)`}},
		{"hint in scene", "scene s {\n  speed 8x\n  idle 2s\n  hold 1s\n  run a\n}\n", nil},
		{"hint in hidden", "hidden {\n  speed 8x\n  run a\n}\n", []string{
			`2:3: "speed" cannot be used in a hidden block (a hidden span has no output; set export hints in the preamble or in a scene)`}},
		{"hint in hidden in scene", "scene s {\n  hidden {\n    hold 1s\n    run a\n  }\n}\n", []string{
			`3:5: "hold" cannot be used in a hidden block (a hidden span has no output; set export hints in the preamble or in a scene)`}},
		{"behaviour anywhere", "typing 1ms\nrun a\ntyping 2ms\nscene s {\n  linger 1s\n  run b\n  jitter 5%\n  hidden {\n    timeout 2s\n    settle 1s\n    run c\n  }\n}\n", nil},
	})
}

func TestValidateDuplicateSettings(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"env repeats", "env A=1\nenv B=2\nenv A=3\nrun a\n", nil},
		{"process once", "size 1x1\nsize 2x2\nrun a\n", []string{`2:1: duplicate "size" (first set on line 1)`}},
		{"every process setting twice", "shell /a\nshell /b\ncwd /a\ncwd /b\nseed 1\nseed 2\noutput a.shot\noutput b.shot\nrun a\n", []string{
			`2:1: duplicate "shell" (first set on line 1)`,
			`4:1: duplicate "cwd" (first set on line 3)`,
			`6:1: duplicate "seed" (first set on line 5)`,
			`8:1: duplicate "output" (first set on line 7)`}},
		{"hint once per scope", "speed 2x\nspeed 3x\nscene a {\n  speed 2x\n  hold 1s\n  run x\n  speed 3x\n}\nscene b {\n  speed 2x\n  run x\n}\n", []string{
			`2:1: duplicate "speed" (first set on line 1)`,
			`7:3: duplicate "speed" (first set on line 4)`}},
		{"behaviour twice with no step between", "typing 1ms\ntyping 2ms\nrun a\n", []string{
			`2:1: "typing" is set twice with no step between (the value on line 1 is never used)`}},
		{"behaviour changed after a step", "typing 1ms\nrun a\ntyping 2ms\nrun b\nscene s {\n  linger 1s\n  run c\n  linger 2s\n  run d\n}\n", nil},
		{"behaviour twice in a block", "scene s {\n  linger 1s\n  linger 2s\n  run c\n}\n", []string{
			`3:3: "linger" is set twice with no step between (the value on line 2 is never used)`}},
		{"block scope is separate from the top level", "typing 1ms\nscene s {\n  typing 2ms\n  run a\n}\n", nil},
		{"misplaced setting is not also a duplicate", "run a\nsize 1x1\nsize 2x2\n", []string{
			`2:1: "size" must appear before the first step (first step: line 1)`,
			`3:1: "size" must appear before the first step (first step: line 1)`}},
	})
}

func TestValidateSettingValues(t *testing.T) {
	cases := []struct{ line, want string }{
		{"size 80x24", ""},
		{"size 1000x1000", ""},
		{"size 1x1", ""},
		{"size 0x24", `1:1: bad size "0x24" (columns must be 1 to 1000)`},
		{"size 1001x24", `1:1: bad size "1001x24" (columns must be 1 to 1000)`},
		{"size 80x0", `1:1: bad size "80x0" (rows must be 1 to 1000)`},
		{"size 80x1001", `1:1: bad size "80x1001" (rows must be 1 to 1000)`},
		{"size 99999999999999999999x1", `1:1: bad size "99999999999999999999x1" (columns must be 1 to 1000)`},
		{"size 80", `1:1: bad size "80" (write COLSxROWS, for example 80x24)`},
		{"size x24", `1:1: bad size "x24" (write COLSxROWS, for example 80x24)`},
		{"size 80x", `1:1: bad size "80x" (write COLSxROWS, for example 80x24)`},
		{"size 80x24x1", `1:1: bad size "80x24x1" (write COLSxROWS, for example 80x24)`},
		{"size -1x5", `1:1: bad size "-1x5" (write COLSxROWS, for example 80x24)`},
		{"size 8.5x5", `1:1: bad size "8.5x5" (write COLSxROWS, for example 80x24)`},
		{"seed 0", ""},
		{"seed 18446744073709551615", ""},
		{"seed 18446744073709551616", `1:1: bad seed "18446744073709551616" (a seed is a whole number, 0 or more)`},
		{"seed -1", `1:1: bad seed "-1" (a seed is a whole number, 0 or more)`},
		{"seed 1.5", `1:1: bad seed "1.5" (a seed is a whole number, 0 or more)`},
		{"seed abc", `1:1: bad seed "abc" (a seed is a whole number, 0 or more)`},
		{"jitter 0%", ""},
		{"jitter 100%", ""},
		{"jitter 12.5%", ""},
		{"jitter 100.1%", `1:1: bad jitter "100.1%" (jitter is 0% to 100%)`},
		{"jitter 101%", `1:1: bad jitter "101%" (jitter is 0% to 100%)`},
		{"jitter 25", `1:1: bad jitter "25" (a percentage is a number with %, like 25%)`},
		{"jitter abc%", `1:1: bad jitter "abc%" (a percentage is a number with %, like 25%)`},
		{"jitter -5%", `1:1: bad jitter "-5%" (a percentage is a number with %, like 25%)`},
		{"speed 8x", ""},
		{"speed 0.5x", ""},
		{"speed 0.001x", ""},
		{"speed 1000000x", ""},
		{"speed 0x", `1:1: bad speed "0x" (speed is 0.001x to 1000000x)`},
		{"speed 0.0001x", `1:1: bad speed "0.0001x" (speed is 0.001x to 1000000x)`},
		{"speed 1000001x", `1:1: bad speed "1000001x" (speed is 0.001x to 1000000x)`},
		{"speed 8", `1:1: bad speed "8" (a speed is a number with x, like 8x)`},
		{"speed fast", `1:1: bad speed "fast" (a speed is a number with x, like 8x)`},
		{"typing 0s", ""},
		{"typing 50ms", ""},
		{"typing 50", `1:1: bad duration "50" (a duration needs a unit: ms, s or m (for example 50s))`},
		{"typing 5h", `1:1: bad duration "5h" (unknown duration unit "h": use ms, s or m)`},
		{"linger 0s", ""},
		{"hold 0s", ""},
		{"settle off", ""},
		{"settle 0s", ""},
		{"settle 300ms", ""},
		{"settle on", `1:1: bad duration "on" (a duration is a number with a unit, like 500ms, 1.5s or 2m)`},
		{"timeout 15s", ""},
		{"timeout 0s", `1:1: bad timeout "0s" (timeout must be greater than 0)`},
		{"timeout off", `1:1: bad duration "off" (a duration is a number with a unit, like 500ms, 1.5s or 2m)`},
		{"idle 2s", ""},
		{"idle 0ms", `1:1: bad idle "0ms" (idle must be greater than 0)`},
		{"env A=1", ""},
		{"env _A1=", ""},
		{"env PS1=$ ", ""},
		{"env 1A=x", `1:1: bad environment variable name "1A" (a name is letters, digits and _, and does not start with a digit)`},
		{"env A-B=x", `1:1: bad environment variable name "A-B" (a name is letters, digits and _, and does not start with a digit)`},
		{"cwd /tmp", ""},
		{`cwd ""`, `1:1: cwd needs a path`},
		{"output a.shot", ""},
		{"output dir/a.shot", ""},
		{`output ""`, `1:1: output needs a path`},
		{"output a.txt", `1:1: bad output path "a.txt" (a recording file name ends in .shot)`},
		{"output a", `1:1: bad output path "a" (a recording file name ends in .shot)`},
		{"output .shot", `1:1: bad output path ".shot" (a recording file name ends in .shot)`},
		{"output dir/", `1:1: bad output path "dir/" (a recording file name ends in .shot)`},
		{"output a.SHOT", `1:1: bad output path "a.SHOT" (a recording file name ends in .shot)`},
		{"shell /bin/sh", ""},
		{"shell /bin/bash --noprofile --norc", ""},
		{`shell /bin/sh -c ""`, ""},
		{`shell ""`, `1:1: shell needs the path of a program (for example shell /bin/bash --noprofile --norc)`},
		{`shell "/bin/s\ex"`, `1:1: shell path contains control character U+001B`},
		{`shell /bin/sh "a\e"`, `1:1: shell argument 1 contains control character U+001B`},
		{`shell /bin/sh a "b\x00"`, `1:1: shell argument 2 contains control character U+0000`},
		{`shell /bin/sh "\xff"`, `1:1: shell argument 1 is not valid UTF-8`},
		{`shell /bin/sh "a	b"`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.line, func(t *testing.T) {
			var want []string
			if tc.want != "" {
				want = []string{tc.want}
			}
			// Every setting is valid in the preamble.
			runValidateCases(t, []validateCase{{"preamble", tc.line + "\nrun a\n", want}})
		})
	}
}

func TestValidateStructure(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"scene with hidden", "scene a {\n  hidden {\n    run x\n  }\n  run y\n}\n", nil},
		{"scene in scene", "scene intro {\n  run x\n  scene inner {\n    run y\n  }\n}\n", []string{
			`3:3: "scene" cannot be nested inside scene "intro" (blocks nest one level at most: only hidden may sit inside a scene)`}},
		{"scene in hidden", "hidden {\n  scene s {\n    run y\n  }\n}\n", []string{
			`2:3: "scene" cannot be nested inside hidden (blocks nest one level at most: only hidden may sit inside a scene)`}},
		{"hidden in hidden", "hidden {\n  hidden {\n    run y\n  }\n}\n", []string{
			`2:3: "hidden" cannot be nested inside hidden (blocks nest one level at most: only hidden may sit inside a scene)`}},
		{"hidden in hidden in scene", "scene s {\n  hidden {\n    hidden {\n      run y\n    }\n  }\n}\n", []string{
			`3:5: "hidden" cannot be nested inside hidden (blocks nest one level at most: only hidden may sit inside a scene)`}},
		{"illegal nesting is not entered", "scene a {\n  scene b {\n    size 1x1\n  }\n  run x\n}\n", []string{
			`2:3: "scene" cannot be nested inside scene "a" (blocks nest one level at most: only hidden may sit inside a scene)`}},
		{"empty scene", "scene a {\n}\n", []string{`1:1: scene "a" has no steps (a block must contain at least one step)`}},
		{"empty hidden", "hidden {\n}\n", []string{`1:1: hidden block has no steps (a block must contain at least one step)`}},
		{"settings only", "scene a {\n  typing 1ms\n}\n", []string{`1:1: scene "a" has no steps (a block must contain at least one step)`}},
		{"empty hidden in scene", "scene a {\n  run x\n  hidden {\n  }\n}\n", []string{`3:3: hidden block has no steps (a block must contain at least one step)`}},
		{"unique scene names", "scene a {\n  run x\n}\nscene b {\n  run x\n}\nscene a {\n  run x\n}\n", []string{
			`7:1: duplicate scene name "a" (first used on line 1)`}},
		{"scene names", "scene a_b-c9 {\n  run x\n}\nscene 9x {\n  run x\n}\nscene A {\n  run x\n}\n", nil},
		{"scene name starting with dash", "scene -a {\n  run x\n}\n", []string{`1:1: bad scene name "-a" (a scene name is letters, digits, _ and -, written bare)`}},
		{"scene name starting with underscore", "scene _a {\n  run x\n}\n", []string{`1:1: bad scene name "_a" (a scene name is letters, digits, _ and -, written bare)`}},
		{"scene name with a dot", "scene a.b {\n  run x\n}\n", []string{`1:1: bad scene name "a.b" (a scene name is letters, digits, _ and -, written bare)`}},
		{"scene name with a non-ASCII letter", "scene é {\n  run x\n}\n", []string{`1:1: bad scene name "é" (a scene name is letters, digits, _ and -, written bare)`}},
		{"empty script", "", nil},
		{"settings only script", "size 1x1\n", nil},
	})
}

func TestValidateHandBuilt(t *testing.T) {
	step := Step{Pos: Pos{Line: 9, Col: 1}, Kind: StepRun, Form: FormRaw, Text: "x"}
	cases := []struct {
		name string
		s    *Script
		want []string
	}{
		{"nil script", nil, nil},
		{"unknown setting", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "typin", Value: "1ms"}}, Items: []Item{step}}, []string{
			`1:1: unknown command "typin" (did you mean "typing"?)`}},
		{"reserved setting name", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "expect", Value: "x"}}, Items: []Item{step}}, []string{
			`1:1: "expect" is reserved for future use (to assert that text is on the screen, use wait "text" 0s)`}},
		{"unknown block", &Script{Items: []Item{Block{Pos: Pos{2, 1, 0}, Kind: "scen", Name: "a", Items: []Item{step}}}}, []string{
			`2:1: unknown command "scen" (did you mean "scene"?)`}},
		{"unknown step", &Script{Items: []Item{Step{Pos: Pos{3, 1, 0}, Kind: "rn", Text: "x"}}}, []string{
			`3:1: unknown command "rn" (did you mean "run"?)`}},
		{"pointer items", &Script{Items: []Item{&step, &Block{Pos: Pos{5, 1, 0}, Kind: BlockHidden, Items: []Item{&step}}, (*Step)(nil), (*Block)(nil)}}, nil},
		{"hidden with a name", &Script{Items: []Item{Block{Pos: Pos{2, 1, 0}, Kind: BlockHidden, Name: "x", Items: []Item{step}}}}, []string{
			`2:1: hidden takes no name (write hidden {)`}},
		{"scene without a name", &Script{Items: []Item{Block{Pos: Pos{2, 1, 0}, Kind: BlockScene, Items: []Item{step}}}}, []string{
			`2:1: scene needs a name (write scene NAME {)`}},
		{"setting after a step with At past the end", &Script{
			Settings: []Setting{{Pos: Pos{4, 1, 0}, Name: "size", Value: "1x1", At: 99}},
			Items:    []Item{step}}, []string{
			`4:1: "size" must appear before the first step (first step: line 9)`}},
		{"empty value", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "size"}}, Items: []Item{step}}, []string{
			`1:1: bad size "" (write COLSxROWS, for example 80x24)`}},
		{"shell without argv", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "shell", Value: "/bin/sh"}}, Items: []Item{step}}, []string{
			`1:1: shell needs the path of a program (for example shell /bin/bash --noprofile --norc)`}},
		{"env value with NUL", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "env", Value: "A=\x00"}}, Items: []Item{step}}, []string{
			`1:1: env value contains a NUL character (an environment variable cannot hold NUL)`}},
		{"env without equals", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "env", Value: "A"}}, Items: []Item{step}}, []string{
			`1:1: bad env "A" (write KEY=VALUE)`}},
		{"env value with invalid UTF-8", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "env", Value: "A=\xff"}}, Items: []Item{step}}, []string{
			`1:1: env value is not valid UTF-8 (a "\xHH" escape must form valid UTF-8 together with its neighbours; the file itself must be UTF-8)`}},
		{"cwd with a control character", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "cwd", Value: "/a\nb"}}, Items: []Item{step}}, []string{
			`1:1: cwd path contains control character U+000A`}},
		{"output with NUL", &Script{Settings: []Setting{{Pos: Pos{1, 1, 0}, Name: "output", Value: "a\x00.shot"}}, Items: []Item{step}}, []string{
			`1:1: output path contains control character U+0000`}},
		{"misplaced setting still gets its value checked", &Script{
			Settings: []Setting{{Pos: Pos{4, 1, 0}, Name: "size", Value: "0x0", At: 1}},
			Items:    []Item{step}}, []string{
			`4:1: "size" must appear before the first step (first step: line 9)`,
			`4:1: bad size "0x0" (columns must be 1 to 1000)`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderDiags(Validate(tc.s))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("got\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(tc.want, "\n  "))
			}
		})
	}
}

func TestValidateSortedAndLimited(t *testing.T) {
	var b strings.Builder
	b.WriteString("run a\n")
	for range 15 {
		b.WriteString("size 0x0\n")
	}
	s, diags := ParseString("t", b.String())
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	got := Validate(s)
	if len(got) != MaxDiagnostics {
		t.Fatalf("got %d diagnostics, want %d", len(got), MaxDiagnostics)
	}
	for i := 1; i < len(got); i++ {
		if got[i].Pos.Line < got[i-1].Pos.Line {
			t.Fatalf("not sorted: %v", renderDiags(got))
		}
	}
	// The first ten by position are kept, not the first ten found.
	if got[0].Pos.Line != 2 || got[len(got)-1].Pos.Line != 6 {
		t.Fatalf("lines %d..%d", got[0].Pos.Line, got[len(got)-1].Pos.Line)
	}
}

func TestValidateSortsAcrossScopes(t *testing.T) {
	// Settings are checked before items, but the result is in source order.
	src := "run a\nscene s {\n  scene t {\n    run b\n  }\n  size 1x1\n  run c\n}\nsize 9x9\n"
	s, diags := ParseString("t", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	got := renderDiags(Validate(s))
	want := []string{
		`3:3: "scene" cannot be nested inside scene "s" (blocks nest one level at most: only hidden may sit inside a scene)`,
		`6:3: "size" must appear before the first step (first step: line 1) (a process setting cannot be set in scene "s")`,
		`9:1: "size" must appear before the first step (first step: line 1)`,
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got\n  %s", strings.Join(got, "\n  "))
	}
}

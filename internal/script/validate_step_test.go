package script

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/michaellorant/shellshot/internal/script/keys"
)

const (
	controlHintText = `a quoted string can hold control characters, for example "\e[1m"`
	emptyTextHint   = `to press Enter alone use "press enter"`
)

func TestValidateText(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"raw", "run echo hi\ttab\ntype x\n", nil},
		{"quoted with escapes", "run \"\\e[1mhi\\n\\x00\"\ntype \"\\xc3\\xa9\"\n", nil},
		{"empty quoted run", "run \"\"\n", []string{`1:1: run text is empty (` + emptyTextHint + `)`}},
		{"empty quoted type", "scene s {\n  type \"\"\n}\n", []string{`2:3: type text is empty (` + emptyTextHint + `)`}},
		{"control character in raw text", "run a\x1bb\n", []string{`1:1: run text contains control character U+001B (` + controlHintText + `)`}},
		{"DEL in raw text", "type a\x7fb\n", []string{`1:1: type text contains control character U+007F (` + controlHintText + `)`}},
		{"C1 control in raw text", "type a\u0085b\n", []string{`1:1: type text contains control character U+0085 (` + controlHintText + `)`}},
		{"invalid UTF-8 from a quoted escape", "run \"\\xff\"\n", []string{`1:1: run text is not valid UTF-8 (` + utf8Hint + `)`}},
		{"split UTF-8 escape is valid", "run \"\\xe2\\x82\\xac\"\n", nil},
		{"text block", "run lines {\n  a\n\tb\n\n  c\n}\ntype paragraphs {\n  one\n  two\n}\n", nil},
		{"control character in a text block", "run lines {\n  a\x01\n}\n", []string{`1:1: run text contains control character U+0001 (` + controlHintText + `)`}},
		{"control character in paragraphs", "type paragraphs {\n  a\x1b[0m\n}\n", []string{`1:1: type text contains control character U+001B (` + controlHintText + `)`}},
		{"text at the size limit", "run " + strings.Repeat("a", maxTextBytes) + "\n", nil},
		{"text over the size limit", "type " + strings.Repeat("a", maxTextBytes+1) + "\n", []string{`1:1: type text is too long: 65537 bytes (the limit is 65536 bytes; split it into several steps)`}},
		{"text block over the size limit", "run lines {\n" + strings.Repeat(strings.Repeat("a", 99)+"\n", 700) + "}\n", []string{`1:1: run text is too long: 70000 bytes (the limit is 65536 bytes; split it into several steps)`}},
	})
}

func TestValidateMark(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"labels", "mark done\nmark \"two words\"\nmark \"a\\tb\"\n", nil},
		{"empty label", "mark \"\"\n", []string{`1:1: mark label is empty (for example mark done)`}},
		{"control character", "mark \"a\\e\"\n", []string{`1:1: mark label contains control character U+001B (` + controlHintText + `)`}},
		{"invalid UTF-8", "mark \"\\xff\"\n", []string{`1:1: mark label is not valid UTF-8 (` + utf8Hint + `)`}},
	})
}

func TestValidateWait(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"valid forms", "wait \"x\"\nwait /a+b/ 5s\nwait \"x\" 0s\nwait /^\\$$/\nwait idle\nwait idle 1s\nwait \"a\\nb\"\nwait /a\\/b/\n", nil},
		{"empty string", "wait \"\"\n", []string{`1:1: wait pattern is empty (an empty pattern matches every screen; write the text to wait for)`}},
		{"invalid UTF-8 string", "wait \"\\xff\"\n", []string{`1:1: wait pattern is not valid UTF-8 (` + utf8Hint + `)`}},
		{"invalid UTF-8 regex", "wait /\\xff/\n", nil}, // the regex text holds the backslash sequence, which RE2 reads as an escape
		{"missing paren", "wait /(/\n", []string{`1:1: bad regular expression /(/ (missing closing ))`}},
		{"extra paren", "wait /a)/\n", []string{`1:1: bad regular expression /a)/ (unexpected ))`}},
		{"missing bracket", "wait /x[a/\n", []string{`1:1: bad regular expression /x[a/ (missing closing ] near "[a")`}},
		{"bad repeat", "wait /a{2,1}/\n", []string{`1:1: bad regular expression /a{2,1}/ (invalid repeat count near "{2,1}")`}},
		{"repeat without operand", "wait /*a/\n", []string{`1:1: bad regular expression /*a/ (missing argument to repetition operator near "*")`}},
		{"nested repeat", "wait /a**/\n", []string{`1:1: bad regular expression /a**/ (invalid nested repetition operator near "**")`}},
		{"bad escape", "wait /a\\q/\n", []string{`1:1: bad regular expression /a\q/ (invalid escape sequence near "\q")`}},
		{"lookahead is not RE2", "wait /a(?=b)/\n", []string{`1:1: bad regular expression /a(?=b)/ (invalid or unsupported Perl syntax near "(?=")`}},
		{"nested repeat count", "wait /((a{999}){999}){999}/\n", []string{`1:1: bad regular expression /((a{999}){999}){999}/ (invalid repeat count near "{999}")`}},
		{"idle with zero duration", "wait idle 0s\n", []string{`1:1: bad duration "0s" (wait idle needs a duration greater than 0)`}},
		{"in a block", "scene s {\n  run a\n  wait /(/ 1s\n}\n", []string{`3:3: bad regular expression /(/ (missing closing ))`}},
	})
}

func TestValidateLongRegexIsShortened(t *testing.T) {
	src := "(" + strings.Repeat("a", 100)
	s := &Script{Items: []Item{Step{Pos: Pos{1, 1, 0}, Kind: StepWait, Pattern: &Pattern{Regex: true, Source: src}}}}
	got := renderDiags(Validate(s))
	want := "1:1: bad regular expression /(" + strings.Repeat("a", 39) + ".../ (missing closing ))"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestValidatePressAndSleep(t *testing.T) {
	runValidateCases(t, []validateCase{
		{"valid", "press enter\npress ctrl+c\npress up up enter\npress down x3\npress left x999\npress a x1\nsleep 0s\nsleep 1.5s\n", nil},
	})
	pos := Pos{Line: 4, Col: 2}
	enter := keys.Chord{Key: keys.KeyEnter}
	cases := []struct {
		name string
		step Step
		want string
	}{
		{"press without keys", Step{Pos: pos, Kind: StepPress}, `4:2: press needs a key (for example press enter, press ctrl+c or press down x3)`},
		{"repeat zero", Step{Pos: pos, Kind: StepPress, Keys: []KeyPress{{enter, 0}}}, `4:2: repeat count "x0" is out of range (a repeat is x1 to x999)`},
		{"repeat 1000", Step{Pos: pos, Kind: StepPress, Keys: []KeyPress{{enter, 1}, {enter, 1000}}}, `4:2: repeat count "x1000" is out of range (a repeat is x1 to x999)`},
		{"negative repeat", Step{Pos: pos, Kind: StepPress, Keys: []KeyPress{{enter, -1}}}, `4:2: repeat count "x-1" is out of range (a repeat is x1 to x999)`},
		{"zero chord", Step{Pos: pos, Kind: StepPress, Keys: []KeyPress{{keys.Chord{}, 1}}}, `4:2: press has a key that is not valid (key names are written like enter, ctrl+c or f5)`},
		{"unknown key", Step{Pos: pos, Kind: StepPress, Keys: []KeyPress{{keys.Chord{Key: 9999}, 1}}}, `4:2: press has a key that is not valid (key names are written like enter, ctrl+c or f5)`},
		{"unknown modifier", Step{Pos: pos, Kind: StepPress, Keys: []KeyPress{{keys.Chord{Key: keys.KeyEnter, Mods: 64}, 1}}}, `4:2: press has a key that is not valid (key names are written like enter, ctrl+c or f5)`},
		{"sleep without duration", Step{Pos: pos, Kind: StepSleep}, `4:2: sleep needs a duration (for example sleep 500ms)`},
		{"negative sleep", Step{Pos: pos, Kind: StepSleep, HasDuration: true, Duration: -time.Second}, `4:2: bad duration "-1s" (a duration cannot be negative)`},
		{"negative wait timeout", Step{Pos: pos, Kind: StepWait, Pattern: &Pattern{Source: "x"}, HasDuration: true, Duration: -time.Second}, `4:2: bad duration "-1s" (a duration cannot be negative)`},
		{"negative wait idle", Step{Pos: pos, Kind: StepWait, Idle: true, HasDuration: true, Duration: -time.Second}, `4:2: bad duration "-1s" (wait idle needs a duration greater than 0)`},
		{"wait without pattern", Step{Pos: pos, Kind: StepWait}, `4:2: wait needs a pattern (write wait "text", wait /regex/ or wait idle)`},
		{"wait idle with pattern", Step{Pos: pos, Kind: StepWait, Idle: true, Pattern: &Pattern{Source: "x"}}, `4:2: wait idle takes no pattern`},
		{"empty regex", Step{Pos: pos, Kind: StepWait, Pattern: &Pattern{Regex: true}}, `4:2: wait pattern is empty (an empty pattern matches every screen; write the text to wait for)`},
		{"empty run text in a block form", Step{Pos: pos, Kind: StepRun, Form: FormLines}, `4:2: run text is empty (` + emptyTextHint + `)`},
		{"line feed in raw text", Step{Pos: pos, Kind: StepType, Form: FormRaw, Text: "a\nb"}, `4:2: type text contains control character U+000A (` + controlHintText + `)`},
		{"line feed in a block is Enter", Step{Pos: pos, Kind: StepType, Form: FormParagraphs, Text: "a\n\nb"}, ``},
		{"carriage return in a block", Step{Pos: pos, Kind: StepType, Form: FormLines, Text: "a\r\nb"}, `4:2: type text contains control character U+000D (` + controlHintText + `)`},
		{"empty mark", Step{Pos: pos, Kind: StepMark}, `4:2: mark label is empty (for example mark done)`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := renderDiags(Validate(&Script{Items: []Item{tc.step}}))
			var want []string
			if tc.want != "" {
				want = []string{tc.want}
			}
			if !slices.Equal(got, want) {
				t.Fatalf("got %q\nwant %q", got, want)
			}
		})
	}
}

func TestValidateReportsAllKindsTogether(t *testing.T) {
	src := "run \"\"\nwait /(/\nscene a {\n}\nscene a {\n  run x\n}\nsleep 1s\nspeed 2x\n"
	s, diags := ParseString("t", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	want := []string{
		`1:1: run text is empty (` + emptyTextHint + `)`,
		`2:1: bad regular expression /(/ (missing closing ))`,
		`3:1: scene "a" has no steps (a block must contain at least one step)`,
		`5:1: duplicate scene name "a" (first used on line 3)`,
		`9:1: "speed" must appear before the first step or inside a scene (first step: line 1)`,
	}
	if got := renderDiags(Validate(s)); !slices.Equal(got, want) {
		t.Fatalf("got\n  %s", strings.Join(got, "\n  "))
	}
}

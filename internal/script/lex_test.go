package script

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// render formats tokens as `KIND line:col "text"` (no text for NEWLINE and
// EOF), so tables can state exact kinds, positions and values.
func render(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		switch t.Kind {
		case KindNewline, KindEOF:
			out[i] = fmt.Sprintf("%s %d:%d", t.Kind, t.Pos.Line, t.Pos.Col)
		default:
			out[i] = fmt.Sprintf("%s %d:%d %q", t.Kind, t.Pos.Line, t.Pos.Col, t.Text)
		}
	}
	return out
}

func renderDiags(ds []Diagnostic) []string {
	out := make([]string, len(ds))
	for i, d := range ds {
		out[i] = d.Error()
	}
	return out
}

type lexCase struct {
	name  string
	src   string
	toks  []string
	diags []string
}

func runLexCases(t *testing.T, cases []lexCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toks, diags := Tokens(tc.src)
			if got := render(toks); !reflect.DeepEqual(got, tc.toks) {
				t.Errorf("tokens:\n got  %q\n want %q", got, tc.toks)
			}
			if got := renderDiags(diags); !reflect.DeepEqual(got, orEmpty(tc.diags)) {
				t.Errorf("diagnostics:\n got  %q\n want %q", got, tc.diags)
			}
		})
	}
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func TestLexRunTypeText(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "rest of line",
			src:  "run npm install\n",
			toks: []string{`WORD 1:1 "run"`, `TEXT 1:5 "npm install"`, `NEWLINE 1:16`, `EOF 2:1`},
		},
		{
			name: "type is the same",
			src:  "type ls -l\n",
			toks: []string{`WORD 1:1 "type"`, `TEXT 1:6 "ls -l"`, `NEWLINE 1:11`, `EOF 2:1`},
		},
		{
			name: "indent, extra spaces, tabs and trailing space trimmed",
			src:  "  run \t git status \t \n",
			toks: []string{`WORD 1:3 "run"`, `TEXT 1:9 "git status"`, `NEWLINE 1:22`, `EOF 2:1`},
		},
		{
			name: "hash, backslash and inner quotes are literal",
			src:  "run echo \"a\" # b \\n \\q\n",
			toks: []string{`WORD 1:1 "run"`, `TEXT 1:5 "echo \"a\" # b \\n \\q"`, `NEWLINE 1:23`, `EOF 2:1`},
		},
		{
			name: "text may start with a hash",
			src:  "run # not a comment\n",
			toks: []string{`WORD 1:1 "run"`, `TEXT 1:5 "# not a comment"`, `NEWLINE 1:20`, `EOF 2:1`},
		},
		{
			name: "text may start with a brace or slash",
			src:  "type {\nrun /bin/ls\n",
			toks: []string{
				`WORD 1:1 "type"`, `TEXT 1:6 "{"`, `NEWLINE 1:7`,
				`WORD 2:1 "run"`, `TEXT 2:5 "/bin/ls"`, `NEWLINE 2:12`, `EOF 3:1`,
			},
		},
		{
			name: "no operand",
			src:  "run\ntype   \n",
			toks: []string{`WORD 1:1 "run"`, `NEWLINE 1:4`, `WORD 2:1 "type"`, `NEWLINE 2:8`, `EOF 3:1`},
		},
		{
			name: "keyword prefix is not run",
			src:  "running fast\n",
			toks: []string{`WORD 1:1 "running"`, `WORD 1:9 "fast"`, `NEWLINE 1:13`, `EOF 2:1`},
		},
		{
			name: "block-like text is plain text outside the exact header",
			src:  "run lines\nrun line {\nrun lines { x\n",
			toks: []string{
				`WORD 1:1 "run"`, `TEXT 1:5 "lines"`, `NEWLINE 1:10`,
				`WORD 2:1 "run"`, `TEXT 2:5 "line {"`, `NEWLINE 2:11`,
				`WORD 3:1 "run"`, `TEXT 3:5 "lines { x"`, `NEWLINE 3:14`,
				`EOF 4:1`,
			},
		},
	})
}

func TestLexRunTypeString(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "plain string",
			src:  "run \"echo hi\"\n",
			toks: []string{`WORD 1:1 "run"`, `STRING 1:5 "echo hi"`, `NEWLINE 1:14`, `EOF 2:1`},
		},
		{
			name: "every escape decoded",
			src:  `type "a\nb\tc\"d\\e\ef\x41\x7F"` + "\n",
			toks: []string{
				`WORD 1:1 "type"`, `STRING 1:6 "a\nb\tc\"d\\e\x1bfA\x7f"`, `NEWLINE 1:32`, `EOF 2:1`,
			},
		},
		{
			name: "leading and trailing spaces kept, trailing comment",
			src:  "run \"  hi  \"   # why\n",
			toks: []string{
				`WORD 1:1 "run"`, `STRING 1:5 "  hi  "`, `COMMENT 1:16 "# why"`, `NEWLINE 1:21`, `EOF 2:1`,
			},
		},
		{
			name: "comment directly after the quote",
			src:  "run \"hi\"#c\n",
			toks: []string{`WORD 1:1 "run"`, `STRING 1:5 "hi"`, `COMMENT 1:9 "#c"`, `NEWLINE 1:11`, `EOF 2:1`},
		},
		{
			name:  "text after the closing quote",
			src:   "run \"hi\" | wc\n",
			toks:  []string{`WORD 1:1 "run"`, `STRING 1:5 "hi"`, `NEWLINE 1:14`, `EOF 2:1`},
			diags: []string{`1:10: text after closing quote (to type a leading quote use run "\"hi\" | wc")`},
		},
		{
			name:  "text glued to the closing quote",
			src:   "type \"a\"b\n",
			toks:  []string{`WORD 1:1 "type"`, `STRING 1:6 "a"`, `NEWLINE 1:10`, `EOF 2:1`},
			diags: []string{`1:9: text after closing quote (to type a leading quote use type "\"hi\" | wc")`},
		},
		{
			name:  "unterminated string",
			src:   "run \"abc\nrun x\n",
			toks:  []string{`WORD 1:1 "run"`, `ILLEGAL 1:5 "\"abc"`, `NEWLINE 1:9`, `WORD 2:1 "run"`, `TEXT 2:5 "x"`, `NEWLINE 2:6`, `EOF 3:1`},
			diags: []string{`1:5: unterminated string (add a closing quote)`},
		},
		{
			name:  "backslash at end of line",
			src:   "run \"abc\\\n",
			toks:  []string{`WORD 1:1 "run"`, `ILLEGAL 1:5 "\"abc\\"`, `NEWLINE 1:10`, `EOF 2:1`},
			diags: []string{`1:5: unterminated string (add a closing quote)`},
		},
		{
			name:  "short hex escape",
			src:   `type "\x4"` + "\n",
			toks:  []string{`WORD 1:1 "type"`, `ILLEGAL 1:6 "\"\\x4\""`, `NEWLINE 1:11`, `EOF 2:1`},
			diags: []string{`1:7: invalid escape "\x" (\x needs two hex digits, for example \x1b)`},
		},
		{
			name:  "hex escape before the closing quote",
			src:   `type "\xg1"` + "\n",
			toks:  []string{`WORD 1:1 "type"`, `ILLEGAL 1:6 "\"\\xg1\""`, `NEWLINE 1:12`, `EOF 2:1`},
			diags: []string{`1:7: invalid escape "\x" (\x needs two hex digits, for example \x1b)`},
		},
		{
			name:  "unknown escape with a multibyte rune",
			src:   "run \"\\é\"\n",
			toks:  []string{`WORD 1:1 "run"`, `ILLEGAL 1:5 "\"\\é\""`, `NEWLINE 1:10`, `EOF 2:1`},
			diags: []string{`1:6: unknown escape "\é" (valid escapes are \n \t \" \\ \e \xHH)`},
		},
	})
}

func TestLexWait(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "string and duration",
			src:  "wait \"passed\" 0s\n",
			toks: []string{`WORD 1:1 "wait"`, `STRING 1:6 "passed"`, `WORD 1:15 "0s"`, `NEWLINE 1:17`, `EOF 2:1`},
		},
		{
			name: "regex with timeout and comment",
			src:  "wait /^[0-9a-f]{7,} .*$/ 10s # anchored\n",
			toks: []string{
				`WORD 1:1 "wait"`, `REGEX 1:6 "^[0-9a-f]{7,} .*$"`, `WORD 1:26 "10s"`,
				`COMMENT 1:30 "# anchored"`, `NEWLINE 1:40`, `EOF 2:1`,
			},
		},
		{
			name: "escaped slash is a slash, other backslashes kept",
			src:  `wait /a\/b\.c\\/` + "\n",
			toks: []string{`WORD 1:1 "wait"`, `REGEX 1:6 "a/b\\.c\\\\"`, `NEWLINE 1:17`, `EOF 2:1`},
		},
		{
			name: "hash and quote inside a regex",
			src:  "wait /#\"x/\n",
			toks: []string{`WORD 1:1 "wait"`, `REGEX 1:6 "#\"x"`, `NEWLINE 1:11`, `EOF 2:1`},
		},
		{
			name: "idle with duration",
			src:  "wait idle 500ms\nwait idle\n",
			toks: []string{
				`WORD 1:1 "wait"`, `WORD 1:6 "idle"`, `WORD 1:11 "500ms"`, `NEWLINE 1:16`,
				`WORD 2:1 "wait"`, `WORD 2:6 "idle"`, `NEWLINE 2:10`, `EOF 3:1`,
			},
		},
		{
			name: "only the first argument is a regex",
			src:  "wait idle /x/\n",
			toks: []string{`WORD 1:1 "wait"`, `WORD 1:6 "idle"`, `WORD 1:11 "/x/"`, `NEWLINE 1:14`, `EOF 2:1`},
		},
		{
			name: "other keywords never scan a regex",
			src:  "cwd /tmp/\nmark /x/\n",
			toks: []string{
				`WORD 1:1 "cwd"`, `WORD 1:5 "/tmp/"`, `NEWLINE 1:10`,
				`WORD 2:1 "mark"`, `WORD 2:6 "/x/"`, `NEWLINE 2:9`, `EOF 3:1`,
			},
		},
		{
			name:  "empty regex",
			src:   "wait //\n",
			toks:  []string{`WORD 1:1 "wait"`, `ILLEGAL 1:6 "//"`, `NEWLINE 1:8`, `EOF 2:1`},
			diags: []string{`1:6: empty regex (write a pattern between the slashes, or use a quoted string)`},
		},
		{
			name:  "unterminated regex",
			src:   "wait /abc\\/ 5s\n",
			toks:  []string{`WORD 1:1 "wait"`, `ILLEGAL 1:6 "/abc\\/ 5s"`, `NEWLINE 1:15`, `EOF 2:1`},
			diags: []string{`1:6: unterminated regex (add a closing "/" (a slash inside the pattern is written \/))`},
		},
		{
			name:  "backslash ends the regex line",
			src:   "wait /abc\\\n",
			toks:  []string{`WORD 1:1 "wait"`, `ILLEGAL 1:6 "/abc\\"`, `NEWLINE 1:11`, `EOF 2:1`},
			diags: []string{`1:6: unterminated regex (add a closing "/" (a slash inside the pattern is written \/))`},
		},
		{
			name:  "regex flags",
			src:   "wait /abc/i 5s\n",
			toks:  []string{`WORD 1:1 "wait"`, `REGEX 1:6 "abc"`, `WORD 1:13 "5s"`, `NEWLINE 1:15`, `EOF 2:1`},
			diags: []string{`1:11: text after closing slash (regex flags are not supported; start the pattern with (?i) instead)`},
		},
		{
			name:  "text after the string",
			src:   "wait \"x\"y\n",
			toks:  []string{`WORD 1:1 "wait"`, `STRING 1:6 "x"`, `NEWLINE 1:10`, `EOF 2:1`},
			diags: []string{`1:9: text after closing quote (put a space or a comment after the closing quote)`},
		},
		{
			name: "wait with no argument",
			src:  "wait\n",
			toks: []string{`WORD 1:1 "wait"`, `NEWLINE 1:5`, `EOF 2:1`},
		},
	})
}

func TestLexSettings(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "typed literals are words",
			src:  "size 100x28\ntyping 50ms\njitter 25%\nspeed 8x\nsettle off\nseed 42\n",
			toks: []string{
				`WORD 1:1 "size"`, `WORD 1:6 "100x28"`, `NEWLINE 1:12`,
				`WORD 2:1 "typing"`, `WORD 2:8 "50ms"`, `NEWLINE 2:12`,
				`WORD 3:1 "jitter"`, `WORD 3:8 "25%"`, `NEWLINE 3:11`,
				`WORD 4:1 "speed"`, `WORD 4:7 "8x"`, `NEWLINE 4:9`,
				`WORD 5:1 "settle"`, `WORD 5:8 "off"`, `NEWLINE 5:11`,
				`WORD 6:1 "seed"`, `WORD 6:6 "42"`, `NEWLINE 6:8`,
				`EOF 7:1`,
			},
		},
		{
			name: "hash starts a comment only at a token start",
			src:  "cwd /tmp/a#b # real comment\n",
			toks: []string{`WORD 1:1 "cwd"`, `WORD 1:5 "/tmp/a#b"`, `COMMENT 1:14 "# real comment"`, `NEWLINE 1:28`, `EOF 2:1`},
		},
		{
			name: "quoted path",
			src:  "output \"my demo.shot\"\n",
			toks: []string{`WORD 1:1 "output"`, `STRING 1:8 "my demo.shot"`, `NEWLINE 1:22`, `EOF 2:1`},
		},
		{
			name: "mark and press",
			src:  "mark done\npress ctrl+c up x3 # go\n",
			toks: []string{
				`WORD 1:1 "mark"`, `WORD 1:6 "done"`, `NEWLINE 1:10`,
				`WORD 2:1 "press"`, `WORD 2:7 "ctrl+c"`, `WORD 2:14 "up"`, `WORD 2:17 "x3"`,
				`COMMENT 2:20 "# go"`, `NEWLINE 2:24`, `EOF 3:1`,
			},
		},
		{
			name: "unknown commands are words",
			src:  "rn echo\n",
			toks: []string{`WORD 1:1 "rn"`, `WORD 1:4 "echo"`, `NEWLINE 1:8`, `EOF 2:1`},
		},
		{
			name:  "text after a string argument",
			src:   "mark \"a\"b c\n",
			toks:  []string{`WORD 1:1 "mark"`, `STRING 1:6 "a"`, `WORD 1:11 "c"`, `NEWLINE 1:12`, `EOF 2:1`},
			diags: []string{`1:9: text after closing quote (put a space or a comment after the closing quote)`},
		},
	})
}

func TestLexEnv(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "word value",
			src:  "env CI=1\n",
			toks: []string{`WORD 1:1 "env"`, `WORD 1:5 "CI"`, `ASSIGN 1:7 "="`, `WORD 1:8 "1"`, `NEWLINE 1:9`, `EOF 2:1`},
		},
		{
			name: "string value with escape",
			src:  `env PS1="\\w $ "` + "\n",
			toks: []string{`WORD 1:1 "env"`, `WORD 1:5 "PS1"`, `ASSIGN 1:8 "="`, `STRING 1:9 "\\w $ "`, `NEWLINE 1:17`, `EOF 2:1`},
		},
		{
			name: "empty value",
			src:  "env EMPTY=\nenv EMPTY= # c\n",
			toks: []string{
				`WORD 1:1 "env"`, `WORD 1:5 "EMPTY"`, `ASSIGN 1:10 "="`, `NEWLINE 1:11`,
				`WORD 2:1 "env"`, `WORD 2:5 "EMPTY"`, `ASSIGN 2:10 "="`, `COMMENT 2:12 "# c"`, `NEWLINE 2:15`, `EOF 3:1`,
			},
		},
		{
			name: "equals and quotes inside the value are literal",
			src:  "env A=b=c\"d\"\n",
			toks: []string{`WORD 1:1 "env"`, `WORD 1:5 "A"`, `ASSIGN 1:6 "="`, `WORD 1:7 "b=c\"d\""`, `NEWLINE 1:13`, `EOF 2:1`},
		},
		{
			name: "hash right after equals is a comment",
			src:  "env A=#x\n",
			toks: []string{`WORD 1:1 "env"`, `WORD 1:5 "A"`, `ASSIGN 1:6 "="`, `COMMENT 1:7 "#x"`, `NEWLINE 1:9`, `EOF 2:1`},
		},
		{
			name: "no equals",
			src:  "env FOO\n",
			toks: []string{`WORD 1:1 "env"`, `WORD 1:5 "FOO"`, `NEWLINE 1:8`, `EOF 2:1`},
		},
		{
			name: "space after equals leaves a stray word for the parser",
			src:  "env A= b\n",
			toks: []string{`WORD 1:1 "env"`, `WORD 1:5 "A"`, `ASSIGN 1:6 "="`, `WORD 1:8 "b"`, `NEWLINE 1:9`, `EOF 2:1`},
		},
		{
			name:  "bad string value",
			src:   "env A=\"x\\q\"\n",
			toks:  []string{`WORD 1:1 "env"`, `WORD 1:5 "A"`, `ASSIGN 1:6 "="`, `ILLEGAL 1:7 "\"x\\q\""`, `NEWLINE 1:12`, `EOF 2:1`},
			diags: []string{`1:9: unknown escape "\q" (valid escapes are \n \t \" \\ \e \xHH)`},
		},
		{
			name: "env alone and with comment",
			src:  "env\nenv # c\n",
			toks: []string{`WORD 1:1 "env"`, `NEWLINE 1:4`, `WORD 2:1 "env"`, `COMMENT 2:5 "# c"`, `NEWLINE 2:8`, `EOF 3:1`},
		},
		{
			name: "missing key",
			src:  "env =x\n",
			toks: []string{`WORD 1:1 "env"`, `ASSIGN 1:5 "="`, `WORD 1:6 "x"`, `NEWLINE 1:7`, `EOF 2:1`},
		},
	})
}

func TestLexShellArgs(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "path only",
			src:  "shell /bin/sh\n",
			toks: []string{`WORD 1:1 "shell"`, `WORD 1:7 "/bin/sh"`, `NEWLINE 1:14`, `EOF 2:1`},
		},
		{
			name: "arguments split on spaces and tabs",
			src:  "shell /bin/bash  --noprofile\t--norc\n",
			toks: []string{
				`WORD 1:1 "shell"`, `WORD 1:7 "/bin/bash"`, `WORD 1:18 "--noprofile"`, `WORD 1:30 "--norc"`,
				`NEWLINE 1:36`, `EOF 2:1`,
			},
		},
		{
			name: "double quotes group and are removed",
			src:  `shell "/my dir/sh" -c "echo hi"` + "\n",
			toks: []string{
				`WORD 1:1 "shell"`, `WORD 1:7 "/my dir/sh"`, `WORD 1:20 "-c"`, `WORD 1:23 "echo hi"`,
				`NEWLINE 1:32`, `EOF 2:1`,
			},
		},
		{
			name: "quoted part inside an argument",
			src:  `shell sh --rcfile="my rc"x` + "\n",
			toks: []string{`WORD 1:1 "shell"`, `WORD 1:7 "sh"`, `WORD 1:10 "--rcfile=my rcx"`, `NEWLINE 1:27`, `EOF 2:1`},
		},
		{
			name: "empty quoted argument and escapes",
			src:  `shell sh "" "a\"b\\c\td"` + "\n",
			toks: []string{`WORD 1:1 "shell"`, `WORD 1:7 "sh"`, `WORD 1:10 ""`, `WORD 1:13 "a\"b\\c\td"`, `NEWLINE 1:25`, `EOF 2:1`},
		},
		{
			name: "outside quotes everything is literal",
			src:  "shell sh 'a b' $HOME \\n a#b # tail\n",
			toks: []string{
				`WORD 1:1 "shell"`, `WORD 1:7 "sh"`, `WORD 1:10 "'a"`, `WORD 1:13 "b'"`, `WORD 1:16 "$HOME"`,
				`WORD 1:22 "\\n"`, `WORD 1:25 "a#b"`, `COMMENT 1:29 "# tail"`, `NEWLINE 1:35`, `EOF 2:1`,
			},
		},
		{
			name:  "unterminated quote",
			src:   "shell sh \"abc\n",
			toks:  []string{`WORD 1:1 "shell"`, `WORD 1:7 "sh"`, `ILLEGAL 1:10 "\"abc"`, `NEWLINE 1:14`, `EOF 2:1`},
			diags: []string{`1:10: unterminated string (add a closing quote)`},
		},
		{
			name:  "bad escape",
			src:   `shell sh "\q"` + "\n",
			toks:  []string{`WORD 1:1 "shell"`, `WORD 1:7 "sh"`, `ILLEGAL 1:10 "\"\\q\""`, `NEWLINE 1:14`, `EOF 2:1`},
			diags: []string{`1:11: unknown escape "\q" (valid escapes are \n \t \" \\ \e \xHH)`},
		},
	})
}

func TestLexBlocks(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "scene header",
			src:  "scene intro {\n  linger 1s\n}\n",
			toks: []string{
				`WORD 1:1 "scene"`, `WORD 1:7 "intro"`, `LBRACE 1:13 "{"`, `NEWLINE 1:14`,
				`WORD 2:3 "linger"`, `WORD 2:10 "1s"`, `NEWLINE 2:12`,
				`RBRACE 3:1 "}"`, `NEWLINE 3:2`, `EOF 4:1`,
			},
		},
		{
			name: "hidden header with comment",
			src:  "hidden {   # setup\n}  # end\n",
			toks: []string{
				`WORD 1:1 "hidden"`, `LBRACE 1:8 "{"`, `COMMENT 1:12 "# setup"`, `NEWLINE 1:19`,
				`RBRACE 2:1 "}"`, `COMMENT 2:4 "# end"`, `NEWLINE 2:9`, `EOF 3:1`,
			},
		},
		{
			name: "indented closing brace and glued comment",
			src:  "  }#c\n",
			toks: []string{`RBRACE 1:3 "}"`, `COMMENT 1:4 "#c"`, `NEWLINE 1:6`, `EOF 2:1`},
		},
		{
			name: "brace is a word outside scene and hidden headers",
			src:  "mark {\nscene a{\n",
			toks: []string{
				`WORD 1:1 "mark"`, `WORD 1:6 "{"`, `NEWLINE 1:7`,
				`WORD 2:1 "scene"`, `WORD 2:7 "a{"`, `NEWLINE 2:9`, `EOF 3:1`,
			},
		},
		{
			name: "text after closing brace is left to the parser",
			src:  "} x\n",
			toks: []string{`RBRACE 1:1 "}"`, `WORD 1:3 "x"`, `NEWLINE 1:4`, `EOF 2:1`},
		},
	})
}

func TestLexLines(t *testing.T) {
	runLexCases(t, []lexCase{
		{
			name: "empty source",
			src:  "",
			toks: []string{`EOF 1:1`},
		},
		{
			name: "blank lines produce no tokens",
			src:  "\n  \t \n\nmark a\n\n",
			toks: []string{`WORD 4:1 "mark"`, `WORD 4:6 "a"`, `NEWLINE 4:7`, `EOF 6:1`},
		},
		{
			name: "comment lines and shebang",
			src:  "#!/usr/bin/env -S shellshot record --script\n  # note\n",
			toks: []string{
				`COMMENT 1:1 "#!/usr/bin/env -S shellshot record --script"`, `NEWLINE 1:44`,
				`COMMENT 2:3 "# note"`, `NEWLINE 2:9`, `EOF 3:1`,
			},
		},
		{
			name: "no final newline",
			src:  "mark a",
			toks: []string{`WORD 1:1 "mark"`, `WORD 1:6 "a"`, `NEWLINE 1:7`, `EOF 1:7`},
		},
		{
			name: "crlf",
			src:  "run echo hi\r\nmark a # c\r\n\r\n}\r\n",
			toks: []string{
				`WORD 1:1 "run"`, `TEXT 1:5 "echo hi"`, `NEWLINE 1:12`,
				`WORD 2:1 "mark"`, `WORD 2:6 "a"`, `COMMENT 2:8 "# c"`, `NEWLINE 2:11`,
				`RBRACE 4:1 "}"`, `NEWLINE 4:2`, `EOF 5:1`,
			},
		},
		{
			name: "crlf string and last line without newline",
			src:  "run \"a\"\r\nrun b\r",
			toks: []string{
				`WORD 1:1 "run"`, `STRING 1:5 "a"`, `NEWLINE 1:8`,
				`WORD 2:1 "run"`, `TEXT 2:5 "b"`, `NEWLINE 2:6`, `EOF 2:7`,
			},
		},
		{
			name:  "invalid utf-8",
			src:   "mark a\nmark \xff\n",
			toks:  []string{`WORD 1:1 "mark"`, `WORD 1:6 "a"`, `NEWLINE 1:7`, `WORD 2:1 "mark"`, `WORD 2:6 "\xff"`, `NEWLINE 2:7`, `EOF 3:1`},
			diags: []string{`2:6: invalid UTF-8 (the file must be valid UTF-8)`},
		},
	})
}

func TestLexNextAfterEOF(t *testing.T) {
	l := NewLexer("mark a\n")
	for range 3 {
		l.Next()
	}
	for range 3 {
		if tok := l.Next(); tok.Kind != KindEOF {
			t.Fatalf("Next after EOF = %v, want EOF", tok.Kind)
		}
	}
}

func TestLexDiagnosticLimit(t *testing.T) {
	src := strings.Repeat("run \"x\n", 15)
	l := NewLexer(src)
	for l.Next().Kind != KindEOF {
	}
	if got := len(l.Diagnostics()); got != MaxDiagnostics {
		t.Errorf("stored %d diagnostics, want %d", got, MaxDiagnostics)
	}
	if got := l.DroppedDiagnostics(); got != 5 {
		t.Errorf("dropped %d, want 5", got)
	}
}

// TestLexPositions checks that every token's Line, Col and Offset agree with
// the source bytes.
func TestLexPositions(t *testing.T) {
	src := "# c\r\nsize 80x24\nenv A=\"b\"\n  run echo \"é\" # x\nwait /a\\/é/ 1s\nshell /bin/sh -l\n  }\n"
	toks, diags := Tokens(src)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %q", renderDiags(diags))
	}
	for _, tok := range toks {
		if want := posAt(src, tok.Pos.Offset); want != tok.Pos {
			t.Errorf("%v %q: Pos %+v, but offset %d is %+v", tok.Kind, tok.Text, tok.Pos, tok.Pos.Offset, want)
		}
		switch tok.Kind {
		case KindWord, KindComment, KindText, KindAssign, KindLBrace, KindRBrace:
			if !strings.HasPrefix(src[tok.Pos.Offset:], tok.Text) {
				t.Errorf("%v %q at offset %d does not match the source", tok.Kind, tok.Text, tok.Pos.Offset)
			}
		}
	}
}

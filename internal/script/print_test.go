package script

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFormatGolden(t *testing.T) {
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
			checkGolden(t, strings.TrimSuffix(file, ".script")+".fmt.golden", Format(s))
		})
	}
}

// checkRoundTrip parses src, which must be accepted, and checks that Format
// gives text that parses to the same AST (without positions) and that
// formatting again changes nothing.
func checkRoundTrip(t testing.TB, src string) {
	t.Helper()
	s, diags := ParseString("f", src)
	if len(diags) > 0 {
		t.Fatalf("diagnostics: %v\nsource %q", renderDiags(diags), src)
	}
	out := Format(s)
	s2, diags2 := ParseString("f", out)
	if len(diags2) > 0 {
		t.Fatalf("formatted text has diagnostics %v\nsource %q\nformatted %q", renderDiags(diags2), src, out)
	}
	if a, b := dumpAST(s, false), dumpAST(s2, false); a != b {
		t.Fatalf("round trip changed the AST\nsource %q\nformatted %q\n--- before\n%s--- after\n%s", src, out, a, b)
	}
	if out2 := Format(s2); out2 != out {
		t.Fatalf("Format is not idempotent\nfirst  %q\nsecond %q", out, out2)
	}
}

func TestFormatRoundTripTestdata(t *testing.T) {
	for _, file := range scriptFiles(t, "testdata/*.script") {
		t.Run(filepath.Base(file), func(t *testing.T) {
			src, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			checkRoundTrip(t, string(src))
		})
	}
}

func TestFormatRoundTripAwkward(t *testing.T) {
	for _, src := range []string{
		"",
		"# only a comment\n",
		"mark \"\"\nmark \"#x\"\nmark 'q'\nmark a\"b\nmark \"a\\\\b\"\nmark \"\\x00\\x7f\\xff\\x0d\"\n",
		"cwd \"\"\ncwd \"a b\"\ncwd a#c\n",
		"env A=\"#x\"\nenv B=\"\"\nenv C=\"a b\"\nenv D=\"\\\"\"\nenv E=\\q\n",
		"shell \"\" \"#\" a#b \"a\\\"b\" x\"y z\"w\n",
		"run a\\r\r\nrun \"trailing \"\ntype \"  lead\"\nrun \"\\\"q\\\"\"\nrun # not a comment\n",
		"run x\r\r\ntype lines {\n  a\r\n  b\n}\n",
		"wait /a\\/b\\\\\\/c/ 1ms\nwait /(?m)^$/\nwait \"\\n\"\nwait \"\\x01\"\n",
		"sleep 0.000000001s\nsleep 1.5m\nwait idle 90s\nwait \"x\" 0.001s\n",
		"press x x2 x ctrl+x x5 x\n",
		"run lines {\n\n    deep\n  \n\n  }\n  }\n}\n",
		"run lines {\n\ta\n\t\tb\n}\n",
		"run paragraphs {\n  a\n  b\n\n\n\n  c\n  }\n}\n",
		"run lines {\n  }\n  x\n}\n",
		"scene a {\n  hidden {\n    run lines {\n      x\n    }\n  }\n  run lines {\n    }\n    y\n  }\n}\n",
		"hidden {\n}\nscene a {\n}\nsize 1x1\nrun x\nsize 2x2\n",
		"scene a { # c\n  typing 1s # c\n  mark x # c\n} # c\n",
		"  run   x   \n\t type  lines  {  \n\t   a\n\t }\n",
	} {
		checkRoundTrip(t, src)
	}
}

func TestFormatKeepsMeaningOfUnrepresentableText(t *testing.T) {
	out := Format(&Script{Items: []Item{
		Step{Kind: StepRun, Form: FormLines, Text: "a\rb\n"},
		Step{Kind: StepType, Form: FormRaw, Text: " x"},
		Step{Kind: StepRun, Form: FormLines, Text: "no final enter"},
		Step{Kind: StepType, Form: FormParagraphs, Text: "a\n\n\nb"},
	}})
	want := "run \"a\\x0db\\n\"\ntype \" x\"\nrun \"no final enter\"\ntype \"a\\n\\n\\nb\"\n"
	if out != want {
		t.Fatalf("got %q\nwant %q", out, want)
	}
}

func TestFormatLayout(t *testing.T) {
	const src = "size 80x24\ntyping 1s\n\n\nrun a\nrun b\nlinger 1s\nscene x {\nrun c\nhidden {\nrun d\n}\nsleep 1s\n}\nhidden {\nrun e\n}\nmark m\n"
	const want = `size 80x24
typing 1s

run a
run b

linger 1s

scene x {
  run c

  hidden {
    run d
  }

  sleep 1s
}

hidden {
  run e
}

mark m
`
	s, diags := ParseString("f", src)
	if len(diags) > 0 {
		t.Fatal(renderDiags(diags))
	}
	if got := Format(s); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

package script

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// renderBlock summarises a KindTextBlock token's block.
func renderBlock(b *TextBlock) string {
	var body []string
	for _, l := range b.Body {
		body = append(body, fmt.Sprintf("%d:%q", l.Pos.Line, l.Text))
	}
	return fmt.Sprintf("%s %s open=%s indent=%q close=%s body=[%s]",
		b.Keyword, b.Shape, b.Open, b.Indent, b.Close, strings.Join(body, " "))
}

func TestLexTextBlock(t *testing.T) {
	for _, tc := range []struct {
		name   string
		src    string
		toks   []string
		blocks []string // one per KindTextBlock token, in order
		diags  []string
	}{
		{
			name:   "run lines",
			src:    "run lines {\n  a\n  b\n}\n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 4:2`, `EOF 5:1`},
			blocks: []string{`run lines open=1:1 indent="" close=4:1 body=[2:"  a" 3:"  b"]`},
		},
		{
			name:   "type paragraphs in a scene, comment on header, literal braces hash and quotes",
			src:    "scene s {\n  type paragraphs {   # note\n    one # two \"x\"\n      }\n    three\n  }  \n}\n",
			toks:   []string{`WORD 1:1 "scene"`, `WORD 1:7 "s"`, `LBRACE 1:9 "{"`, `NEWLINE 1:10`, `WORD 2:3 "type"`, `TEXTBLOCK 2:8 "paragraphs"`, `NEWLINE 6:6`, `RBRACE 7:1 "}"`, `NEWLINE 7:2`, `EOF 8:1`},
			blocks: []string{`type paragraphs open=2:3 indent="  " close=6:3 body=[3:"    one # two \"x\"" 4:"      }" 5:"    three"]`},
		},
		{
			name:   "a brace at another indentation is body text",
			src:    "  run lines {\n}\n }\n   }\n  }\n",
			toks:   []string{`WORD 1:3 "run"`, `TEXTBLOCK 1:7 "lines"`, `NEWLINE 5:4`, `EOF 6:1`},
			blocks: []string{`run lines open=1:3 indent="  " close=5:3 body=[2:"}" 3:" }" 4:"   }"]`},
		},
		{
			name:   "closing line with trailing spaces and tabs",
			src:    "run lines {\n a\n} \t \n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 3:5`, `EOF 4:1`},
			blocks: []string{`run lines open=1:1 indent="" close=3:1 body=[2:" a"]`},
		},
		{
			name:   "lines that look like closing lines but are not",
			src:    "run lines {\n} # c\n}x\n}}\n} }\n{}\n}\n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 7:2`, `EOF 8:1`},
			blocks: []string{`run lines open=1:1 indent="" close=7:1 body=[2:"} # c" 3:"}x" 4:"}}" 5:"} }" 6:"{}"]`},
		},
		{
			name:   "the first closing line wins",
			src:    "run lines {\na\n}\nb\n}\n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 3:2`, `WORD 4:1 "b"`, `NEWLINE 4:2`, `RBRACE 5:1 "}"`, `NEWLINE 5:2`, `EOF 6:1`},
			blocks: []string{`run lines open=1:1 indent="" close=3:1 body=[2:"a"]`},
		},
		{
			name:   "nothing inside is scanned: headers, blocks and comments are body text",
			src:    "type lines {\nrun lines {\n# c\nscene x {\n\"unterminated\n}\n",
			toks:   []string{`WORD 1:1 "type"`, `TEXTBLOCK 1:6 "lines"`, `NEWLINE 6:2`, `EOF 7:1`},
			blocks: []string{`type lines open=1:1 indent="" close=6:1 body=[2:"run lines {" 3:"# c" 4:"scene x {" 5:"\"unterminated"]`},
		},
		{
			name:   "blank lines are kept in the body",
			src:    "run lines {\n\n  a\n   \n\n}\n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 6:2`, `EOF 7:1`},
			blocks: []string{`run lines open=1:1 indent="" close=6:1 body=[2:"" 3:"  a" 4:"   " 5:""]`},
		},
		{
			name:   "empty block lexes; resolving reports it",
			src:    "run lines {\n}\n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 2:2`, `EOF 3:1`},
			blocks: []string{`run lines open=1:1 indent="" close=2:1 body=[]`},
		},
		{
			name:   "crlf",
			src:    "run lines {\r\n  a\r\n\r\n  b\r\n}\r\nmark x\r\n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 5:2`, `WORD 6:1 "mark"`, `WORD 6:6 "x"`, `NEWLINE 6:7`, `EOF 7:1`},
			blocks: []string{`run lines open=1:1 indent="" close=5:1 body=[2:"  a" 3:"" 4:"  b"]`},
		},
		{
			name:   "closing brace on the last line without a newline",
			src:    "run lines {\n a\n}",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 3:2`, `EOF 3:2`},
			blocks: []string{`run lines open=1:1 indent="" close=3:1 body=[2:" a"]`},
		},
		{
			name:   "header spellings",
			src:    "run lines{\n}\ntype   paragraphs\t{#c\n}\n",
			toks:   []string{`WORD 1:1 "run"`, `TEXTBLOCK 1:5 "lines"`, `NEWLINE 2:2`, `WORD 3:1 "type"`, `TEXTBLOCK 3:8 "paragraphs"`, `NEWLINE 4:2`, `EOF 5:1`},
			blocks: []string{`run lines open=1:1 indent="" close=2:1 body=[]`, `type paragraphs open=3:1 indent="" close=4:1 body=[]`},
		},
		{
			name:  "unterminated: no closing brace at all",
			src:   "run lines {\n  a\n",
			toks:  []string{`WORD 1:1 "run"`, `ILLEGAL 1:5 "lines"`, `NEWLINE 3:1`, `EOF 3:1`},
			diags: []string{`1:1: unterminated text block opened here: expected a line with only "}" at column 1`},
		},
		{
			name:  "unterminated: opener is the last line",
			src:   "  run lines {",
			toks:  []string{`WORD 1:3 "run"`, `ILLEGAL 1:7 "lines"`, `NEWLINE 1:14`, `EOF 1:14`},
			diags: []string{`1:3: unterminated text block opened here: expected a line with only "}" at column 3`},
		},
		{
			name:  "unterminated: closing brace at another indentation",
			src:   "# one\n  run lines {\n    a\n  }x\n }\n",
			toks:  []string{`COMMENT 1:1 "# one"`, `NEWLINE 1:6`, `WORD 2:3 "run"`, `ILLEGAL 2:7 "lines"`, `NEWLINE 6:1`, `EOF 6:1`},
			diags: []string{`2:3: unterminated text block opened here: expected a line with only "}" at column 3 (found "}" at line 5 with different indentation; that line is literal text)`},
		},
		{
			name:  "unterminated: tabs and spaces differ",
			src:   "  type lines {\n    a\n\t}\n",
			toks:  []string{`WORD 1:3 "type"`, `ILLEGAL 1:8 "lines"`, `NEWLINE 4:1`, `EOF 4:1`},
			diags: []string{`1:3: unterminated text block opened here: expected a line with only "}" at column 3 (found "}" at line 3 with different indentation: it uses tabs, the "type" line uses spaces; that line is literal text)`},
		},
		{
			name:  "unterminated: spaces where the keyword line has a tab",
			src:   "\trun lines {\n\t\ta\n  }\n",
			toks:  []string{`WORD 1:2 "run"`, `ILLEGAL 1:6 "lines"`, `NEWLINE 4:1`, `EOF 4:1`},
			diags: []string{`1:2: unterminated text block opened here: expected a line with only "}" at column 2 (found "}" at line 3 with different indentation: it uses spaces, the "run" line uses tabs; that line is literal text)`},
		},
		{
			name:  "unterminated: closing brace with a comment",
			src:   "run lines {\n  a\n} # done\n",
			toks:  []string{`WORD 1:1 "run"`, `ILLEGAL 1:5 "lines"`, `NEWLINE 4:1`, `EOF 4:1`},
			diags: []string{`1:1: unterminated text block opened here: expected a line with only "}" at column 1 (found "}" at line 3 followed by a comment; the closing line cannot have a comment)`},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			toks, diags := Tokens(tc.src)
			if got := render(toks); !reflect.DeepEqual(got, tc.toks) {
				t.Errorf("tokens:\n got  %q\n want %q", got, tc.toks)
			}
			if got := renderDiags(diags); !reflect.DeepEqual(got, orEmpty(tc.diags)) {
				t.Errorf("diagnostics:\n got  %q\n want %q", got, tc.diags)
			}
			var blocks []string
			for _, tok := range toks {
				if tok.Kind == KindTextBlock {
					blocks = append(blocks, renderBlock(tok.Block))
				} else if tok.Block != nil {
					t.Errorf("%v token carries a Block", tok.Kind)
				}
			}
			if !reflect.DeepEqual(blocks, tc.blocks) {
				t.Errorf("blocks:\n got  %q\n want %q", blocks, tc.blocks)
			}
		})
	}
}

func TestLexTextBlockOffsets(t *testing.T) {
	src := "scene s {\r\n  run lines {\r\n    a\r\n\r\n  }\r\n}\r\n"
	toks, diags := Tokens(src)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %q", renderDiags(diags))
	}
	for _, tok := range toks {
		if want := posAt(src, tok.Pos.Offset); want != tok.Pos {
			t.Errorf("%v: Pos %+v, but offset %d is %+v", tok.Kind, tok.Pos, tok.Pos.Offset, want)
		}
		if tok.Kind != KindTextBlock {
			continue
		}
		b := tok.Block
		for _, p := range []Pos{b.Open, b.Close} {
			if want := posAt(src, p.Offset); want != p {
				t.Errorf("block pos %+v, but offset %d is %+v", p, p.Offset, want)
			}
		}
		if !strings.HasPrefix(src[b.Open.Offset:], "run lines") || src[b.Close.Offset] != '}' {
			t.Errorf("Open/Close offsets do not point at the keyword and the brace: %+v %+v", b.Open, b.Close)
		}
		for _, l := range b.Body {
			if !strings.HasPrefix(src[l.Pos.Offset:], l.Text) || l.Pos.Col != 1 {
				t.Errorf("body line %+v does not match the source", l)
			}
		}
	}
}

// TestTextBlockDocExamples checks the two block diagnostics quoted in §4.1 of
// the design document, character for character.
func TestTextBlockDocExamples(t *testing.T) {
	unterminated := strings.Repeat("# filler\n", 6) +
		"  run lines {\n    a\n    b\n    c\n    d\n    }\n"
	_, diags := Tokens(unterminated)
	want := `demo.script:7:3: unterminated text block opened here: expected a line with only "}" at column 3 (found "}" at line 12 with different indentation; that line is literal text)`
	if len(diags) != 1 || diags[0].Format("demo.script") != want {
		t.Errorf("unterminated:\n got  %v\n want %s", diags, want)
	}

	empty := strings.Repeat("# filler\n", 6) + "  run lines {\n  }\n"
	toks, diags := Tokens(empty)
	if len(diags) != 0 {
		t.Fatalf("diagnostics: %v", diags)
	}
	var block *TextBlock
	for _, tok := range toks {
		if tok.Kind == KindTextBlock {
			block = tok.Block
		}
	}
	if block == nil {
		t.Fatalf("no text block in %q", render(toks))
	}
	text, d := block.Text()
	want = `demo.script:7:3: empty text block: no non-blank line (to press Enter alone use "press enter")`
	if text != "" || d == nil || d.Format("demo.script") != want {
		t.Errorf("empty:\n got  %q %v\n want %s", text, d, want)
	}
}

func TestTextBlockText(t *testing.T) {
	toks, _ := Tokens("run paragraphs {\n    one\n      two\n\n\n    three\n}\n")
	block := toks[1].Block
	got, d := block.Text()
	if d != nil || got != "one two\n\nthree\n" {
		t.Errorf("Text() = %q, %v", got, d)
	}
	if !reflect.DeepEqual(block.Lines(), []string{"    one", "      two", "", "", "    three"}) {
		t.Errorf("Lines() = %q", block.Lines())
	}
}

func TestResolve(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shape   Shape
		keyword string
		lines   []string
		want    string
	}{
		// The table of §3.5.
		{"lines type", ShapeLines, "type", []string{"a", "b"}, "a\nb"},
		{"lines run", ShapeLines, "run", []string{"a", "b"}, "a\nb\n"},
		{"paragraphs type", ShapeParagraphs, "type", []string{"one", "two", "", "three"}, "one two\n\nthree"},
		{"paragraphs run", ShapeParagraphs, "run", []string{"one", "two", "", "three"}, "one two\n\nthree\n"},

		// Indentation.
		{"common indent stripped", ShapeLines, "type", []string{"    a", "    b"}, "a\nb"},
		{"relative indent kept", ShapeLines, "type", []string{"  a", "    b", "  c"}, "a\n  b\nc"},
		{"minimum indent is the first line", ShapeLines, "type", []string{"    a", "  b"}, "  a\nb"},
		{"tabs", ShapeLines, "type", []string{"\t\ta", "\t\t\tb"}, "a\n\tb"},
		{"mixed tabs and spaces strip the common prefix only", ShapeLines, "type", []string{"\ta", "  b"}, "\ta\n  b"},
		{"common prefix of tab-then-spaces", ShapeLines, "type", []string{"\t  a", "\t    b", "\t c"}, " a\n   b\nc"},
		{"blank lines do not count for the indent", ShapeLines, "type", []string{"    a", "", " ", "    b"}, "a\n\n\nb"},
		{"no indent at all", ShapeLines, "type", []string{"a", "  b"}, "a\n  b"},

		// Trailing whitespace and blank lines.
		{"trailing whitespace trimmed", ShapeLines, "type", []string{"a  ", "b\t", "c \t "}, "a\nb\nc"},
		{"leading and trailing blank lines dropped", ShapeLines, "type", []string{"", "  ", "a", "", "  \t", ""}, "a"},
		{"interior blank lines stay blank lines", ShapeLines, "type", []string{"a", "", "", "b"}, "a\n\n\nb"},
		{"whitespace-only interior line becomes empty", ShapeLines, "run", []string{"  a", "   ", "  b"}, "a\n\nb\n"},
		{"single line", ShapeLines, "type", []string{"  only"}, "only"},
		{"single line run", ShapeLines, "run", []string{"only"}, "only\n"},

		// Raw: nothing is interpreted.
		{"hash quotes backslash and braces are literal", ShapeLines, "type", []string{`# a "b" \n \x41 {`, `}`}, "# a \"b\" \\n \\x41 {\n}"},
		{"shell heredoc", ShapeLines, "run", []string{"  git commit -q -F - <<'EOF'", "  initial commit", "  EOF"}, "git commit -q -F - <<'EOF'\ninitial commit\nEOF\n"},
		{"non-ASCII", ShapeLines, "type", []string{"  héllo", "  wörld"}, "héllo\nwörld"},

		// Paragraphs.
		{"paragraph lines trimmed and joined with one space", ShapeParagraphs, "type", []string{"  one  ", "    two", "  three"}, "one two three"},
		{"many blank lines are one separator", ShapeParagraphs, "type", []string{"a", "", "", " ", "\t", "b"}, "a\n\nb"},
		{"leading and trailing blanks dropped", ShapeParagraphs, "run", []string{"", "a", "b", ""}, "a b\n"},
		{"three paragraphs", ShapeParagraphs, "type", []string{"a", "", "b", "c", "", "d"}, "a\n\nb c\n\nd"},
		{"single word paragraphs", ShapeParagraphs, "type", []string{"a", "", "b"}, "a\n\nb"},
		{"tabs between lines are trimmed", ShapeParagraphs, "type", []string{"\ta", "\t\tb"}, "a b"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := append([]string(nil), tc.lines...)
			got, err := Resolve(tc.shape, tc.keyword, in)
			if err != nil {
				t.Fatalf("Resolve error: %v", err)
			}
			if got != tc.want {
				t.Errorf("Resolve = %q, want %q", got, tc.want)
			}
			if !reflect.DeepEqual(in, tc.lines) {
				t.Errorf("Resolve modified its input: %q -> %q", tc.lines, in)
			}
		})
	}
}

func TestResolveErrors(t *testing.T) {
	for _, tc := range []struct {
		name    string
		shape   Shape
		keyword string
		lines   []string
		is      error
		msg     string
	}{
		{"no lines", ShapeLines, "run", nil, ErrEmptyBlock, "empty text block: no non-blank line"},
		{"only blank lines", ShapeParagraphs, "type", []string{"", "  ", "\t"}, ErrEmptyBlock, ""},
		{"bad shape", "words", "run", []string{"a"}, nil, `text block shape must be lines or paragraphs, not "words"`},
		{"bad keyword", ShapeLines, "press", []string{"a"}, nil, `text block keyword must be run or type, not "press"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Resolve(tc.shape, tc.keyword, tc.lines)
			if err == nil || got != "" {
				t.Fatalf("Resolve = %q, %v; want an error", got, err)
			}
			if tc.is != nil && !errors.Is(err, tc.is) {
				t.Errorf("error %v is not %v", err, tc.is)
			}
			if tc.msg != "" && err.Error() != tc.msg {
				t.Errorf("error %q, want %q", err, tc.msg)
			}
		})
	}
}

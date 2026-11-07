package script

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzLex checks that lexing any input terminates, never panics, and returns
// well-formed positions and diagnostics. CRLF input must lex like LF input.
func FuzzLex(f *testing.F) {
	for _, seed := range []string{
		"",
		"\n",
		"run npm install\n",
		"type \"echo \\\"hi\\\"\\n\" # c\n",
		"run \"hi\" | wc\n",
		"run \"\\x",
		"wait /^[0-9a-f]{7,}$/ 10s\n",
		"wait /a\\/",
		"wait \"x\" 0s\nwait idle 1s\n",
		"env PS1=\"$ \"\nenv A=\nenv =x\n",
		"shell /bin/bash --rcfile=\"my rc\" -i\n",
		"scene a {\n  hidden {\n    run x\n  }\n}\n",
		"run lines {\n  a\n  }\n}\n",
		"  type paragraphs { # c\n    one\n\n    two\n  }\n",
		"run lines {\n\t}\n  }\n",
		"#!/usr/bin/env -S shellshot record --script\r\nmark a\r\n",
		"mark \xff\n",
		"}",
		"run lines {",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, src string) {
		toks, diags := Tokens(src)
		if len(toks) == 0 || toks[len(toks)-1].Kind != KindEOF {
			t.Fatalf("token stream does not end with EOF: %q", render(toks))
		}
		if limit := 4*len(src) + 4; len(toks) > limit {
			t.Fatalf("%d tokens for %d bytes", len(toks), len(src))
		}
		prev := 0
		for i, tok := range toks {
			if tok.Kind == KindEOF && i != len(toks)-1 {
				t.Fatalf("EOF at index %d of %d", i, len(toks))
			}
			if tok.Pos.Offset < prev || tok.Pos.Offset > len(src) {
				t.Fatalf("token %d %v offset %d (previous %d, source %d bytes)", i, tok.Kind, tok.Pos.Offset, prev, len(src))
			}
			prev = tok.Pos.Offset
			if want := posAt(src, tok.Pos.Offset); want != tok.Pos {
				t.Fatalf("token %d %v: Pos %+v, offset %d is %+v", i, tok.Kind, tok.Pos, tok.Pos.Offset, want)
			}
			if (tok.Block != nil) != (tok.Kind == KindTextBlock) {
				t.Fatalf("token %d %v has Block %v", i, tok.Kind, tok.Block)
			}
		}
		if len(diags) > MaxDiagnostics {
			t.Fatalf("%d diagnostics", len(diags))
		}
		for _, d := range diags {
			if d.Pos.Line < 1 || d.Pos.Col < 1 || d.Msg == "" {
				t.Fatalf("malformed diagnostic %+v", d)
			}
		}

		if strings.Contains(src, "\r") {
			return
		}
		crlf := strings.ReplaceAll(src, "\n", "\r\n")
		toks2, diags2 := Tokens(crlf)
		if !reflect.DeepEqual(render(toks), render(toks2)) || !reflect.DeepEqual(renderDiags(diags), renderDiags(diags2)) {
			t.Fatalf("CRLF changes the result:\n LF   %q %q\n CRLF %q %q", render(toks), renderDiags(diags), render(toks2), renderDiags(diags2))
		}
	})
}

var fuzzIndents = []string{"", "  ", "\t", " \t", "    "}

// FuzzTextBlock builds `<indent><keyword> <shape> {`, a fuzzed body and an
// optional closing line, and checks the lexer against an independent search
// for the first closing line.
func FuzzTextBlock(f *testing.F) {
	f.Add("a\nb", uint8(0), false, false, true, false)
	f.Add("  a\n  }\n  b", uint8(1), true, true, true, false)
	f.Add("a\n}\nb\n}", uint8(0), false, false, true, true)
	f.Add("a\n }\n\t}", uint8(1), false, false, false, false)
	f.Add("# c\n\"q\"\n\\n}  ", uint8(2), true, false, true, true)
	f.Add("", uint8(0), false, false, true, false)
	f.Add("x\r\n}\r", uint8(3), false, true, false, true)
	f.Fuzz(func(t *testing.T, body string, indentSel uint8, paragraphs, useType, withClose, crlf bool) {
		indent := fuzzIndents[int(indentSel)%len(fuzzIndents)]
		keyword, shape := "run", ShapeLines
		if useType {
			keyword = "type"
		}
		if paragraphs {
			shape = ShapeParagraphs
		}
		src := indent + keyword + " " + string(shape) + " {  # header comment\n" + body + "\n"
		if withClose {
			src += indent + "}\n"
		}
		if crlf {
			src = strings.ReplaceAll(src, "\n", "\r\n")
		}

		// Oracle: the lines after the header, first line that is exactly
		// indent + "}" + optional trailing whitespace.
		lines := strings.Split(src, "\n")[1:]
		lines = lines[:len(lines)-1]
		for i := range lines {
			lines[i] = strings.TrimSuffix(lines[i], "\r")
		}
		closer := regexp.MustCompile(`^` + regexp.QuoteMeta(indent) + `\}[ \t]*$`)
		closeIdx := -1
		for i, l := range lines {
			if closer.MatchString(l) {
				closeIdx = i
				break
			}
		}

		toks, diags := Tokens(src)
		var blocks []*TextBlock
		illegal := 0
		for _, tok := range toks {
			switch tok.Kind {
			case KindTextBlock:
				blocks = append(blocks, tok.Block)
			case KindIllegal:
				illegal++
			}
		}
		if closeIdx < 0 {
			if len(blocks) != 0 || illegal != 1 {
				t.Fatalf("no closing line, but %d blocks and %d illegal tokens: %q", len(blocks), illegal, render(toks))
			}
			if len(toks) != 4 {
				t.Fatalf("unterminated block must swallow the rest of the file: %q", render(toks))
			}
			want := Pos{Line: 1, Col: len(indent) + 1, Offset: len(indent)}
			if len(diags) == 0 || diags[len(diags)-1].Pos != want || !strings.HasPrefix(diags[len(diags)-1].Msg, "unterminated text block") {
				t.Fatalf("diagnostics %v, want unterminated block at %v", diags, want)
			}
			return
		}
		// The lines after the closing line are lexed normally (and may hold
		// more blocks or errors); the first block is the one we built.
		if len(toks) < 3 || toks[1].Kind != KindTextBlock || toks[2].Kind != KindNewline {
			t.Fatalf("closing line at %d, but the stream starts %q", closeIdx, render(toks))
		}
		b := toks[1].Block
		if closeIdx == len(lines)-1 && utf8.ValidString(src) && len(diags) != 0 {
			t.Fatalf("unexpected diagnostics %v", diags)
		}
		if got := b.Lines(); !reflect.DeepEqual(got, lines[:closeIdx]) && (len(got) != 0 || closeIdx != 0) {
			t.Fatalf("body %q, want %q", got, lines[:closeIdx])
		}
		if b.Close.Line != 2+closeIdx || b.Close.Col != len(indent)+1 || b.Indent != indent || b.Keyword != keyword || b.Shape != shape {
			t.Fatalf("block %+v does not match closing line %d", b, closeIdx)
		}

		// Re-indenting every line (header, body, closing) leaves the result unchanged.
		want, wantErr := b.Text()
		for _, extra := range []string{"  ", "\t", " \t "} {
			shifted := extra + indent
			lines2 := strings.Split(src, "\n")
			for i, l := range lines2[:len(lines2)-1] {
				lines2[i] = extra + l
			}
			toks2, _ := Tokens(strings.Join(lines2, "\n"))
			var b2 *TextBlock
			for _, tok := range toks2 {
				if tok.Kind == KindTextBlock {
					b2 = tok.Block
				}
			}
			if b2 == nil || b2.Indent != shifted || len(b2.Body) != len(b.Body) {
				t.Fatalf("re-indented by %q: block %+v, original %+v", extra, b2, b)
			}
			got, gotErr := b2.Text()
			if got != want || (gotErr == nil) != (wantErr == nil) {
				t.Fatalf("re-indented by %q: %q, %v; original %q, %v", extra, got, gotErr, want, wantErr)
			}
		}
	})
}

// FuzzResolve checks the body rules on arbitrary lines.
func FuzzResolve(f *testing.F) {
	f.Add("a\nb", false, false)
	f.Add("  one\n  two\n\n  three", true, true)
	f.Add("\n\t\n  x  \n\n\n y", false, true)
	f.Add("", false, false)
	f.Add("\t a\n  b", true, false)
	f.Fuzz(func(t *testing.T, body string, paragraphs, useRun bool) {
		shape, keyword := ShapeLines, "type"
		if paragraphs {
			shape = ShapeParagraphs
		}
		if useRun {
			keyword = "run"
		}
		lines := strings.Split(body, "\n")
		orig := append([]string(nil), lines...)
		want, err := Resolve(shape, keyword, lines)
		if !reflect.DeepEqual(lines, orig) {
			t.Fatalf("Resolve modified its input")
		}
		if err != nil {
			if err != ErrEmptyBlock {
				t.Fatalf("unexpected error %v", err)
			}
			for _, l := range lines {
				if strings.Trim(l, " \t") != "" {
					t.Fatalf("ErrEmptyBlock, but line %q is not blank", l)
				}
			}
			return
		}

		// Shape of the result.
		typed := strings.TrimSuffix(want, "\n")
		if keyword == "run" && !strings.HasSuffix(want, "\n") {
			t.Fatalf("run result %q has no final Enter", want)
		}
		if typed == "" || strings.HasPrefix(typed, "\n") || strings.HasSuffix(typed, "\n") || strings.HasSuffix(typed, " ") || strings.HasSuffix(typed, "\t") {
			t.Fatalf("result %q starts or ends with a blank", want)
		}
		for _, l := range strings.Split(typed, "\n") {
			if strings.HasSuffix(l, " ") || strings.HasSuffix(l, "\t") {
				t.Fatalf("result %q has trailing whitespace", want)
			}
		}
		if paragraphs && strings.Contains(want, "\n\n\n") {
			t.Fatalf("paragraphs result %q has more than one blank line", want)
		}

		// Re-indenting, trailing whitespace and extra blank lines change nothing.
		for _, extra := range []string{"  ", "\t", " \t "} {
			shifted := make([]string, len(lines))
			for i, l := range lines {
				shifted[i] = extra + l + extra
			}
			shifted = append(append([]string{"", extra}, shifted...), "\t", "")
			got, err := Resolve(shape, keyword, shifted)
			if err != nil || got != want {
				t.Fatalf("changing whitespace by %q: %q, %v; want %q", extra, got, err, want)
			}
		}
	})
}

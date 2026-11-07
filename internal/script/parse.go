package script

import (
	"fmt"
	"slices"
)

// Parse parses a script. filename is kept in Script.File and used for
// nothing else. The result is never nil: when there are diagnostics it holds
// what could be parsed and must not be run. Diagnostics are in source order,
// at most MaxDiagnostics of them.
//
// The parser checks syntax only: it accepts every setting and every nesting
// of blocks anywhere, and it does not type setting values (sizes, durations
// of settings), check scene names or compile regular expressions. The
// validator does that. Unknown and reserved commands are errors here, and
// the parser resumes at the next line.
func Parse(filename string, src []byte) (*Script, []Diagnostic) {
	return ParseString(filename, string(src))
}

// ParseString is Parse for a string.
func ParseString(filename, src string) (*Script, []Diagnostic) {
	p := &parser{lx: NewLexer(src), src: src}
	body, _, _ := p.parseBody(true)
	return &Script{File: filename, Settings: body.settings, Items: body.items}, p.diagnostics()
}

type parser struct {
	lx  *Lexer // its diagnostics list is shared with the parser
	src string
}

// scope is the settings and items of a script or a block.
type scope struct {
	settings []Setting
	items    []Item
}

// line is the tokens of one source line, without the comment and the
// newline. end is where the comment starts, or else the end of the line.
type line struct {
	toks []Token
	end  Pos
}

func (p *parser) errorf(pos Pos, msg, hint string) {
	p.lx.diags.Add(pos, msg, hint)
}

func (p *parser) diagnostics() []Diagnostic {
	ds := slices.Clone(p.lx.Diagnostics())
	slices.SortStableFunc(ds, func(a, b Diagnostic) int { return a.Pos.Offset - b.Pos.Offset })
	return ds
}

// readLine returns the next line that has tokens other than a comment.
func (p *parser) readLine() (line, bool) {
	var ln line
	for {
		t := p.lx.Next()
		switch t.Kind {
		case KindEOF:
			return line{}, false
		case KindComment:
			ln.end = t.Pos
		case KindNewline:
			if len(ln.toks) > 0 {
				if ln.end.Line == 0 {
					ln.end = t.Pos
				}
				return ln, true
			}
			ln = line{}
		default:
			ln.toks = append(ln.toks, t)
		}
	}
}

// parseBody parses lines until the closing "}" of a block, or to the end of
// the file. At the top level a "}" is stray. It returns the position of the
// closing brace, or of the end of the file, and whether the block was closed.
func (p *parser) parseBody(top bool) (body scope, end Pos, closed bool) {
	for {
		ln, ok := p.readLine()
		if !ok {
			return body, p.lx.eof, false
		}
		kw := ln.toks[0]
		switch {
		case kw.Kind == KindRBrace && top:
			p.errorf(kw.Pos, `unexpected "}"`, `no block is open here; a text block header is exactly "run lines {" or "run paragraphs {" (also with type)`)
		case kw.Kind == KindRBrace:
			p.closing(ln)
			return body, kw.Pos, true
		default:
			p.statement(ln, &body)
		}
	}
}

func (p *parser) closing(ln line) {
	if rest := ln.toks[1:]; len(rest) > 0 && rest[0].Kind != KindIllegal {
		p.errorf(rest[0].Pos, fmt.Sprintf(`unexpected %q after "}"`, rest[0].Text), `only a comment may follow a closing brace`)
	}
}

// statement parses one line that does not start with "}".
func (p *parser) statement(ln line, sc *scope) {
	kw := ln.toks[0]
	switch {
	case isStep(kw.Text):
		if st, ok := p.step(ln); ok {
			sc.items = append(sc.items, st)
		}
	case isBlock(kw.Text):
		sc.items = append(sc.items, p.block(ln))
	case isSetting(kw.Text):
		if st, ok := p.setting(ln, len(sc.items)); ok {
			sc.settings = append(sc.settings, st)
		}
	case isReserved(kw.Text):
		p.errorf(kw.Pos, fmt.Sprintf("%q is reserved for future use", kw.Text), reservedHints[kw.Text])
		p.skipOpenedBlock(ln)
	default:
		p.errorf(kw.Pos, fmt.Sprintf("unknown command %q", kw.Text), DidYouMean(kw.Text, keywordNames))
		p.skipOpenedBlock(ln)
	}
}

// skipOpenedBlock skips the lines of the block that ln opens, if it ends in
// "{", so that the lines inside and the closing brace give no more errors.
func (p *parser) skipOpenedBlock(ln line) {
	if !opensBlock(ln) {
		return
	}
	for depth := 1; depth > 0; {
		next, ok := p.readLine()
		if !ok {
			return
		}
		switch {
		case next.toks[0].Kind == KindRBrace:
			depth--
		case opensBlock(next):
			depth++
		}
	}
}

func opensBlock(ln line) bool {
	last := ln.toks[len(ln.toks)-1]
	return len(ln.toks) > 1 && last.Text == "{" && (last.Kind == KindWord || last.Kind == KindLBrace)
}

func hasIllegal(toks []Token) bool {
	return slices.ContainsFunc(toks, func(t Token) bool { return t.Kind == KindIllegal })
}

// block parses "scene NAME {", "hidden {" or "hidden NAME {" and the lines up to the closing
// brace. A broken header still opens the block, so one mistake in the header
// does not make the whole body fail.
func (p *parser) block(ln line) Block {
	kw := ln.toks[0]
	b := Block{Pos: kw.Pos, Kind: kw.Text}
	if !hasIllegal(ln.toks) {
		b.Name = p.blockHeader(kw.Text, ln)
	}
	body, end, closed := p.parseBody(false)
	b.Settings, b.Items, b.End = body.settings, body.items, end
	if !closed {
		p.errorf(b.Pos, "unterminated "+blockLabel(b)+`: expected a line with only "}" before the end of the file`, "")
	}
	return b
}

// blockHeader checks the words after the block keyword and returns the block
// name, which is empty for an unnamed hidden block.
func (p *parser) blockHeader(kind string, ln line) (name string) {
	args := ln.toks[1:]
	i := 0
	named := i < len(args) && (args[i].Kind == KindWord || args[i].Kind == KindString)
	switch {
	case named && args[i].Kind == KindString:
		p.errorf(args[i].Pos, "a "+kind+" name is written bare, not in quotes", `write `+kind+` `+args[i].Text+` {`)
		name, i = args[i].Text, i+1
	case named:
		name, i = args[i].Text, i+1
	case kind == BlockScene:
		p.errorf(p.posOrEnd(args, i, ln), "scene needs a name", `write scene NAME {`)
	}
	if i >= len(args) || args[i].Kind != KindLBrace {
		p.errorf(p.posOrEnd(args, i, ln), `expected "{" to open the `+kind+` block`, "write the brace after a space on the same line")
		for i < len(args) && args[i].Kind != KindLBrace {
			i++
		}
	}
	if i < len(args) {
		i++
	}
	if i < len(args) {
		p.errorf(args[i].Pos, fmt.Sprintf(`unexpected %q after "{"`, args[i].Text), "the body of the block starts on the next line")
	}
	return name
}

func (p *parser) posOrEnd(args []Token, i int, ln line) Pos {
	if i < len(args) {
		return args[i].Pos
	}
	return ln.end
}

package script

import (
	"errors"
	"fmt"
	"strings"
)

// Shape is how a text block's lines are turned into typed text.
type Shape string

const (
	// ShapeLines types each line as written, with Enter between lines.
	ShapeLines Shape = "lines"
	// ShapeParagraphs joins the lines of a paragraph with one space and
	// separates paragraphs with two Enters.
	ShapeParagraphs Shape = "paragraphs"
)

// BodyLine is one raw line of a text block body, without its line terminator.
// Pos is the start of the line (column 1).
type BodyLine struct {
	Text string
	Pos  Pos
}

// TextBlock is the raw content of `run|type lines|paragraphs { ... }`. The
// lexer does not interpret the body; Text and Resolve apply the body rules.
type TextBlock struct {
	Keyword string // "run" or "type"
	Shape   Shape
	Open    Pos        // the run/type keyword
	Indent  string     // leading whitespace of the keyword line
	Body    []BodyLine // every line between the header and the closing line
	Close   Pos        // the closing "}"
}

// Lines returns the raw body lines.
func (b *TextBlock) Lines() []string {
	lines := make([]string, len(b.Body))
	for i, l := range b.Body {
		lines[i] = l.Text
	}
	return lines
}

// Text resolves the body with Resolve. An empty block is reported at the
// opening keyword.
func (b *TextBlock) Text() (string, *Diagnostic) {
	text, err := Resolve(b.Shape, b.Keyword, b.Lines())
	if err == nil {
		return text, nil
	}
	d := &Diagnostic{Pos: b.Open, Msg: err.Error()}
	if errors.Is(err, ErrEmptyBlock) {
		d.Hint = `to press Enter alone use "press enter"`
	}
	return "", d
}

// ErrEmptyBlock is returned by Resolve for a body without a non-blank line.
var ErrEmptyBlock = errors.New("empty text block: no non-blank line")

// Resolve turns the raw body lines of a text block into the text to type, in
// which "\n" is Enter. The rules, in order:
//
//  1. Whitespace is space and tab. A line with only whitespace is blank.
//  2. Trailing whitespace is trimmed from every line.
//  3. The longest common leading whitespace of the non-blank lines is removed
//     from them (relative indentation stays).
//  4. Leading and trailing blank lines are dropped; if no line is left the
//     error is ErrEmptyBlock.
//  5. ShapeLines: the lines are joined with Enter; blank lines inside stay as
//     blank lines.
//  6. ShapeParagraphs: runs of non-blank lines are paragraphs; the lines of a
//     paragraph are trimmed on both sides and joined with one space, and
//     paragraphs are joined with two Enters, however many blank lines
//     separate them.
//  7. For keyword "run" one more Enter is added at the end; "type" adds none.
func Resolve(shape Shape, keyword string, lines []string) (string, error) {
	if keyword != "run" && keyword != "type" {
		return "", fmt.Errorf("text block keyword must be run or type, not %q", keyword)
	}
	if shape != ShapeLines && shape != ShapeParagraphs {
		return "", fmt.Errorf("text block shape must be lines or paragraphs, not %q", shape)
	}
	trimmed := make([]string, len(lines))
	first, last := -1, -1
	for i, l := range lines {
		trimmed[i] = strings.TrimRight(l, " \t")
		if trimmed[i] != "" {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		return "", ErrEmptyBlock
	}
	body := trimmed[first : last+1]

	prefix := leadingWhitespace(body[0])
	for _, l := range body {
		if l != "" {
			prefix = commonPrefix(prefix, leadingWhitespace(l))
		}
	}
	for i, l := range body {
		if l != "" {
			body[i] = l[len(prefix):]
		}
	}

	var text string
	if shape == ShapeLines {
		text = strings.Join(body, "\n")
	} else {
		var paragraphs []string
		var words []string
		flush := func() {
			if len(words) > 0 {
				paragraphs = append(paragraphs, strings.Join(words, " "))
				words = nil
			}
		}
		for _, l := range body {
			if l == "" {
				flush()
				continue
			}
			words = append(words, strings.TrimLeft(l, " \t"))
		}
		flush()
		text = strings.Join(paragraphs, "\n\n")
	}
	if keyword == "run" {
		text += "\n"
	}
	return text, nil
}

func leadingWhitespace(s string) string {
	return s[:len(s)-len(strings.TrimLeft(s, " \t"))]
}

func commonPrefix(a, b string) string {
	n := min(len(a), len(b))
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return a[:i]
}

// textBlockHeader reports whether text (trimmed, without its keyword) is a
// text-block header: "lines" or "paragraphs", optional spaces, "{", optional
// spaces and an optional comment.
func textBlockHeader(text string) (Shape, bool) {
	for _, shape := range []Shape{ShapeLines, ShapeParagraphs} {
		rest, ok := strings.CutPrefix(text, string(shape))
		if !ok {
			continue
		}
		rest, ok = strings.CutPrefix(strings.TrimLeft(rest, " \t"), "{")
		if !ok {
			continue
		}
		if rest = strings.TrimLeft(rest, " \t"); rest == "" || rest[0] == '#' {
			return shape, true
		}
	}
	return "", false
}

// isClosingBrace reports whether s is "}" and optional trailing whitespace.
func isClosingBrace(s string) bool {
	rest, ok := strings.CutPrefix(s, "}")
	return ok && strings.TrimLeft(rest, " \t") == ""
}

// describeWhitespace names the whitespace characters of s for hints.
func describeWhitespace(s string) string {
	tabs, spaces := strings.Contains(s, "\t"), strings.Contains(s, " ")
	switch {
	case tabs && spaces:
		return "tabs and spaces"
	case tabs:
		return "tabs"
	case spaces:
		return "spaces"
	}
	return "no indentation"
}

// scanTextBlock reads the body of a text block whose header is at sc.i (the
// shape word) and reads lines until the closing line: "}" and optional
// trailing whitespace, with exactly the indentation of the keyword line. The
// first such line closes the block; a "}" at any other indentation, or with a
// comment, is body text. It emits one KindTextBlock, or reports an
// unterminated block at the keyword and emits KindIllegal.
func (sc *lineScan) scanTextBlock(keyword string, shape Shape) {
	l := sc.lx
	open := sc.pos(len(sc.indent))
	headerAt := sc.pos(sc.i)
	sc.i = len(sc.s)

	var body []BodyLine
	var otherIndent BodyLine // last line with only "}" at another indentation
	haveOtherIndent := false
	commentLine := 0 // last line with "}" and a comment at the right indentation
	for {
		text, start, ok := l.readLine()
		if !ok {
			break
		}
		if rest, found := strings.CutPrefix(text, sc.indent); found && isClosingBrace(rest) {
			closeAt := Pos{Line: start.Line, Col: len(sc.indent) + 1, Offset: start.Offset + len(sc.indent)}
			l.queue = append(l.queue, Token{
				Kind: KindTextBlock,
				Pos:  headerAt,
				Text: string(shape),
				Block: &TextBlock{
					Keyword: keyword, Shape: shape, Open: open, Indent: sc.indent,
					Body: body, Close: closeAt,
				},
			})
			end := Pos{Line: start.Line, Col: len(text) + 1, Offset: start.Offset + len(text)}
			sc.nl = &end
			return
		}
		body = append(body, BodyLine{Text: text, Pos: start})
		ws := leadingWhitespace(text)
		rest, braced := strings.CutPrefix(text[len(ws):], "}")
		if !braced {
			continue
		}
		rest = strings.TrimLeft(rest, " \t")
		switch {
		case rest == "" && ws != sc.indent:
			otherIndent, haveOtherIndent = body[len(body)-1], true
		case ws == sc.indent && strings.HasPrefix(rest, "#"):
			commentLine = start.Line
		}
	}

	hint := ""
	switch {
	case haveOtherIndent:
		ws := leadingWhitespace(otherIndent.Text)
		detail := ""
		if sc.indent != "" && ws != "" && describeWhitespace(ws) != describeWhitespace(sc.indent) {
			detail = fmt.Sprintf(`: it uses %s, the %q line uses %s`, describeWhitespace(ws), keyword, describeWhitespace(sc.indent))
		}
		hint = fmt.Sprintf(`found "}" at line %d with different indentation%s; that line is literal text`, otherIndent.Pos.Line, detail)
	case commentLine != 0:
		hint = fmt.Sprintf(`found "}" at line %d followed by a comment; the closing line cannot have a comment`, commentLine)
	}
	l.diags.Add(open, fmt.Sprintf(`unterminated text block opened here: expected a line with only "}" at column %d`, open.Col), hint)
	l.queue = append(l.queue, Token{Kind: KindIllegal, Pos: headerAt, Text: string(shape)})
	end := l.eof
	sc.nl = &end
}

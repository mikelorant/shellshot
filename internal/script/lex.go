package script

import (
	"strings"
	"unicode/utf8"
)

// Lexer turns a script into tokens, one line at a time. The first word of a
// line (the keyword, or a leading "}") selects how the rest of the line is
// scanned:
//
//   - run, type: one KindText (the rest of the line, trimmed, literal: "#" and
//     "\" are ordinary characters) or, when the text starts with a double
//     quote, one KindString and an optional comment.
//   - wait: the first argument is a KindString (starts with a double quote), a
//     KindRegex (starts with "/") or a plain word such as "idle"; later
//     arguments are plain words and strings. Only wait scans a regex.
//   - env: KEY, KindAssign, and an optional value (a string or a word taken
//     directly after "=").
//   - shell: arguments split shell-style, see scanShellArgs.
//   - scene, hidden: words and strings, with "{" as KindLBrace.
//   - everything else: words and strings. A "#" starts a comment only at the
//     start of a token, so "cwd /tmp/a#b" is one word.
//
// CRLF line endings are accepted: a "\r" before the line feed is dropped.
// Errors are collected in Diagnostics; the lexer always makes progress and
// ends with KindEOF.
type Lexer struct {
	src   string
	off   int // offset of the first unread line
	line  int // line number of the first unread line
	eof   Pos
	queue []Token
	head  int
	diags Diagnostics
}

// NewLexer returns a Lexer for src. A source that is not valid UTF-8 gets a
// diagnostic at its first bad byte; lexing goes on.
func NewLexer(src string) *Lexer {
	l := &Lexer{src: src, line: 1, eof: posAt(src, len(src))}
	if !utf8.ValidString(src) {
		for i := 0; i < len(src); {
			r, size := utf8.DecodeRuneInString(src[i:])
			if r == utf8.RuneError && size == 1 {
				l.diags.Add(posAt(src, i), "invalid UTF-8", "the file must be valid UTF-8")
				break
			}
			i += size
		}
	}
	return l
}

// Tokens lexes all of src. The last token is KindEOF.
func Tokens(src string) ([]Token, []Diagnostic) {
	l := NewLexer(src)
	var toks []Token
	for {
		t := l.Next()
		toks = append(toks, t)
		if t.Kind == KindEOF {
			return toks, l.Diagnostics()
		}
	}
}

// Diagnostics returns the errors found so far, at most MaxDiagnostics.
func (l *Lexer) Diagnostics() []Diagnostic { return l.diags.Items() }

// DroppedDiagnostics counts the errors found beyond MaxDiagnostics.
func (l *Lexer) DroppedDiagnostics() int { return l.diags.Dropped() }

// Next returns the next token.
func (l *Lexer) Next() Token {
	for l.head >= len(l.queue) {
		if l.off >= len(l.src) {
			return Token{Kind: KindEOF, Pos: l.eof}
		}
		l.queue, l.head = l.queue[:0], 0
		l.lexLine()
	}
	t := l.queue[l.head]
	l.head++
	return t
}

// posAt is the position of byte offset off in src.
func posAt(src string, off int) Pos {
	before := src[:off]
	return Pos{
		Line:   1 + strings.Count(before, "\n"),
		Col:    off - strings.LastIndexByte(before, '\n'),
		Offset: off,
	}
}

// readLine consumes one line without its line terminator (LF or CRLF).
func (l *Lexer) readLine() (text string, start Pos, ok bool) {
	if l.off >= len(l.src) {
		return "", Pos{}, false
	}
	start = Pos{Line: l.line, Col: 1, Offset: l.off}
	rest := l.src[l.off:]
	if n := strings.IndexByte(rest, '\n'); n >= 0 {
		text, l.off = rest[:n], l.off+n+1
	} else {
		text, l.off = rest, len(l.src)
	}
	l.line++
	return strings.TrimSuffix(text, "\r"), start, true
}

// lineScan scans one line.
type lineScan struct {
	lx     *Lexer
	s      string // the line without terminator
	start  Pos    // position of s[0]
	i      int
	indent string // leading whitespace of the line
	nl     *Pos   // where to put the NEWLINE token, when not at the end of s
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' }

func (sc *lineScan) pos(i int) Pos {
	return Pos{Line: sc.start.Line, Col: i + 1, Offset: sc.start.Offset + i}
}

func (sc *lineScan) emit(k Kind, i int, text string) {
	sc.lx.queue = append(sc.lx.queue, Token{Kind: k, Pos: sc.pos(i), Text: text})
}

func (sc *lineScan) errorf(i int, msg, hint string) {
	sc.lx.diags.Add(sc.pos(i), msg, hint)
}

func (sc *lineScan) eol() bool { return sc.i >= len(sc.s) }

func (sc *lineScan) skipSpace() {
	for sc.i < len(sc.s) && isSpace(sc.s[sc.i]) {
		sc.i++
	}
}

// wordEnd returns the end of the run of non-space bytes starting at i.
func (sc *lineScan) wordEnd(i int) int {
	for i < len(sc.s) && !isSpace(sc.s[i]) {
		i++
	}
	return i
}

func (sc *lineScan) comment() {
	sc.emit(KindComment, sc.i, sc.s[sc.i:])
	sc.i = len(sc.s)
}

func (l *Lexer) lexLine() {
	text, start, _ := l.readLine()
	sc := &lineScan{lx: l, s: text, start: start}
	sc.skipSpace()
	sc.indent = text[:sc.i]
	switch {
	case sc.eol():
		return
	case text[sc.i] == '#':
		sc.comment()
	case text[sc.i] == '}':
		sc.emit(KindRBrace, sc.i, "}")
		sc.i++
		sc.scanArgs(false)
	default:
		j := sc.wordEnd(sc.i)
		keyword := text[sc.i:j]
		sc.emit(KindWord, sc.i, keyword)
		sc.i = j
		switch keyword {
		case "run", "type":
			sc.scanText(keyword)
		case "wait":
			sc.scanWait()
		case "env":
			sc.scanEnv()
		case "shell":
			sc.scanShellArgs()
		case "scene", "hidden":
			sc.scanArgs(true)
		default:
			sc.scanArgs(false)
		}
	}
	if len(l.queue) == 0 {
		return
	}
	nl := sc.pos(len(sc.s))
	if sc.nl != nil {
		nl = *sc.nl
	}
	l.queue = append(l.queue, Token{Kind: KindNewline, Pos: nl})
}

// scanArgs scans words and strings up to an optional comment. With braces, a
// word that is exactly "{" is a KindLBrace.
func (sc *lineScan) scanArgs(braces bool) {
	for {
		sc.skipSpace()
		if sc.eol() {
			return
		}
		switch c := sc.s[sc.i]; {
		case c == '#':
			sc.comment()
		case c == '"':
			if !sc.scanString("put a space or a comment after the closing quote") {
				sc.i = sc.wordEnd(sc.i)
			}
		default:
			j := sc.wordEnd(sc.i)
			w := sc.s[sc.i:j]
			kind := KindWord
			if braces && w == "{" {
				kind = KindLBrace
			}
			sc.emit(kind, sc.i, w)
			sc.i = j
		}
	}
}

// scanText scans the argument of run or type.
func (sc *lineScan) scanText(keyword string) {
	sc.skipSpace()
	if sc.eol() {
		return
	}
	if sc.s[sc.i] == '"' {
		hint := "to type a leading quote use " + keyword + ` "\"hi\" | wc"`
		if !sc.scanString(hint) {
			sc.i = len(sc.s)
			return
		}
		sc.skipSpace()
		switch {
		case sc.eol():
		case sc.s[sc.i] == '#':
			sc.comment()
		default:
			sc.errorf(sc.i, "text after closing quote", hint)
			sc.i = len(sc.s)
		}
		return
	}
	text := strings.TrimRight(sc.s[sc.i:], " \t")
	sc.emit(KindText, sc.i, text)
	sc.i = len(sc.s)
}

// scanWait scans the arguments of wait.
func (sc *lineScan) scanWait() {
	sc.skipSpace()
	if sc.eol() {
		return
	}
	switch sc.s[sc.i] {
	case '"':
		if !sc.scanString("put a space or a comment after the closing quote") {
			sc.i = sc.wordEnd(sc.i)
		}
	case '/':
		if !sc.scanRegex() {
			sc.i = sc.wordEnd(sc.i)
		}
	}
	sc.scanArgs(false)
}

// scanRegex scans "/.../" at sc.i. It reports false if text follows the
// closing slash without a space.
func (sc *lineScan) scanRegex() bool {
	open := sc.i
	var b strings.Builder
	for i := open + 1; i < len(sc.s); i++ {
		switch c := sc.s[i]; c {
		case '/':
			sc.i = i + 1
			raw := sc.s[open:sc.i]
			if i == open+1 {
				sc.errorf(open, "empty regex", `write a pattern between the slashes, or use a quoted string`)
				sc.emit(KindIllegal, open, raw)
				return true
			}
			sc.emit(KindRegex, open, b.String())
			if sc.i < len(sc.s) && !isSpace(sc.s[sc.i]) && sc.s[sc.i] != '#' {
				hint := "put a space or a comment after the closing slash"
				if sc.s[sc.i] >= 'a' && sc.s[sc.i] <= 'z' {
					hint = "regex flags are not supported; start the pattern with (?i) instead"
				}
				sc.errorf(sc.i, "text after closing slash", hint)
				return false
			}
			return true
		case '\\':
			if i+1 >= len(sc.s) {
				i = len(sc.s)
				continue
			}
			if sc.s[i+1] != '/' {
				b.WriteByte('\\')
			}
			b.WriteByte(sc.s[i+1])
			i++
		default:
			b.WriteByte(c)
		}
	}
	sc.errorf(open, "unterminated regex", `add a closing "/" (a slash inside the pattern is written \/)`)
	sc.emit(KindIllegal, open, sc.s[open:])
	sc.i = len(sc.s)
	return true
}

// scanString scans the double-quoted string at sc.i and emits KindString, or
// KindIllegal if it is bad. A string must be followed by a space, a comment or
// the end of the line; otherwise it reports "text after closing quote" with
// the given hint and returns false (the caller skips the junk).
func (sc *lineScan) scanString(trailingHint string) bool {
	open := sc.i
	val, ok := sc.quoted()
	if !ok {
		sc.emit(KindIllegal, open, sc.s[open:sc.i])
		return true
	}
	sc.emit(KindString, open, val)
	if sc.i < len(sc.s) && !isSpace(sc.s[sc.i]) && sc.s[sc.i] != '#' {
		sc.errorf(sc.i, "text after closing quote", trailingHint)
		return false
	}
	return true
}

// quoted decodes the double-quoted string at sc.i and leaves sc.i after the
// closing quote. It reports every bad escape; ok is false if there was one or
// if the string is not closed on this line (sc.i is then at the end of the
// line).
func (sc *lineScan) quoted() (val string, ok bool) {
	const validEscapes = `valid escapes are \n \t \" \\ \e \xHH`
	open := sc.i
	s := sc.s
	var b strings.Builder
	ok = true
	for i := open + 1; i < len(s); {
		c := s[i]
		switch {
		case c == '"':
			sc.i = i + 1
			return b.String(), ok
		case c != '\\':
			b.WriteByte(c)
			i++
		case i+1 >= len(s):
			i++ // a lone backslash at the end: the string is unterminated
		default:
			switch e := s[i+1]; e {
			case 'n':
				b.WriteByte('\n')
				i += 2
			case 't':
				b.WriteByte('\t')
				i += 2
			case '"', '\\':
				b.WriteByte(e)
				i += 2
			case 'e':
				b.WriteByte(0x1b)
				i += 2
			case 'x':
				if i+3 < len(s) && isHex(s[i+2]) && isHex(s[i+3]) {
					b.WriteByte(unhex(s[i+2])<<4 | unhex(s[i+3]))
					i += 4
					continue
				}
				sc.errorf(i, `invalid escape "\x"`, `\x needs two hex digits, for example \x1b`)
				ok = false
				i += 2
			default:
				_, size := utf8.DecodeRuneInString(s[i+1:])
				sc.errorf(i, `unknown escape "`+s[i:i+1+size]+`"`, validEscapes)
				ok = false
				i += 1 + size
			}
		}
	}
	sc.errorf(open, "unterminated string", `add a closing quote`)
	sc.i = len(s)
	return "", false
}

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func unhex(c byte) byte {
	switch {
	case c >= 'a':
		return c - 'a' + 10
	case c >= 'A':
		return c - 'A' + 10
	}
	return c - '0'
}

// scanEnv scans "KEY=VALUE": a KindWord key, a KindAssign, and a value that
// must follow the "=" directly (a string or a word; none means an empty
// value). Anything after that is scanned as ordinary arguments.
func (sc *lineScan) scanEnv() {
	sc.skipSpace()
	if sc.eol() {
		return
	}
	if sc.s[sc.i] == '#' {
		sc.comment()
		return
	}
	j := sc.i
	for j < len(sc.s) && sc.s[j] != '=' && !isSpace(sc.s[j]) {
		j++
	}
	if j > sc.i {
		sc.emit(KindWord, sc.i, sc.s[sc.i:j])
	}
	sc.i = j
	if sc.eol() || sc.s[sc.i] != '=' {
		sc.scanArgs(false)
		return
	}
	sc.emit(KindAssign, sc.i, "=")
	sc.i++
	if !sc.eol() && !isSpace(sc.s[sc.i]) {
		switch sc.s[sc.i] {
		case '#':
		case '"':
			if !sc.scanString("put a space or a comment after the closing quote") {
				sc.i = sc.wordEnd(sc.i)
			}
		default:
			j := sc.wordEnd(sc.i)
			sc.emit(KindWord, sc.i, sc.s[sc.i:j])
			sc.i = j
		}
	}
	sc.scanArgs(false)
}

// scanShellArgs splits the line after "shell" into arguments like a POSIX
// shell, without its expansions. The rule:
//
//   - Arguments are separated by spaces and tabs.
//   - Each argument is one KindWord whose Text is the unquoted value.
//   - A double quote starts a quoted part that runs to the next unescaped
//     double quote and may contain spaces. Inside it the string escapes work
//     (\n \t \" \\ \e \xHH; any other backslash is an error). A quoted part
//     may sit anywhere in an argument: --rcfile="my rc" is one argument.
//     "" is an empty argument.
//   - Outside double quotes every character is literal: no single quotes, no
//     backslash escapes, no variable or glob expansion.
//   - "#" at the start of an argument begins a comment; inside an argument it
//     is literal.
func (sc *lineScan) scanShellArgs() {
	for {
		sc.skipSpace()
		if sc.eol() {
			return
		}
		if sc.s[sc.i] == '#' {
			sc.comment()
			return
		}
		begin := sc.i
		var b strings.Builder
		ok := true
		for !sc.eol() && !isSpace(sc.s[sc.i]) {
			if sc.s[sc.i] != '"' {
				b.WriteByte(sc.s[sc.i])
				sc.i++
				continue
			}
			val, valid := sc.quoted()
			b.WriteString(val)
			ok = ok && valid
		}
		if ok {
			sc.emit(KindWord, begin, b.String())
		} else {
			sc.emit(KindIllegal, begin, sc.s[begin:sc.i])
		}
	}
}

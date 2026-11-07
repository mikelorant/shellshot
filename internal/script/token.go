// Package script implements the shellshot automation script language
// (docs/research/script-format.md). This file set is the lexer; it is pure Go
// and imports no cgo, pty or cobra code.
package script

import "fmt"

// Pos is a position in the source. Line and Col are 1-based; Col and Offset
// count bytes, not runes.
type Pos struct {
	Line   int
	Col    int
	Offset int
}

func (p Pos) String() string { return fmt.Sprintf("%d:%d", p.Line, p.Col) }

// Kind is the kind of a Token.
type Kind int

const (
	// KindIllegal is a lexeme that could not be scanned (unterminated string,
	// bad escape, unterminated text block). The lexer has already reported a
	// Diagnostic; the parser should accept it as an operand without a second
	// error.
	KindIllegal Kind = iota
	// KindEOF ends the token stream and is returned forever after.
	KindEOF
	// KindNewline ends every line that produced a token. Blank lines produce
	// nothing. At the end of a file without a final newline it is synthesised
	// at the end of the last line.
	KindNewline
	// KindComment is "#" up to the end of the line; Text includes the "#".
	KindComment
	// KindWord is an unquoted word. The first word of a line (the keyword) is a
	// KindWord, as are numbers, durations, keys and paths: the parser
	// classifies them.
	KindWord
	// KindString is a quoted string; Text is the decoded value ("\n" in the
	// source is a line feed in Text; "\xHH" is the byte HH, which may be
	// invalid UTF-8: the validator checks that).
	KindString
	// KindRegex is "/.../" after "wait"; Text is the pattern with "\/"
	// replaced by "/". Every other backslash is kept for RE2.
	KindRegex
	// KindText is the rest-of-line text of "run" or "type": trimmed, literal.
	KindText
	// KindAssign is the "=" of "env KEY=VALUE".
	KindAssign
	// KindLBrace is "{" as a word of a "scene" or "hidden" header.
	KindLBrace
	// KindRBrace is a "}" that starts a line.
	KindRBrace
)

var kindNames = [...]string{
	KindIllegal: "ILLEGAL",
	KindEOF:     "EOF",
	KindNewline: "NEWLINE",
	KindComment: "COMMENT",
	KindWord:    "WORD",
	KindString:  "STRING",
	KindRegex:   "REGEX",
	KindText:    "TEXT",
	KindAssign:  "ASSIGN",
	KindLBrace:  "LBRACE",
	KindRBrace:  "RBRACE",
}

func (k Kind) String() string {
	if k < 0 || int(k) >= len(kindNames) {
		return fmt.Sprintf("Kind(%d)", int(k))
	}
	return kindNames[k]
}

// Token is one lexeme with its start position.
type Token struct {
	Kind Kind
	Pos  Pos
	Text string
}

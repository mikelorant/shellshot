package script

import (
	"fmt"
	"strings"
)

// setting parses a setting line. It checks the shape of the arguments only;
// the values are typed by the validator. It reports false, after reporting
// the error, if the line is not usable.
func (p *parser) setting(ln line, at int) (Setting, bool) {
	kw := ln.toks[0]
	if hasIllegal(ln.toks) {
		return Setting{}, false // the lexer has reported it
	}
	st := Setting{Pos: kw.Pos, Name: kw.Text, At: at}
	args := ln.toks[1:]
	switch kw.Text {
	case "shell":
		return st, p.shellSetting(&st, ln)
	case "env":
		return st, p.envSetting(&st, ln)
	}
	if len(args) == 0 {
		p.errorf(ln.end, fmt.Sprintf("%s needs a value", kw.Text), "")
		return st, false
	}
	st.Value = args[0].Text
	return st, p.rejectExtra(args[1:], "a setting takes one value; put a value with spaces in quotes")
}

// shellSetting takes the program and its arguments; Value is the source text
// of the arguments.
func (p *parser) shellSetting(st *Setting, ln line) bool {
	args := ln.toks[1:]
	if len(args) == 0 {
		p.errorf(ln.end, "shell needs the path of a program", "for example shell /bin/bash --noprofile --norc")
		return false
	}
	st.Argv = tokenTexts(args)
	st.Value = strings.TrimRight(p.src[args[0].Pos.Offset:ln.end.Offset], " \t")
	return true
}

// envSetting takes KEY=VALUE, the value directly after the "=".
func (p *parser) envSetting(st *Setting, ln line) bool {
	args := ln.toks[1:]
	switch {
	case len(args) == 0:
		p.errorf(ln.end, "env needs KEY=VALUE", "for example env PS1=\"$ \"")
		return false
	case args[0].Kind == KindAssign:
		p.errorf(args[0].Pos, `env needs a variable name before "="`, "")
		return false
	case len(args) < 2 || args[1].Kind != KindAssign:
		hint := "write KEY=VALUE"
		if len(args) > 1 && args[1].Text == "=" {
			hint = `write KEY=VALUE without spaces around "="`
		}
		p.errorf(args[0].Pos, fmt.Sprintf("expected KEY=VALUE, found %q", args[0].Text), hint)
		return false
	}
	key, assign, rest := args[0], args[1], args[2:]
	st.Value = key.Text + "="
	if len(rest) > 0 && rest[0].Pos.Offset == assign.Pos.Offset+1 {
		st.Value += rest[0].Text
		rest = rest[1:]
	}
	return p.rejectExtra(rest, `write the value directly after "=" and put a value with spaces in quotes`)
}

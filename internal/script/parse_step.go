package script

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/michaellorant/shellshot/internal/script/keys"
)

// step parses a command line: run, type, press, wait, sleep or mark. It
// reports false, after reporting the error, if the line is not a usable step.
func (p *parser) step(ln line) (Step, bool) {
	kw := ln.toks[0]
	if hasIllegal(ln.toks) {
		return Step{}, false // the lexer has reported it
	}
	st := Step{Pos: kw.Pos, Kind: kw.Text}
	switch kw.Text {
	case StepRun, StepType:
		return st, p.textStep(&st, ln)
	case StepPress:
		return st, p.pressStep(&st, ln)
	case StepWait:
		return st, p.waitStep(&st, ln)
	case StepSleep:
		return st, p.sleepStep(&st, ln)
	default:
		return st, p.markStep(&st, ln)
	}
}

// rejectExtra reports the first token of args as unexpected and returns
// false; it returns true when args is empty.
func (p *parser) rejectExtra(args []Token, hint string) bool {
	if len(args) == 0 {
		return true
	}
	p.errorf(args[0].Pos, fmt.Sprintf("unexpected %q", args[0].Text), hint)
	return false
}

func (p *parser) textStep(st *Step, ln line) bool {
	args := ln.toks[1:]
	if len(args) == 0 {
		p.errorf(ln.end, st.Kind+" needs text", "write the text after the command, for example "+st.Kind+" echo hello")
		return false
	}
	switch tok := args[0]; tok.Kind {
	case KindText:
		st.Form, st.Text = FormRaw, tok.Text
	case KindString:
		st.Form, st.Text = FormQuoted, tok.Text
	case KindTextBlock:
		text, diag := tok.Block.Text()
		if diag != nil {
			p.errorf(diag.Pos, diag.Msg, diag.Hint)
			return false
		}
		st.Form, st.Text = string(tok.Block.Shape), text
	}
	return p.rejectExtra(args[1:], "")
}

func (p *parser) pressStep(st *Step, ln line) bool {
	args := ln.toks[1:]
	if len(args) == 0 {
		p.errorf(ln.end, "press needs a key", "for example press enter, press ctrl+c or press down x3")
		return false
	}
	ok, repeated := true, false
	for _, a := range args {
		if a.Kind != KindWord {
			p.errorf(a.Pos, fmt.Sprintf("expected a key, found %q", a.Text), "key names are written without quotes; to type text use type")
			ok = false
			continue
		}
		if n, isRepeat := keys.ParseRepeat(a.Text); isRepeat {
			switch {
			case len(st.Keys) == 0:
				p.errorf(a.Pos, fmt.Sprintf("repeat %q has no key before it", a.Text), "write the key first, for example press down "+a.Text)
				ok = false
			case repeated:
				p.errorf(a.Pos, fmt.Sprintf("repeat %q follows another repeat", a.Text), "a key takes one repeat")
				ok = false
			default:
				st.Keys[len(st.Keys)-1].Count, repeated = n, true
			}
			continue
		}
		if repeatSyntax.MatchString(a.Text) {
			p.errorf(a.Pos, fmt.Sprintf("repeat count %q is out of range", a.Text), "a repeat is x1 to x999")
			ok = false
			continue
		}
		chord, err := keys.Parse(a.Text)
		if err != nil {
			p.errorf(a.Pos, err.Error(), "")
			ok = false
			continue
		}
		st.Keys, repeated = append(st.Keys, KeyPress{Chord: chord, Count: 1}), false
	}
	return ok
}

// repeatSyntax matches the shape of a repeat token, in range or not.
var repeatSyntax = regexp.MustCompile(`^x[0-9]+$`)

func (p *parser) waitStep(st *Step, ln line) bool {
	args := ln.toks[1:]
	if len(args) == 0 {
		p.errorf(ln.end, "wait needs a pattern", `write wait "text", wait /regex/ or wait idle`)
		return false
	}
	switch first := args[0]; {
	case first.Kind == KindString:
		st.Pattern = &Pattern{Source: first.Text}
	case first.Kind == KindRegex:
		st.Pattern = &Pattern{Regex: true, Source: first.Text}
	case first.Kind == KindWord && first.Text == "idle":
		st.Idle = true
	default:
		hint := DidYouMean(first.Text, []string{"idle"})
		if hint == "" {
			hint = `to wait for text put it in quotes (wait "text") or in slashes (wait /regex/)`
		}
		p.errorf(first.Pos, fmt.Sprintf(`expected a "string", a /regex/ or idle after wait, found %q`, first.Text), hint)
		return false
	}
	rest := args[1:]
	if len(rest) > 0 {
		var ok bool
		if st.Duration, ok = p.duration(rest[0]); !ok {
			return false
		}
		st.HasDuration, rest = true, rest[1:]
	}
	return p.rejectExtra(rest, "")
}

// duration parses a duration operand and reports the error.
func (p *parser) duration(tok Token) (d time.Duration, ok bool) {
	if tok.Kind != KindWord {
		p.errorf(tok.Pos, fmt.Sprintf("expected a duration, found %q", tok.Text), "a duration is a number with a unit, like 500ms, 1.5s or 2m, without quotes")
		return 0, false
	}
	d, err := ParseDuration(tok.Text)
	if err != nil {
		p.errorf(tok.Pos, fmt.Sprintf("bad duration %q", tok.Text), err.Error())
		return 0, false
	}
	return d, true
}

func (p *parser) sleepStep(st *Step, ln line) bool {
	args := ln.toks[1:]
	if len(args) == 0 {
		p.errorf(ln.end, "sleep needs a duration", "for example sleep 500ms")
		return false
	}
	var ok bool
	if st.Duration, ok = p.duration(args[0]); !ok {
		return false
	}
	st.HasDuration = true
	return p.rejectExtra(args[1:], "")
}

func (p *parser) markStep(st *Step, ln line) bool {
	args := ln.toks[1:]
	if len(args) == 0 {
		p.errorf(ln.end, "mark needs a label", "for example mark done")
		return false
	}
	st.Label = args[0].Text
	return p.rejectExtra(args[1:], `a label with spaces needs quotes: mark "`+strings.Join(tokenTexts(args), " ")+`"`)
}

func tokenTexts(toks []Token) []string {
	out := make([]string, len(toks))
	for i, t := range toks {
		out[i] = t.Text
	}
	return out
}

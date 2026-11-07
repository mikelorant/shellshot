package script

import (
	"errors"
	"fmt"
	"regexp"
	"regexp/syntax"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/michaellorant/shellshot/internal/script/keys"
)

// Limits on step operands.
const (
	// maxTextBytes is the longest text of one run or type step. The docs set
	// no limit; this one keeps a step typeable in a reasonable recording.
	maxTextBytes = 64 << 10
	maxRepeat    = 999
	// maxShownPattern is how much of a regular expression a message quotes.
	maxShownPattern = 40
)

const controlHint = `a quoted string can hold control characters, for example "\e[1m"`

// step validates the operands of one step.
func (v *validator) step(st Step) {
	switch st.Kind {
	case StepRun, StepType:
		v.textStep(st)
	case StepPress:
		v.pressStep(st)
	case StepWait:
		v.waitStep(st)
	case StepSleep:
		switch {
		case !st.HasDuration:
			v.add(st.Pos, "sleep needs a duration", "for example sleep 500ms")
		case st.Duration < 0:
			v.negativeDuration(st)
		}
	case StepMark:
		v.markStep(st)
	default:
		v.unknownWord(st.Pos, st.Kind)
	}
}

func (v *validator) negativeDuration(st Step) {
	v.add(st.Pos, fmt.Sprintf("bad duration %q", st.Duration.String()), "a duration cannot be negative")
}

// textStep checks the text of run and type: a quoted string may hold control
// characters (that is what it is for); raw text may hold none but tab; a
// text block may also hold line feeds, which separate its lines.
func (v *validator) textStep(st Step) {
	what := st.Kind + " text"
	var problem string
	switch {
	case st.Text == "":
		v.add(st.Pos, what+" is empty", `to press Enter alone use "press enter"`)
		return
	case len(st.Text) > maxTextBytes:
		v.add(st.Pos, fmt.Sprintf("%s is too long: %d bytes", what, len(st.Text)),
			fmt.Sprintf("the limit is %d bytes; split it into several steps", maxTextBytes))
		return
	case st.Form == FormQuoted:
		if !utf8.ValidString(st.Text) {
			problem = notUTF8
		}
	default:
		problem = textProblem(st.Text, st.Form == FormLines || st.Form == FormParagraphs)
	}
	if problem != "" {
		v.add(st.Pos, what+" "+problem, problemHint(problem))
	}
}

func problemHint(problem string) string {
	if problem == notUTF8 {
		return utf8Hint
	}
	return controlHint
}

func (v *validator) markStep(st Step) {
	if st.Label == "" {
		v.add(st.Pos, "mark label is empty", "for example mark done")
		return
	}
	if problem := textProblem(st.Label, false); problem != "" {
		v.add(st.Pos, "mark label "+problem, problemHint(problem))
	}
}

func (v *validator) pressStep(st Step) {
	if len(st.Keys) == 0 {
		v.add(st.Pos, "press needs a key", "for example press enter, press ctrl+c or press down x3")
		return
	}
	for _, kp := range st.Keys {
		if kp.Chord.Key.String() == "" || kp.Chord.Mods&^(keys.ModCtrl|keys.ModAlt|keys.ModShift) != 0 {
			v.add(st.Pos, "press has a key that is not valid", "key names are written like enter, ctrl+c or f5")
			return
		}
		if kp.Count < 1 || kp.Count > maxRepeat {
			v.add(st.Pos, fmt.Sprintf("repeat count %q is out of range", fmt.Sprintf("x%d", kp.Count)),
				fmt.Sprintf("a repeat is x1 to x%d", maxRepeat))
			return
		}
	}
}

func (v *validator) waitStep(st Step) {
	switch {
	case st.Idle && st.Pattern != nil:
		v.add(st.Pos, "wait idle takes no pattern", "")
		return
	case st.Idle:
		if st.HasDuration && st.Duration <= 0 {
			v.add(st.Pos, fmt.Sprintf("bad duration %q", st.Duration.String()), "wait idle needs a duration greater than 0")
		}
		return
	case st.Pattern == nil:
		v.add(st.Pos, "wait needs a pattern", `write wait "text", wait /regex/ or wait idle`)
		return
	}
	if st.HasDuration && st.Duration < 0 {
		v.negativeDuration(st)
	}
	v.pattern(st.Pos, *st.Pattern)
}

func (v *validator) pattern(pos Pos, p Pattern) {
	switch {
	case p.Source == "":
		v.add(pos, "wait pattern is empty", "an empty pattern matches every screen; write the text to wait for")
	case !utf8.ValidString(p.Source):
		v.add(pos, "wait pattern "+notUTF8, utf8Hint)
	case p.Regex:
		v.regex(pos, p.Source)
	}
}

// regex checks that src compiles as a Go RE2 expression. The executor adds
// the flag (?m) in front of it; that cannot change whether it compiles.
func (v *validator) regex(pos Pos, src string) {
	_, err := regexp.Compile(src)
	if err == nil {
		return
	}
	shown := src
	if r := []rune(shown); len(r) > maxShownPattern {
		shown = string(r[:maxShownPattern]) + "..."
	}
	hint := err.Error()
	var serr *syntax.Error
	if errors.As(err, &serr) {
		hint = string(serr.Code)
		if serr.Expr != "" && serr.Expr != src && strings.Contains(src, serr.Expr) {
			hint += " near " + quoteFragment(serr.Expr)
		}
	}
	v.add(pos, fmt.Sprintf("bad regular expression /%s/", shown), hint)
}

// quoteFragment quotes a piece of a regular expression, keeping its
// backslashes single when every character is printable.
func quoteFragment(s string) string {
	if !strings.Contains(s, `"`) && !strings.ContainsFunc(s, func(r rune) bool { return !unicode.IsPrint(r) }) {
		return `"` + s + `"`
	}
	return strconv.Quote(s)
}

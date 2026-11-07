package script

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Setting classes of docs/research/script-format.md §3.2.
const (
	classProcess   = "process"
	classBehaviour = "behaviour"
	classHint      = "export hint"
)

var settingClasses = map[string]string{
	"size": classProcess, "shell": classProcess, "env": classProcess,
	"cwd": classProcess, "seed": classProcess, "output": classProcess,
	"typing": classBehaviour, "jitter": classBehaviour, "timeout": classBehaviour,
	"settle": classBehaviour, "linger": classBehaviour,
	"speed": classHint, "idle": classHint, "hold": classHint,
}

// Limits on setting values (docs §3.2). The speed bounds are the range a
// SPAN hint can hold: milli-x in a uvarint (§7.2), at least 0.001x.
const (
	maxSizeDimension = 1000
	minSpeed         = 0.001
	maxSpeed         = 1_000_000
	recordingExt     = ".shot"
)

// setting validates the placement and the value of one setting. seen holds
// the earlier settings of the same scope.
func (v *validator) setting(ctx blockContext, st Setting, seen map[string]Setting) {
	class, known := settingClasses[st.Name]
	if !known {
		v.unknownWord(st.Pos, st.Name)
		return
	}
	if !v.placed(ctx, st, class) {
		v.value(st)
		return
	}
	prev, again := seen[st.Name]
	seen[st.Name] = st
	switch {
	case st.Name == "env", !again:
	case class == classBehaviour && prev.At != st.At:
		// A behaviour setting may change again after a step: it applies from
		// its line on (docs §3.2).
	case class == classBehaviour:
		v.add(st.Pos, fmt.Sprintf("%q is set twice with no step between", st.Name),
			fmt.Sprintf("the value on line %d is never used", prev.Pos.Line))
	default:
		v.add(st.Pos, fmt.Sprintf("duplicate %q", st.Name), fmt.Sprintf("first set on line %d", prev.Pos.Line))
	}
	v.value(st)
}

// placed checks the legality table of docs §3.2 and reports whether the
// setting is legal where it is.
func (v *validator) placed(ctx blockContext, st Setting, class string) bool {
	preamble := ctx.kind == "" && st.At == 0
	switch class {
	case classBehaviour:
		return true
	case classHint:
		switch {
		case preamble, ctx.kind == BlockScene:
			return true
		case ctx.kind == BlockHidden:
			v.add(st.Pos, fmt.Sprintf("%q cannot be used in a hidden block", st.Name),
				"a hidden span has no output; set export hints in the preamble or in a scene")
		default:
			v.add(st.Pos, fmt.Sprintf("%q must appear before the first step or inside a scene (first step: line %d)", st.Name, v.firstLine), "")
		}
		return false
	}
	if preamble {
		return true
	}
	hint := ""
	if ctx.kind != "" {
		hint = fmt.Sprintf("a process setting cannot be set in %s", ctx.describe())
	}
	v.add(st.Pos, fmt.Sprintf("%q must appear before the first step (first step: line %d)", st.Name, v.firstLine), hint)
	return false
}

// value checks the value of a known setting.
func (v *validator) value(st Setting) {
	switch st.Name {
	case "size":
		v.sizeValue(st)
	case "shell":
		v.shellValue(st)
	case "env":
		v.envValue(st)
	case "cwd":
		v.pathValue(st)
	case "output":
		v.outputValue(st)
	case "seed":
		if _, err := strconv.ParseUint(st.Value, 10, 64); err != nil {
			v.add(st.Pos, fmt.Sprintf("bad seed %q", st.Value), "a seed is a whole number, 0 or more")
		}
	case "jitter":
		v.jitterValue(st)
	case "speed":
		v.speedValue(st)
	case "settle":
		if st.Value != "off" {
			v.durationValue(st, false)
		}
	case "timeout", "idle":
		v.durationValue(st, true)
	default: // typing, linger, hold
		v.durationValue(st, false)
	}
}

func (v *validator) sizeValue(st Setting) {
	cols, rows, ok := strings.Cut(st.Value, "x")
	cn, cok := parseDimension(cols)
	rn, rok := parseDimension(rows)
	switch {
	case !ok || !cok || !rok:
		v.add(st.Pos, fmt.Sprintf("bad size %q", st.Value), "write COLSxROWS, for example 80x24")
	case cn < 1 || cn > maxSizeDimension:
		v.add(st.Pos, fmt.Sprintf("bad size %q", st.Value), fmt.Sprintf("columns must be 1 to %d", maxSizeDimension))
	case rn < 1 || rn > maxSizeDimension:
		v.add(st.Pos, fmt.Sprintf("bad size %q", st.Value), fmt.Sprintf("rows must be 1 to %d", maxSizeDimension))
	}
}

// parseDimension parses decimal digits; a value too long for an int is
// reported as out of range, not as malformed.
func parseDimension(s string) (n int, ok bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return maxSizeDimension + 1, true
	}
	return n, true
}

// parseNumber parses digits with an optional ".digits" fraction. A number
// too large for a float64 is +Inf.
func parseNumber(s string) (float64, bool) {
	whole, frac, hasFrac := strings.Cut(s, ".")
	if whole == "" || strings.Trim(whole, "0123456789") != "" ||
		hasFrac && (frac == "" || strings.Trim(frac, "0123456789") != "") {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0, false
	}
	return f, true
}

func (v *validator) jitterValue(st Setting) {
	number, ok := strings.CutSuffix(st.Value, "%")
	f, numeric := parseNumber(number)
	switch {
	case !ok || !numeric:
		v.add(st.Pos, fmt.Sprintf("bad jitter %q", st.Value), "a percentage is a number with %, like 25%")
	case f > 100:
		v.add(st.Pos, fmt.Sprintf("bad jitter %q", st.Value), "jitter is 0% to 100%")
	}
}

func (v *validator) speedValue(st Setting) {
	number, ok := strings.CutSuffix(st.Value, "x")
	f, numeric := parseNumber(number)
	switch {
	case !ok || !numeric:
		v.add(st.Pos, fmt.Sprintf("bad speed %q", st.Value), "a speed is a number with x, like 8x")
	case f < minSpeed || f > maxSpeed:
		v.add(st.Pos, fmt.Sprintf("bad speed %q", st.Value), "speed is 0.001x to 1000000x")
	}
}

// durationValue checks a duration setting; positive rejects 0s.
func (v *validator) durationValue(st Setting, positive bool) {
	d, err := ParseDuration(st.Value)
	switch {
	case err != nil:
		v.add(st.Pos, fmt.Sprintf("bad duration %q", st.Value), err.Error())
	case positive && d <= 0:
		v.add(st.Pos, fmt.Sprintf("bad %s %q", st.Name, st.Value), st.Name+" must be greater than 0")
	}
}

func (v *validator) shellValue(st Setting) {
	if len(st.Argv) == 0 || st.Argv[0] == "" {
		v.add(st.Pos, "shell needs the path of a program", "for example shell /bin/bash --noprofile --norc")
		return
	}
	for i, arg := range st.Argv {
		if problem := textProblem(arg, false); problem != "" {
			what := "shell path"
			if i > 0 {
				what = fmt.Sprintf("shell argument %d", i)
			}
			v.add(st.Pos, fmt.Sprintf("%s %s", what, problem), "")
			return
		}
	}
}

func (v *validator) envValue(st Setting) {
	key, value, ok := strings.Cut(st.Value, "=")
	switch {
	case !ok:
		v.add(st.Pos, fmt.Sprintf("bad env %q", st.Value), "write KEY=VALUE")
	case !isIdentifier(key):
		v.add(st.Pos, fmt.Sprintf("bad environment variable name %q", key), "a name is letters, digits and _, and does not start with a digit")
	case !utf8.ValidString(value):
		v.add(st.Pos, "env value is not valid UTF-8", utf8Hint)
	case strings.IndexByte(value, 0) >= 0:
		v.add(st.Pos, "env value contains a NUL character", "an environment variable cannot hold NUL")
	}
}

// pathValue checks a path that must be set and free of control characters.
// It reports whether the path is usable.
func (v *validator) pathValue(st Setting) bool {
	if st.Value == "" {
		v.add(st.Pos, st.Name+" needs a path", "")
		return false
	}
	if problem := textProblem(st.Value, false); problem != "" {
		v.add(st.Pos, fmt.Sprintf("%s path %s", st.Name, problem), "")
		return false
	}
	return true
}

func (v *validator) outputValue(st Setting) {
	if !v.pathValue(st) {
		return
	}
	base := filepath.Base(st.Value)
	if strings.HasSuffix(st.Value, "/") || base == recordingExt || filepath.Ext(base) != recordingExt {
		v.add(st.Pos, fmt.Sprintf("bad output path %q", st.Value), "a recording file name ends in "+recordingExt)
	}
}

const utf8Hint = `a "\xHH" escape must form valid UTF-8 together with its neighbours; the file itself must be UTF-8`

// textProblem describes why s may not be used, or returns "" if it may: it
// must be valid UTF-8 and have no control characters other than tab, and
// other than line feed when lineFeed is set.
func textProblem(s string, lineFeed bool) string {
	if !utf8.ValidString(s) {
		return "is not valid UTF-8"
	}
	for _, r := range s {
		if unicode.IsControl(r) && r != '\t' && !(lineFeed && r == '\n') {
			return fmt.Sprintf("contains control character U+%04X", r)
		}
	}
	return ""
}

package run

import (
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/mikelorant/shellshot/internal/recording/bin/event"
)

// maxListedText is the longest text shown for a type op in the listing, in runes.
const maxListedText = 60

// Format returns the readable plan listing printed by "record --script
// --list": the process settings, the estimated durations, and one line per op
// with its source position, its scene and the settings in effect. The text is
// a pure function of the plan.
func (p *Plan) Format() string {
	var b strings.Builder
	pr := p.Process
	b.WriteString("process\n")
	if pr.Cols > 0 {
		fmt.Fprintf(&b, "  size    %dx%d\n", pr.Cols, pr.Rows)
	} else {
		b.WriteString("  size    (terminal)\n")
	}
	if len(pr.Shell) > 0 {
		fmt.Fprintf(&b, "  shell   %s\n", joinArgs(pr.Shell))
	} else {
		b.WriteString("  shell   (default)\n")
	}
	for _, e := range pr.Env {
		fmt.Fprintf(&b, "  env     %s\n", quoteArg(e))
	}
	if pr.Cwd != "" {
		fmt.Fprintf(&b, "  cwd     %s\n", quoteArg(pr.Cwd))
	}
	if pr.Output != "" {
		fmt.Fprintf(&b, "  output  %s\n", quoteArg(pr.Output))
	} else {
		b.WriteString("  output  (default)\n")
	}
	fmt.Fprintf(&b, "  seed    %d\n", pr.Seed)

	est := p.Estimate()
	b.WriteString("estimate\n")
	fmt.Fprintf(&b, "  total   %s to %s, %s, %s\n", dur(est.Min), dur(est.Max),
		plural(est.Waits, "wait"), plural(len(est.Scenes), "scene"))
	for _, s := range est.Scenes {
		fmt.Fprintf(&b, "  scene   #%d %s: %s to %s\n", s.ID, s.Name, dur(s.Min), dur(s.Max))
	}

	b.WriteString("ops\n")
	for i, op := range p.Ops {
		m := op.OpMeta()
		fmt.Fprintf(&b, "%4d  %-7s %-16s %s\n", i+1, posText(m), scopeText(m), opText(op))
	}
	return b.String()
}

func posText(m Meta) string {
	if m.Pos.Line == 0 {
		return "-"
	}
	return fmt.Sprintf("%d:%d", m.Pos.Line, m.Pos.Col)
}

// scopeText names where an op sits: "-", "NAME", "hidden" or "NAME/hidden".
func scopeText(m Meta) string {
	switch {
	case m.Scene != "" && m.Hidden:
		return m.Scene + "/hidden"
	case m.Hidden:
		return "hidden"
	case m.Scene != "":
		return m.Scene
	}
	return "-"
}

func opText(op Op) string {
	switch op := op.(type) {
	case OpType:
		return fmt.Sprintf("type %s typing=%s%s", quoteShort(op.Text), dur(op.PerChar), jitterText(op.Jitter))
	case OpPress:
		s := "press " + op.Chord.String()
		if op.Repeat > 1 {
			s += fmt.Sprintf(" x%d", op.Repeat)
		}
		if op.Gap {
			s += " after-gap"
		}
		return s + " typing=" + dur(op.Eff.Typing) + jitterText(op.Eff.Jitter)
	case OpWait:
		switch {
		case op.Kind == WaitIdle:
			return fmt.Sprintf("wait idle quiet=%s timeout=%s", dur(op.Quiet), dur(op.Timeout))
		case op.Assert:
			return fmt.Sprintf("wait %s assert", patternText(op))
		}
		return fmt.Sprintf("wait %s timeout=%s", patternText(op), dur(op.Timeout))
	case OpSettle:
		switch {
		case op.Off:
			return "settle off"
		case op.Drain:
			return fmt.Sprintf("drain quiet=%s timeout=%s (never fails)", dur(op.Quiet), dur(op.Timeout))
		}
		return fmt.Sprintf("settle quiet=%s timeout=%s (fails at timeout)", dur(op.Quiet), dur(op.Timeout))
	case OpSleep:
		return "sleep " + dur(op.Duration)
	case OpLinger:
		return "linger " + dur(op.Duration)
	case OpMark:
		return "mark " + strconv.Quote(op.Label)
	case OpSpanBegin:
		s := fmt.Sprintf("span-begin #%d ", op.ID)
		if op.Type == event.SpanScene {
			s += "scene " + strconv.Quote(op.Name)
		} else {
			s += "hidden"
		}
		if op.Parent != 0 {
			s += fmt.Sprintf(" parent=#%d", op.Parent)
		}
		return s + hintsText(op.Hints)
	case OpSpanEnd:
		return fmt.Sprintf("span-end #%d", op.ID)
	case OpExit:
		return "exit grace=" + dur(op.Grace)
	}
	return fmt.Sprintf("%T", op)
}

func patternText(op OpWait) string {
	if op.Kind == WaitRegex {
		return "/" + op.Pattern + "/"
	}
	return strconv.Quote(op.Pattern)
}

func hintsText(h event.SpanHints) string {
	var s string
	if h.Speed != 0 {
		s += " speed=" + strconv.FormatFloat(float64(h.Speed)/1000, 'f', -1, 64) + "x"
	}
	if h.Idle != 0 {
		s += " idle=" + dur(h.Idle)
	}
	if h.Hold != 0 {
		s += " hold=" + dur(h.Hold)
	}
	return s
}

func jitterText(pct float64) string {
	if pct == 0 {
		return ""
	}
	return " jitter=" + strconv.FormatFloat(pct, 'f', -1, 64) + "%"
}

// dur formats d rounded to the millisecond.
func dur(d time.Duration) string { return d.Round(time.Millisecond).String() }

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// quoteShort quotes s, cutting it after maxListedText runes.
func quoteShort(s string) string {
	if utf8.RuneCountInString(s) <= maxListedText {
		return strconv.Quote(s)
	}
	r := []rune(s)
	return strconv.Quote(string(r[:maxListedText])) + fmt.Sprintf("... (%d runes)", len(r))
}

// joinArgs shows an argv, quoting the arguments that need it.
func joinArgs(argv []string) string {
	parts := make([]string, len(argv))
	for i, a := range argv {
		parts[i] = quoteArg(a)
	}
	return strings.Join(parts, " ")
}

// quoteArg quotes a if it is empty or has whitespace, a quote, a backslash
// or a leading "#", so that the listing shows it unambiguously.
func quoteArg(a string) string {
	if a == "" || strings.ContainsFunc(a, func(r rune) bool { return unicode.IsSpace(r) || r == '"' || r == '\\' }) || strings.HasPrefix(a, "#") {
		return strconv.Quote(a)
	}
	return a
}

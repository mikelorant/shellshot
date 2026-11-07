package script

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Format returns the canonical text of a script: two spaces of indentation
// per block level, one space between words, a blank line between settings,
// steps and blocks, comments dropped, durations in their shortest unit, text
// blocks as blocks, and strings quoted only when they need it. For a script
// that Parse accepted, Parse(Format(s)) gives the same AST except for
// positions, and Format of that is the same text.
func Format(s *Script) string {
	var f formatter
	f.scope("", s.Settings, s.Items)
	return f.b.String()
}

// Line groups; a blank line separates different groups, and every block
// (including a multi-line text block) is a group of its own.
const (
	groupSetting = "setting"
	groupStep    = "step"
	groupBlock   = "block"
)

type formatter struct {
	b    strings.Builder
	prev string // group of the previous line in this scope, "" at its start
}

func (f *formatter) emit(group, indent, text string) {
	if f.prev != "" && (group != f.prev || group == groupBlock) {
		f.b.WriteByte('\n')
	}
	f.prev = group
	f.b.WriteString(indent + text + "\n")
}

// scope writes the settings and items of a script or block in source order.
func (f *formatter) scope(indent string, settings []Setting, items []Item) {
	si := 0
	for i := 0; i <= len(items); i++ {
		for ; si < len(settings) && settings[si].At <= i; si++ {
			f.emit(groupSetting, indent, formatSetting(settings[si]))
		}
		if i < len(items) {
			f.item(indent, items[i])
		}
	}
	for ; si < len(settings); si++ {
		f.emit(groupSetting, indent, formatSetting(settings[si]))
	}
}

func (f *formatter) item(indent string, it Item) {
	switch it := it.(type) {
	case Block:
		header := it.Kind + " {"
		if it.Kind == BlockScene {
			header = it.Kind + " " + it.Name + " {"
		}
		f.emit(groupBlock, indent, header)
		f.prev = ""
		f.scope(indent+"  ", it.Settings, it.Items)
		f.b.WriteString(indent + "}\n")
		f.prev = groupBlock
	case Step:
		f.step(indent, it)
	}
}

func formatSetting(st Setting) string {
	switch st.Name {
	case "shell":
		args := make([]string, len(st.Argv))
		for i, a := range st.Argv {
			args[i] = formatArg(a)
		}
		return "shell " + strings.Join(args, " ")
	case "env":
		key, val, _ := strings.Cut(st.Value, "=")
		if val != "" {
			val = formatArg(val)
		}
		return "env " + key + "=" + val
	}
	return st.Name + " " + formatArg(st.Value)
}

func (f *formatter) step(indent string, st Step) {
	switch st.Kind {
	case StepRun, StepType:
		f.textStep(indent, st)
	case StepPress:
		parts := make([]string, len(st.Keys))
		for i, k := range st.Keys {
			parts[i] = k.Chord.String()
			if k.Count > 1 {
				parts[i] += fmt.Sprintf(" x%d", k.Count)
			}
		}
		f.emit(groupStep, indent, "press "+strings.Join(parts, " "))
	case StepWait:
		text := "wait " + formatPattern(st)
		if st.HasDuration {
			text += " " + formatDuration(st.Duration)
		}
		f.emit(groupStep, indent, text)
	case StepSleep:
		f.emit(groupStep, indent, "sleep "+formatDuration(st.Duration))
	case StepMark:
		f.emit(groupStep, indent, "mark "+formatArg(st.Label))
	}
}

func formatPattern(st Step) string {
	switch {
	case st.Idle:
		return "idle"
	case st.Pattern.Regex:
		return "/" + escapeSlashes(st.Pattern.Source) + "/"
	}
	return quote(st.Pattern.Source)
}

// escapeSlashes writes "/" as "\/" in a regex, leaving escape pairs alone.
func escapeSlashes(re string) string {
	var b strings.Builder
	for i := 0; i < len(re); i++ {
		switch {
		case re[i] == '\\' && i+1 < len(re):
			b.WriteString(re[i : i+2])
			i++
		case re[i] == '/':
			b.WriteString(`\/`)
		default:
			b.WriteByte(re[i])
		}
	}
	return b.String()
}

// textStep writes a run or type step in the form it was written in, or as a
// quoted string when the text cannot be written that way and read back.
func (f *formatter) textStep(indent string, st Step) {
	switch st.Form {
	case FormRaw:
		if rawSafe(st.Text) {
			f.emit(groupStep, indent, st.Kind+" "+st.Text)
			return
		}
	case FormLines, FormParagraphs:
		if body, ok := blockBody(indent+"  ", st); ok {
			f.emit(groupBlock, indent, st.Kind+" "+st.Form+" {")
			for _, l := range body {
				f.b.WriteString(l + "\n")
			}
			f.b.WriteString(indent + "}\n")
			return
		}
	}
	f.emit(groupStep, indent, st.Kind+" "+quote(st.Text))
}

// rawSafe reports whether text, written after run or type, reads back as the
// same raw text.
func rawSafe(text string) bool {
	if text == "" || strings.ContainsAny(text, "\r\n") || text[0] == '"' {
		return false
	}
	if strings.ContainsAny(text[:1], " \t") || strings.ContainsAny(text[len(text)-1:], " \t") {
		return false
	}
	_, isHeader := textBlockHeader(text)
	return !isHeader
}

// blockBody returns the body lines of a text block for st, each with the
// indent, or false if Resolve would not give st.Text back from them.
func blockBody(indent string, st Step) ([]string, bool) {
	text := st.Text
	if st.Kind == StepRun {
		var ok bool
		if text, ok = strings.CutSuffix(text, "\n"); !ok {
			return nil, false
		}
	}
	sep := "\n"
	if st.Form == FormParagraphs {
		sep = "\n\n"
	}
	raw := strings.Split(text, sep)
	if st.Form == FormParagraphs {
		raw = interleaveBlank(raw)
	}
	var body []string
	for _, l := range raw {
		if strings.ContainsAny(l, "\r\n") {
			return nil, false
		}
		if l != "" {
			l = indent + l
		}
		body = append(body, l)
	}
	got, err := Resolve(Shape(st.Form), st.Kind, body)
	return body, err == nil && got == st.Text
}

// interleaveBlank puts a blank line between paragraphs.
func interleaveBlank(paragraphs []string) []string {
	var out []string
	for i, p := range paragraphs {
		if i > 0 {
			out = append(out, "")
		}
		out = append(out, p)
	}
	return out
}

// formatArg writes s as a bare word, or quoted if it would not read back as
// one word.
func formatArg(s string) string {
	if s == "" || s[0] == '#' || !utf8.ValidString(s) {
		return quote(s)
	}
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '"' || r == '\\' || r < 0x20 || r == 0x7f {
			return quote(s)
		}
	}
	return s
}

// quote writes s as a double-quoted string that the lexer reads back to s.
func quote(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && size == 1:
			fmt.Fprintf(&b, `\x%02x`, s[i])
		case r == '"' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == 0x1b:
			b.WriteString(`\e`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
		i += size
	}
	b.WriteByte('"')
	return b.String()
}

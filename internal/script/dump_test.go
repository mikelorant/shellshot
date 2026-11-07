package script

import (
	"fmt"
	"strings"
)

// dumpAST renders a script as indented text for golden files. With pos false
// it leaves out positions and the raw shell text, and it treats the text
// forms that the printer cannot keep for text with a carriage return as
// quoted, so that two scripts with the same meaning dump alike.
func dumpAST(s *Script, pos bool) string {
	var b strings.Builder
	b.WriteString("Script\n")
	dumpScope(&b, 1, s.Settings, s.Items, pos)
	return b.String()
}

func dumpScope(b *strings.Builder, depth int, settings []Setting, items []Item, pos bool) {
	ind := strings.Repeat("  ", depth)
	if len(settings) > 0 {
		b.WriteString(ind + "Settings\n")
	}
	for _, st := range settings {
		b.WriteString(ind + "  " + dumpSetting(st, pos) + "\n")
	}
	if len(items) > 0 {
		b.WriteString(ind + "Items\n")
	}
	for _, it := range items {
		switch it := it.(type) {
		case Step:
			b.WriteString(ind + "  " + dumpStep(it, pos) + "\n")
		case Block:
			head := fmt.Sprintf("Block %s %q", it.Kind, it.Name)
			if pos {
				head += fmt.Sprintf(" @%v-%v", it.Pos, it.End)
			}
			b.WriteString(ind + "  " + head + "\n")
			dumpScope(b, depth+2, it.Settings, it.Items, pos)
		}
	}
}

func dumpSetting(st Setting, pos bool) string {
	s := fmt.Sprintf("%s %q at=%d", st.Name, st.Value, st.At)
	if st.Name == "shell" {
		s = fmt.Sprintf("shell argv=%q at=%d", st.Argv, st.At)
		if pos {
			s += fmt.Sprintf(" raw=%q", st.Value)
		}
	}
	if pos {
		s += fmt.Sprintf(" @%v", st.Pos)
	}
	return s
}

func dumpStep(st Step, pos bool) string {
	var s string
	switch st.Kind {
	case StepRun, StepType:
		form := st.Form
		if !pos && strings.Contains(st.Text, "\r") {
			form = FormQuoted
		}
		s = fmt.Sprintf("Step %s %s %q", st.Kind, form, st.Text)
	case StepPress:
		s = "Step press"
		for _, k := range st.Keys {
			s += fmt.Sprintf(" %v*%d", k.Chord, k.Count)
		}
	case StepWait:
		s = "Step wait " + dumpPattern(st)
		if st.HasDuration {
			s += " " + formatDuration(st.Duration)
		}
	case StepSleep:
		s = "Step sleep " + formatDuration(st.Duration)
	case StepMark:
		s = fmt.Sprintf("Step mark %q", st.Label)
	}
	if pos {
		s += fmt.Sprintf(" @%v", st.Pos)
	}
	return s
}

func dumpPattern(st Step) string {
	switch {
	case st.Idle:
		return "idle"
	case st.Pattern.Regex:
		return fmt.Sprintf("regex %q", st.Pattern.Source)
	}
	return fmt.Sprintf("string %q", st.Pattern.Source)
}

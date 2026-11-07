package script

import (
	"cmp"
	"fmt"
	"slices"
)

// Validate checks what the parser leaves out (docs/research/script-format.md
// §3.2, §3.3, §4.3): where each setting may appear, the values of settings,
// the structure of blocks and scene names. The result is sorted by position
// and holds at most MaxDiagnostics entries; it is empty for a valid script.
// Diagnostics point at the command, setting or block keyword, because the
// AST keeps no position for operands.
func Validate(s *Script) []Diagnostic {
	if s == nil {
		return nil
	}
	v := &validator{sceneLine: map[string]int{}}
	if len(s.Items) > 0 {
		v.firstLine = s.Items[0].ItemPos().Line
	}
	v.scope(blockContext{}, s.Settings, s.Items)
	slices.SortStableFunc(v.diags, func(a, b Diagnostic) int {
		return cmp.Or(cmp.Compare(a.Pos.Line, b.Pos.Line), cmp.Compare(a.Pos.Col, b.Pos.Col), cmp.Compare(a.Pos.Offset, b.Pos.Offset))
	})
	if len(v.diags) > MaxDiagnostics {
		v.diags = v.diags[:MaxDiagnostics]
	}
	return v.diags
}

type validator struct {
	diags     []Diagnostic
	firstLine int            // line of the first item of the script, 0 if none
	sceneLine map[string]int // scene name -> line of its first use
}

// blockContext is where a setting or an item sits: the top level (the zero
// value), a scene or a hidden block.
type blockContext struct {
	kind string // "", BlockScene or BlockHidden
	name string // the scene name
}

func (c blockContext) describe() string {
	if c.kind == BlockScene {
		return fmt.Sprintf("scene %q", c.name)
	}
	return "hidden"
}

func (v *validator) add(pos Pos, msg, hint string) {
	v.diags = append(v.diags, Diagnostic{Pos: pos, Msg: msg, Hint: hint})
}

// unknownWord reports a command, setting or block keyword that does not
// exist, with the parser's wording.
func (v *validator) unknownWord(pos Pos, word string) {
	if isReserved(word) {
		v.add(pos, fmt.Sprintf("%q is reserved for future use", word), reservedHints[word])
		return
	}
	v.add(pos, fmt.Sprintf("unknown command %q", word), DidYouMean(word, keywordNames))
}

// scope validates the settings and items of the top level or of one block.
func (v *validator) scope(ctx blockContext, settings []Setting, items []Item) {
	seen := map[string]Setting{}
	for _, st := range settings {
		v.setting(ctx, st, seen)
	}
	for _, it := range items {
		switch it := it.(type) {
		case Step:
			v.step(it)
		case *Step:
			if it != nil {
				v.step(*it)
			}
		case Block:
			v.block(ctx, it)
		case *Block:
			if it != nil {
				v.block(ctx, *it)
			}
		}
	}
}

// block validates one block and, when its nesting is legal, its contents. An
// illegally nested block is reported once and not entered.
func (v *validator) block(ctx blockContext, b Block) {
	if !isBlock(b.Kind) {
		v.unknownWord(b.Pos, b.Kind)
		return
	}
	if ctx.kind != "" && !(ctx.kind == BlockScene && b.Kind == BlockHidden) {
		v.add(b.Pos, fmt.Sprintf("%q cannot be nested inside %s", b.Kind, ctx.describe()),
			"blocks nest one level at most: only hidden may sit inside a scene")
		return
	}
	if b.Kind == BlockScene {
		v.sceneName(b)
	} else if b.Name != "" {
		v.add(b.Pos, "hidden takes no name", "write hidden {")
	}
	if len(b.Items) == 0 {
		what := "hidden block"
		if b.Kind == BlockScene {
			what = fmt.Sprintf("scene %q", b.Name)
		}
		v.add(b.Pos, what+" has no steps", "a block must contain at least one step")
	}
	v.scope(blockContext{kind: b.Kind, name: b.Name}, b.Settings, b.Items)
}

func (v *validator) sceneName(b Block) {
	switch {
	case b.Name == "":
		v.add(b.Pos, "scene needs a name", "write scene NAME {")
	case !isSceneName(b.Name):
		v.add(b.Pos, fmt.Sprintf("bad scene name %q", b.Name), "a scene name is letters, digits, _ and -, written bare")
	default:
		if line, dup := v.sceneLine[b.Name]; dup {
			v.add(b.Pos, fmt.Sprintf("duplicate scene name %q", b.Name), fmt.Sprintf("first used on line %d", line))
			return
		}
		v.sceneLine[b.Name] = b.Pos.Line
	}
}

// isSceneName reports whether s is letters, digits, "_" and "-", not
// starting with "_" or "-" (the name rule of docs §3.3 and §4.3).
func isSceneName(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isAlnum(c) || i > 0 && (c == '_' || c == '-') {
			continue
		}
		return false
	}
	return s != ""
}

// isIdentifier reports whether s is an environment variable name: letters,
// digits and "_", not starting with a digit.
func isIdentifier(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isLetter(c) || c == '_' || i > 0 && isDigit(c) {
			continue
		}
		return false
	}
	return s != ""
}

func isDigit(c byte) bool  { return '0' <= c && c <= '9' }
func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }
func isAlnum(c byte) bool  { return isLetter(c) || isDigit(c) }

// step validates one step.
func (v *validator) step(st Step) {
	if !isStep(st.Kind) {
		v.unknownWord(st.Pos, st.Kind)
	}
}

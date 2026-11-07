package script

import (
	"time"

	"github.com/michaellorant/shellshot/internal/script/keys"
)

// Script is a parsed script file: the settings and the items (steps and
// blocks) of the top level, in the shapes of docs/research/script-format.md
// §4.5. The parser accepts every setting and every nesting anywhere; the
// validator (not the parser) decides what is legal.
type Script struct {
	File     string // the file name given to Parse, for diagnostics
	Settings []Setting
	Items    []Item
}

// Item is a Step or a Block, stored as a value. Only those two types
// implement it.
type Item interface {
	ItemPos() Pos
}

// Setting is one setting line such as "size 80x24" or "shell /bin/sh -l".
type Setting struct {
	Pos Pos // the setting name
	// Name is the setting name: "size", "shell", "env", "cwd", "seed",
	// "output", "typing", "jitter", "timeout", "settle", "linger", "speed",
	// "idle" or "hold".
	Name string
	// Value is the argument with quotes and escapes resolved, not yet typed
	// ("80x24", "500ms", "off"). For env it is "KEY=VALUE". For shell it is
	// the argument text as written, for use in messages; Argv is the value.
	Value string
	Argv  []string // shell only: [PATH, ARGS...], quotes and escapes resolved; nil otherwise
	// At is the index in the enclosing Items list of the first item that
	// follows this setting; len(Items) if no item follows. It keeps the
	// source order of settings relative to items.
	At int
}

// Step kinds.
const (
	StepRun   = "run"
	StepType  = "type"
	StepPress = "press"
	StepWait  = "wait"
	StepSleep = "sleep"
	StepMark  = "mark"
)

// Text forms of a run or type step.
const (
	FormRaw        = "raw"        // rest of the line
	FormQuoted     = "quoted"     // a double-quoted string
	FormLines      = "lines"      // a "lines" text block
	FormParagraphs = "paragraphs" // a "paragraphs" text block
)

// Step is one command: run, type, press, wait, sleep or mark. Only the fields
// of its Kind are set.
type Step struct {
	Pos  Pos // the command keyword
	Kind string
	// Form and Text are for run and type. Text has the escapes and the
	// text-block rules applied; "\n" is Enter (for a "run" block it ends with
	// the final Enter).
	Form string
	Text string
	// Keys is for press.
	Keys []KeyPress
	// Pattern is for wait; nil for "wait idle".
	Pattern *Pattern
	// Idle is true for "wait idle".
	Idle bool
	// Duration is the sleep time, or the wait timeout, or the idle time of
	// "wait idle". HasDuration tells a written "0s" (a one-shot check) from
	// no duration (use the setting).
	Duration    time.Duration
	HasDuration bool
	// Label is for mark.
	Label string
}

// ItemPos returns the position of the command keyword.
func (s Step) ItemPos() Pos { return s.Pos }

// KeyPress is one chord of a press step and how often to press it (at least
// 1; "up x3" is Count 3).
type KeyPress struct {
	Chord keys.Chord
	Count int
}

// Pattern is what a wait step looks for on the screen: a substring, or a Go
// RE2 regular expression.
type Pattern struct {
	Regex  bool
	Source string // the text, or the regex without slashes ("\/" resolved to "/")
}

// Block kinds.
const (
	BlockScene  = "scene"
	BlockHidden = "hidden"
)

// Block is a "scene NAME { ... }" or "hidden { ... }" block.
type Block struct {
	Pos  Pos // the block keyword
	End  Pos // the closing "}"; the end of the file if the block is not closed
	Kind string
	Name string // scene only; as written, not yet checked
	// Settings and Items are the contents, related by Setting.At like those
	// of Script.
	Settings []Setting
	Items    []Item
}

// ItemPos returns the position of the block keyword.
func (b Block) ItemPos() Pos { return b.Pos }

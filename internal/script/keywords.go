package script

import "slices"

// Command, block and setting names, in the order did-you-mean prefers them
// on a tie.
var (
	stepNames    = []string{StepRun, StepType, StepPress, StepWait, StepSleep, StepMark}
	blockNames   = []string{BlockScene, BlockHidden}
	settingNames = []string{
		"size", "shell", "env", "cwd", "seed", "output",
		"typing", "jitter", "timeout", "settle", "linger",
		"speed", "idle", "hold",
	}
	// reservedWords may not start a line: they are kept for future use
	// (docs/research/script-format.md §3.4).
	reservedWords = []string{
		"if", "else", "elif", "for", "while", "repeat", "loop", "break",
		"let", "var", "const", "set", "fn", "func", "def", "define", "call",
		"return", "include", "import", "source", "require", "use", "copy",
		"paste", "screenshot", "resize", "hide", "show", "theme", "font",
		"caption", "cleanup", "fresh", "export", "expect", "budget",
	}
)

// keywordNames is every word that may start a line, for did-you-mean.
var keywordNames = slices.Concat(stepNames, blockNames, settingNames)

// reservedHints say what to write instead of a reserved word.
var reservedHints = map[string]string{
	"expect":  `to assert that text is on the screen, use wait "text" 0s`,
	"require": `to check that a program exists, run it in a hidden block and wait for its output`,
	"include": `use cat a.script b.script to join scripts`,
	"source":  `use cat a.script b.script to join scripts`,
}

func isStep(word string) bool    { return slices.Contains(stepNames, word) }
func isBlock(word string) bool   { return slices.Contains(blockNames, word) }
func isSetting(word string) bool { return slices.Contains(settingNames, word) }
func isReserved(word string) bool {
	return slices.Contains(reservedWords, word)
}

// Package keys parses the key chord text used by the script `press` command
// (docs/research/script-format.md §3.1, §4.1, §4.3) into a structured Chord.
//
// Grammar: chord = { modifier "+" } keyname, modifier = "ctrl" | "alt" | "shift".
// Everything is case-sensitive and lower-case. Modifiers may be written in any
// order but not repeated; String writes them in the canonical order ctrl, alt,
// shift. The key names are enter tab esc space backspace delete insert up down
// left right home end pageup pagedown f1..f12, a..z and 0..9.
//
// The following are deliberately NOT part of the grammar: a super modifier,
// f13..f24, keypad keys, and punctuation keys (including "+" itself). Typing
// punctuation is the job of `type`, not `press`. Key and Mods are iota-based so
// a later task can add any of these without changing the existing grammar.
//
// The package is pure Go and knows nothing about the terminal encoder.
package keys

import (
	"fmt"
	"sort"
	"strings"
)

// Key identifies one non-modifier key. The zero value is not a valid key.
type Key uint16

const (
	keyInvalid Key = iota

	KeyEnter
	KeyTab
	KeyEsc
	KeySpace
	KeyBackspace
	KeyDelete
	KeyInsert
	KeyUp
	KeyDown
	KeyLeft
	KeyRight
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown

	KeyF1
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12

	KeyA
	KeyB
	KeyC
	KeyD
	KeyE
	KeyF
	KeyG
	KeyH
	KeyI
	KeyJ
	KeyK
	KeyL
	KeyM
	KeyN
	KeyO
	KeyP
	KeyQ
	KeyR
	KeyS
	KeyT
	KeyU
	KeyV
	KeyW
	KeyX
	KeyY
	KeyZ

	Key0
	Key1
	Key2
	Key3
	Key4
	Key5
	Key6
	Key7
	Key8
	Key9

	keyCount // number of Key values including keyInvalid; keep last
)

var keyNames = [keyCount]string{
	KeyEnter: "enter", KeyTab: "tab", KeyEsc: "esc", KeySpace: "space",
	KeyBackspace: "backspace", KeyDelete: "delete", KeyInsert: "insert",
	KeyUp: "up", KeyDown: "down", KeyLeft: "left", KeyRight: "right",
	KeyHome: "home", KeyEnd: "end", KeyPageUp: "pageup", KeyPageDown: "pagedown",

	KeyF1: "f1", KeyF2: "f2", KeyF3: "f3", KeyF4: "f4", KeyF5: "f5", KeyF6: "f6",
	KeyF7: "f7", KeyF8: "f8", KeyF9: "f9", KeyF10: "f10", KeyF11: "f11", KeyF12: "f12",

	KeyA: "a", KeyB: "b", KeyC: "c", KeyD: "d", KeyE: "e", KeyF: "f", KeyG: "g",
	KeyH: "h", KeyI: "i", KeyJ: "j", KeyK: "k", KeyL: "l", KeyM: "m", KeyN: "n",
	KeyO: "o", KeyP: "p", KeyQ: "q", KeyR: "r", KeyS: "s", KeyT: "t", KeyU: "u",
	KeyV: "v", KeyW: "w", KeyX: "x", KeyY: "y", KeyZ: "z",

	Key0: "0", Key1: "1", Key2: "2", Key3: "3", Key4: "4",
	Key5: "5", Key6: "6", Key7: "7", Key8: "8", Key9: "9",
}

var keysByName = func() map[string]Key {
	m := make(map[string]Key, keyCount)
	for k := Key(1); k < keyCount; k++ {
		m[keyNames[k]] = k
	}
	return m
}()

// String returns the key name, or "" for a value that is not a valid key.
func (k Key) String() string {
	if k == keyInvalid || k >= keyCount {
		return ""
	}
	return keyNames[k]
}

// Mods is a set of modifier keys.
type Mods uint8

const (
	ModCtrl Mods = 1 << iota
	ModAlt
	ModShift

	modCount = iota // number of modifier bits; keep last
)

// modOrder is the canonical order in which String writes modifiers.
var modOrder = [modCount]struct {
	mod  Mods
	name string
}{
	{ModCtrl, "ctrl"},
	{ModAlt, "alt"},
	{ModShift, "shift"},
}

// Chord is a key together with the modifiers held while it is pressed.
type Chord struct {
	Key  Key
	Mods Mods
}

// String returns the canonical chord text: modifiers in the order ctrl, alt,
// shift, each followed by "+", then the key name. Parse(c.String()) == c for
// every Chord that Parse returns.
func (c Chord) String() string {
	var b strings.Builder
	for _, m := range modOrder {
		if c.Mods&m.mod != 0 {
			b.WriteString(m.name)
			b.WriteByte('+')
		}
	}
	b.WriteString(c.Key.String())
	return b.String()
}

// Names returns every key name in canonical order: navigation and editing keys,
// f1..f12, a..z, 0..9. The result is a fresh slice.
func Names() []string {
	return append([]string(nil), keyNames[1:]...)
}

// Parse parses chord text such as "enter", "ctrl+c" or "alt+shift+tab".
// Errors name the offending text and, when a close match exists, suggest it.
func Parse(s string) (Chord, error) {
	c, err := parseExact(s)
	if err == nil {
		return c, nil
	}
	if lower := strings.ToLower(s); lower != s {
		if _, lerr := parseExact(lower); lerr == nil {
			return Chord{}, fmt.Errorf("unknown key %q (did you mean %q?)", s, lower)
		}
	}
	return Chord{}, err
}

func parseExact(s string) (Chord, error) {
	if s == "" {
		return Chord{}, fmt.Errorf("empty key")
	}
	if s == "+" {
		return Chord{}, fmt.Errorf(`"+" is not a key name (use "type +" to send it)`)
	}
	parts := strings.Split(s, "+")
	last := len(parts) - 1

	var c Chord
	for _, p := range parts[:last] {
		mod, ok := modByName(p)
		if !ok {
			if p == "" {
				return Chord{}, fmt.Errorf("empty modifier in %q", s)
			}
			hint := didYouMean(p, modifierNames())
			if hint == "" {
				hint = " (modifiers: " + strings.Join(modifierNames(), ", ") + ")"
			}
			return Chord{}, fmt.Errorf("unknown modifier %q in %q%s", p, s, hint)
		}
		if c.Mods&mod != 0 {
			return Chord{}, fmt.Errorf("duplicate modifier %q in %q", p, s)
		}
		c.Mods |= mod
	}

	name := parts[last]
	if name == "" {
		return Chord{}, fmt.Errorf("missing key after %q", s)
	}
	if _, ok := modByName(name); ok {
		return Chord{}, fmt.Errorf("modifier %q must be followed by a key (e.g. %q)", name, name+"+c")
	}
	key, ok := keysByName[name]
	if !ok {
		if last == 0 {
			return Chord{}, fmt.Errorf("unknown key %q%s", name, didYouMean(name, keyNames[1:]))
		}
		return Chord{}, fmt.Errorf("unknown key %q in %q%s", name, s, didYouMean(name, keyNames[1:]))
	}
	c.Key = key
	return c, nil
}

func modByName(name string) (Mods, bool) {
	for _, m := range modOrder {
		if m.name == name {
			return m.mod, true
		}
	}
	return 0, false
}

func modifierNames() []string {
	names := make([]string, len(modOrder))
	for i, m := range modOrder {
		names[i] = m.name
	}
	return names
}

// maxSuggestions limits how many did-you-mean candidates an error lists.
const maxSuggestions = 3

// maxEditDistance is the largest edit distance at which a name is suggested.
const maxEditDistance = 2

// didYouMean returns a " (did you mean ...?)" suffix naming the candidates
// within maxEditDistance of word, nearest first then alphabetical, or "" if
// there are none. The distance must also be smaller than both the word's and
// the candidate's length, so short words and one-letter keys do not match
// almost everything.
func didYouMean(word string, candidates []string) string {
	type match struct {
		name string
		dist int
	}
	var matches []match
	wordLen := len([]rune(word))
	for _, cand := range candidates {
		d := editDistance(word, cand)
		if d <= maxEditDistance && d < wordLen && d < len(cand) {
			matches = append(matches, match{cand, d})
		}
	}
	if len(matches) == 0 {
		return ""
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].dist != matches[j].dist {
			return matches[i].dist < matches[j].dist
		}
		return matches[i].name < matches[j].name
	})
	if len(matches) > maxSuggestions {
		matches = matches[:maxSuggestions]
	}
	quoted := make([]string, len(matches))
	for i, m := range matches {
		quoted[i] = fmt.Sprintf("%q", m.name)
	}
	return " (did you mean " + strings.Join(quoted, ", ") + "?)"
}

// editDistance is the Levenshtein distance between a and b, counted in runes.
func editDistance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// Repeat limits for the `xN` token that may follow a key in `press`.
const (
	minRepeat = 1
	maxRepeat = 999
)

// ParseRepeat parses a repeat token such as "x3" (an "x" followed by decimal
// digits, value 1..999). ok is false for anything else, including "x0",
// "x1000", "X3" and plain key names.
func ParseRepeat(tok string) (n int, ok bool) {
	if len(tok) < 2 || tok[0] != 'x' {
		return 0, false
	}
	for i := 1; i < len(tok); i++ {
		d := tok[i]
		if d < '0' || d > '9' {
			return 0, false
		}
		n = n*10 + int(d-'0')
		if n > maxRepeat {
			return 0, false
		}
	}
	if n < minRepeat {
		return 0, false
	}
	return n, true
}

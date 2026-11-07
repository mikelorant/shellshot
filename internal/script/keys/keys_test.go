package keys

import (
	"slices"
	"strings"
	"testing"
)

func TestParseTable(t *testing.T) {
	tests := []struct {
		in   string
		want Chord
	}{
		{"enter", Chord{KeyEnter, 0}},
		{"ctrl+c", Chord{KeyC, ModCtrl}},
		{"shift+tab", Chord{KeyTab, ModShift}},
		{"alt+enter", Chord{KeyEnter, ModAlt}},
		{"ctrl+alt+delete", Chord{KeyDelete, ModCtrl | ModAlt}},
		{"alt+ctrl+delete", Chord{KeyDelete, ModCtrl | ModAlt}},
		{"pagedown", Chord{KeyPageDown, 0}},
		{"f5", Chord{KeyF5, 0}},
		{"f12", Chord{KeyF12, 0}},
		{"0", Chord{Key0, 0}},
		{"alt+9", Chord{Key9, ModAlt}},
		{"shift+alt+ctrl+z", Chord{KeyZ, ModCtrl | ModAlt | ModShift}},
		{"x", Chord{KeyX, 0}},
	}
	for _, tt := range tests {
		got, err := Parse(tt.in)
		if err != nil {
			t.Errorf("Parse(%q) error: %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", `empty key`},
		{"+", `"+" is not a key name (use "type +" to send it)`},
		{"Ctrl+C", `unknown key "Ctrl+C" (did you mean "ctrl+c"?)`},
		{"ENTER", `unknown key "ENTER" (did you mean "enter"?)`},
		{"Alt+Ctrl+Delete", `unknown key "Alt+Ctrl+Delete" (did you mean "alt+ctrl+delete"?)`},
		{"ctrl+ctrl+c", `duplicate modifier "ctrl" in "ctrl+ctrl+c"`},
		{"ctrl+alt+ctrl+c", `duplicate modifier "ctrl" in "ctrl+alt+ctrl+c"`},
		{"ctrl+", `missing key after "ctrl+"`},
		{"+c", `empty modifier in "+c"`},
		{"ctrl++c", `empty modifier in "ctrl++c"`},
		{"ctrl", `modifier "ctrl" must be followed by a key (e.g. "ctrl+c")`},
		{"ctrl+shift", `modifier "shift" must be followed by a key (e.g. "shift+c")`},
		{"super+c", `unknown modifier "super" in "super+c" (modifiers: ctrl, alt, shift)`},
		{"contrl+c", `unknown modifier "contrl" in "contrl+c" (did you mean "ctrl"?)`},
		{"c+ctrl+c", `unknown modifier "c" in "c+ctrl+c" (modifiers: ctrl, alt, shift)`},
		{"f13", `unknown key "f13" (did you mean "f1", "f10", "f11"?)`},
		{"zzzzzz", `unknown key "zzzzzz"`},
		{"ctrl+zzzzzz", `unknown key "zzzzzz" in "ctrl+zzzzzz"`},
		{"ctrl+entr", `unknown key "entr" in "ctrl+entr" (did you mean "enter", "end"?)`},
		{" enter", `unknown key " enter" (did you mean "enter"?)`},
		{"ctrl+c ", `unknown key "c " in "ctrl+c "`},
		{"é", `unknown key "é"`},
	}
	for _, tt := range tests {
		c, err := Parse(tt.in)
		if err == nil {
			t.Errorf("Parse(%q) = %+v, want error %q", tt.in, c, tt.want)
			continue
		}
		if err.Error() != tt.want {
			t.Errorf("Parse(%q) error\n got: %s\nwant: %s", tt.in, err, tt.want)
		}
		if c != (Chord{}) {
			t.Errorf("Parse(%q) returned non-zero chord %+v with error", tt.in, c)
		}
	}
}

func TestDidYouMean(t *testing.T) {
	tests := []struct {
		word       string
		candidates []string
		want       string
	}{
		{"entr", Names(), ` (did you mean "enter", "end"?)`},
		{"enterr", Names(), ` (did you mean "enter"?)`},
		{"pagedwn", Names(), ` (did you mean "pagedown"?)`},
		{"pgup", Names(), ` (did you mean "pageup"?)`},
		{"escape", Names(), ``},
		{"bcksp", Names(), ``},
		{"del", Names(), ``},
		{"spce", Names(), ` (did you mean "space"?)`},
		{"homee", Names(), ` (did you mean "home"?)`},
		{"lft", Names(), ` (did you mean "left"?)`},
		{"dwn", Names(), ` (did you mean "down"?)`},
		{"tabb", Names(), ` (did you mean "tab"?)`},
		{"f0", Names(), ` (did you mean "f1", "f10", "f2"?)`},
		{"cmd", []string{"ctrl", "alt", "shift"}, ``},
		{"altt", []string{"ctrl", "alt", "shift"}, ` (did you mean "alt"?)`},
		{"shfit", []string{"ctrl", "alt", "shift"}, ` (did you mean "shift"?)`},
		{"qq", Names(), ``},
		{"", Names(), ``},
	}
	for _, tt := range tests {
		if got := didYouMean(tt.word, tt.candidates); got != tt.want {
			t.Errorf("didYouMean(%q) = %q, want %q", tt.word, got, tt.want)
		}
	}
}

func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"a", "", 1}, {"", "abc", 3}, {"enter", "enter", 0},
		{"entr", "enter", 1}, {"kitten", "sitting", 3}, {"é", "e", 1}, {"ab", "ba", 2},
	}
	for _, tt := range tests {
		if got := editDistance(tt.a, tt.b); got != tt.want {
			t.Errorf("editDistance(%q,%q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestNames(t *testing.T) {
	want := []string{
		"enter", "tab", "esc", "space", "backspace", "delete", "insert",
		"up", "down", "left", "right", "home", "end", "pageup", "pagedown",
		"f1", "f2", "f3", "f4", "f5", "f6", "f7", "f8", "f9", "f10", "f11", "f12",
	}
	for c := 'a'; c <= 'z'; c++ {
		want = append(want, string(c))
	}
	for c := '0'; c <= '9'; c++ {
		want = append(want, string(c))
	}
	got := Names()
	if !slices.Equal(got, want) {
		t.Fatalf("Names() = %v, want %v", got, want)
	}
	got[0] = "mutated"
	if Names()[0] != "enter" {
		t.Error("Names() result aliases package state")
	}
}

func TestKeyStringInvalid(t *testing.T) {
	for _, k := range []Key{0, keyCount, 65535} {
		if got := k.String(); got != "" {
			t.Errorf("Key(%d).String() = %q, want empty", k, got)
		}
	}
}

func allModCombos() []Mods {
	var out []Mods
	for m := Mods(0); m < 1<<modCount; m++ {
		out = append(out, m)
	}
	return out
}

// TestRoundTripAllNames checks Parse(String(c)) == c for every documented
// key name with every modifier combination, and that bare names parse to
// themselves.
func TestRoundTripAllNames(t *testing.T) {
	names := Names()
	if len(names) != int(keyCount)-1 {
		t.Fatalf("Names() has %d entries, want %d", len(names), keyCount-1)
	}
	for _, name := range names {
		bare, err := Parse(name)
		if err != nil {
			t.Errorf("Parse(%q): %v", name, err)
			continue
		}
		if bare.String() != name || bare.Mods != 0 {
			t.Errorf("Parse(%q) = %+v, String() = %q", name, bare, bare.String())
		}
		for _, mods := range allModCombos() {
			c := Chord{Key: bare.Key, Mods: mods}
			got, err := Parse(c.String())
			if err != nil {
				t.Errorf("Parse(%q): %v", c.String(), err)
				continue
			}
			if got != c {
				t.Errorf("Parse(%q) = %+v, want %+v", c.String(), got, c)
			}
		}
	}
}

func TestStringCanonicalOrder(t *testing.T) {
	c := Chord{KeyC, ModShift | ModAlt | ModCtrl}
	if got, want := c.String(), "ctrl+alt+shift+c"; got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// permutations calls fn with every ordering of items.
func permutations(items []string, fn func([]string)) {
	var rec func(int)
	rec = func(i int) {
		if i == len(items) {
			fn(items)
			return
		}
		for j := i; j < len(items); j++ {
			items[i], items[j] = items[j], items[i]
			rec(i + 1)
			items[i], items[j] = items[j], items[i]
		}
	}
	rec(0)
}

// TestModifierOrderIrrelevant checks that every ordering of a modifier set
// parses to the same Chord, whose String is the canonical ordering.
func TestModifierOrderIrrelevant(t *testing.T) {
	for _, key := range []string{"c", "enter", "f5", "9"} {
		for _, mods := range allModCombos() {
			var names []string
			for _, m := range modOrder {
				if mods&m.mod != 0 {
					names = append(names, m.name)
				}
			}
			want := Chord{Key: keysByName[key], Mods: mods}
			permutations(slices.Clone(names), func(p []string) {
				in := strings.Join(append(slices.Clone(p), key), "+")
				got, err := Parse(in)
				if err != nil {
					t.Errorf("Parse(%q): %v", in, err)
					return
				}
				if got != want {
					t.Errorf("Parse(%q) = %+v, want %+v", in, got, want)
				}
				if got.String() != want.String() {
					t.Errorf("Parse(%q).String() = %q, want %q", in, got.String(), want.String())
				}
			})
		}
	}
}

func TestParseRepeat(t *testing.T) {
	tests := []struct {
		in     string
		wantN  int
		wantOK bool
	}{
		{"x1", 1, true},
		{"x3", 3, true},
		{"x10", 10, true},
		{"x999", 999, true},
		{"x03", 3, true},
		{"x0", 0, false},
		{"x00", 0, false},
		{"x1000", 0, false},
		{"x99999999999999999999", 0, false},
		{"x", 0, false},
		{"", 0, false},
		{"X3", 0, false},
		{"3", 0, false},
		{"x-1", 0, false},
		{"x+1", 0, false},
		{"x3x", 0, false},
		{"x 3", 0, false},
		{" x3", 0, false},
		{"x３", 0, false},
		{"down", 0, false},
	}
	for _, tt := range tests {
		n, ok := ParseRepeat(tt.in)
		if n != tt.wantN || ok != tt.wantOK {
			t.Errorf("ParseRepeat(%q) = (%d, %v), want (%d, %v)", tt.in, n, ok, tt.wantN, tt.wantOK)
		}
	}
}

func FuzzParse(f *testing.F) {
	for _, s := range []string{
		"", "enter", "ctrl+c", "Ctrl+C", "alt+ctrl+delete", "ctrl+alt+shift+f12",
		"+", "ctrl+", "+c", "ctrl++c", "ctrl+ctrl+c", "super+c", "f13", "é", "x3",
		"ctrl+\x00", "\xff", "ctrl+alt+shift+pagedown",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		c, err := Parse(s)
		if err != nil {
			if c != (Chord{}) {
				t.Fatalf("Parse(%q) returned %+v with error %v", s, c, err)
			}
			return
		}
		if c.Key == keyInvalid || c.Key >= keyCount {
			t.Fatalf("Parse(%q) returned invalid key %d", s, c.Key)
		}
		canon := c.String()
		again, err := Parse(canon)
		if err != nil {
			t.Fatalf("Parse(%q) ok but Parse(canonical %q) failed: %v", s, canon, err)
		}
		if again != c || again.String() != canon {
			t.Fatalf("Parse(%q) = %+v; canonical %q reparsed as %+v", s, c, canon, again)
		}
	})
}

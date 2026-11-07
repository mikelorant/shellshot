package ghostty

import (
	"errors"
	"testing"

	"github.com/mikelorant/shellshot/internal/script/keys"
	"github.com/mikelorant/shellshot/internal/vt"
)

// newTerminalAndEncoder returns a terminal after feeding it setup, and the key
// encoder bound to it.
func newTerminalAndEncoder(t *testing.T, setup string) (vt.Terminal, *KeyEncoder) {
	t.Helper()
	tm, err := New(20, 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tm.Close() })
	if setup != "" {
		if _, err := tm.Write([]byte(setup)); err != nil {
			t.Fatalf("Write(%q): %v", setup, err)
		}
	}
	enc, err := NewKeyEncoder(tm)
	if err != nil {
		t.Fatalf("NewKeyEncoder: %v", err)
	}
	t.Cleanup(enc.Close)
	return tm, enc
}

func mustChord(t *testing.T, text string) keys.Chord {
	t.Helper()
	c, err := keys.Parse(text)
	if err != nil {
		t.Fatalf("keys.Parse(%q): %v", text, err)
	}
	return c
}

// TestKeyEncoderEncodeVerifiedTable reproduces the bytes of
// docs/research/script-format.md §6.3 through the adapter.
func TestKeyEncoderEncodeVerifiedTable(t *testing.T) {
	rows := []struct {
		chord                     string
		normal, appCursor, kitty1 string
	}{
		{"enter", "\r", "\r", "\r"},
		{"tab", "\t", "\t", "\t"},
		{"shift+tab", "\x1b[Z", "\x1b[Z", "\x1b[9;2u"},
		{"esc", "\x1b", "\x1b", "\x1b[27u"},
		{"backspace", "\x7f", "\x7f", "\x7f"},
		{"space", " ", " ", " "},
		{"up", "\x1b[A", "\x1bOA", "\x1b[A"},
		{"down", "\x1b[B", "\x1bOB", "\x1b[B"},
		{"left", "\x1b[D", "\x1bOD", "\x1b[D"},
		{"right", "\x1b[C", "\x1bOC", "\x1b[C"},
		{"home", "\x1b[H", "\x1bOH", "\x1b[H"},
		{"end", "\x1b[F", "\x1bOF", "\x1b[F"},
		{"pageup", "\x1b[5~", "\x1b[5~", "\x1b[5~"},
		{"pagedown", "\x1b[6~", "\x1b[6~", "\x1b[6~"},
		{"delete", "\x1b[3~", "\x1b[3~", "\x1b[3~"},
		{"insert", "\x1b[2~", "\x1b[2~", "\x1b[2~"},
		{"f1", "\x1bOP", "\x1bOP", "\x1b[P"},
		{"f5", "\x1b[15~", "\x1b[15~", "\x1b[15~"},
		{"f12", "\x1b[24~", "\x1b[24~", "\x1b[24~"},
		{"ctrl+c", "\x03", "\x03", "\x1b[99;5u"},
		{"ctrl+d", "\x04", "\x04", "\x1b[100;5u"},
		{"ctrl+a", "\x01", "\x01", "\x1b[97;5u"},
		{"alt+s", "\x1bs", "\x1bs", "\x1b[115;3u"},
		{"alt+enter", "\x1b\r", "\x1b\r", "\x1b[13;3u"},
		{"ctrl+alt+delete", "\x1b[3;7~", "\x1b[3;7~", "\x1b[3;7~"},
		{"shift+up", "\x1b[1;2A", "\x1b[1;2A", "\x1b[1;2A"},
		{"ctrl+left", "\x1b[1;5D", "\x1b[1;5D", "\x1b[1;5D"},
	}
	modes := []struct {
		name  string
		setup string
		want  func(i int) string
	}{
		{"normal", "", func(i int) string { return rows[i].normal }},
		{"application cursor", "\x1b[?1h", func(i int) string { return rows[i].appCursor }},
		{"kitty disambiguate", "\x1b[>1u", func(i int) string { return rows[i].kitty1 }},
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			_, enc := newTerminalAndEncoder(t, mode.setup)
			for i, r := range rows {
				got, err := enc.Encode(mustChord(t, r.chord))
				if err != nil {
					t.Errorf("%s: %v", r.chord, err)
					continue
				}
				if want := mode.want(i); string(got) != want {
					t.Errorf("%s: got %q, want %q", r.chord, got, want)
				}
			}
		})
	}
}

// Every key of the script grammar must encode to something in every mode, and
// alternating chords on one encoder must not leak fields from the previous one.
func TestKeyEncoderEncodeEveryScriptKey(t *testing.T) {
	for _, setup := range []string{"", "\x1b[?1h", "\x1b[>1u"} {
		_, enc := newTerminalAndEncoder(t, setup)
		for _, mods := range []string{"", "ctrl+", "alt+", "shift+", "ctrl+alt+shift+"} {
			for _, name := range keys.Names() {
				fresh := mustChord(t, mods+name)
				first, err := enc.Encode(fresh)
				if err != nil || len(first) == 0 {
					t.Errorf("setup %q: %s: got %q, %v", setup, mods+name, first, err)
					continue
				}
				// Encode something else in between, then again: same bytes.
				if _, err := enc.Encode(mustChord(t, "alt+z")); err != nil {
					t.Fatal(err)
				}
				again, err := enc.Encode(fresh)
				if err != nil || string(again) != string(first) {
					t.Errorf("setup %q: %s: second encode %q, %v; first %q", setup, mods+name, again, err, first)
				}
			}
		}
	}
}

// A mode change written to the terminal between two encodes is seen by the
// second one.
func TestKeyEncoderFollowsTerminalModes(t *testing.T) {
	tm, enc := newTerminalAndEncoder(t, "")
	up := mustChord(t, "up")
	steps := []struct{ write, want string }{
		{"", "\x1b[A"},
		{"\x1b[?1h", "\x1bOA"},
		{"\x1b[?1l", "\x1b[A"},
		{"\x1b[?1h", "\x1bOA"},
		{"\x1b[>1u", "\x1b[A"}, // kitty overrides DECCKM
		{"\x1b[<u", "\x1bOA"},
	}
	for _, s := range steps {
		if s.write != "" {
			if _, err := tm.Write([]byte(s.write)); err != nil {
				t.Fatal(err)
			}
		}
		got, err := enc.Encode(up)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != s.want {
			t.Errorf("after %q: got %q, want %q", s.write, got, s.want)
		}
	}
}

func TestKeyEncoderEncodeText(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a", "a"},
		{"Hello, World!", "Hello, World!"},
		{"é", "\xc3\xa9"},
		{"世界", "\xe4\xb8\x96\xe7\x95\x8c"},
		{"😀", "\xf0\x9f\x98\x80"},
		{"naïve café 世界 😀", "na\xc3\xafve caf\xc3\xa9 \xe4\xb8\x96\xe7\x95\x8c \xf0\x9f\x98\x80"},
		{"ls\n", "ls\r"},
		{"a\r\nb\n\nc", "a\rb\r\rc"},
		{"tab\there", "tab\there"},
		{" ", " "},
	}
	for _, setup := range []string{"", "\x1b[?1h", "\x1b[>31u"} {
		_, enc := newTerminalAndEncoder(t, setup)
		for _, tc := range tests {
			got, err := enc.EncodeText(tc.in)
			if err != nil {
				t.Errorf("setup %q: EncodeText(%q): %v", setup, tc.in, err)
				continue
			}
			if string(got) != tc.want {
				t.Errorf("setup %q: EncodeText(%q) = %q, want %q", setup, tc.in, got, tc.want)
			}
		}
	}
}

func TestKeyEncoderEncodeTextEdgeCases(t *testing.T) {
	_, enc := newTerminalAndEncoder(t, "")
	if got, err := enc.EncodeText(""); got != nil || err != nil {
		t.Errorf("EncodeText(\"\") = %q, %v; want nil, nil", got, err)
	}
	if got, err := enc.EncodeText("a\xffb"); err == nil {
		t.Errorf("EncodeText(invalid UTF-8) = %q, nil; want an error", got)
	}
}

// The results are owned by the caller: a later encode must not change them.
func TestKeyEncoderResultsAreCopies(t *testing.T) {
	_, enc := newTerminalAndEncoder(t, "")
	first, err := enc.Encode(mustChord(t, "up"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Encode(mustChord(t, "f12")); err != nil {
		t.Fatal(err)
	}
	if string(first) != "\x1b[A" {
		t.Errorf("earlier result changed to %q", first)
	}
	text, err := enc.EncodeText("abc")
	if err != nil {
		t.Fatal(err)
	}
	text[0] = 'X'
	if again, _ := enc.EncodeText("abc"); string(again) != "abc" {
		t.Errorf("EncodeText result aliases shared state: %q", again)
	}
}

func TestKeyEncoderEncodeAllocations(t *testing.T) {
	_, enc := newTerminalAndEncoder(t, "")
	for _, chord := range []string{"up", "ctrl+c", "alt+s"} {
		c := mustChord(t, chord)
		// The binding's Encode costs 3 (stack buffer escape, length, result
		// slice) and its per-press setup 1; the adapter's own code adds none.
		const maxAllocs = 4
		got := testing.AllocsPerRun(100, func() {
			if out, err := enc.Encode(c); err != nil || len(out) == 0 {
				t.Fatalf("Encode(%s) = %q, %v", chord, out, err)
			}
		})
		t.Logf("Encode(%s): %.0f allocs", chord, got)
		if got > maxAllocs {
			t.Errorf("Encode(%s): %.0f allocs, want at most %d", chord, got, maxAllocs)
		}
	}
	got := testing.AllocsPerRun(100, func() {
		if out, err := enc.EncodeText("hello 世界\n"); err != nil || len(out) == 0 {
			t.Fatalf("EncodeText = %q, %v", out, err)
		}
	})
	if got > 1 {
		t.Errorf("EncodeText: %.0f allocs, want at most 1", got)
	}
}

func TestKeyEncoderClosedTerminal(t *testing.T) {
	tm, enc := newTerminalAndEncoder(t, "")
	if err := tm.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := enc.Encode(mustChord(t, "up")); !errors.Is(err, ErrClosed) {
		t.Errorf("Encode after terminal Close: err = %v, want ErrClosed", err)
	}
	if _, err := enc.EncodeText("x"); !errors.Is(err, ErrClosed) {
		t.Errorf("EncodeText after terminal Close: err = %v, want ErrClosed", err)
	}
	if _, err := NewKeyEncoder(tm); !errors.Is(err, ErrClosed) {
		t.Errorf("NewKeyEncoder on closed terminal: err = %v, want ErrClosed", err)
	}
}

func TestKeyEncoderClose(t *testing.T) {
	_, enc := newTerminalAndEncoder(t, "")
	enc.Close()
	enc.Close() // idempotent
	if _, err := enc.Encode(mustChord(t, "up")); !errors.Is(err, ErrEncoderClosed) {
		t.Errorf("Encode after Close: err = %v, want ErrEncoderClosed", err)
	}
	if _, err := enc.EncodeText("x"); !errors.Is(err, ErrEncoderClosed) {
		t.Errorf("EncodeText after Close: err = %v, want ErrEncoderClosed", err)
	}
}

func TestNewKeyEncoderRejectsForeignTerminal(t *testing.T) {
	if _, err := NewKeyEncoder(nil); err == nil {
		t.Error("NewKeyEncoder(nil) succeeded")
	}
	if _, err := NewKeyEncoder(fakeTerminal{}); err == nil {
		t.Error("NewKeyEncoder(fake terminal) succeeded")
	}
}

func TestKeyEncoderRejectsInvalidChord(t *testing.T) {
	_, enc := newTerminalAndEncoder(t, "")
	for name, c := range map[string]keys.Chord{
		"zero chord":    {},
		"unknown key":   {Key: keys.Key(9999)},
		"unknown mod":   {Key: keys.KeyA, Mods: 1 << 7},
		"modifier only": {Mods: keys.ModCtrl},
	} {
		if got, err := enc.Encode(c); err == nil {
			t.Errorf("%s: Encode = %q, nil; want an error", name, got)
		}
	}
}

// fakeTerminal is a vt.Terminal that is not backed by libghostty.
type fakeTerminal struct{ vt.Terminal }

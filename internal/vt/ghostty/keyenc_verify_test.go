package ghostty

// Experiment T6 (docs/research/script-format.md §6): what bytes does the pinned
// libghostty KeyEncoder produce, and how must a KeyEvent be filled in to get
// them? The tests drive a real headless terminal into DECCKM / Kitty keyboard
// modes with VT output and assert exact bytes.

import (
	"bytes"
	"runtime"
	"testing"

	libghostty "go.mitchellh.com/libghostty"
)

const (
	escAppCursorOn      = "\x1b[?1h"  // DECSET 1: application cursor keys (DECCKM)
	escKittyPush1       = "\x1b[>1u"  // push Kitty keyboard flags = disambiguate
	escKittyPushAll     = "\x1b[>31u" // push every Kitty flag
	escModifyOtherKeys2 = "\x1b[>4;2m"
	escAltPrefixOff     = "\x1b[?1036l" // DECRST 1036: alt does not send ESC prefix
)

// keySpec is everything a caller must provide to the encoder for one key.
// text is the character the key types with no modifiers applied (empty for
// function keys); unshifted is its lower-case/unshifted code point. Printable
// keys need both: text alone loses ctrl+<letter> under Kitty, the code point
// alone loses plain letters and space in legacy mode.
type keySpec struct {
	key       libghostty.Key
	mods      libghostty.Mods
	consumed  libghostty.Mods
	text      string
	unshifted rune
	action    libghostty.KeyAction
}

func fn(key libghostty.Key, mods libghostty.Mods) keySpec {
	return keySpec{key: key, mods: mods, action: libghostty.KeyActionPress}
}

func chr(key libghostty.Key, mods libghostty.Mods, r rune) keySpec {
	return keySpec{key: key, mods: mods, text: string(r), unshifted: r, action: libghostty.KeyActionPress}
}

// keyEncoder bundles the long-lived encoder and reusable event.
type keyEncoder struct {
	enc *libghostty.KeyEncoder
	ev  *libghostty.KeyEvent
}

func newKeyEncoder(t *testing.T) *keyEncoder {
	t.Helper()
	enc, err := libghostty.NewKeyEncoder()
	if err != nil {
		t.Fatalf("NewKeyEncoder: %v", err)
	}
	ev, err := libghostty.NewKeyEvent()
	if err != nil {
		enc.Close()
		t.Fatalf("NewKeyEvent: %v", err)
	}
	t.Cleanup(func() { ev.Close(); enc.Close() })
	return &keyEncoder{enc: enc, ev: ev}
}

// syncTerminal copies the terminal's current key modes into the encoder.
// SetFromTerminal resets option-as-alt, so it is set again afterwards: without
// it macOS builds encode alt+<char> as the bare character.
func (k *keyEncoder) syncTerminal(t *testing.T, term *libghostty.Terminal) {
	t.Helper()
	if err := k.enc.SetFromTerminal(term); err != nil {
		t.Fatalf("SetFromTerminal: %v", err)
	}
	if err := k.enc.SetOptionAsAlt(libghostty.OptionAsAltTrue); err != nil {
		t.Fatalf("SetOptionAsAlt: %v", err)
	}
}

// fill sets every event field; the event is reused, so a field left out would
// keep the previous key's value.
func (k *keyEncoder) fill(s keySpec) error {
	for _, err := range []error{
		k.ev.SetAction(s.action),
		k.ev.SetKey(s.key),
		k.ev.SetMods(s.mods),
		k.ev.SetConsumedMods(s.consumed),
		k.ev.SetUTF8(s.text),
		k.ev.SetUnshiftedCodepoint(s.unshifted),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

func (k *keyEncoder) encode(t *testing.T, s keySpec) []byte {
	t.Helper()
	if err := k.fill(s); err != nil {
		t.Fatalf("filling key event: %v", err)
	}
	out, err := k.enc.Encode(k.ev)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	return out
}

// newModeTerminal returns a headless terminal after feeding it setup.
func newModeTerminal(t *testing.T, setup string) *libghostty.Terminal {
	t.Helper()
	tm, err := New(20, 4)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { tm.Close() })
	if _, err := tm.Write([]byte(setup)); err != nil {
		t.Fatalf("Write(%q): %v", setup, err)
	}
	return tm.(*terminal).term
}

const (
	shift = libghostty.ModShift
	ctrl  = libghostty.ModCtrl
	alt   = libghostty.ModAlt
)

func TestKeyEncoderPressTable(t *testing.T) {
	rows := []struct {
		name                      string
		spec                      keySpec
		normal, appCursor, kitty1 string
	}{
		{"enter", fn(libghostty.KeyEnter, 0), "\r", "\r", "\r"},
		{"tab", fn(libghostty.KeyTab, 0), "\t", "\t", "\t"},
		{"shift+tab", fn(libghostty.KeyTab, shift), "\x1b[Z", "\x1b[Z", "\x1b[9;2u"},
		{"esc", fn(libghostty.KeyEscape, 0), "\x1b", "\x1b", "\x1b[27u"},
		{"backspace", fn(libghostty.KeyBackspace, 0), "\x7f", "\x7f", "\x7f"},
		{"space", chr(libghostty.KeySpace, 0, ' '), " ", " ", " "},
		{"up", fn(libghostty.KeyArrowUp, 0), "\x1b[A", "\x1bOA", "\x1b[A"},
		{"down", fn(libghostty.KeyArrowDown, 0), "\x1b[B", "\x1bOB", "\x1b[B"},
		{"left", fn(libghostty.KeyArrowLeft, 0), "\x1b[D", "\x1bOD", "\x1b[D"},
		{"right", fn(libghostty.KeyArrowRight, 0), "\x1b[C", "\x1bOC", "\x1b[C"},
		{"home", fn(libghostty.KeyHome, 0), "\x1b[H", "\x1bOH", "\x1b[H"},
		{"end", fn(libghostty.KeyEnd, 0), "\x1b[F", "\x1bOF", "\x1b[F"},
		{"pageup", fn(libghostty.KeyPageUp, 0), "\x1b[5~", "\x1b[5~", "\x1b[5~"},
		{"pagedown", fn(libghostty.KeyPageDown, 0), "\x1b[6~", "\x1b[6~", "\x1b[6~"},
		{"delete", fn(libghostty.KeyDelete, 0), "\x1b[3~", "\x1b[3~", "\x1b[3~"},
		{"insert", fn(libghostty.KeyInsert, 0), "\x1b[2~", "\x1b[2~", "\x1b[2~"},
		{"f1", fn(libghostty.KeyF1, 0), "\x1bOP", "\x1bOP", "\x1b[P"},
		{"f5", fn(libghostty.KeyF5, 0), "\x1b[15~", "\x1b[15~", "\x1b[15~"},
		{"f12", fn(libghostty.KeyF12, 0), "\x1b[24~", "\x1b[24~", "\x1b[24~"},
		{"ctrl+c", chr(libghostty.KeyC, ctrl, 'c'), "\x03", "\x03", "\x1b[99;5u"},
		{"ctrl+d", chr(libghostty.KeyD, ctrl, 'd'), "\x04", "\x04", "\x1b[100;5u"},
		{"ctrl+a", chr(libghostty.KeyA, ctrl, 'a'), "\x01", "\x01", "\x1b[97;5u"},
		{"alt+s", chr(libghostty.KeyS, alt, 's'), "\x1bs", "\x1bs", "\x1b[115;3u"},
		{"alt+enter", fn(libghostty.KeyEnter, alt), "\x1b\r", "\x1b\r", "\x1b[13;3u"},
		{"ctrl+alt+delete", fn(libghostty.KeyDelete, ctrl|alt), "\x1b[3;7~", "\x1b[3;7~", "\x1b[3;7~"},
		{"shift+up", fn(libghostty.KeyArrowUp, shift), "\x1b[1;2A", "\x1b[1;2A", "\x1b[1;2A"},
		{"ctrl+left", fn(libghostty.KeyArrowLeft, ctrl), "\x1b[1;5D", "\x1b[1;5D", "\x1b[1;5D"},
	}
	states := []struct {
		name  string
		setup string
		want  func(i int) string
	}{
		{"normal", "", func(i int) string { return rows[i].normal }},
		{"application cursor", escAppCursorOn, func(i int) string { return rows[i].appCursor }},
		{"kitty disambiguate", escKittyPush1, func(i int) string { return rows[i].kitty1 }},
	}

	ke := newKeyEncoder(t)
	for _, st := range states {
		t.Run(st.name, func(t *testing.T) {
			ke.syncTerminal(t, newModeTerminal(t, st.setup))
			for i, r := range rows {
				if got, want := ke.encode(t, r.spec), st.want(i); string(got) != want {
					t.Errorf("%s: got %q, want %q", r.name, got, want)
				}
			}
		})
	}
}

// Kitty disambiguate wins over DECCKM: with both set, cursor keys use CSI, not SS3.
func TestKeyEncoderKittyOverridesApplicationCursor(t *testing.T) {
	ke := newKeyEncoder(t)
	ke.syncTerminal(t, newModeTerminal(t, escAppCursorOn+escKittyPush1))
	for name, tc := range map[string]struct {
		spec keySpec
		want string
	}{
		"up":     {fn(libghostty.KeyArrowUp, 0), "\x1b[A"},
		"home":   {fn(libghostty.KeyHome, 0), "\x1b[H"},
		"f1":     {fn(libghostty.KeyF1, 0), "\x1b[P"},
		"ctrl+c": {chr(libghostty.KeyC, ctrl, 'c'), "\x1b[99;5u"},
	} {
		if got := ke.encode(t, tc.spec); string(got) != tc.want {
			t.Errorf("%s: got %q, want %q", name, got, tc.want)
		}
	}
}

// Typed text: a key event with text and no key (KeyUnidentified) encodes to the
// text itself, in every mode, including non-ASCII. Shifted characters under
// Kitty need the shift reported as consumed or they become CSI-u.
func TestKeyEncoderText(t *testing.T) {
	text := func(s string) keySpec {
		return keySpec{key: libghostty.KeyUnidentified, text: s, action: libghostty.KeyActionPress}
	}
	ke := newKeyEncoder(t)
	for name, setup := range map[string]string{"normal": "", "application cursor": escAppCursorOn, "kitty": escKittyPush1} {
		ke.syncTerminal(t, newModeTerminal(t, setup))
		for _, s := range []string{"a", "A", "é", "世", "😀", "!"} {
			if got := ke.encode(t, text(s)); string(got) != s {
				t.Errorf("%s: text %q encoded as %q", name, s, got)
			}
		}
	}

	ke.syncTerminal(t, newModeTerminal(t, ""))
	shiftA := keySpec{key: libghostty.KeyA, mods: shift, text: "A", unshifted: 'a', action: libghostty.KeyActionPress}
	if got := ke.encode(t, shiftA); string(got) != "A" {
		t.Errorf("normal shift+a with text: got %q, want %q", got, "A")
	}

	ke.syncTerminal(t, newModeTerminal(t, escKittyPush1))
	if got := ke.encode(t, shiftA); string(got) != "\x1b[97;2u" {
		t.Errorf("kitty shift+a, shift not consumed: got %q, want %q", got, "\x1b[97;2u")
	}
	shiftA.consumed = shift
	if got := ke.encode(t, shiftA); string(got) != "A" {
		t.Errorf("kitty shift+a, shift consumed: got %q, want %q", got, "A")
	}
}

// Printable keys must carry text, and ctrl/alt chords must carry the unshifted
// code point too, or the result is empty or wrong.
func TestKeyEncoderPrintableKeyNeedsTextAndCodepoint(t *testing.T) {
	press := func(key libghostty.Key, mods libghostty.Mods, text string, cp rune) keySpec {
		return keySpec{key: key, mods: mods, text: text, unshifted: cp, action: libghostty.KeyActionPress}
	}
	tests := []struct {
		name  string
		setup string
		spec  keySpec
		want  string
	}{
		{"legacy: letter without text is empty", "", press(libghostty.KeyA, 0, "", 0), ""},
		{"legacy: letter with code point only is empty", "", press(libghostty.KeyA, 0, "", 'a'), ""},
		{"legacy: space without text is empty", "", press(libghostty.KeySpace, 0, "", 0), ""},
		{"legacy: ctrl+c without text or code point is 0x03", "", press(libghostty.KeyC, ctrl, "", 0), "\x03"},
		{"kitty: ctrl+c without text or code point is empty", escKittyPush1, press(libghostty.KeyC, ctrl, "", 0), ""},
		{"kitty: ctrl+c with text but no code point is bare c", escKittyPush1, press(libghostty.KeyC, ctrl, "c", 0), "c"},
		{"kitty: ctrl+c with code point only", escKittyPush1, press(libghostty.KeyC, ctrl, "", 'c'), "\x1b[99;5u"},
		{"legacy: alt+s without text or code point is empty", "", press(libghostty.KeyS, alt, "", 0), ""},
		{"legacy: alt+s with code point only", "", press(libghostty.KeyS, alt, "", 's'), "\x1bs"},
		{"alt prefix off (DECRST 1036): alt+s is bare s", escAltPrefixOff, press(libghostty.KeyS, alt, "s", 's'), "s"},
		{"modifyOtherKeys 2: alt+s", escModifyOtherKeys2, press(libghostty.KeyS, alt, "s", 's'), "\x1b[27;3;115~"},
		{"modifyOtherKeys 2: ctrl+c still 0x03 without text", escModifyOtherKeys2, press(libghostty.KeyC, ctrl, "", 'c'), "\x03"},
		{"legacy: ctrl+[ is CSI-u, not ESC", "", press(libghostty.KeyBracketLeft, ctrl, "[", '['), "\x1b[91;5u"},
		{"legacy: ctrl+space is NUL", "", press(libghostty.KeySpace, ctrl, " ", ' '), "\x00"},
		{"legacy: ctrl+backspace is BS", "", fn(libghostty.KeyBackspace, ctrl), "\b"},
		{"legacy: alt+backspace", "", fn(libghostty.KeyBackspace, alt), "\x1b\x7f"},
		{"legacy: shift+enter is modifyOtherKeys-style", "", fn(libghostty.KeyEnter, shift), "\x1b[27;2;13~"},
		{"kitty all flags: enter", escKittyPushAll, fn(libghostty.KeyEnter, 0), "\x1b[13u"},
		{"kitty all flags: letter reports text", escKittyPushAll, press(libghostty.KeyA, 0, "a", 'a'), "\x1b[97;;97u"},
	}
	ke := newKeyEncoder(t)
	for _, tc := range tests {
		ke.syncTerminal(t, newModeTerminal(t, tc.setup))
		if got := ke.encode(t, tc.spec); string(got) != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// Some events legitimately produce no bytes: Encode returns (nil, nil).
func TestKeyEncoderNoOutput(t *testing.T) {
	ke := newKeyEncoder(t)
	for name, setup := range map[string]string{"normal": "", "kitty": escKittyPush1} {
		ke.syncTerminal(t, newModeTerminal(t, setup))
		release := fn(libghostty.KeyArrowUp, 0)
		release.action = libghostty.KeyActionRelease
		for what, spec := range map[string]keySpec{
			"bare shift":                          fn(libghostty.KeyShiftLeft, shift),
			"bare control":                        fn(libghostty.KeyControlLeft, ctrl),
			"unidentified key":                    fn(libghostty.KeyUnidentified, 0),
			"release without kitty report-events": release,
		} {
			if got := ke.encode(t, spec); got != nil {
				t.Errorf("%s: %s encoded as %q, want nil", name, what, got)
			}
		}
	}
}

// The encoder copies the terminal's modes at SetFromTerminal: later mode
// changes are not seen until it is called again, and the encoder keeps working
// after the terminal is closed (it holds no terminal reference).
func TestKeyEncoderSnapshotsTerminalModes(t *testing.T) {
	ke := newKeyEncoder(t)
	tm, err := New(20, 4)
	if err != nil {
		t.Fatal(err)
	}
	term := tm.(*terminal).term
	up := fn(libghostty.KeyArrowUp, 0)

	ke.syncTerminal(t, term)
	if got := ke.encode(t, up); string(got) != "\x1b[A" {
		t.Fatalf("before DECCKM: got %q", got)
	}
	tm.Write([]byte(escAppCursorOn))
	if got := ke.encode(t, up); string(got) != "\x1b[A" {
		t.Errorf("DECCKM set after SetFromTerminal must not apply yet: got %q", got)
	}
	ke.syncTerminal(t, term)
	if got := ke.encode(t, up); string(got) != "\x1bOA" {
		t.Errorf("after re-sync: got %q, want ESC O A", got)
	}

	// vim/less style: leave application mode again.
	tm.Write([]byte("\x1b[?1l"))
	ke.syncTerminal(t, term)
	if got := ke.encode(t, up); string(got) != "\x1b[A" {
		t.Errorf("after DECRST 1: got %q", got)
	}

	tm.Write([]byte(escAppCursorOn))
	ke.syncTerminal(t, term)
	tm.Close()
	if got := ke.encode(t, up); string(got) != "\x1bOA" {
		t.Errorf("encode after terminal Close: got %q, want ESC O A", got)
	}
}

// Kitty flags are a stack: pop restores the previous mode.
func TestKeyEncoderKittyPushPop(t *testing.T) {
	ke := newKeyEncoder(t)
	tm, err := New(20, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer tm.Close()
	term := tm.(*terminal).term
	esc := fn(libghostty.KeyEscape, 0)

	tm.Write([]byte(escKittyPush1))
	ke.syncTerminal(t, term)
	if got := ke.encode(t, esc); string(got) != "\x1b[27u" {
		t.Errorf("kitty pushed: got %q", got)
	}
	tm.Write([]byte("\x1b[<u"))
	ke.syncTerminal(t, term)
	if got := ke.encode(t, esc); string(got) != "\x1b" {
		t.Errorf("kitty popped: got %q", got)
	}
}

// SetFromTerminal resets option-as-alt (documented). On darwin the default
// encodes alt+s as bare "s" (option composes characters); syncTerminal sets
// OptionAsAltTrue to get ESC s.
func TestKeyEncoderSetFromTerminalResetsOptionAsAlt(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("option-as-alt only changes alt+<char> encoding on macOS")
	}
	ke := newKeyEncoder(t)
	term := newModeTerminal(t, "")
	if err := ke.enc.SetFromTerminal(term); err != nil {
		t.Fatal(err)
	}
	if got := ke.encode(t, chr(libghostty.KeyS, alt, 's')); string(got) != "s" {
		t.Errorf("darwin without option-as-alt: got %q, want bare s", got)
	}
}

// Allocation behaviour, measured per call and logged rather than asserted (the
// counts are properties of the binding, not of shellshot).
func TestKeyEncoderAllocations(t *testing.T) {
	ke := newKeyEncoder(t)
	term := newModeTerminal(t, "")
	ke.syncTerminal(t, term)
	for name, spec := range map[string]keySpec{
		"function key":    fn(libghostty.KeyArrowUp, 0),
		"printable chord": chr(libghostty.KeyC, ctrl, 'c'),
	} {
		ke.encode(t, spec) // fill the event; Encode below reuses it
		encode := testing.AllocsPerRun(100, func() {
			if out, err := ke.enc.Encode(ke.ev); err != nil || len(out) == 0 {
				t.Fatalf("Encode = %q, %v", out, err)
			}
		})
		sync := testing.AllocsPerRun(100, func() {
			if err := ke.enc.SetFromTerminal(term); err != nil {
				t.Fatal(err)
			}
		})
		fill := testing.AllocsPerRun(100, func() { ke.fill(spec) })
		t.Logf("%s: Encode %.0f allocs, SetFromTerminal %.0f, fill event %.0f", name, encode, sync, fill)
	}
}

func TestKeyEncoderLongTextGrowsBuffer(t *testing.T) {
	// Encode transparently retries with a heap buffer when output exceeds 128
	// bytes; long text is the only way to exceed it.
	long := bytes.Repeat([]byte("x"), 300)
	ke := newKeyEncoder(t)
	ke.syncTerminal(t, newModeTerminal(t, ""))
	got := ke.encode(t, keySpec{key: libghostty.KeyUnidentified, text: string(long), action: libghostty.KeyActionPress})
	if !bytes.Equal(got, long) {
		t.Errorf("300-byte text: got %d bytes", len(got))
	}
}

package ghostty

import (
	libghostty "go.mitchellh.com/libghostty"

	"github.com/michaellorant/shellshot/internal/script/keys"
)

// keyEntry is what the libghostty encoder needs to know about one key
// (docs/research/script-format.md §6.2). Printable keys carry the unmodified
// character as text and its code point; both are required, or the encoder
// returns no bytes or the wrong ones. Function and navigation keys carry
// neither.
type keyEntry struct {
	key       libghostty.Key
	text      string
	unshifted rune
}

func functionKey(k libghostty.Key) keyEntry { return keyEntry{key: k} }

func printableKey(k libghostty.Key, r rune) keyEntry {
	return keyEntry{key: k, text: string(r), unshifted: r}
}

// keyTable maps every key the script grammar accepts (keys.Names) to its
// libghostty key. TestKeyTableCoversEveryScriptKey fails when keys adds a key
// that is missing here.
var keyTable = map[keys.Key]keyEntry{
	keys.KeyEnter:     functionKey(libghostty.KeyEnter),
	keys.KeyTab:       functionKey(libghostty.KeyTab),
	keys.KeyEsc:       functionKey(libghostty.KeyEscape),
	keys.KeySpace:     printableKey(libghostty.KeySpace, ' '),
	keys.KeyBackspace: functionKey(libghostty.KeyBackspace),
	keys.KeyDelete:    functionKey(libghostty.KeyDelete),
	keys.KeyInsert:    functionKey(libghostty.KeyInsert),
	keys.KeyUp:        functionKey(libghostty.KeyArrowUp),
	keys.KeyDown:      functionKey(libghostty.KeyArrowDown),
	keys.KeyLeft:      functionKey(libghostty.KeyArrowLeft),
	keys.KeyRight:     functionKey(libghostty.KeyArrowRight),
	keys.KeyHome:      functionKey(libghostty.KeyHome),
	keys.KeyEnd:       functionKey(libghostty.KeyEnd),
	keys.KeyPageUp:    functionKey(libghostty.KeyPageUp),
	keys.KeyPageDown:  functionKey(libghostty.KeyPageDown),

	keys.KeyF1:  functionKey(libghostty.KeyF1),
	keys.KeyF2:  functionKey(libghostty.KeyF2),
	keys.KeyF3:  functionKey(libghostty.KeyF3),
	keys.KeyF4:  functionKey(libghostty.KeyF4),
	keys.KeyF5:  functionKey(libghostty.KeyF5),
	keys.KeyF6:  functionKey(libghostty.KeyF6),
	keys.KeyF7:  functionKey(libghostty.KeyF7),
	keys.KeyF8:  functionKey(libghostty.KeyF8),
	keys.KeyF9:  functionKey(libghostty.KeyF9),
	keys.KeyF10: functionKey(libghostty.KeyF10),
	keys.KeyF11: functionKey(libghostty.KeyF11),
	keys.KeyF12: functionKey(libghostty.KeyF12),

	keys.KeyA: printableKey(libghostty.KeyA, 'a'),
	keys.KeyB: printableKey(libghostty.KeyB, 'b'),
	keys.KeyC: printableKey(libghostty.KeyC, 'c'),
	keys.KeyD: printableKey(libghostty.KeyD, 'd'),
	keys.KeyE: printableKey(libghostty.KeyE, 'e'),
	keys.KeyF: printableKey(libghostty.KeyF, 'f'),
	keys.KeyG: printableKey(libghostty.KeyG, 'g'),
	keys.KeyH: printableKey(libghostty.KeyH, 'h'),
	keys.KeyI: printableKey(libghostty.KeyI, 'i'),
	keys.KeyJ: printableKey(libghostty.KeyJ, 'j'),
	keys.KeyK: printableKey(libghostty.KeyK, 'k'),
	keys.KeyL: printableKey(libghostty.KeyL, 'l'),
	keys.KeyM: printableKey(libghostty.KeyM, 'm'),
	keys.KeyN: printableKey(libghostty.KeyN, 'n'),
	keys.KeyO: printableKey(libghostty.KeyO, 'o'),
	keys.KeyP: printableKey(libghostty.KeyP, 'p'),
	keys.KeyQ: printableKey(libghostty.KeyQ, 'q'),
	keys.KeyR: printableKey(libghostty.KeyR, 'r'),
	keys.KeyS: printableKey(libghostty.KeyS, 's'),
	keys.KeyT: printableKey(libghostty.KeyT, 't'),
	keys.KeyU: printableKey(libghostty.KeyU, 'u'),
	keys.KeyV: printableKey(libghostty.KeyV, 'v'),
	keys.KeyW: printableKey(libghostty.KeyW, 'w'),
	keys.KeyX: printableKey(libghostty.KeyX, 'x'),
	keys.KeyY: printableKey(libghostty.KeyY, 'y'),
	keys.KeyZ: printableKey(libghostty.KeyZ, 'z'),

	keys.Key0: printableKey(libghostty.KeyDigit0, '0'),
	keys.Key1: printableKey(libghostty.KeyDigit1, '1'),
	keys.Key2: printableKey(libghostty.KeyDigit2, '2'),
	keys.Key3: printableKey(libghostty.KeyDigit3, '3'),
	keys.Key4: printableKey(libghostty.KeyDigit4, '4'),
	keys.Key5: printableKey(libghostty.KeyDigit5, '5'),
	keys.Key6: printableKey(libghostty.KeyDigit6, '6'),
	keys.Key7: printableKey(libghostty.KeyDigit7, '7'),
	keys.Key8: printableKey(libghostty.KeyDigit8, '8'),
	keys.Key9: printableKey(libghostty.KeyDigit9, '9'),
}

// modTable maps the script modifiers to libghostty's. The keys.Mods bits are
// listed explicitly so a modifier added to keys is caught by
// TestModTableCoversEveryScriptModifier.
var modTable = []struct {
	script keys.Mods
	native libghostty.Mods
}{
	{keys.ModCtrl, libghostty.ModCtrl},
	{keys.ModAlt, libghostty.ModAlt},
	{keys.ModShift, libghostty.ModShift},
}

// nativeMods converts script modifiers to libghostty's. ok is false when mods
// has a bit that has no libghostty mapping.
func nativeMods(mods keys.Mods) (native libghostty.Mods, ok bool) {
	for _, m := range modTable {
		if mods&m.script != 0 {
			native |= m.native
			mods &^= m.script
		}
	}
	return native, mods == 0
}

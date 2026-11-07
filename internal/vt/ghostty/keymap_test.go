package ghostty

import (
	"testing"

	"github.com/mikelorant/shellshot/internal/script/keys"
)

// Every key name the script grammar accepts must have an encoder entry, and
// the table must hold nothing else.
func TestKeyTableCoversEveryScriptKey(t *testing.T) {
	names := keys.Names()
	for _, name := range names {
		c, err := keys.Parse(name)
		if err != nil {
			t.Fatalf("keys.Parse(%q): %v", name, err)
		}
		e, ok := keyTable[c.Key]
		if !ok {
			t.Errorf("key %q has no libghostty mapping", name)
			continue
		}
		if printable := e.text != ""; printable != (e.unshifted != 0) {
			t.Errorf("key %q: text %q and code point %q must be both set or both empty", name, e.text, e.unshifted)
		}
	}
	if len(keyTable) != len(names) {
		t.Errorf("keyTable has %d entries, keys.Names has %d", len(keyTable), len(names))
	}
}

func TestKeyTableMapsDistinctKeys(t *testing.T) {
	seen := map[int]keys.Key{}
	for k, e := range keyTable {
		if other, dup := seen[int(e.key)]; dup {
			t.Errorf("keys %v and %v map to the same libghostty key %d", k, other, e.key)
		}
		seen[int(e.key)] = k
	}
}

func TestModTableCoversEveryScriptModifier(t *testing.T) {
	all := keys.ModCtrl | keys.ModAlt | keys.ModShift
	// Each documented modifier maps; the three together cover all bits keys
	// defines (a new keys.Mods bit makes the next check fail).
	for _, m := range []keys.Mods{keys.ModCtrl, keys.ModAlt, keys.ModShift, all} {
		if _, ok := nativeMods(m); !ok {
			t.Errorf("nativeMods(%b) not mapped", m)
		}
	}
	if _, ok := nativeMods(keys.Mods(1 << 3)); ok {
		t.Error("nativeMods accepted a bit keys does not define")
	}
	if len(modTable) != 3 {
		t.Errorf("modTable has %d entries, want 3 (ctrl, alt, shift)", len(modTable))
	}
}

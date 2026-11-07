package ghostty

import (
	"errors"
	"fmt"
	"unicode/utf8"

	libghostty "go.mitchellh.com/libghostty"

	"github.com/michaellorant/shellshot/internal/script/keys"
	"github.com/michaellorant/shellshot/internal/vt"
)

// ErrEncoderClosed is returned by every KeyEncoder method called after Close.
var ErrEncoderClosed = errors.New("ghostty: key encoder is closed")

// KeyEncoder turns key chords and typed text into the bytes a program running
// in a Terminal expects on its input. Chords follow the terminal's modes as
// they are when Encode is called (application cursor keys, the Kitty keyboard
// protocol, ...), so vim, less or fish switching modes between two presses is
// handled.
//
// A KeyEncoder is bound to the Terminal it was created for and is NOT safe for
// concurrent use. It reads the terminal's state, so it must be used from the
// same goroutine that owns the terminal (the session loop), and only after
// all pending program output has been written to the terminal.
type KeyEncoder struct {
	term   *terminal
	enc    *libghostty.KeyEncoder
	ev     *libghostty.KeyEvent
	closed bool
}

// NewKeyEncoder creates the key encoder for t, which must be a terminal made
// by New. Close it before or after the terminal; it holds no native reference
// to it.
func NewKeyEncoder(t vt.Terminal) (*KeyEncoder, error) {
	term, ok := t.(*terminal)
	if !ok || term == nil {
		return nil, fmt.Errorf("ghostty: key encoder needs a terminal made by ghostty.New, got %T", t)
	}
	if term.closed {
		return nil, ErrClosed
	}
	enc, err := libghostty.NewKeyEncoder()
	if err != nil {
		return nil, fmt.Errorf("ghostty: create key encoder: %w", err)
	}
	ev, err := libghostty.NewKeyEvent()
	if err != nil {
		enc.Close()
		return nil, fmt.Errorf("ghostty: create key event: %w", err)
	}
	return &KeyEncoder{term: term, enc: enc, ev: ev}, nil
}

// Encode returns the bytes that pressing c sends, given the terminal's current
// modes. The result is a fresh slice owned by the caller.
//
// A nil result with a nil error means the encoder produced no bytes for the
// chord. None of the chords in the script grammar do, but the caller must
// treat that as a failure of the press rather than send nothing.
//
// It returns ErrClosed if the terminal is closed and ErrEncoderClosed if the
// encoder is.
func (k *KeyEncoder) Encode(c keys.Chord) ([]byte, error) {
	if err := k.usable(); err != nil {
		return nil, err
	}
	entry, ok := keyTable[c.Key]
	if !ok {
		return nil, fmt.Errorf("ghostty: cannot encode key %d: not a script key", c.Key)
	}
	mods, ok := nativeMods(c.Mods)
	if !ok {
		return nil, fmt.Errorf("ghostty: cannot encode %q: unknown modifier bits %#x", c, uint8(c.Mods))
	}

	// SetFromTerminal copies the modes at this instant and resets
	// option-as-alt, which macOS needs set for alt+<char> to send ESC <char>.
	if err := k.enc.SetFromTerminal(k.term.term); err != nil {
		return nil, fmt.Errorf("ghostty: read terminal key modes: %w", err)
	}
	if err := k.enc.SetOptionAsAlt(libghostty.OptionAsAltTrue); err != nil {
		return nil, fmt.Errorf("ghostty: set option-as-alt: %w", err)
	}

	// The event is reused, so every field is set each time.
	for _, err := range [...]error{
		k.ev.SetAction(libghostty.KeyActionPress),
		k.ev.SetKey(entry.key),
		k.ev.SetMods(mods),
		k.ev.SetConsumedMods(0),
		k.ev.SetUTF8(entry.text),
		k.ev.SetUnshiftedCodepoint(entry.unshifted),
	} {
		if err != nil {
			return nil, fmt.Errorf("ghostty: fill key event for %q: %w", c, err)
		}
	}

	out, err := k.enc.Encode(k.ev)
	if err != nil {
		return nil, fmt.Errorf("ghostty: encode %q: %w", c, err)
	}
	return out, nil
}

// EncodeText returns the bytes that typing s sends: its UTF-8, the same in
// every terminal mode, with each newline sent as Enter (CR, 0x0D). "\r\n"
// counts as one newline. An empty s gives nil, nil. The result is a fresh
// slice. s must be valid UTF-8.
func (k *KeyEncoder) EncodeText(s string) ([]byte, error) {
	if err := k.usable(); err != nil {
		return nil, err
	}
	if !utf8.ValidString(s) {
		return nil, fmt.Errorf("ghostty: cannot type %q: invalid UTF-8", s)
	}
	if s == "" {
		return nil, nil
	}
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == '\r' && i+1 < len(s) && s[i+1] == '\n':
			// Drop the CR of "\r\n"; the LF after it becomes the CR.
		case s[i] == '\n':
			out = append(out, '\r')
		default:
			out = append(out, s[i])
		}
	}
	return out, nil
}

// Close releases the native resources. It is safe to call more than once.
func (k *KeyEncoder) Close() {
	if k.closed {
		return
	}
	k.closed = true
	k.ev.Close()
	k.enc.Close()
}

func (k *KeyEncoder) usable() error {
	if k.closed {
		return ErrEncoderClosed
	}
	if k.term.closed {
		return ErrClosed
	}
	return nil
}

// Package fontchain builds the font set for an export: the embedded fonts,
// then fonts the user names, then (opt-in) fonts installed on the machine,
// plus the chain of colour emoji fonts.
//
// The embedded fonts come first so that adding fonts never changes a glyph
// they already draw. Installed fonts are off by default because they make an
// export differ from machine to machine.
package fontchain

import (
	"errors"
	"fmt"
	"os"

	"github.com/michaellorant/shellshot/internal/render/emoji"
	"github.com/michaellorant/shellshot/internal/render/font"
)

// Options selects the fonts added to the embedded chain.
type Options struct {
	// Files are font files (TTF, OTF or a TTC/OTC collection, whose first
	// font is used), consulted in order for runes the embedded fonts lack.
	Files []string
	// System adds the CJK and symbol fonts found in the system font
	// directories after Files.
	System bool
	// EmojiSet selects the built-in colour emoji fonts; the zero value is
	// emoji.SetApple (Apple's artwork, then Noto Color Emoji for what it
	// lacks).
	EmojiSet emoji.Set
	// EmojiFile is an optional colour emoji font (COLR v0 + CPAL, or
	// CBLC + CBDT) that is tried before the built-in set for every emoji.
	EmojiFile string
}

// EmojiFileError is the error New returns when Options.EmojiFile cannot be
// used. Its message names the file.
type EmojiFileError struct {
	File string
	Err  error
}

func (e *EmojiFileError) Error() string { return e.Err.Error() }

func (e *EmojiFileError) Unwrap() error { return e.Err }

// New returns the font set for o. Files that cannot be read or are not fonts
// are errors that name the file.
func New(o Options) (*font.Set, error) {
	extra, err := readFiles(o.Files, true)
	if err != nil {
		return nil, err
	}
	colour, err := emojiBackend(o)
	if err != nil {
		return nil, err
	}
	if o.System {
		// Broken system fonts are skipped: those files are not under the
		// user's control and should not fail the export.
		found, _ := readFiles(SystemFonts(SystemDirs()), false)
		extra = append(extra, found...)
	}
	set, err := font.Default(extra...)
	if err != nil {
		return nil, err
	}
	set.SetEmoji(colour)
	return set, nil
}

// emojiBackend returns the colour emoji chain for o: the user's font if any,
// then the built-in set. The built-in fonts are not decompressed here.
func emojiBackend(o Options) (emoji.Backend, error) {
	builtin, err := emoji.Default(o.EmojiSet)
	if err != nil {
		return nil, err
	}
	if o.EmojiFile == "" {
		return builtin, nil
	}
	data, err := os.ReadFile(o.EmojiFile)
	if err != nil {
		return nil, &EmojiFileError{o.EmojiFile, err}
	}
	user, err := emoji.Load(data)
	if err != nil {
		return nil, &EmojiFileError{o.EmojiFile, fmt.Errorf("%s is not a usable colour emoji font (it needs COLR v0 + CPAL or CBLC + CBDT colour tables): %w", o.EmojiFile, err)}
	}
	return emoji.Chain{user, builtin}, nil
}

// readFiles reads the font files at paths, each named by its path, and checks
// that they parse. With strict it stops at the first file that is unreadable
// or not a font, with an error naming it; otherwise it skips such files.
func readFiles(paths []string, strict bool) ([]font.FallbackFont, error) {
	fonts := make([]font.FallbackFont, 0, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			fb := font.FallbackFont{Name: p, Data: data}
			if err = fb.Validate(); err == nil {
				fonts = append(fonts, fb)
				continue
			}
			var bad *font.FallbackError
			if errors.As(err, &bad) {
				err = fmt.Errorf("%s is not a valid font: %w", p, bad.Err)
			}
		}
		if strict {
			return nil, err
		}
	}
	return fonts, nil
}

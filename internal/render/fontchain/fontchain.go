// Package fontchain builds the font set for an export: the embedded fonts,
// then fonts the user names, then (opt-in) fonts installed on the machine.
//
// The embedded fonts come first so that adding fonts never changes a glyph
// they already draw. Installed fonts are off by default because they make an
// export differ from machine to machine.
package fontchain

import (
	"errors"
	"fmt"
	"os"

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
}

// New returns the font set for o. Files that cannot be read or are not fonts
// are errors that name the file.
func New(o Options) (*font.Set, error) {
	extra, err := readFiles(o.Files, true)
	if err != nil {
		return nil, err
	}
	if o.System {
		// Broken system fonts are skipped: those files are not under the
		// user's control and should not fail the export.
		found, _ := readFiles(SystemFonts(SystemDirs()), false)
		extra = append(extra, found...)
	}
	return font.Default(extra...)
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

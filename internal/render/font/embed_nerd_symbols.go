package font

import (
	_ "embed"
)

// Embedded Symbols Nerd Font Mono v3.5.1 (MIT; bundled icon sets carry their
// own licences). The unmodified 2.6 MB file is used as the fallback for
// private-use icon glyphs. See assets/nerd-symbols/NOTICE.txt.
var (
	//go:embed assets/nerd-symbols/SymbolsNerdFontMono-Regular.ttf
	symbolsNerdFontMono []byte
)

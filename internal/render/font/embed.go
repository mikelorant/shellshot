package font

import (
	_ "embed"
)

// Embedded JetBrains Mono v2.304 (SIL OFL 1.1). See assets/NOTICE.txt for
// source and checksums and assets/OFL.txt for the licence.
var (
	//go:embed assets/JetBrainsMono-Regular.ttf
	jetBrainsMonoRegular []byte
	//go:embed assets/JetBrainsMono-Bold.ttf
	jetBrainsMonoBold []byte
	//go:embed assets/JetBrainsMono-Italic.ttf
	jetBrainsMonoItalic []byte
	//go:embed assets/JetBrainsMono-BoldItalic.ttf
	jetBrainsMonoBoldItalic []byte
)

// Embedded Symbols Nerd Font Mono v3.5.1 (MIT; bundled icon sets carry their
// own licences). The unmodified 2.6 MB file is used as the fallback for
// private-use icon glyphs. See assets/NOTICE.txt.
var (
	//go:embed assets/SymbolsNerdFontMono-Regular.ttf
	symbolsNerdFontMono []byte
)

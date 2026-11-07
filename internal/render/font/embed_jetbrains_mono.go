package font

import (
	_ "embed"
)

// Embedded JetBrains Mono v2.304 (SIL OFL 1.1). See
// assets/jetbrains-mono/NOTICE.txt for source and checksums and
// assets/jetbrains-mono/OFL.txt for the licence.
var (
	//go:embed assets/jetbrains-mono/JetBrainsMono-Regular.ttf
	jetBrainsMonoRegular []byte
	//go:embed assets/jetbrains-mono/JetBrainsMono-Bold.ttf
	jetBrainsMonoBold []byte
	//go:embed assets/jetbrains-mono/JetBrainsMono-Italic.ttf
	jetBrainsMonoItalic []byte
	//go:embed assets/jetbrains-mono/JetBrainsMono-BoldItalic.ttf
	jetBrainsMonoBoldItalic []byte
)

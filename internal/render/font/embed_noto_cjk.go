package font

import (
	_ "embed"
)

// Embedded Noto Sans Mono CJK SC Regular subset (SIL OFL 1.1): the common
// hanzi, Hangul syllables, kana and CJK punctuation. See
// assets/noto-cjk/NOTICE.txt.
var (
	//go:embed assets/noto-cjk/NotoSansMonoCJKsc-Subset.otf
	notoSansMonoCJK []byte
)

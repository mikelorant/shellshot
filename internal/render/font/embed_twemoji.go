package font

import (
	_ "embed"
)

// Embedded Twemoji Mozilla v0.7.0 (COLRv0 colour emoji; artwork CC BY 4.0).
// See assets/twemoji-mozilla/NOTICE.txt.
var (
	//go:embed assets/twemoji-mozilla/TwemojiMozilla.ttf
	twemojiMozilla []byte
)

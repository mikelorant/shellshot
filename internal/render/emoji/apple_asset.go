package emoji

import (
	_ "embed"
)

// appleFontZstd is the compact Apple Color Emoji derivative built by
// scripts/build-apple-emoji-asset.py (one 96 ppem CBDT strike, GSUB "ccmp"
// ligatures for emoji sequences), compressed with zstd. See
// assets/apple-emoji/NOTICE.txt for its provenance and licence notes.
//
//go:embed assets/apple-emoji/AppleColorEmoji-shellshot.ttf.zst
var appleFontZstd []byte

// AppleFontZstd returns the embedded zstd-compressed Apple Color Emoji
// derivative. The caller decompresses and parses it; the bytes must not be
// modified.
func AppleFontZstd() []byte { return appleFontZstd }

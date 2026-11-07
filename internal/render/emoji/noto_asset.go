package emoji

import _ "embed"

// notoFontZstd is Noto Color Emoji (CBDT/CBLC bitmap font, SIL OFL 1.1),
// zstd-compressed. Decompressed it is byte-identical to upstream
// googlefonts/noto-emoji 2D/fonts/NotoColorEmoji.ttf at commit
// 99701e07d3d1fe1bdbcdb14964547f0fd592966f
// (sha256 15671215ab769fdc7162a045d56fd7d7e477c51b04e6b3c761d914d8fdd6cc44).
// Refresh it with `make noto-asset`.
//
//go:embed assets/noto-emoji/NotoColorEmoji.ttf.zst
var notoFontZstd []byte

// NotoFontZstd returns the zstd-compressed Noto Color Emoji font. The caller
// must not modify the returned slice.
func NotoFontZstd() []byte { return notoFontZstd }

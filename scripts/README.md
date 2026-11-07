# scripts

* `build-libghostty.sh` - builds libghostty from the `third_party/ghostty` submodule (`make deps`).
* `subset-cjk-font.sh` - regenerates the embedded Noto CJK subset.
* `build-apple-emoji-asset.py` - regenerates the embedded Apple Color Emoji
  derivative (`make emoji-asset`).

## Regenerating the Apple Color Emoji asset

`make emoji-asset` rebuilds `internal/render/emoji/assets/apple-emoji/AppleColorEmoji-shellshot.ttf.zst`
and its `.sha256` (the hash of the decompressed font) from the font that
github.com/samuelngs/apple-emoji-ttf publishes in its GitHub Releases, so no
Mac is needed. It:

1. initialises the `third_party/apple-emoji-ttf` submodule (`--depth 1`), whose
   conversion code (HarfBuzz shaping oracle, bitmap composition, GSUB builder,
   CBDT/CBLC writer) the script imports;
2. downloads `AppleColorEmoji-Linux.ttf` (116 MB) from the pinned release tag
   into `build/apple-emoji/` and verifies its sha256;
3. keeps one 96 ppem strike, builds a GSUB `ccmp` ligature lookup for every
   emoji-test.txt sequence, drops everything else, quantises the PNGs and
   zstd-compresses the font.

Requirements: `git`, network access, and [`uv`](https://docs.astral.sh/uv/)
(it fetches Python 3.12 and the pinned packages fonttools, pillow, pyyaml,
uharfbuzz, imagequant and zstandard; nothing is installed globally). The
build takes under a minute and is deterministic for the pinned versions.
Options (`--ppem`, `--colors`, `--dither`, `--out-dir`, `--asset-dir`,
`--no-asset`) are listed by `python scripts/build-apple-emoji-asset.py --help`,
for example `--ppem 64 --no-asset --out-dir /tmp/x` to try a smaller strike.

Apple Inc. owns the artwork; see `internal/render/emoji/assets/apple-emoji/NOTICE.txt` for
provenance and the licence notice.

# scripts

* `build-libghostty.sh` - builds libghostty from the `third_party/ghostty` submodule (`make deps`).
* `subset-cjk-font.sh` - regenerates the embedded Noto CJK subset.
* `check-examples-render.sh` and `check-examples-render.py` - render every demo in `contrib/examples`
  and check it (`make check-examples`, see below).
* `build-apple-emoji-asset.py` - regenerates the embedded Apple Color Emoji
  derivative (`make emoji-asset`).

Both emoji scripts compress at zstd level 9 (level 19 saves at most 1.2% on these PNG-based fonts and
takes far longer); the currently committed assets were compressed at level 19 and are not rebuilt
until a font pin changes.

## Checking the examples render

`make check-examples` (`scripts/check-examples-render.sh [demo ...]`) records each
`contrib/examples/*.sh` at 80x24 with `bin/shellshot record --codec none` (speed `fast`), then
`check-examples-render.py` reads the raw output:

1. It follows the cursor through the byte stream (CUP, relative moves, save/restore, alternate
   screen, scroll regions, wide and combining characters) and reports every printable character
   written at column == width, which is a real auto-wrap: demo, byte offset, screen row and column,
   and the text just written. A wrap breaks the layout and overwrites the line below.
2. It lists every non-ASCII character cluster the demos print and looks each one up in the fonts
   shellshot embeds: the sprites for box, block, braille and Powerline characters, JetBrains Mono,
   Symbols Nerd Font Mono, the Noto Sans Mono CJK SC subset (3,755 GB2312 level-1 hanzi, Hangul,
   kana) and Noto Color Emoji (cmap and `ccmp` ligatures, as the renderer resolves them). A
   cluster none of them draws would render blank and is flagged. Best effort: it knows the fonts,
   not the renderer.

Exit status 1 means findings, 2 that the check could not run. The Python helper uses only the
standard library for the wrap check; the shell script runs it through `uv` (fonttools,
zstandard, pinned) for the font check, so it needs `uv` or `python3` with those packages. It is
not part of `make test`: it needs libghostty and `uv` and takes a minute. `check-examples-render.py
--selftest` runs the tracker's unit checks.

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

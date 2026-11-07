#!/usr/bin/env bash
# Rebuilds internal/render/font/assets/noto-cjk/NotoSansMonoCJKsc-Subset.otf from
# Noto Sans Mono CJK SC Regular (SIL OFL 1.1) for the embedded CJK fallback.
# Needs python3 and `pip install fonttools==4.68.0`. The subset keeps:
#   - 3,755 GB2312 level-1 hanzi, the most used simplified Chinese characters
#   - 2,350 KS X 1001 Hangul syllables
#   - U+3000-30FF (CJK punctuation, hiragana, katakana), U+FF00-FFEF (full-
#     and half-width forms) and U+2010-2027 (general punctuation)
# Layout features and hinting are dropped: the export path needs outlines only.
# Usage: scripts/subset-cjk-font.sh SOURCE.otf OUTPUT.otf
set -euo pipefail

src=${1:?usage: $0 SOURCE.otf OUTPUT.otf}
out=${2:?usage: $0 SOURCE.otf OUTPUT.otf}
chars=$(mktemp)
trap 'rm -f "$chars"' EXIT

python3 - "$chars" <<'PY'
import sys

chars = set()
for hi in range(0xB0, 0xD8):  # GB2312 level 1
    for lo in range(0xA1, 0xFF):
        try:
            chars.add(bytes([hi, lo]).decode("gb2312"))
        except UnicodeDecodeError:
            pass
for hi in range(0xB0, 0xC9):  # KS X 1001 Hangul
    for lo in range(0xA1, 0xFF):
        try:
            chars.add(bytes([hi, lo]).decode("euc_kr"))
        except UnicodeDecodeError:
            pass
for lo, hi in [(0x3000, 0x30FF), (0xFF00, 0xFFEF), (0x2010, 0x2027)]:
    chars.update(chr(c) for c in range(lo, hi + 1))
open(sys.argv[1], "w", encoding="utf-8").write("".join(sorted(chars)))
PY

pyftsubset "$src" --text-file="$chars" --layout-features= --no-hinting \
	--desubroutinize --output-file="$out"

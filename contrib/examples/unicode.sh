#!/bin/sh
# shellcheck disable=SC1091,SC2034
# unicode.sh: Unicode text: box drawing, block elements, braille, wide CJK
# characters and combining marks. Safe: prints text only. See contrib/examples/README.md.
#   sh contrib/examples/unicode.sh [--force] [--pause]

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='Unicode: box drawing, blocks, braille, wide CJK, combining marks.'
. "$EX_DIR/lib.sh"
ex_init "$@"

# label TEXT: a dim section label.
label() { printf '\033[2m%s\033[0m\n' "$1"; nap 350; }

ex_heading "unicode.sh: Unicode"

label "box drawing: light, double, rounded, heavy"
printf '┌───────┬───────┐ ╔════════╦═══════╗ ╭─────────╮ ┏━━━━━━━━━┓\n'
printf '│ light │ cells │ ║ double ║ cells ║ │ rounded │ ┃  heavy  ┃\n'
printf '└───────┴───────┘ ╚════════╩═══════╝ ╰─────────╯ ┗━━━━━━━━━┛\n'

label "block elements and shades"
printf '\033[32m▁▂▃▄▅▆▇█\033[0m  ░▒▓█  ▀▄ ▌▐  \033[36m▖▗▘▝ ▙▟▛▜ ▚▞\033[0m  \033[35m■ □ ▪ ▫ ▲ △ ▶ ▷ ● ○ ◆ ◇\033[0m\n'

label "braille patterns (2x4 dots per cell)"
printf '\033[33m⠀⠁⠃⠇⡇⣇⣧⣷⣿\033[0m  ⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏  \033[34m⢀⣠⣴⣾⣿⣷⣦⣄⡀\033[0m  ⣿⣿⣿⣿⣿ ⣀⣀⣀ ⠉⠉⠉\n'

label "wide CJK and fullwidth: two cells per glyph, so the frames stay straight"
ex_rep '─' 18
cjk_line=$EX_REP
printf '┌%s┐ ┌%s┐\n' "$cjk_line" "$cjk_line"
printf '│ ひらがなカタカナ │ │ 汉字 한글 ＡＢＣ │\n'
printf '└%s┘ └%s┘\n' "$cjk_line" "$cjk_line"

label "combining marks: composed and decomposed forms take the same width"
ACUTE=$(printf '\314\201')
GRAVE=$(printf '\314\200')
TILDE=$(printf '\314\203')
UMLAUT=$(printf '\314\210')
RING=$(printf '\314\212')
CIRCUMFLEX=$(printf '\314\202')
DOT_BELOW=$(printf '\314\243')
DOT_ABOVE=$(printf '\314\207')
printf 'composed:   |é|è|ñ|ö|å|    decomposed: |e%s|e%s|n%s|o%s|a%s|\n' "$ACUTE" "$GRAVE" "$TILDE" "$UMLAUT" "$RING"
printf 'stacked:    |s%s%s|  |e%s%s|  |e%s%s|\n' "$DOT_BELOW" "$DOT_ABOVE" "$CIRCUMFLEX" "$ACUTE" "$CIRCUMFLEX" "$DOT_BELOW"
printf 'scripts:    |Ελληνικά|  |Русский|  |한국어|  |ひらがな|\n'

label "symbols"
printf '← ↑ → ↓ ↔  ∑ ∫ √ ∞ ≈ ≠ ≤ ≥  α β γ δ π Ω  ✓ ✗\n'
printf '♠ ♥ ♦ ♣  ⌘ ⌥ ⎋ ⏎  ◉ ◎  € £ ¥ © ® ™ ° ± × ÷\n'
nap 1500
ex_finish

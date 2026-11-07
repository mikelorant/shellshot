#!/bin/sh
# shellcheck disable=SC1091,SC2034,SC1003
# showcase.sh: a curated tour of the other demos, nine short scenes with a title
# each (about 30 s). Safe: prints text and escape sequences only.
# See contrib/examples/README.md.
#   sh contrib/examples/showcase.sh [--force] [--pause]      EXAMPLES_SPEED=fast: about 8 s

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='A curated tour of the demos in nine short scenes (about 30 s).'
. "$EX_DIR/lib.sh"
ex_init "$@"

R=$EX_H
W=$EX_W
GW=$((W - 2))
ZWJ=$(printf '\342\200\215')
VS16=$(printf '\357\270\217')
KEYCAP=$(printf '\342\203\243')
ACUTE=$(printf '\314\201')

# caption N TITLE: clear the screen and show the scene title in a reverse-video bar.
caption() {
    ex_clear
    ex_rep ' ' $((W - ${#2} - 8))
    printf '\033[1;7m %d/9  %s%s \033[0m\n\n' "$1" "$2" "$EX_REP"
    nap 450
}

# card TITLE SUBTITLE: a centred title card.
card() {
    ex_clear
    ex_cup $((R / 2 - 1)) $(((W - ${#1}) / 2))
    printf '\033[1;38;5;75m%s\033[0m' "$1"
    ex_cup $((R / 2 + 1)) $(((W - ${#2}) / 2))
    printf '\033[2m%s\033[0m' "$2"
    nap 1300
}

# attr LABEL SGR TEXT
attr() { printf '  \033[2m%-20s\033[0m \033[%sm%s\033[0m\n' "$1" "$2" "$3"; nap 110; }

card 'shellshot examples' 'colours, attributes, unicode, emoji, cursor, animation, OSC, logs, TUI'

# ---- 1 colours
caption 1 'Colours: 16, 256 and truecolour'
printf ' '
n=0
while [ "$n" -lt 16 ]; do
    if [ "$n" -lt 8 ]; then printf '\033[%dm   ' $((40 + n)); else printf '\033[%dm   ' $((92 + n)); fi
    n=$((n + 1))
done
printf '\033[0m\n\n '
x=0
while [ "$x" -lt "$GW" ]; do printf '\033[48;5;%dm ' $((16 + x * 216 / GW)); x=$((x + 1)); done
printf '\033[0m\n\n '
ex_rainbow "$GW"
printf '\n\n'
nap 400
line=0
while [ "$line" -lt 5 ]; do
    printf ' '
    x=0
    while [ "$x" -lt "$GW" ]; do
        ex_hue $((x * 1530 / GW))
        top=$((255 - line * 50))
        bottom=$((235 - line * 50))
        printf '\033[38;2;%d;%d;%d;48;2;%d;%d;%dm▀' $((EX_R * top / 255)) $((EX_G * top / 255)) $((EX_B * top / 255)) $((EX_R * bottom / 255)) $((EX_G * bottom / 255)) $((EX_B * bottom / 255))
        x=$((x + 1))
    done
    printf '\033[0m\n'
    line=$((line + 1))
    nap 100
done
nap 900

# ---- 2 attributes
caption 2 'Attributes and underline styles'
attr 'bold' 1 'The quick brown fox'
attr 'faint' 2 'The quick brown fox'
attr 'italic' 3 'The quick brown fox'
attr 'underline' 4 'The quick brown fox'
attr 'double underline' 21 'The quick brown fox'
attr 'curly (4:3)' '4:3;58;2;255;90;90' 'The quick brown fox'
attr 'dotted (4:4)' 4:4 'The quick brown fox'
attr 'strikethrough' 9 'The quick brown fox'
attr 'inverse' 7 'The quick brown fox'
attr 'bold italic colour' '1;3;38;2;255;170;0' 'The quick brown fox'
nap 900

# ---- 3 unicode
caption 3 'Unicode: box drawing, blocks, braille, CJK'
printf '┌───────┬───────┐ ╔═══════╦═══════╗ ╭─────────╮ ┏━━━━━━━━━┓\n'
printf '│ light │ cells │ ║ double║ cells ║ │ rounded │ ┃  heavy  ┃\n'
printf '└───────┴───────┘ ╚═══════╩═══════╝ ╰─────────╯ ┗━━━━━━━━━┛\n\n'
nap 400
printf '\033[32m▁▂▃▄▅▆▇█\033[0m  ░▒▓█  \033[36m▖▗▘▝ ▙▟▛▜\033[0m  \033[33m⠁⠃⠇⡇⣇⣧⣷⣿\033[0m  \033[34m⢀⣠⣴⣾⣿⣷⣦⣄⡀\033[0m\n\n'
nap 400
ex_rep '─' 18
printf '┌%s┐ ┌%s┐\n│ 日本語のテキスト │ │ 漢字 한글 ＡＢＣ │\n└%s┘ └%s┘\n\n' "$EX_REP" "$EX_REP" "$EX_REP" "$EX_REP"
nap 400
printf 'combining marks: |e%s|  |é|  accents stay inside their cell\n' "$ACUTE"
nap 1100

# ---- 4 emoji
caption 4 'Emoji: skin tones, ZWJ, flags, keycaps'
printf '  single      😀 🚀 🔥 🎉 🐙 🍕 🌈 🦀 🐧\n\n'
nap 350
printf '  skin tones  👍 👍🏻 👍🏼 👍🏽 👍🏾 👍🏿\n\n'
nap 350
printf '  ZWJ         %s %s %s %s\n\n' "👨${ZWJ}👩${ZWJ}👧${ZWJ}👦" "👩${ZWJ}💻" "🏳${VS16}${ZWJ}🌈" "🧑${ZWJ}🚀"
nap 350
printf '  flags       🇯🇵 🇫🇷 🇧🇷 🇨🇦 🇺🇸 🇬🇧 🇩🇪 🇮🇳 🇰🇷\n\n'
nap 350
printf '  keycaps     1%s%s 2%s%s 3%s%s #%s%s *%s%s\n' "$VS16" "$KEYCAP" "$VS16" "$KEYCAP" "$VS16" "$KEYCAP" "$VS16" "$KEYCAP" "$VS16" "$KEYCAP"
nap 1100

# ---- 5 cursor and screen control
caption 5 'Cursor control, save/restore, scroll region'
ex_box 4 3 7 36 'CUP + box'
ex_cup 6 6; printf 'boxes drawn with absolute moves'
ex_cup 7 6; printf '\033[1;35mDECSC\033[0m / \033[1;35mDECRC\033[0m keep a position'
ex_cup 5 $((W - 30)); printf 'typing with a ticking counter:'
ex_cup 6 $((W - 30))
n=1
for word in ESC 7 saves and ESC 8 restores; do
    ex_save
    ex_cup 8 $((W - 30)); printf '\033[1;7m tick %d \033[0m' "$n"
    ex_restore
    printf '%s ' "$word"
    n=$((n + 1))
    nap 160
done
nap 300
ex_cup 13 3; printf '\033[2mscroll region rows 14-%d, header and footer stay:\033[0m' $((R - 2))
ex_region_on 14 $((R - 2))
ex_cup $((R - 2)) 1
i=1
while [ "$i" -le 12 ]; do
    printf '\033[32m%02d\033[0m scrolling inside the region\n' "$i"
    nap 90
    i=$((i + 1))
done
ex_region_off
ex_cup "$R" 1
nap 700

# ---- 6 animation
caption 6 'Animation: spinner, progress, sparkline, bounce'
i=0
while [ "$i" -lt 14 ]; do
    case $((i % 10)) in
        0) f='⠋' ;; 1) f='⠙' ;; 2) f='⠹' ;; 3) f='⠸' ;; 4) f='⠼' ;;
        5) f='⠴' ;; 6) f='⠦' ;; 7) f='⠧' ;; 8) f='⠇' ;; *) f='⠏' ;;
    esac
    ex_cup 3 3; printf '\033[36m%s\033[0m Resolving dependencies...' "$f"
    nap 70
    i=$((i + 1))
done
ex_cup 3 3; printf '\033[1;32m✔\033[0m Resolved 42 packages        '
p=0
while [ "$p" -le 100 ]; do
    ex_cup 5 3; printf 'Downloading '; ex_bar "$p" 40; printf ' %3d%%' "$p"
    nap 30
    p=$((p + 4))
done
t=0
while [ "$t" -lt 24 ]; do
    ex_cup 7 3; printf 'cpu  '
    x=0
    while [ "$x" -lt 48 ]; do ex_wave $((x + t)); ex_spark "$EX_V"; x=$((x + 1)); done
    printf '\033[0m'
    nap 60
    t=$((t + 1))
done
xr=$((W - 16))
t=0
px=3
while [ "$t" -lt 50 ]; do
    pos=$((t * 2 % (2 * xr)))
    if [ "$pos" -lt "$xr" ]; then x=$pos; else x=$((2 * xr - pos)); fi
    ex_cup 10 "$px"; printf '         '
    px=$((x + 3))
    ex_hue $((t * 31))
    ex_cup 10 "$px"; printf '\033[1;38;2;%d;%d;%dmshellshot\033[0m' "$EX_R" "$EX_G" "$EX_B"
    nap 25
    t=$((t + 1))
done
nap 600

# ---- 7 OSC
caption 7 'OSC: window title and hyperlinks'
printf '\033[22;2t'
EX_TITLE=1
printf '\033]2;shellshot showcase: OSC 2 title\033\\'
printf '  window title set with OSC 2 (see the title bar)\n\n'
nap 500
printf '  project page  \033]8;;https://github.com/ghostty-org/ghostty\033\\Ghostty\033]8;;\033\\\n\n'
nap 400
printf '  styled        \033[4;38;2;90;170;255m\033]8;;https://example.com/styled\033\\underlined blue link\033]8;;\033\\\033[0m\n\n'
nap 400
printf '  \033[2mlinks are printed only, never opened\033[0m\n'
nap 800

# ---- 8 build log
caption 8 'A fake build log with coloured levels'
clock=$((14 * 3600000 + 3 * 60000 + 7 * 1000 + 120))
log() {
    nap "$4"
    clock=$((clock + $4))
    case $1 in DEBUG) lc=2 ;; INFO) lc='1;32' ;; WARN) lc='1;33' ;; ERROR) lc='1;97;41' ;; *) lc='1;30;42' ;; esac
    secs=$((clock / 1000))
    printf '\033[2m%02d:%02d:%02d.%03d\033[0m \033[%sm%-5s\033[0m \033[36m%-8s\033[0m %s\n' $((secs / 3600 % 24)) $((secs / 60 % 60)) $((secs % 60)) $((clock % 1000)) "$lc" "$1" "$2" "$3"
}
log INFO build 'shellshot 0.4.2 (release)' 100
log DEBUG cache 'restored 212 entries' 160
log INFO compile '[ 1/4] internal/vt' 200
log WARN lint 'atlas.go:88: unused parameter' 120
log INFO compile '[ 2/4] internal/render' 220
log INFO compile '[ 3/4] internal/export' 180
log ERROR test 'FAIL pty TestResize (try 1/2)' 350
log WARN test 'flaky test, retrying' 150
log INFO test 'ok    internal/pty (0.84s)' 280
log INFO link 'bin/shellshot (12.4 MiB)' 200
log OK build 'BUILD OK in 2.1s: 4 pkgs, 2 warnings' 90
nap 900

# ---- 9 dashboard in the alternate screen
ex_alt_on
bw=$((W / 2))
ex_rep ' ' "$W"
ex_cup 1 1; printf '\033[7m%s\033[0m' "$EX_REP"
ex_cup 1 2; printf '\033[1;7m9/9  Dashboard in the alternate screen\033[0m'
ex_box 3 1 7 "$bw" 'CPU'
ex_box 3 $((bw + 1)) 7 $((W - bw)) 'History'
ex_box 11 1 $((R - 11)) "$W" 'Processes'
t=0
while [ "$t" -lt 22 ]; do
    i=0
    while [ "$i" -lt 4 ]; do
        ex_wave $((t / 2 + i * 5))
        pct=$((12 + EX_V * 10 + (t + i * 7) % 9))
        ex_cup $((4 + i)) 3; printf 'core%d ' "$i"; ex_bar "$pct" $((bw - 15)); printf ' %3d%%' "$pct"
        i=$((i + 1))
    done
    ex_cup 5 $((bw + 3)); printf 'cpu  '
    x=0
    while [ "$x" -lt $((W - bw - 10)) ]; do ex_wave $((x + t)); ex_spark "$EX_V"; x=$((x + 1)); done
    printf '\033[0m'
    ex_cup 6 $((bw + 3)); printf 'mem  '
    x=0
    while [ "$x" -lt $((W - bw - 10)) ]; do ex_wave $((x * 2 + t + 7)); ex_spark "$EX_V"; x=$((x + 1)); done
    printf '\033[0m'
    ex_cup 12 3; printf '\033[1;4m%-6s %-10s %5s  %-24s\033[0m' PID USER CPU COMMAND
    r=0
    while [ "$r" -lt 6 ]; do
        case $r in
            0) pid=1203; cmd='postgres: writer' ;; 1) pid=4411; cmd='shellshot record' ;;
            2) pid=892; cmd=dockerd ;; 3) pid=3170; cmd=ghostty ;;
            4) pid=2201; cmd='node server.js' ;; *) pid=6012; cmd=zsh ;;
        esac
        ex_cup $((13 + r)) 3
        if [ "$r" -eq $((t / 4 % 6)) ]; then printf '\033[7m'; fi
        printf '%-6s %-10s %4d%%  %-24s\033[0m' "$pid" mike $((60 - r * 9 - (t + r) % 4)) "$cmd"
        r=$((r + 1))
    done
    nap 140
    t=$((t + 1))
done
nap 500
ex_alt_off

card 'That was shellshot' 'run: sh contrib/examples/NAME.sh   (see contrib/examples/README.md)'
ex_cup "$R" 1
ex_finish

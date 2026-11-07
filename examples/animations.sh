#!/bin/sh
# shellcheck disable=SC1091,SC2034,SC2153
# animations.sh: animation: a braille spinner, a gradient progress bar, scrolling
# sparklines, falling "rain" and bouncing text, all redrawn in place with cursor
# addressing. Safe: prints escape sequences only. See examples/README.md.
#   sh examples/animations.sh [--force] [--pause]      about 12 s

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='Animation: spinner, progress bar, sparklines, rain, bouncing text.'
. "$EX_DIR/lib.sh"
ex_init "$@"

R=$EX_H
W=$EX_W

ex_clear
ex_cup 1 1
ex_heading "animations.sh: spinner, progress, sparklines, rain, bounce"

# ------------------------------------------------------------- spinner
i=0
while [ "$i" -lt 28 ]; do
    case $((i % 10)) in
        0) f='⠋' ;; 1) f='⠙' ;; 2) f='⠹' ;; 3) f='⠸' ;; 4) f='⠼' ;;
        5) f='⠴' ;; 6) f='⠦' ;; 7) f='⠧' ;; 8) f='⠇' ;; *) f='⠏' ;;
    esac
    ex_cup 3 3
    printf '\033[36m%s\033[0m Resolving dependencies%.*s\033[K' "$f" $((i / 3 % 4)) '...'
    nap 75
    i=$((i + 1))
done
ex_cup 3 3
printf '\033[1;32m✔\033[0m Resolved 42 packages in 2.1s\033[K'

# ------------------------------------------------------- progress bar
bw=$((W / 2 - 6))
if [ "$bw" -gt 50 ]; then bw=50; fi
p=0
while [ "$p" -le 100 ]; do
    ex_cup 5 3
    printf 'Downloading '
    ex_bar "$p" "$bw"
    printf ' %3d%%  %d.%d MB/s\033[K' "$p" $((8 + p % 7)) $((p * 7 % 10))
    nap 28
    p=$((p + 2))
done
ex_cup 5 3
printf '\033[1;32m✔\033[0m Downloaded '
ex_bar 100 "$bw"
printf ' 100%%  done\033[K'

# ----------------------------------------------------------- sparklines
sw=$((W - 22))
if [ "$sw" -gt 56 ]; then sw=56; fi
t=0
while [ "$t" -lt 40 ]; do
    ex_cup 7 3
    printf 'cpu  '
    x=0
    while [ "$x" -lt "$sw" ]; do ex_wave $((x + t)); ex_spark "$EX_V"; x=$((x + 1)); done
    printf '\033[0m %3d%%' $((10 + EX_V * 12))
    ex_cup 8 3
    printf 'net  '
    x=0
    while [ "$x" -lt "$sw" ]; do ex_wave $((x * 3 + t * 2 + 5)); ex_spark $(((EX_V + x % 3) % 8)); x=$((x + 1)); done
    printf '\033[0m %3d%%' $((20 + EX_V * 10))
    nap 45
    t=$((t + 1))
done

# ----------------------------------------------------------------- rain
top=10
hr=$((R - 6 - top + 1))
trail=5
period=$((hr + trail + 3))
ex_cup 9 3
printf '\033[2mrain\033[0m'
t=0
while [ "$t" -lt 45 ]; do
    k=0
    col=3
    frame=
    while [ "$col" -lt $((W - 2)) ]; do
        speed=$((1 + k * 7 % 3))
        head=$(((t * speed / 2 + k * 13) % period))
        y=$((head - trail - 2))
        while [ "$y" -le "$head" ]; do
            if [ "$y" -ge 0 ] && [ "$y" -lt "$hr" ]; then
                d=$((head - y))
                case $(((k + y + t / 2) % 8)) in
                    0) g=0 ;; 1) g=1 ;; 2) g=7 ;; 3) g=: ;; 4) g='=' ;; 5) g='*' ;; 6) g='#' ;; *) g='$' ;;
                esac
                case $d in
                    0) c='1;38;5;231' ;; 1) c='38;5;46' ;; 2) c='38;5;40' ;; 3) c='38;5;34' ;; 4) c='38;5;28' ;; 5) c='38;5;22' ;; *) c=0; g=' ' ;;
                esac
                frame="${frame}${EX_ESC}[$((top + y));${col}H${EX_ESC}[${c}m${g}"
            fi
            y=$((y + 1))
        done
        col=$((col + 3))
        k=$((k + 1))
    done
    printf '%s\033[0m' "$frame"
    nap 40
    t=$((t + 1))
done

# --------------------------------------------------------------- bounce
bt=$((R - 3))
ex_cup $((R - 4)) 3
printf '\033[2mbouncing text\033[0m'
text='shellshot'
xr=$((W - 16))
px=3
py=$bt
t=0
while [ "$t" -lt 70 ]; do
    pos=$((t % (2 * xr)))
    if [ "$pos" -lt "$xr" ]; then x=$pos; else x=$((2 * xr - pos)); fi
    step=$((t / 3 % 4))
    if [ "$step" -lt 2 ]; then y=$step; else y=$((4 - step)); fi
    ex_cup "$py" "$px"
    printf '         '
    px=$((x + 3))
    py=$((bt + y))
    ex_hue $((t * 23))
    ex_cup "$py" "$px"
    printf '\033[1;38;2;%d;%d;%dm%s\033[0m' "$EX_R" "$EX_G" "$EX_B" "$text"
    nap 25
    t=$((t + 1))
done
ex_cup "$R" 1
nap 800
ex_finish

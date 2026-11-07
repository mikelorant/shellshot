#!/bin/sh
# shellcheck disable=SC1091,SC2034
# colors.sh: ANSI colours on a terminal: 16 colours, the 256-colour palette and
# truecolour gradients. Safe: prints escape sequences only. See contrib/examples/README.md.
#   sh contrib/examples/colors.sh [--force] [--pause]      EXAMPLES_SPEED=fast shortens the pauses

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='ANSI colours: 16, 256 and truecolour gradients.'
. "$EX_DIR/lib.sh"
ex_init "$@"

ex_heading "colors.sh: ANSI colours"
nap 400

printf '\033[2m16 colours: SGR 40-47 and 100-107 (background), 30-37 and 90-97 (foreground)\033[0m\n'
n=0
while [ "$n" -lt 8 ]; do
    if [ "$n" -lt 2 ] || [ "$n" -eq 4 ]; then fgc=97; else fgc=30; fi
    printf '\033[%d;%dm %-3s\033[0m' $((40 + n)) "$fgc" "$n"
    n=$((n + 1))
done
printf '\n'
n=0
while [ "$n" -lt 8 ]; do
    printf '\033[30;%dm %-3s\033[0m' $((100 + n)) $((n + 8))
    n=$((n + 1))
done
printf '\n'
printf '\033[30;47mblack\033[0m   \033[31mred\033[0m     \033[32mgreen\033[0m   \033[33myellow\033[0m  \033[34mblue\033[0m    \033[35mmagenta\033[0m \033[36mcyan\033[0m    \033[37mwhite\033[0m\n'
printf '\033[90mgrey\033[0m    \033[91mred\033[0m     \033[92mgreen\033[0m   \033[93myellow\033[0m  \033[94mblue\033[0m    \033[95mmagenta\033[0m \033[96mcyan\033[0m    \033[97mwhite\033[0m\n'
nap 700

printf '\033[2m256 colours: SGR 48;5;N (6x6x6 cube 16-231, greys 232-255)\033[0m\n'
row=0
while [ "$row" -lt 3 ]; do
    n=$((16 + row * 72))
    while [ "$n" -lt $((16 + row * 72 + 72)) ]; do
        printf '\033[48;5;%dm ' "$n"
        n=$((n + 1))
    done
    printf '\033[0m\n'
    row=$((row + 1))
    nap 120
done
n=232
while [ "$n" -lt 256 ]; do
    printf '\033[48;5;%dm  ' "$n"
    n=$((n + 1))
done
printf '\033[0m\n'
nap 700

gw=$((EX_W - 2))
printf '\033[2mtruecolour: SGR 48;2;R;G;B\033[0m\n'
printf ' '
ex_rainbow "$gw"
printf '\n '
x=0
while [ "$x" -lt "$gw" ]; do
    printf '\033[48;2;%d;%d;%dm ' $((255 * x / gw)) $((64 + 100 * x / gw)) $((255 - 255 * x / gw))
    x=$((x + 1))
done
printf '\033[0m\n '
x=0
while [ "$x" -lt "$gw" ]; do
    g=$((255 * x / gw))
    printf '\033[48;2;%d;%d;%dm ' "$g" "$g" "$g"
    x=$((x + 1))
done
printf '\033[0m\n'
nap 500

# Two vertical samples per text row: foreground = upper half, background = lower half.
line=0
while [ "$line" -lt 6 ]; do
    printf ' '
    x=0
    while [ "$x" -lt "$gw" ]; do
        ex_hue $((x * 1530 / gw))
        hr=$EX_R
        hg=$EX_G
        hb=$EX_B
        top=$((255 - line * 42))
        bottom=$((255 - line * 42 - 21))
        printf '\033[38;2;%d;%d;%d;48;2;%d;%d;%dm▀' $((hr * top / 255)) $((hg * top / 255)) $((hb * top / 255)) $((hr * bottom / 255)) $((hg * bottom / 255)) $((hb * bottom / 255))
        x=$((x + 1))
    done
    printf '\033[0m\n'
    line=$((line + 1))
    nap 80
done
nap 1500
ex_finish

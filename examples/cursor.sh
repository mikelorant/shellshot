#!/bin/sh
# shellcheck disable=SC1091,SC2034
# cursor.sh: cursor movement and screen control: absolute and relative addressing,
# save/restore, erase, insert/delete, a scroll region and the alternate screen.
# Safe: prints escape sequences only. See examples/README.md.
#   sh examples/cursor.sh [--force] [--pause]

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='Cursor movement, save/restore, erase, scroll region, alternate screen.'
. "$EX_DIR/lib.sh"
ex_init "$@"

R=$EX_H
C=$EX_W

# scene_begin TITLE: clear the screen and draw a reverse-video title bar.
scene_begin() {
    ex_clear
    ex_rep ' ' $((C - ${#1} - 2))
    printf '\033[7m %s%s \033[0m' "$1" "$EX_REP"
}

# ---------------------------------------------------------------- 1: CUP
scene_begin 'CUP: absolute addressing   ESC [ row ; col H'
ex_cup 3 1; printf '(3,1)'
ex_cup 3 $((C - 6)); printf '(3,%d)' "$C"
ex_cup "$R" 1; printf '(%d,1)' "$R"
ex_cup "$R" $((C - 7)); printf '(%d,%d)' "$R" "$C"
i=0
while [ "$i" -le 24 ]; do
    ex_hue $((i * 60))
    ex_cup $((4 + i * (R - 6) / 24)) $((8 + i * (C - 22) / 24))
    printf '\033[38;2;%d;%d;%dm●\033[0m' "$EX_R" "$EX_G" "$EX_B"
    nap 40
    i=$((i + 1))
done
ex_cup $((R / 2)) $((C / 2 - 14))
printf '\033[1;7m every glyph placed with CUP \033[0m'
nap 600

# ------------------------------------------------- 2: relative movement
scene_begin 'CUU CUD CUF CUB, CHA, VPA: relative movement'
ex_cup 4 6
w=30
h=9
i=0
while [ "$i" -lt "$w" ]; do printf '\033[36m#'; i=$((i + 1)); done
i=1
while [ "$i" -lt "$h" ]; do printf '\033[B\033[D\033[33m#'; i=$((i + 1)); nap 30; done
i=1
while [ "$i" -lt "$w" ]; do printf '\033[2D\033[35m#'; i=$((i + 1)); done
i=2
while [ "$i" -lt "$h" ]; do printf '\033[A\033[D\033[32m#'; i=$((i + 1)); nap 30; done
printf '\033[0m'
printf '\033[6d\033[10GVPA 6, CHA 10: text inside the frame'
printf '\033[2B\033[10G\033[1mCUD 2 then CHA 10\033[0m'
printf '\033[2B\033[10G\033[3mCUF/CUB: \033[5Cfive right\033[5D<-'
printf '\033[0m'
printf '\033[4;42H\033[1m ESC[nA up\033[0m'
printf '\033[B\033[42G ESC[nB down'
printf '\033[B\033[42G ESC[nC forward'
printf '\033[B\033[42G ESC[nD back'
printf '\033[B\033[42G ESC[nG column'
printf '\033[B\033[42G ESC[nd row'
nap 700

# ------------------------------------------------------ 3: save / restore
scene_begin 'DECSC / DECRC: save and restore the cursor   ESC 7  ESC 8'
ex_cup 4 3
printf 'Typing here; a counter ticks in the corner. '
n=1
for word in the cursor jumps away and returns to this exact spot every time and continues; do
    ex_save
    ex_cup 2 $((C - 10))
    printf '\033[1;7m tick %-2d \033[0m' "$n"
    ex_restore
    printf '%s ' "$word"
    n=$((n + 1))
    nap 130
done
ex_cup 8 3
printf 'Also CSI s / CSI u work in many terminals, ESC 7 / ESC 8 almost everywhere.'
nap 600

# ------------------------------------------------------------ 4: erase
scene_begin 'Erase: EL (ESC [ n K) and ED (ESC [ n J)'
for r in 3 4 5 8 9 10 11 12 13; do
    ex_cup "$r" 3
    printf '\033[34mXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX\033[0m'
done
nap 500
ex_cup 3 23; printf '\033[K';  ex_cup 3 45; printf '<- EL 0: cursor to end of line'
ex_cup 4 23; printf '\033[1K'; ex_cup 4 45; printf '<- EL 1: start of line to cursor'
ex_cup 5 23; printf '\033[2K'; ex_cup 5 45; printf '<- EL 2: whole line'
nap 800
ex_cup 11 23; printf '\033[J'
ex_cup 8 45; printf '<- ED 0 at (11,23) erased'
ex_cup 9 45; printf '   from there to screen end'
nap 600

# ------------------------------------------------- 5: insert and delete
scene_begin 'IL DL ICH DCH: insert and delete lines and characters'
i=1
while [ "$i" -le 6 ]; do ex_cup $((i + 3)) 3; printf 'line %d' "$i"; i=$((i + 1)); done
ex_cup 12 3; printf 'ABCDEFGHIJKLMNOP'
nap 600
ex_cup 6 3; printf '\033[2L'; printf '\033[1;33m<- IL 2: two blank lines opened here\033[0m'
nap 800
ex_cup 6 3; printf '\033[2M'
ex_cup 9 3; printf '\033[1;33mDL 2: and removed again\033[0m'
nap 800
ex_cup 12 6; printf '\033[4@'; printf '\033[1;32m<ICH>\033[0m'
ex_cup 13 3; printf 'ICH 4 opened four cells at column 6'
nap 800
ex_cup 12 6; printf '\033[7P'
ex_cup 14 3; printf 'DCH 7 closed the gap and removed three letters'
nap 600

# --------------------------------------------------------- 6: scroll region
scene_begin 'DECSTBM: a scroll region keeps header and footer fixed'
ex_cup 2 1; printf '\033[2mheader: stays put (rows 1-3)\033[0m'
ex_cup $((R - 1)) 1; printf '\033[2mfooter: stays put (rows %d-%d)\033[0m' "$((R - 1))" "$R"
ex_region_on 4 $((R - 3))
ex_cup $((R - 3)) 1
i=1
while [ "$i" -le 26 ]; do
    printf '\033[32m%02d\033[0m scrolling line inside the region, rows 4-%d\n' "$i" "$((R - 3))"
    ex_save
    ex_cup "$R" 1
    printf '\033[1m%02d/26\033[0m' "$i"
    ex_restore
    i=$((i + 1))
    nap 60
done
ex_region_off
ex_cup "$R" 1
nap 700

# -------------------------------------------------------- 7: alternate screen
ex_cup "$((R - 2))" 1
printf '\033[1mEntering the alternate screen (CSI ? 1049 h)...\033[0m'
nap 700
ex_alt_on
bw=44
ex_box $((R / 2 - 3)) $((C / 2 - bw / 2)) 7 "$bw" 'alternate screen'
ex_cup $((R / 2 - 1)) $((C / 2 - 17)); printf 'A separate buffer: no scrollback here.'
ex_cup $((R / 2)) $((C / 2 - 17)); printf 'Leaving restores the screen you saw.'
ex_cup $((R / 2 + 1)) $((C / 2 - 17)); printf '\033[2mReturning in a moment\033[0m'
nap 800
ex_alt_off
ex_cup "$R" 1
printf '\033[1;32mBack on the main screen: the scroll-region log is still here.\033[0m'
nap 900
printf '\n'
ex_finish

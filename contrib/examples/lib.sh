# shellcheck shell=sh disable=SC2034,SC1003
# lib.sh: shared helpers for the shellshot example scripts. Source it; do not run it.
#
# Every example starts with the same few lines (they find this file whether the
# script is run as `sh contrib/examples/x.sh`, `sh /abs/path/x.sh` or `./x.sh`):
#
#   case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
#   EX_NAME=${0##*/}
#   . "$EX_DIR/lib.sh"
#   ex_init "$@"
#
# Safety contract (the examples never break it): POSIX sh; only printf and sleep,
# plus a guarded stty/tput to learn the terminal size; no file writes or deletes,
# no network, no eval, no environment or file reads, and no stdin reads except the
# documented --pause. The terminal is restored on exit, INT and TERM.
#
# Options handled by ex_init: --force (run even if stdout is not a terminal),
# --pause (wait for Enter at the end), -h/--help.
# Environment: EXAMPLES_SPEED=fast shortens every nap to a quarter.

EX_ACTIVE=0     # 1 once ex_init has touched the terminal; cleanup does nothing before
EX_ALT=0        # 1 while the alternate screen is on
EX_REGION=0     # 1 while a scroll region is set
EX_TITLE=0      # 1 while a window title was pushed
EX_ESC=$(printf '\033')   # for building escape strings without a printf per piece
EX_OWED=0       # milliseconds not yet slept when fractional sleep is missing

# ---------------------------------------------------------------- start/stop

ex_usage() {
    printf 'Usage: sh %s [--force] [--pause]\n' "$EX_NAME"
    printf '%s\n' "${EX_DESC:-A shellshot example.}"
    printf '  --force   run even when stdout is not a terminal\n'
    printf '  --pause   wait for Enter before the final screen is restored\n'
    printf '  EXAMPLES_SPEED=fast shortens the pauses\n'
}

ex_init() {
    EX_FORCE=0
    EX_PAUSE=0
    for ex_arg in "$@"; do
        case $ex_arg in
            --force) EX_FORCE=1 ;;
            --pause) EX_PAUSE=1 ;;
            -h | --help) ex_usage; exit 0 ;;
            *) printf '%s: unknown option %s (try --help)\n' "$EX_NAME" "$ex_arg" >&2; exit 2 ;;
        esac
    done
    if [ "$EX_FORCE" -eq 0 ] && [ ! -t 1 ]; then
        printf '%s: stdout is not a terminal; use --force to print the escape sequences anyway\n' "$EX_NAME" >&2
        exit 2
    fi
    ex_size
    ex_speed
    EX_ACTIVE=1
    printf '\033[0m\033[?25l'
}

# Restores everything a demo may have changed. Safe to call twice.
ex_cleanup() {
    if [ "$EX_ACTIVE" -ne 1 ]; then
        return 0
    fi
    EX_ACTIVE=0
    printf '\033[0m\033]8;;\033\\'
    if [ "$EX_REGION" -eq 1 ]; then
        printf '\033[r'
    fi
    if [ "$EX_ALT" -eq 1 ]; then
        printf '\033[?1049l'
    fi
    if [ "$EX_REGION" -eq 1 ]; then
        printf '\033[r\033[%d;1H' "$EX_ROWS"
    fi
    if [ "$EX_TITLE" -eq 1 ]; then
        printf '\033[23;2t'
    fi
    printf '\033[?25h\033[0m'
}

ex_on_signal() {
    ex_cleanup
    printf '\n'
    exit "$1"
}

trap ex_cleanup EXIT
trap 'ex_on_signal 130' INT
trap 'ex_on_signal 143' TERM

# Waits for Enter when --pause was given; the only place stdin is read.
ex_finish() {
    if [ "$EX_PAUSE" -eq 1 ]; then
        printf '\033[0m\033[?25h'
        read -r ex_reply || :
    fi
}

# ------------------------------------------------------------------- sizing

# Sets EX_COLS/EX_ROWS (the real size, default 80x24) and EX_W/EX_H (the area the
# demos use: at least 40x12, at most 120x40).
ex_size() {
    ex_wh=
    if [ -t 0 ]; then
        ex_wh=$(stty size 2>/dev/null)
    elif [ -t 2 ]; then
        ex_wh=$(stty size <&2 2>/dev/null)
    fi
    EX_ROWS=
    EX_COLS=
    case $ex_wh in
        *' '*) EX_ROWS=${ex_wh% *}; EX_COLS=${ex_wh#* } ;;
    esac
    if [ -z "$EX_COLS" ] && command -v tput >/dev/null 2>&1; then
        EX_COLS=$(tput cols 2>/dev/null)
        EX_ROWS=$(tput lines 2>/dev/null)
    fi
    case $EX_COLS in '' | *[!0-9]*) EX_COLS=${COLUMNS:-80} ;; esac
    case $EX_ROWS in '' | *[!0-9]*) EX_ROWS=${LINES:-24} ;; esac
    case $EX_COLS in '' | *[!0-9]*) EX_COLS=80 ;; esac
    case $EX_ROWS in '' | *[!0-9]*) EX_ROWS=24 ;; esac
    if [ "$EX_COLS" -lt 1 ]; then EX_COLS=80; fi
    if [ "$EX_ROWS" -lt 1 ]; then EX_ROWS=24; fi
    EX_W=$EX_COLS
    EX_H=$EX_ROWS
    if [ "$EX_W" -lt 40 ]; then EX_W=40; fi
    if [ "$EX_H" -lt 12 ]; then EX_H=12; fi
    if [ "$EX_W" -gt 120 ]; then EX_W=120; fi
    if [ "$EX_H" -gt 40 ]; then EX_H=40; fi
}

# ------------------------------------------------------------------- timing

ex_speed() {
    EX_FAST=0
    if [ "${EXAMPLES_SPEED:-normal}" = fast ]; then
        EX_FAST=1
    fi
    EX_FRAC=0
    if sleep 0.001 2>/dev/null; then
        EX_FRAC=1
    fi
}

# nap MS: sleep for MS milliseconds (a quarter of that with EXAMPLES_SPEED=fast).
# Without fractional sleep support the milliseconds are summed up and paid with
# whole `sleep 1` calls, so the total time stays about right.
nap() {
    ex_ms=$1
    if [ "$EX_FAST" -eq 1 ]; then
        ex_ms=$((ex_ms / 4))
    fi
    if [ "$ex_ms" -le 0 ]; then
        return 0
    fi
    if [ "$EX_FRAC" -eq 1 ]; then
        ex_frac=$((ex_ms % 1000))
        case $ex_frac in
            ?) ex_frac=00$ex_frac ;;
            ??) ex_frac=0$ex_frac ;;
        esac
        sleep "$((ex_ms / 1000)).$ex_frac"
    else
        EX_OWED=$((EX_OWED + ex_ms))
        if [ "$EX_OWED" -ge 1000 ]; then
            sleep 1
            EX_OWED=$((EX_OWED - 1000))
        fi
    fi
}

# ---------------------------------------------------------- escape sequences

ex_cup() { printf '\033[%d;%dH' "$1" "$2"; }       # cursor to row, column (1-based)
ex_sgr() { printf '\033[%sm' "$1"; }
ex_rst() { printf '\033[0m'; }
ex_save() { printf '\033%s' 7; }                    # DECSC (a digit right after \033 would read as octal)
ex_restore() { printf '\033%s' 8; }                 # DECRC
ex_clear() { printf '\033[2J\033[H'; }
ex_fg256() { printf '\033[38;5;%dm' "$1"; }
ex_bg256() { printf '\033[48;5;%dm' "$1"; }
ex_fgrgb() { printf '\033[38;2;%d;%d;%dm' "$1" "$2" "$3"; }
ex_bgrgb() { printf '\033[48;2;%d;%d;%dm' "$1" "$2" "$3"; }

ex_alt_on() { EX_ALT=1; printf '\033[?1049h\033[2J\033[H'; }
ex_alt_off() { printf '\033[?1049l'; EX_ALT=0; }
ex_region_on() { EX_REGION=1; printf '\033[%d;%dr' "$1" "$2"; }
ex_region_off() { printf '\033[r'; EX_REGION=0; }

# Prints a bold, coloured heading line.
ex_heading() { printf '\033[1;38;5;75m%s\033[0m\n' "$1"; }

# --------------------------------------------------------------- generators

# ex_rep CHAR COUNT: sets EX_REP to CHAR repeated COUNT times (CHAR may be UTF-8).
ex_rep() {
    EX_REP=
    ex_n=0
    while [ "$ex_n" -lt "$2" ]; do
        EX_REP=$EX_REP$1
        ex_n=$((ex_n + 1))
    done
}

# ex_hue H: sets EX_R EX_G EX_B to a fully saturated colour; H runs 0..1529 round the wheel.
ex_hue() {
    ex_h=$(($1 % 1530))
    ex_hv=$((ex_h % 255))
    case $((ex_h / 255)) in
        0) EX_R=255; EX_G=$ex_hv; EX_B=0 ;;
        1) EX_R=$((255 - ex_hv)); EX_G=255; EX_B=0 ;;
        2) EX_R=0; EX_G=255; EX_B=$ex_hv ;;
        3) EX_R=0; EX_G=$((255 - ex_hv)); EX_B=255 ;;
        4) EX_R=$ex_hv; EX_G=0; EX_B=255 ;;
        *) EX_R=255; EX_G=0; EX_B=$((255 - ex_hv)) ;;
    esac
}

# ex_rainbow WIDTH [OFFSET]: WIDTH spaces on a truecolour rainbow background.
ex_rainbow() {
    ex_x=0
    while [ "$ex_x" -lt "$1" ]; do
        ex_hue $((ex_x * 1530 / $1 + ${2:-0}))
        printf '\033[48;2;%d;%d;%dm ' "$EX_R" "$EX_G" "$EX_B"
        ex_x=$((ex_x + 1))
    done
    printf '\033[0m'
}

# ex_wave N: sets EX_V (0..7) to a sine-like value that repeats every 16 steps.
ex_wave() {
    case $(($1 % 16)) in
        0) EX_V=4 ;; 1) EX_V=5 ;; 2) EX_V=6 ;; 3) EX_V=7 ;; 4) EX_V=7 ;; 5) EX_V=7 ;;
        6) EX_V=6 ;; 7) EX_V=5 ;; 8) EX_V=4 ;; 9) EX_V=2 ;; 10) EX_V=1 ;; 11) EX_V=0 ;;
        12) EX_V=0 ;; 13) EX_V=0 ;; 14) EX_V=1 ;; *) EX_V=2 ;;
    esac
}

# ex_spark V: prints one sparkline glyph (V 0..7) in a green-to-red colour.
ex_spark() {
    case $1 in
        0) printf '\033[38;5;34m▁' ;;
        1) printf '\033[38;5;70m▂' ;;
        2) printf '\033[38;5;106m▃' ;;
        3) printf '\033[38;5;142m▄' ;;
        4) printf '\033[38;5;178m▅' ;;
        5) printf '\033[38;5;214m▆' ;;
        6) printf '\033[38;5;208m▇' ;;
        *) printf '\033[38;5;196m█' ;;
    esac
}

# ex_bar PERCENT WIDTH: a progress bar on a truecolour gradient, then a reset.
ex_bar() {
    ex_filled=$(($1 * $2 / 100))
    ex_x=0
    while [ "$ex_x" -lt "$2" ]; do
        if [ "$ex_x" -lt "$ex_filled" ]; then
            printf '\033[38;2;%d;%d;%dm█' $((40 + 180 * ex_x / $2)) $((210 - 60 * ex_x / $2)) $((120 + 100 * ex_x / $2))
        else
            printf '\033[38;5;238m░'
        fi
        ex_x=$((ex_x + 1))
    done
    printf '\033[0m'
}

# ex_box ROW COL HEIGHT WIDTH [TITLE]: a light box drawn with absolute addressing.
ex_box() {
    ex_rep '─' $(($4 - 2))
    ex_line=$EX_REP
    ex_cup "$1" "$2"
    printf '┌%s┐' "$ex_line"
    ex_y=1
    while [ "$ex_y" -lt $(($3 - 1)) ]; do
        ex_cup $(($1 + ex_y)) "$2"
        printf '│'
        ex_cup $(($1 + ex_y)) $(($2 + $4 - 1))
        printf '│'
        ex_y=$((ex_y + 1))
    done
    ex_cup $(($1 + $3 - 1)) "$2"
    printf '└%s┘' "$ex_line"
    if [ -n "${5:-}" ]; then
        ex_cup "$1" $(($2 + 2))
        printf '\033[1m %s \033[0m' "$5"
    fi
}

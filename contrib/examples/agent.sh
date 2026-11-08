#!/bin/sh
# shellcheck disable=SC1091,SC2034,SC2088
# agent.sh: a fake terminal session, one continuous take. Someone makes a directory,
# starts a (fictional) coding agent called "quill" in its own full-screen TUI, asks for a
# small terminal dashboard, watches it "work", quits, builds the result and runs it.
# NOTHING IS EXECUTED: every command, tool call and number is only printed, typed out one
# character at a time; the dashboard values come from counters. See contrib/examples/README.md.
#   sh contrib/examples/agent.sh [--force] [--pause]      about 55 s

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='A fake session: a coding agent TUI writes a dashboard, then the dashboard runs (nothing is executed).'
. "$EX_DIR/lib.sh"
ex_init "$@"

# ------------------------------------------------------------ typing and cursor

# The cursor is drawn by hand (a reverse-video blank) so it can blink and move freely.
cur_on() { printf '\033[7m \033[0m\033[D'; }
cur_off() { printf ' \033[D'; }

# hold MS: wait MS milliseconds with the cursor blinking (it is shown at the start and the end).
hold() {
    ho=$1
    while [ "$ho" -ge 600 ]; do
        nap 380
        cur_off
        nap 220
        cur_on
        ho=$((ho - 600))
    done
    nap "$ho"
}

# typing TEXT: types TEXT (ASCII) one character at a time. The delay depends on the
# character position only, so every run looks the same, but it is not metronomic.
# TY_SGR is the SGR parameter string the text is typed in (0 = plain).
TY_SGR=0
typing() {
    ty_s=$1
    ty_i=0
    while [ -n "$ty_s" ]; do
        ty_rest=${ty_s#?}
        ty_c=${ty_s%"$ty_rest"}
        ty_s=$ty_rest
        ty_i=$((ty_i + 1))
        printf '\033[%sm%s\033[0m\033[7m \033[0m\033[D' "$TY_SGR" "$ty_c"
        ty_d=$((26 + (ty_i * 37 + ty_i * ty_i * 11) % 41))
        if [ "$ty_c" = ' ' ]; then ty_d=$((ty_d + 22)); fi
        if [ $((ty_i % 13)) -eq 0 ]; then ty_d=$((ty_d + 110)); fi
        nap "$ty_d"
    done
}

# stream TEXT: prints TEXT like a model answer that arrives in small pieces.
stream() {
    st_s=$1
    st_i=0
    while [ -n "$st_s" ]; do
        st_rest=${st_s#?}
        st_c=${st_s%"$st_rest"}
        st_s=$st_rest
        st_i=$((st_i + 1))
        printf '%s' "$st_c"
        if [ $((st_i % 3)) -eq 0 ]; then nap $((16 + st_i * 7 % 13)); fi
    done
}

# spinner N: sets SPIN to the Nth braille spinner frame.
spinner() {
    case $(($1 % 10)) in
        0) SPIN='⠋' ;; 1) SPIN='⠙' ;; 2) SPIN='⠹' ;; 3) SPIN='⠸' ;; 4) SPIN='⠼' ;;
        5) SPIN='⠴' ;; 6) SPIN='⠦' ;; 7) SPIN='⠧' ;; 8) SPIN='⠇' ;; *) SPIN='⠏' ;;
    esac
}

# ---------------------------------------------------------------- the shell

# prompt DIR: a two-part colour prompt, user@host then the directory.
prompt() {
    printf '\033[1;38;5;114mdev@laptop\033[0m \033[1;38;5;75m%s\033[0m \033[38;5;213m$\033[0m ' "$1"
}

# shell_line DIR TEXT THINK PAUSE: prompt, THINK ms of idle, TEXT typed, PAUSE ms, Enter.
shell_line() {
    prompt "$1"
    cur_on
    hold "$3"
    TY_SGR=0
    typing "$2"
    hold "$4"
    printf '\033[K\n'
}

# ------------------------------------------------------------- the agent TUI

AG_VIOLET='38;5;141'
AG_GREY='38;5;240'

# ag_status TOKENS: the status line at the bottom (TOKENS is a text such as 1.3k).
ag_status() {
    ex_cup 24 1
    printf '\033[2m  sprout-2 · ~/tui-demo · %s tokens\033[0m\033[K' "$1"
}

# ag_input_box COLOUR: the rounded input box on rows 19-22 with its prompt glyph.
ag_input_box() {
    ex_rep '─' 78
    ex_cup 19 1; printf '\033[%sm╭%s╮\033[0m' "$1" "$EX_REP"
    ex_cup 20 1; printf '\033[%sm│\033[0m%78s\033[%sm│\033[0m' "$1" '' "$1"
    ex_cup 21 1; printf '\033[%sm│\033[0m%78s\033[%sm│\033[0m' "$1" '' "$1"
    ex_cup 22 1; printf '\033[%sm╰%s╯\033[0m' "$1" "$EX_REP"
}

# ag_screen: banner, tip, input box (empty) and hint.
ag_screen() {
    ex_alt_on
    ex_rep '─' 78
    ex_cup 1 1; printf '\033[%sm╭%s╮\033[0m' "$AG_VIOLET" "$EX_REP"
    ex_cup 2 1; printf '\033[%sm│\033[0m' "$AG_VIOLET"
    ex_cup 2 3; printf '\033[1;%sm◆ quill\033[0m \033[2m0.3\033[0m  ·  \033[2mmodel:\033[0m \033[38;5;150msprout-2\033[0m' "$AG_VIOLET"
    ex_cup 2 68; printf '\033[2m~/tui-demo\033[0m'
    ex_cup 2 80; printf '\033[%sm│\033[0m' "$AG_VIOLET"
    ex_cup 3 1; printf '\033[%sm╰%s╯\033[0m' "$AG_VIOLET" "$EX_REP"
    ex_cup 4 2; printf '\033[2mTip: describe the program you want and quill writes the files here.\033[0m'
    ag_input_box "$AG_VIOLET"
    ex_cup 20 3; printf '\033[1;%sm❯\033[0m' "$AG_VIOLET"
    ex_cup 23 3; printf '\033[2menter send · /exit or ctrl+d to quit\033[0m'
    ag_status 0.0k
}

# ag_tool ROW LABEL STAT STATSGR HOLD: a tool call that spins, then gets its checkmark.
ag_tool() {
    at_n=0
    while [ "$at_n" -lt 7 ]; do
        spinner "$at_n"
        ex_cup "$1" 4
        printf '\033[%sm%s\033[0m \033[2m%s…\033[0m' "$AG_VIOLET" "$SPIN" "$2"
        nap 90
        at_n=$((at_n + 1))
    done
    nap "$5"
    ex_cup "$1" 4
    printf '\033[1;32m✓\033[0m %s\033[K' "$2"
    ex_cup "$1" 64
    printf '\033[%sm%s\033[0m' "$4" "$3"
    nap 260
}

agent_session() {
    ag_screen
    ex_cup 20 5
    cur_on
    hold 1100
    TY_SGR='1;97'
    typing 'Make a small terminal dashboard in Go that shows a live CPU graph and a'
    cur_off
    ex_cup 21 5
    cur_on
    typing 'process list. One file, no dependencies.'
    hold 620
    cur_off

    # the request moves into the conversation, the input box goes quiet
    ag_input_box "$AG_GREY"
    ex_cup 20 3; printf '\033[2m❯ waiting for quill…\033[0m'
    ex_cup 6 2; printf '\033[48;5;236m \033[1;%sm❯\033[0;1;97;48;5;236m Make a small terminal dashboard in Go that shows a live CPU graph and a  \033[0m' "$AG_VIOLET"
    ex_cup 7 2; printf '\033[48;5;236m   \033[1;97mprocess list. One file, no dependencies.%33s\033[0m' ''
    ag_status 0.4k

    # thinking
    at_n=0
    while [ "$at_n" -lt 38 ]; do
        spinner "$at_n"
        ex_cup 9 2
        case $((at_n / 3 % 4)) in 0) dots= ;; 1) dots=. ;; 2) dots=.. ;; *) dots=... ;; esac
        printf '\033[%sm%s\033[0m \033[2mThinking%s\033[0m  \033[2m(%d.%ds)\033[0m\033[K' "$AG_VIOLET" "$SPIN" "$dots" $((at_n / 10)) $((at_n % 10))
        nap 100
        at_n=$((at_n + 1))
    done
    ex_cup 9 2
    printf '\033[2K'
    ex_cup 9 2
    printf '\033[%sm◆\033[0m ' "$AG_VIOLET"
    stream "I'll write one main.go with only the standard library and ANSI escapes."
    ag_status 1.3k
    nap 800

    ag_tool 11 'Write main.go' '+163 -0' 32 900
    ag_status 3.8k
    ag_tool 12 'Write go.mod' '+4 -0' 32 500
    ag_status 4.0k
    ag_tool 13 'Check gofmt, go vet' 'clean' 32 1000
    ag_status 4.1k
    nap 500

    ex_cup 15 2
    printf '\033[%sm◆\033[0m ' "$AG_VIOLET"
    stream 'A single main.go: per-core CPU bars, a scrolling history graph and a'
    ex_cup 16 4
    stream 'process list, redrawn every 120 ms with plain ANSI escapes.'
    ag_status 4.6k
    nap 450
    ex_cup 17 4
    printf '\033[1;32mDone.\033[0m Run it with: \033[1;38;5;117mgo build && ./tui-demo\033[0m'
    nap 1300

    # the input box is ready again; the user leaves
    ag_input_box "$AG_VIOLET"
    ex_cup 20 3; printf '\033[1;%sm❯\033[0m' "$AG_VIOLET"
    ex_cup 20 5
    cur_on
    hold 1900
    TY_SGR='1;97'
    typing '/exit'
    hold 520
    cur_off
    nap 200
    ex_alt_off
}

# ------------------------------------------------------------ the dashboard

# proc N: sets PID, PNAME, BASE (cpu %), MEMB (mem %) and THR for process row N.
proc() {
    case $1 in
        0) PID=7421; PNAME=ffmpeg; BASE=74; MEMB=8; THR=16 ;;
        1) PID=2201; PNAME=node; BASE=58; MEMB=12; THR=11 ;;
        2) PID=3012; PNAME=postgres; BASE=44; MEMB=6; THR=7 ;;
        3) PID=5530; PNAME='go build'; BASE=36; MEMB=4; THR=14 ;;
        4) PID=1180; PNAME=nginx; BASE=27; MEMB=1; THR=5 ;;
        5) PID=4096; PNAME=rsync; BASE=19; MEMB=2; THR=3 ;;
        6) PID=6620; PNAME=tui-demo; BASE=12; MEMB=1; THR=6 ;;
        7) PID=903; PNAME=sshd; BASE=7; MEMB=0; THR=2 ;;
        *) PID=1; PNAME=init; BASE=2; MEMB=0; THR=1 ;;
    esac
}

# braille L R: sets BRA to the braille cell with L dots lit in its left column and R in its
# right column (0..4 each, counted from the bottom).
braille() {
    case $(($1 * 5 + $2)) in
        0) BRA=' ' ;;  1) BRA='⢀' ;;  2) BRA='⢠' ;;  3) BRA='⢰' ;;  4) BRA='⢸' ;;
        5) BRA='⡀' ;;  6) BRA='⣀' ;;  7) BRA='⣠' ;;  8) BRA='⣰' ;;  9) BRA='⣸' ;;
        10) BRA='⡄' ;; 11) BRA='⣄' ;; 12) BRA='⣤' ;; 13) BRA='⣴' ;; 14) BRA='⣼' ;;
        15) BRA='⡆' ;; 16) BRA='⣆' ;; 17) BRA='⣦' ;; 18) BRA='⣶' ;; 19) BRA='⣾' ;;
        20) BRA='⡇' ;; 21) BRA='⣇' ;; 22) BRA='⣧' ;; 23) BRA='⣷' ;; *) BRA='⣿' ;;
    esac
}

# dash_chrome: the static parts: title bar, boxes, table header, footer.
dash_chrome() {
    ex_alt_on
    ex_rep ' ' 80
    bar=$EX_REP
    ex_cup 1 1; printf '\033[7m%s\033[0m' "$bar"
    ex_cup 1 2; printf '\033[1;7mtui-demo\033[0;7m  live CPU and processes\033[0m'
    ex_box 2 1 10 40 'CPU cores'
    ex_box 2 41 10 40 'History'
    ex_box 12 1 12 80 'Processes'
    ex_cup 13 3; printf '\033[1;4;38;5;75m%-7s %-13s %5s  %-20s  %5s  %4s  %-9s\033[0m' PID NAME CPU% LOAD MEM% THR STATE
    ex_cup 24 1; printf '\033[7m%s\033[0m' "$bar"
    ex_cup 24 2; printf '\033[7mq quit   ↑/↓ select   r refresh\033[0m'
}

# dash_frame T: redraws everything that moves for frame T.
dash_frame() {
    ft=$1
    ex_cup 1 68
    printf '\033[7mup 00:00:%02d\033[0m' $((ft / 7))

    # per-core bars
    i=0
    total=0
    while [ "$i" -lt 8 ]; do
        ex_wave $((ft / 2 + i * 3))
        pct=$((8 + EX_V * 9 + (ft + i * 7) % 9))
        total=$((total + pct))
        ex_cup $((3 + i)) 3
        printf 'c%d ' "$i"
        ex_bar "$pct" 22
        printf ' %3d%%' "$pct"
        i=$((i + 1))
    done

    # history graph: 76 samples scrolling left, 8 cells high, 4 dots per cell
    ex_cup 2 70
    printf '\033[1m %3d%% \033[0m' $((total / 8))
    heights=
    k=0
    while [ "$k" -lt 76 ]; do
        n=$((ft * 2 + k))
        ex_wave $((n / 3))
        a=$EX_V
        ex_wave $((n * 2 / 5 + 6))
        h=$((2 + a * 2 + EX_V * 2 + n * 13 % 5))
        if [ "$h" -gt 32 ]; then h=32; fi
        heights="$heights $h"
        k=$((k + 1))
    done
    row=0
    while [ "$row" -lt 8 ]; do
        base=$(((7 - row) * 4))
        case $row in
            0) col=196 ;; 1) col=208 ;; 2) col=214 ;; 3) col=178 ;;
            4) col=142 ;; 5) col=106 ;; 6) col=70 ;; *) col=34 ;;
        esac
        line=
        # shellcheck disable=SC2086
        set -- $heights
        while [ "$#" -ge 2 ]; do
            dl=$(($1 - base))
            dr=$(($2 - base))
            if [ "$dl" -lt 0 ]; then dl=0; elif [ "$dl" -gt 4 ]; then dl=4; fi
            if [ "$dr" -lt 0 ]; then dr=0; elif [ "$dr" -gt 4 ]; then dr=4; fi
            braille "$dl" "$dr"
            line=$line$BRA
            shift 2
        done
        ex_cup $((3 + row)) 42
        printf '\033[38;5;%dm%s\033[0m' "$col" "$line"
        row=$((row + 1))
    done

    # process table
    sel=$((ft / 6 % 9))
    r=0
    while [ "$r" -lt 9 ]; do
        proc "$r"
        ex_wave $((ft / 2 + r * 3))
        cpu10=$(((BASE + EX_V / 2) * 10 + (ft * 7 + r * 3) % 10 - 5))
        if [ "$cpu10" -lt 0 ]; then cpu10=0; fi
        cpu=$((cpu10 / 10))
        if [ "$cpu" -gt 99 ]; then cpu=99; fi
        if [ "$cpu" -gt 25 ]; then state=running; else state=sleeping; fi
        ex_cup $((14 + r)) 3
        if [ "$r" -eq "$sel" ]; then printf '\033[48;5;237m'; fi
        if [ "$PNAME" = tui-demo ]; then printf '\033[1m'; fi
        printf '%-7s %-13s %2d.%d  ' "$PID" "$PNAME" "$cpu" $((cpu10 % 10))
        ex_bar "$cpu" 20
        if [ "$r" -eq "$sel" ]; then printf '\033[48;5;237m'; fi
        printf '  %2d.%d  %4d  ' "$MEMB" $(((ft + r * 3) / 4 % 10)) "$THR"
        if [ "$state" = running ]; then printf '\033[32m'; else printf '\033[2m'; fi
        printf '%-9s  \033[0m' "$state"
        r=$((r + 1))
    done
}

dashboard_session() {
    dash_chrome
    t=0
    while [ "$t" -lt 76 ]; do
        dash_frame "$t"
        nap 120
        t=$((t + 1))
    done
    nap 500
    # the q key is pressed
    ex_cup 24 2
    printf '\033[1;30;43m q \033[0;7m quit   ↑/↓ select   r refresh    \033[0m'
    nap 450
    ex_alt_off
}

# --------------------------------------------------------------------- the take

DIR_PROJECTS='~/projects'
DIR_DEMO='~/tui-demo'

ex_clear
nap 500

shell_line "$DIR_PROJECTS" 'mkdir tui-demo && cd tui-demo' 900 520
nap 150
shell_line "$DIR_DEMO" 'quill' 1000 430

agent_session
printf '\033[2mquill: session saved, 2 files written\033[0m\n'
nap 300

shell_line "$DIR_DEMO" 'go build' 1200 380
cur_on
nap 1300
printf '\033[K'
shell_line "$DIR_DEMO" 'ls' 800 300
nap 120
printf 'go.mod  main.go  \033[1;32mtui-demo\033[0m\n'
nap 300
shell_line "$DIR_DEMO" './tui-demo' 900 560

dashboard_session

prompt "$DIR_DEMO"
cur_on
hold 2200
ex_finish
printf '\033[K\n'

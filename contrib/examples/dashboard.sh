#!/bin/sh
# shellcheck disable=SC1091,SC2034
# dashboard.sh: a small TUI-like dashboard drawn in the alternate screen: title bar,
# tabs, boxes, gauges, sparklines, a process table with a moving selection and a
# scrolling log. All numbers are simulated and no key is read (use --pause to hold
# the last frame until Enter). See contrib/examples/README.md.
#   sh contrib/examples/dashboard.sh [--force] [--pause]      about 11 s

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='A TUI-like dashboard in the alternate screen (simulated data, no input).'
. "$EX_DIR/lib.sh"
ex_init "$@"

W=$EX_W
H=$EX_H
LW=$((W / 2))
RW=$((W - LW))
BOT=$((H - 11))              # height of the bottom box (rows 11..H-1)
CPUBAR=$((LW - 15))
if [ "$CPUBAR" -gt 40 ]; then CPUBAR=40; fi
PAD=$((W - 2 - 46))          # blanks that fill a table row up to the box border
if [ "$PAD" -lt 0 ]; then PAD=0; fi
ex_rep ' ' "$PAD"
ROWPAD=$EX_REP

# clock T: HH:MM:SS of the simulated clock into CLOCK.
clock() {
    ck=$((7 + $1 * 22 / 100))
    CLOCK=$(printf '14:%02d:%02d' $((3 + ck / 60)) $((ck % 60)))
}

# bar_line ROW COL: a full-width reverse-video bar starting at ROW.
bar_line() {
    ex_rep ' ' "$W"
    ex_cup "$1" 1
    printf '\033[7m%s\033[0m' "$EX_REP"
}

# chrome TAB: title bar, tab strip, footer.
chrome() {
    bar_line 1
    ex_cup 1 2
    printf '\033[1;7mshellshot\033[0;7m  dashboard (simulated data)\033[0m'
    ex_cup 2 2
    j=0
    for name in Overview Network Logs; do
        if [ "$j" -eq "$1" ]; then
            printf '\033[1;7m %d %s \033[0m ' $((j + 1)) "$name"
        else
            printf '\033[2m %d %s \033[0m ' $((j + 1)) "$name"
        fi
        j=$((j + 1))
    done
    bar_line "$H"
    ex_cup "$H" 2
    printf '\033[7mq quit   ↑/↓ select   1-3 tabs        (demo: no keys are read)\033[0m'
}

# frames TAB: the three boxes of a view.
frames() {
    case $1 in
        0) lt=CPU; rt=Memory; bt=Processes ;;
        1) lt=Interfaces; rt=Throughput; bt=Connections ;;
        *) lt=Services; rt=Alerts; bt=Log ;;
    esac
    ex_box 3 1 8 "$LW" "$lt"
    ex_box 3 $((LW + 1)) 8 "$RW" "$rt"
    ex_box 11 1 "$BOT" "$W" "$bt"
}

# spark_row ROW COL WIDTH SHIFT T SCALE: a sparkline.
spark_row() {
    ex_cup "$1" "$2"
    sx=0
    while [ "$sx" -lt "$3" ]; do
        ex_wave $((sx * $6 + $4 + $5))
        ex_spark "$EX_V"
        sx=$((sx + 1))
    done
    printf '\033[0m'
}

# proc N: sets PNAME, PPID and PUSER for table row N.
proc() {
    case $(($1 % 10)) in
        0) PPID_=1203; PUSER=postgres; PNAME='postgres: writer' ;;
        1) PPID_=4411; PUSER=mike; PNAME='shellshot record' ;;
        2) PPID_=892; PUSER=root; PNAME=dockerd ;;
        3) PPID_=3170; PUSER=mike; PNAME=ghostty ;;
        4) PPID_=2201; PUSER=mike; PNAME='node server.js' ;;
        5) PPID_=77; PUSER=root; PNAME=systemd-journald ;;
        6) PPID_=3922; PUSER=mike; PNAME='go test ./...' ;;
        7) PPID_=1530; PUSER=www-data; PNAME='nginx: worker' ;;
        8) PPID_=6012; PUSER=mike; PNAME=zsh ;;
        *) PPID_=1; PUSER=root; PNAME=init ;;
    esac
}

# overview T SEL
overview() {
    i=0
    while [ "$i" -lt 4 ]; do
        ex_wave $(($1 / 2 + i * 5))
        pct=$((12 + EX_V * 10 + ($1 + i * 7) % 9))
        ex_cup $((4 + i)) 3
        printf 'core%d ' "$i"
        ex_bar "$pct" "$CPUBAR"
        printf ' %3d%%' "$pct"
        i=$((i + 1))
    done
    ex_cup 9 3
    printf '\033[2mload\033[0m 0.%02d 0.%02d 0.%02d   \033[2mup\033[0m 12d 3h' $((60 + $1 % 30)) $((70 + $1 % 17)) $((55 + $1 % 11))

    ex_wave $(($1 / 3))
    mem=$((55 + EX_V * 3))
    ex_cup 4 $((LW + 3)); printf 'mem  '; ex_bar "$mem" $((RW - 16)); printf ' %3d%%' "$mem"
    ex_cup 5 $((LW + 3)); printf 'swap '; ex_bar 12 $((RW - 16)); printf '  12%%'
    sparkw=$((RW - 10))
    ex_cup 7 $((LW + 3)); printf '\033[2mcpu\033[0m   '
    spark_row 7 $((LW + 9)) "$sparkw" "$1" 0 1
    ex_cup 8 $((LW + 3)); printf '\033[2mio\033[0m    '
    spark_row 8 $((LW + 9)) "$sparkw" "$1" 5 2
    ex_cup 9 $((LW + 3)); printf '\033[2mlast %ds\033[0m' 60

    ex_cup 12 3
    printf '\033[1;4m%-6s %-9s %5s  %5s  %-22s\033[0m' PID USER CPU MEM COMMAND
    r=0
    while [ "$r" -lt $((BOT - 3)) ]; do
        proc "$r"
        ex_wave $(($1 + r))
        cpu=$((72 - r * 7 + EX_V - (($1 + r) % 3)))
        if [ "$cpu" -lt 1 ]; then cpu=1; fi
        mem=$((3 + r * 37 % 25 + ($1 + r) % 2))
        ex_cup $((13 + r)) 3
        if [ "$r" -eq "$2" ]; then printf '\033[7m'; fi
        printf '%-6s %-9s %4d%% %5d%%  %-22s%s\033[0m' "$PPID_" "$PUSER" "$cpu" "$mem" "$PNAME" "$ROWPAD"
        r=$((r + 1))
    done
}

# network T SEL
network() {
    ex_cup 4 3; printf '\033[1meth0\033[0m   \033[32m↓\033[0m %2d.%d MB/s  \033[33m↑\033[0m %d.%d MB/s' $((8 + $1 % 5)) $(($1 * 3 % 10)) $((1 + $1 % 2)) $(($1 * 7 % 10))
    ex_cup 5 3; printf '\033[1mwlan0\033[0m  \033[32m↓\033[0m  0.%d MB/s  \033[33m↑\033[0m 0.%d MB/s ' $(($1 % 10)) $((($1 + 4) % 10))
    ex_cup 6 3; printf '\033[1mlo\033[0m     \033[32m↓\033[0m  0.0 MB/s  \033[33m↑\033[0m 0.0 MB/s '
    ex_cup 8 3; printf '\033[2mtotal this session\033[0m %d.%d GiB' $((1 + $1 / 30)) $(($1 * 3 % 10))
    sparkw=$((RW - 10))
    ex_cup 4 $((LW + 3)); printf '\033[32mrx\033[0m    '
    spark_row 4 $((LW + 9)) "$sparkw" "$1" 0 1
    ex_cup 6 $((LW + 3)); printf '\033[33mtx\033[0m    '
    spark_row 6 $((LW + 9)) "$sparkw" "$1" 9 2
    ex_cup 8 $((LW + 3)); printf '\033[2mpackets/s\033[0m %d' $((1200 + $1 * 37 % 400))

    ex_cup 12 3
    printf '\033[1;4m%-22s %-22s %-12s\033[0m' LOCAL REMOTE STATE
    r=0
    while [ "$r" -lt $((BOT - 3)) ]; do
        case $((r % 5)) in
            0) local_='10.0.0.5:51234'; remote='140.82.112.3:443'; state=ESTABLISHED ;;
            1) local_='10.0.0.5:51240'; remote='151.101.1.69:443'; state=ESTABLISHED ;;
            2) local_='0.0.0.0:8080'; remote='*:*'; state=LISTEN ;;
            3) local_='10.0.0.5:51302'; remote='93.184.216.34:80'; state=TIME_WAIT ;;
            *) local_='127.0.0.1:5432'; remote='127.0.0.1:49210'; state=ESTABLISHED ;;
        esac
        ex_cup $((13 + r)) 3
        if [ "$r" -eq "$2" ]; then printf '\033[7m'; fi
        printf '%-22s %-22s %-12s%s\033[0m' "$local_" "$remote" "$state" "$ROWPAD"
        r=$((r + 1))
    done
}

# logs T
logs() {
    svc=0
    for name in api-gateway postgres redis worker-pool scheduler; do
        ex_cup $((4 + svc)) 3
        if [ "$svc" -eq 3 ] && [ $(($1 / 4 % 2)) -eq 1 ]; then
            printf '\033[33m●\033[0m %-14s degraded' "$name"
        else
            printf '\033[32m●\033[0m %-14s running ' "$name"
        fi
        svc=$((svc + 1))
    done
    ex_cup 4 $((LW + 3)); printf '\033[1;31m!\033[0m disk /var at 91%%'
    ex_cup 5 $((LW + 3)); printf '\033[1;33m!\033[0m queue depth rising'
    ex_cup 6 $((LW + 3)); printf '\033[2m  no other alerts\033[0m'
    r=0
    while [ "$r" -lt $((BOT - 2)) ]; do
        k=$(($1 + r))
        clock $((k * 2))
        case $((k % 8)) in
            0) lv='1;32'; ln=INFO; msg='GET /api/v1/frames 200 4ms' ;;
            1) lv='1;32'; ln=INFO; msg='GET /health 200 1ms' ;;
            2) lv='1;33'; ln=WARN; msg='slow query 412ms (users_by_name)' ;;
            3) lv='1;32'; ln=INFO; msg='POST /api/v1/record 201 18ms' ;;
            4) lv='2'; ln=DEBUG; msg='cache hit ratio 0.93' ;;
            5) lv='1;31'; ln=ERROR; msg='upstream timeout after 3000ms, retrying' ;;
            6) lv='1;32'; ln=INFO; msg='worker 3 restarted' ;;
            *) lv='1;32'; ln=INFO; msg='GET /metrics 200 2ms' ;;
        esac
        ex_cup $((12 + r)) 3
        printf '\033[2m%s\033[0m \033[%sm%-5s\033[0m %-42s%s' "$CLOCK" "$lv" "$ln" "$msg" "$ROWPAD"
        r=$((r + 1))
    done
}

ex_alt_on
tab=-1
t=0
frames_total=50
while [ "$t" -lt "$frames_total" ]; do
    if [ "$t" -lt 22 ]; then now=0; elif [ "$t" -lt 36 ]; then now=1; else now=2; fi
    if [ "$now" -ne "$tab" ]; then
        tab=$now
        ex_clear
        chrome "$tab"
        frames "$tab"
    fi
    clock "$t"
    ex_cup 1 $((W - 9))
    printf '\033[7m%s\033[0m' "$CLOCK"
    sel=$((t / 3 % (BOT - 3)))
    case $tab in
        0) overview "$t" "$sel" ;;
        1) network "$t" "$sel" ;;
        *) logs "$t" ;;
    esac
    nap 150
    t=$((t + 1))
done
nap 600
if [ "$EX_PAUSE" -eq 1 ]; then
    ex_cup "$H" 2
    printf '\033[7mpress Enter to quit                                      \033[0m'
fi
ex_finish
ex_alt_off
printf '\033[1mdashboard closed.\033[0m The main screen is back exactly as it was.\n'
nap 1000

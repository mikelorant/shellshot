#!/bin/sh
# shellcheck disable=SC1091,SC2034
# buildlog.sh: a realistic but entirely fake build log: timestamps, coloured
# levels, compile steps, a warning, a flaky test that is retried, and a summary.
# Nothing is built or run; the clock is simulated. See examples/README.md.
#   sh examples/buildlog.sh [--force] [--pause]      about 9 s

case $0 in */*) EX_DIR=${0%/*} ;; *) EX_DIR=. ;; esac
EX_NAME=${0##*/}
EX_DESC='A fake build log with timestamps and coloured levels.'
. "$EX_DIR/lib.sh"
ex_init "$@"

clock=$((14 * 3600000 + 3 * 60000 + 7 * 1000 + 120))   # simulated 14:03:07.120
seed=42
warnings=0
retries=0

# rand N: sets RAND to a pseudo-random number in 0..N-1 (fixed seed, so every run is the same).
rand() {
    seed=$(((seed * 1664525 + 1013904223) % 4294967296))
    RAND=$((seed / 65536 % $1))
}

# log LEVEL COMPONENT MESSAGE ELAPSED_MS: advance the fake clock, then print one line.
log() {
    nap "$4"
    clock=$((clock + $4))
    ms=$((clock % 1000))
    secs=$((clock / 1000))
    case $1 in
        DEBUG) lc='2' ;;
        INFO) lc='1;32' ;;
        WARN) lc='1;33' ;;
        ERROR) lc='1;97;41' ;;
        *) lc='1;30;42' ;;
    esac
    printf '\033[2m%02d:%02d:%02d.%03d\033[0m \033[%sm%-5s\033[0m \033[36m%-9s\033[0m %s\n' \
        $((secs / 3600 % 24)) $((secs / 60 % 60)) $((secs % 60)) "$ms" "$lc" "$1" "$2" "$3"
}

log INFO build 'shellshot 0.4.2 (release, linux/amd64)' 120
log DEBUG toolchain 'go1.23.1 cc 14.2.0 ld 2.43 jobs=8' 90
log DEBUG cache 'restored 212 entries (84 MiB) from remote cache' 260
log INFO deps 'resolving 9 modules' 180
log INFO deps 'github.com/spf13/cobra v1.8.1 (cached)' 60
log INFO deps 'github.com/klauspost/compress v1.17.9 (cached)' 50
log INFO deps 'github.com/kettek/apng v0.0.0-20230220 (cached)' 50
log INFO gen 'generating font atlas tables' 340
log WARN gen 'no glyph for U+1F980 in DejaVu Sans Mono' 140
warnings=$((warnings + 1))

total=10
n=0
for pkg in internal/tty internal/pty internal/vt internal/recording internal/replay \
    internal/render internal/export internal/script internal/cli cmd/shellshot; do
    n=$((n + 1))
    rand 220
    log INFO compile "[$(printf '%2d' "$n")/$total] $pkg" $((90 + RAND))
    if [ "$pkg" = internal/render ]; then
        log WARN lint 'render/atlas.go:88: unused parameter "scale"' 40
        warnings=$((warnings + 1))
    fi
done

log INFO link 'bin/shellshot (12.4 MiB, stripped)' 420
log INFO test 'running 6 test packages in parallel' 150

for pkg in internal/vt internal/script internal/recording internal/export; do
    rand 300
    log INFO test "ok    $pkg ($(printf '%d.%02d' $((RAND / 100)) $((RAND % 100)))s)" $((200 + RAND))
done

log ERROR test 'FAIL  pty TestResizeRace: timeout (try 1/2)' 650
log WARN test 'flaky test detected, retrying internal/pty' 200
retries=$((retries + 1))
log INFO test 'ok    internal/pty (0.84s) on retry' 450
log INFO test 'ok    internal/replay (0.31s)' 240
log INFO cover 'coverage 81.4% of statements (threshold 75%)' 180
log DEBUG cache 'stored 14 entries (6 MiB) to remote cache' 160
log INFO package 'bin/shellshot-0.4.2-linux-amd64.tar.gz (4.9 MiB)' 320
log DEBUG package 'sha256 3f9c1d0a52e1b7f4c8d3aa94b21e6c07de5f1830' 60

secs=$(((clock - (14 * 3600000 + 3 * 60000 + 7 * 1000 + 120)) / 1000))
tenths=$(((clock - (14 * 3600000 + 3 * 60000 + 7 * 1000 + 120)) / 100 % 10))
log OK build "BUILD OK in ${secs}.${tenths}s: $total pkgs, $warnings warnings, $retries retry" 100
nap 1500
ex_finish

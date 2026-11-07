#!/bin/sh
# Runs every examples/*.sh under several shells on a pty (via script(1)) with
# EXAMPLES_SPEED=fast and checks: exit 0, output ends with the terminal reset
# (cursor shown), SIGINT restores the terminal, and non-tty stdout is refused
# unless --force. Usage: scripts/test-examples.sh [shell...]   (run from the repo root)
# Only writes to a private mktemp directory that it removes at the end.

cd "$(dirname "$0")/.." || exit 2

if [ "$#" -gt 0 ]; then
    SHELLS="$*"
else
    SHELLS="dash /bin/bash sh zsh"
fi

TMP=$(mktemp -d) || exit 2
trap 'rm -rf "$TMP"' EXIT
trap 'exit 130' INT TERM
FAILS=0
RUNS=0

fail() {
    FAILS=$((FAILS + 1))
    printf 'FAIL %s\n' "$1"
}

# run_pty OUTFILE CMD...: runs CMD on a pty, 40 s timeout, stdin from /dev/null.
run_pty() {
    out=$1
    shift
    EXAMPLES_SPEED=fast perl -e 'alarm 40; exec @ARGV' script -q /dev/null "$@" </dev/null >"$out" 2>&1
}

# ends_reset FILE: the last bytes must show the cursor again and reset attributes.
ends_reset() {
    tail -c 40 "$1" | LC_ALL=C tr -d '\r' | od -An -c | LC_ALL=C tr -d ' \n' | grep -Eq '033\[\?25h033\[0m(\\n)?$'
}

# shell_cmd SHELL: prints the command line for the shell (zsh runs in sh emulation).
shell_cmd() {
    case $1 in
        zsh) printf 'zsh --emulate sh' ;;
        *) printf '%s' "$1" ;;
    esac
}

for sh_name in $SHELLS; do
    if ! command -v "${sh_name%% *}" >/dev/null 2>&1; then
        printf 'skip %s (not installed)\n' "$sh_name"
        continue
    fi
    cmd=$(shell_cmd "$sh_name")
    for ex in examples/*.sh; do
        case $ex in */lib.sh) continue ;; esac
        RUNS=$((RUNS + 1))
        label="$sh_name $ex"
        # shellcheck disable=SC2086
        run_pty "$TMP/out" $cmd "$ex"
        rc=$?
        if [ "$rc" -ne 0 ]; then
            fail "$label: exit $rc"
        elif ! ends_reset "$TMP/out"; then
            fail "$label: output does not end with the terminal reset"
        else
            printf 'ok   %s\n' "$label"
        fi
    done
done

# SIGINT in the middle of a run: exit 130 and the terminal is restored.
for ex in examples/dashboard.sh examples/animations.sh; do
    RUNS=$((RUNS + 1))
    EXAMPLES_SPEED=normal perl -e 'alarm 40; exec @ARGV' script -q /dev/null \
        perl -e '$p = fork; if (!$p) { exec @ARGV } select(undef, undef, undef, 2); kill "INT", $p; waitpid($p, 0); exit($? >> 8)' \
        sh "$ex" </dev/null >"$TMP/out" 2>&1
    rc=$?
    if [ "$rc" -ne 130 ]; then
        fail "SIGINT $ex: exit $rc (want 130)"
    elif ! ends_reset "$TMP/out"; then
        fail "SIGINT $ex: terminal not restored"
    else
        printf 'ok   SIGINT %s\n' "$ex"
    fi
done

# Non-tty stdout: refuse with exactly one line, unless --force.
for ex in examples/*.sh; do
    case $ex in */lib.sh) continue ;; esac
    RUNS=$((RUNS + 1))
    sh "$ex" >"$TMP/out" 2>"$TMP/err" </dev/null
    rc=$?
    lines=$(wc -l <"$TMP/err" | tr -d ' ')
    if [ "$rc" -eq 0 ] || [ "$lines" -ne 1 ] || [ -s "$TMP/out" ]; then
        fail "non-tty $ex: rc=$rc stderr-lines=$lines (want non-zero, 1, empty stdout)"
    else
        if ! EXAMPLES_SPEED=fast sh "$ex" --force >"$TMP/out" 2>&1 </dev/null || [ ! -s "$TMP/out" ]; then
            fail "--force $ex: did not print"
        else
            printf 'ok   non-tty %s\n' "$ex"
        fi
    fi
done

printf '%d checks, %d failed\n' "$RUNS" "$FAILS"
[ "$FAILS" -eq 0 ]

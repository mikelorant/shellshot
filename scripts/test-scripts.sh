#!/bin/sh
# Runs every examples/scripts/*.script through "record --script --list" and then
# for real, on its own pty, with a generous timeout. Checks: --list exits 0 and
# writes nothing, the run exits 0, and the recording is complete.
# Usage: scripts/test-scripts.sh [script...]   (run from the repo root, after make build)
# Only writes to a private mktemp directory that it removes at the end.

cd "$(dirname "$0")/.." || exit 2

ROOT=$PWD
BIN=${SHELLSHOT:-$ROOT/bin/shellshot}
LIMIT=${SCRIPT_TIMEOUT:-240}
if [ "$#" -gt 0 ]; then
    SCRIPTS="$*"
else
    SCRIPTS=$(ls examples/scripts/*.script)
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

for s in $SCRIPTS; do
    name=${s##*/}
    name=${name%.script}
    RUNS=$((RUNS + 1))
    printf '== %s\n' "$name"

    # --list must not create anything: run it in an empty directory.
    mkdir "$TMP/list-$name" || exit 2
    (cd "$TMP/list-$name" && "$BIN" record --script "$ROOT/$s" --list >"$TMP/$name.list" 2>&1)
    rc=$?
    if [ "$rc" -ne 0 ]; then
        fail "$name --list (exit $rc)"
        sed 's/^/    /' "$TMP/$name.list"
        continue
    fi
    if [ -n "$(ls -A "$TMP/list-$name")" ]; then
        fail "$name --list created files"
    fi

    perl -e 'alarm shift; exec @ARGV' "$LIMIT" "$BIN" record --script "$s" -o "$TMP/$name.shot" >"$TMP/$name.out" 2>&1 </dev/null
    rc=$?
    if [ "$rc" -ne 0 ]; then
        fail "$name run (exit $rc)"
        tail -n 20 "$TMP/$name.out" | sed 's/^/    /'
        continue
    fi
    if ! "$BIN" info "$TMP/$name.shot" | grep -q '^status *complete$'; then
        fail "$name recording is not complete"
    fi
done

printf '%d scripts, %d failures\n' "$RUNS" "$FAILS"
[ "$FAILS" -eq 0 ]

#!/bin/sh
# Renders every contrib/examples demo for real and checks what comes out. Needs bin/shellshot
# (make build), libghostty and uv; takes a few minutes. Run through `make check-examples`.
#
#   scripts/check-examples-render.sh [demo ...]     (default: every demo)
#
# Each demo is recorded at 80x24 with EXAMPLES_SPEED=fast, then scripts/check-examples-render.py
#   (a) follows the cursor through the raw output and reports every printable character written
#       at column == width (a real auto-wrap: the line breaks and overwrites what is below), with
#       the byte offset, screen row and column and the text just written;
#   (b) lists every non-ASCII character the demos print and flags the ones no embedded font
#       has, which render as blanks (best effort: it knows the fonts, not the renderer).
# Exits 1 on findings, 2 when the check could not run.
set -eu

cd "$(dirname "$0")/.."

[ -x bin/shellshot ] || { echo "bin/shellshot not found: run make build" >&2; exit 2; }

if [ "$#" -gt 0 ]; then
    demos=$*
else
    demos=
    for f in contrib/examples/*.sh; do
        n=${f##*/}
        n=${n%.sh}
        [ "$n" = lib ] && continue
        demos="$demos $n"
    done
fi

HELPER=scripts/check-examples-render.py
if command -v uv >/dev/null 2>&1; then
    # The pins match `make emoji-asset`.
    py() { PYTHONDONTWRITEBYTECODE=1 uv run --no-project --python 3.12 --with fonttools==4.68.0 --with zstandard==0.25.0 python "$@"; }
elif command -v python3 >/dev/null 2>&1; then
    echo "uv not found: using python3; the font check needs fontTools and zstandard" >&2
    py() { python3 "$@"; }
else
    echo "neither uv nor python3 found" >&2
    exit 2
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

py "$HELPER" --selftest

args=
for n in $demos; do
    [ -f "contrib/examples/$n.sh" ] || { echo "no such demo: contrib/examples/$n.sh" >&2; exit 2; }
    echo "recording $n" >&2
    EXAMPLES_SPEED=fast ./bin/shellshot record -o "$tmp/$n.shot" --cols 80 --rows 24 --codec none \
        -c "sh ./contrib/examples/$n.sh" >/dev/null
    args="$args $n=$tmp/$n.shot"
done

status=0
# shellcheck disable=SC2086
py "$HELPER" --cols 80 --rows 24 $args || status=$?
exit "$status"

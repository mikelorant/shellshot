#!/bin/sh
# Records contrib/examples/showcase.sh and exports it to docs/assets/showcase.png, the README
# image. On demand only: see "Regenerating the README image" in docs/development.md.
# Run through `make showcase` (needs bin/shellshot).
set -eu

cd "$(dirname "$0")/.."

SHOT=build/showcase.shot
PNG=docs/assets/showcase.png

# Every look and timing flag is pinned, so a changed default cannot alter the image unnoticed.
# The 35 s demo plays at 1.25x (about 28 s) and pauses are capped at 1 s.
EXPORT_FLAGS='--scale 2 --emoji-set noto --theme dark --font-size 15 --padding 8 --cursor block --fps 60 --speed 1.25 --idle-limit 1s --hold 2s --min-delay 20ms'

[ $# -eq 0 ] || { echo "usage: $0" >&2; exit 2; }
[ -x bin/shellshot ] || { echo "bin/shellshot not found: run make build" >&2; exit 1; }

mkdir -p build
EXAMPLES_SPEED=normal ./bin/shellshot record -o "$SHOT" --cols 80 --rows 24 \
    -c 'sh ./contrib/examples/showcase.sh' >/dev/null
# EXPORT_FLAGS is a list of words on purpose.
# shellcheck disable=SC2086
./bin/shellshot export "$SHOT" -o "$PNG" $EXPORT_FLAGS

#!/bin/sh
# Records contrib/examples/agent.sh and exports it to docs/assets/showcase.png, the README
# image, then writes docs/assets/showcase.png.src. With --check, recomputes that hash and fails
# when the image is stale. On demand only: see "Regenerating the README image" in
# docs/development.md. Run through `make showcase` / `make check-showcase` (needs bin/shellshot).
set -eu

cd "$(dirname "$0")/.."

SHOT=build/showcase.shot
PNG=docs/assets/showcase.png
SRC=$PNG.src

RECORD_SPEC='80x24 EXAMPLES_SPEED=normal'
# Every look and timing flag is pinned, so a changed default cannot alter the image unnoticed.
# The 56 s demo plays at normal speed (about 57 s with the 2 s hold) and pauses are capped at 1 s.
# --poster is the frame viewers without animation support show (Preview, Quick Look, chat previews).
# The demo ends on a bare shell prompt, which says little, so the poster is a frame of the
# dashboard instead: 46 s on the output clock, a few seconds into the dashboard run (it plays from
# about 39 s to 51 s). All panels are filled, the history graph shows a full wave and a process row
# is selected. Re-pick it with `shellshot export --poster Ns` if the demo's timing changes.
EXPORT_FLAGS='--scale 2 --emoji-set noto --theme dark --font-size 15 --padding 8 --cursor block --fps 60 --speed 1 --idle-limit 1s --hold 2s --min-delay 20ms --poster 46s'

if command -v sha256sum >/dev/null 2>&1; then
    sha256() { sha256sum | cut -d' ' -f1; }
else
    sha256() { shasum -a 256 | cut -d' ' -f1; }
fi

# The hash covers the demo, its helpers and the settings that turn it into the image.
inputs_hash() {
    {
        cat contrib/examples/agent.sh contrib/examples/lib.sh
        printf 'record: %s\nexport: %s\n' "$RECORD_SPEC" "$EXPORT_FLAGS"
    } | sha256
}

if [ "${1:-}" = --check ]; then
    if [ ! -f "$PNG" ] || [ ! -f "$SRC" ] || [ "$(cat "$SRC")" != "$(inputs_hash)" ]; then
        echo "$PNG is stale: run make showcase" >&2
        exit 1
    fi
    echo "$PNG is up to date"
    exit 0
fi

[ $# -eq 0 ] || { echo "usage: $0 [--check]" >&2; exit 2; }
[ -x bin/shellshot ] || { echo "bin/shellshot not found: run make build" >&2; exit 1; }

mkdir -p build
EXAMPLES_SPEED=normal ./bin/shellshot record -o "$SHOT" --cols 80 --rows 24 \
    -c 'sh ./contrib/examples/agent.sh' >/dev/null
# EXPORT_FLAGS is a list of words on purpose.
# shellcheck disable=SC2086
./bin/shellshot export "$SHOT" -o "$PNG" $EXPORT_FLAGS
inputs_hash >"$SRC"
echo "$SRC: $(cat "$SRC")"

#!/usr/bin/env bash
# Build libghostty-vt (static + headers + pkg-config files) from the pinned
# ghostty git submodule using only zig. Output prefix: build/libghostty.
# Requires zig 0.17.x (see third_party/ghostty/build.zig.zon minimum_zig_version).
# Idempotent: reruns reuse the zig cache and only rebuild what changed.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
src="$root/third_party/ghostty"
prefix="$root/build/libghostty"

command -v zig >/dev/null 2>&1 || { echo "zig not found (brew install zig)" >&2; exit 1; }

git -C "$root" submodule update --init --depth 1 third_party/ghostty

echo "building libghostty-vt with zig $(zig version) into $prefix"
(cd "$src" && zig build -Demit-lib-vt -Doptimize=ReleaseFast --prefix "$prefix")

pc="$prefix/lib/pkgconfig/libghostty-vt-static.pc"
[ -f "$prefix/lib/libghostty-vt.a" ] || { echo "missing $prefix/lib/libghostty-vt.a" >&2; exit 1; }
[ -f "$pc" ] || { echo "missing $pc" >&2; exit 1; }

PKG_CONFIG_PATH="$prefix/lib/pkgconfig" pkg-config --cflags --libs --static libghostty-vt-static
echo "installed libghostty-vt into $prefix"

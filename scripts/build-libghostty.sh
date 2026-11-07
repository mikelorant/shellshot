#!/usr/bin/env bash
# Build libghostty-vt (static + headers + pkg-config files) from the pinned
# ghostty git submodule using only zig. Output prefix: build/libghostty.
# Requires zig 0.17.x (see third_party/ghostty/build.zig.zon minimum_zig_version).
# Idempotent: reruns reuse the zig cache and only rebuild what changed.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
src="$root/third_party/ghostty"
target="${1:-}"

prefix="$root/build/libghostty"
target_args=()
if [ -n "$target" ]; then
  triple="${target%%.*}"
  case "$triple" in
    x86_64-*)  arch=amd64 ;;
    aarch64-*) arch=arm64 ;;
    *) echo "unsupported target architecture: $target" >&2; exit 1 ;;
  esac
  case "$triple" in
    *-linux*)  os=linux ;;
    *-macos*)  os=darwin ;;
    *) echo "unsupported target os: $target" >&2; exit 1 ;;
  esac
  prefix="$root/build/libghostty-$os-$arch"
  target_args=("-Dtarget=$target")
fi

command -v zig >/dev/null 2>&1 || { echo "zig not found (brew install zig)" >&2; exit 1; }

git -C "$root" submodule update --init --depth 1 third_party/ghostty

echo "building libghostty-vt with zig $(zig version) into $prefix"
(cd "$src" && zig build -Demit-lib-vt -Doptimize=ReleaseFast ${target_args[@]+"${target_args[@]}"} --prefix "$prefix")

pc="$prefix/lib/pkgconfig/libghostty-vt-static.pc"
[ -f "$prefix/lib/libghostty-vt.a" ] || { echo "missing $prefix/lib/libghostty-vt.a" >&2; exit 1; }
[ -f "$pc" ] || { echo "missing $pc" >&2; exit 1; }

PKG_CONFIG_PATH="$prefix/lib/pkgconfig" pkg-config --cflags --libs --static libghostty-vt-static
echo "installed libghostty-vt into $prefix"

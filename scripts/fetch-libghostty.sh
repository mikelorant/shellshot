#!/usr/bin/env bash
# Download the prebuilt libghostty-vt xcframework release asset and install it
# under third_party/libghostty. Never builds ghostty or zig from source.
# Env: LIBGHOSTTY_TAG (default: tip) selects the ghostty-org/ghostty release tag.
set -euo pipefail

REPO="ghostty-org/ghostty"
ASSET="ghostty-vt.xcframework.zip"
TAG="${LIBGHOSTTY_TAG:-tip}"
SLICE="macos-arm64_x86_64"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
dest="$root/third_party/libghostty"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT

echo "fetching $ASSET from $REPO release '$TAG'"
if command -v gh >/dev/null 2>&1; then
  gh release download "$TAG" --repo "$REPO" --pattern "$ASSET" --dir "$work" --clobber
else
  curl -fsSL -o "$work/$ASSET" \
    "https://github.com/$REPO/releases/download/$TAG/$ASSET"
fi

if command -v shasum >/dev/null 2>&1; then
  sha="$(shasum -a 256 "$work/$ASSET" | awk '{print $1}')"
else
  sha="$(sha256sum "$work/$ASSET" | awk '{print $1}')"
fi

unzip -q -o "$work/$ASSET" -d "$work/x"
xc="$work/x/ghostty-vt.xcframework/$SLICE"
[ -f "$xc/libghostty-vt.a" ] || { echo "missing $SLICE/libghostty-vt.a in archive" >&2; exit 1; }
[ -d "$xc/Headers" ] || { echo "missing $SLICE/Headers in archive" >&2; exit 1; }

# Replace previous install so reruns are idempotent and stale headers vanish.
rm -rf "$dest/lib" "$dest/include"
mkdir -p "$dest/lib/pkgconfig" "$dest/include"
cp "$xc/libghostty-vt.a" "$dest/lib/libghostty-vt.a"
cp -R "$xc/Headers/." "$dest/include/"

cat > "$dest/lib/pkgconfig/libghostty-vt-static.pc" <<PC
prefix=$dest
libdir=\${prefix}/lib
includedir=\${prefix}/include

Name: libghostty-vt-static
Description: Ghostty virtual terminal library (static)
Version: $TAG
Libs: -L\${libdir} -lghostty-vt
Cflags: -I\${includedir} -DGHOSTTY_STATIC
PC

cat > "$dest/VERSION" <<V
tag=$TAG
asset=$ASSET
sha256=$sha
V

echo "sha256 $sha"
echo "installed libghostty-vt into $dest"

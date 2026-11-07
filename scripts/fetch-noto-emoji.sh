#!/usr/bin/env bash
# Fetches Noto Color Emoji (CBDT/CBLC bitmap font, SIL OFL 1.1) pinned to one
# commit of https://github.com/googlefonts/noto-emoji, verifies its sha256 and
# writes the embedded assets:
#   internal/render/emoji/assets/noto-emoji/NotoColorEmoji.ttf.zst   (zstd -19 of the font)
#   internal/render/emoji/assets/noto-emoji/NotoColorEmoji-OFL.txt   (2D/fonts/LICENSE)
# The upstream repo is ~1.5 GB, so only these two files are downloaded.
# Idempotent: when the committed files already match the pins below, nothing is
# downloaded or rewritten. Exits non-zero on any checksum mismatch. Needs curl,
# zstd and shasum or sha256sum.
#
# To bump the pin:
#   1. Pick a commit of googlefonts/noto-emoji that has a newer
#      2D/fonts/NotoColorEmoji.ttf and set COMMIT.
#   2. Set FONT_SHA256 and LICENSE_SHA256 to "-" and run `make noto-asset`; each
#      mismatch message prints the actual hash (also update FONT_SIZE).
#   3. Put the new hashes below and re-run `make noto-asset`.
#   4. Update the Noto Color Emoji entry in
#      internal/render/emoji/assets/noto-emoji/NOTICE.txt (commit, sha256, sizes, Unicode
#      version) and check the emoji coverage tests still pass.
set -euo pipefail

COMMIT=99701e07d3d1fe1bdbcdb14964547f0fd592966f
FONT_SHA256=15671215ab769fdc7162a045d56fd7d7e477c51b04e6b3c761d914d8fdd6cc44
FONT_SIZE=10730124
LICENSE_SHA256=6a73f9541c2de74158c0e7cf6b0a58ef774f5a780bf191f2d7ec9cc53efe2bf2

raw=https://raw.githubusercontent.com/googlefonts/noto-emoji/$COMMIT/2D/fonts
lfs=https://media.githubusercontent.com/media/googlefonts/noto-emoji/$COMMIT/2D/fonts

cd "$(dirname "$0")/.."
dest=internal/render/emoji/assets/noto-emoji
font_zst=$dest/NotoColorEmoji.ttf.zst
license=$dest/NotoColorEmoji-OFL.txt

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum | cut -d' ' -f1
	else
		shasum -a 256 | cut -d' ' -f1
	fi
}

for tool in curl zstd; do
	command -v "$tool" >/dev/null 2>&1 || { echo "fetch-noto-emoji: $tool is required" >&2; exit 1; }
done

if [[ -f $font_zst && -f $license ]] &&
	[[ $(zstd -dc "$font_zst" 2>/dev/null | sha256) == "$FONT_SHA256" ]] &&
	[[ $(sha256 <"$license") == "$LICENSE_SHA256" ]]; then
	echo "Noto Color Emoji at $COMMIT is up to date"
	exit 0
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

# fetch NAME: downloads 2D/fonts/NAME, following the Git LFS media URL if the
# raw URL returns an LFS pointer file instead of the content.
fetch() {
	curl -fsSL --retry 3 -o "$tmp/$1" "$raw/$1"
	if head -c 64 "$tmp/$1" | grep -q '^version https://git-lfs'; then
		curl -fsSL --retry 3 -o "$tmp/$1" "$lfs/$1"
	fi
}

verify() { # verify FILE EXPECTED_SHA256
	local got
	got=$(sha256 <"$1")
	if [[ $got != "$2" ]]; then
		echo "fetch-noto-emoji: $(basename "$1") sha256 mismatch: got $got, want $2" >&2
		exit 1
	fi
}

fetch NotoColorEmoji.ttf
fetch LICENSE
verify "$tmp/NotoColorEmoji.ttf" "$FONT_SHA256"
verify "$tmp/LICENSE" "$LICENSE_SHA256"
if [[ $(wc -c <"$tmp/NotoColorEmoji.ttf" | tr -d ' ') != "$FONT_SIZE" ]]; then
	echo "fetch-noto-emoji: unexpected NotoColorEmoji.ttf size" >&2
	exit 1
fi

mkdir -p "$dest"
zstd -19 -q -f -o "$tmp/NotoColorEmoji.ttf.zst" "$tmp/NotoColorEmoji.ttf"
[[ $(zstd -dc "$tmp/NotoColorEmoji.ttf.zst" | sha256) == "$FONT_SHA256" ]] || { echo "fetch-noto-emoji: zstd round trip failed" >&2; exit 1; }
cp "$tmp/NotoColorEmoji.ttf.zst" "$font_zst"
cp "$tmp/LICENSE" "$license"
echo "Wrote $font_zst and $license (font sha256 $FONT_SHA256)"

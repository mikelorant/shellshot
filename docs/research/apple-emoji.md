# Apple-style colour emoji for `shellshot export`

Status: investigated and prototyped, not implemented. Measured on
`samuelngs/apple-emoji-ttf` commit `484daf4e13942437e083d881cae39fbc92d837e1`
(branch `v2`; renamed from `apple-emoji-linux`), Go 1.27, x/image v0.47.0.
Builds on `docs/research/emoji-fallback.md` (branch `task/emoji-fallback`, not
on main) and its `internal/render/emoji` package (`gsub.go`, `Font.Image`).

## 1. What the repo is (and is not)

* A **Python conversion pipeline** (fonttools, PyYAML, Pillow, uharfbuzz;
  ~1.8 MB shallow checkout). Input is **Apple's own
  `Apple Color Emoji.ttc`** (sbix; default path
  `/System/Library/Fonts/Apple Color Emoji.ttc`); it contains **no font data**
  ("We don't provide the font; you can get it from macOS").
  `LICENSE` is MIT, copyright "2026 samuelngs", and covers the scripts only.
* Its CI (`build-os-fonts.yml`, macOS runner) converts Apple's file and
  **publishes the converted fonts on GitHub Releases**. So a prebuilt font
  exists, but it is a redistribution by a third party. Latest release
  `macos-26-20260722-484daf4e` (2026-07-22): `AppleColorEmoji-Linux.ttf`
  115,969,256 B, sha256 `e37c7af6265ac4a0af6d57bc65e86109a776d9966e8343334557f63da482516f`
  (verified; 22,655 downloads); `AppleColorEmoji-Windows.ttf` 256,391,076 B;
  `.deb`/`.rpm`/`.pkg.tar.zst` ~110-112 MB each.
* Full history is 324 MB (a `--depth 1` clone: 552 KB `.git`).
* **Plainly: the repo alone cannot produce or ship the font.** To build one the
  user needs Apple's TTC (192,120,460 B on this Mac, `/System/Library/Fonts/Apple
  Color Emoji.ttc`, version `22.0d3e2`, macOS 27.2). Release assets are the only
  font without a Mac, and unusable for us as is (section 2).

## 2. Font format

Release `AppleColorEmoji-Linux.ttf` (version 21.4d3e1): `CBDT` 115,240,176 B +
`CBLC`, 8 strikes (20-96 ppem), 3,844 glyphs, upem 2048, **no `GSUB`**:
sequences live in Apple's AAT `morx` table (475,632 B), which pure Go would
need a state-machine reader for. The pipeline's `web` recipe instead
*replaces* `morx` with a generated OpenType `GSUB` (HarfBuzz as oracle, so it
matches Apple's shaping): that is what we want, and only a local build makes it.

Our build (`web.yaml` minus font splitting, one 96 ppem strike):

* **CBLC/CBDT**, one strike, ppem 96, bitDepth 32, 33 index subtables of
  **index format 1 / image format 17** (SmallGlyphMetrics 96x96, bearingX 0,
  bearingY 80, advance 96; then `u32 length` + PNG). 6,241 bitmaps of 96x96
  RGBA, 6,397 glyphs (3,688 source + 2,553 sequence glyphs). upem 800.
* **GSUB**: one `ccmp` feature, one type-4 lookup, **3,989 ligature rules**
  (ZWJ, flags, tones, keycaps, FE0F forms). The existing `parseLigatures` from
  the Twemoji branch reads it **unchanged**. `cmap` 1,469 entries (fmt 12).
  `sfnt.Parse` accepts the font (no `glyf`), so `GlyphIndex` works; only the
  bitmap tables are ours.
* Apple's sbix strikes: 20, 26, 32, 40, 48, 52, 64, 96, **160** (79.8 MB of
  PNG; the recipe validator rejects ppem > 127, so 160 is unused). **Strike 48
  is incomplete** (6,109 of 6,241 glyphs, U+1FAE8 blank); 32/64/96 are complete.
* **Coverage** vs `emoji-test.txt` 18.0 (3,963 fully-qualified sequences):
  **3,944 (99.5%)**, all by exact cmap+GSUB match (no fallback to first rune).
  The 19 misses are all E18.0 (cracking face, thumb signs, ...). Twemoji: 91.4%
  (Unicode 14).
* Output is **not byte-reproducible** (two runs: same size, different sha256);
  the art also changes with each macOS release.

## 3. Size

Whole-font bytes from the pipeline, one strike, GSUB, `png.compress` with
128 colours (Pillow quantiser) unless noted. Binary today: 17,405,634 B.

| variant | bytes | comment |
|---|---|---|
| Release Linux TTF (8 strikes + morx) | 115,969,256 | unusable (no GSUB) |
| **96 ppem, 128 colours** | **17,144,708** | recommended; gzip -9 15.78 MB, zstd -19 13.32 MB |
| 96 ppem, pngquant 128 / 256 colours / 64 colours | 20,091,552 / 20,994,292 / 13,034,252 | |
| 64 ppem, 128 colours | 11,615,132 | scale <= 2 on a 14pt font |
| 48 ppem, 128 colours | 8,950,324 | strike has gaps |
| 32 ppem, 128 colours | 6,868,472 | blurry above scale 1 |
| RGI-only glyphs, bitmap bytes at 96 / 64 / 32 ppem | 10.80 / 7.23 / 4.22 MB | 3,944 of 6,241 glyphs; computed, not built |

Compression barely helps (PNGs are already deflated: gzip -8%, zstd -22%).
Embedding 96 ppem would take the binary from 17.4 MB to ~34.5 MB (+98%), on
top of Twemoji (+1.5) and CJK (+1.1); even 64 ppem RGI-only is ~+7.5 MB.
Pillow vs pngquant at 96 px: no visible difference. Build 4 s (11 s cold
`uv run`); load + parse of the 17 MB file ~7 ms, PNGs decoded lazily.

## 4. Licence notes (not legal advice)

* **Repo**: MIT for the scripts. README disclaimer (verbatim core): "for
  educational purposes only. All Apple Color Emoji assets and designs belong to
  Apple Inc. ... Using a font from a different operating system may have
  licensing implications; that responsibility is on you." The MIT licence does
  **not** cover the font; nothing in the repo grants rights to Apple's art.
* **Apple**: the macOS Software License Agreement ("For use on Apple-branded
  Systems", Sequoia text read) licenses fonts as part of the Apple Software:
  "you may use the fonts included with the Apple Software to display and print
  content while running the Apple Software; however, you may only embed fonts in
  content if that is permitted by the embedding restrictions accompanying the
  font", and "you may not rent, lease, lend, sell, **redistribute** or
  sublicense the Apple Software". Apple Color Emoji has `OS/2 fsType = 4`
  (preview/print only); upstream's `web` recipe sets it to 0 (Installable),
  which we must **not** copy (ours omits `fs_type`; verified it stays 4).
* **Consequences**: converting your own copy for your own use on your Mac is
  the least risky reading; embedding the font in a distributed binary, or
  committing/uploading a converted font, is redistribution of Apple's assets
  and is what the licence forbids. Screenshots are a separate, unresearched question.
* Proposed README wording: "Apple-style colour emoji (optional). shellshot does
  not include or distribute Apple Color Emoji. `make emoji-font` converts the
  copy that ships with your Mac (using apple-emoji-ttf, MIT) into a file you
  can pass with `--emoji-font`. The artwork belongs to Apple Inc. and is
  licensed for use on Apple devices; you are responsible for complying with
  Apple's terms when you use or share output made with it. Without the
  option, shellshot draws Twemoji (CC BY 4.0)."
* Proposed `NOTICE.txt`: "Apple Color Emoji: NOT included; optional,
  user-supplied via --emoji-font. (c) Apple Inc. Converter scripts:
  samuelngs/apple-emoji-ttf (MIT), third_party/apple-emoji-ttf @ 484daf4e."

## 5. Integration design

Parser (`internal/render/emoji`): reuse the Twemoji table reader, `gsub.go` and
`glyphFor` (cmap via `sfnt.GlyphIndex`, GSUB ligature for multi-rune
clusters, with/without FE0F); add ~120 lines: `CBLC` header -> `BitmapSize`
(48 B; ppem at +44) -> `IndexSubTableArray` -> index format 1 (u32 offsets)
and 3 (u16), image format 17 (18, 19 cheap) -> `CBDT` slice -> `png.Decode`
(the prototype does exactly this). Render: `xdraw.CatmullRom.Scale` of the
square bitmap onto the cell span (side = cell height as for Twemoji; no
bearings, the bitmap is the whole emoji box), cached by `(glyph, side)`. Both
backends satisfy `Image(cluster string, side int) (*image.RGBA, bool)`, so the
rasteriser is unchanged; a format-detecting `emoji.Parse` (COLR vs CBDT; today
it rejects CBDT) lets `--emoji-font` take either.

**Where the font comes from - recommend ONE: (b), user-supplied file, with the
embedded Twemoji as fallback (which also covers (c), "both").**

* (a) go:embed after a `make assets` step: go:embed cannot use `..`, so the
  generated file must land inside the package, e.g.
  `internal/render/emoji/assets/AppleColorEmoji.ttf` (gitignored; a plain
  `go build` then fails without a stub/build tag). Bakes Apple's art into the
  binary (redistribution), +17 MB, needs a Mac in the release path. Rejected.
* **(b)**: `--emoji-font FILE` (also `SHELLSHOT_EMOJI_FONT`) loads a CBDT/CBLC
  or COLRv0 font; order: given font, embedded Twemoji, first rune from text
  fonts. `make emoji-font` builds it on a Mac. No licence exposure for the
  project, no binary growth, normal builds never need Python.

Submodule: right for the **pipeline** (pinned, auditable, like
`third_party/ghostty`, 1.8 MB), never for the font. Added here as
`third_party/apple-emoji-ttf` @ 484daf4e with `--depth 1`; scripts must run
`git submodule update --init --depth 1` (full history is 324 MB), as
`scripts/build-libghostty.sh` does. Vendoring the scripts or `uvx --from git+...`
loses the pinned tree or offline use. Only `make emoji-font` users init it.

`make emoji-font` (`scripts/build-apple-emoji.sh`), tested end to end:

```
git submodule update --init --depth 1 third_party/apple-emoji-ttf
cd third_party/apple-emoji-ttf && PYTHONDONTWRITEBYTECODE=1 uv run --no-project \
  --python 3.12 --with fonttools --with pyyaml --with pillow --with uharfbuzz \
  python cli.py -c ../../scripts/apple-emoji.yaml --output ../../build/apple-emoji/AppleColorEmoji-shellshot.ttf
```
(`--input` defaults to the macOS path; `APPLE_EMOJI_TTC` would pass a copied TTC.)

`scripts/apple-emoji.yaml` = upstream `web.yaml` with `split` disabled,
`strikes: [96]`, `prefer_pngquant: false`, sequences at
`../third_party/apple-emoji-ttf/sequences/*.txt` (relative to the recipe), and
**no** `fs_type`. Output is gitignored (`build/`).

## 6. Prototype result

`/tmp/aet/proto` (throwaway, not committed): parsed the font in 7 ms, resolved
ten clusters via cmap + GSUB, CatmullRom-scaled the 96 px PNGs to a 5x2 sheet
of 48 px cells. Row 1: grinning face; red heart (U+2764 U+FE0F); the same heart
for bare U+2764; man-woman-girl ZWJ family, drawn by Apple as a blue-grey tile
with three white silhouettes; Australian flag. Row 2: thumbs up (medium tone);
keycap 1 (blue tile); rainbow flag; victory hand; shaking face (U+1FAE8,
Emoji 15.1, absent from Twemoji). Colour, alpha edges and scaling look right.

## 7. Plan of small tasks

1. **Build target** (submodule done): `scripts/apple-emoji.yaml`,
   `scripts/build-apple-emoji.sh`, `make emoji-font` (expect 17,144,708 B).
2. **CBDT/CBLC parser**, tested on a tiny hand-built font (no Apple data).
3. **Backend**: format-detecting `Parse`, shared table reader and `gsub.go`,
   CatmullRom scaling, `(glyph, side)` cache; cluster lookup test (synthetic GSUB).
4. **Wire `--emoji-font`/env var** after `task/emoji-fallback` merges (order:
   given font, Twemoji); golden-image test with the synthetic font; `--help`.
5. **README/NOTICE**, section 4 wording.
6. Optional: RGI-only subset step (fonttools CBDT subsetter, -25%, untested).
   No sbix or `morx` parser needed: the pipeline turns both into CBDT + GSUB.

## 8. Risks
* Licence (section 4): never commit or release the font or its outputs.
* Exports depend on the macOS version; goldens cannot use the real font.
* The pin protects the scripts, not the TTC input (sbix layout). Needs Python
  3.12 + `uv` on a Mac. 160 ppem art is dropped; fine up to about scale 3.

## 9. Decisions for the user

1. Accept **(b)** (user-supplied, nothing shipped), or knowingly embed and take
   the redistribution risk (not recommended).
2. Strike: **96 ppem** (17 MB, best) or 64 ppem (11.6 MB, blurrier at scale 3+).
3. Keep the pinned submodule and a Mac-only Python/`uv` optional target?
4. Warn that sharing output made with Apple emoji is on the user?

Recommendation: **proceed** with tasks 1-5 (99.5% coverage, ~150 lines of Go,
reuses the Twemoji sequence code); the open point is the licence.

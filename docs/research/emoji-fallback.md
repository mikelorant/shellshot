# Emoji and CJK glyphs in `shellshot export`

Status: decided, implemented on branch `task/emoji-fallback`. All sizes below
were measured on real downloads (April 2026 upstream state, Go 1.27,
`golang.org/x/image` v0.47.0). Binary size before the work: **17,405,634 B**
(`go build ./cmd/shellshot`, darwin/arm64).

## 1. Problem

The export path draws text with `golang.org/x/image/font/opentype`, which can
draw monochrome outlines only. JetBrains Mono has no emoji, no CJK and no
Hangul; the Symbols Nerd Font fallback has only private-use icons. A rune no
font has is left blank, so a wide emoji or ideograph came out as two empty
cells (the rest of the layout is right: libghostty gives the width).

## 2. What reaches the rasteriser (D, E)

Measured with `internal/vt/ghostty` (20-column terminal, text then `|`).
"Mode off" is libghostty's default; "mode on" is after `CSI ? 2027 h`
(grapheme clustering). Entries are `rune:cells` from `Snapshot.Widths`.

| input                     | mode off                                     | mode on                      |
|---------------------------|----------------------------------------------|------------------------------|
| U+1F600 (wide emoji)      | `0:2`                                        | `0:2`                        |
| U+2764 U+FE0F             | `0:1 1:0`, VS16 does not widen               | `0:2 1:0`                    |
| U+270C U+FE0F             | FE0F **dropped** (line is just `✌`)          | `0:2 1:0`                    |
| family (3 people, 2 ZWJ)  | three `:2` cells, joiners `:0`: 6 cells      | `0:2`, rest `:0`: 2 cells    |
| flag U+1F1E6 U+1F1FA      | two `:2` cells: 4 cells                      | `0:2 1:0`: 2 cells           |
| U+1F44D U+1F3FD (tone)    | two `:2` cells                               | `0:2 1:0`                    |
| `1` U+FE0F U+20E3 keycap  | FE0F **dropped**, `1 U+20E3` = `1:1 1:0`     | `0:2 1:0 2:0`                |
| U+4E16 U+754C, Hangul, kana | `:2` per character                         | same                         |
| U+263A U+FE0E (text)      | `0:1 1:0`                                    | `0:1 1:0`                    |

Conclusions:

* With mode 2027 off the terminal splits sequences into several wide cells and
  even loses U+FE0F; the picture then disagrees with every modern terminal
  (Ghostty enables the mode by default). With it on, a cluster is **one cell:
  base rune with width 1 or 2, followers (VS, ZWJ, tone, regional indicator,
  keycap mark, the whole family) with width 0** - which `Snapshot.Validate`
  already requires. So `internal/vt/ghostty` should enable the mode by
  default (`WithModeDefault(ModeGraphemeCluster, true)`, which survives `RIS`).
* The rasteriser must therefore treat "a rune plus its width-0 followers" as a
  **cluster**, look the cluster up as a whole string, and draw it once over
  the base rune's 1- or 2-cell span. Today each rune is drawn separately, and
  followers are drawn as font marks at the base's pen position.
* Presentation: a cluster containing U+FE0F, ZWJ, a skin tone, a regional
  indicator pair or U+20E3 is an emoji sequence -> colour font first. U+FE0E
  asks for text presentation -> try the text fonts first. A lone
  text-default symbol (U+2764) goes through the normal chain and ends at the
  colour font only if no text font has it.

## 3. Options

### (A) Monochrome outline emoji font, drawn by x/image in the foreground colour

* Noto Emoji (monochrome, OFL 1.1): `NotoEmoji[wght].ttf` 1,982,596 B
  (variable; the default instance is drawn, x/image ignores `gvar`),
  gzip -9 1.23 MB, 1,489 cmap entries, 3,768 GSUB ligatures (Unicode 16+).
* OpenMoji-black (CC BY-SA 4.0, **share-alike**): 1,518,800 B, 1,958 cmap
  entries, 2,645 ligatures.
* Experiment: `FallbackFont` chain [CJK subset, Noto Emoji] rendered U+1F600,
  U+1F44D and U+2764 as outline faces scaled to the cell by `FitFace`; works
  with zero new code. But the monochrome font maps a ZWJ/flag/tone sequence
  to one glyph by GSUB only; x/image has no GSUB shaper, so sequences would
  draw their first rune (👨 for a family) or each rune separately.
* Verdict: cheapest, loses colour, still needs sequence lookup. Kept as the
  user-supplied path (`--fallback-font NotoEmoji-Regular.ttf` works unchanged).

### (B) Colour emoji

`sfnt.Font` exposes no table accessor (only `WriteSourceTo`; a `TODO` in
`sfnt.go` says CBLC/CBDT are not parsed; `LoadGlyph` returns
`ErrColoredGlyph` for bitmap emoji). We hold the file bytes anyway, so a table
directory reader is 30 lines.

| candidate | size | tech | licence | verdict |
|---|---|---|---|---|
| Noto Color Emoji `NotoColorEmoji.ttf` | 10,730,124 B | CBDT/CBLC PNG strikes | OFL | too big |
| Noto-COLRv1 | 5,038,236 B | COLRv1 paint graph, gradients | OFL | too big, hard |
| Apple Color Emoji | system | sbix | proprietary | never embedded; system only |
| Twemoji Mozilla v0.7.0 | **1,474,284 B** (gzip 730 KB) | **COLRv0** + CPAL + GSUB `ccmp`, glyf outlines | art CC BY 4.0, code Apache-2.0 | **chosen** |
| Twemoji 17 PNG atlas (jdecked) | 4,248,486 B for 4,009 72x72 PNGs | raster | CC BY 4.0 | fallback plan |
| Twemoji 17 SVG | 10.1 MB (gzip 4.0 MB) | needs an SVG rasteriser | CC BY 4.0 | no |
| OpenMoji colour/black | black font 1,518,800 B | COLR / outlines | CC BY-SA 4.0 | share-alike: not considered |

Twemoji Mozilla is COLRv0: each emoji is 1-20 plain glyf outlines with a CPAL
colour, drawn bottom to top. It scales cleanly to any `--scale`, so one 1.5 MB
file replaces an atlas. Sequences: its single GSUB lookup holds 2,298
type-4 ligatures mapping (ZWJ, flag, tone, keycap) sequences to one glyph. Both
tables are small to parse (COLR/CPAL ~60 lines, ligatures ~100 lines).
Prototype result: smiley, thumbs up, red heart (with and without U+FE0F),
family of three, Australian flag, medium skin-tone thumb, keycap 1, rainbow
flag and victory hand all rendered in colour from one cluster string.

Coverage against Unicode `emoji-test.txt` 18.0 (3,963 fully-qualified
sequences): **3,624 (91.4%)**, i.e. every sequence up to Emoji 14.0
(Twemoji 14.0.1). Missing: E15.0 31, E15.1 118, E16.0 8, E17.0 163, E18.0 19
(for example U+1FAE8). For those the first rune of the sequence is tried;
otherwise the cell stays blank unless a text font in the chain has it.

Trade-off for the user: Twemoji is the flat, slightly dated Twitter style, and
stops at Unicode 14. The PNG atlas (jdecked Twemoji 17) would cover everything
at +4.25 MB raw (4,009 PNGs) but is raster, fixed at 72 px, and needs a build
step and a key index. Not implemented; the colour code is isolated in
`internal/render/emoji`, so swapping the source later is local.

### (C) CJK

* Noto Sans Mono CJK SC Regular (OFL, no reserved name): 16,393,784 B whole.
* Subset with `pyftsubset` (fonttools 4.68.0, `--layout-features= --no-hinting
  --desubroutinize`) to 3,755 GB2312 level-1 hanzi + 2,350 KS X 1001 Hangul +
  U+3000-30FF (punctuation, kana) + U+FF00-FFEF + U+2010-2027: 6,625 glyphs,
  **1,117,512 B** (gzip 883 KB). It renders hanzi, Hangul and kana through the
  existing opentype path; the full-width glyphs are 1 em wide, `FitFace` scales
  them to two cells.
* Limits: simplified-Chinese glyph forms (Han unification: Japanese or
  traditional text shows SC shapes for some characters), no rare characters,
  one weight. `--fallback-font FILE` (repeatable) and `--system-fonts` cover
  the rest; Noto Sans CJK TTC files are accepted (first face).
* Bigger embedded sets were rejected: the whole font is 16 MB, the full
  2-weight kit 30+ MB.

### System fonts (opt-in)

Exports must not depend on the machine, so system fonts are off by default.
`--system-fonts` searches fixed directories for a curated list of CJK and symbol
families. It is documented as non-deterministic and appended **after** the
embedded fonts, so embedded glyphs never change.

## 4. Decision

1. Enable grapheme clustering (mode 2027) by default in the ghostty adapter.
2. Rasteriser draws **clusters**: the base rune plus following width-0 runes
   are looked up as one string and drawn once over the base's span; a cluster
   no emoji font knows falls back to its base rune (combining marks still use
   the marks path).
3. Font chain: primary JetBrains Mono -> Symbols Nerd Font -> CJK subset ->
   user `--fallback-font` files -> system fonts (opt-in) -> **colour emoji**
   (Twemoji Mozilla). Clusters that request emoji presentation (FE0F, ZWJ,
   tone, flag, keycap) consult the colour font first.
4. New package `internal/render/emoji` (COLRv0 + CPAL + GSUB ligatures, pure
   Go) renders a cluster as a square RGBA image at the needed size (cached),
   scaled to the cell span and centred.
5. Monochrome Noto Emoji is **not embedded**: colour covers the same code
   points for +1.47 MB instead of +2 MB, and a mono tier would be a second,
   inconsistent look. It stays available through `--fallback-font`.

Binary size delta: Twemoji Mozilla +1,474,284 B, CJK subset +1,117,512 B,
total **+2.59 MB** (Go embeds the files raw; gzip would save ~0.9 MB at the
price of a build step and a startup inflate, not done). Measured binary after
the work: 20,154,882 B, **+2,749,248 B** over the 17,405,634 B baseline (fonts
plus the new Go code). Parsing the embedded emoji font takes about 0.36 ms.

## 5. Open product decisions

* **Colour source**: Twemoji 14 (chosen: 1.5 MB, vector, CC BY 4.0 with
  attribution in `NOTICE.txt` and the README) versus a Twemoji 17 PNG atlas
  (+4.25 MB, full coverage, fixed 72 px) versus Noto COLRv1 (5 MB, needs a
  COLRv1 paint implementation, gradients). OpenMoji would add share-alike
  terms to the binary and was not considered.
* **CJK default**: embedding the SC subset (+1.1 MB) gives readable CJK out of
  the box with SC glyph forms; the alternative is opt-in only.
* Remaining gaps: emoji newer than Unicode 14 are blank unless a user font
  has them; `Apple Color Emoji` (sbix) is not supported; COLRv1/CBDT user
  fonts are rejected for colour (monochrome outlines still work); bitmap
  emoji in `--fallback-font` files are not drawn.

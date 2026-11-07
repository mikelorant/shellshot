# Font spacing and sharpness of `shellshot export`

Measured with `go test -run Spacing -v ./internal/render/raster` (the audit
table) plus throwaway harnesses. JetBrains Mono: UPM 1000, advance 0.600 em,
ascent 1.020 em, descent 0.300 em, line gap 0, design line height 1.320 em,
x-height 0.55 em, cap height 0.73 em. Nothing below hard-codes these: the pitch
and vertical metrics come from the loaded font.

## 1. Findings before the change

Cell width was `ceil(advance('M'))`, height `ceil(ascent)+ceil(descent)`.

| size | scale | cell | true advance | letter spacing it adds | pitch err | cell h / em (design 1.32) |
|---|---|---|---|---|---|---|
| 12 | 1 | 8x17 | 7.203 | +0.80 px | +11.1% | 1.42 |
| 14 (old default) | 1 | 9x20 | 8.406 | +0.59 px | +7.1% | 1.43 |
| 14 | 2 | 17x38 | 16.797 | +0.20 px | +1.2% | 1.36 |
| 15 | 1 | 9x21 | 9.000 | 0 | 0% | 1.40 |
| 16 | 1 | 10x22 | 9.594 | +0.41 px | +4.2% | 1.38 |
| 20 | 1 | 12x27 | 12.000 | 0 | 0% | 1.35 |

1. **Pitch**: loose by 0 to 11% depending on size; at 14 px an 80-column line
   was 48 px wider than the font's design. Scale 2 was not twice scale 1
   (9x20 vs 17x38). Ink was balanced in the true advance (left/right bearing
   1.07/1.03 px at 14) so all of the error was extra space on the right.
2. **Height**: both ascent and descent were rounded up, so rows were 3 to 8%
   taller than the design (20 vs 18.5 px at 14), more than any terminal needs.
3. **Centring**: not a problem once the pitch matches the advance (L/R within
   0.15 px for regular and bold; italic leans by design).
4. **Vertical rhythm**: the underline sat `cellH-3*thickness` down, so its
   distance from the baseline varied with size (0 rows at 12 px: touching glyph
   feet; 3 rows at 20 px) and 2x was not twice 1x. Strikethrough is centred on
   half the x-height (good). Box-drawing lines sit at the cell middle, about
   1 px above the x-height middle (as in other terminals; left alone). The deepest
   descender ('p') and the cap height fit the cell at all sizes (asserted), so
   a next row's background cannot clip a descender.
5. See section 2 for rendering quality. 6. Scale is a native font-size
   multiplication (`sizePx = FontSize*Scale`, `renderer.go`), not a bitmap
   resample. 7. Nerd icons are fitted to integer cells and unaffected; combining
   marks are drawn at the base advance, which now equals the cell.

**Options for the pitch**, measured at 14 px (full% / mid% from section 2):

| option | result |
|---|---|
| fractional pitch, glyph at `x = col*8.4` (sub-pixel) | exact tracking, but each glyph has a different sub-pixel phase: more mid-grey (53.4% vs 50.2%), stems differ glyph to glyph |
| `x = round(col*8.4)`, integer origins | exact on average, gaps alternate 8/9 px (uneven rhythm) |
| default size with integer advance (15, 20, 25) | perfect, but `--font-size 14` stays loose |
| **snap the em**: cell = round(advance), em = cell / (advance/em) | exact tracking, integer crisp cells, em differs from the request by at most half a pixel of advance (-4.8% at 14, +4% at 16) |

**Chosen (final)**: follow Ghostty (`third_party/ghostty/src/font/Metrics.zig`,
`face/coretext.zig`, read in this worktree, so verified) and do **not** snap
the em. Snapping was tried first (cell = round(advance), em = cell / advance
per em) and then dropped, because it makes `--font-size` inexact and cannot
match a terminal that draws at the nominal size. The cell is now

* width = `round(advance('M') * em)`, height = `round(line height)` (Ghostty
  `@round`), baseline = `round(face baseline - (cellH - height) / 2)`, the line
  gap split above and below;
* `Metrics` is computed at the final pixel size (`FontSize * Scale`), exactly
  as a terminal on a 2x display does, so scale 2 is not always exactly twice
  scale 1 (e.g. 14 px: 8x18 cells; 28 px: 17x37);
* `--adjust-cell-width/height` (section 4) changes the cell after that.

The em is exactly the requested `--font-size` (times scale), so the pitch
error of section 1 returns whenever the advance is not a whole number of
pixels (a cell is at most half a pixel from the advance, instead of up to a
whole pixel with `ceil`): 14 px -> 8 px cell for 8.4 (-4.8%), 12 px -> 7 for
7.2 (-2.8%), 16 -> 10 for 9.6 (+4.2%), 15 and 20 are exact. Underline: the font's
`post.underlinePosition` below the baseline (2 rows at 15 px). Default font
size stays 15 (9.0 px advance, 9x20 cell, zero error).

## 2. Sharpness audit

What we send and where (all values are the shipped ones after this change):

| setting | where | value | effect |
|---|---|---|---|
| face Size, DPI | `font/font.go` `face()` | `FontSize*Scale` px, DPI 72 | 1 pt = 1 px |
| Hinting | `font/font.go` `face()` | `HintingNone` | see below: x/image ignores the others |
| glyph origin | `raster/draw.go`, `layout.go` | whole-pixel cell origin + integer baseline | no sub-pixel placement, no fractional blur |
| fractional 26.6 dot | `fallback()` | rounded to whole pixels | icons not blurred by placement |
| fallback scale | `font/fit.go` | face size quantised to 1/64 px | scaled, not resampled; fractional em so outlines are off-grid (inherent) |
| Scale | `renderer.go` | multiplies font size and padding | native glyph rasterisation, no bitmap resampling |
| `x/image/draw` scalers | whole repo | none used (`draw.Draw` with `draw.Src`/`draw.Over` only) | no blur |
| glyph blend | `xfont.Drawer` | `draw.Over` of an alpha mask onto opaque pixels | sRGB blend, greyscale AA |
| LCD/subpixel AA | none | masks are single-channel `image.Alpha` | no colour fringes; keep it that way |
| RGBA -> NRGBA | `apng/png_extract.go` | frames are opaque (A=255): conversion is exact | no premultiply round trip loss |
| APNG | `apng/encoder.go` | `BlendSource`, `DisposeNone`, deflate best compression | lossless; no quantisation or palette step |
| sprites | `sprite/*` | integer pixel rects, thickness `max(1,h/14)` | box lines land on whole pixels |
| cursor/underline | `decor.go`, `cursor.go` | integer rects | crisp |

**Hinting is not implemented by x/image.** `font.HintingVertical` is ignored
and `HintingFull` only rounds advances (`sfnt.go`: "TODO: pass h"). We rendered
a test line under None/Vertical/Full: coverage masks were identical
(full% 34.4, mid% 50.2, gradient 1.037 in all three). Without outline hinting,
the baseline, x-height and cap-height edges of every glyph land on a fractional
row. So the change adds `font/gridfit.go`: a face wrapper that remaps outline y
coordinates piecewise-linearly (baseline 0, x-height and cap height rounded,
"p" descender depth rounded, overshoot within 1.5% em flattened), vertical only
like FreeType's light auto-hinter; x and advances untouched.

Numeric sharpness (text line, dark theme, measured while the em was still snapped to 13.33 at
14; the shipped em is now the requested one; full% = share of ink in near-opaque
pixels, mid% = share of ink pixels between 20% and 80%; higher full / lower mid
is crisper):

| em px | old (ceil cell, no fit) full/mid | snapped em, no fit | snapped em + grid fit |
|---|---|---|---|
| 12 | 28.3 / 51.2 | 30.8 / 51.3 | 29.3 / 50.2 |
| 14 | 34.4 / 50.2 | 28.5 / 54.7 | **34.2 / 50.5** |
| 16 | 41.1 / 49.8 | 52.5 / 42.4 | 50.0 / 47.1 |
| 28 | 66.6 / 35.8 | 65.5 / 36.4 | 68.0 / 34.0 |

(Light theme is within 0.1 of dark.) Grid fit recovers what snapping the em
costs at 14 px and sharpens tops/bottoms of round letters (neutral at 12 px, a
point worse at 16 where the snapped em alone already lands near whole rows); the remaining
mid-grey is vertical stems at arbitrary x phase, which no vertical-only fit
can change (x snapping would change glyph widths). Honest size of the effect:
a few points, visible as crisper x-height tops and descender bottoms.

**Blending.** Terminals blend coverage in sRGB. Perceived (light-linear) ink
weight of the same outline: dark theme 0.77, light theme 1.22 of the
linear-correct weight (white on black 0.71, black on white 1.29). We apply
`coverage^0.8` for light-on-dark and `coverage^1.25` for dark-on-light
(`raster/contrast.go`), which moves the dark theme to about 0.85 without the
anaemic look of a fully linear blend. Trade-off: edges of light text are
slightly brighter than strict sRGB blending.

## 3. HiDPI and supersampling

**Scale as the deliverable.** `--scale 2` rasterises glyphs at twice the em
(native, verified in `renderer.go`); the cell is rounded at the doubled size, so
it is twice scale 1 only when the advance is whole at 1x (15: 9 -> 18). Real recording,
80x24, 32 frames, `export` on this machine:

| scale | image | APNG | encode |
|---|---|---|---|
| 1 | 736x496 | 0.85 MB | 0.58 s |
| 2 | 1472x992 | 2.10 MB (2.5x) | 1.70 s |
| 3 | 2208x1488 | 3.22 MB (3.8x) | 3.05 s |

Delta frames keep growth well below the 4x/9x pixel growth (scale 1 took
0.52 s before grid fit and the contrast curve: about 10% slower). Pitch error shrinks with
scale (rounding half a pixel of a bigger cell), sharpness rises with size
(full% 34 at 14 px to 68 at 28 px) because stems cover whole pixels.
Displayed at half size on a retina screen it renders at the device's resolution.

**Supersampling** (render at N x with exact fractional pitch, reduce by N),
text line at 14 px, dark, same metrics as above:

| variant | full / mid % | note |
|---|---|---|
| native, snapped em + grid fit | 34.2 / 50.5 | integer cells, exact pitch |
| ss2 box | 32.2 / 52.9 | |
| ss3 box / ss4 box | 32.3 / 50.2, 33.5 / 51.4 | tracking exact to 1/N px |
| ss2 CatmullRom | 23.0 / 38.9 | ringing, softer |
| ss3/4 ApproxBiLinear | 79 / 27 | **artefact**: samples a sparse subset of pixels (aliasing) |

At 16 px the box filters give 39.4 to 41.4 full% against 50 for native with
grid fit. Supersampling softens because outlines are not aligned to the
reduced pixel grid: a stem edge at a fractional position stays fractional
after averaging; N-fold finer positions only make the blur smoother. Aligning
stems to the *output* pixels is hinting, which is what grid fit does natively.
It also needs fractional output cell sizes (image width `cols*8.4`), which
blurs sprite block edges. **Not implemented**: no `--supersample` flag, as it
does not beat native on both spacing and sharpness (box average is 1 to 11
points less crisp than native; spacing is already exact natively).

**Recommendation**: keep default scale 1 (small files). For README or web
embeds export with `--scale 2` and set the display size in HTML/Markdown
(`<img src="demo.apng" width="736">`, the 1x width), so retina screens show
native 2x glyphs; use `--scale 3` only for 3x displays.

## 4. Cell size adjustment (`--adjust-cell-width/height`)

Instead of `--letter-spacing`/`--line-height`, `export` has Ghostty's
`adjust-cell-width` and `adjust-cell-height`: VALUE is an integer pixel delta
(`-1`, `2`) or a percentage (`-7%`) of the font-derived cell (after rounding and
after `--scale`), default 0. Verified in Ghostty's source:

* `Metrics.apply`: `new = max(modifier.apply(original), 1)`; a percentage is
  `round(original * (1 + p/100))`. We reject results under 1 px or over 10x
  (Ghostty clamps to 1 and has no upper bound); `-100%` is an error.
* The cell changes, not the glyph: a wider cell centres the glyph
  (`dx = (cell - face_width)/2`; FreeType rounds it, CoreText keeps the
  fraction, we follow CoreText). A narrower cell keeps only the fractional part
  of the (negative) offset, so the glyph stays anchored at the cell's left edge
  and overflows to the right; nothing is clipped or re-centred.
* A height change moves the baseline by half the difference, the odd pixel going
  to the side where the face sits away from centre (`face_y`); underline and
  strikethrough follow. Ours is relative to the baseline, so they move with it.
* Not implemented: the sub-pixel `x` of non-ASCII "stretch" constraints (icons
  are fitted to integer cells already).

`internal/render/font` tests compare `Metrics.Adjust` with a transcription of
the Ghostty arithmetic over 9 sizes x 6 adjustments per axis.

**VHS** (xterm.js: fontSize 22, letterSpacing 1, lineHeight 1.0) has pitch
13.2 + 1 = 14.2 px (0.645 em) and rows of 1.32 em; `--font-size 22
--adjust-cell-width 1` gives cell 14 (13.2 -> 13, +1) with the glyphs at 22 px.
VHS pads 60 px by default (`--padding 60`).

### Comparison with the user's terminal

Ghostty config: `font-size = 12`, `adjust-cell-width = -7%`, default family
(JetBrains Mono), on a 2x display, so 12 pt = 24 px. Advance 0.6 em,
line height 1.32 em:

| step | Ghostty (source) | `export --font-size 12 --scale 2 --adjust-cell-width -7%` |
|---|---|---|
| em | 24 px | 24 px (`12 * 2`) |
| advance / line height | 14.4 / 31.68 | same |
| rounded cell | 14 x 32 | 14 x 32 |
| width -7% | `round(14 * 0.93)` = 13 | 13 |
| cell | **13 x 32** | **13 x 32** (asserted in `TestAdjustCellSize`) |
| glyph x | `(13 - 14.4)/2 = -0.7`, integer part dropped: -0.7 px, left-anchored | -0.7 px (`GlyphOffsetX`) |
| pitch / em | 0.542 (7.2% tighter than 0.6 design) | same |

Measured on a real export (80x12): 1072x416 px = 80 * 13 + 2 * 16 by 12 * 32 +
2 * 16; the default export (15, 9x20) is 736x256. Without the adjustment the
same font would be 14x32. Differences left: Ghostty on macOS rasterises with
CoreText (its own smoothing/thickening, `font-thicken` off by default) and
uses the system's DPI scale; our glyphs are FreeType-style greyscale masks with
vertical grid fit and a contrast curve. Metrics and positions should match to
the pixel; stroke weight may not.

**Recipe**: `shellshot export demo.shot --font-size 12 --scale 2
--adjust-cell-width -7%`. For a 1x export of the same look use `--font-size 12
--adjust-cell-width -7%` (7.2 -> 7, -7% -> 7: no change at that size, so 2x
is the faithful variant). Set the HTML display size to half the image width.

## 5. Decisions for the user

* **Default font size**: 15 (zero pitch error, 9x20 cell, as before the
  research) or back to 14 (8.4 px advance rounds to an 8 px cell, -4.8% pitch,
  11% narrower images; with the em no longer snapped, 14 is now a
  visibly tighter pitch than the font design)? Kept 15.
* **Em snapping**: dropped, because `--font-size` now means the exact em (as in
  a terminal) and a faithful match to Ghostty needs the unsnapped em. The
  trade-off is the up-to-5% pitch error at sizes where the advance is not
  whole; `--adjust-cell-width` is the tool to correct it deliberately.
* **Supersampling**: not implemented. Measured in section 3: box-filter
  supersampling is 1 to 11 points less crisp than native rendering with vertical
  grid fit, and native tracking error is already bounded by cell rounding.
  `--scale 2` with a display size of half the image is the sharp option.
* **Contrast curve** (exponents 0.8 / 1.25, `raster/contrast.go`): a taste
  call; set both to 1 for plain sRGB blending.

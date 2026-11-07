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

**Chosen**: snap the em (`font.Set.Metrics` returns `EmPx`). Cell height is the
font's line height rounded, baseline = ascent + half the rounding. Metrics are
computed at scale 1 and multiplied, so `--scale n` is exactly n times
the layout. Default font size 15 keeps the old 9x20 default cell (glyphs 7%
larger; tracking 0.600 em instead of 0.643). Underline: the font's
`post.underlinePosition` below the baseline (2 rows at 15 px).
Other terminals, from memory (the ghostty submodule is not checked out here, so
unverified): Ghostty, kitty and WezTerm take cell width = ceil/round of the
face's advance at the configured size and keep the em as set (loose at 14 px,
like we were); Ghostty derives height from ascent + descent + line gap and
centres the baseline in it, which is what we now do.

After (audit table, scale 1): 12 -> 7x15, em 11.67; 14 -> 8x18, em 13.33;
15 -> 9x20; 16 -> 10x22, em 16.67; 20 -> 12x26. Pitch error 0.00% everywhere.

## 2. Sharpness audit

What we send and where (all values are the shipped ones after this change):

| setting | where | value | effect |
|---|---|---|---|
| face Size, DPI | `font/font.go` `face()` | `FontSize*Scale` px (snapped em), DPI 72 | 1 pt = 1 px |
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

Numeric sharpness (text line, dark theme; full% = share of ink in near-opaque
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
(native, verified in `renderer.go`), exactly twice the layout. Real recording,
80x24, 32 frames, `export` on this machine:

| scale | image | APNG | encode |
|---|---|---|---|
| 1 | 736x496 | 0.85 MB | 0.58 s |
| 2 | 1472x992 | 2.10 MB (2.5x) | 1.70 s |
| 3 | 2208x1488 | 3.22 MB (3.8x) | 3.05 s |

Delta frames keep growth well below the 4x/9x pixel growth (scale 1 took
0.52 s before grid fit and the contrast curve: about 10% slower). Pitch error is zero at
every scale (layout is n x scale 1), sharpness rises with size
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

## 4. Letter spacing and line height

Effective letter spacing before = cell width - true advance at the requested
size: +0.80 px (12), +0.59 (14), +0.41 (16), 0 (15, 20), +0.20 (14 at scale 2),
varying with size from ceil(); line height 1.36 to 1.43 em. Now the cell equals
the advance of the drawn em: spacing 0, line height 1.30 to 1.35 em (design 1.32,
rounded to a pixel). VHS (xterm.js: fontSize 22, letterSpacing 1, lineHeight 1.0)
has pitch 13.2+1 = 14.2 px = 0.645 em and rows of 1.32 em: the old default
(9/14 = 0.643 em) matched it by accident; the new one is 0.600 em, 7% denser
horizontally, same row density. To mimic a VHS look use `--font-size 24`
(cell 14, pitch 0.600 of 23.3 em, about VHS's 14.2) or `--font-size 22` (cell
13, 8% tighter, same glyph size); VHS pads 60 px by default (`--padding 60`).

**Not implemented**: `--letter-spacing PX` and `--line-height N`. With whole-
pixel cells a fractional spacing is not well defined (the spacing would have
to be rounded into the cell width, or glyphs placed at `round(col*pitch)`
with uneven gaps, see section 1). Recommended if wanted: integer
`--letter-spacing` added to the rounded cell width (glyphs centred in the cell)
and `--line-height` as a multiplier of the font line height before rounding;
both are small now that `font.Metrics` owns the policy.

## 5. Open points for the user

* Default font size 15 (was 14): the only way to keep the 9x20 cell with exact
  tracking. Keep 15 or return to 14 (8x18 cell, 11% narrower images)?
* `--font-size` is now a nominal size: the drawn em moves by at most half a
  pixel of advance (14 -> 13.33).
* The contrast curve (exponents 0.8 / 1.25) is a taste call; set both to 1 in
  `raster/contrast.go` to get plain sRGB blending.

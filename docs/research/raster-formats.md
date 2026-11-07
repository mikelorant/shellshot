# Research: animated raster formats for `shellshot export` (GIF, WebP, AVIF, JXL)

Research date: 2026-11-07 (dates in sources are relative to this). Status: **desk research only. No measurements were taken.**
The research run had no shell tool, so nothing in section 4 is "measured". Section 6 holds a ready-to-run harness for the measuring task.
Every number labelled *estimate* or *unverified* is not evidence.

## 1. Summary and recommendation

**Ship GIF first, then animated lossless WebP. Do not ship AVIF or JXL now.**

| Order | Format | Library / approach | Verdict |
|---|---|---|---|
| 1 | GIF | stdlib `image/gif` (encoder) + our own palette code (exact palette when <=255 colours, else `go-quantize` median cut, no dither by default) + our own delta/transparency logic | **Yes.** Zero new dependency (or one tiny one). Plays everywhere, including Slack and GitHub. |
| 2 | WebP **lossless** (VP8L) | Our own small `RIFF/VP8X/ANIM/ANMF` muxer (about 150 lines, the same shape as `export/apng`) + a pure-Go VP8L frame encoder (`HugoSmits86/nativewebp`, or `gen2brain/vpx` as the alternative) | **Yes, after measuring.** Pure Go, no cgo and no wasm. Sub-rect delta frames need our own muxer (see 2.2). |
| - | WebP lossy | `gen2brain/vpx` could do it | **No** by default. Chroma subsampling and quantisation hurt coloured text edges (researcher inference). |
| - | AVIF | `gen2brain/avif` (wasm) has `EncodeAll`; `gen2brain/gav1d` (pure Go) is still-only | **No.** Heavy, slow, and poor sharing support. |
| - | JXL | `gen2brain/jxl` (pure Go) encodes a single frame only; `gen2brain/jpegxl` (wasm) animation is decode-only | **No.** No usable animated encoder, and sharing support is nearly absent. Revisit in 2027. |

Default CLI surface (minimal): `--format apng|gif|webp` (inferred from the `-o` extension). Flags are optional extras:
- `--gif-colors N` (default 256)
- `--gif-dither` (off)
- `--webp-effort` (only if the library offers it)

Everything else stays internal.

## 2. Per-format evidence

### 2.1 GIF (stdlib plus our own quantiser)

All of this is from library knowledge and was **not re-verified in this run** unless a citation is given.

**Encoder.** `image/gif.EncodeAll(w, *gif.GIF)` writes:
- per-frame `Image []*image.Paletted`, whose `Bounds()` are used as the frame sub-rectangle (so delta rects work)
- `Delay []int` in 1/100 s
- `Disposal []byte`
- `LoopCount`
- a global `Config.ColorModel` palette, plus per-frame local palettes
- transparency, by putting a `color.Transparent` entry in the palette. Use disposal 1 (do-not-dispose) with a transparent index for unchanged pixels inside the changed rect.
- The stdlib LZW is slower and about 5-15 % bigger than gifsicle's (estimate). Nothing better is available in pure Go.

**Quantisers**

| Option | Notes | Verdict |
|---|---|---|
| stdlib `palette.Plan9` / `WebSafe` | Fixed palette that does not contain the theme colours, so flat colours shift. | Reject. |
| `x/image/draw` `FloydSteinberg` | Needs a palette. It dithers hard edges, which puts speckle noise around text and makes deltas noisy (every pixel differs next frame). | Off by default. |
| `github.com/ericpauley/go-quantize` | Median cut implementing `draw.Quantizer`. Last push 2022-03-08 (API: GitHub). Licence believed MIT (**unverified**). Stable, tiny. | **Use as the fallback.** |
| `github.com/soniakeys/quant` | Several algorithms (median cut, mean, `ssim`-style). Last push 2018-03-04 (API: GitHub). Licence unverified. | Backup. |
| Own exact-palette pass | Count distinct colours across all frames. If <=255, use them verbatim: lossless, no dither, one global palette. | **Primary path.** Terminal text without emoji nearly always qualifies (researcher inference; measure). |

**Terminal specifics (researcher inference, to confirm in measurements)**
- Anti-aliased text with a theme palette usually gives hundreds of distinct greys/blends per fg/bg pair. The union over a whole recording can exceed 256 even without emoji.
  - So the real policy is: exact palette if the count is <=255. Otherwise build ONE global palette by popularity-weighted median cut over all frames (weights from pixel counts), reserving index 0 for transparent.
  - A global palette keeps colours stable across frames. Per-frame palettes cause flicker and bigger files.
- Apple colour-emoji gradients exceed 256 colours by themselves. Expect banding or noise there. Offer dither only for that case (`--gif-dither`).
  - A cheap trick is to dither only the cells that hold emoji. That needs renderer cooperation, so it is not an MVP item.
- **Delays.** GIF stores 1/100 s. Browsers historically treat delays of 0 or 1 cs (<=10 ms) as 100 ms, so clamp to a minimum of 2 cs (20 ms). This is from memory and was not re-checked this run.
  - Round delays to the nearest cs and carry the rounding error forward so the total duration is preserved.
  - Merged identical frames give long delays. The max is 65535 cs (about 10 min), so split longer ones.
- **File size.** GIF is expected to be larger than APNG for gradient/emoji content and smaller or equal for flat text with delta frames (estimate). Measure.

### 2.2 WebP animated

| Library | URL | Version / last push | Licence | Impl. | Animation write | Verdict |
|---|---|---|---|---|---|---|
| nativewebp | github.com/HugoSmits86/nativewebp | v1.3.0 (pkg.go.dev); pushed 2026-05-10; 462 stars | licence not confirmed here (**verify**; believed MIT) | pure Go | Lossless VP8L only. `EncodeAll(w, *Animation, *Options)`. `Animation{Images, Durations(ms), Disposals(0 keep/1 background), LoopCount, BackgroundColor}`. **No per-frame offsets listed.** | **Best VP8L frame encoder candidate.** Options: `CompressionLevel` 0-6 (default 4), `UseExtendedFormat`. |
| vpx | github.com/gen2brain/vpx | created 2026-08-15, pushed 2026-09-18; 15 stars | BSD-3-Clause (libwebp port) plus a patent grant file | pure Go (arm64 NEON, amd64 AVX2, `-tags noasm`) | `webp.EncodeAll(w, *WEBP, ...EncodeOptions)`. README says "animation with its disposal and blending". pkg.go.dev says **"all frames must share the same bounds"**. `EncodeOptions{Quality, Lossless, Method, Exact, Threads}`. | Strong alternative: lossy **and** lossless, a byte-exact libwebp port, and it claims lossless encoding faster than libwebp. **Very young**, so a maturity risk. |
| gen2brain/webp | github.com/gen2brain/webp | pushed 2026-08-15; 64 stars | MIT (API); wasm is libwebp, BSD-3 | wasm2go-transpiled pure Go; optionally loads a system libwebp via purego | `EncodeAll` for animation, `Options{Quality, Lossless, Method...}`, `webp.wasm` is 533,555 B (API) | Works, but it is a bigger blob, and it **silently prefers a system libwebp** if installed (use `-tags nodynamic`). Not needed if vpx or nativewebp work. |
| `x/image/webp` | golang.org/x/image | - | BSD-3 | pure Go | Decode only | Reject. |
| libwebp via cgo (`chai2010/webp` etc.) | - | - | BSD | cgo | yes | Reject: cgo for users. |

**Design decision (researcher inference, from the facts above).** Libraries take whole-canvas frames, so the ANMF frame offset/size and `blend`/`dispose` bits go unused.
- Our APNG encoder already computes delta sub-rectangles.
- So write our own container muxer: `RIFF 'WEBP'` + `VP8X`(animation flag + alpha flag) + `ANIM`(background, loop) + one `ANMF` per delta rect (x, y, w, h, duration, flags) holding a VP8L chunk.
- Obtain the VP8L bitstream from `nativewebp.Encode` or `vpx webp.Encode(Lossless)` by stripping the RIFF header. Both write a simple `VP8L` chunk (to confirm).
- Check `ANMF` needs even x/y offsets (the spec stores them as offset/2), so round rect origins down to even.
- The result matches what `export/apng` already does (sub-rect, blend source).
- The whole-canvas frames used by the libraries would make a mostly-idle recording many times larger, so this is worth the 150 lines.

**Lossless vs lossy.** Lossless VP8L is exact, handles flat colours very well (palette/colour cache/LZ77), and keeps alpha. Lossy VP8 is 4:2:0 YUV, which smears saturated one-pixel strokes, and its inter-frame-free WebP animation gains little. Recommend lossless only (inference).

**Browser/app support** is in section 3.

### 2.3 AVIF animated

- **`gen2brain/avif`** (https://github.com/gen2brain/avif): MIT; pushed 2026-08-14; 84 stars; libavif + aom (encode) + dav1d (decode) compiled to WASM.
  - Run with **wazero** by default, or transpiled with `-tags wasm2go`. It tries a system libavif via purego first (`-tags nodynamic` to disable).
  - `avif.EncodeAll(w, *AVIF{Image, Delay (seconds), LoopCount}, Options{Quality(60 default), QualityAlpha, Speed 0-10 (default 10), ChromaSubsampling, Lossless})`. Whole-frame images, no sub-rects. Lossless forces 4:4:4.
  - Size: `lib/avif.wasm.gz` is **1,292,476 B** (API). The raw wasm is larger (not measured). Adding wazero would add a few MB (estimate).
- **`gen2brain/gav1d`** (pure Go, BSD-2): created 2026-08-11, 8 stars. Its encoder is **all-intra, 8-bit, single image**. Image *sequences* are decode only. No animated encode, so no.
- Others (cgo `libavif`, `rav1e`/`SVT-AV1` CLIs) are rejected: cgo or external binaries.
- **Quality for text.** AV1 at low quality blurs hard text edges. Lossless AVIF is large. 4:2:0 hurts coloured text. Encode via aom is slow for 100+ large frames (estimate; measure if revisited).
- **Sharing:** GitHub's documented upload list has no AVIF (see section 3). Slack does not list it. Discord supports it but converts to WebP.
- **Verdict: no.** Support is incomplete where users share, and the build cost is high for a format that does not beat lossless WebP on flat screens.

### 2.4 JPEG XL animated

- **`gen2brain/jxl`** (https://github.com/gen2brain/jxl, v0.2.0 per pkg.go.dev): pure Go, port of jxl-rs, BSD-3-Clause. Created 2026-08-14, pushed 2026-10-30, 8 stars. pkg.go.dev says encoding "writes a single frame". `EncodeOptions{Quality(90), Effort(max 7), Lossless, Threads}`. `DecodeAll` reads animation frames with delays. **No animated encode.**
- **`gen2brain/jpegxl`** (wasm, libjxl, MIT, pushed 2026-08-14): README says animated JXL is **decode only**. `encode.wasm.gz` is 714,353 B (API). No animated encode.
- libjxl via cgo / `cjxl`: rejected (cgo or external binary).
- We could hand-write the JXL animation container around single-frame codestreams? **Not feasible**: frames share one codestream and header (researcher inference).
- **Browsers (2026-11-07):**
  - Chrome 155+ (2026-10-06) and Firefox 158+ (2026-10-13) support JXL (caniuse, fetched).
  - Safari 17+ is "partial" (caniuse). A secondary summary says it lacks animation. The caniuse note text was not extracted, so Safari animation is **unverified**.
  - Edge is "Not supported" (web-features explorer).
  - No GitHub/Slack/Discord support found.
- **Verdict: no.** Revisit when a Go library writes animated JXL and GitHub supports it.

## 3. Compatibility table (animated playback)

Legend: Y = verified animated, P = partial, N = no, ? = unverified.

| Where | APNG | GIF | WebP | AVIF | JXL |
|---|---|---|---|---|---|
| Chrome | Y (long-standing, background knowledge) | Y | Y (background knowledge) | Y from 85 (caniuse; animation inference/`?`) | Y from 155 (animation `?`) |
| Firefox | Y | Y | Y | Y from 93 (caniuse; animation `?`) | Y from 158 (animation `?`) |
| Safari | Y | Y | Y (background knowledge) | Y from 16.4 (caniuse; animation `?`) | P from 17 (caniuse; animation reported absent, unverified) |
| GitHub README `<img>` | Y (background knowledge, **unverified**) | Y | Y? WebP support announced in community discussion #171343 (date not extracted); animated not stated there | N/? not in docs | N/? |
| GitHub issue/PR comment upload | Y? (PNG allowed) | Y | WebP uploads now allowed (discussion #171343); docs page still lists only PNG, GIF, JPEG, SVG, video, BMP/TIFF | not in docs | not in docs |
| Slack | ? | Y (animated thumbnails documented for GIF, per search summary) | ? likely static (unverified) | N/? | ? |
| Discord | ? | Y | Y animated since mobile release 252 (2024-11-02); blog 2025-03-12 | Y, converted to WebP by Discord (blog 2025-03-12) | ? |
| macOS Preview / Quick Look | Quick Look: ? | Quick Look plays GIF; Preview shows frames as stills (Apple Support) | Quick Look plays, can be choppy on Sequoia (third-party source, unverified) | ? not verified | ? |
| VS Code Markdown preview | ? (Chromium: plays) | Y (Chromium) | Y (Chromium) | Y (Chromium, version-dependent) | Y/? (depends on Electron Chromium >=155) |
| Windows Photos | ? | ? | ? | ? | ? |

Sources: caniuse AVIF and JXL pages, the web-features explorer, the Discord blog, the GitHub docs and discussion, Apple Support (see section 7).
**Contradiction:** the GitHub "Attaching files" doc (fetched) does not list WebP, but community discussion #171343 says WebP upload is supported. The docs look stale. Test with a real issue before documenting WebP support.

## 4. Measurements

**Measured: none** (no shell available). The table below is the required format for the follow-up.

| Recording | APNG bytes (baseline) | GIF exact | GIF median-cut | GIF +dither | WebP lossless (nativewebp) | WebP lossless (vpx) | Encode ms / peak MB |
|---|---|---|---|---|---|---|---|
| A: 60 lines, 60x12, scale 2 | not measured | not measured | not measured | not measured | not measured | not measured | not measured |
| B: mostly-idle session | not measured | | | | | | |
| C: colour emoji | not measured | | | | | | |

**Estimates (not evidence):**
- Binary size: `vpx` or `nativewebp` +0.3 to 1.5 MB; `gen2brain/webp` +1 to 3 MB (533 KB wasm becomes transpiled Go); `avif` +5 to 10 MB with wazero.
- GIF: +0.
- Expected size ordering for flat-text screens: GIF with delta frames is about equal to APNG, and lossless WebP is about 20-40 % smaller than APNG with the same delta rects.
- This ordering is a prior only.

## 5. Recommendation detail

**Defaults**
- **GIF.**
  - 256 colours (255 plus transparent). Global palette. No dither. Delta frames with a transparent index, disposal 1.
  - Delays rounded to cs, minimum 2 cs, remainder carried forward. Loop 0 (forever). A very long idle hold is split into a series of <=65535 cs frames.
  - Merge identical frames, as the APNG path does.
- **WebP.**
  - Lossless. Library default effort (nativewebp level 4 as the default; level 6 only if encode time stays <2 s per 100 frames).
  - Canvas `ANIM` background is transparent black. Frames use `blend=0` (do not blend) and `dispose=0`. Delta rects have even offsets. The loop count follows the APNG option.
- **Flags:**
  - `--format` (or by extension of `-o`).
  - Optional `--gif-colors` and `--gif-dither` only if the measurements show an emoji win. No lossy flag.

**Shared pipeline.** The existing `internal/export/pipeline` merges frames, produces deltas and fixes timing. Reuse it so GIF and WebP only write containers. (I did not read the pipeline code this run. Confirm it exposes delta rects and dedup'd delays before coding.)

## 6. Follow-up plan (small atomic tasks) and measurement harness

1. **Measure.** In a temp module, decode APNG frames with `github.com/kettek/apng`. Run candidates over recordings A (`--cols 60 --rows 12`), B (mostly idle) and C (emoji). Record bytes, ms, and peak RSS from `/usr/bin/time -l`. Save results into section 4. No repo code.
   - Verify the unverified claims: VP8L chunk extraction, nativewebp licence, even offsets.
2. `export/gifenc`: palette builder (exact <=255, else go-quantize median cut) + unit tests with a synthetic 3-colour image and an emoji-like gradient.
3. `export/gifenc`: delta-frame encoder (sub-rect, transparent index, disposal 1) + test that round-trips through `image/gif.DecodeAll` and compares composited frames to the input.
4. `export/gifenc`: delay quantiser (cs, min 2, carry, split >65535) + table test.
5. Wire `--format gif` / `.gif` extension in the CLI and `docs/export.md`.
6. `export/webpenc`: ANMF muxer with a VP8L chunk from the chosen library + a test that `dwebp`/`libwebp` (or `vpx` decode, `webp.DecodeAll`) composites back to the input.
7. Wire `.webp` in the CLI, with a golden size test against the APNG on recording A.
8. Docs: the compatibility table (with the GitHub WebP check) in `docs/export.md`.
9. Optional later: dither emoji cells only; `-tags` binary-size check in CI.

**Harness sketch (for task 1):**
```go
// go.mod: github.com/kettek/apng, github.com/HugoSmits86/nativewebp, github.com/gen2brain/vpx,
//         github.com/ericpauley/go-quantize
a, _ := apng.DecodeAll(f)            // a.Frames[i].Image, DelayNumerator/Denominator, X/YOffset, Dispose/Blend
// composite onto canvas -> []*image.RGBA + delays; encode each variant; log len(bytes), time.Since, runtime.MemStats
// view: crop 200x100 at 2x of a text region -> PNG < 10 KB, and inspect.
```

## 7. Risks and open questions

- **No measurements:** sizes, speeds and visual quality are all unmeasured, so the ordering in section 4 is a prior.
- **Young libraries:** `gen2brain/vpx`, `gav1d` and `jxl` all date from August 2026 (8-15 stars). Pin versions and test with a decoder. `nativewebp` is older (462 stars) but its last push was 2026-05-10 and its licence is unconfirmed.
- **Patent grants:** the vpx, gav1d and jxl repos add patent-grant files (`PATENTS`). The licence is BSD, but legal should read them before shipping.
- **WebP in GitHub:** not documented in the GitHub docs, and the date of the discussion post is not known. Animated playback in READMEs is unverified.
- **Slack WebP animation, macOS Preview/Quick Look for AVIF/JXL, Windows Photos, VS Code:** unverified.
- **Safari animated JXL:** unverified (see above).
- **GIF clamp:** the 10 ms to 100 ms behaviour is from memory.
- **Search tooling:** many compat claims came from an LLM-backed search summary and were checked against the fetched primary page only where cited. Treat the rest as leads.

## 8. Sources

- Kept: gen2brain/webp (github.com/gen2brain/webp, API for licence/dates/wasm size, pkg.go.dev); gen2brain/avif (same; code read: `avif.go` struct and options); gen2brain/jpegxl (README: animation decode only); gen2brain/vpx README and pkg.go.dev (`EncodeAll`, same-bounds sentence); gen2brain/gav1d README and pkg.go.dev; gen2brain/jxl README and pkg.go.dev (v0.2.0, single-frame encoding); nativewebp (pkg.go.dev: Animation fields, Options); caniuse.com/jpegxl and /avif; web-platform-dx.github.io/web-features-explorer/features/jpegxl/; hacks.mozilla.org/2026/08/intent-to-ship-jpeg-xl/; docs.github.com attaching-files; github.com/orgs/community/discussions/171343; discord.com/blog/modern-image-formats-at-discord-supporting-webp-and-avif; Apple Support Preview GIF page and mshibanami.github.io/webp-for-quicklook (secondary).
- Rejected: `x/image/webp` (decode only); libwebp/libavif/libjxl via cgo; soniakeys/quant as primary (inactive since 2018); LLM search summaries as sole evidence.

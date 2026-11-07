# Research: APNG export for shellshot recordings

Date of research: this run. Scope: turn `.shot` recordings (timed snapshot frames) into an APNG with no headless browser, Go-first, macOS first.

## 1. Summary and recommendation

**Recommendation (single best approach): a pure-Go, cgo-free rasteriser plus a small in-house APNG encoder.**

- A new `render` package set draws each `snapshot.Snapshot` cell by cell.
  - Text comes from `golang.org/x/image/font/opentype` with embedded OFL/BSD fonts.
  - Box-drawing, block, braille and powerline glyphs are drawn procedurally.
  - Underline, strike and cursor are drawn as rectangles.
- A new `export/apng` package writes the file. It builds on stdlib `image/png` plus about 150–250 lines of acTL/fcTL/fdAT chunk code. It adds delta sub-rectangle frames, `BLEND_OP_SOURCE`, de-duplication of identical frames, and a global timing policy.
- Nothing in the export path needs cgo or libghostty at export time.

**How much "leverage libghostty" is real here: very little, honestly.**

- libghostty-vt is a state and parsing library. It does not rasterise, shape, load fonts or render at the pinned commit (details in section 2).
- The real leverage was already taken at record time. libghostty parses the VT stream and maintains the cell grid. It also resolves cell styles, graphemes, the 256-colour palette and the default fg/bg/cursor colours.
- For export, the useful move is to persist what libghostty resolved but we currently drop: palette, default fg/bg and cursor colour. That keeps the output faithful to the user's terminal (section 5.4).
- Do not claim that libghostty renders the APNG.

**Ranked alternatives**

1. Pure-Go x/image rasteriser plus own APNG encoder (recommended).
2. Same, with an optional macOS-only CoreText glyph backend behind a build tag. It gives better hinting, emoji and CJK fallback. It needs cgo and its output is not reproducible across machines.
3. SVG-first, then rasterise with `resvg-go` (resvg compiled to WASM, run on wazero, which freeze uses). It is cgo-free but heavy (WASM blob, slow per frame) and gives no quality gain over direct rasterising.
4. Build extra zig targets from ghostty's font stack. Not feasible as a library today (section 2.3).
5. Wrap an external tool (agg, Rust, GPL, GIF-only, needs a cast file). Rejected: wrong format, wrong licence, and an external binary.

## 2. What libghostty-vt exposes (evidence)

**Evidence caveat.** I had no shell in this run, so I could not run `go list -m -f '{{.Dir}}' go.mitchellh.com/libghostty`. `third_party/ghostty` is also not populated at `/Users/michael.lorant/Workspace/projects/shellshot/third_party/ghostty`. Headers were therefore fetched from GitHub at the pinned commit `66af4c7458db7e19ae7adb44aa7054879be58b6b`, and extracted via a summarising tool, not read line by line. The Go binding was read from `mitchellh/go-libghostty` **main**, which may be newer than the pinned module version `v0.0.0-20261106041537-e247028bdf51`. Re-verify method names against the module cache before coding.

### 2.1 Header facts

All of these are direct evidence from `include/ghostty/vt.h` and the headers at the pinned commit.

- The umbrella header documents these groups: terminal ("Complete terminal emulator state and rendering"), render ("Incremental render state updates for **custom renderers**"), formatter, snapshot, search, osc, sgr, paste, unicode, wasm, and key/mouse/focus encoding.
- Optional build features include `%render_state`, `%formatter`, `%kitty_graphics`, `%glyph_protocol` and `%grid_introspection`.
- The words "font" and "GPU" do not appear in `vt.h`.

**`render.h` is render state, not rendering.**
- It provides `ghostty_render_state_{new,update,get,...}`, a row iterator (`next`, `next_dirty`, per-row `DIRTY`), and row cells (`ghostty_render_state_row_cells_*`).
- Dirty tracking exists: `GhosttyRenderStateDirty` FALSE/PARTIAL/FULL, plus per-row dirty.
- Data kinds include `COLOR_BACKGROUND`, `COLOR_FOREGROUND`, `COLOR_CURSOR`, `COLOR_CURSOR_HAS_VALUE`, `COLOR_PALETTE` (`GhosttyColorRgb[256]`), `CURSOR_VISUAL_STYLE`, `CURSOR_VISIBLE`, `CURSOR_BLINKING`, `CURSOR_VIEWPORT_{X,Y,WIDE_TAIL}`, `OVERSCAN` and an image iterator.
- Kitty images are exposed as metadata plus raw pixels. The caller draws them.

**`formatter.h`** can emit `PLAIN`, `VT` and `HTML`.
- Options are `EMIT`, `UNWRAP`, `TRIM`, `SELECTION` and `EXTRAS`.
- HTML output is markup, not pixels, and rendering it would need a browser, so it is out.
- VT output (with the PALETTE and STYLE extras) could reserialise a screen to escape codes. That is useful for text export, not for pixels.

**`snapshot.h`** is a binary, CRC-protected full terminal state encoder and decoder (`ghostty_snapshot_encode*`, `ghostty_snapshot_decoder_*`). It exists to persist and resume terminals, and it is unrelated to our `snapshot` package.
- It is interesting as a possible future lossless recording format. Its stability across ghostty versions is unknown.

**`kitty_graphics.h`** says explicitly: "The API does not render or composite. It only provides pixels and metadata." Pixel data is RGBA or RGB, already decoded. PNG decode needs an installed decoder via `ghostty_sys_set`.

**Glyph protocol.** `terminal.h` has `GHOSTTY_TERMINAL_OPT_GLYPH_PROTOCOL = 25`, which is on by default. It handles APC register/query/clear sequences. No rasteriser API was found beyond the toggle.

### 2.2 Go binding facts

Source: `mitchellh/go-libghostty` main (see the caveat above).

- `RenderState` getters include:
  - `Colors()` (returns Background, Foreground, Cursor, CursorHasValue and a 256-entry Palette of `ColorRGB`)
  - `ColorPalette()`, `Cursor()`, `Dirty()`, `RowIterator()` and `ImageIterator()`
- `RenderStateRowCells` methods:
  - `Graphemes()`, `GraphemesInto`, `AppendGraphemes`
  - `StyleInto(*RenderCellStyle)`, whose fields are `Foreground`, `Background`, `HasForeground`, `HasBackground`, `HasStyling`, `Bold`, `Faint`, `Italic`, `Underline`, `Strikethrough` and `Inverse`
  - `FgColor()`, `BgColor()` (resolved RGB, nil means use the default), `Raw()` and `Selected()`
- The example `examples/render/main.go` renders ANSI text to stdout, not pixels.
- The binding's own `README` says there are no API stability promises.

### 2.3 Is there any pixel renderer or font stack usable as a library?

**No. Direct evidence.**
- The pinned `build.zig` ships these lib-vt artifacts: static, shared, wasm (`GhosttyLibVt.initWasm`) and xcframework.
- It has no renderer or font library target. The `ghostty-internal` library is described in a code comment as "not libghostty, only glue for the macOS GUI".
- Upstream's own words (Mitchell Hashimoto, [libghostty is coming](https://mitchellh.com/writing/libghostty-is-coming)):
  - `libghostty-vt` is "a zero-dependency (not even libc) library that provides an API for parsing terminal sequences and maintaining terminal state".
  - Later libs are planned, including "GPU rendering (provide us with an OpenGL or Metal surface and we'll take care of the rest)".
  - That is a GPU-surface renderer, not a software or CPU image renderer. No date is given, and the post says the C API "isn't ready yet".
- The wasm target of lib-vt is parser and state only. `coder/ghostty-web` (MIT) uses a patched ghostty WASM parser plus its own TypeScript renderer, i.e. the renderer is not part of libghostty.

**Using ghostty's own font and renderer code (option e): not realistic.**
- The font and renderer code lives in the app source and depends on freetype, harfbuzz, fontconfig and CoreText wiring.
- It would mean writing our own zig C-ABI glue, pulling those zig dependencies and tracking unstable internals. Ghostty is MIT, so the licence is fine, but the size and maintenance cost are not worth it for an APNG feature.
- I did not read `src/font` contents (the fetch failed). That is the one point in this section that is not verified.
- Researcher inference: ghostty's sprite-font approach (procedural box/powerline drawing) is the right model to copy conceptually, not to link.

**Upstream plans:** only the GPU-surface renderer mentioned above. I found no commitment to a software renderer or image export.

## 3. Options for producing pixels

### (a) Pure-Go rasteriser over x/image (recommended)

Components:
- **Text:** `golang.org/x/image/font/opentype` gives `opentype.NewFace`, whose `Glyph` returns an alpha mask and advance. Cache masks per (rune, face, size), then composite with `draw.DrawMask` and a uniform fg colour.
- **Cell grid:** fixed cell W×H from face metrics (advance of "M", ascent plus descent). Draw the bg rect, then the glyph, then decorations, per cell.
- **Shaping and ligatures:** x/image has no shaping. Per-cell rendering matches terminals that do not ligate, so skip ligatures (state this in docs).
- **Fonts and licences:**
  - JetBrains Mono is OFL-1.1. Embedding is allowed; ship the licence text, and do not use the reserved name for modified fonts. Freeze and agg both bundle it. Four faces (regular, bold, italic, bold italic) give the true style set.
  - Go Mono (`golang.org/x/image/font/gofont/gomono*`) is BSD-style and already in x/image. It is a zero-extra-licence fallback and a good default for the golden tests.
  - Nerd Font symbols (agg bundles `SymbolsNerdFont-Regular`) are OFL/MIT-ish. Optional fallback, not v1.
- **Procedural glyphs:** draw box drawing (U+2500–257F), block elements (2580–259F), braille (2800–28FF) and powerline (E0B0–E0B3 and similar) as filled rects and polygons using `x/image/vector` or integer rects. This avoids dependence on font coverage and aligns cells without gaps.
- **Wide characters and CJK:** use a cell-width table (a `go-runewidth` or `x/text/width` equivalent). A wide rune takes 2 cells. Embedding CJK is too big (agg bundles NotoSansCJKjp, which is large). Allow `--font-file` / `--fallback-font` and render a tofu box if no font covers the rune.
- **Colour emoji:** `opentype` in x/image does not decode CBDT/sbix/COLR colour glyph tables. Colour emoji is therefore not available in a pure-Go v1. This is an inference from my knowledge of x/image, not verified in this run, so confirm before promising anything. v1 fallback: monochrome Noto Emoji, or tofu.
- **Attributes:**
  - Bold, italic and bold-italic select face variants.
  - Faint blends fg 50% toward bg.
  - Inverse swaps fg and bg.
  - Invisible draws nothing.
  - Blink is treated as static.
  - Underline and strike are 1–2 px rects at font-derived positions.
- **Cursor:** block, bar and underline from the cursor state (block by default). Only draw it if visible. The shape is not in our `Cursor` struct yet (only X, Y, Visible).

### (b) cgo to CoreText, FreeType or HarfBuzz

- CoreText via cgo gives the best macOS quality, plus system font fallback, colour emoji and CJK. Cost: macOS-only, no reproducible goldens, and a dependency on installed fonts.
- FreeType and HarfBuzz are a heavy native chain (pkg-config, static linking). That contradicts "avoid heavy native dependency chains" and the single-binary goal.
- Fit: only as an optional later `font.Provider` behind a build tag.

### (c) Existing tools: borrow ideas, not code

| Tool | Licence | Notes | Reusable as a Go library? |
|---|---|---|---|
| asciinema/agg | GPL-3.0-or-later | Rust. GIF only, via gifski. Uses swash (default) or resvg. Bundles JetBrains Mono, Noto Color Emoji, Noto CJK and Nerd symbols. Supports idle-time limiting, speed, fps cap, last-frame duration. | No. GPL and not Go. Borrow the CLI semantics only. |
| charmbracelet/freeze | MIT | Go. SVG/PNG/WebP of a single screenshot. `go.mod` shows `github.com/kanrichan/resvg-go` (plus `wazero`) for rasterising SVG. Single frame only; no documented library API. | Not a library. The SVG then resvg-go pipeline is a data point for option (d). |
| charmbracelet/vhs | MIT | Uses a headless browser (ttyd and chromium). Excluded by your constraint. | No. |
| termtosvg, svg-term | (not checked) | SVG animations. Do not produce APNG. | No. |

I did not evaluate silicon (Rust) or `carbon`, which is web-based. Neither is a candidate given the constraints.

### (d) SVG-first then pure-Go rasterise

- Pros: SVG is a nice shareable artefact, and freeze proves the cgo-free path (resvg-go on wazero).
- Cons: an extra markup layer per frame, a multi-MB WASM blob in the binary, and slow rendering of hundreds of frames. Font handling inside resvg is fiddly. Per-cell control is lost.
- `oksvg`/`rasterx`: from memory (not verified here), oksvg does not render `<text>`, so it would need glyphs converted to paths. That is more work than option (a).
- Verdict: only worth doing if an SVG exporter is wanted for its own sake.

### (e) Ghostty's own font stack or renderer

See section 2.3. Not feasible or reasonable.

## 4. APNG encoding in Go

### 4.1 Library or own encoder

- **`github.com/kettek/apng`** (BSD-3-style, based on Go's `image/png`; I did not find the last-commit date):
  - It encodes multi-frame APNGs, with `Frame{Image, DelayNumerator, DelayDenominator, DisposeOp, BlendOp, XOffset, YOffset, IsDefault}` and `APNG.LoopCount`.
  - A custom `Encoder.CompressionWriter` hook is available.
  - Its README admits the encoder "doesn't handle differences of Image formats and similar and has not been tested as thoroughly".
  - It does not compute deltas, de-duplicate or quantise. We would write that logic anyway.
- **Own encoder (recommended):**
  - Encode each frame's sub-image (cropped delta rect) with stdlib `png.Encoder{CompressionLevel: png.BestCompression}` into a buffer.
  - Parse that buffer's chunks: take the IHDR (for dimensions) and concatenate the IDAT payloads.
  - Emit `signature, IHDR(full size), acTL, [fcTL#0, IDAT…], then for each later frame fcTL + fdAT(seq, data)…, IEND`.
  - Size: roughly 150–250 lines, plus a chunk parser and CRC (`hash/crc32`).
  - Pitfalls:
    - Sequence numbers are shared across fcTL and fdAT and must run 0,1,2…
    - `acTL.num_frames` must be known before the first IDAT, so buffer the whole output or run two passes.
    - Every fdAT is the frame's IDAT data with a 4-byte sequence prefix.
    - All frames share one colour type and PLTE. PLTE comes before the first IDAT and cannot be redefined per frame, so the palette must be global for indexed output.
    - `fcTL` regions must lie within the canvas.
    - Delay is `delay_num/delay_den` as uint16 each; use den=1000, which caps one frame at 65 s.
    - Frame 0 is the default image. If frame 0 is a full-canvas frame it gets the fcTL (animated, shown in the viewer); a hidden default image is not needed.
    - `dispose_op`: use NONE and `blend_op`: SOURCE for opaque delta rects.
    - `loop count 0` means forever.
  - Add a validity check in tests by decoding via `kettek/apng.DecodeAll` (a test-only dependency) and comparing the composited frames against the source images. The spec reference is the [Mozilla APNG spec](https://wiki.mozilla.org/APNG_Specification). My extraction of it failed, so the chunk rules above come from my knowledge of the spec and must be checked against it.

### 4.2 Optimisations for terminal content

- **Delta frames:** compute the bounding box of changed cells between consecutive frames, and encode only that rect with `x/y offset`, `BLEND_OP_SOURCE` and `DISPOSE_OP_NONE`. This is the single largest win. Compare at cell granularity, since we have the snapshots: the diff is just the cells whose rune or style changed, with no pixel scanning.
- **De-duplicate:** the session already emits only changed frames, but cursor-only or title-only changes can render identically. If the rendered image is identical, add its delay to the previous frame.
- **Indexed PNG:** terminal pixels are few colours, but anti-aliased text produces blends. Distinct colours are roughly (number of fg/bg pairs × AA levels). Options:
  - Global 256-colour palette built from the used theme colours plus quantised AA ramps. Exact for monochrome themes, lossy for very colourful output.
  - Truecolour RGB (colour type 2) as the lossless default.
  - Offer `--palette` / `--colors 256` as an opt-in size optimisation.
  - Researcher inference: start lossless RGB, measure, then add indexed.
- **Compression:** stdlib `compress/flate` at best compression is fine. A custom `CompressionWriter`, or klauspost/compress deflate (an extra dependency), would give a further ~5–10% (unmeasured; treat as an estimate). Use PNG filters via the stdlib encoder (it picks adaptively).
- **Size expectation, 80×24, 30 s (inference, not measured):** with 14 px font at 1× (about 680×410 px), a full frame is ~10–25 KB. Delta frames (one line changed) are ~0.5–3 KB. With about 100–300 changed frames the file is around 100 KB–1 MB. At 2× (retina) multiply by about 2–3×. Measure this in the walking skeleton.

### 4.3 Viewer support (brief)

- **Browsers:** [caniuse](https://caniuse.com/apng) lists support in Chrome 59+, Edge 79+, Safari 8+, Firefox 3+ and Opera 46+. Where unsupported, only the first frame is displayed.
- **GitHub READMEs:** a web search summary says APNG-capable browsers animate `.png` sources in Markdown. This is an indirect claim from a search summary, not verified first-hand. Check once with a test repo.
- **Slack:** unverified; the one source found points to GIF as the documented animated route. Treat APNG as non-animating there.
- **Discord:** APNG is supported for stickers; ordinary attachments may not animate (unverified).
- **Fallback:** GIF is the universal chat format. Keep the frame/timeline layer format-agnostic (`export/gif` can be added later with `image/gif` and a quantiser), and mention WebP/MP4 only as future options.

## 5. Timing model and architecture

### 5.1 Mapping `Frame.At` to APNG delays

Our frames are `At` timestamps in ms. Frame *i* is shown from `At[i]` until `At[i+1]`.

1. `raw_i = At[i+1] - At[i]`. The last frame has no successor, so use a final hold.
2. Idle cap: `d = min(raw_i, idleMax)`, default 2 s (like agg's idle-time limit; `--idle-limit`).
3. Speed: `d = d / speed` (`--speed`, default 1.0).
4. Minimum delay clamp: `d = max(d, 20 ms)`. Many viewers treat very small delays (≤10 ms) as 100 ms, so avoid anything below ~20 ms. This clamp behaviour is browser lore, not verified here, so test in Chrome, Firefox and Safari. Avoid losing time: when a frame is raised to the minimum, carry the remainder (subtract it from the next frame's delay), or merge such frames into the previous one. Drop-and-merge is the simplest approach: frames shorter than the minimum are folded into the previous delay when the rendered result is only an intermediate state.
5. Final hold: `--hold` (default 2 s); the last frame is held on screen, then the loop restarts.
6. Encode as `delay_num = ms`, `delay_den = 1000`. Cap at 65535 ms (guaranteed by the idle cap).
7. Loop count: default 0 (infinite), with `--loops N`. For one-shot plays use N=1 (note that "1 plus hold" is a common README choice).

Because the session samples on a ticker (`--interval`), animation fidelity is limited to the sampling interval. A 10–50 ms sampling interval is advisable for smooth spinners. Researcher note: a very small interval enlarges the `.shot` file but not the APNG, thanks to de-duplication.

### 5.2 Package breakdown (consumer-defined interfaces, no cgo)

```
internal/render/theme     // Theme{Fg,Bg,Cursor Color; Palette [256]RGB}; Resolve(snapshot.Color, role) -> color.RGBA; Default() (xterm-like or Ghostty default); pure
internal/render/font      // Set{Regular,Bold,Italic,BoldItalic *opentype.Font}; go:embed JetBrains Mono (+OFL.txt); Load(path) for --font-file; Metrics(size) ; no cgo
internal/render/sprite    // procedural glyphs: Draw(dst *image.RGBA, r rune, cell image.Rectangle, fg) bool  (box, block, braille, powerline)
internal/render/raster    // Renderer{Theme, Fonts, Scale, Padding}; Render(snapshot.Snapshot) *image.RGBA; Diff(prev, next snapshot) image.Rectangle
internal/export/timeline  // []snapshot.Frame -> []Step{Snapshot, Delay}; idle cap, speed, hold, min delay; pure, no image deps
internal/export/apng      // Encoder: AddFrame(img image.Image, rect image.Rectangle, delay time.Duration); Close(w io.Writer) ; chunk writer
internal/cli/export.go    // flags -> config; wires recording.Reader -> timeline -> raster -> apng
```

Dependency direction: `cli` → (`recording`, `export/timeline`, `render/raster`, `export/apng`); `raster` → (`snapshot`, `theme`, `font`, `sprite`); `timeline` → `snapshot`; `apng` imports only the standard library. `export/*` depends on nothing that needs cgo, so `GOFLAGS` / tests for these packages run without `PKG_CONFIG_PATH` and without zig. Define small interfaces at the consumer, for example `raster` declares `type Fonts interface{ Face(attrs snapshot.Attr) font.Face; CellSize() (w, h int) }` so a CoreText backend can slot in later.

### 5.3 CLI shape

```
shellshot export file.shot -o out.apng \
  [--theme default|<name>|<file>] [--font-size 14] [--scale 1|2] [--font-file path] \
  [--speed 1.0] [--idle-limit 2s] [--hold 2s] [--loops 0] [--min-delay 20ms] \
  [--cursor block|bar|underline|none] [--padding 8] [--colors rgb|256]
```

### 5.4 Theming

- Our spans carry `ColorDefault`, `ColorPalette` (index 0–255) or `ColorRGB`. Resolving them needs a theme.
- libghostty provides the live values: `RenderState.Colors()` returns Background, Foreground, Cursor (and `CursorHasValue`) plus the 256-entry palette. The ghostty adapter does not record them today.
- **Proposed:** add optional `theme` data to the recording header, captured once at record start: `{"fg":"#…","bg":"#…","cursor":"#…","palette":[256 hex or only overrides]}`.
  - This is a backward-compatible addition: the header is JSON and `version` stays 1, with the field optional.
  - It needs a small `vt.Terminal` extension, e.g. `Theme() (snapshot.Theme, error)`, implemented in `internal/vt/ghostty` from `state.Colors()`. The `snapshot` package stays dependency-free.
- Export precedence: `--theme` flag > the header's theme > a built-in default. A built-in default palette (e.g. xterm or Ghostty's default) must exist for old recordings.
- Indices 16–255 can be computed (6×6×6 cube plus grayscale ramp), so only the 16 base colours really need a theme.

## 6. Walking-skeleton plan (small atomic tasks)

Each task is independently mergeable and testable.

1. `export/apng`: chunk writer producing a valid 1-frame APNG from `image.Image` (signature, IHDR, acTL, fcTL, IDAT, IEND). Test: decode with stdlib `png.Decode` (shows the default image) and with `kettek/apng` (test-only).
2. `export/apng`: multi-frame with fcTL/fdAT, sequence numbers and delays. Test: `kettek/apng.DecodeAll` frame count, delays and loop count.
3. `export/apng`: sub-rectangle delta frames (`AddFrame` with a rect, source blend) and identical-frame merging. Test: recompose the frames and compare them to the originals pixel by pixel.
4. `export/timeline`: delay policy (idle cap, speed, min delay carry, final hold). Pure table tests.
5. `render/theme`: default palette, 256-colour cube and ramp, `Resolve`. Table tests.
6. `render/font`: embed JetBrains Mono (plus `OFL.txt` and a `NOTICE`), metrics at a given size. Test: cell metrics are stable.
7. `render/raster`: backgrounds and text for plain lines (no spans). Golden PNG test.
8. `render/raster`: spans (fg/bg, bold, italic, faint, inverse, underline, strike) and the cursor. Golden PNGs.
9. `render/raster`: cell-width handling and wide characters. This also covers the rune-index versus column issue below.
10. `render/sprite`: box drawing, block elements, braille, powerline. Golden PNGs (a character-sheet image).
11. `cli export`: wiring `recording.Reader` → timeline → raster → apng, with flags. Integration test using a fixture `.shot`.
12. Header theme capture: `vt.Terminal` gains `Theme()`, the ghostty adapter reads `Colors()`, and the recording header persists it (cgo test, runs in the build subagent).
13. Optional later: indexed output, `--font-file` fallback, CoreText backend (build tag), GIF exporter on the same `timeline`/`raster`.

**Golden-image tests.**
- Store PNGs in `testdata/`, add an `-update` flag, and compare with a tolerant comparator: at most N pixels differing, and each channel off by at most 2.
- Exact equality is risky: float32 fused-multiply-add differences between arm64 and amd64 can change anti-aliased edge pixels in the x/image rasteriser. This is my reasoning, not something I tested.
- Use the embedded fonts only (never system fonts) so goldens are stable.
- Also keep one character-sheet golden for sprites, and a frames-composition test for the encoder.

## 7. Risks and open questions

1. **Spans index runes, not cells.**
   - `snapshot.Span.Start/End` are rune offsets into `Lines[y]`, validated against `utf8.RuneCountInString(line)` (`snapshot.go`, `Validate`).
   - `Lines` come from libghostty's `AppendText` and are right-trimmed, so column positions are lost.
   - Wide characters (two cells), combining marks and grapheme clusters (one cell, several runes), and interior spaces/blank cells, all make rune index differ from the cell column.
   - The rasteriser must recompute columns from rune widths, and could disagree with libghostty's own width tables on edge cases (emoji ZWJ, variation selectors).
   - Mitigation: the export library should use one width function consistently, ideally the same Unicode data as libghostty. The `unicode` header group is in `vt.h`, but I did not check whether it exposes width functions; treat that as an open question. Alternatively, extend the snapshot with an explicit per-cell column or width. This is a format decision to make before the rasteriser is built.
2. **Theme is not recorded.** Without the header theme, exports use a generic default and may not match the recorded terminal (section 5.4). Fix in task 12.
3. **Cursor shape and blink are not in the snapshot** (`Cursor` has X, Y, Visible only). The block shape is the default; capture `CursorVisualStyle` later.
4. **Kitty graphics, hyperlinks and selection are not in our snapshots**, so they cannot appear in exports.
5. **Sampling fidelity.** Snapshots are taken on a ticker, so fast animation is quantised, and intermediate output between ticks is lost for good.
6. **Colour emoji and CJK** are unsupported in a pure-Go v1 without extra fonts (section 3a).
7. **Binding drift.** go-libghostty has no API stability promise, and the evidence above was read from its main branch. Re-verify at the pinned module version.
8. **Viewer behaviour** (Slack, Discord, minimum delays) comes from indirect sources; verify empirically with a sample file.
9. **File size** estimates are not measured.

## 8. Raw bytes versus snapshots as the export input

- **Current choice (snapshots).** Benefits: export is deterministic and independent of the libghostty version, needs no cgo, and the `.shot` file is small and trivially replayable. Costs: it is sampled (the interval above), carries no cell-column data or theme, and cannot re-render at a different size or re-theme from the original palette.
- **Raw bytes (asciicast-like event log) plus re-feeding libghostty at export.** This would give exact timing, re-theming, resizing and Kitty graphics. It would also make the export binary depend on libghostty and cgo, and it needs a new format (v2) with resize events.
- **Recommendation:** build the exporter on snapshots now, but add the header theme and a per-cell width/column representation to the format. Keep the `timeline` + `raster` input as `[]snapshot.Frame`, so a future "replay raw events through libghostty to produce frames at export time" feature plugs in at the front without touching the rendering code.

## Sources

Kept:
- ghostty `vt.h` at the pinned commit: https://raw.githubusercontent.com/ghostty-org/ghostty/66af4c7458db7e19ae7adb44aa7054879be58b6b/include/ghostty/vt.h (API groups, build features)
- `render.h`, `formatter.h`, `snapshot.h`, `kitty_graphics.h`, `terminal.h`, `build.zig` at the same commit and path pattern, under `include/ghostty/vt/` (render state, formatter, kitty, glyph protocol toggle, artifacts). Read via summarising extraction, not line by line.
- go-libghostty (main): https://github.com/mitchellh/go-libghostty, files `render_state_cell.go`, `render_state_data.go`, `examples/render/main.go`
- libghostty roadmap: https://mitchellh.com/writing/libghostty-is-coming
- coder/ghostty-web (MIT, WASM parser plus own renderer): https://github.com/coder/ghostty-web
- asciinema/agg (GPL-3.0+, GIF, bundled fonts): https://github.com/asciinema/agg
- charmbracelet/freeze (MIT, resvg-go in `go.mod`): https://github.com/charmbracelet/freeze and https://raw.githubusercontent.com/charmbracelet/freeze/main/go.mod
- kettek/apng: https://github.com/kettek/apng and https://raw.githubusercontent.com/kettek/apng/master/LICENSE
- caniuse APNG: https://caniuse.com/apng
- APNG spec (cited, extraction failed; confirm manually): https://wiki.mozilla.org/APNG_Specification
- Local repo: `docs/ARCHITECTURE.md`, `internal/snapshot/snapshot.go`, `internal/snapshot/style.go`, `internal/vt/ghostty/terminal.go`, `internal/vt/ghostty/snapshot.go`

Rejected or deprioritised:
- charmbracelet/vhs: browser-based (known to me, not re-checked here).
- Search-engine summaries on Slack, Discord and GitHub APNG behaviour: indirect, so treated as low confidence.

## Missing evidence

- The pinned `third_party/ghostty` submodule and the Go module cache were not read (no shell). Headers and binding were read from GitHub, and the binding from main, not the pinned version.
- Ghostty's `src/font` and sprite code was not read.
- Whether `unicode.h` exposes grapheme or width functions.
- Measured APNG sizes, viewer minimum-delay behaviour, and real Slack, Discord and GitHub behaviour.
- The last-commit date of kettek/apng, and whether it works cleanly with Go 1.26.
- Whether x/image `opentype` handles colour glyph tables (from memory: no).
- Verification that the APNG spec rules above match the spec text exactly.

## Next steps

1. Run `go list -m -f '{{.Dir}}' go.mitchellh.com/libghostty` and `git submodule update --init third_party/ghostty`, then re-check the points above against the pinned sources.
2. Decide the format question in risk 1 (per-cell column data and the header theme) before building the rasteriser.
3. Start with tasks 1–4, which are pure Go and have no format dependency.

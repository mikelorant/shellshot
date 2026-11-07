# Research: SVG export for shellshot

Date: this run. **Important limitation:** this run had no shell tool (only file read/write and web tools). Nothing
could be executed, so **no sizes below are measured**; every number is an *estimate* from stated arithmetic.
An unrun prototype generator is at `/tmp/svgproto/main.go` (header has the run/validate/render commands). Run it
first to replace the estimates in section 5 with measurements. Repo files were only read.

## 1. Summary and recommendation

Build an SVG exporter that works **from snapshots, not raster frames**, in two shapes sharing one builder:

1. **Static SVG** (`export x.shot -o x.svg --frame last`): `<text>` in a font stack, one `<tspan x=…>` per style run,
   background `<rect>` runs, cursor `<rect>`, presentation attributes only (no `<style>`, no CSS). Roughly 2–6 KB
   for an 80x24 screen (estimate). Works in nearly every viewer, including static-only ones.
2. **Animated SVG** (`-o x.svg` default for `.svg`): **diff-based row strips** driven by CSS `@keyframes` with
   `steps(1,end)`. Each screen row holds its distinct line variants stacked vertically inside a clipped nested `<svg>`.
   One keyframe list per row translates the strip to the right variant. The cursor is one more stepped `translate`.
   This is termtosvg's proven mechanism (stepped `translateY`, shared `<defs>`) with the redundancy removed.
   Estimated about 20–25 KB for a 30-step 80x24 coloured session, against 120 KB for per-screen `<g>` groups
   (section 5).
3. **Text mode by default**, `--svg-text paths` as the opt-in exact-look mode (glyph `<path>` in `<defs>` plus `<use>`,
   from `x/image/font/sfnt` outlines, which we already depend on). Do **not** embed WOFF2 in v1 (needs a subsetter and
   brotli; Go stdlib has neither).
4. Two new flags only: `--svg-text text|paths` and `--frame TIME|first|last` (when given, output is static).
5. SVG needs a **snapshot-level path**: factor the shared front half of `pipeline.Export` (read frames, `timeline.Build`)
   into a function that returns `[]timeline.Step`; the raster/APNG sink and the SVG writer are two consumers.

Compatibility headline (section 6): animated SVG in `<img>` is allowed by the SVG spec's "secure animated mode"
(CSS and SMIL run, scripts and external loads do not). Real-world support varies by viewer and is partly
unverified here. Docs should say "animated SVG in browsers and GitHub READMEs; elsewhere you get the first frame or
nothing" and keep APNG/GIF as the chat-app answer.

## 2. Evidence (what was actually checked)

1. **Claim:** SVG used as an image has JavaScript disabled and cannot load external resources, but inline `data:` URLs
   work. **Sources:** [MDN: SVG as an image](https://developer.mozilla.org/en-US/docs/Web/SVG/Guides/SVG_as_an_image)
   (last modified 2025-05-13). **Support:** direct. **Confidence:** high. Consequence: no `<script>`, no `@import` or
   `url(http…)` fonts; an embedded font must be a `data:` URI inside `@font-face`.
2. **Claim:** the SVG 2 conformance text defines a "secure animated mode" for image contexts: declarative animation (CSS,
   SMIL) allowed, scripts/interaction/external loads not. **Sources:** [SVG 2 conformance](https://www.w3.org/TR/SVG/conform.html),
   surfaced via a search summary, not read line by line. **Support:** interpretation. **Confidence:** medium.
3. **Claim:** SMIL in an `<img>` does not start until page load completes, whereas CSS animation starts while loading;
   WebKit browsers mis-render both when the page zoom is not 100%. **Sources:**
   [svg-spinners README](https://github.com/n3r4zzurr0/svg-spinners/blob/main/README.md) (fetched; its issue #2 is linked, not read).
   **Support:** direct (project author's statement, one project). **Confidence:** medium. Implication: prefer CSS keyframes over SMIL.
4. **Claim:** GitHub's image proxy (camo) once sent `Content-Security-Policy: default-src 'none'`, which blackened SVGs with
   inline `style` attributes; fixed 2014-04-17 by adding `style-src 'unsafe-inline'`. **Sources:**
   [github/markup#289](https://github.com/github/markup/issues/289). **Support:** direct but **old (2014)**; current policy
   unverified. **Confidence:** medium for "inline styles and `<style>` were intentionally allowed", low for today.
   A relative-path README image is not necessarily served via camo (unverified). Implication: keep the static SVG free of
   `<style>`; keep the animated SVG's CSS in one `<style>` and have no `url()` references except `#id` fragments and data URIs.
5. **Claim:** termtosvg renders **every frame as a full `<g>`, stacks them vertically in `#screen_view`, and scrolls that
   group with `@keyframes roll { t%{transform:translateY(-offset)} }` and `animation-timing-function: steps(1,end)`,
   `animation-iteration-count: infinite`**. Lines are deduplicated by serialised content: the text group is stored once in
   `<defs>` with id `g<N>` and referenced by `<use y=…>`. Consecutive same-style cells merge into one `<text>`/`<rect>`. The
   code comments that offsets are made even because of one-pixel line jumping seen in Firefox. A WAAPI/JS template also exists
   (not usable in `<img>`). **Sources:**
   [termtosvg anim.py](https://raw.githubusercontent.com/nbedos/termtosvg/master/termtosvg/anim.py) (fetched; summarised by the fetch tool).
   **Support:** direct. **Confidence:** high.
6. **Claim:** termtosvg's README says an animation is embedded on GitHub "with a relative link", i.e.
   `![Example](./docs/examples/x.svg)`. **Sources:** [termtosvg README](https://github.com/nbedos/termtosvg).
   **Support:** direct, but it asserts usage and not a tested compatibility matrix. **Confidence:** medium.
7. **Claim:** freeze (static SVG) builds the document with `etree` and uses `<rect>` clip paths, filters for shadow, `<circle>` window
   controls. **Sources:** [freeze svg.go](https://raw.githubusercontent.com/charmbracelet/freeze/main/svg/svg.go). **Support:**
   direct. **Confidence:** high. We do not need `etree`: stdlib `strings.Builder`/`encoding/xml` escaping suffices.
8. **Claim (existing repo):** `docs/research/apng-export.md` already ranks "SVG-first then rasterise with resvg-go" third. This brief
   is about *deliverable* SVG, not an intermediate. **Support:** direct (repo doc).
9. **Repo facts read (direct):** `snapshot.Snapshot` has `Lines`, rune-indexed `Spans` (`Style{Fg,Bg,Attrs}`), sparse `Widths`
   (`Cells` 0 or 2), `Columns(row)` and `CellWidth`, `Cursor{X,Y,Visible}` and **no cursor shape or blink**, which is
   therefore an export option (`--cursor`). `theme.Theme.Colors(style)` already resolves Inverse, Faint (50% blend) and Invisible to
   concrete RGBA. `timeline.Build` returns `[]Step{Snapshot, Delay}` after dedup and min-delay folding. `font.Metrics`
   gives integer `CellWidth/CellHeight/Ascent/UnderlineOffset` for a pixel size. `sprite.Draw(dst *image.RGBA, r, cell, fg)` paints **pixels**,
   so its geometry is not reusable for vectors without a seam (section 4.3). `pipeline.Export` mixes frame reading, timeline and
   raster/APNG in one function and its `Renderer` returns `*image.RGBA`.

## 3. Design choices with reasoning

### 3.1 Animation approaches (estimates in section 5)

| Approach | How | Pros | Cons |
|---|---|---|---|
| A. Per-screen `<g>` + SMIL `<set>` on `visibility` | one `<g visibility="hidden">` per distinct screen, `<set begin="Ns" dur=… attributeName="visibility" to="visible">` | simple; no CSS | size grows with screens x lines; SMIL waits for page load in `<img>` (item 3); SMIL looping needs `begin="x.end"` chains; ~30 elements animated per step in total |
| B. termtosvg-style frame strip | `<defs>` of unique lines, per-frame `<g>` of `<use>`, one `translateY` keyframe list | proven, one animated element, `steps()` | still one `<use>` per non-empty row per frame |
| **C. Row strips (diff)** | per row: stacked distinct variants, own keyframe list | lowest size for typical sessions; cost proportional to what changes; no per-frame markup | up to 24 animated elements; keyframe percentages need ~0.01% precision for long sessions (see risks) |
| D. Per-line visibility lifetimes | each line variant `visibility` via keyframes | same info as C | one keyframe list per *variant*, many CSS rules; worse than C |
| E. Scrolling tricks (whole-screen `translateY` for scroll) | translate a tall text block | tiny for long scrolling output | only helps pure scroll; needs terminal scroll detection; skip |

Choice: C, built from the same line-variant dedupe as B. A frame-strip fallback is unnecessary.
All use `steps(1,end)`: the value jumps at each keyframe with no interpolation.

### 3.2 Text versus paths versus embedded font

| Mode | Look | Size | Selectable | Font dependency | Verdict |
|---|---|---|---|---|---|
| `text` (default) | host's monospace; our grid kept by per-run `x` | smallest | yes, searchable | yes, drifts if the family is missing | default for docs |
| `paths` | exact JetBrains Mono look, same glyphs as PNG | about 4–6x text (estimate) | no | none | opt-in |
| `embed` (WOFF2 `data:` in `@font-face`) | exact + selectable | text + a font subset (tens of KB, estimate) | yes | none, if the viewer honours data-URI fonts in `<img>` (spec/MDN allow; per-viewer unverified) | **defer**: no Go WOFF2/subsetter; brotli only via third-party `andybalholm/brotli` |

**Grid exactness (text mode).** Per-cell `x` lists (`x="0 9 18 …"`) cost ~3–4 bytes per character and are exact in every
renderer, but double the size. Recommendation: **one `<tspan x=…>` per style run** (a run restarts the grid, so
drift is bounded by run length), set `font-family` stack `'JetBrains Mono', ui-monospace, 'SF Mono', Menlo, Consolas,
'DejaVu Sans Mono', monospace`, `xml:space="preserve"` (or `white-space:pre`). Emit per-character `x` **only** for runs containing a rune whose
cell width is not 1 (wide CJK, emoji, combining) via `snapshot.Columns`. `textLength`+`lengthAdjust="spacing"`
fixes widths in browsers but per-renderer behaviour is unverified; offer it only if the pixel test shows drift
(open question). `letter-spacing` is rejected: it cannot fix a fallback font whose advance differs from the cell width unless we know it.
If a viewer substitutes a font with a different advance, the screen stays aligned at run starts but not inside runs; this is the main
visible cost of text mode, and the reason `--svg-text paths` exists.

**Decorations and attributes.**
- Backgrounds: `<rect>` per horizontal run of equal resolved bg, skipping the theme bg (one full-size rect covers it).
  Lines are right-trimmed, so trailing bg is lost, as in PNG (documented in `export.md`).
- Underline/strike: `<rect>` of 1 px (or `Metrics`-derived) height at `baseline + UnderlineOffset` and mid x-height; avoids
  `text-decoration`, which varies by renderer and per tspan.
- Bold/italic: `font-weight="bold"`, `font-style="italic"`; in paths mode choose the bold/italic face from `font.Set`.
- Faint, Inverse, Invisible: resolved by `theme.Colors` to a concrete colour. No `opacity` needed.
- Colours: static output uses `fill="#rrggbb"` attributes. Animated output may use CSS classes for the 16 theme colours
  (`.c1{fill:…}`). **Re-themeable via CSS variables** (`fill:var(--c1,#f7768e)`) works only when inlined in HTML, because an `<img>`
  cannot be styled from outside; value is low, so v1: no variables, palette resolved at export time. Possible later `--svg-vars`.
- Cursor: `<rect>` (block), 2 px bar or underline-height rect, from `--cursor`. Static: drawn at snapshot cursor if
  `Visible`. Animated: an element with stepped `translate`, hidden when invisible (keyframe `visibility`). Blink is optional
  and off by default (CSS opacity `steps(1)` 1 s; mixing it with the move animation needs a nested `<g>`). Block cursor draws
  *over* text, so the character under it should be redrawn in bg colour as the raster does; check `raster` cursor code for the rule (not read).
- Box drawing/blocks/braille/Powerline: font glyphs have gaps at some cell sizes, which is why `sprite` exists. For SVG they should
  be `<rect>`/`<path>` from the same geometry. See 4.3 for the seam; in v1 the fallback is to render them as `<text>` (accept gaps)
  and make sprite a follow-up.
- Wide CJK: `<text x=colX>` single char, in a 2-cell slot. Fallback font family list should include `'Noto Sans Mono CJK SC'`,
  `'PingFang SC'`, `'Microsoft YaHei'`. Paths mode can reuse `fontchain` fallbacks (outline only).
- Colour emoji: `<image href="data:image/png;base64,…" x y width height>`; define once in `<defs>` per unique cluster and `<use>`. Source: the
  existing bitmap renderer (`render/emoji`) downscaled to 2x cell. Estimated 5–15 KB base64 each for a 128-px PNG (unverified); budget this
  as the dominant cost of emoji-heavy output and cap with `--emoji-set`. Never leave emoji to the host font in `paths` mode.
- Flags mapping: compute `font.Metrics(fontSize*scale)` + `--adjust-cell-*` exactly as raster, so geometry equals the PNG. Use the scaled
  integers for the `viewBox`, and set `width`/`height` attributes to `viewBox/scale` so a `--scale 2` file is the same logical size and
  crisp (vector, so scale is only a coordinate precision/size knob). `--padding` is the offset of the screen `<g>` (scaled).
  `--theme` supplies fg/bg/palette; `--cursor` the shape; `--font-size` the `font-size` attribute (the em is not the cell height).
  Timing flags (`--fps --speed --idle-limit --hold --min-delay --loops`) work unchanged via `timeline.Build`; `--loops N` maps to
  `animation-iteration-count: N` (0 = `infinite`). The final hold lives in the last step's delay, so a finite loop stops on the last frame via
  `animation-fill-mode: forwards`.

## 4. Go implementation design

### 4.1 Packages

```
internal/export/svg          writer + builder for the document (stdlib only)
  doc.go       Options, Meta{Cols,Rows,Title}, Write(w, []timeline.Step, Options) and WriteStatic(w, snapshot, Options)
  layout.go    cell geometry from Metrics: cell W/H, baseline, underline y; coordinates formatted with strconv.AppendFloat
  lines.go     snapshot line -> []Run{Col, Cells, Text, Style} (uses Snapshot.Columns/CellWidth and Spans; merges equal styles)
  text.go      Run -> <tspan>/<text>; paths.go Run -> <use href=#gN>; glyphs.go outline -> path d via a small GlyphSource interface
  anim.go      variant dedupe per row, keyframe CSS, cursor keyframes
  rects.go     background runs, underline/strike, cursor
```
Dependencies: `snapshot`, `export/timeline` (Step), `render/theme`. Geometry arrives through small consumer-declared interfaces so
`export/svg` need not import `raster` or cgo: `type Metrics struct{CellW, CellH, Ascent, UnderlineOffset int}` copied in by `cli`, and
`type Glyphs interface{ Outline(r rune, bold, italic bool) (d string, advance float64, ok bool) }` (only for paths mode,
implemented by an adapter around `render/font`/`fontchain`), plus `type Emoji interface{ PNG(cluster string) ([]byte, bool) }`.

### 4.2 Plumbing into the format registry

Keep `Format{Name, Extensions, ...}` as the registry unit and add *one* optional capability checked by type assertion, so APNG code is untouched:

```go
// in export/format (consumer side)
type StepWriter interface { // snapshot-level exporters
    WriteSteps(ctx context.Context, w io.Writer, steps []timeline.Step, m Meta, o Options) error
}
```
`cli` flow: parse flags -> `pipeline.ReadSteps(ctx, src, timelineOpts)` (new function extracted from the first half of `Export`;
returns `[]timeline.Step` or "recording has no frames") -> if the format implements `StepWriter` call it, otherwise run the existing
raster sink. Atomic write and "refuse to overwrite input" stay in the shared `cli` code. `--frame` selects one step by cumulative delay
(`TIME` is on the *output* clock after speed/idle policy, documented) or `first`/`last`. Combining `--frame` with `.png` is an error until
raster supports it. Flags `--svg-text` and `--frame` are registered only once; others are shared. Unsupported-combination errors:
`--fallback-font` etc. are accepted in paths mode and ignored in text mode with a one-line stderr note.

### 4.3 Reusing sprite geometry

`sprite.Draw` takes `*image.RGBA`, but internally it already goes through a `canvas` with a cell origin and size and integer geometry (read only the top of
`sprite.go`). Smallest seam: introduce `type Painter interface{ FillRect(x0,y0,x1,y1 int, a uint8) }`-style recording in `sprite` (a second
entry `Shapes(r, w, h) ([]Rect, ok)` for rect-only glyphs: blocks, braille dots as rects, straight box lines). Diagonals and rounded corners
(arcs, Powerline triangles) are antialiased pixels there; they need `<path>` output and are separate tasks. Unrelated to the SVG exporter's first release.

### 4.4 Determinism, tests, budgets

- Determinism: no maps iterated for output; ids by first occurrence (`g1`, `r0`); fixed-precision number formatter (`%.2f` trimmed); no timestamps or
  tool version in the file (maybe a fixed `<!-- shellshot svg v1 -->`).
- Golden tests: (1) byte-for-byte golden of a small hand-made `[]Step` in `testdata/*.svg` (updated with `-update`);
  (2) structure tests with `encoding/xml` decoder: well-formed, root `viewBox`, counts of `<rect>`/`<tspan>`/`@keyframes`, row count
  equals Rows, no `<script>`, no `href` that is not `#id` or `data:` (guards the `<img>` rules); (3) pixel tests: rasterise the **paths**
  mode with `srwiley/oksvg`+`rasterx` (test-only dependency; **its coverage of `<text>` is believed to be none and should be checked**) and compare with the
  `raster` PNG of the same snapshot within a tolerance (anti-aliasing differs), for text mode compare structure only because font
  availability differs; for animation, render the SVG at fixed times by *building static SVGs* (`WriteStatic` for step i) and also assert that
  the CSS keyframe offsets at each step time pick the right variant (pure arithmetic test).
- Budgets enforced in tests: static 80x24 typical < 8 KB; 30-step coloured text-mode animation < 40 KB; paths mode < 6x text. A budget failure
  prints the section counts (rects/tspans/defs bytes).

### 4.5 Atomic task plan (each is one small PR)

1. Extract `pipeline.ReadSteps` from `Export`; no behaviour change (tests exist).
2. `export/svg` layout + line-to-run builder with tests (Spans, Widths, Columns, bg merge).
3. Static text-mode writer + `--frame` + `StepWriter` hook in `export/format` + CLI wiring + docs/export.md section.
4. Underline/strike/cursor/faint/inverse conformance tests vs theme.Colors.
5. Animated writer: variant dedupe, row keyframes, cursor keyframes, `--loops`; size-budget tests.
6. Sprite `Shapes()` seam for rect-only glyphs; use in SVG.
7. Paths mode (glyph outlines via `sfnt.LoadGlyph`, `<defs>` dedupe, fallback chain).
8. Emoji `<image>` defs.
9. Manual compat pass on the viewers in section 6; update the table; README blurb.

## 5. Size estimates (ESTIMATED, not measured)

Assumptions: 80x24; 30 steps; 15 non-empty rows; avg 40 chars/row; 4 style runs per row; each step changes ~2 rows (typing +
output), so 15 + 2*30 = 75 distinct line variants. Line markup: `<text y="…"><tspan x="…" fill="…">…</tspan>…</text>`, ~45 B per run
+ ~60 B wrapper = ~240 B per line in text mode; screen ≈ 15 x 240 + ~15 bg rects x 50 = ~4 KB.

| Output | Arithmetic | Estimated raw size |
|---|---|---|
| Static, text | 4 KB + header | **~4 KB** (gzip ~1 KB) |
| Static, paths | 600 chars x ~28 B `<use>` + ~90 glyph defs x ~400 B | ~50 KB |
| Anim A: per-screen `<g>` + SMIL | 30 x 4 KB + 30 `<set>` x 110 B | ~125 KB |
| Anim B: frame strip (termtosvg) | 75 x 240 B lines + 30 x 15 `<use>` x 28 B + CSS | ~32 KB |
| **Anim C: row strips (recommended)** | 75 x 240 B + ~60 row keyframes x 40 B + 30 cursor keyframes x 45 B + wrappers 24 x 120 B | **~25 KB** |
| Anim C, paths | 75 lines x ~40 chars x 28 B + 36 KB defs | ~120 KB |
| 30 s real session (about 300 steps, text) | C scales with changes: ~300 x (2 lines x 240 B + 2 keyframes x 40 B + cursor 45 B) | ~190 KB; B: ~300 x 15 x 28 B + lines ~ 200 KB+; A: ~1.2 MB |

Gzip on repetitive SVG is typically 5–10x (general experience, unverified here); GitHub/CDN transfer compression for SVG is unverified. Compare
with APNG delta frames for the same session, which are orders of magnitude bigger (see `apng-export.md`; not re-measured).
Keyframe percentage precision: for a 30 s timeline two steps 20 ms apart are 0.067%, so print `%.3f`; at `%.2f` they would collide. Use `%.3f` (adds ~1 B each).

## 6. Viewer compatibility (animation of an SVG loaded in `<img>` or equivalent)

"Verified" means a primary source read in this run; everything else is explicitly unverified and must be tested in task 9.

| Viewer | CSS keyframes | SMIL | Evidence / status |
|---|---|---|---|
| Browsers, `<img src=x.svg>` | yes (spec: secure animated mode) | yes, but starts after page load | spec text + svg-spinners README (items 2, 3). Chrome/Firefox/Safari assumed; WebKit bug at non-100% zoom reported for both (item 3). **Partly verified** |
| Browsers, opened directly / `<object>` | yes | yes | MDN says restrictions do not apply there (item 1) |
| GitHub README (`<img>`) | reportedly yes | unclear | CSP once blocked inline styles then allowed `style-src 'unsafe-inline'` (2014, item 4); termtosvg documents README embedding (item 6); search summaries called SMIL "inconsistent". **Unverified for 2025/26: test in a throwaway repo** |
| macOS Quick Look / Preview | unknown (likely static first render) | unknown | WebKit supports SVG animation, but the Quick Look generator behaviour is **unverified**; `qlmanage -t` is a static thumbnail only. Document as "may be static" |
| Slack / Discord | not expected | not expected | **Unverified**; both are believed to not preview SVG uploads inline (the search timed out). Keep APNG/GIF for chat |
| VS Code (Markdown preview / image viewer) | expected yes (Chromium webview) | expected yes | **Unverified**; markdown preview may block remote SVG by CSP |
| Static consumers (Figma, Inkscape, resvg, rsvg, oksvg) | ignore animation; often skip `<style>` or CSS transforms | mostly ignore | Static output must therefore not depend on CSS. Animated file should show the **first frame** when animation is stripped: put the first-variant default (translate 0) and cursor at its first position as the base state. **Design rule, unverified in practice** |

## 7. Contradictions and caveats

- Search summaries disagree on GitHub: one says SMIL "may play in certain cases" and recommends GIF, another says use SMIL because CSS from inside
  an image "may be less reliable". Neither is a primary source. The 2014 camo issue and termtosvg's documented README embedding point toward
  CSS working. Treated as unresolved.
- The camo CSP is only known for 2014 and for the open-source camo (`pypi/camo` summary says `style-src 'unsafe-inline'` default). Current policy not checked.
- `docs/research/apng-export.md` prefers rasterising for PNG; nothing here contradicts it.

## 8. Risks and open questions

- **Font drift in text mode** is the main fidelity risk; mitigated by per-run x and the paths mode. Decide whether to ship `textLength` after seeing drift.
- **Non-100% zoom bug** in WebKit `<img>` (item 3) may make animations flicker for some users; can't fix.
- **Long sessions:** 24 CSS rules + many keyframes can slow some renderers; cap with `--min-delay` default 20 ms -> maybe a higher default for SVG (open).
- **Apple emoji licence** applies to SVG as well (already in `export.md`); for `--emoji-set apple` embed the notice, or default SVG to noto? Open.
- Static SVG selectable text includes layout whitespace; no `aria`/`<title>` yet. Add `<title>` from `Snapshot.Title`? Open.
- Atomic write: reuse the cli code. Resize mid-recording: same error as APNG.
- `--frame` semantics on output clock vs recording clock: pick output clock; confirm.
- Whether the `Format` registry's final shape (parallel work) accepts the `StepWriter` assertion; adapt names if not.
- Is Style on trailing spaces ever present? Lines are right-trimmed so bg on trailing cells is lost (existing limitation).

## 9. Sources

- Kept: [MDN SVG as an image](https://developer.mozilla.org/en-US/docs/Web/SVG/Guides/SVG_as_an_image) — restrictions in image context, data URIs.
- Kept: [termtosvg anim.py](https://raw.githubusercontent.com/nbedos/termtosvg/master/termtosvg/anim.py) — primary design of CSS frame strip.
- Kept: [termtosvg README](https://github.com/nbedos/termtosvg) — README embedding statement.
- Kept: [svg-spinners README](https://github.com/n3r4zzurr0/svg-spinners/blob/main/README.md) — SMIL vs CSS in `<img>`, zoom bug.
- Kept: [github/markup#289](https://github.com/github/markup/issues/289) — camo CSP and fix (2014).
- Kept: [freeze svg.go](https://raw.githubusercontent.com/charmbracelet/freeze/main/svg/svg.go) — static SVG prior art with etree.
- Kept: [SVG 2 conformance](https://www.w3.org/TR/SVG/conform.html) — secure animated mode (via search summary only).
- Deprioritised: GitHub Docs "non-code files" via source_check summary (not page text); `svg-creator-skill`, `living-canvas`, `pixel-art` repos (blog-style, unvetted); svg-term (marionebl) source URL 404'd, so its mechanism is not described here.

## 10. Next step

Run `/tmp/svgproto/main.go` (go run, `xmllint --noout`, render with `uv run --with resvg-py` or rsvg-convert; `qlmanage -t -s 800` for static
Quick Look), replace section 5 with measurements, and test the produced `anim.svg` in a scratch GitHub repo README, Safari, Slack and Discord to fill
section 6.

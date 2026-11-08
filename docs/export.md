# Export reference

`shellshot export FILE [-o OUT] [flags]` renders a recording as an animated PNG (the
default), an animated GIF, an animated WebP or an SVG. Browsers,
GitHub, Slack and Discord animate it; at the time of writing, macOS Preview and Quick Look show
only one image, which export makes the end of the recording (see [Poster](#poster-png)). Run `shellshot export --help` for the flag list.

## Output format

`--format NAME` chooses the output format. Without it, the format comes from the extension of
`-o` (case-insensitive), and is `png` when there is no `-o` or `-o` has no extension. An
extension that no format owns, or an unknown `--format` name, fails with the list of supported
formats (`supported: png, gif, svg, webp`); `--format` then lets you write any file name.

| Format | `--format` | Extensions | Notes |
| --- | --- | --- | --- |
| Animated PNG | `png` | `.png` | The default. Full colour. Plays in browsers, GitHub, Slack and Discord. |
| Animated GIF | `gif` | `.gif` | At most 255 colours plus transparency. Plays everywhere. See [GIF](#gif). |
| SVG | `svg` | `.svg` | Vector text, animated with CSS (or one still with `--frame`). Small for text, but animation support in viewers varies; see [SVG](#svg). |
| Animated WebP | `webp` | `.webp` | Lossless, smaller than PNG. Plays in current browsers; see [WebP](#webp) for sharing sites. |

## GIF

`--format gif` (or an `-o` ending in `.gif`) writes an animated GIF that plays in every browser,
chat tool and image viewer.

- **256 colours.** A GIF has one palette of 256 entries, one of them transparent, so a frame has
  at most 255 colours. Export counts the colours of the whole animation. If they fit, the GIF is
  exact. If not, which is usual because anti-aliased text has hundreds of blends (a 7-colour text
  session had about 1,000 colours), export builds one palette for all frames by a
  popularity-weighted median cut. Plain text stays visually unchanged (the measured error was
  about 0.01 of 255 levels on average), because the rare colours it merges are few pixels.
- **Emoji banding.** Colour emoji have smooth gradients, so a GIF with emoji bands slightly
  (stepped colours in the gradients of a face). `--gif-dither` hides the steps with a
  Floyd-Steinberg dither, at the price of visible grain, a 6 to 25 % larger file and
  several times the encode time (still under a second for the recordings measured). Plain text shows no benefit, so it is off by default. `--gif-colors N` (2 to 255, default
  255) lowers the palette size for smaller files and coarser colour.
- **Size.** Only the rectangle that changed is stored for each frame, and identical frames
  merge. On the recordings measured, the GIF was about 2 to 2.5 times smaller than the PNG of the same
  recording because it has 8 bits per pixel. Smooth gradients are where a GIF loses the most
  quality.
- **Timing.** A GIF delay is a whole number of centiseconds, so every frame lasts at least 2 cs
  (20 ms), because browsers show shorter frames as 100 ms. Rounding errors carry to the next
  frame, so the total length stays within 1 cs. Holds longer than 655.35 s are split.
- **Looping.** `--loops 0` (default) loops forever, `--loops N` plays the animation N times.
- `--gif-colors` and `--gif-dither` are ignored for other formats, and `--gif-dither` does
  nothing when the palette is exact.

| Flag | Default | Meaning |
|---|---|---|
| `--gif-colors N` | 255 | Largest number of picture colours, 2 to 255 |
| `--gif-dither` | off | Dither when the animation has more colours than the palette |

## WebP

`--format webp` (or `-o out.webp`) writes an animated lossless WebP. The pixels are exactly the
ones the PNG has: there is no lossy mode and no quality flag, because lossy WebP smears the
coloured one-pixel strokes of terminal text.

- Each frame after the first stores only the rectangle that changed since the previous frame,
  with "do not blend" and no disposal, so every frame recomposes exactly. WebP stores a frame's
  offset in units of two pixels, so a rectangle starts at the even coordinates up and left of the
  first changed pixel and may be one pixel larger than the PNG's.
- A frame is never shorter than 10 ms; shorter delays are raised to it (`--min-delay` defaults to
  20ms, so this only matters when you lower it). Whether a given player clamps short delays
  itself was not verified. One frame can last up to 16777215 ms (about 4.6 hours); longer holds
  are split.
- `--loops` is stored in 16 bits, so the WebP maximum is 65535 (the PNG has no such limit).
- The canvas is at most 16384 pixels in each direction (a limit of the lossless bitstream).
- Exports are opaque, so the file does not set the WebP alpha flag.
- Encoding is pure Go (the lossless encoder of `github.com/HugoSmits86/nativewebp`, MIT). It adds
  about 0.3 MB to the binary and is slower and uses more memory than the PNG encoder: the
  whole file is held in memory until the export ends.

Measured on the 60x12 line-counting recording and the 80x30 emoji recording
(`contrib/examples/emoji.sh`), same machine, same flags:

| Recording | `--scale` | PNG | WebP |
|---|---|---|---|
| lines, 41 frames | 1 | 260,389 B | 192,572 B |
| lines, 41 frames | 2 | 537,828 B | 354,858 B |
| emoji, 14 frames | 1 | 87,757 B | 71,170 B |
| emoji, 14 frames | 2 | 241,793 B | 186,100 B |

The WebP files are 19 to 34 percent smaller. Export time was 0.2 to 0.6 s (PNG 0.1 to 0.5 s);
peak memory was up to 188 MB for the emoji recording at `--scale 2` (PNG 124 MB).

Where the result plays:

| Where | Animated WebP |
|---|---|
| Chrome, Firefox, Safari (current) | Yes (background knowledge, not re-checked) |
| VS Code Markdown preview | Probably (it is Chromium; unverified) |
| Discord | Yes, since the mobile app release of November 2024 (from Discord's blog; unverified here) |
| GitHub README `<img>` | Unverified |
| GitHub issue and PR upload | Unverified. Community discussion 171343 says WebP uploads work, but the GitHub documentation does not list WebP |
| Slack | Unverified; likely a still image |
| macOS Preview and Quick Look | Unverified; Quick Look may play it, Preview probably not |

Use `png` or `gif` for places you have not tested. The files were checked
with libwebp's `webpinfo` and `webpmux` and decoded with `ffmpeg`, which gives frames identical to
the PNG's.

## SVG

`--format svg` (or a `.svg` output name) writes the recording as vector graphics instead of
pixels: the text of the screen is drawn as text, cell backgrounds as rectangles, emoji as small
embedded PNG images. By default the SVG is **animated** with CSS (`@keyframes` with `steps()`;
no script), looping like the other formats (`--loops`). Give `--frame` to write one still instead.

| Flag | Default | Meaning |
|---|---|---|
| `--frame first\|last\|TIME` | off (animate) | Write one frame as a static SVG. `TIME` is a time on the output clock, such as `1.5s` |
| `--svg-text text\|paths` | `text` | `text`: `<text>` drawn in the viewer's monospace font (small, selectable, every glyph placed at its grid column). `paths`: glyph outlines of the built-in fonts, exact and independent of the viewer's fonts, but larger |

`--frame` is an error for the raster formats and `--svg-text` is an error for any format but
`svg`.
`--fallback-font` and `--system-fonts` change SVG output only with `--svg-text paths`.

Each glyph in `text` mode carries its own x position, so text stays on the terminal grid even
when the viewer's monospace font has a different width. The look of the glyphs still depends on
the font the viewer picks (JetBrains Mono, then `ui-monospace`, Menlo, `monospace`); use
`--svg-text paths` when the exact glyphs matter, for example for CJK, symbols or box drawing.
The exception is Nerd Font icons: in `text` mode they are drawn as outlines, so they show even
where no Nerd Font is installed; everything else stays text.

Measured sizes on this implementation (80x24 unless noted; the 60x12 scrolling recording is 41
frames; sizes include everything in the file):

| Recording | Animated SVG | Static (`--frame last`) | Static, `--svg-text paths` | PNG |
|---|---|---|---|---|
| scrolling lines, 60x12 | 49,351 B | 1,446 B | 7,747 B | 260,389 B |
| colors.sh | 55,805 B | 54,410 B | 79,910 B | 30,167 B |
| emoji.sh | 126,734 B | 124,344 B | 144,163 B | 87,188 B |

An SVG wins on text and loses on dense colour: colors.sh is thousands of one-cell rectangles,
and emoji are embedded as PNG images (about 1 KB each, once per distinct emoji).

Where the result plays (from `docs/research/svg-export.md`; only the first rows are well
supported):

| Where | Animated SVG |
|---|---|
| Browser, SVG opened directly or in `<object>` | Yes (per MDN; not re-checked here) |
| Browser, `<img src="x.svg">` | CSS animations should run in the secure animated mode of the spec; partly verified in research, not tested here |
| GitHub README `<img>` | **Unverified.** Reports conflict and no current test was made. Use PNG or GIF there |
| macOS Quick Look, Preview | Unverified; probably a still image |
| Slack, Discord | Unverified; not expected to preview SVG inline |
| VS Code Markdown preview | Unverified; may block the image |
| Figma, Inkscape, resvg, rsvg | Unlikely to run the CSS animation. They draw the base state, which is the first frame (a design rule, not tested in each) |

**PNG is the safe choice for READMEs and chat.** Use SVG where you control the viewer (your own
site, documentation, a browser tab) or want a small, sharp still (`--frame last`).

## Poster (PNG)

The file is a standard PNG using the APNG extension, so a viewer without animation support shows one image: the *default image*. Preview, Quick Look and
many chat previews are such viewers. An animated PNG normally has no separate default image, so they show
its first frame. `--poster` (PNG only) chooses what they show instead.

| `--poster` | Default image |
|---|---|
| `last` (default) | The final screen of the recording, the end result |
| `first` | The first frame, which is what `none` writes |
| `none` | None: the first frame is both the default image and the first animation frame |
| a time such as `12s` | The frame on screen at that time on the output clock, after `--speed`, `--idle-limit` and `--hold` and the [leading blank trim](#leading-blank-frame), like `--frame` of SVG; a time past the end picks the last frame |

- The poster is drawn by the same renderer, with the same theme, font and cursor, and stored as
  the PNG's `IDAT` image *without* an `fcTL` chunk before it, which is how a default image
  outside the animation is stored. Every animation frame is then an `fdAT`
  chunk, `acTL` counts only them, and animating viewers play exactly the same animation as
  without a poster. A viewer that decodes the file as a plain PNG shows the poster.
- **Size.** A poster other than the first frame costs one full-canvas image. Measured on the
  README showcase recording (80x24, scale 2, 1472x992 px, 204 frames): 1,742,152 B with
  `--poster none`, 1,761,500 B with `last`, +19,348 B (+1.1 %). On the short 80x24 example
  recordings (`contrib/examples`, scale 1) the cost is 7 to 107 KB: +0.5 % (dashboard) to +13 %
  (animations, cursor, buildlog) for sessions of hundreds of frames, but nearly double (+94 to
  +101 %) for a recording of a few screens (attributes, colours, emoji, osc, unicode).
  Use `--poster none` where file size matters more than the preview.
- `first` and a time that falls in the first frame write the same file as `none`: the default
  image is already the first frame, so no copy is added.
- `--poster` with another format (`--format gif`, `.webp`, `.svg`) is an error. Those formats have
  no default image; a GIF viewer shows the first frame, as before, which the
  [leading blank trim](#leading-blank-frame) makes the first output instead of an empty screen.

## Output file

- Without `-o`, the output is `FILE` with its extension replaced by the first extension of the
  format (`.png` for `png`, `.gif` for `gif`, `.svg` for `svg`, `.webp` for `webp`).
- `-o` is used as typed, whatever its extension, and the extension picks the format unless
  `--format` is given. `-o -` writes a file named `-`; export does not write to stdout.
- Export refuses to overwrite the input recording and replaces any other existing file.
- The file is written atomically: a failed run leaves no partial output.
- The `wrote N frames` line on stderr counts the frames in the file, not the steps of the
  timeline: the encoders merge frames that render the same picture (for example a cursor move
  with `--cursor none`) into one longer frame. A PNG's poster is not a frame.
- A recording cut short by a crash exports up to the damage, with the same note on stderr as
  `replay`.
- The canvas size is fixed. A recording whose terminal changed size part-way fails with
  `recording changes size mid-way (80x24 -> 100x30); export does not support resizes yet`.

## Themes

`--theme NAME` selects a built-in theme, `dark` (default) or `light`. `--theme FILE` loads a
theme from a JSON file. Themes are applied at export time; recordings do not store them.

All fields are required, unknown fields are rejected, and colours are `#rrggbb`. `palette` holds
the 16 ANSI colours in order; colours 16–255 are computed.

```json
{
  "name": "mine",
  "foreground": "#c0caf5",
  "background": "#1a1b26",
  "cursor": "#c0caf5",
  "palette": [
    "#15161e", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#a9b1d6",
    "#414868", "#f7768e", "#9ece6a", "#e0af68", "#7aa2f7", "#bb9af7", "#7dcfff", "#c0caf5"
  ]
}
```

## Size and cursor

| Flag | Default | Meaning |
|---|---|---|
| `--font-size` | 15 | Font size in pixels, before `--scale` |
| `--scale` | 1 | Integer factor applied to font size and padding. `--scale 2` draws native 2x glyphs for sharp retina embeds |
| `--padding` | 8 | Border around the screen in pixels (at least 1), before `--scale` |
| `--cursor` | `block` | `block`, `bar`, `underline` or `none` |

## Cell size

The cell is the font's advance and line height rounded to whole pixels, as in Ghostty.
`--adjust-cell-width` and `--adjust-cell-height` change those pixels after `--scale`. A value is
either pixels (`-1`, `2`) or a percentage (`-7%`). The result must be between 1 pixel and 10 times
the font cell. A glyph is centred in a wider cell; in a narrower one it stays at the left edge and
overflows to the right. `--adjust-cell-height` keeps the text vertically centred.

To match a Ghostty terminal configured with `font-size = 12` and `adjust-cell-width = -7%` on a
2x display, use:

```sh
shellshot export demo.shot --font-size 12 --scale 2 --adjust-cell-width -7%
```

This gives 13x32 pixel cells, the same as in Ghostty.

## Fonts, emoji and CJK

Text is drawn with the embedded JetBrains Mono (regular, bold, italic, bold italic). Box-drawing,
block, braille and Powerline characters are drawn as shapes, not from a font. A glyph the main
font lacks comes from a chain of fallback fonts; the first font that has it wins:

1. the embedded Symbols Nerd Font Mono (icons),
2. the embedded Noto Sans Mono CJK SC subset (about 3,750 hanzi, Hangul syllables, kana and CJK
   punctuation, with Simplified Chinese shapes for Han characters that differ between languages),
3. each `--fallback-font FILE`, in the order given,
4. with `--system-fonts`, installed CJK and symbol fonts.

Nerd Font icons are sized by height, not width: an icon is drawn at the font size, or smaller if
that would make it taller than the cell. It starts at the left edge of its cell and may extend into
the next cell, as in a terminal that uses the non-Mono Nerd Font. Other fallback glyphs are scaled
to fit their cells.

`--fallback-font FILE` takes a TTF, OTF or TTC file and can be repeated. An unreadable or invalid
file is an error. User fonts are drawn as monochrome outlines, so colour-only fonts do not work
there (use `--emoji-font` for a colour emoji font), and bold or italic does not change a fallback glyph.

`--system-fonts` also searches the macOS and Linux font folders. The output then depends on the
fonts installed on the machine, so exports differ between machines. Without it, exports are
deterministic.

Emoji are drawn in colour from embedded bitmap fonts: by default a compact derivative of Apple
Color Emoji, with Noto Color Emoji behind it for the emoji Apple's font lacks. A grapheme cluster is
drawn once over its one or two cells: ZWJ families, flags, skin tones, keycaps and `U+FE0F`
sequences look up the whole sequence and fall back to the first emoji if no font has it. The
emoji fonts are decompressed the first time an emoji is drawn, so a recording without emoji pays
nothing. The Noto font covers Emoji 18.0; emoji newer than that and rare hanzi outside the CJK
subset are blank unless `--emoji-font` (emoji) or `--fallback-font` (outlines) has them.

| Flag | Default | Meaning |
|---|---|---|
| `--emoji-set apple\|noto` | `apple` | `apple`: Apple-derived artwork, then Noto for what it lacks. `noto`: Noto Color Emoji only |
| `--emoji-font FILE` | none | a COLR v0 or CBDT/CBLC colour emoji font, tried before the `--emoji-set` fonts for every emoji. An unreadable or invalid file is an error |

The Apple artwork belongs to Apple Inc. and is not licensed by this project, so whoever shares
exports or binaries that contain it must follow Apple's terms; `--emoji-set noto` draws Noto
Color Emoji only (SIL OFL 1.1) and is the licence-clean choice for exports you share. Licences and
checksums of the embedded fonts are in the `NOTICE.txt` of each `internal/render/*/assets/<group>/`
directory (see also the root `NOTICE`).

Colour comes only from the emoji fonts (`--emoji-font` and the built-in sets): `--fallback-font` and
`--system-fonts` fonts are drawn as monochrome outlines.

A background colour, inverse, underline or strikethrough on trailing blanks is drawn (a status bar
filled with `ESC[K`, a row of colour swatches, a full-screen background). Blanks that would show
nothing (no background, or only a foreground colour, bold, italic, faint or blink) are trimmed.

## Timing

| Flag | Default | Meaning |
|---|---|---|
| `--fps` | 60 | Sampling rate in frames per second (1 to 1000). A frame is kept only if the screen changed |
| `--speed` | 1 | Playback speed multiplier |
| `--idle-limit` | 2s | Longest pause between frames, before `--speed` (0 for no limit) |
| `--hold` | 2s | How long the final frame stays |
| `--min-delay` | 20ms | Shortest frame delay; shorter frames merge into the previous one (0 keeps all) |
| `--loops` | 0 | Times the animation plays (0 for forever) |
| `--keep-leading-blank` | off | Keep a blank screen at the start of the animation; see below |

### Leading blank frame

A recording starts with the empty terminal that existed before the program printed anything.
Viewers that show only the first frame (macOS Preview, Quick Look, chat previews) would show an
empty terminal. So, in every format, export drops the first frame when it shows
nothing (no text and no styled cell; the cursor does not count), however long it is on screen
(after `--idle-limit` and `--speed`). The recording then starts at the first output. Decisions:

- The dropped frame is removed from playback: its time is not added to the next frame, so the
  animation is that much shorter. The idle before the first output is not interesting.
- A recording that has only one frame keeps it, blank or not.
- Only the first frame is considered, once. If a hidden span covers the start, the first frame is
  the screen at the end of that span, and the same rule applies to it.
- `--keep-leading-blank` turns the trim off.

The trim is the boolean timeline option `TrimLeadingBlank`; the command line sets it unless
`--keep-leading-blank` is given.

`shellshot replay` shares this timing policy: it takes `--idle-limit` (default 2s, 0 for no
limit, applied before `--speed`) and `--keep-leading-blank`, and builds its waits with the same
timeline code. A recording with a long pause, or a long wait before the first output, therefore
replays with no wait longer than `--idle-limit` and starts at the first output. `replay` holds
the whole recording in memory while it plays. When it draws in place (stdout is a terminal, or
`--clear`), it ends by showing the cursor, moving below the last frame and printing a summary to
stderr: how it ended, frames shown, wall time played against time recorded (and saved), idle
pauses capped, the crop, and the file. `-q`/`--quiet` suppresses it; a piped stdout gets none.
Ctrl-C is a normal end: the summary says where it stopped, and the exit status is 130.

## Scenes and hidden spans

A recording can carry spans, marked by a script run. `export` applies the speed, idle and hold
hints stored in scenes and cuts hidden spans from the output. A recording without spans exports
as before.

| Flag | Meaning |
|---|---|
| `--no-hints` | Ignore the hints stored in scenes. Hidden spans are still cut |
| `--show-hidden` | Render hidden spans instead of cutting them |

`--speed`, `--idle-limit` and `--hold`, when given, each apply to the whole recording and replace
the matching scene hint. An explicit `--hold` also drops scene hold hints. Without `--hold`, a
scene hold is added to the default 2s final hold where a scene ends. The precedence is flag, then
scene hint, then default.

A scene or hidden span that was never closed (the recorder was killed) ends at the last event. A
malformed span event ends the span list, and export warns on stderr.

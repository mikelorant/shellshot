# Export reference

`shellshot export FILE [-o OUT.png] [flags]` renders a recording as an animated PNG
(APNG). The result is a standard PNG that carries animation data. Browsers, GitHub, Slack
and Discord animate it; at the time of writing, macOS Preview and Quick Look show only the
first frame. Run `shellshot export --help` for the flag list.

## Output format

`--format NAME` chooses the output format. Without it, the format comes from the extension of
`-o` (case-insensitive), and is `apng` when there is no `-o` or `-o` has no extension. An
extension that no format owns, or an unknown `--format` name, fails with the list of supported
formats (`supported: apng`); `--format` then lets you write any file name.

| Format | `--format` | Extensions | Notes |
| --- | --- | --- | --- |
| Animated PNG | `apng` | `.png`, `.apng` | The default. Plays in browsers, GitHub, Slack and Discord. |

## Output file

- Without `-o`, the output is `FILE` with its extension replaced by the first extension of the
  format (`.png` for `apng`).
- `-o` is used as typed, whatever its extension, and the extension picks the format unless
  `--format` is given. `-o -` writes a file named `-`; export does not write to stdout.
- Export refuses to overwrite the input recording and replaces any other existing file.
- The file is written atomically: a failed run leaves no partial output.
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

1. the embedded Symbols Nerd Font Mono (icons, scaled down to fit their cell),
2. the embedded Noto Sans Mono CJK SC subset (about 3,750 hanzi, Hangul syllables, kana and CJK
   punctuation, with Simplified Chinese shapes for Han characters that differ between languages),
3. each `--fallback-font FILE`, in the order given,
4. with `--system-fonts`, installed CJK and symbol fonts.

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

Lines are right-trimmed, so a background colour painted on trailing spaces is lost.

## Timing

| Flag | Default | Meaning |
|---|---|---|
| `--fps` | 60 | Sampling rate in frames per second (1 to 1000). A frame is kept only if the screen changed |
| `--speed` | 1 | Playback speed multiplier |
| `--idle-limit` | 2s | Longest pause between frames, before `--speed` (0 for no limit) |
| `--hold` | 2s | How long the final frame stays |
| `--min-delay` | 20ms | Shortest frame delay; shorter frames merge into the previous one (0 keeps all) |
| `--loops` | 0 | Times the animation plays (0 for forever) |

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
scene hint, then default. The script-level `meta` hint is not read yet.

A scene or hidden span that was never closed (the recorder was killed) ends at the last event. A
malformed span event ends the span list, and export warns on stderr.

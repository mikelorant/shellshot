# Research briefs

Design history. The briefs are kept as written; for current behaviour read the guides in the parent
directory. Each line gives the question a brief answered and where it stands.

- [apng-export.md](apng-export.md): how to export an animated image without a browser. Decided: a pure-Go rasteriser and APNG encoder. Built; the format is now called `png` (`--format png`, the default for `export`).
- [binary-format.md](binary-format.md): how to store recordings compactly and crash-safely. Decided: a chunked event log with zstd blocks. Built without the optional `grid` keyframes and `indx` footer; the user reference is [../format.md](../format.md).
- [emoji-fallback.md](emoji-fallback.md): how to draw emoji and CJK text in exports. Decided: embedded colour emoji (COLRv0 Twemoji in the brief) and a CJK subset with a fallback chain. Built; the emoji are now Apple-derived artwork with Noto Color Emoji as fallback, not Twemoji.
- [font-spacing.md](font-spacing.md): how to size cells and keep glyphs sharp, and how to match Ghostty. Decided. Built (`--scale`, `--adjust-cell-*`).
- [script-format.md](script-format.md): the `.script` language for automated recordings. Decided; the final syntax lives here. Built (`record --script`); reserved words not built are listed in [../scripting.md](../scripting.md).
- [script-scenes.md](script-scenes.md): scenes, hidden spans and their export semantics. Partly superseded by `script-format.md`; the `SPAN` event design is built.
- [apple-emoji.md](apple-emoji.md): whether to ship Apple-style colour emoji. Prototyped, then built: a compact Apple Color Emoji derivative is embedded and is the default (`--emoji-set apple|noto`).
- [sandboxing.md](sandboxing.md): whether and how to run recorded commands in a sandbox. Out of scope: dropped by the maintainer; the brief is kept as a record only, nothing is implemented or planned.
- [readme-improvements.md](readme-improvements.md): how The Good Docs Project's README guidance applies to this README. Research only; recommendations not adopted yet, the maintainer decides which to act on.
- [raster-formats.md](raster-formats.md): which animated raster formats `export` should offer (GIF, WebP, AVIF, JXL). Desk research only. GIF and lossless WebP built; AVIF and JXL rejected for now.
- [svg-export.md](svg-export.md): how to export SVG from snapshots, static and animated. Built; the measured sizes differ from the brief's estimates.

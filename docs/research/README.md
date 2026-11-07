# Research briefs

Design history. The briefs are kept as written; for current behaviour read the guides in the parent
directory. Each line gives the question a brief answered and where it stands.

- [apng-export.md](apng-export.md): how to export an animated image without a browser. Decided: a pure-Go rasteriser and APNG encoder. Implemented (`export`).
- [binary-format.md](binary-format.md): how to store recordings compactly and crash-safely. Decided: a chunked event log with zstd blocks. Implemented without the optional `grid` keyframes and `indx` footer; the user reference is [../format.md](../format.md).
- [emoji-fallback.md](emoji-fallback.md): how to draw emoji and CJK text in exports. Decided: embedded Twemoji (COLRv0) and a CJK subset with a fallback chain. Implemented.
- [font-spacing.md](font-spacing.md): how to size cells and keep glyphs sharp, and how to match Ghostty. Decided. Implemented (`--scale`, `--adjust-cell-*`).
- [script-format.md](script-format.md): the `.script` language for automated recordings. Decided; the final syntax lives here. Lexer and parser built, runner in progress; see [../scripting.md](../scripting.md).
- [script-scenes.md](script-scenes.md): scenes, hidden spans and their export semantics. Partly superseded by `script-format.md`; the `SPAN` event design is implemented.
- [apple-emoji.md](apple-emoji.md): whether to ship Apple-style colour emoji. Investigated and prototyped; in progress.
- [sandboxing.md](sandboxing.md): whether and how to run recorded commands in a sandbox. Design brief only; undecided, not started.

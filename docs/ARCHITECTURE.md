# Architecture

How shellshot is put together. Usage is in the [README](../README.md); the user-facing references are
[export.md](export.md), [format.md](format.md) and [scripting.md](scripting.md); build and test
instructions are in [development.md](development.md). Design briefs are indexed in
[research/README.md](research/README.md).

## Data flow

```
record:  command --PTY--> session --> live tee --> your terminal
                             |
                             +--> recording/bin Writer --> .shot file

replay:  .shot --> recording/bin Reader --> recording/frames --(vt.Terminal)--> snapshot.Frame --> replay --> stdout
export:  .shot --> recording/bin Reader --> recording/frames --> export/timeline --> render/raster --> export/apng --> .png
info:    .shot --> recording/bin (scan only, no terminal) --> recording/spans --> stdout
```

`record` stores raw terminal output with timestamps. Screens are derived later: `recording/frames`
feeds the events to a headless terminal and samples snapshots at `--fps` (default 60), keeping a frame
only when the screen changed. Replay and export therefore share one reading path, and themes, fonts and
speed are export-time choices.

## Packages

Module `github.com/michaellorant/shellshot`.

```
cmd/shellshot               main: one line, no logic
internal/cli                cobra commands and flags; wires concrete types. One file per command
internal/snapshot           pure data: Snapshot, Cursor, Frame, Style, Wide (no dependencies)
internal/vt                 Terminal interface and Factory (no cgo)
internal/vt/ghostty         the only cgo package: libghostty adapter and key encoder
internal/pty                spawn a command in a PTY: Read, Write, Resize, Wait, Close
internal/tty                IsTerminal, Size, MakeRaw (wraps golang.org/x/term)
internal/live               stdin -> process pump and live output tee (io interfaces only)
internal/session            pty + vt + sampler: emits frames and recording events to sinks
internal/recording/bin      binary format: chunk, varint, codec, head, event; Writer and Reader
internal/recording/spans    SPAN events -> scenes and hidden spans
internal/recording/frames   recorded events -> snapshot frames through a vt.Factory
internal/replay             plays frames to an io.Writer with timing
internal/export/timeline    frames -> timed steps: idle cap, speed, hold, minimum delay
internal/export/pipeline    timeline + raster + apng: frames -> animated PNG
internal/export/apng        APNG encoder (standard library only)
internal/render/raster      frame -> image: cells, clusters, colours, cursor
internal/render/font        embedded fonts and the fallback chain
internal/render/fontchain   builds the font set for export: embedded, --fallback-font, --system-fonts
internal/render/emoji       pure Go colour emoji renderer (COLR v0, CBDT/CBLC); the built-in Apple and Noto sets
internal/render/sprite      box-drawing, block, braille and Powerline glyphs
internal/render/theme       built-in themes and the JSON theme loader
internal/script             .script lexer and parser (the runner is not built)
internal/script/keys        key chord text -> structured chord
scripts/build-libghostty.sh, Makefile   dependency and build tooling
```

Dependency rules, enforced in review:

- `snapshot` imports nothing. `vt`, `replay`, `export/timeline` and `render/theme` import only `snapshot`.
- Only `vt/ghostty` imports cgo and libghostty. Everything else builds without it.
- Only `cli` imports cobra and wires concrete types.
- `recording/bin` imports only its own subpackages. `recording/frames` imports `snapshot`, `vt` and
  `recording/bin`. `recording/spans` imports `recording/bin`.
- `session` imports `snapshot` and `vt`, and talks to the PTY through interfaces it declares.
- Declare interfaces at the consumer.

## libghostty

libghostty-vt is built from the pinned `third_party/ghostty` submodule with zig; the Go binding
`go.mitchellh.com/libghostty` links it through pkg-config. The pin and the build are described in
[development.md](development.md). The recording header stores the pinned commit as the engine version.

## Recording: how `record` writes

The format is in [format.md](format.md). The wiring:

- `session.Config.Events` takes an event sink; `internal/cli/eventsink.go` adapts it to `bin.Writer`.
- One clock: event times are offsets from the session start, which `Config.OnStart` hands to the sink.
  Input events share it through the decorator that logs process writes. `bin.Writer` has a single owner,
  so the sink serialises the session goroutine and the input pump with a mutex.
- `Config.OnTick` calls `Writer.Tick` from the session ticker (4 Hz for `record`), so a quiet session
  still flushes on the 2-second interval. With nothing pending, Tick writes nothing.
- The file is written as the command runs, with no temporary file. After a crash it reads up to the last
  good block (`bin.ErrTruncated` for a torn tail). A recorder killed exactly at a block boundary leaves
  no torn tail, so completeness is judged by the `EXIT` event that `session` logs when the child ends.
- The size is fixed for a recording: SIGWINCH is not propagated, so `record` writes no `RESIZE` events.
  The format can carry them and `replay` prints them, but `export` fails on a size change.
- With a non-terminal stdin, `live.Pump` sends one Ctrl-D byte to the process when stdin ends
  (`PumpOptions.OnEOF`). It is never sent when stdin is a terminal.

## Session

- One goroutine reads PTY output and writes it to the terminal. Snapshots are taken on that goroutine
  because `vt.Terminal` is not safe for concurrent use.
- Snapshots are taken on a ticker at `Config.FPS` (default 60). A tick snapshots only if output arrived
  since the last one, and a frame is emitted only if it differs from the previous. The first frame is at
  t=0 and a final frame follows process exit, after output is drained.
- Context cancellation kills the process and returns.
- `Config.Driver` (optional, nil = off) is polled on the same goroutine after output and at its own
  deadlines; it writes to the PTY and logs marks and spans, and adds no goroutine that touches the terminal.

## Cell widths (`Snapshot.Widths`)

`Lines` holds text, so a rune is not always one terminal cell. `Spans` and `Widths` both index runes.
`Widths` records only the exceptions so the rasteriser places runes in the cell columns libghostty used:
`nil` (every rune is one cell) or exactly `Rows` entries, one `[]snapshot.Wide{Rune, Cells}` per line.
`Cells` is 2 for a wide rune (CJK) or 0 for a zero-width rune (a combining mark). Entries are sorted by
`Rune`, unique and within the line (`Snapshot.Validate`). `Snapshot.Columns(row)` returns each rune's
starting cell column (`"世界ab"` gives `0,2,4,5`) and `Snapshot.CellWidth(row, runeIdx)` its width.
Lines are right-trimmed, so columns are meaningful only up to the last rune.

Widths are produced by replaying a recording (`recording/frames`) and are not stored in it. The adapter
enables grapheme clustering (mode 2027), so an emoji sequence (ZWJ family, flag, skin tone, keycap,
`U+FE0F`) is one cluster: a base rune of 1 or 2 cells followed by `Cells: 0` runes. See
`internal/vt/ghostty/doc.go`.

## Rendering

Design and measurements: [research/emoji-fallback.md](research/emoji-fallback.md) and
[research/font-spacing.md](research/font-spacing.md). User-facing behaviour: [export.md](export.md).

- `raster` lays out a row as clusters (a base rune plus zero-width followers) and draws each once.
  Order: sprite; colour emoji for sequences that ask for emoji presentation (`FE0F`, ZWJ, skin tone,
  keycap, regional indicators, tags); the primary face; the fallback chain; and finally the colour
  emoji font for a rune no text font has. Colour emoji are scaled to a square of
  `min(span width, cell height)`, centred in the base's span, and ignore the foreground colour.
- `font.Set` holds JetBrains Mono and an ordered fallback chain; `FitFace` scales a fallback glyph to its
  one- or two-cell span. `font.Default(extra...)` builds: Symbols Nerd Font Mono, the embedded Noto Sans
  Mono CJK SC subset, then `extra`. `Set.Emoji(cluster, side)` renders from the colour emoji backend
  (`emoji.Default(emoji.SetApple)` unless `SetEmoji` replaces it).
- `emoji` is a pure Go colour emoji renderer. `Chain` tries backends in order; `Default(SetApple|SetNoto)`
  returns the built-in chains (an Apple Color Emoji derivative then Noto Color Emoji, or Noto only), whose
  zstd fonts are inflated on first use, so a session without emoji never decompresses them. `Load` accepts
  COLR/CBDT user fonts. A backend reads the COLR v0, CPAL, CBLC/CBDT and GSUB ligature tables directly
  (`x/image`'s sfnt cannot draw colour), maps a cluster to a glyph (whole sequence, with and without
  variation selectors, then the first rune) and rasterises its layers or bitmaps, cached per glyph and size.
- `fontchain.New` builds the Set for `export`: embedded fonts, then `--fallback-font` files (colour emoji:
  `--emoji-font`, then the `--emoji-set` chain), then, opt-in and machine dependent, `--system-fonts`.
  Exports are deterministic unless `--system-fonts` is used.
- Cell size follows Ghostty: the font advance and line height rounded to whole pixels, then
  `--adjust-cell-*` applied to those pixels.

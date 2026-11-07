# shellshot

Proof of concept. `shellshot` runs a command in a PTY and feeds its output into
a headless terminal ([libghostty-vt](https://github.com/ghostty-org/ghostty)).
It captures the screen as timed snapshot frames, so it can:

- **record**: record an interactive session (asciicast-style) to a `.shot` file;
- **replay**: play a recording back in a terminal with its original timing;
- **export**: render a recording as an animated PNG (APNG);
- **info**: show a recording's header and statistics.

Target platform: macOS (native arch). Linux and Windows are out of scope.
Design details are in [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md).

## Requirements

- Go 1.26 or newer (`go.mod` says `go 1.26.0`), with cgo enabled and a C toolchain.
- [zig](https://ziglang.org) **0.17.x**: `brew install zig`. It is the only tool
  needed to build libghostty-vt (no cmake, no Xcode project).
- `pkg-config` and `git` (the libghostty source is a git submodule).
- Network access on the first build, to fetch the submodule and zig's own dependencies.
  A cold `make deps` takes roughly 1–6 minutes.

## Build

```sh
git clone --recurse-submodules <repo-url> && cd shellshot   # or: git submodule update --init --depth 1 third_party/ghostty
make deps    # builds libghostty-vt into build/libghostty (zig; slow the first time)
make build   # go build -o bin/shellshot ./cmd/shellshot
./bin/shellshot version
```

`make deps` is idempotent and reuses the zig cache. The Makefile exports
`PKG_CONFIG_PATH=$PWD/build/libghostty/lib/pkgconfig` and `CGO_ENABLED=1`, so use
`make` targets (or export the same variables) when running `go` directly.

## Usage

All examples below were run with the built binary. Output is trimmed.

### `version`

```console
$ shellshot version
dev
```

The value can be set with `-ldflags "-X github.com/michaellorant/shellshot/internal/cli.Version=1.2.3"`.

### `record`: interactive recording

```console
$ shellshot record -o demo.shot --cols 60 --rows 10 -c 'echo hello; sleep 0.5; printf "\033[32mgreen\033[0m done\n"'
hello
green done
recorded 4 events (0.4 KB) to demo.shot
```

With no command, `record` runs `$SHELL` (fallback `/bin/sh`) and records until it
exits. Use `-c 'string'` (run via `$SHELL -c`) or `-- cmd args...` for a specific
command. On a terminal, stdin is put in raw mode and output is shown live while
being recorded. `record` writes the compressed binary event-log format: the raw
terminal output with its timing, plus the exit status. Flags: `-o` (default
`recording.shot`), `--cols`/`--rows` (default: current terminal size, else 80x24),
`--codec none|flate|zstd` (block compression, default `zstd`), `--coalesce DURATION`
(merge output reads that arrive within this time of each other; default 0 = off) and
`--record-input` (also log the keystrokes sent to the command, default off; a notice
is printed to stderr). Nothing is written while the program is idle, and the file is
written as the command runs, so a crash leaves a recording readable up to its last
flushed block (at most about 2 seconds are lost). Only the header's size, argv,
start time, `TERM`, `COLORTERM`, `LANG`, `SHELL`, a random session id and the
recorder version are stored; the working directory, host name and the rest of the
environment are not.
If the command exits non-zero, `record` still writes the recording and then exits
with the command's status (a note goes to stderr); `--ignore-exit-status` makes it
exit 0. When stdin is a pipe or file rather than a terminal, reaching its end sends
Ctrl-D to the command, so a shell reading from the pipe exits.

### `replay`: play a recording

```console
$ shellshot replay demo.shot --speed 4
hello
green done
```

`replay` reads the binary recording format and re-renders the terminal output
through libghostty at `--fps n` frames per second (default 60, 1 to 1000; the
screen is sampled on that grid and a frame is shown only when it changed).
A file that is not a recording (for example an old JSON Lines `.shot`) is rejected with
`not a shellshot recording (bad magic)`. A recording cut short by a crash plays up
to the damage, then `shellshot: recording is truncated (N bytes valid)` goes to
stderr and the exit status is 0. A recording whose recorder was killed at a block
boundary plays in full, followed by
`shellshot: recording is incomplete (no exit event; recorder was interrupted)` on
stderr and exit status 0. A corrupt recording is an error. If the terminal
was resized while recording, the new size simply shows up in the following frames.

`--speed f` multiplies the speed. `--clear` redraws from the top of the screen
for every frame; it defaults to on only when stdout is a terminal (piped output
prints each full frame in sequence, so the output above is the final one).
`--color auto|always|never`: `auto` (default) emits SGR colour codes only when stdout is a
terminal and `NO_COLOR` is unset. With `--color always` the second line is
`^[[32mgreen^[[0m done`.

### `export`: animated PNG

```console
$ shellshot export demo.shot -o demo.png --theme light
wrote 3 frames (3.2 KB) to demo.png
$ shellshot export demo.shot        # writes demo.png next to the input
```

The output is a standard PNG that carries APNG animation data. Viewers that
support APNG animate it (browsers, GitHub, Slack, Discord and many image
viewers); others show only the first frame. At the time of writing, macOS
Preview and Quick Look show only the first frame, so open the file in a browser
to check the animation. `-o` is written as typed, so `-o demo.apng` also works.

| Flag | Default | Meaning |
|---|---|---|
| `-o` | input with `.png` | output file (never overwrites the input) |
| `--theme` | `dark` | built-in (`dark`, `light`) or a theme JSON file |
| `--font-size`, `--scale`, `--padding` | 15, 1, 8 | em size in pixels, integer scale of font size and padding (2 = native 2x glyphs, for sharp retina embeds), border (≥1) |
| `--adjust-cell-width`, `--adjust-cell-height` | 0, 0 | change the cell size after `--scale`, in pixels (`-1`, `2`) or percent (`-7%`), like Ghostty's options of the same name |
| `--speed` | 1 | playback speed multiplier |
| `--idle-limit` | 2s | longest pause between frames (0 = no limit) |
| `--hold` | 2s | how long the last frame stays |
| `--loops` | 0 | plays (0 = forever) |
| `--min-delay` | 20ms | shorter frames are merged into the previous one |
| `--cursor` | `block` | `block`, `bar`, `underline`, `none` |
| `--fps` | 60 | frame sampling rate in frames per second (1 to 1000) |
| `--fallback-font FILE` | none | extra TTF/OTF/TTC for glyphs the built-in fonts lack; repeatable, tried in order after the built-in fonts; an unreadable or invalid file is an error |
| `--emoji-set` | `apple` | colour emoji artwork: `apple` (Apple Color Emoji, with Noto Color Emoji for what Apple's lacks) or `noto` (Noto Color Emoji only). Apple's artwork belongs to Apple Inc.; `noto` is open source (SIL OFL 1.1) and the licence-clean choice for exports you share |
| `--emoji-font FILE` | none | a COLR v0 or CBDT/CBLC colour emoji font tried before the `--emoji-set` fonts; an unreadable or invalid file is an error |
| `--system-fonts` | off | also use installed CJK and symbol fonts (macOS and Linux font folders). **The output then depends on this machine's fonts**, so exports differ between machines |

Cell size follows Ghostty: the cell is the font's advance and line height
rounded to whole pixels, and `--adjust-cell-*` applies to those pixels. A glyph
is centred in a wider cell; in a narrower one it stays anchored at the left edge
and overflows to the right. For a Ghostty config of `font-size = 12` and
`adjust-cell-width = -7%` on a 2x (retina) display use
`--font-size 12 --scale 2 --adjust-cell-width -7%` (13x32 pixel cells, as there).

A theme file is JSON with all fields required:
`{"name":"x","foreground":"#rrggbb","background":"#rrggbb","cursor":"#rrggbb","palette":[16 × "#rrggbb"]}`.
Output is written atomically (a failed run leaves no partial file).
A truncated or interrupted recording exports with the same stderr note as `replay`.

Known limitation: the APNG canvas has a fixed size, so a recording whose terminal
was resized part-way fails with `recording changes size mid-way (80x24 -> 100x30);
export does not support resizes yet`.

### `info`: header and statistics

```console
$ shellshot info demo.shot
file          demo.shot
size          80x24
started       2025-01-02T03:04:05Z
duration      2s
recorder      shellshot 0.0.0-test
engine        ghostty abc1234
command       ls -l
input         recorded
blocks        2
events        6
  OUT         2
  IN          1
  RESIZE      1
  MARK        1
  EXIT        1
  other       0
out bytes     12
payload bytes 24
file bytes    180
file/payload  7.50
status        complete
```

`info` reads the event log without replaying it through a terminal, so it is quick on
large files. `payload bytes` is the raw data of all events and `file/payload` is the
file size divided by it (below 1 when compression wins). `status` is one of:

- `complete`: the file ends at a chunk boundary after the recording's exit event.
- `interrupted`: the file ends at a chunk boundary but has no exit event, so the
  recorder was killed or crashed; everything up to its last flushed block is intact.
- `truncated (N bytes valid)`: bytes follow the last valid chunk (a torn write);
  the first N bytes are intact.
- `damaged: ...`: the file is corrupt. `info` prints what could be read and exits 1.

## Recording format

- **Binary format** (`record` writes it; `replay`, `export` and `info` read it). A chunked, compressed
  file designed in [docs/research/binary-format.md](docs/research/binary-format.md)
  and implemented in `internal/recording/bin` (with `varint`, `codec`, `head`,
  `event`, `chunk` below it): a `HEAD` chunk, then blocks of timestamped events (`OUT`,
  optional `IN`, `RESIZE`, `MARK`, `EXIT`), zstd-compressed by default. Themes are not
  stored; they are chosen at export time. `internal/recording/frames` replays the
  events through libghostty to produce the screen frames the commands use.
- Details of the chunks and events: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#recording-format-binary-event-log-extension-shot).

There is no other recording format; the earlier JSON Lines `.shot` files are not read.

## Why libghostty is a pinned submodule

The Go binding `go.mitchellh.com/libghostty` is written against one specific
libghostty-vt C API. The rolling `tip` release of libghostty no longer matches
it, so prebuilt downloads do not work. Instead `third_party/ghostty` is a
submodule pinned to commit **66af4c7** (`66af4c7458db7e19ae7adb44aa7054879be58b6b`),
the commit the binding pins, and `make deps` builds it with zig.

To bump it:

```sh
git -C third_party/ghostty fetch --depth 1 origin <commit>
git -C third_party/ghostty checkout <commit>
git add third_party/ghostty
# update go.mitchellh.com/libghostty to a version built for that commit
go get go.mitchellh.com/libghostty@<version> && go mod tidy
make deps && make vet test build
```

Check the new commit's `minimum_zig_version` in `third_party/ghostty/build.zig.zon`
against your zig. Commit the submodule pointer and `go.mod`/`go.sum` together.

## Project layout

```
cmd/shellshot          main: one line, no logic
internal/cli           cobra commands and flags (version, record, replay, export, info)
internal/snapshot      pure data: Snapshot, Cursor, Frame, Style (no deps)
internal/vt            Terminal interface + Factory (no cgo)
internal/vt/ghostty    the only cgo package: libghostty adapter
internal/pty           spawn a command in a PTY
internal/tty           IsTerminal, Size, MakeRaw
internal/live          stdin -> process pump and live output tee
internal/session       pty + vt + sampler -> frames emitted to a sink
internal/recording/bin     binary recording format: container + codec + Writer/Reader (written by record)
internal/recording/frames  binary events -> snapshot frames through libghostty (replay, export)
internal/replay        play frames to an io.Writer with timing
internal/export        timeline (timing), apng (encoder), pipeline (frames -> APNG)
internal/render        font (embedded + fallback chain), fontchain (--fallback-font/--system-fonts), emoji (colour emoji backends and the built-in sets), theme, raster (frame -> image), sprite (box/block/braille/powerline glyphs)
scripts/build-libghostty.sh, Makefile
```

Dependency rules: `snapshot` imports nothing; only `vt/ghostty` imports cgo;
only `cli` imports cobra. Interfaces are declared at the consumer.

## Export details

- APNG output, `dark` and `light` built-in themes, or your own theme JSON.
- Text is drawn with an **embedded JetBrains Mono** (Regular, Bold, Italic, Bold
  Italic; licence in `internal/render/font/assets/jetbrains-mono/OFL.txt`). Box-drawing, block,
  braille and powerline characters are drawn as sprites.
  Glyphs the main font lacks come from a fallback chain, first font wins:
  the bundled Symbols Nerd Font Mono (icons), a bundled Noto Sans Mono CJK SC
  subset (about 3,750 hanzi, Hangul syllables, kana and CJK punctuation), then
  any `--fallback-font` files, then (only with `--system-fonts`) installed CJK
  and symbol fonts.
- **Emoji are drawn in colour** from bundled bitmap fonts: a compact Apple
  Color Emoji derivative first and Noto Color Emoji behind it (`--emoji-set
  noto` uses Noto alone). A grapheme cluster is drawn once over its one or two
  cells: ZWJ families, flags, skin tones, keycaps and `U+FE0F` sequences each
  look up the whole sequence and fall back to the first emoji if no font has
  it. The fonts are decompressed the first time an emoji is drawn, so text
  without emoji pays nothing.
- Emoji artwork: the Apple set draws Apple Inc.'s artwork, which is not open
  source and is not licensed by this project; see
  `internal/render/emoji/assets/apple-emoji/NOTICE.txt`. Noto Color Emoji is
  SIL OFL 1.1. All font licences and checksums are in the `NOTICE.txt` of each
  `internal/render/*/assets/<group>/` directory. The fonts add about 23 MB to
  the binary (Apple emoji 13.0 MB, Noto emoji 9.7 MB, CJK subset 1.1 MB).
- Idle gaps are capped by `--idle-limit`; very short frames are merged.

## Known limitations

- **Fixed terminal size during recording.** The format has one size; SIGWINCH is
  not propagated, so resizing your terminal mid-recording does not change it.
- **Trailing coloured blanks are dropped.** Lines are right-trimmed, so a
  background colour painted on trailing spaces (e.g. a full-width status bar's
  empty part) is lost.
- **Emoji and CJK coverage is bounded.** Emoji newer than Unicode 18 and rare
  hanzi outside the bundled subset are blank unless `--emoji-font` (emoji) or
  `--fallback-font` (or `--system-fonts`) supplies them. The CJK subset has Simplified Chinese glyph
  shapes for Han characters that differ between Chinese, Japanese and Korean.
  Colour comes only from the emoji fonts (`--emoji-font` and the bundled sets):
  `--fallback-font` and `--system-fonts` fonts are drawn as monochrome outlines. Bold and italic do not change fallback glyphs. (`replay` prints these
  characters as text.)
- **Nerd Font icons are scaled down to fit their cell.** Icons from the fallback
  font are shrunk to the cell width (and height) and centred, as in the "Nerd Font
  Mono" variants, so they render smaller than in terminals that let icons overflow
  into the next cell.
- Stable only on macOS; `record` logs input events only with `--record-input`, and no resize events (SIGWINCH is not propagated).

## Development

- Work in git worktrees under `.worktrees/<task>` on branch `task/<task>` (the directory is git-ignored).
  A new worktree needs its own submodule checkout and `build/` (copy `build/` from
  another checkout, then fix the absolute `prefix=` line in
  `build/libghostty/lib/pkgconfig/*.pc` with `sed`, or run `make deps`).
- Before committing: `make vet test build` (`make fmt` runs `gofmt -w .`).
- Run `go` through `make` or export `PKG_CONFIG_PATH=$PWD/build/libghostty/lib/pkgconfig CGO_ENABLED=1`.
- Fuzz targets exist for the binary-format parts, for example:
  `go test ./internal/recording/bin/varint -run '^$' -fuzz FuzzUvarint -fuzztime 30s`
  (others: `FuzzReader`, `FuzzDecompress`, `FuzzZstdDecompress`, `FuzzUnmarshal`,
  `FuzzUnmarshalTheme`, `FuzzUnmarshalMeta`, `FuzzDecode`; list with `grep -rn 'func Fuzz' internal`).
- Golden image tests live in `internal/render/raster` and `internal/render/sprite`.
  Regenerate after an intentional rendering change, then review the diff in `testdata/`:
  `go test ./internal/render/raster ./internal/render/sprite -update`
- `make clean` removes `bin/`; `make distclean` also removes `build/` (needs `make deps` again).
- Commits follow Conventional Commits.

## Troubleshooting

- **`pkg-config: libghostty-vt-static not found`** or cgo link errors: run
  `make deps`, and make sure `PKG_CONFIG_PATH` points at `build/libghostty/lib/pkgconfig`
  (the Makefile sets it; plain `go build` does not).
- **Build works in one worktree and fails in another** (missing headers or
  libraries from a different path): the cgo build cache can hold results from another
  worktree's `build/`. Run `go clean -cache`, and check that `prefix=` in the
  `.pc` files names this worktree's `build/libghostty`.
- **`zig not found` / zig version errors**: `brew install zig` and check that
  `zig version` is 0.17.x.
- **Empty `third_party/ghostty`**: `git submodule update --init --depth 1 third_party/ghostty`.
- **Colours missing in `replay` output**: `auto` disables colour when piped or
  when `NO_COLOR` is set; pass `--color always`.

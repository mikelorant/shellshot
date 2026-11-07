# shellshot

Proof of concept. `shellshot` runs a command in a PTY and feeds its output into
a headless terminal ([libghostty-vt](https://github.com/ghostty-org/ghostty)).
It captures the screen as timed snapshot frames, so it can:

- **snap**: print the final screen of a command as text;
- **record**: record an interactive session (asciicast-style) to a `.shot` file;
- **replay**: play a recording back in a terminal with its original timing;
- **export**: render a recording as an animated PNG (APNG).

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

### `snap`: final screen of a command

```console
$ shellshot snap -- ls /usr
X11        bin        libexec    sbin       standalone
X11R6      lib        local      share
```

Flags: `--cols N` (80), `--rows N` (24), `--fps N` (60),
`--color auto|always|never`. Trailing blank rows are trimmed. `auto` emits SGR
colour codes only when stdout is a terminal and `NO_COLOR` is unset.

```console
$ shellshot snap --color always -- printf '\033[31mred\033[0m plain\n' | cat -v
^[[31mred^[[0m plain
```

If the command exits non-zero, `snap` prints the screen and a note on stderr
(`shellshot: command exited with 3`), then exits with the command's status.
Pass `--ignore-exit-status` to exit 0 regardless.

### `record`: interactive recording

```console
$ shellshot record -o demo.shot --cols 60 --rows 10 -c 'echo hello; sleep 0.5; printf "\033[32mgreen\033[0m done\n"'
hello
green done
recorded 3 frames to demo.shot
```

With no command, `record` runs `$SHELL` (fallback `/bin/sh`) and records until it
exits. Use `-c 'string'` (run via `$SHELL -c`) or `-- cmd args...` for a specific
command. On a terminal, stdin is put in raw mode and output is shown live while
being recorded. Flags: `-o` (default `recording.shot`), `--cols`/`--rows` (default:
current terminal size, else 80x24), `--fps` (screen sampling rate in frames per second, default 60, max 1000).
A frame is stored only when the screen differs from the previous one.
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

`--speed f` multiplies the speed. `--clear` redraws from the top of the screen
for every frame; it defaults to on only when stdout is a terminal (piped output
prints each full frame in sequence, so the output above is the final one).
`--color auto|always|never` behaves as for `snap`; with `--color always` the second line is
`^[[32mgreen^[[0m done`.

### `export`: animated PNG

```console
$ shellshot export demo.shot -o demo.apng --theme light
wrote 3 frames (3.2 KB) to demo.apng
$ shellshot export demo.shot        # writes demo.apng next to the input
```

| Flag | Default | Meaning |
|---|---|---|
| `-o` | input with `.apng` | output file (never overwrites the input) |
| `--theme` | `dark` | built-in (`dark`, `light`) or a theme JSON file |
| `--font-size`, `--scale`, `--padding` | 14, 1, 8 | pixel size, integer scale, border (≥1) |
| `--speed` | 1 | playback speed multiplier |
| `--idle-limit` | 2s | longest pause between frames (0 = no limit) |
| `--hold` | 2s | how long the last frame stays |
| `--loops` | 0 | plays (0 = forever) |
| `--min-delay` | 20ms | shorter frames are merged into the previous one |
| `--cursor` | `block` | `block`, `bar`, `underline`, `none` |

A theme file is JSON with all fields required:
`{"name":"x","foreground":"#rrggbb","background":"#rrggbb","cursor":"#rrggbb","palette":[16 × "#rrggbb"]}`.
Output is written atomically (a failed run leaves no partial file).

## Recording format

- **Today: JSON Lines, version 2**, extension `.shot`. Line 1 is a header
  (`version`, `cols`, `rows`, `command`, `started`); each following line is a
  frame (`t` in ms, `lines`, cursor, `title`, optional `spans` for styles and
  `widths` for wide/zero-width runes). The reader also accepts version 1
  (unstyled).

  ```
  {"version":2,"cols":60,"rows":10,"command":["zsh","-c","echo hello; ..."],"started":"..."}
  {"t":101,"lines":["hello","",...],"cx":0,"cy":1,"cv":true,"title":""}
  {"t":513,"lines":["hello","green done",...],"cx":0,"cy":2,"cv":true,"title":"","spans":[[],[[0,5,"32"]],...]}
  ```
- **Next: a compressed binary format.** It is designed in
  [docs/research/binary-format.md](docs/research/binary-format.md). Building blocks
  (`internal/recording/bin/{varint,codec,head,event,chunk}`, with zstd) and a
  binary writer and reader (`internal/recording/bin`) exist, but nothing uses them
  yet: `record` still writes JSON and `replay` and `export` read JSON only.
- Full JSON field reference: [docs/ARCHITECTURE.md](docs/ARCHITECTURE.md#recording-format-json-lines-extension-shot).

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
internal/cli           cobra commands and flags (version, record, snap, replay, export)
internal/snapshot      pure data: Snapshot, Cursor, Frame, Style (no deps)
internal/vt            Terminal interface + Factory (no cgo)
internal/vt/ghostty    the only cgo package: libghostty adapter
internal/pty           spawn a command in a PTY
internal/tty           IsTerminal, Size, MakeRaw
internal/live          stdin -> process pump and live output tee
internal/session       pty + vt + sampler -> frames emitted to a sink
internal/recording     file format (JSON Lines Reader/Writer); bin/ = binary format parts
internal/replay        play frames to an io.Writer with timing
internal/export        timeline (timing), apng (encoder), pipeline (frames -> APNG)
internal/render        font (embedded), theme, raster (frame -> image), sprite (box/block/braille/powerline glyphs)
scripts/build-libghostty.sh, Makefile
```

Dependency rules: `snapshot` imports nothing; only `vt/ghostty` imports cgo;
only `cli` imports cobra. Interfaces are declared at the consumer.

## Export details

- APNG output, `dark` and `light` built-in themes, or your own theme JSON.
- Text is drawn with an **embedded JetBrains Mono** (Regular, Bold, Italic, Bold
  Italic; licence in `internal/render/font/assets/OFL.txt`). Box-drawing, block,
  braille and powerline characters are drawn as sprites.
  Symbols Nerd Font Mono is bundled as a fallback for icon glyphs the main font
  lacks.
- Idle gaps are capped by `--idle-limit`; very short frames are merged.

## Known limitations

- **Fixed terminal size during recording.** The format has one size; SIGWINCH is
  not propagated, so resizing your terminal mid-recording does not change it.
- **Trailing coloured blanks are dropped.** Lines are right-trimmed, so a
  background colour painted on trailing spaces (e.g. a full-width status bar's
  empty part) is lost.
- **CJK, emoji and other glyphs missing from JetBrains Mono are not drawn.** Wide
  characters are recorded and keep their two-cell column positions, but the only
  fallback font bundled is Symbols Nerd Font Mono (icons and Powerline/Nerd Font
  symbols), so CJK and emoji cells render blank in `export`. (`snap` and `replay`
  print them as text.)
- **Nerd Font icons are clipped to their cell.** Icons from the fallback font are
  not yet scaled to fit, so oversized ones are cut off at the cell edge.
- JSON recordings are large and store full-screen text per frame; there is no compression yet.
- Stable only on macOS; no input events or resize events are recorded.

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
- **Colours missing in `snap`/`replay` output**: `auto` disables colour when piped or
  when `NO_COLOR` is set; pass `--color always`.

# shellshot — Architecture

See the [README](../README.md) for build and usage instructions.

POC: run a command in a PTY, feed its output into a **headless terminal
(libghostty-vt)**, take screen **snapshots**, and store them as a **recording**
that can be replayed.

## Commands (cobra)
| Command | Purpose |
|---|---|
| `shellshot record [-o file] [-c "cmd"] [--cols N --rows N] [--interval d] [--ignore-exit-status] [-- cmd args...]` | Interactive recorder (asciicast-style): runs $SHELL (or the given command) on a live passthrough terminal and records snapshot frames. Exits with the child's exit code (128+sig for signal deaths) after writing the recording, unless `--ignore-exit-status` |
| `shellshot replay file [--speed f] [--clear] [--color auto\|always\|never]` | Play recording frames to stdout with original timing; `--clear` defaults to on for a terminal; `--color auto` (default) emits SGR only on a terminal with `NO_COLOR` unset |
| `shellshot version` | Print version |

## Package layout (module `github.com/michaellorant/shellshot`)
```
cmd/shellshot/main.go     // ONE line: os.Exit(cli.Run(os.Args[1:]...))-style; no logic
internal/cli              // cobra wiring only: flags -> config structs -> calls domain pkgs. Each command in its own file.
internal/snapshot         // pure data: Snapshot, Cursor, Frame (no deps)            [exists]
internal/vt               // interface Terminal + Factory (no cgo)                   [exists]
internal/vt/ghostty       // cgo adapter over go.mitchellh.com/libghostty implementing vt.Terminal
internal/pty              // spawn command in a PTY: Process{Read, Write, Resize, Wait, Close}
internal/tty              // IsTerminal, Size, MakeRaw; wraps golang.org/x/term (no cgo)
internal/live             // stdin -> process pump + output tee; depends only on io interfaces
internal/session          // orchestrates pty + vt + sampler -> emits snapshot.Frame to a Sink
internal/recording        // file format (JSON Lines): Writer, Reader
internal/replay           // plays frames to an io.Writer with timing
scripts/build-libghostty.sh, Makefile   // dependency + build tooling
```
Dependency rules (enforced in review): `snapshot` imports nothing; `vt`, `recording`,
`replay` import only `snapshot`; `session` imports `snapshot`, `vt`, `pty` (via small
interfaces it declares itself); only `vt/ghostty` imports cgo/libghostty; only `cli`
imports cobra and wires concrete types. Define interfaces at the consumer.

## libghostty: git submodule, built from source with zig only
libghostty-vt is built from source. `third_party/ghostty` is a git submodule of
`ghostty-org/ghostty` pinned to commit `66af4c7458db7e19ae7adb44aa7054879be58b6b`
(the commit `go.mitchellh.com/libghostty` pins; its headers match). Bump the
pin by checking out another commit in the submodule and committing the pointer.
`scripts/build-libghostty.sh` (`make deps`) runs
`git submodule update --init --depth 1 third_party/ghostty` and then, inside the
submodule, `zig build -Demit-lib-vt -Doptimize=ReleaseFast --prefix <repo>/build/libghostty`.
Only zig (0.17.x, per `minimum_zig_version` in the submodule's `build.zig.zon`) is
needed: no xcodebuild, no cmake. The output (git-ignored under `/build/`) is
`build/libghostty/{include,lib/libghostty-vt.a,lib/pkgconfig/libghostty-vt-static.pc}`.
The Go binding links via pkg-config, so builds run with
`PKG_CONFIG_PATH=$PWD/build/libghostty/lib/pkgconfig` (set by the Makefile).
`make clean` keeps `build/libghostty`; `make distclean` removes it.
POC target: macOS (native arch). Linux/Windows: out of scope.

## Recording format (JSON Lines, extension `.shot`)
Line 1 header: `{"version":2,"cols":80,"rows":24,"command":["ls"],"started":"RFC3339"}`
Following lines, one per frame: `{"t":<ms int>,"lines":[...],"cx":0,"cy":0,"cv":true,"title":"","spans":[...],"widths":[...]}`

- Version 2 is written; Reader accepts versions 1 and 2 (other versions → `ErrVersion`). Version 1 has no `spans`: its frames are unstyled (`Spans` nil).
- `spans` is optional and emitted only when the frame has at least one span. It has exactly `rows` entries (one per line); each entry is an array of `[start,end,"<SGR>"]` triples, e.g. `"spans":[[[0,3,"31"]],[],[]]`. `start`/`end` are a half-open range of rune indexes into that line; `<SGR>` is `snapshot.Style.SGR()` (parsed with `snapshot.ParseSGR`). Spans are non-empty, in range, non-zero-styled, sorted and non-overlapping (`Snapshot.Validate`); Writer refuses to write, and Reader rejects (`ErrMalformed`), frames that violate this.
- `widths` is optional and emitted only when some line has an entry (additive to version 2: the version stays 2, and version 2 files without it still read). It has exactly `rows` entries (one per line); each entry is an array of `[runeIndex,cells]` pairs, e.g. `"widths":[[[0,2],[1,2]],[],[]]`, mirroring `snapshot.Wide`: `runeIndex` indexes runes of that line and `cells` is 2 (wide) or 0 (zero-width); runes without a pair are one cell. Pairs are sorted, unique and in range (`Snapshot.Validate`); Writer refuses to write, and Reader rejects (`ErrMalformed`), violating frames. Version 1 has no `widths`; the key is ignored there, as `spans` is.

## Cell widths (`Snapshot.Widths`)
`Lines` holds text, so a rune is not always one terminal cell, yet `Spans` and `Widths` both index **runes**. `Widths` is a sparse record of the exceptions so a rasteriser can place runes in the same cell columns libghostty used: `nil`/empty (every rune is one cell) or exactly `Rows` entries, one `[]snapshot.Wide{Rune, Cells}` per line. `Rune` is a rune index into that line; `Cells` is 2 for a wide rune (e.g. CJK) or 0 for a zero-width rune (e.g. a combining mark). Runes without an entry occupy exactly one cell. Entries are sorted by `Rune`, unique and within the line's rune count; `Cells` is 0 or 2 (`Snapshot.Validate`). `Snapshot.Columns(row)` returns each rune's starting cell column (a prefix sum of widths; `"世界ab"` → `0,2,4,5`), and `Snapshot.CellWidth(row, runeIdx)` the cells one rune occupies. Lines are right-trimmed, so cell columns are only meaningful up to the last rune. `Equal` treats nil and all-empty `Widths` as equal. The adapter (populating `Widths`) is a separate follow-up; the recording format is described under Recording format.

## Session semantics
- Session owns one goroutine reading PTY output and writing to the terminal; snapshots are taken on that goroutine (vt is not concurrency-safe) on a ticker, emitted only if different from the previous frame; first frame at t=0 and a final frame on process exit (after draining output).
- Context cancellation kills the process and returns.

## Workflow
Worktrees in `.worktrees/<task>`, branch `task/<task>`. Workers write code + tests only;
the **build subagent** runs `make test`/`make build`; the **merge subagent** merges
to `main` (fast-forward/squash, one task = one commit, Conventional Commits).

## Interactive `record` (asciicast-style) — design addendum
- No command given → run `$SHELL` (fallback `/bin/sh`) interactively. `-c` runs a shell command string via `$SHELL -c`; `-- cmd args` runs argv directly. Recording ends when the child exits.
- When stdin is a TTY: put it in raw mode (restored on exit), forward stdin → PTY. PTY output is teed to the real stdout live AND fed to the headless terminal (session).
- Size defaults to the current terminal size (override with --cols/--rows). POC limit: size is fixed for the recording; SIGWINCH is not propagated (the format has one fixed size). Follow-up: resize events in the format.
- Non-TTY stdin/stdout still works (pipes): no raw mode, no size detection (80x24 default). When a non-TTY stdin ends (EOF), `live.Pump` forwards one EOT byte (0x04, Ctrl-D) to the process (`PumpOptions.OnEOF`), so `printf 'echo hi\n' | shellshot record` ends the shell like a user pressing Ctrl-D. It is never sent when stdin is a TTY.
- New packages: `internal/tty` (IsTerminal, Size, MakeRaw; wraps golang.org/x/term, no cgo) and `internal/live` (stdin→process pump + output tee; depends only on io interfaces). `pty.Process` gains `Write` (io.ReadWriter). `session` is unchanged: the tee is a Process decorator supplied by `live`.

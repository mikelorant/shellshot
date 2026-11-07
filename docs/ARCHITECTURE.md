# shellshot — Architecture

See the [README](../README.md) for build and usage instructions.

POC: run a command in a PTY, feed its output into a **headless terminal
(libghostty-vt)**, take screen **snapshots**, and store them as a **recording**
that can be replayed.

## Commands (cobra)
| Command | Purpose |
|---|---|
| `shellshot record [-o file] [-c "cmd"] [--cols N --rows N] [--record-input] [--coalesce DUR] [--codec none\|flate\|zstd] [--ignore-exit-status] [-- cmd args...]` | Interactive recorder (asciicast-style): runs $SHELL (or the given command) on a live passthrough terminal and writes the binary event log (raw output with timing, exit status; keystrokes only with `--record-input`). Exits with the child's exit code (128+sig for signal deaths) after writing the recording, unless `--ignore-exit-status` |
| `shellshot replay file [--speed f] [--fps N] [--clear] [--color auto\|always\|never]` | Read a binary recording, replay it through libghostty (`recording/frames`, sampled at `--fps`, default 60, 1..1000) and play the frames to stdout with original timing; `--clear` defaults to on for a terminal; `--color auto` (default) emits SGR only on a terminal with `NO_COLOR` unset. A file with the wrong magic is rejected (`not a shellshot recording (bad magic)`); a torn tail (`bin.ErrTruncated`) plays the valid prefix, prints `shellshot: recording is truncated (N bytes valid)` to stderr and exits 0; a clean file with no EXIT event (recorder killed) plays in full, prints `shellshot: recording is incomplete (no exit event; recorder was interrupted)` to stderr and exits 0; `bin.ErrCorrupt` is fatal. Resize events change the frame size mid-stream, which replay just prints |
| `shellshot export file [-o out.apng] [--fps N] [timeline, theme and raster flags]` | Same reading path as `replay`, rendered to an animated PNG by `export/pipeline`. The pipeline has a fixed canvas (the header size), so a recording that resizes fails with `recording changes size mid-way (...); export does not support resizes yet` (known limitation). Truncated and interrupted recordings give the same stderr notes as `replay` and still export (exit 0) |
| `shellshot info file` | Header facts (size, start, duration, recorder, engine, argv, input flag) and event statistics (blocks, events by kind, OUT bytes, file size against raw payload, status `complete`, `interrupted` (clean chunk boundary but no EXIT event), `truncated (N bytes valid)` or `damaged`) from a scan with `bin.OpenAt`, without libghostty |
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
internal/recording/bin    // binary format: container (chunk, varint, codec, head, event), Writer, Reader (written by record; read by replay, export, info)
internal/recording/frames // binary event log -> snapshot frames through a vt.Factory (Options.FPS)
internal/replay           // plays frames to an io.Writer with timing
scripts/build-libghostty.sh, Makefile   // dependency + build tooling
```
Dependency rules (enforced in review): `snapshot` imports nothing; `vt`,
`replay` import only `snapshot`; `recording/bin` imports only its own subpackages; `recording/frames` imports `snapshot`, `vt` and `recording/bin`; `session` imports `snapshot`, `vt`, `pty` (via small
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

## Recording format: binary event log, extension `.shot`
`record` writes the format designed in `docs/research/binary-format.md` (`internal/recording/bin`): a chunk file with a `HEAD` chunk then zstd/flate/none-compressed `EVNT` blocks of events `OUT`, `IN` (only with `--record-input`, which sets head flag bit 0), `RESIZE`, `EXIT`. `record` wires it through `session.Config.Events` (a `binSink` in `internal/cli/eventsink.go` over `bin.Writer`):
- HEAD: size, default 100 µs tick, start time and zone, engine `libghostty-vt` at the pinned submodule commit, `TERM`, argv as executed, an env allowlist (`TERM`, `COLORTERM`, `LANG`, `SHELL`), random 16-byte session id, recorder `shellshot <version>`. Not recorded: cwd, host name, other environment, theme (themes apply at export time). `--coalesce` merges adjacent OUT events within a duration and sets head flag bit 2.
- One clock: event times are offsets from the session start (`Config.OnStart` hands the origin to the sink, so `IN` events logged by the process-write decorator `inputRecorder` share it). `bin.Writer` is single-owner, so `binSink` serialises the session goroutine and the input pump with a mutex; no goroutine of its own.
- Flushing: `Config.OnTick` calls `Writer.Tick` from the session ticker (4 Hz for `record`), so a quiet session flushes on the 2 s interval; with nothing pending Tick writes no bytes. The file is written live (no temp file): after a crash it reads up to the last good block (`bin.ErrTruncated` for a torn tail). A recorder killed exactly at a block boundary leaves no torn tail, so completeness is judged by the `EXIT` event, which `session` logs when the child ends: `info` reports a clean file without one as `interrupted`, and `replay`/`export` warn.
- `session` still runs the headless terminal and samples frames into a discarding sink; `record` no longer has `--fps` (sampling is moving to replay/export).
- `replay`, `export` and `info` read it back (`bin.Reader`/`bin.OpenAt`); `recording/frames` replays the events through libghostty into `snapshot.Frame`s. There is no other recording format: files that do not start with the shellshot magic are rejected (`bad magic`).

## Cell widths (`Snapshot.Widths`)
`Lines` holds text, so a rune is not always one terminal cell, yet `Spans` and `Widths` both index **runes**. `Widths` is a sparse record of the exceptions so a rasteriser can place runes in the same cell columns libghostty used: `nil`/empty (every rune is one cell) or exactly `Rows` entries, one `[]snapshot.Wide{Rune, Cells}` per line. `Rune` is a rune index into that line; `Cells` is 2 for a wide rune (e.g. CJK) or 0 for a zero-width rune (e.g. a combining mark). Runes without an entry occupy exactly one cell. Entries are sorted by `Rune`, unique and within the line's rune count; `Cells` is 0 or 2 (`Snapshot.Validate`). `Snapshot.Columns(row)` returns each rune's starting cell column (a prefix sum of widths; `"世界ab"` → `0,2,4,5`), and `Snapshot.CellWidth(row, runeIdx)` the cells one rune occupies. Lines are right-trimmed, so cell columns are only meaningful up to the last rune. `Equal` treats nil and all-empty `Widths` as equal. The libghostty adapter populates `Widths`; they are produced by replaying a recording (`recording/frames`) and are not stored in it.

## Session semantics
- Session owns one goroutine reading PTY output and writing to the terminal; snapshots are taken on that goroutine (vt is not concurrency-safe) on a ticker at `Config.FPS` (default 60; `Config.OnTick` also runs on each tick; a tick only snapshots if output arrived since the last one, so idle sessions cost nothing), emitted only if different from the previous frame; first frame at t=0 and a final frame on process exit (after draining output).
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

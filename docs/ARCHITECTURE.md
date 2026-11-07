# shellshot — Architecture

POC: run a command in a PTY, feed its output into a **headless terminal
(libghostty-vt)**, take screen **snapshots**, and store them as a **recording**
that can be replayed.

## Commands (cobra)
| Command | Purpose |
|---|---|
| `shellshot record [-o file] [--cols N --rows N] [--interval d] -- cmd args...` | Run cmd in a PTY, snapshot on an interval (only when the screen changed) plus a final frame, write recording |
| `shellshot snap [--cols --rows] -- cmd args...` | Run cmd, print the final screen as plain text |
| `shellshot replay file [--speed f]` | Play recording frames to stdout with original timing |
| `shellshot version` | Print version |

## Package layout (module `github.com/michaellorant/shellshot`)
```
cmd/shellshot/main.go     // ONE line: os.Exit(cli.Run(os.Args[1:]...))-style; no logic
internal/cli              // cobra wiring only: flags -> config structs -> calls domain pkgs. Each command in its own file.
internal/snapshot         // pure data: Snapshot, Cursor, Frame (no deps)            [exists]
internal/vt               // interface Terminal + Factory (no cgo)                   [exists]
internal/vt/ghostty       // cgo adapter over go.mitchellh.com/libghostty implementing vt.Terminal
internal/pty              // spawn command in a PTY: Process{Read, Resize, Wait, Close}
internal/session          // orchestrates pty + vt + sampler -> emits snapshot.Frame to a Sink
internal/recording        // file format (JSON Lines): Writer, Reader
internal/replay           // plays frames to an io.Writer with timing
scripts/fetch-libghostty.sh, Makefile   // dependency + build tooling
```
Dependency rules (enforced in review): `snapshot` imports nothing; `vt`, `recording`,
`replay` import only `snapshot`; `session` imports `snapshot`, `vt`, `pty` (via small
interfaces it declares itself); only `vt/ghostty` imports cgo/libghostty; only `cli`
imports cobra and wires concrete types. Define interfaces at the consumer.

## libghostty: distributed release, not compiled in-tree
We never build Ghostty/zig. `scripts/fetch-libghostty.sh` downloads the official
release asset `ghostty-vt.xcframework.zip` from the GitHub release of
`ghostty-org/ghostty` (currently the `tip` release; version pinned via a
variable + SHA-256 recorded in the script), extracts
`macos-arm64_x86_64/{libghostty-vt.a,Headers}` into `third_party/libghostty/`
(git-ignored) and generates `third_party/libghostty/lib/pkgconfig/libghostty-vt-static.pc`.
The Go binding `go.mitchellh.com/libghostty` links via pkg-config, so builds run with
`PKG_CONFIG_PATH=$PWD/third_party/libghostty/lib/pkgconfig` (set by the Makefile).
POC target: macOS (arm64/x86_64). Linux/Windows: out of scope.

## Recording format (JSON Lines, extension `.shot`)
Line 1 header: `{"version":1,"cols":80,"rows":24,"command":["ls"],"started":"RFC3339"}`
Following lines, one per frame: `{"t":<ms int>,"lines":[...],"cx":0,"cy":0,"cv":true,"title":""}`

## Session semantics
- Session owns one goroutine reading PTY output and writing to the terminal; snapshots are taken on that goroutine (vt is not concurrency-safe) on a ticker, emitted only if different from the previous frame; first frame at t=0 and a final frame on process exit (after draining output).
- Context cancellation kills the process and returns.

## Workflow
Worktrees in `.worktrees/<task>`, branch `task/<task>`. Workers write code + tests only;
the **build subagent** runs `make test`/`make build`; the **merge subagent** merges
to `main` (fast-forward/squash, one task = one commit, Conventional Commits).

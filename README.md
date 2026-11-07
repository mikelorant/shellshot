<p align="center"><picture><source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg"><img alt="shellshot" src="docs/assets/logo.svg" width="360"></picture></p>

Record terminal sessions. Replay them. Export them as animated PNGs, GIFs, WebPs or SVGs.

# shellshot

## What it does

`shellshot` records a terminal session, either your shell or a single command,
through a headless terminal emulator ([libghostty](https://github.com/ghostty-org/ghostty)).
It stores the session as a compact event log (a `.shot` file), replays it in your
terminal, and exports it as an animated PNG, GIF, WebP or SVG.

## Install

Requirements: Go 1.26 or newer with cgo, a C toolchain (Xcode command line tools),
`git`, `pkg-config` and [zig](https://ziglang.org) 0.17.x (`brew install zig`). Only macOS on
Apple silicon is tested; other platforms are not supported.

```sh
git clone https://github.com/mikelorant/shellshot.git
cd shellshot
make deps     # fetches the pinned libghostty source and builds it with zig
make build    # writes bin/shellshot
```

The first `make deps` downloads zig dependencies and takes several minutes. There is no
`make install`: copy the binary to a directory on your `PATH`, for example:

```sh
mkdir -p ~/.local/bin && cp bin/shellshot ~/.local/bin/
shellshot version
```

Build details: [docs/development.md](docs/development.md).

## Use

Record your shell until you exit it, or record one command:

```console
$ shellshot record -o demo.shot
$ shellshot record -o demo.shot -c 'ls -l'
recorded 3 events (0.4 KB) to demo.shot
$ shellshot record -o demo.shot --cols 100 --rows 30 -- ls -l
```

`record` runs `$SHELL` when you give it no command. The terminal size defaults to
your current one (80x24 when stdin is not a terminal). Keystrokes are not stored
unless you pass `--record-input`. If the command exits non-zero, the recording is still
written and `record` exits with the command's status; `--ignore-exit-status` exits 0.

Play it back in the terminal, at any speed:

```sh
shellshot replay demo.shot --speed 2
```

Export it as an animated PNG:

```console
$ shellshot export demo.shot -o demo.png --scale 2 --theme light
wrote 2 frames (20.3 KB) to demo.png
```

The file is a standard PNG with animation data. Browsers, GitHub and Slack animate it;
at the time of writing, macOS Preview and Quick Look show only the first frame.
`--scale 2` gives sharp images on retina displays. Without `-o`, the output is the input
name with a `.png` extension. To match the look of a Ghostty terminal, see
[docs/export.md](docs/export.md#cell-size).

Show a recording's header and statistics (no terminal replay, so it is fast):

```console
$ shellshot info demo.shot
file          demo.shot
size          80x24
duration      6.8ms
command       /opt/homebrew/bin/zsh -c ls -l
events        3
status        complete
```

Every command has `--help`. Output formats (`--format`), themes, fonts, cell size and timing flags for `export` are in
[docs/export.md](docs/export.md). The file format is in [docs/format.md](docs/format.md).

[contrib/examples/](contrib/examples/) has safe demo scripts (colours, emoji, cursor, animations) to record;
`make examples` renders them all to `build/examples`.

## Script

A `.script` file types into a shell and waits for output for you, so a recording does not depend on
your typing. Save this as `demo.script`:

```
shell /bin/sh
env PS1="$ "
size 50x8
output demo.shot

scene greet {
  linger 500ms
  run echo hello, world
  wait /^hello, world$/ 0s    # anchored: the typed command is on screen too
}
```

```console
$ shellshot record --script demo.script
recorded 28 events (0.5 KB) to demo.shot
$ shellshot info demo.shot | grep -A1 ^scenes
scenes        1
  1 greet  0:00.2 +2.1s
```

`--list` checks the script and prints the plan and its estimated duration without running it
(exit 2 and `file:line:col: message` if it is invalid). `-o`, `--cols` and `--rows` beat the script's
`output` and `size`. `--watch` shows the terminal while it runs. A failed `wait` or a `run` that never
goes quiet exits 3 and keeps the partial recording; otherwise `record` exits with the shell's status,
as without `--script`. Scenes and hidden blocks are stored in the file and used by
`export`. The format and the exit codes are in [docs/scripting.md](docs/scripting.md);
[contrib/examples/scripts/](contrib/examples/scripts/) has runnable samples (`make test-scripts`).

Not built yet: `fresh`, `cleanup`, `caption`, `require` and `export --scene`.

Without a script, a recording is made from the command line. Run a command, or pipe input to a
shell, and get a recording with a fixed size:

```sh
shellshot record -o ci.shot --cols 100 --rows 30 -c 'make test'
printf 'echo hi\nexit\n' | SHELL=/bin/sh shellshot record -o hi.shot --cols 40 --rows 6
```

When stdin is a pipe, its end sends Ctrl-D to the program, so a shell reading it exits.

`record` passes the command's exit status through after it writes the file, so a failing
command fails the CI step and still leaves a recording to attach. Add `--ignore-exit-status`
to always exit 0.

`export` takes a file and writes a file, so it works in a loop:

```sh
for f in recordings/*.shot; do shellshot export "$f" --scale 2; done
```

## Limits

- The terminal size is fixed for a whole recording. Resizing your terminal while recording does
  not change it; set `--cols` and `--rows` instead.
- Lines are right-trimmed, so a background colour painted on trailing spaces is lost in replay and
  export.
- Export draws emoji up to Emoji 18.0 and a subset of CJK characters. Others are blank unless you
  pass `--emoji-font` (emoji) or `--fallback-font`. See [docs/export.md](docs/export.md#fonts-emoji-and-cjk).
- Emoji are drawn with Apple-derived artwork by default, with Noto Color Emoji as fallback
  (`--emoji-set apple|noto`, `--emoji-font FILE`). Apple's artwork belongs to Apple Inc. and is not
  licensed by this project, so exports that contain it are your responsibility to share;
  `--emoji-set noto` is the open source (SIL OFL 1.1), licence-clean choice.

## Troubleshooting

- `pkg-config` reports `libghostty-vt-static` not found: run `make deps` and build with `make build`.
  The Makefile sets `PKG_CONFIG_PATH`; a plain `go build` does not.
- `zig not found`: `brew install zig`, and check that `zig version` prints 0.17.x.
- Build errors about missing libghostty headers after switching worktrees or moving the checkout:
  `go clean -cache`, then `make deps build`.

Licences and third-party attributions: [NOTICE](NOTICE).

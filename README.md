<p align="center"><picture><source media="(prefers-color-scheme: dark)" srcset="docs/assets/logo-dark.svg"><img alt="shellshot" src="docs/assets/logo.svg" width="360"></picture></p>

<p align="center">Record terminal sessions. Replay them. Export them as animated PNGs, GIFs, WebPs or SVGs.</p>

<p align="center"><img src="docs/assets/showcase.png" width="736" alt="A shellshot export of its demo: colours, text styles, Unicode, emoji, cursor control, animation and a dashboard."></p>

## What it does

`shellshot` runs your shell or a single command on a pseudo-terminal, feeds the output to a headless
terminal emulator ([libghostty](https://github.com/ghostty-org/ghostty)) and stores it as a compact
event log (a `.shot` file). You can replay the file in your terminal, or export it as an animated
image. A script can drive the session so that a recording does not depend on your typing.

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
`make install`; copy the binary onto your `PATH`:

```sh
mkdir -p ~/.local/bin && cp bin/shellshot ~/.local/bin/
shellshot version
```

Build details: [docs/development.md](docs/development.md).

## Use

Record your shell until you exit it, or record one command:

```console
$ shellshot record -o demo.shot
$ shellshot record -o demo.shot -c 'echo hello'
recorded 2 events (0.3 KB) to demo.shot
$ shellshot record -o demo.shot --cols 100 --rows 30 -- ls -l
```

`record` runs `$SHELL` when you give it no command. The terminal size is fixed for the whole
recording: it defaults to your current one (80x24 when stdin is not a terminal), and `--cols` and
`--rows` set it. Keystrokes are not stored unless you pass `--record-input`. If the command exits
non-zero, the recording is still written and `record` exits with the command's status, so a failing
command fails a CI step and still leaves a recording to attach; `--ignore-exit-status` exits 0.
When stdin is a pipe, its end sends Ctrl-D to the program, so a shell reading it exits:

```sh
printf 'echo hi\nexit\n' | SHELL=/bin/sh shellshot record -o hi.shot --cols 40 --rows 6
```

Play it back in the terminal, at any speed:

```sh
shellshot replay demo.shot --speed 2
```

On a terminal, each frame is drawn in place as one synchronized update (DEC mode 2026), without
clearing the screen first, so supporting terminals do not flicker. A recording larger than the
terminal is cropped to it, keeping the newest rows in view, with a note on stderr; `--no-crop`
turns that off. Resizing the terminal during playback is not followed.

Export it as an animated image:

```console
$ shellshot export demo.shot -o demo.png --scale 2 --theme light
wrote 1 frames (9.1 KB) to demo.png
```

Formats: `apng` (default), `gif`, `webp` and `svg`, chosen by the extension of `-o` or by
`--format`; the comparison is in [docs/export.md](docs/export.md#output-format). Without `-o`, the
output is the input name with the format's extension. `--scale 2` gives sharp images on retina
displays. An APNG is a standard PNG with animation data: browsers, GitHub and Slack animate it, while
macOS Preview and Quick Look show one image, which export makes the final screen
([`--poster`](docs/export.md#poster-apng)). Use PNG or GIF for READMEs and chat; SVG animation
depends on the viewer. To match a Ghostty terminal, see [cell size](docs/export.md#cell-size).

`export` takes a file and writes a file, so it works in a loop:

```sh
for f in recordings/*.shot; do shellshot export "$f" --scale 2; done
```

Show a recording's header and statistics (abridged; the file is not replayed, so it is fast):

```console
$ shellshot info demo.shot
file          demo.shot
size          80x24
duration      5ms
command       /opt/homebrew/bin/zsh -c echo hello
events        2
status        complete
```

Every command has `--help`. Themes, fonts, cell size and timing flags are in
[docs/export.md](docs/export.md); the file format is in [docs/format.md](docs/format.md).
[contrib/examples/](contrib/examples/) has safe demo programs (colours, emoji, cursor, animations)
to record; `make examples` renders them all to `build/examples`.

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

Reserved, not built: `fresh`, `cleanup`, `caption`, `require` and `export --scene`.

## Limits

- Resizing your terminal while recording does not change the recording; set `--cols` and `--rows`.
- Export draws emoji up to Emoji 18.0 and a subset of CJK characters (about 3,750 hanzi, Hangul,
  kana). Others are blank unless you pass `--emoji-font` (emoji) or `--fallback-font`, or
  `--system-fonts` (which makes the output depend on your machine). See
  [docs/export.md](docs/export.md#fonts-emoji-and-cjk).
- Recorded but not drawn: underline colour, overline, hyperlink targets, window titles and inline
  images. Blink is drawn steady.
- A GIF has at most 255 colours plus transparency; a WebP is lossless but its support in chat tools
  and READMEs is unverified (see [docs/export.md](docs/export.md#webp)).
- A blank first frame shown for 250 ms or less is dropped, so viewers that show only the first
  frame do not show an empty terminal; `--keep-leading-blank` turns that off.
- Emoji are drawn with Apple-derived artwork by default, with Noto Color Emoji as fallback
  (`--emoji-set apple|noto`, `--emoji-font FILE`). Apple's artwork belongs to Apple Inc. and is not
  licensed by this project, so exports that contain it are your responsibility to share;
  `--emoji-set noto` is the open source (SIL OFL 1.1), licence-clean choice.

## Troubleshooting

- `pkg-config` reports `libghostty-vt-static` not found: run `make deps` and build with `make build`.
  The Makefile sets `PKG_CONFIG_PATH`; a plain `go build` does not.
- `zig not found`: `brew install zig`, and check that `zig version` prints 0.17.x.
- A glyph is blank in the export: the built-in fonts lack it. Pass `--fallback-font FILE`, or
  `--emoji-font FILE` for colour emoji.
- `wait "text"` in a script passes at once: the typed command is echoed on screen, so anchor the
  pattern (`/^text$/`).
- No colour in `replay` output: `--color auto` disables colour when stdout is not a terminal or
  `NO_COLOR` is set. Pass `--color always`.
- `replay` flickers or scrolls: the recording is probably larger than the terminal. `replay` crops
  to the terminal size; enlarge the terminal or record with `--cols` and `--rows` that fit it.
- Build errors about missing libghostty headers after switching worktrees or moving the checkout:
  `go clean -cache`, then `make deps build`.

Licences and third-party attributions: [NOTICE](NOTICE).

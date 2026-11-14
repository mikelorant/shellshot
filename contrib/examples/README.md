# Examples

Eleven POSIX `sh` demo scripts that print colourful, animated terminal output. They
are demo material for `shellshot` and a test bed for terminals. Each one needs a
terminal, fits 80x24, ends by itself and restores the terminal (cursor, alternate
screen, scroll region, attributes) on exit, Ctrl-C or SIGTERM. They only print
(plus `sleep`) and never write files, read input, or touch the network.

Run one: `sh contrib/examples/colors.sh`. Options: `--force` (run even when stdout is not
a terminal), `--pause` (wait for Enter at the end), `--help`.
`EXAMPLES_SPEED=fast` shortens every pause to a quarter.

## Record and export

    shellshot record -o colors.shot --cols 80 --rows 24 -c ./contrib/examples/colors.sh
    shellshot export colors.shot          # writes colors.png (animated PNG)
    shellshot export colors.shot --format gif   # or gif, webp, svg

`make examples` does this for every demo into `build/examples/` (fast speed).
`make showcase` records `agent.sh` at normal speed into the README image `docs/assets/showcase.svg` (on demand; see `docs/development.md`).
`make test-examples` runs each demo under dash, bash, sh and zsh on a pty.
`make check-examples` renders each demo with the real binary and fails on lines that wrap past column 80 or on characters no embedded font has (they would come out blank); run it after changing a demo. It needs libghostty and `uv`.

## Demos

| script | shows | time |
| --- | --- | --- |
| `colors.sh` | 16, 256 and truecolour backgrounds and gradients | 5 s |
| `attributes.sh` | bold, faint, italic, underline styles and colour, strike, overline, blink, hidden, inverse | 5 s |
| `unicode.sh` | box drawing, blocks, braille, wide CJK, combining marks | 4 s |
| `emoji.sh` | emoji, skin tones, ZWJ sequences, flags, keycaps | 7 s |
| `cursor.sh` | cursor addressing, save/restore, erase, insert/delete, scroll region, alternate screen | 16 s |
| `animations.sh` | spinner, progress bar, sparklines, rain, bouncing text | 12 s |
| `osc.sh` | window title (OSC 0/2) and hyperlinks (OSC 8) | 8 s |
| `buildlog.sh` | a fake build log with coloured levels and a retried test | 9 s |
| `dashboard.sh` | a TUI-style dashboard in the alternate screen | 10 s |
| `agent.sh` | a fake coding-agent session: typed shell commands, an agent TUI that "writes" a dashboard, then the dashboard runs (nothing is executed) | 55 s |
| `showcase.sh` | nine short scenes from the others | 34 s |

`lib.sh` holds the shared helpers (sourced, not run). `scripts/*.script` are
automation samples for `shellshot record --script FILE`, run from the repository
root; `make test-scripts` runs them all (about a minute).

## What the exporter draws

Checked by exporting the demos and looking at the frames. "Recorded" means the
bytes are in the `.shot` file and the terminal engine tracks them.

| feature | exported image |
| --- | --- |
| 16, 256 and truecolour foreground | drawn |
| background colour, inverse, underline, strikethrough on cells | drawn, including trailing blanks (`ESC[K` bars, colour swatches) |
| blanks with only a foreground colour, bold, italic, faint or blink | nothing to draw, trimmed |
| bold, faint, italic, inverse, strikethrough | drawn |
| underline: single, double, curly, dotted, dashed | drawn |
| underline colour (SGR 58) | recorded, not drawn (uses the text colour) |
| overline (SGR 53) | recorded, not drawn |
| blink | recorded, drawn steady |
| hidden (SGR 8) | text is not drawn |
| hyperlinks (OSC 8) | text drawn, no marker, link target dropped |
| window title (OSC 0/2) | recorded, not drawn |
| inline images (iTerm2, Kitty, Sixel) | recorded, not drawn |
| colour emoji, wide CJK, combining marks | drawn; CJK glyphs the built-in fonts lack (e.g. 語, 漢) come out blank without `--system-fonts` or `--fallback-font` |
| box drawing, blocks, braille | drawn |
| alternate screen, scroll regions, cursor moves | drawn as the screen state of each frame |

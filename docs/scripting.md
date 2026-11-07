Status: designed, partly implemented. The lexer, parser, validator and lowering to a run plan exist; the runner (`record --script`) does not.

# Script format

A `.script` file describes a terminal session to record: what to type, what to wait for, and how to
split it into scenes. The planned way to run one is `shellshot record --script FILE`; that flag does
not exist yet. Everything below is the design. The reasoning behind it is in
[research/script-format.md](research/script-format.md) and [research/script-scenes.md](research/script-scenes.md).

A script has top-level settings, then steps and blocks. It has no conditions, loops, variables or
functions. The whole file is parsed and validated before anything starts (exit 2 on error).

## Syntax rules

- One statement per line. Indentation is insignificant outside text blocks. The file is UTF-8.
- `#` starts a comment at the start of a token: a whole line, or after a statement's arguments.
  In rest-of-line text and text blocks `#` is literal.
- Everything is case-sensitive; keywords, setting names and key names are lower case.
- Strings are `"..."` with the escapes `\n \t \" \\ \e \xHH`. Any other backslash is an error.
- A pattern is a quoted string (substring match) or `/regex/` (Go RE2, no flags, matched per screen
  row; `\/` is a slash).
- Durations carry a unit: `ms`, `s` or `m` (`500ms`, `1.5s`, `2m`). A bare number is an error.
  Other literals are a percentage (`25%`), a size (`100x28`) and a speed (`8x`).
- Unknown commands, settings, keys and units are errors with `file:line:col` and a suggestion.

```
demo.script:12:3: unknown command "rn" (did you mean "run"?)
```

## Commands

| Command | Meaning |
|---|---|
| `run TEXT` | Type the text, press Enter, then wait for a quiet screen (`settle`). Fails the script if the screen is not quiet within `timeout` |
| `type TEXT` | Type the text without Enter |
| `press KEY...` | Press keys or chords: `press ctrl+c`, `press up up enter`. `x3` after a key repeats it: `press down x3` |
| `wait PATTERN [TIMEOUT]` | Wait until the visible screen matches the pattern. `wait "passed" 0s` checks once and fails at once if it does not match: this is the assertion |
| `wait idle [DURATION]` | Wait until the screen has had no output for `DURATION` (default `settle`) |
| `sleep DURATION` | Pause |
| `mark LABEL` | Add a `MARK` event to the recording |

Keys: `enter tab esc space backspace delete insert up down left right home end pageup pagedown`,
`f1` to `f12`, a single `a`-`z` or `0`-`9`, and chords with `ctrl+`, `alt+` or `shift+` in any order.

`run` waits for a quiet screen, not for the command to finish. A command that prints continuously
for longer than `timeout` makes the `run` fail. Write it with `settle off` and an explicit wait:

```
settle off
run make build; echo BUILD-STATUS=$?
wait /^BUILD-STATUS=0$/ 5m
```

Anchor patterns: the typed command is echoed on screen, so an unanchored `wait "DONE"` can match
the echo of `echo DONE`.

## Text forms for `run` and `type`

- Rest of line: `run npm install` types `npm install`. The text is trimmed; quotes, `#` and `\` are literal.
  It cannot start with `"`.
- Quoted: `type "echo \"hi\"\n"`. `\n` types Enter.
- Text block: `lines` types each line, with Enter between lines. `paragraphs` joins the lines of a
  paragraph with one space and separates paragraphs with a blank line. `run` adds a final Enter and
  settles once; `type` does not.

```
run lines {
  git commit -q -F - <<'EOF'
  initial commit
  EOF
}
```

The block body is raw text. The closing `}` is alone on its line, at the same indentation as `run` or
`type`; a `}` at any other indentation is part of the body. The common indentation of the body is
removed, trailing whitespace is trimmed, and leading and trailing blank lines are ignored.
A block is one step.

## Settings

| Setting | Value | Default | Kind |
|---|---|---|---|
| `size` | `COLSxROWS` | current terminal, else 80x24 | process |
| `shell` | program path and optional arguments | `$SHELL`, else `/bin/sh` | process |
| `env` | `KEY=VALUE`, repeatable | inherited | process |
| `cwd` | path | current directory | process |
| `seed` | integer, seeds `jitter` | 1 | process |
| `output` | recording path (`-o` wins) | `recording.shot` | process |
| `typing` | delay between typed characters | 50ms | behaviour |
| `jitter` | `0%` to `100%` variation of `typing` | 0% | behaviour |
| `timeout` | default for `wait` and `run` settling | 15s | behaviour |
| `settle` | quiet time `run` waits for, or `off` | 200ms | behaviour |
| `linger` | pause after every step so a viewer can read | 0s | behaviour |
| `speed` | export playback speed of a span (`8x`) | export default | export hint |
| `idle` | export cap on each pause in a span | none | export hint |
| `hold` | extra export time on the last screen of a span | none | export hint |

- Process settings must appear before the first step.
- Behaviour settings may appear anywhere. Inside a block, a setting applies from its line to the
  closing `}` and then reverts.
- Export hints go before the first step (script-wide) or inside a `scene`.
- `linger` follows `run`, `type`, `press` and `wait`, not `sleep` or `mark`, and once per text block.
- `shell` arguments are split on spaces and tabs. Double quotes group, with the escapes above inside
  them. Single quotes have no meaning and nothing is expanded: `shell /bin/sh -c "echo $HOME"` passes
  the text `echo $HOME` to `sh`.
- Explicit command-line flags win over the script, which wins over defaults.

## Blocks

```
scene NAME {      # letters, digits, _ and -; unique
  ...
}
hidden {          # runs and is recorded; export may drop it
  ...
}
```

A block contains at least one step. A `hidden` block may sit inside a `scene`; no other nesting is
allowed. Scenes and hidden blocks are recorded as `SPAN` events (see [format.md](format.md#spans)).
After the last step the runner always exits the shell inside an implicit hidden block, so a script
cannot leave a live shell running.

These words are reserved and give a "reserved for future use" error: `if else elif for while repeat
loop break let var const set fn func def define call return include import source require use copy
paste screenshot resize hide show theme font caption cleanup fresh export expect budget`.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | The script completed and the shell exited 0 |
| child's status | The script completed and the shell exited non-zero (`--ignore-exit-status` makes it 0) |
| 1 | Runtime error, including a program that cannot be started |
| 2 | Parse or validation error; nothing started |
| 3 | Script failure: a `wait` or assertion failed, a `run` did not settle, or the child exited when input was needed |
| 130 | Cancelled |

On a script failure the final screen goes to stderr and the recording is kept.

## Examples

A minimal script:

```
shell /bin/bash --noprofile --norc    # no startup files, so the prompt is ours
env PS1="$ "
size 60x10
linger 1s              # the viewer gets a second on the final screen

run echo hello, world
```

Scenes, a hidden setup and a text block:

```
size 100x28
shell /bin/sh
cwd /tmp/demo-repo
env PS1="\\w $ "
output git-demo.shot
typing 40ms

hidden {
  run lines {
    rm -rf .git README.md
    clear
  }
}

scene init {
  linger 800ms
  run git init -q
  run git status -s
}

scene commit {
  typing 70ms          # slower for the interesting part; reverts at the closing brace
  linger 1s
  run echo 'hi' > README.md
  run git add README.md
  run lines {
    git commit -q -F - <<'EOF'
    initial commit
    EOF
  }
  run git log --oneline
  wait /^[0-9a-f]{7,} .*initial commit$/ 10s
}

mark done
```

A deterministic smoke test with an assertion:

```
size 80x24
shell /bin/sh
cwd /tmp/ci-work
env PS1="$ "
env NO_COLOR=1
seed 42
jitter 25%
timeout 20s
output smoke.shot

scene version {
  run ./bin/app --version
  wait /^app v[0-9]+\.[0-9]+\.[0-9]+$/
}

scene help {
  run ./bin/app --help | head -n 5
  wait "Usage:" 0s      # run has settled, so the text must be on screen now
  mark help-shown
}
```

# Research: automation script format for shellshot (`record --script`)

Status: design brief. Read-only research; no repo file other than this one was changed.

**Evidence limits of this run (read first).**
- I had **no shell**, only file read and web tools. So I could **not** run `go list`, build, or run the throwaway Go program that encodes arrows with and without application cursor mode. Those facts are **unverified**. Section 5.2 gives a ~20-line program to run (≈5 min).
- The Go module cache was not at `~/go/pkg/mod/go.mitchellh.com/libghostty@v0.0.0-20261106041537-e247028bdf51/`. I read the binding from GitHub `main` (`key_encoder.go`, `key_event.go`), **not the pinned version**. The C headers in `build/libghostty/include/ghostty/vt/key/{encoder,event}.h` I read directly from the repo's build dir (`event.h` only the first 60 lines).
- I did not read `internal/cli/{capture,interactive}.go`, `internal/live`, `internal/pty`, `internal/export`, `internal/render`. From `record.go` and `session.go` (lines 1–80) I know only what is quoted below. Claims about the new `record` command tree and the binary-format MARK/RESIZE events come from the task statement and `docs/research/binary-format.md` (first 200 lines), not from code.
- Prior-art claims are labelled **fetched** (README or docs page read this run) or **memory** (unverified).

---

## 1. Summary and recommendation

**Build a ~10-command, line-oriented script language ("shotscript", `.shotscript`). It extends `shellshot record` via `--script FILE`. The executor is a pure, non-blocking state machine that the session's existing single goroutine polls. libghostty provides two things VHS cannot: a `Wait` that matches the real screen state, and a mode-aware key encoder.**

The language is VHS-flavoured so users already know it, with these VHS mistakes fixed:
- Bare-number durations: durations are always `500ms` / `2s`.
- Silently ignored late `Set`: `Set` and `Env` are legal only in a preamble, and misuse is a parse error.
- Last-line `Wait` default: `Wait` matches the whole visible screen.
- Browser dependency and heavyweight settings.

Commands (10): `Set`, `Env`, `Output`, `Type`, `Run`, `Key`, `Sleep`, `Wait`, `Mark`, `Resize`.

Decisions in one line each:
- **Time:** real-time only in v1. The clock is an interface, so a "skip delays" mode can come later. Recordings are reproducible in *inputs and screen at marks*, **not byte-identical**, because the child's output timing is real.
- **Keys:** use libghostty's `KeyEncoder` with `SetFromTerminal` for non-text keys (arrows, F-keys, Home/End, chords). Plain text bypasses it. The script package owns its own `Key` enum and stays pure Go.
- **Settings:** only run-affecting settings in the script (size, command, cwd, env, typing speed, timeout, jitter, seed). Theme and font are **not** script settings in v1 (the keyword is reserved).
- **Precedence:** CLI flags > script `Set` > defaults.
- **Exit codes:** script-level failures are distinct from the child's exit status.
- **Deferred:** `Hide/Show`, `Require`, `Source`, `Copy/Paste`, `Screenshot`, heredocs, `Wait` scopes.

---

## 2. Prior art

### 2.1 Comparison

| Tool | Model | Source type | Take | Leave |
|---|---|---|---|---|
| **VHS** `.tape` ([README](https://github.com/charmbracelet/vhs/blob/main/README.md)) | Line DSL; drives a real shell via ttyd, headless Chromium (xterm.js) and ffmpeg | **fetched** | Verb-per-line; `Type@50ms`; `Key[@t] [count]`; `Wait`/`Sleep`/`Env`/`Output`/`Require`; Ctrl+/Alt+ chords | Browser stack; ~25 visual `Set`s; `Source`; `Copy/Paste`; `Screenshot`; `Hide/Show`; the pitfalls below |
| **asciinema-automation** | Script file with directive comments (`#$ expect`, `#$ delay`, `#$ wait`, `#$ send`), driving a recording via pexpect-style matching | **memory**, one search hit for a similarly named tool (`asciinwriter`: `SEND/ENTER/EXPECT/DELAY`) | The idea that a *wait/expect on output* is the core primitive | Python/pexpect byte-stream matching (see 2.2) |
| **demo-magic** | Bash script sourcing `demo-magic.sh`; `pe "cmd"` prints and runs, `p "text"` prints only, `TYPE_SPEED` ([sample](https://github.com/paxtonhare/demo-magic/blob/master/samples/demo-template.sh)) | **fetched via search summary** | `Run` ≈ `pe`. A pre-typed/paced demo is just "type, Enter, pause" | Needs a real human to press keys; the script is code, not data |
| **doitlive** | Session file of commands; `#doitlive speed: 3` magic comments ([docs](https://doitlive.readthedocs.io/en/stable/)) | **fetched via search summary** | In-file `# key: value` directives are a cheap settings syntax | Interactive (human presses keys); not CI-friendly |
| **Terminalizer** | Records to YAML with timed `records`; config has `frameDelay`, `maxIdleTime` ([config.yml](https://github.com/faressoft/terminalizer/blob/master/config.yml)) | **fetched via search summary** | Idle-time capping (`maxIdleTime`) as an export-time concern | YAML as an *authoring* format; it is a recording format, not a script |
| **`script` / `scriptreplay`** | Raw typescript plus a separate timing file | **memory** | Confirms "raw bytes + timing" (already our binary format) | Not an authoring format |
| **expect / pexpect** | Programmatic send/expect | **memory** | `expect` + timeout failing loudly | Matching against a raw byte stream (see 2.2) |
| **GitHub Actions** | VHS ships a `vhs-action` | **memory** | CI usage = one command that reads a file and writes artifacts. For us: a single static binary, no browser | n/a |

### 2.2 What caused pain (and what we do about it)

1. **Ignored late settings (fetched).** VHS README: "Any setting except `TypingSpeed` applied after a non-setting or non-output command is ignored." The script author gets no error. → We make a late `Set` a parse error with line:col.
2. **Ambiguous duration units (fetched).** `Sleep 2` is seconds, `Set TypingSpeed 0.1` is 0.1 s/char, and `Sleep 100ms` is also legal. → Durations always carry a unit.
3. **Weak `Wait` defaults (fetched).** Defaults are regex `/>$/`, scope `Line`, timeout `15s`. Matching just the last line is fragile (wrapped lines, prompts that don't end in `>`). → Screen scope only.
4. **Sleep-driven flakiness (memory/inference).** Without a reliable wait, authors pad `Sleep`s. Too short breaks on slow CI; too long makes slow GIFs. → Make `Wait` first-class and robust, and document it as the right primitive.
5. **Echo matches itself (inference, classic expect trap).** `Run "make && echo DONE"` puts `DONE` on screen as typed echo *before* the build finishes, so `Wait "DONE"` returns instantly. → Wait matches in multiline mode; document `^DONE$`-style patterns (the echo line starts with a prompt).
6. **Browser stack (memory).** Chromium + ttyd + ffmpeg installs are the biggest CI cost. → N/A for us.

---

## 3. Proposed command set (10)

| # | Command | Purpose | Why it earns a slot |
|---|---|---|---|
| 1 | `Set NAME VALUE` | Preamble-only settings (below) | One extension point. New settings add no grammar |
| 2 | `Env NAME "value"` | Environment of the spawned process | Needed for reproducible prompts (`PS1`, `TERM`, `NO_COLOR`). Preamble-only because the process is spawned after the preamble |
| 3 | `Output "path.shot"` | Recording path | Lets a script be self-contained in CI. `-o` wins. Only `.shot` accepted in v1 |
| 4 | `Type "text"` | Send text, one character at a time at the typing speed | Core |
| 5 | `Run "cmd"` | `Type` + `Key Enter` | The 90% case; two lines otherwise. Pure sugar in the parser (desugars to Type+Enter), so the executor does not know it |
| 6 | `Key CHORD [count]` | Press a named key or chord | Core. Separate from `Type` so text never needs escaping for control keys |
| 7 | `Sleep DURATION` | Pause | Core |
| 8 | `Wait [/re/ \| "lit"]` | Block until the screen matches | The only robust sync primitive |
| 9 | `Mark "name"` | Emit a MARK event | Maps 1:1 to the format. Gives exports named anchors ("export at mark `done`"); also usable as a poor man's screenshot target |
| 10 | `Resize COLSxROWS` | RESIZE event plus SIGWINCH | The format has RESIZE events, and TUIs react to resizing. Cheap: syntax identical to `Set Size` |

### 3.1 Settings (all preamble-only)

| Setting | Default | Notes |
|---|---|---|
| `Size COLSxROWS` | Current terminal size, else 80x24 (as `record` does today) | One setting, not Cols/Rows/Width/Height |
| `Command "argv"` | `$SHELL`, else `/bin/sh` | Split into words (no shell involved). Direct-run TUIs: `Set Command "htop"` |
| `Cwd "dir"` | CWD of `shellshot` | |
| `TypingSpeed DURATION` | `50ms` | Per character. Overridable per command with `@` |
| `Jitter PERCENT` | `0%` | Seeded; see §6.3 |
| `Seed INT` | `1` | PRNG seed for Jitter |
| `Timeout DURATION` | `15s` | Default `Wait` timeout |

### 3.2 Decisions on the other items the task listed

- **Starting command/shell:** there is only `Set Command`. The default is an interactive shell and the script drives it with `Run`. For reproducible prompts the docs recommend `Set Command "bash --noprofile --norc"` plus `Env PS1 "$ "`. A separate `Shell` setting is redundant.
- **Theme / font-size hints:** **excluded from v1.** They are export-time concerns, and export CLI flags already exist. A second source of truth causes precedence bugs. The binary header's hint slot is the future home: a later `Set Hint.Theme "…"` writes the header hint, and export flag > header hint. Keyword reserved, so v1 errors with "unknown setting (reserved)".
- **Hide/Show: excluded for now.** VHS's semantic is "stop capturing frames". In an event log the child's output must still be replayed to keep the terminal state correct, so "hide" can only mean "drop this time range at export". That needs a marked range plus export support, and a design for the time gap. Researcher recommendation: setup that must not be visible is better served by `Set Command` + `Env` + `Set Cwd`, which cover most real cases. Revisit as `Mark "setup-end"` plus an export-time `--from-mark` once export lands. Never "skip recording": the recording should stay a faithful log.
- **Source/include: excluded.** Reuse is solved by the shell (`cat a b > c`) and adds path/cycle semantics.
- **Require: excluded.** `Run "command -v gum"` then `Wait` covers it. Add `Require` only if CI users ask.
- **Copy/Paste, Screenshot: excluded.** Paste is a bracketed-paste feature; screenshots are export-time (`export --at mark`).
- **Wait scopes (`+Line`, `+Screen`): excluded.** Screen only. Add scope later as an optional prefix.
- **`Wait idle DURATION` (quiet period): excluded for now.** Tempting, but it re-introduces timing flakiness. Leave room (see grammar: `Wait` takes a matcher; `idle` is not a valid matcher yet).

---

## 4. Concrete syntax

### 4.1 Rules

- One statement per line. A statement is `COMMAND[@DURATION] args…`.
- **Style choice: `Type@50ms "…"`** (VHS's form) over `--speed 50ms`. It is shorter, familiar, and `@` unambiguously means "timing for this command", which is also `Key@100ms Down 5` (repeat interval) and `Wait@30s /re/` (timeout). Flags would need a second lexical class.
- Comments: `#` at line start, or after a statement outside a string/regex. The first line `#!…` is a comment (see 4.5).
- Case: command, setting and key names are **case-insensitive** (docs show PascalCase). Strings, regexes and `Env` names are case-sensitive.
- Strings:
  - `"…"` with escapes `\n \r \t \e \\ \" \xHH \u{HEX}`.
  - `` `…` `` raw, no escapes (for shell snippets full of `"` and `\`). A backtick cannot appear inside a raw string; use `"…"` then.
  - In `Type`/`Run`, `\n` is typed as Enter (CR, 0x0D), not LF.
  - **No heredocs in v1.** Multi-line text is multiple `Run` lines. A heredoc can be added later as `Type <<EOF`; the grammar leaves `<<` unused.
- Regex: `/…/` with Go RE2 syntax, flags none; `\/` escapes a slash. Matching is **multiline** (`^`/`$` match at line boundaries), applied to the visible screen text with lines joined by `\n`.
- Durations: `<number><ms|s|m>` (`500ms`, `2s`, `1.5s`, `1m`). A bare number is an error.
- Key names: `Enter Tab Esc Space Backspace Delete Insert Up Down Left Right Home End PageUp PageDown F1…F12`, a single printable character (`a`, `/`), and chords `Ctrl+X`, `Alt+X`, `Shift+X`, `Ctrl+Alt+X` (modifier order irrelevant). `Key Up 3` repeats 3 times; `Key@200ms Down 5` waits 200 ms between repeats (default repeat interval = TypingSpeed).
- Errors: `file:line:col: message` plus the offending line and a caret. All parse errors are reported (up to 10), not only the first.
  Example: `demo.shotscript:7:1: Set must appear before the first action (Type at line 5)`.

### 4.2 EBNF

```ebnf
script      = { line } ;
line        = [ stmt ] [ comment ] NL ;
comment     = "#" { ANY - NL } ;
stmt        = setStmt | envStmt | outStmt | typeStmt | runStmt | keyStmt
            | sleepStmt | waitStmt | markStmt | resizeStmt ;

setStmt     = "Set" ident value ;                       (* preamble only *)
envStmt     = "Env" ident string ;                      (* preamble only *)
outStmt     = "Output" string ;                         (* preamble only *)
typeStmt    = "Type"  [ "@" duration ] string ;
runStmt     = "Run"   [ "@" duration ] string ;
keyStmt     = "Key"   [ "@" duration ] chord [ int ] ;
sleepStmt   = "Sleep" duration ;
waitStmt    = "Wait"  [ "@" duration ] [ regex | string ] ;   (* none = prompt-less: error in v1 *)
markStmt    = "Mark"  string ;
resizeStmt  = "Resize" size ;

value       = string | duration | percent | int | size ;
chord       = { modifier "+" } keyname ;
modifier    = "Ctrl" | "Alt" | "Shift" ;                (* case-insensitive *)
keyname     = namedkey | printable ;
size        = int "x" int ;
duration    = number ( "ms" | "s" | "m" ) ;
percent     = number "%" ;
string      = '"' { char | escape } '"' | "`" { ANY - "`" - NL } "`" ;
regex       = "/" { ANY - "/" - NL | "\/" } "/" ;
ident       = letter { letter | digit | "_" | "." } ;
```

Parse-level rules (not expressible in EBNF): the preamble is all leading `Set/Env/Output` statements; any of them after the first action is an error; `Wait` with no matcher is an error in v1 (VHS's implicit `>$` is exactly what we avoid); `Key` repeat count is 1–1000.

### 4.3 Examples

**1. Hello world**
```
Set Command "bash --noprofile --norc"
Env PS1 "$ "
Run "echo hello, world"
Sleep 1s
```

**2. Git workflow demo**
```
#!/usr/bin/env -S shellshot record --script
Set Size 100x28
Set Cwd "/tmp/demo-repo"
Set Command "bash --noprofile --norc"
Env PS1 "\w $ "
Output "git-demo.shot"

Mark "start"
Run "git init -q && git status -s"
Sleep 800ms
Run "echo 'hi' > README.md"
Run "git add README.md && git commit -qm 'initial commit'"
Run "git log --oneline"
Wait /^[0-9a-f]{7} initial commit$/
Mark "done"
Sleep 1500ms
```

**3. Interactive TUI (vim) with keys**
```
Set Size 90x24
Set Command "vim --clean demo.txt"
Wait /demo\.txt/                 # vim has drawn its status line
Key i
Type@80ms "Hello from a scripted vim session"
Key Esc
Type ":wq"
Mark "saving"
Key Enter
```
`--clean` is a vim flag I did not verify against a specific version; any editor works.

**4. Waiting for a build**
```
Set Timeout 120s
Env CI "1"
Run "make build 2>&1 | tail -n 5; echo BUILD-STATUS=${PIPESTATUS[0]}"
Wait@120s /^BUILD-STATUS=0$/     # anchored: the echoed command line starts with the prompt
Mark "built"
Run "./bin/app --version"
Wait /v\d+\.\d+/
```

**5. Env + size + resize + chords**
```
Set Size 120x30
Set TypingSpeed 35ms
Set Jitter 25%
Set Seed 42
Env TERM "xterm-256color"
Env NO_COLOR "1"
Set Command "htop"
Wait /CPU/
Key F6 
Key Down 3
Key Enter
Sleep 2s
Resize 80x24
Sleep 1s
Key q
```

**6. Shell chords**
```
Type "sleep 1000"
Key Ctrl+A                       # go to line start
Type "time "
Key Enter
Sleep 500ms
Key Ctrl+C
Wait /real/
Key Up 2
```

### 4.4 Extension

`.shotscript` is the recommended extension (the `.shot` extension is the recording; `.tape` is rejected to avoid being mistaken for a VHS-compatible file, which this is not). Alternatives the user may prefer: `.shots`.

### 4.5 Header and shebang

- A first line `#!/usr/bin/env -S shellshot record --script` makes the file executable (`chmod +x`); the OS appends the file path so the invocation is `shellshot record --script FILE`. `env -S` support (GNU coreutils ≥ 8.30, macOS) is **unverified here**. The lexer treats `#!` as an ordinary comment, so no special parsing is needed.
- An optional `# shellshot: 1` pragma on line 1 or 2 pins the language version; a higher number fails with "script requires newer shellshot". Cheap, and it lets the grammar change without silent misparses. If skipped now, the cost of adding it later is high, so I recommend including it.

---

## 5. Key encoding with libghostty (verified from headers/binding)

### 5.1 What is verified (direct evidence)

- Header `ghostty/vt/key/encoder.h` defines `ghostty_key_encoder_new/free/set/encode` and **`ghostty_key_encoder_set_from_terminal(encoder, terminal)`**, which "reads the terminal's current modes and flags and applies them … cursor key application mode, keypad mode, alt escape prefix, modifyOtherKeys state, and Kitty keyboard protocol flags".
- Options include `CURSOR_KEY_APPLICATION` (DEC mode 1, DECCKM), `KEYPAD_KEY_APPLICATION`, `ALT_ESC_PREFIX`, `MODIFY_OTHER_KEYS_STATE_2`, `KITTY_FLAGS`, `MACOS_OPTION_AS_ALT`, `BACKARROW_KEY_MODE` (Backspace 0x7f default vs 0x08).
- `macos_option_as_alt` is reset to `FALSE` by `set_from_terminal` (header says so); irrelevant to us because we never send Option-composed text.
- Go binding (GitHub `main`, **not the pinned version**) exposes: `NewKeyEncoder()`, `(*KeyEncoder).SetFromTerminal(*Terminal)`, `SetBool(opt, val)`, `SetKittyFlags`, `Encode(*KeyEvent) ([]byte, error)` (nil slice if the key produces no output), `Close()`. Not safe for concurrent use. `KeyEvent` has `SetAction/SetKey/SetMods/SetUTF8/SetUnshiftedCodepoint`. `Key` constants follow the W3C `code` list (`KeyArrowUp`, `KeyEnter`, `KeyA`, `KeyF1`…). `Mods`: `ModShift ModCtrl ModAlt ModSuper …`. `SetUTF8` doc: pass the *unmodified* character; do not pass C0 control characters.
- DECCKM is the reason this matters: with the mode on, `Up` should be `ESC O A`; off it is `ESC [ A` (standard xterm behaviour, **memory**). Full-screen apps such as vim, less and htop commonly turn it on, so a static table sends the wrong bytes to them.

### 5.2 What is NOT verified

- Actual output bytes (e.g. that `Up` yields `\x1b[A` off and `\x1bOA` on, that `Ctrl+C` yields `0x03`, that `Enter` yields `0x0D`). I could not run code. Run this to close the gap:

```go
// /tmp/keyprobe/main.go  (go.mod: require go.mitchellh.com/libghostty <pinned version>)
// PKG_CONFIG_PATH=$REPO/build/libghostty/lib/pkgconfig go run .
// For each of: Up, Ctrl+C, Enter, Backspace, F5, Alt+x:
//   enc, _ := ghostty.NewKeyEncoder(); ev := ... NewKeyEvent(); ev.SetAction(KeyActionPress); ev.SetKey(...); ev.SetMods(...)
//   print enc.Encode(ev)  before and after term.VTWrite([]byte("\x1b[?1h")) + enc.SetFromTerminal(term)
```
  I could not verify the exact constructor names (`NewKeyEvent`, `NewTerminal`, `VTWrite`) against the pinned version; check them before running.
- Whether the pinned module version has the same key API as GitHub `main` (it may be older: the pinned pseudo-version is 20261106).
- Whether `Encode` for `Ctrl+<letter>` needs `SetUTF8("c")` (the doc implies yes) or derives it from the key.

### 5.3 Recommendation (researcher inference)

Use libghostty's encoder for every `Key` statement; send `Type` text as raw UTF-8 bytes (no encoder; Kitty "report all keys" mode aside, typing is text). Reasons:
1. It tracks DECCKM, keypad, modifyOtherKeys and Kitty flags from the *live* headless terminal, so scripted `Up` is correct whichever mode vim, less or fzf have switched on. This is the one place libghostty clearly beats a table.
2. A hand-written table covers ~60 keys, but the modifier combinations (Ctrl+Shift+Arrow, Alt+letter with `ALT_ESC_PREFIX`) multiply.
3. Cost: cgo coupling. Mitigate with an interface at the consumer (§7) plus a tiny Go fallback only for tests.

---

## 6. Semantics and determinism

### 6.1 Time model

- The child process is real, so its output timing is real. Timestamps in the recording come from the real monotonic clock, as in live `record`.
- **v1: real time.** `Type` and `Sleep` really wait. A 60-second script takes 60 seconds.
- **Accelerated mode (v2, not in v1):** an interface `Clock{Now() time.Duration; Sleep(ctx, d)}` allows a `--fast` clock whose `Sleep` returns immediately but advances an offset `skipped`; recorded timestamps are `real + skipped`. Consequences, stated so the user can decide:
  - The *input* timeline matches what a slow run would have produced; the child just sees keystrokes sooner.
  - Programs with wall-clock animation (spinners, `sleep`, progress bars) produce fewer frames in skipped periods, so a `--fast` file differs from a real one. Therefore it is a different mode, not a transparent speedup.
  - `Wait` always takes real time (it waits for the child).
  - Researcher inference: a good compromise is to cap, not skip: `Sleep` and typing delays are scaled by `--time-scale 0.25`. Same caveats. Do not build this until someone needs it.
- **Tests use a fake Clock** (§8), which is what makes the executor deterministic without a real mode.

### 6.2 Wait semantics

- The executor is polled on the **session's single goroutine** (§7.2). After each output chunk is written to the terminal, and on a timer tick, the driver is given a `Screen` view and calls `Text()`, which is the terminal's current visible text (the existing `Snapshot.Lines`, joined by `\n`, right-trimmed). **No extra goroutine touches the terminal**, which matches the session rule (vt is not concurrency-safe).
- **Why this is more robust than VHS (interpretation):** VHS matches against the xterm.js buffer in a browser, reached through a poll over a remote-control channel, with last-line default (README, fetched). We match against libghostty's *rendered grid*, in-process: cursor movement, overwrite, clear-line, alt-screen and wrapping have all been resolved, so `Wait /Compiling/` sees what a person would see, not the byte stream. Expect/pexpect-style matching of raw bytes breaks on cursor-addressed output (a progress bar redrawn in place leaves partial fragments in the byte stream). Matching state, not bytes, also removes polling latency: the check happens right after each output chunk is applied.
- Matching is against the **visible screen only** (alt screen included when active). Scrollback is not searched in v1: a `Wait` for something that has scrolled off will time out. Document this. (Is scrollback accessible via the binding? Not verified.)
- **Timeout:** `Wait` fails after `Wait@T` or `Set Timeout` (default 15s). On timeout:
  1. The error is `file:line:col: Wait timed out after 15s waiting for /re/`, plus a dump of the final screen to stderr (the single most useful CI artefact).
  2. The child is closed (SIGHUP, then kill after grace).
  3. The recording is **finalised and kept** (a clean close, so partial recordings stay valid and can be inspected with `replay`).
  4. Exit code 3 (§6.4).
- If the child has already exited, `Wait` still tests the final screen once; if there is no match it fails immediately with "child exited (status N) before match", rather than burning the timeout.
- A `Wait` that already matches at the moment it is reached returns immediately. Because of the echo trap (§2.2 #5), the docs must say: anchor on a line start, or print the sentinel via a split literal (`echo BUILD-"DONE"`).

### 6.3 Jitter and typing speed

- `TypingSpeed` is a fixed per-character delay. `Type` text is sent character by character (grapheme/rune boundaries; one `Write` per rune so a multi-byte rune never splits).
- `Set Jitter 25%` draws each delay uniformly from `[d·(1−j), d·(1+j)]` using a `math/rand/v2` PCG seeded from `Set Seed`. **Same seed + same script gives the same *scheduled* delays.** The recorded timestamps still differ run to run, because write time ≠ scheduled time (scheduler latency) and output is real.
- Default is no jitter: fully fixed delays. Researcher recommendation: leave it off in CI.

### 6.4 Exit rules and codes

- **End of script:** (1) a short settle so the final output lands (Researcher suggestion: wait until the PTY has been quiet 200 ms, max 2 s, configurable later); (2) if the child is still running: shell → write EOF (Ctrl-D, 0x04), TUI/command → close the PTY master (SIGHUP); (3) wait a grace (default 2 s, `--grace`), then kill. Final frame/footer written on clean close, as `session` does today.
- **Child exits mid-script:** subsequent `Type/Key/Run` error with "child exited"; `Sleep/Mark/Wait` are allowed (see §6.2). Many demos end with `exit` or `q`, so the last statement exiting is *normal*.
- **Exit codes** (decision for the user, §9):
  - `0` script completed (regardless of child exit status). Matches today's `record`, which reports a non-zero child status on stderr only.
  - `1` runtime error (I/O, PTY, recording write).
  - `2` parse/validation error (nothing was spawned, no file written).
  - `3` script failure: `Wait` timeout, child exited when input was required.
  - `130` cancelled by signal.
  - Optional `--propagate-exit` makes the child's final status the exit code, for CI that asserts a command succeeded.

### 6.5 What is recorded

- OUT events: the raw PTY output, including the echo of scripted typing. Same as live.
- IN events: off by default (the format decision). Scripted keystrokes go through the **same** write path as live ones, so with `--record-input` they are recorded as IN events without extra code. Researcher recommendation: no special "scripted" flag; the header's `flags` could note `source=script` later.
- MARK events: one per `Mark`; also a mark `script:start` / `script:end` is a candidate (open question).
- RESIZE events: per `Resize`; initial size from `Set Size`.
- Scripted mode **does not** forward real stdin to the child (no raw mode) and, by default, does not mirror output to stdout. Open question: `--watch`.

### 6.6 Cancellation

`cmd.Context()` cancellation (SIGINT/SIGTERM handled in `cli`) flows into the session, which already "closes proc and returns an error wrapping ctx.Err()". Required extra: finalise the recording (partial file valid) and exit 130. The executor must never block without a `ctx`-aware wait.

---

## 7. Architecture fit

### 7.1 Packages

```
internal/script            // ast + lexer + parser, pure Go, no deps.   (one package at first; split if it grows)
   ast.go  lex.go  parse.go  validate.go (preamble rules, ranges)
internal/script/keys       // chord text <-> (Key, Mods) enum; pure Go; NO ghostty types
internal/script/run        // executor state machine; depends only on the interfaces it declares
internal/vt/ghostty        // + KeyEncoder adapter (cgo stays here): keys.Key -> ghostty.Key table
internal/session           // + optional Driver hook (see 7.2)
internal/cli               // record.go: --script flag, merge settings, wire concrete types
```
Dependency rules preserved: `script`, `script/keys`, `script/run` import no cgo, no pty, no cobra; `run` declares its own interfaces (consumer-side):

```go
// in internal/script/run
type Screen interface{ Text() string }                      // visible text, \n-joined
type Keys interface{ Encode(keys.Chord) ([]byte, error) }   // adapter over libghostty KeyEncoder
type Clock interface {                                      // injectable
    Now() time.Duration
}
type Sink interface {                                       // what the executor asks the host to do
    // implemented by the session adapter
}
```

### 7.2 Hook into the session (the important design point)

Make the executor a **non-blocking state machine** the session loop polls, not a goroutine that blocks:

```go
// in internal/session (consumer defines it)
type Driver interface {
    // Poll is called on the session goroutine after each output chunk is
    // applied and when the deadline returned by the previous Poll passes.
    Poll(now time.Duration, scr Screen) (Step, error)
}
type Step struct {
    Write    []byte        // inject into the PTY (via the same path as live input)
    Mark     string        // emit MARK
    Resize   *Size         // resize pty + terminal + RESIZE event
    Deadline time.Duration // wake me no later than this (0 = only on output)
    Done     bool
}
```
- No new goroutine ever touches the terminal; `Wait` is just "keep polling and `regexp.MatchString` on `scr.Text()`".
- A fake clock and fake screen make the executor a pure function: trivial table-driven tests, no PTY.
- The session select loop already has a ticker and output channel; the driver adds one timer case. (Exact structure of `session.go` lines 81–236 not read; adjust.)
- Key encoding needs the live terminal, so `Keys` is implemented by the ghostty adapter that holds the `*Terminal` and calls `SetFromTerminal` before each `Encode`.

### 7.3 CLI wiring

- **Extend `record`, not a new `run` command.** The CLI is being reduced to `record/replay/export/version`; scripted recording is `record` with a different input source. `shellshot record --script demo.shotscript [-o out.shot] [--cols/--rows] [--record-input]`.
- `--script` is mutually exclusive with `-c` and `-- cmd`... *or* the CLI command overrides `Set Command` (decision, §9). Recommendation: CLI overrides script, mirroring the precedence rule.
- Note an existing naming wart in `record.go`: the variable `script` holds the `-c/--command` string. Rename it when adding the flag to avoid confusion.
- **Precedence: CLI flag > script `Set`/`Output` > environment defaults.** `--cols/--rows` override `Set Size`; `-o` overrides `Output`. Only flags the user *explicitly set* override (use `cmd.Flags().Changed`), because `--cols` defaults are computed.
- Parse and validate the whole script before spawning anything (exit 2 on error).

---

## 8. Task plan (small, atomic, each independently testable)

| # | Task | Output | Tests |
|---|---|---|---|
| T1 | AST + lexer | `internal/script` tokens with line:col | Table tests on token streams; error positions |
| T2 | Parser + validator | AST, preamble rules, ranges, multi-error | Golden `testdata/*.shotscript` → `*.ast.golden`; `go test -fuzz` for parse (never panics, stable error text) |
| T3 | Durations/strings/regex literals | Escapes, units | Table tests incl. invalid forms |
| T4 | `keys` package | Chord grammar → `(Key, Mods)`; names, aliases | Table: every documented name parses; invalid reject |
| T5 | ghostty KeyEncoder adapter | `keys.Chord` → bytes via libghostty, `SetFromTerminal` | cgo test: arrows with/without `ESC[?1h`; Ctrl+C; F-keys. This test **is** the §5.2 verification |
| T6 | Executor state machine (Type/Key/Run/Sleep/Mark/Resize, jitter) | `run.New(ast, Keys, Clock, rng)` + `Poll` | Fake clock → expected `[]Step` timeline, golden |
| T7 | Wait semantics | regex/literal, timeout, exited child | Fake screen sequences; timeout error text; anchored vs echo-trap |
| T8 | Session `Driver` hook | Poll after chunks and on deadline; inject writes; end-of-script/grace | session tests with the existing fake Process/Terminal |
| T9 | CLI wiring | `record --script`, precedence merge, exit codes | cli tests with a fake runner; one real end-to-end `echo hello` script, gated on libghostty |
| T10 | Docs | `docs/SCRIPT.md`, ARCHITECTURE update, example scripts under `examples/` run in CI | doc-tested: CI executes `examples/*.shotscript` and checks the final-screen golden |

T1–T4 can start now in parallel with recording-format work (pure Go, zero overlap). T5 needs the build tree. T6/T7 depend on T1–T4 only. T8 is the only change to a shared package (`session`), so schedule it after the binary-format session changes land.

### Test strategy
- **Golden scripts:** `testdata/<name>.shotscript` + `<name>.timeline.golden` (one line per Step: `t=+50ms write "h"`, `t=+1.2s mark done`). Update with `-update`.
- **Fake clock/pty/screen** in `run` tests: `Poll(now, screen)` is called by a harness table: `{at, screenText} → want Step`.
- **End-to-end (opt-in):** `record --script examples/hello.shotscript` against `/bin/sh`, then replay and assert the final screen contains the expected text. Never assert timestamps; assert screen state at marks.
- **Fuzz:** parser and lexer (inputs must never panic; any accepted script re-prints and re-parses to the same AST).

---

## 9. Open questions, risks, decisions for the user

**Decisions (my recommendation first)**
1. **Name/extension:** `.shotscript`. Alternatives: `.shots`.
2. **Real-time only in v1?** Yes; accelerated mode later behind `Clock`.
3. **Exit code on child failure:** `0` by default with `--propagate-exit` opt-in, or the opposite? Affects CI ergonomics.
4. **CLI `-c`/`--` with `--script`:** override `Set Command`, or forbid the combination?
5. **Mirror output to stdout while scripting?** Recommendation: off by default, `--watch` to show.
6. **Should `Output` be allowed to name non-`.shot` files (e.g. `demo.apng`) so the script triggers export?** Recommendation: no in v1; keep `record` and `export` separate. A future `Output "x.apng"` can chain.
7. **Include the `# shellshot: 1` version pragma now?** Recommendation: yes.
8. **`script:start`/`script:end` automatic marks?** Cheap and useful for export ranges; but they add MARK events the user didn't write.

**Risks**
- **Echo trap on `Wait`** is the most likely source of user complaints; mitigated only by docs and anchoring.
- **cgo key encoder API** may differ at the pinned commit (unverified), and the `keys.Key` → `ghostty.Key` mapping table is manual.
- **Prompt reproducibility** (user's shell rc files) is not controllable by the tool; docs recommend `bash --noprofile --norc` with explicit `PS1`.
- **Scrollback not matchable** may surprise authors of long-output scripts.
- **Session hook** (T8) touches the shared session loop that the binary-format work is also changing; a merge conflict risk.
- **Non-reproducible bytes**: the same script produces slightly different files each run. Promise reproducible *screens at marks* only.
- **`env -S` shebang** portability unverified.

**Unverified claims (summary):** actual encoder output bytes; pinned-version API match; DECCKM sequences (from memory); `Mark`/`RESIZE` event availability in the final binary format (taken from the task statement); asciinema-automation directive names (memory); VHS vhs-action and browser-stack pain (memory); vim `--clean`; `env -S` availability; scrollback access via the binding.

## Final recommendation

Ship `record --script` with the 10-command language above, in the order T1→T4 (pure Go, now), T6/T7 (executor against fakes), T5 (key encoder verification, which doubles as the missing experiment), then T8–T10. Keep VHS's surface where it helps, adopt libghostty's real screen state for `Wait` and its mode-aware encoder for `Key`, stay real-time, and reserve (not build) `Hide/Show`, `Require`, `Source`, `Wait idle`, theme hints and accelerated time.

## Sources
- Kept: VHS README (https://github.com/charmbracelet/vhs/blob/main/README.md): fetched; commands, defaults (Wait `/>$/`, 15 s, Line scope), "late Set ignored".
- Kept: go-libghostty `key_encoder.go` / `key_event.go` on GitHub main (https://github.com/mitchellh/go-libghostty): fetched; API signatures; not the pinned version.
- Kept: repo `build/libghostty/include/ghostty/vt/key/encoder.h`, `event.h` (partial): read directly; option list, `set_from_terminal`.
- Kept: `docs/ARCHITECTURE.md`, `docs/research/binary-format.md`, `internal/cli/record.go`, `internal/session/session.go` (lines 1–80).
- Kept (search-summary level only): demo-magic template (https://github.com/paxtonhare/demo-magic/blob/master/samples/demo-template.sh), doitlive docs (https://doitlive.readthedocs.io/en/stable/), Terminalizer config (https://github.com/faressoft/terminalizer/blob/master/config.yml), asciinwriter (https://pypi.org/project/asciinwriter/).
- Rejected: any asciinema-automation page. I did not find or fetch its README, so its directive syntax is memory.

```acceptance-report
{
  "criteriaSatisfied": [
    {"id": "criterion-1", "status": "satisfied", "evidence": "Brief written to docs/research/script-format.md with summary, prior art, command set, EBNF, examples, semantics, architecture, task plan, open questions; limits and unverified claims flagged."}
  ],
  "changedFiles": ["docs/research/script-format.md"],
  "testsAddedOrUpdated": [],
  "commandsRun": [
    {"command": "go run key encoder probe", "result": "not-run", "summary": "No shell tool available; encoder output bytes unverified, probe sketch provided in section 5.2"}
  ],
  "validationOutput": ["Header API (encoder.h) and Go binding (GitHub main) read directly; module cache not found so pinned version unverified"],
  "residualRisks": [
    "Key encoder output bytes and DECCKM behaviour not executed",
    "Binding read from GitHub main, not pinned version",
    "internal/pty, live, cli/capture, export, render not read",
    "asciinema-automation, vhs-action and VHS pain points partly from memory"
  ],
  "noStagedFiles": true,
  "diffSummary": "Added docs/research/script-format.md only",
  "reviewFindings": ["no blockers"],
  "manualNotes": "Write tool reported no error, but I could not run a wc or read-back to confirm the line count (target <=600)."
}
```

# Research: automation script format for shellshot (`record --script`), v2

Status: design brief, **v2**. It supersedes the v1 brief (a VHS-like flat command list), which the user rejected: it felt limiting and had no grouping. Only this file was changed; no code exists yet.

**Evidence limits (read first).**
- v2 is a design rewrite. **No code was run.** The grammar and examples were checked by re-reading them against the EBNF in §4.3, not by a parser.
- Everything about libghostty key encoding (§6) is inherited from the v1 research run, which had no shell. Encoder output bytes, the pinned-version API and DECCKM sequences are **unverified**. The binding was read from GitHub `main`, not the pinned version. The C headers in `build/libghostty/include/ghostty/vt/key/` were read directly.
- The MARK event (kind 3, UTF-8 label) comes from `docs/research/binary-format.md`, a design, not shipped code. Of the code I know only `record.go` and `session.go` (lines 1–80) as quoted; `live`, `pty`, `export`, `render` were not read.
- Prior-art claims are marked **fetched** (read in the v1 run) or **memory** (unverified).

---

## 1. Summary and recommendation

**Build a small, line-oriented, declarative script language in `.script` files, run by `shellshot record --script FILE`. A script is top-level *settings*, then *steps* and *blocks*. It has six commands and two block kinds. It has no conditions, loops, variables, functions or expressions.**

- **Structure:** `scene NAME { … }` is a named group of steps. `hidden { … }` is a group that runs and is recorded, but exporters may skip it. A `hidden` may sit inside a `scene`. Any other nesting is an error.
- **Marks:** blocks become MARK events in the recording (`scene:NAME:start`/`scene:NAME:end`, `hidden:start`/`hidden:end`). Exporters can chapter or trim from them without re-running anything.
- **Scoped settings:** `typing`, `jitter` and `timeout` may appear inside a block. They apply until the closing `}` and then revert.
- **Commands (6):** `run`, `type`, `press`, `wait`, `sleep`, `mark`.
- **Settings (9):** `size`, `shell`, `env`, `cwd`, `typing`, `jitter`, `timeout`, `seed`, `output`.
- **Text rule:** `run`/`type` take the **rest of the line** as literal text, so `run npm install` just works. A leading `"` switches to an escaped string.
- **Static:** the whole file is parsed and validated before anything is spawned (exit 2). Scene names can be listed without running.
- **Kept from v1:** the executor is a non-blocking state machine polled by the session's single goroutine. `wait` matches libghostty's rendered screen, not raw bytes. Key chords go through libghostty's mode-aware `KeyEncoder`. Real time only in v1. Recordings are reproducible in inputs and screen at marks, **not byte-identical**. Script failures have their own exit codes.
- **Deferred or excluded:** §3.4.

---

## 2. Prior art

### 2.1 Comparison

| Tool | Model | Source type | Take | Leave |
|---|---|---|---|---|
| **VHS** `.tape` ([README](https://github.com/charmbracelet/vhs/blob/main/README.md)) | Flat line DSL; drives a shell via ttyd, headless Chromium (xterm.js) and ffmpeg | **fetched** | Verb-per-line; `Wait`/`Sleep`/`Env`/`Output`; Ctrl/Alt chords | Browser stack; ~25 visual `Set`s; `Source`; `Copy/Paste`; `Screenshot`; `Hide/Show`; flat structure; the pitfalls in 2.2 |
| **asciinema-automation** | Directive comments (`#$ expect`, `#$ delay`, `#$ send`) | **memory** (one hit for a similar tool, `asciinwriter`) | A wait/expect on output is the core primitive | pexpect byte-stream matching |
| **demo-magic** | Bash script: `pe "cmd"` types and runs, `p` prints only, `TYPE_SPEED` | **fetched via search summary** | `run` ≈ `pe` | Needs a human at the keyboard; script is code |
| **doitlive** | Session file of commands, `#doitlive speed: 3` | **fetched via search summary** | In-file `key: value` directives as a cheap settings syntax | Interactive; not CI-friendly |
| **Terminalizer** | YAML recording with timed `records`; `frameDelay`, `maxIdleTime` | **fetched via search summary** | Idle capping belongs at export | YAML as an authoring format |
| **expect/pexpect** | Programmatic send/expect | **memory** | Fail loudly on timeout | Matching a raw byte stream |

### 2.2 What caused pain (and what we do about it)

1. **Ignored late settings (fetched).** VHS README: "Any setting except `TypingSpeed` applied after a non-setting or non-output command is ignored." No error is given. → Every setting we accept is always effective. A setting that cannot take effect where it appears is a parse-time error with line:col (§3.2).
2. **Ambiguous duration units (fetched).** `Sleep 2` is seconds, `TypingSpeed 0.1` is 0.1 s. → Durations always carry a unit.
3. **Weak `Wait` defaults (fetched).** Default regex `/>$/`, scope `Line`, 15 s. → We match the whole visible screen; the pattern is mandatory.
4. **Sleep-driven flakiness (inference).** Authors pad `Sleep` where `Wait` is unreliable. → Make `wait` robust and the documented primitive.
5. **Echo matches itself (classic expect trap).** `run make && echo DONE` puts `DONE` on screen as typed echo before the build ends. → Matching is multiline; docs say to anchor (`/^DONE$/`; the echo line starts with the prompt).

### 2.3 Why v2 differs from VHS

- **Grouping.** VHS is a flat list. Here, `scene` gives named, exportable chapters, and one file can describe a multi-part demo.
- **Scoping.** Behaviour changes (`typing 80ms`) apply to a block and revert at `}`. VHS `Set` is global and, when late, ignored.
- **`hidden`.** VHS `Hide/Show` means "stop capturing frames", which would corrupt a recording that must stay a faithful log. `hidden` keeps recording and marks the span instead.
- **No mid-script `Set`.** Process-level settings (`size`, `shell`, `env`, `cwd`, `seed`, `output`) are legal only before the first step, because the child is spawned after them. Run-time settings are legal anywhere and scoped.
- **Rest-of-line text.** `run git commit -m "msg"` needs no quoting or escaping. VHS needs `Type "git commit -m 'msg'"`.

---

## 3. The language

### 3.1 Commands (6)

| Command | Meaning |
|---|---|
| `run TEXT` | Type TEXT, then press Enter. The common case. |
| `type TEXT` | Type TEXT without Enter. |
| `press KEY…` | One or more key chords, e.g. `press ctrl+c`, `press up up enter`. Each key may be followed by a repeat count `xN`: `press down x3`. |
| `wait PATTERN [TIMEOUT]` | Block until the visible screen matches PATTERN. PATTERN is a quoted string (literal substring) or `/regex/`. TIMEOUT overrides `timeout` for this wait. On timeout the script fails (§7.3). |
| `sleep DURATION` | Pause. |
| `mark LABEL` | Emit a MARK event. |

`run` does not wait for the command to finish. Use `wait` for that.

### 3.2 Settings (9)

| Setting | Argument | Default | Where legal |
|---|---|---|---|
| `size` | `COLSxROWS`, each 1–1000 | current terminal size, else 80x24 (as `record` does now) | before first step |
| `shell` | PATH of the program to run | `$SHELL`, else `/bin/sh` | before first step |
| `env` | `KEY=VALUE`, repeatable; later wins | inherited | before first step |
| `cwd` | PATH | cwd of `shellshot` | before first step |
| `seed` | N (unsigned integer), seed for the jitter PRNG | `1` | before first step |
| `output` | PATH of the recording file | `recording.shot` (README). `-o` wins | before first step |
| `typing` | DURATION, delay between typed keys | `50ms` | anywhere; scoped in blocks |
| `jitter` | PERCENT (`0%`–`100%`) | `0%` | anywhere; scoped in blocks |
| `timeout` | DURATION (>0), default for `wait` | `15s` | anywhere; scoped in blocks |

Scoping rules:
- A setting at top level applies to everything after it. A setting inside a block applies from that line to the closing `}`, then the previous value is restored (a block that sets `typing` twice still reverts to the value from before the block).
- **Decision (needs user confirmation, §11):** only `typing`, `jitter` and `timeout` can appear inside a block or after the first step. The other six configure the child before it is spawned. Accepting them later would recreate VHS's silently ignored `Set`. They are therefore errors there, with the message "`size` must appear before the first step (line N)".
- Precedence: CLI flags (when explicitly set) > script > defaults.

### 3.3 Blocks

```
scene NAME {        # NAME: letters, digits, _ and -; unique within the script
  …steps and scoped settings…
}
hidden {
  …
}
```
- Blocks nest **at most one level**: a `hidden` inside a `scene` is allowed. `scene` in `scene`, `hidden` in `hidden` and `scene` in `hidden` are errors (decision: the last two are pointless or confusing; flagged in §11).
- A block must contain at least one step.

### 3.4 Deliberately excluded

| Excluded | Why |
|---|---|
| Conditions, loops | The user rejected a programming language. They make scripts non-static and non-deterministic, and turn "script fails" into "script has a bug". |
| Variables, expressions, functions | Same. `env` plus the shell cover real needs (`run export X=1`); copying lines is clear and greppable. |
| `include`/`source` | Adds path and cycle semantics. `cat a.script b.script > c.script` works. |
| `require` | `run command -v gum` plus `wait` covers it. Add only if CI users ask. |
| `copy`/`paste` | Bracketed paste is a separate feature; use `type`. |
| `screenshot` | Export-time (`export --at MARK`), not a script concern. |
| Mid-script `resize` | Dropped from the v1 draft to keep six commands. The format still has RESIZE events; `size` sets the initial size. Could return as a step. |
| Theme/font settings | Export-time. The header hint slot is their future home. Reserved. |
| Heredoc, `wait idle` | Heredoc deferred (backtick reserved; use several `run` lines). `wait idle` reintroduces timing flakiness. |

**How to repeat something:** copy the lines, or use `press down x3`. For a long repeated group, copy the block. There is no `repeat`.

**Extending later without breaking the grammar.** New statements are new keywords at the start of a line, new settings are new names, and new block kinds fit the existing header shape `KIND [ARGS] {`. To keep this open, these words are **reserved**: using one at the start of a line gives "reserved for future use" rather than "unknown command":

`if else elif for while repeat loop break let var const set fn func def define call return include import source require use copy paste screenshot resize hide show theme font idle`

---

## 4. Concrete syntax

### 4.1 Rules

- **Line-oriented.** One statement per line. Indentation is insignificant (two spaces by convention). Blank lines are ignored.
- **Comments.** `#` starts a comment at the start of a token: a whole line, or after the arguments of a statement. The first line `#!…` is an ordinary comment. Exception: unquoted `run`/`type` text runs to the end of the line, so `#` there is literal text (see below).
- **Blocks.** The header line ends with `{` (after a space). `}` is alone on its line (a comment may follow).
- **Case.** Everything is case-sensitive; keywords, setting names and key names are lower-case.
- **Strings.** `"…"` with escapes `\n \t \" \\ \e \xHH`. Any other backslash sequence is an error. `\xHH` produces the byte HH; the result must be valid UTF-8. In typed text, `\n` is typed as Enter (CR, 0x0D), not LF.
- **Regex.** `/…/`, Go RE2 syntax, no flags. `\/` is a literal slash; every other `\` is passed to RE2 unchanged. Applied in multiline mode: `^`/`$` match at screen row boundaries.
- **Durations.** `<number><unit>`, unit `ms`, `s` or `m` (`500ms`, `1.5s`, `2m`); a bare number is an error. Percent is `25%`, size is `100x28`.
- **Keys.** Named keys: `enter tab esc backspace delete insert space up down left right home end pageup pagedown f1`…`f12`. Plus a single character `a`–`z` or `0`–`9`. Chords are `ctrl+KEY`, `alt+KEY`, `shift+KEY` and combinations (`ctrl+shift+left`, any order, no repeats). The KEY in a chord is a named key or `a`–`z`/`0`–`9`. Other characters are typed with `type`. A repeat is a separate token `xN` (N: 1–999) after a key. It cannot be mistaken for a key, since keys other than single characters never match `x` + digits.
- **Unknown identifiers** (commands, settings, keys, block kinds, units) are errors with `file:line:col`, the offending line with a caret, and a **did-you-mean** suggestion (edit distance ≤ 2 against the names legal at that position). All errors are reported, up to 10.

  ```
  demo.script:12:3: unknown command "rn" (did you mean "run"?)
  demo.script:4:1: "size" must appear before the first step (first step: line 3)
  demo.script:9:5: "scene" cannot be nested inside scene "intro"
  ```

#### The text rule for `run` and `type` (decision)

**`run`/`type` take the rest of the line, literally, unless the text starts with `"`.** Then it is a quoted string with escapes, and only a comment may follow the closing quote.

- `run npm install` types `npm install`. `run git commit -m "fix: x"` types the line as written. The quotes inside are literal because the text does not *start* with one.
- `run "echo \"hi\"\n"` uses the escaped form. It is the only way to get `\e`, `\xHH`, an embedded Enter, leading or trailing spaces, or a trailing comment.
- Unquoted text is trimmed of leading and trailing whitespace. `#` and `\` are literal in it.
- Why: nearly every `run` line is a shell command, and shell commands are full of quotes, `$`, `\` and `#`. Making the common case need no escaping beats a uniform quoting rule. The one surprise (`run "hi" | wc` starts with a quote) is not silent: the parser reports "text after closing quote; to type a leading quote, use `run "\"hi\" | wc"`".
- `run` or `type` with no text is an error (`press enter` presses Enter alone). Multi-line text is deferred (§3.4).

### 4.2 Files and header

- Extension: **`.script`** (the recording is `.shot`).
- A first line `#!/usr/bin/env -S shellshot record --script` makes a script executable. The OS appends the path, giving `shellshot record --script FILE`. `env -S` availability (GNU coreutils ≥ 8.30, macOS) is **unverified**. The lexer treats `#!` as a comment.
- No `# shellshot: 1` version pragma: the reserved-words list is the compatibility mechanism.

### 4.3 EBNF

`SP` is a space or tab; `NL` is a newline or end of file; `ANY` is any character. Every line may start with `{ SP }` (indentation); it is omitted below except where it matters.

```ebnf
script         = { blank | setting } , { blank | scoped-setting | step | scene | hidden } ;
                 (* the first group is the preamble: process settings may appear only here *)

blank          = tail , NL ;
tail           = { SP } , [ comment ] ;
comment        = "#" , { ANY - NL } ;

scene          = "scene" , SP , { SP } , name , SP , { SP } , "{" , tail , NL ,
                 { blank | scoped-setting | step | hidden } , close ;
hidden         = "hidden" , SP , { SP } , "{" , tail , NL ,
                 { blank | scoped-setting | step } , close ;
close          = { SP } , "}" , tail , NL ;

setting        = process-setting | scoped-setting ;
process-setting= ( size | shell | env | cwd | seed | output ) , tail , NL ;
scoped-setting = ( typing | jitter | timeout ) , tail , NL ;
size           = "size" , SP , { SP } , int , "x" , int ;
shell          = "shell" , SP , { SP } , arg ;
env            = "env" , SP , { SP } , key , "=" , [ arg ] ;
cwd            = "cwd" , SP , { SP } , arg ;
seed           = "seed" , SP , { SP } , int ;
output         = "output" , SP , { SP } , arg ;
typing         = "typing" , SP , { SP } , duration ;
jitter         = "jitter" , SP , { SP } , number , "%" ;
timeout        = "timeout" , SP , { SP } , duration ;

step           = run | type | ( ( press | wait | sleep | mark ) , tail , NL ) ;
run            = { SP } , "run" , SP , { SP } , text ;
type           = { SP } , "type" , SP , { SP } , text ;
text           = ( string , tail , NL ) | ( raw-text , NL ) ;
raw-text       = ( ANY - SP - NL - '"' ) , { ANY - NL } ;   (* trailing SP trimmed; # and \ are literal *)
press          = "press" , SP , { SP } , keypress , { SP , { SP } , keypress } ;
keypress       = chord , [ SP , { SP } , repeat ] ;
wait           = "wait" , SP , { SP } , pattern , [ SP , { SP } , duration ] ;
sleep          = "sleep" , SP , { SP } , duration ;
mark           = "mark" , SP , { SP } , arg ;

pattern        = string | regex ;
chord          = { modifier , "+" } , keyname ;
modifier       = "ctrl" | "alt" | "shift" ;
keyname        = "enter" | "tab" | "esc" | "space" | "backspace" | "delete" | "insert"
               | "up" | "down" | "left" | "right" | "home" | "end" | "pageup" | "pagedown"
               | "f1" | "f2" | "f3" | "f4" | "f5" | "f6" | "f7" | "f8" | "f9" | "f10" | "f11" | "f12"
               | lower | digit ;
repeat         = "x" , digit , { digit } ;

duration       = number , ( "ms" | "s" | "m" ) ;
number         = digit , { digit } , [ "." , digit , { digit } ] ;
int            = digit , { digit } ;
name           = ( letter | digit ) , { letter | digit | "_" | "-" } ;
key            = ( letter | "_" ) , { letter | digit | "_" } ;
arg            = string | word ;
word           = ( ANY - SP - NL - '"' - "#" ) , { ANY - SP - NL } ;
string         = '"' , { ( ANY - '"' - "\" - NL ) | escape } , '"' ;
escape         = "\" , ( "n" | "t" | '"' | "\" | "e" | ( "x" , hex , hex ) ) ;
regex          = "/" , regexchar , { regexchar } , "/" ;
regexchar      = ( ANY - "/" - "\" - NL ) | ( "\" , ANY - NL ) ;
```

Notes:
- A `press` line may end in a comment (the `tail` of `step`). `run`/`type` carry their own line end, because unquoted text cannot take a comment.
- **Validator-only rules** (the grammar above is a superset in these respects; the parser accepts them so the validator can give a precise message, §8): setting and command names are checked against the known sets; spawn settings before the first step and not in blocks; no duplicate spawn settings except `env`; no duplicate scene names; non-empty blocks; `timeout` > 0; size ≤ 1000; repeat 1–999; modifiers not repeated; `mark` labels may not start with `scene:` or `hidden:`; `\xHH` results are valid UTF-8.

### 4.4 Tokenizer note

Hand-written scanner, one line at a time. The first word on a line is the keyword (or `}`). The keyword selects how the rest of the line is scanned:
- `run`, `type`: one **TEXT** token (rest of line, trimmed) or a **STRING** followed by an optional COMMENT.
- `wait`: first argument is **STRING** or **REGEX** (decided by the first character, `"` or `/`). This is why `cwd /tmp` is not a regex: other keywords never scan a REGEX.
- Everything else: **WORD**, **STRING**, `{`, `}`. A `#` starts a COMMENT only at the start of a token, so `cwd /tmp/a#b` is one WORD.
- Each token has line:col. NL ends the statement. Typed literals (duration, percent, size, key, repeat) are WORDs classified in the parser, so "durations need a unit" and did-you-mean errors live in one place.

### 4.5 AST shape

```go
type Pos struct{ Line, Col int }

type Script struct {
    Settings []Setting // in source order, all top-level settings
    Items    []Item    // steps and blocks in source order
}
type Item interface{ ItemPos() Pos } // implemented only by Step and Block

type Setting struct {
    Pos   Pos
    Name  string // "size", "typing", …; unknown names kept for the validator
    Value string // raw argument text; typed by the validator
    At    int    // index into the enclosing Items: effective before Items[At]
}
type Step struct {
    Pos      Pos
    Kind     string      // run | type | press | wait | sleep | mark
    Text     string      // run/type, escapes already resolved
    Keys     []KeyPress  // press: {Chord string, Count int}
    Pattern  *Pattern    // wait: {Regex bool, Source string}
    Duration time.Duration // sleep duration, or wait timeout (0 = use setting)
    Label    string      // mark
}
type Block struct {
    Pos      Pos
    Kind     string    // scene | hidden
    Name     string    // scene only
    Settings []Setting // At indexes Items
    Items    []Item    // Steps, plus a hidden Block inside a scene
}
```
`Block.Items` rather than `Steps`: a `hidden` may nest in a `scene`. `At` keeps source order between settings and items without making settings items. A canonical printer (AST → text) serves golden tests and the round-trip fuzz property.

---

## 5. Examples

Each was re-read against §4.3. 
`/bin/sh` plus `env PS1=…` is used for a reproducible prompt. That sh honours an imported `PS1` is **unverified** (memory). The grammar has no way to pass `--norc` to a shell (§11).

**1. Hello world**
```
#!/usr/bin/env -S shellshot record --script
# hello.script: the smallest useful demo
shell /bin/sh
env PS1="$ "
size 60x10

run echo hello, world
sleep 1s
```

**2. Git workflow demo with scenes** (assumes `/tmp/demo-repo` exists)
```
size 100x28
shell /bin/sh
cwd /tmp/demo-repo
env PS1="\\w $ "
output git-demo.shot
typing 40ms

hidden {
  run rm -rf .git README.md
  run clear
}

scene init {
  run git init -q
  run git status -s
  sleep 800ms
}

scene commit {
  typing 70ms          # slower for the interesting part; reverts at the closing brace
  run echo 'hi' > README.md
  run git add README.md
  run git commit -qm 'initial commit'
  run git log --oneline
  wait /^[0-9a-f]{7} initial commit$/ 10s
}

mark done
sleep 1500ms
```

**3. TUI session (vim) with `press` and `wait`**
(`--clean` and vim's `~` filler lines are from memory.)
```
size 90x24
shell /bin/sh
env PS1="$ "
cwd /tmp
timeout 10s

scene open {
  run vim --clean demo.txt
  wait /^~/                     # vim's empty-line filler; the echoed command line does not start with ~
}

scene edit {
  typing 80ms
  press i
  type Hello from a scripted vim session
  press esc
  press left x5
}

scene save {
  type :wq
  mark saving
  press enter
  wait /^\$$/                   # a bare prompt: the shell is back
}
```

**4. Long build, with a hidden setup block**
```
size 110x30
shell /bin/sh
env CI=1
env PS1="$ "
timeout 120s
typing 30ms

hidden {
  run cd ~/src/app
  run make clean > /dev/null 2>&1
  run clear
}

scene build {
  run make build > build.log 2>&1; echo BUILD-STATUS=$?
  wait /^BUILD-STATUS=0$/ 5m     # anchored: the echoed command starts with the prompt
  mark built
}

scene check {
  run ./bin/app --version
  wait /v[0-9]+\.[0-9]+/
}
```

**5. Deterministic CI script**
```
#!/usr/bin/env -S shellshot record --script
# ci-smoke.script: reproducible smoke-test recording
size 80x24
shell /bin/sh
cwd /tmp/ci-work
env PS1="$ "
env TERM=xterm-256color
env NO_COLOR=1
env LANG=C.UTF-8
seed 42
jitter 25%
typing 35ms
timeout 20s
output smoke.shot

scene version {
  run ./bin/app --version
  wait /^app v[0-9]+\.[0-9]+\.[0-9]+$/
}

scene help {
  run ./bin/app --help | head -n 5
  wait "Usage:"
  mark help-shown
}

scene interrupt {
  run sleep 30
  sleep 500ms
  press ctrl+c
  wait /^\$$/ 5s
  press up up enter     # recall and run a previous command
}
```

---

## 6. Key encoding with libghostty (carried over from v1; mostly unverified)

Only `press` uses the encoder. `run`/`type` text is sent as raw UTF-8, one rune per write (so a multi-byte rune never splits).

### 6.1 Verified from headers and binding (direct evidence)
- `ghostty/vt/key/encoder.h` defines `ghostty_key_encoder_new/free/set/encode` and **`ghostty_key_encoder_set_from_terminal(encoder, terminal)`**, which reads cursor-key application mode, keypad mode, alt-escape prefix, modifyOtherKeys state and Kitty keyboard flags from the live terminal.
- Options include `CURSOR_KEY_APPLICATION` (DECCKM), `KEYPAD_KEY_APPLICATION`, `ALT_ESC_PREFIX`, `MODIFY_OTHER_KEYS_STATE_2`, `KITTY_FLAGS`, `BACKARROW_KEY_MODE`.
- Go binding (GitHub `main`, **not the pinned version**): `NewKeyEncoder()`, `SetFromTerminal(*Terminal)`, `SetBool`, `SetKittyFlags`, `Encode(*KeyEvent) ([]byte, error)`, `Close()`. Not safe for concurrent use. `KeyEvent` has `SetAction/SetKey/SetMods/SetUTF8/SetUnshiftedCodepoint`. `Key` constants follow the W3C `code` list. `SetUTF8` takes the *unmodified* character, never C0 controls.

### 6.2 NOT verified
- Actual output bytes (`up` → `ESC [ A` off and `ESC O A` under DECCKM, `ctrl+c` → `0x03`, `enter` → `0x0D`). The DECCKM sequences are from memory (xterm behaviour).
- Whether the pinned module (pseudo-version 20261106) has the same API as GitHub `main`, including the constructor names (`NewKeyEvent`, `NewTerminal`, `VTWrite`).
- Whether `ctrl+<letter>` needs `SetUTF8("c")` or derives it from the key.
- Task T5 (§9) is the experiment that closes these: encode `up`, `ctrl+c`, `enter`, `backspace`, `f5`, `alt+x` before and after feeding `ESC[?1h` to the terminal, and assert on the bytes.

### 6.3 Recommendation (inference)
Use the encoder for every `press` key, because it follows DECCKM and Kitty flags of the *live* headless terminal (vim, less and htop switch them on; a static table sends wrong bytes). Modifier combinations also multiply in a hand table. Cost: cgo coupling, contained by a consumer-side interface (§8) and a tiny pure-Go fallback table for tests only.

---

## 7. Semantics and determinism

### 7.1 Time model
- The child is real, so its output timing is real. Timestamps in the recording come from the monotonic clock, as in live `record`.
- **v1 is real time.** `type`, `press` and `sleep` really wait. A 60 s script takes 60 s.
- Delay between typed keys is `typing`, applied between units (runes, key presses and repeats), not after the last. `run` is `type` plus Enter, with the same delay before the Enter.
- With `jitter J`, each delay is drawn uniformly from `[d(1−J), d(1+J)]` using a `math/rand/v2` PCG seeded by `seed`. Same seed + same script gives the same *scheduled* delays. Recorded timestamps still vary (scheduler latency, real child output).
- The clock is an interface (`Now`, `Sleep(ctx, d)`), so tests use a fake clock. A `--fast` mode that skips delays changes the recording (spinners get fewer frames), so it would be a different mode. **Not in v1.**

### 7.2 Scenes, hidden spans and marks
- Entering `scene NAME` emits MARK `scene:NAME:start` before its first step is issued. Leaving it emits `scene:NAME:end` after its last step has completed (including its last `wait`).
- `hidden { }` emits `hidden:start` and `hidden:end` the same way. Nested in a scene, the hidden pair lies between the scene's pair.
- Marks carry no payload but the label. User `mark` labels may not start with `scene:` or `hidden:` (validator error), so the prefixes identify structure unambiguously.
- Steps in a `hidden` block **run and are recorded normally.** The child's output must keep flowing so terminal state stays correct. `hidden` is metadata only: v1 replay and export ignore it and behave as today.
- If the script fails inside a block, the recording is finalised with an unclosed `start` mark. Readers must tolerate that (a span with no end runs to the end of the recording).
- **How exporters and the timeline may use this (future ideas, not specified here):** `export --scene NAME` (render only that span), `export --skip-hidden` (drop hidden spans), and chapter markers for a timeline UI. None is part of this work.

### 7.3 Wait semantics
- The executor is polled on the **session's single goroutine** (§8.2). After each output chunk is applied to the terminal, and on a timer tick, it is handed a `Screen` and calls `Text()`: the current visible rows (alt screen included when active), each right-trimmed, joined by `\n`. No extra goroutine touches the terminal, which fits the session rule that the vt is not concurrency-safe.
- A quoted pattern is `strings.Contains`. A regex is compiled with `(?m)` and used with `MatchString`. A literal may contain `\n` to match across rows.
- **Why more robust than VHS (interpretation):** VHS polls the xterm.js buffer remotely and defaults to the last line. We match libghostty's *rendered grid* in-process, where cursor movement, overwrite, clear-line, alt-screen and wrapping are already resolved. Raw-byte matchers break on progress bars redrawn in place.
- Scrollback is not searched (it is not verified that the binding exposes it). A `wait` for text that has scrolled off times out. Document this.
- **Timeout** is, in order: the explicit `wait PATTERN TIMEOUT`, else the `timeout` in effect (block-scoped), else 15 s. On timeout:
  1. Error `file:line:col: wait timed out after 15s waiting for /re/ (scene build)`, plus the final screen dumped to stderr (the most useful CI artefact).
  2. The child is closed (SIGHUP, kill after grace).
  3. The recording is **finalised and kept**, so it stays valid for `replay`.
  4. Exit code 3.
- A `wait` that already matches when reached returns at once. If the child has exited, the final screen is tested once; with no match it fails immediately with "child exited (status N) before match".
- The echo trap (§2.2 #5) means docs must say: anchor on line start (`/^DONE$/`), or print the sentinel through a split literal (`echo DO"NE"`).

### 7.4 Exit rules and codes
- **End of script:** (1) short settle so final output lands (suggestion: PTY quiet for 200 ms, max 2 s); (2) if the child still runs: shell → EOF (Ctrl-D, 0x04), other program → close the PTY master (SIGHUP); (3) grace (default 2 s), then kill. The final frame and footer are written as `session` does today.
- **Child exits mid-script:** later `run`/`type`/`press` fail with "child exited"; `sleep`/`mark`/`wait` are allowed (see 7.3). Ending with `run exit` or `press q` is normal.
- **Exit codes:** `0` script completed (regardless of child status, as today's `record`); `1` runtime error (I/O, PTY, recording write); `2` parse or validation error (nothing spawned, no file written); `3` script failure (`wait` timeout, child exited when input was required); `130` cancelled by signal. Optional `--propagate-exit` makes the child's final status the exit code.

### 7.5 What is recorded, and cancellation
- OUT events: raw PTY output including echo of scripted typing, as live.
- IN events: off by default. Scripted keystrokes use the same write path as live ones, so `--record-input` records them with no extra code.
- MARK events: `mark`, plus the block marks of §7.2.
- RESIZE: only the initial size, from `size`. Scripted mode forwards no real stdin and does not mirror output to stdout by default.

`cmd.Context()` cancellation flows into the session, which already closes the process and returns an error wrapping `ctx.Err()`. Extra requirement: finalise the recording and exit 130. The executor never blocks without a context-aware wait.

---

## 8. Architecture fit

### 8.1 Packages
```
internal/script            ast.go lex.go parse.go validate.go diag.go   // pure Go, no deps
internal/script/keys       // "ctrl+shift+left" ⇄ (Key, Mods); pure Go, NO ghostty types
internal/script/run        // lower.go: AST → Plan (scopes resolved, marks inserted)
                           // exec.go: Plan → non-blocking state machine; declares its own interfaces
internal/vt/ghostty        // + KeyEncoder adapter (cgo stays here)
internal/session           // + optional Driver hook (8.2)
internal/cli               // record.go: --script flag, merge settings, wire concrete types
testdata/script/*.script   // golden inputs; *.ast.golden, *.timeline.golden
```
`script`, `keys` and `run` import no cgo, no pty and no cobra. `run` declares its own consumer-side interfaces:
```go
type Screen interface{ Text() string }                      // visible text, \n-joined
type Keys   interface{ Encode(keys.Chord) ([]byte, error) } // adapter over libghostty
type Clock  interface{ Now() time.Duration }
```
**Lowering** resolves scopes once: every op in the `Plan` already carries its effective `typing`, `jitter` and `timeout`, and the block marks are explicit ops. The state machine then has no scope logic, and golden tests on the Plan are trivial.

### 8.2 Hook into the session
The executor is a **non-blocking state machine** the session loop polls, not a goroutine that blocks:
```go
type Driver interface { // defined in internal/session (consumer)
    // Poll runs on the session goroutine after each output chunk and when the
    // previous deadline passes.
    Poll(now time.Duration, scr Screen) (Step, error)
}
type Step struct {
    Write    []byte        // inject into the PTY via the live-input path
    Mark     string        // emit a MARK event
    Deadline time.Duration // wake me no later than this (0 = on output only)
    Done     bool
}
```
`wait` is "keep polling and match `scr.Text()`". A fake clock and screen make the executor a pure function. (The exact structure of the session select loop was not read; adjust.) `Keys` is implemented by the ghostty adapter holding the `*Terminal` and calling `SetFromTerminal` before each `Encode`.

### 8.3 CLI wiring
- Extend `record`: `shellshot record --script demo.script [-o out.shot] [--cols/--rows] [--record-input]`. The CLI is reduced to `record/replay/export/version`, and a script is a different input source for `record`.
- Precedence: explicitly set flags (`cmd.Flags().Changed`) > script > defaults. `-o` overrides `output`; `--cols/--rows` override `size`. Whether `-c`/`-- cmd` may be combined with `--script` is open (§11); the recommendation is that the CLI command overrides `shell`.
- Parse and validate the whole file before spawning anything (exit 2). Rename the variable `script` in `record.go` (it holds the `-c` string) when adding the flag.

---

## 9. Task plan (small, atomic, each independently testable)

| # | Task | Output | Tests |
|---|---|---|---|
| T1 | Lexer | tokens with line:col; keyword-directed modes (§4.4) | Table tests per mode: TEXT, STRING escapes, REGEX with `\/`, `#` rules, comments, `{`/`}`; error positions |
| T2 | Parser + canonical printer | AST (§4.5); syntax errors; permissive superset of the grammar | Golden `testdata/*.script` → `*.ast.golden`; `go test -fuzz` (never panics; stable error text; accepted input prints and re-parses to the same AST); the five examples of §5 are golden inputs |
| T3 | Validator + diagnostics | Setting names, scoping (process settings placement), nesting, block rules, unique scenes, durations/percent/size ranges, key names, mark prefixes, UTF-8; did-you-mean | Table tests, one per error with exact message and line:col; max 10 errors |
| T4 | `keys` package | chord text → `(Key, Mods)`; names | Every documented name parses; invalid rejected; modifier order irrelevant |
| T5 | ghostty `KeyEncoder` adapter | `keys.Chord` → bytes with `SetFromTerminal` | cgo test: arrows with and without `ESC[?1h`, `ctrl+c`, F-keys. **This is the §6.2 verification** |
| T6 | Lowering + executor state machine | `Lower(AST) → Plan`; `run.New(plan, Keys, Clock, rng)` with `Poll` for `run/type/press/sleep`, jitter | Fake clock → expected `[]Step` timeline golden; scoped `typing` reverts at `}` |
| T7 | Wait semantics | string and regex, timeout precedence, child exited, screen text rules | Fake screen sequences; timeout error text including scene name; echo-trap cases anchored vs not |
| T8 | Mark emission | `mark`, `scene:*`, `hidden:*` ops incl. nesting and failure inside a block | Timeline goldens; unclosed-span case |
| T9 | Session `Driver` hook | Poll after chunks and on deadline; inject writes; end-of-script and grace | session tests with the existing fake Process/Terminal |
| T10 | CLI wiring | `record --script`; precedence merge; exit codes 0/1/2/3/130 | cli tests with a fake runner; one real end-to-end `hello.script` against `/bin/sh`, gated on libghostty |
| T11 | Docs | `docs/SCRIPT.md`, ARCHITECTURE update, `examples/*.script` | CI runs the examples and checks screen state at marks |

T1–T4 are pure Go and can start now. T6–T8 depend on T1–T4. T5 needs the build tree. T9 is the only change to a shared package (`session`); schedule it after the binary-format session changes land.

**Test strategy:** golden scripts plus timeline goldens (`t=+50ms write "h"`, `t=+1.2s mark scene:init:start`, `-update` to refresh). End-to-end tests assert screen state at marks, never timestamps.

---

## 10. Alternatives considered

- **Flat VHS-like list (the v1 brief).** Rejected by the user: limiting, no grouping, no way to chapter or hide a span, and settings can only live in a preamble.
- **YAML (or TOML/JSON) steps.** Easy to parse, but verbose for typed text (`- run: "git commit -m \"x\""`), quoting hazards, no comments in JSON, and grouping is just indentation with no clearer error positions. Terminalizer shows how it reads (fetched via search summary).
- **Transcript style** (`$ cmd` to run, `> pattern` to wait, as in cram or doc-tests). Very readable for the simple case, but no room for keys, durations, settings or blocks without inventing a second syntax inside the transcript.
- **Embedded languages: Starlark, Lua (gopher-lua), goja (JS), Tengo, Risor, Yaegi, CEL/expr.** Rejected by the user, and for these reasons (library characteristics are from memory, not re-verified):
  - *Complexity:* a new dependency, a host API to design and document, and a language users must learn for what is a list of keystrokes.
  - *Static analysis:* a program cannot be validated, nor its scenes listed, without running it. We want "exit 2 before spawning anything" and exporters reading structure.
  - *Determinism:* loops, maps, time and randomness make a run depend on more than the file. We want the file to define the run (plus a seed).
  - *Execution model:* `wait()` in a script is a blocking call, but the session is a single goroutine that owns the vt. A scripting runtime would run on its own goroutine and need a channel handshake with the session loop on every `wait`/`sleep`, breaking "no extra goroutine touches the terminal". Our state machine needs none. CEL and expr are expression-only and would not give statements at all.

---

## 11. Open questions, risks, decisions for the user

**Decisions (recommendation first)**
1. **Process settings only before the first step** (`size shell env cwd seed output`), with only `typing jitter timeout` scopable (§3.2). This narrows "settings may appear inside a block" to settings that can take effect. Confirm.
2. **Nesting:** only `hidden` inside `scene` is legal (so `scene` in `hidden` is an error), empty blocks are errors, scene names unique. Confirm.
3. **`shell PATH` takes no arguments.** A reproducible prompt is therefore `shell /bin/sh` plus `env PS1=…`, which is unverified. Allow `shell PATH ARGS…` (e.g. `/bin/bash --noprofile --norc`)? Recommendation: yes, it is a small addition.
4. **Mark labels:** `scene:NAME:start`, `scene:NAME:end`, `hidden:start`, `hidden:end`. Automatic `script:start`/`script:end` marks too? Cheap, but adds events the user did not write.
5. **Exit code on child failure:** `0` with `--propagate-exit` opt-in, or the reverse?
6. **CLI `-c`/`--` with `--script`:** override `shell`, or forbid the combination?
7. **Mirror output to stdout while scripting?** Recommendation: off, `--watch` to show.
8. **`output` naming non-`.shot` files** (e.g. `demo.apng`, which would trigger export)? Recommendation: no in v1.
9. **AST shape:** `Block.Items` (not `Steps`) and `Setting.At` deviate slightly from the requested `Block{Steps}`; the alternative is to make `Setting` an `Item` variant, which is simpler but changes the stated `Item = Step | Block`.

**Risks**
- **Echo trap on `wait`:** the most likely source of user complaints. Mitigated by docs and anchoring only.
- **Rest-of-line text** surprises: `run "hi" | wc` is an error, not literal text; trailing `#` comments on `run` are text. Mitigated by the specific error message.
- **cgo key encoder API** may differ at the pinned commit; the `keys` → `ghostty.Key` table is manual.
- **Scrollback is not matchable**; long-output scripts may time out unexpectedly.
- **Session hook (T9)** touches the loop that the binary-format work is also changing. **Non-reproducible bytes:** only screens at marks are promised.

**Unverified claims (summary):** encoder output bytes; pinned-version API match; DECCKM sequences; that `/bin/sh` honours imported `PS1`; vim `--clean` and `~` filler; `env -S` availability; scrollback access via the binding; the embedded-language library characteristics in §10; asciinema-automation directives, `vhs-action` and VHS browser-stack pain (memory); availability of MARK events in the final binary format.

## Final recommendation

Ship `record --script` in this order: T1→T4 (pure Go, now), T6–T8 (executor against fakes), T5 (key-encoder verification), then T9–T11. Confirm decisions 1–3 before T3, since they fix the validator rules. Keep the surface at six commands, nine settings and two block kinds; the reserved-word list is the growth path.

## Sources
- Kept: VHS README (https://github.com/charmbracelet/vhs/blob/main/README.md): fetched in the v1 run; commands, `Wait` defaults, "late Set ignored".
- go-libghostty `key_encoder.go`/`key_event.go` on GitHub main (https://github.com/mitchellh/go-libghostty): fetched in the v1 run; not the pinned version.
- Repo: `build/libghostty/include/ghostty/vt/key/{encoder,event}.h` (partial); `docs/research/binary-format.md` (MARK, kind 3, UTF-8 label); `internal/cli/record.go`, `internal/session/session.go` (lines 1–80).
- Search-summary level only: demo-magic template, doitlive docs, Terminalizer config, asciinwriter.

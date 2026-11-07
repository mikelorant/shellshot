# Research: automation script format for shellshot (`record --script`), v2 consolidated

Status: design brief, **v2 consolidated**. One design: the v2 grouped-block language, the scenes refinement ([`script-scenes.md`](script-scenes.md)) and the user's latest decisions (named text blocks, `linger`, the command/setting budget, SPAN events). `script-scenes.md` stays as supporting detail (recording, export, the VHS-tape reference case); **where the two differ, this file wins**. No code exists yet.

**Evidence limits (read first).**
- This is a design. **No code was run.** Examples were checked by a throwaway Python checker written to this file's EBNF (§4.3), not by the real parser. The checker is not committed. It approximates RE2 with Python `re`.
- libghostty key encoding (§6) is inherited from the v1 research run (no shell). Encoder output bytes, the pinned-version API and DECCKM sequences are **unverified**. The headers in `build/libghostty/include/ghostty/vt/key/` were read directly; the Go binding was read from GitHub `main`, not the pinned version.
- MARK (kind 3) and the rule "kinds ≥ 64 are ancillary" come from `docs/research/binary-format.md` (a design). SPAN (kind 64) and its payload are from `script-scenes.md` §4.3 and are unchanged here. Whether `Writer` flushes on kind 64, and how `frames` treats ancillary events, are **unverified**. Of the code I know only what the two earlier briefs quote (`record.go`, `session.go` 1–80, `timeline.go`, `event.go`).
- Prior-art claims are marked **fetched** (v1 run) or **memory** (unverified).

---

## 1. Summary and recommendation

**Build a small, line-oriented, declarative script language in `.script` files, run by `shellshot record --script FILE`. A script is top-level settings, then steps and blocks. It has six commands, two block kinds, and named text blocks for multi-line text. It has no conditions, loops, variables, functions or expressions.**

- **Commands (6):** `run`, `type`, `press`, `wait`, `sleep`, `mark`. `expect` is not a seventh: `wait "x" 0s` is an assertion (§3.1).
- **Blocks (2):** `scene NAME { … }` is a named, exportable chapter. `hidden { … }` runs and is recorded but export may drop it. A `hidden` may sit in a `scene`; no other nesting.
- **Settings (14) in three classes:** process (`size shell env cwd seed output`, before the first step only), behaviour (`typing jitter timeout settle linger`, anywhere, scoped in blocks) and export hints (`speed idle hold`, preamble or in a `scene`). A setting line inside a block applies to the closing `}`, then reverts.
- **Text:** `run npm install` takes the rest of the line literally; a leading `"` switches to an escaped string. Multi-line text uses **named text blocks** `type|run lines { … }` and `type|run paragraphs { … }` (§3.5). There is no line continuation.
- **Waiting:** `run` waits for a quiet screen (`settle`, 200 ms). `wait idle` waits for quiet explicitly. `wait PATTERN` matches libghostty's rendered screen. `linger 2s` pauses after each step so a viewer can read (§3.2).
- **Recording:** scenes and hidden spans are **SPAN events** (kind 64, ancillary, binary); user `mark` stays MARK (§7.2).
- **Static:** the whole file is parsed and validated before anything spawns (exit 2). `record --script FILE --list` prints the plan.
- **Kept from v1:** a non-blocking executor polled by the session goroutine; key chords through libghostty's `KeyEncoder`; real time only; reproducible inputs and screens at marks, not byte-identical recordings; own exit codes.
- **Deferred or excluded:** §3.4.

---

## 2. Prior art

| Tool | Model | Take | Leave |
|---|---|---|---|
| **VHS** `.tape` ([README](https://github.com/charmbracelet/vhs/blob/main/README.md), **fetched**) | Flat line DSL over ttyd, headless Chromium, ffmpeg | Verb per line; `Wait`/`Sleep`/`Env`/`Output`; chords | Browser stack; ~25 visual `Set`s; `Source`; `Copy/Paste`; `Hide/Show`; flat structure |
| **asciinema-automation** (**memory**) | `#$ expect`, `#$ delay` comments | Waiting on output is the core primitive | Raw byte-stream matching |
| **demo-magic**, **doitlive** (**search summary**) | Bash script / session file with `#doitlive speed: 3` | `run` ≈ `pe`; in-file `key: value` settings | Needs a human; script is code |
| **Terminalizer** (**search summary**) | YAML records; `maxIdleTime` | Idle capping belongs at export | YAML as authoring format |
| **expect/pexpect** (**memory**) | Programmatic send/expect | Fail loudly on timeout | Matching a raw byte stream |

**Pain points and our answer.**
1. *Late settings silently ignored (fetched; VHS README).* Every accepted setting is effective, or a parse error with line:col.
2. *Unitless durations (fetched; `Sleep 2` s vs `TypingSpeed 0.1` s).* Durations always carry a unit.
3. *Weak `Wait` defaults (fetched; regex `/>$/`, last line, 15 s).* We match the whole screen; the pattern is mandatory.
4. *`Sleep` padding (inference).* `wait` is robust; `run` settles; `linger` names the "give the viewer time" intent.
5. *Echo matches itself (classic expect trap).* `run make && echo DONE` puts `DONE` on screen as echo. Docs say: anchor (`/^DONE$/`).

**Why v2 differs from VHS.** Named scenes instead of a flat list; scoped settings that revert at `}` instead of global `Set`; `hidden` keeps recording (VHS `Hide` stops capture, which would falsify a log) and marks the span instead; process settings only before the first step; rest-of-line text so `run git commit -m "msg"` needs no quoting.

---

## 3. The language

### 3.1 Commands (6)

| Command | Meaning |
|---|---|
| `run TEXT` | Type TEXT, press Enter, wait for a quiet screen (`settle`). The common case. |
| `type TEXT` | Type TEXT without Enter. |
| `press KEY…` | Key chords, e.g. `press ctrl+c`, `press up up enter`. A key may be followed by a repeat `xN`: `press down x3`. |
| `wait PATTERN [TIMEOUT]` | Block until the visible screen matches PATTERN: a quoted string (substring) or `/regex/`. TIMEOUT overrides `timeout`. **`wait "passed" 0s` checks the current screen once and fails at once if it does not match: this is the assertion** (there is no `expect`, §11). |
| `wait idle [DURATION]` | Block until the screen has had no output for DURATION (default: `settle`, or 200 ms if `settle off`). Fails at `timeout`. |
| `sleep DURATION` | Pause. |
| `mark LABEL` | Emit a MARK event (a user label, §7.2). |

`run` and `type` take **one text form** (§3.5): rest-of-line, a quoted string, or a text block. `run` does not wait for the command to *finish*, only for a quiet screen; use `wait PATTERN` for slow commands.

### 3.2 Settings (14)

| Setting | Argument | Default | Class |
|---|---|---|---|
| `size` | `COLSxROWS`, each 1–1000 | current terminal, else 80x24 | process |
| `shell` | PATH of the program to run | `$SHELL`, else `/bin/sh` | process |
| `env` | `KEY=VALUE`, repeatable; later wins | inherited | process |
| `cwd` | PATH | cwd of `shellshot` | process |
| `seed` | unsigned int, jitter PRNG seed | `1` | process |
| `output` | PATH of the recording | `recording.shot`; `-o` wins | process |
| `typing` | DURATION between typed units | `50ms` | behaviour |
| `jitter` | PERCENT `0%`–`100%` | `0%` | behaviour |
| `timeout` | DURATION > 0, default for `wait` | `15s` | behaviour |
| `settle` | DURATION, or `off`: quiet time `run` waits for after Enter | `200ms` | behaviour |
| `linger` | DURATION: **pause this long after every step in the block so the viewer can read the screen** | `0s` | behaviour |
| `speed` | NUMBERx, > 0 (`8x`): export playback speed of the span | export default | hint |
| `idle` | DURATION: export cap per gap in the span | none | hint |
| `hold` | DURATION: extra export time on the span's last screen | none | hint |

**`linger`, precisely.** After every `run`, `type`, `press` and `wait` (including `wait idle`) completes, the executor pauses `linger` before the next step; for `run` that is after the settle. `sleep` and `mark` get no linger (a `sleep` already is a pause). A text block is **one** step, so it gets one linger after the whole block, never one per line. The last step in a block lingers too (inside the span). Unlike `hold`, `linger` costs real recording time; `hold` is export-time only.

**Where each class is legal:**

| Class | Preamble (before first step or block) | Later at top level | In `scene` | In `hidden` |
|---|---|---|---|---|
| process | yes | **error** | **error** | **error** |
| behaviour | yes | yes (from that line on) | yes | yes |
| hint | yes (script-wide default) | **error** | yes | **error** (a hidden span has no output) |

Scoping: a setting at top level applies to everything after it. **A setting line inside a block applies from that line to the closing `}`, then the previous value is restored**; it may appear anywhere in the block, not only first. Process settings need a new child ("`size` must appear before the first step (line N)"). Precedence: CLI flags (when explicitly set) > script > defaults. Hints resolve as: export flag > scene hint > script preamble hint > export default.

### 3.3 Blocks

```
scene NAME {        # NAME: letters, digits, _ and -; unique; written bare, never quoted
  …settings and steps…
}
hidden {
  …
}
```
- Nesting is at most one level: `hidden` inside `scene` is allowed. `scene` in `scene`, `hidden` in `hidden`, `scene` in `hidden` are errors (the last two are pointless or confusing).
- A block must contain at least one step.
- `hidden` steps run exactly like others (failures fail the run); `hidden` only marks a span that export may drop. A trailing `hidden` runs on success only; `cleanup { }` is deferred.
- At the end of the script the executor shuts the shell down inside an **implicit trailing hidden span** (§7.4). A script author writes nothing.

### 3.4 Deliberately excluded or deferred

| Item | Status | Why |
|---|---|---|
| Conditions, loops, variables, expressions, functions | Excluded | Rejected by the user; they make scripts non-static. `env` plus the shell cover real needs; copy lines instead |
| `include`/`source` | Excluded | Path and cycle semantics; `cat a.script b.script` works |
| `require` | **Deferred, reserved** | A hidden `command -v x && echo OK` plus `wait` covers it (tape, `script-scenes.md` §8.4). Add if CI users ask |
| `expect`, `expect not`, `wait now` | Excluded | `wait "x" 0s` is the assertion. "Absent now" is unsupported (RE2 has no lookahead); add `expect not` only if asked |
| `caption`, `cleanup { }`, `fresh`, `export { }`, per-scene `budget` | Deferred, reserved | `script-scenes.md` §3 |
| `copy`/`paste`, `screenshot`, mid-script `resize` | Excluded | Use `type`; `export --at MARK`; `size` sets the initial size |
| Theme/font/padding/pixel settings, GIF | Export-time | Export flags; shellshot exports APNG |
| `type@250ms` one-off suffix | Dropped | Two mechanisms for one idea. Put `typing 250ms` before the `type` line (it reverts at `}`) |
| `\` line continuation | Dropped | Replaced by text blocks (§3.5) |
| Heredoc `<<` | Excluded | Text blocks cover it (they can also type a shell heredoc, Example 2) |

**Repeating:** copy lines, or `press down x3`. **Extending without breaking the grammar:** new keywords at the start of a line, new setting names, new block kinds fitting `KIND [ARGS] {`. These words are **reserved** (error "reserved for future use", not "unknown command"):

`if else elif for while repeat loop break let var const set fn func def define call return include import source require use copy paste screenshot resize hide show theme font caption cleanup fresh export expect budget`

### 3.5 Text forms for `run` and `type`

**Single line (unchanged).**
- *Rest of line:* `run npm install` types `npm install`. The text is trimmed; `#`, `\` and inner quotes are literal. Valid only if the text does not start with `"` and is not a text-block header.
- *Quoted:* `type "echo \"hi\"\n"` allows `\n \t \" \\ \e \xHH` (any other `\` is an error), leading/trailing spaces, and a trailing comment. `\n` is typed as Enter (CR, 0x0D). `run "hi" | wc` is an error: "text after closing quote; to type a leading quote use `run "\"hi\" | wc"`".

**Named text blocks (multi-line).** Four forms: `type lines {`, `type paragraphs {`, `run lines {`, `run paragraphs {`.

```
run lines {                 # header: keyword, shape, "{", optional comment
  git commit -q -F - <<'EOF'
  fix: racing of requests (#1)
  EOF
}                           # alone on its line, at the SAME indentation as "run"
```

Body rules (all deterministic, applied in this order):
1. **Raw.** The body is never parsed as script: no escapes, `#` and quotes are literal, `}` is literal text unless it is the closing line.
2. **Closing line:** a line with only `}` (and optional trailing whitespace; **no comment**), at exactly the same leading whitespace as the `run`/`type` line. A `}` at any other indentation is literal body text. To type a line that is only `}` at that indentation, indent the body deeper or use `type "}"`.
3. **Indentation:** the common leading whitespace of the non-blank lines (longest common prefix; the minimum indentation when one kind of whitespace is used) is stripped. Whitespace-only lines count as blank. Trailing whitespace on every line is trimmed (use a quoted string if it matters).
4. **Leading and trailing blank lines are ignored.** A body with no non-blank line is an error.
5. **`lines`:** each remaining line is typed as written (relative indentation kept; blank lines inside stay blank lines), with Enter **between** lines. `type lines` does not press Enter after the last line; `run lines` does, and then settles once. This matches single-line `type` vs `run`.
6. **`paragraphs`:** runs of non-blank lines form a paragraph; its lines are trimmed and joined with **one space** (soft wrapping). Paragraphs are separated by a blank line, typed as **two Enters** (a blank line in the typed text); any number of blank lines in the source is one separator. `type paragraphs` has no trailing Enter; `run paragraphs` presses one and settles once.

| Source body | `type …` types | `run …` types |
|---|---|---|
| `lines { a ⏎ b }` | `a`⏎`b` | `a`⏎`b`⏎ |
| `paragraphs { one ⏎ two ⏎⏎ three }` | `one two`⏎⏎`three` | `one two`⏎⏎`three`⏎ |

**Timing.** A block is one step, typed as one stream of units: every character and every Enter is one unit with `typing` between consecutive units (jitter applies the same way), no extra pause at line ends. `run` presses its final Enter after the same delay, then **settles once** after the last line. `linger` follows the whole block once.

**Caveats.** In a shell, `run lines` hands lines over without waiting for each command; the tty buffers typeahead, which is fine for shell commands and wrong for interactive programs (use separate `run` steps with `wait`). **Disambiguation:** `run lines {` is a block header only if the rest of the line is exactly `lines` or `paragraphs`, optional spaces, `{`, optional comment; otherwise it is rest-of-line text (`run lines` types `lines`; `run "lines {"` types it with a brace). A mistyped `run line {` is rest-of-line text, and its body followed by a lone `}` gives `unexpected "}"`, whose message suggests `lines`/`paragraphs`.

---

## 4. Concrete syntax

### 4.1 Rules

- **Line-oriented.** One statement per line; indentation is insignificant outside text blocks (two spaces by convention); blank lines are ignored. CRLF files are accepted (the lexer normalises to LF). The file must be valid UTF-8.
- **Comments.** `#` starts a comment at the start of a token: a whole line, or after a statement's arguments or a block header. Not in rest-of-line text or text blocks, where `#` is literal. `#!…` on line 1 is an ordinary comment.
- **Blocks.** The header ends with `{` (after a space); `}` is alone on its line (a comment may follow).
- **Case.** Everything is case-sensitive; keywords, setting and key names are lower-case.
- **Strings.** `"…"` with `\n \t \" \\ \e \xHH`; `\xHH` produces byte HH and the result must be valid UTF-8.
- **Regex.** `/…/`, Go RE2, no flags; `\/` is a slash, every other `\` goes to RE2. Applied multiline: `^`/`$` match at screen row boundaries.
- **Durations.** `<number><unit>`, unit `ms`, `s`, `m` (`500ms`, `1.5s`, `2m`); a bare number is an error. Percent `25%`, size `100x28`, speed `8x`.
- **Keys.** Named: `enter tab esc backspace delete insert space up down left right home end pageup pagedown f1`…`f12`; plus a single `a`–`z` or `0`–`9`. Chords `ctrl+KEY`, `alt+KEY`, `shift+KEY`, combinations in any order, no repeats. A repeat is a separate token `xN` (N: 1–999) after a key.
- **Unknown identifiers** (commands, settings, keys, block kinds, units) are errors with `file:line:col`, the line with a caret, and a **did-you-mean** (edit distance ≤ 2 among names legal there). All errors are reported, up to 10. Reserved words say so.

  ```
  demo.script:12:3: unknown command "rn" (did you mean "run"?)
  demo.script:4:1: "size" must appear before the first step (first step: line 3)
  demo.script:9:5: "scene" cannot be nested inside scene "intro"
  demo.script:7:3: unterminated text block opened here: expected a line with only "}" at column 3 (found "}" at line 12 with different indentation; that line is literal text)
  demo.script:7:3: empty text block: no non-blank line (to press Enter alone use "press enter")
  ```

### 4.2 Files and header

Extension **`.script`** (the recording is `.shot`). A first line `#!/usr/bin/env -S shellshot record --script` makes a script executable (`env -S` availability is **unverified**). No version pragma: the reserved-word list is the compatibility mechanism.

### 4.3 EBNF

`SP` is a space or tab; `NL` is a newline or end of file; `ANY` is any character. Lines may start with `{ SP }`.

```ebnf
script          = preamble , { blank | behaviour-setting | step | scene | hidden } ;
preamble        = { blank | process-setting | behaviour-setting | hint-setting } ;
                  (* the preamble ends at the first step or block; the parser accepts every setting
                     anywhere and the validator applies the legality table of 3.2 *)
blank           = tail , NL ;
tail            = { SP } , [ comment ] ;
comment         = "#" , { ANY - NL } ;

scene           = "scene" , SP , { SP } , name , SP , { SP } , "{" , tail , NL ,
                  { blank | behaviour-setting | hint-setting | step | hidden } , close ;
hidden          = "hidden" , SP , { SP } , "{" , tail , NL ,
                  { blank | behaviour-setting | step } , close ;
close           = { SP } , "}" , tail , NL ;

process-setting   = ( size | shell | env | cwd | seed | output ) , tail , NL ;
behaviour-setting = ( typing | jitter | timeout | settle | linger ) , tail , NL ;
hint-setting      = ( speed | idle | hold ) , tail , NL ;
size    = "size" , SP , { SP } , int , "x" , int ;      shell  = "shell" , SP , { SP } , arg ;
env     = "env" , SP , { SP } , key , "=" , [ arg ] ;   cwd    = "cwd" , SP , { SP } , arg ;
seed    = "seed" , SP , { SP } , int ;                  output = "output" , SP , { SP } , arg ;
typing  = "typing" , SP , { SP } , duration ;           timeout = "timeout" , SP , { SP } , duration ;
jitter  = "jitter" , SP , { SP } , number , "%" ;       linger  = "linger" , SP , { SP } , duration ;
settle  = "settle" , SP , { SP } , ( duration | "off" ) ;
speed   = "speed" , SP , { SP } , number , "x" ;
idle    = "idle" , SP , { SP } , duration ;             hold    = "hold" , SP , { SP } , duration ;

step    = run | type | ( ( press | wait | sleep | mark ) , tail , NL ) ;
run     = { SP } , "run" , SP , { SP } , ( text-block | text ) ;
type    = { SP } , "type" , SP , { SP } , ( text-block | text ) ;
text    = ( string , tail , NL ) | ( raw-text , NL ) ;
raw-text = ( ANY - SP - NL - '"' ) , { ANY - NL } ;     (* trailing SP trimmed; # and \ literal;
                  a raw-text that matches a text-block header is a text block (longest match) *)
text-block = ( "lines" | "paragraphs" ) , { SP } , "{" , tail , NL , { body-line } , block-close ;
body-line  = { ANY - NL } , NL ;                        (* raw; any line except block-close *)
block-close = indent , "}" , { SP } , NL ;              (* indent = exactly the leading whitespace of the
                  run/type line; the first matching line closes the block; EBNF cannot say this *)
press   = "press" , SP , { SP } , keypress , { SP , { SP } , keypress } ;
keypress = chord , [ SP , { SP } , repeat ] ;
wait    = "wait" , SP , { SP } , ( "idle" , [ SP , { SP } , duration ]
                                 | pattern , [ SP , { SP } , duration ] ) ;
sleep   = "sleep" , SP , { SP } , duration ;
mark    = "mark" , SP , { SP } , arg ;

pattern  = string | regex ;
chord    = { modifier , "+" } , keyname ;   modifier = "ctrl" | "alt" | "shift" ;
keyname  = "enter" | "tab" | "esc" | "space" | "backspace" | "delete" | "insert"
         | "up" | "down" | "left" | "right" | "home" | "end" | "pageup" | "pagedown"
         | "f1" | … | "f12" | lower | digit ;
repeat   = "x" , digit , { digit } ;
duration = number , ( "ms" | "s" | "m" ) ;
number   = digit , { digit } , [ "." , digit , { digit } ] ;      int = digit , { digit } ;
name     = ( letter | digit ) , { letter | digit | "_" | "-" } ;
key      = ( letter | "_" ) , { letter | digit | "_" } ;
arg      = string | word ;
word     = ( ANY - SP - NL - '"' - "#" ) , { ANY - SP - NL } ;
string   = '"' , { ( ANY - '"' - "\" - NL ) | escape } , '"' ;
escape   = "\" , ( "n" | "t" | '"' | "\" | "e" | ( "x" , hex , hex ) ) ;
regex    = "/" , regexchar , { regexchar } , "/" ;
regexchar = ( ANY - "/" - "\" - NL ) | ( "\" , ANY - NL ) ;
```

Notes:
- `press`, `wait`, `sleep`, `mark` may end in a comment. `run`/`type` carry their own line end: unquoted text cannot take one.
- **Validator-only rules** (the grammar is a superset so errors are precise): names against the known sets; legality table (3.2); no duplicate process settings except `env`; unique scene names; non-empty blocks; nesting (3.3); `timeout` and `idle` > 0, `wait idle` duration > 0, `speed` > 0; size ≤ 1000; repeat 1–999; modifiers not repeated; `\xHH` results valid UTF-8; raw text, text blocks and marks contain no control characters other than tab (use a quoted string with `\e`).

### 4.4 Tokenizer note

Hand-written scanner, one line at a time; the first word is the keyword (or `}`). The keyword selects how the rest of the line is scanned:
- `run`, `type`: one **TEXT** (rest of line, trimmed) or **STRING** plus optional COMMENT, **or a mode switch**: if the remainder is exactly a text-block header (`lines`|`paragraphs`, `{`, optional comment), the scanner emits `TEXTBLOCK{keyword, shape, opener Pos, body lines with Pos}` as **one token**: it reads raw lines until the first line equal to the keyword line's indentation + `}` + optional spaces, and tokenises nothing inside. End of file first: `unterminated text block` at the opener. All body-rule processing (strip, trim, join) happens in the parser, so the lexer stays a pure line splitter.
- `wait`: first argument is **STRING**, **REGEX** (decided by `"` or `/`) or the bare word `idle`. Other keywords never scan a REGEX, so `cwd /tmp` is not a regex.
- Everything else: **WORD**, **STRING**, `{`, `}`; `#` starts a COMMENT only at a token start (`cwd /tmp/a#b` is one WORD).
- Each token has line:col. Typed literals (duration, percent, size, speed, key, repeat) are WORDs classified in the parser, so "needs a unit" and did-you-mean errors live in one place.

### 4.5 AST shape

```go
type Pos struct{ Line, Col int }
type Script struct { Settings []Setting; Items []Item } // preamble and top-level settings; steps and blocks
type Item interface{ ItemPos() Pos }                    // implemented only by Step and Block
type Setting struct {
    Pos   Pos
    Name  string // "size", "typing", "linger", …; unknown names kept for the validator
    Value string // raw argument text; typed by the validator
    At    int    // effective before Items[At] of the enclosing list
}
type Step struct {
    Pos      Pos
    Kind     string        // run | type | press | wait | sleep | mark
    Form     string        // run/type: raw | quoted | lines | paragraphs
    Text     string        // run/type, escapes and block rules applied; "\n" = Enter
    Keys     []KeyPress    // press: {Chord string, Count int}
    Pattern  *Pattern      // wait: {Regex bool, Source string}; nil for wait idle
    Idle     bool          // wait idle
    Duration time.Duration // sleep; wait timeout or idle duration (0 = use setting)
    Label    string        // mark
}
type Block struct { Pos Pos; Kind string /* scene | hidden */; Name string
                    Settings []Setting; Items []Item }
```
`At` keeps source order between settings and items without making settings items. A canonical printer (AST → text) serves golden tests and the round-trip fuzz property; it prints blocks with the resolved text re-indented one level deeper, which the strip rule reverses.

---

## 5. Examples

Each was checked against §4.3 by the throwaway checker (every example passes; invalid variants (nesting, a process setting in a block, an unterminated or empty text block, a mis-indented closing line) are rejected). `/bin/sh` plus `env PS1=…` for a reproducible prompt is **unverified** (memory).

**1. Hello world**
```
#!/usr/bin/env -S shellshot record --script
# hello.script: the smallest useful demo
shell /bin/sh
env PS1="$ "
size 60x10
linger 1s              # the viewer gets a second on the final screen

run echo hello, world
```

**2. Git workflow with scenes and a shell heredoc** (assumes `/tmp/demo-repo` exists)
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

**3. TUI session (vim)** (`--clean` and the `~` filler are from memory)
```
size 90x24
shell /bin/sh
env PS1="$ "
cwd /tmp
timeout 10s

scene open {
  run vim --clean demo.txt
  wait /^~/                     # vim's empty-line filler; the echoed command line starts with the prompt
}

scene edit {
  typing 80ms
  linger 1s
  press i
  type lines {
    Hello from a scripted vim session.
    Second line, typed by shellshot.
  }
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

**4. Long build, hidden setup, export hints**
```
size 110x30
shell /bin/sh
env CI=1
env PS1="$ "
timeout 120s
typing 30ms
hold 1s                         # script-wide export default (written to meta)

hidden {
  run lines {
    cd ~/src/app
    make clean > /dev/null 2>&1
    clear
  }
}

scene build {
  speed 8x                      # export: play the wait at 8x
  idle 2s
  run make build > build.log 2>&1; echo BUILD-STATUS=$?
  wait /^BUILD-STATUS=0$/ 5m    # anchored: the echoed command starts with the prompt
  mark built
}

scene check {
  linger 2s
  run ./bin/app --version
  wait /v[0-9]+\.[0-9]+/
}
```

**5. Deterministic CI script with assertions**
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
settle 300ms
output smoke.shot

scene version {
  run ./bin/app --version
  wait /^app v[0-9]+\.[0-9]+\.[0-9]+$/
}

scene help {
  run ./bin/app --help | head -n 5
  wait "Usage:" 0s      # assertion: run has settled, so the text must be on screen now
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

**6. The VHS tape** (a `committed` TUI walkthrough): both translations are in `script-scenes.md` §8.2 (literal) and §8.4 (idiomatic: `linger`, `type paragraphs`, `hold`).

---

## 6. Key encoding with libghostty (carried over from v1; mostly unverified)

Only `press` uses the encoder. `run`/`type` text is sent as raw UTF-8, one rune per write; Enter inside text is CR (0x0D).
- **Verified from headers:** `ghostty/vt/key/encoder.h` has `ghostty_key_encoder_new/free/set/encode` and `ghostty_key_encoder_set_from_terminal`, which reads cursor-key application mode (DECCKM), keypad mode, alt-escape prefix, modifyOtherKeys and Kitty flags from the live terminal. Go binding (GitHub `main`, **not the pinned version**): `NewKeyEncoder()`, `SetFromTerminal`, `Encode(*KeyEvent)`; not safe for concurrent use. `SetUTF8` takes the unmodified character, never C0 controls.
- **NOT verified:** actual bytes (`up` → `ESC [ A` normally, `ESC O A` under DECCKM; `ctrl+c` → `0x03`; `enter` → `0x0D`); whether the pinned module (pseudo-version 20261106) has the same API; whether `ctrl+<letter>` needs `SetUTF8`.
- **Recommendation:** use the encoder for every `press` key (a static table sends wrong bytes once vim, less or htop switch DECCKM). Cost: cgo coupling, contained by a consumer-side interface (§8.1). **Task T6 is the verification experiment that must pass before T7** (encode `up`, `ctrl+c`, `enter`, `backspace`, `f5`, `alt+x` before and after feeding `ESC[?1h`, assert bytes). `shift+tab`, `alt+s`, `alt+enter`, `pagedown` (tape) are in the same experiment.

---

## 7. Semantics and determinism

### 7.1 Time model
- The child is real; timestamps come from the monotonic clock. **Real time only in v1**: `type`, `press`, `sleep`, `linger` and settles really wait. A `--fast` mode would change the recording (spinners get fewer frames); not in v1.
- Typed text is a stream of **units** (runes, Enters, key presses, repeats). `typing` is the delay between consecutive units within a step, not after the last. With `jitter J` each delay is drawn from `[d(1−J), d(1+J)]` using a `math/rand/v2` PCG seeded by `seed`: the *scheduled* delays repeat; recorded timestamps still vary.
- Step order: text/keys → (`run`: Enter, then settle) → `linger`.
- The clock is an interface (`Now`, `Sleep(ctx, d)`) so tests use a fake.

### 7.2 Recording: SPAN and MARK
- **`scene` and `hidden` are SPAN events** (kind 64, ancillary, inline, versioned binary). Old readers pass them through; there is no collision with user labels, so no label-prefix rule. The payload is exactly `script-scenes.md` §4.3:

  ```
  every SPAN: u8 version=1 | u8 op (1 begin, 2 end)
  END:   version, op=2, uvarint id
  BEGIN: version, op=1, uvarint id (1-based, begin order), u8 type (1 scene, 2 hidden, 3 beat reserved),
         uvarint parent_id (0 none; hidden-in-scene: the scene's id), uvarint name_len, name (hidden: empty),
         uvarint n_hints, n_hints x { uvarint tag, uvarint len, value }
  hint tags: 1 speed (uvarint milli-x) | 2 idle (uvarint µs) | 3 hold (uvarint µs) | 4 caption (reserved) | 5+ reserved
  ```
- Hint values are **resolved** (inheritance applied by the executor), so readers need no scope logic. Script-wide preamble hints are also written to `meta` keys `hint.speed`, `hint.idle`, `hint.hold` at start (crash-safe).
- **Placement:** begin before the span's first step, end after its last step and its `linger`. The executor drains pending PTY output before stamping a boundary (the writer clamps backward timestamps). The implicit end-of-script span (§7.4) is a hidden span with the next id.
- **Failure:** the executor writes the span ends before EXIT. After a kill or crash an unclosed span ends at the last event (readers must tolerate it). Unknown id, duplicate id, unknown type or `version > 1` makes that event ignorable, never a file error.
- **User `mark LABEL`** is a MARK (kind 3, UTF-8 label); labels carry no structure. `info` lists scenes and hidden spans from SPAN events.
- `hidden` output is still replayed (terminal state stays correct); export drops only its frames and time. `binary-format.md` does not yet list kind 64 (docs task T18).

### 7.3 Wait and settle semantics
- The executor is polled on the **session's single goroutine**. After each output chunk and on a timer tick it is handed a `Screen` and calls `Text()`: the visible rows (alt screen when active), each right-trimmed, joined by `\n`. No extra goroutine touches the terminal.
- A quoted pattern is `strings.Contains`; a regex is compiled with `(?m)` and used with `MatchString`; a quoted literal may contain `\n` to match across rows. Scrollback is not searched (binding access unverified): text that scrolled off times out. This is more robust than raw-byte matching because the *rendered grid* already resolved cursor movement, clear-line, alt-screen and redraws.
- **Timeout:** explicit `wait PATTERN TIMEOUT`, else the `timeout` in effect, else 15 s. **`TIMEOUT` of `0s`** tests the current screen once. On failure: error `file:line:col: scene "build": wait timed out after 15s waiting for /re/`, the final screen to stderr, child closed (SIGHUP, kill after grace), recording **finalised and kept**, exit 3. A wait that already matches returns at once. If the child exited, the final screen is tested once, then "child exited (status N) before match".
- **Settle** (`run`, after the last Enter): wait until the screen has had no output for `settle` (default 200 ms), capped by `timeout`. **Reaching the cap is not an error** (the implicit settle is best effort: a spinner or very chatty command never settles); `settle off` skips it. `wait idle [D]` is the explicit form and **does** fail at `timeout`. Quiet is a heuristic: a command silent for more than `settle` returns early; use `wait PATTERN` for slow commands. OSC 133 prompt detection needs shell integration, so it is not the default.
- The echo trap (§2): anchor on line start, or print the sentinel through a split literal (`echo DO"NE"`).

### 7.4 Exit rules and codes
- **End of script:** inside the implicit hidden span: (1) final settle (`settle`, max 2 s); (2) if the child still runs: shell → EOF (Ctrl-D, 0x04), other program → close the PTY master (SIGHUP); (3) grace 2 s, then kill. The final frame and footer are written as `session` does today. Opting out (a `keep-open` setting) is open (§11).
- **Child exits mid-script:** later `run`/`type`/`press` fail "child exited"; `sleep`/`mark`/`wait` still work. Ending with `run exit` or `press q` is normal.
- **Exit codes** (these override the scenes brief's "else child's code"): `0` completed (regardless of child status); `1` runtime error; `2` parse or validation error (nothing spawned, no file written); `3` script failure (`wait`/assertion failed, child exited when input was required); `130` cancelled. `--propagate-exit` makes the child's status the exit code.

### 7.5 What is recorded, and cancellation
OUT: raw PTY output including echo of scripted typing. IN: off by default (`--record-input` records scripted keystrokes through the live write path). MARK: `mark`. SPAN: blocks. RESIZE: the initial size only. Scripted mode forwards no stdin and does not mirror output by default. `cmd.Context()` cancellation flows into the session; the recording is finalised and the exit code is 130. The executor never blocks without a context-aware wait.

---

## 8. Architecture fit

### 8.1 Packages
```
internal/script            ast.go lex.go textblock.go parse.go validate.go diag.go   // pure Go
internal/script/keys       // "ctrl+shift+left" ⇄ (Key, Mods); NO ghostty types
internal/script/run        // lower.go: AST → Plan (scopes resolved, SPAN ops explicit)
                           // exec.go: Plan → non-blocking state machine; declares its own interfaces
internal/recording/bin/event  // + KindSpan, AppendSpan/ParseSpan
internal/recording/spans   // Scan(reader) → spans + meta hints (info and export)
internal/export/timeline   // + Options.Segments (script-scenes.md §5.2A)
internal/vt/ghostty        // + KeyEncoder adapter (cgo stays here)
internal/session           // + optional Driver hook
internal/cli               // record.go: --script, --list; export.go: spans → segments
testdata/script/*.script   // golden inputs; *.ast.golden, *.timeline.golden
```
`script`, `keys` and `run` import no cgo, no pty, no cobra. `run` declares `Screen{Text() string}`, `Keys{Encode(Chord) ([]byte, error)}` and `Clock`. **Lowering** resolves scopes once: every op already carries its effective `typing`, `jitter`, `timeout`, `settle`, `linger` and resolved hints, and `SpanBegin`/`SpanEnd` and the implicit end span are explicit ops, so the state machine has no scope logic.

### 8.2 Hook into the session
The executor is a non-blocking state machine polled by the session loop:
```go
type Driver interface { // defined in internal/session (consumer)
    Poll(now time.Duration, scr Screen) (Step, error)
}
type Step struct { Write []byte; Mark string; Span *SpanOp; Deadline time.Duration; Done bool }
```
`wait` is "keep polling and match `scr.Text()`". The session select loop was not read; adjust. `Keys` is implemented by the ghostty adapter holding the `*Terminal` and calling `SetFromTerminal` before each `Encode`.

### 8.3 CLI and export
- `shellshot record --script demo.script [-o out.shot] [--cols/--rows] [--record-input] [--list]`. `--list` parses, validates, and prints the plan with scene names and estimated duration (typing × chars + sleeps + lingers; waits as "up to timeout"); it is the lint. Explicit flags (`Changed`) > script > defaults; `-o` overrides `output`; `--cols/--rows` override `size`. `-c`/`--` with `--script` overrides `shell` (open, §11). Rename the `script` variable in `record.go`.
- Export (detail: `script-scenes.md` §5): SPAN → `timeline.Segment{From,To,Speed,IdleLimit,Hold,Skip}`; a gap is split at boundaries; hidden spans are replayed but add no frames or time; `frames` gets a forced sample at span boundaries; flag > scene hint > preamble hint > default; `--no-hints` ignores hints. `export --scene` and `replay --from` are deferred.

---

## 9. Task plan (small, atomic, independently testable)

| # | Task | Output | Tests | Depends on |
|---|---|---|---|---|
| T1 | Lexer core | tokens with line:col; keyword-directed modes (§4.4); CRLF; comments | Table tests per mode: TEXT, STRING escapes, REGEX with `\/`, `#` rules, `{`/`}`; positions | none |
| T2 | **Text-block lexing and resolution** | `TEXTBLOCK` token (opener, raw body lines); `Resolve(shape, keyword, lines) (text, error)`: closing-indent rule, strip, trim, blank handling, `lines`/`paragraphs`, `run` Enter | Table tests for every rule of §3.5 incl. `}` at other indent, `#`, quotes, tabs vs spaces, blank lines, empty block, EOF; error positions; **fuzz**: never panics, terminates, the closing line is found iff present, re-indenting the body leaves the result unchanged | T1 token types |
| T3 | Parser + canonical printer | AST (§4.5); syntax errors; permissive superset | Golden `testdata/*.script` → `*.ast.golden`; **fuzz**: no panics, stable error text, accepted input prints and re-parses to the same AST; §5 examples as goldens | T1, T2 |
| T4 | Validator + diagnostics | legality table, nesting, unique names, ranges, key names, UTF-8/control chars, reserved words, did-you-mean | One table test per error with exact message and line:col; max 10 errors; negative text-block cases | AST types only (hand-built ASTs; parallel with T3) |
| T5 | `keys` package | chord text → `(Key, Mods)` | every documented name parses; order of modifiers irrelevant | none |
| T6 | **Verify libghostty key encoder** (experiment) | bytes for the §6 key set, before/after `ESC[?1h`, on the pinned module | cgo test asserts bytes; findings written to §6 | none (needs build tree) |
| T7 | KeyEncoder adapter | `keys.Chord` → bytes via `SetFromTerminal` | cgo tests as T6 | T5, **T6 passing** |
| T8 | Lowering | `Lower(AST) → Plan`; scopes resolved; SPAN ops; implicit end span | Plan goldens: scoped `typing`/`linger` revert at `}`; hint inheritance | T3 (AST) |
| T9 | Executor: input and pacing | `run/type/press/sleep`, text-block units, `typing`, `jitter`, `linger` | fake clock → expected `[]Step` timeline golden; block = one step, one linger | T8, T5 |
| T10 | Wait and settle semantics | string/regex, `0s` assertion, `wait idle`, implicit settle (best effort), timeouts, child exited | fake screen sequences; timeout text with scene name; echo-trap cases | T9 interfaces (parallel with T9 against fakes) |
| T11 | **SPAN encoding in `bin`** | `KindSpan` 64, `AppendSpan`/`ParseSpan` per §7.2; check Writer flush on kind 64 | round trip, the hand-computed bytes of `script-scenes.md` §4.3, **fuzz**, torn tails | none |
| T12 | Span scan + `info` | `spans.Scan`, `info` lists scenes/hidden | open spans end at last event | T11 |
| T13 | Session `Driver` hook | Poll after chunks and at deadlines; writes, span events; end-of-script and grace | session tests with fake Process/Terminal | T9 interface; after binary-format session changes land |
| T14 | `timeline` segments | split gaps at boundaries; skip; hold | dedup/min-delay invariant; double-idle-cap case | none |
| T15 | `frames` forced samples at boundaries | `Options.Boundaries` | screen after a hidden span is emitted | none (reads frames code first) |
| T16 | `export` wiring | spans → segments + boundaries; flag precedence; `--no-hints` | golden timelines | T11, T12, T14, T15 |
| T17 | CLI `record --script` and `--list` | precedence merge; exit codes 0/1/2/3/130 | fake runner; end-to-end `hello.script` on `/bin/sh`, gated on libghostty | `--list`: T3, T4, T8; run: T13 |
| T18 | Docs | `docs/SCRIPT.md`, ARCHITECTURE, add SPAN to `binary-format.md`, `examples/*.script` | CI runs examples, checks screens at marks | all |

**Parallel start (no dependencies):** T1, T5, T6, T11, T14, T15. Then T2 (needs T1's token types), T4 (AST types), T7 (after T5+T6), T12 (after T11). T3 follows T1+T2. T9 and T10 can proceed against fakes once T8's Plan exists. T13 is the only change to a shared package (`session`).

**Test strategy:** golden scripts plus timeline goldens (`t=+50ms write "h"`, `t=+1.2s span-begin scene:init`, `-update` to refresh). End-to-end tests assert screen state at marks, never timestamps.

---

## 10. Alternatives considered

- **Flat VHS-like list (the v1 brief).** Rejected by the user: limiting, no grouping, no way to chapter or hide a span, settings only in a preamble.
- **YAML (or TOML/JSON) steps.** Verbose for typed text (`- run: "git commit -m \"x\""`), quoting hazards, no comments in JSON. Terminalizer shows how it reads.
- **Transcript style** (`$ cmd` to run, `> pattern` to wait, as in cram). Readable for the simple case, but no room for keys, durations, settings or blocks without a second syntax.
- **Backslash line continuation (earlier draft).** Replaced by text blocks: `\` joined lines without a space, forced escaping thinking into every long line, and gave no way to type a multi-line paste or a blank line.
- **Heredoc-style text (`<<EOF`) or a bare `"""` fence.** Needs a delimiter word or collides with content; named blocks reuse the `{ }` the format already has.
- **Embedded languages (Starlark, Lua, goja, Tengo, Risor, Yaegi, CEL/expr).** Rejected by the user, and: *complexity* (a dependency, a host API, a language to learn for a list of keystrokes); *static analysis* (no "exit 2 before spawning", no scene listing without running); *determinism* (loops, maps, time and randomness); *execution model* (a blocking `wait()` needs a goroutine and a channel handshake with the session loop on every `wait`/`sleep`, breaking "no extra goroutine touches the terminal"; CEL/expr have no statements). Library characteristics are from memory.
- **MARK pairs for scenes (`scene:NAME:start`).** Replaced by SPAN: label parsing is fragile, collides with user marks, and cannot carry resolved hints (`script-scenes.md` §4.2).

---

## 11. Decisions needing confirmation, risks, unverified claims

**Decisions needing confirmation** (my defaults; recommendation first)
1. **Command budget:** six commands (`run type press wait sleep mark`). `expect` is not a command: `wait "x" 0s` asserts. No `wait now` alias (it would add a second pattern-position keyword for little gain), no `expect not`.
2. **`require` deferred** (reserved); **kept:** `run`'s implicit settle (default 200 ms, setting `settle`, best effort at the cap), `wait idle [DURATION]`, export hints `speed`/`idle`/`hold`, and `linger`.
3. **Settings:** 14 in three classes; process settings preamble-only; export hints preamble (script-wide) or in a `scene`, not in `hidden`; behaviour settings anywhere in a block (not first-only as the scenes brief said).
4. **`linger`** follows `run`, `type`, `press`, `wait` (not `sleep`/`mark`), including the last step in a block, once per text block.
5. **Text blocks:** `lines` Enter *between* lines, `run` adds the final Enter; `paragraphs` soft-wrap with one space and blank line = two Enters; trailing whitespace trimmed; closing `}` has no comment; header disambiguated by an exact-match rule (§3.5).
6. **`type@DURATION` dropped** (use `typing` before the line); **`\` continuation dropped**.
7. **Nesting:** only `hidden` in `scene`; empty blocks are errors; scene names unique and bare.
8. **Implicit end-of-script shutdown** as a hidden span: always on, or a `keep-open` opt-out?
9. **Implicit settle hitting `timeout`** is silent (best effort) vs failing. `wait idle` fails.
10. **`shell PATH` takes no arguments** today; allow `shell PATH ARGS…` (e.g. `/bin/bash --noprofile --norc`)? Recommendation: yes.
11. **Exit code on child failure:** `0` with `--propagate-exit` opt-in, or the reverse? Name of the kept recording on failure (`FILE.failed.shot`)?
12. **CLI `-c`/`--` with `--script`:** override `shell`, or forbid? **Mirror output to stdout while scripting:** off, `--watch` to show. **`output` with non-`.shot` extension:** no in v1.
13. **AST:** `Block.Items` and `Setting.At` rather than `Block{Steps}`.

**Risks**
- Echo trap on `wait` (docs, anchoring only). Rest-of-line text surprises (`run "hi" | wc`, trailing `#` is text, `run lines {` header rule).
- Quiet-screen `run` is a heuristic; `run lines` with an interactive middle command can mis-feed it.
- Text blocks: whitespace-only differences in the closing line's indentation (tabs vs spaces) fail with the "different indentation" hint; fuzz covers it.
- SPAN touches shared policy: `timeline` segments double an idle cap at a boundary; forced `frames` samples change the frame stream; writer flush on kind 64 unverified.
- cgo key-encoder API may differ at the pinned commit. Scrollback is not matchable. Session hook T13 collides with binary-format session work. Only screens at marks are reproducible, not bytes.
- A failing run leaves a partial recording that could be published by mistake.

**Contradictions found between the two briefs and how they were resolved:** (a) `read` → `linger`; (b) quoted `run "cmd"` / string scene names / `env ident string` / `command` / `wait … timeout 120s` / `press enter 2` in the scenes grammar → v2 rest-of-line text, bare names, `env K=V`, `shell`, `wait PAT 120s`, `press enter x2`; (c) scenes: "settings first in the block" vs v2 "anywhere" → anywhere; (d) `expect` as a 7th step → `wait … 0s`; (e) `require` kept vs v2 excluded → deferred; (f) `wait idle` and `idle` were reserved/excluded in v2 → `wait idle` kept, `idle` is now a hint setting (removed from the reserved list); (g) MARK pairs (v2) vs SPAN (scenes) → SPAN; (h) auto-exit as hidden span vs v2 §7.4 exit sequence → one sequence, recorded as an implicit hidden span; (i) exit codes "else child's code" vs v2 codes → v2; (j) `type@` and `\` continuation → dropped; (k) span ends on failure written (scenes) vs unclosed start (v2) → ends written, unclosed tolerated after kill.

**Unverified claims:** encoder output bytes; pinned-version API; DECCKM sequences; `/bin/sh` honouring imported `PS1`; vim `--clean` and `~`; `env -S`; scrollback access; `git log --oneline` decorations (the regex tolerates them); embedded-language library facts; asciinema-automation, demo-magic, doitlive, Terminalizer details; Writer/frames behaviour on kind 64; MARK/SPAN availability in the final binary format.

## Final recommendation

Ship `record --script` in this order: start T1, T5, T6, T11, T14, T15 in parallel; T2→T3 and T4; T8–T10 against fakes; T7 after T6 passes; then T12, T13, T16, T17, T18. Confirm decisions 1–5 before T4, since they fix the validator rules, and decision 8 before T13. Keep the surface at six commands, fourteen settings, two block kinds and four text-block forms; the reserved-word list is the growth path.

## Sources
- Kept: VHS README (https://github.com/charmbracelet/vhs/blob/main/README.md; fetched in the v1 run); go-libghostty `key_encoder.go` on GitHub main (https://github.com/mitchellh/go-libghostty; not the pinned version).
- Repo: `build/libghostty/include/ghostty/vt/key/{encoder,event}.h` (partial); `docs/research/binary-format.md` (MARK, ancillary kinds ≥ 64); `docs/research/script-scenes.md` (SPAN §4.3, export §5, tape §8); `internal/cli/record.go`, `internal/session/session.go` (lines 1–80).
- Search-summary level only: demo-magic, doitlive, Terminalizer, asciinwriter.

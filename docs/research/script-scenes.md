# Research: scenes in the shellshot `.script` format

> **Superseded in part by [`script-format.md`](script-format.md); see that file for the final syntax.** This file stays as supporting detail: SPAN recording (§4), export semantics (§5), executor ideas (§6) and the VHS-tape reference case (§8). Where a syntax example below conflicts with `script-format.md`, the example is marked **[superseded]** or was updated. Summary of what changed:
>
> | Here (original) | Final (`script-format.md`) |
> |---|---|
> | scoped setting `read DURATION` | **`linger DURATION`**: pause this long after every step in the block so the viewer can read the screen |
> | trailing `\` line continuation | dropped; named text blocks `type\|run lines { … }`, `type\|run paragraphs { … }` |
> | `expect` / `expect not` step | not a command; `wait "x" 0s` asserts; `expect not` unsupported |
> | `require` (top level, kept) | deferred, reserved word (a hidden `command -v x && echo OK` plus `wait` stands in) |
> | `type@250ms "bug"` one-off suffix | dropped; `typing 250ms` line before the `type` (reverts at `}`) |
> | quoted `run "cmd"`, string scene names, `command`, `env ident string`, `wait … timeout 120s`, `press enter 2` | rest-of-line `run cmd`, bare names, `shell`, `env K=V`, `wait PAT 120s`, `press enter x2` |
> | settings must come first in a block | setting lines may appear anywhere in a block and apply to its `}` |
> | scene/hidden as MARK labels (v2 draft) | SPAN events (§4.3 payload unchanged) |
> | "auto-exit at end of script" | the same, recorded as an implicit trailing hidden span (`script-format.md` §7.4) |
> | task plan §10 | replaced by `script-format.md` §9 |

Status: design brief (read-only research), updated for the consolidated design. Original length about 520 lines; §8 and §9 were rewritten for the final syntax.
## 0. Evidence limits (read first)

- I had **no shell**, only file read/write and web tools. I ran nothing: no build, no tests. Claims about code come from reading source.
- **Read:** `docs/ARCHITECTURE.md`; `docs/research/script-format.md` on main (lines 1–150, v1 design without blocks; the v2 rewrite was not seen); `docs/research/binary-format.md` (lines 1–400); `docs/research/sandboxing.md` (lines 1–100); `internal/export/timeline/timeline.go` (full); `internal/export/pipeline/pipeline.go` (Export); `internal/recording/bin/event/event.go` (lines 1–140); `bin/writer.go` (lines 1–150, options only); `bin/reader.go` (lines 1–120); `internal/recording/frames/frames.go` (lines 1–80); `internal/cli/info.go` (lines 1–120).
- **NOT read:** `README.md`, `internal/cli/{record,replay,export}.go`, rest of `frames.go`/`writer.go`/`reader.go` (so whether the writer flushes on kind 64, and how `frames` treats MARK/ancillary events, are **unverified**), `internal/session`, the raster/theme/font flags of `export` (flag names in §8 are illustrative), `script-format.md` lines 151+.
- Labels: **direct** = read in a file; **inference** = my reasoning; **proposal** = my design.

---

## 1. Summary and recommendation

**A scene is a named, labelled stretch of the script and of the recording.** Inside it, steps run in order like a plain list. A scene adds four things a flat list lacks: (1) settings that apply only inside it and revert at `}`; (2) a name and time span stored in the recording, so `info` and export can see it; (3) a unit of failure reporting ("scene `install` failed at line 14"); (4) a unit of export-time treatment (speed, idle cap, hold). No conditions, loops, nesting of scenes, variables or includes.

**Tiny v1 (recommended):**
- Blocks: `scene NAME { ... }` and `hidden { ... }`. **Options are setting lines inside the block** (same shape as top-level settings), not header attributes.
- Steps: `run`, `type`, `press`, `wait`, `sleep`, `mark` (six; `expect` was folded into `wait … 0s`).
- Scene-legal settings: run-time `typing`, `jitter`, `timeout`, `settle`, `linger` (was `read`); export hints `speed`, `idle`, `hold`.
- **Lessons from the VHS tape (§8):** `run` = type + Enter + wait for a quiet screen (`settle`, default 200 ms) replaces VHS `Enter Wait`; `wait idle [DURATION]` is the explicit form; `linger DURATION` (scoped reading pause; was `read`) replaces most hand-written `Sleep`s; `require` is deferred; `press` takes modifier chords; multi-line text uses named text blocks (was `\` continuation); the shell is exited automatically (as a hidden span) at the end of the script; **theme/font/padding/pixel size/output are export options and stay out of the script** (a reserved `export` section may come later).
- `record --script FILE --list`: parse, print the plan with scene names and estimated duration (this is also the lint).
- Recording: a **new ancillary event kind 64 (`SPAN`)**, inline, small versioned binary payload. Old readers pass it through. User `mark` keeps using `MARK`.
- Export: per-scene `speed`/`idle`/`hold` through a small `timeline` extension (segments); hidden spans dropped but still replayed; explicit export flags beat scene hints. `info` lists scenes.

**Deferred (keywords reserved):** `export --scene NAME`, `caption`, `cleanup { }`, `fresh`, `replay --from`, `export { }` section, JUnit, per-scene `budget`, export-time `read`.
**Dropped:** multi-terminal scenes, per-scene sandbox profile, record-time `--only/--skip`, "required/optional" scenes.

The largest costs are not syntax: `timeline` segments and a forced `frames` sample at span boundaries (§5).

---

## 2. Scene options: three syntax shapes compared

| Shape | Example | For | Against |
|---|---|---|---|
| **A. Lines inside the block** | `scene install {`<br>`  speed 8x`<br>`  run make`<br>`}` | Same as top-level settings: **one concept**. Scales to more options; diff-friendly; no `key=value` token class | Options sit with the body (**superseded:** setting lines may appear anywhere in a block and apply from that line to `}`; the original "must come first" rule was dropped) |
| B. Header attributes | `scene install speed=8x idle=2s {` | Whole config on one line | Second lexical class; long headers; duplicates the setting syntax |
| C. Keyword clause | `scene install with speed 8x, idle 2s {` | Reads like English | New keyword and commas; still a second way to set the same thing |

**Pick A.** One-sentence rule for the docs: *"A block can start with setting lines; they apply until the closing brace."* The same rule covers `hidden { typing 1ms ... }`. Top-level settings are defaults.

| Setting | Top level | `scene` | `hidden` | Meaning |
|---|---|---|---|---|
| `typing` | yes | yes | yes | delay per typed character |
| `jitter`, `timeout` | yes | yes | yes | typing jitter; default `wait` timeout |
| `settle` | yes | yes | yes | quiet time `run` waits for after Enter (default 200ms; `off` disables) |
| `linger` (was `read`) | yes | yes | yes | pause after every step so the viewer can read the screen (default 0; see §8.4) |
| `speed`, `idle`, `hold` | yes | yes | no | export hints (a hidden span has no output) |
| `size`, `cwd`, `env`, `shell` (was `command`), `output`, `seed` | yes | **error** | **error** | process-level: "needs a new shell (see `fresh`, not available yet)" |

Names: `typing` (record-time, per character) and `speed` (export playback) are deliberately different words (proposal; align with v2 naming).

---

## 3. Decision table for the candidate ideas

Cost: S = hours, M = a day or two, L = a week+.

| # | Idea | Decision | Cost | Reason |
|---|---|---|---|---|
| 1 | Per-scene `speed`/`idle`/`hold` as export-time metadata | **Keep v1** | M | Headline use: `install` at 8x, `demo` at 1x; recording stays faithful. Needs `timeline` segments + forced sample at boundaries |
| 2a | `export --scene NAME` | Defer v1.1 | M | Needs start/end window in `frames`; design in §5 so v1 does not block it |
| 2b | Poster frame, chapter index | Defer | S–M | Needs PNG still / chapter format |
| 3a | `expect` / `expect not` | **Superseded:** `wait "x" 0s`; no `expect not` | S | It is `wait` with timeout 0; same matcher |
| 3b | Per-scene `budget` | Defer | S | `timeout` already bounds waits |
| 3c | Per-scene pass/fail on stderr | **Keep v1** | S | Free once scenes exist |
| 3d | JUnit / CI summary file | Defer | S | Easy on top of 3c |
| 3e | Lint / dry-run | **Keep v1** as `--list` | S | Static plan: no control flow |
| 4 | `fresh` (restart shell, env/cwd, size) | Defer | L | Second child in one recording. **Direct:** `export` rejects any mid-recording resize today, so per-scene size cannot export yet |
| 4b | Per-scene sandbox profile | **Drop** | L | `sandboxing.md` puts the boundary at child spawn (one trust decision). Mid-run widening is a footgun |
| 5a/b | `caption`; markdown transcript | Defer | M | Text layout / PNG stills. SPAN reserves a caption tag |
| 6 | `cleanup { }` (always runs) | Defer | M | Needs failure-path executor. v1 stand-in: trailing `hidden`, which runs on success only |
| 7 | Multi-terminal scenes | **Drop** | L | Several PTYs and streams |
| 8a | `--list` | **Keep v1** | S | Parser plus arithmetic |
| 8b | `--only`/`--skip` at record time | **Drop** | — | Scenes share one shell's state; skipping `install` breaks `demo`. Selection belongs to export (2a) |
| + | Scoped `typing`/`jitter` | **Keep v1** | S | Free from scoping |
| + | **`run` waits for a quiet screen (`settle`) and `wait idle`** (from tape) | **Keep v1** | S | One mechanism replaces VHS `Enter Wait` |
| + | **`linger` reading pause** (was `read`; from tape) | **Keep v1, flagged optional** | S | Removes most `Sleep` noise; no new grammar (§8.4) |
| + | **`require`** (from tape) | **Deferred, reserved word** (user decision; superseded) | S | A hidden check plus `wait` stands in |
| + | **Key chords in `press`** (from tape) | **Keep v1** | M (key encoder) | Alt+S, Alt+Enter, Shift+Tab, PageDown, Ctrl+D |
| + | **Auto-exit at end of script** (from tape) | **Keep v1** | S | Replaces `Hide`+`Ctrl+D` |
| + | **`\` line continuation** (from tape) | **Superseded:** named text blocks | S | `type paragraphs { … }` replaces split `Type` lines |
| + | `export { }` section in the file | Defer | M | §8.3 |
| + | Point-span "beat" for export-time reading pauses | Defer | M | §8.4 |
| + | `replay --from SCENE` | Defer | S–M | Reader has `Seek` |
| + | Scene theme/crop/zoom hints | Defer | M | More TLV tags |
| + | "Required/optional" scene | **Drop** | — | Control flow in disguise |

---

## 4. How scenes are represented in the recording

### 4.1 Facts (direct)

- Kinds: `OUT 0, IN 1, RESIZE 2, MARK 3 (UTF-8 label), EXIT 4`; **5–63 reserved critical** (`ErrUnsupported`); **≥ 64 ancillary**, "surfaced as raw events with their kind number kept; exporters ignore them" (`event.go`, `binary-format.md` §3.2).
- Brief's flush policy: flush at `min(64 KiB, 2 s, MARK/EXIT/RESIZE, close)`. Flush on kind 64 in code: **unverified**.
- `meta` chunk (ancillary key/values) is written after HEAD by `NewWriter` (`WriterOptions.Meta`). `indx` is written only on clean close. `info` counts the five kinds and lumps others as `otherEvents`.

### 4.2 Options

| Option | Crash-safe | Old readers | Collision | Cost |
|---|---|---|---|---|
| (i) `MARK` with structured text label | yes | ignore | **User `mark "scene:begin x"` collides**; ad hoc text parsing | S, fragile |
| (ii) `MARK` with TLV/JSON payload | yes | ignore | Breaks the "UTF-8 label" contract; JSON bloats; same collision | S–M |
| (iii) dedicated chunk at close (+ inline marks) | **no** alone; two representations | ignore | none | M |
| **(iv) new event kind 64 `SPAN`, binary, versioned** | **yes** | **ignore by existing spec** | none | S |

**Recommendation: (iv).** No format change (kind 64 is already ancillary), no collision with user marks, binary-safe, version byte. A scene table in `indx`/a `scne` chunk at clean close may be added later as an accelerator; inline events stay authoritative.

Script-wide export defaults go in `meta` keys `hint.speed`, `hint.idle`, `hint.hold`, written at start (crash-safe).

### 4.3 Exact `SPAN` payload (kind 64)

Integers are uvarints (the repo's `varint` package); strings are `uvarint len` + UTF-8.

```
Every SPAN:   u8 version = 1 | u8 op (1 begin, 2 end)
END:          version, op=2, uvarint id                              (3 bytes for id < 128)
BEGIN:        version, op=1,
              uvarint id          1-based, unique, in begin order
              u8      type        1 scene, 2 hidden   (3 = beat, reserved)
              uvarint parent_id   0 none; hidden-in-scene: that scene's id
              uvarint name_len, name   (hidden: empty)
              uvarint n_hints, then n_hints x { uvarint tag, uvarint len, value }
```

| Hint tag | Name | Value | Meaning |
|---|---|---|---|
| 1 | speed | uvarint milli-x (8x = 8000; 0 invalid) | divides delays in the span |
| 2 | idle | uvarint µs | cap per gap; absent = none |
| 3 | hold | uvarint µs | extra time on the span's last screen |
| 4 | caption | UTF-8 | reserved |
| 5+ | reserved | | theme, crop, zoom |

Values are **resolved** (inheritance applied by the executor), so readers need no scope logic. Hand-computed bytes (**not run**) for `scene install` with `speed 8x`, `idle 2s`, `hold 1s`, id 1:

```
01 01 01 01 00 07 69 6E 73 74 61 6C 6C 03   ver=1 begin id=1 scene parent=0 name="install" n_hints=3
01 02 C0 3E                                  speed 8000
02 03 80 89 7A                               idle 2000000 us
03 03 C0 84 3D                               hold 1000000 us          (28 bytes total)
end: 01 02 01
hidden inside install (id 2):  begin 01 01 02 02 01 00 00   end 01 02 02
```

### 4.4 Rules

- **Placement:** begin before a scene's first step, end after its last step (including a trailing `wait`). The executor should **drain pending PTY output before stamping a boundary**; the writer clamps backward timestamps (`ClampedTimestamps`) so earlier output could otherwise land after the begin (inference).
- **Unclosed span** (kill/crash): ends at the last event. On script failure the executor writes the ends before `EXIT`.
- **Validity:** scenes never nest; hidden never nests; hidden may sit in a scene. An unknown id, duplicate id, unknown `type` or `version > 1` makes that event ignorable, never a file error.
- **`info` output** (proposal):

```
scenes        3
  1 install    0:02.3 - 0:17.9   15.6s  speed 8x idle 2s hold 1s
  ...
hidden        2 spans, 4.4s
```

---

## 5. Export semantics

### 5.1 Today (direct)
`timeline.Build(frames, Options)`: per-gap `raw = next.At - this.At`, `min(raw, IdleLimit)`, `/ Speed`; last frame gets `Hold` unmodified; equal snapshots merge; steps below `MinDelay` fold into the previous kept step. One global `Options`.

### 5.2 Changes

**A. `timeline` segments (backward compatible).**

```go
type Segment struct {
    From, To  time.Duration // [From, To) on the recording clock
    Speed     float64       // 0 = inherit
    IdleLimit time.Duration // 0 = inherit
    Hold      time.Duration // extra time after the segment's last frame
    Skip      bool          // hidden: no time, no frames
}
// Options.Segments []Segment  (sorted, non-overlapping; nil = today's behaviour)
```
1. A gap is **split at segment boundaries**; each piece is capped by its segment's idle and divided by its speed; pieces summed. Otherwise a pause at the start of scene 2 would be scaled by scene 1's speed. Side effect: a gap crossing a boundary can get two idle caps (documented).
2. `Skip` pieces add zero; frames inside are dropped before dedup.
3. `Hold` is added to the last step at or before the segment end; not capped, not divided (same rule as the existing final hold). The recording's last frame still uses `Options.Hold`.
4. Dedup and min-delay folding run after, unchanged; they only move time. A hold-lengthened step is not folded (inference; add a test).
5. `timeline` stays pure; `cli/export` converts spans to segments.

**B. `frames` forced sample at span boundaries (unverified area).** After a hidden span the screen has changed but no output may follow; without a forced sample the post-hidden screen is never emitted. Add `frames.Options.Boundaries []time.Duration`: close the group and emit a frame if the screen differs.

**C. Span pre-scan** shared by `info` and export (`spans.Scan(reader)` via `bin.OpenAt`, plus `meta` hints). The pipeline signature is unchanged.

### 5.3 Behaviour
- **Precedence:** export flag > scene hint > script top-level hint (`meta`) > default; `--no-hints` ignores hints.
- **Hidden:** output still replayed (state stays correct); only frames and time are dropped.
- **`export --scene NAME` (v1.1):** replay from 0, emit only `[begin, end)`, force a frame at `begin`, apply that scene's hints, keep hidden spans skipped; unknown or repeated name lists the names.
- A long `hold` is safe: `addFrame` splits delays over 65 535 ms (**direct**). Mid-recording resize remains unsupported.

---

## 6. Executor semantics (proposal)

- **Model:** a non-blocking state machine on the session goroutine (v1 brief). Scenes add a scope stack and two events.
- **Order:** apply settings, `SPAN begin`, steps, `SPAN end`, revert.
- **`run`:** type text, press Enter, then `wait idle settle` (no OUT event for `settle`, capped by `timeout`; hitting the cap is not an error). `settle off` skips it. Then `linger`.
- **Prompt detection:** libghostty exposes OSC 133 semantic-prompt effects (**direct**, `binary-format.md` §2.3), but they require shell integration, so they cannot be the default. Possible later refinement: if a prompt mark was seen during recording, `run` waits for it; otherwise quiet screen.
- **Failure: stop at the first failure.** A `wait` timeout, failed `wait … 0s` assertion, or unexpected child exit: print `file:line: scene "install": wait "done" timed out after 15s` plus the last screen to stderr; write span ends; kill the child; finish the recording; exit with a script-failure code. Codes (proposal, align with v2): 0 ok; 2 usage/parse; 3 script step failed; else child's code (`128+sig`).
- **Assertions:** `wait "text" 0s` (was `expect "text"`) is a `wait` with timeout 0: match once against the visible screen text of the headless libghostty terminal (rows joined by `\n`). `expect not` ("absent now") is dropped: RE2 has no lookahead; revisit if asked.
- **End of script:** the executor sends Ctrl-D (EOT) as an implicit trailing `hidden` span, the way `record` already forwards EOT on non-TTY EOF (**direct**, ARCHITECTURE). No script line needed.
- **Hidden:** runs normally; failures fail the run.
- **`cleanup { }`:** deferred; a trailing `hidden` runs on success only.
- **`fresh` (deferred):** would put two process lifetimes into one recording; SPAN needs nothing; export needs resize support first.
- **Unverified:** where `record` wires scripts in, and which writer (`bin` vs JSON Lines) `record` calls today (ARCHITECTURE still says JSON Lines for `record`).

---

## 7. Grammar (scene-related productions only) **[superseded: the final EBNF is `script-format.md` §4.3; the text below is the original draft, kept for the legality reasoning]**

Lowercase keywords; one statement per line; `{` ends the header line; `}` alone on its line; `#` comments; durations carry a unit; a trailing `\` continues a line. Step productions are as in the main brief; only additions are shown.

```ebnf
script      = { NL } { require } { topSetting } { item } ;
require     = "require" string { string } NL ;                 (* top level only *)
item        = step | scene | hidden ;

scene       = "scene" name "{" NL { sceneSetting } { sceneItem } "}" NL ;
sceneItem   = step | hidden ;                                   (* scenes never nest *)
hidden      = "hidden" "{" NL { hiddenSetting } { step } "}" NL ; (* hidden never nests *)
name        = ident | string ;                                  (* unique in the script *)
ident       = ( letter | "_" ) { letter | digit | "_" | "-" } ;

topSetting    = runSetting | hintSetting | processSetting ;
sceneSetting  = runSetting | hintSetting ;
hiddenSetting = runSetting ;
runSetting  = ( "typing" duration | "jitter" percent | "timeout" duration
              | "settle" ( duration | "off" ) | "linger" duration ) NL ;
hintSetting = ( "speed" speed | "idle" duration | "hold" duration ) NL ;
processSetting = ( "size" size | "cwd" string | "env" ident string
                 | "command" string | "output" string | "seed" int ) NL ;

step        = run | type | press | wait | sleep | mark | expect ;
type        = "type" [ "@" duration ] string NL ;               (* one-off typing delay *)
press       = "press" chord [ int ] NL ;                        (* press alt+enter, press enter 2 *)
wait        = "wait" ( matcher | "idle" [ duration ] ) NL ;
expect      = "expect" [ "not" ] matcher NL ;
chord       = { modifier "+" } keyname ;  modifier = "ctrl" | "alt" | "shift" ;
speed       = number "x" ;                                      (* 8x, 0.5x; > 0 *)
reserved    = "fresh" | "cleanup" | "caption" | "export" ;      (* error: "not available yet" *)
```

Parse errors worth having: ~~setting after a step in a block~~ (superseded: legal); process setting in a block; nested `scene`/`hidden`; duplicate scene name; unclosed block (points at the opening line); reserved word; `require` after a step.

---

## 8. Reference case: VHS tape

The user's tape (`committed`, a TUI that prompts for type, scope and message) is the main test. Keystrokes matter more than shell commands. It has **23 `Sleep`s** totalling about **32 s** of fixed pauses (my count), 6 setup commands each followed by `Enter Wait`, and 9 setup/export `Set` lines.

### 8.1 Problems in the flat VHS style and what the new design does

| # | Issue in the tape | What scenes/the new design do | Still awkward |
|---|---|---|---|
| 1 | `Hide` … `Show` toggles, plus a temporary `Set TypingSpeed 1ms` … `Set TypingSpeed 50ms` to hide the typing | `hidden { typing 1ms ... }`: the override reverts at `}`; hiding is a block, not a state toggle | Hidden steps still take real time and are dropped only at export. If `hidden` is long it still delays the run |
| 2 | `Enter Wait` repeated on 6 lines | `run cmd` = type + Enter + wait quiet screen (`settle`) | "Quiet" is a heuristic: a slow command that is silent for 200 ms returns early. Use `wait "pattern"` or raise `settle`. Prompt detection needs shell integration (OSC 133) |
| 3 | `Sleep 1s Type … Sleep 1s Enter`: a pause before and after nearly every keystroke group | `linger 1s` as a scoped setting pauses after every step (`run`, `type`, `press`, `wait`) | Pauses vary (3, 3, 2, 2, 2, 1…), so one value per scene only approximates; "before Enter" and "after Enter" cannot be set separately |
| 4 | Global `Set`s mixed with mid-script `Set TypingSpeed`, no scope, no revert; VHS silently ignores late `Set` | Scoped settings revert at `}`; a setting line may appear anywhere in a block and applies from that line (process settings in a block are errors with line:col) | A one-off change mid-flow needs its own `typing` line (and a scene boundary to revert it) |
| 5 | `Type@250ms "bug"`: one-off override | `typing 250ms` on the line before `type bug`; it reverts at the closing `}` (the `@` suffix was **dropped**) | A flat script sets it back by hand (`typing 50ms`) |
| 6 | One long string split over several `Type` lines | `type paragraphs { … }` (soft-wrapped lines joined with one space) or `type lines { … }`; the `\` continuation was **dropped** | The body is raw text: `#` and quotes are literal |
| 7 | Blank-line idiom `Sleep 0.5s Enter Sleep 0.25s` / `Sleep 0.25s Enter Sleep 0.25s` | Inside `paragraphs`, a blank line is typed as two Enters, so the idiom is one block; pauses between become `linger` | The tape's 250/500 ms pauses around the Enters are lost |
| 8 | `Require committed` / `Require git` | `require` is **deferred**; a hidden `run command -v committed > /dev/null && command -v git > /dev/null && echo TOOLS-OK` plus `wait /^TOOLS-OK$/ 5s` fails the run (exit 3) when a tool is missing | Checks the guest `PATH`, after the shell starts; `--list` does not show it |
| 9 | `Output demo.gif`, `Width/Height/Padding/FontSize/Theme/LetterSpacing` at the top: these are **export** settings | Out of the script (see 8.3) | A single file is no longer the whole recipe; the `export` command line or a future `export { }` section carries it |
| 10 | Trailing `Hide` + `Ctrl+D` just to exit | Automatic: at the end of the script the executor sends EOT inside an implicit hidden span | A script that must keep the shell alive needs `settle`-free opt-out (not defined; open question) |
| 11 | Magic numbers meaning "give the viewer time to read" | `linger` names the intent; end-of-scene `hold` is **export-time** (the final `Sleep 5s` becomes `hold 5s`, so recording and CI run faster) | Numbers are still magic; `linger` is record-time so it still costs wall time |
| 12 | `Set Width 794 / Height 760` are pixels | Recording size is `size COLSxROWS`; pixels come from font size and padding at export | Cannot copy the pixel size over directly (see 8.2) |
| 13 | `cd /tmp` and `git init` run in a shared `/tmp` | Same problem. Better: `hidden { run cd $(mktemp -d) ... }` | Both the tape and the translation share state in a user-visible directory |

### 8.2 Translation A: literal (flat, no scenes except `hidden`)

Same keys, text and pauses as the tape (final syntax). Simplifications are annotated.

```
size 80x38                                      # was: Set Width 794 / Set Height 760 (see note 1)
typing 50ms                                     # was: Set TypingSpeed 50ms

hidden {                                        # was: Hide ... Show
  typing 1ms                                    # was: Set TypingSpeed 1ms; reverts at "}"
  # was: Require x2 (note 2). Unquoted run text takes no trailing comment, so annotations sit on their own line
  run command -v committed > /dev/null && command -v git > /dev/null && echo TOOLS-OK
  wait /^TOOLS-OK$/ 5s
  # was: Type ... Enter Wait. run = type + Enter + wait for a quiet screen
  run cd /tmp
  run git init
  run git commit --allow-empty --allow-empty-message --no-edit
  run touch test
  run git add test
  run clear
}

sleep 1s
type committed
sleep 1s
press enter

sleep 3s
press shift+tab
sleep 3s
press enter

sleep 2s
press pagedown
sleep 2s
press pagedown
sleep 2s
typing 250ms                                    # was: Type@250ms "bug" (note 3)
type bug
typing 50ms
sleep 2s
press enter

sleep 1s
type Prevent racing of requests
sleep 1s
press enter

sleep 1s
type Introduce a request id and a reference to latest request. Dismiss incoming responses other than from latest request.
sleep 500ms
press enter
sleep 500ms                                     # was: Sleep 0.25s + Sleep 0.25s (merged; same total)
press enter
sleep 250ms
type Remove timeouts which were used to mitigate the racing issue but are obsolete now.
sleep 500ms
press enter
sleep 500ms                                     # was: 0.25s + 0.25s
press enter
sleep 250ms
type Resolves: #1
sleep 500ms
press enter
sleep 2s

press alt+s
sleep 3s

press alt+enter
sleep 5s
                                                # was: Hide + Ctrl+D; now the implicit end-of-script shutdown (note 6)
```

Notes:
1. **`size 80x38` is a guess.** VHS sizes the window in pixels (794x760, 18 px padding, 16 px font); shellshot records a character grid and derives pixels at export (inference: about 80 columns and 38 rows at a 16 px font; cell metrics were not measured). Adjust after the first export.
2. `require` is **deferred** (reserved word). The hidden `command -v … && echo TOOLS-OK` line plus `wait /^TOOLS-OK$/` stands in: a missing tool times out and the run fails with exit 3. The regex is anchored because the echoed command line starts with the prompt. The trailing `clear` removes it from the first frame.
3. The `@` one-off suffix was dropped. A top-level behaviour setting is legal between steps, so `typing 250ms` / `typing 50ms` bracket the one `type` (VHS silently ignored a late `Set`; here it is effective).
4. The two `Type` lines of the long string are one `type` line, because the original split only wrapped the text. `#1` is literal in rest-of-line text. The `\` continuation no longer exists.
5. `Sleep 0.5s` becomes `sleep 500ms` (bare numbers are errors; `0.5s` is also valid).
6. The final `Hide` + `Ctrl+D` is automatic: the executor shuts the shell down inside an implicit hidden span. If explicit: `hidden { press ctrl+d }`.
7. `press shift+tab`, `alt+s`, `alt+enter`, `pagedown` rely on the libghostty key encoder (`script-format.md` §6, unverified). Blank lines between groups carry no meaning.

Timing difference: `run` waits for a quiet screen (≥ 200 ms of no output), so the hidden setup runs a few hundred ms longer per line than VHS's `Wait`; it is hidden anyway.

### 8.3 Where the export settings go

| VHS line | New home |
|---|---|
| `Output demo.gif` | `shellshot export demo.shot -o demo.apng`. **GIF is consciously dropped**: shellshot exports APNG (ARCHITECTURE). The script's own `output demo.shot` (top level) names the recording |
| `Set Width 794`, `Set Height 760` | `size 80x38` in the script (recording grid); final pixels come from the export font/padding. An exact pixel size would need an export option (not verified to exist) |
| `Set Padding 18`, `Set FontSize 16`, `Set Theme "Builtin Dark"`, `Set LetterSpacing 0` | export options, e.g. `shellshot export demo.shot -o demo.apng --padding 18 --font-size 16 --theme "Builtin Dark"` (**flag names illustrative; unverified**). LetterSpacing: no equivalent is known |

**Decision: no `export` block in the script v1.** Reasons: (1) the recording stays re-exportable with another theme without editing the script; (2) the v1 brief already excluded theme/font from the script to avoid two sources of truth; (3) export flags already exist. Cost: the full recipe is not in one file. Mitigation: a Makefile line or a later `export { }` section at the end of the script (reserved keyword; parsed, ignored by `record`, read by `export --script`), with precedence flag > section > default. Defer until users ask. Export-time **timing** hints (`speed`, `idle`, `hold`) do stay in the script because scenes name them.

### 8.4 Translation B: idiomatic (scenes, `linger`, `type paragraphs`, `hold`)

This is the final script. `linger` is the setting that pauses after every step so the viewer can read the screen; the commit body is one text block.

```
size 80x38
typing 50ms

hidden {                                   # tool check + fake commit; invisible in the export
  typing 1ms
  run command -v committed > /dev/null && command -v git > /dev/null && echo TOOLS-OK
  wait /^TOOLS-OK$/ 5s
  run lines {                              # six commands, one settle; the shell buffers typeahead
    cd /tmp
    git init
    git commit --allow-empty --allow-empty-message --no-edit
    touch test
    git add test
    clear
  }
}

scene launch {
  linger 1s                                # pause 1s after every step in this scene
  sleep 1s                                 # let the empty terminal be seen
  type committed
  press enter
}

scene choose-type {
  linger 2s
  press shift+tab
  press enter
  press pagedown
  press pagedown
  typing 250ms                             # was: Type@250ms; reverts at the closing brace
  type bug
  press enter
}

scene message {
  linger 1s
  type Prevent racing of requests
  press enter
  type paragraphs {                        # one step: typed, then ONE linger after the whole block
    Introduce a request id and a reference to latest request. Dismiss
    incoming responses other than from latest request.

    Remove timeouts which were used to mitigate the racing issue but are
    obsolete now.

    Resolves: #1
  }
  press enter
}

scene submit {
  linger 3s
  press alt+s
  press alt+enter
  hold 5s                                  # export-time: the final Sleep 5s costs no recording time
}
```

The `paragraphs` block types exactly what the tape typed: `Introduce … latest request.`, Enter, Enter, `Remove … obsolete now.`, Enter, Enter, `Resolves: #1` (soft-wrapped lines joined with one space; a blank line is two Enters; `#` is literal). The closing `press enter` ends the message as in the tape.

Differences from the tape (accepted): the reading pause now comes after every step in the scene, so pauses are uniform per scene. `launch` pauses 1 s after `committed` and after Enter (the tape had 1 s and 3 s); `choose-type` pauses 2 s after each key (the tape 3, 3, 2, 2, 2, 2), so about 12 s against about 15 s; `message` pauses 1 s after the title, its Enter, the whole body and the last Enter; `submit` ends with `linger 3s` plus a 5 s export `hold` instead of a real 5 s wait. Add `sleep` lines where exact timing matters. `require` is deferred, so the hidden check stands in.

**Is `linger` worth its complexity?** It is one scoped setting, about 10 lines in the executor and no grammar change; it removes 12 of 23 `sleep` lines here and names the intent. Cost: another concept for the reader, and it cannot place a pause *before* an action (use `sleep`). Recommendation: **keep in v1, mark optional** (it can slip without changing anything else). Two refinements are deferred: (a) `linger` as an **export-time** hold by emitting a zero-length "beat" span (type 3, reserved) after each action, so recording and CI do not wait; (b) per-step override. Rejected alternative: a general `pace` concept with before/after and per-kind values.

### 8.5 VHS features the tape needs and how the 6 commands (`run`, `type`, `press`, `wait`, `sleep`, `mark`) cover them

| VHS feature | Covered by the 6 commands? | Final design |
|---|---|---|
| `Output demo.gif` | No (export concern) | `output` names the recording; GIF **dropped**; APNG via `export` |
| `Require` | No | **Deferred** (reserved word); hidden `command -v` check plus `wait` |
| `Hide`/`Show` | No (a block, not a command) | `hidden { }` |
| `Wait` after Enter | Partly: `wait` needs a pattern | `run` waits for a quiet screen; `wait idle [DURATION]` explicit; pattern waits for slow commands. Bare `wait` remains a parse error |
| `Type`, `Type@250ms` | `type` | `typing` line for the one-off; the `@` suffix was dropped |
| `Type` of a long or multi-line string | `type` | `type paragraphs { }` / `type lines { }` |
| `Sleep 0.5s` | `sleep` | accepts `0.5s` and `500ms`; `linger` for reading pauses |
| Enter | `press enter` | |
| Shift+Tab, Alt+S, Alt+Enter, PageDown, Ctrl+D | `press` with chords | needs the libghostty key encoder; the largest piece of work in the tape's needs |
| Window/theme/padding/font/letter-spacing | No | export flags (8.3) |
| `Set TypingSpeed` | Setting, not a command | `typing` scoped |
| Hide + Ctrl+D to exit | `press ctrl+d` | implicit end-of-script shutdown |

Verdict: the 6 commands cover the interaction (type, keys, sleep, wait); `wait … 0s` covers assertions. The tape's remaining needs are small additions: `hidden`/`scene` blocks, `wait idle` and the implicit settle, and text blocks. `require` and the `@` suffix are not needed.

---

## 9. Five example scripts

All use the final syntax of `script-format.md` (the originals used quoted `run "cmd"`, string scene names, `expect` and `wait … timeout`). See also the larger examples in `script-format.md` §5.

### 9.1 Hello world (one scene)
```
scene hello {
  run echo hello
  wait "hello" 0s
}
```

### 9.2 Docs walkthrough (3 scenes, per-scene speed)
```
size 100x28
typing 40ms
idle 1500ms

scene install {
  speed 8x
  idle 500ms
  run brew install mytool
  wait "Summary" 120s
  hold 500ms
}

scene first-run {
  run mytool init demo
  wait "Created demo/"
  hold 1s
}

scene show-output {
  typing 80ms
  speed 1x
  run cat demo/report.txt
  hold 3s
}
```
`wait "Summary" 120s`: a per-step timeout is a trailing duration (the original draft wrote `timeout 120s`).

### 9.3 CI smoke test
```
timeout 20s

scene build {
  run make -j4
  wait "build ok"
}

scene test {
  run make test
  wait "PASS"
}

scene cli-help {
  run ./bin/mytool --help
  wait "Usage:" 0s
  wait "--version" 0s
}
```
Failure prints `smoke.script:5: scene "build": wait "build ok" timed out after 20s` and exits 3. `--list` is the lint. `expect not "warning:"` (original) is unsupported; RE2 has no lookahead to express absence.

### 9.4 Hidden setup and a success-only trailing hidden
```
size 90x24

hidden {
  typing 1ms
  run lines {
    rm -rf /tmp/demo && mkdir /tmp/demo && cd /tmp/demo
    clear
  }
  wait "$" 0s
}

scene create {
  hold 1s
  run git init
  run echo hi > a.txt
  run git add a.txt
}

scene commit {
  speed 2x
  run git commit -m first
  wait "1 file changed"
}

# runs only if everything above passed
hidden {
  run rm -rf /tmp/demo
}
```
If `commit` fails, `/tmp/demo` is left behind until `cleanup { }` exists. A `hidden` inside a scene is legal (encoded with `parent_id`).

### 9.5 Fresh-state scene: not in v1
Deferred (cost L, needs mid-recording resize in export). The tape in §8 is the better fifth example. Reserved shape: `scene second-shell { fresh  run echo $PWD }`.

---

## 10. Ordered task plan for v1 **[superseded by `script-format.md` §9, which also adds text-block lexing, the key-encoder verification and parallelism]**

1. **Event kind 64 `SPAN`**: constant, `AppendSpan`/`ParseSpan` per §4.3, round-trip and fuzz tests; check `Writer` flush on it.
2. **Span scan** `spans.Scan(reader)`: spans plus `meta` hints; open spans end at the last event; tests with torn tails.
3. **`info`**: print scenes/hidden lines.
4. **Parser**: blocks, scope stack, legality table, text blocks (was `\` continuation), `press` chords, `wait idle`, errors, reserved words. No execution.
5. **`record --script FILE --list`**: plan and estimated duration (typing chars × delay + sleeps + reads; waits shown as "up to timeout").
6. **Key chord encoder** for `press` (libghostty `KeyEncoder`; the largest independent piece).
7. **Executor**: scopes, SPAN begin/end (drain first), `run` settle, `wait idle`, `linger`, failure handling, stderr summary, exit code, implicit end-of-script exit; top-level hints into `meta`.
8. **`timeline` segments** (§5.2A) with tests (dedup/fold invariant).
9. **`frames` boundaries** (§5.2B).
10. **`export` wiring**: spans → segments and boundaries; flag precedence; `--no-hints`.
11. ~~`expect` / `expect not`~~ (superseded: `wait … 0s`).

Tasks 1–3 do not depend on the parser. `linger` (task 7) can slip. **[superseded task plan: see `script-format.md` §9]**

**Left for later:** `fresh`, `cleanup`, `caption`, theme/crop/zoom hints, `budget`, `export --scene`, `replay --from`, `export { }` section, export-time `read` (beat span), poster frames, transcript, `indx` scene table, multi-terminal, per-scene sandbox.

**Extension points that do not break the grammar:** new setting names (legality by table); new hint tags (unknown tags skipped); new SPAN `type` and `version` (ignored by old readers); reserved block keywords.

---

## 11. Risks, open questions, decisions for the user

**[Decisions 3, 4, 5, 9 and 10 were answered: see `script-format.md` §11. `read` is `linger`; `require` is deferred; `type@` and `expect` were dropped.]**

**Decisions**
1. Scene options as inside-block lines (recommended) vs header attributes.
2. Export flags beat scene hints (recommended) vs reverse.
3. `run` waits for a quiet screen by default (`settle 200ms`) vs requires explicit `wait`. Recommended: yes.
4. `linger` (was `read`) in v1 (recommended, optional) or defer; record-time now, export-time later.
5. ~~`require` in v1~~ (answered: deferred, reserved).
6. No `export` block in v1; theme/font/padding/pixels via `export` flags.
7. GIF dropped; APNG only.
8. Auto-exit at end of script: always, or opt out (open: how, e.g. a `keep-open` setting)?
9. ~~`type@dur` suffix~~ (answered: dropped; use a `typing` line).
10. Exit-code numbers; `FILE.failed.shot` naming for failed runs; `expect` in v1 or v1.1.
11. Record-time `--only/--skip` dropped in favour of export-time `--scene`.

**Risks**
- `timeline` changes touch shared policy; the split-at-boundary rule can double an idle cap at a crossing.
- Forced `frames` samples change the frame stream when spans exist.
- Quiet-screen `run` is a heuristic (slow silent commands, spinners that keep emitting output never settle until `timeout`).
- Hidden spans can mask state: the export may show a screen the visible commands cannot reach.
- Scenes look independent but share shell state.
- A failing run leaves a partial recording that could be published by mistake.
- Pixel-accurate reproduction of the VHS output (794x760) is not guaranteed; `size 80x38` is an estimate.

**Not verified**
- The v2 script format; `script-format.md` lines 151+.
- CLI record/replay/export code; how `frames` handles MARK/ancillary events; writer flush on kind 64; whether `Writer.WriteEvent` accepts kinds ≥ 64 unchanged.
- Export flag names and whether a pixel-size option exists; whether LetterSpacing can be reproduced.
- Hand-computed varint bytes and the tape's Sleep count/total (I counted by eye; 23 sleeps, about 32 s).
- `README.md` was not read.

---

## Sources
- Kept (local, direct): `docs/ARCHITECTURE.md`; `docs/research/binary-format.md`; `docs/research/script-format.md` (main, v1); `docs/research/sandboxing.md`; `internal/export/timeline/timeline.go`; `internal/export/pipeline/pipeline.go`; `internal/recording/bin/event/event.go`, `writer.go`, `reader.go`; `internal/recording/frames/frames.go`; `internal/cli/info.go`; and the VHS tape supplied by the user (§8).
- Deprioritised: web sources not used; none needed for a repo-internal design. VHS behaviour (`Wait` defaults, late-`Set` ignored) is as quoted in the main v1 brief, not re-fetched.

## Next steps
- Read `writer.go` 150+, the rest of `frames.go`, and `cli/{record,export}.go`; confirm §4.1 and §5.2 unknowns and the export flag names for §8.3.
- Reconcile naming and exit codes with the v2 `script-format.md`.
- Round-trip the §4.3 bytes through a real `ParseSpan`; run the translated tape against `committed` to check the 80x38 estimate.


# Research: compressed binary recording format for shellshot (`.shot` v3 proposal)

Status: design brief. Date: this run.

**Read this first: evidence limits of this run.**
- This run had **no shell and no code-execution tool**. I could not run `go list`, compile anything, or run the prototype benchmark.
- **Every size and speed number below is an ESTIMATE from reasoning, not a measurement.** Appendix A contains an untested prototype program (never compiled or run) so the numbers can be replaced with measurements in about 10 minutes.
- The go-libghostty module cache was not found at `~/go/pkg/mod/go.mitchellh.com/...`. The binding was read from GitHub `main`, not the pinned `v0.0.0-20261106041537-e247028bdf51`. The tool returned only partial extractions of `terminal.go` and `snapshot.go`.
- The C headers in `build/libghostty/include/ghostty/vt/` were read directly. I read `snapshot.h` lines 1–600, `terminal.h` lines 1–180 and `formatter.h` lines 1–230. I did not read `render.h`, `modes.h`, `grid_ref.h` or `kitty_graphics.h` in this run; facts about them come from `docs/research/apng-export.md` §2, which has the same caveat.
- The repo files I read: `docs/ARCHITECTURE.md`, `docs/research/apng-export.md`, `internal/snapshot/snapshot.go`, `internal/session/session.go` (lines 1–200 of 236), `internal/recording/writer.go` (lines 1–~80), and `internal/vt/ghostty/snapshot.go`.
- I did not read `style.go`, the session `pump`, the Reader, or the other `internal/recording` files.
- There is no `internal/recording/recording.go` in this checkout; the package is split across files.

---

## 1. Summary and recommendation

**Recommendation: a hybrid file with the raw PTY event log as the single source of truth.**

Container and layout:
- **Container:** a PNG-style chunk file. Each chunk is length, 4-byte type, payload and CRC32C. Case bits in the type mark chunks as critical or ancillary.
- **Event log:** raw PTY output, optional input, resize and marker events. Timestamps are varint deltas in a header-declared tick (default 100 µs, monotonic clock). Events are grouped into independent blocks with a columnar layout, compressed with zstd.
- **Derived data (ancillary, droppable):** sparse GRID keyframes, written only when the screen changed since the last keyframe, and an INDX footer written on clean close.

Why this model:
- **Idle time:** zero bytes. The file only grows when something is written to the PTY. The time gap sits in the next event's delta, which costs about 3 bytes for a 5-minute gap.
- **Fidelity:** the log is exact. This includes sub-sample detail, scrollback, OSC titles and palette changes, OSC 8 hyperlinks, kitty graphics, cursor style, bells, semantic prompts, mode changes and resizes. All of these are only derivable by replay or are lost in a snapshot-only design.
- **Keyframes:** the GRID keyframes (cell grid with style table, palette indices preserved) add three things:
  - fast preview and seek without a terminal
  - insurance against future libghostty drift (the file still shows what was seen at record time)
  - a pure-Go path for simple tools
- **Packaging:** the codec is pure Go. Only the recorder adapter and the replay driver (via the existing `vt.Factory` interface) touch libghostty.

**Ranked alternatives**

| Rank | Model | Verdict |
|---|---|---|
| 1 | **Hybrid** (raw log + sparse GRID keyframes + footer index) | Recommended. |
| 2 | Raw event log only (asciicast-style, binary and compressed) | Smallest and simplest. Slower seek (replay from 0), no insurance against libghostty drift. A fine first milestone: GRID and INDX chunks are purely additive, so nothing is lost by shipping it first. |
| 3 | Snapshot/keyframe + cell-grid deltas only (today's model, binary) | Pure-Go export, but lossy forever (§3). Not recommended. |
| 4 | Wrap libghostty's native `ghostty_snapshot_encode` as keyframes | Header says "work in progress … no binary-compatibility guarantee" (`snapshot.h` lines 1–140 region). Usable only as a version-stamped optimisation. |

**Decisions the user must make** (details in §10):
1. Record input events by default? Recommendation: **off**.
2. Extension: keep `.shot` with magic-sniffing, or use a new one?
3. Is a `github.com/klauspost/compress` dependency acceptable? Fallback: stdlib `compress/flate` with a preset dictionary.
4. Keyframe default: every 30 s, only if changed (roughly doubles the size of tiny sessions; can be disabled).
5. Do we promise resizable re-export, given that cursor-addressed TUIs break when replayed at a different size (§6)?

---

## 2. A. What to persist

### 2.1 Current design and what it loses (direct evidence)

`internal/session/session.go` shows the current model:
- A reader goroutine forwards PTY chunks.
- The main goroutine does `term.Write(chunk)` and sets `s.dirty = true`.
- On each ticker tick, `sample()` runs `term.Snapshot()`. It emits a `Frame` only if `!snap.Equal(s.last)` (doc comment lines 25–37, `sample` ~line 175).
- The raw bytes are discarded.

`snapshot.Snapshot` (`internal/snapshot/snapshot.go`) holds `Lines` (right-trimmed text), `Spans` (rune-indexed SGR strings), `Widths` (sparse), a `Cursor{X,Y,Visible}` and a `Title`.

Frame time is stored in milliseconds (`f.At.Milliseconds()` in `recording/writer.go`).

### 2.2 Fidelity matrix

Legend: ✔ exact, ◐ partial or derived, ✘ lost.

| Feature | (ii) snapshot+delta only (today) | (i) raw event log (replay at export) | (iii) hybrid |
|---|---|---|---|
| Intra-sample detail (spinners, flashes between ticks) | ✘ ticker-quantised | ✔ | ✔ |
| Timing precision | ◐ ms, tick-sampled | ✔ tick-resolution (PTY read granularity) | ✔ |
| Scrollback | ✘ (visible rows only) | ✔ via replay | ✔ |
| Mode state (DECCKM, bracketed paste, mouse, alt screen) | ✘ | ✔ | ✔ (+ in keyframe) |
| Kitty graphics | ✘ | ✔ (APC bytes are in the stream) | ✔ (pixels only via replay) |
| Sixel | ✘ | ✔ bytes kept. *Unverified:* whether libghostty-vt renders sixel; the header listings I read mention only Kitty. | same |
| OSC 8 hyperlinks | ✘ | ✔ | ✔ (+ link table in GRID) |
| OSC 0/2 title *history* | ◐ last title per frame | ✔ | ✔ |
| OSC 4/10/11/104 palette changes | ✘ (resolved at record time only) | ✔ | ✔ |
| Underline colour and style | ◐ whatever `Style.SGR()` encodes | ✔ | ✔ |
| Cursor shape / blink | ✘ (only X,Y,Visible) | ✔ (DECSCUSR in stream) | ✔ |
| Bell | ✘ | ✔ (0x07 in stream) | ✔ |
| OSC 133 semantic prompts | ✘ | ✔ | ✔ |
| Resize mid-session | ✘ (fixed size) | ✔ with RESIZE events | ✔ |
| Wide/grapheme data | ◐ rune-indexed `Widths`; cell columns reconstructed | ✔ | ✔ (GRID stores per-cell widths) |
| Selection | n/a (the real terminal owns it; the headless term has none) | n/a | n/a |
| Export without libghostty/cgo | ✔ | ✘ | ◐ (GRID keyframes only) |
| Survives a libghostty change | ✔ (frames are data) | ◐ replay may differ (§2.4) | ✔ keyframes + ◐ replay |
| Re-theme from palette indices | ✔ for what Spans keep (Color kinds palette/RGB) | ✔ | ✔ |
| Reflow / different cols×rows | ✘ | ◐ (§6) | ◐ |

Source interpretation: the matrix rows for the snapshot model follow directly from the `Snapshot` struct fields.

### 2.3 What libghostty-vt exposes that we could capture additionally

Verified directly in headers read this run:

- **Effects table** (`terminal.h` lines 71–120). The terminal can call back for these events:
  - BEL
  - title change (OSC 0/2)
  - pwd change (OSC 7/9/1337)
  - clipboard write/read (OSC 52 and others)
  - desktop notification (OSC 9/777)
  - progress (OSC 9;4)
  - **semantic prompt (OSC 133)**
  - synchronized-output render hold (mode 2026)
  - unknown OSC/APC
  - reset (RIS)
  - XTWINOPS resize request
  - `WRITE_PTY`, which carries replies to queries (DA, DSR, mode reports).
- **Query replies (replay hazard).** Replay must not forward `WRITE_PTY` replies anywhere.
  - Rationale: the program already received live replies during recording, and the consequences are already in the recorded output.
  - Researcher inference: an exporter configures no WRITE_PTY sink, so replays are side-effect free.
- **Formatter** (`formatter.h` lines 1–230). `ghostty_formatter_*` can emit PLAIN, VT or HTML for the *active screen*.
  - VT output with `GHOSTTY_FORMATTER_EXTRAS_ALL` (0xFFF) restores:
    - palette (OSC 4)
    - modes that differ from defaults
    - scrolling region
    - tab stops
    - pwd (OSC 7)
    - keyboard modes
    - cursor position
    - cursor style (SGR)
    - cursor hyperlink
    - protection
    - kitty keyboard flags
    - charsets
  - It takes a selection. It is documented to "recreate the terminal as closely as possible".
  - Gaps, by omission from that list: scrollback beyond the active screen, kitty images, title, and default fg/bg/cursor colours (OSC 10/11/12). I did not verify these gaps against the implementation.
  - This is a version-independent bootstrap: the VT bytes can be treated as a synthetic OUT event at t=0 of a trimmed recording.
- **Native snapshot** (`snapshot.h`):
  - ~~Magic~~ envelope is `"GHOSTSNP"` + u16 version, LE, records of tag u16 / len u32 / CRC32C u32 / payload.
  - It includes terminal-wide state, screens, pages (active, then history/scrollback newest to oldest), **Kitty image storage and pixels**, and the **VT parser CONTINUATION** (unfinished escape sequence).
  - It is explicitly "version 1 is a work in progress and does not yet carry a binary-compatibility guarantee".
  - Encoding needs continuation tracking enabled before the input if the parser is mid-sequence. The binding exposes `WithContinuationMaxBytes(limit uint)` as a terminal option.
  - Unknown record tags are skipped by decoders and CRC-checked. This is a good design to copy, and I do copy it for chunks (§4).
  - Researcher inference: a native snapshot is a precise seek/resume point but is tied to libghostty versions. It is a reserved optional keyframe kind, not v1.
- **Binding facts** (go-libghostty `main`, partial extraction):
  - `Terminal.Resize(cols, rows uint16, cellWidthPx, cellHeightPx uint32)`. Cell pixel size matters because Kitty placement and size reports depend on it, so it goes in HEAD and in RESIZE events.
  - `ScrollViewport*`, `Compress`, `KittyGraphics()`.
  - Options `WithBell`, `WithTitleChanged`, `WithPwdChanged`, `WithContinuationMaxBytes`.
  - Setters including `SetTitleReport`, `SetChecksumReport`.
  - The code and comments call `Terminal.Mode`, `SetMode`, `Pwd`, but the signatures were not in the extract.
- **Render state** (from `docs/research/apng-export.md` §2.1–2.2, not re-read):
  - `RenderState.Colors()` gives bg/fg/cursor/`CursorHasValue` and a 256-entry palette.
  - `CURSOR_VISUAL_STYLE`, `CURSOR_VISIBLE`, `CURSOR_BLINKING`.
  - Per-row dirty flags.
  - Row cells with `Graphemes()`, `StyleInto`, `Raw()`, `Selected()`.
  - An image iterator.
- **Not found or not verified:**
  - Hyperlink URI per cell. `GHOSTTY_FORMATTER_EXTRA_HYPERLINK` implies hyperlink state exists, and `grid_ref.h` is probably the route. *Unverified.*
  - A palette-index-preserving style getter in the binding. The existing `snapshot.Style` already has `ColorPalette`, so the repo's adapter extracts it somehow. I did not read `internal/vt/ghostty/style.go`.

### 2.4 Determinism and libghostty drift (researcher inference, not verified)

- Replaying the same bytes through a different libghostty commit may give a different screen, for example when width tables, parser bug fixes or newly supported sequences change.
- Mitigations:
  1. **Pin and record:** HEAD stores the ghostty commit (`66af4c74…` today), the build options, the scrollback limit, the kitty storage limit, and the initial cell pixel size.
  2. **Insurance via GRID keyframes:** what the user saw at record time is preserved as data, and the exporter can prefer GRID when replay disagrees.
  3. **Drift test in CI:** golden `.shot` files with checked-in expected grids. A submodule bump that changes a replay then fails loudly. This test is cheap.
- Header and test strategy cost almost nothing; no mitigation can make a future libghostty reproduce the past byte-for-byte.

### 2.5 Input events

Direct fact: OUT already includes echoed typing.
- Passwords typed with echo off appear only in the IN stream.
- IN is only useful for features like a keystroke overlay.
- Default must be **off** (`--record-input`).
- When on, the file flags it in HEAD (`flags` bit) so tools can warn on sharing.

---

## 3. B. Container design

### 3.1 Prior art, compared concretely

| Format | Structure | Take / leave |
|---|---|---|
| **asciicast v3** (docs.asciinema.org, fetched via search summary) | NDJSON; header `{"version":3,"term":{cols,rows},…}`; events `[interval, code, data]`, interval is seconds since the previous event; codes `o i m r x`; `#` comments; unknown codes/attrs must be ignored; `.cast` | **Take** the event model (o/i/m/r/x), relative intervals, forward-compat rule. **Leave** text (JSON escapes of control bytes bloat output), float time, no CRC, no seek, no compression. |
| **ttyrec** (search result) | No file header; records of `sec u32, usec u32, len u32` LE + payload | Simplest possible log and crash-safe by construction. No resize, no metadata, 12 B/record, no compression. |
| **ttyrec2 / termrec** | termrec can write several formats including asciicast (Debian man page). I did not verify any ttyrec2 layout. | Not evidence. |
| **script/scriptreplay** | Separate timing file (`delay nbytes` text lines) beside the raw typescript | Confirms "raw bytes + timing columns" is the proven minimal model. Not verified by fetch; from memory. |
| **PNG / APNG** | 8-byte signature (`89 'P' 'N' 'G' 0D 0A 1A 0A`), chunks `len, type, data, CRC`; type-case bits: ancillary / private / reserved / safe-to-copy | **Take** wholesale: critical vs ancillary, CRC per chunk, signature that detects text-mode corruption. APNG shows additional chunk types can ride in a stable container. |
| **pcapng** | Blocks with type, total-length at *both* ends, section header with options | Trailing length enables backward scanning, but I use a footer pointer instead. Options-as-TLV for metadata is the right idea. |
| **MP4 / Matroska** | MP4 `moov` at end breaks streaming; fragmented MP4 (`moof`) and Matroska Clusters + Cues solve it | Our blocks are clusters; INDX is Cues; "index optional, scan works" is the fragmented-MP4 lesson. From memory, not fetched. |
| **Parquet / Arrow IPC** | Columnar row groups + footer; footer is mandatory | **Take** the columnar-within-block idea. **Leave** the mandatory footer: it kills crash safety. |
| **SQLite** | Page-based DB with journal/WAL | **Reject**: pure-Go builds are large (modernc) or cgo; WAL isn't a portable single-file append log; opaque to `hexdump`/fuzzing; overkill for append-only data. Crash safety is excellent, but chunks + CRC give us that for free. |
| **protobuf / FlatBuffers / Cap'n Proto** | Schema'd, tag-based forward compat | Needs generated code and a dependency for a 20-field header. Rejected. |
| **CBOR / MessagePack** | Self-describing | Would work for metadata (needs a dependency, e.g. `fxamacker/cbor`, unevaluated). I propose a tiny tagged-varint TLV in stdlib instead. |
| **Warp / Ghostty / Kitty / WezTerm recording** | — | **Not researched.** I know of no mature in-terminal recording format in these projects. Unverified. |

### 3.2 Proposed layout

All fixed-width integers are **little-endian** (the same as ttyrec and libghostty's snapshot). All other integers are **LEB128 uvarint** (Go `binary.AppendUvarint`); signed values use zigzag.

```
file := magic chunk* [footer-trailer]

magic (8 B)   89 53 48 4F 54 0D 0A 1A        "\x89SHOT\r\n\x1a"
              (high bit catches 7-bit stripping, CR LF catches newline translation,
               ^Z stops DOS `type`; same trick as PNG)

chunk         +-----------+-----------+-------------------+-----------+
              | len u32   | type [4]B | payload (len B)   | crc u32   |
              +-----------+-----------+-------------------+-----------+
              crc = CRC-32C (Castagnoli) over type || payload
              len <= 64 MiB hard limit; readers apply a lower configurable cap (default 16 MiB)
              type byte 0, bit 5 (0x20): 0 = critical (reader must understand or fail),
                                         1 = ancillary (skip if unknown)

footer-trailer (8 B, optional, after a clean close):
              footer_chunk_total_len u32 | "SHTE"   (footer_chunk_total_len = 12 + payload len)
```

Chunk types:

| Type | Class | Count | Content |
|---|---|---|---|
| `HEAD` | critical | exactly 1, first | Tagged fields; see below. |
| `DICT` | critical | 0..n | Compression dictionary: `dict_id uvarint`, `codec uvarint`, bytes. Must precede blocks that use it. |
| `EVNT` | critical | many | Event block (below). |
| `them` | ancillary | 0..1 after HEAD | Theme: default fg, bg, cursor (RGB + has-value), selection colours, 256×RGB palette (768 B), initial cursor style. |
| `meta` | ancillary | 0..n | Free-form user key/value strings (`--meta k=v`). |
| `GRID` | ancillary (named `grid` on disk) | 0..n | Derived keyframe (below). |
| `indx` | ancillary | 0..1, last | Footer index (below). |
| `blob` | ancillary | 0..n | Reserved: attachments such as kitty image pixels extracted for tools. Not v1. |

Chunk types in actual bytes: `HEAD DICT EVNT them meta grid indx blob`. (Case bits encode criticality, as in PNG.)

**HEAD payload: tagged fields** `[tag uvarint][len uvarint][value]...`. Readers skip unknown tags. Features a reader must understand are listed in `required_features`, similar to ext4 incompat flags.

| Tag | Field |
|---|---|
| 1 | `format_minor` uvarint, `required_features` uvarint bitmask (bit0: block dictionaries, bit1: reserved) |
| 2 | `cols`, `rows` (uvarint, uvarint) |
| 3 | `tick_ns` uvarint (default 100 000 = 100 µs) |
| 4 | `started_unix_ns` svarint, `tz_offset_s` svarint |
| 5 | `cell_px_w`, `cell_px_h` uvarint (initial; 0 = unknown) |
| 6 | engine: name ("libghostty-vt"), commit (40-char string), build flags string |
| 7 | engine config: scrollback limit, kitty storage limit |
| 8 | `TERM` env string |
| 9 | argv (repeated tag, one string each) |
| 10 | cwd |
| 11 | env allowlist `K=V` (repeated; default: `TERM`, `COLORTERM`, `LANG`, `SHELL`) |
| 12 | initial title |
| 13 | font hints: family string, size pt (float32 bits), cell aspect hint |
| 14 | `flags` uvarint: bit0 = input recorded, bit1 = clean close, bit2 = coalesced (writer merged adjacent OUT within `coalesce_ticks`) |
| 15 | hostname (opt-in only) |
| 16 | session UUID (16 B random) |
| 17 | recorder name/version |

Bit1 of `flags` ("clean close") can't be set in HEAD during streaming unless the file is rewritten. Instead, clean close is signalled by the presence of `indx` + trailer. Drop the flag from HEAD; keep bit0 and bit2.

**EVNT payload** (header uncompressed so tools can scan time ranges without decompressing):

```
codec        uvarint   0 = none, 1 = deflate (stdlib), 2 = zstd
dict_id      uvarint   0 = none
start_tick   uvarint   absolute ticks since recording start
end_tick     uvarint   absolute tick of the last event in the block
n_events     uvarint
raw_len      uvarint   length of the uncompressed body (decoder bound, DoS cap)
body         compressed(  columns ) :
   dt[n]     uvarint   ticks since previous event (first is relative to start_tick, so 0)
   kind[n]   byte
   plen[n]   uvarint   payload length (0 if none)
   payload   concatenated payload bytes
```

Event kinds:

| Kind | Name | Payload |
|---|---|---|
| 0 | OUT | raw PTY output bytes |
| 1 | IN | raw input bytes (only when enabled) |
| 2 | RESIZE | uvarint cols, rows, cell_px_w, cell_px_h |
| 3 | MARK | UTF-8 label |
| 4 | EXIT | zigzag exit status (wait status) |
| 5–63 | reserved, critical | reader fails with `ErrUnsupported` if encountered |
| ≥ 64 | ancillary | reader surfaces them as `Kind(n)` raw events; exporters ignore |

Rationale for columnar-within-block: the payload bytes form one contiguous text stream for the compressor, and the dt/kind/plen columns are tiny and highly repetitive. *Estimate, unmeasured:* columnar saves 5–15 % over interleaved. Appendix A includes a way to test this; if it saves under 5 % switch to interleaved for simplicity.

**GRID keyframe payload** (columnar cell grid, see §6.2) begins with:

`at_tick uvarint | block_seq uvarint | event_index uvarint | kind uvarint (1 = cell grid v1, 2 = VT dump, 3 = reserved native ghostty snapshot) | codec | dict_id | raw_len | body`

It states "state after event `event_index` of EVNT block number `block_seq`", so a replayer can verify its own replay against it.

**INDX payload** (footer): the following fields.
- `n_blocks`
- per block: `offset` (delta from the previous block), `start_tick`, `end_tick`, `n_events` (delta-coded varints)
- `n_keyframes`, then per keyframe: `offset`, `at_tick`, `kind`
- totals: `duration_ticks`, `n_events`, `out_bytes`, `in_bytes`, `exit_status`

For 300 blocks this is about 2–3 KB raw, which compresses to 1–2 KB. *Estimate.*

### 3.3 Crash safety, finalisation, appendability

- **Write-ahead blocks:** each chunk is written with one `Write` call as `len|type|payload|crc`. A kill leaves at most one torn tail chunk.
- **Recovery:** the reader scans chunk by chunk. A short read, an absurd length, or a CRC mismatch means "end of valid data". Everything before it is returned. This is the same approach as PNG, ttyrec and snapshot.h's CRC records.
  - `ErrTruncated` is reported as a non-fatal condition alongside a valid prefix.
- **Footer:** `indx` plus trailer is written only on clean close.
  - If the trailer is missing, or the footer chunk fails its CRC, the reader falls back to a scan. The scan walks the 12-byte chunk headers only; it never decompresses.
  - Scan cost: 300 blocks × one small read each. That is trivial, so the footer is an optimisation, not a necessity. It can ship after the base format.
- **Appendable:** to append, read the trailer, truncate the file at the start of `indx`, continue writing blocks, and write a new footer on close. A file without a trailer is appended directly after the last valid chunk (and the torn tail is truncated away).
- **Durability:** `Write` survives process kill. Surviving OS crash/power loss needs `fsync`; default every 5 s or on close (configurable).
- **Loss window on kill:** the unflushed in-memory block, bounded by the flush policy (below), default 2 s.
- **Idle:** no event means no block is flushed. The flush timer only fires if there is pending data.

Flush policy (writer): flush a block at `min(64 KiB raw, 2 s since the oldest pending event, MARK/EXIT/RESIZE event, close)`.

### 3.4 Versioning policy

- The magic is fixed forever; `format_major` is in the HEAD chunk type itself (bump = new magic or new `HEAD` type if a break is ever needed).
- `format_minor` and `required_features`: a reader that sees an unknown required feature must refuse with `ErrUnsupported`.
- New info goes into new ancillary chunks or new HEAD tags; old readers skip them.
- Never reuse a tag, kind or chunk type.

---

## 4. C. Compression

### 4.1 Options

| Codec | Fit | Notes |
|---|---|---|
| **zstd** (`github.com/klauspost/compress/zstd`, pure Go) | **Best.** Small-block `EncodeAll`/`DecodeAll`, built-in dictionary support (`WithEncoderDict`, `BuildDict`, per the pkg.go.dev snippet), levels from `SpeedFastest` up. | The only dependency to approve. I did not verify the current module version, licence file, or its binary size. |
| **deflate** (stdlib `compress/flate`) | Zero-dependency fallback. `flate.NewWriterDict` supports a preset dictionary (≤32 KiB window). | Ratio is about 10–25 % worse than zstd on text *(estimate, from general experience)*. Decode is fast. |
| **LZ4** (`pierrec/lz4`) | Faster, but typically 1.3–2× larger than zstd on terminal text. | Recording is not CPU bound. Not worth it. |
| **brotli** | Strong on text; pure-Go encoders are slow; the built-in web dictionary is unhelpful for escape sequences. | Reject. |
| **xz/LZMA** | Best ratio, slow, no streaming-friendly blocks. | Reject. |

Codec ids in the file are 0 none / 1 deflate / 2 zstd. The codec package exposes `type Codec interface{ Compress(dst, src []byte, dict []byte) []byte; Decompress(dst, src []byte, dict []byte, maxLen int) ([]byte, error) }` with a registry, so zstd stays a replaceable dependency.

### 4.2 Block structure

Three options considered:
1. **One zstd stream for the whole file, flush per block.** Best ratio (full history). No random access: decoding block N needs blocks 0..N−1. A crash is still recoverable to the last flush. Chunk CRC and "scan without decompressing" still work, but seeking requires decompressing from the start.
2. **Independent frame per block + shared dictionary** (recommended). Seekable, corruption isolated to one block, concurrent decode. Costs ratio on small blocks, recovered partly by a dictionary.
3. **Segmented:** a stream restarted every ~1 MiB with per-flush chunks inside. It is the best of both on large sessions. More complex (chunks stop being self-contained). Consider later only if measurements demand it.

**Dictionary:** a static dictionary trained on terminal output (shell prompts, ANSI SGR, cursor movement, common tools). Either ship it in the binary (id known, not stored) or store it in a `DICT` chunk so the file is self-contained.
- *Researcher inference:* store it in the file for archival safety.
- Training corpus and dictionary size (~16–32 KiB) are open work. I have no measurement of the gain.

### 4.3 Byte transforms

- Delta timestamps as uvarints (already in the layout).
- Columnar separation of dt/kind/plen from payload.
- **Writer-side coalescing:** merge adjacent OUT events that arrive within `coalesce_ticks` (default ~1–2 ms, max 16 KiB). This is lossless at the rendering granularity that matters, and PTY reads already return up to 32 KiB per read (`readBufferSize` in `session.go`).
  - It is **opt-out**, and the HEAD flag records that coalescing was applied.
- No run-length coding of idle gaps is needed: an idle gap is one number.

### 4.4 Expected sizes (ESTIMATES; nothing below was measured)

Assumptions are stated; each number is reasoning, not a benchmark.

| Scenario | Raw output | Events | Estimated `.shot` v3 | Reasoning |
|---|---|---|---|---|
| **Interactive shell, 5 min, ~20 % active** (prompts, `ls`, `git`, editing) | ~30–60 KB | ~300–800 | **~8–20 KB**, +~5 KB per 10 keyframes | Small blocks (a block per ~2–5 s of activity, 1–3 KB) compress ~2× without a dictionary, 3–4× with one. Header ~300 B, footer ~1 KB. Idle contributes about 3 bytes per long gap. |
| **Build log burst** (e.g. 5 MB of compiler output in 30 s) | ~5 MB | ~200–400 (PTY chunks) | **~0.5–1.0 MB** | Text with repeated paths/warnings: zstd -3 typically 5–10×. Coalescing makes ~64 KiB blocks, where independent-block penalty is small. |
| **TUI animation, 30 fps, 5 min (vim scrolling / htop-like)** | 9000 redraws × 0.2–2 KB = ~2–18 MB | ~9000–18000 | **~0.2–2.5 MB** | Redraw diffs are highly repetitive (cursor-move + SGR + same labels); 8–15× zstd. The size is dominated by how much the app repaints, not by the format. |
| **Current JSON v2** for the shell scenario (comparison) | n/a | ~300 frames | **~0.3–0.8 MB** | Each frame repeats 24 lines of text plus spans; ~1–2.5 KB uncompressed per frame. The v3 estimate is therefore **~20–50× smaller**. |

Idle: a 5-minute recording with 4 minutes idle grows by the same amount as one with no idle (only dt varint bytes).

**Keyframe overhead (estimate):** an 80×24 mostly-text grid is ~2 KB of cells + styles raw; ~0.4–0.8 KB after zstd. At 30 s that is ~10 keyframes per 5 min = ~4–8 KB, which may exceed the log for a quiet shell session. Hence the default is "every 30 s, only if changed, plus one at close", and it is configurable (`off`).

### 4.5 Measured numbers

**None.** See Appendix A for the prototype to run. Until measured, treat §4.4 as ±3× uncertain.

---

## 5. D. Idle-time efficiency

- **Event-log model:** idle costs 0 bytes. The next event's dt carries the gap exactly. A 5-minute gap in 100 µs ticks is 3,000,000, a 3-byte uvarint.
- **Keyframe+delta snapshot model (today's):** the current design already emits nothing during idle because `sample()` returns early when `!s.dirty`, but:
  - Intra-interval changes are missed.
  - Cursor blink is not captured. That's right: blink is a render-time property, so the exporter should synthesise the blink phase from `CURSOR_BLINKING` and the time, not store toggles.
  - Title changes arrive via OSC, which already marks the terminal dirty.
  - Any heartbeat, e.g. periodic full snapshots, would violate requirement 2, so the format has none.
- **Time units and precision:** monotonic clock (Go `time.Since` uses the monotonic reading), ticks declared in HEAD (`tick_ns`, default 100 µs).
  - uvarint sizes at 100 µs: 1 B up to 12.7 ms, 2 B up to 1.6 s, 3 B up to 3.4 min, 4 B up to 7.5 h.
  - Use 1 ms instead if the file size on very bursty TUI logs matters more than timing fidelity; the tick is a header value, so both are the same code.
  - On macOS, Go's monotonic clock does not advance during system sleep (my recollection; unverified), so a laptop sleep mid-recording does not create a huge gap. Wall time is in HEAD (`started_unix_ns`) for display only.
- **Timestamp source:** stamp at **PTY read return** (in the reader goroutine), not when the main loop dequeues the chunk. Otherwise queueing delay (up to a tick) distorts timing.
- **Exporters apply idle limits, the file does not:** store exact gaps; the timeline stage (`--idle-limit`, `--speed`, min delay) operates on event times. This is the same as the APNG plan's timeline (`apng-export.md` §5.1) but now over exact times.
- Do not cap idle at write time: it destroys information and also hides "how long did this take".

---

## 6. E. Export-path implications

### 6.1 Per output type

| Output | Needs | Path |
|---|---|---|
| APNG/GIF/WebP | Cell grids + theme at sample times | Replay events → `snapshot.Frame`s → existing raster+timeline (`apng-export.md` §5.2). The rasteriser input stays `iter.Seq2[snapshot.Frame, error]`. |
| MP4 | Constant-frame-rate sampling | Same frame iterator sampled at 1/fps; dedupe unchanged frames into repeats. |
| SVG / HTML | Per-cell attributes, hyperlinks, palette indices | Needs more than today's `Snapshot`: hyperlink table, underline style/colour, palette indices. Either extend `snapshot` (backward-compatible fields) or have SVG/HTML consume the GRID directly. |
| Plain text / markdown | Text only | Formatter PLAIN output at a chosen time, or `Snapshot.Text()`. |
| Re-theme | Palette **indices** preserved | Events replay with the recorded `them` chunk; GRID stores `palette index` vs `rgb` per style (not resolved). |
| Different font / cell size | No font data stored | HEAD font hints are only defaults; pixel size is an export option. |
| Different cols×rows | Event log | See caveat below. |

**Resize/reflow caveat (researcher inference).** Only the raw log can reflow. However, a replay at a different size only gives a sane result for scrolling output (shell, build logs). A cursor-addressed TUI (vim/htop) emits absolute positions for the *recorded* size. Replaying it into another size is wrong, and a real program would have redrawn after SIGWINCH. Say so in the CLI help: "re-size applies to cell/pixel size and padding; changing columns/rows is best-effort".

### 6.2 Keyframe cell-grid layout (GRID kind 1)

Pure data, ordered so zstd sees homogeneous columns:

```
screen_kind    u8   0 primary, 1 alternate
cols,rows      uvarint
cursor         x,y uvarint; flags (visible, blinking, pending-wrap); style (block/bar/underline/hollow)
title, pwd     strings
modes          uvarint count + (mode id uvarint, value bit) for non-default modes only
style_table    n uvarint; each style:
                 fg: kind u8 {default, palette, rgb} + value (u8 | 3 bytes)
                 bg: same;  underline colour: same
                 attrs bitmask u16: bold, faint, italic, blink, inverse, invisible,
                                    strike, overline; underline style (0..5) in its own u8
link_table     n uvarint; each: uri string, id string (OSC 8 params)
rows (column-major per section, every column RLE'd):
  cell_kind[]   per cell, RLE (value,run): 0 empty, 1 narrow, 2 wide-head, 3 wide-tail(spacer)
  style_id[]    per cell, RLE (id,run)             <- the dominant saving: runs of the same style
  link_id[]     per cell, RLE (0 = none)           <- omitted section if no links (flag bit)
  text          UTF-8 of each non-empty, non-spacer cell's first codepoint, concatenated
  grapheme_ext  sparse: (cell index delta, extra codepoints) for multi-codepoint clusters
  row_flags[]   per row: wrapped, semantic-prompt kind (OSC 133)
scrollback     optional: R extra rows above using the same row sections (default 0)
```

- A cell grid is the representation that fixes risk 1 in `apng-export.md` §7 (rune vs cell columns): it stores explicit cells, so the rasteriser no longer has to guess columns.
- **Decision link:** `snapshot.Snapshot` stays as the in-memory frame type. A pure adapter `grid → snapshot.Snapshot` (and back) gives existing code its input. It is lossy where Snapshot is, which is why SVG/HTML would read GRID directly.

### 6.3 `Reader` API

Define at the consumer, in Go 1.26 style (`go.mod` says `go 1.26.0`):

```go
package recording // facade; implemented by binary and legacy readers

type Kind uint8
const (KindOutput Kind = iota; KindInput; KindResize; KindMark; KindExit)

type Event struct {
    At         time.Duration // from recording start, monotonic
    Kind       Kind
    Data       []byte        // valid until the next Next(); OUT/IN/MARK payload
    Cols, Rows int           // RESIZE
    CellW, CellH int         // RESIZE (pixels)
    Exit       int           // EXIT
}

type Reader interface {
    Header() Header                           // cols, rows, tick, started, engine, argv, theme, meta
    Next() (Event, error)                     // io.EOF at end; ErrTruncated wraps a valid prefix
    Seek(t time.Duration) error               // positions at the last block starting <= t
    Keyframe(t time.Duration) (Grid, bool)    // nearest GRID at/before t, if any
    Close() error
}

type FrameOptions struct {
    Interval   time.Duration // 0 = a frame after every event batch where the screen differs
    From, To   time.Duration
    Source     FrameSource   // Auto | Replay | Keyframes
}
func Frames(ctx context.Context, r Reader, newTerm vt.Factory, o FrameOptions) iter.Seq2[snapshot.Frame, error]
```

- `Frames` hides the model: for event files it seeks, replays through `vt.Terminal` (feeding RESIZE with `term.Resize`), and snapshots; for legacy JSON files it yields stored frames.
- With `Source: Keyframes` it yields GRID-derived frames at keyframe times only (pure Go, no `vt.Factory` needed).
- `vt` is an interface-only package (no cgo), so `recording` can depend on it, but this stretches the rule "recording imports only snapshot" in `ARCHITECTURE.md`. Keep `Frames` in a separate package, `internal/recording/frames`, to keep the rule intact.
- **Seeking by replay:** to jump to time T, replay from the start. libghostty parses at far more than MB/s, so a few-MB log replays in a fraction of a second. *Unmeasured.* Native snapshots (kind 3) are the escape hatch if this proves too slow.
- **Trim:** to cut the first part of a recording, create a new file whose first OUT event is a formatter VT dump (`EXTRAS_ALL`) of state at the cut. It is pure terminal bytes and independent of libghostty versions. Scrollback and kitty images are lost in that bootstrap (formatter gaps, §2.3).

---

## 7. F. Migration and layering

### 7.1 Package layout (pure Go unless noted)

```
internal/recording/                    facade: Reader/Writer interfaces, Header, Event, errors; sniffs magic
internal/recording/bin/chunk           magic, chunk framing, CRC32C, limits, torn-tail detection          (stdlib only)
internal/recording/bin/varint          uvarint/zigzag helpers with bounds checking                         (stdlib only)
internal/recording/bin/codec           Codec registry: none, flate (stdlib), zstd (klauspost, build-tag-free)
internal/recording/bin/head            HEAD tagged fields + them + meta encode/decode
internal/recording/bin/event           block encode/decode (columnar), coalescer
internal/recording/bin/keyframe        GRID encode/decode + Grid type, pure data
internal/recording/bin/index           indx footer + scan-based index builder
internal/recording/bin                 Writer (flush policy, fsync) and Reader (scan, seek, recover) composing the above
internal/recording/legacy              existing JSON Lines v1/v2 reader (kept) + converter
internal/recording/frames              Frames() — depends on vt (interface only) and snapshot
internal/vt/ghostty                    (cgo) adds Grid() extraction, Theme(), engine version; the only cgo change
internal/session                       gains an optional EventSink (below)
internal/cli                           record flags, `shellshot convert`, `shellshot info`
```

Dependency rules still hold: `bin/*` import nothing but stdlib, plus `snapshot` for the Grid adapter only.

### 7.2 Minimal `session` change

Today the reader goroutine forwards `chunks chan []byte` and the main loop calls `s.term.Write(chunk)`. Change:

```go
type chunk struct{ at time.Duration; b []byte }   // stamped in the reader goroutine at read return
type EventSink interface {
    Output(at time.Duration, p []byte) error
    Resize(at time.Duration, cols, rows int) error
    // Input / Mark are fed by `live`
}
// Config gains: Events EventSink (nil = no raw log)
```

- Main loop: on chunk, call `Events.Output(chunk.at, chunk.b)` **before** `term.Write` (the log precedes the effect) and keep the existing `dirty` / ticker `sample()` / `Sink` for live features. The sampled `Frame`s keep working unchanged.
- Keyframes are produced on the same goroutine: every N seconds, if dirty since the last keyframe, call the new `term.Grid()` and pass it to the sink with `(blockSeq, eventIndex)` position.
- The reader goroutine must copy the buffer (it already forwards copies; "forwards copies of the output" per `run`'s doc).
- `pump` was not read this run; confirm the stamp can be taken there.
- Input events come from `live` (stdin pump), resizes from SIGWINCH handling in `cli`, both through the same `EventSink` (it must be goroutine-safe, or `session` should funnel them through a channel to preserve the one-owner rule).

### 7.3 Compatibility with JSON v2

- `recording.Open` sniffs the first byte: `{` means legacy JSON (kept as `legacy`), `0x89` means binary.
- `legacy` yields frames; `Frames()` passes them through.
- `shellshot convert old.shot new.shot` writes a binary file whose events are synthetic OUT bytes: `ESC[H ESC[2J` + each frame's lines rendered with SGR from `Spans`, at frame time. This is lossy (no raw bytes existed) and the HEAD flags mark the file "synthetic".
- `replay` and the exporters work on both through `Frames()`.

### 7.4 Atomic task breakdown

Each task is independently mergeable, testable, and sized like earlier tasks. All but T12–T13 need neither cgo nor libghostty.

| # | Task | Tests |
|---|---|---|
| T1 | `bin/varint`: bounded uvarint/zigzag helpers | table + property round trip; fuzz decoder never panics or over-reads |
| T2 | `bin/chunk`: magic, `WriteChunk`/`ReadChunk`, CRC32C, length caps, ancillary-bit helpers | golden bytes for 3 chunks; truncation at every byte offset; bit-flip → `ErrCorrupt`; fuzz |
| T3 | `bin/codec`: interface + `none` + `flate` (stdlib, preset dict) + registry | round trip; decompression-bomb cap (`maxLen`); fuzz |
| T4 | `bin/codec`: zstd implementation (the one dependency task) | round trip, dict round trip, benchmark vs flate recorded in the PR |
| T5 | `bin/head`: HEAD tagged fields, unknown-tag skip, `required_features` refusal; `them`, `meta` | golden file; unknown-tag forward-compat test; fuzz |
| T6 | `bin/event`: columnar block encode/decode, kinds, ≥64 passthrough, reserved kinds error | property test (random events ↔ bytes); golden; fuzz; huge-count guard |
| T7 | `bin/event`: writer-side coalescer with tick threshold | table tests incl. never merging across non-OUT, size cap |
| T8 | `bin` Writer: flush policy (size, time, close), injectable clock, `Close` writes nothing extra yet | fake clock tests; idle = zero bytes written assertion |
| T9 | `bin` Reader: sequential scan, torn-tail recovery (`ErrTruncated` with valid prefix), `Seek` via scanned block table | truncate-at-every-offset test over a golden file; kill-simulation with partial writer |
| T10 | `bin/index`: `indx` footer + trailer; reader fast path; append-after-close | footer present/absent/corrupt equivalence; append test |
| T11 | `bin/keyframe`: Grid type + GRID encode/decode (style table, RLE columns, links, graphemes) | property round trip with random grids; golden; fuzz; size regression test |
| T12 | `internal/vt/ghostty`: `Grid()`, `Theme()`, engine version (cgo; run by the build agent) | cgo tests comparing `Grid()` → `Snapshot` against existing `Snapshot()` |
| T13 | `session`: `EventSink`, stamping in the reader goroutine, keyframe cadence | fake terminal + fake sink tests; ordering (log before write); idle emits nothing |
| T14 | `recording/frames`: replay driver, `Frames()` with fake `vt.Factory` | fake-terminal tests; seek then replay; legacy passthrough |
| T15 | `cli record`: use the binary writer; `--record-input`, `--keyframes`, `--meta`, `--coalesce` flags; `shellshot info` | CLI golden of `info`; flag tests |
| T16 | `legacy` reader move + `convert` command; magic sniffing | existing JSON tests moved intact; converter golden |
| T17 | CI drift test: golden `.shot` + expected GRID replay comparison | fails when the ghostty pin changes output |

T1–T11 can proceed in parallel after T1/T2; the minimal shippable path is T1–T3, T5, T6, T8, T9, T13–T15 (flate fallback, no zstd, no keyframes, no footer).

---

## 8. Contradictions

None found between sources. One tension to be aware of:
- `docs/research/apng-export.md` §8 recommends snapshots as export input *for now* and notes that raw bytes bring exact timing, re-theming, resizing and Kitty graphics. This brief goes further and recommends the event log as the source of truth, because the user's requirement 1 (change output type later) outweighs that note's concern about making export depend on cgo. The mitigation is the `Frames()` abstraction and GRID keyframes for pure-Go consumers.
- The user text says "only the raw event log can reflow correctly". Qualified in §6.1: true for scrolling output, wrong for cursor-addressed TUIs.

## 9. Missing evidence

- **All sizes, ratios, speeds** in §4.4 and the replay speed claim in §6.3 are unmeasured.
- Pinned binding version and module cache location: unread. `Terminal.Mode/SetMode/Pwd`, hyperlink-per-cell getter, and cursor style getters were not located in the extraction.
- Whether libghostty-vt processes sixel: unverified.
- `render.h`, `modes.h`, `grid_ref.h`, `kitty_graphics.h` not re-read this run.
- Formatter VT dump completeness (title, default colours, kitty images, scrollback): inferred from the extras list only.
- Behaviour of the native snapshot with `CONTINUATION` when tracking is off, beyond what the header says.
- ttyrec2, termrec file layouts; script/scriptreplay details; Warp/Ghostty/Kitty/WezTerm recording support: not researched.
- macOS monotonic clock behaviour across sleep: recollection only.
- klauspost/compress current version, licence, and dictionary-training API signatures (`BuildDict` shape taken from a search summary of pkg.go.dev).
- `internal/session` `pump`, `Config`, `Sink` definitions and `internal/recording` Reader were not read.

## 10. Decisions for the user and risks

**Decisions**
1. **Record input by default?** Recommendation: no. Provide `--record-input`, flag it in HEAD, and print a notice. Reason: IN contains passwords.
2. **Raw bytes vs snapshots?** Recommendation: raw bytes (this brief). They may contain secrets that were visible on screen anyway; they also include any control sequences (e.g. OSC 52 clipboard writes). Exporters must never replay OSC 52 clipboard effects (do not register a clipboard effect).
3. **File extension:** keep `.shot` (sniffed by magic; simplest for users) or `.shotb`/`.shot3` (explicit). Default: `.shot`.
4. **Dependency on klauspost/compress:** accept for the better ratio and dictionary support, or ship deflate-only first. Default: accept, behind the `codec` interface.
5. **Keyframe cadence and default** (30 s, only on change), and whether to store scrollback in keyframes (default no).
6. **Environment capture privacy:** only an allowlist; hostname and cwd opt-in or off.

**Risks**
- Replay drift across libghostty versions (mitigated by pinning, GRID insurance, CI drift test; not eliminated).
- Small-block compression penalty for quiet sessions (dictionary and 2 s flush windows help; unmeasured).
- Keyframes may dominate tiny files (configurable).
- Mid-file corruption stops the scan at the first bad chunk (no resync marker). Acceptable for local files; flag if recordings are transferred over lossy media.
- Secrets in output and OSC sequences inside recorded bytes.
- The `snapshot.Snapshot` type is lossy for SVG/HTML; the GRID or an extended snapshot is needed before those exporters.
- go-libghostty API has "no stability promise" (README); adapter-only exposure limits the blast radius.

---

## Appendix A. Untested prototype benchmark (NOT RUN; compile and fix before trusting)

Setup (outside the repo):

```sh
mkdir /tmp/zbench && cd /tmp/zbench && go mod init zbench
go get github.com/klauspost/compress/zstd
script -q /tmp/typescript sh -c 'ls -lR /usr/lib | head -2000; cat /etc/hosts'   # or any real session
go run . /tmp/typescript
```

```go
package main

import (
	"bytes"
	"compress/flate"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"

	"github.com/klauspost/compress/zstd"
)

func main() {
	raw, _ := os.ReadFile(os.Args[1])
	// Synthetic events: split raw into pseudo PTY reads of 1..4096 bytes with random gaps.
	r := rand.New(rand.NewSource(1))
	var dts, lens []uint64
	var chunks [][]byte
	for off := 0; off < len(raw); {
		n := 1 + r.Intn(4096)
		if off+n > len(raw) {
			n = len(raw) - off
		}
		chunks = append(chunks, raw[off:off+n])
		dts = append(dts, uint64(1+r.Intn(3000))) // 100µs ticks
		lens = append(lens, uint64(n))
		off += n
	}
	var col, row, payload bytes.Buffer
	tmp := make([]byte, 10)
	for i, c := range chunks {
		k := binary.PutUvarint(tmp, dts[i]); col.Write(tmp[:k])
		k = binary.PutUvarint(tmp, lens[i]); col.Write(tmp[:k])
		payload.Write(c)
		k = binary.PutUvarint(tmp, dts[i]); row.Write(tmp[:k])
		row.WriteByte(0)
		k = binary.PutUvarint(tmp, lens[i]); row.Write(tmp[:k])
		row.Write(c)
	}
	columnar := append(col.Bytes(), payload.Bytes()...)
	fmt.Println("raw", len(raw), "columnar", len(columnar), "interleaved", row.Len())

	for _, lvl := range []zstd.EncoderLevel{zstd.SpeedFastest, zstd.SpeedDefault, zstd.SpeedBetterCompression} {
		enc, _ := zstd.NewWriter(nil, zstd.WithEncoderLevel(lvl))
		fmt.Printf("zstd lvl %v whole-stream columnar=%d interleaved=%d\n", lvl,
			len(enc.EncodeAll(columnar, nil)), len(enc.EncodeAll(row.Bytes(), nil)))
		for _, bs := range []int{4 << 10, 16 << 10, 64 << 10} {
			total := 0
			for off := 0; off < len(columnar); off += bs {
				end := min(off+bs, len(columnar))
				total += len(enc.EncodeAll(columnar[off:end], nil))
			}
			fmt.Printf("  independent blocks %dK: %d\n", bs>>10, total)
		}
	}
	var fb bytes.Buffer
	fw, _ := flate.NewWriter(&fb, 6)
	fw.Write(columnar); fw.Close()
	fmt.Println("deflate-6 whole stream", fb.Len())
	// TODO: zstd.BuildDict (see pkg.go.dev example) trained on half the blocks, test on the rest.
}
```

Expected use: replace §4.4 ranges with real numbers for (a) a real `script` capture, (b) a `make` log, (c) a recorded `htop`/`vim` session via `shellshot record` once binary output exists. Note the synthetic splitting above does not model real PTY read sizes or timing, so only compare relative effects (columnar vs interleaved, block sizes).

## Sources

- Kept: repo files listed at the top (`ARCHITECTURE.md`, `apng-export.md`, `snapshot.go`, `session.go`, `recording/writer.go`, `vt/ghostty/snapshot.go`): ground truth for current behaviour.
- Kept: local headers `build/libghostty/include/ghostty/vt/snapshot.h`, `terminal.h`, `formatter.h`: direct evidence for native snapshot, effects, formatter extras.
- Kept: go-libghostty `main` (`terminal.go`, `snapshot.go` partial extractions): https://github.com/mitchellh/go-libghostty. The pinned version was not read.
- Kept: asciicast v3 spec (via search summary): https://docs.asciinema.org/manual/asciicast/v3/
- Kept: ttyrec record layout (search summary of Wikipedia mirror): https://en.wikipedia.org/wiki/Ttyrec
- Kept: klauspost zstd package docs (search summary): https://pkg.go.dev/github.com/klauspost/compress/zstd
- Deprioritised: termrec man page (only says multiple formats are supported); no layout details.

## Next steps

1. Run Appendix A on real captures, and replace §4.4 with measured numbers (decides columnar vs interleaved and block size, dictionary value, zstd vs deflate).
2. With the module cache present, verify `Terminal.Mode`, hyperlink access, cursor style getters, `WithContinuationMaxBytes`, and the exact `RenderCellStyle` fields at the pinned version.
3. Decide §10 items 1, 3 and 4, then start T1–T3.

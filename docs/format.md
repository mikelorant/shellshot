# Recording format

`shellshot record` writes `.shot` files; `replay`, `export` and `info` read them. A `.shot` file is a
chunked binary event log: the raw terminal output with its timing, plus the exit status. It stores no
screen contents and no theme. The code is in `internal/recording/bin`; the design discussion is in
[research/binary-format.md](research/binary-format.md).

## Container

The file is the 8-byte magic `89 53 48 4F 54 0D 0A 1A` (`\x89SHOT\r\n\x1a`), then chunks:

```
len u32 | type [4]byte | payload (len bytes) | crc u32
```

Integers are little-endian. `crc` is CRC-32C (Castagnoli) over `type || payload`. A chunk payload is
at most 64 MiB; readers apply a lower default cap of 16 MiB. As in PNG, a lowercase first letter in
the type marks an ancillary chunk that readers skip when unknown; an uppercase first letter marks a
critical chunk that readers must understand.

| Type | Class | Content |
|---|---|---|
| `HEAD` | critical | Exactly one, first. Tagged header fields |
| `DICT` | critical | Optional compression dictionary for later blocks |
| `EVNT` | critical | A block of timestamped events |
| `them`, `meta` | ancillary | Optional theme and key/value strings after `HEAD`. `record --script` writes `meta` `script` (the script's file name) |
| `grid`, `indx`, `blob` | ancillary | Reserved. `record` does not write them and readers skip them |

## Header

`HEAD` is a sequence of `tag uvarint | len uvarint | value` fields in ascending tag order. Readers
keep unknown tags and refuse files that set an unknown bit in `required_features`. Integers other
than the fixed-width ones above are LEB128 uvarints; signed values use zigzag.

`record` writes:

- the size in cells (`--cols`, `--rows`, default the current terminal, else 80x24);
- the tick length, 100 µs by default: all event times are multiples of it;
- the start time and time zone offset;
- the engine name and the libghostty commit the recorder was built against;
- `TERM` and the command's argv as executed;
- an environment allowlist: `TERM`, `COLORTERM`, `LANG`, `SHELL`;
- a random 16-byte session id and the recorder name and version;
- flags: bit 0 means keystrokes were recorded (`--record-input`), bit 2 means adjacent output events
  were merged (`--coalesce`).

It does not store the working directory, the host name or the rest of the environment.

## Events

An `EVNT` payload starts with an uncompressed header so tools can read time ranges without
decompressing:

```
codec uvarint | dict_id uvarint | start_tick uvarint | end_tick uvarint | n_events uvarint | raw_len uvarint | body
```

The body is compressed with the codec and holds three columns followed by the payloads: `dt[n]`
(uvarint ticks since the previous event; the first is relative to `start_tick`), `kind[n]` (one byte
each), `plen[n]` (uvarint payload length) and the concatenated payload bytes.

| Kind | Name | Payload |
|---|---|---|
| 0 | `OUT` | Raw terminal output bytes |
| 1 | `IN` | Raw input bytes; present only with `--record-input` |
| 2 | `RESIZE` | uvarints: cols, rows, cell pixel width, cell pixel height (0 if unknown) |
| 3 | `MARK` | UTF-8 label |
| 4 | `EXIT` | Zigzag exit status |
| 5–63 | reserved | A reader fails with `ErrUnsupported` |
| 64 | `SPAN` | Start or end of a scene or hidden span; see below |
| 65+ | ancillary | Surfaced as raw events; export ignores them |

Time is stored as the gap since the previous event, so idle time costs no bytes: the writer writes
nothing while no event is pending.

### Spans

A `SPAN` payload begins with `version` (1) and `op` (1 begin, 2 end), then:

```
END:   id
BEGIN: id | type | parent_id | name_len | name | n_hints | n_hints x { tag | len | value }
```

`type` is 1 for a scene, 2 for a hidden span and 3 (reserved). `name` is at most 255 bytes; it is the scene name, and for a hidden span the optional name given in the script (empty when unnamed). Hint tags
are 1 speed (milli-x), 2 idle (µs), 3 hold (µs) and 4 caption (UTF-8). Decoders skip unknown hint
tags and bytes after the last known field. An unknown version, op or type makes the event ignorable.
A span still open at the end of the readable events ends at the last event. `info` lists scenes and
hidden spans (with their names, when they have one) when a recording has them, and `export` applies their hints. `record --script` writes
them (see [scripting.md](scripting.md)); a plain `record` writes none. A script that opens with a hidden block
has that span begin at time 0, before the first `OUT` event.

## Compression

Each `EVNT` block is compressed on its own, which isolates damage to one block and allows seeking.
Codec ids: 0 none, 1 deflate, 2 zstd (default). `record --codec none|flate|zstd` selects one.
`record --coalesce DURATION` merges output reads that arrive within `DURATION` of each other
(default 0, off). A block is written when it reaches 64 KiB of raw data, when its oldest event is 2
seconds old, or when the recording ends.

## Crash behaviour and versioning

The writer emits each chunk with one `Write` call, and the file grows as the command runs, so a kill
leaves at most one torn chunk. A reader returns every event of the intact blocks and then reports
the torn tail with the offset where valid data ends. At most about two seconds of output are lost.

- `replay` and `export` play the valid prefix, print `shellshot: recording is truncated (N bytes
  valid)` to stderr and exit 0.
- A file that ends cleanly but has no `EXIT` event, because the recorder was killed at a block
  boundary, plays in full with `shellshot: recording is incomplete (no exit event; recorder was
  interrupted)` on stderr.
- A chunk that fails its CRC and is followed by more data is corruption: reading fails.
- A file that does not start with the magic is rejected with `not a shellshot recording (bad magic)`.

The magic never changes. New information goes into new ancillary chunks or new `HEAD` tags, which old
readers skip. Tags, kinds and chunk types are never reused. A change that old readers must understand
is signalled by a bit in `required_features`.

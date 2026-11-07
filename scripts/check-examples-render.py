#!/usr/bin/env python3
"""Helper for scripts/check-examples-render.sh. Not meant to be run on its own.

  check-examples-render.py [--cols N] [--rows N] [--root DIR] NAME=FILE.shot ...
  check-examples-render.py --selftest

For every recording (made with `shellshot record --codec none`) it
  (a) tracks the cursor through the raw output stream and reports every printable
      character written at column == width, i.e. a real auto-wrap (also a wide glyph
      that does not fit in the last column);
  (b) lists every non-ASCII character cluster the demos print and checks it against the
      fonts shellshot embeds: sprites for box/block/braille/powerline, JetBrains Mono,
      Symbols Nerd Font Mono, the Noto Sans Mono CJK SC subset, and the Noto Color Emoji
      font (a superset of what the Apple chain draws, which falls back to Noto).
Part (a) needs only the standard library. Part (b) needs fontTools and zstandard (the shell
script gets them through uv); without them part (b) is skipped and the exit status is 2.
Exit status: 0 clean, 1 findings, 2 incomplete or bad input.
"""

import argparse
import os
import re
import sys
import unicodedata

CSI_SPLIT = re.compile(r"[;:]")
TAIL_LEN = 40


def uvarint(buf, i):
    value = shift = 0
    while True:
        b = buf[i]
        i += 1
        value |= (b & 0x7F) << shift
        if b < 0x80:
            return value, i
        shift += 7


def read_output(path):
    """Returns the concatenated OUT event payloads of an uncompressed .shot file."""
    with open(path, "rb") as f:
        data = f.read()
    if data[:8] != b"\x89SHOT\r\n\x1a":
        raise ValueError("%s: not a shellshot recording" % path)
    out = bytearray()
    i = 8
    while i + 8 <= len(data):
        size = int.from_bytes(data[i:i + 4], "little")
        kind = data[i + 4:i + 8]
        payload = data[i + 8:i + 8 + size]
        i += 8 + size + 4  # payload, then the crc
        if kind != b"EVNT":
            continue
        codec, p = uvarint(payload, 0)
        if codec != 0:
            raise ValueError("%s: compressed recording (record with --codec none)" % path)
        _dict_id, p = uvarint(payload, p)
        _start, p = uvarint(payload, p)
        _end, p = uvarint(payload, p)
        count, p = uvarint(payload, p)
        _raw_len, p = uvarint(payload, p)
        for _ in range(count):  # dt column
            _, p = uvarint(payload, p)
        kinds = payload[p:p + count]
        p += count
        lengths = []
        for _ in range(count):
            n, p = uvarint(payload, p)
            lengths.append(n)
        for k, n in zip(kinds, lengths):
            if k == 0:  # OUT
                out += payload[p:p + n]
            p += n
    return bytes(out)


class Cluster:
    def __init__(self, runes, width):
        self.runes = runes
        self.width = width
        self.zwj = False


class Term:
    """Cursor tracking for a terminal of cols x rows, in the way libghostty moves it."""

    def __init__(self, cols, rows):
        self.cols = cols
        self.rows = rows
        self.reset()
        self.findings = []

    def reset(self):
        self.col = self.row = 0
        self.pending = False
        self.autowrap = True
        self.top, self.bottom = 0, self.rows - 1
        self.saved = (0, 0, False)
        self.alt_saved = (0, 0, False)
        self.cluster = None
        self.tail = []

    # ---------------------------------------------------------------- findings

    def finding(self, kind, off, ch):
        text = "".join(self.tail[-TAIL_LEN:]) + ch
        self.findings.append((kind, off, self.row + 1, self.col + 1, text))

    # ------------------------------------------------------------------ moves

    def lf(self):
        if self.row == self.bottom:
            return
        self.row = min(self.row + 1, self.rows - 1)

    def wrap(self):
        self.col = 0
        self.pending = False
        self.lf()

    def move(self, row=None, col=None):
        if row is not None:
            self.row = max(0, min(self.rows - 1, row))
        if col is not None:
            self.col = max(0, min(self.cols - 1, col))
        self.pending = False

    def save(self):
        return (self.col, self.row, self.pending)

    def restore(self, state):
        self.col, self.row, self.pending = state

    # ---------------------------------------------------------------- printing

    @staticmethod
    def width_of(ch):
        o = ord(ch)
        if 0x1F1E6 <= o <= 0x1F1FF:
            return 2
        return 2 if unicodedata.east_asian_width(ch) in ("W", "F") else 1

    @staticmethod
    def is_zero_width(ch):
        o = ord(ch)
        return unicodedata.category(ch) in ("Mn", "Me", "Cf") or 0x1F3FB <= o <= 0x1F3FF

    def printable(self, ch, off):
        o = ord(ch)
        cl = self.cluster
        if cl is not None:
            if self.is_zero_width(ch):
                cl.runes.append(ch)
                if o == 0xFE0F and cl.width == 1:
                    self.widen(cl, off)
                if o == 0x200D:
                    cl.zwj = True
                return
            if cl.zwj:
                cl.runes.append(ch)
                cl.zwj = False
                return
            if 0x1F1E6 <= o <= 0x1F1FF and len(cl.runes) == 1 and 0x1F1E6 <= ord(cl.runes[0]) <= 0x1F1FF:
                cl.runes.append(ch)
                return
        elif self.is_zero_width(ch):
            return
        w = self.width_of(ch)
        if self.pending and self.autowrap:
            self.finding("wrap", off, ch)
            self.wrap()
        if w == 2 and self.col == self.cols - 1 and self.autowrap:
            self.finding("wide glyph does not fit in the last column", off, ch)
            self.wrap()
        self.col += w
        if self.col >= self.cols:
            self.col = self.cols - 1
            self.pending = self.autowrap
        self.tail.append(ch)
        self.cluster = Cluster([ch], w)

    def widen(self, cl, off):
        """An emoji variation selector made a narrow character two cells wide."""
        cl.width = 2
        if self.pending:
            self.finding("emoji presentation does not fit in the last column", off, "\ufe0f")
            self.wrap()
            self.col = 2
        else:
            self.col += 1
            if self.col >= self.cols:
                self.col = self.cols - 1
                self.pending = self.autowrap

    # ----------------------------------------------------------------- control

    def control(self, b):
        self.cluster = None
        if b == 0x0D:
            self.col = 0
            self.pending = False
        elif b in (0x0A, 0x0B, 0x0C):
            self.pending = False
            self.lf()
        elif b == 0x08:
            self.move(col=self.col - 1)
        elif b == 0x09:
            self.move(col=min((self.col // 8 + 1) * 8, self.cols - 1))

    def csi(self, params, final):
        private = params[:1] in ("?", "<", "=", ">")
        raw = params.lstrip("?<=>")
        nums = [int(x) if x.isdigit() else None for x in CSI_SPLIT.split(raw)] if raw else []

        def arg(i, default):
            v = nums[i] if i < len(nums) else None
            return default if v is None else v

        def count():
            return arg(0, 1) or 1

        if final == "A":
            self.move(row=self.row - count())
        elif final in ("B", "e"):
            self.move(row=self.row + count())
        elif final in ("C", "a"):
            self.move(col=self.col + count())
        elif final == "D":
            self.move(col=self.col - count())
        elif final == "E":
            self.move(row=self.row + count(), col=0)
        elif final == "F":
            self.move(row=self.row - count(), col=0)
        elif final in ("G", "`"):
            self.move(col=(arg(0, 1) or 1) - 1)
        elif final == "d":
            self.move(row=(arg(0, 1) or 1) - 1)
        elif final in ("H", "f"):
            self.move(row=(arg(0, 1) or 1) - 1, col=(arg(1, 1) or 1) - 1)
        elif final in ("L", "M"):
            self.move(col=0)
        elif final == "r" and not private:
            top, bottom = (arg(0, 1) or 1) - 1, (arg(1, self.rows) or self.rows) - 1
            if 0 <= top < bottom < self.rows:
                self.top, self.bottom = top, bottom
            self.move(row=0, col=0)
        elif final == "s" and not private and not nums:
            self.saved = self.save()
        elif final == "u" and not private and not nums:
            self.restore(self.saved)
        elif final in ("h", "l") and private:
            on = final == "h"
            for n in nums:
                if n == 7:
                    self.autowrap = on
                elif n in (1047, 1048, 1049):
                    if n != 1047 and on:
                        self.alt_saved = self.save()
                    elif n != 1047:
                        self.restore(self.alt_saved)

    def esc(self, b):
        if b == 0x37:    # ESC 7
            self.saved = self.save()
        elif b == 0x38:  # ESC 8
            self.restore(self.saved)
        elif b == 0x44:  # IND
            self.pending = False
            self.lf()
        elif b == 0x45:  # NEL
            self.col = 0
            self.pending = False
            self.lf()
        elif b == 0x4D:  # RI
            self.move(row=self.row - 1)
        elif b == 0x63:  # RIS
            self.reset()

    # -------------------------------------------------------------- the stream

    def feed(self, data):
        i, n = 0, len(data)
        while i < n:
            b = data[i]
            if b == 0x1B:
                self.cluster = None
                i = self.escape(data, i + 1)
            elif b < 0x20 or b == 0x7F:
                self.control(b)
                i += 1
            elif b < 0x80:
                self.printable(chr(b), i)
                i += 1
            else:
                size = 2 if 0xC2 <= b <= 0xDF else 3 if 0xE0 <= b <= 0xEF else 4 if 0xF0 <= b <= 0xF4 else 1
                try:
                    ch = data[i:i + size].decode("utf-8")
                except UnicodeDecodeError:
                    ch, size = "\ufffd", 1
                self.printable(ch, i)
                i += size

    def escape(self, data, i):
        """Handles the bytes after an ESC at data[i-1]; returns the index after them."""
        n = len(data)
        if i >= n:
            return n
        b = data[i]
        if b == 0x5B:  # CSI
            j = i + 1
            while j < n and 0x30 <= data[j] <= 0x3F:
                j += 1
            k = j
            while k < n and 0x20 <= data[k] <= 0x2F:
                k += 1
            if k >= n:
                return n
            if 0x40 <= data[k] <= 0x7E:
                self.csi(data[i + 1:j].decode("ascii", "replace"), chr(data[k]))
            return k + 1
        if b in (0x5D, 0x50, 0x58, 0x5E, 0x5F):  # OSC, DCS, SOS, PM, APC: skip to BEL or ST
            j = i + 1
            while j < n and data[j] not in (0x07, 0x1B, 0x18, 0x1A):
                j += 1
            if j < n and data[j] == 0x1B:
                j += 1
            return j + 1
        if 0x20 <= b <= 0x2F:  # intermediates, then one final byte (charset selection and the like)
            while i < n and 0x20 <= data[i] <= 0x2F:
                i += 1
            return i + 1
        self.esc(b)
        return i + 1


# --------------------------------------------------------------------- fonts

SPRITES = ((0x2500, 0x259F), (0x2800, 0x28FF), (0xE0B0, 0xE0B7))  # internal/render/sprite
SKIN_TONES = range(0x1F3FB, 0x1F400)
EMOJI_MARKS = {0xFE0F, 0x200D, 0x20E3} | set(SKIN_TONES) | set(range(0x1F1E6, 0x1F200)) | set(range(0xE0020, 0xE0080))
FORMAT_RUNES = set(range(0xFE00, 0xFE10)) | {0x200B, 0x200C, 0x200D, 0x2060} | set(range(0xE0020, 0xE0080))


def clusters(text):
    """Splits printed text into character clusters the way Term.printable groups them."""
    out = []
    zwj = False
    for ch in text:
        o = ord(ch)
        if out and (Term.is_zero_width(ch) or zwj or (
                0x1F1E6 <= o <= 0x1F1FF and len(out[-1]) == 1 and 0x1F1E6 <= ord(out[-1]) <= 0x1F1FF)):
            out[-1] += ch
        else:
            out.append(ch)
        zwj = o == 0x200D
    return out


def printed_text(data):
    """The characters of the stream outside escape sequences (for the codepoint listing)."""
    class Collector(Term):
        def __init__(self):
            super().__init__(1 << 30, 1 << 30)
            self.text = []

        def printable(self, ch, off):
            self.text.append(ch)

        def control(self, b):
            self.text.append("\n")

    c = Collector()
    c.feed(data)
    return "".join(c.text)


class Fonts:
    def __init__(self, root):
        from fontTools.ttLib import TTFont
        import io
        import zstandard

        base = os.path.join(root, "internal", "render")
        self.text = []
        for name, path in (
            ("JetBrains Mono", "font/assets/jetbrains-mono/JetBrainsMono-Regular.ttf"),
            ("Symbols Nerd Font Mono", "font/assets/nerd-symbols/SymbolsNerdFontMono-Regular.ttf"),
            ("Noto Sans Mono CJK SC subset", "font/assets/noto-cjk/NotoSansMonoCJKsc-Subset.otf"),
        ):
            self.text.append((name, set(TTFont(os.path.join(base, path), lazy=True).getBestCmap())))
        with open(os.path.join(base, "emoji/assets/noto-emoji/NotoColorEmoji.ttf.zst"), "rb") as f:
            raw = zstandard.ZstdDecompressor().decompressobj().decompress(f.read())
        noto = TTFont(io.BytesIO(raw))
        self.emoji_cmap = noto.getBestCmap()
        self.emoji_bitmaps = set()
        for strike in noto["CBDT"].strikeData:
            self.emoji_bitmaps.update(strike)
        self.ligatures = set()
        for lookup in noto["GSUB"].table.LookupList.Lookup:
            for sub in lookup.SubTable:
                sub = getattr(sub, "ExtSubTable", sub)
                for first, ligs in (getattr(sub, "ligatures", None) or {}).items():
                    for lig in ligs:
                        self.ligatures.add((first,) + tuple(lig.Component))

    def sprite(self, o):
        return any(lo <= o <= hi for lo, hi in SPRITES)

    def emoji_glyph(self, runes):
        """Mirrors emoji.glyphMap.lookup: cmap for one rune, GSUB ligatures for several."""
        names = []
        for r in runes:
            g = self.emoji_cmap.get(r)
            if g is None:
                return False
            names.append(g)
        if len(names) == 1:
            return names[0] in self.emoji_bitmaps
        return tuple(names) in self.ligatures

    def emoji_exact(self, runes):
        bare = [r for r in runes if r not in (0xFE0E, 0xFE0F)]
        variants = [runes]
        if len(bare) != len(runes):
            variants.append(bare)
        if len(bare) > 1:
            variants.append([bare[0], 0xFE0F] + bare[1:])
        return any(self.emoji_glyph(v) for v in variants)

    def classify(self, cluster):
        """Returns (what draws it, ok) for a cluster with a non-ASCII character."""
        runes = [ord(c) for c in cluster]
        base = runes[0]
        if len(runes) == 1 and self.sprite(base):
            return "sprite", True
        if EMOJI_MARKS.intersection(runes[1:]):
            if self.emoji_exact(runes):
                return "Noto Color Emoji", True
            if self.emoji_glyph([base]):
                return "PARTIAL: no sequence glyph in Noto, draws only the first emoji", False
            return "BLANK: sequence unknown to Noto", False
        for name, cmap in self.text:
            if base in cmap:
                missing = [r for r in runes[1:] if r not in FORMAT_RUNES and not any(r in c for _, c in self.text)]
                if missing:
                    return "BLANK mark U+%04X (base is in %s)" % (missing[0], name), False
                return name, True
        if self.emoji_glyph([base]):
            return "Noto Color Emoji", True
        return "BLANK: no embedded font has U+%04X" % base, False


def describe(cluster):
    return " ".join("U+%04X" % ord(c) for c in cluster)


# ---------------------------------------------------------------------- main


def selftest():
    def run(data, cols=80, rows=24):
        t = Term(cols, rows)
        t.feed(data)
        return t.findings

    def check(name, cond):
        if not cond:
            raise SystemExit("selftest failed: " + name)

    check("80 chars fit", not run(b"a" * 80))
    check("80 chars then CRLF", not run(b"a" * 80 + b"\r\n" + b"b" * 80))
    check("81st char wraps", len(run(b"a" * 81)) == 1)
    check("escapes take no column", not run(b"\x1b[1;31m" + b"a" * 80 + b"\x1b[0m"))
    check("OSC takes no column", not run(b"\x1b]0;" + b"t" * 200 + b"\x07" + b"a" * 80))
    check("CUP then overflow", len(run(b"\x1b[3;75H" + b"a" * 7)) == 1)
    check("CUP exact fit", not run(b"\x1b[3;75H" + b"a" * 6))
    check("wide glyph pairs", not run("語".encode() * 40) and len(run("語".encode() * 41)) == 1)
    check("wide glyph at last column", len(run(b"\x1b[1;80H" + "語".encode())) == 1)
    check("save/restore keeps pending", len(run(b"a" * 80 + b"\x1b7\x1b[1;1H\x1b8x")) == 1)
    check("CUP clears pending", not run(b"a" * 80 + b"\x1b[2;1Hx"))
    check("CR clears pending", not run(b"a" * 80 + b"\rx"))
    check("alt screen restores cursor", not run(b"abc\x1b[?1049h\x1b[10;70H" + b"z" * 5 + b"\x1b[?1049l" + b"a" * 70))
    check("autowrap off", not run(b"\x1b[?7l" + b"a" * 200))
    check("combining mark takes no column", not run(b"e\xcc\x81" * 80))
    check("ZWJ sequence is one cluster", not run("👨‍👩‍👧‍👦".encode() * 40))
    check("flag is one cluster", not run("🇯🇵".encode() * 40))
    check("emoji presentation widens", len(run(b"a" * 79 + "✔\ufe0f".encode())) == 1)
    check("scroll region reset homes", not run(b"\x1b[5;20H\x1b[r" + b"a" * 80))
    check("tracker records the text", run(b"x" * 80 + b"y")[0][4].endswith("y"))
    print("selftest ok")


def main():
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--cols", type=int, default=80)
    ap.add_argument("--rows", type=int, default=24)
    ap.add_argument("--root", default=os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    ap.add_argument("--selftest", action="store_true")
    ap.add_argument("recordings", nargs="*", metavar="NAME=FILE")
    args = ap.parse_args()
    if args.selftest:
        selftest()
        return 0

    status = 0
    streams = {}
    for item in args.recordings:
        name, _, path = item.partition("=")
        streams[name] = read_output(path)

    print("== auto-wrap check (%dx%d)" % (args.cols, args.rows))
    wraps = 0
    for name, data in streams.items():
        term = Term(args.cols, args.rows)
        term.feed(data)
        for kind, off, row, col, text in term.findings[:20]:
            print("WRAP %s: byte %d, row %d col %d: %s: ...%s" % (name, off, row, col, kind, repr(text)))
        if len(term.findings) > 20:
            print("WRAP %s: %d more" % (name, len(term.findings) - 20))
        wraps += len(term.findings)
        print("%-12s %7d bytes  %s" % (name, len(data), "%d wrap(s)" % len(term.findings) if term.findings else "ok"))
    if wraps:
        status = 1

    print("== non-ASCII characters and the embedded fonts")
    seen = {}
    for name, data in streams.items():
        for cl in clusters(printed_text(data)):
            if any(ord(c) > 127 for c in cl):
                seen.setdefault(cl, []).append(name)
    try:
        fonts = Fonts(args.root)
    except ImportError as e:
        print("SKIPPED: %s (run through uv: see scripts/check-examples-render.sh)" % e)
        return status or 2
    bad = 0
    for cl in sorted(seen, key=lambda c: [ord(x) for x in c]):
        who, ok = fonts.classify(cl)
        users = ",".join(sorted(set(seen[cl])))
        print("%s %-40s %-4s %s  [%s]" % ("  " if ok else "!!", describe(cl), cl, who, users))
        bad += not ok
    print("%d distinct clusters, %d without a glyph" % (len(seen), bad))
    if bad:
        status = 1
    print("== %s" % ("findings: %d wrap(s), %d glyph problem(s)" % (wraps, bad) if status else "clean"))
    return status


if __name__ == "__main__":
    try:
        sys.exit(main())
    except (OSError, ValueError) as e:
        print("check-examples-render: %s" % e, file=sys.stderr)
        sys.exit(2)

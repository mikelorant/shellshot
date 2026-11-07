#!/usr/bin/env python3
"""Build the compact Apple Color Emoji asset embedded in shellshot.

Input is the *release* font published by github.com/samuelngs/apple-emoji-ttf
(AppleColorEmoji-Linux.ttf, CBDT/CBLC with eight strikes, sequences in Apple's
AAT `morx` table, no GSUB). No Mac is needed. The script:

  1. downloads and sha256-verifies the release font (or uses --release-font);
  2. keeps ONE bitmap strike (default 96 ppem; another size is taken from the
     font's own strike, or downscaled from a larger one with LANCZOS);
  3. uses the upstream pipeline modules from third_party/apple-emoji-ttf
     (HarfBuzz shaping oracle on `morx`, bitmap composition for multi-glyph
     shapes, GSUB ligature builder, CBDT/CBLC writer) to materialise one glyph
     per emoji sequence in emoji-test.txt / emoji-variation-sequences.txt;
  4. drops every glyph that is neither a cmap component nor a sequence result,
     merges sequence glyphs whose bitmap duplicates an existing glyph, and
     palette-quantises the PNGs with libimagequant (--colors, 0 = off);
  5. writes a font with head hhea hmtx maxp OS/2 post cmap name CBLC CBDT GSUB
     only (a `ccmp` ligature lookup maps each multi-codepoint sequence to its
     glyph). OS/2.fsType is left exactly as in the source font (4);
  6. zstd-compresses it (level 19) into the embeddable asset and records the
     sha256 of the decompressed font.

Run through `make emoji-asset`, which initialises the submodule and supplies the
pinned Python dependencies (fonttools, pillow, pyyaml, uharfbuzz, imagequant,
zstandard) with `uv run --no-project --python 3.12 --with ...`.
"""

from __future__ import annotations

import argparse
import hashlib
import logging
import sys
import urllib.request
from pathlib import Path

REPO = Path(__file__).resolve().parent.parent
UPSTREAM = REPO / "third_party" / "apple-emoji-ttf"

RELEASE_TAG = "macos-26-20260722-484daf4e"
RELEASE_ASSET = "AppleColorEmoji-Linux.ttf"
RELEASE_URL = (
    "https://github.com/samuelngs/apple-emoji-ttf/releases/download/"
    f"{RELEASE_TAG}/{RELEASE_ASSET}"
)
RELEASE_SHA256 = "e37c7af6265ac4a0af6d57bc65e86109a776d9966e8343334557f63da482516f"

KEEP_TABLES = ("head", "hhea", "hmtx", "maxp", "OS/2", "post", "cmap", "name", "CBLC", "CBDT", "GSUB")
ASSET_STEM = "AppleColorEmoji-shellshot"
ZWJ = 0x200D
VS16 = 0xFE0F

LOG = logging.getLogger("build-apple-emoji-asset")


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as f:
        for chunk in iter(lambda: f.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def fetch_release_font(path: Path) -> Path:
    if not path.exists():
        path.parent.mkdir(parents=True, exist_ok=True)
        LOG.info("Downloading %s", RELEASE_URL)
        tmp = path.with_suffix(".part")
        urllib.request.urlretrieve(RELEASE_URL, tmp)
        tmp.replace(path)
    got = sha256_file(path)
    if got != RELEASE_SHA256:
        raise SystemExit(f"{path}: sha256 {got} != expected {RELEASE_SHA256}")
    return path


def import_upstream() -> None:
    if not (UPSTREAM / "pipeline.py").exists():
        raise SystemExit(
            "third_party/apple-emoji-ttf is not checked out; run:\n"
            "  git submodule update --init --depth 1 third_party/apple-emoji-ttf"
        )
    sys.path.insert(0, str(UPSTREAM))


def cbdt_strike(font, ppem: int):
    """Return the font's CBDT strike as an upstream BitmapStrike at `ppem`."""
    from bitmap.png import resize_png
    from models import BitmapGlyph, BitmapStrike

    ppems = {s.bitmapSizeTable.ppemX: i for i, s in enumerate(font["CBLC"].strikes)}
    order = font.getGlyphOrder()
    gid = {name: i for i, name in enumerate(order)}
    if ppem in ppems:
        source_ppem = ppem
    else:
        larger = [p for p in ppems if p > ppem]
        if not larger:
            raise SystemExit(f"no strike >= {ppem} ppem in source font: {sorted(ppems)}")
        source_ppem = min(larger)
        LOG.info("No %d ppem strike; downscaling the %d ppem strike", ppem, source_ppem)
    glyphs = []
    for name, bitmap in font["CBDT"].strikeData[ppems[source_ppem]].items():
        png = bitmap.imageData
        if source_ppem != ppem:
            png = resize_png(png, ppem)
        glyphs.append(BitmapGlyph(gid=gid[name], name=name, png=png))
    return BitmapStrike(ppem=ppem, glyphs=tuple(glyphs))


def quantize_png(png: bytes, colors: int, dither: float) -> bytes:
    """Palette-quantise with libimagequant (what pngquant uses)."""
    import io

    import imagequant
    from PIL import Image

    image = Image.open(io.BytesIO(png)).convert("RGBA")
    out = io.BytesIO()
    imagequant.quantize_pil_image(image, dithering_level=dither, max_colors=colors).save(
        out, format="PNG", optimize=True, compress_level=9
    )
    return out.getvalue()


def build(args: argparse.Namespace) -> bytes:
    import_upstream()
    from fontTools.ttLib import TTFont
    from fontTools.ttLib.tables._c_m_a_p import CmapSubtable
    from models import BitmapGlyph, BitmapStrike
    from shaping.gsub import build_gsub_from_sequence_rules
    from shaping.sequence_glyphs import materialize_sequence_glyphs
    from shaping.sequences import SequenceRule, load_sequence_inventory
    from bitmap.transforms import _flip_directional_variants
    from tables.cbdt_cblc import FontMetrics, build_cbdt_strikes, build_cblc_strikes
    from tables.raw import add_raw_table

    release = fetch_release_font(args.release_font)
    font = TTFont(release, recalcTimestamp=False)
    source_fs_type = font["OS/2"].fsType

    # Apple draws right-facing people by mirroring the base glyph; the source
    # font stores the unmirrored bitmap under the ".u27A1" glyph names. Mirror
    # first so sequence glyphs copied from them face the right way.
    strike = _flip_directional_variants(cbdt_strike(font, args.ppem))

    seq_dir = UPSTREAM / "sequences"
    records = load_sequence_inventory([seq_dir / "emoji-test.txt", seq_dir / "emoji-variation-sequences.txt"])
    LOG.info("%d normalised sequence records", len(records))
    built = materialize_sequence_glyphs(font, release, 0, [strike], records)
    if built.skips:
        by_reason: dict[str, int] = {}
        for skip in built.skips:
            by_reason[skip.reason] = by_reason.get(skip.reason, 0) + 1
        LOG.info("skipped records: %s", by_reason)
    glyphs = {g.name: g for g in built.strikes[0].glyphs}

    # Merge generated sequence glyphs that duplicate another glyph's bitmap.
    first_by_png: dict[bytes, str] = {}
    alias: dict[str, str] = {}
    for g in sorted(glyphs.values(), key=lambda g: (g.name.startswith("seq."), g.gid)):
        first = first_by_png.setdefault(g.png, g.name)
        if first != g.name and g.name.startswith("seq."):
            alias[g.name] = first
    rules = [
        SequenceRule(r.components, alias.get(r.replacement, r.replacement), r.normalized_sequence, r.raw_sequence)
        for r in built.rules
    ]
    LOG.info("%d sequence glyphs merged into existing bitmaps", len(alias))

    # Glyphs to keep: .notdef, cmap components of any sequence, rule results.
    cps = {ZWJ, VS16}
    for rec in records:
        for seq in (rec.normalized_sequence, *rec.raw_sequences):
            cps.update(seq)
    old_cmap = font.getBestCmap()
    new_cmap = {cp: old_cmap[cp] for cp in sorted(cps) if cp in old_cmap}
    keep = {".notdef", *new_cmap.values(), *(r.replacement for r in rules)}
    old_order = font.getGlyphOrder()
    new_order = [n for n in old_order if n in keep]
    LOG.info("keeping %d of %d glyphs, %d cmap entries, %d rules", len(new_order), len(old_order), len(new_cmap), len(rules))

    font.setGlyphOrder(new_order)
    font["maxp"].numGlyphs = len(new_order)
    hmtx = font["hmtx"]
    hmtx.metrics = {n: hmtx.metrics[n] for n in new_order}

    cmap = font["cmap"]
    cmap.tables = []
    for fmt, plat_enc, mapping in (
        (4, (3, 1), {cp: n for cp, n in new_cmap.items() if cp <= 0xFFFF}),
        (12, (3, 10), new_cmap),
    ):
        sub = CmapSubtable.newSubtable(fmt)
        sub.platformID, sub.platEncID = plat_enc
        sub.language = 0
        sub.cmap = dict(mapping)
        cmap.tables.append(sub)

    font["GSUB"] = build_gsub_from_sequence_rules(font, rules)

    # Bitmaps: quantise, then write CBDT/CBLC with the upstream writers.
    final = []
    for gid, name in enumerate(new_order):
        g = glyphs.get(name)
        if g is None:
            continue
        png = g.png
        if args.colors:
            png = quantize_png(png, args.colors, args.dither)
        final.append((gid, name, png))
    head, hhea = font["head"], font["hhea"]
    metrics = FontMetrics(upem=head.unitsPerEm, ascent=hhea.ascent, descent=abs(hhea.descent))
    cbdt, strike_data = build_cbdt_strikes([(args.ppem, final)], metrics)
    add_raw_table(font, "CBDT", cbdt)
    add_raw_table(font, "CBLC", build_cblc_strikes(strike_data, metrics))

    for tag in list(font.keys()):
        if tag != "GlyphOrder" and tag not in KEEP_TABLES:
            del font[tag]

    if font["OS/2"].fsType != source_fs_type:
        raise SystemExit("OS/2.fsType changed; it must stay as in the source font")
    LOG.info("OS/2.fsType = %d (unchanged)", source_fs_type)

    out = args.out_dir / f"{ASSET_STEM}.ttf"
    args.out_dir.mkdir(parents=True, exist_ok=True)
    font.save(out)
    data = out.read_bytes()
    LOG.info("font: %d bytes, %d bitmaps", len(data), len(final))
    return data


def write_asset(data: bytes, args: argparse.Namespace) -> None:
    import zstandard

    compressed = zstandard.ZstdCompressor(level=19, write_checksum=True).compress(data)
    args.asset_dir.mkdir(parents=True, exist_ok=True)
    (args.asset_dir / f"{ASSET_STEM}.ttf.zst").write_bytes(compressed)
    digest = hashlib.sha256(data).hexdigest()
    (args.asset_dir / f"{ASSET_STEM}.ttf.sha256").write_text(f"{digest}  {ASSET_STEM}.ttf\n")
    LOG.info("asset: %d bytes zstd (raw %d), sha256 %s", len(compressed), len(data), digest)


def main() -> None:
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--release-font", type=Path, default=REPO / "build" / "apple-emoji" / RELEASE_ASSET,
                   help="release font; downloaded and verified when missing")
    p.add_argument("--ppem", type=int, default=96, help="the single bitmap strike to keep")
    p.add_argument("--colors", type=int, default=128, help="PNG palette size, 0 = keep 32-bit PNGs")
    p.add_argument("--dither", type=float, default=0.5, help="libimagequant dithering level, 0..1")
    p.add_argument("--out-dir", type=Path, default=REPO / "build" / "apple-emoji",
                   help="where the uncompressed font is written")
    p.add_argument("--asset-dir", type=Path, default=REPO / "internal" / "render" / "emoji" / "assets" / "apple-emoji",
                   help="where the .zst and .sha256 are written")
    p.add_argument("--no-asset", action="store_true", help="only write the uncompressed font")
    args = p.parse_args()
    logging.basicConfig(level=logging.INFO, format="%(levelname)s %(message)s")
    data = build(args)
    if not args.no_asset:
        write_asset(data, args)


if __name__ == "__main__":
    main()

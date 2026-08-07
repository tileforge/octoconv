#!/usr/bin/env python3
"""gen_heavy_png.py -- deterministic stdlib-only generator for Phase 55
(QUOT-05 gap closure, plan 55-07)'s committed burst fixture:
scripts/fixtures/quota-burst-heavy.png.

CALIBRATION TARGET: an image-engine conversion long enough that an admitted
job cannot reach a terminal status inside the multi-replica quota live
gate's 250 ms burst window (scripts/quota-multireplica-liveproof.sh,
BURST_SEND_SPREAD_MS). The 2026-08-07 live run failed at round 4/10 because
the previous fixture -- internal/e2e/testdata/sample.png, 86 bytes, 16x16 --
converts (png->jpg) in ~234 ms, well inside a straggler request's send-to-
admit window; a concurrency slot legitimately freed mid-burst and the
gate's `admitted == K` assertion, sound only while no admitted job can go
terminal during the burst, silently stopped holding. This generator exists
to make that premise TRUE, not merely assumed.

MEASURED NUMBERS (established during Phase 55 plan-07 planning on this
host; re-measured, not copied forward, by the plan that generates and
commits this file's output -- see that plan's SUMMARY for the actual
numbers observed on execution):
  - 9000x9000, 8-bit truecolor, identical rows, Up-filtered, zlib level 9:
    259,682 bytes on the wire, 81,000,000 decoded pixels.
  - `vips copy` inside octoconv-worker:dev (vips-8.14.1) at the chart's own
    worker limits (--cpus=2 --memory=1g): png->webp 5194-5561 ms across
    three serial runs (parallel: ~5307 ms wall for two); png->jpg 601 ms,
    REJECTED as the live gate's target format -- far too fast to establish
    the long-job premise, and an 18.9 MB result upload per job for nothing.

CHART CEILING: deploy/chart/octoconv's configmap sets
MAX_IMAGE_PIXELS: 100000000 (internal/api's declared-pixel-dimension guard,
internal/convert/dimensions.go, rejects with 422 above this, checked AFTER
PreAdmit and BEFORE storage.Upload). 9000*9000 = 81,000,000 px sits 19%
under that ceiling -- large enough to be expensive to decode/resample/
re-encode, comfortably clear of the guard that would otherwise 422 every
burst request for the wrong reason.

WHY THE RASTER IS "SMALL ON THE WIRE, EXPENSIVE TO CONVERT": one
deterministic RGB row is built once as the ramp
((x*7)&0xFF, (x*13)&0xFF, (x*29)&0xFF) for x in 0..width-1. Row 0 is
emitted with PNG filter byte 0 (None); every subsequent row is IDENTICAL to
the row above it, so it is emitted with filter byte 2 (Up) followed by
width*3 zero bytes -- an Up-filtered scanline that is byte-for-byte
identical to the row above compresses to almost nothing under DEFLATE,
because the filter has reduced every byte in that scanline to zero. The
compressed stream is therefore tiny (254 KiB) while the DECODED raster the
converter must actually decode, resample and re-encode is still a real
width*height*3 = 243,000,000 bytes. This is precisely the property D1 (see
55-07-PLAN.md <design_decisions_already_made>) requires, and precisely why
a "just use a bigger file" approach (a large POST body) would be the wrong
fix -- a bigger body spreads the ADMISSION DECISIONS themselves apart
(handlers.go: PreAdmit -> dimension peek -> storage.Upload -> Create),
worsening the burst-window-too-loose defect while trying to fix the
premise defect. Compression uses a single zlib.compressobj(9), fed one
scanline at a time and flushed once at the end -- the full 243 MB decoded
raster is never held in memory by this generator, only one scanline at a
time.

WHY THIS GENERATOR'S OUTPUT IS COMMITTED, UNLIKE gen_heavy_docx.py's (D3,
55-07-PLAN.md): gen_heavy_docx.py's generated .docx is deliberately NEVER
committed, because that generator needs a third-party library
(python-docx, pulled fresh via ephemeral `uv run --with` on every
invocation) and produces a multi-MB artifact -- committing either would be
wrong. Neither reason applies here: this generator is stdlib-only
(argparse, struct, zlib, os, sys -- nothing else, no pip, no uv, no
requirements manifest touched) and deterministic, and its output is 254
KiB, the same order of magnitude as the committed fixtures under
internal/e2e/testdata/. Committing the output also keeps
scripts/quota-multireplica-liveproof.sh's existing `-f "$FIXTURE"` preflight
check and its sha256 + byte-count host<->pod transfer-integrity path
working completely unchanged, and avoids reintroducing a host python3
runtime dependency into that gate at all -- a dependency Plan 55-06
deliberately removed when the burst driver moved entirely into the burst
pod.

THE ONLY CONSUMER of scripts/fixtures/quota-burst-heavy.png (this
generator's committed output) is scripts/quota-multireplica-liveproof.sh's
FIXTURE default. This generator is a reproducibility/audit tool for that
committed artifact, not something the live gate itself invokes at run
time.
"""
import argparse
import os
import struct
import sys
import zlib

PNG_SIGNATURE = b"\x89PNG\r\n\x1a\n"


def chunk(tag: bytes, data: bytes) -> bytes:
    """Build one PNG chunk: big-endian length, 4-byte tag, data, CRC-32 of
    tag+data. Shared by IHDR/IDAT/IEND so the framing logic exists in
    exactly one place."""
    return (
        struct.pack(">I", len(data))
        + tag
        + data
        + struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF)
    )


def build_row0(width: int) -> bytes:
    """The single deterministic RGB row every row in the image reuses --
    built once, never per-row, so generation cost stays O(width) not
    O(width*height)."""
    pixels = bytearray(width * 3)
    for x in range(width):
        pixels[x * 3 + 0] = (x * 7) & 0xFF
        pixels[x * 3 + 1] = (x * 13) & 0xFF
        pixels[x * 3 + 2] = (x * 29) & 0xFF
    return bytes(pixels)


def generate(width: int, height: int, out_path: str) -> None:
    """Write a valid 8-bit truecolor (colour type 2, no interlace) PNG of
    width x height to out_path. Row 0 carries real content (filter None);
    every other row is filter Up over an identical row above, so its
    filtered bytes are all zero -- the compressed-small/decoded-large
    property this whole module exists for. The full raster is never
    materialized in memory: one scanline is fed to a single
    zlib.compressobj(9) per iteration, flushed once at the end."""
    row0_data = build_row0(width)
    row0_scanline = bytes([0]) + row0_data  # filter byte 0 = None
    up_scanline = bytes([2]) + bytes(width * 3)  # filter byte 2 = Up, all-zero deltas

    compressor = zlib.compressobj(9)
    idat_parts = [compressor.compress(row0_scanline)]
    for _ in range(height - 1):
        idat_parts.append(compressor.compress(up_scanline))
    idat_parts.append(compressor.flush())
    idat_data = b"".join(idat_parts)

    # Bit depth 8, colour type 2 (truecolour), no interlace (0,0,0 are
    # compression/filter/interlace method -- compression method 0 and
    # filter method 0 are the only PNG-defined values; interlace 0 = none).
    ihdr_data = struct.pack(">IIBBBBB", width, height, 8, 2, 0, 0, 0)

    with open(out_path, "wb") as f:
        f.write(PNG_SIGNATURE)
        f.write(chunk(b"IHDR", ihdr_data))
        f.write(chunk(b"IDAT", idat_data))
        f.write(chunk(b"IEND", b""))


def main() -> None:
    parser = argparse.ArgumentParser(
        description=(
            "Generate the deterministic, stdlib-only heavy PNG fixture used by "
            "scripts/quota-multireplica-liveproof.sh (Phase 55, QUOT-05)."
        )
    )
    parser.add_argument("--width", type=int, required=True, help="Image width in pixels (>= 1).")
    parser.add_argument("--height", type=int, required=True, help="Image height in pixels (>= 1).")
    parser.add_argument("--out", type=str, required=True, help="Output path for the generated PNG.")
    args = parser.parse_args()

    if args.width < 1:
        print("FAIL: --width must be >= 1", file=sys.stderr)
        sys.exit(1)
    if args.height < 1:
        print("FAIL: --height must be >= 1", file=sys.stderr)
        sys.exit(1)

    generate(args.width, args.height, args.out)

    byte_size = os.path.getsize(args.out)
    pixel_count = args.width * args.height
    print(f"Generated heavy png: {args.out}")
    print(f"dimensions: {args.width}x{args.height}")
    print(f"pixels: {pixel_count}")
    print(f"bytes: {byte_size}")


if __name__ == "__main__":
    main()

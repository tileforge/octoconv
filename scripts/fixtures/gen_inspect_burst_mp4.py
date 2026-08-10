#!/usr/bin/env python3
"""gen_inspect_burst_mp4.py -- deterministic pathological MP4 generator for
Phase 58 (INSP-06/SC6), scripts/inspect-burst-liveproof.sh's committed
fixture inspect-burst-pathological.mp4.

WHAT THIS TARGETS -- internal/inspect/media.go's head-then-tail `moov`
search (findMoov) and its box-walk ceiling (maxISOBMFFBoxes = 4096):

  (a) `moov` lives at the file TAIL, not the head -- media.go's own
      file-level doc comment names this as the realistic case for
      non-faststart output (ffmpeg's default). Placing it there forces
      findMoov's STEP 2 tail-anchored fallback to actually run, instead of
      resolving in the cheap head-window path.

  (b) a chain of `free` boxes BEFORE `moov`, sized to land the traversal
      step count at maxISOBMFFBoxes (4096) EXACTLY when walkTopLevelBoxes
      scans the head window -- "maximizing steps within the ceiling", not
      past it: this fixture's head region contains precisely 4096 minimal
      (8-byte) boxes, so the walk consumes its full per-call budget before
      concluding (correctly) that `moov` is not there, rather than
      terminating early on a short chain.

  (c) valid `mvhd`/`tkhd`/`stsd` INSIDE `moov` -- media.go's own
      isobmffCharacteristics extracts duration (mvhd), resolution (tkhd),
      and declared codec (stsd's first sample-entry fourCC) ONLY when these
      are well-formed (ISO/IEC 14496-12 8.2.2.1 / 8.3.2.1 / 8.5.2); a
      fixture that merely LOOKS like an MP4 without these would report
      moov_found but nothing else, and would not exercise the
      characteristic-extraction code paths at all.

SIZE / WINDOW ARITHMETIC (see scripts/fixtures/README.md for the numbers
this script prints when run) -- media.go's findMoov derives its windows
from BOTH mp4TailWindow (2 MiB, a media.go constant) AND the caller's
maxBytes (INSPECT_MAX_WORK_BYTES): headLen = min(maxBytes, size); tailLen =
min(mp4TailWindow, maxBytes, size); tailOff = size - tailLen. At this gate's
overlay value (INSPECT_MAX_WORK_BYTES = 1 MiB,
values-inspect-gate.yaml), headLen == tailLen == 1 MiB regardless of the 2
MiB constant (bounded down by maxBytes) -- this script's TOTAL_BYTES is
chosen so the head window [0, 1 MiB) and the tail window
[TOTAL_BYTES - 1 MiB, TOTAL_BYTES) never overlap, and `moov` sits entirely
inside the tail window, nowhere near the head window's own reach.

DETERMINISTIC -- pure struct.pack, no timestamps, no randomness.

Usage:
    python3 scripts/fixtures/gen_inspect_burst_mp4.py \
        --out scripts/fixtures/inspect-burst-pathological.mp4
"""
import argparse
import struct
import sys

# MAX_ISOBMFF_BOXES mirrors internal/inspect/media.go's maxISOBMFFBoxes
# EXACTLY -- the number of minimal `free` boxes this script places before
# `moov` in the head region.
MAX_ISOBMFF_BOXES = 4096

# INSPECT_MAX_WORK_BYTES_OVERRIDE mirrors this gate's own overlay value
# (values-inspect-gate.yaml) -- both media.go's head window AND its tail
# window are bounded by this number at the gate's configured ceiling (see
# this file's own doc comment above for the exact derivation).
INSPECT_MAX_WORK_BYTES_OVERRIDE = 1 * 1024 * 1024

# TOTAL_BYTES: 3 MiB -- comfortably above INSPECT_MAX_WORK_BYTES_OVERRIDE
# (so this fixture also satisfies the general "above work ceiling, below
# upload ceiling" sizing rule every fixture in this directory follows) and
# small enough that head=[0,1MiB) and tail=[2MiB,3MiB) never overlap, with a
# full 1 MiB "dead zone" in between that neither window ever reads -- the
# realistic shape of a large non-faststart video's `mdat` payload.
TOTAL_BYTES = 3 * 1024 * 1024


def box(box_type: bytes, payload: bytes) -> bytes:
    if len(box_type) != 4:
        raise ValueError("box type must be exactly 4 bytes")
    size = 8 + len(payload)
    return struct.pack(">I4s", size, box_type) + payload


def build_ftyp() -> bytes:
    major_brand = b"isom"
    minor_version = struct.pack(">I", 0x200)
    compatible_brands = b"isom" + b"mp41"
    return box(b"ftyp", major_brand + minor_version + compatible_brands)


def build_free_box_chain(count: int) -> bytes:
    """count minimal 8-byte `free` boxes (header only, zero payload) --
    walkTopLevelBoxes counts EACH as one traversal step."""
    one = box(b"free", b"")
    if len(one) != 8:
        raise AssertionError("minimal free box must be exactly 8 bytes")
    return one * count


def build_mvhd() -> bytes:
    """Version-0 mvhd, full 100-byte standard payload (parseMVHD only reads
    the first 20 bytes -- version/flags/creation/modification/timescale/
    duration -- but the fuller shape is kept for realism, per this phase's
    own requirement that mvhd/tkhd/stsd be genuinely well-formed)."""
    version_flags = struct.pack(">I", 0)
    creation_time = struct.pack(">I", 0)
    modification_time = struct.pack(">I", 0)
    timescale = struct.pack(">I", 1000)  # 1000 units/second
    duration = struct.pack(">I", 42000)  # 42.000s -- parseMVHD: duration/timescale
    rate = struct.pack(">I", 0x00010000)  # 1.0, 16.16 fixed
    volume = struct.pack(">h", 0x0100)  # 1.0, 8.8 fixed
    reserved16 = struct.pack(">h", 0)
    reserved2x32 = struct.pack(">II", 0, 0)
    matrix = struct.pack(
        ">9i", 0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000
    )
    pre_defined = b"\x00" * 24
    next_track_id = struct.pack(">I", 2)
    payload = (
        version_flags
        + creation_time
        + modification_time
        + timescale
        + duration
        + rate
        + volume
        + reserved16
        + reserved2x32
        + matrix
        + pre_defined
        + next_track_id
    )
    assert len(payload) == 100, f"mvhd payload should be 100 bytes, got {len(payload)}"
    return box(b"mvhd", payload)


def build_tkhd(width: int, height: int) -> bytes:
    """Version-0 tkhd, standard 84-byte payload. parseTKHD reads
    width/height as 16.16 fixed-point at payload offset 76 (version0
    off=24+52=76)."""
    version_flags = struct.pack(">I", 0x000007)  # track enabled/in movie/in preview
    creation_time = struct.pack(">I", 0)
    modification_time = struct.pack(">I", 0)
    track_id = struct.pack(">I", 1)
    reserved4 = struct.pack(">I", 0)
    duration = struct.pack(">I", 42000)
    reserved8 = struct.pack(">II", 0, 0)
    layer = struct.pack(">h", 0)
    alternate_group = struct.pack(">h", 0)
    volume = struct.pack(">h", 0x0100)
    reserved16 = struct.pack(">h", 0)
    matrix = struct.pack(
        ">9i", 0x00010000, 0, 0, 0, 0x00010000, 0, 0, 0, 0x40000000
    )
    width_fixed = struct.pack(">I", width << 16)
    height_fixed = struct.pack(">I", height << 16)
    payload = (
        version_flags
        + creation_time
        + modification_time
        + track_id
        + reserved4
        + duration
        + reserved8
        + layer
        + alternate_group
        + volume
        + reserved16
        + matrix
        + width_fixed
        + height_fixed
    )
    assert len(payload) == 84, f"tkhd payload should be 84 bytes, got {len(payload)}"
    return box(b"tkhd", payload)


def build_stsd(codec_fourcc: bytes) -> bytes:
    """Minimal stsd -- version/flags(4) + entry_count(4) + one sample entry
    whose own size(4)+fourCC(4) is all parseSTSDCodec ever reads
    (b[entryStart+4:entryStart+8], entryStart=8 -> b[12:16])."""
    version_flags = struct.pack(">I", 0)
    entry_count = struct.pack(">I", 1)
    sample_entry_size = struct.pack(">I", 8)  # size(4)+fourCC(4), no extra fields
    payload = version_flags + entry_count + sample_entry_size + codec_fourcc
    assert len(payload) == 16, f"stsd payload should be 16 bytes, got {len(payload)}"
    return box(b"stsd", payload)


def build_moov(width: int, height: int, codec_fourcc: bytes) -> bytes:
    mvhd = build_mvhd()
    tkhd = build_tkhd(width, height)
    stsd = build_stsd(codec_fourcc)
    stbl = box(b"stbl", stsd)
    minf = box(b"minf", stbl)
    mdia = box(b"mdia", minf)
    trak = box(b"trak", tkhd + mdia)
    return box(b"moov", mvhd + trak)


def build(total_bytes: int, width: int, height: int, codec_fourcc: bytes):
    ftyp = build_ftyp()
    free_chain = build_free_box_chain(MAX_ISOBMFF_BOXES)
    moov = build_moov(width, height, codec_fourcc)

    head_prefix = ftyp + free_chain
    mdat_header_len = 8
    fixed_len = len(head_prefix) + mdat_header_len + len(moov)
    if total_bytes <= fixed_len:
        raise ValueError("total_bytes too small for ftyp+free-chain+mdat-header+moov")

    mdat_payload_len = total_bytes - fixed_len
    mdat_payload = b"\x00" * mdat_payload_len
    mdat = struct.pack(">I4s", 8 + mdat_payload_len, b"mdat") + mdat_payload

    data = head_prefix + mdat + moov
    assert len(data) == total_bytes, f"expected {total_bytes} bytes, built {len(data)}"

    moov_offset = len(head_prefix) + len(mdat)
    return data, moov_offset


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, help="output path for the generated MP4")
    parser.add_argument("--total-bytes", type=int, default=TOTAL_BYTES)
    parser.add_argument("--width", type=int, default=1920)
    parser.add_argument("--height", type=int, default=1080)
    parser.add_argument("--codec", default="avc1", help="4-character declared codec fourCC")
    args = parser.parse_args()

    codec_fourcc = args.codec.encode("ascii")
    if len(codec_fourcc) != 4:
        print("FAIL: --codec must be exactly 4 ASCII characters", file=sys.stderr)
        return 1

    data, moov_offset = build(args.total_bytes, args.width, args.height, codec_fourcc)

    with open(args.out, "wb") as f:
        f.write(data)

    head_end = min(INSPECT_MAX_WORK_BYTES_OVERRIDE, len(data))
    tail_start = len(data) - min(2 * 1024 * 1024, INSPECT_MAX_WORK_BYTES_OVERRIDE, len(data))

    print(f"Generated pathological MP4: {args.out}")
    print(f"total_bytes={len(data)}")
    print(f"free_box_count={MAX_ISOBMFF_BOXES} (mirrors maxISOBMFFBoxes)")
    print(f"moov_offset={moov_offset} moov_size={len(build_moov(args.width, args.height, codec_fourcc))}")
    print(f"gate_head_window=[0,{head_end}) gate_tail_window=[{tail_start},{len(data)})")
    print(f"moov_within_tail_window={tail_start <= moov_offset < len(data)}")
    print(f"moov_within_head_window={moov_offset < head_end}")
    print(f"declared width={args.width} height={args.height} codec={args.codec}")
    return 0


if __name__ == "__main__":
    sys.exit(main())

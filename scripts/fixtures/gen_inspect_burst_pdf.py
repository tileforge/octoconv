#!/usr/bin/env python3
"""gen_inspect_burst_pdf.py -- deterministic pathological PDF generator for
Phase 58 (INSP-06/SC6), scripts/inspect-burst-liveproof.sh's committed
fixture inspect-burst-pathological.pdf.

WHAT THIS TARGETS -- internal/inspect/pdf.go's token-scan ceiling, not a
parser-correctness edge case (this fixture's whole family exists to stress
CEILINGS, not to find a parsing bug):

  (a) `/ObjStm` literal, once, near the head -- containsCompressionSignal
      (pdf.go's pdfCompressionSignalTokens) trips on mere PRESENCE, forcing
      every risk-token verdict in this file down to `unknown` regardless of
      what else the scan finds (ISO 32000 Sec7.5.7's object-stream
      restriction, pdf.go's own file-level doc comment). This exercises that
      downgrade path deliberately, not by accident.
  (b) tens of thousands of NEAR-MISS occurrences of a risk token
      (`/JavaScrip`, the literal `/JavaScript` token with its final byte
      cut off) -- scanTokenOccurrences (pdf.go) walks every occurrence of a
      token spelling up to its own per-token cap; a near-miss never matches
      `bytes.Contains`, so the scan runs the FULL width of the head buffer
      every time instead of stopping early on the first real hit. This is
      what "maximizes token-scan work without finding a match early" means
      concretely.
  (c) a valid `%PDF-` signature at offset 0 (convert/sniff.go's matchPDF) --
      without it the upload never reaches AnalyzePDF at all; it would be
      rejected/reclassified at the sniff step, and none of the above would
      matter.

SIZE CONTRACT -- must sit strictly above the gate's own (lowered)
INSPECT_MAX_WORK_BYTES override and strictly below INSPECT_MAX_UPLOAD_BYTES
(deploy/chart/octoconv/values-inspect-gate.yaml's four api.extraEnv
boundaries), so the request exercises the SELECTIVE/partial-report branch
(size > maxBytes -> AnalyzePDF's own `truncated` flag, pdf.go:155) rather
than either a 413 (over the upload ceiling) or a full/complete-coverage scan
(under the work ceiling, no selectivity ever exercised). See
scripts/fixtures/README.md for the exact numbers and the arithmetic that
ties this script's TARGET_TOTAL_BYTES to those two boundaries.

DETERMINISTIC -- no timestamps, no randomness. Re-running this script
produces byte-identical output; the fixture is committed precisely so no
operator ever needs to run this script to reproduce the live gate, but it
exists (per this phase's own constraint: "committed, re-runnable script...
never hand-crafted binaries whose provenance nobody can reproduce") so a
future editor CAN regenerate or recalibrate without guessing at the byte
layout by hand.

Usage:
    python3 scripts/fixtures/gen_inspect_burst_pdf.py \
        --out scripts/fixtures/inspect-burst-pathological.pdf
"""
import argparse
import sys

# TARGET_TOTAL_BYTES: 2 MiB. Above the gate overlay's INSPECT_MAX_WORK_BYTES
# (1 MiB, values-inspect-gate.yaml) so AnalyzePDF's own `truncated` branch
# fires (head window == the first 1 MiB, tail window == the last 64 KiB,
# defaultTailWindow -- pdf.go:160, NOT capped by maxBytes the way MP4's tail
# window is, see media.go's own findMoov). Far below
# INSPECT_MAX_UPLOAD_BYTES (183 MiB default, unchanged by the overlay), so
# the request never hits the pre-parse 413 path at all.
TARGET_TOTAL_BYTES = 2 * 1024 * 1024

# NEAR_MISS_TOKEN: `/JavaScrip` -- pdf.go's real risk token is `/JavaScript`
# (pdfRiskTokens, CodePDFJavaScript); dropping the final byte guarantees
# bytes.Contains (scanTokenOccurrences's own match primitive) never matches
# this repeated filler, so the per-token occurrence walk runs the entire
# scanned window on every request instead of returning after the first real
# hit.
NEAR_MISS_TOKEN = b"/JavaScrip"

# COMPRESSION_SIGNAL: pdf.go's pdfCompressionSignalTokens[0] -- present
# exactly once, near the head, so containsCompressionSignal trips and every
# risk-token verdict this file computes downgrades to `unknown`
# (ReasonCompressedObjectStreams) rather than a false `clean`.
COMPRESSION_SIGNAL = b"/ObjStm"

PDF_HEADER = b"%PDF-1.7\n"
PDF_FOOTER = b"\n%%EOF\n"


def build(total_bytes: int) -> bytes:
    if total_bytes <= len(PDF_HEADER) + len(PDF_FOOTER) + 64:
        raise ValueError("total_bytes too small to hold header+signal+footer")

    parts = [PDF_HEADER]
    # The compression signal sits right after the header -- well inside the
    # 1 MiB head window at every INSPECT_MAX_WORK_BYTES setting this project
    # has ever used (8 MiB default, 1 MiB gate override alike).
    parts.append(COMPRESSION_SIGNAL + b"\n")

    filler_budget = total_bytes - len(PDF_HEADER) - len(COMPRESSION_SIGNAL) - 1 - len(PDF_FOOTER)
    token_line = NEAR_MISS_TOKEN + b"\n"
    repeats = filler_budget // len(token_line)
    parts.append(token_line * repeats)

    remainder = filler_budget - repeats * len(token_line)
    if remainder > 0:
        parts.append(b"%" + b" " * (remainder - 1))

    parts.append(PDF_FOOTER)
    return b"".join(parts), repeats


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, help="output path for the generated PDF")
    parser.add_argument(
        "--total-bytes",
        type=int,
        default=TARGET_TOTAL_BYTES,
        help=f"total file size in bytes (default: {TARGET_TOTAL_BYTES})",
    )
    args = parser.parse_args()

    data, repeats = build(args.total_bytes)

    with open(args.out, "wb") as f:
        f.write(data)

    print(f"Generated pathological PDF: {args.out}")
    print(f"total_bytes={len(data)}")
    print(f"near_miss_token={NEAR_MISS_TOKEN!r} repeats={repeats}")
    print(f"compression_signal={COMPRESSION_SIGNAL!r} occurrences=1")
    return 0


if __name__ == "__main__":
    sys.exit(main())

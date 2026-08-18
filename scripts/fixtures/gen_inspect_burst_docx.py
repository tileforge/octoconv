#!/usr/bin/env python3
"""gen_inspect_burst_docx.py -- deterministic pathological DOCX generator
for Phase 58 (INSP-06/SC6), scripts/inspect-burst-liveproof.sh's committed
fixture inspect-burst-pathological.docx.

WHAT THIS TARGETS -- internal/inspect/container.go's central-directory and
per-part scanning ceilings, and internal/api/inspect_handlers.go's own
pre-parse structural ceiling. Stdlib `zipfile` only -- no external
dependency, unlike scripts/fixtures/gen_heavy_docx.py's `python-docx`
(Phase 28), because this fixture needs precise control over the number and
naming of central-directory entries, not realistic Word body content.

  (a) `word/document.xml` present -- convert/docsniff.go's ooxmlRootParts
      disambiguates "docx" from the five other office/epub formats SOLELY
      by this literal part name; without it SniffContainer never sets
      Format at all and this whole fixture would be reported as an
      unrecognized bare zip.

  (b) filler entries near (but under) container.go's maxZipEntriesScanned
      (4096) -- BUT bounded, by this script's own arithmetic below, to stay
      under internal/api/inspect_handlers.go's EARLIER, cheaper structural
      gate: inspectMaxZipDirEntries * zipCentralDirMinRecordLen = 4096 * 46
      = 188416 bytes of DECLARED central-directory size. That gate reads
      only the EOCD record (zero decompression) and, if tripped, returns
      zipDirectoryCeilingFamilyResult() WITHOUT ever calling
      AnalyzeContainer at all -- a real 4096-entry archive with non-empty
      filenames almost always trips it first (46 bytes/entry is the
      ABSOLUTE floor: zero-length name, zero extra, zero comment), so this
      script computes the LARGEST entry count that keeps every entry
      real (short filler names, real central-directory bytes) while
      staying under that earlier gate -- letting container.go's own scan
      genuinely run, rather than getting short-circuited before it starts.
      See scripts/fixtures/README.md for this run's actual observed
      entries/dirBytes numbers -- computed by this script, not hand-typed.

  (c) several `*.rels` candidate parts (isExternalRefCandidate: suffix
      `.rels` AND contains `_rels/`), each padded to just under
      maxRelsPartBytes (256 KiB) with `TargetMode="External"` placed near
      the very END of the part -- so whichever one archive/zip's central
      directory order presents FIRST to appendExternalReferenceResult's
      scan gets read almost to its own ceiling before the match is found
      (appendExternalReferenceResult stops at the FIRST match, so only ONE
      part is actually read per request -- placing the marker near the end
      of EVERY candidate makes this true regardless of scan order, per this
      phase's PLAN.md).

  (d) NO macro part (word/vbaProject.bin etc.) -- a macro finding matches on
      the FIRST central-directory entry that names it (findMacroPartName),
      which would short the token-scan work this fixture exists to
      maximize down to a handful of iterations.

DETERMINISTIC -- every entry's content and name is a pure function of its
index; no timestamps (ZipInfo.date_time is fixed to the epoch), no
compression-ratio randomness (ZIP_STORED throughout, so declared and actual
sizes are identical and this script's own byte-budget arithmetic is exact,
matching Go's zipCentralDirMinRecordLen assumption of zero extra/comment
fields).

Usage:
    python3 scripts/fixtures/gen_inspect_burst_docx.py \
        --out scripts/fixtures/inspect-burst-pathological.docx
"""
import argparse
import sys
import zipfile

# INSPECT_MAX_ZIP_DIR_ENTRIES / _RECORD_LEN mirror
# internal/api/inspect_handlers.go's inspectMaxZipDirEntries (4096) and
# zipCentralDirMinRecordLen (46) EXACTLY -- these are the numbers the
# earlier, cheaper structural gate uses to decide "exceeds" from the EOCD
# record alone, before AnalyzeContainer ever runs.
INSPECT_MAX_ZIP_DIR_ENTRIES = 4096
ZIP_CENTRAL_DIR_MIN_RECORD_LEN = 46
INSPECT_MAX_ZIP_DIR_BYTES = INSPECT_MAX_ZIP_DIR_ENTRIES * ZIP_CENTRAL_DIR_MIN_RECORD_LEN  # 188416

# SAFETY_MARGIN_BYTES: stay this far under INSPECT_MAX_ZIP_DIR_BYTES so the
# fixture's own declared dirBytes -- which depends on the exact filenames
# this script chooses below, not a value hand-tuned per run -- never
# accidentally crosses the gate due to an off-by-one in this arithmetic.
SAFETY_MARGIN_BYTES = 512

# RELS_PART_TARGET_BYTES: just under maxRelsPartBytes (256 KiB,
# container.go) -- large enough that io.LimitReader (the actual read
# ceiling) never truncates the External marker placed near this part's own
# end, comfortably under the ceiling so "near the end, within budget" holds
# without depending on exact ceiling arithmetic.
MAX_RELS_PART_BYTES = 256 * 1024
RELS_PART_TARGET_BYTES = MAX_RELS_PART_BYTES - 4 * 1024  # 252 KiB

RELS_CANDIDATE_NAMES = [
    "_rels/.rels",
    "word/_rels/document.xml.rels",
    "word/_rels/header1.xml.rels",
    "word/_rels/footer1.xml.rels",
]

CONTENT_TYPES_XML = (
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
    '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
    '<Default Extension="xml" ContentType="application/xml"/>'
    '<Override PartName="/word/document.xml" '
    'ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>'
    "</Types>"
).encode("utf-8")

DOCUMENT_XML = (
    '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
    '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">'
    "<w:body><w:p><w:r><w:t>"
    "inspect-burst-pathological fixture -- Phase 58 (INSP-06/SC6)"
    "</w:t></w:r></w:p></w:body></w:document>"
).encode("utf-8")

FIXED_TIME = (1980, 1, 1, 0, 0, 0)  # zipfile's own epoch floor; keeps output deterministic


def rels_part_content() -> bytes:
    """One candidate .rels part: padding up to just under the target size,
    then a single Relationship element carrying TargetMode="External" near
    the very end -- placed so the scan (bounded to maxRelsPartBytes from the
    START of the part) still reaches it before the read ceiling would cut
    it off."""
    tail = (
        '<Relationship Id="rIdExternal" '
        'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" '
        'Target="https://example.invalid/pathological" TargetMode="External"/>'
        "</Relationships>"
    ).encode("utf-8")
    header = (
        '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
    ).encode("utf-8")
    pad_unit = b"<!-- pad -->"
    budget = RELS_PART_TARGET_BYTES - len(header) - len(tail)
    if budget < 0:
        raise ValueError("RELS_PART_TARGET_BYTES too small for header+tail")
    pad = pad_unit * (budget // len(pad_unit))
    return header + pad + tail


def compute_filler_count(fixed_names: list) -> tuple:
    """Returns (filler_count, projected_dir_bytes) -- the largest filler
    entry count such that the TOTAL declared central-directory size (fixed
    real parts + N minimal filler entries) stays under
    INSPECT_MAX_ZIP_DIR_BYTES - SAFETY_MARGIN_BYTES. Filler names are the
    shortest possible unique decimal strings ("0", "1", ... ), so this
    mirrors container.go's own maxZipEntriesScanned=4096 ceiling as closely
    as a real (non-empty-name) archive can."""
    fixed_bytes = sum(ZIP_CENTRAL_DIR_MIN_RECORD_LEN + len(n.encode("utf-8")) for n in fixed_names)
    ceiling = INSPECT_MAX_ZIP_DIR_BYTES - SAFETY_MARGIN_BYTES
    remaining = ceiling - fixed_bytes
    if remaining <= 0:
        raise ValueError("fixed parts alone exceed the zip-dir ceiling budget")

    count = 0
    used = 0
    while count < INSPECT_MAX_ZIP_DIR_ENTRIES:
        name_len = len(str(count))
        cost = ZIP_CENTRAL_DIR_MIN_RECORD_LEN + name_len
        if used + cost > remaining:
            break
        used += cost
        count += 1
    return count, fixed_bytes + used


def build(out_path: str):
    fixed_names = ["[Content_Types].xml", "word/document.xml"] + RELS_CANDIDATE_NAMES
    filler_count, projected_dir_bytes = compute_filler_count(fixed_names)

    with zipfile.ZipFile(out_path, "w", compression=zipfile.ZIP_STORED) as zf:
        zi = zipfile.ZipInfo("[Content_Types].xml", date_time=FIXED_TIME)
        zf.writestr(zi, CONTENT_TYPES_XML)

        zi = zipfile.ZipInfo("word/document.xml", date_time=FIXED_TIME)
        zf.writestr(zi, DOCUMENT_XML)

        rels_content = rels_part_content()
        for name in RELS_CANDIDATE_NAMES:
            zi = zipfile.ZipInfo(name, date_time=FIXED_TIME)
            zf.writestr(zi, rels_content)

        for i in range(filler_count):
            zi = zipfile.ZipInfo(str(i), date_time=FIXED_TIME)
            zf.writestr(zi, b"x")

    return filler_count, projected_dir_bytes, len(rels_content)


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", required=True, help="output path for the generated DOCX")
    args = parser.parse_args()

    filler_count, projected_dir_bytes, rels_size = build(args.out)

    import os

    actual_size = os.path.getsize(args.out)

    with zipfile.ZipFile(args.out) as zf:
        actual_entries = len(zf.infolist())

    print(f"Generated pathological DOCX: {args.out}")
    print(f"actual_file_bytes={actual_size}")
    print(f"total_entries={actual_entries} (filler={filler_count} + fixed={actual_entries - filler_count})")
    print(f"projected_central_dir_bytes={projected_dir_bytes} ceiling={INSPECT_MAX_ZIP_DIR_BYTES}")
    print(f"rels_candidate_part_bytes={rels_size} (each of {len(RELS_CANDIDATE_NAMES)} parts)")
    return 0


if __name__ == "__main__":
    sys.exit(main())

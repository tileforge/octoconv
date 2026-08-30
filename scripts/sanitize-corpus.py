#!/usr/bin/env python3
"""sanitize-corpus.py -- deterministic generator for the Phase 60 sanitize
corpus (internal/convert/testdata/sanitize/).

WHY THIS EXISTS, AND WHY IT RUNS BEFORE THE SANITIZER

A fixture committed alongside the sanitizer proves what the sanitizer already
does. A fixture committed first proves what it MUST do. Phase 59 paid for that
distinction twice (its corpus caught a 19x overcount that every pre-existing
fixture was blind to), so Phase 60 inherits the protocol: this script and the
three files it emits land in the first commit of the phase, before a single
line of internal/convert/sanitize*.go exists.

WHERE THE PROVENANCE LIVES -- and why it is not scripts/fixtures/README.md

This project has two fixture conventions, split by consumer:

  * scripts/fixtures/  -- fixtures driven by live-gate SHELL scripts; the
    generator sits next to its output and README.md is the provenance.
  * internal/<pkg>/testdata/ -- corpora consumed by GO TESTS; PROVENANCE.md
    sits next to the files (internal/inspect/testdata/pdf/PROVENANCE.md,
    Phase 59).

This corpus is read by Go tests, so it follows the second convention: the
files and their PROVENANCE.md live under internal/convert/testdata/sanitize/.
The generator lives in scripts/ because it is phase tooling, not a committed
gate fixture. Recorded here so a future reader does not "fix" the split.

DETERMINISM

No timestamps, no randomness, no host paths inside the output bytes. ZIP
entries carry a fixed date_time. Re-running this script on any machine must
produce byte-identical files -- a corpus that cannot be compared between runs
cannot be shown to be unchanged.

SELF-CHECK

Every fixture is greped for the markers it exists to carry, and the script
EXITS NON-ZERO if any is missing. A generator that silently emits a threat
fixture carrying no threat hands the whole phase a green test on an empty
subject -- which is the single defect class this corpus is here to prevent.

Payloads are deliberately inert: an app.alert() and an `echo`. The point is
that the CONSTRUCTS are present and structurally live, not that they do
anything.

Usage:
    python3 scripts/sanitize-corpus.py [--out-dir DIR] [--check-only]
"""

import argparse
import os
import struct
import sys
import zipfile

DEFAULT_OUT = os.path.join("internal", "convert", "testdata", "sanitize")

# Fixed ZIP timestamp (2020-01-01 00:00:00). Any real clock reading here would
# make the archive differ between runs and defeat the determinism contract.
ZIP_DATE = (2020, 1, 1, 0, 0, 0)

BODY_TEXT = "Body text that must survive sanitization intact."


# ---------------------------------------------------------------------------
# threat_all_classes.pdf
# ---------------------------------------------------------------------------

def build_threat_pdf() -> bytes:
    """A one-page PDF carrying every SANI-01 threat class plus /Info metadata.

    Reproduces the fixture 60-RESEARCH.md Q1 used to measure R1 -- the table
    showing that a single `gs -sDEVICE=pdfwrite` pass leaves three of the seven
    classes alive. That measurement is the entire reason the pipeline has two
    steps, so its subject has to be a committed file rather than a /tmp path
    from a session that has since ended.

    Object 6 carries /Type /EmbeddedFile, which the research fixture did not.
    That makes this file a strictly harder target (two occurrences of the
    literal "EmbeddedFile" instead of one, one of them on a real stream object)
    and it is why the counts recorded in PROVENANCE.md are re-measured against
    THIS file rather than transcribed from the research document.
    """
    js = b"app.alert('benign fixture payload');"
    attachment = b"#!/bin/sh\necho 'benign fixture payload'\n"
    content = (
        "BT /F1 14 Tf 72 700 Td (" + BODY_TEXT + ") Tj ET\n"
    ).encode("ascii")

    objs = {}

    objs[1] = (
        b"<< /Type /Catalog /Pages 2 0 R\n"
        b"   /OpenAction 7 0 R\n"
        b"   /AA << /O 7 0 R >>\n"
        b"   /Names << /JavaScript << /Names [(fixture) 8 0 R] >>\n"
        b"             /EmbeddedFiles << /Names [(payload.sh) 9 0 R] >> >>\n"
        b">>"
    )
    objs[2] = b"<< /Type /Pages /Kids [3 0 R] /Count 1 >>"
    objs[3] = (
        b"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792]\n"
        b"   /Resources << /Font << /F1 5 0 R >> >>\n"
        b"   /Contents 4 0 R /Annots [10 0 R 11 0 R] >>"
    )
    objs[4] = (
        b"<< /Length " + str(len(content)).encode() + b" >>\nstream\n"
        + content + b"endstream"
    )
    objs[5] = b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>"
    objs[6] = (
        b"<< /Type /EmbeddedFile /Subtype /application#2Fx-sh /Length "
        + str(len(attachment)).encode() + b" >>\nstream\n"
        + attachment + b"endstream"
    )
    objs[7] = b"<< /Type /Action /S /JavaScript /JS 12 0 R >>"
    objs[8] = b"<< /Type /Action /S /JavaScript /JS 12 0 R >>"
    objs[9] = (
        b"<< /Type /Filespec /F (payload.sh) /UF (payload.sh)\n"
        b"   /EF << /F 6 0 R >> >>"
    )
    objs[10] = (
        b"<< /Type /Annot /Subtype /Link /Rect [72 640 300 660] /Border [0 0 0]\n"
        b"   /A << /Type /Action /S /URI /URI (http://evil.example/beacon) >> >>"
    )
    objs[11] = (
        b"<< /Type /Annot /Subtype /Link /Rect [72 600 300 620] /Border [0 0 0]\n"
        b"   /A << /Type /Action /S /Launch /F (payload.sh) >> >>"
    )
    objs[12] = (
        b"<< /Length " + str(len(js)).encode() + b" >>\nstream\n"
        + js + b"\nendstream"
    )
    objs[13] = (
        b"<< /Title (Secret Internal Title)\n"
        b"   /Author (jane.doe@corp.example)\n"
        b"   /Producer (octoconv sanitize corpus)\n"
        b"   /Creator (octoconv sanitize corpus) >>"
    )

    out = bytearray(b"%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
    offsets = {}
    for num in sorted(objs):
        offsets[num] = len(out)
        out += str(num).encode() + b" 0 obj\n" + objs[num] + b"\nendobj\n"

    xref_off = len(out)
    count = max(objs) + 1
    out += b"xref\n0 " + str(count).encode() + b"\n"
    out += b"0000000000 65535 f \n"
    for num in range(1, count):
        out += ("%010d 00000 n \n" % offsets[num]).encode("ascii")
    out += (
        b"trailer\n<< /Size " + str(count).encode()
        + b" /Root 1 0 R /Info 13 0 R >>\nstartxref\n"
        + str(xref_off).encode() + b"\n%%EOF\n"
    )
    return bytes(out)


# ---------------------------------------------------------------------------
# macro_enabled.docm
# ---------------------------------------------------------------------------

# Minimal OLE compound-file header: the 8-byte signature every CFB container
# starts with (ISO/IEC 29500-4 / [MS-CFB] 2.2). The verifier D-04 requires must
# recognise the OLE container by looking INSIDE the part, so the fixture has to
# carry a real signature -- a part named vbaProject.bin holding arbitrary bytes
# would let a name-only checker pass and prove nothing.
OLE_SIGNATURE = b"\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"

CT_MACRO = "application/vnd.ms-word.document.macroEnabled.main+xml"
CT_PLAIN = "application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"


def build_vba_project() -> bytes:
    """A vbaProject.bin carrying the OLE signature and a p-code marker.

    The p-code marker matters for SANI-01's own wording: VBA stomping keeps a
    benign source stream and hides the real behaviour in compiled p-code, so a
    sanitizer that reads module source and declares the document clean is
    defeated by construction. D-05 answers that by deleting the whole part --
    and this fixture is what makes the difference between "deleted the source"
    and "deleted the part" observable.
    """
    header = bytearray(OLE_SIGNATURE)
    header += b"\x00" * 16                     # CLSID
    header += struct.pack("<HH", 0x003E, 0x0003)  # minor/major version
    header += struct.pack("<H", 0xFFFE)        # little-endian byte order mark
    header += struct.pack("<HH", 9, 6)         # sector shift, mini sector shift
    header += b"\x00" * (512 - len(header))

    body = bytearray()
    body += b"Attribute VB_Name = \"Module1\"\r\n"
    body += b"Sub AutoOpen()\r\n  ' benign fixture payload\r\nEnd Sub\r\n"
    # The compiled-p-code marker: _VBA_PROJECT is the stream name that carries
    # p-code in a real project, and its presence is what a stomping-aware check
    # has to look for.
    body += b"_VBA_PROJECT\x00"
    body += b"\x00" * (512 - (len(body) % 512))
    return bytes(header) + bytes(body)


def build_macro_docm() -> bytes:
    content_types = (
        '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        '<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">'
        '<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>'
        '<Default Extension="xml" ContentType="application/xml"/>'
        '<Default Extension="bin" ContentType="application/vnd.ms-office.vbaProject"/>'
        f'<Override PartName="/word/document.xml" ContentType="{CT_MACRO}"/>'
        '<Override PartName="/word/vbaProject.bin" ContentType="application/vnd.ms-office.vbaProject"/>'
        '</Types>'
    )
    root_rels = (
        '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
        '<Relationship Id="rId1" '
        'Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" '
        'Target="word/document.xml"/>'
        '</Relationships>'
    )
    doc_rels = (
        '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        '<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">'
        '<Relationship Id="rId1" '
        'Type="http://schemas.microsoft.com/office/2006/relationships/vbaProject" '
        'Target="vbaProject.bin"/>'
        '</Relationships>'
    )
    document = (
        '<?xml version="1.0" encoding="UTF-8" standalone="yes"?>'
        '<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">'
        f'<w:body><w:p><w:r><w:t>{BODY_TEXT}</w:t></w:r></w:p></w:body>'
        '</w:document>'
    )

    parts = [
        ("[Content_Types].xml", content_types.encode("utf-8")),
        ("_rels/.rels", root_rels.encode("utf-8")),
        ("word/document.xml", document.encode("utf-8")),
        ("word/_rels/document.xml.rels", doc_rels.encode("utf-8")),
        ("word/vbaProject.bin", build_vba_project()),
    ]

    import io
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        for name, data in parts:
            info = zipfile.ZipInfo(name, date_time=ZIP_DATE)
            info.compress_type = zipfile.ZIP_DEFLATED
            info.external_attr = 0o600 << 16
            info.create_system = 0
            zf.writestr(info, data)
    return buf.getvalue()


# ---------------------------------------------------------------------------
# exif_gps.jpg
# ---------------------------------------------------------------------------

def build_exif_jpeg() -> bytes:
    """A minimal JPEG carrying an APP1/Exif segment with GPS and camera model.

    Built byte by byte rather than through an imaging library: the corpus has a
    zero-new-dependency constraint, and hand-assembling the IFD is what makes
    every offset in PROVENANCE.md checkable. The image data itself is a 1x1
    grey pixel -- SANI-03 is about the metadata, not the picture.
    """
    def ifd_entry(tag, typ, count, value):
        return struct.pack(">HHI", tag, typ, count) + value

    # --- GPS IFD: latitude/longitude as three rationals each (deg/min/sec) ---
    gps_values = bytearray()
    gps_value_base = 0  # patched once the IFD size is known

    def rational(num, den):
        return struct.pack(">II", num, den)

    lat = rational(51, 1) + rational(30, 1) + rational(0, 1)
    lon = rational(0, 1) + rational(7, 1) + rational(0, 1)

    gps_entries = []
    gps_count = 4
    # entry block = 2 (count) + 12*n + 4 (next-IFD offset)
    gps_block_len = 2 + 12 * gps_count + 4
    gps_value_base = gps_block_len

    gps_entries.append(ifd_entry(0x0001, 2, 2, b"N\x00\x00\x00"))  # GPSLatitudeRef
    gps_entries.append(ifd_entry(0x0002, 5, 3, struct.pack(">I", gps_value_base + len(gps_values))))
    gps_values += lat
    gps_entries.append(ifd_entry(0x0003, 2, 2, b"W\x00\x00\x00"))  # GPSLongitudeRef
    gps_entries.append(ifd_entry(0x0004, 5, 3, struct.pack(">I", gps_value_base + len(gps_values))))
    gps_values += lon

    gps_ifd = struct.pack(">H", gps_count) + b"".join(gps_entries) + struct.pack(">I", 0)
    gps_blob = gps_ifd + bytes(gps_values)

    # --- IFD0: Make, Model, and a pointer to the GPS IFD ---
    make = b"OctoConvCam\x00"
    model = b"FixtureModel X1\x00"

    ifd0_count = 3
    ifd0_block_len = 2 + 12 * ifd0_count + 4
    ifd0_value_base = 8 + ifd0_block_len   # 8 = TIFF header length

    ifd0_values = bytearray()
    e_make = ifd_entry(0x010F, 2, len(make), struct.pack(">I", ifd0_value_base + len(ifd0_values)))
    ifd0_values += make
    e_model = ifd_entry(0x0110, 2, len(model), struct.pack(">I", ifd0_value_base + len(ifd0_values)))
    ifd0_values += model
    gps_offset = ifd0_value_base + len(ifd0_values)
    e_gps = ifd_entry(0x8825, 4, 1, struct.pack(">I", gps_offset))

    ifd0 = struct.pack(">H", ifd0_count) + e_make + e_model + e_gps + struct.pack(">I", 0)
    tiff = b"MM\x00\x2a" + struct.pack(">I", 8) + ifd0 + bytes(ifd0_values) + gps_blob

    exif = b"Exif\x00\x00" + tiff
    app1 = b"\xff\xe1" + struct.pack(">H", len(exif) + 2) + exif

    # Minimal baseline JPEG around it: SOI, APP1, DQT, SOF0, DHT, SOS, EOI.
    qt = bytes([16] * 64)
    dqt = b"\xff\xdb" + struct.pack(">H", 67) + b"\x00" + qt
    sof0 = b"\xff\xc0" + struct.pack(">H", 11) + b"\x08" + struct.pack(">HH", 1, 1) + b"\x01\x11\x00"
    # One trivial Huffman table (all codes length 1) for both DC and AC.
    def dht(cls_id, counts, symbols):
        payload = bytes([cls_id]) + bytes(counts) + bytes(symbols)
        return b"\xff\xc4" + struct.pack(">H", len(payload) + 2) + payload
    dht_dc = dht(0x00, [1] + [0] * 15, [0x00])
    dht_ac = dht(0x10, [1] + [0] * 15, [0x00])
    sos = b"\xff\xda" + struct.pack(">H", 8) + b"\x01\x01\x00\x00\x3f\x00"
    scan = b"\x00"
    return b"\xff\xd8" + app1 + dqt + sof0 + dht_dc + dht_ac + sos + scan + b"\xff\xd9"


# ---------------------------------------------------------------------------
# self-check
# ---------------------------------------------------------------------------

# Each fixture's contract, as bytes that must be findable in the file it is
# generated into. A generator that emits a threat fixture with no threat in it
# hands every downstream test a green result on an empty subject, so a missing
# marker is a hard failure, not a warning.
EXPECTED = {
    "threat_all_classes.pdf": [
        b"/OpenAction", b"/AA", b"/JavaScript", b"EmbeddedFile",
        b"/URI", b"/Launch", b"evil.example",
        b"Secret Internal Title", b"jane.doe@corp.example",
        BODY_TEXT.encode(),
    ],
    # Only the central-directory NAME is greppable in a deflate archive's raw
    # bytes; every content marker for this fixture is checked against the
    # decompressed part below. The first run of this script asserted
    # b"macroEnabled" here and failed -- the self-check catching its own
    # author's mistake is the reason it exists.
    "macro_enabled.docm": [
        b"word/vbaProject.bin",
    ],
    "exif_gps.jpg": [
        b"Exif\x00\x00", b"OctoConvCam", b"FixtureModel X1",
    ],
}

# macro_enabled.docm is a deflate archive, so its part CONTENTS are not
# greppable in the raw bytes -- only the central-directory names are. These
# markers are checked against the decompressed parts instead.
EXPECTED_IN_ZIP_PARTS = {
    "macro_enabled.docm": [
        ("word/vbaProject.bin", OLE_SIGNATURE),
        ("word/vbaProject.bin", b"_VBA_PROJECT"),
        ("[Content_Types].xml", CT_MACRO.encode()),
        ("[Content_Types].xml", b'<Default Extension="bin"'),
        ("word/_rels/document.xml.rels", b"vbaProject.bin"),
    ],
}


def check(out_dir: str) -> int:
    failures = []
    for name, markers in EXPECTED.items():
        path = os.path.join(out_dir, name)
        if not os.path.exists(path):
            failures.append(f"{name}: missing")
            continue
        blob = open(path, "rb").read()
        for m in markers:
            if m not in blob:
                failures.append(f"{name}: marker {m!r} absent from raw bytes")

    for name, entries in EXPECTED_IN_ZIP_PARTS.items():
        path = os.path.join(out_dir, name)
        if not os.path.exists(path):
            continue
        with zipfile.ZipFile(path) as zf:
            names = set(zf.namelist())
            for part, marker in entries:
                if part not in names:
                    failures.append(f"{name}: part {part} absent")
                    continue
                if marker not in zf.read(part):
                    failures.append(f"{name}: marker {marker!r} absent from {part}")

    if failures:
        print("SELF-CHECK FAILED:", file=sys.stderr)
        for f in failures:
            print("  " + f, file=sys.stderr)
        return 1

    for name in sorted(EXPECTED):
        path = os.path.join(out_dir, name)
        print(f"  {name}: {os.path.getsize(path)} bytes")
    print("self-check passed: every fixture carries the markers it exists to carry")
    return 0


def main() -> int:
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--out-dir", default=DEFAULT_OUT)
    ap.add_argument("--check-only", action="store_true",
                    help="verify existing fixtures without rewriting them")
    args = ap.parse_args()

    if not args.check_only:
        os.makedirs(args.out_dir, exist_ok=True)
        for name, blob in (
            ("threat_all_classes.pdf", build_threat_pdf()),
            ("macro_enabled.docm", build_macro_docm()),
            ("exif_gps.jpg", build_exif_jpeg()),
        ):
            with open(os.path.join(args.out_dir, name), "wb") as fh:
                fh.write(blob)
        print(f"generated sanitize corpus into {args.out_dir}")

    return check(args.out_dir)


if __name__ == "__main__":
    sys.exit(main())

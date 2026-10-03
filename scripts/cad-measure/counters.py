#!/usr/bin/env python3
"""counters.py -- textual size counters for CAD files, independent of OCP and ezdxf.

Usage:
    counters.py [--names] FILE...

One line per file: CNT,name=..,sha=..,fmt=..,bytes=..,<fields of the format>.
name= (and the STEP originating system) is printed only with --names, so that a run over a
private directory emits sha256, format, sizes and counters and nothing else.
"""

import argparse
import hashlib
import mmap
import os
import re
import sys

STEP_ENTITY = re.compile(rb"(?:^|(?<=;))[ \t]*#\d+[ \t]*=", re.M)
STEP_NAUO = re.compile(rb"(?<![A-Z_0-9])NEXT_ASSEMBLY_USAGE_OCCURRENCE[ \t\r\n]*\(")
STEP_PRODUCT = re.compile(rb"(?<![A-Z_0-9])PRODUCT[ \t\r\n]*\(")
STEP_MSB = re.compile(rb"(?<![A-Z_0-9])MANIFOLD_SOLID_BREP[ \t\r\n]*\(")
STEP_GUAC = re.compile(rb"GLOBAL_UNIT_ASSIGNED_CONTEXT[ \t\r\n]*\(")
STEP_STRING = re.compile(rb"'((?:[^']|'')*)'")
NON_ASCII = bytes(range(128, 256))


def sha256_of(path):
    h = hashlib.sha256()
    with open(path, "rb") as fh:
        for block in iter(lambda: fh.read(1 << 20), b""):
            h.update(block)
    return h.hexdigest()


def safe(value, limit=48):
    text = value.decode("latin-1") if isinstance(value, bytes) else str(value)
    text = re.sub(r"[^A-Za-z0-9._+-]+", "_", text).strip("_")
    return text[:limit] or "-"


def sniff(path):
    ext = os.path.splitext(path)[1].lower()
    if ext in (".step", ".stp"):
        return "step"
    if ext in (".iges", ".igs"):
        return "iges"
    if ext == ".stl":
        return "stl"
    if ext == ".dxf":
        return "dxf"
    with open(path, "rb") as fh:
        head = fh.read(4096)
    if b"ISO-10303-21" in head:
        return "step"
    if len(head) >= 80 and head[72:73] == b"S":
        return "iges"
    if head.lstrip()[:3] == b"  0" or head.lstrip()[:1] == b"0":
        return "dxf"
    if head[:5] == b"solid":
        return "stl"
    return "unknown"


def closed(value, pattern, strict):
    text = safe(value)
    if not strict or re.fullmatch(pattern, text):
        return text
    return "other"


STEP_UNIT_NAMES = r"(?i)(INCH|MILLIMETRE|MILLIMETER|CENTIMETRE|METRE|FOOT|MILE|MICROMETRE|MICROINCH|MIL)"
IGES_UNIT_NAMES = r"(IN|INCH|MM|FT|MI|M|KM|MIL|UM|CM|UIN)"


def mapped(path):
    size = os.path.getsize(path)
    if size == 0:
        return None
    fh = open(path, "rb")
    return fh, mmap.mmap(fh.fileno(), 0, access=mmap.ACCESS_READ)


def count_token(mm, token):
    return sum(1 for _ in re.finditer(re.escape(token), mm))


def step_entity_text(mm, number):
    pattern = re.compile(rb"(?:^|(?<=;))[ \t]*#%d[ \t]*=([^;]*);" % number, re.M)
    match = pattern.search(mm)
    return match.group(1) if match else b""


def step_unit_text(mm, strict):
    match = STEP_GUAC.search(mm)
    if not match:
        return "none"
    end = mm.find(b";", match.end())
    refs = [int(r) for r in re.findall(rb"#(\d+)", mm[match.end():end])]
    for ref in refs:
        text = step_entity_text(mm, ref)
        if b"LENGTH_UNIT" not in text:
            continue
        cbu = re.search(rb"CONVERSION_BASED_UNIT[ \t\r\n]*\([ \t\r\n]*'([^']*)'", text)
        if cbu:
            return "CBU:" + closed(cbu.group(1), STEP_UNIT_NAMES, strict)
        si = re.search(rb"SI_UNIT[ \t\r\n]*\(([^,)]*),([^)]*)\)", text)
        if si:
            prefix = si.group(1).strip(b" .$\r\n\t")
            unit = si.group(2).strip(b" .\r\n\t")
            return "SI:" + closed((prefix + b"_" if prefix else b"") + unit, r"(MILLI_|CENTI_|MICRO_|KILO_)?METRE", strict)
        return "other"
    return "unresolved"


def step_header(mm):
    head = mm[: min(len(mm), 1 << 20)]
    end = head.find(b"DATA;")
    if end >= 0:
        head = head[:end]
    match = re.search(rb"FILE_NAME[ \t\r\n]*\(", head)
    if not match:
        return "-"
    stmt_end = head.find(b";", match.end())
    strings = [s.group(1) for s in STEP_STRING.finditer(head[match.end():stmt_end])]
    if len(strings) < 3:
        return "-"
    return safe(strings[-2])


def count_step(path, names):
    pair = mapped(path)
    if pair is None:
        return ["entities=0", "nauo=0", "products=0", "msb=0", "unit_text=none", "nonascii_bytes=0", "x2_escapes=0"]
    fh, mm = pair
    try:
        data_at = mm.find(b"DATA;")
        start = data_at if data_at >= 0 else 0
        entities = sum(1 for _ in STEP_ENTITY.finditer(mm, start))
        nauo = sum(1 for _ in STEP_NAUO.finditer(mm, start))
        products = sum(1 for _ in STEP_PRODUCT.finditer(mm, start))
        msb = sum(1 for _ in STEP_MSB.finditer(mm, start))
        fields = [
            "entities=%d" % entities,
            "nauo=%d" % nauo,
            "products=%d" % products,
            "msb=%d" % msb,
            "unit_text=%s" % step_unit_text(mm, not names),
        ]
        if names:
            fields.append("originating_system=%s" % step_header(mm))
        nonascii = 0
        step = 1 << 24
        for off in range(0, len(mm), step):
            block = mm[off:off + step]
            nonascii += len(block) - len(block.translate(None, NON_ASCII))
        fields.append("nonascii_bytes=%d" % nonascii)
        fields.append("x2_escapes=%d" % (count_token(mm, b"\\X2\\") + count_token(mm, b"\\X4\\")))
        return fields
    finally:
        mm.close()
        fh.close()


def iges_records(raw):
    if b"\n" in raw or b"\r" in raw:
        lines = [ln.rstrip(b"\r") for ln in raw.split(b"\n")]
        return [ln for ln in lines if ln]
    if len(raw) % 80 == 0:
        return [raw[i:i + 80] for i in range(0, len(raw), 80)]
    return []


def iges_params(text):
    pd, rd = ",", ";"
    out = []
    pos = 0
    if text.startswith("1H") and len(text) > 3:
        pd = text[2]
        out.append(pd)
        pos = 3
        if text[pos:pos + 1] == pd:
            pos += 1
    elif text.startswith(","):
        out.append("")
        pos = 1
    text_len = len(text)
    while pos < text_len and text[pos] == " ":
        pos += 1
    if len(out) == 1:
        if text[pos:pos + 2] == "1H" and pos + 2 < text_len:
            rd = text[pos + 2]
            out.append(rd)
            pos += 3
        elif text[pos:pos + 1] == pd:
            out.append("")
        if text[pos:pos + 1] == pd:
            pos += 1
    while pos < text_len:
        while pos < text_len and text[pos] == " ":
            pos += 1
        m = re.match(r"(\d+)H", text[pos:])
        if m:
            n = int(m.group(1))
            start = pos + len(m.group(0))
            out.append(text[start:start + n])
            pos = start + n
            if text[pos:pos + 1] == rd:
                break
            pos += 1
            continue
        stops = [i for i in (text.find(pd, pos), text.find(rd, pos)) if i >= 0]
        stop = min(stops) if stops else text_len
        out.append(text[pos:stop])
        if stop < text_len and text[stop] == rd:
            break
        pos = stop + 1
    return out


def count_iges(path, names):
    with open(path, "rb") as fh:
        raw = fh.read()
    records = iges_records(raw)
    newline_free = 0 if (b"\n" in raw or b"\r" in raw) else 1
    hist = {}
    g_text = ""
    for rec in records:
        if len(rec) < 73:
            continue
        section = rec[72:73]
        if section == b"G":
            g_text += rec[:72].decode("latin-1")
    d_lines = [r for r in records if len(r) >= 73 and r[72:73] == b"D"]
    for i in range(0, len(d_lines), 2):
        try:
            etype = int(d_lines[i][:8])
        except ValueError:
            continue
        hist[etype] = hist.get(etype, 0) + 1
    params = iges_params(g_text.rstrip())
    unit_flag = closed(params[13].strip(), r"[0-9]{1,2}", not names) if len(params) > 13 and params[13].strip() else "-"
    unit_name = closed(params[14].strip(), IGES_UNIT_NAMES, not names) if len(params) > 14 and params[14].strip() else "-"
    types = ";".join("%d:%d" % (t, hist[t]) for t in sorted(hist)) or "-"
    has_solid = 1 if (186 in hist or 514 in hist) else 0
    return [
        "types=%s" % types,
        "has_solid=%d" % has_solid,
        "unit_flag=%s" % unit_flag,
        "unit_name=%s" % unit_name,
        "newline_free=%d" % newline_free,
    ]


def count_stl(path):
    size = os.path.getsize(path)
    with open(path, "rb") as fh:
        head = fh.read(84)
    starts_solid = 1 if head[:5] == b"solid" else 0
    if size >= 84:
        n = int.from_bytes(head[80:84], "little")
        if size == 84 + 50 * n:
            return ["kind=binary", "triangles=%d" % n, "header_starts_solid=%d" % starts_solid]
    pair = mapped(path)
    if pair is None:
        return ["kind=unknown", "triangles=0", "header_starts_solid=0"]
    fh, mm = pair
    try:
        if head[:5] == b"solid":
            return ["kind=ascii", "triangles=%d" % count_token(mm, b"facet normal"), "header_starts_solid=1"]
        return ["kind=unknown", "triangles=0", "header_starts_solid=%d" % starts_solid]
    finally:
        mm.close()
        fh.close()


def count_dxf(path, names):
    acadver = codepage = insunits = "-"
    counts = {"INSERT": 0, "DIMENSION": 0, "HATCH": 0, "BLOCK": 0}
    minsert = 0
    entities = 0
    has_999 = 0
    parse_error = 0
    section = ""
    cur = ""
    cur_cols = 1
    cur_rows = 1
    var = ""

    def close_entity():
        nonlocal minsert
        if cur == "INSERT" and (cur_cols > 1 or cur_rows > 1):
            minsert += 1

    with open(path, "rb") as fh:
        it = iter(fh)
        for raw_code in it:
            raw_val = next(it, b"")
            try:
                code = int(raw_code.strip())
            except ValueError:
                parse_error = 1
                continue
            val = raw_val.strip()
            if code == 999:
                has_999 = 1
                continue
            if code == 0:
                close_entity()
                cur = val.decode("latin-1")
                cur_cols = cur_rows = 1
                if cur == "SECTION":
                    section = "?"
                elif cur in ("ENDSEC", "EOF"):
                    section = ""
                if cur in counts:
                    counts[cur] += 1
                if section == "ENTITIES" and cur not in ("ENDSEC", "SEQEND", "VERTEX", "SECTION"):
                    entities += 1
                continue
            if code == 2 and cur == "SECTION" and section == "?":
                section = val.decode("latin-1")
                continue
            if section == "HEADER":
                if code == 9:
                    var = val.decode("latin-1")
                elif var == "$ACADVER" and code == 1:
                    acadver = closed(val, r"AC[0-9]{4}", not names)
                elif var == "$DWGCODEPAGE" and code == 3:
                    codepage = closed(val, r"(ANSI_[0-9]{3,4}|DOS[0-9]{3}|UTF-8)", not names)
                elif var == "$INSUNITS" and code == 70:
                    insunits = closed(val, r"[0-9]{1,2}", not names)
            if cur == "INSERT":
                try:
                    if code == 70:
                        cur_cols = int(val)
                    elif code == 71:
                        cur_rows = int(val)
                except ValueError:
                    parse_error = 1
        close_entity()
    return [
        "acadver=%s" % acadver,
        "codepage=%s" % codepage,
        "insunits=%s" % insunits,
        "insert=%d" % counts["INSERT"],
        "minsert=%d" % minsert,
        "blocks=%d" % counts["BLOCK"],
        "dimension=%d" % counts["DIMENSION"],
        "hatch=%d" % counts["HATCH"],
        "entities=%d" % entities,
        "has_999=%d" % has_999,
        "parse_error=%d" % parse_error,
    ]


def describe(path, names):
    fmt = sniff(path)
    head = ["CNT"]
    if names:
        head.append("name=%s" % os.path.basename(path))
    head.append("sha=%s" % sha256_of(path))
    head.append("fmt=%s" % fmt)
    head.append("bytes=%d" % os.path.getsize(path))
    if fmt == "step":
        fields = count_step(path, names)
    elif fmt == "iges":
        fields = count_iges(path, names)
    elif fmt == "stl":
        fields = count_stl(path)
    elif fmt == "dxf":
        fields = count_dxf(path, names)
    else:
        fields = []
    return ",".join(head + fields)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--names", action="store_true")
    ap.add_argument("files", nargs="+")
    args = ap.parse_args()
    rc = 0
    for path in args.files:
        try:
            print(describe(path, args.names), flush=True)
        except OSError as exc:
            print("CNT,error=%s" % safe(exc.__class__.__name__), flush=True)
            rc = 1
    return rc


if __name__ == "__main__":
    sys.exit(main())

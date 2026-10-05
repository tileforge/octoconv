#!/usr/bin/env python3
import argparse
import json
import math
import re
import struct
import sys

RECORD = struct.Struct("<12fH")
BLOCK = 100000

STRING = re.compile(r"'(?:[^']|'')*'")
VERTEX = re.compile(r"^#(\d+)\s*=\s*VERTEX_POINT\s*\(\s*'(?:[^']|'')*'\s*,\s*#(\d+)\s*\)$", re.S)
POINT = re.compile(r"^#(\d+)\s*=\s*CARTESIAN_POINT\s*\(\s*'(?:[^']|'')*'\s*,\s*\(([^()]*)\)\s*\)$", re.S)
CONVERSION = re.compile(r"CONVERSION_BASED_UNIT\s*\(\s*'(?:[^']|'')*'\s*,\s*#(\d+)\s*\)", re.S)
MEASURE = re.compile(r"^#\d+\s*=\s*(?:LENGTH_)?MEASURE_WITH_UNIT\s*\(\s*LENGTH_MEASURE\s*\(\s*([^()\s]+)\s*\)\s*,\s*#(\d+)\s*\)$", re.S)
SI_LENGTH = re.compile(r"SI_UNIT\s*\(\s*(?:\.([A-Z]+)\.|\$)\s*,\s*\.METRE\.\s*\)")
STATEMENT_ID = re.compile(r"^#(\d+)\s*=")

PREFIX_TO_MM = {"MILLI": 1.0, "CENTI": 10.0, "": 1000.0}


class JudgeError(Exception):
    pass


def read_stl(path):
    with open(path, "rb") as fh:
        data = fh.read(84)
        if len(data) < 84:
            raise JudgeError("short_header")
        count = struct.unpack("<I", data[80:84])[0]
        fh.seek(0, 2)
        size = fh.tell()
        if size != 84 + 50 * count:
            raise JudgeError("size_mismatch")
        if count == 0:
            raise JudgeError("no_triangles")
        fh.seek(84)
        lo = [math.inf] * 3
        hi = [-math.inf] * 3
        remaining = count
        while remaining:
            take = min(remaining, BLOCK)
            chunk = fh.read(50 * take)
            if len(chunk) != 50 * take:
                raise JudgeError("short_read")
            for rec in RECORD.iter_unpack(chunk):
                for v in range(3):
                    for axis in range(3):
                        x = rec[3 + 3 * v + axis]
                        if not math.isfinite(x):
                            raise JudgeError("non_finite_vertex")
                        if x < lo[axis]:
                            lo[axis] = x
                        if x > hi[axis]:
                            hi[axis] = x
            remaining -= take
    return count, lo, hi


def diag_of(lo, hi):
    return math.sqrt(sum((h - l) ** 2 for l, h in zip(lo, hi)))


def last_verdict(path):
    found = None
    with open(path, "rb") as fh:
        for line in fh:
            if line.startswith(b"CADVERDICT "):
                found = line[len(b"CADVERDICT "):]
    if found is None:
        raise JudgeError("no_verdict_line")
    try:
        return json.loads(found.decode("ascii"))
    except ValueError:
        raise JudgeError("verdict_unparsable")


def step_statements(path):
    with open(path, "rb") as fh:
        text = fh.read().decode("latin-1")
    at = text.find("DATA;")
    if at < 0:
        raise JudgeError("no_data_section")
    text = text[at + len("DATA;"):]
    out = []
    pos = 0
    start = 0
    while pos < len(text):
        if text[pos] == "'":
            m = STRING.match(text, pos)
            if m is None:
                raise JudgeError("unterminated_string")
            pos = m.end()
            continue
        if text[pos] == ";":
            out.append(text[start:pos].strip())
            start = pos + 1
        pos += 1
    return [s for s in out if s.startswith("#")]


def length_factor(statements, by_id):
    factors = set()
    for stmt in statements:
        if "LENGTH_UNIT" not in stmt:
            continue
        m = CONVERSION.search(stmt)
        if m is None:
            continue
        target = by_id.get(m.group(1))
        if target is None:
            raise JudgeError("conversion_target_missing")
        mm = MEASURE.match(target)
        if mm is None:
            raise JudgeError("conversion_not_a_length_measure")
        unit = by_id.get(mm.group(2))
        si = SI_LENGTH.search(unit) if unit else None
        if si is None or si.group(1) != "MILLI":
            raise JudgeError("conversion_base_not_millimetre")
        factors.add(float(mm.group(1)))
    if factors:
        if len(factors) != 1:
            raise JudgeError("conflicting_length_units")
        return factors.pop()
    for stmt in statements:
        if "LENGTH_UNIT" in stmt:
            si = SI_LENGTH.search(stmt)
            if si is None:
                raise JudgeError("unknown_length_unit")
            factors.add(PREFIX_TO_MM.get(si.group(1) or "", None))
    if factors == {1.0}:
        return 1.0
    raise JudgeError("unsupported_length_unit")


def step_expectation(path):
    statements = step_statements(path)
    by_id = {}
    for stmt in statements:
        m = STATEMENT_ID.match(stmt)
        if m:
            by_id[m.group(1)] = stmt
    points = {}
    refs = []
    for stmt in statements:
        m = POINT.match(stmt)
        if m:
            points[m.group(1)] = m.group(2)
            continue
        m = VERTEX.match(stmt)
        if m:
            refs.append(m.group(2))
    if not refs:
        raise JudgeError("no_vertex_points")
    lo = [math.inf] * 3
    hi = [-math.inf] * 3
    for ref in refs:
        raw = points.get(ref)
        if raw is None:
            raise JudgeError("vertex_point_missing")
        coords = [float(c) for c in raw.split(",")]
        if len(coords) != 3 or not all(math.isfinite(c) for c in coords):
            raise JudgeError("vertex_point_bad")
        for axis in range(3):
            lo[axis] = min(lo[axis], coords[axis])
            hi[axis] = max(hi[axis], coords[axis])
    return diag_of(lo, hi), length_factor(statements, by_id)


def judge(args):
    fields = {"stl": args.stl}
    try:
        count, lo, hi = read_stl(args.stl)
        diag = diag_of(lo, hi)
        extents = sorted(h - l for l, h in zip(lo, hi))
        fields["triangles"] = count
        fields["diag"] = "%.6f" % diag
        fields["extents"] = ":".join("%.6f" % e for e in extents)
        if args.verdict_log:
            verdict = last_verdict(args.verdict_log)
            if verdict.get("verdict") != "ok" or verdict.get("triangles") != count:
                raise JudgeError("verdict_disagrees")
        if args.expect_triangles is not None and count != args.expect_triangles:
            raise JudgeError("triangle_count")
        if args.expect_extents:
            want = sorted(float(x) for x in args.expect_extents.split(","))
            if len(want) != 3 or any(abs(a - b) > 1e-3 for a, b in zip(extents, want)):
                raise JudgeError("extents")
        if args.step_source:
            vdiag, factor = step_expectation(args.step_source)
            fields["vertex_diag"] = "%.6f" % vdiag
            fields["factor"] = factor
            expected = vdiag * factor
            if not (expected * 0.999 <= diag <= expected * 1.10):
                raise JudgeError("diag_outside_expected_box")
            if factor != 1.0 and 0.9 * vdiag <= diag <= 1.1 * vdiag:
                raise JudgeError("unscaled_signature")
    except JudgeError as exc:
        fields["result"] = "fail"
        fields["reason"] = str(exc)
        return fields, 1
    except OSError as exc:
        fields["result"] = "fail"
        fields["reason"] = "os_error_%s" % exc.__class__.__name__
        return fields, 1
    fields["result"] = "ok"
    fields["reason"] = ""
    return fields, 0


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("stl")
    ap.add_argument("--verdict-log")
    ap.add_argument("--expect-triangles", type=int)
    ap.add_argument("--expect-extents")
    ap.add_argument("--step-source")
    args = ap.parse_args()
    fields, rc = judge(args)
    print("JUDGE," + ",".join("%s=%s" % (k, v) for k, v in fields.items()))
    return rc


if __name__ == "__main__":
    sys.exit(main())

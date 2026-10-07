#!/opt/venv/bin/python -IB
import math
import mmap
import re
import sys

UNIT_NAMES = {
    "millimetre": "mm", "millimeter": "mm", "mm": "mm", "milli_metre": "mm",
    "centimetre": "cm", "centimeter": "cm", "cm": "cm", "centi_metre": "cm",
    "metre": "m", "meter": "m", "m": "m",
    "inch": "inch", "in": "inch",
    "foot": "foot", "feet": "foot", "ft": "foot",
    "micrometre": "micrometre", "micrometer": "micrometre", "micron": "micrometre",
    "micro_metre": "micrometre",
}
SI_PREFIX = {"MILLI": "mm", "CENTI": "cm", "MICRO": "micrometre", "": "m"}
UNIT_TO_MM = {"mm": 1.0, "cm": 10.0, "m": 1000.0, "inch": 25.4, "foot": 304.8, "micrometre": 0.001}

GUAC_RE = re.compile(rb"GLOBAL_UNIT_ASSIGNED_CONTEXT\s*\(\s*\(([^)]*)\)\s*\)")
SI_RE = re.compile(rb"SI_UNIT\s*\(\s*(?:\.([A-Z]+)\.|\$)\s*,\s*\.METRE\.\s*\)")
CONV_RE = re.compile(rb"CONVERSION_BASED_UNIT\s*\(\s*'([^']*)'")

STRING = re.compile(r"'(?:[^']|'')*'")
VERTEX = re.compile(r"^#(\d+)\s*=\s*VERTEX_POINT\s*\(\s*'(?:[^']|'')*'\s*,\s*#(\d+)\s*\)$", re.S)
POINT = re.compile(r"^#(\d+)\s*=\s*CARTESIAN_POINT\s*\(\s*'(?:[^']|'')*'\s*,\s*\(([^()]*)\)\s*\)$", re.S)


def normalize_unit(name):
    key = re.sub(r"[^a-z_]", "", (name or "").strip().lower().replace(" ", "_"))
    return UNIT_NAMES.get(key, "unknown")


def length_unit_of(mm_map, ref):
    pat = re.compile(rb"(?<![0-9A-Za-z])#" + ref + rb"\s*=\s*([^;]*);")
    m = pat.search(mm_map)
    if not m or b"LENGTH_UNIT" not in m.group(1):
        return None
    body = m.group(1)
    conv = CONV_RE.search(body)
    if conv:
        return normalize_unit(conv.group(1).decode("latin-1"))
    si = SI_RE.search(body)
    if si:
        return SI_PREFIX.get((si.group(1) or b"").decode("ascii"), "unknown")
    return "unknown"


def text_unit(path):
    with open(path, "rb") as fh:
        with mmap.mmap(fh.fileno(), 0, access=mmap.ACCESS_READ) as mm:
            seen = []
            contexts = 0
            for m in GUAC_RE.finditer(mm):
                contexts += 1
                for ref in re.findall(rb"#(\d+)", m.group(1)):
                    found = length_unit_of(mm, ref)
                    if found is not None:
                        seen.append(found)
                        break
                if contexts >= 8:
                    break
    if not seen:
        return "unknown"
    if len(set(seen)) > 1:
        return "mixed"
    return seen[0]


def step_statements(path):
    with open(path, "rb") as fh:
        text = fh.read().decode("latin-1")
    at = text.find("DATA;")
    if at < 0:
        raise ValueError("no_data_section")
    text = text[at + len("DATA;"):]
    out = []
    pos = 0
    start = 0
    while pos < len(text):
        if text[pos] == "'":
            m = STRING.match(text, pos)
            if m is None:
                raise ValueError("unterminated_string")
            pos = m.end()
            continue
        if text[pos] == ";":
            out.append(text[start:pos].strip())
            start = pos + 1
        pos += 1
    return [s for s in out if s.startswith("#")]


def diag_of(lo, hi):
    return math.sqrt(sum((h - l) ** 2 for l, h in zip(lo, hi)))


def text_diag(path):
    points = {}
    refs = []
    for stmt in step_statements(path):
        m = POINT.match(stmt)
        if m:
            points[m.group(1)] = m.group(2)
            continue
        m = VERTEX.match(stmt)
        if m:
            refs.append(m.group(2))
    if not refs:
        raise ValueError("no_vertex_points")
    lo = [math.inf] * 3
    hi = [-math.inf] * 3
    for ref in refs:
        coords = [float(c) for c in points[ref].split(",")]
        for axis in range(3):
            lo[axis] = min(lo[axis], coords[axis])
            hi[axis] = max(hi[axis], coords[axis])
    return diag_of(lo, hi)


def ocp_read(path):
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.Interface import Interface_Static
    from OCP.STEPControl import STEPControl_Reader

    reader = STEPControl_Reader()
    if not Interface_Static.SetCVal_s("xstep.cascade.unit", "MM"):
        raise RuntimeError("cannot pin xstep.cascade.unit")
    if reader.ReadFile(path) != IFSelect_RetDone:
        raise RuntimeError("read_failed")
    reader.TransferRoots()
    shape = reader.OneShape()
    if shape.IsNull():
        raise RuntimeError("no_shape")
    return reader, shape


def ocp_diag(shape):
    from OCP.BRep import BRep_Tool
    from OCP.TopAbs import TopAbs_VERTEX
    from OCP.TopExp import TopExp_Explorer
    from OCP.TopoDS import TopoDS

    lo = [math.inf] * 3
    hi = [-math.inf] * 3
    count = 0
    explorer = TopExp_Explorer(shape, TopAbs_VERTEX)
    while explorer.More():
        p = BRep_Tool.Pnt_s(TopoDS.Vertex_s(explorer.Current()))
        for axis, value in enumerate((p.X(), p.Y(), p.Z())):
            lo[axis] = min(lo[axis], value)
            hi[axis] = max(hi[axis], value)
        count += 1
        explorer.Next()
    if count == 0:
        raise RuntimeError("no_vertices")
    return diag_of(lo, hi), count


def file_units_length(reader):
    try:
        from OCP.TColStd import TColStd_SequenceOfAsciiString as AsciiSequence
    except ImportError:
        from OCP.collections import Sequence_TCollection_AsciiString as AsciiSequence
    length = AsciiSequence()
    angle = AsciiSequence()
    solid_angle = AsciiSequence()
    reader.FileUnits(length, angle, solid_angle)
    names = [length.Value(i).ToCString() for i in range(1, length.Length() + 1)]
    if not names:
        raise RuntimeError("no_length_unit")
    return normalize_unit(names[0])


def model_length_unit(reader):
    model = reader.StepModel()
    for name in ("LocalLengthUnit", "GetLocalLengthUnit"):
        fn = getattr(model, name, None)
        if fn is not None:
            return float(fn())
    raise RuntimeError("no_local_length_unit_binding")


def attempt(fn, *args):
    try:
        return fn(*args), ""
    except Exception as exc:
        return None, "%s:%s" % (exc.__class__.__name__, str(exc)[:80])


def fmt(value):
    if value is None:
        return "na"
    if isinstance(value, float):
        return repr(value)
    return re.sub(r"[,=;\s]", "_", str(value))


def factor_of(unit):
    return UNIT_TO_MM.get(unit)


def row(path):
    t_unit, t_err = attempt(text_unit, path)
    t_diag, td_err = attempt(text_diag, path)
    pair, o_err = attempt(ocp_read, path)
    ocp_d = vertices = None
    reader = shape = None
    if pair is not None:
        reader, shape = pair
        res, o_err = attempt(ocp_diag, shape)
        if res is not None:
            ocp_d, vertices = res
    fileunits, f_err = (None, o_err or "no_reader") if reader is None else attempt(file_units_length, reader)
    model_factor, m_err = (None, o_err or "no_reader") if reader is None else attempt(model_length_unit, reader)
    ratio = (ocp_d / t_diag) if ocp_d is not None and t_diag else None
    fields = [
        ("file", path.rsplit("/", 1)[-1]),
        ("text_unit", t_unit),
        ("text_factor", factor_of(t_unit)),
        ("fileunits", fileunits),
        ("fileunits_factor", factor_of(fileunits)),
        ("model_length_unit", model_factor),
        ("vertices", vertices),
        ("ocp_diag", ocp_d),
        ("text_diag", t_diag),
        ("ratio", ratio),
        ("err_text", t_err or td_err or None),
        ("err_fileunits", f_err or None),
        ("err_model", m_err or None),
    ]
    return "UNIT_ROW," + ",".join("%s=%s" % (k, fmt(v)) for k, v in fields)


def main(argv):
    if len(argv) < 2:
        sys.stderr.write("usage: cad-unit-probe.py FILE...\n")
        return 64
    for path in argv[1:]:
        print(row(path), flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv))

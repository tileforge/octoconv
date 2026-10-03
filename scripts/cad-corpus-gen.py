#!/usr/bin/env python3
"""cad-corpus-gen.py -- deterministic generator for the phase 71 synthetic CAD corpus.

Usage:
    scripts/cad-corpus-gen.py [--out-dir DIR] [--check-only]

Generation needs OCP and runs inside the measurement image:
    docker run --rm --network none -v "$PWD:/src:ro" -v OUT:/out -w /src TAG \
        /opt/venv/bin/python -I -B scripts/cad-corpus-gen.py --out-dir /out
--check-only needs only the standard library.
"""

import argparse
import math
import os
import re
import struct
import sys
import tempfile

DEFAULT_OUT = os.path.join("internal", "convert", "testdata", "cad", "synthetic")

STEP_DATE = b"2020-01-01T00:00:00"
IGES_DATE_15 = b"20200101.000000"
IGES_DATE_13 = b"200101.000000"

CP1251_NAME = "Деталь".encode("cp1251")
CP1251_DESC = "Деталь болт М5".encode("cp1251")
LATIN1_NAME = "Bügel Ø5".encode("latin-1")
LATIN1_DESC = "Halterung Ø5 für Ø8".encode("latin-1")

BOX_FACE_FLAGS = [b".F.", b".T.", b".F.", b".T.", b".F.", b".T."]
REVERSED_FACE_FLAGS = [b".T.", b".F.", b".T.", b".F.", b".T.", b".F."]

FIXTURES = [
    "units_inch.step",
    "legacy_cp1251.step",
    "legacy_latin1.step",
    "legacy_cp1251.dxf",
    "legacy_latin1.dxf",
    "comments_999.dxf",
    "surfaces_only.iges",
    "iges_no_newlines.iges",
    "stl_binary_solid_header.stl",
    "stl_ascii_box.stl",
    "degenerate_plate.step",
    "degenerate_rod.step",
    "single_face.step",
    "open_shell.step",
    "reversed_solid.step",
    "asymmetric_l_bracket.step",
    "assembly_nauo4.step",
    "blocks_minsert.dxf",
]

EXPECTED = {
    "units_inch.step": [b"CONVERSION_BASED_UNIT('INCH'", b"LENGTH_MEASURE(25.4)", b"MANIFOLD_SOLID_BREP"],
    "legacy_cp1251.step": [CP1251_NAME, CP1251_DESC, b"MANIFOLD_SOLID_BREP"],
    "legacy_latin1.step": [LATIN1_NAME, LATIN1_DESC, b"MANIFOLD_SOLID_BREP"],
    "legacy_cp1251.dxf": [b"$DWGCODEPAGE", b"ANSI_1251", CP1251_NAME],
    "legacy_latin1.dxf": [b"$DWGCODEPAGE", b"ANSI_1252", LATIN1_NAME],
    "comments_999.dxf": [b"999", b"SECTION", b"ENTITIES"],
    "surfaces_only.iges": [b"S0000001", b"T0000001"],
    "iges_no_newlines.iges": [b"S0000001", b"T0000001"],
    "stl_binary_solid_header.stl": [b"solid"],
    "stl_ascii_box.stl": [b"solid", b"facet normal", b"endsolid"],
    "degenerate_plate.step": [b"MANIFOLD_SOLID_BREP"],
    "degenerate_rod.step": [b"MANIFOLD_SOLID_BREP", b"CYLINDRICAL_SURFACE"],
    "single_face.step": [b"ADVANCED_FACE"],
    "open_shell.step": [b"OPEN_SHELL", b"ADVANCED_FACE"],
    "reversed_solid.step": [b"MANIFOLD_SOLID_BREP", b"CLOSED_SHELL"],
    "asymmetric_l_bracket.step": [b"MANIFOLD_SOLID_BREP", b"CYLINDRICAL_SURFACE"],
    "assembly_nauo4.step": [b"NEXT_ASSEMBLY_USAGE_OCCURRENCE", b"PRODUCT_DEFINITION_FORMATION"],
    "blocks_minsert.dxf": [b"INSERT", b"DIMENSION", b"BLOCKS"],
}


def normalize_step(blob):
    blob = re.sub(rb"(FILE_NAME\('[^']*',')\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}'", rb"\g<1>" + STEP_DATE + b"'", blob)
    blob = re.sub(rb"(STEP translator \d+\.\d+) [\d.]+'", rb"\1 1'", blob)
    return blob


def write_step(shape):
    from OCP.STEPControl import STEPControl_Writer, STEPControl_AsIs
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "part.step")
        writer = STEPControl_Writer()
        writer.Transfer(shape, STEPControl_AsIs)
        writer.Write(path)
        with open(path, "rb") as fh:
            return normalize_step(fh.read())


def make_box(dx, dy, dz):
    from OCP.BRepPrimAPI import BRepPrimAPI_MakeBox
    return BRepPrimAPI_MakeBox(dx, dy, dz).Shape()


def faces_of(shape):
    from OCP.TopAbs import TopAbs_FACE
    from OCP.TopExp import TopExp_Explorer
    out = []
    explorer = TopExp_Explorer(shape, TopAbs_FACE)
    while explorer.More():
        out.append(explorer.Current())
        explorer.Next()
    return out


def build_units_inch():
    blob = write_step(make_box(2, 3, 1))
    ids = [int(i) for i in re.findall(rb"#(\d+)", blob)]
    nxt = max(ids) + 1
    pattern = re.compile(rb"#(\d+) = \( LENGTH_UNIT\(\) NAMED_UNIT\(\*\) SI_UNIT\(\.MILLI\.,\.METRE\.\) \);")
    match = pattern.search(blob)
    if not match:
        raise SystemExit("units_inch.step: the length unit entity was not found in the OCP output")
    unit_id = int(match.group(1))
    conv, dims, si = nxt, nxt + 1, nxt + 2
    replacement = (
        b"#%d = ( CONVERSION_BASED_UNIT('INCH',#%d) LENGTH_UNIT() NAMED_UNIT(#%d) );\n"
        b"#%d = LENGTH_MEASURE_WITH_UNIT(LENGTH_MEASURE(25.4),#%d);\n"
        b"#%d = DIMENSIONAL_EXPONENTS(1.,0.,0.,0.,0.,0.,0.);\n"
        b"#%d = ( LENGTH_UNIT() NAMED_UNIT(*) SI_UNIT(.MILLI.,.METRE.) );"
    ) % (unit_id, conv, dims, conv, si, dims, si)
    return blob[:match.start()] + replacement + blob[match.end():]


def with_product_text(name, desc):
    blob = write_step(make_box(3, 2, 1))
    pattern = re.compile(rb"PRODUCT\('Open CASCADE STEP translator 7\.\d+ 1',\s*'Open CASCADE STEP translator 7\.\d+ 1'")
    if not pattern.search(blob):
        raise SystemExit("legacy step: the PRODUCT entity was not found in the OCP output")
    return pattern.sub(lambda _m: b"PRODUCT('" + name + b"',\n  '" + desc + b"'", blob, count=1)


def dxf_pairs(pairs):
    out = bytearray()
    for code, value in pairs:
        if isinstance(value, str):
            value = value.encode("ascii")
        out += b"%3d\r\n" % code + value + b"\r\n"
    return bytes(out)


def dxf_header(codepage):
    return [
        (0, "SECTION"), (2, "HEADER"),
        (9, "$ACADVER"), (1, "AC1009"),
        (9, "$DWGCODEPAGE"), (3, codepage),
        (0, "ENDSEC"),
    ]


def dxf_text(text_bytes):
    return [
        (0, "SECTION"), (2, "ENTITIES"),
        (0, "TEXT"), (8, "0"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (40, "2.5"), (1, text_bytes),
        (0, "ENDSEC"), (0, "EOF"),
    ]


def build_legacy_dxf(codepage, text):
    return dxf_pairs(dxf_header(codepage) + dxf_text(text))


def build_comments_999():
    pairs = [(999, "generated by scripts/cad-corpus-gen.py"), (999, "comment group before the first SECTION")]
    pairs += [(0, "SECTION"), (2, "ENTITIES"),
              (0, "LINE"), (8, "0"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (11, "10.0"), (21, "5.0"), (31, "0.0"),
              (0, "ENDSEC"), (0, "EOF")]
    return dxf_pairs(pairs)


def fix_iges_dates(blob):
    lines = blob.split(b"\n")
    out = []
    for line in lines:
        if len(line) == 80 and line[72:73] == b"G":
            line = re.sub(rb"15H\d{8}\.\d{6}", b"15H" + IGES_DATE_15, line)
            line = re.sub(rb"13H\d{6}\.\d{6}", b"13H" + IGES_DATE_13, line)
        out.append(line)
    return b"\n".join(out)


def build_surfaces_iges():
    from OCP.BRep import BRep_Builder
    from OCP.IGESControl import IGESControl_Writer
    from OCP.TopoDS import TopoDS_Compound
    compound = TopoDS_Compound()
    builder = BRep_Builder()
    builder.MakeCompound(compound)
    for face in faces_of(make_box(2, 3, 1)):
        builder.Add(compound, face)
    writer = IGESControl_Writer()
    writer.AddShape(compound)
    writer.ComputeModel()
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "part.iges")
        writer.Write(path)
        with open(path, "rb") as fh:
            return fix_iges_dates(fh.read())


def build_iges_no_newlines(iges):
    return iges.replace(b"\r", b"").replace(b"\n", b"")


def box_triangles(dx, dy, dz):
    x, y, z = dx, dy, dz
    faces = [
        ((-1, 0, 0), [(0, 0, 0), (0, 0, z), (0, y, z), (0, y, 0)]),
        ((1, 0, 0), [(x, 0, 0), (x, y, 0), (x, y, z), (x, 0, z)]),
        ((0, -1, 0), [(0, 0, 0), (x, 0, 0), (x, 0, z), (0, 0, z)]),
        ((0, 1, 0), [(0, y, 0), (0, y, z), (x, y, z), (x, y, 0)]),
        ((0, 0, -1), [(0, 0, 0), (0, y, 0), (x, y, 0), (x, 0, 0)]),
        ((0, 0, 1), [(0, 0, z), (x, 0, z), (x, y, z), (0, y, z)]),
    ]
    tris = []
    for normal, quad in faces:
        tris.append((normal, quad[0], quad[1], quad[2]))
        tris.append((normal, quad[0], quad[2], quad[3]))
    return tris


def build_stl_binary():
    tris = box_triangles(10, 20, 30)
    header = b"solid octoconv binary box fixture"
    header = header + b"\x00" * (80 - len(header))
    out = bytearray(header + struct.pack("<I", len(tris)))
    for normal, a, b, c in tris:
        out += struct.pack("<12fH", *normal, *a, *b, *c, 0)
    return bytes(out)


def build_stl_ascii():
    lines = ["solid box"]
    for normal, a, b, c in box_triangles(10, 20, 30):
        lines.append("  facet normal %s" % " ".join("%.6e" % v for v in normal))
        lines.append("    outer loop")
        for vertex in (a, b, c):
            lines.append("      vertex %s" % " ".join("%.6e" % v for v in vertex))
        lines.append("    endloop")
        lines.append("  endfacet")
    lines.append("endsolid box")
    return ("\n".join(lines) + "\n").encode("ascii")


def build_plate():
    return write_step(make_box(100, 100, 0.1))


def build_rod():
    from OCP.BRepPrimAPI import BRepPrimAPI_MakeCylinder
    return write_step(BRepPrimAPI_MakeCylinder(0.5, 200).Shape())


def build_single_face():
    from OCP.BRepBuilderAPI import BRepBuilderAPI_MakeFace
    from OCP.gp import gp_Dir, gp_Pln, gp_Pnt
    plane = gp_Pln(gp_Pnt(0, 0, 0), gp_Dir(0, 0, 1))
    return write_step(BRepBuilderAPI_MakeFace(plane, 0, 100, 0, 100).Face())


def build_open_shell():
    from OCP.BRep import BRep_Builder
    from OCP.TopoDS import TopoDS_Shell
    shell = TopoDS_Shell()
    builder = BRep_Builder()
    builder.MakeShell(shell)
    for face in faces_of(make_box(2, 3, 1))[:5]:
        builder.Add(shell, face)
    return write_step(shell)


def build_reversed():
    return write_step(make_box(2, 3, 1).Reversed())


def build_l_bracket():
    from OCP.BRepAlgoAPI import BRepAlgoAPI_Cut
    from OCP.BRepBuilderAPI import BRepBuilderAPI_MakeFace, BRepBuilderAPI_MakePolygon
    from OCP.BRepPrimAPI import BRepPrimAPI_MakeCylinder, BRepPrimAPI_MakePrism
    from OCP.gp import gp_Ax2, gp_Dir, gp_Pnt, gp_Vec
    polygon = BRepBuilderAPI_MakePolygon()
    for px, py in ((0, 0), (60, 0), (60, 10), (10, 10), (10, 40), (0, 40)):
        polygon.Add(gp_Pnt(px, py, 0))
    polygon.Close()
    face = BRepBuilderAPI_MakeFace(polygon.Wire()).Face()
    body = BRepPrimAPI_MakePrism(face, gp_Vec(0, 0, 20)).Shape()
    hole = BRepPrimAPI_MakeCylinder(gp_Ax2(gp_Pnt(-1, 28, 7), gp_Dir(1, 0, 0)), 3, 12).Shape()
    return write_step(BRepAlgoAPI_Cut(body, hole).Shape())


def build_assembly():
    from OCP.STEPCAFControl import STEPCAFControl_Writer
    from OCP.STEPControl import STEPControl_AsIs
    from OCP.TCollection import TCollection_ExtendedString
    from OCP.TDataStd import TDataStd_Name
    from OCP.TDocStd import TDocStd_Document
    from OCP.TopLoc import TopLoc_Location
    from OCP.XCAFApp import XCAFApp_Application
    from OCP.XCAFDoc import XCAFDoc_DocumentTool
    from OCP.gp import gp_Trsf, gp_Vec
    app = XCAFApp_Application.GetApplication_s()
    doc = TDocStd_Document(TCollection_ExtendedString("XmlOcaf"))
    app.InitDocument(doc)
    tool = XCAFDoc_DocumentTool.ShapeTool_s(doc.Main())
    part = tool.AddShape(make_box(10, 10, 10), False)
    TDataStd_Name.Set_s(part, TCollection_ExtendedString("cube"))
    asm = tool.NewShape()
    TDataStd_Name.Set_s(asm, TCollection_ExtendedString("assembly"))
    for i in range(4):
        trsf = gp_Trsf()
        trsf.SetTranslation(gp_Vec(20 * i, 0, 0))
        tool.AddComponent(asm, part, TopLoc_Location(trsf))
    tool.UpdateAssemblies()
    writer = STEPCAFControl_Writer()
    writer.Transfer(doc, STEPControl_AsIs)
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "part.step")
        writer.Write(path)
        with open(path, "rb") as fh:
            return normalize_step(fh.read())


def build_blocks_minsert():
    def square(name_layer="0"):
        pairs = []
        for (x0, y0, x1, y1) in ((0, 0, 10, 0), (10, 0, 10, 10), (10, 10, 0, 10), (0, 10, 0, 0)):
            pairs += [(0, "LINE"), (8, name_layer),
                      (10, "%.1f" % x0), (20, "%.1f" % y0), (30, "0.0"),
                      (11, "%.1f" % x1), (21, "%.1f" % y1), (31, "0.0")]
        return pairs

    pairs = dxf_header("ANSI_1252")
    pairs += [(0, "SECTION"), (2, "BLOCKS")]
    pairs += [(0, "BLOCK"), (8, "0"), (2, "BOX"), (70, "0"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (3, "BOX")]
    pairs += square()
    pairs += [(0, "ENDBLK"), (8, "0")]
    pairs += [(0, "BLOCK"), (8, "0"), (2, "*D1"), (70, "1"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (3, "*D1"),
              (0, "LINE"), (8, "0"), (10, "0.0"), (20, "-5.0"), (30, "0.0"), (11, "10.0"), (21, "-5.0"), (31, "0.0"),
              (0, "ENDBLK"), (8, "0"), (0, "ENDSEC")]
    pairs += [(0, "SECTION"), (2, "ENTITIES")]
    pairs += [(0, "INSERT"), (8, "0"), (2, "BOX"), (10, "0.0"), (20, "0.0"), (30, "0.0")]
    pairs += [(0, "INSERT"), (8, "0"), (2, "BOX"), (10, "50.0"), (20, "0.0"), (30, "0.0"),
              (70, "3"), (71, "3"), (44, "15.0"), (45, "15.0")]
    pairs += [(0, "DIMENSION"), (8, "0"), (2, "*D1"),
              (10, "10.0"), (20, "-5.0"), (30, "0.0"),
              (11, "5.0"), (21, "-5.0"), (31, "0.0"),
              (70, "0"),
              (13, "0.0"), (23, "0.0"), (33, "0.0"),
              (14, "10.0"), (24, "0.0"), (34, "0.0")]
    pairs += [(0, "ENDSEC"), (0, "EOF")]
    return dxf_pairs(pairs)


def build_all():
    iges = build_surfaces_iges()
    return {
        "units_inch.step": build_units_inch(),
        "legacy_cp1251.step": with_product_text(CP1251_NAME, CP1251_DESC),
        "legacy_latin1.step": with_product_text(LATIN1_NAME, LATIN1_DESC),
        "legacy_cp1251.dxf": build_legacy_dxf("ANSI_1251", CP1251_NAME),
        "legacy_latin1.dxf": build_legacy_dxf("ANSI_1252", LATIN1_NAME),
        "comments_999.dxf": build_comments_999(),
        "surfaces_only.iges": iges,
        "iges_no_newlines.iges": build_iges_no_newlines(iges),
        "stl_binary_solid_header.stl": build_stl_binary(),
        "stl_ascii_box.stl": build_stl_ascii(),
        "degenerate_plate.step": build_plate(),
        "degenerate_rod.step": build_rod(),
        "single_face.step": build_single_face(),
        "open_shell.step": build_open_shell(),
        "reversed_solid.step": build_reversed(),
        "asymmetric_l_bracket.step": build_l_bracket(),
        "assembly_nauo4.step": build_assembly(),
        "blocks_minsert.dxf": build_blocks_minsert(),
    }


def read_pairs(blob):
    lines = blob.split(b"\r\n")
    if lines and lines[-1] == b"":
        lines.pop()
    pairs = []
    for i in range(0, len(lines) - 1, 2):
        try:
            pairs.append((int(lines[i].strip()), lines[i + 1]))
        except ValueError:
            return None
    return pairs


def step_entities(blob):
    data = blob.split(b"DATA;", 1)[-1]
    return {int(m.group(1)): m.group(2) for m in re.finditer(rb"#(\d+)\s*=\s*([^;]*);", data)}


def step_points(blob):
    pts = []
    for m in re.finditer(rb"CARTESIAN_POINT\(\s*'[^']*'\s*,\s*\(([^)]*)\)\s*\)", blob):
        coords = [float(v) for v in m.group(1).split(b",")]
        if len(coords) == 3:
            pts.append(coords)
    return pts


def point_extents(blob):
    pts = step_points(blob)
    if not pts:
        return None
    return [max(p[i] for p in pts) - min(p[i] for p in pts) for i in range(3)]


def close(a, b, tol=1e-6):
    return abs(a - b) <= tol


def face_flags(blob):
    return re.findall(rb"ADVANCED_FACE\(\s*'[^']*'\s*,\s*\([^)]*\)\s*,\s*#\d+\s*,\s*(\.[TF]\.)\s*\)", blob)


def check_units_inch(blob):
    problems = []
    ents = step_entities(blob)
    ctx = [v for v in ents.values() if b"GLOBAL_UNIT_ASSIGNED_CONTEXT" in v]
    if not ctx:
        return ["no GLOBAL_UNIT_ASSIGNED_CONTEXT entity"]
    refs = [int(r) for r in re.findall(rb"#(\d+)", re.search(rb"GLOBAL_UNIT_ASSIGNED_CONTEXT\s*\(\(([^)]*)\)\)", ctx[0]).group(1))]
    length = [ents[r] for r in refs if r in ents and b"LENGTH_UNIT()" in ents[r]]
    if len(length) != 1:
        return ["the context does not reference exactly one length unit"]
    conv = re.search(rb"CONVERSION_BASED_UNIT\('INCH',#(\d+)\)", length[0])
    if not conv:
        return ["the referenced length unit is not CONVERSION_BASED_UNIT('INCH', ...)"]
    factor = ents.get(int(conv.group(1)), b"")
    fm = re.search(rb"LENGTH_MEASURE_WITH_UNIT\(LENGTH_MEASURE\(([^)]*)\),#(\d+)\)", factor)
    if not fm or not close(float(fm.group(1)), 25.4):
        return ["the inch conversion factor is not 25.4"]
    base = ents.get(int(fm.group(2)), b"")
    if b"SI_UNIT(.MILLI.,.METRE.)" not in base:
        problems.append("the inch factor is not relative to SI_UNIT(.MILLI.,.METRE.)")
    return problems


def check_legacy_step(blob, name, desc):
    problems = []
    if b"PRODUCT('" + name + b"'" not in blob:
        problems.append("raw 8-bit PRODUCT name not found")
    if desc not in blob:
        problems.append("raw 8-bit PRODUCT description not found")
    for esc in (b"\\X2\\", b"\\X\\", b"\\S\\"):
        if esc in blob:
            problems.append("escape %r present, the bytes would not be raw" % esc)
    return problems


def check_legacy_dxf(blob, codepage, text):
    pairs = read_pairs(blob)
    if pairs is None:
        return ["not a DXF group-code/value stream"]
    problems = []
    cp = None
    for i, (code, value) in enumerate(pairs):
        if code == 9 and value == b"$DWGCODEPAGE" and i + 1 < len(pairs):
            cp = pairs[i + 1][1]
    if cp != codepage:
        problems.append("$DWGCODEPAGE is %r, want %r" % (cp, codepage))
    if not any(code == 1 and value == text for code, value in pairs):
        problems.append("no group 1 value with the raw legacy bytes")
    try:
        text.decode("utf-8")
        problems.append("the text decodes as UTF-8, it is not a legacy encoding")
    except UnicodeDecodeError:
        pass
    return problems


def check_comments_999(blob):
    pairs = read_pairs(blob)
    if not pairs or len(pairs) < 3:
        return ["not a DXF group-code/value stream"]
    problems = []
    if pairs[0][0] != 999 or pairs[1][0] != 999:
        problems.append("the file does not start with group 999 comments")
    if not any(code == 0 and value == b"SECTION" for code, value in pairs):
        problems.append("no SECTION after the comments")
    first_section = next((i for i, (c, v) in enumerate(pairs) if c == 0 and v == b"SECTION"), None)
    if first_section is not None and first_section < 2:
        problems.append("fewer than two 999 groups before the first SECTION")
    return problems


def iges_records(blob):
    return [blob[i:i + 80] for i in range(0, len(blob), 80)]


def check_iges_structure(blob):
    problems = []
    if len(blob) % 80:
        problems.append("length %d is not a multiple of 80" % len(blob))
    for n, rec in enumerate(iges_records(blob)):
        if len(rec.rstrip(b"\n")) != 80 and not (rec.endswith(b"\n") and len(rec) == 81):
            if len(rec) != 80:
                problems.append("record %d is not 80 columns" % n)
                break
        if rec[72:73] not in (b"S", b"G", b"D", b"P", b"T"):
            problems.append("record %d has no section letter in column 73" % n)
            break
    return problems


def check_surfaces_iges(blob):
    problems = []
    lines = blob.split(b"\n")
    if lines and lines[-1] == b"":
        lines.pop()
    for n, line in enumerate(lines):
        if len(line) != 80 or line[72:73] not in (b"S", b"G", b"D", b"P", b"T"):
            problems.append("line %d is not an 80 column IGES record" % n)
            return problems
    dlines = [l for l in lines if l[72:73] == b"D"]
    types = set()
    for l in dlines[0::2]:
        types.add(int(l[0:8]))
    if not (types & {128, 144, 108}):
        problems.append("no surface entity (108, 128 or 144) in the directory section: %s" % sorted(types))
    if 186 in types or 514 in types:
        problems.append("a solid or shell entity is present: %s" % sorted(types))
    return problems


def check_iges_flat(blob, surfaces):
    problems = check_iges_structure(blob)
    if b"\n" in blob or b"\r" in blob:
        problems.append("line breaks present")
    if blob != surfaces.replace(b"\n", b""):
        problems.append("not the surfaces_only.iges content without its line breaks")
    return problems


def stl_signed_volume(tris):
    total = 0.0
    for a, b, c in tris:
        total += (a[0] * (b[1] * c[2] - b[2] * c[1])
                  - a[1] * (b[0] * c[2] - b[2] * c[0])
                  + a[2] * (b[0] * c[1] - b[1] * c[0])) / 6.0
    return total


def check_stl_binary(blob):
    problems = []
    if len(blob) < 84:
        return ["shorter than the 84 byte header"]
    if not blob[:80].startswith(b"solid"):
        problems.append("the 80 byte header does not start with the word solid")
    n = struct.unpack("<I", blob[80:84])[0]
    if len(blob) != 84 + 50 * n:
        problems.append("size %d != 84 + 50 * %d" % (len(blob), n))
        return problems
    if n != 12:
        problems.append("%d triangles, want 12" % n)
    tris = []
    for i in range(n):
        f = struct.unpack("<12fH", blob[84 + 50 * i:84 + 50 * (i + 1)])
        tris.append((f[3:6], f[6:9], f[9:12]))
    if not close(stl_signed_volume(tris), 6000.0, 1e-3):
        problems.append("signed volume %.3f, want 6000" % stl_signed_volume(tris))
    return problems


def check_stl_ascii(blob):
    problems = []
    text = blob.decode("ascii", "replace")
    if not text.startswith("solid"):
        problems.append("does not start with solid")
    if text.count("facet normal") != 12:
        problems.append("%d facets, want 12" % text.count("facet normal"))
    if "endsolid" not in text:
        problems.append("no endsolid")
    verts = [tuple(float(v) for v in m.groups())
             for m in re.finditer(r"vertex\s+(\S+)\s+(\S+)\s+(\S+)", text)]
    tris = [tuple(verts[i:i + 3]) for i in range(0, len(verts) - 2, 3)]
    if len(tris) != 12 or not close(stl_signed_volume(tris), 6000.0, 1e-3):
        problems.append("vertex data does not make a 10 x 20 x 30 box")
    return problems


def check_extents(blob, want):
    ext = point_extents(blob)
    if ext is None:
        return ["no CARTESIAN_POINT entities"]
    if sorted(round(e, 6) for e in ext) != sorted(want):
        return ["point extents %s, want %s" % (sorted(round(e, 6) for e in ext), sorted(want))]
    return []


def check_rod(blob):
    problems = []
    if not re.search(rb"CYLINDRICAL_SURFACE\(\s*''\s*,\s*#\d+\s*,\s*0\.5\s*\)", blob):
        problems.append("no cylindrical surface of radius 0.5")
    zs = [p[2] for p in step_points(blob)]
    if not zs or not close(max(zs), 200.0):
        problems.append("no point at z = 200")
    return problems


def check_single_face(blob):
    problems = []
    if len(face_flags(blob)) != 1:
        problems.append("%d ADVANCED_FACE entities, want 1" % len(face_flags(blob)))
    for token in (b"MANIFOLD_SOLID_BREP", b"CLOSED_SHELL"):
        if token in blob:
            problems.append("%s present, the file must hold a face and no solid" % token.decode())
    return problems


def check_open_shell(blob):
    problems = []
    if len(face_flags(blob)) != 5:
        problems.append("%d ADVANCED_FACE entities, want 5" % len(face_flags(blob)))
    for token in (b"MANIFOLD_SOLID_BREP", b"CLOSED_SHELL"):
        if token in blob:
            problems.append("%s present, the shell must be open" % token.decode())
    return problems


def check_reversed(blob):
    flags = face_flags(blob)
    if flags != REVERSED_FACE_FLAGS:
        return ["face orientation flags %s are not the complement of a plain box %s" % (flags, BOX_FACE_FLAGS)]
    return []


def check_bracket(blob):
    problems = []
    pts = step_points(blob)
    maxima = [max(p[i] for p in pts) for i in range(3)] if pts else None
    if maxima is None or any(not close(a, b) for a, b in zip(maxima, (60.0, 40.0, 20.0))):
        problems.append("maximum point coordinates %s, want (60, 40, 20)" % (maxima,))
    if not re.search(rb"CYLINDRICAL_SURFACE\(\s*''\s*,\s*#\d+\s*,\s*3\.\s*\)", blob):
        problems.append("no cylindrical surface of radius 3 (the hole)")
    return problems


def check_assembly(blob):
    n = blob.count(b"NEXT_ASSEMBLY_USAGE_OCCURRENCE(")
    if n != 4:
        return ["%d NEXT_ASSEMBLY_USAGE_OCCURRENCE entities, want 4" % n]
    return []


def check_blocks(blob):
    pairs = read_pairs(blob)
    if pairs is None:
        return ["not a DXF group-code/value stream"]
    problems = []
    inserts = []
    cur = None
    in_entities = False
    for i, (code, value) in enumerate(pairs):
        if code == 2 and i and pairs[i - 1] == (0, b"SECTION"):
            in_entities = value == b"ENTITIES"
        if code == 0:
            cur = {"type": value, "g": {}} if in_entities else None
            if cur is not None:
                inserts.append(cur)
        elif cur is not None:
            cur["g"][code] = value
    ins = [e for e in inserts if e["type"] == b"INSERT"]
    multi = [e for e in ins if int(e["g"].get(70, b"1")) > 1 and int(e["g"].get(71, b"1")) > 1]
    if not multi:
        problems.append("no INSERT with column and row counts above 1 (MINSERT)")
    if len(ins) < 2:
        problems.append("fewer than two INSERT entities")
    if not any(e["type"] == b"DIMENSION" for e in inserts):
        problems.append("no DIMENSION entity")
    return problems


def check(out_dir):
    failures = []
    blobs = {}
    for name in FIXTURES:
        path = os.path.join(out_dir, name)
        if not os.path.exists(path):
            failures.append("%s: missing" % name)
            continue
        with open(path, "rb") as fh:
            blobs[name] = fh.read()
        for marker in EXPECTED[name]:
            if marker not in blobs[name]:
                failures.append("%s: marker %r absent from raw bytes" % (name, marker))

    structural = {
        "units_inch.step": check_units_inch,
        "legacy_cp1251.step": lambda b: check_legacy_step(b, CP1251_NAME, CP1251_DESC),
        "legacy_latin1.step": lambda b: check_legacy_step(b, LATIN1_NAME, LATIN1_DESC),
        "legacy_cp1251.dxf": lambda b: check_legacy_dxf(b, b"ANSI_1251", CP1251_NAME),
        "legacy_latin1.dxf": lambda b: check_legacy_dxf(b, b"ANSI_1252", LATIN1_NAME),
        "comments_999.dxf": check_comments_999,
        "surfaces_only.iges": check_surfaces_iges,
        "stl_binary_solid_header.stl": check_stl_binary,
        "stl_ascii_box.stl": check_stl_ascii,
        "degenerate_plate.step": lambda b: check_extents(b, [100.0, 100.0, 0.1]),
        "degenerate_rod.step": check_rod,
        "single_face.step": check_single_face,
        "open_shell.step": check_open_shell,
        "reversed_solid.step": check_reversed,
        "asymmetric_l_bracket.step": check_bracket,
        "assembly_nauo4.step": check_assembly,
        "blocks_minsert.dxf": check_blocks,
    }
    for name, fn in structural.items():
        if name in blobs:
            failures += ["%s: %s" % (name, p) for p in fn(blobs[name])]
    if "iges_no_newlines.iges" in blobs:
        surfaces = blobs.get("surfaces_only.iges", b"")
        failures += ["iges_no_newlines.iges: %s" % p for p in check_iges_flat(blobs["iges_no_newlines.iges"], surfaces)]

    if failures:
        print("SELF-CHECK FAILED:", file=sys.stderr)
        for f in failures:
            print("  " + f, file=sys.stderr)
        return 1
    for name in FIXTURES:
        print("  %s: %d bytes" % (name, os.path.getsize(os.path.join(out_dir, name))))
    print("self-check passed: every fixture carries the markers it exists to carry")
    return 0


def read_back(out_dir):
    from OCP.BRepBndLib import BRepBndLib
    from OCP.BRepGProp import BRepGProp
    from OCP.Bnd import Bnd_Box
    from OCP.GProp import GProp_GProps
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.STEPControl import STEPControl_Reader
    from OCP.TColStd import TColStd_SequenceOfAsciiString
    from OCP.TopAbs import TopAbs_SOLID
    from OCP.TopExp import TopExp_Explorer

    problems = []
    report = []
    expected = {
        "units_inch.step": (1, 98322.4, (50.8, 76.2, 25.4)),
        "legacy_cp1251.step": (1, 6.0, (3.0, 2.0, 1.0)),
        "legacy_latin1.step": (1, 6.0, (3.0, 2.0, 1.0)),
        "degenerate_plate.step": (1, 1000.0, (100.0, 100.0, 0.1)),
        "degenerate_rod.step": (1, math.pi * 0.25 * 200, (1.0, 1.0, 200.0)),
        "single_face.step": (0, None, (100.0, 100.0, 0.0)),
        "open_shell.step": (0, None, None),
        "reversed_solid.step": (1, 6.0, (2.0, 3.0, 1.0)),
        "assembly_nauo4.step": (4, 4000.0, (70.0, 10.0, 10.0)),
    }
    for name in sorted(expected):
        reader = STEPControl_Reader()
        if reader.ReadFile(os.path.join(out_dir, name)) != IFSelect_RetDone:
            problems.append("%s: OCP could not read the file" % name)
            continue
        reader.TransferRoots()
        shape = reader.OneShape()
        solids, volume = 0, 0.0
        explorer = TopExp_Explorer(shape, TopAbs_SOLID)
        while explorer.More():
            props = GProp_GProps()
            BRepGProp.VolumeProperties_s(explorer.Current(), props)
            solids += 1
            volume += props.Mass()
            explorer.Next()
        box = Bnd_Box()
        BRepBndLib.Add_s(shape, box)
        lo, hi = box.CornerMin(), box.CornerMax()
        dims = (hi.X() - lo.X(), hi.Y() - lo.Y(), hi.Z() - lo.Z())
        length = TColStd_SequenceOfAsciiString()
        reader.FileUnits(length, TColStd_SequenceOfAsciiString(), TColStd_SequenceOfAsciiString())
        units = ",".join(length.Value(i).ToCString() for i in range(1, length.Length() + 1))
        report.append("%s: solids=%d volume=%.4f dims=(%.4f, %.4f, %.4f) units=%s" % (name, solids, volume, *dims, units))
        want_solids, want_volume, want_dims = expected[name]
        if name == "assembly_nauo4.step":
            if solids < 1:
                problems.append("%s: no solid after reading" % name)
            continue
        if solids != want_solids:
            problems.append("%s: %d solids, want %d" % (name, solids, want_solids))
        if want_volume is not None and abs(volume - want_volume) > 1e-3 * max(1.0, want_volume):
            problems.append("%s: volume %.4f, want %.4f" % (name, volume, want_volume))
        if want_dims is not None and any(abs(a - b) > 1e-3 for a, b in zip(dims, want_dims)):
            problems.append("%s: dims %s, want %s" % (name, dims, want_dims))
        if name == "units_inch.step" and "inch" not in units.lower():
            problems.append("units_inch.step: OCP reads the length unit as %r, not inch" % units)

    bracket = os.path.join(out_dir, "asymmetric_l_bracket.step")
    reader = STEPControl_Reader()
    reader.ReadFile(bracket)
    reader.TransferRoots()
    shape = reader.OneShape()
    props = GProp_GProps()
    BRepGProp.VolumeProperties_s(shape, props)
    box = Bnd_Box()
    BRepBndLib.Add_s(shape, box)
    lo, hi = box.CornerMin(), box.CornerMax()
    com = props.CentreOfMass()
    offset = max(abs(com.X() - (lo.X() + hi.X()) / 2), abs(com.Y() - (lo.Y() + hi.Y()) / 2), abs(com.Z() - (lo.Z() + hi.Z()) / 2))
    report.append("asymmetric_l_bracket.step: volume=%.4f centroid_offset=%.4f" % (props.Mass(), offset))
    if offset < 1.0:
        problems.append("asymmetric_l_bracket.step: the centroid is within 1 mm of the bbox centre")

    import ezdxf
    for name, want in (("legacy_cp1251.dxf", "Деталь"), ("legacy_latin1.dxf", "Bügel Ø5")):
        doc = ezdxf.readfile(os.path.join(out_dir, name))
        texts = [e.dxf.text for e in doc.modelspace().query("TEXT")]
        report.append("%s: ezdxf text=%r" % (name, texts))
        if texts != [want]:
            problems.append("%s: ezdxf decodes %r, want %r" % (name, texts, want))
    doc = ezdxf.readfile(os.path.join(out_dir, "comments_999.dxf"))
    report.append("comments_999.dxf: ezdxf entities=%d" % len(doc.modelspace()))
    doc = ezdxf.readfile(os.path.join(out_dir, "blocks_minsert.dxf"))
    msp = doc.modelspace()
    inserts = [(e.dxf.column_count, e.dxf.row_count) for e in msp.query("INSERT")]
    dims = len(msp.query("DIMENSION"))
    report.append("blocks_minsert.dxf: inserts=%s dimensions=%d" % (inserts, dims))
    if (3, 3) not in inserts or dims != 1:
        problems.append("blocks_minsert.dxf: ezdxf sees inserts=%s dimensions=%d" % (inserts, dims))
    return report, problems


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--out-dir", default=DEFAULT_OUT)
    ap.add_argument("--check-only", action="store_true",
                    help="verify existing fixtures without rewriting them")
    args = ap.parse_args()

    if not args.check_only:
        try:
            os.makedirs(args.out_dir, exist_ok=True)
        except FileExistsError:
            # OrbStack bind mounts created moments earlier on the host sometimes stat as a non-directory
            pass
        for name, blob in build_all().items():
            with open(os.path.join(args.out_dir, name), "wb") as fh:
                fh.write(blob)
        print("generated CAD corpus into %s" % args.out_dir)

    rc = check(args.out_dir)
    if rc == 0 and not args.check_only:
        report, problems = read_back(args.out_dir)
        for line in report:
            print("  readback " + line)
        if problems:
            print("SELF-CHECK FAILED:", file=sys.stderr)
            for p in problems:
                print("  " + p, file=sys.stderr)
            return 1
    return rc


if __name__ == "__main__":
    sys.exit(main())

#!/usr/bin/env python3
"""cad-corpus-gen.py -- deterministic generator for the phase 71 synthetic CAD corpus.

Usage:
    scripts/cad-corpus-gen.py [--out-dir DIR] [--check-only]
    scripts/cad-corpus-gen.py --scaled-from FILE --kind step-copies|step-nauo|stl-tile|stl-ascii|dxf-replicate|dxf-minsert|iges-from-step
        --targets LIST [--scaled-dir DIR]

Generation needs OCP and runs inside the measurement image:
    docker run --rm --network none -v "$PWD:/src:ro" -v OUT:/out -w /src TAG \
        /opt/venv/bin/python -I -B scripts/cad-corpus-gen.py --out-dir /out
--check-only needs only the standard library.
"""

import argparse
import hashlib
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


MIB = 1024 * 1024
DEFAULT_SCALED = os.path.join(
    os.environ.get("OCTOCONV_CAD_CACHE") or os.path.join(os.path.expanduser("~"), ".cache", "octoconv", "cad-corpus"),
    "scaled",
)
KINDS = ("step-copies", "step-nauo", "stl-tile", "stl-ascii", "dxf-replicate", "dxf-minsert", "iges-from-step")
STEP_TOKEN = re.compile(rb"'(?:[^']|'')*'|#(\d+)")
STEP_DEF = re.compile(rb"(?:^|(?<=;))[ \t]*#\d+[ \t]*=", re.M)
STEP_NAUO = re.compile(rb"(?<![A-Z_0-9])NEXT_ASSEMBLY_USAGE_OCCURRENCE[ \t\r\n]*\(")
DXF_NORMALIZED_NUMERIC = {"$TDCREATE", "$TDUCREATE", "$TDUPDATE", "$TDUUPDATE", "$TDINDWG", "$TDUSRTIMER"}
DXF_NORMALIZED_GUID = {"$FINGERPRINTGUID", "$VERSIONGUID"}
NIL_GUID = "{00000000-0000-0000-0000-000000000000}"


def parse_targets(text):
    out = []
    for part in text.split(","):
        part = part.strip()
        if not part:
            continue
        out.append(int(part))
    if not out:
        raise SystemExit("--targets is empty")
    return out


def stem_of(path):
    stem = os.path.splitext(os.path.basename(path))[0]
    return re.sub(r"[^A-Za-z0-9._-]+", "-", stem)


def sha_of(blob):
    return hashlib.sha256(blob).hexdigest()


def scaled_line(kind, label, size, digest, check, extra=""):
    print("SCALED,kind=%s,target=%s,bytes=%d,sha=%s,check=%s%s" % (kind, label, size, digest, check, extra), flush=True)


def write_blob(path, blob):
    with open(path, "wb") as fh:
        fh.write(blob)


def step_sections(blob):
    data_at = blob.find(b"DATA;")
    end_at = blob.rfind(b"ENDSEC;")
    if data_at < 0 or end_at < data_at:
        raise SystemExit("step-copies: the base has no DATA section")
    data_start = data_at + len(b"DATA;")
    return blob[:data_start], blob[data_start:end_at], blob[end_at:]


def step_renumber(data, offset):
    if offset == 0:
        return data

    def swap(match):
        if match.group(1) is None:
            return match.group(0)
        return b"#%d" % (int(match.group(1)) + offset)

    return STEP_TOKEN.sub(swap, data)


def step_max_id(data):
    top = 0
    for match in STEP_TOKEN.finditer(data):
        if match.group(1) is not None:
            top = max(top, int(match.group(1)))
    return top


def ocp_step_solids_volume(path):
    from OCP.BRepGProp import BRepGProp
    from OCP.GProp import GProp_GProps
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.STEPControl import STEPControl_Reader
    from OCP.TopAbs import TopAbs_SOLID
    from OCP.TopExp import TopExp_Explorer
    reader = STEPControl_Reader()
    if reader.ReadFile(path) != IFSelect_RetDone:
        raise SystemExit("OCP could not read %s" % path)
    reader.TransferRoots()
    shape = reader.OneShape()
    solids = 0
    volume = 0.0
    explorer = TopExp_Explorer(shape, TopAbs_SOLID)
    while explorer.More():
        props = GProp_GProps()
        BRepGProp.VolumeProperties_s(explorer.Current(), props)
        volume += props.Mass()
        solids += 1
        explorer.Next()
    return solids, volume


def scale_step_copies(base_path, targets, out_dir):
    with open(base_path, "rb") as fh:
        base = fh.read()
    head, data, tail = step_sections(base)
    max_id = step_max_id(data)
    base_defs = sum(1 for _ in STEP_DEF.finditer(data))
    copies = {}

    def copy_of(k):
        if k not in copies:
            copies[k] = step_renumber(data, k * max_id)
        return copies[k]

    failures = 0
    ocp_checked = False
    base_ocp = None
    stem = stem_of(base_path)
    for target in targets:
        want = target * MIB
        k = 0
        total = len(head) + len(tail)
        while total + len(copy_of(k)) // 2 < want:
            total += len(copy_of(k))
            k += 1
        if k < 2:
            print("SCALED_SKIP,kind=step-copies,target=%dMiB,reason=target_below_two_copies_of_base,base_bytes=%d" % (target, len(base)), flush=True)
            continue
        blob = head + b"".join(copy_of(i) for i in range(k)) + tail
        name = "step-copies-%s-%dMiB.step" % (stem, target)
        path = os.path.join(out_dir, name)
        write_blob(path, blob)
        defs = sum(1 for _ in STEP_DEF.finditer(blob, len(head)))
        top = step_max_id(blob[len(head):len(blob) - len(tail)])
        problems = []
        if defs != k * base_defs:
            problems.append("entities %d != %d x %d" % (defs, k, base_defs))
        if top != k * max_id:
            problems.append("max id %d != %d x %d" % (top, k, max_id))
        if blob[:len(head)] != head or not blob.endswith(tail):
            problems.append("header or trailer changed")
        method = "struct"
        if not ocp_checked and len(blob) <= 24 * MIB:
            try:
                base_solids, base_volume = ocp_step_solids_volume(base_path)
                solids, volume = ocp_step_solids_volume(path)
            except ImportError:
                solids = volume = None
            if solids is not None:
                ocp_checked = True
                method = "struct+ocp-volume"
                if solids != k * base_solids or abs(volume - k * base_volume) > 1e-6 * max(1.0, abs(k * base_volume)):
                    problems.append("ocp solids %d volume %.6f != %d x (%d, %.6f)" % (solids, volume, k, base_solids, base_volume))
        if problems:
            failures += 1
            print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
            continue
        scaled_line("step-copies", "%dMiB" % target, len(blob), sha_of(blob), "ok",
                    ",base=%s,k=%d,entities=%d,method=%s,file=%s" % (os.path.basename(base_path), k, defs, method, name))
    return failures


def scale_step_nauo(base_path, targets, out_dir):
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.STEPCAFControl import STEPCAFControl_Writer
    from OCP.STEPControl import STEPControl_AsIs, STEPControl_Reader
    from OCP.TCollection import TCollection_ExtendedString
    from OCP.TDataStd import TDataStd_Name
    from OCP.TDocStd import TDocStd_Document
    from OCP.TopLoc import TopLoc_Location
    from OCP.XCAFApp import XCAFApp_Application
    from OCP.XCAFDoc import XCAFDoc_DocumentTool
    from OCP.gp import gp_Trsf, gp_Vec

    reader = STEPControl_Reader()
    if reader.ReadFile(base_path) != IFSelect_RetDone:
        raise SystemExit("step-nauo: OCP could not read the base")
    reader.TransferRoots()
    shape = reader.OneShape()
    stem = stem_of(base_path)
    failures = 0
    for count in targets:
        app = XCAFApp_Application.GetApplication_s()
        doc = TDocStd_Document(TCollection_ExtendedString("XmlOcaf"))
        app.InitDocument(doc)
        tool = XCAFDoc_DocumentTool.ShapeTool_s(doc.Main())
        part = tool.AddShape(shape, False)
        TDataStd_Name.Set_s(part, TCollection_ExtendedString("prototype"))
        asm = tool.NewShape()
        TDataStd_Name.Set_s(asm, TCollection_ExtendedString("assembly"))
        side = max(1, int(round(count ** (1.0 / 3.0))))
        for i in range(count):
            trsf = gp_Trsf()
            trsf.SetTranslation(gp_Vec(200.0 * (i % side), 200.0 * ((i // side) % side), 200.0 * (i // (side * side))))
            tool.AddComponent(asm, part, TopLoc_Location(trsf))
        tool.UpdateAssemblies()
        writer = STEPCAFControl_Writer()
        writer.Transfer(doc, STEPControl_AsIs)
        name = "step-nauo-%s-n%d.step" % (stem, count)
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = os.path.join(tmp, "assembly.step")
            writer.Write(tmp_path)
            with open(tmp_path, "rb") as fh:
                blob = normalize_step(fh.read())
        found = sum(1 for _ in STEP_NAUO.finditer(blob))
        if found != count:
            failures += 1
            print("SCALE CHECK FAILED %s: %d NEXT_ASSEMBLY_USAGE_OCCURRENCE, expected %d" % (name, found, count), file=sys.stderr)
            continue
        write_blob(os.path.join(out_dir, name), blob)
        scaled_line("step-nauo", "n%d" % count, len(blob), sha_of(blob), "ok",
                    ",base=%s,instances=%d,nauo=%d,file=%s" % (os.path.basename(base_path), count, found, name))
    return failures


def read_binary_stl(path):
    with open(path, "rb") as fh:
        blob = fh.read()
    if len(blob) < 84:
        raise SystemExit("the base STL is shorter than its header")
    count = struct.unpack("<I", blob[80:84])[0]
    if len(blob) != 84 + 50 * count:
        raise SystemExit("the base is not a binary STL of 84 + 50 x n bytes")
    return blob, count


def stl_pitch(blob, count):
    lo = [float("inf")] * 3
    hi = [float("-inf")] * 3
    for rec in struct.iter_unpack("<12fH", blob[84:84 + 50 * count]):
        for v in range(3):
            base = 3 + 3 * v
            for axis in range(3):
                value = rec[base + axis]
                if value < lo[axis]:
                    lo[axis] = value
                if value > hi[axis]:
                    hi[axis] = value
    return [max(1.0, (hi[a] - lo[a]) * 1.05) for a in range(3)]


def tile_triangles(blob, count, pitch, total):
    side = max(1, int(math.ceil(math.sqrt(math.ceil(total / count)))))
    emitted = 0
    tile = 0
    while emitted < total:
        shift_x = (tile % side) * pitch[0]
        shift_y = (tile // side) * pitch[1]
        for rec in struct.iter_unpack("<12fH", blob[84:84 + 50 * count]):
            if emitted >= total:
                return
            vals = list(rec)
            if tile:
                for v in range(3):
                    vals[3 + 3 * v] += shift_x
                    vals[4 + 3 * v] += shift_y
            yield vals
            emitted += 1
        tile += 1


def scale_stl_tile(base_path, targets, out_dir):
    blob, count = read_binary_stl(base_path)
    pitch = stl_pitch(blob, count)
    stem = stem_of(base_path)
    failures = 0
    for target in targets:
        tiles = max(1, round(target * MIB / len(blob)))
        if tiles < 2:
            print("SCALED_SKIP,kind=stl-tile,target=%dMiB,reason=target_below_two_tiles_of_base,base_bytes=%d" % (target, len(blob)), flush=True)
            continue
        total = tiles * count
        header = (b"octoconv cad-corpus-gen stl-tile " + os.path.basename(base_path).encode("ascii", "replace"))[:80]
        header = header + b"\x00" * (80 - len(header))
        name = "stl-tile-%s-%dMiB.stl" % (stem, target)
        path = os.path.join(out_dir, name)
        digest = hashlib.sha256()
        with open(path, "wb") as fh:
            part = header + struct.pack("<I", total)
            fh.write(part)
            digest.update(part)
            pending = bytearray()
            for vals in tile_triangles(blob, count, pitch, total):
                pending += struct.pack("<12fH", *vals)
                if len(pending) >= 1 << 22:
                    fh.write(pending)
                    digest.update(pending)
                    pending = bytearray()
            fh.write(pending)
            digest.update(pending)
        size = os.path.getsize(path)
        problems = []
        if size != 84 + 50 * total:
            problems.append("size %d != 84 + 50 x %d" % (size, total))
        with open(path, "rb") as fh:
            head = fh.read(84 + 50 * min(count, 64))
        if struct.unpack("<I", head[80:84])[0] != total:
            problems.append("count field differs from %d" % total)
        if head[84:84 + 50 * min(count, 64)] != blob[84:84 + 50 * min(count, 64)]:
            problems.append("first tile differs from the base")
        if problems:
            failures += 1
            print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
            continue
        scaled_line("stl-tile", "%dMiB" % target, size, digest.hexdigest(), "ok",
                    ",base=%s,tiles=%d,triangles=%d,file=%s" % (os.path.basename(base_path), tiles, total, name))
    return failures


def ascii_facet(vals):
    return (
        "facet normal %.6e %.6e %.6e\n  outer loop\n    vertex %.6e %.6e %.6e\n    vertex %.6e %.6e %.6e\n"
        "    vertex %.6e %.6e %.6e\n  endloop\nendfacet\n" % tuple(vals[:12])
    )


def scale_stl_ascii(base_path, targets, out_dir):
    blob, count = read_binary_stl(base_path)
    pitch = stl_pitch(blob, count)
    stem = stem_of(base_path)
    failures = 0
    for target in targets:
        want = target * MIB
        name = "stl-ascii-%s-%dMiB.stl" % (stem, target)
        path = os.path.join(out_dir, name)
        digest = hashlib.sha256()
        total_bytes = 0
        facets = 0
        first = None
        horizon = max(count, int(want / 200) + 2)
        with open(path, "wb") as fh:
            part = b"solid octoconv\n"
            fh.write(part)
            digest.update(part)
            total_bytes += len(part)
            pending = bytearray()
            for vals in tile_triangles(blob, count, pitch, horizon * 2):
                text = ascii_facet(vals)
                if first is None:
                    first = text
                data = text.encode("ascii")
                if total_bytes + len(pending) + len(data) + len(b"endsolid octoconv\n") > want:
                    break
                pending += data
                facets += 1
                if len(pending) >= 1 << 22:
                    fh.write(pending)
                    digest.update(pending)
                    total_bytes += len(pending)
                    pending = bytearray()
            tail = b"endsolid octoconv\n"
            pending += tail
            fh.write(pending)
            digest.update(pending)
            total_bytes += len(pending)
        size = os.path.getsize(path)
        problems = []
        found = 0
        with open(path, "rb") as fh:
            head = fh.read(len(b"solid octoconv\n") + len(first.encode("ascii")))
            fh.seek(0)
            for line in fh:
                if line.lstrip().startswith(b"facet normal"):
                    found += 1
        if found != facets:
            problems.append("%d facets read back, %d written" % (found, facets))
        if size > want or want - size > 400:
            problems.append("size %d is not within one facet of %d" % (size, want))
        if not head.startswith(b"solid octoconv\n" + first.encode("ascii")):
            problems.append("first facet differs")
        if problems:
            failures += 1
            print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
            continue
        scaled_line("stl-ascii", "%dMiB" % target, size, digest.hexdigest(), "ok",
                    ",base=%s,triangles=%d,file=%s" % (os.path.basename(base_path), facets, name))
    return failures


def dxf_split(blob):
    lines = blob.split(b"\n")
    if lines and lines[-1] == b"":
        lines.pop()
    return lines


def dxf_pairs_of(lines):
    return [(lines[i], lines[i + 1]) for i in range(0, len(lines) - 1, 2)]


def dxf_normalize(pairs):
    out = []
    in_header = False
    var = None
    for code_raw, value in pairs:
        try:
            code = int(code_raw.strip())
        except ValueError:
            out.append((code_raw, value))
            continue
        text = value.strip().decode("latin-1")
        if code == 0:
            in_header = False
            var = None
        if code == 2 and text == "HEADER" and out and out[-1][1].strip() == b"SECTION":
            in_header = True
        if in_header and code == 9:
            var = text
        elif in_header and var in DXF_NORMALIZED_NUMERIC:
            value = b"0.0"
            var = None
        elif in_header and var in DXF_NORMALIZED_GUID:
            value = NIL_GUID.encode("ascii")
            var = None
        out.append((code_raw, value))
    return out


def dxf_entity_range(pairs):
    start = end = None
    for i, (code_raw, value) in enumerate(pairs):
        if code_raw.strip() != b"0":
            continue
        if value.strip() == b"SECTION" and i + 1 < len(pairs) and pairs[i + 1][1].strip() == b"ENTITIES":
            start = i + 2
        elif start is not None and end is None and value.strip() == b"ENDSEC":
            end = i
    if start is None or end is None:
        raise SystemExit("dxf-replicate: the base has no ENTITIES section")
    return start, end


def dxf_records(pairs):
    records = []
    current = []
    for pair in pairs:
        if pair[0].strip() == b"0" and current:
            records.append(current)
            current = []
        current.append(pair)
    if current:
        records.append(current)
    return records


def dxf_handle_seed(pairs):
    for i, (code_raw, value) in enumerate(pairs):
        if code_raw.strip() == b"9" and value.strip() == b"$HANDSEED" and i + 1 < len(pairs):
            return i + 1, int(pairs[i + 1][1].strip(), 16)
    return None, None


def dxf_shift(code, value, offset):
    if code in (10, 11, 12, 13, 14, 15, 16):
        try:
            return b"%.12g" % (float(value) + offset)
        except ValueError:
            return value
    return value


def scale_dxf_replicate(base_path, targets, out_dir):
    with open(base_path, "rb") as fh:
        base = fh.read()
    nl = b"\r\n" if b"\r\n" in base else b"\n"
    lines = [ln.rstrip(b"\r") for ln in dxf_split(base)]
    pairs = dxf_normalize(dxf_pairs_of(lines))
    start, end = dxf_entity_range(pairs)
    records = dxf_records(pairs[start:end])
    seed_index, seed = dxf_handle_seed(pairs)
    if seed is None:
        raise SystemExit("dxf-replicate: the base has no $HANDSEED")
    handles_per_copy = sum(1 for rec in records for code_raw, _ in rec if code_raw.strip() == b"5")
    head_pairs = pairs[:start]
    tail_pairs = pairs[end:]

    def render(pair_list):
        return b"".join(code_raw.rstrip(b"\r") + nl + value.rstrip(b"\r") + nl for code_raw, value in pair_list)

    base_entities = len(records)
    stem = stem_of(base_path)
    failures = 0
    entities_bytes = len(render([p for rec in records for p in rec]))
    fixed = len(base) - entities_bytes
    verified = False
    for target in targets:
        copies = max(1, int(round((target * MIB - fixed) / entities_bytes)))
        if copies < 2:
            print("SCALED_SKIP,kind=dxf-replicate,target=%dMiB,reason=target_below_two_copies_of_modelspace,base_bytes=%d" % (target, len(base)), flush=True)
            continue
        new_seed = seed + copies * handles_per_copy + 1
        head = list(head_pairs)
        head[seed_index] = (head[seed_index][0], b"%X" % new_seed)
        name = "dxf-replicate-%s-%dMiB.dxf" % (stem, target)
        path = os.path.join(out_dir, name)
        counter = seed + 1
        with open(path, "wb") as fh:
            digest = hashlib.sha256()
            part = render(head)
            fh.write(part)
            digest.update(part)
            for k in range(copies):
                shift = 1000.0 * k
                chunk = []
                for rec in records:
                    for code_raw, value in rec:
                        code = int(code_raw.strip()) if code_raw.strip().lstrip(b"-").isdigit() else None
                        if code == 5 and k:
                            value = b"%X" % counter
                            counter += 1
                        elif k and code is not None:
                            value = dxf_shift(code, value, shift)
                        chunk.append((code_raw, value))
                part = render(chunk)
                fh.write(part)
                digest.update(part)
            part = render(tail_pairs)
            fh.write(part)
            digest.update(part)
        size = os.path.getsize(path)
        with open(path, "rb") as fh:
            out_blob = fh.read()
        out_lines = [ln.rstrip(b"\r") for ln in dxf_split(out_blob)]
        out_pairs = dxf_pairs_of(out_lines)
        o_start, o_end = dxf_entity_range(out_pairs)
        out_records = dxf_records(out_pairs[o_start:o_end])
        problems = []
        if len(out_records) != copies * base_entities:
            problems.append("%d entities, expected %d x %d" % (len(out_records), copies, base_entities))
        handles = [value.strip() for rec in out_records for code_raw, value in rec if code_raw.strip() == b"5"]
        if len(set(handles)) != len(handles):
            problems.append("duplicate handles")
        kinds_in = {}
        kinds_out = {}
        for rec in records:
            kinds_in[rec[0][1].strip()] = kinds_in.get(rec[0][1].strip(), 0) + 1
        for rec in out_records:
            kinds_out[rec[0][1].strip()] = kinds_out.get(rec[0][1].strip(), 0) + 1
        for kind_name, n in kinds_in.items():
            if kinds_out.get(kind_name, 0) != copies * n:
                problems.append("%s count %d, expected %d" % (kind_name.decode("ascii", "replace"), kinds_out.get(kind_name, 0), copies * n))
        method = "struct"
        if not verified and size <= 24 * MIB:
            try:
                import ezdxf
            except ImportError:
                ezdxf = None
            if ezdxf is not None:
                doc = ezdxf.readfile(path)
                base_doc = ezdxf.readfile(base_path)
                if len(doc.modelspace()) != copies * len(base_doc.modelspace()):
                    problems.append("ezdxf reads %d modelspace entities, expected %d x %d" % (len(doc.modelspace()), copies, len(base_doc.modelspace())))
                verified = True
                method = "struct+ezdxf"
        if problems:
            failures += 1
            print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
            continue
        scaled_line("dxf-replicate", "%dMiB" % target, size, digest.hexdigest(), "ok",
                    ",base=%s,copies=%d,entities=%d,method=%s,file=%s" % (os.path.basename(base_path), copies, len(out_records), method, name))
    return failures


def scale_dxf_minsert(base_path, targets, out_dir):
    # ezdxf writes the OBJECTS section in set order, so the bytes depend on the hash seed
    if os.environ.get("PYTHONHASHSEED") != "0":
        raise SystemExit("dxf-minsert: run with PYTHONHASHSEED=0 (python -B, not -I) for a byte-identical output")
    import ezdxf

    ezdxf.options.write_fixed_meta_data_for_testing = True
    stem = stem_of(base_path)
    failures = 0
    for n in targets:
        if n < 1:
            raise SystemExit("dxf-minsert: a target is an expanded instance count of at least 1")
        cols = int(math.isqrt(n))
        while cols > 1 and n % cols:
            cols -= 1
        rows = n // cols
        doc = ezdxf.new("R2018")
        block = doc.blocks.new("CELL")
        block.add_circle((0.0, 0.0), 3.0)
        block.add_line((-4.0, 0.0), (4.0, 0.0))
        block.add_line((0.0, -4.0), (0.0, 4.0))
        block.add_lwpolyline([(-5.0, -5.0), (5.0, -5.0), (5.0, 5.0), (-5.0, 5.0)], close=True)
        block.add_text("A", height=1.0).set_placement((1.0, 1.0))
        doc.modelspace().add_blockref(
            "CELL", (0.0, 0.0),
            dxfattribs={"column_count": cols, "row_count": rows, "column_spacing": 12.0, "row_spacing": 12.0})
        name = "dxf-minsert-%s-n%d.dxf" % (stem, n)
        path = os.path.join(out_dir, name)
        doc.saveas(path)
        with open(path, "rb") as fh:
            blob = fh.read()
        problems = []
        back = ezdxf.readfile(path)
        inserts = [e for e in back.modelspace() if e.dxftype() == "INSERT"]
        if len(inserts) != 1:
            problems.append("%d INSERT entities, expected 1" % len(inserts))
        elif inserts[0].dxf.column_count * inserts[0].dxf.row_count != n:
            problems.append("grid %d x %d, expected %d cells" % (inserts[0].dxf.column_count, inserts[0].dxf.row_count, n))
        if problems:
            failures += 1
            print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
            continue
        scaled_line("dxf-minsert", "n%d" % n, len(blob), hashlib.sha256(blob).hexdigest(), "ok",
                    ",expanded=%d,cols=%d,rows=%d,block_entities=5,file=%s" % (n, cols, rows, name))
    return failures


def scale_iges_from_step(base_path, targets, out_dir):
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.IGESControl import IGESControl_Writer
    from OCP.STEPControl import STEPControl_Reader

    reader = STEPControl_Reader()
    if reader.ReadFile(base_path) != IFSelect_RetDone:
        raise SystemExit("iges-from-step: OCP cannot read the STEP base")
    reader.TransferRoots()
    shape = reader.OneShape()
    writer = IGESControl_Writer("MM", 0)
    writer.AddShape(shape)
    writer.ComputeModel()
    name = "iges-%s.igs" % stem_of(base_path)
    path = os.path.join(out_dir, name)
    if not writer.Write(path):
        raise SystemExit("iges-from-step: OCP could not write %s" % name)
    with open(path, "rb") as fh:
        blob = fh.read()
    stamped = re.sub(rb"15H[0-9]{8}\.[0-9]{6}", b"15H20000101.000000", blob)
    with open(path, "wb") as fh:
        fh.write(stamped)
    lines = stamped.split(b"\n")
    lines = [ln for ln in lines if ln.strip()]
    problems = []
    if not lines or b"T0000001" not in lines[-1]:
        problems.append("no terminate section")
    if not any(len(ln.rstrip(b"\r")) >= 73 and ln.rstrip(b"\r")[72:73] == b"D" for ln in lines):
        problems.append("no directory entries")
    if problems:
        print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
        return 1
    scaled_line("iges-from-step", "x1", len(stamped), hashlib.sha256(stamped).hexdigest(), "ok",
                ",base=%s,file=%s" % (os.path.basename(base_path), name))
    return 0


def run_scaled(args):
    if args.kind not in KINDS:
        raise SystemExit("--kind must be one of %s" % ", ".join(KINDS))
    if not args.targets:
        raise SystemExit("--targets is required with --scaled-from")
    if not os.path.isfile(args.scaled_from):
        raise SystemExit("--scaled-from: no such file")
    out_dir = args.scaled_dir or DEFAULT_SCALED
    try:
        os.makedirs(out_dir, exist_ok=True)
    except FileExistsError:
        pass
    targets = parse_targets(args.targets)
    runner = {
        "step-copies": scale_step_copies,
        "step-nauo": scale_step_nauo,
        "stl-tile": scale_stl_tile,
        "stl-ascii": scale_stl_ascii,
        "dxf-replicate": scale_dxf_replicate,
        "dxf-minsert": scale_dxf_minsert,
        "iges-from-step": scale_iges_from_step,
    }[args.kind]
    return 1 if runner(args.scaled_from, targets, out_dir) else 0


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--out-dir", default=DEFAULT_OUT)
    ap.add_argument("--check-only", action="store_true",
                    help="verify existing fixtures without rewriting them")
    ap.add_argument("--scaled-from", help="real base file to scale (not committed output)")
    ap.add_argument("--kind", help="step-copies|step-nauo|stl-tile|stl-ascii|dxf-replicate")
    ap.add_argument("--targets", help="comma separated MiB sizes, or instance counts for step-nauo, or expanded cells (columns x rows) for dxf-minsert")
    ap.add_argument("--scaled-dir", help="output directory for scaled files")
    args = ap.parse_args()

    if args.scaled_from:
        return run_scaled(args)

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

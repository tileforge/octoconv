#!/usr/bin/env python3
"""cad-corpus-gen.py -- deterministic generator for the phase 71 synthetic CAD corpus.

Usage:
    scripts/cad-corpus-gen.py [--out-dir DIR] [--check-only]
    scripts/cad-corpus-gen.py --reject [--out-dir DIR] [--check-only] [--provenance]
    scripts/cad-corpus-gen.py --scaled-from FILE --kind step-copies|step-nauo|stl-tile|stl-ascii|dxf-replicate|dxf-minsert|iges-from-step
        --targets LIST [--scaled-dir DIR]
    scripts/cad-corpus-gen.py --dxf-fixtures [--out-dir DIR] [--check-only]
    scripts/cad-corpus-gen.py --kind dxf-guard-stress --shape chain|fanout|blocks|wide --targets MiB [--scaled-dir DIR]
    scripts/cad-corpus-gen.py --scaled-from FILE --kind dxf-minsert --targets N [--block-entities N] [--scaled-dir DIR]
    scripts/cad-corpus-gen.py --scaled-from REPLICATE --kind dxf-combo --cells N [--scaled-dir DIR]
    scripts/cad-corpus-gen.py --scaled-from FILE --kind dxf-far-point --targets D,... [--scaled-dir DIR]

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
DEFAULT_REJECT_OUT = os.path.join("internal", "convert", "testdata", "cad", "reject")

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


def comments_999_pairs():
    pairs = [(999, "generated by scripts/cad-corpus-gen.py"), (999, "comment group before the first SECTION")]
    pairs += [(0, "SECTION"), (2, "ENTITIES"),
              (0, "LINE"), (8, "0"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (11, "10.0"), (21, "5.0"), (31, "0.0"),
              (0, "ENDSEC"), (0, "EOF")]
    return pairs


def build_comments_999():
    return dxf_pairs(comments_999_pairs())


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


def stl_binary_blob(header, tris):
    out = bytearray(header + struct.pack("<I", len(tris)))
    for normal, a, b, c in tris:
        out += struct.pack("<12fH", *normal, *a, *b, *c, 0)
    return bytes(out)


def build_stl_binary():
    header = b"solid octoconv binary box fixture"
    header = header + b"\x00" * (80 - len(header))
    return stl_binary_blob(header, box_triangles(10, 20, 30))


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
KINDS = ("step-copies", "step-nauo", "stl-tile", "stl-ascii", "dxf-replicate", "dxf-minsert", "iges-from-step", "dxf-combo", "dxf-far-point")
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


def dxf_grid(n):
    cols = int(math.isqrt(n))
    while cols > 1 and n % cols:
        cols -= 1
    return cols, n // cols


def dxf_cell_block(doc, block_entities):
    block = doc.blocks.new("CELL")
    for i in range(block_entities):
        dx = 0.1 * (i // 5)
        kind = i % 5
        if kind == 0:
            block.add_circle((0.0 + dx, 0.0), 3.0)
        elif kind == 1:
            block.add_line((-4.0 + dx, 0.0), (4.0 + dx, 0.0))
        elif kind == 2:
            block.add_line((0.0 + dx, -4.0), (0.0 + dx, 4.0))
        elif kind == 3:
            block.add_lwpolyline([(-5.0 + dx, -5.0), (5.0 + dx, -5.0), (5.0 + dx, 5.0), (-5.0 + dx, 5.0)], close=True)
        else:
            block.add_text("A", height=1.0).set_placement((1.0 + dx, 1.0))
    return block


def dxf_minsert_doc(n, block_entities):
    import ezdxf

    ezdxf.options.write_fixed_meta_data_for_testing = True
    cols, rows = dxf_grid(n)
    doc = ezdxf.new("R2018")
    dxf_cell_block(doc, block_entities)
    doc.modelspace().add_blockref(
        "CELL", (0.0, 0.0),
        dxfattribs={"column_count": cols, "row_count": rows, "column_spacing": 12.0, "row_spacing": 12.0})
    return doc, cols, rows


def scale_dxf_minsert(base_path, targets, out_dir, block_entities=5):
    # ezdxf writes the OBJECTS section in set order, so the bytes depend on the hash seed
    if os.environ.get("PYTHONHASHSEED") != "0":
        raise SystemExit("dxf-minsert: run with PYTHONHASHSEED=0 (python -B, not -I) for a byte-identical output")
    if block_entities < 1:
        raise SystemExit("dxf-minsert: --block-entities is at least 1")
    import ezdxf

    stem = stem_of(base_path)
    failures = 0
    for n in targets:
        if n < 1:
            raise SystemExit("dxf-minsert: a target is an expanded instance count of at least 1")
        doc, cols, rows = dxf_minsert_doc(n, block_entities)
        if block_entities == 5:
            name = "dxf-minsert-%s-n%d.dxf" % (stem, n)
        else:
            name = "dxf-minsert-%s-n%d-b%d.dxf" % (stem, n, block_entities)
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
        if len(back.blocks.get("CELL")) != block_entities:
            problems.append("CELL holds %d entities, expected %d" % (len(back.blocks.get("CELL")), block_entities))
        if problems:
            failures += 1
            print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
            continue
        scaled_line("dxf-minsert", "n%d" % n, len(blob), hashlib.sha256(blob).hexdigest(), "ok",
                    ",expanded=%d,cols=%d,rows=%d,block_entities=%d,file=%s" % (n, cols, rows, block_entities, name))
    return failures


def scale_dxf_far_point(base_path, targets, out_dir):
    if os.environ.get("PYTHONHASHSEED") != "0":
        raise SystemExit("dxf-far-point: run with PYTHONHASHSEED=0 (python -B, not -I) for a byte-identical output")
    import ezdxf

    ezdxf.options.write_fixed_meta_data_for_testing = True
    stem = stem_of(base_path)
    failures = 0
    for d in targets:
        if d < 1:
            raise SystemExit("dxf-far-point: a target is a distance of at least 1 drawing unit")
        doc = ezdxf.readfile(base_path)
        before = len(doc.modelspace().query("POINT"))
        doc.modelspace().add_point((float(d), float(d)))
        name = "dxf-farpoint-%s-d%d.dxf" % (stem, d)
        path = os.path.join(out_dir, name)
        doc.saveas(path)
        with open(path, "rb") as fh:
            blob = fh.read()
        back = ezdxf.readfile(path)
        auditor = back.audit()
        problems = []
        if len(back.modelspace().query("POINT")) != before + 1:
            problems.append("POINT count %d, expected %d" % (len(back.modelspace().query("POINT")), before + 1))
        if auditor.errors:
            problems.append("audit errors %s" % ",".join(str(e.code) for e in auditor.errors))
        if problems:
            failures += 1
            print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
            continue
        scaled_line("dxf-far-point", "d%d" % d, len(blob), hashlib.sha256(blob).hexdigest(), "ok",
                    ",base=%s,distance=%d,file=%s" % (os.path.basename(base_path), d, name))
    return failures


DXF_HANDLE_REF_CODES = set(range(320, 370)) | {390, 1005}


def dxf_text_records(text):
    lines = text.split("\n")
    pairs = [(int(lines[i].strip()), lines[i + 1]) for i in range(0, len(lines) - 1, 2)]
    records = []
    section = None
    pending_section = False
    for code, value in pairs:
        if code == 0:
            records.append({"section": section, "pairs": []})
            pending_section = value.strip() == "SECTION"
        elif code == 2 and pending_section:
            section = value.strip()
            pending_section = False
        records[-1]["pairs"].append((code, value))
    return records


def dxf_record_value(record, code):
    for c, v in record["pairs"]:
        if c == code:
            return v.strip()
    return None


def dxf_combo_template(cells):
    import io

    doc, cols, rows = dxf_minsert_doc(cells, 5)
    stream = io.StringIO()
    doc.write(stream)
    records = dxf_text_records(stream.getvalue())
    br_table = msp = None
    br_cell = None
    block = []
    insert = None
    in_block = False
    for rec in records:
        kind = rec["pairs"][0][1].strip()
        name = dxf_record_value(rec, 2)
        if rec["section"] == "TABLES" and kind == "TABLE" and name == "BLOCK_RECORD":
            br_table = dxf_record_value(rec, 5)
        elif rec["section"] == "TABLES" and kind == "BLOCK_RECORD" and name is not None:
            if name.lower() == "*model_space":
                msp = dxf_record_value(rec, 5)
            elif name == "CELL":
                br_cell = rec["pairs"]
        elif rec["section"] == "BLOCKS" and kind == "BLOCK" and name == "CELL":
            in_block = True
            block.extend(rec["pairs"])
        elif in_block:
            block.extend(rec["pairs"])
            if kind == "ENDBLK":
                in_block = False
        elif rec["section"] == "ENTITIES" and kind == "INSERT" and name == "CELL":
            insert = rec["pairs"]
    if None in (br_table, msp, br_cell, insert) or not block or in_block:
        raise SystemExit("dxf-combo: the template drawing lacks the CELL block, its record or its MINSERT")
    return br_table, msp, br_cell, block, insert, cols, rows


def dxf_lines_with_offsets(blob):
    pos = 0
    size = len(blob)
    while pos < size:
        end = blob.find(b"\n", pos)
        if end < 0:
            end = size
        yield pos, blob[pos:end].rstrip(b"\r")
        pos = end + 1


def dxf_combo_anchors(blob):
    it = dxf_lines_with_offsets(blob)
    section = None
    pending_section = False
    table = None
    pending_table = False
    record = None
    record_handle = record_name = None
    want_seed = False
    found = {"seed": None, "br_endtab": None, "blocks_endsec": None, "entities_endsec": None,
             "br_table": None, "msp": None, "names": set()}
    in_header_var = None

    def close_record():
        if record == b"BLOCK_RECORD" and section == b"TABLES":
            if record_name is not None:
                found["names"].add(record_name.lower())
                if record_name.lower() == b"*model_space":
                    found["msp"] = record_handle
        elif record == b"TABLE" and table == b"BLOCK_RECORD" and found["br_table"] is None:
            found["br_table"] = record_handle

    for (cpos, code_raw), (vpos, value) in zip(it, it):
        code = int(code_raw.strip())
        v = value.strip()
        if code == 0:
            close_record()
            record = v
            record_handle = record_name = None
            pending_section = v == b"SECTION"
            if v == b"TABLE":
                pending_table = True
            if v == b"ENDTAB" and table == b"BLOCK_RECORD" and found["br_endtab"] is None:
                found["br_endtab"] = cpos
            if v == b"ENDSEC" and section == b"BLOCKS":
                found["blocks_endsec"] = cpos
            if v == b"ENDSEC" and section == b"ENTITIES":
                found["entities_endsec"] = cpos
            continue
        if code == 2 and pending_section:
            section = v
            pending_section = False
            continue
        if code == 2 and pending_table:
            table = v
            pending_table = False
        if section == b"HEADER" and code == 9:
            in_header_var = v
            continue
        if section == b"HEADER" and in_header_var == b"$HANDSEED" and code == 5:
            found["seed"] = (vpos, len(value), int(v, 16))
            in_header_var = None
            continue
        if code == 5:
            record_handle = v.decode("ascii")
        elif code == 2:
            record_name = v
    close_record()
    return found


def scale_dxf_combo(base_path, cells, out_dir):
    if os.environ.get("PYTHONHASHSEED") != "0":
        raise SystemExit("dxf-combo: run with PYTHONHASHSEED=0 (python -B, not -I) for a byte-identical output")
    if cells is None or cells < 1:
        raise SystemExit("dxf-combo: --cells is at least 1")
    import ezdxf

    br_table, msp, br_cell, block, insert, cols, rows = dxf_combo_template(cells)
    with open(base_path, "rb") as fh:
        blob = fh.read()
    nl = b"\r\n" if b"\r\n" in blob[:4096] else b"\n"
    found = dxf_combo_anchors(blob)
    for key in ("seed", "br_endtab", "blocks_endsec", "entities_endsec", "br_table", "msp"):
        if found[key] is None:
            raise SystemExit("dxf-combo: the base lacks %s" % key)
    if b"cell" in found["names"]:
        raise SystemExit("dxf-combo: the base already has a block named CELL")
    seed_pos, seed_len, seed = found["seed"]
    own = [v.strip() for part in (br_cell, block, insert) for c, v in part if c == 5]
    mapping = {h: "%X" % (seed + i) for i, h in enumerate(own)}
    fixed = {br_table: found["br_table"], msp: found["msp"], "0": "0"}

    def render(pairs):
        out = []
        for code, value in pairs:
            if code == 5:
                value = mapping[value.strip()]
            elif code in DXF_HANDLE_REF_CODES:
                ref = value.strip()
                if ref in mapping:
                    value = mapping[ref]
                elif ref in fixed:
                    value = fixed[ref]
                else:
                    raise SystemExit("dxf-combo: the template references handle %s outside the copied records" % ref)
            out.append(b"%3d" % code + nl + value.encode("ascii") + nl)
        return b"".join(out)

    parts = [
        blob[:seed_pos], b"%X" % (seed + len(own)), blob[seed_pos + seed_len:found["br_endtab"]],
        render(br_cell), blob[found["br_endtab"]:found["blocks_endsec"]],
        render(block), blob[found["blocks_endsec"]:found["entities_endsec"]],
        render(insert), blob[found["entities_endsec"]:],
    ]
    if not (seed_pos < found["br_endtab"] < found["blocks_endsec"] < found["entities_endsec"]):
        raise SystemExit("dxf-combo: the base sections are not in HEADER, TABLES, BLOCKS, ENTITIES order")
    name = "dxf-combo-%s-c%d.dxf" % (stem_of(base_path), cells)
    path = os.path.join(out_dir, name)
    digest = hashlib.sha256()
    with open(path + ".tmp", "wb") as fh:
        for part in parts:
            fh.write(part)
            digest.update(part)
    del blob, parts
    problems = []
    doc = ezdxf.readfile(path + ".tmp")
    auditor = doc.audit()
    if auditor.errors:
        problems.append("audit errors %s" % ",".join(str(e.code) for e in auditor.errors))
    found_insert = [e for e in doc.modelspace() if e.dxftype() == "INSERT" and e.dxf.name == "CELL"]
    if len(found_insert) != 1:
        problems.append("%d CELL inserts, expected 1" % len(found_insert))
    elif found_insert[0].dxf.column_count * found_insert[0].dxf.row_count != cells:
        problems.append("grid %d x %d, expected %d cells" % (found_insert[0].dxf.column_count, found_insert[0].dxf.row_count, cells))
    if doc.blocks.get("CELL") is None or len(doc.blocks.get("CELL")) != 5:
        problems.append("CELL block missing or not 5 entities")
    if problems:
        os.remove(path + ".tmp")
        print("SCALE CHECK FAILED %s: %s" % (name, "; ".join(problems)), file=sys.stderr)
        return 1
    os.replace(path + ".tmp", path)
    scaled_line("dxf-combo", "c%d" % cells, os.path.getsize(path), digest.hexdigest(), "ok",
                ",base=%s,cells=%d,cols=%d,rows=%d,block_entities=5,auditor_fixes=%d,method=bytes+ezdxf,file=%s" % (
                    os.path.basename(base_path), cells, cols, rows, len(auditor.fixes), name))
    return 0


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


GUARD_STRESS_SHAPES = ("chain", "fanout", "blocks", "wide")
GUARD_STRESS_HEAD = b"0\nSECTION\n2\nHEADER\n9\n$ACADVER\n1\nAC1009\n9\n$DWGCODEPAGE\n3\nANSI_1252\n0\nENDSEC\n"
GUARD_STRESS_LINE = b"0\nLINE\n8\n0\n10\n0.0\n20\n0.0\n30\n0.0\n11\n1.0\n21\n0.0\n31\n0.0\n"


def guard_stress_parts(shape):
    if shape == "chain":
        head = b"0\nSECTION\n2\nBLOCKS\n"
        unit = lambda i: b"0\nBLOCK\n2\nC%d\n0\nINSERT\n2\nC%d\n0\nENDBLK\n" % (i, i + 1)
        tail = lambda n: (b"0\nBLOCK\n2\nC%d\n" % n + GUARD_STRESS_LINE + b"0\nENDBLK\n0\nENDSEC\n"
                          b"0\nSECTION\n2\nENTITIES\n0\nINSERT\n2\nC0\n0\nENDSEC\n0\nEOF\n")
    elif shape == "fanout":
        head = (b"0\nSECTION\n2\nBLOCKS\n0\nBLOCK\n2\nS\n" + GUARD_STRESS_LINE
                + b"0\nENDBLK\n0\nENDSEC\n0\nSECTION\n2\nENTITIES\n")
        unit = lambda i: b"0\nINSERT\n2\nS\n"
        tail = lambda n: b"0\nENDSEC\n0\nEOF\n"
    elif shape == "blocks":
        head = b"0\nSECTION\n2\nBLOCKS\n"
        unit = lambda i: b"0\nBLOCK\n2\nB%d\n0\nENDBLK\n" % i
        tail = lambda n: b"0\nENDSEC\n0\nSECTION\n2\nENTITIES\n" + GUARD_STRESS_LINE + b"0\nENDSEC\n0\nEOF\n"
    elif shape == "wide":
        head = b"0\nSECTION\n2\nENTITIES\n"
        unit = lambda i: b"0\nINSERT\n2\nW%d\n" % i
        tail = lambda n: b"0\nENDSEC\n0\nEOF\n"
    else:
        raise SystemExit("--shape must be one of %s" % ", ".join(GUARD_STRESS_SHAPES))
    return head, unit, tail


def run_guard_stress(args):
    if not args.shape or not args.targets:
        raise SystemExit("--kind dxf-guard-stress needs --shape and --targets")
    out_dir = args.scaled_dir or DEFAULT_SCALED
    os.makedirs(out_dir, exist_ok=True)
    for target in parse_targets(args.targets):
        head, unit, tail = guard_stress_parts(args.shape)
        name = "dxf-guard-stress-%s-%dMiB.dxf" % (args.shape, target)
        path = os.path.join(out_dir, name)
        limit = target * MIB - len(tail(10 ** 9))
        digest = hashlib.sha256()
        count = 0
        with open(path + ".tmp", "wb") as fh:
            size = len(GUARD_STRESS_HEAD) + len(head)
            chunk = [GUARD_STRESS_HEAD, head]
            while True:
                part = unit(count)
                if size + len(part) > limit:
                    break
                chunk.append(part)
                size += len(part)
                count += 1
                if len(chunk) >= 65536:
                    blob = b"".join(chunk)
                    fh.write(blob)
                    digest.update(blob)
                    chunk = []
            chunk.append(tail(count))
            blob = b"".join(chunk)
            fh.write(blob)
            digest.update(blob)
        os.replace(path + ".tmp", path)
        scaled_line("dxf-guard-stress", "%dMiB" % target, os.path.getsize(path), digest.hexdigest(), "ok",
                    ",shape=%s,units=%d,file=%s" % (args.shape, count, name))
    return 0


def run_scaled(args):
    if args.kind not in KINDS:
        raise SystemExit("--kind must be one of %s" % ", ".join(KINDS))
    if not os.path.isfile(args.scaled_from):
        raise SystemExit("--scaled-from: no such file")
    out_dir = args.scaled_dir or DEFAULT_SCALED
    try:
        os.makedirs(out_dir, exist_ok=True)
    except FileExistsError:
        pass
    if args.kind == "dxf-combo":
        if args.targets:
            raise SystemExit("--kind dxf-combo takes --cells, not --targets")
        return scale_dxf_combo(args.scaled_from, args.cells, out_dir)
    if not args.targets:
        raise SystemExit("--targets is required with --scaled-from")
    if args.block_entities is not None and args.kind != "dxf-minsert":
        raise SystemExit("--block-entities applies to --kind dxf-minsert only")
    if args.kind == "dxf-minsert":
        return 1 if scale_dxf_minsert(args.scaled_from, parse_targets(args.targets), out_dir,
                                      5 if args.block_entities is None else args.block_entities) else 0
    targets = parse_targets(args.targets)
    runner = {
        "step-copies": scale_step_copies,
        "step-nauo": scale_step_nauo,
        "stl-tile": scale_stl_tile,
        "stl-ascii": scale_stl_ascii,
        "dxf-replicate": scale_dxf_replicate,
        "dxf-minsert": scale_dxf_minsert,
        "dxf-far-point": scale_dxf_far_point,
        "iges-from-step": scale_iges_from_step,
    }[args.kind]
    return 1 if runner(args.scaled_from, targets, out_dir) else 0


UTF8_BOM = b"\xef\xbb\xbf"
DXF_BINARY_SENTINEL = b"AutoCAD Binary DXF\r\n\x1a\x00"
STEP_END = b"END-ISO-10303-21;"
BOX_REJECT_HEADER = "octoconv phase 72 reject fixture"
WS = b" \t\r\n\f\v"

REJECT_FIXTURES = [
    "step_truncated.step",
    "step_missing_header.step",
    "step_trailing_nul.step",
    "step_bom.step",
    "iges_truncated.iges",
    "dxf_truncated.dxf",
    "dxf_bom.dxf",
    "dxf_binary.dxf",
    "dwg_ac1027.dwg",
    "stl_binary_truncated.stl",
    "stl_binary_trailing_byte.stl",
    "stl_ascii_truncated.stl",
    "stl_ambiguous.stl",
    "stl_binary_ac10_header.stl",
    "stl_binary_empty.stl",
]

REJECT_EXPECTED = {
    "step_truncated.step": [b"ISO-10303-21;", b"HEADER;"],
    "step_missing_header.step": [b"ISO-10303-21;", b"HEADEX;", STEP_END],
    "step_trailing_nul.step": [b"ISO-10303-21;", b"HEADER;", STEP_END],
    "step_bom.step": [UTF8_BOM + b"ISO-10303-21;", b"HEADER;", STEP_END],
    "iges_truncated.iges": [b"S0000001", b"P0000049"],
    "dxf_truncated.dxf": [b"SECTION", b"ENDSEC"],
    "dxf_bom.dxf": [UTF8_BOM + b"999", b"EOF"],
    "dxf_binary.dxf": [DXF_BINARY_SENTINEL],
    "dwg_ac1027.dwg": [b"AC1027"],
    "stl_binary_truncated.stl": [BOX_REJECT_HEADER.encode("ascii")],
    "stl_binary_trailing_byte.stl": [BOX_REJECT_HEADER.encode("ascii")],
    "stl_ascii_truncated.stl": [b"solid box", b"facet normal"],
    "stl_ambiguous.stl": [b"solid ambiguous", b"endsolid"],
    "stl_binary_ac10_header.stl": [b"AC1027 bracket"],
    "stl_binary_empty.stl": [b"octoconv empty binary stl"],
}

REJECT_PROVES = {
    "step_truncated.step": "Truncated STEP. The first half of synthetic/asymmetric_l_bracket.step: the ISO-10303-21; magic and HEADER; are present, the END-ISO-10303-21; trailer is not, so a head-only check would call it STEP and the tail window must refuse it.",
    "step_missing_header.step": "STEP without a header section. synthetic/units_inch.step with its single HEADER; replaced by HEADEX;: the magic and the trailer are present, so only the HEADER; requirement can refuse it.",
    "step_trailing_nul.step": "STEP with a trailing NUL byte. synthetic/units_inch.step plus one 0x00 after the trailer: ASCII whitespace stripping does not remove a NUL, so the trailer is not at the end and the file is refused.",
    "step_bom.step": "STEP behind a UTF-8 byte order mark. EF BB BF followed by synthetic/units_inch.step: the magic is not at the start of the file, so the file stays refused until a differential against the engine decides otherwise.",
    "iges_truncated.iges": "Truncated IGES. synthetic/surfaces_only.iges without its last 81-byte record, the T terminate record: the start, global, directory and parameter sections are intact and the terminate record is not.",
    "dxf_truncated.dxf": "Truncated ASCII DXF. The comments_999 pair list without its final (0, EOF) pair: the file ends after ENDSEC, so the EOF group is missing from the tail.",
    "dxf_bom.dxf": "ASCII DXF behind a UTF-8 byte order mark. EF BB BF followed by the comments_999 file: the first group code is not at the start of the file, so the file stays refused until a differential decides otherwise.",
    "dxf_binary.dxf": "Binary DXF sentinel. The 22-byte AutoCAD Binary DXF sentinel followed by 64 zero bytes, not a readable binary DXF: it exercises the sentinel rule only.",
    "dwg_ac1027.dwg": "DWG version magic. ASCII AC1027 (AutoCAD 2013) followed by 122 zero bytes, 128 bytes in all: a DWG is recognised by its magic and refused for being a binary format the service has no engine for.",
    "stl_binary_truncated.stl": "Truncated binary STL. A 12-triangle box with its last byte removed: the size formula 84 + 50 * n is off by one byte short, and the header carries no solid marker.",
    "stl_binary_trailing_byte.stl": "Binary STL with a trailing byte. A 12-triangle box plus one 0x00: the size formula is off by one byte long.",
    "stl_ascii_truncated.stl": "Truncated ASCII STL. The ascii box cut at the first byte of its 7th facet normal line: it starts with solid and carries six whole facets but no endsolid line.",
    "stl_ambiguous.stl": "Ambiguous STL. A binary box whose 80-byte header starts with solid ambiguous and whose final 10 bytes are LF endsolid LF: the size formula holds, it starts with solid and its last line is endsolid, so both the ASCII and the binary reading are valid on the markers.",
    "stl_binary_ac10_header.stl": "Binary STL whose header starts with AC1027. A 12-triangle box whose size formula holds: acceptance case kept beside the rejections, so that the STL size formula takes precedence over the DWG magic.",
    "stl_binary_empty.stl": "Binary STL with no triangles. An 80-byte header plus uint32 0, 84 bytes: acceptance case kept beside the rejections, n = 0 passes the size formula at sniff level and the triangle guard belongs to phase 73.",
}


def read_synthetic(name):
    with open(os.path.join(DEFAULT_OUT, name), "rb") as fh:
        return fh.read()


def stl_header80(text, pad):
    raw = text.encode("ascii")
    return raw + pad * (80 - len(raw))


def build_reject_all():
    box = box_triangles(10, 20, 30)
    box_header = stl_header80(BOX_REJECT_HEADER, b"\x00")
    box_blob = stl_binary_blob(box_header, box)
    bracket = read_synthetic("asymmetric_l_bracket.step")
    inch = read_synthetic("units_inch.step")
    iges = read_synthetic("surfaces_only.iges")
    if inch.count(b"HEADER;") != 1:
        raise SystemExit("units_inch.step must carry exactly one HEADER;")
    ascii_stl = build_stl_ascii()
    offset = 0
    seen = 0
    for line in ascii_stl.split(b"\n"):
        if line.startswith(b"  facet normal"):
            seen += 1
            if seen == 7:
                break
        offset += len(line) + 1
    else:
        raise SystemExit("build_stl_ascii has fewer than 7 facets")
    ambiguous = bytearray(stl_binary_blob(stl_header80("solid ambiguous", b" "), box))
    ambiguous[-10:] = b"\nendsolid\n"
    return {
        "step_truncated.step": bracket[:len(bracket) // 2],
        "step_missing_header.step": inch.replace(b"HEADER;", b"HEADEX;"),
        "step_trailing_nul.step": inch + b"\x00",
        "step_bom.step": UTF8_BOM + inch,
        "iges_truncated.iges": iges[:-81],
        "dxf_truncated.dxf": dxf_pairs(comments_999_pairs()[:-1]),
        "dxf_bom.dxf": UTF8_BOM + build_comments_999(),
        "dxf_binary.dxf": DXF_BINARY_SENTINEL + b"\x00" * 64,
        "dwg_ac1027.dwg": b"AC1027" + b"\x00" * 122,
        "stl_binary_truncated.stl": box_blob[:-1],
        "stl_binary_trailing_byte.stl": box_blob + b"\x00",
        "stl_ascii_truncated.stl": ascii_stl[:offset],
        "stl_ambiguous.stl": bytes(ambiguous),
        "stl_binary_ac10_header.stl": stl_binary_blob(stl_header80("AC1027 bracket", b"\x00"), box),
        "stl_binary_empty.stl": stl_header80("octoconv empty binary stl", b"\x00") + struct.pack("<I", 0),
    }


def stl_size_gap(blob):
    if len(blob) < 84:
        return None
    return len(blob) - (84 + 50 * struct.unpack("<I", blob[80:84])[0])


def last_nonblank_line(blob):
    for line in reversed(blob.split(b"\n")):
        if line.strip():
            return line.strip()
    return b""


def check_reject_structure(name, blob):
    problems = []
    if name == "step_truncated.step":
        if STEP_END in blob[-4096:]:
            problems.append("trailer present in the last 4096 bytes")
    elif name == "step_missing_header.step":
        if b"HEADER;" in blob:
            problems.append("HEADER; still present")
    elif name == "step_trailing_nul.step":
        if not blob.endswith(b"\x00"):
            problems.append("does not end with a NUL byte")
    elif name in ("step_bom.step", "dxf_bom.dxf"):
        if not blob.startswith(UTF8_BOM):
            problems.append("does not start with EF BB BF")
    elif name == "iges_truncated.iges":
        records = [line for line in blob.split(b"\n") if line]
        last = records[-1] if records else b""
        if len(last) != 80:
            problems.append("last record is %d columns, want 80" % len(last))
        if last[72:73] == b"T" or last[72:73] not in (b"S", b"G", b"D", b"P"):
            problems.append("last record column 73 is %r, want a section letter other than T" % last[72:73])
    elif name == "dxf_truncated.dxf":
        lines = [line for line in blob.split(b"\r\n") if line.strip()]
        if not lines or lines[-1].strip() == b"EOF":
            problems.append("ends with an EOF line")
    elif name == "dxf_binary.dxf":
        if len(blob) != len(DXF_BINARY_SENTINEL) + 64 or not blob.startswith(DXF_BINARY_SENTINEL):
            problems.append("not the 22-byte sentinel followed by 64 zero bytes")
    elif name == "dwg_ac1027.dwg":
        if len(blob) != 128 or not blob.startswith(b"AC1027"):
            problems.append("not 128 bytes starting with AC1027")
    elif name == "stl_binary_truncated.stl":
        if stl_size_gap(blob) != -1:
            problems.append("size formula gap is %r, want -1" % stl_size_gap(blob))
    elif name == "stl_binary_trailing_byte.stl":
        if stl_size_gap(blob) != 1:
            problems.append("size formula gap is %r, want 1" % stl_size_gap(blob))
    elif name == "stl_ascii_truncated.stl":
        if b"endsolid" in blob or blob.count(b"facet normal") != 6:
            problems.append("not six whole facets without endsolid")
    elif name == "stl_ambiguous.stl":
        if stl_size_gap(blob) != 0:
            problems.append("size formula does not hold, gap %r" % stl_size_gap(blob))
        if not blob.startswith(b"solid"):
            problems.append("does not start with solid")
        if not last_nonblank_line(blob).startswith(b"endsolid"):
            problems.append("last non-blank line does not start with endsolid")
    elif name == "stl_binary_ac10_header.stl":
        if stl_size_gap(blob) != 0 or not blob.startswith(b"AC1027"):
            problems.append("size formula must hold and the header must start with AC1027")
    elif name == "stl_binary_empty.stl":
        if len(blob) != 84 or blob[80:84] != b"\x00\x00\x00\x00":
            problems.append("not an 84-byte header with triangle count 0")
    return problems


def reject_failures(out_dir):
    failures = []
    for name in REJECT_FIXTURES:
        path = os.path.join(out_dir, name)
        if not os.path.exists(path):
            failures.append("%s: missing" % name)
            continue
        with open(path, "rb") as fh:
            blob = fh.read()
        for marker in REJECT_EXPECTED[name]:
            if marker not in blob:
                failures.append("%s: marker %r absent from raw bytes" % (name, marker))
        failures += ["%s: %s" % (name, p) for p in check_reject_structure(name, blob)]
    return failures


def check_reject(out_dir):
    failures = reject_failures(out_dir)
    if failures:
        print("SELF-CHECK FAILED:", file=sys.stderr)
        for f in failures:
            print("  " + f, file=sys.stderr)
        return 1
    for name in REJECT_FIXTURES:
        print("  %s: %d bytes" % (name, os.path.getsize(os.path.join(out_dir, name))))
    print("self-check passed: every reject fixture carries the markers and the structure it exists to carry")
    return 0


def reject_provenance(out_dir):
    lines = [
        "<!-- reject-provenance-begin -->",
        "## Rejection fixtures (reject/)",
        "Generated by `python3 scripts/cad-corpus-gen.py --reject` from committed files in `synthetic/` and the stdlib builders of that script, no OCP; committed before any code that judges them; excluded from the three phase-71 corpus walkers so the inventory numbers stay comparable. `stl_binary_ac10_header.stl` and `stl_binary_empty.stl` are acceptance cases kept beside the rejections, for STL-over-magic precedence and for n = 0 accepted at sniff level with the triangle guard in phase 73; `dxf_binary.dxf` is the sentinel followed by zero bytes, not a readable binary DXF, so it exercises the sentinel rule only.",
        "Generation command for every file below: `python3 scripts/cad-corpus-gen.py --reject`",
        "",
    ]
    for name in REJECT_FIXTURES:
        path = os.path.join(out_dir, name)
        with open(path, "rb") as fh:
            blob = fh.read()
        lines += [
            "## `reject/%s` — %d bytes" % (name, os.path.getsize(path)),
            "",
            "- Licence: generated",
            "- sha256: %s" % hashlib.sha256(blob).hexdigest(),
            "- Command: see the generation command above",
            "",
            "**Proves:** %s" % REJECT_PROVES[name],
            "",
        ]
    lines.append("<!-- reject-provenance-end -->")
    return "\n".join(lines) + "\n"


def run_reject(args):
    out_dir = args.out_dir or DEFAULT_REJECT_OUT
    if args.provenance:
        failures = reject_failures(out_dir)
        if failures:
            for f in failures:
                print("  " + f, file=sys.stderr)
            return 1
        sys.stdout.buffer.write(reject_provenance(out_dir).encode("utf-8"))
        return 0
    if not args.check_only:
        try:
            os.makedirs(out_dir, exist_ok=True)
        except FileExistsError:
            pass
        for name, blob in build_reject_all().items():
            with open(os.path.join(out_dir, name), "wb") as fh:
                fh.write(blob)
        print("generated reject fixtures into %s" % out_dir)
    return check_reject(out_dir)


DEFAULT_DXF_OUT = os.path.join("internal", "convert", "testdata", "cad", "dxf")
DXF_INSERT_CAP = 170031
DXF_UNIT_CODES = (0, 1, 2, 3, 4, 5, 6, 13, 21)
DXF_TEXT_SPECIAL = "A&B <x> \"q\" 'a' & <script>alert(1)</script>"
DXF_TEXT_WIDE = "Lat中文Ω😀end"
DXF_TEXT_ILLEGAL = b"AB\x01CD\xffEF"
DXF_TEXT_PLACEHOLDER = "ILLEGALPLACEHOLDER"
DXF_NONNUMERIC = {
    "nonnumeric_abc": "abc",
    "nonnumeric_comma": "1,5",
    "nonnumeric_nan": "nan",
    "nonnumeric_inf": "inf",
    "nonnumeric_underscore": "1_0",
    "coord_1e300": "1e300",
    "coord_1e400": "1e400",
}
DXF_NONFINITE = {
    "nonfinite_header": ("HEADER", "nan"),
    "nonfinite_tables": ("TABLES", "inf"),
    "nonfinite_objects": ("OBJECTS", "1_0"),
}
DXF_FIXTURES = (
    ["text_cases.dxf", "text_illegal_chars.dxf", "layers_off_frozen.dxf", "all_hidden.dxf",
     "never_resolved.dxf", "wrong_extents.dxf", "header_far_extents.dxf", "wide.dxf", "tall.dxf",
     "far_point.dxf", "single_point.dxf", "xline_ray.dxf", "text_alpha.dxf", "text_alpha_faint.dxf", "text_fonts.dxf",
     "alpha_clear_geometry.dxf", "alpha_clear_all.dxf", "alpha_one_visible.dxf", "alpha_one_fill.dxf"]
    + ["units_%d.dxf" % c for c in DXF_UNIT_CODES] + ["units_missing.dxf"]
    + ["cyclic_self.dxf", "cyclic_ab.dxf", "cyclic_unreferenced.dxf", "cyclic_case.dxf",
       "nest_d16.dxf", "nest_d17.dxf", "minsert_over_inserts.dxf", "entities_over.dxf",
       "dimension_minsert.dxf"]
    + ["%s.dxf" % n for n in DXF_NONNUMERIC] + ["%s.dxf" % n for n in DXF_NONFINITE]
)


DXF_ALPHA_HALF = b"440\n33554559\n"
DXF_ALPHA_CLEAR = b"440\n33554432\n"
DXF_ALPHA_BYBLOCK = b"440\n16777216\n"
DXF_HALF_TRANSPARENCY = 0x0200007F
DXF_CLEAR_MARKERS = [DXF_ALPHA_CLEAR, DXF_ALPHA_BYBLOCK, b"\nLINE\n", b"\nCIRCLE\n", b"\nARC\n", b"\nLWPOLYLINE\n",
                     b"\nHATCH\n", b"\nSOLID\n", b"\nPOINT\n", b"\nXLINE\n", b"\nINSERT\n", b"CLEARLAYER"]


def dxf_value_pair(code, value):
    return b"%3d\r\n" % code + value.encode("ascii") + b"\r\n"


def dxf_expected():
    exp = {
        "text_cases.dxf": [b"AC1032", b"$INSUNITS\n 70\n4\n", DXF_TEXT_SPECIAL.encode("utf-8"), DXF_TEXT_WIDE.encode("utf-8"),
                           b"%%c50 %%d %%p0.1", b"Attr<&>", b"ATTRIB", b"MTEXT", b"DIMENSION", b"\\S1^2;"],
        "text_illegal_chars.dxf": [b"AC1032", b"\n" + DXF_TEXT_ILLEGAL + b"\n"],
        "layers_off_frozen.dxf": [b"OFFL", b"FROZ", b"HIDDEN_OFF", b"HIDDEN_FROZEN", b"VISIBLE"],
        "all_hidden.dxf": [b"OFFL", b"FROZ", b"HIDDEN_OFF", b"HIDDEN_FROZEN"],
        "never_resolved.dxf": [b"IMAGEDEF", b"/etc/passwd", b"../../outside/secret.png", b"PDFDEFINITION",
                               b"/etc/hostname", b"OLE2FRAME", b"VISIBLE"],
        "wrong_extents.dxf": [b"$EXTMIN\n 10\n-1000000000.0\n", b"$EXTMAX\n 10\n1000000000.0\n"],
        "header_far_extents.dxf": [b"$EXTMIN\n 10\n-1e+20\n", b"$EXTMAX\n 10\n1e+20\n"],
        "wide.dxf": [b"1000.0", b"WIDE"],
        "tall.dxf": [b"1000.0", b"TALL"],
        "far_point.dxf": [b"POINT", b"100000.0"],
        "single_point.dxf": [b"POINT"],
        "xline_ray.dxf": [b"XLINE", b"RAY", b" 11\n999.0\n"],
        "text_alpha.dxf": [b"AC1032", b"OPAQUE TEXT", b"HALF TEXT", b"CLEAR TEXT", b"OPAQUE MTEXT", b"HALF MTEXT",
                           b"CLEAR MTEXT", b"LAYER TEXT", b"BLOCK TEXT", b"HALF\\PSPLIT", b"\\C1;INLINE RED", b"CLEAR\\PSPLIT",
                           b"XLINE", b"HATCH", DXF_ALPHA_HALF, DXF_ALPHA_CLEAR,
                           DXF_ALPHA_BYBLOCK, b"AcCmTransparency"],
        "text_alpha_faint.dxf": [b"AC1032", b"FAINT ONLY", b"GONE", DXF_ALPHA_HALF, DXF_ALPHA_CLEAR],
        "text_fonts.dxf": [b"AC1032"] + [f.encode("ascii") for _, f, _ in DXF_FONT_STYLES]
                          + [b"ACAD", b"DejaVu Sans Mono", b"\\fDejaVu Sans Mono|b1|i1;INLINE MONO BOLD ITALIC",
                             b"SHEARED STYLE", b"SHEARED TEXT", b" 51\n15.0\n", b" 41\n0.7\n"],
        "alpha_clear_geometry.dxf": [b"AC1032"] + DXF_CLEAR_MARKERS,
        "alpha_clear_all.dxf": [b"AC1032", b"CLEAR TEXT", b"CLEAR MTEXT"] + DXF_CLEAR_MARKERS,
        "alpha_one_visible.dxf": [b"AC1032", b"CLEAR TEXT", b"CLEAR MTEXT", DXF_ALPHA_HALF] + DXF_CLEAR_MARKERS,
        "alpha_one_fill.dxf": [b"AC1032", b"CLEAR TEXT", b"CLEAR MTEXT", DXF_ALPHA_HALF] + DXF_CLEAR_MARKERS,
        "units_missing.dxf": [b"AC1032", b"LINE"],
        "cyclic_self.dxf": [b"BLOCKS", b"INSERT"],
        "cyclic_ab.dxf": [b"BLOCKS", b"INSERT"],
        "cyclic_unreferenced.dxf": [b"BLOCKS", b"INSERT"],
        "cyclic_case.dxf": [b"BLOCKS", b"\r\n  2\r\na\r\n"],
        "nest_d16.dxf": [b"N16\r\n"],
        "nest_d17.dxf": [b"N17\r\n"],
        "minsert_over_inserts.dxf": [b" 70\r\n16\r\n", b" 71\r\n10627\r\n"],
        "entities_over.dxf": [b"BIG\r\n"],
        "dimension_minsert.dxf": [b"DIMENSION", b"*D1", b" 70\r\n1000\r\n", b" 71\r\n1000\r\n"],
    }
    for c in DXF_UNIT_CODES:
        exp["units_%d.dxf" % c] = [b"AC1032", b"$INSUNITS\n 70\n%d\n" % c]
    for name, literal in DXF_NONNUMERIC.items():
        exp[name + ".dxf"] = [b"ENTITIES", dxf_value_pair(10, literal)]
    for name, (section, literal) in DXF_NONFINITE.items():
        exp[name + ".dxf"] = [b"AC1032", section.encode("ascii"), b"\n" + literal.encode("ascii") + b"\n"]
    return exp


DXF_ABSENT = {"units_missing.dxf": [b"$INSUNITS"], "text_alpha_faint.dxf": [b"\nLINE\n", b"\nHATCH\n", b"\nMTEXT\n"],
              "alpha_clear_geometry.dxf": [DXF_ALPHA_HALF, b"\nTEXT\n", b"\nMTEXT\n"],
              "alpha_clear_all.dxf": [DXF_ALPHA_HALF]}


def dxf_ezdxf_bytes(doc):
    with tempfile.TemporaryDirectory() as tmp:
        path = os.path.join(tmp, "fixture.dxf")
        doc.saveas(path)
        with open(path, "rb") as fh:
            return fh.read()


def dxf_new(ezdxf, units=4, setup=False):
    doc = ezdxf.new("R2018", setup=setup)
    if units is None:
        del doc.header["$INSUNITS"]
    else:
        doc.header["$INSUNITS"] = units
    return doc


def dxf_rect(layout, x0, y0, x1, y1, dxfattribs=None):
    for a, b in (((x0, y0), (x1, y0)), ((x1, y0), (x1, y1)), ((x1, y1), (x0, y1)), ((x0, y1), (x0, y0))):
        layout.add_line(a, b, dxfattribs=dxfattribs)


def build_dxf_text_cases(ezdxf):
    from ezdxf.enums import TextEntityAlignment as Align
    doc = dxf_new(ezdxf, 4, setup=True)
    msp = doc.modelspace()
    msp.add_text(DXF_TEXT_SPECIAL, height=2.5).set_placement((0, 100))
    msp.add_text("Rotated 30", height=2.5, rotation=30).set_placement((0, 85))
    msp.add_text("Centre", height=2.5).set_placement((100, 85), align=Align.MIDDLE_CENTER)
    msp.add_text("Right", height=2.5).set_placement((200, 85), align=Align.RIGHT)
    msp.add_text("Fit text", height=2.5).set_placement((0, 72), (60, 72), align=Align.FIT)
    msp.add_text("Aligned text", height=2.5).set_placement((100, 72), (180, 72), align=Align.ALIGNED)
    msp.add_text("Width half", height=2.5, dxfattribs={"width": 0.5}).set_placement((0, 62))
    msp.add_text("%%c50 %%d %%p0.1", height=2.5).set_placement((100, 62))
    msp.add_mtext(
        "First paragraph\\PSecond \\H5;tall\\H2.5; \\C1;red\\C256; \\fDejaVu Sans|b1|i0;bold\\fDejaVu Sans|b0|i0; \\S1^2;",
        dxfattribs={"char_height": 2.5, "width": 150.0},
    ).set_location((0, 52))
    block = doc.blocks.new("TAG")
    block.add_line((0, 0), (20, 0))
    block.add_attdef("VALUE", (0, 1), dxfattribs={"height": 2.5})
    msp.add_blockref("TAG", (0, 25)).add_auto_attribs({"VALUE": "Attr<&>"})
    msp.add_linear_dim(base=(100, 35), p1=(100, 25), p2=(160, 25), dimstyle="EZDXF").render()
    msp.add_text(DXF_TEXT_WIDE, height=2.5).set_placement((0, 8))
    dxf_rect(msp, -10, -10, 230, 115)
    return dxf_ezdxf_bytes(doc)


def build_dxf_text_illegal(ezdxf):
    doc = dxf_new(ezdxf, 4)
    msp = doc.modelspace()
    msp.add_text(DXF_TEXT_PLACEHOLDER, height=2.5).set_placement((0, 0))
    msp.add_line((0, -5), (40, -5))
    blob = dxf_ezdxf_bytes(doc)
    marker = b"\n" + DXF_TEXT_PLACEHOLDER.encode("ascii") + b"\n"
    if blob.count(marker) != 1:
        raise SystemExit("text_illegal_chars.dxf: the placeholder TEXT value is not unique")
    return blob.replace(marker, b"\n" + DXF_TEXT_ILLEGAL + b"\n")


def build_dxf_layers(ezdxf, with_visible):
    doc = dxf_new(ezdxf, 4)
    doc.layers.add("OFFL").off()
    doc.layers.add("FROZ").freeze()
    msp = doc.modelspace()
    msp.add_text("HIDDEN_OFF", height=2.5, dxfattribs={"layer": "OFFL"}).set_placement((0, 20))
    msp.add_line((0, 15), (40, 15), dxfattribs={"layer": "OFFL"})
    msp.add_text("HIDDEN_FROZEN", height=2.5, dxfattribs={"layer": "FROZ"}).set_placement((0, 10))
    msp.add_line((0, 5), (40, 5), dxfattribs={"layer": "FROZ"})
    if with_visible:
        msp.add_text("VISIBLE", height=2.5, dxfattribs={"layer": "0"}).set_placement((0, 0))
        msp.add_line((0, -5), (40, -5), dxfattribs={"layer": "0"})
    return dxf_ezdxf_bytes(doc)


def build_dxf_never_resolved(ezdxf):
    doc = dxf_new(ezdxf, 4)
    msp = doc.modelspace()
    passwd = doc.add_image_def(filename="/etc/passwd", size_in_pixel=(64, 64), name="PASSWD")
    msp.add_image(passwd, insert=(0, 0), size_in_units=(10, 10))
    outside = doc.add_image_def(filename="../../outside/secret.png", size_in_pixel=(64, 64), name="SECRET")
    msp.add_image(outside, insert=(20, 0), size_in_units=(10, 10))
    underlay = doc.add_underlay_def(filename="/etc/hostname", fmt="pdf", name="1")
    msp.add_underlay(underlay, insert=(40, 0), scale=(1, 1, 1))
    doc.add_xref_def(filename="/etc/passwd", name="XREFPASSWD")
    msp.add_blockref("XREFPASSWD", (60, 0))
    from ezdxf.lldxf.tags import Tags
    from ezdxf.lldxf.types import DXFBinaryTag, DXFTag, DXFVertex
    ole = msp.new_entity("OLE2FRAME", dxfattribs={})
    ole.acdb_ole2frame = Tags([
        DXFTag(100, "AcDbOle2Frame"), DXFTag(70, 2), DXFTag(3, "Paintbrush Picture"),
        DXFVertex(10, (80.0, 0.0, 0.0)), DXFVertex(11, (90.0, -10.0, 0.0)),
        DXFTag(71, 2), DXFTag(72, 0), DXFTag(73, 2), DXFTag(90, 4),
        DXFBinaryTag(310, b"\x01\x02\x03\x04"), DXFTag(1, "OLE"),
    ])
    msp.add_line((0, -20), (90, -20))
    msp.add_text("VISIBLE", height=2.5).set_placement((0, -30))
    return dxf_ezdxf_bytes(doc)


def dxf_set_header_point(blob, var, values):
    lines = blob.split(b"\n")
    for i in range(0, len(lines) - 7, 2):
        if lines[i].strip() == b"9" and lines[i + 1].strip() == var.encode("ascii"):
            for k, code in enumerate((b"10", b"20", b"30")):
                if lines[i + 2 + 2 * k].strip() != code:
                    raise SystemExit("%s: unexpected group code layout in the header" % var)
                lines[i + 3 + 2 * k] = values[k].encode("ascii")
            return b"\n".join(lines)
    raise SystemExit("%s: header variable not found" % var)


def build_dxf_extents(ezdxf, extmin, extmax):
    doc = dxf_new(ezdxf, 4)
    doc.modelspace().add_line((10, 20), (40, 30))
    blob = dxf_ezdxf_bytes(doc)
    blob = dxf_set_header_point(blob, "$EXTMIN", extmin)
    return dxf_set_header_point(blob, "$EXTMAX", extmax)


def build_dxf_box(ezdxf, w, h, label):
    doc = dxf_new(ezdxf, 4)
    msp = doc.modelspace()
    dxf_rect(msp, 0, 0, w, h)
    msp.add_text(label, height=2.0).set_placement((1, 1))
    return dxf_ezdxf_bytes(doc)


def build_dxf_far_point(ezdxf):
    doc = dxf_new(ezdxf, 4)
    msp = doc.modelspace()
    dxf_rect(msp, 0, 0, 10, 10)
    msp.add_line((0, 0), (10, 10))
    msp.add_point((100000, 100000))
    return dxf_ezdxf_bytes(doc)


def build_dxf_single_point(ezdxf):
    doc = dxf_new(ezdxf, 4)
    doc.modelspace().add_point((5, 5))
    return dxf_ezdxf_bytes(doc)


def build_dxf_xline_ray(ezdxf):
    doc = dxf_new(ezdxf, 4)
    msp = doc.modelspace()
    dxf_rect(msp, 0, 0, 100, 50)
    xline = msp.add_xline((50, 25), (1, 0, 0))
    xline.dxf.unit_vector = (999, 0, 0)
    msp.add_ray((0, 0), (1, 1, 0))
    blob = dxf_ezdxf_bytes(doc)
    if b" 11\n999.0\n" not in blob:
        raise SystemExit("xline_ray.dxf: ezdxf normalised the XLINE direction")
    return blob


def build_dxf_text_alpha(ezdxf):
    doc = dxf_new(ezdxf, 4)
    doc.layers.add("HALF", transparency=0.5)
    msp = doc.modelspace()
    for y, label, alpha in ((100, "OPAQUE TEXT", None), (90, "HALF TEXT", 0.5), (80, "CLEAR TEXT", 1.0)):
        text = msp.add_text(label, height=5).set_placement((0, y))
        if alpha is not None:
            text.transparency = alpha
    for y, label, alpha in ((75, "OPAQUE MTEXT", None), (65, "HALF MTEXT", 0.5), (55, "CLEAR MTEXT", 1.0)):
        mtext = msp.add_mtext(label, dxfattribs={"char_height": 5}).set_location((0, y))
        if alpha is not None:
            mtext.transparency = alpha
    for y, label, alpha in ((100, "HALF\\PSPLIT", 0.5), (85, "\\C1;INLINE RED", 0.5), (70, "CLEAR\\PSPLIT", 1.0)):
        msp.add_mtext(label, dxfattribs={"char_height": 5}).set_location((70, y)).transparency = alpha
    msp.add_text("LAYER TEXT", height=5, dxfattribs={"layer": "HALF"}).set_placement((0, 40))
    block = doc.blocks.new("BYBLK")
    block.add_text("BLOCK TEXT", height=5, dxfattribs={"transparency": 0x01000000}).set_placement((0, 0))
    msp.add_blockref("BYBLK", (0, 30)).transparency = 0.5
    msp.add_line((0, 20), (80, 20)).transparency = 0.5
    hatch = msp.add_hatch(color=1)
    hatch.paths.add_polyline_path([(90, 10), (110, 10), (110, 30), (90, 30)], is_closed=True)
    hatch.transparency = 0.5
    msp.add_xline((0, 15), (1, 0, 0)).transparency = 0.5
    dxf_rect(msp, -10, 0, 120, 110)
    return dxf_ezdxf_bytes(doc)


def build_dxf_text_alpha_faint(ezdxf):
    doc = dxf_new(ezdxf, 4)
    msp = doc.modelspace()
    msp.add_text("FAINT ONLY", height=10).set_placement((0, 0)).transparency = 0.5
    msp.add_text("GONE", height=10).set_placement((0, -15)).transparency = 1.0
    return dxf_ezdxf_bytes(doc)


def build_dxf_alpha(ezdxf, texts, visible):
    doc = dxf_new(ezdxf, 4)
    doc.layers.add("CLEARLAYER", transparency=1.0)
    msp = doc.modelspace()
    clear = {"transparency": 0x02000000}
    msp.add_line((0, 0), (100, 0), dxfattribs=clear)
    msp.add_circle((20, 30), 10, dxfattribs=clear)
    msp.add_arc((50, 30), 10, 0, 180, dxfattribs=clear)
    msp.add_lwpolyline([(70, 20), (90, 20), (90, 40)], dxfattribs=clear)
    hatch = msp.add_hatch(color=1, dxfattribs=clear)
    hatch.paths.add_polyline_path([(0, 50), (20, 50), (20, 70), (0, 70)], is_closed=True)
    msp.add_solid([(30, 50), (50, 50), (30, 70), (50, 70)], dxfattribs=clear)
    msp.add_point((60, 60), dxfattribs=clear)
    msp.add_xline((0, 80), (1, 0, 0), dxfattribs=clear)
    msp.add_line((0, 90), (100, 90), dxfattribs={"layer": "CLEARLAYER"})
    block = doc.blocks.new("BYBLK")
    block.add_line((0, 0), (100, 0), dxfattribs={"transparency": 0x01000000})
    msp.add_blockref("BYBLK", (0, 100), dxfattribs=clear)
    if texts:
        msp.add_text("CLEAR TEXT", height=5, dxfattribs=clear).set_placement((0, 110))
        msp.add_mtext("CLEAR MTEXT", dxfattribs={"char_height": 5, "transparency": 0x02000000}).set_location((50, 120))
    if visible == "line":
        msp.add_line((0, -10), (100, -10)).transparency = 0.5
    if visible == "fill":
        hatch = msp.add_hatch(color=5, dxfattribs={"transparency": DXF_HALF_TRANSPARENCY})
        hatch.paths.add_polyline_path([(0, -20), (100, -20), (100, -10), (0, -10)], is_closed=True)
    return dxf_ezdxf_bytes(doc)


DXF_FONT_STYLES = (
    ("MONO", "DejaVuSansMono.ttf", "MONO BOOK"),
    ("MONOB", "DejaVuSansMono-Bold.ttf", "MONO BOLD"),
    ("MONOO", "DejaVuSansMono-Oblique.ttf", "MONO OBLIQUE"),
    ("MONOBO", "DejaVuSansMono-BoldOblique.ttf", "MONO BOLD OBLIQUE"),
    ("SANS", "DejaVuSans.ttf", "SANS BOOK"),
    ("SANSB", "DejaVuSans-Bold.ttf", "SANS BOLD"),
    ("SERIF", "DejaVuSerif.ttf", "SERIF BOOK"),
    ("SERIFB", "DejaVuSerif-Bold.ttf", "SERIF BOLD"),
)


def build_dxf_text_fonts(ezdxf):
    doc = dxf_new(ezdxf, 4)
    msp = doc.modelspace()
    y = 0
    for style, font, label in DXF_FONT_STYLES:
        doc.styles.new(style, dxfattribs={"font": font})
        msp.add_text(label, height=5, dxfattribs={"style": style}).set_placement((0, y))
        y -= 10
    for style, bold, label in (("XMI", False, "XDATA MONO ITALIC"), ("XMBI", True, "XDATA MONO BOLD ITALIC")):
        doc.styles.new(style, dxfattribs={"font": ""}).set_extended_font_data("DejaVu Sans Mono", italic=True, bold=bold)
        msp.add_text(label, height=5, dxfattribs={"style": style}).set_placement((0, y))
        y -= 10
    msp.add_mtext("\\fDejaVu Sans Mono|b1|i1;INLINE MONO BOLD ITALIC", dxfattribs={"char_height": 5}).set_location((0, y + 5))
    y -= 10
    doc.styles.new("SHEAR", dxfattribs={"font": "DejaVuSansMono.ttf", "oblique": 15.0, "width": 0.7})
    msp.add_text("SHEARED STYLE", height=5, dxfattribs={"style": "SHEAR"}).set_placement((0, y))
    y -= 10
    msp.add_text("SHEARED TEXT", height=5, dxfattribs={"style": "MONO", "oblique": 15.0, "width": 0.7}).set_placement((0, y))
    dxf_rect(msp, -10, y - 10, 150, 15)
    return dxf_ezdxf_bytes(doc)


def build_dxf_units(ezdxf, code):
    doc = dxf_new(ezdxf, code)
    doc.modelspace().add_line((0, 0), (10, 5))
    return dxf_ezdxf_bytes(doc)


def dxf_block(name, body):
    return ([(0, "BLOCK"), (8, "0"), (2, name), (70, "0"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (3, name)]
            + body + [(0, "ENDBLK"), (8, "0")])


def dxf_insert(name, x=0.0, y=0.0, extra=None):
    return [(0, "INSERT"), (8, "0"), (2, name), (10, "%.1f" % x), (20, "%.1f" % y), (30, "0.0")] + (extra or [])


def dxf_line_pairs(x0, y0, x1, y1, x0_literal=None):
    return [(0, "LINE"), (8, "0"),
            (10, x0_literal if x0_literal is not None else "%.1f" % x0), (20, "%.1f" % y0), (30, "0.0"),
            (11, "%.1f" % x1), (21, "%.1f" % y1), (31, "0.0")]


def dxf_cell_body():
    return ([(0, "CIRCLE"), (8, "0"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (40, "3.0")]
            + dxf_line_pairs(-4, 0, 4, 0) + dxf_line_pairs(0, -4, 0, 4)
            + dxf_line_pairs(-5, -5, 5, -5) + dxf_line_pairs(-5, 5, 5, 5))


def dxf_document(blocks, entities):
    pairs = dxf_header("ANSI_1252")
    if blocks:
        pairs += [(0, "SECTION"), (2, "BLOCKS")] + blocks + [(0, "ENDSEC")]
    pairs += [(0, "SECTION"), (2, "ENTITIES")] + entities + [(0, "ENDSEC"), (0, "EOF")]
    return dxf_pairs(pairs)


def build_dxf_cyclic(kind):
    line = dxf_line_pairs(0, 0, 10, 0)
    if kind == "self":
        return dxf_document(dxf_block("A", line + dxf_insert("A", 5, 5)), dxf_insert("A") + dxf_line_pairs(0, -5, 10, -5))
    if kind == "ab":
        blocks = dxf_block("A", line + dxf_insert("B", 5, 5)) + dxf_block("B", line + dxf_insert("A", 5, 5))
        return dxf_document(blocks, dxf_insert("A"))
    if kind == "unreferenced":
        blocks = dxf_block("A", line + dxf_insert("B", 5, 5)) + dxf_block("B", line + dxf_insert("A", 5, 5))
        return dxf_document(blocks, dxf_line_pairs(0, -5, 10, -5))
    if kind == "case":
        return dxf_document(dxf_block("A", line + dxf_insert("a", 5, 5)), dxf_insert("A"))
    raise ValueError(kind)


def build_dxf_nest(depth):
    blocks = []
    for level in range(1, depth + 1):
        name = "N%d" % level
        body = dxf_line_pairs(0, 0, 10, 0) if level == depth else dxf_insert("N%d" % (level + 1), 1, 1)
        blocks += dxf_block(name, body)
    return dxf_document(blocks, dxf_insert("N1"))


def build_dxf_minsert_over():
    cells = DXF_INSERT_CAP + 1
    cols = 16
    rows = cells // cols
    if cols * rows != cells:
        raise SystemExit("minsert_over_inserts.dxf: %d is not %d columns of whole rows" % (cells, cols))
    extra = [(70, str(cols)), (71, str(rows)), (44, "12.0"), (45, "12.0")]
    return dxf_document(dxf_block("CELL", dxf_cell_body()), dxf_insert("CELL", 0, 0, extra))


def build_dxf_entities_over():
    body = []
    for i in range(1000):
        body += dxf_line_pairs(i * 0.01, 0, i * 0.01, 1)
    entities = []
    for i in range(851):
        entities += dxf_insert("BIG", (i % 29) * 20.0, (i // 29) * 5.0)
    return dxf_document(dxf_block("BIG", body), entities)


def build_dxf_dimension_minsert():
    extra = [(70, "1000"), (71, "1000"), (44, "12.0"), (45, "12.0")]
    blocks = dxf_block("CELL", dxf_cell_body())
    blocks += [(0, "BLOCK"), (8, "0"), (2, "*D1"), (70, "1"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (3, "*D1")]
    blocks += dxf_insert("CELL", 0, 0, extra) + [(0, "ENDBLK"), (8, "0")]
    dim = [(0, "DIMENSION"), (8, "0"), (2, "*D1"),
           (10, "10.0"), (20, "-5.0"), (30, "0.0"),
           (11, "5.0"), (21, "-5.0"), (31, "0.0"),
           (70, "0"),
           (13, "0.0"), (23, "0.0"), (33, "0.0"),
           (14, "10.0"), (24, "0.0"), (34, "0.0")]
    return dxf_document(blocks, dim)


def build_dxf_nonnumeric(literal):
    return dxf_document([], dxf_line_pairs(0, 0, 10, 5, x0_literal=literal) + dxf_line_pairs(0, 10, 10, 15))


def dxf_section_of_pairs(lines):
    section = None
    out = []
    for i in range(0, len(lines) - 1, 2):
        code, value = lines[i].strip(), lines[i + 1].strip()
        if code == b"2" and i >= 2 and lines[i - 2].strip() == b"0" and lines[i - 1].strip() == b"SECTION":
            section = value.decode("ascii", "replace")
        out.append((i, section, code, value))
        if code == b"0" and value == b"ENDSEC":
            section = None
    return out


def dxf_is_real(value):
    return re.fullmatch(rb"[+-]?(\d+\.?\d*|\.\d+)([eE][+-]?\d+)?", value) is not None


def build_dxf_nonfinite(ezdxf, section, literal):
    doc = dxf_new(ezdxf, 4)
    doc.modelspace().add_line((0, 0), (10, 5))
    blob = dxf_ezdxf_bytes(doc)
    lines = blob.split(b"\n")
    for i, sec, code, value in dxf_section_of_pairs(lines):
        if sec == section and code.isdigit() and 40 <= int(code) <= 49 and dxf_is_real(value):
            lines[i + 1] = literal.encode("ascii")
            return b"\n".join(lines)
    raise SystemExit("nonfinite: no real-valued tag with a code in 40..49 in the %s section" % section)


def build_dxf_all():
    if os.environ.get("PYTHONHASHSEED") != "0":
        raise SystemExit("--dxf-fixtures: run with PYTHONHASHSEED=0 (python -B, not -I) for a byte-identical output")
    import ezdxf
    if ezdxf.__version__ != "1.4.4":
        raise SystemExit("--dxf-fixtures: ezdxf 1.4.4 is required, found %s" % ezdxf.__version__)
    ezdxf.options.write_fixed_meta_data_for_testing = True
    out = {
        "text_cases.dxf": build_dxf_text_cases(ezdxf),
        "text_illegal_chars.dxf": build_dxf_text_illegal(ezdxf),
        "layers_off_frozen.dxf": build_dxf_layers(ezdxf, True),
        "all_hidden.dxf": build_dxf_layers(ezdxf, False),
        "never_resolved.dxf": build_dxf_never_resolved(ezdxf),
        "wrong_extents.dxf": build_dxf_extents(ezdxf, ("-1000000000.0", "-1000000000.0", "0.0"), ("1000000000.0", "1000000000.0", "0.0")),
        "header_far_extents.dxf": build_dxf_extents(ezdxf, ("-1e+20", "-1e+20", "-1e+20"), ("1e+20", "1e+20", "1e+20")),
        "wide.dxf": build_dxf_box(ezdxf, 1000, 10, "WIDE"),
        "tall.dxf": build_dxf_box(ezdxf, 10, 1000, "TALL"),
        "far_point.dxf": build_dxf_far_point(ezdxf),
        "single_point.dxf": build_dxf_single_point(ezdxf),
        "xline_ray.dxf": build_dxf_xline_ray(ezdxf),
        "text_alpha.dxf": build_dxf_text_alpha(ezdxf),
        "text_alpha_faint.dxf": build_dxf_text_alpha_faint(ezdxf),
        "text_fonts.dxf": build_dxf_text_fonts(ezdxf),
        "alpha_clear_geometry.dxf": build_dxf_alpha(ezdxf, False, None),
        "alpha_clear_all.dxf": build_dxf_alpha(ezdxf, True, None),
        "alpha_one_visible.dxf": build_dxf_alpha(ezdxf, True, "line"),
        "alpha_one_fill.dxf": build_dxf_alpha(ezdxf, True, "fill"),
        "units_missing.dxf": build_dxf_units(ezdxf, None),
        "cyclic_self.dxf": build_dxf_cyclic("self"),
        "cyclic_ab.dxf": build_dxf_cyclic("ab"),
        "cyclic_unreferenced.dxf": build_dxf_cyclic("unreferenced"),
        "cyclic_case.dxf": build_dxf_cyclic("case"),
        "nest_d16.dxf": build_dxf_nest(16),
        "nest_d17.dxf": build_dxf_nest(17),
        "minsert_over_inserts.dxf": build_dxf_minsert_over(),
        "entities_over.dxf": build_dxf_entities_over(),
        "dimension_minsert.dxf": build_dxf_dimension_minsert(),
    }
    for c in DXF_UNIT_CODES:
        out["units_%d.dxf" % c] = build_dxf_units(ezdxf, c)
    for name, literal in DXF_NONNUMERIC.items():
        out[name + ".dxf"] = build_dxf_nonnumeric(literal)
    for name, (section, literal) in DXF_NONFINITE.items():
        out[name + ".dxf"] = build_dxf_nonfinite(ezdxf, section, literal)
    if sorted(out) != sorted(DXF_FIXTURES):
        raise SystemExit("--dxf-fixtures: the builder set differs from DXF_FIXTURES")
    return out


def check_dxf_nonfinite(blob, section, literal):
    problems = []
    hits = [(sec, code) for _, sec, code, value in dxf_section_of_pairs(blob.split(b"\n"))
            if value == literal.encode("ascii") and code.isdigit() and 40 <= int(code) <= 49]
    if [sec for sec, _ in hits] != [section]:
        problems.append("literal %s found in sections %s, expected exactly one in %s" % (literal, [s for s, _ in hits], section))
    for _, sec, code, value in dxf_section_of_pairs(blob.split(b"\n")):
        if sec in ("ENTITIES", "BLOCKS") and code.isdigit() and 10 <= int(code) <= 59:
            if not dxf_is_real(value) or not math.isfinite(float(value)):
                problems.append("non-finite real %r in %s" % (value, sec))
    return problems


def dxf_fixture_failures(out_dir):
    failures = []
    expected = dxf_expected()
    present = sorted(n for n in os.listdir(out_dir) if n.endswith(".dxf")) if os.path.isdir(out_dir) else []
    extra = sorted(set(present) - set(DXF_FIXTURES))
    if extra:
        failures.append("unexpected files: %s" % ", ".join(extra))
    for name in DXF_FIXTURES:
        path = os.path.join(out_dir, name)
        if not os.path.exists(path):
            failures.append("%s: missing" % name)
            continue
        with open(path, "rb") as fh:
            blob = fh.read()
        for marker in expected[name]:
            if marker not in blob:
                failures.append("%s: marker %r absent from raw bytes" % (name, marker))
        for marker in DXF_ABSENT.get(name, []):
            if marker in blob:
                failures.append("%s: marker %r must be absent" % (name, marker))
        stem = name[:-4]
        if stem in DXF_NONFINITE:
            failures += ["%s: %s" % (name, p) for p in check_dxf_nonfinite(blob, *DXF_NONFINITE[stem])]
    return failures


def check_dxf_fixtures(out_dir):
    failures = dxf_fixture_failures(out_dir)
    if failures:
        print("SELF-CHECK FAILED:", file=sys.stderr)
        for f in failures:
            print("  " + f, file=sys.stderr)
        return 1
    for name in DXF_FIXTURES:
        print("  %s: %d bytes" % (name, os.path.getsize(os.path.join(out_dir, name))))
    print("self-check passed: %d dxf fixtures carry the markers they exist to carry" % len(DXF_FIXTURES))
    return 0


def run_dxf_fixtures(args):
    out_dir = args.out_dir or DEFAULT_DXF_OUT
    if not args.check_only:
        try:
            os.makedirs(out_dir, exist_ok=True)
        except FileExistsError:
            pass
        for name, blob in build_dxf_all().items():
            with open(os.path.join(out_dir, name), "wb") as fh:
                fh.write(blob)
        print("generated dxf fixtures into %s" % out_dir)
    return check_dxf_fixtures(out_dir)


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("--out-dir", default=None)
    ap.add_argument("--reject", action="store_true",
                    help="generate or check the rejection fixtures (standard library only)")
    ap.add_argument("--provenance", action="store_true",
                    help="with --reject: print the PROVENANCE.md block computed from the files on disk")
    ap.add_argument("--check-only", action="store_true",
                    help="verify existing fixtures without rewriting them")
    ap.add_argument("--scaled-from", help="real base file to scale (not committed output)")
    ap.add_argument("--kind", help="step-copies|step-nauo|stl-tile|stl-ascii|dxf-replicate|dxf-minsert|dxf-combo|dxf-far-point|dxf-guard-stress")
    ap.add_argument("--shape", help="dxf-guard-stress shape: chain|fanout|blocks|wide")
    ap.add_argument("--targets", help="comma separated MiB sizes, or instance counts for step-nauo, or expanded cells (columns x rows) for dxf-minsert")
    ap.add_argument("--scaled-dir", help="output directory for scaled files")
    ap.add_argument("--block-entities", type=int, help="dxf-minsert: entities in the CELL block (default 5)")
    ap.add_argument("--cells", type=int, help="dxf-combo: cells of the MINSERT appended to the base")
    ap.add_argument("--dxf-fixtures", action="store_true",
                    help="generate or check the phase 75 DXF fixtures (generation needs ezdxf and PYTHONHASHSEED=0)")
    args = ap.parse_args()

    if args.dxf_fixtures:
        return run_dxf_fixtures(args)

    if args.reject:
        return run_reject(args)

    if args.kind == "dxf-guard-stress":
        return run_guard_stress(args)

    if args.scaled_from:
        return run_scaled(args)

    args.out_dir = args.out_dir or DEFAULT_OUT

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

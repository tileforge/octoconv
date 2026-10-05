#!/usr/bin/env python3
import argparse
import hashlib
import json
import math
import mmap
import os
import re
import sys
import time

CHUNK = 1 << 24
NAUO = b"NEXT_ASSEMBLY_USAGE_OCCURRENCE"
MAPPED = b"MAPPED_ITEM"
STEP_ENTITY = re.compile(rb"(?:^|(?<=;))[ \t]*#\d+[ \t]*=", re.M)
FINITE_LIMIT = 1e12
QUALITY = (0.001, 0.35)
REPO_ROOT = os.path.abspath(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
CORPUS = os.path.join(REPO_ROOT, "internal", "convert", "testdata", "cad")
CEILING_NAUO = 1518
TIMEOUT_FLOOR_S = 316
GUARD_FLOOR_S = 30
IO_ALLOWANCE_S = 60
POD_GRACE_PAD_S = 20
MIB = 1048576
PEAK_LIMIT_MIB = 2867
IGES_71_CEILING = 155 * MIB
STEP_71_CEILING = 63 * MIB
RULE_KEYS = ("load_check", "assembly_cycle", "expansion", "mapped_item", "entity_counter")
DERIVED_KEYS = ("cad_guard_timeout_s", "cad_mesh_timeout_s", "cad_io_allowance_s", "cad_engine_timeout_s", "cad_pod_grace_s")


def sanitize(value):
    if value is None:
        return "null"
    if isinstance(value, float):
        value = "%.6g" % value
    text = re.sub(r"[^A-Za-z0-9._+:-]+", "_", str(value)).strip("_")
    return text or "empty"


def count_semicolons(path):
    total = 0
    started = False
    tail = b""
    with open(path, "rb") as fh:
        while True:
            block = fh.read(CHUNK)
            if not block:
                return total if started else None
            if not started:
                joined = tail + block
                at = joined.find(b"DATA;")
                if at < 0:
                    tail = joined[-8:]
                    continue
                started = True
                block = joined[at:]
            total += block.count(b";")


def count_token(mm, token):
    n = 0
    pos = mm.find(token)
    while pos != -1:
        n += 1
        pos = mm.find(token, pos + len(token))
    return n


def line_regex_count(mm):
    at = mm.find(b"DATA;")
    if at < 0:
        return None
    return sum(1 for _ in STEP_ENTITY.finditer(mm, at + len(b"DATA;")))


def cmd_count(args):
    t0 = time.monotonic()
    size = os.path.getsize(args.input)
    out = {"fmt": args.format, "bytes": size}
    if size == 0:
        out["error"] = "empty_file"
    elif args.format == "step":
        semis = count_semicolons(args.input)
        with open(args.input, "rb") as fh, mmap.mmap(fh.fileno(), 0, access=mmap.ACCESS_READ) as mm:
            out["nauo"] = count_token(mm, NAUO)
            out["mapped_item"] = count_token(mm, MAPPED)
            if args.with_regex:
                out["line_regex"] = line_regex_count(mm)
        out["semicolons"] = semis
    else:
        records = 0
        newlines = 0
        with open(args.input, "rb") as fh:
            while True:
                block = fh.read(CHUNK)
                if not block:
                    break
                newlines += block.count(b"\n")
        records = newlines if newlines else size // 80
        out["records"] = records
    out["elapsed_s"] = round(time.monotonic() - t0, 3)
    print(json.dumps(out, sort_keys=True), flush=True)
    return 0


def read_text(path):
    with open(path, "rb") as fh:
        return fh.read()


def first_vertex_point(blob):
    vertex = re.search(rb"#(\d+)\s*=\s*VERTEX_POINT\s*\(\s*'[^']*'\s*,\s*#(\d+)\s*\)", blob)
    if vertex is None:
        raise SystemExit("no VERTEX_POINT in the source")
    return vertex.group(2).decode("ascii")


def replace_coordinate(blob, point_id, index, text):
    pattern = re.compile(rb"(#" + point_id.encode("ascii") + rb"\s*=\s*CARTESIAN_POINT\s*\(\s*'[^']*'\s*,\s*\()([^)]*)(\)\s*\))")
    m = pattern.search(blob)
    if m is None:
        raise SystemExit("CARTESIAN_POINT #%s not found" % point_id)
    coords = m.group(2).split(b",")
    coords[index] = text.encode("ascii")
    return blob[:m.start()] + m.group(1) + b",".join(coords) + m.group(3) + blob[m.end():]


def data_bounds(blob):
    start = blob.find(b"DATA;") + len(b"DATA;")
    end = blob.rfind(b"ENDSEC;")
    if start < len(b"DATA;") or end < start:
        raise SystemExit("no DATA section")
    return start, end


def max_entity_id(blob):
    return max(int(m.group(1)) for m in re.finditer(rb"#(\d+)\s*=", blob))


def write_out(outdir, name, blob):
    path = os.path.join(outdir, name)
    with open(path, "wb") as fh:
        fh.write(blob)
    return path


def cmd_gen_bombs(args):
    os.makedirs(args.outdir, exist_ok=True)
    units = read_text(os.path.join(CORPUS, "synthetic", "units_inch.step"))
    nauo4 = read_text(os.path.join(CORPUS, "synthetic", "assembly_nauo4.step"))
    point = first_vertex_point(units)
    written = []
    for tag, text in (("abc", "abc"), ("nan", "NaN"), ("1e999", "1E999"), ("dotdot", "1..2"), ("dblminus", "--1"), ("1e300", "1E300")):
        written.append(write_out(args.outdir, "s1_coord_%s.step" % tag, replace_coordinate(units, point, 1, text)))
    start, end = data_bounds(units)
    head, data, tail = units[:start], units[start:end], units[end:]
    commented = re.sub(rb"(?m)^(#\d+\s*=)", rb"/* x */ \1", data)
    written.append(write_out(args.outdir, "s4_comments.step", head + commented + tail))
    oneline = data.replace(b"\r\n", b" ").replace(b"\n", b" ")
    written.append(write_out(args.outdir, "s4_oneline.step", head + oneline + tail))
    cr_only = units.replace(b"\r\n", b"\n").replace(b"\n", b"\r")
    written.append(write_out(args.outdir, "s4_cr.step", cr_only))
    nauo_ids = re.findall(rb"#(\d+)\s*=\s*NEXT_ASSEMBLY_USAGE_OCCURRENCE\(([^)]*)\)", nauo4)
    if len(nauo_ids) < 2:
        raise SystemExit("assembly_nauo4.step has fewer than two NAUO statements")
    relating = re.search(rb"NEXT_ASSEMBLY_USAGE_OCCURRENCE\('[^']*','[^']*','[^']*',(#\d+),(#\d+),\$\)", nauo4)
    rel, rld = relating.group(1), relating.group(2)
    first = re.compile(rb"(#" + nauo_ids[0][0] + rb"\s*=\s*NEXT_ASSEMBLY_USAGE_OCCURRENCE\('[^']*','[^']*','[^']*',)(#\d+),(#\d+)(,\$\))")
    second = re.compile(rb"(#" + nauo_ids[1][0] + rb"\s*=\s*NEXT_ASSEMBLY_USAGE_OCCURRENCE\('[^']*','[^']*','[^']*',)(#\d+),(#\d+)(,\$\))")
    self_ref = first.sub(lambda m: m.group(1) + rel + b"," + rel + m.group(4), nauo4, count=1)
    written.append(write_out(args.outdir, "s2_self_ref.step", self_ref))
    cycle = second.sub(lambda m: m.group(1) + rld + b"," + rel + m.group(4), nauo4, count=1)
    written.append(write_out(args.outdir, "s2_cycle2.step", cycle))
    if args.flood_entities:
        top = max_entity_id(units)
        extra = b"".join(b"#%d = DIRECTION('',(0.,0.,1.));\n" % (top + 1 + i) for i in range(args.flood_entities))
        end_at = units.rfind(b"ENDSEC;")
        written.append(write_out(args.outdir, "flood_entities_%d.step" % args.flood_entities, units[:end_at] + extra + units[end_at:]))
    if args.flood_nauo:
        top = max_entity_id(units)
        extra = b"".join(b"#%d = NEXT_ASSEMBLY_USAGE_OCCURRENCE('','','',#5,#5,$);\n" % (top + 1 + i) for i in range(args.flood_nauo))
        end_at = units.rfind(b"ENDSEC;")
        written.append(write_out(args.outdir, "flood_nauo_%d.step" % args.flood_nauo, units[:end_at] + extra + units[end_at:]))
    for path in written:
        blob = read_text(path)
        print("BOMB,name=%s,bytes=%d,sha256=%s" % (os.path.basename(path), len(blob), hashlib.sha256(blob).hexdigest()), flush=True)
    return 0


def token_counts(path):
    blob = read_text(path)
    return {
        "bytes": len(blob),
        "nauo": blob.count(NAUO),
        "mapped_item": blob.count(MAPPED),
        "semicolons": count_semicolons(path),
    }


def cmd_gen_mapped_text(args):
    units = read_text(os.path.join(CORPUS, "synthetic", "units_inch.step"))
    top = max_entity_id(units)
    nxt = top + 1
    prev_rep, prev_origin = 10, 11
    extra = []
    for level in range(1, args.levels + 1):
        rm, p_o, p_b, org, t_a, t_b, mi_a, mi_b, rep = range(nxt, nxt + 9)
        nxt += 9
        shift = 100.0 * (2 ** level)
        extra += [
            "#%d = REPRESENTATION_MAP(#%d,#%d);" % (rm, prev_origin, prev_rep),
            "#%d = CARTESIAN_POINT('',(0.,0.,0.));" % p_o,
            "#%d = CARTESIAN_POINT('',(%r,0.,0.));" % (p_b, shift),
            "#%d = AXIS2_PLACEMENT_3D('',#%d,#13,#14);" % (org, p_o),
            "#%d = AXIS2_PLACEMENT_3D('',#%d,#13,#14);" % (t_a, p_o),
            "#%d = AXIS2_PLACEMENT_3D('',#%d,#13,#14);" % (t_b, p_b),
            "#%d = MAPPED_ITEM('',#%d,#%d);" % (mi_a, rm, t_a),
            "#%d = MAPPED_ITEM('',#%d,#%d);" % (mi_b, rm, t_b),
            "#%d = SHAPE_REPRESENTATION('',(#%d,#%d,#%d),#345);" % (rep, org, mi_a, mi_b),
        ]
        prev_rep, prev_origin = rep, org
    swapped = units.replace(b"SHAPE_DEFINITION_REPRESENTATION(#4,#10)", b"SHAPE_DEFINITION_REPRESENTATION(#4,#%d)" % prev_rep, 1)
    if swapped == units:
        raise SystemExit("SHAPE_DEFINITION_REPRESENTATION(#4,#10) not found")
    end_at = swapped.rfind(b"ENDSEC;")
    blob = swapped[:end_at] + ("\n".join(extra) + "\n").encode("ascii") + swapped[end_at:]
    with open(args.out, "wb") as fh:
        fh.write(blob)
    counts = token_counts(args.out)
    counts.update({"kind": "mapped_text", "levels": args.levels, "instances": 2 ** args.levels})
    print(json.dumps(counts, sort_keys=True), flush=True)
    return 0


def cmd_gen_hierarchy(args):
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.STEPControl import STEPControl_Reader

    reader = STEPControl_Reader()
    if reader.ReadFile(args.leaf) != IFSelect_RetDone:
        raise SystemExit("cannot read the leaf %s" % args.leaf)
    reader.TransferRoots()
    leaf = reader.OneShape()
    if args.kind == "nauo":
        build_nauo(leaf, args.levels, args.out)
    else:
        build_mapped(leaf, args.levels, args.out)
    counts = token_counts(args.out)
    counts.update({"kind": args.kind, "levels": args.levels, "instances": 2 ** args.levels})
    print(json.dumps(counts, sort_keys=True), flush=True)
    return 0


def shifted(level):
    from OCP.TopLoc import TopLoc_Location
    from OCP.gp import gp_Trsf, gp_Vec

    trsf = gp_Trsf()
    trsf.SetTranslation(gp_Vec(200.0 * (2 ** level), 0.0, 0.0))
    return TopLoc_Location(trsf)


def build_nauo(leaf, levels, out):
    from OCP.STEPCAFControl import STEPCAFControl_Writer
    from OCP.STEPControl import STEPControl_AsIs
    from OCP.TCollection import TCollection_ExtendedString
    from OCP.TDocStd import TDocStd_Document
    from OCP.TopLoc import TopLoc_Location
    from OCP.XCAFApp import XCAFApp_Application
    from OCP.XCAFDoc import XCAFDoc_DocumentTool

    app = XCAFApp_Application.GetApplication_s()
    doc = TDocStd_Document(TCollection_ExtendedString("XmlOcaf"))
    app.InitDocument(doc)
    tool = XCAFDoc_DocumentTool.ShapeTool_s(doc.Main())
    below = tool.AddShape(leaf, False)
    for level in range(1, levels + 1):
        asm = tool.NewShape()
        tool.AddComponent(asm, below, TopLoc_Location())
        tool.AddComponent(asm, below, shifted(level))
        below = asm
    tool.UpdateAssemblies()
    writer = STEPCAFControl_Writer()
    writer.Transfer(doc, STEPControl_AsIs)
    if not writer.Write(out):
        raise SystemExit("writer failed")


def build_mapped(leaf, levels, out):
    from OCP.BRep import BRep_Builder
    from OCP.STEPControl import STEPControl_AsIs, STEPControl_Writer
    from OCP.TopLoc import TopLoc_Location
    from OCP.TopoDS import TopoDS_Compound

    below = leaf
    for level in range(1, levels + 1):
        builder = BRep_Builder()
        comp = TopoDS_Compound()
        builder.MakeCompound(comp)
        builder.Add(comp, below.Moved(TopLoc_Location()))
        builder.Add(comp, below.Moved(shifted(level)))
        below = comp
    writer = STEPControl_Writer()
    writer.Transfer(below, STEPControl_AsIs)
    if not writer.Write(out):
        raise SystemExit("writer failed")


def load_corpus_gen():
    import importlib.util

    spec = importlib.util.spec_from_file_location("cad_corpus_gen", os.path.join(REPO_ROOT, "scripts", "cad-corpus-gen.py"))
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def cmd_copies_target(args):
    mod = load_corpus_gen()
    base = read_text(args.base)
    head, data, tail = mod.step_sections(base)
    max_id = mod.step_max_id(data)
    copies = {}

    def copy_of(k):
        if k not in copies:
            copies[k] = mod.step_renumber(data, k * max_id)
        return copies[k]

    best = None
    for target in range(2, 4096):
        want = target * 1048576
        k = 0
        total = len(head) + len(tail)
        while total + len(copy_of(k)) // 2 < want:
            total += len(copy_of(k))
            k += 1
        if k < 2:
            continue
        size = len(head) + len(tail) + sum(len(copy_of(i)) for i in range(k))
        if size > args.limit_bytes:
            break
        best = (target, size)
    if best is None:
        raise SystemExit("no target fits")
    print("COPIES_TARGET,target=%d,expected_bytes=%d" % best)
    return 0


def box_diag(box):
    lo, hi = box.CornerMin(), box.CornerMax()
    return ((hi.X() - lo.X()) ** 2 + (hi.Y() - lo.Y()) ** 2 + (hi.Z() - lo.Z()) ** 2) ** 0.5


def load_check(reader):
    ws = reader.WS()
    checks = ws.ModelCheckList(False)
    fails = 0
    warns = 0
    numeric = 0
    checks.Start()
    while checks.More():
        check = checks.Value()
        fails += check.NbFails()
        warns += check.NbWarnings()
        for i in range(1, check.NbFails() + 1):
            text = check.Fail(i).ToCString()
            if "not a Real" in text or "not an Integer" in text:
                numeric += 1
        checks.Next()
    global_fails = reader.Model().GlobalCheck().NbFails()
    return fails, warns, numeric, global_fails


def cmd_read(args):
    result = {"phase": "start", "fmt": args.format}
    progress = os.path.join(args.outdir, "read.json")

    def save(phase):
        result["phase"] = phase
        tmp = progress + ".tmp"
        with open(tmp, "w") as fh:
            json.dump(result, fh, sort_keys=True)
        os.replace(tmp, progress)

    def finish(rc):
        save(result["phase"])
        print(json.dumps(result, sort_keys=True), flush=True)
        return rc

    t = time.monotonic()
    from OCP.BRepBndLib import BRepBndLib
    from OCP.BRepMesh import BRepMesh_IncrementalMesh
    from OCP.Bnd import Bnd_Box
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.IGESControl import IGESControl_Reader
    from OCP.Interface import Interface_Static
    from OCP.STEPControl import STEPControl_Reader
    from OCP.StlAPI import StlAPI_Writer
    from OCP.TopAbs import TopAbs_FACE
    from OCP.TopExp import TopExp_Explorer

    result["t_import"] = round(time.monotonic() - t, 3)
    reader = IGESControl_Reader() if args.format == "iges" else STEPControl_Reader()
    pinned = Interface_Static.SetCVal_s("xstep.cascade.unit", "MM")
    result["binding_unit"] = "Interface_Static.SetCVal_s('xstep.cascade.unit','MM') after reader construction -> %s, CVal_s -> %s" % (
        pinned, Interface_Static.CVal_s("xstep.cascade.unit"))
    save("read")
    t = time.monotonic()
    try:
        status = reader.ReadFile(args.input)
    except Exception as exc:
        result["error"] = exc.__class__.__name__
        return finish(2)
    result["status"] = str(status).rsplit(".", 1)[-1]
    result["t_read"] = round(time.monotonic() - t, 3)
    if status != IFSelect_RetDone:
        return finish(2)
    save("check")
    t = time.monotonic()
    try:
        fails, warns, numeric, global_fails = load_check(reader)
        result["load_fails"], result["load_warns"] = fails, warns
        result["load_fails_numeric"], result["load_fails_global"] = numeric, global_fails
        result["binding_check_list"] = "reader.WS().ModelCheckList(False) -> Interface_CheckIterator; Start/More/Value/Next, Interface_Check.NbFails/NbWarnings"
    except Exception as exc:
        result["load_fails"], result["load_warns"] = None, None
        result["binding_check_list"] = "unavailable"
        result["check_error"] = exc.__class__.__name__
    result["t_check"] = round(time.monotonic() - t, 3)
    save("transfer")
    t = time.monotonic()
    try:
        reader.TransferRoots()
        shape = reader.OneShape()
    except Exception as exc:
        result["error"] = exc.__class__.__name__
        return finish(2)
    result["t_transfer"] = round(time.monotonic() - t, 3)
    if shape.IsNull():
        result["shape"] = "null"
        return finish(2)
    result["shape"] = "ok"
    save("box")
    box = Bnd_Box()
    BRepBndLib.Add_s(shape, box)
    result["box_source"] = "shape"
    if box.IsVoid() or box_diag(box) > FINITE_LIMIT:
        box = Bnd_Box()
        result["box_source"] = "finite_faces"
        faces = TopExp_Explorer(shape, TopAbs_FACE)
        while faces.More():
            fbox = Bnd_Box()
            BRepBndLib.AddOptimal_s(faces.Current(), fbox, False, False)
            if not fbox.IsVoid() and box_diag(fbox) <= FINITE_LIMIT:
                box.Add(fbox)
            faces.Next()
    if box.IsVoid():
        result["triangles"] = 0
        result["box"] = "void"
        return finish(3)
    diag = box_diag(box)
    result["bbox_diag"] = round(diag, 6)
    if diag <= 0:
        result["triangles"] = 0
        return finish(3)
    save("mesh")
    t = time.monotonic()
    stl = os.path.join(args.outdir, "part.stl")
    try:
        BRepMesh_IncrementalMesh(shape, QUALITY[0] * diag, False, QUALITY[1], False)
        result["t_mesh"] = round(time.monotonic() - t, 3)
        save("write")
        writer = StlAPI_Writer()
        writer.ASCIIMode = False
        written = writer.Write(shape, stl)
    except Exception as exc:
        result["error"] = exc.__class__.__name__
        return finish(3)
    result["triangles"] = (os.path.getsize(stl) - 84) // 50 if written and os.path.exists(stl) else 0
    result["stl_bytes"] = os.path.getsize(stl) if os.path.exists(stl) else 0
    return finish(0 if result["triangles"] > 0 else 3)


WRAP_DROP = ("out",)


def parse_wrap(path):
    with open(path) as fh:
        lines = [ln.strip() for ln in fh if ln.startswith("WRAP,")]
    if not lines:
        return {}
    fields = {}
    for part in lines[-1].split(",")[1:]:
        key, _, value = part.partition("=")
        if key not in WRAP_DROP:
            fields[key] = value
    return fields


def last_json(path):
    if not path or not os.path.isfile(path):
        return {}
    found = {}
    with open(path, "rb") as fh:
        for raw in fh:
            line = raw.strip()
            if line.startswith(b"{"):
                try:
                    found = json.loads(line.decode("utf-8", "replace"))
                except ValueError:
                    continue
    return found


READ_FIELDS = ("phase", "status", "load_fails", "load_warns", "load_fails_numeric", "load_fails_global", "shape", "triangles", "bbox_diag", "box_source",
               "t_import", "t_read", "t_check", "t_transfer", "t_mesh", "stl_bytes", "error")
COUNT_KEYS = (("nauo", "nauo_tokens"), ("mapped_item", "mapped_tokens"), ("semicolons", "semicolons"),
              ("line_regex", "line_regex"), ("records", "records"), ("elapsed_s", "count_elapsed_s"))


def cmd_rowline(args):
    blob = read_text(args.input) if args.input and os.path.isfile(args.input) else b""
    fields = [("spike", args.spike), ("name", os.path.basename(args.input) if args.input else args.name),
              ("fmt", args.format), ("bytes", len(blob)), ("sha256", hashlib.sha256(blob).hexdigest()),
              ("image_id", args.image_id)]
    for item in args.extra:
        key, _, value = item.partition("=")
        fields.append((key, value))
    wrap = parse_wrap(args.wrap_file) if args.wrap_file else {}
    for key, value in wrap.items():
        fields.append((key, value))
    read = last_json(args.read_json)
    for key in READ_FIELDS:
        if key in read:
            fields.append((key, read[key]))
    count = last_json(args.count_json)
    for key, name in COUNT_KEYS:
        if key in count:
            fields.append((name, count[key]))
    if isinstance(count.get("semicolons"), int):
        fields.append(("entities", count["semicolons"] - 3))
    print("ROW," + ",".join("%s=%s" % (k, sanitize(v)) for k, v in fields))
    return 0


def cmd_loadcheck(args):
    read = last_json(args.read_json)
    fails, warns = read.get("load_fails"), read.get("load_warns")
    numeric, global_fails = read.get("load_fails_numeric"), read.get("load_fails_global")
    print("LOADCHECK,file=%s,status=%s,fails=%s,warns=%s,fails_numeric=%s,fails_global=%s,shape=%s,triangles=%s" % (
        sanitize(args.name), sanitize(read.get("status", "none")),
        "unknown" if fails is None else fails, "unknown" if warns is None else warns,
        "unknown" if numeric is None else numeric, "unknown" if global_fails is None else global_fails,
        sanitize(read.get("shape", "none")), sanitize(read.get("triangles", "na"))))
    return 0


def plain(text):
    return re.sub(r"\s+", "_", str(text).replace(",", ";"))


def cmd_binding(args):
    read = last_json(args.read_json)
    print("BINDING,key=unit_pin,spelling=%s" % plain(read.get("binding_unit", "unavailable")))
    spelling = read.get("binding_check_list", "unavailable")
    if spelling == "unavailable":
        print("BINDING,check_list=unavailable")
    else:
        print("BINDING,key=check_list,spelling=%s" % plain(spelling))
    return 0


def parse_line(line):
    parts = line.rstrip("\n").split(",")
    kind = parts[0]
    fields = {}
    for part in parts[1:]:
        key, _, value = part.partition("=")
        fields[key] = value
    return kind, fields


def num(value):
    try:
        return float(value)
    except (TypeError, ValueError):
        return None


def load_evidence(path):
    rows = []
    checks = []
    with open(path) as fh:
        for line in fh:
            kind, fields = parse_line(line)
            if kind == "ROW":
                rows.append(fields)
            elif kind == "LOADCHECK":
                checks.append(fields)
    return rows, checks


def row_trips_bound(row):
    nauo = num(row.get("nauo_tokens"))
    if nauo is None or nauo > CEILING_NAUO:
        return False
    if row.get("timeout") == "1":
        return True
    oom = num(row.get("oom_kill"))
    if oom is not None and oom > 0:
        return True
    wall = num(row.get("wall_s"))
    if wall is not None and wall > TIMEOUT_FLOOR_S:
        return True
    peak = num(row.get("cg_peak_mib"))
    return peak is not None and peak > 2867


def doubling(rows):
    by_kind = {}
    for row in rows:
        k = num(row.get("K"))
        tri = num(row.get("triangles"))
        if k is not None and tri is not None and tri > 0:
            by_kind.setdefault(row.get("kind"), {})[int(k)] = tri
    for series in by_kind.values():
        for k in sorted(series):
            if k + 1 in series and k + 2 in series:
                if series[k + 1] / series[k] >= 1.8 and series[k + 2] / series[k + 1] >= 1.8:
                    return True
    return False


def hierarchy_bound(rows):
    return doubling(rows) or any(row_trips_bound(row) for row in rows)


def max_wall(rows, op):
    walls = [num(r.get("wall_s")) for r in rows if r.get("spike") == "S5" and r.get("op") == op]
    walls = [w for w in walls if w is not None]
    return max(walls) if walls else None


def summarize_loadcheck(checks):
    with_fails = 0
    with_numeric = 0
    unknown = 0
    for check in checks:
        fails = num(check.get("fails"))
        numeric = num(check.get("fails_numeric"))
        if check.get("status") != "IFSelect_RetDone" or fails is None or numeric is None:
            unknown += 1
            continue
        if fails > 0:
            with_fails += 1
        if numeric > 0:
            with_numeric += 1
    return len(checks), with_fails, with_numeric, unknown


def s1_classes(rows):
    numeric = []
    accepted = []
    unexplained = []
    ended = []
    for r in rows:
        if r.get("spike") != "S1":
            continue
        name = r.get("name")
        if (num(r.get("load_fails_numeric")) or 0) > 0:
            numeric.append(name)
        elif r.get("shape") == "ok" and (num(r.get("triangles")) or 0) > 0:
            entries = (num(r.get("load_fails")) or 0) + (num(r.get("load_warns")) or 0) + (num(r.get("load_fails_global")) or 0)
            (accepted if entries == 0 else unexplained).append(name)
        else:
            ended.append(name)
    return numeric, accepted, unexplained, ended


def iges_ceiling(rows):
    points = []
    for r in rows:
        if r.get("spike") == "S5" and r.get("op") == "mesh" and r.get("fmt") == "iges":
            size, peak = num(r.get("bytes")), num(r.get("cg_peak_mib"))
            clean = r.get("rc") == "0" and r.get("timeout") == "0" and r.get("oom_kill") == "0"
            if size is not None and peak is not None:
                points.append((size, peak, clean))
    points.sort()
    passing = [p for p in points if p[2] and p[1] <= PEAK_LIMIT_MIB]
    best = max((p[0] for p in passing), default=0)
    ceiling = min(int(best // MIB) * MIB, IGES_71_CEILING)
    return ceiling, points


def step_ceiling(rows):
    points = []
    for r in rows:
        if r.get("spike") == "S5" and r.get("op") == "mesh" and r.get("fmt") == "step" and str(r.get("series", "")).startswith("step_copies"):
            size, peak = num(r.get("bytes")), num(r.get("cg_peak_mib"))
            clean = r.get("rc") == "0" and r.get("timeout") == "0" and r.get("oom_kill") == "0"
            if size is not None and peak is not None and clean:
                points.append((size, peak))
    top = max(points, default=None)
    keep = top is not None and top[1] <= PEAK_LIMIT_MIB and top[0] <= STEP_71_CEILING
    return (STEP_71_CEILING if keep else 0), top


def compute_rules(rows, checks):
    lines = []
    files, with_fails, with_numeric, unknown = summarize_loadcheck(checks)
    numeric_bombs, accepted, unexplained, ended = s1_classes(rows)
    if with_fails == 0 and unknown == 0:
        load_value = "strict"
    elif with_numeric == 0 and unknown == 0 and numeric_bombs and not unexplained:
        load_value = "numeric_strict"
    else:
        load_value = "diagnostic"
    s1_total = len(numeric_bombs) + len(accepted) + len(unexplained) + len(ended)
    lines.append("RULE,key=load_check,value=%s,inputs=files=%d;with_fails=%d;with_numeric_fails=%d;unknown=%d;s1_numeric_hits=%d;s1_total=%d;s1_reader_accepted=%d;s1_unexplained=%d" % (
        load_value, files, with_fails, with_numeric, unknown, len(numeric_bombs), s1_total, len(accepted), len(unexplained)))
    s2 = [r for r in rows if r.get("spike") == "S2"]
    handled = bool(s2) and all(
        r.get("rc") in ("0", "2") and r.get("timeout") == "0" and r.get("oom_kill") == "0" and (num(r.get("wall_s")) or 1e9) < 30
        for r in s2)
    lines.append("RULE,key=assembly_cycle,value=%s,inputs=s2_rows=%d" % ("reader_handles" if handled else "guard_rejects", len(s2)))
    nauo_rows = [r for r in rows if r.get("spike") == "S3" and r.get("kind") == "nauo" and r.get("op") == "mesh"]
    mapped_rows = [r for r in rows if r.get("spike") == "S3" and r.get("kind") in ("mapped", "mapped_text") and r.get("op") == "mesh"
                   and (num(r.get("mapped_tokens")) or 0) > 0]
    lines.append("RULE,key=expansion,value=%s,inputs=rows=%d;doubling=%d" % (
        "guard_bounds_expanded_instances" if hierarchy_bound(nauo_rows) else "edge_count_sufficient",
        len(nauo_rows), 1 if doubling(nauo_rows) else 0))
    mapped_value = "guard_bounds" if not mapped_rows or hierarchy_bound(mapped_rows) else "not_needed"
    lines.append("RULE,key=mapped_item,value=%s,inputs=rows_with_mapped_item_tokens=%d;doubling=%d" % (
        mapped_value, len(mapped_rows), 1 if doubling(mapped_rows) else 0))
    s4 = [r for r in rows if r.get("spike") == "S4"]
    evasion_ok = bool(s4)
    for r in s4:
        semis, regex = num(r.get("semicolons")), num(r.get("line_regex"))
        if semis is None or regex is None or semis - 3 < regex:
            evasion_ok = False
    lines.append("RULE,key=entity_counter,value=semicolons_minus_3,evasion_ok=%d,inputs=s4_rows=%d" % (1 if evasion_ok else 0, len(s4)))
    mesh_wall = max_wall(rows, "mesh")
    count_wall = max_wall(rows, "count")
    mesh = TIMEOUT_FLOOR_S if mesh_wall is None else max(TIMEOUT_FLOOR_S, math.ceil(1.5 * mesh_wall))
    guard = GUARD_FLOOR_S if count_wall is None or 3 * count_wall <= GUARD_FLOOR_S else math.ceil(3 * count_wall)
    engine = guard + mesh + IO_ALLOWANCE_S
    lines.append("DERIVED,key=cad_guard_timeout_s,value=%d,formula=30_if_3x_largest_S5_count_wall_le_30_else_ceil_3x,inputs=largest_S5_count_wall=%s" % (
        guard, "none" if count_wall is None else "%.2f" % count_wall))
    lines.append("DERIVED,key=cad_mesh_timeout_s,value=%d,formula=max_316_ceil_1.5x_largest_S5_mesh_wall,inputs=largest_S5_mesh_wall=%s;floor=316_is_ceil_210.63x1.5_from_71_ladder_line_917" % (
        mesh, "none" if mesh_wall is None else "%.2f" % mesh_wall))
    lines.append("DERIVED,key=cad_io_allowance_s,value=%d,formula=assumption_A7,inputs=none_measured" % IO_ALLOWANCE_S)
    lines.append("DERIVED,key=cad_engine_timeout_s,value=%d,formula=guard+mesh+io,inputs=guard=%d;mesh=%d;io=%d" % (engine, guard, mesh, IO_ALLOWANCE_S))
    lines.append("DERIVED,key=cad_pod_grace_s,value=%d,formula=engine+20,inputs=engine=%d" % (engine + POD_GRACE_PAD_S, engine))
    iges, iges_points = iges_ceiling(rows)
    pts = ";".join("%.1fMiB:%dMiB%s" % (p[0] / MIB, p[1], "" if p[2] else ":unclean") for p in iges_points)
    lines.append("DERIVED,key=cad_iges_ceiling_bytes,value=%d,formula=largest_measured_clean_iges_mesh_row_with_cgroup_peak_le_%dMiB_floored_to_MiB_capped_at_155MiB_no_upward_extrapolation,inputs=mib=%d;points_size:peak=%s" % (
        iges, PEAK_LIMIT_MIB, iges // MIB, pts or "none"))
    step, step_top = step_ceiling(rows)
    lines.append("DERIVED,key=cad_step_ceiling_bytes,value=%d,formula=71_ceiling_63MiB_kept_when_the_largest_measured_step_copies_row_at_or_below_it_has_cgroup_peak_le_%dMiB,inputs=largest_measured=%s" % (
        step, PEAK_LIMIT_MIB, "none" if step_top is None else "%.1fMiB:%dMiB" % (step_top[0] / MIB, step_top[1])))
    lines.append("GAP,key=iges_inch_scaling,value=unproven,reason=no inch IGES in the corpus (R-01)")
    if accepted:
        base = next((r.get("bbox_diag") for r in rows if r.get("spike") == "S4" and r.get("triangles") == "12"), None)
        diag = next((r.get("bbox_diag") for r in rows if r.get("spike") == "S1" and r.get("name") in accepted), None)
        lines.append("GAP,key=lexer_accepts_double_dot_real,value=reader_accepted,reason=OCCT reads 1..2 with no load-check entry; files=%s; bbox_diag %s against baseline %s; no text scan by decision" % (
            "+".join(accepted), diag, base))
    lines.append("GAP,key=iges_ceiling_supersedes_71,value=%dMiB,reason=the 71 value of 155 MiB breaks the D-07 70 percent line of %d MiB on the production helper; this derived value supersedes it for the production engine" % (
        iges // MIB, PEAK_LIMIT_MIB))
    return lines


def cmd_rules(args):
    rows, checks = load_evidence(args.evidence)
    computed = compute_rules(rows, checks)
    files, with_fails, with_numeric, unknown = summarize_loadcheck(checks)
    summary = "LOADCHECK_SUMMARY,files=%d,with_fails=%d,with_numeric_fails=%d,unknown=%d" % (files, with_fails, with_numeric, unknown)
    if not args.check:
        print(summary)
        for line in computed:
            print(line)
        return 0
    existing = []
    have_summary = None
    with open(args.evidence) as fh:
        for line in fh:
            line = line.rstrip("\n")
            if line.startswith(("RULE,", "DERIVED,", "GAP,")):
                existing.append(line)
            elif line.startswith("LOADCHECK_SUMMARY,"):
                have_summary = line
    problems = []
    if have_summary != summary:
        problems.append("- %s\n+ %s" % (have_summary, summary))
    for line in sorted(set(existing) - set(computed)):
        problems.append("- " + line)
    for line in sorted(set(computed) - set(existing)):
        problems.append("+ " + line)
    if len(existing) != len(computed):
        problems.append("count: have %d RULE/DERIVED/GAP lines, computed %d" % (len(existing), len(computed)))
    if problems:
        print("\n".join(problems))
        return 1
    return 0


def cmd_stops(args):
    rows, checks = load_evidence(args.evidence)
    computed = compute_rules(rows, checks)
    values = {}
    for line in computed:
        kind, fields = parse_line(line)
        values[fields["key"]] = fields["value"]
    problems = []
    if values["load_check"] == "diagnostic":
        partial = [r.get("name") for r in rows if r.get("spike") == "S1" and r.get("shape") == "ok" and (num(r.get("triangles")) or 0) > 0]
        if partial:
            problems.append("STOP,reason=strictness_ambiguity,detail=load_check_diagnostic_and_S1_partial_stl,files=%s" % "+".join(partial))
    if int(values["cad_engine_timeout_s"]) >= 900:
        problems.append("STOP,reason=engine_timeout_at_or_above_900,detail=%s" % values["cad_engine_timeout_s"])
    for line in problems:
        print(line)
    return 1 if problems else 0


def main():
    ap = argparse.ArgumentParser()
    sub = ap.add_subparsers(dest="cmd", required=True)

    p = sub.add_parser("count")
    p.add_argument("--format", choices=("step", "iges"), required=True)
    p.add_argument("--with-regex", action="store_true")
    p.add_argument("input")
    p.set_defaults(fn=cmd_count)

    p = sub.add_parser("read")
    p.add_argument("--format", choices=("step", "iges"), required=True)
    p.add_argument("input")
    p.add_argument("outdir")
    p.set_defaults(fn=cmd_read)

    p = sub.add_parser("gen-bombs")
    p.add_argument("outdir")
    p.add_argument("--flood-entities", type=int, default=0)
    p.add_argument("--flood-nauo", type=int, default=0)
    p.set_defaults(fn=cmd_gen_bombs)

    p = sub.add_parser("gen-hierarchy")
    p.add_argument("--kind", choices=("nauo", "mapped"), required=True)
    p.add_argument("--levels", type=int, required=True)
    p.add_argument("--leaf", required=True)
    p.add_argument("out")
    p.set_defaults(fn=cmd_gen_hierarchy)

    p = sub.add_parser("copies-target")
    p.add_argument("--base", required=True)
    p.add_argument("--limit-bytes", type=int, required=True)
    p.set_defaults(fn=cmd_copies_target)

    p = sub.add_parser("gen-mapped-text")
    p.add_argument("--levels", type=int, required=True)
    p.add_argument("out")
    p.set_defaults(fn=cmd_gen_mapped_text)

    p = sub.add_parser("rowline")
    p.add_argument("--spike", required=True)
    p.add_argument("--name", default="")
    p.add_argument("--format", required=True)
    p.add_argument("--input", default="")
    p.add_argument("--image-id", required=True)
    p.add_argument("--wrap-file", default="")
    p.add_argument("--read-json", default="")
    p.add_argument("--count-json", default="")
    p.add_argument("--extra", action="append", default=[])
    p.set_defaults(fn=cmd_rowline)

    p = sub.add_parser("loadcheck")
    p.add_argument("--name", required=True)
    p.add_argument("--read-json", required=True)
    p.set_defaults(fn=cmd_loadcheck)

    p = sub.add_parser("binding")
    p.add_argument("--read-json", required=True)
    p.set_defaults(fn=cmd_binding)

    p = sub.add_parser("rules")
    p.add_argument("evidence")
    p.add_argument("--check", action="store_true")
    p.set_defaults(fn=cmd_rules)

    p = sub.add_parser("stops")
    p.add_argument("evidence")
    p.set_defaults(fn=cmd_stops)

    args = ap.parse_args()
    return args.fn(args)


if __name__ == "__main__":
    sys.exit(main())

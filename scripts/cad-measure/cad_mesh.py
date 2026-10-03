#!/usr/bin/env python3
"""cad_mesh.py -- measure a STEP/IGES part with OCP or an STL with numpy.

Usage:
    cad_mesh.py mesh IN OUTDIR [--quality draft|standard|fine] [--parallel on|off|affinity] [--dump-maps PATH]
    cad_mesh.py stlstats IN OUTDIR [--dump-maps PATH]
"""

import argparse
import json
import mmap
import os
import re
import sys

QUALITY = {
    "draft": (0.004, 0.5),
    "standard": (0.001, 0.35),
    "fine": (0.0003, 0.2),
}

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
IGES_FLAG = {1: "inch", 2: "mm", 4: "foot", 6: "m", 9: "micrometre", 10: "cm"}

GUAC_RE = re.compile(rb"GLOBAL_UNIT_ASSIGNED_CONTEXT\s*\(\s*\(([^)]*)\)\s*\)")
SI_RE = re.compile(rb"SI_UNIT\s*\(\s*(?:\.([A-Z]+)\.|\$)\s*,\s*\.METRE\.\s*\)")
CONV_RE = re.compile(rb"CONVERSION_BASED_UNIT\s*\(\s*'([^']*)'")
PRODUCT_RE = re.compile(rb"PRODUCT\s*\(\s*'([^']*)'")
X2_RE = re.compile(r"\\X2\\([0-9A-Fa-f]+)\\X0\\")
X1_RE = re.compile(r"\\X\\([0-9A-Fa-f]{2})")


FINITE_LIMIT = 1e12


def box_diag(box):
    lo, hi = box.CornerMin(), box.CornerMax()
    return ((hi.X() - lo.X()) ** 2 + (hi.Y() - lo.Y()) ** 2 + (hi.Z() - lo.Z()) ** 2) ** 0.5


def ocp_version():
    try:
        import importlib.metadata as md
        return md.version("cadquery-ocp-novtk")
    except Exception:
        return "unknown"


def dump_maps(path):
    if not path:
        return
    libs = set()
    with open("/proc/self/maps") as fh:
        for line in fh:
            parts = line.split(None, 5)
            if len(parts) == 6 and ".so" in parts[5]:
                libs.add(parts[5].strip().replace(" (deleted)", ""))
    with open(path, "w") as fh:
        fh.write("\n".join(sorted(libs)) + "\n")


def normalize_unit(name):
    key = re.sub(r"[^a-z_]", "", (name or "").strip().lower().replace(" ", "_"))
    return UNIT_NAMES.get(key, "unknown")


def step_unescape(text):
    text = X2_RE.sub(lambda m: bytes.fromhex(m.group(1)).decode("utf-16-be", "replace"), text)
    return X1_RE.sub(lambda m: bytes([int(m.group(1), 16)]).decode("latin-1"), text)


def decode_text(raw):
    try:
        return raw.decode("utf-8"), "utf-8"
    except UnicodeDecodeError:
        return raw.decode("latin-1"), "latin-1-fallback"


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


def step_text_facts(path):
    unit = "unknown"
    contexts = 0
    text0 = ""
    decoding = ""
    with open(path, "rb") as fh:
        with mmap.mmap(fh.fileno(), 0, access=mmap.ACCESS_READ) as mm:
            seen = []
            for m in GUAC_RE.finditer(mm):
                contexts += 1
                for ref in re.findall(rb"#(\d+)", m.group(1)):
                    found = length_unit_of(mm, ref)
                    if found is not None:
                        seen.append(found)
                        break
                if contexts >= 8:
                    break
            if seen:
                unit = seen[0]
                if len(set(seen)) > 1:
                    unit = "mixed"
            prod = PRODUCT_RE.search(mm)
            if prod:
                text0, decoding = decode_text(prod.group(1))
                text0 = step_unescape(text0)
    return unit, text0, decoding, contexts


def iges_records(data):
    if b"\n" in data[:400] or b"\r" in data[:400]:
        return [ln.rstrip(b"\r") for ln in data.split(b"\n") if ln.strip()]
    return [data[i:i + 80] for i in range(0, len(data), 80)]


def iges_params(text):
    out = []
    delim = ","
    pos = 0
    if text.startswith(","):
        out.append("")
        pos = 1
    else:
        m = re.match(r"1H(.)", text)
        if not m:
            return out
        delim = m.group(1)
        out.append(delim)
        pos = m.end()
        if text[pos:pos + 1] == delim:
            pos += 1
    while pos < len(text) and len(out) < 20:
        m = re.match(r"(\d+)H", text[pos:])
        if m:
            n = int(m.group(1))
            start = pos + m.end()
            out.append(text[start:start + n])
            pos = start + n
        else:
            end = pos
            while end < len(text) and text[end] not in (delim, ";"):
                end += 1
            out.append(text[pos:end].strip())
            pos = end
        if text[pos:pos + 1] == ";":
            break
        if text[pos:pos + 1] == delim:
            pos += 1
    return out


def iges_text_facts(path):
    with open(path, "rb") as fh:
        data = fh.read()
    lines = [r[:72].decode("latin-1") for r in iges_records(data) if len(r) >= 73 and r[72:73] == b"G"]
    params = iges_params("".join(lines))
    flag = None
    name = ""
    if len(params) >= 14:
        try:
            flag = int(params[13])
        except ValueError:
            flag = None
    if len(params) >= 15:
        name = params[14]
    if flag == 3:
        return normalize_unit(name), flag, name
    return IGES_FLAG.get(flag, "unknown"), flag, name


def file_units_step(reader):
    from OCP.TColStd import TColStd_SequenceOfAsciiString
    try:
        length = TColStd_SequenceOfAsciiString()
        angle = TColStd_SequenceOfAsciiString()
        solid_angle = TColStd_SequenceOfAsciiString()
        reader.FileUnits(length, angle, solid_angle)
        names = [length.Value(i).ToCString() for i in range(1, length.Length() + 1)]
        return normalize_unit(names[0]) if names else "none"
    except Exception as exc:
        return "error_" + type(exc).__name__


def file_units_iges(reader):
    try:
        glob = reader.IGESModel().GlobalSection()
        flag = glob.UnitFlag()
        if flag == 3:
            return normalize_unit(glob.UnitName().ToCString())
        return IGES_FLAG.get(flag, "unknown")
    except Exception as exc:
        return "error_" + type(exc).__name__


def cpu_quota_count():
    total = len(os.sched_getaffinity(0))
    try:
        with open("/sys/fs/cgroup/cpu.max") as fh:
            fields = fh.read().split()
        if fields and fields[0] != "max" and len(fields) >= 2:
            quota, period = int(fields[0]), int(fields[1])
            if quota > 0 and period > 0:
                return max(1, min(total, quota // period))
    except (OSError, ValueError):
        pass
    return total


def apply_parallel(mode):
    if mode == "off":
        return False, len(os.sched_getaffinity(0))
    if mode == "affinity":
        n = cpu_quota_count()
        cpus = sorted(os.sched_getaffinity(0))[:n]
        os.sched_setaffinity(0, set(cpus))
        return True, len(cpus)
    return True, len(os.sched_getaffinity(0))


def mesh(src, outdir, quality, parallel):
    from OCP.BRep import BRep_Tool
    from OCP.BRepBndLib import BRepBndLib
    from OCP.BRepCheck import BRepCheck_Analyzer
    from OCP.BRepGProp import BRepGProp
    from OCP.BRepMesh import BRepMesh_IncrementalMesh
    from OCP.Bnd import Bnd_Box
    from OCP.GProp import GProp_GProps
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.IGESControl import IGESControl_Reader
    from OCP.STEPControl import STEPControl_Reader
    from OCP.StlAPI import StlAPI_Writer
    from OCP.TopAbs import TopAbs_FACE, TopAbs_SHELL, TopAbs_SOLID
    from OCP.TopExp import TopExp_Explorer

    ext = os.path.splitext(src)[1].lower()
    is_iges = ext in (".iges", ".igs")
    reader = IGESControl_Reader() if is_iges else STEPControl_Reader()
    if reader.ReadFile(src) != IFSelect_RetDone:
        print("cad_mesh: read failed", file=sys.stderr)
        return 2
    reader.TransferRoots()
    shape = reader.OneShape()
    if shape.IsNull():
        print("cad_mesh: no shape transferred", file=sys.stderr)
        return 2

    solids = 0
    closed = 0
    open_solids = 0
    invalid = 0
    volume = 0.0
    area = 0.0
    explorer = TopExp_Explorer(shape, TopAbs_SOLID)
    while explorer.More():
        solid = explorer.Current()
        solids += 1
        shells_closed = True
        shells = TopExp_Explorer(solid, TopAbs_SHELL)
        while shells.More():
            shells_closed = shells_closed and BRep_Tool.IsClosed_s(shells.Current())
            shells.Next()
        if not shells_closed:
            open_solids += 1
        elif not BRepCheck_Analyzer(solid).IsValid():
            invalid += 1
        else:
            closed += 1
            props = GProp_GProps()
            BRepGProp.VolumeProperties_s(solid, props)
            volume += props.Mass()
            sprops = GProp_GProps()
            BRepGProp.SurfaceProperties_s(solid, sprops)
            area += sprops.Mass()
        explorer.Next()

    faces = 0
    fx = TopExp_Explorer(shape, TopAbs_FACE)
    while fx.More():
        faces += 1
        fx.Next()

    reason = None
    vol_out = None
    area_out = None
    if closed == 0:
        if solids == 0:
            reason = "no_solids"
        elif open_solids:
            reason = "open_shell"
        else:
            reason = "invalid_solid"
    elif volume < 0:
        reason = "negative_volume"
    else:
        vol_out = volume
        area_out = area

    box = Bnd_Box()
    BRepBndLib.Add_s(shape, box)
    unbounded_faces = 0
    bbox_source = "shape"
    if box.IsVoid() or box_diag(box) > FINITE_LIMIT:
        box = Bnd_Box()
        bbox_source = "finite_faces"
        fx = TopExp_Explorer(shape, TopAbs_FACE)
        while fx.More():
            fbox = Bnd_Box()
            BRepBndLib.AddOptimal_s(fx.Current(), fbox, False, False)
            if fbox.IsVoid() or box_diag(fbox) > FINITE_LIMIT:
                unbounded_faces += 1
            else:
                box.Add(fbox)
            fx.Next()
    if box.IsVoid():
        print("cad_mesh: empty bounding box", file=sys.stderr)
        return 3
    lo, hi = box.CornerMin(), box.CornerMax()
    bmin = [lo.X(), lo.Y(), lo.Z()]
    bmax = [hi.X(), hi.Y(), hi.Z()]
    diag = sum((b - a) ** 2 for a, b in zip(bmin, bmax)) ** 0.5
    if diag <= 0:
        print("cad_mesh: zero-size bounding box", file=sys.stderr)
        return 3

    lin_k, ang = QUALITY[quality]
    lin = lin_k * diag
    in_parallel, ncpu = apply_parallel(parallel)
    BRepMesh_IncrementalMesh(shape, lin, False, ang, in_parallel)
    stl = os.path.join(outdir, "part.stl")
    writer = StlAPI_Writer()
    writer.ASCIIMode = False
    if not writer.Write(shape, stl):
        print("cad_mesh: STL write failed", file=sys.stderr)
        return 3
    triangles = (os.path.getsize(stl) - 84) // 50

    if is_iges:
        unit_text, iges_flag, iges_name = iges_text_facts(src)
        fileunits = file_units_iges(reader)
        text0, decoding, contexts = "", "", 0
    else:
        unit_text, text0, decoding, contexts = step_text_facts(src)
        fileunits = file_units_step(reader)
        iges_flag, iges_name = None, ""

    result = {
        "solids": solids,
        "closed_solids": closed,
        "open_solids": open_solids,
        "invalid_solids": invalid,
        "faces": faces,
        "volume": vol_out,
        "area": area_out,
        "volume_null_reason": reason,
        "bbox": {"min": bmin, "max": bmax},
        "bbox_diag": round(diag, 6),
        "bbox_unit": "mm_occt",
        "bbox_source": bbox_source,
        "unbounded_faces": unbounded_faces,
        "unit_text": unit_text,
        "unit_fileunits": fileunits,
        "unit_agree": 1 if unit_text == fileunits else 0,
        "unit_contexts": contexts,
        "triangles": triangles,
        "quality": quality,
        "lin_deflection": lin,
        "ang_deflection": ang,
        "parallel": parallel,
        "cpus_used": ncpu,
        "ocp_version": ocp_version(),
    }
    if is_iges:
        result["iges_unit_flag"] = iges_flag
        result["iges_unit_name"] = iges_name
    if os.environ.get("CAD_EMIT_TEXT") == "1":
        result["text0"] = text0
        result["text0_decoding"] = decoding
    with open(os.path.join(outdir, "measure.json"), "w") as fh:
        json.dump(result, fh, sort_keys=True)
    return 0


def read_stl(path):
    import numpy as np

    with open(path, "rb") as fh:
        data = fh.read()
    binary = False
    if len(data) >= 84:
        n = int.from_bytes(data[80:84], "little")
        binary = len(data) == 84 + 50 * n
    if binary:
        rec = np.frombuffer(data, dtype=np.dtype([
            ("n", "<f4", 3), ("v", "<f4", (3, 3)), ("a", "<u2")]), count=n, offset=84)
        tri = rec["v"].astype(np.float64)
    else:
        nums = re.findall(rb"vertex\s+(\S+)\s+(\S+)\s+(\S+)", data)
        if not nums or len(nums) % 3:
            return None, False
        tri = np.array(nums, dtype=np.float64).reshape(-1, 3, 3)
    return tri, binary


def stlstats(src, outdir):
    import numpy as np

    tri, binary = read_stl(src)
    if tri is None or len(tri) == 0:
        print("cad_mesh: STL has no triangles", file=sys.stderr)
        return 2
    pts = tri.reshape(-1, 3)
    bmin = pts.min(axis=0)
    bmax = pts.max(axis=0)
    diag = float(np.sqrt(((bmax - bmin) ** 2).sum()))
    _, inv = np.unique(pts, axis=0, return_inverse=True)
    idx = inv.reshape(-1, 3).astype(np.int64)
    nv = int(idx.max()) + 1
    a = np.concatenate([idx[:, 0], idx[:, 1], idx[:, 2]])
    b = np.concatenate([idx[:, 1], idx[:, 2], idx[:, 0]])
    fwd = a * nv + b
    rev = b * nv + a
    keys, counts = np.unique(fwd, return_counts=True)
    watertight = bool((a != b).all() and (counts == 1).all() and np.isin(rev, keys).all())

    vol = None
    reason = None
    if watertight:
        signed = float(np.einsum("ij,ij->i", tri[:, 0], np.cross(tri[:, 1], tri[:, 2])).sum() / 6.0)
        if signed < 0:
            reason = "negative_volume"
        else:
            vol = signed
    else:
        reason = "stl_not_watertight"

    result = {
        "triangles": int(len(tri)),
        "vertices": nv,
        "ascii": 0 if binary else 1,
        "bbox": {"min": [float(x) for x in bmin], "max": [float(x) for x in bmax]},
        "bbox_diag": round(diag, 6),
        "bbox_unit": "file",
        "watertight": 1 if watertight else 0,
        "volume": vol,
        "volume_null_reason": reason,
        "unit_text": "unspecified",
        "ocp_version": ocp_version(),
    }
    with open(os.path.join(outdir, "measure.json"), "w") as fh:
        json.dump(result, fh, sort_keys=True)
    return 0


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("cmd", choices=("mesh", "stlstats"))
    ap.add_argument("src")
    ap.add_argument("outdir")
    ap.add_argument("--quality", choices=sorted(QUALITY), default="standard")
    ap.add_argument("--parallel", choices=("on", "off", "affinity"), default="on")
    ap.add_argument("--dump-maps", default="")
    args = ap.parse_args(argv[1:])
    try:
        if args.cmd == "mesh":
            return mesh(args.src, args.outdir, args.quality, args.parallel)
        return stlstats(args.src, args.outdir)
    finally:
        dump_maps(args.dump_maps)


if __name__ == "__main__":
    sys.exit(main(sys.argv))

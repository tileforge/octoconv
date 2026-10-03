#!/usr/bin/env python3
"""cad_mesh.py -- measure a STEP part with OCP and write an STL.

Usage:
    cad_mesh.py mesh IN OUTDIR
"""

import json
import mmap
import os
import re
import sys

from OCP.BRep import BRep_Tool
from OCP.BRepBndLib import BRepBndLib
from OCP.BRepGProp import BRepGProp
from OCP.BRepMesh import BRepMesh_IncrementalMesh
from OCP.Bnd import Bnd_Box
from OCP.GProp import GProp_GProps
from OCP.IFSelect import IFSelect_RetDone
from OCP.StlAPI import StlAPI_Writer
from OCP.STEPControl import STEPControl_Reader
from OCP.TColStd import TColStd_SequenceOfAsciiString
from OCP.TopAbs import TopAbs_SHELL, TopAbs_SOLID
from OCP.TopExp import TopExp_Explorer

OCP_VERSION = "unknown"
try:
    import importlib.metadata as _md
    OCP_VERSION = _md.version("cadquery-ocp-novtk")
except Exception:
    pass

LIN_FACTOR = 0.001
ANGULAR = 0.35

SI_RE = re.compile(rb"SI_UNIT\(\s*(?:\.(\w+)\.|\$)\s*,\s*\.METRE\.\s*\)")
CONV_RE = re.compile(rb"CONVERSION_BASED_UNIT\(\s*'([^']*)'")
PRODUCT_RE = re.compile(rb"PRODUCT\(\s*'([^']*)'")


def step_text_facts(path):
    unit = "none"
    text0 = ""
    with open(path, "rb") as fh:
        with mmap.mmap(fh.fileno(), 0, access=mmap.ACCESS_READ) as mm:
            conv = CONV_RE.search(mm)
            if conv:
                unit = conv.group(1).decode("latin-1")
            else:
                si = SI_RE.search(mm)
                if si:
                    unit = ((si.group(1) or b"").decode("latin-1") + "_METRE").lstrip("_")
            prod = PRODUCT_RE.search(mm)
            if prod:
                text0 = prod.group(1).decode("latin-1")
    return unit, text0


def file_units(reader):
    try:
        length = TColStd_SequenceOfAsciiString()
        angle = TColStd_SequenceOfAsciiString()
        solid_angle = TColStd_SequenceOfAsciiString()
        reader.FileUnits(length, angle, solid_angle)
        names = [length.Value(i).ToCString() for i in range(1, length.Length() + 1)]
        return ",".join(names) if names else "none"
    except Exception as exc:
        return "error_" + type(exc).__name__


def mesh(src, outdir):
    reader = STEPControl_Reader()
    if reader.ReadFile(src) != IFSelect_RetDone:
        print("cad_mesh: STEP read failed", file=sys.stderr)
        return 2
    reader.TransferRoots()
    shape = reader.OneShape()

    solids = 0
    closed = 0
    volume = 0.0
    explorer = TopExp_Explorer(shape, TopAbs_SOLID)
    while explorer.More():
        solid = explorer.Current()
        solids += 1
        props = GProp_GProps()
        BRepGProp.VolumeProperties_s(solid, props)
        volume += props.Mass()
        shells = TopExp_Explorer(solid, TopAbs_SHELL)
        all_closed = True
        while shells.More():
            all_closed = all_closed and BRep_Tool.IsClosed_s(shells.Current())
            shells.Next()
        if all_closed:
            closed += 1
        explorer.Next()

    box = Bnd_Box()
    BRepBndLib.Add_s(shape, box)
    lo, hi = box.CornerMin(), box.CornerMax()
    bmin = [lo.X(), lo.Y(), lo.Z()]
    bmax = [hi.X(), hi.Y(), hi.Z()]
    diag = sum((b - a) ** 2 for a, b in zip(bmin, bmax)) ** 0.5

    BRepMesh_IncrementalMesh(shape, LIN_FACTOR * diag, False, ANGULAR, True)
    stl = os.path.join(outdir, "part.stl")
    writer = StlAPI_Writer()
    writer.ASCIIMode = False
    if not writer.Write(shape, stl):
        print("cad_mesh: STL write failed", file=sys.stderr)
        return 3
    triangles = (os.path.getsize(stl) - 84) // 50

    unit_text, text0 = step_text_facts(src)
    result = {
        "solids": solids,
        "closed_solids": closed,
        "volume": volume if solids else None,
        "unit_text": unit_text,
        "unit_fileunits": file_units(reader),
        "bbox": {"min": bmin, "max": bmax},
        "bbox_diag": round(diag, 6),
        "triangles": triangles,
        "ocp_version": OCP_VERSION,
        "quality": "standard",
    }
    if not solids:
        result["volume_null_reason"] = "no_solids"
    if os.environ.get("CAD_EMIT_TEXT") == "1":
        result["text0"] = text0
    with open(os.path.join(outdir, "measure.json"), "w") as fh:
        json.dump(result, fh, sort_keys=True)
    return 0


def main(argv):
    if len(argv) != 4 or argv[1] != "mesh":
        print(__doc__, file=sys.stderr)
        return 64
    return mesh(argv[2], argv[3])


if __name__ == "__main__":
    sys.exit(main(sys.argv))

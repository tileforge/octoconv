#!/usr/bin/env python3
"""cad_dxf.py -- draw a DXF model space to a square SVG with ezdxf.

Usage:
    cad_dxf.py IN OUTDIR [--size 1280] [--dump-maps PATH]
"""

import argparse
import json
import os
import re
import sys
import time

PAGE_MM = 200.0
MARGIN_MM = 10.0


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


def run(src, outdir, size):
    home = os.path.join(outdir, "home")
    os.makedirs(home, exist_ok=True)
    os.environ["HOME"] = home
    os.environ["XDG_CACHE_HOME"] = os.path.join(home, ".cache")

    import ezdxf
    from ezdxf import bbox, recover, units
    from ezdxf.addons.drawing import Frontend, RenderContext, config, layout, svg

    skipped = {}

    class RecordingFrontend(Frontend):
        def skip_entity(self, entity, msg):
            kind = entity.dxftype()
            skipped[kind] = skipped.get(kind, 0) + 1

    t0 = time.monotonic()
    try:
        doc, auditor = recover.readfile(src)
    except Exception as exc:
        print("cad_dxf: read failed: %s" % type(exc).__name__, file=sys.stderr)
        return 2
    t_read = time.monotonic() - t0

    msp = doc.modelspace()
    counts = {}
    text0 = None
    for e in msp:
        kind = e.dxftype()
        counts[kind] = counts.get(kind, 0) + 1
        if text0 is None and kind in ("TEXT", "MTEXT"):
            try:
                text0 = e.plain_text() if kind == "MTEXT" else e.dxf.text
            except Exception:
                text0 = ""
    if not counts:
        print("cad_dxf: empty model space", file=sys.stderr)
        return 3

    extents = bbox.extents(msp)
    bbox2d = None
    if extents.has_data:
        bbox2d = [extents.extmin.x, extents.extmin.y, extents.extmax.x, extents.extmax.y]

    raw_units = doc.header.get("$INSUNITS", None)
    try:
        unit_name = units.decode(raw_units).lower() if raw_units else "unspecified"
    except Exception:
        unit_name = "unknown"
    result = {
        "bbox2d": bbox2d,
        "insunits": "%s:%s" % (raw_units if raw_units is not None else "none", unit_name or "unspecified"),
        "codepage": doc.header.get("$DWGCODEPAGE", None),
        "acadver": doc.dxfversion,
        "entity_counts": counts,
        "auditor_errors": len(auditor.errors) + len(auditor.fixes),
        "size": size,
        "t_read_s": round(t_read, 3),
        "ezdxf_version": ezdxf.__version__,
    }
    if os.environ.get("CAD_EMIT_TEXT") == "1":
        result["text0"] = text0

    t1 = time.monotonic()
    backend = svg.SVGBackend()
    cfg = config.Configuration(background_policy=config.BackgroundPolicy.WHITE)
    RecordingFrontend(RenderContext(doc), backend, config=cfg).draw_layout(msp)
    result["skipped_types"] = sorted("%s-%d" % kv for kv in skipped.items())
    page = layout.Page(PAGE_MM, PAGE_MM, layout.Units.mm, margins=layout.Margins.all(MARGIN_MM))
    try:
        text = backend.get_string(page, settings=layout.Settings(fit_page=True))
    except ValueError:
        result["drawn"] = 0
        write_result(outdir, result)
        print("cad_dxf: nothing drawable in the model space", file=sys.stderr)
        return 3
    t_draw = time.monotonic() - t1
    with open(os.path.join(outdir, "out.svg"), "w") as fh:
        fh.write(text)
    result["drawn"] = 1
    result["t_render_s"] = round(t_draw, 3)
    result["svg_bytes"] = len(text.encode("utf-8"))
    result["svg_counts"] = {
        "script": len(re.findall(r"<script\b", text, re.I)),
        "foreignObject": len(re.findall(r"<foreignObject\b", text, re.I)),
        "external_href": len(re.findall(r"""(?:xlink:)?href\s*=\s*["'](?!#)""", text, re.I)),
    }
    write_result(outdir, result)
    return 0


def write_result(outdir, result):
    with open(os.path.join(outdir, "measure.json"), "w") as fh:
        json.dump(result, fh, sort_keys=True)


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("src")
    ap.add_argument("outdir")
    ap.add_argument("--size", type=int, default=1280)
    ap.add_argument("--dump-maps", default="")
    args = ap.parse_args(argv[1:])
    try:
        return run(args.src, args.outdir, args.size)
    finally:
        dump_maps(args.dump_maps)


if __name__ == "__main__":
    sys.exit(main(sys.argv))

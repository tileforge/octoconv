#!/usr/bin/env python3
"""cad_render.py -- render an STL to a PNG with VTK on OSMesa.

Usage:
    cad_render.py IN.stl OUT.png [--view iso|front|back|left|right|top|bottom] [--size 1280]
        [--background white|transparent] [--lp-threads N|default] [--margin 0.06] [--dump-maps PATH]

Exit codes: 2 empty input, 3 zero-size geometry, 64 usage.
"""

import argparse
import itertools
import json
import math
import os
import sys
import time

VIEWS = {
    "iso": ((1.0, -1.0, 1.0), (0.0, 0.0, 1.0)),
    "front": ((0.0, -1.0, 0.0), (0.0, 0.0, 1.0)),
    "back": ((0.0, 1.0, 0.0), (0.0, 0.0, 1.0)),
    "left": ((-1.0, 0.0, 0.0), (0.0, 0.0, 1.0)),
    "right": ((1.0, 0.0, 0.0), (0.0, 0.0, 1.0)),
    "top": ((0.0, 0.0, 1.0), (0.0, 1.0, 0.0)),
    "bottom": ((0.0, 0.0, -1.0), (0.0, -1.0, 0.0)),
}
FILL = (0.80, 0.80, 0.80)
EDGE = (0.08, 0.08, 0.10)
FEATURE_ANGLE = 30.0
CAMERA_DISTANCE = 2.0
LIGHTS = (
    ((-0.5, -1.0, 1.5), 0.80),
    ((1.0, 0.5, 0.7), 0.40),
    ((0.0, 1.0, -1.0), 0.25),
)


def sub(a, b):
    return [x - y for x, y in zip(a, b)]


def dot(a, b):
    return sum(x * y for x, y in zip(a, b))


def cross(a, b):
    return [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]]


def unit(a):
    n = math.sqrt(dot(a, a)) or 1.0
    return [x / n for x in a]


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


def render(args):
    from vtkmodules.vtkCommonCore import vtkObject
    from vtkmodules.vtkFiltersCore import vtkCleanPolyData, vtkFeatureEdges, vtkPolyDataNormals
    from vtkmodules.vtkIOGeometry import vtkSTLReader
    from vtkmodules.vtkIOImage import vtkPNGWriter
    from vtkmodules.vtkRenderingCore import (
        vtkActor, vtkLight, vtkPolyDataMapper, vtkRenderer, vtkWindowToImageFilter,
    )
    from vtkmodules.vtkRenderingOpenGL2 import vtkOSOpenGLRenderWindow

    vtkObject.GlobalWarningDisplayOff()
    t0 = time.monotonic()
    reader = vtkSTLReader()
    reader.SetFileName(args.src)
    reader.Update()
    poly = reader.GetOutput()
    triangles = poly.GetNumberOfCells()
    if triangles == 0:
        print("cad_render: input has no triangles", file=sys.stderr)
        return 2
    t_read = time.monotonic() - t0

    x0, x1, y0, y1, z0, z1 = poly.GetBounds()
    center = [(x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2]
    diag = math.sqrt((x1 - x0) ** 2 + (y1 - y0) ** 2 + (z1 - z0) ** 2)
    if not diag > 0 or math.isnan(diag):
        print("cad_render: geometry has zero extent in all axes", file=sys.stderr)
        return 3

    direction, up_hint = VIEWS[args.view]
    direction = unit(direction)
    up = unit(sub(up_hint, [d * dot(up_hint, direction) for d in direction]))
    right = unit(cross([-d for d in direction], up))
    corners = list(itertools.product((x0, x1), (y0, y1), (z0, z1)))
    half_w = max(abs(dot(sub(c, center), right)) for c in corners)
    half_h = max(abs(dot(sub(c, center), up)) for c in corners)
    long_half = max(half_w, half_h)
    short_half = min(half_w, half_h)
    scale = long_half / (1.0 - 2.0 * args.margin)

    clean = vtkCleanPolyData()
    clean.SetInputData(poly)
    clean.Update()

    normals = vtkPolyDataNormals()
    normals.SetInputConnection(clean.GetOutputPort())
    normals.SetFeatureAngle(FEATURE_ANGLE)
    normals.SplittingOn()

    edges = vtkFeatureEdges()
    edges.SetInputConnection(clean.GetOutputPort())
    edges.BoundaryEdgesOn()
    edges.FeatureEdgesOn()
    edges.NonManifoldEdgesOn()
    edges.ManifoldEdgesOff()
    edges.SetFeatureAngle(FEATURE_ANGLE)
    edges.ColoringOff()

    surf_mapper = vtkPolyDataMapper()
    surf_mapper.SetInputConnection(normals.GetOutputPort())
    surf_mapper.SetResolveCoincidentTopologyToPolygonOffset()
    surf = vtkActor()
    surf.SetMapper(surf_mapper)
    surf.GetProperty().SetColor(*FILL)
    surf.GetProperty().SetAmbient(0.45)
    surf.GetProperty().SetDiffuse(0.55)
    surf.GetProperty().SetSpecular(0.0)

    edge_mapper = vtkPolyDataMapper()
    edge_mapper.SetInputConnection(edges.GetOutputPort())
    edge_mapper.ScalarVisibilityOff()
    edge = vtkActor()
    edge.SetMapper(edge_mapper)
    edge.GetProperty().SetColor(*EDGE)
    edge.GetProperty().SetLineWidth(max(1.0, args.size / 640.0))
    edge.GetProperty().LightingOff()

    transparent = args.background == "transparent"
    renderer = vtkRenderer()
    renderer.SetBackground(1.0, 1.0, 1.0)
    renderer.SetBackgroundAlpha(0.0 if transparent else 1.0)
    renderer.AutomaticLightCreationOff()
    renderer.AddActor(surf)
    renderer.AddActor(edge)
    for vec, intensity in LIGHTS:
        light = vtkLight()
        light.SetLightTypeToSceneLight()
        light.SetPositional(False)
        light.SetFocalPoint(*center)
        light.SetPosition(*[c + v * diag for c, v in zip(center, unit(vec))])
        light.SetIntensity(intensity)
        renderer.AddLight(light)
    renderer.UseFXAAOn()

    win = vtkOSOpenGLRenderWindow()
    win.SetOffScreenRendering(1)
    win.SetMultiSamples(0)
    if transparent:
        win.SetAlphaBitPlanes(1)
    win.SetSize(args.size, args.size)
    win.AddRenderer(renderer)

    cam = renderer.GetActiveCamera()
    cam.ParallelProjectionOn()
    cam.SetFocalPoint(*center)
    cam.SetPosition(*[c + d * CAMERA_DISTANCE * diag for c, d in zip(center, direction)])
    cam.SetViewUp(*up)
    cam.SetParallelScale(scale)
    renderer.ResetCameraClippingRange()

    t1 = time.monotonic()
    win.Render()
    t_render = time.monotonic() - t1

    t2 = time.monotonic()
    grab = vtkWindowToImageFilter()
    grab.SetInput(win)
    grab.ReadFrontBufferOff()
    if transparent:
        grab.SetInputBufferTypeToRGBA()
    grab.Update()
    writer = vtkPNGWriter()
    writer.SetFileName(args.dst)
    writer.SetInputConnection(grab.GetOutputPort())
    writer.Write()
    t_write = time.monotonic() - t2

    info = {
        "triangles": triangles,
        "bbox": {"min": [x0, y0, z0], "max": [x1, y1, z1]},
        "view": args.view,
        "direction": direction,
        "up": up,
        "parallel_scale": scale,
        "margin": args.margin,
        "size": args.size,
        "background": args.background,
        "lp_threads": args.lp_threads,
        "proj_half_w": half_w,
        "proj_half_h": half_h,
        "proj_aspect": (short_half / long_half) if long_half > 0 else 0.0,
        "t_read_s": round(t_read, 3),
        "t_render_s": round(t_render, 3),
        "t_write_s": round(t_write, 3),
        "t_total_s": round(time.monotonic() - t0, 3),
    }
    with open(args.dst + ".json", "w") as fh:
        json.dump(info, fh, sort_keys=True)
    return 0


def main(argv):
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("src")
    ap.add_argument("dst")
    ap.add_argument("--view", default="iso", choices=sorted(VIEWS))
    ap.add_argument("--size", type=int, default=1280)
    ap.add_argument("--background", default="white", choices=("white", "transparent"))
    ap.add_argument("--lp-threads", default="default")
    ap.add_argument("--margin", type=float, default=0.06)
    ap.add_argument("--dump-maps", default="")
    args = ap.parse_args(argv[1:])
    if args.size < 16 or not 0.0 <= args.margin < 0.5:
        print("cad_render: size or margin out of range", file=sys.stderr)
        return 64
    if args.lp_threads != "default":
        if not args.lp_threads.isdigit() or int(args.lp_threads) < 1:
            print("cad_render: --lp-threads must be a positive integer or default", file=sys.stderr)
            return 64
        os.environ["LP_NUM_THREADS"] = args.lp_threads
    try:
        return render(args)
    finally:
        dump_maps(args.dump_maps)


if __name__ == "__main__":
    sys.exit(main(sys.argv))

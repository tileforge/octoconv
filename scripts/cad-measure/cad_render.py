#!/usr/bin/env python3
"""cad_render.py -- render an STL to a PNG with VTK on OSMesa.

Usage:
    cad_render.py IN.stl OUT.png [--view iso] [--size 1280]
"""

import argparse
import itertools
import json
import math
import sys
import time

from vtkmodules.vtkCommonCore import vtkObject
from vtkmodules.vtkFiltersCore import vtkCleanPolyData, vtkFeatureEdges, vtkPolyDataNormals
from vtkmodules.vtkIOGeometry import vtkSTLReader
from vtkmodules.vtkIOImage import vtkPNGWriter
from vtkmodules.vtkRenderingCore import (
    vtkActor, vtkPolyDataMapper, vtkRenderer, vtkWindowToImageFilter,
)
from vtkmodules.vtkRenderingOpenGL2 import vtkOSOpenGLRenderWindow

FIELD = 0.06
VIEWS = {"iso": (1.0, -1.0, 1.0)}
FILL = (0.78, 0.79, 0.81)
EDGE = (0.08, 0.08, 0.10)


def sub(a, b):
    return [x - y for x, y in zip(a, b)]


def dot(a, b):
    return sum(x * y for x, y in zip(a, b))


def cross(a, b):
    return [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]]


def unit(a):
    n = math.sqrt(dot(a, a)) or 1.0
    return [x / n for x in a]


def render(src, dst, view, size):
    vtkObject.GlobalWarningDisplayOff()
    t0 = time.monotonic()
    reader = vtkSTLReader()
    reader.SetFileName(src)
    reader.Update()
    poly = reader.GetOutput()
    triangles = poly.GetNumberOfCells()
    t_read = time.monotonic() - t0

    x0, x1, y0, y1, z0, z1 = poly.GetBounds()
    center = [(x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2]
    corners = list(itertools.product((x0, x1), (y0, y1), (z0, z1)))
    diag = math.sqrt((x1 - x0) ** 2 + (y1 - y0) ** 2 + (z1 - z0) ** 2) or 1.0

    direction = unit(VIEWS[view])
    z_axis = [0.0, 0.0, 1.0]
    up = unit(sub(z_axis, [d * dot(z_axis, direction) for d in direction]))
    right = unit(cross([-d for d in direction], up))
    half_w = max(abs(dot(sub(c, center), right)) for c in corners)
    half_h = max(abs(dot(sub(c, center), up)) for c in corners)
    scale = max(max(half_w, half_h) / (1.0 - 2.0 * FIELD), diag * 1e-6)

    clean = vtkCleanPolyData()
    clean.SetInputData(poly)
    clean.Update()

    normals = vtkPolyDataNormals()
    normals.SetInputConnection(clean.GetOutputPort())
    normals.SetFeatureAngle(30.0)
    normals.SplittingOn()

    edges = vtkFeatureEdges()
    edges.SetInputConnection(clean.GetOutputPort())
    edges.BoundaryEdgesOn()
    edges.FeatureEdgesOn()
    edges.NonManifoldEdgesOn()
    edges.ManifoldEdgesOff()
    edges.SetFeatureAngle(30.0)
    edges.ColoringOff()

    surf_mapper = vtkPolyDataMapper()
    surf_mapper.SetInputConnection(normals.GetOutputPort())
    surf_mapper.SetResolveCoincidentTopologyToPolygonOffset()
    surf = vtkActor()
    surf.SetMapper(surf_mapper)
    surf.GetProperty().SetColor(*FILL)
    surf.GetProperty().SetAmbient(0.45)
    surf.GetProperty().SetDiffuse(0.55)

    edge_mapper = vtkPolyDataMapper()
    edge_mapper.SetInputConnection(edges.GetOutputPort())
    edge_mapper.ScalarVisibilityOff()
    edge = vtkActor()
    edge.SetMapper(edge_mapper)
    edge.GetProperty().SetColor(*EDGE)
    edge.GetProperty().SetLineWidth(2.0)

    renderer = vtkRenderer()
    renderer.SetBackground(1.0, 1.0, 1.0)
    renderer.AddActor(surf)
    renderer.AddActor(edge)

    win = vtkOSOpenGLRenderWindow()
    win.SetOffScreenRendering(1)
    win.SetSize(size, size)
    win.AddRenderer(renderer)

    cam = renderer.GetActiveCamera()
    cam.ParallelProjectionOn()
    cam.SetFocalPoint(*center)
    cam.SetPosition(*[c + d * 3.0 * diag for c, d in zip(center, direction)])
    cam.SetViewUp(*up)
    cam.SetParallelScale(scale)
    renderer.ResetCameraClippingRange()

    t1 = time.monotonic()
    win.Render()
    t_render = time.monotonic() - t1

    grab = vtkWindowToImageFilter()
    grab.SetInput(win)
    grab.ReadFrontBufferOff()
    grab.Update()
    writer = vtkPNGWriter()
    writer.SetFileName(dst)
    writer.SetInputConnection(grab.GetOutputPort())
    writer.Write()

    info = {
        "triangles": triangles,
        "t_read_s": round(t_read, 3),
        "t_render_s": round(t_render, 3),
        "t_total_s": round(time.monotonic() - t0, 3),
        "view": view,
        "size": size,
    }
    with open(dst + ".json", "w") as fh:
        json.dump(info, fh, sort_keys=True)
    return 0


def main():
    ap = argparse.ArgumentParser(description=__doc__.split("\n")[0])
    ap.add_argument("src")
    ap.add_argument("dst")
    ap.add_argument("--view", default="iso", choices=sorted(VIEWS))
    ap.add_argument("--size", type=int, default=1280)
    args = ap.parse_args()
    return render(args.src, args.dst, args.view, args.size)


if __name__ == "__main__":
    sys.exit(main())

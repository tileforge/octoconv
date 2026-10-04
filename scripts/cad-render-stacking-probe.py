#!/usr/bin/env python3
"""cad-render-stacking-probe.py -- Phase 71 (EVAL-03): is the render time cliff of the step-copies
ladder a triangle-count effect or an effect of copies stacked at the same place?

Meshes a step-copies STEP file, then renders the STL twice with the same triangle count: as
meshed (every copy at the same coordinates) and with each copy shifted along x by 1.2 times
the bounding box width. Prints STACKING lines.

Run inside the measurement image (numpy, OCP and VTK come from its venv):
    docker run --rm --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody \\
        -v WORKDIR:/job -v STEPFILE:/in.step:ro --entrypoint /opt/venv/bin/python IMAGE \\
        -I -B /job/cad-render-stacking-probe.py COPIES TRIANGLES_PER_COPY
WORKDIR must be writable by nobody and hold a copy of this script.
"""

import json
import os
import struct
import subprocess
import sys
import time

import numpy as np

LIB = "/opt/cad-measure"
PY = ["/opt/venv/bin/python", "-I", "-B"]
WORK = "/job/stacking"


def render(path):
    t0 = time.monotonic()
    subprocess.run(PY + [LIB + "/cad_render.py", path, path + ".png"], check=True, stdout=subprocess.DEVNULL)
    wall = time.monotonic() - t0
    with open(path + ".png.json") as fh:
        meta = json.load(fh)
    return wall, meta


def main():
    if len(sys.argv) != 3:
        print(__doc__, file=sys.stderr)
        return 64
    copies, per = int(sys.argv[1]), int(sys.argv[2])
    os.makedirs(WORK, exist_ok=True)
    os.chmod(WORK, 0o777)
    subprocess.run(PY + [LIB + "/cad_mesh.py", "mesh", "/in.step", WORK], check=True, stdout=subprocess.DEVNULL)
    with open(WORK + "/part.stl", "rb") as fh:
        raw = fh.read()
    n = struct.unpack("<I", raw[80:84])[0]
    if n != copies * per:
        print("error: %d triangles, expected %d copies x %d" % (n, copies, per), file=sys.stderr)
        return 1
    dt = np.dtype([("n", "<f4", 3), ("v", "<f4", (3, 3)), ("a", "<u2")])
    arr = np.frombuffer(raw, dtype=dt, count=n, offset=84).copy()
    span = float(arr["v"][:, :, 0].max() - arr["v"][:, :, 0].min())
    for i in range(copies):
        arr["v"][i * per:(i + 1) * per, :, 0] += i * span * 1.2
    with open(WORK + "/unstacked.stl", "wb") as fh:
        fh.write(raw[:80] + struct.pack("<I", n) + arr.tobytes())
    for variant, name in (("stacked", "part.stl"), ("unstacked", "unstacked.stl")):
        wall, meta = render(WORK + "/" + name)
        print("STACKING,variant=%s,triangles=%d,copies=%d,wall_s=%.2f,t_total_s=%s,t_render_s=%s" % (
            variant, n, copies, wall, meta.get("t_total_s"), meta.get("t_render_s")), flush=True)
    return 0


if __name__ == "__main__":
    sys.exit(main())

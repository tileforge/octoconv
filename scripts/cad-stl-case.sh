#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

cd "$(dirname "$0")/.."
ROOT=$(pwd)

usage() {
	cat <<'USAGE'
usage: scripts/cad-stl-case.sh [--out FILE]

Builds octoconv-cad-worker:latest from this checkout and runs the STL letter-case
differential on the real helpers inside it: SNIFF (the intake admits every variant),
BASELINE (the pre-fix helper f36fd35 and a direct render of the client bytes fail where
expected), CASE (the fixed stats writes one normalized mesh.stl per variant that measures
and renders like the lowercase original), NONFINITE (a coordinate beyond float32 is
stl_malformed with no mesh.stl), SHELLS (nine binary STLs of outward, inward, hollow,
vertex-touching and zero-volume shells against the per-shell rule, with the pre-fix helper
ac81afe as the differential), UNION (the shell union-find on a randomly labelled cycle at the
ceiling triangle count stays within its round bound), STEP_SIGN (a STEP/IGES compound with
one reversed solid is negative_volume in memory and through a STEP file) and STATS_RSS
(stats peak memory and wall time at the binary ceiling, at the largest uppercase ASCII STL
under it and on one shuffled closed shell at the ceiling, each with its shell count and
within 70% of the memory budget and of the mesh timeout). One evidence line per check goes
to FILE (relative to the repository root, default
.planning/milestones/v1.16-phases/74-3d-render-measurements/evidence/74-08-stl-case.txt).
USAGE
}

EVID=".planning/milestones/v1.16-phases/74-3d-render-measurements/evidence/74-08-stl-case.txt"
while [ $# -gt 0 ]; do
	case "$1" in
	--out)
		[ $# -ge 2 ] || {
			usage >&2
			exit 64
		}
		EVID=$2
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		usage >&2
		exit 64
		;;
	esac
done

BASE_REV=f36fd35
SHELL_BASE_REV=ac81afe
IMAGE=octoconv-cad-worker:latest
SYN=internal/convert/testdata/cad/synthetic
WAVE0=.planning/milestones/v1.16-phases/74-3d-render-measurements/evidence/74-01-wave0.txt
WRAP_PY=scripts/cad-measure/measure_wrap.py
LIMIT_MIB=2867.2
RSS_SHARE_MIB=2007.04
WALL_SHARE_S=221.2
ONE_SHELL_N=563
ONE_SHELL_SEED=74
UNION_NODES=3816804
UNION_SEED=74
STEP_RTOL=1e-9
FLOOR_KIB=$((6 * 1048576))
ASCII_BLOCK=33554432
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2)
VARIANTS=(lower upper vertex_upper mixed_body header_upper header_title lower_header_upper_rest leading_ws binary_solid_header)
OLD_STATS_OK=" lower header_upper header_title leading_ws binary_solid_header "
RENDER_OK=" lower vertex_upper mixed_body lower_header_upper_rest leading_ws binary_solid_header "
export GOTOOLCHAIN=go1.27.3

W=""
cleanup() {
	local rc=$?
	[ -n "$W" ] && rm -rf "$W"
	exit "$rc"
}
trap cleanup EXIT

log() { echo "$*" >&2; }
ev() { echo "$*" | tee -a "$EVID"; }
fail() {
	ev "SUMMARY,result=FAIL,reason=$*"
	exit 1
}
stop() {
	ev "STOP,reason=$*"
	exit 3
}

disk_floor() {
	local free
	free=$(docker run --rm --network none alpine df -k / | awk 'NR==2 { print $4 }')
	case "$free" in '' | *[!0-9]*) stop "cannot read the VM free space (got '$free')" ;; esac
	ev "DISK,at=$1,free_kib=$free,free_gib=$(awk -v k="$free" 'BEGIN { printf "%.1f", k / 1048576 }'),floor_kib=$FLOOR_KIB"
	[ "$free" -ge "$FLOOR_KIB" ] || stop "VM free space $free KiB is below the 6 GiB floor at $1"
}

for tool in docker go python3 shasum; do
	command -v "$tool" >/dev/null 2>&1 || {
		echo "$tool not found on PATH" >&2
		exit 1
	}
done
mkdir -p "$(dirname "$EVID")"
: > "$EVID"
ARCH=$(docker version --format '{{.Server.Arch}}')
[ "$ARCH" = arm64 ] || fail "docker server arch is $ARCH, want arm64"
disk_floor start

W=$(mktemp -d)
chmod 755 "$W"
mkdir -p "$W/bin" "$W/base" "$W/v" "$W/jobs" "$W/wrap" "$W/tile" "$W/probe" "$W/shells"
chmod 755 "$W/base" "$W/v" "$W/wrap" "$W/tile" "$W/probe" "$W/shells"
chmod 777 "$W/jobs"

disk_floor before_build
docker compose build cad-worker >&2 || fail "docker compose build cad-worker failed"

img_sha() { docker run --rm --network none --entrypoint sha256sum "$IMAGE" "$1" | cut -d' ' -f1; }
MESH_IMG=$(img_sha /usr/local/bin/cad-mesh)
RENDER_IMG=$(img_sha /usr/local/bin/cad-render)
MESH_TREE=$(shasum -a 256 cmd/cad-worker/helper/cad-mesh | cut -d' ' -f1)
RENDER_TREE=$(shasum -a 256 cmd/cad-worker/helper/cad-render | cut -d' ' -f1)
[ "$MESH_IMG" = "$MESH_TREE" ] || fail "the image cad-mesh sha256 $MESH_IMG differs from the tree $MESH_TREE"
[ "$RENDER_IMG" = "$RENDER_TREE" ] || fail "the image cad-render sha256 $RENDER_IMG differs from the tree $RENDER_TREE"

ev "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ev "HDR,commit=$(git rev-parse HEAD)"
DIRT=$(git status --porcelain --untracked-files=no | awk -v e="$EVID" '$2 != e { print $2 }')
ev "HDR,tracked_dirt=$(printf '%s' "$DIRT" | grep -c . || true)"
ev "HDR,tracked_dirt_paths=$(printf '%s' "$DIRT" | tr '\n' ';' | sed 's/;$//')"
ev "HDR,image=$IMAGE"
ev "HDR,image_id=$(docker image inspect -f '{{.Id}}' "$IMAGE")"
ev "HDR,helper=cad-mesh,sha256=$MESH_IMG,image_equals_tree=1"
ev "HDR,helper=cad-render,sha256=$RENDER_IMG,image_equals_tree=1"
ev "HDR,server_arch=$ARCH"
ev "HDR,kernel=$(docker run --rm --network none --entrypoint uname "$IMAGE" -r)"
ev "HDR,base_rev=$(git rev-parse "$BASE_REV")"
ev "HDR,shells_base_rev=$(git rev-parse "$SHELL_BASE_REV")"
ev "HDR,limits=$(echo "${LIMITS[*]}" | tr ' ' ';')"

go build -o "$W/bin/cad-sniff" ./scripts/cmd/cad-sniff || fail "cannot build cad-sniff"
go build -o "$W/bin/cad-pixel-judge" ./scripts/cmd/cad-pixel-judge || fail "cannot build cad-pixel-judge"
git show "$BASE_REV:cmd/cad-worker/helper/cad-mesh" > "$W/base/cad-mesh"
chmod 755 "$W/base/cad-mesh"
git show "$SHELL_BASE_REV:cmd/cad-worker/helper/cad-mesh" > "$W/base/cad-mesh-shells"
chmod 755 "$W/base/cad-mesh-shells"
cp "$WRAP_PY" "$W/wrap/measure_wrap.py"
chmod 644 "$W/wrap/measure_wrap.py"

cat > "$W/tool.py" <<'PY'
import json
import math
import struct
import sys

RULES = (
    (b"facet normal", b"Facet Normal"),
    (b"outer loop", b"OUTER loop"),
    (b"vertex", b"VeRtEx"),
    (b"endloop", b"EndLoop"),
    (b"endfacet", b"endFACET"),
)


def variants(src, binsrc, out):
    with open(src, "rb") as fh:
        data = fh.read()
    lines = data.split(b"\n")
    if not (lines[0].startswith(b"solid ") and lines[-2].startswith(b"endsolid ") and lines[-1] == b""):
        raise SystemExit("unexpected layout of %s" % src)
    first, last = lines[0], lines[-2]
    body = b"\n".join(lines[1:-2])
    mixed = body
    for old, new in RULES:
        mixed = mixed.replace(old, new)
    seed = b"vertex 0.000000e+00 0.000000e+00 3.000000e+01"
    if seed not in data:
        raise SystemExit("no seed vertex in %s" % src)
    with open(binsrc, "rb") as fh:
        binary = fh.read()
    out_set = {
        "lower": data,
        "upper": data.upper(),
        "vertex_upper": data.replace(b"vertex", b"VERTEX"),
        "mixed_body": b"\n".join([first, mixed, last, b""]),
        "header_upper": b"\n".join([b"SOLID" + first[5:], body, b"ENDSOLID" + last[8:], b""]),
        "header_title": b"\n".join([b"Solid" + first[5:], body, b"Endsolid" + last[8:], b""]),
        "lower_header_upper_rest": first + b"\n" + b"\n".join(lines[1:]).upper(),
        "leading_ws": b" \t\r\n\n  " + data.replace(b"\n", b"\r\n"),
        "binary_solid_header": binary,
        "nonfinite": data.replace(seed, b"vertex 0.000000e+00 0.000000e+00 1.000000e+39", 1),
    }
    for name, blob in out_set.items():
        with open("%s/%s.stl" % (out, name), "wb") as fh:
            fh.write(blob)


def last_verdict(path, stage):
    last = None
    with open(path, "rb") as fh:
        for raw in fh:
            line = raw.decode("ascii", "replace").rstrip("\r\n")
            if line.startswith("CADVERDICT "):
                try:
                    obj = json.loads(line[len("CADVERDICT "):])
                except ValueError:
                    continue
                if obj.get("stage") == stage:
                    last = obj
    return last


def verdict(path, stage):
    v = last_verdict(path, stage)
    if v is None:
        print("verdict=none,reason=none,triangles=na,view=na,size=na,background=na")
        return
    print("verdict=%s,reason=%s,triangles=%s,view=%s,size=%s,background=%s" % (
        v.get("verdict"), v.get("reason") or "none", v.get("triangles"),
        v.get("view", "na"), v.get("size", "na"), v.get("background", "na")))


def draft(path):
    with open(path) as fh:
        d = json.load(fh)
    lo, hi = d["bbox"]["min"], d["bbox"]["max"]
    print("draft_triangles=%s,bbox=%s-%s,volume=%r,area=%r,null_reason=%s" % (
        d["triangles"], ":".join(repr(float(x)) for x in lo), ":".join(repr(float(x)) for x in hi),
        d["volume"], d["area"], d["null_reason"] or "empty"))


def mesh(path):
    with open(path, "rb") as fh:
        blob = fh.read()
    size = len(blob)
    header_solid = int(blob[:5].lower() == b"solid")
    count = struct.unpack("<I", blob[80:84])[0] if size >= 84 else -1
    records = zero_n = zero_a = finite = coords = 0
    for i in range(max(0, (size - 84) // 50)):
        rec = struct.unpack("<12fH", blob[84 + 50 * i:134 + 50 * i])
        records += 1
        zero_n += int(all(x == 0.0 for x in rec[0:3]) and blob[84 + 50 * i:96 + 50 * i] == b"\0" * 12)
        zero_a += int(rec[12] == 0)
        for x in rec[3:12]:
            coords += 1
            finite += int(math.isfinite(x))
    ok = int(size == 84 + 50 * count and header_solid == 0 and records == count and zero_n == count
             and zero_a == count and finite == coords == 9 * count and count > 0)
    print("mesh_bytes=%d,mesh_header_solid=%d,mesh_count=%d,records=%d,zero_normals=%d,zero_attrs=%d,finite_coords=%d,coords=%d,mesh_ok=%d" % (
        size, header_solid, count, records, zero_n, zero_a, finite, coords, ok))


def base_records(path):
    with open(path, "rb") as fh:
        blob = fh.read()
    count = struct.unpack("<I", blob[80:84])[0]
    recs = [struct.unpack("<12fH", blob[84 + 50 * i:134 + 50 * i]) for i in range(count)]
    span = []
    for axis in (0, 1):
        vals = [r[3 + 3 * v + axis] for r in recs for v in range(3)]
        span.append(max(1.0, (max(vals) - min(vals)) * 2.0))
    return recs, span


def tile_binary(base, out, ceiling):
    recs, span = base_records(base)
    count = len(recs)
    tiles = (ceiling - 84) // (50 * count)
    side = int(math.ceil(math.sqrt(tiles)))
    with open(out, "wb") as fh:
        fh.write(b"octoconv probe tile".ljust(80, b"\0") + struct.pack("<I", tiles * count))
        for t in range(tiles):
            dx, dy = (t % side) * span[0], (t // side) * span[1]
            buf = bytearray()
            for r in recs:
                v = list(r)
                for k in range(3):
                    v[3 + 3 * k] += dx
                    v[4 + 3 * k] += dy
                buf += struct.pack("<12fH", *v)
            fh.write(buf)
    print(tiles * count)


def tile_text(recs, dx, dy):
    parts = []
    for r in recs:
        parts.append("FACET NORMAL 0 0 0\nOUTER LOOP\n")
        for k in range(3):
            parts.append("VERTEX %.9g %.9g %.9g\n" % (r[3 + 3 * k] + dx, r[4 + 3 * k] + dy, r[5 + 3 * k]))
        parts.append("ENDLOOP\nENDFACET\n")
    return "".join(parts).encode("ascii")


def tile_ascii(base, out, ceiling):
    recs, span = base_records(base)
    head, foot = b"SOLID TILE\n", b"ENDSOLID TILE\n"
    estimate = (ceiling - len(head) - len(foot)) // len(tile_text(recs, 0.0, 0.0))
    side = int(math.ceil(math.sqrt(estimate)))
    total = len(head) + len(foot)
    tiles = 0
    with open(out, "wb") as fh:
        fh.write(head)
        while True:
            t = tiles
            text = tile_text(recs, float((t % side) * span[0]), float((t // side) * span[1]))
            if total + len(text) > ceiling:
                break
            fh.write(text)
            total += len(text)
            tiles += 1
        fh.write(foot)
    print(tiles * len(recs))


CUBE_QUADS = (
    ((0, 0, 0), (0, 1, 0), (1, 1, 0), (1, 0, 0)),
    ((0, 0, 1), (1, 0, 1), (1, 1, 1), (0, 1, 1)),
    ((0, 0, 0), (1, 0, 0), (1, 0, 1), (0, 0, 1)),
    ((0, 1, 0), (0, 1, 1), (1, 1, 1), (1, 1, 0)),
    ((0, 0, 0), (0, 0, 1), (0, 1, 1), (0, 1, 0)),
    ((1, 0, 0), (1, 1, 0), (1, 1, 1), (1, 0, 1)),
)


def six_volume(tris):
    total = 0
    for a, b, c in tris:
        total += a[0] * (b[1] * c[2] - b[2] * c[1]) - a[1] * (b[0] * c[2] - b[2] * c[0]) + a[2] * (b[0] * c[1] - b[1] * c[0])
    return total


def cube(origin, size, inward):
    tris = []
    for quad in CUBE_QUADS:
        a, b, c, d = [tuple(origin[k] + size * v[k] for k in range(3)) for v in quad]
        for t in ((a, b, c), (a, c, d)):
            tris.append((t[0], t[2], t[1]) if inward else t)
    want = -6 * size ** 3 if inward else 6 * size ** 3
    if six_volume(tris) != want:
        raise SystemExit("cube %r %r inward=%r has 6V %r, want %r" % (origin, size, inward, six_volume(tris), want))
    return tris


def shells(out):
    base = cube((0, 0, 0), 10, False)
    sheet = ((40, 0, 5), (50, 0, 5), (40, 10, 5))
    fixtures = {
        "cube_out": base,
        "two_out": base + cube((20, 0, 0), 10, False),
        "cube_in": cube((0, 0, 0), 10, True),
        "two_in": cube((0, 0, 0), 10, True) + cube((20, 0, 0), 10, True),
        "out_in_disjoint": base + cube((20, 0, 0), 5, True),
        "hollow": base + cube((3, 3, 3), 4, True),
        "corner_touch_mixed": base + cube((10, 10, 10), 5, True),
        "corner_touch_out": base + cube((10, 10, 10), 5, False),
        "zero_shell": base + [sheet, (sheet[0], sheet[2], sheet[1])],
    }
    if six_volume(fixtures["zero_shell"][12:]) != 0:
        raise SystemExit("the zero shell does not enclose zero volume")
    for name, tris in fixtures.items():
        with open("%s/%s.stl" % (out, name), "wb") as fh:
            fh.write(b"octoconv shells fixture".ljust(80, b"\0") + struct.pack("<I", len(tris)))
            for t in tris:
                fh.write(struct.pack("<12fH", 0.0, 0.0, 0.0, *[float(x) for v in t for x in v], 0))


def outcome(path):
    try:
        with open(path) as fh:
            d = json.load(fh)
    except (OSError, ValueError):
        print("nodraft")
        return
    v, a, r = d.get("volume"), d.get("area"), d.get("null_reason")
    if v is None and a is None and r:
        print(r)
    elif v is not None and a is not None and not r:
        print("%r/%r" % (float(v), float(a)))
    else:
        print("inconsistent")


def summary(path):
    try:
        with open(path) as fh:
            d = json.load(fh)
    except (OSError, ValueError):
        print("solids=na,closed=na,volume=na,area=na,reason=nodraft")
        return
    print("solids=%s,closed=%s,volume=%r,area=%r,reason=%s" % (
        d.get("solids"), d.get("closed_solids"), d.get("volume"), d.get("area"), d.get("null_reason") or "empty"))


def near(got, want, rtol):
    try:
        g, w = float(got), float(want)
    except ValueError:
        return False
    return math.isfinite(g) and abs(g - w) <= rtol * abs(w)


def expect_draft(path, volume, area):
    with open(path) as fh:
        d = json.load(fh)
    v, a, r = d.get("volume"), d.get("area"), d.get("null_reason")
    ok = int(v is not None and a is not None and not r and near(v, volume, 1e-9) and near(a, area, 1e-9))
    print("volume=%r,volume_expected=%r,area=%r,area_expected=%r,draft_null_reason=%s,draft_ok=%d" % (
        v, float(volume), a, float(area), r or "empty", ok))


def main():
    cmd = sys.argv[1]
    if cmd == "shells":
        shells(sys.argv[2])
    elif cmd == "outcome":
        outcome(sys.argv[2])
    elif cmd == "summary":
        summary(sys.argv[2])
    elif cmd == "near":
        sys.exit(0 if near(sys.argv[2], sys.argv[3], float(sys.argv[4])) else 1)
    elif cmd == "expect-draft":
        expect_draft(sys.argv[2], sys.argv[3], sys.argv[4])
    elif cmd == "variants":
        variants(*sys.argv[2:5])
    elif cmd == "verdict":
        verdict(sys.argv[2], sys.argv[3])
    elif cmd == "draft":
        draft(sys.argv[2])
    elif cmd == "mesh":
        mesh(sys.argv[2])
    elif cmd == "tile-binary":
        tile_binary(sys.argv[2], sys.argv[3], int(sys.argv[4]))
    elif cmd == "tile-ascii":
        tile_ascii(sys.argv[2], sys.argv[3], int(sys.argv[4]))
    else:
        raise SystemExit("unknown command %s" % cmd)


main()
PY

cat > "$W/probe/probe.py" <<'PY'
import importlib.machinery
import importlib.util
import math
import os
import sys
import time

import numpy as np


def load(path):
    loader = importlib.machinery.SourceFileLoader("cadmesh_probe", path)
    spec = importlib.util.spec_from_loader("cadmesh_probe", loader)
    mod = importlib.util.module_from_spec(spec)
    loader.exec_module(mod)
    return mod


def count(helper, src):
    h = load(helper)
    vertices, _ = h.stats_read(src)
    if vertices is None:
        print("PROBE,watertight=unreadable,triangles=0,shells=0,positive=0,negative=0,zero=0,rounds=0")
        return
    triangles = len(vertices) // 3
    vol, _ = h.stats_mass(vertices)
    index, nv = h.stats_index(vertices)
    del vertices
    pairs = h.stats_edges(index, nv)
    del index
    if pairs is None:
        print("PROBE,watertight=0,triangles=%d,shells=0,positive=0,negative=0,zero=0,rounds=0" % triangles)
        return
    root, rounds = h.stats_union(triangles, *pairs)
    del pairs
    shells = h.stats_shells(vol, root)
    print("PROBE,watertight=1,triangles=%d,shells=%d,positive=%d,negative=%d,zero=%d,rounds=%d" % (
        triangles, len(shells), int((shells > 0).sum()), int((shells < 0).sum()), int((shells == 0).sum()), rounds))


def union(helper, n, seed):
    h = load(helper)
    a = np.random.default_rng(seed).permutation(n).astype(np.int64)
    b = np.roll(a, -1)
    start = time.monotonic()
    root, rounds = h.stats_union(n, a, b)
    wall = time.monotonic() - start
    roots = int((root == np.arange(n)).sum())
    bound = 2 * math.ceil(math.log2(n)) + 2
    print("PROBE,nodes=%d,seed=%d,roots=%d,rounds=%d,bound=%d,wall_s=%.2f" % (n, seed, roots, rounds, bound, wall))


def compound(variant):
    from OCP.BRep import BRep_Builder
    from OCP.BRepBuilderAPI import BRepBuilderAPI_MakeSolid
    from OCP.BRepPrimAPI import BRepPrimAPI_MakeBox
    from OCP.TopAbs import TopAbs_SHELL
    from OCP.TopExp import TopExp_Explorer
    from OCP.TopoDS import TopoDS, TopoDS_Compound
    from OCP.gp import gp_Pnt

    big = BRepPrimAPI_MakeBox(10.0, 10.0, 10.0).Shape()
    small = BRepPrimAPI_MakeBox(gp_Pnt(20.0, 0.0, 0.0), 5.0, 5.0, 5.0).Shape()
    if variant == "reversed_solid":
        negative = small.Reversed()
    elif variant == "inward_shell":
        shells = TopExp_Explorer(small, TopAbs_SHELL)
        negative = BRepBuilderAPI_MakeSolid(TopoDS.Shell_s(shells.Current().Reversed())).Solid()
    else:
        raise SystemExit("unknown variant %s" % variant)
    builder = BRep_Builder()
    comp = TopoDS_Compound()
    builder.MakeCompound(comp)
    builder.Add(comp, big)
    builder.Add(comp, negative)
    return comp


def solid_summary(helper, variant):
    h = load(helper)
    solids, closed, volume, area, reason = h.solid_summary(compound(variant))
    print("PROBE,solids=%d,closed=%d,volume=%r,area=%r,reason=%s" % (solids, closed, volume, area, reason or "empty"))


def step_build(variant, out):
    from OCP.IFSelect import IFSelect_RetDone
    from OCP.STEPControl import STEPControl_AsIs, STEPControl_Writer

    writer = STEPControl_Writer()
    writer.Transfer(compound(variant), STEPControl_AsIs)
    written = writer.Write(out) == IFSelect_RetDone
    print("PROBE,variant=%s,written=%d,bytes=%d" % (variant, int(written), os.path.getsize(out) if written else 0))


def gen_one_shell(out, n, seed):
    rng = np.random.default_rng(seed)
    i, j = np.meshgrid(np.arange(n, dtype=np.float64), np.arange(n, dtype=np.float64), indexing="ij")
    i, j = i.reshape(-1), j.reshape(-1)
    p00, p10 = np.stack([i, j], 1), np.stack([i + 1, j], 1)
    p11, p01 = np.stack([i + 1, j + 1], 1), np.stack([i, j + 1], 1)
    grid = np.concatenate([np.stack([p00, p10, p11], 1), np.stack([p00, p11, p01], 1)])
    del i, j, p00, p10, p11, p01
    faces = []
    for w, flip in ((float(n), False), (0.0, True)):
        uvw = np.concatenate([grid, np.full(grid.shape[:2] + (1,), w)], axis=2)
        if flip:
            uvw = uvw[:, [0, 2, 1]]
        for perm in ((0, 1, 2), (2, 0, 1), (1, 2, 0)):
            faces.append(uvw[:, :, list(perm)])
    del grid
    tri = np.concatenate(faces)
    del faces
    tri = tri[rng.permutation(len(tri))]
    turn = (np.arange(3)[None, :] + rng.integers(0, 3, len(tri))[:, None]) % 3
    tri = np.take_along_axis(tri, turn[:, :, None], axis=1)
    del turn
    six = float(np.einsum("ij,ij->i", tri[:, 0], np.cross(tri[:, 1], tri[:, 2])).sum())
    if six != 6.0 * n ** 3 or len(tri) != 12 * n * n:
        raise SystemExit("one shell N=%d: 6V %r and %d triangles, want %r and %d" % (n, six, len(tri), 6.0 * n ** 3, 12 * n * n))
    record = np.dtype([("n", "<f4", 3), ("v", "<f4", (3, 3)), ("a", "<u2")])
    rec = np.zeros(len(tri), dtype=record)
    rec["v"] = tri.astype(np.float32)
    del tri
    with open(out, "wb") as fh:
        fh.write(b"octoconv probe one shell".ljust(80, b"\0"))
        fh.write(len(rec).to_bytes(4, "little"))
        fh.write(rec.tobytes())
    print("PROBE,triangles=%d,bytes=%d" % (len(rec), os.path.getsize(out)))


def main():
    cmd = sys.argv[1]
    if cmd == "count":
        count(sys.argv[2], sys.argv[3])
    elif cmd == "union":
        union(sys.argv[2], int(sys.argv[3]), int(sys.argv[4]))
    elif cmd == "solid-summary":
        solid_summary(sys.argv[2], sys.argv[3])
    elif cmd == "step-build":
        step_build(sys.argv[2], sys.argv[3])
    elif cmd == "gen-one-shell":
        gen_one_shell(sys.argv[2], int(sys.argv[3]), int(sys.argv[4]))
    else:
        raise SystemExit("unknown command %s" % cmd)


main()
PY
chmod 644 "$W/probe/probe.py"

tool() { python3 "$W/tool.py" "$@"; }
field() { echo "$2" | tr ',' '\n' | sed -n "s/^$1=//p" | head -n 1; }
wrap_field() { grep '^WRAP,' "$2" | tail -n 1 | tr ',' '\n' | sed -n "s/^$1=//p" | head -n 1; }
sha_of() { shasum -a 256 "$1" | cut -d' ' -f1; }
in_set() { case "$2" in *" $1 "*) return 0 ;; esac; return 1; }

new_job() {
	local d
	d=$(mktemp -d "$W/jobs/job.XXXXXX")
	chmod 777 "$d"
	echo "$d"
}

probe() {
	local job=$1 rc=0
	shift
	docker run --rm "${LIMITS[@]}" -v "$W/probe:/probe:ro" -v "$W/base:/base:ro" -v "$W/shells:/shells:ro" -v "$W/tile:/tile:ro" -v "$job:/job" --entrypoint /opt/venv/bin/python "$IMAGE" -I -B /probe/probe.py "$@" > "$job/probe.out" 2> "$job/probe.err" < /dev/null || rc=$?
	PROBE_RC=$rc
	PROBE_LINE=$(grep '^PROBE,' "$job/probe.out" | tail -n 1 | sed 's/^PROBE,//' || true)
}

judge() {
	local rc=0
	JUDGE_LINE=$("$W/bin/cad-pixel-judge" --size 1280 --background opaque "$1" 2>&1) || rc=$?
	JUDGE_RC=$rc
	JUDGE_LINE=$(printf '%s' "$JUDGE_LINE" | tail -n 1 | sed 's/^GOJUDGE,png=[^,]*,//; s/\([a-z_]*\)=/judge_\1=/g')
}

render_on() {
	local job=$1 src=$2 rc=0
	docker run --rm "${LIMITS[@]}" -v "$job:/job" --entrypoint /usr/local/bin/cad-render "$IMAGE" --view iso --size 1280 --background opaque "$src" /job/out.png > "$job/render.out" 2> "$job/render.err" < /dev/null || rc=$?
	RENDER_RC=$rc
	RENDER_V=$(tool verdict "$job/render.out" render)
	RENDER_STATE=fail
	JUDGE_RC=na
	JUDGE_LINE=judge_png=none
	if [ -s "$job/out.png" ]; then
		judge "$job/out.png"
	fi
	if [ "$rc" = 0 ] && [ "$(field verdict "$RENDER_V")" = ok ] && [ "$(field view "$RENDER_V")" = iso ] &&
		[ "$(field size "$RENDER_V")" = 1280 ] && [ "$(field background "$RENDER_V")" = opaque ] && [ "$JUDGE_RC" = 0 ]; then
		RENDER_STATE=ok
	fi
}

tool variants "$SYN/stl_ascii_box.stl" "$SYN/stl_binary_solid_header.stl" "$W/v"
chmod 644 "$W"/v/*.stl

N_SNIFF=0
for name in "${VARIANTS[@]}" nonfinite; do
	line=$("$W/bin/cad-sniff" -group stl_case -root "$W/v" "$W/v/$name.stl") || fail "SNIFF $name: cad-sniff failed"
	fmt=$(printf '%s' "$line" | cut -f5)
	rej=$(printf '%s' "$line" | cut -f6)
	[ "$fmt" = stl ] && [ "$rej" = - ] || fail "SNIFF $name: format=$fmt rejection=$rej, the intake does not admit it"
	ev "SNIFF,name=$name,bytes=$(printf '%s' "$line" | cut -f4),sha256=$(printf '%s' "$line" | cut -f3),format=$fmt,rejection=$rej,result=ok"
	N_SNIFF=$((N_SNIFF + 1))
done

N_BASE=0
for name in "${VARIANTS[@]}"; do
	f="$W/v/$name.stl"
	job=$(new_job)
	rc=0
	docker run --rm "${LIMITS[@]}" -v "$job:/job" -v "$f:/job/in.stl:ro" -v "$W/base:/base:ro" --entrypoint /opt/venv/bin/python "$IMAGE" -I -B /base/cad-mesh stats --measure /job/draft.json /job/in.stl > "$job/old.out" 2> "$job/old.err" < /dev/null || rc=$?
	OV=$(tool verdict "$job/old.out" mesh)
	old="$(field verdict "$OV")/$(field reason "$OV")"
	if in_set "$name" "$OLD_STATS_OK"; then old_want=ok/none; else old_want=unreadable/stl_malformed; fi
	rm -rf "$job"
	job=$(new_job)
	cp "$f" "$job/in.stl"
	chmod 644 "$job/in.stl"
	render_on "$job" /job/in.stl
	if in_set "$name" "$RENDER_OK"; then render_want=ok; else render_want=fail; fi
	row="BASELINE,name=$name,old_stats_rc=$rc,old_stats=$old,old_expected=$old_want,direct_render_rc=$RENDER_RC,direct_render_verdict=$(field verdict "$RENDER_V")/$(field reason "$RENDER_V"),judge_rc=$JUDGE_RC,direct_render=$RENDER_STATE,direct_render_expected=$render_want"
	rm -rf "$job"
	[ "$old" = "$old_want" ] || fail "BASELINE $name: old stats cell is $old, want $old_want ($row)"
	[ "$RENDER_STATE" = "$render_want" ] || fail "BASELINE $name: direct render cell is $RENDER_STATE, want $render_want ($row)"
	ev "$row,result=ok"
	N_BASE=$((N_BASE + 1))
done

N_CASE=0
LOWER_DRAFT=""
LOWER_MESH=""
LOWER_PNG=""
for name in "${VARIANTS[@]}"; do
	f="$W/v/$name.stl"
	job=$(new_job)
	rc=0
	docker run --rm "${LIMITS[@]}" -v "$job:/job" -v "$f:/job/in.stl:ro" --entrypoint /usr/local/bin/cad-mesh "$IMAGE" stats --measure /job/draft.json /job/in.stl /job/mesh.stl > "$job/stats.out" 2> "$job/stats.err" < /dev/null || rc=$?
	SV=$(tool verdict "$job/stats.out" mesh)
	[ "$rc" = 0 ] && [ "$(field verdict "$SV")" = ok ] && [ "$(field triangles "$SV")" = 12 ] ||
		fail "CASE $name: stats rc=$rc $SV: $(tail -n 3 "$job/stats.err" | tr '\n' ' ')"
	[ -f "$job/draft.json" ] && [ -f "$job/mesh.stl" ] || fail "CASE $name: stats wrote no draft or no mesh.stl"
	D=$(tool draft "$job/draft.json")
	M=$(tool mesh "$job/mesh.stl")
	[ "$(field mesh_ok "$M")" = 1 ] && [ "$(field mesh_bytes "$M")" = 684 ] && [ "$(field mesh_count "$M")" = 12 ] ||
		fail "CASE $name: mesh.stl is not a normalized 12-record binary STL: $M"
	MSHA=$(sha_of "$job/mesh.stl")
	if [ "$name" = lower ]; then
		[ "$D" = "draft_triangles=12,bbox=0.0:0.0:0.0-10.0:20.0:30.0,volume=6000.0,area=2200.0,null_reason=empty" ] ||
			fail "CASE lower: the reference draft is $D, want 12 triangles, [0,0,0]-[10,20,30], 6000, 2200"
		LOWER_DRAFT=$D
		LOWER_MESH=$MSHA
	fi
	[ "$D" = "$LOWER_DRAFT" ] || fail "CASE $name: draft $D differs from lower $LOWER_DRAFT"
	MEQ=0
	[ "$MSHA" = "$LOWER_MESH" ] && MEQ=1
	if [ "$name" != binary_solid_header ] && [ "$MEQ" != 1 ]; then
		fail "CASE $name: mesh.stl sha256 $MSHA differs from lower $LOWER_MESH"
	fi
	render_on "$job" /job/mesh.stl
	[ "$RENDER_STATE" = ok ] || fail "CASE $name: the render of mesh.stl is $RENDER_STATE: rc=$RENDER_RC $RENDER_V judge_rc=$JUDGE_RC $JUDGE_LINE"
	PSHA=$(sha_of "$job/out.png")
	[ "$name" = lower ] && LOWER_PNG=$PSHA
	PEQ=0
	[ "$PSHA" = "$LOWER_PNG" ] && PEQ=1
	ev "CASE,name=$name,stats_rc=$rc,verdict=$(field verdict "$SV"),triangles=$(field triangles "$SV"),$D,$M,mesh_sha256=$MSHA,mesh_equals_lower=$MEQ,render_rc=$RENDER_RC,render_verdict=$(field verdict "$RENDER_V"),view=$(field view "$RENDER_V"),size=$(field size "$RENDER_V"),background=$(field background "$RENDER_V"),judge_rc=$JUDGE_RC,$JUDGE_LINE,png_sha256=$PSHA,png_equals_lower=$PEQ,result=ok"
	N_CASE=$((N_CASE + 1))
	rm -rf "$job"
done

N_NONFINITE=0
job=$(new_job)
rc=0
docker run --rm "${LIMITS[@]}" -v "$job:/job" -v "$W/v/nonfinite.stl:/job/in.stl:ro" --entrypoint /usr/local/bin/cad-mesh "$IMAGE" stats --measure /job/draft.json /job/in.stl /job/mesh.stl > "$job/stats.out" 2> "$job/stats.err" < /dev/null || rc=$?
NV=$(tool verdict "$job/stats.out" mesh)
MESH_LEFT=0
[ -e "$job/mesh.stl" ] && MESH_LEFT=1
row="NONFINITE,name=nonfinite,coordinate=1.000000e+39,stats_rc=$rc,verdict=$(field verdict "$NV"),reason=$(field reason "$NV"),mesh_stl_exists=$MESH_LEFT"
rm -rf "$job"
[ "$rc" = 2 ] && [ "$(field verdict "$NV")" = unreadable ] && [ "$(field reason "$NV")" = stl_malformed ] && [ "$MESH_LEFT" = 0 ] ||
	fail "NONFINITE: want exit 2, unreadable/stl_malformed and no mesh.stl ($row)"
ev "$row,result=ok"
N_NONFINITE=1

SHELL_CASES="cube_out|12|1000.0/600.0|1000.0/600.0|1|1|0|0
two_out|24|2000.0/1200.0|2000.0/1200.0|2|2|0|0
cube_in|12|negative_volume|negative_volume|1|0|1|0
two_in|24|negative_volume|negative_volume|2|0|2|0
out_in_disjoint|24|stl_ambiguous_shells|875.0/750.0|2|1|1|0
hollow|24|stl_ambiguous_shells|936.0/696.0|2|1|1|0
corner_touch_mixed|24|stl_ambiguous_shells|875.0/750.0|2|1|1|0
corner_touch_out|24|1125.0/750.0|1125.0/750.0|2|2|0|0
zero_shell|14|invalid_solid|1000.0/700.0|2|1|0|1"
tool shells "$W/shells"
chmod 644 "$W"/shells/*.stl

N_SHELLS=0
while IFS='|' read -r name tri want_new want_old want_sh want_pos want_neg want_zero; do
	f="$W/shells/$name.stl"
	sline=$("$W/bin/cad-sniff" -group stl_case -root "$W/shells" "$f") || fail "SHELLS $name: cad-sniff failed"
	sniff="$(printf '%s' "$sline" | cut -f5)/$(printf '%s' "$sline" | cut -f6)"
	[ "$sniff" = stl/- ] || fail "SHELLS $name: sniff is $sniff, want stl/-"
	job=$(new_job)
	rc_old=0
	docker run --rm "${LIMITS[@]}" -v "$job:/job" -v "$f:/job/in.stl:ro" -v "$W/base:/base:ro" --entrypoint /opt/venv/bin/python "$IMAGE" -I -B /base/cad-mesh-shells stats --measure /job/draft.json /job/in.stl /job/mesh.stl > "$job/old.out" 2> "$job/old.err" < /dev/null || rc_old=$?
	OV=$(tool verdict "$job/old.out" mesh)
	old=$(tool outcome "$job/draft.json")
	rm -rf "$job"
	[ "$rc_old" = 0 ] && [ "$(field verdict "$OV")" = ok ] || fail "SHELLS $name: the ac81afe helper rc=$rc_old $OV"
	[ "$old" = "$want_old" ] || fail "SHELLS $name: old_stats cell is $old, want $want_old"
	job=$(new_job)
	rc=0
	docker run --rm "${LIMITS[@]}" -v "$job:/job" -v "$f:/job/in.stl:ro" --entrypoint /usr/local/bin/cad-mesh "$IMAGE" stats --measure /job/draft.json /job/in.stl /job/mesh.stl > "$job/stats.out" 2> "$job/stats.err" < /dev/null || rc=$?
	SV=$(tool verdict "$job/stats.out" mesh)
	new=$(tool outcome "$job/draft.json")
	D=$(tool draft "$job/draft.json" 2>/dev/null || echo "volume=na,area=na,null_reason=na")
	mesh_bytes=0
	[ -f "$job/mesh.stl" ] && mesh_bytes=$(wc -c < "$job/mesh.stl" | tr -d ' ')
	rm -rf "$job"
	[ "$rc" = 0 ] && [ "$(field verdict "$SV")" = ok ] && [ "$(field triangles "$SV")" = "$tri" ] ||
		fail "SHELLS $name: stats rc=$rc $SV, want rc 0, ok and $tri triangles"
	[ "$new" = "$want_new" ] || fail "SHELLS $name: the new draft cell is $new, want $want_new ($D)"
	[ "$mesh_bytes" = $((84 + 50 * tri)) ] || fail "SHELLS $name: mesh.stl is $mesh_bytes bytes, want $((84 + 50 * tri))"
	job=$(new_job)
	probe "$job" count /usr/local/bin/cad-mesh "/shells/$name.stl"
	rm -rf "$job"
	[ "$PROBE_RC" = 0 ] && [ -n "$PROBE_LINE" ] || fail "SHELLS $name: probe count rc=$PROBE_RC"
	got_sh="$(field shells "$PROBE_LINE")|$(field positive "$PROBE_LINE")|$(field negative "$PROBE_LINE")|$(field zero "$PROBE_LINE")"
	[ "$(field watertight "$PROBE_LINE")" = 1 ] && [ "$got_sh" = "$want_sh|$want_pos|$want_neg|$want_zero" ] ||
		fail "SHELLS $name: shells/positive/negative/zero cell is $got_sh, want $want_sh|$want_pos|$want_neg|$want_zero ($PROBE_LINE)"
	ev "SHELLS,name=$name,triangles=$tri,sniff=$sniff,old_stats=$old,old_expected=$want_old,stats_rc=$rc,verdict=$(field verdict "$SV"),volume=$(field volume "$D"),area=$(field area "$D"),null_reason=$(field null_reason "$D"),expected=$want_new,mesh_bytes=$mesh_bytes,shells=$(field shells "$PROBE_LINE"),positive=$(field positive "$PROBE_LINE"),negative=$(field negative "$PROBE_LINE"),zero=$(field zero "$PROBE_LINE"),rounds=$(field rounds "$PROBE_LINE"),result=ok"
	N_SHELLS=$((N_SHELLS + 1))
done <<EOF
$SHELL_CASES
EOF
[ "$N_SHELLS" = 9 ] || fail "SHELLS ran $N_SHELLS fixtures, want 9"

N_UNION=0
job=$(new_job)
probe "$job" union /usr/local/bin/cad-mesh "$UNION_NODES" "$UNION_SEED"
rm -rf "$job"
[ "$PROBE_RC" = 0 ] && [ -n "$PROBE_LINE" ] || fail "UNION: probe rc=$PROBE_RC"
u_roots=$(field roots "$PROBE_LINE")
u_rounds=$(field rounds "$PROBE_LINE")
u_bound=$(field bound "$PROBE_LINE")
row="UNION,name=cycle_random,nodes=$(field nodes "$PROBE_LINE"),seed=$(field seed "$PROBE_LINE"),roots=$u_roots,rounds=$u_rounds,bound=$u_bound,wall_s=$(field wall_s "$PROBE_LINE")"
[ "$(field nodes "$PROBE_LINE")" = "$UNION_NODES" ] && [ "$u_roots" = 1 ] && [ "$u_rounds" -le "$u_bound" ] ||
	fail "UNION: want one root within the round bound ($row)"
ev "$row,result=ok"
N_UNION=1

N_STEP=0
STEP_REACHED=0
for variant in reversed_solid inward_shell; do
	job=$(new_job)
	probe "$job" solid-summary /usr/local/bin/cad-mesh "$variant"
	[ "$PROBE_RC" = 0 ] && [ -n "$PROBE_LINE" ] || fail "STEP_SIGN $variant memory: new probe rc=$PROBE_RC $(tail -n 3 "$job/probe.err" | tr '\n' ' ')"
	P_NEW=$PROBE_LINE
	probe "$job" solid-summary /base/cad-mesh-shells "$variant"
	[ "$PROBE_RC" = 0 ] && [ -n "$PROBE_LINE" ] || fail "STEP_SIGN $variant memory: old probe rc=$PROBE_RC $(tail -n 3 "$job/probe.err" | tr '\n' ' ')"
	P_OLD=$PROBE_LINE
	rm -rf "$job"
	new="$(field solids "$P_NEW"):$(field closed "$P_NEW"):$(field volume "$P_NEW"):$(field area "$P_NEW"):$(field reason "$P_NEW")"
	old="$(field solids "$P_OLD"):$(field closed "$P_OLD"):$(field volume "$P_OLD"):$(field area "$P_OLD"):$(field reason "$P_OLD")"
	if [ "$(field closed "$P_NEW")" = 2 ]; then
		[ "$(field volume "$P_NEW")" = None ] && [ "$(field reason "$P_NEW")" = negative_volume ] ||
			fail "STEP_SIGN $variant memory: two closed solids, one negative, and the new helper gives $new, want negative_volume"
		tool near "$(field volume "$P_OLD")" 875 "$STEP_RTOL" && [ "$(field reason "$P_OLD")" = empty ] ||
			fail "STEP_SIGN $variant memory: the ac81afe helper gives $old, want about 875 published (the teeth)"
		ev "STEP_SIGN,variant=$variant,path=memory,new=$new,old=$old,outcome=negative_volume,branch_reached=1,result=ok"
		STEP_REACHED=$((STEP_REACHED + 1))
	else
		ev "STEP_SIGN,variant=$variant,path=memory,new=$new,old=$old,outcome=closed_$(field closed "$P_NEW"),branch_reached=0,result=recorded"
	fi
	N_STEP=$((N_STEP + 1))
	job=$(new_job)
	probe "$job" step-build "$variant" /job/in.step
	[ "$PROBE_RC" = 0 ] && [ "$(field written "$PROBE_LINE")" = 1 ] || fail "STEP_SIGN $variant file: step-build rc=$PROBE_RC $PROBE_LINE"
	rc_old=0
	docker run --rm "${LIMITS[@]}" -v "$job:/job" -v "$W/base:/base:ro" --entrypoint /opt/venv/bin/python "$IMAGE" -I -B /base/cad-mesh-shells --format step --quality standard --measure /job/draft.json /job/in.step /job/out.stl > "$job/old.out" 2> "$job/old.err" < /dev/null || rc_old=$?
	S_OLD=$(tool summary "$job/draft.json")
	rm -f "$job/draft.json" "$job/out.stl"
	rc=0
	docker run --rm "${LIMITS[@]}" -v "$job:/job" --entrypoint /usr/local/bin/cad-mesh "$IMAGE" --format step --quality standard --measure /job/draft.json /job/in.step /job/out.stl > "$job/new.out" 2> "$job/new.err" < /dev/null || rc=$?
	S_NEW=$(tool summary "$job/draft.json")
	NV=$(tool verdict "$job/new.out" mesh)
	rm -rf "$job"
	new="$(field solids "$S_NEW"):$(field closed "$S_NEW"):$(field volume "$S_NEW"):$(field area "$S_NEW"):$(field reason "$S_NEW")"
	old="$(field solids "$S_OLD"):$(field closed "$S_OLD"):$(field volume "$S_OLD"):$(field area "$S_OLD"):$(field reason "$S_OLD")"
	[ "$rc" = 0 ] && [ "$(field verdict "$NV")" = ok ] || fail "STEP_SIGN $variant file: new helper rc=$rc $NV"
	if tool near "$(field volume "$S_NEW")" 875 "$STEP_RTOL"; then
		fail "STEP_SIGN $variant file: the new helper publishes about 875 ($new)"
	elif [ "$(field volume "$S_NEW")" = None ] && [ "$(field reason "$S_NEW")" = negative_volume ]; then
		cls=negative_volume
		reached=1
	elif [ "$(field closed "$S_NEW")" = 2 ] && [ "$(field reason "$S_NEW")" = empty ] && tool near "$(field volume "$S_NEW")" 1125 "$STEP_RTOL"; then
		cls=repaired
		reached=0
	elif [ "$(field closed "$S_NEW")" = 1 ] && [ "$(field reason "$S_NEW")" = empty ] && tool near "$(field volume "$S_NEW")" 1000 "$STEP_RTOL"; then
		cls=partial
		reached=0
	else
		fail "STEP_SIGN $variant file: the new helper gives $new, which is none of negative_volume, repaired or partial"
	fi
	ev "STEP_SIGN,variant=$variant,path=file,new=$new,old=$old,old_rc=$rc_old,outcome=$cls,branch_reached=$reached,result=ok"
	N_STEP=$((N_STEP + 1))
done
[ "$STEP_REACHED" -ge 1 ] || fail "STEP_SIGN: the negative-solid branch was not reached"

CEILING=$(sed -n 's/^DERIVED,key=cad_stl_ceiling_bytes,value=\([0-9]*\),.*/\1/p' "$WAVE0")
[ "$CEILING" = 190840832 ] || fail "cad_stl_ceiling_bytes in $WAVE0 is '$CEILING', want 190840832"

N_RSS=0
for kind in binary ascii_upper binary_one_shell; do
	disk_floor "stats_rss_$kind"
	tile="$W/tile/$kind.stl"
	tile_sha=na
	if [ "$kind" = binary ]; then
		expect=$(tool tile-binary "$SYN/stl_binary_solid_header.stl" "$tile" "$CEILING")
	elif [ "$kind" = ascii_upper ]; then
		expect=$(tool tile-ascii "$SYN/stl_binary_solid_header.stl" "$tile" "$CEILING")
	else
		job=$(new_job)
		probe "$job" gen-one-shell /job/one.stl "$ONE_SHELL_N" "$ONE_SHELL_SEED"
		[ "$PROBE_RC" = 0 ] && [ -f "$job/one.stl" ] || fail "STATS_RSS $kind: gen-one-shell rc=$PROBE_RC $(tail -n 3 "$job/probe.err" | tr '\n' ' ')"
		expect=$(field triangles "$PROBE_LINE")
		mv "$job/one.stl" "$tile"
		rm -rf "$job"
		[ "$expect" = $((12 * ONE_SHELL_N * ONE_SHELL_N)) ] || fail "STATS_RSS $kind: the one shell has $expect triangles, want $((12 * ONE_SHELL_N * ONE_SHELL_N))"
		tile_sha=$(sha_of "$tile")
	fi
	if [ "$kind" = binary_one_shell ]; then
		shells_expected=1
		vol_expected=$((ONE_SHELL_N * ONE_SHELL_N * ONE_SHELL_N))
		area_expected=$((6 * ONE_SHELL_N * ONE_SHELL_N))
	else
		[ $((expect % 12)) = 0 ] || fail "STATS_RSS $kind: $expect triangles is not a whole number of 12-triangle boxes"
		shells_expected=$((expect / 12))
		vol_expected=$((shells_expected * 6000))
		area_expected=$((shells_expected * 2200))
	fi
	chmod 644 "$tile"
	bytes=$(wc -c < "$tile" | tr -d ' ')
	[ "$bytes" -le "$CEILING" ] || fail "STATS_RSS $kind: the tile is $bytes bytes, above the $CEILING ceiling"
	blocks=$(((bytes + ASCII_BLOCK - 1) / ASCII_BLOCK))
	sline=$("$W/bin/cad-sniff" -group stl_case -root "$W/tile" "$tile") || fail "STATS_RSS $kind: cad-sniff failed"
	[ "$(printf '%s' "$sline" | cut -f5)" = stl ] && [ "$(printf '%s' "$sline" | cut -f6)" = - ] ||
		fail "STATS_RSS $kind: the tile does not sniff as an admitted stl: $(printf '%s' "$sline" | cut -f5-6 | tr '\t' ' ')"
	job=$(new_job)
	wrapout="$W/wrap.out"
	rc_w=0
	docker run --rm "${LIMITS[@]}" -v "$W/wrap:/wrap:ro" -v "$job:/job" -v "$tile:/job/in.stl:ro" --entrypoint /opt/venv/bin/python "$IMAGE" -I -B /wrap/measure_wrap.py --budget-s 400 --outdir /job -- /usr/local/bin/cad-mesh stats --measure /job/draft.json /job/in.stl /job/mesh.stl > "$wrapout" 2> "$W/wrap.err" < /dev/null || rc_w=$?
	rc=$(wrap_field rc "$wrapout")
	wall=$(wrap_field wall_s "$wrapout")
	rss=$(wrap_field child_rss_mib "$wrapout")
	peak=$(wrap_field cg_peak_mib "$wrapout")
	oom=$(wrap_field oom_kill "$wrapout")
	tmo=$(wrap_field timeout "$wrapout")
	V=$(tool verdict "$job/child.log" mesh)
	mesh_bytes=0
	[ -f "$job/mesh.stl" ] && mesh_bytes=$(wc -c < "$job/mesh.stl" | tr -d ' ')
	res=ok
	[ "$rc_w" = 0 ] && [ "$rc" = 0 ] && [ "$oom" = 0 ] && [ "$tmo" = 0 ] || res=fail
	[ "$(field verdict "$V")" = ok ] && [ "$(field triangles "$V")" = "$expect" ] || res=fail
	[ "$mesh_bytes" = $((84 + 50 * expect)) ] || res=fail
	python3 -c "import sys; sys.exit(0 if max(float(sys.argv[1] or 99999), float(sys.argv[2] or 99999)) <= $LIMIT_MIB else 1)" "$rss" "$peak" || res=fail
	DX="volume=na,volume_expected=$vol_expected,area=na,area_expected=$area_expected,draft_null_reason=na,draft_ok=0"
	[ -f "$job/draft.json" ] && DX=$(tool expect-draft "$job/draft.json" "$vol_expected" "$area_expected")
	[ "$(field draft_ok "$DX")" = 1 ] || res=fail
	rm -rf "$job"
	job=$(new_job)
	probe "$job" count /usr/local/bin/cad-mesh "/tile/$kind.stl"
	rm -rf "$job"
	shells_counted=$(field shells "$PROBE_LINE")
	union_rounds=$(field rounds "$PROBE_LINE")
	[ "$PROBE_RC" = 0 ] && [ "$(field watertight "$PROBE_LINE")" = 1 ] && [ "$shells_counted" = "$shells_expected" ] &&
		[ "$(field positive "$PROBE_LINE")" = "$shells_expected" ] || res=fail
	share=ok
	python3 -c "import sys; sys.exit(0 if max(float(sys.argv[1] or 99999), float(sys.argv[2] or 99999)) <= $RSS_SHARE_MIB and float(sys.argv[3] or 99999) <= $WALL_SHARE_S else 1)" "$rss" "$peak" "$wall" || share=budget_share_exceeded
	[ "$share" = ok ] || res=fail
	row="STATS_RSS,kind=$kind,bytes=$bytes,ceiling_bytes=$CEILING,tile_sha256=$tile_sha,triangles_expected=$expect,blocks=$blocks,rc=$rc,wall_s=$wall,wall_limit_s=$WALL_SHARE_S,child_rss_mib=$rss,cg_peak_mib=$peak,rss_share_limit_mib=$RSS_SHARE_MIB,limit_mib=$LIMIT_MIB,oom_kill=$oom,verdict=$(field verdict "$V"),triangles=$(field triangles "$V"),mesh_bytes=$mesh_bytes,shells_expected=$shells_expected,shells_counted=$shells_counted,union_rounds=$union_rounds,$DX,result=$res"
	rm -f "$tile"
	[ "$share" = ok ] || {
		ev "$row"
		fail "budget_share_exceeded kind=$kind child_rss_mib=$rss cg_peak_mib=$peak wall_s=$wall rss_share_limit_mib=$RSS_SHARE_MIB wall_limit_s=$WALL_SHARE_S"
	}
	[ "$res" = ok ] || {
		ev "$row"
		fail "STATS_RSS $kind: $PROBE_LINE $(tail -n 5 "$W/wrap.err" | tr '\n' ' ')"
	}
	ev "$row"
	N_RSS=$((N_RSS + 1))
done

ev "SUMMARY,result=ALL PASS,sniff=$N_SNIFF,baseline=$N_BASE,case=$N_CASE,nonfinite=$N_NONFINITE,shells=$N_SHELLS,union=$N_UNION,step_sign=$N_STEP,stats_rss=$N_RSS"

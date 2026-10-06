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
stl_malformed with no mesh.stl) and STATS_RSS (stats peak memory at the binary ceiling and
at the largest uppercase ASCII STL under it). One evidence line per check goes to FILE
(relative to the repository root, default
.planning/phases/74-3d-render-measurements/evidence/74-08-stl-case.txt).
USAGE
}

EVID=".planning/phases/74-3d-render-measurements/evidence/74-08-stl-case.txt"
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
IMAGE=octoconv-cad-worker:latest
SYN=internal/convert/testdata/cad/synthetic
WAVE0=.planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt
WRAP_PY=scripts/cad-measure/measure_wrap.py
LIMIT_MIB=2867.2
FLOOR_KIB=$((6 * 1048576))
ASCII_BLOCK=33554432
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2)
VARIANTS=(lower upper vertex_upper mixed_body header_upper header_title lower_header_upper_rest leading_ws binary_solid_header)
OLD_STATS_OK=" lower header_upper header_title leading_ws binary_solid_header "
RENDER_OK=" lower vertex_upper mixed_body lower_header_upper_rest leading_ws binary_solid_header "
export GOTOOLCHAIN=go1.26.4

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
mkdir -p "$W/bin" "$W/base" "$W/v" "$W/jobs" "$W/wrap" "$W/tile"
chmod 755 "$W/base" "$W/v" "$W/wrap" "$W/tile"
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
ev "HDR,limits=$(echo "${LIMITS[*]}" | tr ' ' ';')"

go build -o "$W/bin/cad-sniff" ./scripts/cmd/cad-sniff || fail "cannot build cad-sniff"
go build -o "$W/bin/cad-pixel-judge" ./scripts/cmd/cad-pixel-judge || fail "cannot build cad-pixel-judge"
git show "$BASE_REV:cmd/cad-worker/helper/cad-mesh" > "$W/base/cad-mesh"
chmod 755 "$W/base/cad-mesh"
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


def main():
    cmd = sys.argv[1]
    if cmd == "variants":
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

CEILING=$(sed -n 's/^DERIVED,key=cad_stl_ceiling_bytes,value=\([0-9]*\),.*/\1/p' "$WAVE0")
[ "$CEILING" = 190840832 ] || fail "cad_stl_ceiling_bytes in $WAVE0 is '$CEILING', want 190840832"

N_RSS=0
for kind in binary ascii_upper; do
	disk_floor "stats_rss_$kind"
	tile="$W/tile/$kind.stl"
	if [ "$kind" = binary ]; then
		expect=$(tool tile-binary "$SYN/stl_binary_solid_header.stl" "$tile" "$CEILING")
	else
		expect=$(tool tile-ascii "$SYN/stl_binary_solid_header.stl" "$tile" "$CEILING")
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
	row="STATS_RSS,kind=$kind,bytes=$bytes,ceiling_bytes=$CEILING,triangles_expected=$expect,blocks=$blocks,rc=$rc,wall_s=$wall,child_rss_mib=$rss,cg_peak_mib=$peak,limit_mib=$LIMIT_MIB,oom_kill=$oom,verdict=$(field verdict "$V"),triangles=$(field triangles "$V"),mesh_bytes=$mesh_bytes,result=$res"
	rm -f "$tile"
	rm -rf "$job"
	[ "$res" = ok ] || {
		ev "$row"
		fail "STATS_RSS $kind: $(tail -n 5 "$W/wrap.err" | tr '\n' ' ')"
	}
	ev "$row"
	N_RSS=$((N_RSS + 1))
done

ev "SUMMARY,result=ALL PASS,sniff=$N_SNIFF,baseline=$N_BASE,case=$N_CASE,nonfinite=$N_NONFINITE,stats_rss=$N_RSS"

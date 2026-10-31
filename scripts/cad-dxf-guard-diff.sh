#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

PHASE_DIR="$REPO_ROOT/.planning/milestones/v1.16-phases/75-dxf-2d-pipeline"
EVID="$PHASE_DIR/evidence/75-03-guard.txt"
EVID_NAME="evidence/75-03-guard.txt"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
DOCKERFILE="$REPO_ROOT/Dockerfile.cad-worker"
CAD_CACHE="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
WRAP_PY="$REPO_ROOT/scripts/cad-measure/measure_wrap.py"
GEN_PY="$REPO_ROOT/scripts/cad-corpus-gen.py"
GO_TOOLCHAIN=go1.27.3
MIN_FREE_GIB=6
CAP_INSERTS=170031
CAP_ENTITIES=850155
CAP_NESTING=16
CAP_ABS=1e12
H_DEADLINE_S=30
MAX_DRAWN=850155
BUDGET_MIB=2842.3
COST_WALL_S=20
COST_REPS=2
CEILING_MIB=118
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
STAGE_ENV=(-e HOME=/job/.home -e XDG_CACHE_HOME=/job/.home/.cache -e XDG_CONFIG_HOME=/job/.home/.config -e TMPDIR=/job)
GUARD_ARGS=(--format dxf --max-expanded-inserts "$CAP_INSERTS" --max-expanded-entities "$CAP_ENTITIES"
  --max-nesting "$CAP_NESTING" --max-abs-coordinate "$CAP_ABS")
HELPERS=(cad-mesh cad-guard cad-render cad-dxf)
CORPUS_FILES=(public/3dsolids public/acad_table_with_blk_ref public/columns_R2007 public/dimension_in_nested_blocks
  public/hatches_1 public/hatches_2 public/houses_of_parliament_georeferenced public/uncommon
  synthetic/blocks_minsert synthetic/comments_999 synthetic/legacy_cp1251 synthetic/legacy_latin1)
REPLICATES=(2MiB 5MiB 10MiB 20MiB 50MiB)
STRESS_SHAPES=(chain fanout blocks wide)

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/cad-dxf-guard-diff.sh diff [--out FILE]
  scripts/cad-dxf-guard-diff.sh cost [--out FILE]

diff  build the engine image of Dockerfile.cad-worker, generate the key, structure, depth, cap and
      numeric variants (ezdxf where structure matters, byte pairs where bytes matter), add every
      committed DXF fixture, reject file and corpus file, and run each through three readers: G, the
      cad-guard DXF mode; A, ezdxf.readfile plus audit; H, the production cad-dxf without a guard,
      confined by its candidate Landlock set with a 30 s deadline. Every row is judged by the
      pre-registered rules P1-P3 against expectations and policy declarations fixed in this script
      before any run. Also writes the R-8 SCOPE rows and the DERIVED nesting bound (4 x the guard
      nesting maximum over the 12 corpus files and the five cached replicates).
cost  generate the 118 MiB guard stress files (chain, fanout, blocks, wide) and the 118 MiB
      replicate of uncommon.dxf into the cache, and run cad-guard on each twice inside the image
      under --cpus 2 --memory 4g --memory-swap 4g --network none --user nobody, wrapped by
      measure_wrap.py. Each wall must be at most 20 s and each peak at most 2842.3 MiB.

Each mode rewrites its own lines in the evidence file (default
.planning/milestones/v1.16-phases/75-dxf-2d-pipeline/evidence/75-03-guard.txt) and removes the image tags it built.
Exit codes: 64 usage, 69 a tool is missing, 3 VM disk below 6 GiB, 1 a check failed or a STOP.
USAGE
  exit 64
}

fail() { echo "FAIL: $*" >&2; exit 1; }

need_tool() { command -v "$1" >/dev/null 2>&1 || { echo "error: $1 is required" >&2; exit 69; }; }

sha_of() { shasum -a 256 "$1" | awk '{print $1}'; }

vm_free_gib() { docker run --rm alpine df -k / | awk 'NR == 2 { print int($4 / 1048576) }'; }

disk_guard() {
  local free
  free=$(vm_free_gib)
  if [ -z "$free" ] || [ "$free" -lt "$MIN_FREE_GIB" ]; then
    echo "STOP,reason=disk_below_threshold,free_gib=${free:-unknown},threshold_gib=$MIN_FREE_GIB" >&2
    exit 3
  fi
}

triplet_for() {
  case "$1" in
    arm64) echo aarch64-linux-gnu ;;
    amd64) echo x86_64-linux-gnu ;;
    *) echo "error: no multiarch triplet for $1" >&2; return 1 ;;
  esac
}

inputs_short() {
  {
    shasum -a 256 "$DOCKERFILE" "$REPO_ROOT"/cmd/cad-worker/requirements-*.txt
    for h in "${HELPERS[@]}"; do shasum -a 256 "$REPO_ROOT/cmd/cad-worker/helper/$h"; done
  } | awk '{print $1}' | shasum -a 256 | cut -c1-12
}

BUILT_TAGS=()
WORK=""

cleanup() {
  local t
  if [ "${#BUILT_TAGS[@]}" -gt 0 ]; then
    for t in "${BUILT_TAGS[@]}"; do docker rmi "$t" >/dev/null 2>&1 || true; done
  fi
  [ -z "$WORK" ] || rm -rf "$WORK"
}

build_engine() {
  local tag stage h
  tag="octoconv-cad-worker:engine75-$(inputs_short)"
  stage=$(mktemp -d "${TMPDIR:-/tmp}/cad-guard-build.XXXXXX")
  mkdir -p "$stage/cmd/cad-worker/helper"
  cp "$DOCKERFILE" "$stage/Dockerfile.cad-worker"
  cp "$REPO_ROOT"/cmd/cad-worker/requirements-*.txt "$REPO_ROOT"/cmd/cad-worker/vtk_keeplist.py \
    "$REPO_ROOT"/cmd/cad-worker/vtk-keeplist.txt "$stage/cmd/cad-worker/"
  for h in "$REPO_ROOT"/cmd/cad-worker/helper/*; do
    [ ! -f "$h" ] || cp "$h" "$stage/cmd/cad-worker/helper/"
  done
  cad_log "building $tag"
  if ! docker build -f "$stage/Dockerfile.cad-worker" --target engine -t "$tag" "$stage" >&2; then
    rm -rf "$stage"
    cad_log "error: image build failed"
    return 1
  fi
  rm -rf "$stage"
  printf '%s\n' "$tag"
}

build_probe() {
  local arch=$1 dir=$2
  mkdir -p "$dir"
  chmod 0755 "$dir"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$dir/sandbox-probe" ./scripts/cmd/sandbox-probe
  chmod 0755 "$dir/sandbox-probe"
}

new_job() {
  local d
  d=$(mktemp -d "$WORK/job.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

probe_refused() {
  grep -aqE 'the ruleset confines nothing|is not readable before the ruleset|not a permission denial|^apply:|^lookpath:|^exec:' "$1"
}

tracked_dirt() {
  git status --porcelain --untracked-files=no | awk '{print $2}' | grep -vc "$EVID_NAME" || true
}

evid_put() {
  local lines=$1 pattern=$2 keep
  mkdir -p "$(dirname "$EVID")"
  keep=$(mktemp "${TMPDIR:-/tmp}/cad-guard-keep.XXXXXX")
  if [ -f "$EVID" ]; then grep -vE "$pattern" "$EVID" > "$keep" || true; fi
  { cat "$keep" "$lines"; } > "$EVID"
  rm -f "$keep"
}

setup() {
  local mode=$1
  need_tool docker
  need_tool go
  need_tool python3
  [ "$(docker version --format '{{.Server.Arch}}')" = arm64 ] || fail "docker server is not arm64"
  [ "$(GOTOOLCHAIN=$GO_TOOLCHAIN go env GOVERSION)" = "$GO_TOOLCHAIN" ] || fail "go toolchain is not $GO_TOOLCHAIN"
  disk_guard
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-guard-diff.XXXXXX")
  chmod 0755 "$WORK"
  trap cleanup EXIT
  TAG=$(build_engine)
  BUILT_TAGS+=("$TAG")
  cad_require_native "$TAG" || fail "engine image is not native"
  ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
  TRIPLET=$(triplet_for "$ARCH")
  DXF_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/share/fonts:/usr/lib/$TRIPLET:/usr/local/bin/cad-dxf"
  mkdir -p "$WORK/wrap" "$WORK/tools"
  chmod 0755 "$WORK/wrap" "$WORK/tools"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
  chmod 0644 "$WORK/wrap/measure_wrap.py"
  LINES="$WORK/lines.txt"
  : > "$LINES"
  {
    printf 'HDR,mode=%s,date_utc=%s\n' "$mode" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'HDR,mode=%s,commit=%s,tracked_dirt=%s\n' "$mode" "$(git rev-parse HEAD)" "$(tracked_dirt)"
    printf 'HDR,mode=%s,image=%s,image_id=%s\n' "$mode" "$TAG" "$(docker image inspect "$TAG" --format '{{.Id}}')"
    printf 'HDR,mode=%s,guard_sha256=%s,guard_python=%s\n' "$mode" "$(sha_of "$REPO_ROOT/cmd/cad-worker/helper/cad-guard")" \
      "$(docker run --rm --network none --entrypoint /usr/bin/python3 "$TAG" --version 2>&1 | tr ' ' '_')"
    printf 'HDR,mode=%s,caps=inserts_%s+entities_%s+nesting_%s+abs_coordinate_%s,limits=cpus2_memory4g_swap4g_network_none_user_nobody\n' \
      "$mode" "$CAP_INSERTS" "$CAP_ENTITIES" "$CAP_NESTING" "$CAP_ABS"
  } >> "$LINES"
}

guard_in_image() {
  local dir=$1
  cat > "$WORK/tools/guard_all.py" <<'PY'
import json
import os
import subprocess
import sys

folder = sys.argv[1]
args = sys.argv[2:]
for name in sorted(os.listdir(folder)):
    if not name.endswith(".dxf"):
        continue
    proc = subprocess.run(["/usr/bin/python3", "-IB", "/usr/local/bin/cad-guard"] + args + [os.path.join(folder, name)],
                          capture_output=True, timeout=600)
    found = [ln for ln in proc.stdout.decode("ascii", "replace").splitlines() if ln.startswith("CADVERDICT ")]
    obj = json.loads(found[-1][len("CADVERDICT "):]) if found else {}
    print("%s\t%d\t%s" % (name[:-4], proc.returncode, json.dumps(obj, sort_keys=True, separators=(",", ":"))))
PY
  chmod 0644 "$WORK/tools/guard_all.py"
  docker run --rm "${LIMITS[@]}" -v "$dir:/in:ro" -v "$WORK/tools:/tools:ro" --entrypoint /usr/bin/python3 "$TAG" \
    -I -B /tools/guard_all.py /in "${GUARD_ARGS[@]}" </dev/null
}

audit_in_image() {
  local dir=$1
  cat > "$WORK/tools/audit_all.py" <<'PY'
import os
import subprocess
import sys

CHILD = """
import sys
import ezdxf
try:
    doc = ezdxf.readfile(sys.argv[1])
except BaseException as exc:
    print("exc:" + exc.__class__.__name__)
    sys.exit(0)
try:
    auditor = doc.audit()
except BaseException as exc:
    print("audit_exc:" + exc.__class__.__name__)
    sys.exit(0)
codes = sorted({int(e.code) for e in auditor.errors})
print("audit:" + ("+".join(str(c) for c in codes) if codes else "clean"))
"""
folder = sys.argv[1]
for name in sorted(os.listdir(folder)):
    if not name.endswith(".dxf"):
        continue
    try:
        proc = subprocess.run([sys.executable, "-I", "-B", "-c", CHILD, os.path.join(folder, name)],
                              capture_output=True, timeout=300)
        lines = [ln for ln in proc.stdout.decode("ascii", "replace").splitlines() if ln.strip()]
        out = lines[-1] if lines and proc.returncode == 0 else "crash_rc%d" % proc.returncode
    except subprocess.TimeoutExpired:
        out = "timeout"
    print("%s\t%s" % (name[:-4], out))
PY
  chmod 0644 "$WORK/tools/audit_all.py"
  docker run --rm "${LIMITS[@]}" -e HOME=/tmp -v "$dir:/in:ro" -v "$WORK/tools:/tools:ro" --entrypoint /opt/venv/bin/python "$TAG" \
    -I -B /tools/audit_all.py /in </dev/null 2>/dev/null
}

helper_unguarded() {
  local name=$1 src=$2 job wrapout rc_w=0
  job=$(new_job)
  cp "$src" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  mkdir -p "$job/wrap"
  chmod 0777 "$job/wrap"
  wrapout="$WORK/h_$name.wrap"
  docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -e "PROBE_RO=$DXF_RO" -e "PROBE_RW=/job" \
    -v "$WORK/probe:/probe:ro" -v "$WORK/wrap:/wrap:ro" -v "$job:/job" --entrypoint /usr/bin/python3 "$TAG" \
    -I -B /wrap/measure_wrap.py --budget-s "$H_DEADLINE_S" --outdir /job/wrap -- \
    /probe/sandbox-probe /usr/local/bin/cad-dxf --max-drawn-entities "$MAX_DRAWN" --texts /job/out.texts.json \
    --render-svg /job/out.render.svg /job/in.dxf /job/out.svg > "$wrapout" 2> "$WORK/h_$name.err" </dev/null || rc_w=$?
  cp "$job/wrap/child.log" "$WORK/h_$name.log" 2>/dev/null || : > "$WORK/h_$name.log"
  rm -rf "$job"
  if probe_refused "$WORK/h_$name.log"; then
    printf '%s\tprobe_refused\tnone\tna\tna\tna\n' "$name"
    return 0
  fi
  python3 - "$name" "$wrapout" "$WORK/h_$name.log" "$rc_w" <<'PY'
import json
import sys

name, wrap, log, rc_w = sys.argv[1:5]
fields = {}
with open(wrap, errors="replace") as fh:
    for line in fh:
        if line.startswith("WRAP,"):
            fields = dict(p.split("=", 1) for p in line.strip().split(",")[1:] if "=" in p)
verdict = None
with open(log, "rb") as fh:
    for raw in fh:
        line = raw.decode("ascii", "replace").strip()
        if line.startswith("CADVERDICT "):
            try:
                verdict = json.loads(line[len("CADVERDICT "):])
            except ValueError:
                verdict = None
if not fields or rc_w != "0":
    cls, reason = "wrap_failed", "none"
elif fields.get("timeout") == "1":
    cls, reason = "timeout", "deadline"
elif verdict is not None and verdict.get("stage") == "dxf" and verdict.get("verdict") in ("ok", "empty", "unreadable", "too_complex", "failed"):
    cls, reason = verdict["verdict"], verdict.get("reason") or "none"
else:
    cls, reason = "crash", "no_verdict"
print("%s\t%s\t%s\t%s\t%s\t%s" % (name, cls, reason, fields.get("rc", "na"), fields.get("wall_s", "na"), fields.get("oom_kill", "na")))
PY
}

replicate_src() {
  local target=$1 path gen
  path="$CAD_CACHE/scaled/dxf-replicate-uncommon-$target.dxf"
  if [ ! -s "$path" ]; then
    mkdir -p "$CAD_CACHE/scaled"
    gen=$(mktemp -d "$WORK/gen.XXXXXX")
    chmod 0777 "$gen"
    docker run --rm "${LIMITS[@]}" -e HOME=/tmp -v "$REPO_ROOT:/src:ro" -v "$gen:/out" -w /src \
      --entrypoint /opt/venv/bin/python "$TAG" -B scripts/cad-corpus-gen.py \
      --scaled-from internal/convert/testdata/cad/public/uncommon.dxf --kind dxf-replicate --targets "${target%MiB}" \
      --scaled-dir /out </dev/null >&2
    cp "$gen/dxf-replicate-uncommon-$target.dxf" "$path"
    rm -rf "$gen"
  fi
  printf '%s\n' "$path"
}

write_variants_py() {
  cat > "$WORK/tools/variants.py" <<'PY'
import os
import sys

OUT = sys.argv[1]
SRC = sys.argv[2]
INSERT_CAP = int(sys.argv[3])
ENTITY_CAP = int(sys.argv[4])
NESTING = int(sys.argv[5])
manifest = []


def put(name, data, expect, policy="none", at_cap=0):
    with open(os.path.join(OUT, name + ".dxf"), "wb") as fh:
        fh.write(data)
    manifest.append("%s\t%s\t%s\t%d" % (name, expect, policy, at_cap))


def enc(value):
    return value if isinstance(value, bytes) else value.encode("utf-8")


def pairs(items):
    return b"".join(b"%d\n" % code + enc(value) + b"\n" for code, value in items)


def header(acadver, codepage):
    out = [(0, "SECTION"), (2, "HEADER"), (9, "$ACADVER"), (1, acadver)]
    if codepage:
        out += [(9, "$DWGCODEPAGE"), (3, codepage)]
    return out + [(0, "ENDSEC")]


def document(acadver, codepage, blocks, entities, extra=()):
    items = header(acadver, codepage) + list(extra)
    if blocks:
        items += [(0, "SECTION"), (2, "BLOCKS")] + blocks + [(0, "ENDSEC")]
    items += [(0, "SECTION"), (2, "ENTITIES")] + entities + [(0, "ENDSEC"), (0, "EOF")]
    return pairs(items)


def line(x0="0.0", y0="0.0", x1="10.0", y1="0.0"):
    return [(0, "LINE"), (8, "0"), (10, x0), (20, y0), (30, "0.0"), (11, x1), (21, y1), (31, "0.0")]


def block(name, *bodies):
    out = [(0, "BLOCK"), (8, "0"), (2, name), (70, "0"), (10, "0.0"), (20, "0.0"), (30, "0.0"), (3, name)]
    for body in bodies:
        out += body
    return out + [(0, "ENDBLK"), (8, "0")]


def insert(name, *extra):
    return [(0, "INSERT"), (8, "0"), (2, name), (10, "1.0"), (20, "1.0"), (30, "0.0")] + list(extra)


def self_ref(acadver, codepage, outer, inner):
    return document(acadver, codepage, block(outer, line(), insert(inner)), insert(outer))


CYCLE = "too_complex/cyclic_block_reference"
put("case_ascii", self_ref("AC1009", "ANSI_1252", "A", "a"), CYCLE)
put("case_cp1251_io", self_ref("AC1015", "ANSI_1251", b"\xa8", b"\xb8"), CYCLE)
put("case_cp1252_bytes_differ", self_ref("AC1015", "ANSI_1252", b"\xa8", b"\xb8"), "ok")
put("case_utf8_kelvin", self_ref("AC1032", "ANSI_1252", "k", "K"), CYCLE)
put("unicode_escape", self_ref("AC1032", "ANSI_1252", "\\U+0416AB", "\\u+0416ab"), CYCLE)
put("unicode_escape_decoded", self_ref("AC1032", "ANSI_1252", "\\U+0416", "ж"), "ok")
put("space_trailing", self_ref("AC1009", "ANSI_1252", "A ", "A"), "ok")
put("space_leading", self_ref("AC1009", "ANSI_1252", " A", "A"), "ok")
dim = [(0, "DIMENSION"), (8, "0"), (2, "*d1"), (10, "10.0"), (20, "-5.0"), (30, "0.0"), (11, "5.0"), (21, "-5.0"),
       (31, "0.0"), (70, "0"), (13, "0.0"), (23, "0.0"), (33, "0.0"), (14, "10.0"), (24, "0.0"), (34, "0.0")]
put("dim_cycle", document("AC1009", "ANSI_1252", block("*D1", line(), insert("HOST")) + block("HOST", line(), dim),
                          insert("HOST")), CYCLE)

import ezdxf
from ezdxf.math import Vec2
from ezdxf.render import mleader as rm

ezdxf.options.write_fixed_meta_data_for_testing = True


def ezdxf_bytes(doc, name):
    path = os.path.join(OUT, name + ".dxf")
    doc.saveas(path)
    with open(path, "rb") as fh:
        return fh.read()


def ezdxf_case(name, version, encoding, outer, inner):
    doc = ezdxf.new(version)
    if encoding:
        doc.encoding = encoding
    blk = doc.blocks.new(outer)
    blk.add_line((0, 0), (10, 0))
    blk.add_blockref(inner, (5, 5))
    doc.modelspace().add_blockref(outer, (0, 0))
    put(name, ezdxf_bytes(doc, name), CYCLE)


ezdxf_case("case_cp1251", "R2000", "cp1251", "ДЕТАЛЬ", "деталь")
ezdxf_case("case_cp1252", "R2000", "cp1252", "ÉLÉMENT", "élément")
ezdxf_case("case_utf8_cyrillic", "R2018", None, "ДЕТАЛЬ", "деталь")
ezdxf_case("case_utf8_greek", "R2018", None, "ΣΧΗΜΑ", "σχημα")

doc = ezdxf.readfile(os.path.join(SRC, "public", "acad_table_with_blk_ref.dxf"))
msp = doc.modelspace()
table = msp.query("ACAD_TABLE")[0]
host = doc.blocks.new("HOST")
msp.move_to_layout(table, host)
table.dxf.owner = host.block_record_handle
doc.blocks.get(table.dxf.geometry).add_blockref("HOST", (0, 0))
msp.add_blockref("HOST", (0, 0))
put("table_cycle", ezdxf_bytes(doc, "table_cycle"), CYCLE)

doc = ezdxf.new("R2018", setup=True)
content = doc.blocks.new("X")
content.add_line((0, 0), (3, 0))
holder = doc.blocks.new("M")
holder.add_line((0, 0), (10, 0))
builder = holder.add_multileader_block(style="Standard")
builder.set_content(name="X", alignment=rm.BlockAlignment.center_extents)
builder.add_leader_line(rm.ConnectionSide.left, [Vec2(-5, 5)])
builder.build(insert=Vec2(20, 20))
handle = holder.block_record_handle
builder.multileader.context.block.block_record_handle = handle
builder.multileader.dxf.block_record_handle = handle
doc.modelspace().add_blockref("M", (0, 0))
put("mleader_cycle", ezdxf_bytes(doc, "mleader_cycle"), CYCLE)

doc = ezdxf.new("R2000", setup=True)
arrow = doc.blocks.new("ARR")
arrow.add_line((0, 0), (1, 0))
style = doc.dimstyles.get("EZDXF")
style.dxf.dimldrblk = "ARR"
arrow.add_leader([(0, 0), (5, 5)], dimstyle="EZDXF")
doc.modelspace().add_leader([(0, 0), (5, 5)], dimstyle="EZDXF")
put("leader_cycle", ezdxf_bytes(doc, "leader_cycle"), CYCLE)

for depth in (4, 16, 17, 64, 199, 200, 250):
    blocks = []
    for level in range(1, depth + 1):
        body = line() if level == depth else insert("N%d" % (level + 1))
        blocks += block("N%d" % level, body)
    over = depth > NESTING
    put("nest_d%d" % depth, document("AC1009", "ANSI_1252", blocks, insert("N1")),
        "too_complex/nesting_too_deep" if over else "ok", "nesting" if over else "none")


def grid(cells, cell_body, extra_lines):
    cols = 1
    rows = cells
    return document("AC1009", "ANSI_1252", block("CELL", *cell_body),
                    insert("CELL", (70, str(cols)), (71, str(rows)), (44, "1.0"), (45, "1.0")) + sum(extra_lines, []))


put("minsert_at_insert_cap", grid(INSERT_CAP, [line()], []), "ok", "none", 1)
put("minsert_over_insert_cap", grid(INSERT_CAP + 1, [line()], []), "too_complex/expanded_inserts_over_cap", "inserts")
five = [line(), line(), line(), line(), line()]
cells = INSERT_CAP - 1
pad = ENTITY_CAP - 1 - cells * 5
put("grid_at_entity_cap", grid(cells, five, [line()] * pad), "ok", "none", 1)
put("grid_over_entity_cap", grid(cells, five, [line()] * (pad + 1)), "too_complex/expanded_entities_over_cap", "entities")

NUMERICS = [("abc", "abc", "unreadable/non_numeric_value"), ("comma", "1,5", "unreadable/non_numeric_value"),
            ("nan", "nan", "unreadable/non_numeric_value"), ("inf", "inf", "unreadable/non_numeric_value"),
            ("neginf", "-inf", "unreadable/non_numeric_value"), ("underscore", "1_0", "unreadable/non_numeric_value"),
            ("1e300", "1e300", "unreadable/coordinate_out_of_range"),
            ("1e400", "1e400", "unreadable/coordinate_out_of_range"),
            ("at_bound", "1e12", "ok"), ("above_bound", "1.0000000001e12", "unreadable/coordinate_out_of_range")]
for label, literal, expect in NUMERICS:
    policy = "none" if expect == "ok" else "numeric"
    put("num_entities_" + label, document("AC1009", "ANSI_1252", [], line(x0=literal) + line(y0="5.0", y1="5.0")),
        expect, policy)
    put("num_blocks_" + label, document("AC1009", "ANSI_1252", block("B", line(x0=literal), line(y0="5.0", y1="5.0")),
                                        insert("B")), expect, policy)
put("int_code_real", document("AC1009", "ANSI_1252", block("C", line()), insert("C", (70, "2.0"))), "ok")
put("int_code_text", document("AC1009", "ANSI_1252", block("C", line()), insert("C", (70, "x"))),
    "unreadable/non_numeric_value", "numeric")

with open(os.path.join(OUT, "manifest.tsv"), "w") as fh:
    fh.write("\n".join(manifest) + "\n")
print("variants=%d" % len(manifest))
PY
  chmod 0644 "$WORK/tools/variants.py"
}

declared_fixtures() {
  cat <<'TSV'
fixture__cyclic_self	too_complex/cyclic_block_reference	none	0
fixture__cyclic_ab	too_complex/cyclic_block_reference	none	0
fixture__cyclic_unreferenced	too_complex/cyclic_block_reference	none	0
fixture__cyclic_case	too_complex/cyclic_block_reference	none	0
fixture__nest_d16	ok	none	0
fixture__nest_d17	too_complex/nesting_too_deep	nesting	0
fixture__minsert_over_inserts	too_complex/expanded_inserts_over_cap	inserts	0
fixture__dimension_minsert	too_complex/expanded_inserts_over_cap	inserts	0
fixture__entities_over	too_complex/expanded_entities_over_cap	entities	0
fixture__nonnumeric_abc	unreadable/non_numeric_value	numeric	0
fixture__nonnumeric_comma	unreadable/non_numeric_value	numeric	0
fixture__nonnumeric_nan	unreadable/non_numeric_value	numeric	0
fixture__nonnumeric_inf	unreadable/non_numeric_value	numeric	0
fixture__nonnumeric_underscore	unreadable/non_numeric_value	numeric	0
fixture__coord_1e300	unreadable/coordinate_out_of_range	numeric	0
fixture__coord_1e400	unreadable/coordinate_out_of_range	numeric	0
reject__dxf_binary	unreadable/bad_group_code	none	0
reject__dxf_bom	unreadable/bad_group_code	none	0
reject__dxf_truncated	ok	none	0
TSV
}

judge_diff() {
  python3 - "$WORK" "$CAP_NESTING" <<'PY'
import json
import sys

work, cap_nesting = sys.argv[1], int(sys.argv[2])


def table(name, width):
    rows = {}
    with open("%s/%s" % (work, name), encoding="utf-8") as fh:
        for line in fh:
            parts = line.rstrip("\n").split("\t")
            if len(parts) >= width:
                rows[parts[0]] = parts[1:]
    return rows


declared = table("declared.frozen", 4)
sha = table("sha.tsv", 2)
gtab = table("g.tsv", 3)
atab = table("a.tsv", 2)
htab = table("h.tsv", 6)
POLICY_REASONS = {
    "nesting": {"nesting_too_deep"},
    "inserts": {"expanded_inserts_over_cap"},
    "entities": {"expanded_entities_over_cap"},
    "numeric": {"non_numeric_value", "coordinate_out_of_range"},
}
POLICY_CLASS = {"nesting": "conservative_count", "inserts": "conservative_count", "entities": "conservative_count",
                "numeric": "numeric_policy"}
EXPLAIN = {
    "admitted_slow": "at_a_cap_by_construction_the_guard_admits_it_and_the_helper_needs_longer_than_the_differential_deadline",
    "backstop_caught": "the_guard_admits_it_and_the_helper_backstop_ends_it_terminal_too_complex",
    "reader_rejects": "the_guard_admits_it_and_ezdxf_strict_read_or_audit_ends_it_terminal_unreadable",
    "preempted": "the_guard_rejects_an_input_that_unguarded_ends_failed_crash_or_timeout",
    "conservative_count": "declared_over_a_count_or_nesting_bound_before_the_run_and_ezdxf_would_have_coped",
    "numeric_policy": "declared_R-8_numeric_before_the_run_and_ezdxf_reads_it_leniently",
    "both_terminal": "both_reject_with_different_terminal_classes",
    "audit_blind": "the_guard_sees_a_handle_cycle_that_ezdxf_audit_does_not_report_and_the_helper_ends_it_too_complex",
}
rows = divergences = unexplained = 0
out = []
scope_fail = []
nest = {}
for name in sorted(declared):
    if name not in gtab:
        out.append("DIFF,variant=%s,problems=no_guard_row,class=unexplained,result=fail" % name)
        rows += 1
        unexplained += 1
        continue
    expect, policy, at_cap = declared[name][0], declared[name][1], declared[name][2] == "1"
    exit_code, obj = gtab[name][0], json.loads(gtab[name][1] or "{}")
    g = obj.get("verdict", "none")
    g_reason = obj.get("reason") or "none"
    g_full = g if g == "ok" else "%s/%s" % (g, g_reason)
    a = atab.get(name, ["missing"])[0]
    h, h_reason, h_rc, h_wall, h_oom = (htab.get(name) or ["missing", "none", "na", "na", "na"])[:5]
    source = name.split("__", 1)[0]
    a104 = a.startswith("audit:") and "104" in a[len("audit:"):].split("+")
    problems = []
    if g_full != expect:
        problems.append("G_differs_from_expectation")
    if h in ("probe_refused", "wrap_failed", "missing"):
        problems.append("H_not_measured")
    if h in ("failed", "crash", "timeout") and g == "ok" and not (at_cap and h == "timeout"):
        problems.append("P1")
    if a104 and g != "too_complex":
        problems.append("P2")
    declared_ok = policy in POLICY_REASONS and g_reason in POLICY_REASONS[policy]
    if (source == "corpus" or h == "ok") and g != "ok" and not (source != "corpus" and declared_ok):
        problems.append("P3")
    if (g == "ok" and h in ("ok", "empty")) or (g != "ok" and g == h):
        cls = "agree"
    elif g == "ok" and h == "timeout" and at_cap:
        cls = "admitted_slow"
    elif g == "ok" and h == "too_complex":
        cls = "backstop_caught"
    elif g == "ok" and h == "unreadable":
        cls = "reader_rejects"
    elif g != "ok" and h in ("failed", "crash", "timeout"):
        cls = "preempted"
    elif g != "ok" and h in ("ok", "empty") and declared_ok:
        cls = POLICY_CLASS[policy]
    elif g != "ok" and h in ("too_complex", "unreadable"):
        cls = "both_terminal"
    else:
        cls = "unexplained"
    if cls == "agree" and g_reason == "cyclic_block_reference" and not a104:
        cls = "audit_blind" if h == "too_complex" else "unexplained"
    if problems:
        cls = "unexplained"
    if cls == "unexplained":
        unexplained += 1
        result = "fail"
    elif cls != "agree":
        divergences += 1
        result = "divergence"
    else:
        result = "ok"
    rows += 1
    h_full = h if h_reason == "none" else "%s/%s" % (h, h_reason)
    out.append("DIFF,variant=%s,source=%s,sha256=%s,at_cap=%d,policy=%s,expect=%s,G=%s,g_exit=%s,g_nesting=%s,g_expanded_inserts=%s,g_expanded_entities=%s,A=%s,H=%s,h_rc=%s,h_wall_s=%s,h_oom_kill=%s,class=%s,problems=%s,result=%s" % (
        name, source, sha.get(name, ["na"])[0], at_cap, policy, expect, g_full, exit_code, obj.get("nesting", "na"),
        obj.get("expanded_inserts", "na"), obj.get("expanded_entities", "na"), a, h_full, h_rc, h_wall, h_oom,
        cls, "+".join(problems) or "none", result))
    if cls not in ("agree", "unexplained"):
        out.append("DIVERGENCE,variant=%s,class=%s,G=%s,A=%s,H=%s,explanation=%s" % (name, cls, g_full, a, h_full, EXPLAIN[cls]))
    if cls == "admitted_slow":
        out.append("FACT,key=admitted_slow,mode=diff,variant=%s,G=%s,H=%s,deadline_s=%s,meaning=at_cap_input_admitted_by_the_guard_needs_more_than_the_differential_deadline_75-05_measures_the_stage_timeout" % (
            name, g_full, h_full, h_wall))
    if name.startswith("fixture__nonfinite_"):
        ok = g == "ok" and h in ("ok", "empty", "unreadable")
        out.append("SCOPE,fixture=%s,section=%s,G=%s,A=%s,H=%s,required=G_ok_and_H_ok_empty_or_unreadable,result=%s" % (
            name.split("__", 1)[1], name.rsplit("_", 1)[1], g_full, a, h_full, "ok" if ok else "fail"))
        if h in ("failed", "crash", "timeout"):
            scope_fail.append("%s_H_%s" % (name.split("__", 1)[1], h_full.replace("/", "_")))
        elif not ok:
            scope_fail.append("%s_G_%s" % (name.split("__", 1)[1], g_full.replace("/", "_")))
    if name.startswith("variant__nest_d"):
        nest[int(name[len("variant__nest_d"):])] = h_full
corpus_nesting = {}
for name, (exit_code, blob) in gtab.items():
    if name.startswith("corpus__"):
        obj = json.loads(blob or "{}")
        corpus_nesting[name] = obj.get("nesting", -1) if obj.get("verdict") == "ok" else -1
ok_depths = sorted(d for d, h in nest.items() if h.split("/")[0] in ("ok", "empty"))
recursion = sorted(d for d, h in nest.items() if h.startswith("too_complex"))
out.append("FACT,key=ezdxf_recursion_depth,mode=diff,depths=%s,h_ok_up_to=%s,first_h_too_complex=%s,source=variant_nest_dN_H_runs" % (
    "+".join("%d:%s" % (d, nest[d].replace("/", "_")) for d in sorted(nest)), ok_depths[-1] if ok_depths else "none",
    recursion[0] if recursion else "none"))
derived_ok = True
if len(corpus_nesting) != 17 or min(corpus_nesting.values()) < 0:
    out.append("STOP,mode=diff,reason=corpus_guard_not_ok,files=%d,detail=%s" % (len(corpus_nesting), "+".join(
        "%s:%s" % (k, v) for k, v in sorted(corpus_nesting.items()))))
    derived_ok = False
else:
    m = max(corpus_nesting.values())
    top = [k.split("__", 1)[1] for k, v in sorted(corpus_nesting.items()) if v == m]
    per_file = "+".join("%s:%d" % (k.split("__", 1)[1], v) for k, v in sorted(corpus_nesting.items()))
    if 4 * m == cap_nesting:
        out.append("DERIVED,key=cad_dxf_nesting_bound,value=%d,derivation=4 x corpus max M=%d,corpus_files=%d,max_files=%s,per_file=%s,first_h_too_complex_depth=%s,source=guard_nesting_counter" % (
            4 * m, m, len(corpus_nesting), "+".join(top), per_file, recursion[0] if recursion else "none"))
    else:
        out.append("STOP,mode=diff,reason=nesting_bound_differs,derived=%d,M=%d,locked=%d,per_file=%s" % (4 * m, m, cap_nesting, per_file))
        derived_ok = False
if scope_fail:
    out.append("STOP,mode=diff,reason=r8_scope_leaves_retryable_crash,facts=%s" % "+".join(scope_fail))
result = "ok" if unexplained == 0 and derived_ok and not scope_fail else "fail"
out.append("SUMMARY,mode=diff,variants=%d,divergences=%d,unexplained=%d,result=%s" % (rows, divergences, unexplained, result))
print("\n".join(out))
PY
}

diff_mode() {
  local f name n=0 hrow
  setup diff
  build_probe "$ARCH" "$WORK/probe"
  mkdir -p "$WORK/var" "$WORK/rep"
  chmod 0777 "$WORK/var"
  write_variants_py
  if ! docker run --rm "${LIMITS[@]}" -e HOME=/tmp -v "$WORK/var:/out" -v "$WORK/tools:/tools:ro" -v "$CORPUS:/corpus:ro" \
    --entrypoint /opt/venv/bin/python "$TAG" -I -B /tools/variants.py /out /corpus "$CAP_INSERTS" "$CAP_ENTITIES" "$CAP_NESTING" \
    </dev/null > "$WORK/variants.out" 2> "$WORK/variants.err"; then
    tail -n 20 "$WORK/variants.err" >&2
    fail "variant generation failed"
  fi
  mv "$WORK/var/manifest.tsv" "$WORK/manifest.tsv"
  sed 's/^/variant__/' "$WORK/manifest.tsv" > "$WORK/declared.tsv"
  for f in "$WORK"/var/*.dxf; do mv "$f" "$WORK/var/variant__$(basename "$f")"; done
  declared_fixtures >> "$WORK/declared.tsv"
  for f in "$CORPUS"/dxf/*.dxf; do
    cp "$f" "$WORK/var/fixture__$(basename "$f")"
    grep -q "^fixture__$(basename "$f" .dxf)	" "$WORK/declared.tsv" || printf 'fixture__%s\tok\tnone\t0\n' "$(basename "$f" .dxf)" >> "$WORK/declared.tsv"
  done
  for f in "$CORPUS"/reject/dxf_*.dxf; do cp "$f" "$WORK/var/reject__$(basename "$f")"; done
  for f in "${CORPUS_FILES[@]}"; do
    cp "$CORPUS/$f.dxf" "$WORK/var/corpus__$(basename "$f").dxf"
    printf 'corpus__%s\tok\tnone\t0\n' "$(basename "$f")" >> "$WORK/declared.tsv"
  done
  for f in "${REPLICATES[@]}"; do cp "$(replicate_src "$f")" "$WORK/rep/corpus__dxf-replicate-uncommon-$f.dxf"; done
  chmod 0644 "$WORK"/var/*.dxf "$WORK"/rep/*.dxf
  sort "$WORK/declared.tsv" > "$WORK/declared.frozen"
  chmod 0444 "$WORK/declared.frozen"
  [ "$(wc -l < "$WORK/declared.frozen")" -eq "$(ls "$WORK"/var/*.dxf | wc -l)" ] || fail "an input has no declaration"
  cad_log "declared $(wc -l < "$WORK/declared.frozen" | tr -d ' ') inputs before any run"
  {
    ( cd "$WORK/var" && shasum -a 256 -- *.dxf )
    ( cd "$WORK/rep" && shasum -a 256 -- *.dxf )
  } | awk '{sub(/\.dxf$/, "", $2); print $2 "\t" $1}' > "$WORK/sha.tsv"
  cad_log "G: cad-guard on every input"
  guard_in_image "$WORK/var" > "$WORK/g.tsv"
  guard_in_image "$WORK/rep" >> "$WORK/g.tsv"
  cad_log "A: ezdxf read and audit"
  audit_in_image "$WORK/var" > "$WORK/a.tsv"
  cad_log "H: unguarded cad-dxf under its ruleset, ${H_DEADLINE_S}s deadline"
  : > "$WORK/h.tsv"
  for f in "$WORK"/var/*.dxf; do
    n=$((n + 1))
    [ $((n % 15)) -ne 1 ] || disk_guard
    name=$(basename "$f" .dxf)
    hrow=$(helper_unguarded "$name" "$f")
    printf '%s\n' "$hrow" >> "$WORK/h.tsv"
    cad_log "H $hrow"
  done
  {
    printf 'RULE,key=p3_declared_exceptions,mode=diff,rule=P3_holds_for_every_corpus_file_and_every_variant_except_inputs_declared_in_this_script_before_any_G_A_or_H_run_as_over_a_policy_bound(nesting_above_%s+insert_cap+entity_cap+R-8_numerics);a_declared_input_whose_guard_reason_is_its_policy_reason_is_DIVERGENCE_conservative_count_or_numeric_policy;an_undeclared_divergence_is_unexplained_and_fails,declared=%s,source=75-03_orchestrator_decision_2\n' \
      "$CAP_NESTING" "$(awk -F'\t' '$3 != "none" {print $1}' "$WORK/declared.frozen" | paste -sd '+' -)"
    printf 'RULE,key=p1,mode=diff,rule=H_failed_crash_or_timeout_implies_G_too_complex_or_unreadable;an_input_at_a_cap_by_construction_whose_H_times_out_at_%ss_is_admitted_slow_and_recorded_as_a_FACT,at_cap=%s,source=75-03-PLAN_and_orchestrator_decision_3\n' \
      "$H_DEADLINE_S" "$(awk -F'\t' '$4 == "1" {print $1}' "$WORK/declared.frozen" | paste -sd '+' -)"
    printf 'RULE,key=p2,mode=diff,rule=ezdxf_audit_code_104_implies_G_too_complex\n'
    printf 'RULE,key=expectation,mode=diff,rule=every_input_carries_a_G_expectation_fixed_in_this_script_before_the_run;any_other_G_verdict_is_unexplained\n'
    printf 'RULE,key=h_setup,mode=diff,helper=/usr/local/bin/cad-dxf,confined_by=sandbox-probe,ro=%s,rw=/job,deadline_s=%s,max_drawn_entities=%s,env=HOME=/job/.home_XDG_CACHE_HOME=/job/.home/.cache_XDG_CONFIG_HOME=/job/.home/.config_TMPDIR=/job\n' \
      "$DXF_RO" "$H_DEADLINE_S" "$MAX_DRAWN"
    judge_diff
  } >> "$LINES"
  evid_put "$LINES" '^(HDR,mode=diff,|DIFF,|DIVERGENCE,|SCOPE,|DERIVED,|FACT,[^,]*,mode=diff,|RULE,key=[^,]*,mode=diff,|STOP,mode=diff,|SUMMARY,mode=diff,)'
  grep -E '^(STOP|SUMMARY|DERIVED|SCOPE),' "$LINES" || true
  grep -q '^SUMMARY,mode=diff,.*result=ok$' "$LINES" || exit 1
}

stress_src() {
  local shape=$1 path
  path="$CAD_CACHE/scaled/dxf-guard-stress-$shape-${CEILING_MIB}MiB.dxf"
  if [ ! -s "$path" ]; then
    mkdir -p "$CAD_CACHE/scaled"
    python3 "$GEN_PY" --kind dxf-guard-stress --shape "$shape" --targets "$CEILING_MIB" --scaled-dir "$CAD_CACHE/scaled" >&2
  fi
  printf '%s\n' "$path"
}

cost_row() {
  local shape=$1 src=$2 rep=$3 expect=$4 job wrapout rc_w=0
  disk_guard
  job=$(new_job)
  wrapout="$WORK/cost_${shape}_$rep.wrap"
  docker run --rm "${LIMITS[@]}" -v "$src:/in/in.dxf:ro" -v "$WORK/wrap:/wrap:ro" -v "$job:/job" \
    --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py --budget-s 120 --outdir /job -- \
    /usr/bin/python3 -IB /usr/local/bin/cad-guard "${GUARD_ARGS[@]}" /in/in.dxf > "$wrapout" 2>/dev/null </dev/null || rc_w=$?
  python3 - "$shape" "$rep" "$src" "$wrapout" "$job/child.log" "$rc_w" "$expect" "$BUDGET_MIB" "$COST_WALL_S" \
    "$(wc -c < "$src" | tr -d ' ')" "$(sha_of "$src")" <<'PY' >> "$LINES"
import json
import sys

shape, rep, src, wrap, log, rc_w, expect, budget, wall_bound, size, digest = sys.argv[1:12]
fields = {}
with open(wrap, errors="replace") as fh:
    for line in fh:
        if line.startswith("WRAP,"):
            fields = dict(p.split("=", 1) for p in line.strip().split(",")[1:] if "=" in p)
obj = {}
try:
    with open(log, "rb") as fh:
        for raw in fh:
            line = raw.decode("ascii", "replace").strip()
            if line.startswith("CADVERDICT "):
                obj = json.loads(line[len("CADVERDICT "):])
except OSError:
    pass
verdict = obj.get("verdict", "none")
reason = obj.get("reason") or "none"
got = verdict if verdict == "ok" else "%s/%s" % (verdict, reason)
try:
    wall = float(fields["wall_s"])
    peak = float(fields["cg_peak_mib"])
except (KeyError, ValueError):
    wall = peak = float("inf")
ok = rc_w == "0" and fields.get("timeout") == "0" and fields.get("oom_kill") == "0" and got == expect \
    and wall <= float(wall_bound) and peak <= float(budget)
print("GUARD_COST,shape=%s,rep=%s,file=%s,bytes=%s,sha256=%s,verdict=%s,reason=%s,expect=%s,counters=%s,wall_s=%s,child_rss_mib=%s,cg_peak_mib=%s,pct_of_budget=%s,wall_bound_s=%s,budget_mib=%s,oom_kill=%s,timeout=%s,result=%s" % (
    shape, rep, src.rsplit("/", 1)[-1], size, digest, verdict, reason, expect,
    "+".join("%s:%s" % (k, obj[k]) for k in ("entities", "expanded_inserts", "expanded_entities", "nesting", "blocks") if k in obj),
    fields.get("wall_s", "na"), fields.get("child_rss_mib", "na"), fields.get("cg_peak_mib", "na"),
    "%.4f" % (peak / float(budget)) if peak != float("inf") else "na", wall_bound, budget,
    fields.get("oom_kill", "na"), fields.get("timeout", "na"), "ok" if ok else "fail"))
PY
  rm -rf "$job"
}

cost_mode() {
  local shape src rep rows fails expect
  setup cost
  for shape in "${STRESS_SHAPES[@]}"; do
    src=$(stress_src "$shape")
    case "$shape" in
      chain) expect=too_complex/nesting_too_deep ;;
      fanout|wide) expect=too_complex/expanded_inserts_over_cap ;;
      *) expect=ok ;;
    esac
    for rep in $(seq 1 "$COST_REPS"); do cost_row "$shape" "$src" "$rep" "$expect"; done
  done
  src=$(replicate_src "${CEILING_MIB}MiB")
  for rep in $(seq 1 "$COST_REPS"); do cost_row replicate "$src" "$rep" ok; done
  rows=$(grep -c '^GUARD_COST,' "$LINES" || true)
  fails=$(grep -c '^GUARD_COST,.*result=fail$' "$LINES" || true)
  {
    printf 'RULE,key=cost_bound,mode=cost,rule=every_row_wall_at_most_%ss_and_cgroup_peak_at_most_%s_MiB(R-7_budget_0.70x4096-24.9);chain_too_complex_nesting+fanout_and_wide_too_complex_inserts+blocks_and_replicate_ok,runner=/usr/bin/python3_in_the_engine_image\n' "$COST_WALL_S" "$BUDGET_MIB"
    if [ "$fails" != 0 ]; then
      printf 'STOP,mode=cost,reason=guard_cost_over_bound_or_wrong_verdict,failed_rows=%s\n' "$fails"
    fi
    printf 'SUMMARY,mode=cost,rows=%s,failed=%s,result=%s\n' "$rows" "$fails" "$( [ "$fails" = 0 ] && echo ok || echo fail )"
  } >> "$LINES"
  evid_put "$LINES" '^(HDR,mode=cost,|GUARD_COST,|RULE,key=[^,]*,mode=cost,|STOP,mode=cost,|SUMMARY,mode=cost,)'
  grep -E '^(GUARD_COST|STOP|SUMMARY),' "$LINES" || true
  [ "$fails" = 0 ] || exit 1
}

MODE=${1:-}
[ -n "$MODE" ] || usage
shift
while [ $# -gt 0 ]; do
  case "$1" in
    --out) [ $# -ge 2 ] || usage; EVID=$2; shift 2 ;;
    *) usage ;;
  esac
done
case "$MODE" in
  diff) diff_mode ;;
  cost) cost_mode ;;
  *) usage ;;
esac

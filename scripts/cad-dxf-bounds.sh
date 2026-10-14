#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

PHASE_DIR="$REPO_ROOT/.planning/phases/75-dxf-2d-pipeline"
EVID="$PHASE_DIR/evidence/75-05-bounds.txt"
EVID_NAME="evidence/75-05-bounds.txt"
DOCKERFILE="$REPO_ROOT/Dockerfile.cad-worker"
CAD_CACHE="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
WRAP_PY="$REPO_ROOT/scripts/cad-measure/measure_wrap.py"
BASE_DXF=internal/convert/testdata/cad/public/uncommon.dxf
GO_TOOLCHAIN=go1.26.4
MIN_FREE_GIB=6
SIZE=2048
STAGE_BUDGET_S=1800
COUNT_CAP=4611686018427387903
CAP_NESTING=16
CAP_ABS=1e12
SMOKE_CAP_INSERTS=170031
SMOKE_CAP_ENTITIES=850155
LADDER=(60000 100000 120000 135000 150000 170031)
CEILING_TARGETS=(100 118)
STRESS_SHAPES=(chain fanout blocks wide)
STRESS_REPS=2
B_BLOCK_ENTITIES=50
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
STAGE_ENV=(-e HOME=/job/.home -e XDG_CACHE_HOME=/job/.home/.cache -e XDG_CONFIG_HOME=/job/.home/.config -e TMPDIR=/job)
HELPERS=(cad-mesh cad-guard cad-render cad-dxf)
GO_CLIS=(cad-svg-check cad-pdf-check cad-pixel-judge)

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/cad-dxf-bounds.sh smoke [--out FILE]
  scripts/cad-dxf-bounds.sh run [--out FILE]
  scripts/cad-dxf-bounds.sh floor [--out FILE]
  scripts/cad-dxf-bounds.sh stack [--smoke] [--out FILE]
  scripts/cad-dxf-bounds.sh bounds76 --init|--smoke|--cells-only|--group G [--rerun]|--amend-rerun [--row-log LOG]|--finish [--out FILE]

Measures the DXF bounds of plan 75-05 on the production engine image of Dockerfile.cad-worker.
Every row runs, each step in its own container under --cpus 2 --memory 4g --memory-swap 4g
--network none --user nobody wrapped by measure_wrap.py: cad-guard, cad-dxf confined by its
Landlock set, cad-svg-check on the client and the render-only SVG, rsvg-convert to PDF from the
validated render-only SVG, cad-pdf-check, rsvg-convert to PNG 2048 opaque and transparent in the
letterbox of cad-pixel-judge --print-letterbox, and cad-pixel-judge --drawing on both PNGs.

smoke  runs the same steps on dxf-minsert n=100 and n=1000, the 2 MiB replicate of uncommon.dxf,
       a 50-entity block at n=20 and a dxf-combo of the 2 MiB replicate with 100 cells, into a
       temporary evidence file (default: a file under TMPDIR), and derives nothing.
run    runs the 75-03 guard stress shapes, the ceiling rows (100 and 118 MiB replicates), the
       dxf-minsert ladder (60000 to 170031 cells of the 5-entity block), the second repetitions,
       the two check rows and the pre-registered derivation into
       .planning/phases/75-dxf-2d-pipeline/evidence/75-05-bounds.txt.

floor  the R-10 sweep: uncommon.dxf with one far POINT at 61 distances and the 2 to 50 MiB replicates,
       each drawn, validated, converted to PDF and reported per text by cad-pdf-check --report, and
       the DERIVED cad_dxf_pdf_text_floor = 2 x the largest rendered size of a lost text. Its lines
       replace only the floor lines of the evidence file; run replaces only the others.

stack  the pre-registered 3D stacking probe of plan 76-01 on the production engine image: stacked STL
       (cube and the largest liveproof STL), NAUO assemblies on a grid and coincident, STEP copies and
       IGES written from STEP copies, each rung through the production stages (cad-guard, cad-mesh or
       cad-mesh stats, cad-render iso 2048 opaque), into
       .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt with the
       STACK_DECISION and STACK_GLOBAL lines. --smoke runs one cube rung into a file under TMPDIR,
       checks stl-tile output against the main-branch generator, and decides nothing.

bounds76  the pre-registered stage bounds of plan 76-02 on the production engine image. --init writes the HDR and RULE
       lines into a new evidence file (default
       .planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-02-bounds.txt); --cells-only builds,
       sniffs and guards every D-02 cell and writes CELL lines; --group refusal|dxf|stl|step|iges|pathb appends one
       group of rows after checking the commit and image id against the init HDR; --finish checks every required
       group and writes SUMMARY; --smoke runs one 3D and one DXF row through every stage kind into a file under
       TMPDIR. --amend-rerun appends the one rerun_after_watchdog amendment; --group G --rerun then re-runs once a
       group the disk watchdog stopped, keeping its partial rows and its STOP line. A second --amend-rerun appends
       the one rerun_after_product_fix amendment for the step group stopped by the render judge (stage pixel) and
       its NOTE lines; --group step --rerun then re-runs it once at a commit that differs from the init commit only
       in .planning, this script and the judge fix of quick 261014-0lo, and every later group runs at that commit.
       --amend-rerun --row-log LOG closes a group the host's sleep interrupted: it writes STOP reason=host_sleep with
       the pmset Sleep..Wake intervals, the rerun_after_host_sleep amendment and one contaminated=1 NOTE per row of
       that attempt whose interval (from LOG, the group's stderr log) overlaps a sleep; --group G --rerun then
       re-runs it once under caffeinate -dimsu at the same commit and image.
       The on-record walls of D-01 (onrecord: 75-05 BOUND_ROW, cad-dxf 123.61 s, rsvg 96.60 s) are named
       in the RULE lines and applied by 76-03.

The RULE lines are written before any row is measured. Generated inputs live in
$OCTOCONV_CAD_CACHE/scaled (default ~/.cache/octoconv/cad-corpus/scaled). Every image tag the
script builds is removed on exit.
Exit codes: 64 usage, 69 a tool is missing, 3 VM disk below 6 GiB, 1 a STOP or a failed run.
USAGE
  exit 64
}

fail() { echo "FAIL: $*" >&2; exit 1; }

need_tool() { command -v "$1" >/dev/null 2>&1 || { echo "error: $1 is required" >&2; exit 69; }; }

sha_of() { shasum -a 256 "$1" | awk '{print $1}'; }

bytes_of() { wc -c < "$1" | tr -d ' '; }

vm_free_gib() { docker run --rm alpine df -k / | awk 'NR == 2 { print int($4 / 1048576) }'; }

log() { echo "[$(date -u +%H:%M:%S)] $*" >&2; }

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
  tag="octoconv-cad-worker:${ENGINE_TAG_PREFIX:-engine75-}$(inputs_short)"
  stage=$(mktemp -d "${TMPDIR:-/tmp}/cad-bounds-build.XXXXXX")
  mkdir -p "$stage/cmd/cad-worker/helper"
  cp "$DOCKERFILE" "$stage/Dockerfile.cad-worker"
  cp "$REPO_ROOT"/cmd/cad-worker/requirements-*.txt "$REPO_ROOT"/cmd/cad-worker/vtk_keeplist.py \
    "$REPO_ROOT"/cmd/cad-worker/vtk-keeplist.txt "$stage/cmd/cad-worker/"
  for h in "$REPO_ROOT"/cmd/cad-worker/helper/*; do
    [ ! -f "$h" ] || cp "$h" "$stage/cmd/cad-worker/helper/"
  done
  log "building $tag"
  if ! docker build -f "$stage/Dockerfile.cad-worker" --target engine -t "$tag" "$stage" >&2; then
    rm -rf "$stage"
    log "error: image build failed"
    return 1
  fi
  rm -rf "$stage"
  printf '%s\n' "$tag"
}

build_go() {
  local arch=$1 dir=$2 c
  mkdir -p "$dir"
  chmod 0755 "$dir"
  CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$dir/sandbox-probe" ./scripts/cmd/sandbox-probe
  for c in "${GO_CLIS[@]}"; do
    CGO_ENABLED=0 GOOS=linux GOARCH="$arch" GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$dir/$c" "./scripts/cmd/$c"
  done
  chmod 0755 "$dir"/*
}

tracked_dirt() {
  git status --porcelain --untracked-files=no | awk '{print $2}' | grep -vc "$EVID_NAME" || true
}

new_job() {
  local d
  d=$(mktemp -d "$WORK/job.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

FLOOR_PATTERN='^(RULE,key=r10_[^,]*,mode=floor,|HDR,mode=floor,|FLOOR_ROW,|FLOOR_FILE,|FLOOR_GEN,|DERIVED,key=cad_dxf_pdf_text_floor,|STOP,mode=floor,|SUMMARY,mode=floor,)'

write_evidence() {
  local keep
  mkdir -p "$(dirname "$EVID")"
  keep=$(mktemp "${TMPDIR:-/tmp}/cad-bounds-keep.XXXXXX")
  : > "$keep"
  if [ -f "$EVID" ] && [ "$MODE" != smoke ]; then
    if [ "$MODE" = floor ]; then
      grep -vE "$FLOOR_PATTERN" "$EVID" > "$keep" || true
      cat "$LINES" "$keep" > "$EVID"
    else
      grep -E "$FLOOR_PATTERN" "$EVID" > "$keep" || true
      cat "$keep" "$LINES" > "$EVID"
    fi
  else
    cp "$LINES" "$EVID"
  fi
  rm -f "$keep"
}

finish() {
  local code=$1
  python3 "$WORK/tools/derive.py" emit "$MODE" "$MEAS" >> "$LINES"
  write_evidence
  grep -E '^(DERIVED|CHECK_ROW|GAP|STOP|SUMMARY),' "$LINES" >&2 || true
  exit "$code"
}

disk_guard() {
  local free
  free=$(vm_free_gib)
  if [ -z "$free" ] || [ "$free" -lt "$MIN_FREE_GIB" ]; then
    echo "STOP,reason=disk_below_threshold,free_gib=${free:-unknown},threshold_gib=$MIN_FREE_GIB" >&2
    if [ -n "${MEAS:-}" ] && [ -f "$MEAS" ]; then
      printf 'X,stop=disk_below_threshold,free_gib=%s\n' "${free:-unknown}" >> "$MEAS"
      finish 3
    fi
    exit 3
  fi
}

write_tools() {
  cat > "$WORK/tools/wrapline.py" <<'PY'
import json
import re
import sys

kind, row, rep, step, wrap, log, rc_w, out_bytes = sys.argv[1:9]
fields = {}
try:
    with open(wrap, errors="replace") as fh:
        for line in fh:
            if line.startswith("WRAP,"):
                fields = dict(p.split("=", 1) for p in line.strip().split(",")[1:] if "=" in p)
except OSError:
    pass
text = []
try:
    with open(log, "rb") as fh:
        text = [raw.decode("utf-8", "replace").strip() for raw in fh]
except OSError:
    pass
wrap_ok = 1 if fields and rc_w == "0" else 0
base = "row=%s,rep=%s,%s=%s,rc=%s,wall_s=%s,child_rss_mib=%s,cg_peak_mib=%s,oom_kill=%s,timeout=%s,wrap_ok=%d" % (
    row, rep, "stage" if kind == "M" else "step", step, fields.get("rc", "na"), fields.get("wall_s", "na"),
    fields.get("child_rss_mib", "na"), fields.get("cg_peak_mib", "na"), fields.get("oom_kill", "na"),
    fields.get("timeout", "na"), wrap_ok)


def clean(v):
    return re.sub(r"[,=\s]", "_", str(v))


if kind == "M":
    obj = {}
    for line in text:
        if line.startswith("CADVERDICT "):
            try:
                obj = json.loads(line[len("CADVERDICT "):])
            except ValueError:
                obj = {}
    verdict = obj.get("verdict", "none")
    if step not in ("guard", "dxf"):
        verdict = "na"
    reason = obj.get("reason") or "none"
    keys = ("expanded_inserts", "expanded_entities", "entities", "nesting", "blocks") if step == "guard" else \
        ("entities_drawn", "texts", "uncovered", "render_substituted", "hatch_truncated", "page_w_mm", "page_h_mm")
    detail = "+".join("%s:%s" % (k, clean(obj[k])) for k in keys if k in obj) or "-"
    print("M,%s,verdict=%s,reason=%s,out_bytes=%s,detail=%s" % (base, verdict, clean(reason), out_bytes, detail))
else:
    ok = "0"
    facts = "-"
    for line in text:
        if line.startswith(("GOSVG,", "GOPDF,", "GOJUDGE,")):
            parts = dict(p.split("=", 1) for p in line.split(",")[1:] if "=" in p)
            ok = parts.get("ok", "0")
            facts = "+".join("%s:%s" % (k, clean(v)) for k, v in parts.items() if k not in ("svg", "pdf", "png", "ok"))
    print("G,%s,ok=%s,facts=%s" % (base, ok, facts))
PY
  cat > "$WORK/tools/derive.py" <<'PY'
import math
import sys
from decimal import Decimal, ROUND_CEILING

LIMIT = Decimal("2842.3")
GO_MIB = Decimal("24.9")
MIB = 1048576
LADDER = [60000, 100000, 120000, 135000, 150000, 170031]
CAP_MAX = 170031
CEILINGS = [(118, "R118"), (100, "R100")]
GUARD_TIMEOUT_S = 30
GUARD_WALL_MAX = Decimal(20)
IO_S = 60
ENGINE_S = 886
PNG_MAX = 96 * MIB
STAGES = ["guard", "dxf", "rsvg_pdf", "rsvg_png_opaque", "rsvg_png_transparent"]
RSVG = ["rsvg_pdf", "rsvg_png_opaque", "rsvg_png_transparent"]
GO_STEPS = ["svg_client", "svg_render", "pdf", "pixel_opaque", "pixel_transparent"]
PIPELINE = ["guard", "dxf", "svg_client", "svg_render", "rsvg_pdf", "pdf", "rsvg_png_opaque", "rsvg_png_transparent",
            "pixel_opaque", "pixel_transparent"]
OOM_PEAK = Decimal(4096)


def kv(line):
    head, _, rest = line.partition(",")
    out = {"_": head}
    for part in rest.split(","):
        if "=" in part:
            k, v = part.split("=", 1)
            out[k] = v
    return out


def dec(v):
    try:
        return Decimal(v)
    except Exception:
        return None


def load(path):
    info, ms, gs, guard_rows, notes = {}, [], [], [], []
    with open(path) as fh:
        for line in fh:
            line = line.rstrip("\n")
            if not line:
                continue
            d = kv(line)
            if d["_"] == "I":
                info[d["row"]] = d
            elif d["_"] == "M":
                ms.append(d)
            elif d["_"] == "G":
                gs.append(d)
            elif d["_"] == "S":
                guard_rows.append(d)
            elif d["_"] == "X":
                notes.append(d)
    return info, ms, gs, guard_rows, notes


def stage_peak(m):
    vals = [dec(m.get("child_rss_mib")), dec(m.get("cg_peak_mib"))]
    vals = [v for v in vals if v is not None]
    p = max(vals) if vals else Decimal(0)
    if m.get("oom_kill") not in (None, "na", "0"):
        p = max(p, OOM_PEAK)
    return p


def oom(m):
    return m.get("oom_kill") not in (None, "na", "0")


class Model:
    def __init__(self, path):
        self.info, self.ms, self.gs, self.guard_rows, self.notes = load(path)

    def reps(self, row):
        return sorted({int(m["rep"]) for m in self.ms if m["row"] == row})

    def stages(self, row, rep=None):
        return [m for m in self.ms if m["row"] == row and (rep is None or int(m["rep"]) == rep)]

    def gos(self, row, rep=None):
        return [g for g in self.gs if g["row"] == row and (rep is None or int(g["rep"]) == rep)]

    def measured(self, row):
        return bool(self.stages(row))

    def status(self, row):
        if not self.measured(row):
            return "absent"
        if any(oom(m) for m in self.stages(row)):
            return "over_oom"
        for rep in self.reps(row):
            ss = {m["stage"]: m for m in self.stages(row, rep)}
            gg = {g["step"]: g for g in self.gos(row, rep)}
            for st in PIPELINE:
                if st in STAGES:
                    m = ss.get(st)
                    if m is None:
                        return "failed:%s_not_run:rep%d" % (st, rep)
                    if m["wrap_ok"] != "1" or m["rc"] != "0" or m.get("timeout") != "0":
                        return "failed:%s:rep%d" % (st, rep)
                    if st in ("guard", "dxf") and m["verdict"] != "ok":
                        return "failed:%s_verdict_%s:rep%d" % (st, m["verdict"], rep)
                else:
                    g = gg.get(st)
                    if g is None:
                        return "failed:%s_not_run:rep%d" % (st, rep)
                    if g["wrap_ok"] != "1" or g["rc"] != "0" or g["ok"] != "1":
                        return "failed:%s_judge:rep%d:%s" % (st, rep, g.get("facts", "-")[-160:])
        return "ok"

    def row_peak(self, row):
        return max((stage_peak(m) for m in self.stages(row)), default=Decimal(0))

    def stage_wall(self, row, stage):
        vals = [dec(m["wall_s"]) for m in self.stages(row) if m["stage"] == stage and dec(m["wall_s"]) is not None]
        return max(vals, default=None)

    def out_bytes(self, row, key):
        v = self.info.get(row, {}).get(key)
        return int(v) if v not in (None, "", "na") else None

    def go_sum(self, row):
        best = None
        for rep in self.reps(row):
            walls = [dec(g["wall_s"]) for g in self.gos(row, rep)]
            if walls and None not in walls:
                s = sum(walls)
                best = s if best is None or s > best else best
        return best


def ceil15(x):
    return int((x * Decimal("1.5")).to_integral_value(rounding=ROUND_CEILING))


def ceil_mib_15(b):
    need = (3 * b + 1) // 2
    return ((need + MIB - 1) // MIB) * MIB


def derive(model, mode):
    r = {"stop": None, "stop_detail": "", "cap": None, "cap_e": None, "ceiling": None, "ceiling_row": None,
         "admitted": [], "need": [], "not_run": []}
    for note in model.notes:
        if note.get("stop"):
            r["stop"] = note["stop"]
            r["stop_detail"] = ",".join("%s=%s" % (k, v) for k, v in note.items() if k not in ("_", "stop"))
            return r
    for row in model.info:
        st = model.status(row)
        if st.startswith("failed"):
            group = model.info[row].get("group", "")
            if group == "check" and model.stages(row) and any(
                    m["stage"] == "guard" and m["verdict"] not in ("ok",) and m["wrap_ok"] == "1" for m in model.stages(row)):
                r["stop"] = "check_row_not_admitted"
            else:
                r["stop"] = "stage_failed"
            r["stop_detail"] = "row=%s,status=%s" % (row, st)
            return r
    for d in model.guard_rows:
        if d.get("result") != "ok":
            r["stop"] = "guard_stress_wrong_verdict_or_failed"
            r["stop_detail"] = "shape=%s,rep=%s" % (d.get("shape"), d.get("rep"))
            return r
    if mode == "smoke":
        r["admitted"] = []
        return r
    cap = None
    for i, n in enumerate(LADDER):
        row = "L%d" % n
        if not model.measured(row):
            break
        over = model.status(row) == "over_oom" or model.row_peak(row) > LIMIT
        if over:
            if i == 0:
                r["stop"] = "smallest_rung_over_limit"
                r["stop_detail"] = "row=%s,row_peak_mib=%s" % (row, model.row_peak(row))
                return r
            r["not_run"] = ["L%d" % m for m in LADDER[i + 1:] if not model.measured("L%d" % m)]
            break
        cap = min(n, CAP_MAX)
    r["cap"] = cap
    r["cap_e"] = 5 * cap if cap is not None else None
    for mib, row in CEILINGS:
        if model.measured(row) and model.status(row) == "ok" and model.row_peak(row) <= LIMIT:
            r["ceiling"] = mib * MIB
            r["ceiling_row"] = row
            break
    if all(model.measured(row) for _, row in CEILINGS) and r["ceiling"] is None:
        r["stop"] = "ceiling_rows_over_limit"
        r["stop_detail"] = ";".join("%s:%s" % (row, model.row_peak(row)) for _, row in CEILINGS)
        return r
    if cap is not None and r["ceiling_row"] is not None:
        cap_row = "L%d" % cap
        r["need"] = [row for row in (cap_row, r["ceiling_row"]) if len(model.reps(row)) < 2]
    admitted = []
    if cap is not None:
        admitted += ["L%d" % n for n in LADDER if n <= cap and model.measured("L%d" % n)]
    if r["ceiling"] is not None:
        admitted += [row for mib, row in sorted(CEILINGS) if mib * MIB <= r["ceiling"] and model.measured(row)]
    for row in ("B", "C"):
        if not model.measured(row):
            continue
        inf = model.info[row]
        if inf.get("caps") != "%s+%s" % (r["cap"], r["cap_e"]):
            r["stop"] = "derivation_changed_after_check_rows"
            r["stop_detail"] = "row=%s,caps_used=%s,caps_now=%s+%s" % (row, inf.get("caps"), r["cap"], r["cap_e"])
            return r
        if model.row_peak(row) > LIMIT or model.status(row) == "over_oom":
            r["stop"] = "check_row_over_limit"
            r["stop_detail"] = "row=%s,row_peak_mib=%s" % (row, model.row_peak(row))
            return r
        if row == "C" and int(inf["bytes"]) > r["ceiling"]:
            r["stop"] = "check_row_not_admitted"
            r["stop_detail"] = "row=C,bytes=%s,ceiling_bytes=%s" % (inf["bytes"], r["ceiling"])
            return r
        admitted.append(row)
    r["admitted"] = admitted
    if not admitted:
        return r
    walls = {}
    for key, stages in (("dxf", ["dxf"]), ("rsvg", RSVG), ("guard", ["guard"])):
        best = (Decimal(-1), None, None)
        for row in admitted:
            for st in stages:
                w = model.stage_wall(row, st)
                if w is not None and w > best[0]:
                    best = (w, row, st)
        walls[key] = best
    for d in model.guard_rows:
        w = dec(d.get("wall_s"))
        if w is not None and w > walls["guard"][0]:
            walls["guard"] = (w, "stress_" + d["shape"], "guard")
    r["walls"] = walls
    r["dxf_t"] = ceil15(walls["dxf"][0])
    r["rsvg_t"] = ceil15(walls["rsvg"][0])
    r["sum"] = GUARD_TIMEOUT_S + r["dxf_t"] + r["rsvg_t"] + IO_S
    sizes = {}
    for key, cols in (("svg", ["svg_bytes", "render_svg_bytes"]), ("pdf", ["pdf_bytes"]),
                      ("png", ["png_opaque_bytes", "png_transparent_bytes"])):
        best = (-1, None, None)
        for row in admitted:
            for c in cols:
                b = model.out_bytes(row, c)
                if b is not None and b > best[0]:
                    best = (b, row, c)
        sizes[key] = best
    r["sizes"] = sizes
    r["svg_max"] = ceil_mib_15(sizes["svg"][0])
    r["pdf_max"] = ceil_mib_15(sizes["pdf"][0])
    go = (Decimal(-1), None)
    for row in admitted:
        s = model.go_sum(row)
        if s is not None and s > go[0]:
            go = (s, row)
    r["go"] = go
    r["gaps"] = []
    for row in admitted:
        for g in model.gos(row):
            v = dec(g["child_rss_mib"])
            if v is not None and v > GO_MIB:
                r["gaps"].append((row, g["rep"], g["step"], v))
    setters = {walls["dxf"][1], walls["rsvg"][1], sizes["svg"][1], sizes["pdf"][1]}
    setters.discard(None)
    r["need"] = sorted(set(r["need"]) | {row for row in setters if row in model.info and len(model.reps(row)) < 2})
    if walls["guard"][0] > GUARD_WALL_MAX:
        r["stop"] = "guard_wall_over_20s"
        r["stop_detail"] = "wall_s=%s,row=%s" % walls["guard"][:2]
        return r
    if r["sum"] > ENGINE_S:
        r["stop"] = "engine_sum_over_886"
        r["stop_detail"] = "sum_s=%d" % r["sum"]
        return r
    if sizes["png"][0] > PNG_MAX:
        r["stop"] = "png_over_96mib"
        r["stop_detail"] = "bytes=%d,row=%s" % sizes["png"][:2]
        return r
    if go[0] >= 0 and r["sum"] + go[0] > ENGINE_S:
        r["stop"] = "go_side_over_engine"
        r["stop_detail"] = "sum_s=%d,go_s=%s,row=%s" % (r["sum"], go[0], go[1])
        return r
    return r


def state(model, mode):
    r = derive(model, mode)
    print("STOP=%s" % (r["stop"] or ""))
    print("CAP=%s" % (r["cap"] if r["cap"] is not None else ""))
    print("CAPE=%s" % (r["cap_e"] if r["cap_e"] is not None else ""))
    print("CEIL=%s" % (r["ceiling"] if r["ceiling"] is not None else ""))
    print("CEILROW=%s" % (r["ceiling_row"] or ""))
    print("NEED='%s'" % " ".join(r["need"]))


def over(model, row):
    print("1" if model.status(row) == "over_oom" or model.row_peak(row) > LIMIT else "0")


def emit(model, mode):
    r = derive(model, mode)
    admitted = set(r["admitted"])
    for row, inf in model.info.items():
        if inf.get("gen"):
            print("GEN,row=%s,%s" % (row, inf["gen"].replace(";", ",")))
    for d in model.guard_rows:
        print("GUARD_ROW," + ",".join("%s=%s" % (k, v) for k, v in d.items() if k != "_"))
    for row, inf in model.info.items():
        rp = model.row_peak(row)
        for m in model.stages(row):
            p = stage_peak(m)
            print("BOUND_ROW,row=%s,group=%s,n=%s,bytes=%s,sha256=%s,stage=%s,rep=%s,rc=%s,wall_s=%s,child_rss_mib=%s,"
                  "cg_peak_mib=%s,oom_kill=%s,timeout=%s,peak_mib=%s,pct_of_budget=%.4f,row_peak_mib=%s,row_pct_of_budget=%.4f,"
                  "verdict=%s,out_bytes=%s,svg_bytes=%s,render_svg_bytes=%s,pdf_bytes=%s,png_opaque_bytes=%s,"
                  "png_transparent_bytes=%s,detail=%s,admitted=%d" % (
                      row, inf.get("group"), inf.get("n"), inf.get("bytes"), inf.get("sha256"), m["stage"], m["rep"],
                      m["rc"], m["wall_s"], m["child_rss_mib"], m["cg_peak_mib"], m["oom_kill"], m["timeout"], p,
                      p / LIMIT, rp, rp / LIMIT, m["verdict"], m["out_bytes"], inf.get("svg_bytes", "na"),
                      inf.get("render_svg_bytes", "na"), inf.get("pdf_bytes", "na"), inf.get("png_opaque_bytes", "na"),
                      inf.get("png_transparent_bytes", "na"), m["detail"], 1 if row in admitted else 0))
        for g in model.gos(row):
            print("GO_SIDE,row=%s,rep=%s,step=%s,rc=%s,ok=%s,wall_s=%s,child_rss_mib=%s,cg_peak_mib=%s,go_peak_mib=%s,"
                  "facts=%s,admitted=%d" % (row, g["rep"], g["step"], g["rc"], g["ok"], g["wall_s"], g["child_rss_mib"],
                                           g["cg_peak_mib"], g["child_rss_mib"], g["facts"], 1 if row in admitted else 0))
    for row in r["not_run"]:
        print("NOT_RUN,row=%s,reason=above_the_first_rung_over_LIMIT(rule_b)" % row)
    for row, inf in model.info.items():
        if inf.get("group") != "check":
            continue
        st = model.status(row)
        ok = st == "ok" and model.row_peak(row) <= LIMIT and (mode == "smoke" or row in admitted)
        print("CHECK_ROW,row=%s,shape=%s,cells=%s,block_entities=%s,bytes=%s,sha256=%s,guard_caps=%s,guard_counts=%s,"
              "row_peak_mib=%s,pct_of_budget=%.4f,status=%s,result=%s" % (
                  row, inf.get("shape"), inf.get("n"), inf.get("block_entities"), inf.get("bytes"), inf.get("sha256"),
                  inf.get("caps"), inf.get("counts"), model.row_peak(row), model.row_peak(row) / LIMIT, st,
                  "ok" if ok else "fail"))
    if mode == "run" and r["stop"] is None and r.get("walls"):
        w, s = r["walls"], r["sizes"]
        cap_row = "L%d" % r["cap"]
        print("DERIVED,key=cad_dxf_peak_limit_mib,value=2842.3,derivation=0.70x4096-24.9=2842.3(R-7_budget_itself)")
        print("DERIVED,key=cad_dxf_ceiling_bytes,value=%d,derivation=rule_d:%s_row_peak_%s_MiB<=2842.3;%d_MiB_x_1048576=%d" % (
            r["ceiling"], r["ceiling_row"], model.row_peak(r["ceiling_row"]), r["ceiling"] // MIB, r["ceiling"]))
        print("DERIVED,key=cad_dxf_expanded_inserts_cap,value=%d,derivation=rule_b:largest_rung_with_it_and_every_smaller_rung<=2842.3;%s_row_peak_%s_MiB;clamp_170031" % (
            r["cap"], cap_row, model.row_peak(cap_row)))
        print("DERIVED,key=cad_dxf_expanded_entities_cap,value=%d,derivation=rule_c:5x%d=%d" % (r["cap_e"], r["cap"], r["cap_e"]))
        print("DERIVED,key=cad_dxf_timeout_s,value=%d,derivation=rule_f:ceil(1.5x%s_s)=%d;max_cad-dxf_wall_over_admitted_rows_at_%s" % (
            r["dxf_t"], w["dxf"][0], r["dxf_t"], w["dxf"][1]))
        print("DERIVED,key=cad_rsvg_timeout_s,value=%d,derivation=rule_f:ceil(1.5x%s_s)=%d;max_rsvg-convert_wall_over_admitted_rows_and_three_variants_at_%s_%s" % (
            r["rsvg_t"], w["rsvg"][0], r["rsvg_t"], w["rsvg"][1], w["rsvg"][2]))
        print("DERIVED,key=cad_dxf_svg_max_bytes,value=%d,derivation=rule_g:ceil(1.5x%d)=%d_rounded_up_to_%d_MiB;max_helper_svg_over_admitted_rows_at_%s_%s" % (
            r["svg_max"], s["svg"][0], (3 * s["svg"][0] + 1) // 2, r["svg_max"] // MIB, s["svg"][1], s["svg"][2]))
        print("DERIVED,key=cad_dxf_pdf_max_bytes,value=%d,derivation=rule_g:ceil(1.5x%d)=%d_rounded_up_to_%d_MiB;max_pdf_over_admitted_rows_at_%s" % (
            r["pdf_max"], s["pdf"][0], (3 * s["pdf"][0] + 1) // 2, r["pdf_max"] // MIB, s["pdf"][1]))
        print("CHECK,key=guard_timeout,value=30,max_guard_wall_s=%s,at=%s,rule=1.5xmax<=30,result=ok" % (w["guard"][0], w["guard"][1]))
        print("CHECK,key=engine_sum,value=%d,arithmetic=30+%d+%d+60=%d,bound=886,result=ok" % (r["sum"], r["dxf_t"], r["rsvg_t"], r["sum"]))
        print("CHECK,key=png_bytes,max=%d,at=%s_%s,bound=%d,result=ok" % (s["png"][0], s["png"][1], s["png"][2], PNG_MAX))
        print("CHECK,key=go_side,max_go_wall_sum_s=%s,at=%s,engine_sum_plus_go_s=%s,bound=886,result=ok" % (
            r["go"][0], r["go"][1], r["sum"] + r["go"][0]))
        for row, rep, step, v in r["gaps"]:
            print("GAP,key=go_worker_rss_dxf,row=%s,rep=%s,step=%s,go_peak_mib=%s,go_worker_mib=24.9,owner=phase_76" % (row, rep, step, v))
    rows = len(model.info)
    if r["stop"]:
        print("STOP,mode=%s,reason=%s,%s" % (mode, r["stop"], r["stop_detail"]))
        print("SUMMARY,mode=%s,rows=%d,admitted=%d,stop=%s,result=stop" % (mode, rows, len(admitted), r["stop"]))
    elif mode == "run" and not r.get("walls"):
        print("SUMMARY,mode=run,rows=%d,admitted=%d,stop=none,result=incomplete" % (rows, len(admitted)))
    elif mode == "run":
        print("SUMMARY,mode=run,rows=%d,admitted=%d,cap_inserts=%d,cap_entities=%d,ceiling_bytes=%d,dxf_timeout_s=%d,rsvg_timeout_s=%d,engine_sum_s=%d,stop=none,result=ok" % (
            rows, len(admitted), r["cap"], r["cap_e"], r["ceiling"], r["dxf_t"], r["rsvg_t"], r["sum"]))
    else:
        print("SUMMARY,mode=smoke,rows=%d,bound_rows=%d,go_side=%d,derived=0,stop=none,result=ok" % (rows, len(model.ms), len(model.gs)))


if __name__ == "__main__":
    cmd, mode_or_row, path = sys.argv[1:4]
    model = Model(path)
    if cmd == "state":
        state(model, mode_or_row)
    elif cmd == "over":
        over(model, mode_or_row)
    elif cmd == "emit":
        emit(model, mode_or_row)
    else:
        sys.exit(64)
PY
  chmod 0644 "$WORK/tools/"*.py
}

rule_lines() {
  cat <<'RULES'
RULE,key=limit,mode=all,rule=LIMIT=0.70x4096-24.9=2842.3_MiB_the_budget_itself_(R-7_corrected;not_70%_of_it);stage_peak=max(child_rss_mib;cg_peak_mib)_and_at_least_4096_when_the_cgroup_OOM-killed_the_stage;row_peak=max_over_reps_of_the_guard;cad-dxf;rsvg_pdf;rsvg_png_opaque;rsvg_png_transparent_peaks;every_BOUND_ROW_records_pct_of_budget=peak/2842.3
RULE,key=a,mode=all,rule=LIMIT_2842.3_MiB;pct_of_budget=row_peak/2842.3_on_every_row
RULE,key=b,mode=run,rule=cap_inserts=the_largest_ladder_n_in_60000;100000;120000;135000;150000;170031_whose_row_peak_and_every_smaller_rung_row_peak_are_at_most_LIMIT_clamped_to_170031;rungs_above_the_first_rung_over_LIMIT_are_not_run_(NOT_RUN);STOP_smallest_rung_over_limit_if_60000_is_over
RULE,key=c,mode=run,rule=cap_entities=5xcap_inserts_(R-8_derivation_of_850155_from_the_5-entity_series_carried_to_the_new_cap)
RULE,key=d,mode=run,rule=ceiling=118_MiB_(123731968_bytes)_if_the_118_MiB_replicate_row_peak_is_at_most_LIMIT;else_100_MiB_(104857600)_if_the_100_MiB_row_is;else_STOP_ceiling_rows_over_limit
RULE,key=e,mode=run,rule=after_b-d:B=dxf-minsert_with_a_50-entity_block_at_min(cap_inserts;floor((cap_entities-1)/50))_cells;C=dxf-combo_on_the_replicate_at_the_derived_ceiling_with_min(cap_inserts-I;floor((cap_entities-E-1)/5))_cells_where_I_and_E_are_the_replicate_expanded_inserts_and_entities_from_the_guard;the_-1_is_the_MINSERT_entity_itself_which_the_guard_counts_once;the_guard_with_the_derived_caps_must_say_ok_for_both;C_bytes_at_most_the_ceiling;both_row_peaks_at_most_LIMIT;else_STOP_check_row_not_admitted_or_check_row_over_limit;no_remaining_cells_is_STOP_no_remaining_expansion
RULE,key=f,mode=run,rule=admitted=ladder_rungs_up_to_cap_inserts+ceiling_rows_up_to_the_ceiling+B+C;cad_dxf_timeout_s=ceil(1.5xmax_cad-dxf_wall_over_admitted);cad_rsvg_timeout_s=ceil(1.5xmax_rsvg_wall_over_admitted_and_pdf;png_opaque;png_transparent);the_guard_keeps_30_s_only_if_1.5xmax_guard_wall_over_admitted_rows_and_the_four_75-03_stress_shapes_at_118_MiB_is_at_most_30_else_STOP;30+cad_dxf_timeout_s+cad_rsvg_timeout_s+60_at_most_886_else_STOP
RULE,key=g,mode=run,rule=cad_dxf_svg_max_bytes=ceil(1.5xmax_helper_svg_bytes_(client_and_render-only)_over_admitted)_rounded_up_to_a_whole_MiB;cad_dxf_pdf_max_bytes=the_same_over_pdf_bytes;every_admitted_png_at_most_96_MiB_(cadPixelMaxBytes)_else_STOP
RULE,key=h,mode=all,rule=Go_side:wall_and_peak_of_cad-svg-check_(client_and_render-only);cad-pdf-check;cad-pixel-judge_--drawing_(opaque_and_transparent)_per_row_and_rep;go_peak_mib=child_rss_mib_(the_process_RSS);30+cad_dxf_timeout_s+cad_rsvg_timeout_s+60+max_Go_wall_sum_over_admitted_rows_at_most_886_else_STOP;a_Go_peak_above_24.9_MiB_is_GAP_go_worker_rss_dxf_for_phase_76_not_a_stop
RULE,key=reps,mode=run,rule=rep_1_for_every_row;rep_2_for_the_rows_that_set_a_DERIVED_value_(the_cap_rung;the_ceiling_row;the_rows_of_the_max_cad-dxf_wall;max_rsvg_wall;max_svg_bytes;max_pdf_bytes);peak_and_wall_of_a_row_and_stage_are_maxima_over_reps;a_rep_2_that_changes_b-d_after_the_check_rows_is_STOP_derivation_changed_after_check_rows
RULE,key=failures,mode=all,rule=every_stage_must_exit_0_within_its_1800_s_wrap_budget_with_guard_and_cad-dxf_verdict_ok_and_every_Go_judge_ok_else_STOP_stage_failed;a_stage_killed_by_the_cgroup_OOM_counts_as_over_LIMIT_(not_a_stop)_and_ends_the_row;nothing_is_extrapolated_upward
RULE,key=r10,mode=all,rule=R-10_(added_2026-10-11_before_any_row_of_this_run):cad-pdf-check_judges_with_--svg_the_validated_render-only_SVG;every_text_whose_rendered_size_on_the_PDF_page_is_below_F=cad_dxf_pdf_text_floor_(the_Go_constant_pinned_to_the_floor-mode_DERIVED_line)_is_excluded_from_the_R-3_expectation_and_counted_below_render_floor;every_text_at_or_above_F_must_be_found_in_order;the_teeth_rows_must_reject_an_altered_text_above_F_and_accept_an_altered_text_below_F
RULE,key=rows,mode=all,rule=measurement_rows_run_the_guard_with_caps_ten_times_the_row_counts_from_an_unmeasured_counting_pass_and_cad-dxf_with_--max-drawn-entities_ten_times_the_expanded_entities;check_rows_run_the_guard_with_the_derived_caps;rsvg_reads_the_Go-re-serialized_render-only_SVG_(R-9);letterbox_from_cad-pixel-judge_--print-letterbox_at_2048;outputs_deleted_after_each_row
RULES
}

setup() {
  local mode=$1
  need_tool docker
  need_tool go
  need_tool python3
  need_tool shasum
  [ "$(docker version --format '{{.Server.Arch}}')" = arm64 ] || fail "docker server is not arm64"
  [ "$(GOTOOLCHAIN=$GO_TOOLCHAIN go env GOVERSION)" = "$GO_TOOLCHAIN" ] || fail "go toolchain is not $GO_TOOLCHAIN"
  disk_guard
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-dxf-bounds.XXXXXX")
  chmod 0755 "$WORK"
  trap cleanup EXIT
  mkdir -p "$WORK/wrap" "$WORK/tools" "$WORK/gen"
  chmod 0755 "$WORK/wrap" "$WORK/tools"
  chmod 0777 "$WORK/gen"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
  chmod 0644 "$WORK/wrap/measure_wrap.py"
  write_tools
  LINES="$WORK/lines.txt"
  MEAS="$WORK/meas.txt"
  : > "$MEAS"
  if [ "$mode" = floor ]; then floor_rule_lines > "$LINES"; else rule_lines > "$LINES"; fi
  TAG=$(build_engine)
  BUILT_TAGS+=("$TAG")
  cad_require_native "$TAG" || fail "engine image is not native"
  ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
  TRIPLET=$(triplet_for "$ARCH")
  DXF_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/share/fonts:/usr/lib/$TRIPLET:/usr/local/bin/cad-dxf"
  RSVG_RO="/usr/share/fonts:/usr/lib/$TRIPLET:/usr/bin/rsvg-convert"
  build_go "$ARCH" "$WORK/bin"
  {
    printf 'HDR,mode=%s,date_utc=%s\n' "$mode" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'HDR,mode=%s,commit=%s,tracked_dirt=%s\n' "$mode" "$(git rev-parse HEAD)" "$(tracked_dirt)"
    printf 'HDR,mode=%s,image=%s,image_id=%s,server_arch=%s\n' "$mode" "$TAG" "$(docker image inspect "$TAG" --format '{{.Id}}')" \
      "$(docker version --format '{{.Server.Arch}}')"
    printf 'HDR,mode=%s,cad_dxf_sha256=%s,cad_guard_sha256=%s,generator_sha256=%s\n' "$mode" \
      "$(sha_of cmd/cad-worker/helper/cad-dxf)" "$(sha_of cmd/cad-worker/helper/cad-guard)" "$(sha_of scripts/cad-corpus-gen.py)"
    printf 'HDR,mode=%s,versions=%s\n' "$mode" "$(docker run --rm --network none --entrypoint sh "$TAG" -c \
      'printf "librsvg2-bin_%s+ezdxf_%s" "$(dpkg-query -W -f="\${Version}" librsvg2-bin)" "$(/opt/venv/bin/python -I -c "import importlib.metadata as m; print(m.version(\"ezdxf\"))")"' </dev/null)"
    printf 'HDR,mode=%s,limits=cpus2_memory4g_swap4g_network_none_user_nobody,env=HOME=/job/.home_XDG_CACHE_HOME=/job/.home/.cache_XDG_CONFIG_HOME=/job/.home/.config_TMPDIR=/job,go=%s,size=%s\n' \
      "$mode" "$GO_TOOLCHAIN" "$SIZE"
    printf 'HDR,mode=%s,ro_cad_dxf=%s,ro_rsvg=%s,rw=/job,guard=unconfined_/usr/bin/python3_-IB_as_in_75-03,go_clis=unconfined_static_linux_%s\n' \
      "$mode" "$DXF_RO" "$RSVG_RO" "$ARCH"
  } >> "$LINES"
}

gen() {
  local out
  out=$(docker run --rm "${LIMITS[@]}" -e HOME=/tmp -e PYTHONHASHSEED=0 -v "$REPO_ROOT:/src:ro" -v "$WORK/gen:/out" \
    ${GEN_MOUNTS[@]+"${GEN_MOUNTS[@]}"} -w /src --entrypoint /opt/venv/bin/python "$TAG" -B scripts/cad-corpus-gen.py \
    "$@" --scaled-dir /out </dev/null) || { echo "$out" >&2; fail "generator failed: $*"; }
  printf '%s\n' "$out"
}

scaled_field() { printf '%s' "$2" | tr ',' '\n' | sed -n "s/^$1=//p" | head -n 1; }

stash_gen() {
  local name=$1 dest
  mkdir -p "$CAD_CACHE/scaled"
  dest="$CAD_CACHE/scaled/$name"
  mv -f "$WORK/gen/$name" "$dest"
  printf '%s\n' "$dest"
}

replicate_src() {
  local target=$1 path out
  path="$CAD_CACHE/scaled/dxf-replicate-uncommon-${target}MiB.dxf"
  if [ ! -s "$path" ]; then
    GEN_MOUNTS=()
    out=$(gen --scaled-from "$BASE_DXF" --kind dxf-replicate --targets "$target")
    log "$out"
    path=$(stash_gen "dxf-replicate-uncommon-${target}MiB.dxf")
  fi
  printf '%s\n' "$path"
}

far_point_src() {
  local d=$1 path out
  path="$CAD_CACHE/scaled/dxf-farpoint-uncommon-d$d.dxf"
  if [ ! -s "$path" ]; then
    GEN_MOUNTS=()
    out=$(gen --scaled-from "$BASE_DXF" --kind dxf-far-point --targets "$d")
    log "$out"
    path=$(stash_gen "dxf-farpoint-uncommon-d$d.dxf")
  fi
  printf '%s\n' "$path"
}

stress_src() {
  local shape=$1 path
  path="$CAD_CACHE/scaled/dxf-guard-stress-$shape-118MiB.dxf"
  if [ ! -s "$path" ]; then
    mkdir -p "$CAD_CACHE/scaled"
    python3 scripts/cad-corpus-gen.py --kind dxf-guard-stress --shape "$shape" --targets 118 --scaled-dir "$CAD_CACHE/scaled" >&2
  fi
  printf '%s\n' "$path"
}

verdict_json() {
  python3 - "$1" <<'PY'
import json, sys
obj = {}
with open(sys.argv[1], "rb") as fh:
    for raw in fh:
        line = raw.decode("ascii", "replace").strip()
        if line.startswith("CADVERDICT "):
            obj = json.loads(line[len("CADVERDICT "):])
print("%s %s %s" % (obj.get("verdict", "none"), obj.get("expanded_inserts", -1), obj.get("expanded_entities", -1)))
PY
}

count_pass() {
  local src=$1 job out
  job=$(new_job)
  cp "$src" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  docker run --rm "${LIMITS[@]}" -v "$job:/job" --entrypoint /usr/bin/python3 "$TAG" -IB /usr/local/bin/cad-guard \
    --format dxf --max-expanded-inserts "$COUNT_CAP" --max-expanded-entities "$COUNT_CAP" --max-nesting "$CAP_NESTING" \
    --max-abs-coordinate "$CAP_ABS" /job/in.dxf > "$job/count.out" 2>/dev/null </dev/null || true
  out=$(verdict_json "$job/count.out")
  rm -rf "$job"
  printf '%s\n' "$out"
}

wrapped() {
  local kind=$1 row=$2 rep=$3 step=$4 job=$5 confine=$6 out=$7 rc_w=0 dir
  shift 7
  dir="$job/w_$step"
  mkdir -p "$dir"
  chmod 0777 "$dir"
  if [ "$confine" = none ]; then
    docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -v "$WORK/bin:/gobin:ro" -v "$WORK/wrap:/wrap:ro" -v "$job:/job" \
      --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py --budget-s "$STAGE_BUDGET_S" --outdir "/job/w_$step" -- \
      "$@" > "$dir/wrap.out" 2>/dev/null </dev/null || rc_w=$?
  else
    docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -e "PROBE_RO=$confine" -e "PROBE_RW=/job" -v "$WORK/bin:/gobin:ro" \
      -v "$WORK/wrap:/wrap:ro" -v "$job:/job" --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py \
      --budget-s "$STAGE_BUDGET_S" --outdir "/job/w_$step" -- /gobin/sandbox-probe "$@" > "$dir/wrap.out" 2>/dev/null </dev/null || rc_w=$?
  fi
  local ob=na
  [ -z "$out" ] || [ ! -f "$job/$out" ] || ob=$(bytes_of "$job/$out")
  python3 "$WORK/tools/wrapline.py" "$kind" "$row" "$rep" "$step" "$dir/wrap.out" "$dir/child.log" "$rc_w" "$ob" | tee -a "$MEAS" >/dev/null
  tail -n 1 "$MEAS" | grep -q ',rc=0,' && tail -n 1 "$MEAS" | grep -q ',wrap_ok=1' || {
    tail -n 5 "$dir/child.log" >&2 2>/dev/null || true
    return 1
  }
  case "$kind" in
    G) tail -n 1 "$MEAS" | grep -q ',ok=1,' || return 1 ;;
    M) case "$step" in guard|dxf) tail -n 1 "$MEAS" | grep -q ',verdict=ok,' || return 1 ;; esac ;;
  esac
  return 0
}

measure_row() {
  local row=$1 rep=$2 src=$3 group=$4 n=$5 caps_mode=$6 extra=$7 job ins ents counts cap_i cap_e verdict ci ce
  local lb sw sh left top
  disk_guard
  log "row $row rep $rep ($(bytes_of "$src") bytes)"
  job=$(new_job)
  cp -c "$src" "$job/in.dxf" 2>/dev/null || cp "$src" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  read -r verdict ci ce <<< "$(count_pass "$job/in.dxf")"
  counts="inserts:$ci+entities:$ce+verdict:$verdict"
  if [ "$caps_mode" = ten ]; then
    cap_i=$(( (ci > 1 ? ci : 1) * 10 )); cap_e=$(( (ce > 1 ? ce : 1) * 10 ))
  else
    cap_i=${caps_mode%+*}; cap_e=${caps_mode#*+}
  fi
  if [ "$rep" = 1 ]; then
    printf 'I,row=%s,group=%s,n=%s,bytes=%s,sha256=%s,file=%s,counts=%s,caps=%s,guard_caps_used=%s+%s%s\n' "$row" "$group" "$n" \
      "$(bytes_of "$src")" "$(sha_of "$src")" "$(basename "$src")" "$counts" \
      "$( [ "$caps_mode" = ten ] && echo ten_x_counts || echo "$caps_mode")" "$cap_i" "$cap_e" "$extra" >> "$MEAS"
  fi
  local ok=1
  wrapped M "$row" "$rep" guard "$job" none "" /usr/bin/python3 -IB /usr/local/bin/cad-guard --format dxf \
    --max-expanded-inserts "$cap_i" --max-expanded-entities "$cap_e" --max-nesting "$CAP_NESTING" --max-abs-coordinate "$CAP_ABS" \
    /job/in.dxf || ok=0
  if [ "$ok" = 1 ]; then
    wrapped M "$row" "$rep" dxf "$job" "$DXF_RO" draw.svg /usr/local/bin/cad-dxf \
      --max-drawn-entities "$(( (ce > 1 ? ce : 1) * 10 ))" --texts /job/draw.texts.json --render-svg /job/draw.render.svg \
      --measure /job/measure.json /job/in.dxf /job/draw.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped G "$row" "$rep" svg_client "$job" none clean.svg /gobin/cad-svg-check --out /job/clean.svg /job/draw.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped G "$row" "$rep" svg_render "$job" none clean.render.svg /gobin/cad-svg-check --out /job/clean.render.svg /job/draw.render.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped M "$row" "$rep" rsvg_pdf "$job" "$RSVG_RO" out.pdf /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/clean.render.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped G "$row" "$rep" pdf "$job" none "" /gobin/cad-pdf-check --texts /job/draw.texts.json --svg /job/clean.render.svg /job/out.pdf || ok=0
  fi
  if [ "$ok" = 1 ]; then
    lb=$(docker run --rm "${LIMITS[@]}" -v "$WORK/bin:/gobin:ro" -v "$job:/job" --entrypoint /gobin/cad-pixel-judge "$TAG" \
      --print-letterbox --svg /job/clean.render.svg --size "$SIZE" </dev/null) || ok=0
    sw=$(scaled_field sw "$lb"); sh=$(scaled_field sh "$lb"); left=$(scaled_field left "$lb"); top=$(scaled_field top "$lb")
    [ -n "$sw" ] || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped M "$row" "$rep" rsvg_png_opaque "$job" "$RSVG_RO" out_o.png /usr/bin/rsvg-convert -b white -w "$sw" -h "$sh" \
      --page-width "$SIZE" --page-height "$SIZE" --left "$left" --top "$top" -o /job/out_o.png /job/clean.render.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped M "$row" "$rep" rsvg_png_transparent "$job" "$RSVG_RO" out_t.png /usr/bin/rsvg-convert -w "$sw" -h "$sh" \
      --page-width "$SIZE" --page-height "$SIZE" --left "$left" --top "$top" -o /job/out_t.png /job/clean.render.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped G "$row" "$rep" pixel_opaque "$job" none "" /gobin/cad-pixel-judge --drawing --letterbox-svg /job/clean.render.svg \
      --size "$SIZE" --background opaque /job/out_o.png || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped G "$row" "$rep" pixel_transparent "$job" none "" /gobin/cad-pixel-judge --drawing --letterbox-svg /job/clean.render.svg \
      --size "$SIZE" --background transparent /job/out_t.png || ok=0
  fi
  if [ "$rep" = 1 ] && [ "$ok" = 1 ]; then
    python3 - "$MEAS" "$row" "$(bytes_of "$job/draw.svg")" "$(bytes_of "$job/draw.render.svg")" "$(bytes_of "$job/clean.svg")" \
      "$(bytes_of "$job/out.pdf")" "$(bytes_of "$job/out_o.png")" "$(bytes_of "$job/out_t.png")" "$sw" "$sh" "$left" "$top" <<'PY'
import sys
path, row = sys.argv[1:3]
svg, rsvg, clean, pdf, pngo, pngt, sw, sh, left, top = sys.argv[3:13]
lines = open(path).read().splitlines()
for i, line in enumerate(lines):
    if line.startswith("I,row=%s," % row):
        lines[i] = line + ",svg_bytes=%s,render_svg_bytes=%s,clean_svg_bytes=%s,pdf_bytes=%s,png_opaque_bytes=%s,png_transparent_bytes=%s,letterbox=%sx%s+%s+%s" % (
            svg, rsvg, clean, pdf, pngo, pngt, sw, sh, left, top)
open(path, "w").write("\n".join(lines) + "\n")
PY
  fi
  rm -rf "$job"
  return 0
}

check_state() {
  local mode=$1
  eval "$(python3 "$WORK/tools/derive.py" state "$mode" "$MEAS")"
  if [ -n "$STOP" ]; then
    log "STOP $STOP"
    finish 1
  fi
}

stress_rows() {
  local shape src rep job wrapout rc_w expect
  for shape in "${STRESS_SHAPES[@]}"; do
    src=$(stress_src "$shape")
    case "$shape" in
      chain) expect=too_complex/nesting_too_deep ;;
      fanout|wide) expect=too_complex/expanded_inserts_over_cap ;;
      *) expect=ok ;;
    esac
    for rep in $(seq 1 "$STRESS_REPS"); do
      disk_guard
      job=$(new_job)
      rc_w=0
      docker run --rm "${LIMITS[@]}" -v "$src:/in/in.dxf:ro" -v "$WORK/wrap:/wrap:ro" -v "$job:/job" \
        --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py --budget-s 120 --outdir /job -- \
        /usr/bin/python3 -IB /usr/local/bin/cad-guard --format dxf --max-expanded-inserts "$SMOKE_CAP_INSERTS" \
        --max-expanded-entities "$SMOKE_CAP_ENTITIES" --max-nesting "$CAP_NESTING" --max-abs-coordinate "$CAP_ABS" \
        /in/in.dxf > "$job/wrap.out" 2>/dev/null </dev/null || rc_w=$?
      python3 - "$shape" "$rep" "$src" "$job/wrap.out" "$job/child.log" "$rc_w" "$expect" "$(bytes_of "$src")" "$(sha_of "$src")" <<'PY' >> "$MEAS"
import json
import sys

shape, rep, src, wrap, log, rc_w, expect, size, digest = sys.argv[1:10]
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
got = verdict if verdict == "ok" else "%s/%s" % (verdict, obj.get("reason") or "none")
peak = max(float(fields.get("child_rss_mib", "inf")), float(fields.get("cg_peak_mib", "inf")))
ok = rc_w == "0" and fields.get("timeout") == "0" and fields.get("oom_kill") == "0" and got == expect
print("S,shape=%s,rep=%s,file=%s,bytes=%s,sha256=%s,got=%s,expect=%s,caps=170031+850155,wall_s=%s,child_rss_mib=%s,cg_peak_mib=%s,peak_mib=%s,pct_of_budget=%.4f,result=%s" % (
    shape, rep, src.rsplit("/", 1)[-1], size, digest, got, expect, fields.get("wall_s", "na"),
    fields.get("child_rss_mib", "na"), fields.get("cg_peak_mib", "na"), peak, peak / 2842.3, "ok" if ok else "fail"))
PY
      rm -rf "$job"
      log "stress $shape rep $rep: $(tail -n 1 "$MEAS" | tr ',' '\n' | grep -E '^(got|wall_s|result)=' | tr '\n' ' ')"
    done
  done
}

gen_minsert() {
  local targets=$1 block=$2 out line name n
  GEN_MOUNTS=()
  if [ "$block" = 5 ]; then
    out=$(gen --scaled-from "$BASE_DXF" --kind dxf-minsert --targets "$targets")
  else
    out=$(gen --scaled-from "$BASE_DXF" --kind dxf-minsert --targets "$targets" --block-entities "$block")
  fi
  printf '%s\n' "$out" | grep '^SCALED,' > "$WORK/gen.lines" || fail "no SCALED line from dxf-minsert"
  while IFS= read -r line; do
    name=$(scaled_field file "$line")
    n=$(scaled_field expanded "$line")
    stash_gen "$name" > /dev/null
    printf '%s\t%s\t%s\n' "$n" "$CAD_CACHE/scaled/$name" "$(printf '%s' "$line" | tr ',' ';')"
  done < "$WORK/gen.lines"
}

gen_combo() {
  local base=$1 cells=$2 out line name
  GEN_MOUNTS=(-v "$base:/base/$(basename "$base"):ro")
  out=$(gen --scaled-from "/base/$(basename "$base")" --kind dxf-combo --cells "$cells")
  line=$(printf '%s\n' "$out" | grep '^SCALED,' | tail -n 1)
  [ -n "$line" ] || fail "no SCALED line from dxf-combo"
  name=$(scaled_field file "$line")
  stash_gen "$name" > /dev/null
  printf '%s\t%s\n' "$CAD_CACHE/scaled/$name" "$(printf '%s' "$line" | tr ',' ';')"
}

run_ladder_row() {
  local n=$1 rep=$2 path=${LADDER_PATH[$3]} gline=${LADDER_GEN[$3]}
  measure_row "L$n" "$rep" "$path" ladder "$n" ten ",gen=$gline"
}

rep2_rows() {
  local row i n guard=0
  while [ -n "$NEED" ]; do
    guard=$((guard + 1))
    [ "$guard" -le 12 ] || fail "rep 2 loop did not settle: $NEED"
    for row in $NEED; do
      case "$row" in
        L*) i=0; for n in "${LADDER[@]}"; do [ "L$n" != "$row" ] || run_ladder_row "$n" 2 "$i"; i=$((i + 1)); done ;;
        R*) measure_row "$row" 2 "$(replicate_src "${row#R}")" ceiling "${row#R}MiB" ten "" ;;
        B) measure_row B 2 "$B_PATH" check "$CELLS_B" "$CAP+$CAPE" "" ;;
        C) measure_row C 2 "$C_PATH" check "$CELLS_C" "$CAP+$CAPE" "" ;;
      esac
    done
    check_state run
  done
}

teeth_row() {
  local name=$1 src=$2 side=$3 job pick idx size got
  disk_guard
  log "teeth $name"
  job=$(new_job)
  cp -c "$src" "$job/in.dxf" 2>/dev/null || cp "$src" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -v "$WORK/bin:/gobin:ro" -v "$job:/job" --entrypoint sh "$TAG" -c \
    '/usr/local/bin/cad-dxf --max-drawn-entities 100000000 --texts /job/draw.texts.json --render-svg /job/draw.render.svg /job/in.dxf /job/draw.svg >/dev/null 2>&1 &&
     /gobin/cad-svg-check --out /job/clean.render.svg /job/draw.render.svg >/dev/null &&
     /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/clean.render.svg &&
     /gobin/cad-pdf-check --texts /job/draw.texts.json --svg /job/clean.render.svg /job/out.pdf > /job/plain.out;
     /gobin/cad-pdf-check --report --texts /job/draw.texts.json --svg /job/clean.render.svg /job/out.pdf > /job/report.out' </dev/null || true
  pick=$(python3 - "$job" "$side" <<'PY'
import json
import sys

job, side = sys.argv[1:3]
floor = None
for ln in open(job + "/plain.out"):
    if ln.startswith("GOPDF,"):
        parts = dict(p.split("=", 1) for p in ln.strip().split(",")[1:] if "=" in p)
        floor = float(parts["floor_pt"])
rows = []
for ln in open(job + "/report.out"):
    if ln.startswith("REPORT,"):
        parts = dict(p.split("=", 1) for p in ln.strip().split(",")[1:] if "=" in p)
        rows.append((int(parts["i"]), float(parts["svg_pt"]), parts["present"]))
cands = [r for r in rows if r[2] == "1" and r[1] >= 10 * floor] if side == "above" else [r for r in rows if r[2] in ("0", "1") and r[1] < floor]
if not cands:
    print("none 0")
    sys.exit(0)
i, size, _ = cands[0]
doc = json.load(open(job + "/draw.texts.json"))
doc["texts"][i] = {"text": "☃☃", "covered": "☃☃"}
open(job + "/altered.texts.json", "w").write(json.dumps(doc, separators=(",", ":"), ensure_ascii=True))
print(i, "%.6g" % size)
PY
)
  read -r idx size <<< "$pick"
  if [ "$idx" = none ]; then
    printf 'TEETH,name=%s,side=%s,file=%s,sha256=%s,result=fail,reason=no_text_on_this_side_of_the_floor\n' "$name" "$side" "$(basename "$src")" "$(sha_of "$src")" >> "$LINES"
    printf 'X,stop=teeth_failed,name=%s\n' "$name" >> "$MEAS"
    rm -rf "$job"
    return 0
  fi
  docker run --rm "${LIMITS[@]}" -v "$WORK/bin:/gobin:ro" -v "$job:/job" --entrypoint /gobin/cad-pdf-check "$TAG" \
    --texts /job/altered.texts.json --svg /job/clean.render.svg /job/out.pdf > "$job/altered.out" </dev/null || true
  got=$(python3 - "$job" "$side" "$name" "$idx" "$size" "$(basename "$src")" "$(sha_of "$src")" <<'PY'
import sys

job, side, name, idx, size, fname, digest = sys.argv[1:8]
def facts(path):
    for ln in open(path):
        if ln.startswith("GOPDF,"):
            return dict(p.split("=", 1) for p in ln.strip().split(",")[1:] if "=" in p)
    return {}
plain, alt = facts(job + "/plain.out"), facts(job + "/altered.out")
if side == "above":
    ok = plain.get("ok") == "1" and alt.get("ok") == "0" and "missing_text" in alt.get("reason", "").replace(" ", "_")
    expect = "unaltered_ok_and_altered_rejected_missing_text"
else:
    ok = plain.get("ok") == "1" and alt.get("ok") == "1" and int(alt.get("below_render_floor", "0")) >= 1 \
        and int(alt.get("below_floor_runes", "0")) >= 2
    expect = "unaltered_ok_and_altered_ok_with_the_text_counted_below_render_floor"
print("TEETH,name=%s,side=%s,file=%s,sha256=%s,altered_index=%s,altered_svg_pt=%s,floor_pt=%s,expect=%s,unaltered_ok=%s,unaltered_below_render_floor=%s,altered_ok=%s,altered_below_render_floor=%s,altered_expected_runes=%s,altered_reason=%s,result=%s" % (
    name, side, fname, digest, idx, size, alt.get("floor_pt", "na"), expect, plain.get("ok", "na"), plain.get("below_render_floor", "na"),
    alt.get("ok", "na"), alt.get("below_render_floor", "na"), alt.get("expected_runes", "na"), alt.get("reason", "na"), "ok" if ok else "fail"))
PY
)
  printf '%s\n' "$got" >> "$LINES"
  log "$got"
  case "$got" in *result=ok) ;; *) printf 'X,stop=teeth_failed,name=%s\n' "$name" >> "$MEAS" ;; esac
  rm -rf "$job"
}

floor_rule_lines() {
  cat <<'RULES'
RULE,key=r10_sweep,mode=floor,rule=rows=public/uncommon.dxf_plus_one_POINT_at_(D;D)_for_D=round(2000x1.12^k)_k=0..60_(dxf-far-point)_and_the_2;5;10;20;50_MiB_replicates_of_uncommon.dxf;each_row_runs_cad-dxf_confined;cad-svg-check_on_the_render-only_SVG;rsvg-convert_-f_pdf_-b_white_confined_from_the_validated_SVG;cad-pdf-check_--report
RULE,key=r10_size,mode=floor,rule=rendered_size_pt=SVG_font-size_x_sqrt(abs(det(text_matrix)))_x_min(MediaBox_w/viewBox_w;MediaBox_h/viewBox_h);a_text_is_present_when_its_render-only_SVG_text_without_whitespace_is_the_next_run_of_the_decoded_PDF_text_in_document_order;the_PDF-side_size_(CTM_x_Tm_x_Tf)_of_present_texts_is_recorded_as_a_cross-check
RULE,key=r10_floor,mode=floor,rule=L=the_largest_rendered_size_pt_of_any_lost_text_over_all_rows;F=2xL;STOP_floor_stage_failed_if_a_stage_or_a_report_fails;STOP_alignment_residual_if_a_row_leaves_decoded_text_unmatched;STOP_no_loss_observed_if_no_text_is_lost
RULES
}

floor_row() {
  local row=$1 src=$2 label=$3 job ok=1
  disk_guard
  log "floor row $row ($(bytes_of "$src") bytes)"
  job=$(new_job)
  cp -c "$src" "$job/in.dxf" 2>/dev/null || cp "$src" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  wrapped M "$row" 1 dxf "$job" "$DXF_RO" draw.svg /usr/local/bin/cad-dxf --max-drawn-entities 100000000 \
    --texts /job/draw.texts.json --render-svg /job/draw.render.svg /job/in.dxf /job/draw.svg || ok=0
  if [ "$ok" = 1 ]; then
    wrapped G "$row" 1 svg_render "$job" none clean.render.svg /gobin/cad-svg-check --out /job/clean.render.svg /job/draw.render.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    wrapped M "$row" 1 rsvg_pdf "$job" "$RSVG_RO" out.pdf /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/clean.render.svg || ok=0
  fi
  if [ "$ok" = 1 ]; then
    docker run --rm "${LIMITS[@]}" -v "$WORK/bin:/gobin:ro" -v "$job:/job" --entrypoint /gobin/cad-pdf-check "$TAG" \
      --report --texts /job/draw.texts.json --svg /job/clean.render.svg /job/out.pdf > "$FLOOR_DIR/$row.report" </dev/null || ok=0
  fi
  printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$row" "$label" "$(basename "$src")" "$(bytes_of "$src")" "$(sha_of "$src")" "$ok" >> "$FLOOR_DIR/index.tsv"
  rm -rf "$job"
}

floor_aggregate() {
  python3 - "$FLOOR_DIR" <<'PY'
import collections
import os
import sys

d = sys.argv[1]
index = [ln.rstrip("\n").split("\t") for ln in open(os.path.join(d, "index.tsv"))]
stop = None
lost_max = None
lost_at = None
present_sizes = []
diff_abs = 0.0
diff_rel = 0.0
lines = []
for row, label, name, size, digest, ok in index:
    path = os.path.join(d, row + ".report")
    reps = []
    summary = {}
    if ok == "1" and os.path.exists(path):
        for ln in open(path):
            ln = ln.strip()
            parts = dict(p.split("=", 1) for p in ln.split(",")[1:] if "=" in p)
            if ln.startswith("REPORT,"):
                reps.append(parts)
            elif ln.startswith("GOPDFREPORT,"):
                summary = parts
    if ok != "1" or not summary:
        stop = stop or ("floor_stage_failed", "row=%s" % row)
        lines.append("FLOOR_FILE,row=%s,label=%s,file=%s,bytes=%s,sha256=%s,result=fail" % (row, label, name, size, digest))
        continue
    if summary.get("residual") != "0":
        stop = stop or ("alignment_residual", "row=%s,residual=%s" % (row, summary.get("residual")))
    lines.append("FLOOR_FILE,row=%s,label=%s,file=%s,bytes=%s,sha256=%s,texts=%s,present=%s,lost=%s,empty=%s,residual=%s,result=%s" % (
        row, label, name, size, digest, summary["texts"], summary["present"], summary["lost"], summary["empty"], summary["residual"],
        "ok" if summary.get("residual") == "0" else "fail"))
    groups = collections.OrderedDict()
    for r in reps:
        if r["present"] == "-1":
            continue
        g = groups.setdefault(r["svg_pt"], {"texts": 0, "present": 0, "lost": 0, "pdf": []})
        g["texts"] += 1
        sv = float(r["svg_pt"])
        if r["present"] == "1":
            g["present"] += 1
            pv = float(r["pdf_pt"])
            g["pdf"].append(pv)
            diff_abs = max(diff_abs, abs(sv - pv))
            if sv >= 0.1:
                diff_rel = max(diff_rel, abs(sv - pv) / sv)
            present_sizes.append(sv)
        else:
            g["lost"] += 1
            if lost_max is None or sv > lost_max:
                lost_max, lost_at = sv, (row, r["svg_pt"])
    for size_pt in sorted(groups, key=float):
        g = groups[size_pt]
        lines.append("FLOOR_ROW,row=%s,label=%s,size_pt=%s,texts=%d,present=%d,lost=%d,pdf_pt_min=%s,pdf_pt_max=%s,all_present=%d" % (
            row, label, size_pt, g["texts"], g["present"], g["lost"],
            "%.6g" % min(g["pdf"]) if g["pdf"] else "na", "%.6g" % max(g["pdf"]) if g["pdf"] else "na", 1 if g["lost"] == 0 else 0))
if stop is None and lost_max is None:
    stop = ("no_loss_observed", "rows=%d" % len(index))
for ln in lines:
    print(ln)
if stop:
    print("STOP,mode=floor,reason=%s,%s" % stop)
    print("SUMMARY,mode=floor,rows=%d,stop=%s,result=stop" % (len(index), stop[0]))
    sys.exit(1)
floor = float("%.6g" % (2 * lost_max))
print("DERIVED,key=cad_dxf_pdf_text_floor,value=%.6g,derivation=F=2xL=2x%s=%.6g_pt;L=largest_rendered_size_of_a_lost_text_at_row_%s;sizes_in_PDF_page_points" % (
    floor, lost_at[1], floor, lost_at[0]))
print("SUMMARY,mode=floor,rows=%d,largest_lost_pt=%s,smallest_present_pt=%.6g,svg_vs_pdf_size_max_abs_pt=%.6g,svg_vs_pdf_size_max_rel_at_or_above_0.1pt=%.6g,floor_pt=%.6g,stop=none,result=ok" % (
    len(index), lost_at[1], min(present_sizes), diff_abs, diff_rel, floor))
PY
}

floor_mode() {
  local out line name dist path mib rc=0
  setup floor
  FLOOR_DIR="$WORK/floor"
  mkdir -p "$FLOOR_DIR"
  : > "$FLOOR_DIR/index.tsv"
  GEN_MOUNTS=()
  out=$(gen --scaled-from "$BASE_DXF" --kind dxf-far-point --targets "$(python3 -c 'print(",".join(str(round(2000 * 1.12 ** k)) for k in range(61)))')")
  printf '%s\n' "$out" | grep '^SCALED,' > "$WORK/gen.lines" || fail "no SCALED line from dxf-far-point"
  [ "$(wc -l < "$WORK/gen.lines" | tr -d ' ')" = 61 ] || fail "dxf-far-point produced $(wc -l < "$WORK/gen.lines") files"
  while IFS= read -r line; do
    name=$(scaled_field file "$line")
    dist=$(scaled_field distance "$line")
    path=$(stash_gen "$name")
    printf 'FLOOR_GEN,row=D%s,%s\n' "$dist" "$(printf '%s' "$line" | sed 's/^SCALED,//')" >> "$LINES"
    floor_row "D$dist" "$path" "far_point_d$dist"
  done < "$WORK/gen.lines"
  for mib in 2 5 10 20 50; do
    path=$(replicate_src "$mib")
    floor_row "R$mib" "$path" "replicate_${mib}MiB"
  done
  floor_aggregate >> "$LINES" || rc=1
  write_evidence
  grep -E '^(DERIVED|STOP|SUMMARY),' "$LINES" >&2 || true
  exit "$rc"
}

smoke_mode() {
  local n path gline c_line c_path b_path b_line k=0
  setup smoke
  GEN_MOUNTS=()
  while IFS=$'\t' read -r n path gline; do
    measure_row "S$n" 1 "$path" smoke "$n" ten ",gen=$gline"
    k=$((k + 1))
  done < <(gen_minsert 100,1000 5)
  [ "$k" = 2 ] || fail "smoke generation produced $k dxf-minsert files"
  path=$(replicate_src 2)
  measure_row SR2 1 "$path" smoke 2MiB ten ""
  IFS=$'\t' read -r n b_path b_line <<< "$(gen_minsert 20 "$B_BLOCK_ENTITIES")"
  measure_row SB 1 "$b_path" check 20 "$SMOKE_CAP_INSERTS+$SMOKE_CAP_ENTITIES" ",shape=B_smoke,block_entities=$B_BLOCK_ENTITIES,gen=$b_line"
  IFS=$'\t' read -r c_path c_line <<< "$(gen_combo "$path" 100)"
  measure_row SC 1 "$c_path" check 100 "$SMOKE_CAP_INSERTS+$SMOKE_CAP_ENTITIES" ",shape=C_smoke,block_entities=5,gen=$c_line"
  rm -f "$b_path" "$c_path"
  eval "$(python3 "$WORK/tools/derive.py" state smoke "$MEAS")"
  if [ -n "$STOP" ]; then finish 1; fi
  finish 0
}

run_mode() {
  local i n path gline mib ci ce verdict b_line c_line ceil_src over
  setup run
  teeth_row above_the_floor "$REPO_ROOT/$BASE_DXF" above
  teeth_row below_the_floor "$(far_point_src 367332)" below
  check_state run
  stress_rows
  check_state run
  LADDER_PATH=(); LADDER_GEN=()
  i=0
  while IFS=$'\t' read -r n path gline; do
    LADDER_PATH[$i]=$path
    LADDER_GEN[$i]=$gline
    i=$((i + 1))
  done < <(gen_minsert "$(IFS=,; echo "${LADDER[*]}")" 5)
  [ "${#LADDER_PATH[@]}" = "${#LADDER[@]}" ] || fail "ladder generation produced ${#LADDER_PATH[@]} files"
  for mib in "${CEILING_TARGETS[@]}"; do
    path=$(replicate_src "$mib")
    measure_row "R$mib" 1 "$path" ceiling "${mib}MiB" ten ""
    check_state run
  done
  i=0
  for n in "${LADDER[@]}"; do
    run_ladder_row "$n" 1 "$i"
    check_state run
    over=$(python3 "$WORK/tools/derive.py" over "L$n" "$MEAS")
    i=$((i + 1))
    [ "$over" = 0 ] || break
  done
  check_state run
  rep2_rows
  [ -n "$CAP" ] && [ -n "$CEIL" ] || { printf 'X,stop=no_cap_or_ceiling,cap=%s,ceiling=%s\n' "$CAP" "$CEIL" >> "$MEAS"; finish 1; }
  CELLS_B=$(( (CAPE - 1) / B_BLOCK_ENTITIES )); [ "$CELLS_B" -le "$CAP" ] || CELLS_B=$CAP
  IFS=$'\t' read -r n B_PATH b_line <<< "$(gen_minsert "$CELLS_B" "$B_BLOCK_ENTITIES")"
  [ -s "$B_PATH" ] || fail "check row B was not generated"
  measure_row B 1 "$B_PATH" check "$CELLS_B" "$CAP+$CAPE" ",shape=B_50_entity_block_at_the_entity_cap,block_entities=$B_BLOCK_ENTITIES,gen=$b_line"
  check_state run
  ceil_src=$(replicate_src "$((CEIL / 1048576))")
  read -r verdict ci ce <<< "$(count_pass "$ceil_src")"
  [ "$verdict" = ok ] || { printf 'X,stop=ceiling_replicate_count_failed,verdict=%s\n' "$verdict" >> "$MEAS"; finish 1; }
  CELLS_C=$(( CAP - ci ))
  [ $(( (CAPE - ce - 1) / 5 )) -ge "$CELLS_C" ] || CELLS_C=$(( (CAPE - ce - 1) / 5 ))
  if [ "$CELLS_C" -lt 1 ]; then
    printf 'X,stop=no_remaining_expansion,replicate_inserts=%s,replicate_entities=%s\n' "$ci" "$ce" >> "$MEAS"
    finish 1
  fi
  IFS=$'\t' read -r C_PATH c_line <<< "$(gen_combo "$ceil_src" "$CELLS_C")"
  [ -s "$C_PATH" ] || fail "check row C was not generated"
  measure_row C 1 "$C_PATH" check "$CELLS_C" "$CAP+$CAPE" ",shape=C_ceiling_replicate_plus_remaining_expansion,block_entities=5,replicate_counts=inserts:$ci+entities:$ce,gen=$c_line"
  check_state run
  rep2_rows
  finish 0
}

STACK_EVID_REL=.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt
STACK_EVID_NAME=evidence/76-01-stack.txt
STACK_BUDGET_S=900
STACK_STEP_BASE=internal/convert/testdata/cad/public/LinearSlide-MGN15x-Block.step
STACK_STEP_MAX_BYTES=66060288
STACK_IGES_MAX_BYTES=141557760
STACK_STL_MAX_BYTES=190840832
STACK_MAX_ENTITIES=754762
STACK_MAX_NAUO=1518
STACK_TRI_CAP=3835382
STACK_S1=(64 256 1024 4096 16384 65536 262144 318067)
STACK_S2=(2 4 8 16 32 64 128 256)
STACK_NAUO=(36 100 300 644 1000 1518)
STACK_S4=(10 20 32 40 48 56)
STACK_S4_CEIL_TARGET=63
STACK_S5_MIB=(32 64 96)
STACK_ENV=(-e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2)

stack_rule_lines() {
  cat <<'RULES'
RULE,key=limits,mode=stack,rule=production_engine_stage_of_Dockerfile.cad-worker;one_container_per_stage;--network_none_--cpus_2_--memory_4g_--memory-swap_4g_--user_nobody;cad-mesh_and_cad-render_confined_by_sandbox-probe_with_PROBE_RO=/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/<triplet>:/usr/local/bin/<helper>_and_PROBE_RW=/job;cad-guard_unconfined_/usr/bin/python3_-IB_as_in_75-03;env_HOME=/job_LANG=C.UTF-8_TMPDIR=/job_LP_NUM_THREADS=2_on_every_stage_(cadStage_sets_LP_NUM_THREADS=2);wrap_budget_900_s_per_stage_(measure_wrap.py)
RULE,key=budget,mode=stack,rule=B_render=(886-30-316-60)/1.5=320_s;B_mesh=316/1.5=210.67_s;from_the_current_deadlines_(guard_30_mesh_316_render_480_io_60_engine_886);provisional_for_this_probe_only;76-03_re-derives_them;cad-mesh_stats_(STL)_runs_under_the_mesh_deadline_so_B_mesh_applies_to_stats
RULE,key=shapes,mode=stack,rule=S1=stl-stack_of_the_12-triangle_unit_cube_at_K_in_64;256;1024;4096;16384;65536;262144;318067|S2=stl-stack_of_LARGE_STL_at_K_in_2;4;8;16;32;64;128;256_while_84+50xKxT<=190840832_(T=triangles_of_LARGE_STL)|S3=step-nauo_of_public/LinearSlide-MGN15x-Block.step_at_n_in_36;100;300;644;1000;1518_(1518=CADMaxNAUO)_on_the_generator's_200_mm_grid|S4=step-copies_of_LinearSlide_at_10;20;32;40;48;56_MiB_and_at_the_63_MiB_ceiling_(the_largest_whole-MiB_step-copies_target_at_or_below_63_whose_file_is_at_most_66060288_bytes)|S5=iges-from-step_of_step-copies_of_LinearSlide_at_32;64;96_MiB_of_IGES_and_the_largest_IGES_the_generator_yields_at_or_below_141557760_bytes_(each_rung=the_largest_whole-MiB_step-copies_target_whose_IGES_is_at_or_below_the_rung_bytes)|S6=step-nauo_of_LinearSlide_at_the_S3_counts_with_--nauo-pitch_0_(every_instance_coincident;added_by_the_executor_because_the_S3_grid_places_instances_200_mm_apart_and_so_does_not_stack_them)
RULE,key=rows,mode=stack,rule=N=1_per_rung_(a_premise_test_not_a_W_setter);STL:cad-mesh_stats_--measure_then_cad-render_--view_iso_--size_2048_--background_opaque;STEP_and_IGES:cad-guard_--max-entities_754762_--max-nauo_1518_then_cad-mesh_--quality_standard_--measure_then_cad-render_iso_2048_opaque_or_band=refused_by_cap_above_3835382_triangles_(render_not_run);inputs_generated_inside_the_engine_image_with_PYTHONHASHSEED=0_under_the_same_limits_except_iges-from-step_which_gets_8g_(generation_is_not_measured);inputs_and_outputs_deleted_after_each_rung
RULE,key=bands,mode=stack,rule=band=over_when_a_render_wall_exceeds_B_render_or_a_stats_or_mesh_wall_exceeds_B_mesh_or_the_stage_hit_the_900_s_wrap_budget_(timeout=1)_or_was_OOM-killed;band=refused_by_cap_for_a_guard_too_complex_verdict_(rc_4;the_production_refusal)_or_a_mesh_above_3835382_triangles;band=within_otherwise;a_guard_row_is_within_unless_refused
RULE,key=ladder_stop,mode=stack,rule=a_ladder_ends_at_its_first_rung_whose_render_wall_exceeds_B_render_or_whose_stats_or_mesh_wall_exceeds_B_mesh_or_which_hits_the_900_s_wrap_budget_(a_timeout_counts_as_over);every_larger_rung_is_written_NOT_RUN_reason=over_budget_below_and_never_counted;nothing_is_extrapolated;no_row_is_dropped;a_refused_by_cap_rung_does_not_end_a_ladder
RULE,key=floors,mode=stack,rule=floor_f=bytes_of_the_largest_file_of_format_f_that_scripts/cad-liveproof.sh_renders_today;STL:LARGE_STL=the_first_of_ls_-S_public/*.stl_fetched/*.stl;STEP_and_IGES:the_largest_.step/.stp_and_.igs/.iges_input_named_on_its_render_one;render_job;meas_case_and_create_job_png_render_lines;each_written_as_a_FLOOR_line_with_file_bytes_and_sha256
RULE,key=decision,mode=stack,rule=per_format_f_mechanical:within_budget_if_no_row_of_any_shape_of_f_is_over;byte_bound_if_every_over_shape_of_f_is_a_copies_shape_(S4_for_step;S5_for_iges)_whose_first_over_rung_has_bytes>=floor_f_and_every_other_probed_mechanism_of_f_stays_within_budget_(step:S3_and_S6;stl:S1_and_S2)_and_f_has_no_unprobed_stacking_mechanism;IGES_308/408_subfigure_instancing_is_unprobed_so_iges_is_never_byte_bound;not_byte_bound_otherwise;shapes:stl=S1+S2;step=S3+S4+S6;iges=S5
RULE,key=assumption_step_instancing,mode=stack,rule=STEP_MAPPED_ITEM_instancing_is_bounded_by_the_same_1518_expanded-instance_guard_cap_as_NAUO_and_meshes_to_the_same_coincident_instances;S6_is_counted_as_its_probe;this_is_an_assumption_written_before_any_row_not_a_measurement
RULE,key=global,mode=stack,rule=render_deadline_rule=remainder_if_any_format_is_not_byte_bound_with_an_over_row_else_ceil_1.5W
RULE,key=stop,mode=stack,rule=STOP_on_any_stage_rc_other_than_0_that_is_not_a_timeout_or_an_OOM_kill_(oom_kill>0;a_container_exit_137_without_a_WRAP_line;cad-mesh_rc_4_too_complex/out_of_memory)_or_a_guard_too_complex_refusal;on_any_generator_self-check_failure;on_VM_disk_below_6_GiB_(exit_3)
RULES
}

write_stack_tools() {
  cat > "$WORK/tools/stack.py" <<'PY'
import glob
import hashlib
import os
import re
import struct
import sys
from decimal import Decimal

B_RENDER = Decimal(320)
B_MESH = Decimal("210.67")
TRI_CAP = 3835382
SHAPES = {"stl": ["S1", "S2"], "step": ["S3", "S4", "S6"], "iges": ["S5"]}
COPIES = {"step": "S4", "iges": "S5"}
UNPROBED = {"iges": "iges_308_408_subfigure_instancing_unprobed"}


def kv(line):
    head, _, rest = line.partition(",")
    out = {"_": head}
    for part in rest.split(","):
        if "=" in part:
            k, v = part.split("=", 1)
            out[k] = v
    return out


def wrap_fields(path):
    try:
        with open(path, errors="replace") as fh:
            for line in fh:
                if line.startswith("WRAP,"):
                    return dict(p.split("=", 1) for p in line.strip().split(",")[1:] if "=" in p)
    except OSError:
        pass
    return {}


def verdict_of(path):
    import json
    obj = {}
    try:
        with open(path, "rb") as fh:
            for raw in fh:
                line = raw.decode("utf-8", "replace").strip()
                if line.startswith("CADVERDICT "):
                    try:
                        obj = json.loads(line[len("CADVERDICT "):])
                    except ValueError:
                        obj = {}
    except OSError:
        pass
    return obj


def stl_count(path):
    try:
        with open(path, "rb") as fh:
            head = fh.read(84)
        if len(head) == 84 and os.path.getsize(path) == 84 + 50 * struct.unpack("<I", head[80:84])[0]:
            return struct.unpack("<I", head[80:84])[0]
    except OSError:
        pass
    return None


def clean(v):
    return re.sub(r"[,=\s]", "_", str(v))


def row(argv):
    shape, fmt, rung, size, digest, stage, wrap, log, rc_w, tri_in, mesh, load1, vmfree = argv
    f = wrap_fields(wrap)
    v = verdict_of(log)
    rc = f.get("rc", "na")
    timeout = f.get("timeout", "na")
    oom = f.get("oom_kill", "na")
    wall = f.get("wall_s", "na")
    reason = v.get("reason") or "none"
    verdict = v.get("verdict", "none")
    triangles = tri_in
    cls = "fail"
    if not f:
        cls = "oom" if rc_w == "137" else "fail"
        oom = "container_137" if rc_w == "137" else oom
    elif timeout == "1":
        cls = "timeout"
    elif oom not in ("na", "0"):
        cls = "oom"
    elif rc == "0":
        cls = "ok"
    elif stage == "guard" and rc == "4" and verdict == "too_complex":
        cls = "refused"
    elif stage in ("mesh", "stats") and rc == "4" and verdict == "too_complex" and reason == "out_of_memory":
        cls = "oom"
    if stage in ("mesh", "stats") and cls == "ok":
        n = stl_count(mesh)
        vt = v.get("triangles")
        if n is None or vt != n or verdict != "ok":
            cls = "fail"
            reason = "stl_count_%s_verdict_%s_%s" % (n, vt, verdict)
        triangles = str(n)
    if stage == "render" and cls == "ok" and (verdict != "ok" or v.get("size") != 2048 or v.get("view") != "iso"):
        cls = "fail"
        reason = "render_verdict_%s_view_%s_size_%s" % (verdict, v.get("view"), v.get("size"))
    if stage == "guard" and cls == "ok" and verdict != "ok":
        cls = "fail"
    band = "within"
    if cls in ("timeout", "oom"):
        band = "over"
    elif cls == "refused":
        band = "refused_by_cap"
    elif cls == "ok":
        w = Decimal(wall)
        if stage == "render" and w > B_RENDER:
            band = "over"
        elif stage in ("mesh", "stats") and w > B_MESH:
            band = "over"
    elif cls == "fail":
        band = "failed"
    print("STACK_ROW,shape=%s,format=%s,rung=%s,bytes=%s,triangles=%s,stage=%s,rc=%s,wall_s=%s,child_rss_mib=%s,cg_peak_mib=%s,"
          "oom_kill=%s,timeout=%s,band=%s,load1=%s,vm_free_gib=%s,sha256=%s,verdict=%s,reason=%s,class=%s,threads_max=%s" % (
              shape, fmt, rung, size, triangles, stage, rc, wall, f.get("child_rss_mib", "na"), f.get("cg_peak_mib", "na"),
              oom, timeout, band, load1, vmfree, digest, clean(verdict), clean(reason), cls, f.get("threads_max", "na")))


def floors(argv):
    liveproof, corpus, fetched = argv
    text = open(liveproof).read()
    picks = {"step": None, "iges": None}
    pat = re.compile(r'"\$(CORPUS|FETCHED)/([^"]+)"')
    for line in text.splitlines():
        s = line.strip()
        if not (s.startswith(("render_one ", "render_job ", "meas_case ")) or
                (s.startswith("create_job ") and " png render" in s)):
            continue
        for root, rel in pat.findall(s):
            path = os.path.join(corpus if root == "CORPUS" else fetched, rel)
            ext = os.path.splitext(rel)[1].lower()
            fmt = "step" if ext in (".step", ".stp") else "iges" if ext in (".igs", ".iges") else None
            if fmt is None or not os.path.isfile(path):
                continue
            size = os.path.getsize(path)
            if picks[fmt] is None or size > picks[fmt][0]:
                picks[fmt] = (size, path, s.split()[0])
    stls = glob.glob(os.path.join(corpus, "public", "*.stl")) + glob.glob(os.path.join(fetched, "*.stl"))
    stls.sort(key=lambda p: -os.path.getsize(p))
    picks["stl"] = (os.path.getsize(stls[0]), stls[0], "LARGE_STL") if stls else None
    rc = 0
    for fmt in ("step", "iges", "stl"):
        p = picks[fmt]
        if p is None:
            print("STOP,mode=stack,reason=no_floor_file,format=%s" % fmt)
            rc = 1
            continue
        digest = hashlib.sha256(open(p[1], "rb").read()).hexdigest()
        print("FLOOR,format=%s,file=%s,bytes=%d,sha256=%s,source=%s,path=%s" % (fmt, os.path.basename(p[1]), p[0], digest, p[2], p[1]))
    return rc


def decide(argv):
    path = argv[0]
    rows, floors_, not_run = [], {}, []
    for line in open(path):
        line = line.rstrip("\n")
        if line.startswith("STACK_ROW,"):
            rows.append(kv(line))
        elif line.startswith("FLOOR,"):
            d = kv(line)
            floors_[d["format"]] = int(d["bytes"])
        elif line.startswith("NOT_RUN,"):
            not_run.append(kv(line))
    first_over = {}
    measured = {}
    order = {}
    for r in rows:
        sh = r["shape"]
        measured.setdefault(sh, [])
        if r["rung"] not in measured[sh]:
            measured[sh].append(r["rung"])
        order.setdefault((sh, r["rung"]), len(order))
        if r["band"] == "over" and sh not in first_over:
            first_over[sh] = (r["rung"], int(r["bytes"]), r["stage"], r["wall_s"], r["timeout"], r["oom_kill"])
    problems = []
    for fmt, shapes in SHAPES.items():
        for sh in shapes:
            if sh not in measured:
                problems.append("shape_%s_has_no_row" % sh)
        if fmt not in floors_:
            problems.append("no_floor_%s" % fmt)
    for d in not_run:
        if d.get("shape") not in first_over:
            problems.append("not_run_%s_%s_without_an_over_rung" % (d.get("shape"), d.get("rung")))
    if problems:
        print("STOP,mode=stack,reason=decision_inputs_incomplete,detail=%s" % ";".join(problems))
        print("SUMMARY,mode=stack,rows=%d,not_run=%d,result=STOP" % (len(rows), len(not_run)))
        return 1
    decisions = {}
    for fmt in ("step", "iges", "stl"):
        shapes = SHAPES[fmt]
        over = [sh for sh in shapes if sh in first_over]
        floor = floors_[fmt]
        if not over:
            decision, why = "within_budget", "no_row_of_%s_over" % "+".join(shapes)
            brk = ("none", "na")
        else:
            best = min(over, key=lambda sh: first_over[sh][1])
            brk = (best, str(first_over[best][1]))
            copies = COPIES.get(fmt)
            others = [sh for sh in shapes if sh != copies]
            if fmt in UNPROBED:
                decision, why = "not_byte_bound", UNPROBED[fmt]
            elif copies is None or set(over) != {copies}:
                decision = "not_byte_bound"
                why = "over_beyond_copies:" + ";".join("%s@%s:%s_%s_wall_%s_timeout_%s" % (sh, first_over[sh][0], first_over[sh][1], first_over[sh][2], first_over[sh][3], first_over[sh][4]) for sh in over if sh != copies)
            elif first_over[copies][1] < floor:
                decision = "not_byte_bound"
                why = "copies_break_%d_below_floor_%d" % (first_over[copies][1], floor)
            elif any(sh in first_over for sh in others):
                decision, why = "not_byte_bound", "another_mechanism_over"
            else:
                decision = "byte_bound"
                why = "only_copies_over_at_%d>=floor_%d;%s_within" % (first_over[copies][1], floor, "+".join(others))
        decisions[fmt] = (decision, bool(over))
        print("STACK_DECISION,format=%s,decision=%s,break_shape=%s,break_bytes=%s,floor_bytes=%d,over_shapes=%s,reason=%s" % (
            fmt, decision, brk[0], brk[1], floor, "+".join(over) or "none", why))
    nbb = [f for f in ("step", "iges", "stl") if decisions[f][0] == "not_byte_bound" and decisions[f][1]]
    rule = "remainder" if nbb else "ceil_1.5W"
    print("STACK_GLOBAL,render_deadline_rule=%s,not_byte_bound_with_over=%s,byte_bound=%s,within_budget=%s" % (
        rule, "+".join(nbb) or "none", "+".join(f for f in ("step", "iges", "stl") if decisions[f][0] == "byte_bound") or "none",
        "+".join(f for f in ("step", "iges", "stl") if decisions[f][0] == "within_budget") or "none"))
    rungs = len({(r["shape"], r["rung"]) for r in rows})
    print("SUMMARY,mode=stack,rows=%d,rungs=%d,not_run=%d,over_shapes=%s,result=ok" % (
        len(rows), rungs, len(not_run), "+".join(sorted(first_over)) or "none"))
    return 0


if __name__ == "__main__":
    cmd = sys.argv[1]
    if cmd == "row":
        row(sys.argv[2:])
    elif cmd == "floors":
        sys.exit(floors(sys.argv[2:]))
    elif cmd == "decide":
        sys.exit(decide(sys.argv[2:]))
    else:
        sys.exit(64)
PY
  chmod 0644 "$WORK/tools/stack.py"
}

load1_now() { sysctl -n vm.loadavg 2>/dev/null | awk '{print $2}'; }

stack_stop() {
  printf 'STOP,mode=%s,%s\n' "$STACK_MODE" "$1" >> "$LINES"
  printf 'SUMMARY,mode=%s,rows=%s,not_run=%s,result=STOP\n' "$STACK_MODE" "$(grep -c '^STACK_ROW,' "$LINES" || true)" \
    "$(grep -c '^NOT_RUN,' "$LINES" || true)" >> "$LINES"
  log "STOP $1"
  exit "${2:-1}"
}

stack_disk_guard() {
  local free
  free=$(vm_free_gib)
  if [ -z "$free" ] || [ "$free" -lt "$MIN_FREE_GIB" ]; then
    stack_stop "reason=disk_below_threshold,free_gib=${free:-unknown},threshold_gib=$MIN_FREE_GIB" 3
  fi
  STACK_FREE=$free
}

stack_preflight() {
  local n
  n=$(docker ps --filter name=octoconv- -q | wc -l | tr -d ' ')
  [ "$n" = 0 ] || fail "preflight: $n octoconv- compose containers are running"
  if command -v kubectl >/dev/null 2>&1; then
    n=$(kubectl get pods -A 2>/dev/null | grep -c octoconv || true)
    [ "$n" = 0 ] || fail "preflight: $n octoconv pods are running in k8s"
  fi
  if pgrep -f 'go test' >/dev/null 2>&1; then fail "preflight: a go test process is running"; fi
  if pgrep -f 'docker build|buildx' >/dev/null 2>&1; then fail "preflight: a docker build is running"; fi
}

stack_setup() {
  STACK_MODE=$1
  need_tool docker
  need_tool go
  need_tool python3
  need_tool shasum
  need_tool git
  [ "$(docker version --format '{{.Server.Arch}}')" = arm64 ] || fail "docker server is not arm64"
  [ "$(GOTOOLCHAIN=$GO_TOOLCHAIN go env GOVERSION)" = "$GO_TOOLCHAIN" ] || fail "go toolchain is not $GO_TOOLCHAIN"
  stack_preflight
  disk_guard
  EVID_NAME=$STACK_EVID_NAME
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-stack.XXXXXX")
  chmod 0755 "$WORK"
  trap cleanup EXIT
  mkdir -p "$WORK/wrap" "$WORK/tools" "$WORK/gen"
  chmod 0755 "$WORK/wrap" "$WORK/tools"
  chmod 0777 "$WORK/gen"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
  chmod 0644 "$WORK/wrap/measure_wrap.py"
  write_stack_tools
  LINES=$EVID
  mkdir -p "$(dirname "$LINES")"
  stack_rule_lines > "$LINES"
  ENGINE_TAG_PREFIX=engine76-
  TAG=$(build_engine) || stack_stop "reason=engine_build_failed"
  BUILT_TAGS+=("$TAG")
  cad_require_native "$TAG" || stack_stop "reason=engine_image_not_native"
  ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
  TRIPLET=$(triplet_for "$ARCH")
  MESH_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/$TRIPLET:/usr/local/bin/cad-mesh"
  RENDER_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/$TRIPLET:/usr/local/bin/cad-render"
  mkdir -p "$WORK/bin"
  chmod 0755 "$WORK/bin"
  CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$WORK/bin/sandbox-probe" ./scripts/cmd/sandbox-probe
  chmod 0755 "$WORK/bin/sandbox-probe"
  stack_disk_guard
  {
    printf 'HDR,mode=%s,date_utc=%s\n' "$STACK_MODE" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'HDR,mode=%s,commit=%s,tracked_dirt=%s\n' "$STACK_MODE" "$(git rev-parse HEAD)" "$(tracked_dirt)"
    printf 'HDR,mode=%s,image=%s,image_id=%s,server_arch=%s\n' "$STACK_MODE" "$TAG" "$(docker image inspect "$TAG" --format '{{.Id}}')" \
      "$(docker version --format '{{.Server.Arch}}')"
    printf 'HDR,mode=%s,load1=%s,vm_free_gib=%s\n' "$STACK_MODE" "$(load1_now)" "$STACK_FREE"
    printf 'HDR,mode=%s,cad_mesh_sha256=%s,cad_render_sha256=%s,cad_guard_sha256=%s,generator_sha256=%s,measure_wrap_sha256=%s\n' "$STACK_MODE" \
      "$(sha_of cmd/cad-worker/helper/cad-mesh)" "$(sha_of cmd/cad-worker/helper/cad-render)" "$(sha_of cmd/cad-worker/helper/cad-guard)" \
      "$(sha_of scripts/cad-corpus-gen.py)" "$(sha_of "$WRAP_PY")"
    printf 'HDR,mode=%s,limits=cpus2_memory4g_swap4g_network_none_user_nobody,env=HOME=/job_LANG=C.UTF-8_TMPDIR=/job_LP_NUM_THREADS=2,wrap_budget_s=%s,go=%s,size=%s,view=iso,background=opaque,quality=standard\n' \
      "$STACK_MODE" "$STACK_BUDGET_S" "$GO_TOOLCHAIN" "$SIZE"
    printf 'HDR,mode=%s,ro_mesh=%s,ro_render=%s,rw=/job,guard=unconfined_/usr/bin/python3_-IB_as_in_75-03,guard_caps=--max-entities_%s_--max-nauo_%s,triangle_cap=%s\n' \
      "$STACK_MODE" "$MESH_RO" "$RENDER_RO" "$STACK_MAX_ENTITIES" "$STACK_MAX_NAUO" "$STACK_TRI_CAP"
  } >> "$LINES"
}

stack_gen() {
  local out
  out=$(docker run --rm --network none --cpus 2 --memory "${GEN_MEM:-4g}" --memory-swap "${GEN_MEM:-4g}" --user nobody \
    -e HOME=/tmp -e PYTHONHASHSEED=0 -v "$REPO_ROOT:/src:ro" -v "$WORK/gen:/out" \
    ${GEN_MOUNTS[@]+"${GEN_MOUNTS[@]}"} -w /src --entrypoint /opt/venv/bin/python "$TAG" -B "${GEN_SCRIPT:-scripts/cad-corpus-gen.py}" \
    "$@" --scaled-dir "${GEN_OUT:-/out}" </dev/null 2>"$WORK/gen.err") || {
    tail -n 5 "$WORK/gen.err" >&2
    stack_stop "reason=generator_failed,args=$(printf '%s' "$*" | tr ' ,=' '___')"
  }
  printf '%s\n' "$out"
}

stack_stage() {
  local job=$1 stage=$2 confine=$3 rc_w=0 dir
  shift 3
  dir="$job/w_$stage"
  mkdir -p "$dir"
  chmod 0777 "$dir"
  if [ "$confine" = none ]; then
    docker run --rm "${LIMITS[@]}" "${STACK_ENV[@]}" -v "$WORK/wrap:/wrap:ro" -v "$job:/job" \
      --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py --budget-s "$STACK_BUDGET_S" --outdir "/job/w_$stage" -- \
      "$@" > "$dir/wrap.out" 2>/dev/null </dev/null || rc_w=$?
  else
    docker run --rm "${LIMITS[@]}" "${STACK_ENV[@]}" -e "PROBE_RO=$confine" -e "PROBE_RW=/job" -v "$WORK/bin:/gobin:ro" \
      -v "$WORK/wrap:/wrap:ro" -v "$job:/job" --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py \
      --budget-s "$STACK_BUDGET_S" --outdir "/job/w_$stage" -- /gobin/sandbox-probe "$@" > "$dir/wrap.out" 2>/dev/null </dev/null || rc_w=$?
  fi
  printf '%s\n' "$rc_w" > "$dir/rc_w"
}

stack_emit() {
  local shape=$1 fmt=$2 rung=$3 size=$4 digest=$5 job=$6 stage=$7 tri_in=$8 load=$9 free=${10} line
  line=$(python3 "$WORK/tools/stack.py" row "$shape" "$fmt" "$rung" "$size" "$digest" "$stage" "$job/w_$stage/wrap.out" \
    "$job/w_$stage/child.log" "$(cat "$job/w_$stage/rc_w")" "$tri_in" "$job/mesh.stl" "$load" "$free")
  printf '%s\n' "$line" >> "$LINES"
  log "$(printf '%s' "$line" | tr ',' '\n' | grep -E '^(shape|rung|stage|wall_s|triangles|band|class)=' | tr '\n' ' ')"
  ROW_BAND=$(scaled_field band "$line")
  ROW_CLASS=$(scaled_field class "$line")
  ROW_TRI=$(scaled_field triangles "$line")
  if [ "$ROW_CLASS" = fail ]; then
    tail -n 8 "$job/w_$stage/child.log" >&2 2>/dev/null || true
    stack_stop "reason=stage_failed,shape=$shape,rung=$rung,stage=$stage,rc=$(scaled_field rc "$line"),rc_wrap=$(cat "$job/w_$stage/rc_w")"
  fi
}

stack_sample() {
  stack_disk_guard
  STACK_LOAD=$(load1_now)
}

stack_rung() {
  local shape=$1 fmt=$2 rung=$3 src=$4 job size digest in
  log "rung $shape $rung ($(bytes_of "$src") bytes)"
  job=$(new_job)
  in="/job/in.$fmt"
  cp -c "$src" "$job/in.$fmt" 2>/dev/null || cp "$src" "$job/in.$fmt"
  chmod 0644 "$job/in.$fmt"
  size=$(bytes_of "$src")
  digest=$(sha_of "$src")
  RUNG_BAND=within
  if [ "$fmt" = stl ]; then
    stack_sample
    stack_stage "$job" stats "$MESH_RO" /usr/local/bin/cad-mesh stats --measure /job/draft.json "$in" /job/mesh.stl
    stack_emit "$shape" "$fmt" "$rung" "$size" "$digest" "$job" stats na "$STACK_LOAD" "$STACK_FREE"
  else
    stack_sample
    stack_stage "$job" guard none /usr/bin/python3 -IB /usr/local/bin/cad-guard --format "$fmt" --max-entities "$STACK_MAX_ENTITIES" \
      --max-nauo "$STACK_MAX_NAUO" "$in"
    stack_emit "$shape" "$fmt" "$rung" "$size" "$digest" "$job" guard na "$STACK_LOAD" "$STACK_FREE"
    if [ "$ROW_BAND" = refused_by_cap ]; then
      RUNG_BAND=refused_by_cap
      rm -rf "$job"
      return 0
    fi
    stack_sample
    stack_stage "$job" mesh "$MESH_RO" /usr/local/bin/cad-mesh --format "$fmt" "$in" /job/mesh.stl --quality standard --measure /job/draft.json
    stack_emit "$shape" "$fmt" "$rung" "$size" "$digest" "$job" mesh na "$STACK_LOAD" "$STACK_FREE"
  fi
  if [ "$ROW_BAND" = over ]; then
    RUNG_BAND=over
    rm -rf "$job"
    return 0
  fi
  if [ "$ROW_TRI" -gt "$STACK_TRI_CAP" ]; then
    printf 'STACK_ROW,shape=%s,format=%s,rung=%s,bytes=%s,triangles=%s,stage=render,rc=na,wall_s=na,child_rss_mib=na,cg_peak_mib=na,oom_kill=na,timeout=na,band=refused_by_cap,load1=%s,vm_free_gib=%s,sha256=%s,verdict=not_run,reason=triangles_over_%s,class=refused,threads_max=na\n' \
      "$shape" "$fmt" "$rung" "$size" "$ROW_TRI" "$(load1_now)" "$STACK_FREE" "$digest" "$STACK_TRI_CAP" >> "$LINES"
    log "rung $shape $rung render refused_by_cap ($ROW_TRI triangles)"
    RUNG_BAND=refused_by_cap
    rm -rf "$job"
    return 0
  fi
  local tri=$ROW_TRI
  rm -f "$job/in.$fmt"
  stack_sample
  stack_stage "$job" render "$RENDER_RO" /usr/local/bin/cad-render --view iso --size "$SIZE" --background opaque /job/mesh.stl /job/out.png
  stack_emit "$shape" "$fmt" "$rung" "$size" "$digest" "$job" render "$tri" "$STACK_LOAD" "$STACK_FREE"
  [ "$ROW_BAND" != over ] || RUNG_BAND=over
  rm -rf "$job"
  return 0
}

stack_not_run() {
  local shape=$1 r
  shift
  for r in "$@"; do printf 'NOT_RUN,shape=%s,rung=%s,reason=over_budget_below\n' "$shape" "$r" >> "$LINES"; done
}

gen_file_of() {
  local out=$1 kind=$2 line
  line=$(printf '%s\n' "$out" | grep "^SCALED,kind=$kind," | tail -n 1)
  [ -n "$line" ] || stack_stop "reason=generator_gave_no_scaled_line,kind=$kind"
  printf '%s\n' "$line" | grep -q ',check=ok' || stack_stop "reason=generator_self_check_failed,kind=$kind"
  GEN_LINE=$line
  GEN_PATH="$WORK/gen/$(scaled_field file "$line")"
  [ -s "$GEN_PATH" ] || stack_stop "reason=generated_file_missing,kind=$kind"
}

stack_s1() {
  local i k out
  for i in "${!STACK_S1[@]}"; do
    k=${STACK_S1[$i]}
    GEN_MOUNTS=()
    out=$(stack_gen --kind stl-stack --part cube --targets "$k")
    gen_file_of "$out" stl-stack
    printf 'GEN,shape=S1,rung=k%s,%s\n' "$k" "${GEN_LINE#SCALED,}" >> "$LINES"
    stack_rung S1 stl "k$k" "$GEN_PATH"
    rm -f "$GEN_PATH"
    if [ "$RUNG_BAND" = over ]; then
      stack_not_run S1 $(for k in "${STACK_S1[@]:$((i + 1))}"; do printf 'k%s ' "$k"; done)
      return 0
    fi
  done
}

stack_s2() {
  local large tri i k out size list=()
  large=$(sed -n 's/^FLOOR,format=stl,.*,path=\(.*\)$/\1/p' "$LINES")
  [ -s "$large" ] || stack_stop "reason=no_large_stl"
  tri=$(( ($(bytes_of "$large") - 84) / 50 ))
  for k in "${STACK_S2[@]}"; do
    size=$(( 84 + 50 * k * tri ))
    if [ "$size" -le "$STACK_STL_MAX_BYTES" ]; then
      list+=("$k")
    else
      printf 'RUNG_SKIP,shape=S2,rung=k%s,bytes=%s,triangles=%s,reason=over_stl_ceiling_190840832\n' "$k" "$size" "$((k * tri))" >> "$LINES"
    fi
  done
  [ "${#list[@]}" -gt 0 ] || stack_stop "reason=s2_has_no_rung_under_the_stl_ceiling"
  for i in "${!list[@]}"; do
    k=${list[$i]}
    GEN_MOUNTS=(-v "$large:/base/$(basename "$large"):ro")
    out=$(stack_gen --kind stl-stack --scaled-from "/base/$(basename "$large")" --targets "$k")
    gen_file_of "$out" stl-stack
    printf 'GEN,shape=S2,rung=k%s,%s\n' "$k" "${GEN_LINE#SCALED,}" >> "$LINES"
    stack_rung S2 stl "k$k" "$GEN_PATH"
    rm -f "$GEN_PATH"
    if [ "$RUNG_BAND" = over ]; then
      stack_not_run S2 $(for k in "${list[@]:$((i + 1))}"; do printf 'k%s ' "$k"; done)
      return 0
    fi
  done
}

stack_nauo() {
  local shape=$1 pitch=$2 i n out
  for i in "${!STACK_NAUO[@]}"; do
    n=${STACK_NAUO[$i]}
    GEN_MOUNTS=()
    if [ "$pitch" = grid ]; then
      out=$(stack_gen --scaled-from "$STACK_STEP_BASE" --kind step-nauo --targets "$n")
    else
      out=$(stack_gen --scaled-from "$STACK_STEP_BASE" --kind step-nauo --targets "$n" --nauo-pitch "$pitch")
    fi
    gen_file_of "$out" step-nauo
    printf 'GEN,shape=%s,rung=n%s,%s\n' "$shape" "$n" "${GEN_LINE#SCALED,}" >> "$LINES"
    stack_rung "$shape" step "n$n" "$GEN_PATH"
    rm -f "$GEN_PATH"
    if [ "$RUNG_BAND" = over ]; then
      stack_not_run "$shape" $(for n in "${STACK_NAUO[@]:$((i + 1))}"; do printf 'n%s ' "$n"; done)
      return 0
    fi
  done
}

step_copies_gen() {
  local t=$1 out
  GEN_MOUNTS=()
  out=$(stack_gen --scaled-from "$STACK_STEP_BASE" --kind step-copies --targets "$t")
  gen_file_of "$out" step-copies
}

stack_s4() {
  local i t targets=()
  t=$STACK_S4_CEIL_TARGET
  while :; do
    step_copies_gen "$t"
    if [ "$(bytes_of "$GEN_PATH")" -le "$STACK_STEP_MAX_BYTES" ]; then break; fi
    printf 'RUNG_PICK,shape=S4,target=%sMiB,bytes=%s,reason=over_66060288\n' "$t" "$(bytes_of "$GEN_PATH")" >> "$LINES"
    rm -f "$GEN_PATH"
    t=$((t - 1))
    [ "$t" -gt "${STACK_S4[${#STACK_S4[@]}-1]}" ] || stack_stop "reason=s4_no_ceiling_rung"
  done
  printf 'RUNG_PICK,shape=S4,target=%sMiB,bytes=%s,reason=ceiling_rung\n' "$t" "$(bytes_of "$GEN_PATH")" >> "$LINES"
  rm -f "$GEN_PATH"
  targets=("${STACK_S4[@]}" "$t")
  for i in "${!targets[@]}"; do
    t=${targets[$i]}
    step_copies_gen "$t"
    printf 'GEN,shape=S4,rung=%sMiB,%s\n' "$t" "${GEN_LINE#SCALED,}" >> "$LINES"
    stack_rung S4 step "${t}MiB" "$GEN_PATH"
    rm -f "$GEN_PATH"
    if [ "$RUNG_BAND" = over ]; then
      stack_not_run S4 $(for t in "${targets[@]:$((i + 1))}"; do printf '%sMiB ' "$t"; done)
      return 0
    fi
  done
}

iges_of_copies() {
  local t=$1 out step
  step_copies_gen "$t"
  step=$GEN_PATH
  GEN_MOUNTS=()
  out=$(GEN_MEM=8g stack_gen --scaled-from "/out/$(basename "$step")" --kind iges-from-step --targets 1)
  gen_file_of "$out" iges-from-step
  IGES_STEP_BYTES=$(bytes_of "$step")
  rm -f "$step"
}

iges_pick() {
  local limit=$1 ratio=$2 t best="" best_path="" tries=0 b
  t=$(python3 -c "import sys; print(max(2, int(int(sys.argv[1]) / float(sys.argv[2]) / 1048576)))" "$limit" "$ratio")
  while [ "$tries" -lt 8 ]; do
    tries=$((tries + 1))
    iges_of_copies "$t"
    b=$(bytes_of "$GEN_PATH")
    printf 'RUNG_PICK,shape=S5,limit=%s,step_target=%sMiB,step_bytes=%s,iges_bytes=%s,fits=%s\n' "$limit" "$t" "$IGES_STEP_BYTES" "$b" \
      "$([ "$b" -le "$limit" ] && echo 1 || echo 0)" >> "$LINES"
    if [ "$b" -le "$limit" ]; then
      [ -z "$best_path" ] || rm -f "$best_path"
      best=$t
      best_path="$WORK/s5-pick-$t.igs"
      mv -f "$GEN_PATH" "$best_path"
      PICK_LINE=$GEN_LINE
      t=$((t + 1))
    else
      rm -f "$GEN_PATH"
      [ -z "$best" ] || break
      t=$((t - 1))
      [ "$t" -ge 2 ] || stack_stop "reason=s5_no_iges_under_limit,limit=$limit"
    fi
  done
  [ -n "$best" ] || stack_stop "reason=s5_pick_did_not_settle,limit=$limit"
  PICK_T=$best
  PICK_PATH=$best_path
}

stack_s5() {
  local ratio i limit limits=() labels=() t
  iges_of_copies 4
  ratio=$(python3 -c "import sys; print('%.6f' % (int(sys.argv[1]) / int(sys.argv[2])))" "$(bytes_of "$GEN_PATH")" "$IGES_STEP_BYTES")
  printf 'RUNG_PICK,shape=S5,calibration=step_copies_4MiB,step_bytes=%s,iges_bytes=%s,ratio=%s\n' "$IGES_STEP_BYTES" "$(bytes_of "$GEN_PATH")" "$ratio" >> "$LINES"
  rm -f "$GEN_PATH"
  for t in "${STACK_S5_MIB[@]}"; do limits+=("$((t * 1048576))"); labels+=("${t}MiB"); done
  limits+=("$STACK_IGES_MAX_BYTES"); labels+=(max)
  for i in "${!limits[@]}"; do
    limit=${limits[$i]}
    iges_pick "$limit" "$ratio"
    printf 'GEN,shape=S5,rung=%s,step_target=%sMiB,%s\n' "${labels[$i]}" "$PICK_T" "${PICK_LINE#SCALED,}" >> "$LINES"
    stack_rung S5 iges "${labels[$i]}" "$PICK_PATH"
    rm -f "$PICK_PATH"
    if [ "$RUNG_BAND" = over ]; then
      stack_not_run S5 "${labels[@]:$((i + 1))}"
      return 0
    fi
  done
}

gen_identity() {
  local ref out_main out_new sha_main sha_new base
  ref=main
  git rev-parse --verify -q "$ref^{commit}" >/dev/null || ref=origin/main
  mkdir -p "$WORK/maingen" "$WORK/gen/main" "$WORK/gen/new"
  chmod 0755 "$WORK/maingen"
  chmod 0777 "$WORK/gen/main" "$WORK/gen/new"
  git show "$ref:scripts/cad-corpus-gen.py" > "$WORK/maingen/cad-corpus-gen.py"
  chmod 0644 "$WORK/maingen/cad-corpus-gen.py"
  base=$1
  GEN_MOUNTS=(-v "$WORK/maingen:/maingen:ro")
  out_main=$(GEN_SCRIPT=/maingen/cad-corpus-gen.py GEN_OUT=/out/main stack_gen --scaled-from "/out/$(basename "$base")" --kind stl-tile --targets 2)
  GEN_MOUNTS=()
  out_new=$(GEN_OUT=/out/new stack_gen --scaled-from "/out/$(basename "$base")" --kind stl-tile --targets 2)
  sha_main=$(scaled_field sha "$(printf '%s\n' "$out_main" | grep '^SCALED,kind=stl-tile,')")
  sha_new=$(scaled_field sha "$(printf '%s\n' "$out_new" | grep '^SCALED,kind=stl-tile,')")
  local f_main f_new
  f_main=$(ls "$WORK/gen/main"/stl-tile-*.stl 2>/dev/null | head -n 1)
  f_new=$(ls "$WORK/gen/new"/stl-tile-*.stl 2>/dev/null | head -n 1)
  local eq=0
  if [ -n "$sha_main" ] && [ "$sha_main" = "$sha_new" ] && [ -s "$f_main" ] && [ "$(sha_of "$f_main")" = "$(sha_of "$f_new")" ]; then eq=1; fi
  printf 'GEN_IDENTITY,kind=stl-tile,base=%s,targets=2MiB,main_ref=%s,main_commit=%s,main_sha256=%s,new_sha256=%s,main_file_sha256=%s,new_file_sha256=%s,equal=%s\n' \
    "$(basename "$base")" "$ref" "$(git rev-parse "$ref")" "${sha_main:-none}" "${sha_new:-none}" \
    "$( [ -s "$f_main" ] && sha_of "$f_main" || echo none)" "$( [ -s "$f_new" ] && sha_of "$f_new" || echo none)" "$eq" >> "$LINES"
  rm -rf "$WORK/gen/main" "$WORK/gen/new"
  GEN_EQUAL=$eq
}

stack_smoke_mode() {
  local out bad
  stack_setup stack_smoke
  GEN_MOUNTS=()
  out=$(stack_gen --kind stl-stack --part cube --targets 64)
  gen_file_of "$out" stl-stack
  printf 'GEN,shape=S1,rung=k64,%s\n' "${GEN_LINE#SCALED,}" >> "$LINES"
  stack_rung S1 stl k64 "$GEN_PATH"
  gen_identity "$GEN_PATH"
  rm -f "$GEN_PATH"
  bad=$(grep '^STACK_ROW,' "$LINES" | grep -vc ',rc=0,' || true)
  if [ "$bad" = 0 ] && [ "$GEN_EQUAL" = 1 ] && [ "$(grep -c '^STACK_ROW,' "$LINES")" = 2 ]; then
    printf 'SUMMARY,mode=stack_smoke,rows=1,result=ok,stack_rows=2,gen_identity=1\n' >> "$LINES"
    grep -E '^(STACK_ROW|GEN_IDENTITY|SUMMARY),' "$LINES" >&2
    exit 0
  fi
  printf 'SUMMARY,mode=stack_smoke,rows=1,result=fail,stack_rows=%s,gen_identity=%s\n' "$(grep -c '^STACK_ROW,' "$LINES")" "$GEN_EQUAL" >> "$LINES"
  exit 1
}

stack_mode() {
  stack_setup stack
  python3 "$WORK/tools/stack.py" floors scripts/cad-liveproof.sh "$REPO_ROOT/internal/convert/testdata/cad" "$CAD_CACHE/fetched" >> "$LINES" \
    || stack_stop "reason=floors_failed"
  stack_s1
  stack_s2
  stack_nauo S3 grid
  stack_s4
  stack_s5
  stack_nauo S6 0
  python3 "$WORK/tools/stack.py" decide "$LINES" > "$WORK/decide.out" || { cat "$WORK/decide.out" >> "$LINES"; exit 1; }
  cat "$WORK/decide.out" >> "$LINES"
  grep -E '^(STACK_DECISION|STACK_GLOBAL|SUMMARY),' "$LINES" >&2
  exit 0
}

B76_EVID_REL=.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-02-bounds.txt
B76_STACK_REL=.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-01-stack.txt
B76_SETTER_S=1800
B76_CLASS_S=900
B76_N=5
B76_STEP_MAX=66060288
B76_IGES_MAX=141557760
B76_STL_MAX=190840832
B76_DXF_MAX=123731968
B76_ENTITIES=754762
B76_NAUO=1518
B76_TRI_CAP=3835382
B76_DXF_INSERTS=170031
B76_DXF_ENTITIES=850155
B76_C_ENTITIES=850155
B76_L170030_ENTITIES=850151
B76_ONRECORD_DXF=123.61
B76_ONRECORD_RSVG=96.60
B76_QUALITIES=(fine standard draft)
B76_VARIANTS=(iso:opaque iso:transparent top:opaque top:transparent)
B76_GROUPS=(refusal dxf stl step iges)
B76_DXF_CELLS=(C L170030 I170031x4 R123731968)
B76_STRESS=(chain fanout blocks wide)
B76_STRESS_REPS=2
B76_ENV3D=(-e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2)
B76_IGES_GEN_MEM=${B76_IGES_GEN_MEM:-8g}

b76_rule_lines() {
  local classes=$1 large=$2
  cat <<RULES
RULE,key=limits,mode=bounds76,rule=production_engine_stage_of_Dockerfile.cad-worker_(tag_octoconv-cad-worker:engine76-<inputs>;removed_on_exit);one_container_per_stage;--network_none_--cpus_2_--memory_4g_--memory-swap_4g_--user_nobody;cad-mesh_and_cad-render_confined_by_sandbox-probe_PROBE_RO=/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/<triplet>:/usr/local/bin/<helper>;cad-dxf_PROBE_RO=/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/share/fonts:/usr/lib/<triplet>:/usr/local/bin/cad-dxf;rsvg-convert_PROBE_RO=/usr/share/fonts:/usr/lib/<triplet>:/usr/bin/rsvg-convert;PROBE_RW=/job;cad-guard_unconfined_/usr/bin/python3_-IB_as_in_75-03_and_76-01;Go_judges_(cad-svg-check;cad-pdf-check;cad-pixel-judge)_unconfined_static_linux;guard;mesh;stats;render_env_HOME=/job_LANG=C.UTF-8_TMPDIR=/job_LP_NUM_THREADS=2_(cadStage;LP_NUM_THREADS=2_for_cad-render);cad-dxf_and_rsvg_env_HOME=/job/.home_XDG_CACHE_HOME=/job/.home/.cache_XDG_CONFIG_HOME=/job/.home/.config_TMPDIR=/job_(cadDrawStage);wrap_budget_1800_s_for_setter_rows_and_900_s_for_classification_rows_(measure_wrap.py)
RULE,key=load,mode=bounds76,rule=every_row_line_carries_load1_(host_vm.loadavg)_and_vm_free_gib_(df_of_the_OrbStack_VM_root);every_group_preflight_refuses_to_start_if_an_octoconv-_container_is_running;an_octoconv_k8s_pod_exists;a_go_test_process_runs;or_a_docker_build_or_buildx_process_runs;every_row_checks_the_VM_disk_first_and_exits_3_below_6_GiB_(STOP_reason=disk_below_threshold);a_60_s_watchdog_outside_the_script_kills_a_group_below_6_GiB;groups_run_strictly_one_after_another
RULE,key=cells_3d,mode=bounds76,rule=M1=step-copies_of_public/LinearSlide-MGN15x-Block.step_at_exactly_66060288_bytes:_the_largest_copies_count_K_the_production_guard_admits_(--max-entities_754762_--max-nauo_1518)_padded_by_one_STEP_comment_block_between_the_last_entity_instance_and_ENDSEC;_of_the_DATA_section_(the_block_holds_no_;_so_the_guard_count_is_unchanged);K_is_found_by_the_guard_itself_(CELL_PICK_lines:_K_ok_and_K+1_too_complex_at_the_same_66060288_bytes)_because_76-01_showed_step-copies_from_40_MiB_up_refused_(entities_over_cap)_so_a_63_MiB_file_of_copies_alone_is_not_admissible|M2=step-nauo_of_the_same_part_at_n=1518_(CADMaxNAUO)_on_the_generator's_200_mm_grid|M3=the_largest_iges-from-step_file_of_step-copies_of_LinearSlide:_K'_the_largest_copies_count_whose_unpadded_IGES_is_at_most_141557760_bytes_(CELL_PICK_lines_with_K'+1_over)_padded_with_blank_Start-section_records_(Terminate_S_count_updated)_to_within_one_record_of_141557760|M4=stl-tile_of_LARGE_STL=${large}_at_190840784_bytes_(3816814_triangles;the_largest_84+50xT_at_or_below_190840832)|classification(76-01_STACK_DECISION_not_byte_bound;break_shape_brackets;N=1;setter=0)=${classes}|stacked_cells=M1;M3_and_every_classification_rung_(coincident_copies_or_instances);non_stacked_cells=M2_(grid)_and_M4_(tiles)
RULE,key=qualities,mode=bounds76,rule=cad-mesh_rows_for_M1;M2;M3_at_draft;standard_and_fine:_fine_N=5_first;then_draft_and_standard_N=1_each;either_is_promoted_to_N=5_(reps_2..5)_if_its_N=1_wall_exceeds_fine's_maximum;M4_runs_cad-mesh_stats_N=5;classification_rungs_mesh_at_fine_N=1_and_also_at_standard_then_draft_while_the_previous_quality_is_over_3835382_triangles;cad-mesh_--format_<f>_/job/in.<f>_/job/mesh.stl_--quality_<q>_--measure_/job/draft.json_(meshStage_argv);cad-mesh_stats_--measure_/job/draft.json_/job/in.stl_/job/mesh.stl_(statsStage_argv);every_mesh_row_checks_the_STL_triangle_count_against_the_verdict
RULE,key=variants,mode=bounds76,rule=render_size_2048_at_the_admitted_quality_with_the_most_triangles_(at_most_3835382=CADMaxRenderTriangles)_from_the_rep-1_mesh_of_that_quality;views_iso_and_top;backgrounds_opaque_and_transparent;N=1_per_variant;the_slowest_N=1_variant_(a_wrap_timeout_counts_as_its_budget)_is_promoted_to_N=5_(reps_2..5);a_variant_whose_N=1_wall_exceeds_the_promoted_variant's_maximum_is_also_promoted;every_render_row_is_followed_by_stage_pixel_(cad-pixel-judge_--size_2048_--background_<b>=JudgeCADPNG)_on_its_PNG;a_cell_with_no_admitted_quality_is_band=refused_by_cap_and_writes_NOT_RUN_stage=render_instead_of_a_render_row;for_a_stacked_cell_whose_promoted_variant's_N=1_row_hit_the_1800_s_wrap_budget_reps_2..5_are_NOT_RUN_reason=refused_at_wrap_budget_(a_wrap-budget_render_is_over_every_R<=886_so_more_reps_cannot_change_its_band);classification_rungs_render_iso_2048_opaque_N=1_only
RULE,key=cells_dxf,mode=bounds76,rule=C=dxf-combo_on_dxf-replicate_of_public/uncommon.dxf_at_118_MiB_with_min(170031-I;floor((850155-E-1)/5))_cells_(75-05_rule_e;I;E_the_replicate's_guard_counts)_and_exactly_850155_expanded_entities_by_the_guard_else_STOP|L170030=dxf-minsert_5-entity_block_170030_cells_(850151_entities)|I170031x4=dxf-minsert_--block-entities_4_at_the_170031_insert_cap_(680125_entities)|R123731968=dxf-replicate_of_uncommon.dxf_at_exactly_123731968_bytes_(999_comment_pairs_before_0/EOF);each_N=5;stages_guard_(--max-expanded-inserts_170031_--max-expanded-entities_850155_--max-nesting_16_--max-abs-coordinate_1e12);dxf_(cad-dxf_--max-drawn-entities_850155_--texts_--render-svg_--measure);svg_client;svg_render_(cad-svg-check);rsvg_ink_(rsvg-convert_-b_white_at_the_2048_letterbox_of_cad-pixel-judge_--print-letterbox=the_dxfInk_argv;also_the_argv_of_the_opaque_PNG_route_at_2048);pixel_ink_(cad-pixel-judge_--ink=JudgeCADDrawingInk);pixel_opaque_(--drawing_opaque_on_the_ink_frame);rsvg_pdf_(rsvg-convert_-f_pdf_-b_white);pdf_(cad-pdf-check);rsvg_png_transparent;pixel_transparent
RULE,key=refusal,mode=bounds76,rule=L170031=dxf-minsert_5-entity_block_170031_cells_(850156_expanded_entities)_through_the_production_guard_N=1;the_verdict_must_be_too_complex_(REFUSAL_ROW_result=ok)_else_STOP;its_guard_row_is_a_setter_guard_row_with_band=refused_by_cap
RULE,key=guard_stress,mode=bounds76,rule=the_four_75-03_stress_shapes_chain;fanout;blocks;wide_at_118_MiB_(dxf-guard-stress;generated_in_the_engine_image)_through_the_production_guard_N=2;expected_chain=too_complex/nesting_too_deep;fanout=too_complex/expanded_inserts_over_cap;wide=too_complex/expanded_inserts_over_cap;blocks=ok;setter_guard_rows;a_refusal_is_band=refused_by_cap;another_verdict_is_STOP
RULE,key=pathb_ladder,mode=bounds76,rule=only_for_a_format_the_76-01_decision_marks_byte_bound:_its_76-01_copies_rungs_rendered_N=1_at_the_admitted_quality_with_the_most_triangles_and_the_slowest_variant;then_N=5_at_the_three_largest_rungs_whose_N=1_wall_W_satisfies_ceil(1.5W)<=886-30-ceil(1.5_x_max_mesh_wall_so_far)-90_(90=1.5x60_a_provisional_I/O);76-03_chooses_with_the_measured_I/O_and_STOPs_if_none_of_the_three_fits;with_no_byte_bound_format_the_group_is_NOT_RUN_reason=no_byte_bound_format
RULE,key=reps,mode=bounds76,rule=rep_numbers_1..N_per_(cell;stage;quality;view;background);setter=1_on_every_row_of_a_D-02_cell_(M1-M4;C;L170030;I170031x4;R123731968;L170031;the_stress_shapes);setter=0_on_every_classification_row;a_promoted_variant_or_quality_keeps_its_N=1_row_as_rep_1
RULE,key=stat,mode=bounds76,rule=W_per_stage=the_maximum_wall_s_over_setter_rows_of_that_stage_(mesh_covers_mesh_and_stats;rsvg_covers_rsvg_ink;rsvg_pdf;rsvg_png_transparent);every_rep;every_loaded_run;every_band=refused_setter_row_at_its_wall_s_(a_wrap_timeout_at_its_budget)_included;plus_the_on-record_walls;no_percentile;no_exclusion;no_re-run_to_replace_a_row;a_W_that_breaks_886_is_resolved_by_D-04_Path-B_in_76-03_never_by_exclusion
RULE,key=onrecord,mode=bounds76,rule=DXF_W_also_includes_every_75-05_BOUND_ROW_wall_of_the_same_stage_(evidence/75-05-bounds.txt;rsvg_png_opaque_counts_as_rsvg);cad-dxf_123.61_s_(0og_row_C)_and_rsvg_96.60_s_(0og_L170031_ink_raster;quick/261013-0og/evidence/probe-ink.txt_lines_125-130);3D_W_uses_only_76_rows_(AR-6)
RULE,key=deadline_stage,mode=bounds76,rule=guard;mesh_(mesh_and_stats_rows);dxf_and_rsvg_(ink;pdf;png)_deadlines_are_each_ceil(1.5_x_W)
RULE,key=deadline_render,mode=bounds76,rule=76-01_STACK_GLOBAL_render_deadline_rule=remainder:_render=R=886-guard-mesh-io;every_non-stacked_render_row_(M2;M4_and_every_cell_of_a_within_budget_format)_must_satisfy_ceil(1.5W)<=R_else_STOP;every_stacked_render_row_is_classified_band=margin_(ceil(1.5W)<=R);no_margin_(W<=R<ceil(1.5W))_or_refused_(W>R_or_a_wrap_timeout);with_ceil_1.5W_the_render_deadline_would_be_ceil(1.5_x_W_render)_over_admitted_render_rows
RULE,key=bands,mode=bounds76,rule=76-02_writes_band=within_(stage_ok);refused_by_cap_(a_guard_too_complex_refusal_or_a_mesh_over_3835382_triangles);refused_(a_wrap_timeout_or_an_OOM_kill_of_a_stacked_cell_or_a_classification_rung);failed_(STOP);the_margin;no_margin;refused_classification_against_R_is_assigned_in_76-03_from_these_walls;every_row_carries_stacked=0|1
RULE,key=inworker,mode=bounds76,rule=rows_run_through_the_API_of_a_compose_stack_built_from_a_fresh_clone;the_cad-worker_limited_to_2_CPU_and_4g;measurement_deadlines_from_an_untracked_docker-compose.override.yml_(guard_120s;mesh_900s;render_900s;dxf_600s;rsvg_600s;engine_3600s)_never_production_values;webhook-worker_absent;the_container_restarts_before_each_row_and_the_preflight-only_memory.peak_is_recorded;a_/proc_watcher_polls_every_0.2_s_for_cad-guard;cad-mesh;cad-render;cad-dxf_and_rsvg-convert_recording_each_start_(/proc/PID/stat_starttime)_and_last-seen_time;attempt_wall=finished_at-started_at_with_attempts=1;go_peak=VmHWM_of_PID_1;whole-container_memory.peak_and_memory.events_oom_kill;cells=the_worst_margin-band_cell_of_each_route_(3D_convert_step;3D_convert_iges;3D_render_step;iges_and_stl;DXF_svg;pdf_and_png)_N=3
RULE,key=io,mode=bounds76,rule=io=ceil(1.5_x_max_over_in-worker_rows_of_(attempt_wall-sum_of_the_stage_walls_observed_in_that_attempt));covers_S3_download_and_upload_plus_the_Go-side_validation;judges_and_DB_transitions
RULE,key=engine,mode=bounds76,rule=engine=max(guard+mesh+render+io;guard+dxf+2xrsvg+io);at_most_886_else_the_D-04_Path-B_lever_for_a_byte_bound_format_else_STOP
RULE,key=crosscheck,mode=bounds76,rule=engine>=ceil(1.5_x_the_slowest_in-worker_attempt_wall_of_every_route)_else_STOP;W_also_takes_every_in-worker_observed_stage_wall
RULE,key=pod,mode=bounds76,rule=go_peak=max_VmHWM_over_in-worker_rows;stage_peak=max_peak_mib_over_admitted_setter_rows;stage_peak+go_peak<=0.70x4096=2867.2_MiB_else_STOP;every_in-worker_oom_kill=0_and_memory.peak<4096_else_STOP;closes_GAP_go_worker_rss_dxf_(D-06)
RULE,key=bytes,mode=bounds76,rule=cad_dxf_svg_max_bytes_and_cad_dxf_pdf_max_bytes=ceil(1.5_x_max_output_bytes_over_admitted_DXF_rows)_rounded_up_to_a_whole_MiB_(75-05_rule_g;svg=the_client_and_render-only_SVG_of_cad-dxf);every_PNG_at_most_96_MiB
RULE,key=ceilings,mode=bounds76,rule=STEP_66060288;IGES_141557760;STL_190840832;DXF_123731968_bytes;DXF_caps_170031_inserts;850155_entities;nesting_16;STEP_754762_entities;1518_NAUO;3835382_render_triangles_restated_unchanged_unless_a_rule_above_lowers_them;nothing_is_raised
RULE,key=grace,mode=bounds76,rule=cad_pod_grace_s=engine+20;asynq_ShutdownTimeout=engine+10;compose_stop_grace_period=cad_pod_grace_s;keda.cad.scaleDownStabilizationSeconds_(900)_must_be_at_least_engine_(D-05)
RULE,key=failures,mode=bounds76,rule=a_setter_row_with_rc_other_than_0_that_is_not_a_timeout_or_OOM_kill_(and_not_an_expected_guard_refusal)_is_STOP;a_setter_row_with_a_timeout_or_OOM_kill_is_STOP_for_a_non-stacked_cell_and_band=refused_for_a_stacked_cell;a_classification_row_timeout_or_OOM_is_band=refused;a_judge_that_is_not_ok_is_STOP;a_guard_verdict_other_than_the_expected_one_is_STOP;a_sniff_other_than_the_cell's_format_is_STOP;a_generator_self-check_failure_is_STOP;a_STOP_is_never_followed_by_DERIVED_lines
RULE,key=cells_exact,mode=bounds76,rule=scripts/cad-corpus-gen.py_--exact-bytes_N_writes_the_largest_file_of_the_kind_at_or_below_N_and_pads_it:_STEP_one_comment_block_with_no_;_before_ENDSEC;_of_DATA;DXF_999_comment_pairs_before_0/EOF;IGES_blank_Start-section_records;STL_no_pad_(T=floor((N-84)/50));pad_gap_bytes_below_one_record_(STEP;DXF_0;STL_<50;IGES_<81_or_82);every_cell_is_sniffed_(cad-sniff)_and_guarded_with_the_production_caps_before_any_timed_row_and_written_as_a_CELL_line;a_cell_file_is_reused_from_\$OCTOCONV_CAD_CACHE/scaled/b76_only_when_the_generator;cad-guard_and_the_argument_key_match_its_meta_and_its_sha256_matches
RULE,key=order,mode=bounds76,rule=groups_refusal;dxf;stl;step;iges_(and_pathb_only_if_76-01_marks_a_format_byte_bound)_then_finish;within_a_group_cells_run_one_after_another_and_reps_run_cell-major;a_group_refuses_a_commit_or_image_id_other_than_the_init_HDR_and_refuses_to_run_twice;a_STOP_ends_the_plan
RULES
}

b76_tools() {
  cat > "$WORK/tools/b76.py" <<'PY'
import json
import os
import re
import struct
import sys

STAGES_GO = ("svg_client", "svg_render", "pdf", "pixel_ink", "pixel_opaque", "pixel_transparent", "pixel")


def kv(line):
    head, _, rest = line.partition(",")
    out = {"_": head}
    for part in rest.split(","):
        if "=" in part:
            k, v = part.split("=", 1)
            out[k] = v
    return out


def wrap_fields(path):
    try:
        with open(path, errors="replace") as fh:
            for line in fh:
                if line.startswith("WRAP,"):
                    return dict(p.split("=", 1) for p in line.strip().split(",")[1:] if "=" in p)
    except OSError:
        pass
    return {}


def log_lines(path):
    try:
        with open(path, "rb") as fh:
            return [raw.decode("utf-8", "replace").strip() for raw in fh]
    except OSError:
        return []


def verdict_of(lines):
    obj = {}
    for line in lines:
        if line.startswith("CADVERDICT "):
            try:
                obj = json.loads(line[len("CADVERDICT "):])
            except ValueError:
                obj = {}
    return obj


def judge_of(lines):
    for line in lines:
        if line.startswith(("GOSVG,", "GOPDF,", "GOJUDGE,")):
            return dict(p.split("=", 1) for p in line.split(",")[1:] if "=" in p)
    return None


def stl_count(path):
    try:
        with open(path, "rb") as fh:
            head = fh.read(84)
        if len(head) == 84 and os.path.getsize(path) == 84 + 50 * struct.unpack("<I", head[80:84])[0]:
            return struct.unpack("<I", head[80:84])[0]
    except OSError:
        pass
    return None


def clean(v):
    return re.sub(r"[,=\s]", "_", str(v))


def num(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return None


def row(argv):
    a = dict(x.split("=", 1) for x in argv)
    stage = a["stage"]
    f = wrap_fields(a["wrap"])
    lines = log_lines(a["log"])
    v = verdict_of(lines)
    rc = f.get("rc", "na")
    timeout = f.get("timeout", "na")
    oom = f.get("oom_kill", "na")
    wall = f.get("wall_s", "na")
    verdict = v.get("verdict", "none")
    reason = v.get("reason") or "none"
    triangles = a.get("triangles", "na")
    detail = "-"
    cls = "fail"
    why = "none"
    if not f:
        cls = "oom" if a["rc_w"] == "137" else "fail"
        oom = "container_137" if a["rc_w"] == "137" else oom
        why = "no_wrap_line_rc_w_%s" % a["rc_w"]
    elif timeout == "1":
        cls = "timeout"
    elif oom not in ("na", "0"):
        cls = "oom"
    elif rc == "0":
        cls = "ok"
    elif stage == "guard" and rc == "4" and verdict == "too_complex":
        cls = "refused"
    elif stage in ("mesh", "stats") and rc == "4" and verdict == "too_complex" and reason == "out_of_memory":
        cls = "oom"
    else:
        why = "rc_%s_verdict_%s_reason_%s" % (rc, verdict, reason)
    expect = a.get("expect", "ok")
    if stage == "guard":
        keys = ("expanded_inserts", "expanded_entities", "entities", "nauo", "expanded", "nesting", "blocks", "records")
        detail = "+".join("%s:%s" % (k, clean(v[k])) for k in keys if k in v) or "-"
        got = "ok" if verdict == "ok" else "%s/%s" % (verdict, reason)
        if cls == "ok" and verdict != "ok":
            cls, why = "fail", "guard_rc_0_verdict_%s" % verdict
        if cls in ("ok", "refused"):
            want_ok = expect == "ok"
            if want_ok and cls != "ok":
                cls, why = "fail", "guard_%s_expected_ok" % clean(got)
            elif not want_ok and not (got == expect or (expect == "too_complex" and verdict == "too_complex")):
                cls, why = "fail", "guard_%s_expected_%s" % (clean(got), clean(expect))
    elif stage in ("mesh", "stats"):
        if cls == "ok":
            n = stl_count(a["mesh"])
            if n is None or v.get("triangles") != n or verdict != "ok":
                cls, why = "fail", "stl_count_%s_verdict_%s_%s" % (n, v.get("triangles"), verdict)
            triangles = str(n)
    elif stage == "render":
        if cls == "ok" and (verdict != "ok" or v.get("size") != int(a["size"]) or v.get("view") != a["view"] or v.get("background") != a["background"]):
            cls, why = "fail", "render_verdict_%s_view_%s_size_%s_background_%s" % (verdict, v.get("view"), v.get("size"), v.get("background"))
    elif stage == "dxf":
        keys = ("entities_drawn", "texts", "uncovered", "render_substituted", "hatch_truncated", "page_w_mm", "page_h_mm")
        detail = "+".join("%s:%s" % (k, clean(v[k])) for k in keys if k in v) or "-"
        if cls == "ok" and verdict != "ok":
            cls, why = "fail", "dxf_verdict_%s_%s" % (verdict, reason)
    if stage in STAGES_GO:
        j = judge_of(lines)
        if j is not None:
            detail = "+".join("%s:%s" % (k, clean(x)) for k, x in j.items() if k not in ("svg", "pdf", "png", "ok"))[:400] or "-"
        if cls == "ok" and (j is None or j.get("ok") != "1"):
            cls, why = "fail", "judge_not_ok"
    stacked = a.get("stacked", "0")
    setter = a.get("setter", "1")
    if cls == "ok":
        band = "within"
    elif cls == "refused":
        band = "refused_by_cap"
    elif cls in ("timeout", "oom"):
        band = "refused" if (stacked == "1" or setter == "0") else "failed"
        if band == "failed":
            why = "%s_on_a_non_stacked_setter_cell" % cls
    else:
        band = "failed"
    vals = [num(f.get("child_rss_mib")), num(f.get("cg_peak_mib"))]
    vals = [x for x in vals if x is not None]
    peak = max(vals) if vals else None
    if cls == "oom":
        peak = max(peak or 0, 4096)
    peak_s = "na" if peak is None else ("%d" % peak if float(peak).is_integer() else "%s" % peak)
    out = a.get("out", "")
    out_bytes = str(os.path.getsize(out)) if out and os.path.isfile(out) else "na"
    extra = a.get("extra", "")
    print("BOUND76_ROW,cell=%s,format=%s,group=%s,stage=%s,quality=%s,view=%s,background=%s,size=%s,triangles=%s,rep=%s,rc=%s,wall_s=%s,"
          "child_rss_mib=%s,cg_peak_mib=%s,oom_kill=%s,timeout=%s,peak_mib=%s,out_bytes=%s,setter=%s,band=%s,load1=%s,vm_free_gib=%s,"
          "stacked=%s,bytes=%s,sha256=%s,verdict=%s,reason=%s,class=%s,why=%s,threads_max=%s,budget_s=%s,detail=%s%s" % (
              a["cell"], a["format"], a["group"], stage, a.get("quality", "na"), a.get("view", "na"), a.get("background", "na"),
              a.get("size", "na"), triangles, a["rep"], rc, wall, f.get("child_rss_mib", "na"), f.get("cg_peak_mib", "na"), oom, timeout,
              peak_s, out_bytes, setter, band, a["load1"], a["vmfree"], stacked, a["bytes"], a["sha"], clean(verdict), clean(reason), cls,
              clean(why), f.get("threads_max", "na"), a["budget"], detail, extra))


def guard_probe(argv):
    lines = log_lines(argv[0])
    v = verdict_of(lines)
    verdict = v.get("verdict", "none")
    reason = v.get("reason") or "none"
    inserts = v.get("expanded_inserts", "na")
    if "expanded_entities" in v:
        entities = v["expanded_entities"]
    else:
        entities = v.get("entities", "na")
    instances = v.get("expanded", v.get("nauo", "na"))
    print("%s %s %s %s %s %s" % (verdict, clean(reason), inserts, entities, instances, v.get("entities", "na")))


def brackets(argv):
    path = argv[0]
    decisions, glob_line, rows = {}, None, []
    for line in open(path):
        line = line.rstrip("\n")
        if line.startswith("STACK_DECISION,"):
            d = kv(line)
            decisions[d["format"]] = d
        elif line.startswith("STACK_GLOBAL,"):
            glob_line = kv(line)
        elif line.startswith("STACK_ROW,"):
            rows.append(kv(line))
    missing = [f for f in ("step", "iges", "stl") if f not in decisions]
    if missing or glob_line is None:
        print("STOP reason=76-01_lines_missing:%s%s" % ("+".join(missing), "" if glob_line else "+STACK_GLOBAL"))
        return 1
    print("GLOBAL %s" % glob_line.get("render_deadline_rule"))
    for fmt in ("stl", "step", "iges"):
        d = decisions[fmt]
        print("DECISION %s %s" % (fmt, d["decision"]))
        if d["decision"] != "not_byte_bound":
            continue
        shape = d["break_shape"]
        order, band = [], {}
        for r in rows:
            if r["shape"] != shape:
                continue
            if r["rung"] not in band:
                order.append(r["rung"])
                band[r["rung"]] = "within"
            if r["band"] == "over":
                band[r["rung"]] = "over"
        first_over = next((i for i, rg in enumerate(order) if band[rg] == "over"), None)
        if first_over is None:
            print("STOP reason=no_over_rung_for_%s" % shape)
            return 1
        below = [rg for rg in order[:first_over] if band[rg] == "within"]
        print("BRACKET %s %s %s %s" % (fmt, shape, below[-1] if below else "none", order[first_over]))
    return 0


def contaminated_lines(lines):
    out = set()
    for line in lines:
        if line.startswith("NOTE,key=contaminated_row,"):
            d = kv(line.rstrip("\n"))
            if d.get("contaminated") == "1" and d.get("line", "").isdigit():
                out.add(int(d["line"]))
    return out


def pmset_intervals(text):
    import datetime
    ev = []
    for line in text.splitlines():
        m = re.match(r"^(\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}) ([+-]\d{4}) ([^\t]*?)\s*\t", line)
        if not m:
            continue
        kind = m.group(3).strip()
        if kind not in ("Sleep", "DarkWake", "Wake"):
            continue
        t = datetime.datetime.strptime(m.group(1) + " " + m.group(2), "%Y-%m-%d %H:%M:%S %z").astimezone(datetime.timezone.utc)
        ev.append((t, kind))
    out, start = [], None
    for t, kind in ev:
        if kind == "Sleep" and start is None:
            start = t
        elif kind in ("DarkWake", "Wake") and start is not None:
            out.append((start, t, kind))
            start = None
    if start is not None:
        out.append((start, None, "none"))
    return out, ev


def utc(v):
    import datetime
    return datetime.datetime.strptime(v, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=datetime.timezone.utc)


def zs(t):
    return "open" if t is None else t.strftime("%Y-%m-%dT%H:%M:%SZ")


def overlapping(intervals, a, b):
    out = []
    for s0, e0, k in intervals:
        e1 = b if e0 is None else e0
        if s0 < b and e1 > a:
            out.append((s0, e0, k))
    return out


def sleeps(argv):
    since, until = utc(argv[0]), utc(argv[1])
    ivs, _ = pmset_intervals(sys.stdin.read())
    print("+".join("%s..%s" % (zs(a), zs(b)) for a, b, k in overlapping(ivs, since, until)))
    return 0


def sleepstop(argv):
    import datetime
    path, rowlog, group, now_s = argv
    now = utc(now_s)
    text = open(path).read().splitlines()
    hidx = [i for i, l in enumerate(text) if l.startswith("HDR,group=%s," % group)]
    if len(hidx) != 1:
        print("group_%s_hdr_count_%d" % (group, len(hidx)))
        return 1
    hdr_t = utc(kv(text[hidx[0]])["date_utc"])
    ivs, ev = pmset_intervals(sys.stdin.read())
    hit = overlapping(ivs, hdr_t, now)
    if not hit:
        print("no_pmset_sleep_overlaps_group_%s" % group)
        return 1
    full = next((t for t, k in ev if k == "Wake" and t >= hit[0][0]), None)
    log = []
    day = hdr_t.date()
    prev = None
    for line in open(rowlog, errors="replace"):
        m = re.match(r"^\[(\d\d):(\d\d):(\d\d)\] (.*)$", line.rstrip("\n"))
        if not m:
            continue
        t = datetime.datetime(day.year, day.month, day.day, int(m.group(1)), int(m.group(2)), int(m.group(3)), tzinfo=datetime.timezone.utc)
        if prev is not None and t < prev:
            day = day + datetime.timedelta(days=1)
            t = t + datetime.timedelta(days=1)
        if t < hdr_t - datetime.timedelta(seconds=60):
            t = t + datetime.timedelta(days=1)
            day = t.date()
        prev = t
        f = dict(x.split("=", 1) for x in m.group(4).split() if "=" in x)
        log.append((t, f))
    rows = [(i + 1, kv(l)) for i, l in enumerate(text) if i > hidx[0] and l.startswith("BOUND76_ROW,") and kv(l).get("group") == group]
    keys = ("cell", "stage", "quality", "view", "background", "rep")
    pos, notes, bad = 0, [], 0
    for ln, d in rows:
        j = pos
        while j < len(log) and not ("stage" in log[j][1] and all(log[j][1].get(k) == d.get(k) for k in keys)):
            j += 1
        if j < len(log):
            end = log[j][0]
            start = log[j - 1][0] if j > 0 else hdr_t
            pos = j + 1
            ov = sum(max(0.0, (min(end, b or now) - max(start, a)).total_seconds()) for a, b, k in ivs)
            src = "row_log"
            cont = ov > 0
        else:
            start = end = None
            ov = None
            src = "none"
            cont = True
        if cont:
            bad += 1
            notes.append("NOTE,key=contaminated_row,mode=bounds76,group=%s,line=%d,cell=%s,stage=%s,quality=%s,view=%s,background=%s,rep=%s,contaminated=1,"
                         "row_start_utc=%s,row_end_utc=%s,overlap_s=%s,wall_s=%s,rc=%s,timeout=%s,interval_source=%s,excluded_from_W=1" % (
                             group, ln, d.get("cell"), d.get("stage"), d.get("quality"), d.get("view"), d.get("background"), d.get("rep"),
                             zs(start) if start else "unknown", zs(end) if end else "unknown", "unknown" if ov is None else "%d" % ov,
                             d.get("wall_s"), d.get("rc"), d.get("timeout"), src))
    allrows = sum(1 for l in text if l.startswith("BOUND76_ROW,") and kv(l).get("group") == group)
    print("STOP,mode=bounds76,group=%s,reason=host_sleep,source=pmset_-g_log,first_sleep_utc=%s,full_wake_utc=%s,sleep_intervals_utc=%s,"
          "interval=Sleep_to_the_next_DarkWake_or_Wake" % (group, zs(hit[0][0]), zs(full), "+".join("%s..%s" % (zs(a), zs(b)) for a, b, k in hit)))
    print("GROUP_END,group=%s,rows=%d,duration_s=%d,date_utc=%s,result=STOP" % (group, allrows, int((now - hdr_t).total_seconds()), now_s))
    for n in notes:
        print(n)
    print("NOTE,key=host_sleep_attempt,mode=bounds76,group=%s,hdr_utc=%s,stopped_utc=%s,attempt_rows=%d,contaminated=%d,clean=%d,"
          "note=clean_rows_of_the_stopped_attempt_stay_and_count_in_W;contaminated_rows_are_excluded_from_W" % (
              group, zs(hdr_t), now_s, len(rows), bad, len(rows) - bad))
    return 0


def maxwall(argv):
    path, cell, stage, quality, view, background = argv
    best = None
    lines = open(path).read().splitlines()
    skip = contaminated_lines(lines)
    for n, line in enumerate(lines, 1):
        if n in skip or not line.startswith("BOUND76_ROW,"):
            continue
        d = kv(line.rstrip("\n"))
        if d["cell"] != cell or d["stage"] != stage or d["quality"] != quality or d["view"] != view or d["background"] != background:
            continue
        w = num(d["wall_s"])
        if w is None:
            continue
        best = w if best is None or w > best else best
    print("na" if best is None else "%.2f" % best)


def fixnote(argv):
    path = argv[0]
    text = open(path).read().splitlines()
    stops = [kv(l) for l in text if l.startswith("STOP,") and ",reason=row_failed," in l and ",stage=pixel," in l]
    hdrs = [i for i, l in enumerate(text) if l.startswith("HDR,group=step,") and ",rerun_key=rerun_after_product_fix," in l]
    if len(stops) != 1 or not hdrs:
        print("NOTE,key=fix_row_compare,mode=bounds76,result=not_determinable,why=no_single_judge_stop_or_no_fix_hdr")
        return 0
    s = stops[0]
    keys = ("cell", "quality", "view", "background", "rep")

    def match(d, stage):
        return d.get("stage") == stage and all(d.get(k) == s.get(k) for k in keys)

    old_render = old_pixel = new_render = new_pixel = None
    for i, l in enumerate(text):
        if not l.startswith("BOUND76_ROW,"):
            continue
        d = kv(l)
        if i < hdrs[-1]:
            if match(d, "pixel") and d.get("band") == "failed":
                old_pixel = d
            elif match(d, "render") and old_pixel is None:
                old_render = d
        else:
            if match(d, "render") and new_render is None:
                new_render = d
            elif match(d, "pixel") and new_pixel is None:
                new_pixel = d
    if not (old_render and old_pixel and new_render and new_pixel):
        print("NOTE,key=fix_row_compare,mode=bounds76,cell=%s,result=not_determinable,why=rows_missing" % s.get("cell"))
        return 0

    def facts(d):
        return dict(p.split(":", 1) for p in d.get("detail", "").split("+") if ":" in p)

    fo, fn = facts(old_pixel), facts(new_pixel)
    same = lambda k: "1" if old_render.get(k) == new_render.get(k) else "0"
    cmp = "+".join("%s:%s>%s" % (k, fo.get(k, "na"), fn.get(k, "na")) for k in ("mode", "count", "dark_count", "box", "margin_px", "alpha", "frac_transparent", "ring_nontransparent"))
    print("NOTE,key=fix_row_compare,mode=bounds76,cell=%s,quality=%s,view=%s,background=%s,rep=%s,input_sha256_same=%s,triangles_same=%s,"
          "render_out_bytes=%s>%s,out_bytes_same=%s,png_sha256=not_recorded_in_either_attempt,png_bytes_identical=not_determinable,"
          "judge_argv_same=1,old_pixel_class=%s,new_pixel_class=%s,facts=%s,render_wall_s=%s>%s,"
          "note=the_judge_input_is_the_render_PNG_of_the_same_cell_file_and_mesh_quality;its_size_is_compared;its_bytes_were_not_hashed;count_differs_by_the_fixed_part-pixel_definition_even_for_identical_bytes" % (
              s.get("cell"), s.get("quality"), s.get("view"), s.get("background"), s.get("rep"), same("sha256"), same("triangles"),
              old_render.get("out_bytes"), new_render.get("out_bytes"), same("out_bytes"), old_pixel.get("class"), new_pixel.get("class"), cmp,
              old_render.get("wall_s"), new_render.get("wall_s")))
    return 0


def finish(argv):
    path, groups = argv[0], argv[1].split("+")
    text = open(path).read().splitlines()
    problems = []
    amend = [kv(l) for l in text if l.startswith("RULE_AMEND,key=rerun_after_watchdog,")]
    amend_fix = [kv(l) for l in text if l.startswith("RULE_AMEND,key=rerun_after_product_fix,")]
    watchdog = [kv(l) for l in text if l.startswith("STOP,") and ",reason=watchdog_disk_below_threshold," in l + ","]
    amend_sleep = [kv(l) for l in text if l.startswith("RULE_AMEND,key=rerun_after_host_sleep,")]
    slept_stops = [kv(l) for l in text if l.startswith("STOP,") and ",reason=host_sleep," in l + ","]
    if slept_stops and (len(amend_sleep) != 1 or len(slept_stops) != 1):
        problems.append("host_sleep_stop_without_one_amendment_or_more_than_one")
    slept = {d.get("group") for d in slept_stops}
    other = [kv(l) for l in text if l.startswith("STOP,") and ",reason=watchdog_disk_below_threshold," not in l + "," and ",reason=host_sleep," not in l + ","]
    fix_ok = len(amend_fix) == 1 and len(other) == 1 and other[0].get("group") == "step" and other[0].get("reason") == "row_failed" and other[0].get("stage") == "pixel"
    if other and not fix_ok:
        problems.append("stop_line_present")
    if watchdog and len(amend) != 1:
        problems.append("watchdog_stop_without_the_rerun_amendment")
    stopped = {d.get("group") for d in watchdog}
    fixed = {other[0].get("group")} if fix_ok else set()
    init = [kv(l) for l in text if l.startswith("HDR,mode=bounds76,commit=")]
    image = [kv(l) for l in text if l.startswith("HDR,mode=bounds76,image=")]
    if len(init) != 1 or len(image) != 1:
        problems.append("init_hdr_missing")
    last_rule = max((i for i, l in enumerate(text) if l.startswith("RULE,")), default=-1)
    first_row = min((i for i, l in enumerate(text) if l.startswith(("BOUND76_ROW,", "REFUSAL_ROW,", "CELL,"))), default=len(text))
    if last_rule < 0 or first_row < last_rule:
        problems.append("a_row_before_the_last_rule")
    fix_commit = None
    for g in groups:
        hdr = [kv(l) for l in text if l.startswith("HDR,group=%s," % g)]
        end = [kv(l) for l in text if l.startswith("GROUP_END,group=%s," % g)]
        want = 1 + (g in stopped) + (g in fixed) + (g in slept)
        reruns = [h.get("rerun") for h in hdr[1:]]
        expect = (["1"] if g in stopped else []) + (["2"] if g in fixed else []) + (["3"] if g in slept else [])
        if len(hdr) != want or reruns != expect:
            problems.append("group_%s_hdr_count_%d" % (g, len(hdr)))
            continue
        last = hdr[-1]
        if g in slept:
            same_in = last.get("commit") == hdr[-2].get("commit") or (last.get("inputs_identical_to_stopped_hdr") == "1" and last.get("stopped_hdr_commit") == hdr[-2].get("commit"))
            if last.get("rerun_key") != "rerun_after_host_sleep" or last.get("prevent_system_sleep") != "1" or not same_in \
                    or last.get("image_id") != hdr[-2].get("image_id"):
                problems.append("group_%s_sleep_rerun_hdr" % g)
        if g in fixed:
            if last.get("rerun_key") != "rerun_after_product_fix":
                problems.append("group_%s_fix_rerun_key" % g)
            fix_commit = last.get("commit")
        if init and image:
            same_commit = last.get("commit") == init[0].get("commit")
            same_image = last.get("image_id") == image[0].get("image_id")
            fix_inputs = amend_fix and last.get("inputs_identical_except") == "judge" and last.get("judge_commit") == amend_fix[0].get("judge_commit")
            if not same_commit and not ((amend and last.get("inputs_identical") == "1") or fix_inputs):
                problems.append("group_%s_commit_differs" % g)
            if not same_image and not ((amend or amend_fix) and last.get("image") == image[0].get("image")):
                problems.append("group_%s_image_differs" % g)
        oks = [e for e in end if e.get("result") == "ok"]
        stops = [e for e in end if e.get("result") != "ok"]
        if len(oks) != 1 or not end or end[-1].get("result") != "ok" or len(stops) != (1 if g in fixed else 0) + (1 if g in slept else 0):
            problems.append("group_%s_did_not_end_ok" % g)
    fpos = [i for i, l in enumerate(text) if l.startswith("HDR,group=") and ",rerun_key=rerun_after_product_fix," in l]
    if fixed and fpos:
        for i, l in enumerate(text):
            h = kv(l) if l.startswith("HDR,group=") else {}
            if i > fpos[-1] and h and h.get("commit") != fix_commit and not (h.get("inputs_identical_to_stopped_hdr") == "1" and h.get("stopped_hdr_commit") == fix_commit):
                problems.append("group_%s_not_at_the_fix_commit" % kv(l).get("group"))
    rows = [l for l in text if l.startswith("BOUND76_ROW,")]
    failed = [kv(l) for l in rows if ",band=failed," in l]
    if fixed:
        s0 = other[0]
        failed = [d for d in failed if not (d.get("group") == s0.get("group") and d.get("stage") == s0.get("stage") and all(
            d.get(k) == s0.get(k) for k in ("cell", "quality", "view", "background", "rep")))]
        if len([l for l in rows if ",band=failed," in l]) != 1:
            problems.append("failed_band_rows_besides_the_fix_stop")
    if failed:
        problems.append("failed_band_row")
    if problems:
        print("STOP,mode=bounds76,reason=finish_check_failed,detail=%s" % "+".join(problems))
        print("SUMMARY,mode=bounds76,groups=%s,rows=%d,result=STOP" % ("+".join(groups), len(rows)))
        return 1
    refusals = sum(1 for l in text if l.startswith("REFUSAL_ROW,"))
    cells = sum(1 for l in text if l.startswith("CELL,"))
    not_run = sum(1 for l in text if l.startswith("NOT_RUN,"))
    setter = sum(1 for l in rows if ",setter=1," in l)
    contaminated = len(contaminated_lines(text))
    print("SUMMARY,mode=bounds76,groups=%s,rows=%d,setter_rows=%d,classification_rows=%d,refusal_rows=%d,cells=%d,not_run=%d,contaminated_rows=%d,result=ok" % (
        "+".join(groups), len(rows), setter, len(rows) - setter, refusals, cells, not_run, contaminated))
    return 0


if __name__ == "__main__":
    cmd = sys.argv[1]
    fn = {"row": row, "guard_probe": guard_probe, "brackets": brackets, "maxwall": maxwall, "finish": finish, "fixnote": fixnote,
          "sleeps": sleeps, "sleepstop": sleepstop}.get(cmd)
    if fn is None:
        sys.exit(64)
    sys.exit(fn(sys.argv[2:]) or 0)
PY
  chmod 0644 "$WORK/tools/b76.py"
}

b76_stop() {
  local reason=$1 code=${2:-1}
  if [ -n "${B76_OUT:-}" ]; then
    printf 'STOP,mode=bounds76,group=%s,%s\n' "${B76_GROUP:-none}" "$reason" >> "$B76_OUT"
    [ "${B76_GROUP:-none}" = none ] || printf 'GROUP_END,group=%s,rows=%s,duration_s=%s,result=STOP\n' "$B76_GROUP" \
      "$(grep -c "^BOUND76_ROW,.*,group=$B76_GROUP," "$B76_OUT" || true)" "$(( $(date +%s) - ${B76_T0:-$(date +%s)} ))" >> "$B76_OUT"
  fi
  log "STOP $reason"
  exit "$code"
}

b76_sample() {
  local free
  free=$(vm_free_gib)
  if [ -z "$free" ] || [ "$free" -lt "$MIN_FREE_GIB" ]; then
    b76_stop "reason=disk_below_threshold,free_gib=${free:-unknown},threshold_gib=$MIN_FREE_GIB" 3
  fi
  B76_FREE=$free
  B76_LOAD=$(load1_now)
}

b76_preflight() {
  local n
  n=$(docker ps --filter name=octoconv- -q | wc -l | tr -d ' ')
  [ "$n" = 0 ] || fail "preflight: $n octoconv- compose containers are running"
  if command -v kubectl >/dev/null 2>&1; then
    n=$(kubectl get pods -A 2>/dev/null | grep -c octoconv || true)
    [ "$n" = 0 ] || fail "preflight: $n octoconv pods are running in k8s"
  fi
  if pgrep -f 'go test' >/dev/null 2>&1; then fail "preflight: a go test process is running"; fi
  if pgrep -f 'docker build|buildx' >/dev/null 2>&1; then fail "preflight: a docker build is running"; fi
}

b76_setup() {
  need_tool docker
  need_tool go
  need_tool python3
  need_tool shasum
  need_tool git
  [ "$(docker version --format '{{.Server.Arch}}')" = arm64 ] || fail "docker server is not arm64"
  [ "$(GOTOOLCHAIN=$GO_TOOLCHAIN go env GOVERSION)" = "$GO_TOOLCHAIN" ] || fail "go toolchain is not $GO_TOOLCHAIN"
  b76_preflight
  disk_guard
  EVID_NAME=evidence/76-02-bounds.txt
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-b76.XXXXXX")
  chmod 0755 "$WORK"
  trap cleanup EXIT
  mkdir -p "$WORK/wrap" "$WORK/tools" "$WORK/gen" "$WORK/hostbin"
  chmod 0755 "$WORK/wrap" "$WORK/tools"
  chmod 0777 "$WORK/gen"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
  chmod 0644 "$WORK/wrap/measure_wrap.py"
  b76_tools
  ENGINE_TAG_PREFIX=engine76-
  TAG=$(build_engine) || b76_stop "reason=engine_build_failed"
  BUILT_TAGS+=("$TAG")
  cad_require_native "$TAG" || b76_stop "reason=engine_image_not_native"
  IMAGE_ID=$(docker image inspect "$TAG" --format '{{.Id}}')
  ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
  TRIPLET=$(triplet_for "$ARCH")
  MESH_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/$TRIPLET:/usr/local/bin/cad-mesh"
  RENDER_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/$TRIPLET:/usr/local/bin/cad-render"
  DXF_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/share/fonts:/usr/lib/$TRIPLET:/usr/local/bin/cad-dxf"
  RSVG_RO="/usr/share/fonts:/usr/lib/$TRIPLET:/usr/bin/rsvg-convert"
  build_go "$ARCH" "$WORK/bin"
  GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$WORK/hostbin/cad-sniff" ./scripts/cmd/cad-sniff
  B76_CACHE="$CAD_CACHE/scaled/b76"
  mkdir -p "$B76_CACHE"
  B76_GEN_SHA=$(sha_of scripts/cad-corpus-gen.py)
  B76_GUARD_SHA=$(sha_of cmd/cad-worker/helper/cad-guard)
  LARGE_STL=$(ls -S "$REPO_ROOT"/internal/convert/testdata/cad/public/*.stl "$CAD_CACHE"/fetched/*.stl 2>/dev/null | head -n 1)
  [ -s "$LARGE_STL" ] || fail "no LARGE_STL"
}

b76_hdr_lines() {
  local mode=$1
  b76_sample
  printf 'HDR,mode=%s,date_utc=%s\n' "$mode" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'HDR,mode=%s,commit=%s,tracked_dirt=%s\n' "$mode" "$(git rev-parse HEAD)" "$(tracked_dirt)"
  printf 'HDR,mode=%s,image=%s,image_id=%s,server_arch=%s\n' "$mode" "$TAG" "$IMAGE_ID" "$(docker version --format '{{.Server.Arch}}')"
  printf 'HDR,mode=%s,load1=%s,vm_free_gib=%s\n' "$mode" "$B76_LOAD" "$B76_FREE"
  printf 'HDR,mode=%s,cad_mesh_sha256=%s,cad_render_sha256=%s,cad_guard_sha256=%s,cad_dxf_sha256=%s,generator_sha256=%s,measure_wrap_sha256=%s\n' "$mode" \
    "$(sha_of cmd/cad-worker/helper/cad-mesh)" "$(sha_of cmd/cad-worker/helper/cad-render)" "$B76_GUARD_SHA" \
    "$(sha_of cmd/cad-worker/helper/cad-dxf)" "$B76_GEN_SHA" "$(sha_of "$WRAP_PY")"
  printf 'HDR,mode=%s,versions=%s\n' "$mode" "$(docker run --rm --network none --entrypoint sh "$TAG" -c \
    'printf "librsvg2-bin_%s+ezdxf_%s" "$(dpkg-query -W -f="\${Version}" librsvg2-bin)" "$(/opt/venv/bin/python -I -c "import importlib.metadata as m; print(m.version(\"ezdxf\"))")"' </dev/null)"
  printf 'HDR,mode=%s,limits=cpus2_memory4g_swap4g_network_none_user_nobody,go=%s,size=%s,setter_budget_s=%s,classification_budget_s=%s,n=%s\n' \
    "$mode" "$GO_TOOLCHAIN" "$SIZE" "$B76_SETTER_S" "$B76_CLASS_S" "$B76_N"
  printf 'HDR,mode=%s,ro_mesh=%s,ro_render=%s,ro_cad_dxf=%s,ro_rsvg=%s,rw=/job\n' "$mode" "$MESH_RO" "$RENDER_RO" "$DXF_RO" "$RSVG_RO"
}

b76_classes() {
  python3 "$WORK/tools/b76.py" brackets "$REPO_ROOT/$B76_STACK_REL" > "$WORK/brackets.txt" || true
  if grep -q '^STOP' "$WORK/brackets.txt"; then
    b76_stop "$(sed -n 's/^STOP //p' "$WORK/brackets.txt" | head -n 1)"
  fi
  B76_GLOBAL=$(awk '$1 == "GLOBAL" {print $2}' "$WORK/brackets.txt")
  B76_BYTE_BOUND=$(awk '$1 == "DECISION" && $3 == "byte_bound" {print $2}' "$WORK/brackets.txt" | tr '\n' ' ')
  B76_CLASS_CELLS=()
  B76_CLASS_TEXT=""
  local fmt shape lo hi
  while read -r _ fmt shape lo hi; do
    if [ "$lo" = none ]; then
      B76_CLASS_TEXT="${B76_CLASS_TEXT}${B76_CLASS_TEXT:+;}${fmt}:${shape}_within=absent_in_76-01(NOT_RUN)+${shape}_over=${hi}"
    else
      B76_CLASS_TEXT="${B76_CLASS_TEXT}${B76_CLASS_TEXT:+;}${fmt}:${shape}_within=${lo}+${shape}_over=${hi}"
      B76_CLASS_CELLS+=("${shape}_$lo")
    fi
    B76_CLASS_CELLS+=("${shape}_$hi")
  done < <(awk '$1 == "BRACKET"' "$WORK/brackets.txt")
}

b76_gen() {
  local outfile=$1
  shift
  docker run --rm --network none --cpus 2 --memory "${GEN_MEM:-4g}" --memory-swap "${GEN_MEM:-4g}" --user nobody \
    -e HOME=/tmp -e PYTHONHASHSEED=0 -v "$REPO_ROOT:/src:ro" -v "$WORK/gen:/out" \
    ${GEN_MOUNTS[@]+"${GEN_MOUNTS[@]}"} -w /src --entrypoint /opt/venv/bin/python "$TAG" -B "${GEN_SCRIPT:-scripts/cad-corpus-gen.py}" \
    "$@" --scaled-dir "${GEN_OUT:-/out}" > "$outfile" </dev/null 2>"$WORK/gen.err" || {
    tail -n 5 "$WORK/gen.err" >&2
    b76_stop "reason=generator_failed,args=$(printf '%s' "$*" | tr ' ,=/' '____')"
  }
}

b76_gen_file() {
  local outfile=$1 kind=$2 line
  line=$(grep "^SCALED,kind=$kind," "$outfile" | tail -n 1 || true)
  [ -n "$line" ] || b76_stop "reason=generator_gave_no_scaled_line,kind=$kind"
  printf '%s\n' "$line" | grep -q ',check=ok' || b76_stop "reason=generator_self_check_failed,kind=$kind"
  GEN_LINE=$line
  GEN_PATH="$WORK/gen/$(scaled_field file "$line")"
  [ -s "$GEN_PATH" ] || b76_stop "reason=generated_file_missing,kind=$kind"
}

b76_guard_probe() {
  local file=$1 fmt=$2 out dir
  dir=$(mktemp -d "$WORK/probe.XXXXXX")
  chmod 0777 "$dir"
  if [ "$fmt" = dxf ]; then
    docker run --rm "${LIMITS[@]}" "${B76_ENV3D[@]}" -v "$(dirname "$file"):/in:ro" --entrypoint /usr/bin/python3 "$TAG" -IB /usr/local/bin/cad-guard \
      --format dxf --max-expanded-inserts "$B76_DXF_INSERTS" --max-expanded-entities "$B76_DXF_ENTITIES" --max-nesting "$CAP_NESTING" \
      --max-abs-coordinate "$CAP_ABS" "/in/$(basename "$file")" > "$dir/out" 2>/dev/null </dev/null || true
  else
    docker run --rm "${LIMITS[@]}" "${B76_ENV3D[@]}" -v "$(dirname "$file"):/in:ro" --entrypoint /usr/bin/python3 "$TAG" -IB /usr/local/bin/cad-guard \
      --format "$fmt" --max-entities "$B76_ENTITIES" --max-nauo "$B76_NAUO" "/in/$(basename "$file")" > "$dir/out" 2>/dev/null </dev/null || true
  fi
  read -r GP_VERDICT GP_REASON GP_INSERTS GP_ENTITIES GP_INSTANCES GP_RAW <<< "$(python3 "$WORK/tools/b76.py" guard_probe "$dir/out")"
  rm -rf "$dir"
}

b76_count_probe() {
  local file=$1 dir
  dir=$(mktemp -d "$WORK/probe.XXXXXX")
  chmod 0777 "$dir"
  docker run --rm "${LIMITS[@]}" "${B76_ENV3D[@]}" -v "$(dirname "$file"):/in:ro" --entrypoint /usr/bin/python3 "$TAG" -IB /usr/local/bin/cad-guard \
    --format dxf --max-expanded-inserts "$COUNT_CAP" --max-expanded-entities "$COUNT_CAP" --max-nesting "$CAP_NESTING" \
    --max-abs-coordinate "$CAP_ABS" "/in/$(basename "$file")" > "$dir/out" 2>/dev/null </dev/null || true
  read -r CP_VERDICT _ CP_INSERTS CP_ENTITIES _ _ <<< "$(python3 "$WORK/tools/b76.py" guard_probe "$dir/out")"
  rm -rf "$dir"
}

b76_cache_key() { printf 'gen=%s;guard=%s;spec=%s' "$B76_GEN_SHA" "$B76_GUARD_SHA" "$1"; }

b76_cache_get() {
  local name=$1 key=$2 meta file
  meta="$B76_CACHE/$name.meta"
  [ -f "$meta" ] || return 1
  [ "$(sed -n 's/^KEY=//p' "$meta")" = "$key" ] || return 1
  file="$B76_CACHE/$(sed -n 's/^FILE=//p' "$meta")"
  [ -s "$file" ] || return 1
  [ "$(sha_of "$file")" = "$(sed -n 's/^SHA=//p' "$meta")" ] || return 1
  CELL_PATH=$file
  CELL_GEN=$(sed -n 's/^GEN=//p' "$meta")
  sed -n 's/^EXTRA=//p' "$meta" > "$WORK/extra.$name"
  CELL_CACHED=1
  return 0
}

b76_cache_put() {
  local name=$1 key=$2 src=$3 genline=$4 extras=$5 dest
  rm -f "$B76_CACHE/$name".*
  dest="$B76_CACHE/$name.$(printf '%s' "${src##*.}")"
  mv -f "$src" "$dest"
  {
    printf 'KEY=%s\n' "$key"
    printf 'FILE=%s\n' "$(basename "$dest")"
    printf 'SHA=%s\n' "$(sha_of "$dest")"
    printf 'GEN=%s\n' "$genline"
    [ ! -s "$extras" ] || sed 's/^/EXTRA=/' "$extras"
  } > "$B76_CACHE/$name.meta"
  CELL_PATH=$dest
  CELL_GEN=$genline
  if [ -s "$extras" ]; then cp "$extras" "$WORK/extra.$name"; else : > "$WORK/extra.$name"; fi
  CELL_CACHED=0
}

b76_make_m1() {
  local key k e2 e3 per fixed tries=0 ok_k="" extras="$WORK/picks.M1"
  key=$(b76_cache_key "M1:step-copies:$STACK_STEP_BASE:exact=$B76_STEP_MAX:largest_guard_admitted")
  b76_cache_get M1 "$key" && return 0
  : > "$extras"
  GEN_MOUNTS=()
  for k in 2 3; do
    b76_gen "$WORK/gen.out" --scaled-from "$STACK_STEP_BASE" --kind step-copies --copies "$k" --exact-bytes "$B76_STEP_MAX"
    b76_gen_file "$WORK/gen.out" step-copies
    b76_guard_probe "$GEN_PATH" step
    [ "$GP_VERDICT" = ok ] || b76_stop "reason=m1_calibration_refused,copies=$k"
    printf 'CELL_PICK,cell=M1,copies=%s,bytes=%s,guard=%s,entities=%s,role=calibration\n' "$k" "$(bytes_of "$GEN_PATH")" "$GP_VERDICT" "$GP_ENTITIES" >> "$extras"
    if [ "$k" = 2 ]; then e2=$GP_ENTITIES; else e3=$GP_ENTITIES; fi
    rm -f "$GEN_PATH"
  done
  per=$((e3 - e2))
  fixed=$((e2 - 2 * per))
  k=$(( (B76_ENTITIES - fixed) / per ))
  while [ "$tries" -lt 8 ]; do
    tries=$((tries + 1))
    b76_gen "$WORK/gen.out" --scaled-from "$STACK_STEP_BASE" --kind step-copies --copies "$k" --exact-bytes "$B76_STEP_MAX"
    b76_gen_file "$WORK/gen.out" step-copies
    b76_guard_probe "$GEN_PATH" step
    printf 'CELL_PICK,cell=M1,copies=%s,bytes=%s,guard=%s,reason=%s,entities=%s,role=search\n' "$k" "$(bytes_of "$GEN_PATH")" "$GP_VERDICT" "$GP_REASON" "$GP_ENTITIES" >> "$extras"
    if [ "$GP_VERDICT" = ok ]; then
      [ -z "$ok_k" ] || rm -f "$WORK/m1-keep.step"
      ok_k=$k
      mv -f "$GEN_PATH" "$WORK/m1-keep.step"
      M1_LINE=$GEN_LINE
      k=$((k + 1))
    elif [ "$GP_VERDICT" = too_complex ]; then
      rm -f "$GEN_PATH"
      if [ -n "$ok_k" ] && [ "$k" = $((ok_k + 1)) ]; then
        b76_cache_put M1 "$key" "$WORK/m1-keep.step" "$M1_LINE" "$extras"
        return 0
      fi
      k=$((k - 1))
    else
      b76_stop "reason=m1_search_guard_verdict_$GP_VERDICT,copies=$k"
    fi
  done
  b76_stop "reason=m1_search_did_not_settle"
}

b76_iges_try() {
  local k=$1 step
  GEN_MOUNTS=()
  b76_gen "$WORK/gen.out" --scaled-from "$STACK_STEP_BASE" --kind step-copies --copies "$k"
  b76_gen_file "$WORK/gen.out" step-copies
  step=$GEN_PATH
  GEN_MEM=$B76_IGES_GEN_MEM b76_gen "$WORK/gen.out" --scaled-from "/out/$(basename "$step")" --kind iges-from-step --exact-bytes "$B76_IGES_MAX"
  IT_STEP_BYTES=$(bytes_of "$step")
  rm -f "$step"
  if grep -q '^SCALED_SKIP,kind=iges-from-step,.*reason=over_target_bytes' "$WORK/gen.out"; then
    IT_FIT=0
    IT_BYTES=$(scaled_field bytes "$(grep '^SCALED_SKIP,kind=iges-from-step,' "$WORK/gen.out" | tail -n 1)")
    GEN_PATH=""
  else
    b76_gen_file "$WORK/gen.out" iges-from-step
    IT_FIT=1
    IT_BYTES=$(scaled_field unpadded_bytes "$GEN_LINE")
  fi
}

b76_make_m3() {
  local key k b4 b8 per fixed tries=0 ok_k="" extras="$WORK/picks.M3"
  key=$(b76_cache_key "M3:iges-from-step:step-copies:$STACK_STEP_BASE:exact=$B76_IGES_MAX:largest")
  b76_cache_get M3 "$key" && return 0
  : > "$extras"
  for k in 4 8; do
    b76_iges_try "$k"
    [ "$IT_FIT" = 1 ] || b76_stop "reason=m3_calibration_over,copies=$k"
    printf 'CELL_PICK,cell=M3,copies=%s,step_bytes=%s,iges_unpadded_bytes=%s,fits=1,role=calibration\n' "$k" "$IT_STEP_BYTES" "$IT_BYTES" >> "$extras"
    if [ "$k" = 4 ]; then b4=$IT_BYTES; else b8=$IT_BYTES; fi
    rm -f "$GEN_PATH"
  done
  per=$(( (b8 - b4) / 4 ))
  fixed=$(( b4 - 4 * per ))
  k=$(( (B76_IGES_MAX - fixed) / per ))
  while [ "$tries" -lt 8 ]; do
    tries=$((tries + 1))
    b76_iges_try "$k"
    printf 'CELL_PICK,cell=M3,copies=%s,step_bytes=%s,iges_unpadded_bytes=%s,fits=%s,role=search\n' "$k" "$IT_STEP_BYTES" "$IT_BYTES" "$IT_FIT" >> "$extras"
    log "M3 pick copies=$k iges=$IT_BYTES fits=$IT_FIT"
    if [ "$IT_FIT" = 1 ]; then
      [ -z "$ok_k" ] || rm -f "$WORK/m3-keep.igs"
      ok_k=$k
      mv -f "$GEN_PATH" "$WORK/m3-keep.igs"
      M3_LINE=$GEN_LINE
      k=$((k + 1))
    else
      if [ -n "$ok_k" ] && [ "$k" = $((ok_k + 1)) ]; then
        b76_cache_put M3 "$key" "$WORK/m3-keep.igs" "$M3_LINE" "$extras"
        return 0
      fi
      k=$((k - 1))
    fi
  done
  b76_stop "reason=m3_search_did_not_settle"
}

b76_make_simple() {
  local name=$1 kind=$2 spec=$3 key
  shift 3
  key=$(b76_cache_key "$name:$spec")
  b76_cache_get "$name" "$key" && return 0
  : > "$WORK/picks.$name"
  b76_gen "$WORK/gen.out" "$@"
  b76_gen_file "$WORK/gen.out" "$kind"
  b76_cache_put "$name" "$key" "$GEN_PATH" "$GEN_LINE" "$WORK/picks.$name"
}

b76_make_c() {
  local key rep ci ce cells extras="$WORK/picks.C"
  key=$(b76_cache_key "C:dxf-combo:dxf-replicate:$BASE_DXF:118:caps=$B76_DXF_INSERTS+$B76_DXF_ENTITIES")
  b76_cache_get C "$key" && return 0
  : > "$extras"
  GEN_MOUNTS=()
  b76_gen "$WORK/gen.out" --scaled-from "$BASE_DXF" --kind dxf-replicate --targets 118
  b76_gen_file "$WORK/gen.out" dxf-replicate
  rep=$GEN_PATH
  b76_count_probe "$rep"
  [ "$CP_VERDICT" = ok ] || b76_stop "reason=c_replicate_count_failed,verdict=$CP_VERDICT"
  cells=$(( B76_DXF_INSERTS - CP_INSERTS ))
  [ $(( (B76_DXF_ENTITIES - CP_ENTITIES - 1) / 5 )) -ge "$cells" ] || cells=$(( (B76_DXF_ENTITIES - CP_ENTITIES - 1) / 5 ))
  [ "$cells" -ge 1 ] || b76_stop "reason=c_no_remaining_expansion"
  printf 'CELL_PICK,cell=C,replicate=%s,replicate_bytes=%s,replicate_inserts=%s,replicate_entities=%s,cells=%s,rule=min(170031-I;floor((850155-E-1)/5))\n' \
    "$(basename "$rep")" "$(bytes_of "$rep")" "$CP_INSERTS" "$CP_ENTITIES" "$cells" >> "$extras"
  b76_gen "$WORK/gen.out" --scaled-from "/out/$(basename "$rep")" --kind dxf-combo --cells "$cells"
  b76_gen_file "$WORK/gen.out" dxf-combo
  rm -f "$rep"
  b76_cache_put C "$key" "$GEN_PATH" "$GEN_LINE" "$extras"
}

b76_make_cell() {
  local name=$1 large_base
  CELL_CACHED=0
  GEN_MOUNTS=()
  GEN_MEM=4g
  case "$name" in
    M1) CELL_FMT=step; b76_make_m1 ;;
    M2) CELL_FMT=step; b76_make_simple M2 step-nauo "step-nauo:$STACK_STEP_BASE:1518:grid" --scaled-from "$STACK_STEP_BASE" --kind step-nauo --targets 1518 ;;
    M3) CELL_FMT=iges; b76_make_m3 ;;
    M4)
      CELL_FMT=stl
      large_base=$(basename "$LARGE_STL")
      GEN_MOUNTS=(-v "$LARGE_STL:/base/$large_base:ro")
      b76_make_simple M4 stl-tile "stl-tile:$large_base:exact=$B76_STL_MAX" --scaled-from "/base/$large_base" --kind stl-tile --exact-bytes "$B76_STL_MAX" ;;
    C) CELL_FMT=dxf; b76_make_c ;;
    L170030) CELL_FMT=dxf; b76_make_simple L170030 dxf-minsert "dxf-minsert:$BASE_DXF:170030:5" --scaled-from "$BASE_DXF" --kind dxf-minsert --targets 170030 ;;
    L170031) CELL_FMT=dxf; b76_make_simple L170031 dxf-minsert "dxf-minsert:$BASE_DXF:170031:5" --scaled-from "$BASE_DXF" --kind dxf-minsert --targets 170031 ;;
    I170031x4) CELL_FMT=dxf; b76_make_simple I170031x4 dxf-minsert "dxf-minsert:$BASE_DXF:170031:4" --scaled-from "$BASE_DXF" --kind dxf-minsert --targets 170031 --block-entities 4 ;;
    R123731968) CELL_FMT=dxf; b76_make_simple R123731968 dxf-replicate "dxf-replicate:$BASE_DXF:exact=$B76_DXF_MAX" --scaled-from "$BASE_DXF" --kind dxf-replicate --exact-bytes "$B76_DXF_MAX" ;;
    stress_*) CELL_FMT=dxf; b76_make_simple "$name" dxf-guard-stress "dxf-guard-stress:${name#stress_}:118" --kind dxf-guard-stress --shape "${name#stress_}" --targets 118 ;;
    S1_k*) CELL_FMT=stl; b76_make_simple "$name" stl-stack "stl-stack:cube:${name#S1_k}" --kind stl-stack --part cube --targets "${name#S1_k}" ;;
    S6_n*) CELL_FMT=step; b76_make_simple "$name" step-nauo "step-nauo:$STACK_STEP_BASE:${name#S6_n}:pitch0" --scaled-from "$STACK_STEP_BASE" --kind step-nauo --targets "${name#S6_n}" --nauo-pitch 0 ;;
    S5_*) CELL_FMT=iges; b76_make_s5 "$name" ;;
    *) b76_stop "reason=unknown_cell_$name" ;;
  esac
}

b76_make_s5() {
  local name=$1 key mib step extras="$WORK/picks.$1"
  mib=$(sed -n 's/^GEN,shape=S5,rung=32MiB,step_target=\([0-9]*\)MiB,.*/\1/p' "$REPO_ROOT/$B76_STACK_REL" | head -n 1)
  [ -n "$mib" ] || b76_stop "reason=no_76-01_s5_step_target"
  key=$(b76_cache_key "$name:iges-from-step:step-copies:$STACK_STEP_BASE:${mib}MiB")
  b76_cache_get "$name" "$key" && return 0
  printf 'CELL_PICK,cell=%s,step_target=%sMiB,source=76-01_GEN_shape=S5_rung=32MiB\n' "$name" "$mib" > "$extras"
  b76_gen "$WORK/gen.out" --scaled-from "$STACK_STEP_BASE" --kind step-copies --targets "$mib"
  b76_gen_file "$WORK/gen.out" step-copies
  step=$GEN_PATH
  GEN_MEM=$B76_IGES_GEN_MEM b76_gen "$WORK/gen.out" --scaled-from "/out/$(basename "$step")" --kind iges-from-step --targets 1
  b76_gen_file "$WORK/gen.out" iges-from-step
  rm -f "$step"
  b76_cache_put "$name" "$key" "$GEN_PATH" "$GEN_LINE" "$extras"
}

b76_role_of() {
  case "$1" in
    L170031) echo refusal ;;
    stress_*) echo stress ;;
    S1_*|S5_*|S6_*) echo classification ;;
    *) echo setter ;;
  esac
}

b76_stacked_of() {
  case "$1" in
    M1|M3|S1_*|S5_*|S6_*) echo 1 ;;
    *) echo 0 ;;
  esac
}

b76_expect_of() {
  case "$1" in
    L170031) echo too_complex/expanded_entities_over_cap ;;
    stress_chain) echo too_complex/nesting_too_deep ;;
    stress_fanout|stress_wide) echo too_complex/expanded_inserts_over_cap ;;
    *) echo ok ;;
  esac
}

b76_match_7601() {
  local name=$1 shape rung sha
  case "$name" in
    S1_*|S5_*|S6_*) shape=${name%%_*}; rung=${name#*_} ;;
    *) echo na; return 0 ;;
  esac
  sha=$(grep "^STACK_ROW,shape=$shape,.*,rung=$rung," "$REPO_ROOT/$B76_STACK_REL" | head -n 1 | tr ',' '\n' | sed -n 's/^sha256=//p')
  if [ -z "$sha" ]; then echo absent; elif [ "$sha" = "$(sha_of "$CELL_PATH")" ]; then echo 1; else echo 0; fi
}

b76_cell() {
  local name=$1 out=$2 sniff_line sniff rej got expect target gap role stacked
  b76_make_cell "$name"
  CELL_SHA=$(sha_of "$CELL_PATH")
  CELL_BYTES=$(bytes_of "$CELL_PATH")
  sniff_line=$("$WORK/hostbin/cad-sniff" -group b76 -root "$(dirname "$CELL_PATH")" "$CELL_PATH")
  sniff=$(printf '%s' "$sniff_line" | awk -F'\t' '{print $5}')
  rej=$(printf '%s' "$sniff_line" | awk -F'\t' '{print $6}')
  [ "$rej" = - ] || sniff="$sniff:$rej"
  if [ "$CELL_FMT" = stl ]; then
    GP_VERDICT=na; GP_REASON=none; GP_INSERTS=na; GP_ENTITIES=na; GP_INSTANCES=na; GP_RAW=na
  else
    b76_guard_probe "$CELL_PATH" "$CELL_FMT"
  fi
  got=$GP_VERDICT
  [ "$got" = ok ] || [ "$got" = na ] || got="$GP_VERDICT/$GP_REASON"
  target=$(scaled_field target_bytes "$CELL_GEN"); target=${target:-na}
  gap=$(scaled_field pad_gap_bytes "$CELL_GEN"); gap=${gap:-na}
  role=$(b76_role_of "$name")
  stacked=$(b76_stacked_of "$name")
  if [ -s "$WORK/extra.$name" ]; then sed "s/\$/,cached=$CELL_CACHED/" "$WORK/extra.$name" >> "$out"; fi
  local earlier
  earlier=$( { grep "^CELL,cell=$name," "$out" 2>/dev/null || true; } | tr ',' '\n' | sed -n 's/^sha256=//p' | sort -u | tr '\n' '+' | sed 's/+$//')
  if [ -n "$earlier" ] && [ "$earlier" != "$CELL_SHA" ]; then
    b76_stop "reason=cell_sha_differs_from_its_earlier_cell_line,cell=$name,sha256=$CELL_SHA,earlier=$earlier"
  fi
  printf 'CELL,cell=%s,format=%s,file=%s,bytes=%s,target_bytes=%s,pad_gap_bytes=%s,sha256=%s,sniff=%s,inserts=%s,entities=%s,instances=%s,guard=%s,reason=%s,raw_entities=%s,role=%s,stacked=%s,cached=%s,match_76_01=%s,path=%s,gen=%s\n' \
    "$name" "$CELL_FMT" "$(basename "$CELL_PATH")" "$CELL_BYTES" "$target" "$gap" "$CELL_SHA" "$sniff" "$GP_INSERTS" "$GP_ENTITIES" \
    "$GP_INSTANCES" "$GP_VERDICT" "$GP_REASON" "$GP_RAW" "$role" "$stacked" "$CELL_CACHED" "$(b76_match_7601 "$name")" "$CELL_PATH" \
    "$(printf '%s' "$CELL_GEN" | tr ',=' ';:')" >> "$out"
  log "cell $name $CELL_BYTES bytes sniff=$sniff guard=$got"
  [ "$sniff" = "$CELL_FMT" ] || b76_stop "reason=cell_sniff_mismatch,cell=$name,sniff=$sniff"
  expect=$(b76_expect_of "$name")
  if [ "$CELL_FMT" != stl ] && [ "$got" != "$expect" ]; then
    b76_stop "reason=cell_guard_mismatch,cell=$name,guard=$(printf '%s' "$got" | tr '/' '_'),expect=$(printf '%s' "$expect" | tr '/' '_')"
  fi
  case "$name" in
    M1) [ "$CELL_BYTES" = "$B76_STEP_MAX" ] && [ "$gap" = 0 ] || b76_stop "reason=m1_not_at_the_ceiling,bytes=$CELL_BYTES" ;;
    M2) [ "$GP_INSTANCES" = "$B76_NAUO" ] || b76_stop "reason=m2_instances_$GP_INSTANCES" ;;
    M3) [ "$gap" -ge 0 ] && [ "$gap" -lt 82 ] || b76_stop "reason=m3_gap_$gap" ;;
    M4) [ "$CELL_BYTES" = $(( 84 + 50 * ((B76_STL_MAX - 84) / 50) )) ] || b76_stop "reason=m4_bytes_$CELL_BYTES" ;;
    C) [ "$GP_ENTITIES" = "$B76_C_ENTITIES" ] && [ "$CELL_BYTES" -le "$B76_DXF_MAX" ] || b76_stop "reason=c_entities_$GP_ENTITIES,bytes=$CELL_BYTES" ;;
    L170030) [ "$GP_ENTITIES" = "$B76_L170030_ENTITIES" ] || b76_stop "reason=l170030_entities_$GP_ENTITIES" ;;
    I170031x4) [ "$GP_INSERTS" = "$B76_DXF_INSERTS" ] || b76_stop "reason=i170031x4_inserts_$GP_INSERTS" ;;
    R123731968) [ "$CELL_BYTES" = "$B76_DXF_MAX" ] && [ "$gap" = 0 ] || b76_stop "reason=r_not_at_the_ceiling,bytes=$CELL_BYTES" ;;
  esac
  return 0
}

b76_gen_identity() {
  local out=$1 kind=$2 base=$3 ref eq sha_main sha_new f_main f_new
  shift 3
  ref=main
  git rev-parse --verify -q "$ref^{commit}" >/dev/null || ref=origin/main
  mkdir -p "$WORK/maingen" "$WORK/gen/main" "$WORK/gen/new"
  chmod 0755 "$WORK/maingen"
  chmod 0777 "$WORK/gen/main" "$WORK/gen/new"
  git show "$ref:scripts/cad-corpus-gen.py" > "$WORK/maingen/cad-corpus-gen.py"
  chmod 0644 "$WORK/maingen/cad-corpus-gen.py"
  GEN_MOUNTS=(-v "$WORK/maingen:/maingen:ro")
  GEN_SCRIPT=/maingen/cad-corpus-gen.py GEN_OUT=/out/main b76_gen "$WORK/gid.main" --scaled-from "$base" --kind "$kind" "$@"
  GEN_MOUNTS=()
  GEN_OUT=/out/new b76_gen "$WORK/gid.new" --scaled-from "$base" --kind "$kind" "$@"
  sha_main=$(scaled_field sha "$(grep "^SCALED,kind=$kind," "$WORK/gid.main" | tail -n 1)")
  sha_new=$(scaled_field sha "$(grep "^SCALED,kind=$kind," "$WORK/gid.new" | tail -n 1)")
  f_main=$(find "$WORK/gen/main" -type f | head -n 1)
  f_new=$(find "$WORK/gen/new" -type f | head -n 1)
  eq=0
  if [ -n "$sha_main" ] && [ "$sha_main" = "$sha_new" ] && [ -s "$f_main" ] && [ "$(sha_of "$f_main")" = "$(sha_of "$f_new")" ]; then eq=1; fi
  printf 'GEN_IDENTITY,kind=%s,base=%s,args=%s,main_ref=%s,main_commit=%s,main_sha256=%s,new_sha256=%s,main_file_sha256=%s,new_file_sha256=%s,equal=%s\n' \
    "$kind" "$(basename "$base")" "$(printf '%s' "$*" | tr ' ' '_')" "$ref" "$(git rev-parse "$ref")" "${sha_main:-none}" "${sha_new:-none}" \
    "$( [ -s "$f_main" ] && sha_of "$f_main" || echo none)" "$( [ -s "$f_new" ] && sha_of "$f_new" || echo none)" "$eq" >> "$out"
  rm -rf "$WORK/gen/main" "$WORK/gen/new"
  [ "$eq" = 1 ] || b76_stop "reason=generator_identity_broken,kind=$kind"
}

b76_stage() {
  local job=$1 tag=$2 confine=$3 envkind=$4 budget=$5 dir rc_w=0
  shift 5
  dir="$job/w_$tag"
  mkdir -p "$dir"
  chmod 0777 "$dir"
  local envs=("${B76_ENV3D[@]}")
  [ "$envkind" = 3d ] || envs=("${STAGE_ENV[@]}")
  if [ "$confine" = none ]; then
    docker run --rm "${LIMITS[@]}" "${envs[@]}" -v "$WORK/bin:/gobin:ro" -v "$WORK/wrap:/wrap:ro" -v "$job:/job" \
      --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py --budget-s "$budget" --outdir "/job/w_$tag" -- \
      "$@" > "$dir/wrap.out" 2>/dev/null </dev/null || rc_w=$?
  else
    docker run --rm "${LIMITS[@]}" "${envs[@]}" -e "PROBE_RO=$confine" -e "PROBE_RW=/job" -v "$WORK/bin:/gobin:ro" \
      -v "$WORK/wrap:/wrap:ro" -v "$job:/job" --entrypoint /usr/bin/python3 "$TAG" -I -B /wrap/measure_wrap.py \
      --budget-s "$budget" --outdir "/job/w_$tag" -- /gobin/sandbox-probe "$@" > "$dir/wrap.out" 2>/dev/null </dev/null || rc_w=$?
  fi
  printf '%s\n' "$rc_w" > "$dir/rc_w"
}

b76_emit() {
  local job=$1 tag=$2 line
  shift 2
  line=$(python3 "$WORK/tools/b76.py" row "group=$B76_GROUP" "wrap=$job/w_$tag/wrap.out" "log=$job/w_$tag/child.log" \
    "rc_w=$(cat "$job/w_$tag/rc_w")" "load1=$B76_LOAD" "vmfree=$B76_FREE" "$@")
  printf '%s\n' "$line" >> "$B76_OUT"
  log "$(printf '%s' "$line" | tr ',' '\n' | grep -E '^(cell|stage|quality|view|background|rep|wall_s|triangles|band|class)=' | tr '\n' ' ')"
  ROW_BAND=$(scaled_field band "$line")
  ROW_CLASS=$(scaled_field class "$line")
  ROW_TRI=$(scaled_field triangles "$line")
  ROW_WALL=$(scaled_field wall_s "$line")
  ROW_TIMEOUT=$(scaled_field timeout "$line")
  if [ "$ROW_BAND" = failed ]; then
    tail -n 8 "$job/w_$tag/child.log" >&2 2>/dev/null || true
    b76_stop "reason=row_failed,$(printf '%s' "$line" | tr ',' '\n' | grep -E '^(cell|stage|quality|view|background|rep|rc|class|why)=' | tr '\n' ',' | sed 's/,$//')"
  fi
}

b76_common() {
  printf 'cell=%s format=%s bytes=%s sha=%s stacked=%s setter=%s budget=%s' "$1" "$CELL_FMT" "$CELL_BYTES" "$CELL_SHA" "$(b76_stacked_of "$1")" "$2" "$3"
}

b76_guard_row() {
  local name=$1 rep=$2 setter=$3 budget=$4 job
  local -a cm
  read -ra cm <<< "$(b76_common "$name" "$setter" "$budget")"
  b76_sample
  job=$(new_job)
  cp -c "$CELL_PATH" "$job/in.$CELL_FMT" 2>/dev/null || cp "$CELL_PATH" "$job/in.$CELL_FMT"
  chmod 0644 "$job/in.$CELL_FMT"
  if [ "$CELL_FMT" = dxf ]; then
    b76_stage "$job" guard none 3d "$budget" /usr/bin/python3 -IB /usr/local/bin/cad-guard --format dxf --max-expanded-inserts "$B76_DXF_INSERTS" \
      --max-expanded-entities "$B76_DXF_ENTITIES" --max-nesting "$CAP_NESTING" --max-abs-coordinate "$CAP_ABS" /job/in.dxf
  else
    b76_stage "$job" guard none 3d "$budget" /usr/bin/python3 -IB /usr/local/bin/cad-guard --format "$CELL_FMT" --max-entities "$B76_ENTITIES" \
      --max-nauo "$B76_NAUO" "/job/in.$CELL_FMT"
  fi
  b76_emit "$job" guard "${cm[@]}" stage=guard rep="$rep" "expect=$(b76_expect_of "$name")"
  GUARD_LAST=$(tail -n 1 "$B76_OUT")
  rm -rf "$job"
}

b76_mesh_row() {
  local name=$1 q=$2 rep=$3 setter=$4 budget=$5 keepdir=$6 job
  local -a cm
  read -ra cm <<< "$(b76_common "$name" "$setter" "$budget")"
  b76_sample
  job=$(new_job)
  cp -c "$CELL_PATH" "$job/in.$CELL_FMT" 2>/dev/null || cp "$CELL_PATH" "$job/in.$CELL_FMT"
  chmod 0644 "$job/in.$CELL_FMT"
  if [ "$CELL_FMT" = stl ]; then
    b76_stage "$job" stats "$MESH_RO" 3d "$budget" /usr/local/bin/cad-mesh stats --measure /job/draft.json /job/in.stl /job/mesh.stl
      b76_emit "$job" stats "${cm[@]}" stage=stats quality=na rep="$rep" "mesh=$job/mesh.stl" "out=$job/mesh.stl"
  else
    b76_stage "$job" mesh "$MESH_RO" 3d "$budget" /usr/local/bin/cad-mesh --format "$CELL_FMT" "/job/in.$CELL_FMT" /job/mesh.stl \
      --quality "$q" --measure /job/draft.json
      b76_emit "$job" mesh "${cm[@]}" stage=mesh quality="$q" rep="$rep" "mesh=$job/mesh.stl" "out=$job/mesh.stl"
  fi
  if [ "$rep" = 1 ] && [ "$ROW_CLASS" = ok ] && [ "$ROW_TRI" -le "$B76_TRI_CAP" ]; then
    mv -f "$job/mesh.stl" "$keepdir/mesh_$q.stl"
    printf '%s\n' "$ROW_TRI" > "$keepdir/tri_$q"
  fi
  rm -rf "$job"
}

b76_render_row() {
  local name=$1 q=$2 view=$3 bg=$4 rep=$5 setter=$6 budget=$7 keepdir=$8 tri job
  local -a cm
  read -ra cm <<< "$(b76_common "$name" "$setter" "$budget")"
  tri=$(cat "$keepdir/tri_$q")
  b76_sample
  job=$(new_job)
  cp -c "$keepdir/mesh_$q.stl" "$job/mesh.stl" 2>/dev/null || cp "$keepdir/mesh_$q.stl" "$job/mesh.stl"
  chmod 0644 "$job/mesh.stl"
  b76_stage "$job" render "$RENDER_RO" 3d "$budget" /usr/local/bin/cad-render --view "$view" --size "$SIZE" --background "$bg" /job/mesh.stl /job/out.png
  b76_emit "$job" render "${cm[@]}" stage=render quality="$q" view="$view" background="$bg" size="$SIZE" \
    triangles="$tri" rep="$rep" "out=$job/out.png"
  RENDER_CLASS=$ROW_CLASS
  RENDER_WALL=$ROW_WALL
  RENDER_TIMEOUT=$ROW_TIMEOUT
  if [ "$ROW_CLASS" = ok ]; then
    b76_sample
    b76_stage "$job" pixel none 2d "$budget" /gobin/cad-pixel-judge --size "$SIZE" --background "$bg" /job/out.png
      b76_emit "$job" pixel "${cm[@]}" stage=pixel quality="$q" view="$view" background="$bg" size="$SIZE" \
      triangles="$tri" rep="$rep"
  fi
  rm -rf "$job"
}

b76_wall_or_budget() {
  if [ "$2" = 1 ] || [ "$1" = na ]; then printf '%s\n' "$3"; else printf '%s\n' "$1"; fi
}

b76_gt() { python3 -c "import sys; sys.exit(0 if float(sys.argv[1]) > float(sys.argv[2]) else 1)" "$1" "$2"; }

b76_maxwall() { python3 "$WORK/tools/b76.py" maxwall "$B76_OUT" "$@"; }

b76_mesh_cell() {
  local name=$1 keepdir=$2 rep q fine_max w1
  if [ "$CELL_FMT" = stl ]; then
    for rep in $(seq 1 "$B76_N"); do b76_mesh_row "$name" na "$rep" 1 "$B76_SETTER_S" "$keepdir"; done
    return 0
  fi
  for rep in $(seq 1 "$B76_N"); do b76_mesh_row "$name" fine "$rep" 1 "$B76_SETTER_S" "$keepdir"; done
  fine_max=$(b76_maxwall "$name" mesh fine na na)
  for q in standard draft; do
    b76_mesh_row "$name" "$q" 1 1 "$B76_SETTER_S" "$keepdir"
    w1=$(b76_wall_or_budget "$ROW_WALL" "$ROW_TIMEOUT" "$B76_SETTER_S")
    if [ "$fine_max" = na ] || b76_gt "$w1" "$fine_max"; then
      printf 'PROMOTE,cell=%s,stage=mesh,quality=%s,n1_wall_s=%s,fine_max_s=%s\n' "$name" "$q" "$w1" "$fine_max" >> "$B76_OUT"
      for rep in $(seq 2 "$B76_N"); do b76_mesh_row "$name" "$q" "$rep" 1 "$B76_SETTER_S" "$keepdir"; done
    fi
  done
}

b76_pick_quality() {
  local keepdir=$1 q best="" best_tri=-1 t
  for q in fine standard draft na; do
    [ -f "$keepdir/tri_$q" ] || continue
    t=$(cat "$keepdir/tri_$q")
    if [ "$t" -gt "$best_tri" ]; then best=$q; best_tri=$t; fi
  done
  PICK_Q=$best
}

b76_render_cell() {
  local name=$1 keepdir=$2 i view bg slow=-1 slow_w=-1 w rep pmax changed
  local -a n1 n1_to promoted
  b76_pick_quality "$keepdir"
  if [ -z "$PICK_Q" ]; then
    printf 'NOT_RUN,cell=%s,stage=render,reason=refused_by_cap_no_admitted_quality,band=refused_by_cap,triangle_cap=%s\n' "$name" "$B76_TRI_CAP" >> "$B76_OUT"
    return 0
  fi
  for i in "${!B76_VARIANTS[@]}"; do
    view=${B76_VARIANTS[$i]%%:*}; bg=${B76_VARIANTS[$i]#*:}
    b76_render_row "$name" "$PICK_Q" "$view" "$bg" 1 1 "$B76_SETTER_S" "$keepdir"
    w=$(b76_wall_or_budget "$RENDER_WALL" "$RENDER_TIMEOUT" "$B76_SETTER_S")
    n1[$i]=$w
    if [ "$RENDER_CLASS" = timeout ]; then n1_to[$i]=1; else n1_to[$i]=0; fi
    promoted[$i]=0
    if b76_gt "$w" "$slow_w"; then slow=$i; slow_w=$w; fi
  done
  b76_promote_render "$name" "$keepdir" "$slow" "${n1[$slow]}" "${n1_to[$slow]}"
  promoted[$slow]=1
  pmax=$B76_PMAX
  changed=1
  while [ "$changed" = 1 ]; do
    changed=0
    for i in "${!B76_VARIANTS[@]}"; do
      [ "${promoted[$i]}" = 0 ] || continue
      if b76_gt "${n1[$i]}" "$pmax"; then
        b76_promote_render "$name" "$keepdir" "$i" "${n1[$i]}" "${n1_to[$i]}"
        promoted[$i]=1
        pmax=$B76_PMAX
        changed=1
      fi
    done
  done
}

b76_promote_render() {
  local name=$1 keepdir=$2 i=$3 n1w=$4 to=$5 view bg rep
  view=${B76_VARIANTS[$i]%%:*}; bg=${B76_VARIANTS[$i]#*:}
  printf 'PROMOTE,cell=%s,stage=render,quality=%s,view=%s,background=%s,n1_wall_s=%s\n' "$name" "$PICK_Q" "$view" "$bg" "$n1w" >> "$B76_OUT"
  if [ "$(b76_stacked_of "$name")" = 1 ] && [ "$to" = 1 ]; then
    for rep in $(seq 2 "$B76_N"); do
      printf 'NOT_RUN,cell=%s,stage=render,quality=%s,view=%s,background=%s,rep=%s,reason=refused_at_wrap_budget\n' "$name" "$PICK_Q" "$view" "$bg" "$rep" >> "$B76_OUT"
    done
  else
    for rep in $(seq 2 "$B76_N"); do b76_render_row "$name" "$PICK_Q" "$view" "$bg" "$rep" 1 "$B76_SETTER_S" "$keepdir"; done
  fi
  B76_PMAX=$(b76_maxwall "$name" render "$PICK_Q" "$view" "$bg")
  if [ "$B76_PMAX" = na ] || [ "$to" = 1 ]; then B76_PMAX=$(python3 -c "import sys; print(max(float(x) for x in sys.argv[1:] if x != 'na'))" "$n1w" "$B76_PMAX"); fi
}

b76_setter_3d() {
  local name=$1 keepdir rep
  b76_cell "$name" "$B76_OUT"
  keepdir=$(mktemp -d "$WORK/keep.XXXXXX")
  if [ "$CELL_FMT" != stl ]; then
    for rep in $(seq 1 "$B76_N"); do b76_guard_row "$name" "$rep" 1 "$B76_SETTER_S"; done
  fi
  b76_mesh_cell "$name" "$keepdir"
  b76_render_cell "$name" "$keepdir"
  rm -rf "$keepdir"
}

b76_class_3d() {
  local name=$1 keepdir q
  b76_cell "$name" "$B76_OUT"
  keepdir=$(mktemp -d "$WORK/keep.XXXXXX")
  if [ "$CELL_FMT" = stl ]; then
    b76_mesh_row "$name" na 1 0 "$B76_CLASS_S" "$keepdir"
  else
    b76_guard_row "$name" 1 0 "$B76_CLASS_S"
    for q in "${B76_QUALITIES[@]}"; do
      b76_mesh_row "$name" "$q" 1 0 "$B76_CLASS_S" "$keepdir"
      [ -f "$keepdir/tri_$q" ] && break
      [ "$ROW_CLASS" = ok ] || break
    done
  fi
  b76_pick_quality "$keepdir"
  if [ -z "$PICK_Q" ]; then
    printf 'NOT_RUN,cell=%s,stage=render,reason=no_admitted_mesh,band=%s\n' "$name" "$ROW_BAND" >> "$B76_OUT"
  else
    b76_render_row "$name" "$PICK_Q" iso opaque 1 0 "$B76_CLASS_S" "$keepdir"
  fi
  rm -rf "$keepdir"
}

b76_dxf_row() {
  local name=$1 rep=$2 job lb sw sh left top setter=1 budget=$B76_SETTER_S
  local -a cm
  read -ra cm <<< "$(b76_common "$name" "$setter" "$budget")"
  b76_sample
  job=$(new_job)
  cp -c "$CELL_PATH" "$job/in.dxf" 2>/dev/null || cp "$CELL_PATH" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  b76_stage "$job" guard none 3d "$budget" /usr/bin/python3 -IB /usr/local/bin/cad-guard --format dxf --max-expanded-inserts "$B76_DXF_INSERTS" \
    --max-expanded-entities "$B76_DXF_ENTITIES" --max-nesting "$CAP_NESTING" --max-abs-coordinate "$CAP_ABS" /job/in.dxf
  b76_emit "$job" guard "${cm[@]}" stage=guard rep="$rep" expect=ok
  b76_sample
  b76_stage "$job" dxf "$DXF_RO" 2d "$budget" /usr/local/bin/cad-dxf --max-drawn-entities "$B76_DXF_ENTITIES" --texts /job/draw.texts.json \
    --render-svg /job/draw.render.svg --measure /job/measure.json /job/in.dxf /job/draw.svg
  b76_emit "$job" dxf "${cm[@]}" stage=dxf rep="$rep" "out=$job/draw.svg" \
    "extra=,svg_bytes=$(bytes_of "$job/draw.svg" 2>/dev/null || echo na),render_svg_bytes=$(bytes_of "$job/draw.render.svg" 2>/dev/null || echo na)"
  b76_sample
  b76_stage "$job" svg_client none 2d "$budget" /gobin/cad-svg-check --out /job/clean.svg /job/draw.svg
  b76_emit "$job" svg_client "${cm[@]}" stage=svg_client rep="$rep" "out=$job/clean.svg"
  b76_sample
  b76_stage "$job" svg_render none 2d "$budget" /gobin/cad-svg-check --out /job/clean.render.svg /job/draw.render.svg
  b76_emit "$job" svg_render "${cm[@]}" stage=svg_render rep="$rep" "out=$job/clean.render.svg"
  lb=$(docker run --rm "${LIMITS[@]}" -v "$WORK/bin:/gobin:ro" -v "$job:/job" --entrypoint /gobin/cad-pixel-judge "$TAG" \
    --print-letterbox --svg /job/clean.render.svg --size "$SIZE" </dev/null) || b76_stop "reason=letterbox_failed,cell=$name,rep=$rep"
  sw=$(scaled_field sw "$lb"); sh=$(scaled_field sh "$lb"); left=$(scaled_field left "$lb"); top=$(scaled_field top "$lb")
  [ -n "$sw" ] || b76_stop "reason=letterbox_empty,cell=$name,rep=$rep"
  b76_sample
  b76_stage "$job" rsvg_ink "$RSVG_RO" 2d "$budget" /usr/bin/rsvg-convert -b white -w "$sw" -h "$sh" --page-width "$SIZE" --page-height "$SIZE" \
    --left "$left" --top "$top" -o /job/ink.png /job/clean.render.svg
  b76_emit "$job" rsvg_ink "${cm[@]}" stage=rsvg_ink background=opaque size="$SIZE" rep="$rep" "out=$job/ink.png" "extra=,letterbox=${sw}x${sh}+${left}+${top}"
  b76_sample
  b76_stage "$job" pixel_ink none 2d "$budget" /gobin/cad-pixel-judge --ink --size "$SIZE" /job/ink.png
  b76_emit "$job" pixel_ink "${cm[@]}" stage=pixel_ink background=opaque size="$SIZE" rep="$rep"
  b76_sample
  b76_stage "$job" pixel_opaque none 2d "$budget" /gobin/cad-pixel-judge --drawing --letterbox-svg /job/clean.render.svg --size "$SIZE" \
    --background opaque /job/ink.png
  b76_emit "$job" pixel_opaque "${cm[@]}" stage=pixel_opaque background=opaque size="$SIZE" rep="$rep"
  b76_sample
  b76_stage "$job" rsvg_pdf "$RSVG_RO" 2d "$budget" /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/clean.render.svg
  b76_emit "$job" rsvg_pdf "${cm[@]}" stage=rsvg_pdf rep="$rep" "out=$job/out.pdf"
  b76_sample
  b76_stage "$job" pdf none 2d "$budget" /gobin/cad-pdf-check --texts /job/draw.texts.json --svg /job/clean.render.svg /job/out.pdf
  b76_emit "$job" pdf "${cm[@]}" stage=pdf rep="$rep"
  b76_sample
  b76_stage "$job" rsvg_png_transparent "$RSVG_RO" 2d "$budget" /usr/bin/rsvg-convert -w "$sw" -h "$sh" --page-width "$SIZE" --page-height "$SIZE" \
    --left "$left" --top "$top" -o /job/out_t.png /job/clean.render.svg
  b76_emit "$job" rsvg_png_transparent "${cm[@]}" stage=rsvg_png_transparent background=transparent size="$SIZE" rep="$rep" "out=$job/out_t.png"
  b76_sample
  b76_stage "$job" pixel_transparent none 2d "$budget" /gobin/cad-pixel-judge --drawing --letterbox-svg /job/clean.render.svg --size "$SIZE" \
    --background transparent /job/out_t.png
  b76_emit "$job" pixel_transparent "${cm[@]}" stage=pixel_transparent background=transparent size="$SIZE" rep="$rep"
  rm -rf "$job"
}

b76_group_refusal() {
  local s rep v
  b76_cell L170031 "$B76_OUT"
  b76_guard_row L170031 1 1 "$B76_SETTER_S"
  v=$(scaled_field verdict "$GUARD_LAST")
  [ "$v" = too_complex ] || b76_stop "reason=l170031_not_refused,verdict=$v"
  printf 'REFUSAL_ROW,cell=L170031,format=dxf,bytes=%s,sha256=%s,inserts=%s,entities=%s,guard_caps=%s+%s,wall_s=%s,peak_mib=%s,verdict=%s,reason=%s,expect=too_complex,result=ok\n' \
    "$CELL_BYTES" "$CELL_SHA" "$GP_INSERTS" "$GP_ENTITIES" "$B76_DXF_INSERTS" "$B76_DXF_ENTITIES" "$(scaled_field wall_s "$GUARD_LAST")" \
    "$(scaled_field peak_mib "$GUARD_LAST")" "$v" "$(scaled_field reason "$GUARD_LAST")" >> "$B76_OUT"
  for s in "${B76_STRESS[@]}"; do
    b76_cell "stress_$s" "$B76_OUT"
    for rep in $(seq 1 "$B76_STRESS_REPS"); do b76_guard_row "stress_$s" "$rep" 1 "$B76_SETTER_S"; done
  done
}

b76_group_dxf() {
  local name rep
  b76_gen_identity "$B76_OUT" dxf-replicate "$BASE_DXF" --targets 2
  for name in "${B76_DXF_CELLS[@]}"; do
    b76_cell "$name" "$B76_OUT"
    for rep in $(seq 1 "$B76_N"); do b76_dxf_row "$name" "$rep"; done
  done
}

b76_class_for() {
  local fmt=$1 c
  for c in ${B76_CLASS_CELLS[@]+"${B76_CLASS_CELLS[@]}"}; do
    case "$fmt:$c" in stl:S1_*|stl:S2_*|step:S3_*|step:S4_*|step:S6_*|iges:S5_*) b76_class_3d "$c" ;; esac
  done
  local line
  while read -r line; do
    case "$line" in
      "BRACKET $fmt "*" none "*) printf 'NOT_RUN,format=%s,shape=%s,role=classification_within,reason=absent_in_76-01\n' "$fmt" "$(printf '%s' "$line" | awk '{print $3}')" >> "$B76_OUT" ;;
    esac
  done < "$WORK/brackets.txt"
}

b76_group_stl() { b76_setter_3d M4; b76_class_for stl; }
b76_group_step() { b76_setter_3d M1; b76_setter_3d M2; b76_class_for step; }
b76_group_iges() { b76_setter_3d M3; b76_class_for iges; }

b76_init_check() {
  [ -f "$B76_OUT" ] || fail "no init file $B76_OUT; run bounds76 --init first"
  INIT_COMMIT=$(sed -n 's/^HDR,mode=bounds76,commit=\([0-9a-f]*\),.*/\1/p' "$B76_OUT" | head -n 1)
  INIT_IMAGE=$(sed -n 's/^HDR,mode=bounds76,image=.*,image_id=\([^,]*\),.*/\1/p' "$B76_OUT" | head -n 1)
  INIT_TAG=$(sed -n 's/^HDR,mode=bounds76,image=\([^,]*\),image_id=.*/\1/p' "$B76_OUT" | head -n 1)
  [ -n "$INIT_COMMIT" ] && [ -n "$INIT_IMAGE" ] && [ -n "$INIT_TAG" ] || fail "the init HDR lines are missing in $B76_OUT"
  B76_AMEND=$(grep -c '^RULE_AMEND,key=rerun_after_watchdog,' "$B76_OUT" || true)
  B76_AMEND_FIX=$(grep -c '^RULE_AMEND,key=rerun_after_product_fix,' "$B76_OUT" || true)
  B76_AMEND_SLEEP=$(grep -c '^RULE_AMEND,key=rerun_after_host_sleep,' "$B76_OUT" || true)
}

B76_FIX_STOP_RE='^STOP,mode=bounds76,group=step,reason=row_failed,cell=[^,]*,stage=pixel,'
B76_FIX_GROUP=step
B76_FIX_PATHS='^(scripts/cad-dxf-bounds\.sh|internal/convert/cadpixel[^/]*\.go|internal/convert/cad_docs_sync_test\.go|internal/convert/testdata/cad/PROVENANCE\.md|internal/convert/testdata/cad/render-bg/[^/]*\.png|scripts/cmd/cad-pixel-judge/[^/]*|scripts/cad-liveproof\.sh|docs/[^/]*\.md)$'
B76_JUDGE_PATHS=(internal/convert/cadpixel.go scripts/cmd/cad-pixel-judge)

b76_go_bin_shas() {
  local c out=""
  for c in "${GO_CLIS[@]}" sandbox-probe; do out="$out+$c:$(sha_of "$WORK/bin/$c")"; done
  printf '%s' "${out#+}"
}

b76_other_stops() {
  { grep '^STOP,' "$B76_OUT" || true; } | { grep -vE ',reason=(watchdog_disk_below_threshold|host_sleep),' || true; }
}

b76_sleep_groups() {
  sed -n 's/^STOP,mode=bounds76,group=\([a-z0-9]*\),reason=host_sleep,.*/\1/p' "$B76_OUT" | sort -u
}

b76_caffeinated() {
  local pid=$$ ancestors="" a
  B76_CAFF_PID=""
  while [ -n "$pid" ] && [ "$pid" -gt 1 ]; do
    ancestors="$ancestors $pid"
    pid=$(ps -o ppid= -p "$pid" 2>/dev/null | tr -d ' ')
  done
  for a in $ancestors; do
    B76_CAFF_PID=$(pmset -g assertions | awk -v want="$a" '
      /\(caffeinate\):/ { cp = $2; sub(/\(.*/, "", cp); ps = ($0 ~ / PreventSystemSleep named/) }
      /asserting on behalf of/ && ps && !found { w = $NF; gsub(/[()]/, "", w); if (w == want) { print cp; found = 1 } }' || true)
    if [ -n "$B76_CAFF_PID" ]; then B76_CAFF_FOR=$a; break; fi
  done
  [ -n "$B76_CAFF_PID" ] || fail "the host_sleep re-run must run under caffeinate -dimsu holding PreventSystemSleep for this process or an ancestor"
}

b76_sleep_check() {
  local since=$1 found
  found=$(pmset -g log | python3 "$WORK/tools/b76.py" sleeps "$since" "$(date -u +%Y-%m-%dT%H:%M:%SZ)")
  [ -z "$found" ] || b76_stop "reason=host_sleep,source=pmset_-g_log,during=rerun_after_host_sleep,sleep_intervals_utc=$found"
}

b76_fix_diff() {
  local p cls bad="" planning=0 harness=0 judge_go=0 judge_test=0 judge_testdata=0 judge_cli=0 liveproof=0 docs=0
  B76_JUDGE_COMMIT=$(git log -1 --format=%H "$INIT_COMMIT..HEAD" -- "${B76_JUDGE_PATHS[@]}")
  [ -n "$B76_JUDGE_COMMIT" ] || fail "no judge fix commit between $INIT_COMMIT and HEAD"
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    case "$p" in .planning/*) planning=$((planning + 1)); continue ;; esac
    if ! printf '%s\n' "$p" | grep -qE "$B76_FIX_PATHS"; then bad="$bad $p"; continue; fi
    case "$p" in
      scripts/cad-dxf-bounds.sh) harness=$((harness + 1)) ;;
      internal/convert/*_test.go) judge_test=$((judge_test + 1)) ;;
      internal/convert/testdata/*) judge_testdata=$((judge_testdata + 1)) ;;
      internal/convert/*) judge_go=$((judge_go + 1)) ;;
      scripts/cmd/cad-pixel-judge/*) judge_cli=$((judge_cli + 1)) ;;
      scripts/cad-liveproof.sh) liveproof=$((liveproof + 1)) ;;
      docs/*) docs=$((docs + 1)) ;;
    esac
  done < <(git diff --name-only "$INIT_COMMIT" HEAD)
  [ -z "$bad" ] || fail "commit $(git rev-parse HEAD) changes${bad} against the init commit; rerun_after_product_fix allows only .planning, this script and the judge fix"
  B76_DIFF_CLASS="planning:$planning+harness:$harness+judge_go:$judge_go+judge_test:$judge_test+judge_testdata:$judge_testdata+judge_cli:$judge_cli+liveproof:$liveproof+docs:$docs"
  B76_GO_SRC=$( { git diff --name-only "$INIT_COMMIT" HEAD -- '*.go' || true; } | { grep -v '_test\.go$' || true; } | tr '\n' '+' | sed 's/+$//')
  [ -n "$B76_GO_SRC" ] || B76_GO_SRC=none
}

b76_watchdog_groups() {
  sed -n 's/^STOP,mode=bounds76,group=\([a-z0-9]*\),reason=watchdog_disk_below_threshold,.*/\1/p' "$B76_OUT" | sort -u
}

b76_group_gate() {
  local x other changed fixhdr
  other=$(b76_other_stops)
  if [ -n "$other" ]; then
    [ "$B76_AMEND_FIX" = 1 ] || fail "$B76_OUT holds a STOP line"
    [ "$(printf '%s\n' "$other" | wc -l | tr -d ' ')" = 1 ] && printf '%s\n' "$other" | grep -qE "$B76_FIX_STOP_RE" \
      || fail "$B76_OUT holds a STOP line the rerun_after_product_fix amendment does not cover"
    if ! { [ "$B76_GROUP" = "$B76_FIX_GROUP" ] && [ "$B76_RERUN" = 1 ]; }; then
      grep -q "^GROUP_END,group=$B76_FIX_GROUP,.*,result=ok" "$B76_OUT" || fail "group $B76_FIX_GROUP was stopped by the render judge and has not been re-run"
    fi
  fi
  for x in $(b76_sleep_groups); do
    [ "$B76_AMEND_SLEEP" = 1 ] || fail "group $x was stopped by host sleep and $B76_OUT has no rerun_after_host_sleep amendment"
    if [ "$x" = "$B76_GROUP" ] && [ "$B76_RERUN" = 1 ]; then continue; fi
    grep -q "^GROUP_END,group=$x,.*,result=ok" "$B76_OUT" || fail "group $x was stopped by host sleep and has not been re-run"
  done
  for x in $(b76_watchdog_groups); do
    [ "$B76_AMEND" = 1 ] || fail "group $x was stopped by the disk watchdog and $B76_OUT has no rerun amendment"
    if [ "$x" = "$B76_GROUP" ] && [ "$B76_RERUN" = 1 ]; then continue; fi
    grep -q "^GROUP_END,group=$x,.*,result=ok" "$B76_OUT" || fail "group $x was stopped by the disk watchdog and has not been re-run"
  done
  B76_RERUN_N=0
  B76_RERUN_KEY=none
  B76_SLEEP_HDR=""
  B76_SLEEP_COMMIT=""
  if [ "$B76_RERUN" = 1 ]; then
    if b76_sleep_groups | grep -qx "$B76_GROUP"; then
      [ "$(grep -c "^HDR,group=$B76_GROUP," "$B76_OUT")" = 1 ] || fail "group $B76_GROUP was already re-run after host sleep"
      [ "$(grep -c "^GROUP_END,group=$B76_GROUP," "$B76_OUT")" = 1 ] && grep -q "^GROUP_END,group=$B76_GROUP,.*,result=STOP$" "$B76_OUT" \
        || fail "group $B76_GROUP does not end in the one host-sleep STOP"
      B76_SLEEP_HDR=$(grep "^HDR,group=$B76_GROUP," "$B76_OUT")
      B76_SLEEP_COMMIT=$(printf '%s\n' "$B76_SLEEP_HDR" | tr ',' '\n' | sed -n 's/^commit=//p')
      changed=$(git diff --name-only "$B76_SLEEP_COMMIT" HEAD -- . ':(exclude).planning' | tr '\n' ' ' | sed 's/ $//')
      [ -z "$changed" ] || [ "$changed" = scripts/cad-dxf-bounds.sh ] \
        || fail "the host_sleep re-run of $B76_GROUP changes $changed against the commit of its stopped HDR"
      B76_RERUN_N=3
      B76_RERUN_KEY=rerun_after_host_sleep
    elif [ -n "$other" ] && [ "$B76_GROUP" = "$B76_FIX_GROUP" ]; then
      [ "$(grep -c "^HDR,group=$B76_GROUP," "$B76_OUT")" = 2 ] || fail "group $B76_GROUP was already re-run after the product fix"
      [ "$(grep -c "^GROUP_END,group=$B76_GROUP," "$B76_OUT")" = 1 ] && grep -q "^GROUP_END,group=$B76_GROUP,.*,result=STOP$" "$B76_OUT" \
        || fail "group $B76_GROUP does not end in the one render-judge STOP"
      B76_RERUN_N=2
      B76_RERUN_KEY=rerun_after_product_fix
    else
      [ "$B76_AMEND" = 1 ] || fail "--rerun needs the rerun_after_watchdog amendment"
      b76_watchdog_groups | grep -qx "$B76_GROUP" || fail "group $B76_GROUP was not stopped by the disk watchdog; it may not be re-run"
      [ "$(grep -c "^HDR,group=$B76_GROUP," "$B76_OUT")" = 1 ] || fail "group $B76_GROUP was already re-run once"
      grep -q "^GROUP_END,group=$B76_GROUP," "$B76_OUT" && fail "group $B76_GROUP has a GROUP_END; it may not be re-run"
      B76_RERUN_N=1
      B76_RERUN_KEY=rerun_after_watchdog
    fi
  else
    grep -q "^HDR,group=$B76_GROUP," "$B76_OUT" && fail "group $B76_GROUP already ran in $B76_OUT; a group never runs twice"
  fi
  B76_INPUTS_SAME=1
  if [ "$(git rev-parse HEAD)" != "$INIT_COMMIT" ]; then
    [ "$B76_AMEND" = 1 ] || fail "commit $(git rev-parse HEAD) differs from the init commit $INIT_COMMIT"
    if [ "$B76_AMEND_FIX" = 1 ]; then
      b76_fix_diff
      B76_INPUTS_SAME=0
      if [ "$B76_RERUN_KEY" != rerun_after_product_fix ]; then
        fixhdr=$(grep "^HDR,group=$B76_FIX_GROUP,.*,rerun_key=rerun_after_product_fix," "$B76_OUT" | tail -n 1 | tr ',' '\n' | sed -n 's/^commit=//p')
        if [ "$B76_RERUN_KEY" = rerun_after_host_sleep ] && [ "$fixhdr" = "$B76_SLEEP_COMMIT" ]; then
          :
        else
          [ "$fixhdr" = "$(git rev-parse HEAD)" ] || fail "group $B76_GROUP must run at the commit of the product-fix re-run (${fixhdr:-none})"
        fi
      fi
    else
      changed=$(git diff --name-only "$INIT_COMMIT" HEAD -- . ':(exclude).planning' | tr '\n' ' ' | sed 's/ $//')
      [ -z "$changed" ] || [ "$changed" = scripts/cad-dxf-bounds.sh ] || fail "commit $(git rev-parse HEAD) changes $changed against the init commit"
    fi
  fi
}

b76_amend_fix() {
  local other g n ok tr retained
  [ "$B76_AMEND_FIX" = 0 ] || fail "$B76_OUT already holds the rerun_after_product_fix amendment"
  other=$(b76_other_stops)
  [ -n "$other" ] && [ "$(printf '%s\n' "$other" | wc -l | tr -d ' ')" = 1 ] && printf '%s\n' "$other" | grep -qE "$B76_FIX_STOP_RE" \
    || fail "$B76_OUT holds no single render-judge STOP of group $B76_FIX_GROUP"
  [ "$(grep -c "^HDR,group=$B76_FIX_GROUP," "$B76_OUT")" = 2 ] && [ "$(grep -c "^GROUP_END,group=$B76_FIX_GROUP," "$B76_OUT")" = 1 ] \
    && grep -q "^GROUP_END,group=$B76_FIX_GROUP,.*,result=STOP$" "$B76_OUT" || fail "group $B76_FIX_GROUP is not in the state the amendment covers"
  b76_fix_diff
  WORK_NOTES=$(mktemp "${TMPDIR:-/tmp}/b76-notes.XXXXXX")
  retained=$( { find "${TMPDIR:-/tmp}" -maxdepth 4 -path '*cad-b76.*' -name '*.png' 2>/dev/null; find "$CAD_CACHE" -name '*.png' 2>/dev/null; } | wc -l | tr -d ' ')
  for g in dxf stl; do
    n=$(grep -cE "^BOUND76_ROW,.*,group=$g,stage=pixel(_[a-z]+)?," "$B76_OUT" || true)
    ok=$(grep -E "^BOUND76_ROW,.*,group=$g,stage=pixel(_[a-z]+)?," "$B76_OUT" | grep -c ',class=ok,' || true)
    tr=$(grep -E "^BOUND76_ROW,.*,group=$g,stage=pixel(_[a-z]+)?," "$B76_OUT" | grep -c ',background=transparent,' || true)
    printf 'NOTE,key=prefix_judge_%s,mode=bounds76,group=%s,judge_rows=%s,judge_rows_ok=%s,transparent_judge_rows=%s,group_end=%s,retained_pngs=%s,note=the_%s_group_was_judged_by_the_pre-fix_judge_(clone_commit_%s);the_judge_runs_as_its_own_wrapped_stage_never_inside_a_timed_stage_(guard;mesh;stats;render;dxf;rsvg_*)_so_the_stage_walls_of_the_group_are_unaffected;its_judge_rows_were_all_ok_under_the_old_judge;its_PNG_outputs_are_not_retained_(each_row_job_directory_is_removed_after_the_row_and_the_group_work_directory_on_exit;none_found_under_TMPDIR_cad-b76.*_or_the_CAD_cache)_so_no_offline_re-judge_with_the_fixed_cad-pixel-judge_was_run\n' \
      "$g" "$g" "$n" "$ok" "$tr" "$(grep "^GROUP_END,group=$g," "$B76_OUT" | tail -n 1 | tr ',' '\n' | sed -n 's/^result=//p')" "$retained" "$g" \
      "$(git rev-parse --short "$INIT_COMMIT")" >> "$WORK_NOTES"
    [ "$n" = "$ok" ] && [ "$n" -gt 0 ] || { rm -f "$WORK_NOTES"; fail "group $g has a judge row that is not ok"; }
  done
  [ "$retained" = 0 ] || { rm -f "$WORK_NOTES"; fail "$retained PNG files are retained; re-judge them before the NOTE claims none"; }
  printf 'RULE_AMEND,key=rerun_after_product_fix,mode=bounds76,date_utc=%s,commit=%s,init_commit=%s,judge_commit=%s,stopped_groups=%s,diff_class=%s,go_src_changed=%s,rule=group_step_stopped_by_STOP_reason=row_failed_at_stage=pixel_(the_render_judge_JudgeCADPNG_refused_the_M2_transparent_render_while_the_opaque_render_of_the_same_mesh_passed;a_product_defect_fixed_by_quick_261014-0lo)_may_be_re-run_once_more_after_the_judge_fix;its_partial_rows_from_both_earlier_attempts_and_both_STOP_lines_stay_in_the_file_and_count_in_W_(max)_which_can_only_raise_a_bound;the_re-run_commit_may_differ_from_init_commit_%s_only_in_.planning/**;scripts/cad-dxf-bounds.sh_and_the_judge_fix_paths_of_261014-0lo_(internal/convert/cadpixel*.go_and_its_tests_and_testdata;scripts/cmd/cad-pixel-judge/;scripts/cad-liveproof.sh;docs)_checked_by_git_diff_--name-only_against_the_init_commit_and_written_on_the_HDR_as_diff_class;judge_commit_and_inputs_identical_except=judge;any_other_differing_path_is_STOP;the_engine_image_must_have_the_same_tag_inputs_hash_as_init_(cad-render;cad-mesh;cad-guard;cad-dxf)_with_image_id_and_image_id_match_recorded;the_Go_judge_binaries_may_differ_(judge_only)_and_their_sha256_is_recorded;the_same_pre-registered_cells_and_rules_(a_regenerated_cell_whose_sha256_differs_from_its_earlier_CELL_line_is_STOP);group_iges_runs_at_the_same_commit_as_the_step_re-run_with_the_fixed_judge_and_records_judge_commit_on_its_HDR;no_other_rule_changes;any_other_re-run_is_refused\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(git rev-parse HEAD)" "$INIT_COMMIT" "$B76_JUDGE_COMMIT" "$B76_FIX_GROUP" "$B76_DIFF_CLASS" "$B76_GO_SRC" \
    "$(git rev-parse --short "$INIT_COMMIT")" >> "$B76_OUT"
  cat "$WORK_NOTES" >> "$B76_OUT"
  rm -f "$WORK_NOTES"
  tail -n 3 "$B76_OUT" >&2
  exit 0
}

b76_amend_sleep() {
  local g hdr out
  [ "$B76_AMEND_SLEEP" = 0 ] || fail "$B76_OUT already holds the rerun_after_host_sleep amendment"
  [ -s "$B76_ROW_LOG" ] || fail "no row log $B76_ROW_LOG"
  hdr=$(grep -n '^HDR,group=' "$B76_OUT" | tail -n 1)
  g=$(printf '%s\n' "$hdr" | tr ',' '\n' | sed -n 's/^group=//p')
  [ -n "$g" ] || fail "no group HDR in $B76_OUT"
  [ "$(grep -c "^HDR,group=$g," "$B76_OUT")" = 1 ] || fail "group $g already ran more than once"
  tail -n +"${hdr%%:*}" "$B76_OUT" | grep -qE '^(GROUP_END|STOP),' && fail "group $g already ended"
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-b76.XXXXXX")
  trap cleanup EXIT
  mkdir -p "$WORK/tools"
  b76_tools
  out=$(pmset -g log | python3 "$WORK/tools/b76.py" sleepstop "$B76_OUT" "$B76_ROW_LOG" "$g" "$(date -u +%Y-%m-%dT%H:%M:%SZ)") || fail "sleepstop: $out"
  printf '%s\n' "$out" | grep -E '^(STOP|GROUP_END),' >> "$B76_OUT"
  printf 'RULE_AMEND,key=rerun_after_host_sleep,mode=bounds76,date_utc=%s,commit=%s,init_commit=%s,stopped_groups=%s,rule=the_%s_group_stopped_by_STOP_reason=host_sleep_(the_host_Mac_entered_Idle_Sleep_at_2026-10-14_05:37:59_+0300_and_cycled_through_Sleep_and_DarkWake_until_FullWake_at_11:32:16_+0300_per_pmset_-g_log;a_wall_that_overlaps_host_sleep_measures_the_laptop_not_the_product)_may_be_re-run_once_with_the_same_measured_inputs_and_image_as_its_HDR_(the_re-run_commit_may_differ_from_the_stopped_HDR_commit_only_in_.planning_and_in_this_permission_in_scripts/cad-dxf-bounds.sh;checked_by_git_diff_and_written_as_inputs_identical_to_stopped_hdr=1;the_same_image_id_written_as_image_id_match_stopped_hdr=1;the_same_cells_and_sha256_else_STOP)_under_caffeinate_-dimsu_with_PreventSystemSleep_verified_by_pmset_-g_assertions_before_the_first_timed_row_and_recorded_on_the_HDR;rows_of_the_stopped_attempt_whose_[start;end]_(start=the_previous_row_log_line_or_the_group_HDR;end=its_row_log_line;a_row_without_a_reliable_interval_counts_as_overlapping)_overlaps_any_pmset_Sleep..Wake_interval_(Sleep_to_the_next_DarkWake_or_Wake_line)_are_marked_contaminated=1_in_a_NOTE_line_each_and_are_excluded_from_W_because_their_wall_is_host_sleep_not_product_time;every_non-overlapping_row_of_the_stopped_attempt_stays_and_counts_in_W;this_is_the_only_exclusion_and_it_is_defined_by_the_pmset_evidence_not_by_the_wall_value;before_the_re-run_ends_pmset_-g_log_is_re-checked_and_a_Sleep_during_the_re-run_is_STOP_reason=host_sleep_with_no_third_attempt;the_STOP_line_stays;no_other_rule_changes;any_other_re-run_is_refused\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(git rev-parse HEAD)" "$INIT_COMMIT" "$g" "$g" >> "$B76_OUT"
  printf '%s\n' "$out" | grep -E '^NOTE,' >> "$B76_OUT"
  grep -E '^(STOP|GROUP_END|RULE_AMEND|NOTE),' "$B76_OUT" | tail -n 30 | cut -c1-260 >&2
  exit 0
}

b76_amend() {
  local stopped
  [ -z "$B76_ROW_LOG" ] || b76_amend_sleep
  [ "$B76_AMEND" = 0 ] && [ -z "$(b76_other_stops)" ] || b76_amend_fix
  stopped=$(b76_watchdog_groups | tr '\n' '+' | sed 's/+$//')
  [ -n "$stopped" ] || fail "$B76_OUT holds no disk-watchdog STOP"
  printf 'RULE_AMEND,key=rerun_after_watchdog,mode=bounds76,date_utc=%s,commit=%s,init_commit=%s,stopped_groups=%s,rule=a_group_stopped_by_the_environment_disk_watchdog_(STOP_reason=watchdog_disk_below_threshold)_may_be_re-run_once;its_partial_rows_stay_in_the_file_and_are_counted_in_W_(max)_which_can_only_raise_a_bound;the_re-run_uses_the_measured_inputs_of_clone_commit_%s_(the_re-run_commit_may_differ_only_in_.planning_and_in_this_permission_in_scripts/cad-dxf-bounds.sh;checked_by_git_diff_and_written_as_inputs_identical=1);the_same_image_build_inputs_(the_same_engine_tag_inputs_hash;an_image_id_other_than_the_init_one_is_recorded);the_same_pre-registered_cells_and_rules_(a_regenerated_cell_whose_sha256_differs_from_its_earlier_CELL_line_is_STOP);the_STOP_line_stays;no_other_rule_changes;any_other_re-run_is_refused\n' \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$(git rev-parse HEAD)" "$INIT_COMMIT" "$stopped" "$(git rev-parse --short "$INIT_COMMIT")" >> "$B76_OUT"
  tail -n 1 "$B76_OUT" >&2
  exit 0
}

b76_mode() {
  local sub=$1 g t0 groups
  case "$sub" in
    init)
      [ ! -s "$B76_OUT" ] || fail "$B76_OUT already holds lines; --init writes only a new file"
      b76_setup
      b76_classes
      mkdir -p "$(dirname "$B76_OUT")"
      { b76_hdr_lines bounds76; b76_rule_lines "$B76_CLASS_TEXT" "$(basename "$LARGE_STL")"; } > "$B76_OUT.tmp"
      mv -f "$B76_OUT.tmp" "$B76_OUT"
      grep -E '^(HDR|RULE),' "$B76_OUT" | cut -c1-160 >&2
      exit 0 ;;
    smoke)
      b76_setup
      b76_classes
      mkdir -p "$(dirname "$B76_OUT")"
      { b76_hdr_lines bounds76; b76_rule_lines "$B76_CLASS_TEXT" "$(basename "$LARGE_STL")"; } > "$B76_OUT"
      b76_smoke ;;
    cells)
      b76_setup
      b76_classes
      B76_GROUP=cells
      : > "$B76_OUT"
      b76_hdr_lines bounds76_cells >> "$B76_OUT"
      b76_gen_identity "$B76_OUT" dxf-replicate "$BASE_DXF" --targets 2
      b76_gen_identity "$B76_OUT" step-copies "$STACK_STEP_BASE" --targets 2
      for g in M1 M2 M3 M4 C L170030 I170031x4 R123731968 L170031 ${B76_CLASS_CELLS[@]+"${B76_CLASS_CELLS[@]}"}; do b76_cell "$g" "$B76_OUT"; done
      printf 'SUMMARY,mode=bounds76_cells,cells=%s,result=ok\n' "$(grep -c '^CELL,' "$B76_OUT")" >> "$B76_OUT"
      grep -E '^(CELL|GEN_IDENTITY|SUMMARY),' "$B76_OUT" | cut -c1-220 >&2
      exit 0 ;;
    group)
      b76_init_check
      b76_group_gate
      b76_setup
      B76_IMAGE_MATCH=1
      [ "$B76_AMEND_FIX" = 0 ] || [ "$TAG" = "$INIT_TAG" ] || fail "engine tag $TAG differs from the init tag $INIT_TAG"
      if [ "$B76_RERUN_KEY" = rerun_after_host_sleep ]; then
        [ "$IMAGE_ID" = "$(printf '%s\n' "$B76_SLEEP_HDR" | tr ',' '\n' | sed -n 's/^image_id=//p')" ] || fail "image id $IMAGE_ID differs from the stopped $B76_GROUP HDR"
        b76_caffeinated
      fi
      if [ "$IMAGE_ID" != "$INIT_IMAGE" ]; then
        [ "$B76_AMEND" = 1 ] && [ "$TAG" = "$INIT_TAG" ] || fail "image id $IMAGE_ID differs from the init image id $INIT_IMAGE"
        B76_IMAGE_MATCH=0
      fi
      b76_classes
      B76_T0=$(date +%s)
      b76_sample
      B76_HDR_FIX=""
      if [ "$B76_AMEND_FIX" = 1 ]; then
        B76_HDR_FIX=$(printf ',rerun_key=%s,judge_commit=%s,inputs_identical_except=judge,diff_class=%s,go_src_changed=%s,go_bin_sha256=%s' \
          "$B76_RERUN_KEY" "$B76_JUDGE_COMMIT" "$B76_DIFF_CLASS" "$B76_GO_SRC" "$(b76_go_bin_shas)")
      fi
      if [ "$B76_RERUN_KEY" = rerun_after_host_sleep ]; then
        B76_HDR_FIX="$B76_HDR_FIX,stopped_hdr_commit=$B76_SLEEP_COMMIT,inputs_identical_to_stopped_hdr=1,image_id_match_stopped_hdr=1,caffeinate=-dimsu,caffeinate_pid=$B76_CAFF_PID,caffeinate_on_behalf_of=$B76_CAFF_FOR,prevent_system_sleep=1"
      fi
      B76_HDR_DATE=$(date -u +%Y-%m-%dT%H:%M:%SZ)
      printf 'HDR,group=%s,mode=bounds76,commit=%s,tracked_dirt=%s,image=%s,image_id=%s,date_utc=%s,load1=%s,vm_free_gib=%s,rerun=%s,init_commit=%s,inputs_identical=%s,init_image_id=%s,image_id_match=%s%s\n' "$B76_GROUP" \
        "$(git rev-parse HEAD)" "$(tracked_dirt)" "$TAG" "$IMAGE_ID" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$B76_LOAD" "$B76_FREE" \
        "$B76_RERUN_N" "$INIT_COMMIT" "$B76_INPUTS_SAME" "$INIT_IMAGE" "$B76_IMAGE_MATCH" "$B76_HDR_FIX" >> "$B76_OUT"
      case "$B76_GROUP" in
        refusal) b76_group_refusal ;;
        dxf) b76_group_dxf ;;
        stl) b76_group_stl ;;
        step) b76_group_step ;;
        iges) b76_group_iges ;;
        pathb)
          [ -n "${B76_BYTE_BOUND// /}" ] || b76_stop "reason=pathb_without_a_byte_bound_format" ;;
        *) fail "unknown group $B76_GROUP" ;;
      esac
      [ "$B76_RERUN_KEY" != rerun_after_product_fix ] || python3 "$WORK/tools/b76.py" fixnote "$B76_OUT" >> "$B76_OUT"
      if [ "$B76_RERUN_KEY" = rerun_after_host_sleep ]; then
        b76_sleep_check "$B76_HDR_DATE"
        printf 'NOTE,key=host_sleep_recheck,mode=bounds76,group=%s,since_utc=%s,until_utc=%s,source=pmset_-g_log,sleeps=0\n' "$B76_GROUP" "$B76_HDR_DATE" \
          "$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> "$B76_OUT"
      fi
      b76_sample
      printf 'GROUP_END,group=%s,rows=%s,duration_s=%s,date_utc=%s,load1=%s,vm_free_gib=%s,result=ok\n' "$B76_GROUP" \
        "$(grep -c "^BOUND76_ROW,.*,group=$B76_GROUP," "$B76_OUT" || true)" "$(( $(date +%s) - B76_T0 ))" "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        "$B76_LOAD" "$B76_FREE" >> "$B76_OUT"
      exit 0 ;;
    amend)
      b76_init_check
      b76_amend ;;
    finish)
      b76_init_check
      WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-b76.XXXXXX")
      trap cleanup EXIT
      mkdir -p "$WORK/tools"
      b76_tools
      python3 "$WORK/tools/b76.py" brackets "$REPO_ROOT/$B76_STACK_REL" > "$WORK/brackets.txt" || true
      groups=$(IFS=+; echo "${B76_GROUPS[*]}")
      if awk '$1 == "DECISION" && $3 == "byte_bound"' "$WORK/brackets.txt" | grep -q .; then
        groups="$groups+pathb"
      elif ! grep -q '^NOT_RUN,group=pathb,' "$B76_OUT"; then
        printf 'NOT_RUN,group=pathb,reason=no_byte_bound_format_in_76-01\n' >> "$B76_OUT"
      fi
      python3 "$WORK/tools/b76.py" finish "$B76_OUT" "$groups" > "$WORK/finish.out" || { cat "$WORK/finish.out" >> "$B76_OUT"; cat "$WORK/finish.out" >&2; exit 1; }
      cat "$WORK/finish.out" >> "$B76_OUT"
      cat "$WORK/finish.out" >&2
      exit 0 ;;
  esac
  usage
}

b76_smoke() {
  local keepdir path
  B76_GROUP=smoke
  CELL_PATH="$REPO_ROOT/$STACK_STEP_BASE"
  CELL_FMT=step
  CELL_BYTES=$(bytes_of "$CELL_PATH")
  CELL_SHA=$(sha_of "$CELL_PATH")
  keepdir=$(mktemp -d "$WORK/keep.XXXXXX")
  b76_guard_row smoke3d 1 1 "$B76_SETTER_S"
  b76_mesh_row smoke3d fine 1 1 "$B76_SETTER_S" "$keepdir"
  [ -f "$keepdir/tri_fine" ] || b76_stop "reason=smoke_no_fine_mesh"
  b76_render_row smoke3d fine iso opaque 1 1 "$B76_SETTER_S" "$keepdir"
  rm -rf "$keepdir"
  GEN_MOUNTS=()
  b76_gen "$WORK/gen.out" --scaled-from "$BASE_DXF" --kind dxf-replicate --exact-bytes 2097152
  b76_gen_file "$WORK/gen.out" dxf-replicate
  CELL_PATH=$GEN_PATH
  CELL_FMT=dxf
  CELL_BYTES=$(bytes_of "$CELL_PATH")
  CELL_SHA=$(sha_of "$CELL_PATH")
  printf 'CELL,cell=smoke2d,format=dxf,file=%s,bytes=%s,target_bytes=2097152,pad_gap_bytes=%s,sha256=%s\n' "$(basename "$CELL_PATH")" "$CELL_BYTES" \
    "$(scaled_field pad_gap_bytes "$GEN_LINE")" "$CELL_SHA" >> "$B76_OUT"
  b76_dxf_row smoke2d 1
  rm -f "$CELL_PATH"
  if grep -q '^STOP,' "$B76_OUT" || grep -q ',band=failed,' "$B76_OUT"; then
    printf 'SUMMARY,mode=bounds76_smoke,rows=2,result=STOP\n' >> "$B76_OUT"
    exit 1
  fi
  printf 'SUMMARY,mode=bounds76_smoke,rows=2,result=ok,stage_rows=%s\n' "$(grep -c '^BOUND76_ROW,' "$B76_OUT")" >> "$B76_OUT"
  grep -E '^(BOUND76_ROW|SUMMARY),' "$B76_OUT" | cut -c1-200 >&2
  exit 0
}

MODE=${1:-}
[ -n "$MODE" ] || usage
shift
OUT_SET=0
SMOKE=0
B76_SUB=""
B76_RERUN=0
B76_ROW_LOG=""
B76_GROUP=none
b76_sub() { [ "$MODE" = bounds76 ] && [ -z "$B76_SUB" ] || usage; B76_SUB=$1; }
while [ $# -gt 0 ]; do
  case "$1" in
    --out) [ $# -ge 2 ] || usage; EVID=$2; OUT_SET=1; shift 2 ;;
    --smoke)
      case "$MODE" in
        stack) SMOKE=1 ;;
        bounds76) b76_sub smoke ;;
        *) usage ;;
      esac
      shift ;;
    --init) b76_sub init; shift ;;
    --cells-only) b76_sub cells; shift ;;
    --finish) b76_sub finish; shift ;;
    --group) [ $# -ge 2 ] || usage; b76_sub group; B76_GROUP=$2; shift 2 ;;
    --rerun) [ "$MODE" = bounds76 ] || usage; B76_RERUN=1; shift ;;
    --amend-rerun) b76_sub amend; shift ;;
    --row-log) [ $# -ge 2 ] && [ "$MODE" = bounds76 ] || usage; B76_ROW_LOG=$2; shift 2 ;;
    *) usage ;;
  esac
done
case "$MODE" in
  smoke)
    [ "$OUT_SET" = 1 ] || EVID="${TMPDIR:-/tmp}/75-05-bounds-smoke.txt"
    smoke_mode ;;
  run) run_mode ;;
  floor) floor_mode ;;
  stack)
    if [ "$SMOKE" = 1 ]; then
      [ "$OUT_SET" = 1 ] || EVID="${TMPDIR:-/tmp}/76-01-stack-smoke.txt"
      stack_smoke_mode
    fi
    [ "$OUT_SET" = 1 ] || EVID="$REPO_ROOT/$STACK_EVID_REL"
    stack_mode ;;
  bounds76)
    [ -n "$B76_SUB" ] || usage
    [ "$B76_RERUN" = 0 ] || [ "$B76_SUB" = group ] || usage
    if [ "$OUT_SET" = 0 ]; then
      case "$B76_SUB" in
        smoke) EVID="${TMPDIR:-/tmp}/76-02-smoke.txt" ;;
        cells) EVID="${TMPDIR:-/tmp}/76-02-cells.txt" ;;
        *) EVID="$REPO_ROOT/$B76_EVID_REL" ;;
      esac
    fi
    B76_OUT=$EVID
    b76_mode "$B76_SUB" ;;
  *) usage ;;
esac

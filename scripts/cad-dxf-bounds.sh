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
  tag="octoconv-cad-worker:engine75-$(inputs_short)"
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

finish() {
  local code=$1
  python3 "$WORK/tools/derive.py" emit "$MODE" "$MEAS" >> "$LINES"
  mkdir -p "$(dirname "$EVID")"
  cp "$LINES" "$EVID"
  grep -E '^(DERIVED|CHECK_ROW|GAP|STOP|SUMMARY),' "$EVID" >&2 || true
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
            for st in STAGES:
                m = ss.get(st)
                if m is None or m["wrap_ok"] != "1" or m["rc"] != "0" or m.get("timeout") != "0":
                    return "failed:%s:rep%d" % (st, rep)
                if st in ("guard", "dxf") and m["verdict"] != "ok":
                    return "failed:%s_verdict_%s:rep%d" % (st, m["verdict"], rep)
            gg = {g["step"]: g for g in self.gos(row, rep)}
            for st in GO_STEPS:
                g = gg.get(st)
                if g is None or g["wrap_ok"] != "1" or g["rc"] != "0" or g["ok"] != "1":
                    return "failed:%s:rep%d" % (st, rep)
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
  rule_lines > "$LINES"
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
    wrapped G "$row" "$rep" pdf "$job" none "" /gobin/cad-pdf-check --texts /job/draw.texts.json /job/out.pdf || ok=0
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

MODE=${1:-}
[ -n "$MODE" ] || usage
shift
OUT_SET=0
while [ $# -gt 0 ]; do
  case "$1" in
    --out) [ $# -ge 2 ] || usage; EVID=$2; OUT_SET=1; shift 2 ;;
    *) usage ;;
  esac
done
case "$MODE" in
  smoke)
    [ "$OUT_SET" = 1 ] || EVID="${TMPDIR:-/tmp}/75-05-bounds-smoke.txt"
    smoke_mode ;;
  run) run_mode ;;
  *) usage ;;
esac

#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

PHASE_DIR="$REPO_ROOT/.planning/phases/75-dxf-2d-pipeline"
EVID="$PHASE_DIR/evidence/75-01-engine.txt"
EVID_NAME="evidence/75-01-engine.txt"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
DOCKERFILE="$REPO_ROOT/Dockerfile.cad-worker"
COMMON_LOCK="$REPO_ROOT/scripts/cad-measure/requirements-common.txt"
DXF_LOCK="$REPO_ROOT/cmd/cad-worker/requirements-dxf.txt"
MESH_LOCK="$REPO_ROOT/cmd/cad-worker/requirements-mesh.txt"
GO_TOOLCHAIN=go1.26.4
MIN_FREE_GIB=6
FRAME=1280
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
STAGE_ENV=(-e HOME=/job/.home -e XDG_CACHE_HOME=/job/.home/.cache -e XDG_CONFIG_HOME=/job/.home/.config -e TMPDIR=/job)
HELPERS=(cad-mesh cad-guard cad-render cad-dxf)
PY_PACKAGES=(ezdxf fonttools numpy pillow pyparsing typing-extensions)
RW_SET=/job
OUT_OVERRIDE=""
FIXTURE_DIR="$CORPUS/dxf"
FIXTURE_COUNT=43
HELPER_EVID="$PHASE_DIR/evidence/75-02-helper.txt"
HELPER_EVID_NAME="evidence/75-02-helper.txt"
DXF_OUT="$CORPUS/dxf-out"
MAX_DRAWN=850155
CAD_CACHE="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
WRAP_PY="$REPO_ROOT/scripts/cad-measure/measure_wrap.py"
GROUP=all
WRITE_OUT=0
JUDGE_EVID="$PHASE_DIR/evidence/75-08-judge.txt"
JUDGE_EVID_NAME="evidence/75-08-judge.txt"
LANDLOCK_EVID="$PHASE_DIR/evidence/75-08-landlock.txt"
PIXELCHECK="$REPO_ROOT/scripts/cad-measure/pixelcheck.py"
JUDGE_DRAWINGS="public/hatches_2 public/acad_table_with_blk_ref public/uncommon public/dimension_in_nested_blocks
  public/columns_R2007 synthetic/legacy_cp1251 synthetic/legacy_latin1 public/houses_of_parliament_georeferenced
  dxf/wide dxf/tall dxf/far_point dxf/xline_ray dxf/text_cases"
JUDGE_SIZES="256 1280 2048"

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/cad-dxf-probe.sh tracer   [--out FILE]
  scripts/cad-dxf-probe.sh fixtures [--write] [--out FILE]
  scripts/cad-dxf-probe.sh helper   [--group semantics|safety|corpus|all] [--write-out] [--out FILE]
  scripts/cad-dxf-probe.sh judge    [--out FILE]

tracer    build the engine image of Dockerfile.cad-worker, draw legacy_cp1251.dxf and
          uncommon.dxf with cad-dxf under its candidate Landlock ruleset, turn each SVG into a
          PDF and a letterboxed 1280 x 1280 PNG with rsvg-convert under its own candidate
          ruleset, check the SVG text elements, the PDF text (pdftotext on the host) and the PNG
          size, and prove that a DXF or an SVG outside every grant is not read.
fixtures  generate the phase fixture set with scripts/cad-corpus-gen.py --dxf-fixtures inside
          the engine image (PYTHONHASHSEED=0), require every committed file under
          internal/convert/testdata/cad/dxf to be byte-identical to the fresh generation (--write
          copies the fresh files in first), then strict-read and audit each with ezdxf and print
          one FIXTURE line per file. Drawing, units and extents fixtures must read with no audit
          error; every cyclic fixture must show audit code 104; the rest are recorded as facts.
helper    run the production cad-dxf under its candidate Landlock ruleset and the stage
          environment of the tracer and judge every output against pre-registered expectations:
          semantics (what is drawn, skipped, framed and declared), safety (texts.json, XML-safe
          text, font failure, backstops), corpus (the 17 corpus DXF wrapped by measure_wrap.py,
          and text fidelity against ezdxf's own outline rendering at 1280 px). One HELPER,
          CORPUS or FIDELITY row per check and one SUMMARY line per group. --write-out refreshes
          internal/convert/testdata/cad/dxf-out with the real outputs. The evidence file defaults
          to .planning/phases/75-dxf-2d-pipeline/evidence/75-02-helper.txt and a group re-run
          replaces only its own lines.
judge     draw the 8 phase-71 drawings and wide, tall, far_point, xline_ray and text_cases with
          cad-dxf under its measured ruleset (MEASURED,stage=dxf of evidence/75-08-landlock.txt),
          validate the render-only SVG with the Go validator, and rasterize it with rsvg-convert
          under its measured ruleset into the Go letterbox at 256, 1280 and 2048, opaque and
          transparent. Every frame is judged by the Go drawing judge (cad-pixel-judge --drawing)
          and by the 71 judge (pixelcheck.py single --mode drawing, in the engine image); each
          transparent frame's Go ink box is compared with its opaque twin's, and a blank 1280
          frame is judged by both. Disagreements are classified by the pre-registered classes E1,
          E2 and E3; anything else is unexplained. The evidence file defaults to
          .planning/phases/75-dxf-2d-pipeline/evidence/75-08-judge.txt and is rewritten whole.

Every mode rewrites its own lines in the evidence file (default
.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt for tracer and fixtures) and
removes the image tags it built. Needs a native arm64 docker server, go, python3 and pdftotext.
Exit codes: 64 usage, 69 a tool is missing, 3 VM disk below 6 GiB, 1 a check failed.
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

lock_blocks() { awk '/^[A-Za-z0-9_.-]+==/{keep = ($0 !~ /^numpy==/)} keep' "$COMMON_LOCK"; }

numpy_block() { awk '/^[A-Za-z0-9_.-]+==/{keep = ($0 ~ /^numpy==/)} keep' "$1"; }

lock_line() {
  local hashes packages numpy=differs
  if ! lock_blocks | cmp -s - "$DXF_LOCK"; then
    printf 'LOCK,file=requirements-dxf.txt,cmp=differs,result=fail\n'
    return 1
  fi
  hashes=$(grep -c 'hash=sha256:' "$DXF_LOCK" || true)
  packages=$(grep -E '^[A-Za-z0-9_.-]+==' "$DXF_LOCK" | awk '{print $1}' | paste -sd '+' -)
  if numpy_block "$MESH_LOCK" | cmp -s - <(numpy_block "$COMMON_LOCK"); then numpy=identical; fi
  if [ "$hashes" != 183 ] || [ "$numpy" != identical ]; then
    printf 'LOCK,file=requirements-dxf.txt,cmp=identical,hashes=%s,numpy_block=%s,result=fail\n' "$hashes" "$numpy"
    return 1
  fi
  printf 'LOCK,file=requirements-dxf.txt,source=scripts/cad-measure/requirements-common.txt,cmp=identical,packages=%s,hashes=%s,sha256=%s,numpy_block_vs_mesh=%s,result=ok\n' \
    "$packages" "$hashes" "$(sha_of "$DXF_LOCK")" "$numpy"
}

inputs_short() {
  {
    shasum -a 256 "$DOCKERFILE" "$MESH_LOCK" "$REPO_ROOT/cmd/cad-worker/requirements-vtk.txt" "$DXF_LOCK"
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
  local tag stage
  tag="octoconv-cad-worker:engine75-$(inputs_short)"
  stage=$(mktemp -d "${TMPDIR:-/tmp}/cad-dxf-build.XXXXXX")
  mkdir -p "$stage/cmd/cad-worker/helper"
  cp "$DOCKERFILE" "$stage/Dockerfile.cad-worker"
  cp "$REPO_ROOT"/cmd/cad-worker/requirements-*.txt "$REPO_ROOT"/cmd/cad-worker/vtk_keeplist.py \
    "$REPO_ROOT"/cmd/cad-worker/vtk-keeplist.txt "$stage/cmd/cad-worker/"
  cp "$REPO_ROOT"/cmd/cad-worker/helper/* "$stage/cmd/cad-worker/helper/"
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

header_lines() {
  local mode=$1 tag=$2 inner
  inner='
    printf "kernel=%s\n" "$(uname -r)"
    printf "librsvg2_bin=%s\n" "$(dpkg-query -W -f="\${Version}" librsvg2-bin)"
    printf "fonts_dejavu_core=%s\n" "$(dpkg-query -W -f="\${Version}" fonts-dejavu-core)"
    /opt/venv/bin/python -I -B -c "
import importlib.metadata as m, sys
for p in sys.argv[1:]:
    print(\"py_%s=%s\" % (p.replace(\"-\", \"_\"), m.version(p)))
" '"${PY_PACKAGES[*]}"'
  '
  printf 'HDR,mode=%s,date_utc=%s\n' "$mode" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'HDR,mode=%s,commit=%s,tracked_dirt=%s\n' "$mode" "$(git rev-parse HEAD)" "$(tracked_dirt)"
  printf 'HDR,mode=%s,image=%s,image_id=%s,image_bytes=%s\n' "$mode" "$tag" \
    "$(docker image inspect "$tag" --format '{{.Id}}')" "$(docker image inspect "$tag" --format '{{.Size}}')"
  printf 'HDR,mode=%s,server_arch=%s\n' "$mode" "$(docker version --format '{{.Server.Arch}}')"
  docker run --rm --network none --entrypoint sh "$tag" -c "$inner" </dev/null | sed "s/^/HDR,mode=$mode,/"
  if [ "$mode" = tracer ]; then
    printf 'HDR,mode=%s,limits=cpus2_memory4g_swap4g_network_none_user_nobody,env=HOME=/job/.home_XDG_CACHE_HOME=/job/.home/.cache_XDG_CONFIG_HOME=/job/.home/.config_TMPDIR=/job_LANG=unset\n' "$mode"
  elif [ "${mode%%,*}" = helper ] || [ "$mode" = judge ]; then
    printf 'HDR,mode=%s,limits=cpus2_memory4g_swap4g_network_none_user_nobody,env=HOME=/job/.home_XDG_CACHE_HOME=/job/.home/.cache_XDG_CONFIG_HOME=/job/.home/.config_TMPDIR=/job_LANG=unset,max_drawn_entities=%s\n' "$mode" "$MAX_DRAWN"
  else
    printf 'HDR,mode=%s,limits=cpus2_memory4g_swap4g_network_none_user_nobody,env=HOME=/tmp_PYTHONHASHSEED=0_python=-B,unconfined=1\n' "$mode"
  fi
}

evid_put() {
  local lines=$1 pattern=$2 keep
  mkdir -p "$(dirname "$EVID")"
  keep=$(mktemp "${TMPDIR:-/tmp}/cad-dxf-keep.XXXXXX")
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
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-dxf-probe.XXXXXX")
  chmod 0755 "$WORK"
  trap cleanup EXIT
  LOCK_LINE=$(lock_line) || { echo "$LOCK_LINE" >&2; fail "requirements-dxf.txt is not the five non-numpy blocks of the 71 lock"; }
  TAG=$(build_engine)
  BUILT_TAGS+=("$TAG")
  cad_require_native "$TAG" || fail "engine image is not native"
  ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
  TRIPLET=$(triplet_for "$ARCH")
  DXF_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/share/fonts:/usr/lib/$TRIPLET:/usr/local/bin/cad-dxf"
  RSVG_RO="/usr/share/fonts:/usr/lib/$TRIPLET:/usr/bin/rsvg-convert"
  DXF_NOFONT_RO="/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/$TRIPLET:/usr/local/bin/cad-dxf"
  RSVG_NOFONT_RO="/usr/lib/$TRIPLET:/usr/bin/rsvg-convert"
  build_probe "$ARCH" "$WORK/probe"
  LINES="$WORK/lines.txt"
  : > "$LINES"
  {
    printf '%s\n' "$LOCK_LINE"
    header_lines "$mode" "$TAG"
    [ "$mode" != tracer ] || printf 'HDR,mode=%s,ro_cad_dxf=%s,ro_rsvg=%s,rw=%s\n' "$mode" "$DXF_RO" "$RSVG_RO" "$RW_SET"
  } >> "$LINES"
  HDR_LINES="$WORK/hdr.txt"
  grep -v '^LOCK,' "$LINES" > "$HDR_LINES" || true
}

run_confined() {
  local ro=$1 job=$2 stdout=$3 stderr=$4 rc=0
  shift 4
  docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -e "PROBE_RO=$ro" -e "PROBE_RW=$RW_SET" \
    --entrypoint /probe/sandbox-probe -v "$WORK/probe:/probe:ro" -v "$job:/job" ${MOUNTS[@]+"${MOUNTS[@]}"} "$TAG" \
    "$@" > "$stdout" 2> "$stderr" </dev/null || rc=$?
  return $rc
}

run_unconfined() {
  local job=$1 stdout=$2 stderr=$3 rc=0
  shift 3
  docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -v "$job:/job" ${MOUNTS[@]+"${MOUNTS[@]}"} "$TAG" \
    "$@" > "$stdout" 2> "$stderr" </dev/null || rc=$?
  return $rc
}

verdict_fields() {
  python3 - "$1" <<'PY'
import json, sys
last = ""
with open(sys.argv[1], "rb") as fh:
    for raw in fh:
        line = raw.decode("ascii", "replace").rstrip("\n")
        if line.startswith("CADVERDICT "):
            last = line
if not last:
    print("verdict=none,reason=none,verdict_texts=na")
    sys.exit(0)
try:
    obj = json.loads(last[len("CADVERDICT "):])
except ValueError:
    print("verdict=unparsable,reason=none,verdict_texts=na")
    sys.exit(0)
verdict = obj.get("verdict") if obj.get("stage") == "dxf" else "wrong_stage"
print("verdict=%s,reason=%s,verdict_texts=%s,entities_drawn=%s,page_mm=%sx%s,insunits=%s,codepage=%s" % (
    verdict, obj.get("reason") or "none", obj.get("texts"), obj.get("entities_drawn"),
    obj.get("page_w_mm"), obj.get("page_h_mm"), obj.get("insunits"), obj.get("codepage")))
PY
}

svg_fields() {
  python3 - "$1" "$2" "$3" <<'PY'
import sys
import xml.etree.ElementTree as ET
path, expect, texts_out = sys.argv[1:4]
NS = "{http://www.w3.org/2000/svg}"
ALLOWED = {"svg", "defs", "rect", "g", "path", "text"}
BANNED = {"style", "class", "href", "id"}
try:
    root = ET.parse(path).getroot()
except Exception as exc:
    print("svg=unparsable_%s,svg_texts=0,elements_ok=0,attrs_ok=0,text_ok=0" % exc.__class__.__name__)
    sys.exit(0)
elements_ok = 1
attrs_ok = 1
texts = []
for el in root.iter():
    if not el.tag.startswith(NS) or el.tag[len(NS):] not in ALLOWED:
        elements_ok = 0
    for name in el.attrib:
        local = name.rsplit("}", 1)[-1].split(":")[-1].lower()
        if local in BANNED or local.startswith("on"):
            attrs_ok = 0
    if el.tag == NS + "text":
        texts.append(el.text or "")
with open(texts_out, "w", encoding="utf-8") as fh:
    fh.write("".join("".join(t.split()) for t in texts))
if expect.startswith("exact:"):
    text_ok = 1 if texts == [expect[len("exact:"):]] else 0
else:
    text_ok = 1 if len(texts) >= int(expect[len("min:"):]) else 0
print("svg=parsed,svg_texts=%d,elements_ok=%d,attrs_ok=%d,text_ok=%d" % (len(texts), elements_ok, attrs_ok, text_ok))
PY
}

pdf_fields() {
  python3 - "$1" "$2" "$3" <<'PY'
import subprocess, sys
pdf, texts_file, mode = sys.argv[1:4]
with open(pdf, "rb") as fh:
    head = fh.read(5)
if head != b"%PDF-":
    print("pdf_magic=0,pdf_text_ok=0,pdf_chars=0")
    sys.exit(0)
out = subprocess.run(["pdftotext", "-raw", pdf, "-"], capture_output=True).stdout.decode("utf-8", "replace")
got = "".join(out.split())
with open(texts_file, encoding="utf-8") as fh:
    want = fh.read()
if mode == "substring":
    ok = want in got and len(want) > 0
else:
    it = iter(got)
    ok = len(want) > 0 and all(c in it for c in want)
print("pdf_magic=1,pdf_text_ok=%d,pdf_chars=%d,svg_chars=%d" % (1 if ok else 0, len(got), len(want)))
PY
}

png_fields() {
  python3 - "$1" <<'PY'
import struct, sys
with open(sys.argv[1], "rb") as fh:
    head = fh.read(26)
if head[:8] != b"\x89PNG\r\n\x1a\n" or head[12:16] != b"IHDR":
    print("png_w=0,png_h=0,png_color_type=na")
else:
    w, h = struct.unpack(">II", head[16:24])
    print("png_w=%d,png_h=%d,png_color_type=%d" % (w, h, head[25]))
PY
}

letterbox_args() {
  python3 - "$1" "$FRAME" <<'PY'
import math, sys
import xml.etree.ElementTree as ET
root = ET.parse(sys.argv[1]).getroot()
s = int(sys.argv[2])
w = float(root.get("width")[:-2])
h = float(root.get("height")[:-2])
k = s * 0.94 / max(w, h)
sw = max(1, math.floor(w * k + 0.5))
sh = max(1, math.floor(h * k + 0.5))
print(sw, sh, (s - sw) // 2, (s - sh) // 2)
PY
}

field() { printf '%s' "$2" | tr ',' '\n' | sed -n "s/^$1=//p" | head -n 1; }

tracer_input() {
  local name=$1 src=$2 expect=$3 pdfmode=$4 job so se rc vf sf pf pngf dims sw sh left top result
  disk_guard
  job=$(new_job)
  cp "$src" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  so="$WORK/$name.dxf.out"; se="$WORK/$name.dxf.err"
  rc=0
  MOUNTS=()
  run_confined "$DXF_RO" "$job" "$so" "$se" /usr/local/bin/cad-dxf --max-drawn-entities "$MAX_DRAWN" --texts /job/draw.texts.json \
    --render-svg /job/draw.render.svg /job/in.dxf /job/draw.svg || rc=$?
  result=fail
  if probe_refused "$se"; then
    vf="verdict=none,reason=probe_refused"; sf="svg=none"
  elif [ ! -s "$job/draw.svg" ]; then
    vf=$(verdict_fields "$so"); sf="svg=none"
    tail -n 8 "$se" >&2
  else
    vf=$(verdict_fields "$so")
    sf=$(svg_fields "$job/draw.svg" "$expect" "$WORK/$name.texts")
    if [ "$rc" = 0 ] && [ "$(field verdict "$vf")" = ok ] && [ "$(field elements_ok "$sf")" = 1 ] && [ -s "$job/draw.render.svg" ] \
       && [ "$(field attrs_ok "$sf")" = 1 ] && [ "$(field text_ok "$sf")" = 1 ] \
       && [ "$(field verdict_texts "$vf")" = "$(field svg_texts "$sf")" ]; then
      result=ok
    fi
  fi
  [ "$result" = ok ] || OK=0
  printf 'TRACER,stage=cad-dxf,input=%s,sha256=%s,rc=%s,confined=1,%s,%s,svg_bytes=%s,result=%s\n' \
    "$name" "$(sha_of "$src")" "$rc" "$vf" "$sf" "$( [ -f "$job/draw.svg" ] && wc -c < "$job/draw.svg" | tr -d ' ' || echo 0 )" "$result" >> "$LINES"
  if [ "$result" != ok ]; then
    printf 'TRACER,stage=rsvg-pdf,input=%s,result=fail,reason=no_svg\n' "$name" >> "$LINES"
    printf 'TRACER,stage=rsvg-png,input=%s,result=fail,reason=no_svg\n' "$name" >> "$LINES"
    OK=0
    rm -rf "$job"
    return 0
  fi
  cp "$job/draw.render.svg" "$WORK/$name.svg"

  so="$WORK/$name.pdf.out"; se="$WORK/$name.pdf.err"
  rc=0
  run_confined "$RSVG_RO" "$job" "$so" "$se" /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/draw.render.svg || rc=$?
  result=fail
  if probe_refused "$se"; then
    pf="pdf=probe_refused"
  elif [ ! -s "$job/out.pdf" ]; then
    pf="pdf=none"
    tail -n 8 "$se" >&2
  else
    pf=$(pdf_fields "$job/out.pdf" "$WORK/$name.texts" "$pdfmode")
    if [ "$rc" = 0 ] && [ "$(field pdf_text_ok "$pf")" = 1 ]; then result=ok; fi
  fi
  [ "$result" = ok ] || OK=0
  printf 'TRACER,stage=rsvg-pdf,input=%s,rc=%s,confined=1,%s,check=%s,pdf_bytes=%s,stderr_lines=%s,result=%s\n' \
    "$name" "$rc" "$pf" "$pdfmode" "$( [ -f "$job/out.pdf" ] && wc -c < "$job/out.pdf" | tr -d ' ' || echo 0 )" \
    "$(wc -l < "$se" | tr -d ' ')" "$result" >> "$LINES"

  read -r sw sh left top <<< "$(letterbox_args "$job/draw.render.svg")"
  so="$WORK/$name.png.out"; se="$WORK/$name.png.err"
  rc=0
  run_confined "$RSVG_RO" "$job" "$so" "$se" /usr/bin/rsvg-convert -b white -w "$sw" -h "$sh" \
    --page-width "$FRAME" --page-height "$FRAME" --left "$left" --top "$top" -o /job/out.png /job/draw.render.svg || rc=$?
  result=fail
  if probe_refused "$se"; then
    pngf="png=probe_refused"
  elif [ ! -s "$job/out.png" ]; then
    pngf="png=none"
    tail -n 8 "$se" >&2
  else
    pngf=$(png_fields "$job/out.png")
    if [ "$rc" = 0 ] && [ "$(field png_w "$pngf")" = "$FRAME" ] && [ "$(field png_h "$pngf")" = "$FRAME" ]; then result=ok; fi
  fi
  [ "$result" = ok ] || OK=0
  printf 'TRACER,stage=rsvg-png,input=%s,rc=%s,confined=1,frame=%s,scaled=%sx%s,offset=%s+%s,%s,png_bytes=%s,result=%s\n' \
    "$name" "$rc" "$FRAME" "$sw" "$sh" "$left" "$top" "$pngf" \
    "$( [ -f "$job/out.png" ] && wc -c < "$job/out.png" | tr -d ' ' || echo 0 )" "$result" >> "$LINES"
  rm -rf "$job"
}

tracer_negatives() {
  local job ctl so se rc crc control state verdict_ok result
  job=$(new_job); ctl=$(new_job)
  MOUNTS=(-v "$CORPUS/synthetic/legacy_cp1251.dxf:/outside/in.dxf:ro")
  crc=0
  run_unconfined "$ctl" "$WORK/ctl1.out" "$WORK/ctl1.err" /usr/local/bin/cad-dxf --max-drawn-entities "$MAX_DRAWN" --texts /job/draw.texts.json \
    --render-svg /job/draw.render.svg /outside/in.dxf /job/draw.svg || crc=$?
  if [ "$crc" = 0 ] && [ -s "$ctl/draw.svg" ]; then control=unconfined_ok; else control=unconfined_failed; fi
  rc=0
  run_confined "$DXF_RO" "$job" "$WORK/neg1.out" "$WORK/neg1.err" /usr/local/bin/cad-dxf --max-drawn-entities "$MAX_DRAWN" --texts /job/draw.texts.json \
    --render-svg /job/draw.render.svg /outside/in.dxf /job/draw.svg || rc=$?
  verdict_ok=$(grep -ac '"verdict":"ok"' "$WORK/neg1.out" || true)
  if [ -s "$job/draw.svg" ]; then state=present; else state=absent; fi
  result=fail
  if probe_refused "$WORK/neg1.err"; then
    state=probe_refused
  elif [ "$state" = absent ] && [ "$verdict_ok" = 0 ] && [ "$rc" != 0 ] && [ "$control" = unconfined_ok ]; then
    result=denied
  fi
  [ "$result" = denied ] || OK=0
  printf 'TRACER,stage=cad-dxf,input=legacy_cp1251_outside,rc=%s,confined=1,%s,svg=%s,verdict_ok_lines=%s,control=%s,control_rc=%s,result=%s\n' \
    "$rc" "$(verdict_fields "$WORK/neg1.out")" "$state" "$verdict_ok" "$control" "$crc" "$result" >> "$LINES"
  rm -rf "$job" "$ctl"

  job=$(new_job); ctl=$(new_job)
  MOUNTS=(-v "$WORK/legacy_cp1251.svg:/outside/draw.svg:ro")
  crc=0
  run_unconfined "$ctl" "$WORK/ctl2.out" "$WORK/ctl2.err" /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /outside/draw.svg || crc=$?
  if [ "$crc" = 0 ] && [ -s "$ctl/out.pdf" ]; then control=unconfined_ok; else control=unconfined_failed; fi
  rc=0
  run_confined "$RSVG_RO" "$job" "$WORK/neg2.out" "$WORK/neg2.err" /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /outside/draw.svg || rc=$?
  if [ -s "$job/out.pdf" ]; then state=present; else state=absent; fi
  result=fail
  if probe_refused "$WORK/neg2.err"; then
    state=probe_refused
  elif [ "$state" = absent ] && [ "$rc" != 0 ] && [ "$control" = unconfined_ok ]; then
    result=denied
  fi
  [ "$result" = denied ] || OK=0
  printf 'TRACER,stage=rsvg-pdf,input=legacy_cp1251_svg_outside,rc=%s,confined=1,pdf=%s,control=%s,control_rc=%s,stderr=%s,result=%s\n' \
    "$rc" "$state" "$control" "$crc" "$(head -n 1 "$WORK/neg2.err" | tr -c 'A-Za-z0-9 ._/:-' '_' | cut -c1-120)" "$result" >> "$LINES"
  rm -rf "$job" "$ctl"
  MOUNTS=()
}

tracer() {
  need_tool pdftotext
  setup tracer
  OK=1
  MOUNTS=()
  tracer_input legacy_cp1251 "$CORPUS/synthetic/legacy_cp1251.dxf" "exact:Деталь" substring
  tracer_input uncommon "$CORPUS/public/uncommon.dxf" "min:40" subsequence
  if [ -s "$WORK/legacy_cp1251.svg" ]; then
    tracer_negatives
  else
    printf 'TRACER,stage=negative,result=fail,reason=no_tracer_svg\n' >> "$LINES"
    OK=0
  fi
  local runs_ok denied
  runs_ok=$(grep -c '^TRACER,.*result=ok$' "$LINES" || true)
  denied=$(grep -c '^TRACER,.*result=denied$' "$LINES" || true)
  [ "$runs_ok" = 6 ] && [ "$denied" = 2 ] || OK=0
  printf 'SUMMARY,mode=tracer,runs_ok=%s,runs_expected=6,denied=%s,denied_expected=2,result=%s\n' \
    "$runs_ok" "$denied" "$( [ "$OK" = 1 ] && echo ok || echo fail )" >> "$LINES"
  evid_put "$LINES" '^HDR,mode=tracer,|^LOCK,|^TRACER,|^TRACER-OK$|^SUMMARY,mode=tracer,'
  if [ "$OK" = 1 ]; then
    echo TRACER-OK >> "$EVID"
  fi
  cat "$LINES"
  if [ "$OK" = 1 ]; then
    echo TRACER-OK
    return 0
  fi
  echo TRACER-FAILED >&2
  return 1
}

generate_fixtures() {
  local attempt=1 gen rc
  while [ "$attempt" -le 3 ]; do
    gen=$(mktemp -d "$WORK/gen.XXXXXX")
    chmod 0777 "$gen"
    rc=0
    docker run --rm "${LIMITS[@]}" -e HOME=/tmp -e PYTHONHASHSEED=0 -v "$REPO_ROOT:/src:ro" -v "$gen:/out" -w /src \
      --entrypoint /opt/venv/bin/python "$TAG" -B scripts/cad-corpus-gen.py --dxf-fixtures --out-dir /out \
      > "$WORK/gen.log" 2>&1 </dev/null || rc=$?
    if [ "$rc" = 0 ] && [ "$(find "$gen" -maxdepth 1 -name '*.dxf' | wc -l | tr -d ' ')" = "$FIXTURE_COUNT" ]; then
      printf '%s\n' "$gen"
      return 0
    fi
    tail -n 5 "$WORK/gen.log" >&2
    attempt=$((attempt + 1))
  done
  return 1
}

read_fixtures() {
  docker run --rm -i "${LIMITS[@]}" -e HOME=/tmp -v "$1:/fx:ro" --entrypoint /opt/venv/bin/python "$TAG" -B - /fx <<'PY'
import os, sys
sys.stdout = sys.stderr
import ezdxf
d = sys.argv[1]
for name in sorted(os.listdir(d)):
    if not name.endswith(".dxf"):
        continue
    strict, codes, count = "ok", "na", "na"
    try:
        doc = ezdxf.readfile(os.path.join(d, name))
        auditor = doc.audit()
        codes = "+".join(str(c) for c in sorted({int(e.code) for e in auditor.errors})) or "none"
        count = str(len(doc.modelspace()))
    except Exception as exc:
        strict = exc.__class__.__name__
    sys.__stdout__.write("%s %s %s %s\n" % (name[:-4], strict, codes, count))
PY
}

fixture_expectation() {
  case "$1" in
    cyclic_*) echo audit_104 ;;
    text_cases|text_illegal_chars|layers_off_frozen|all_hidden|never_resolved|wide|tall|far_point|single_point|xline_ray|text_alpha|text_alpha_faint|units_*|header_far_extents|wrong_extents) echo strict_clean ;;
    *) echo fact ;;
  esac
}

fixtures() {
  setup fixtures
  local gen name strict codes count expect expect_ok same total=0 identical=0 failed=0 ok=1 committed
  gen=$(generate_fixtures) || { printf 'SUMMARY,mode=fixtures,fixtures=0,identical=0,strict_expectations_failed=0,result=fail,reason=generation_failed\n' >> "$LINES"; ok=0; gen=""; }
  if [ -n "$gen" ]; then
    if [ "$WRITE" = 1 ]; then
      mkdir -p "$FIXTURE_DIR"
      find "$FIXTURE_DIR" -maxdepth 1 -name '*.dxf' -exec rm -f {} +
      cp "$gen"/*.dxf "$FIXTURE_DIR/"
    fi
    committed=$(find "$FIXTURE_DIR" -maxdepth 1 -name '*.dxf' 2>/dev/null | wc -l | tr -d ' ')
    [ "$committed" = "$FIXTURE_COUNT" ] || ok=0
    while read -r name strict codes count; do
      total=$((total + 1))
      same=0
      if [ -f "$FIXTURE_DIR/$name.dxf" ] && cmp -s "$gen/$name.dxf" "$FIXTURE_DIR/$name.dxf"; then
        same=1
        identical=$((identical + 1))
      fi
      expect=$(fixture_expectation "$name")
      case "$expect" in
        strict_clean) if [ "$strict" = ok ] && [ "$codes" = none ]; then expect_ok=1; else expect_ok=0; fi ;;
        audit_104) if printf '+%s+' "$codes" | grep -q '+104+'; then expect_ok=1; else expect_ok=0; fi ;;
        *) expect_ok=na ;;
      esac
      [ "$expect_ok" != 0 ] || failed=$((failed + 1))
      printf 'FIXTURE,name=%s,sha256=%s,bytes=%s,identical=%s,strict=%s,audit_codes=%s,msp_entities=%s,expect=%s,expect_ok=%s\n' \
        "$name" "$(sha_of "$gen/$name.dxf")" "$(wc -c < "$gen/$name.dxf" | tr -d ' ')" "$same" "$strict" "$codes" "$count" \
        "$expect" "$expect_ok" >> "$LINES"
    done < <(read_fixtures "$gen")
    [ "$total" = "$FIXTURE_COUNT" ] && [ "$identical" = "$FIXTURE_COUNT" ] && [ "$failed" = 0 ] || ok=0
    printf 'SUMMARY,mode=fixtures,fixtures=%s,committed=%s,identical=%s,strict_expectations_failed=%s,write=%s,result=%s\n' \
      "$total" "$committed" "$identical" "$failed" "$WRITE" "$( [ "$ok" = 1 ] && echo ok || echo fail )" >> "$LINES"
  fi
  evid_put "$LINES" '^HDR,mode=fixtures,|^LOCK,|^FIXTURE,|^SUMMARY,mode=fixtures,'
  cat "$LINES"
  [ "$ok" = 1 ]
}

write_judge() {
  cat > "$WORK/judge.py" <<'PY'
import json
import os
import sys
import xml.etree.ElementTree as ET

NS = "{http://www.w3.org/2000/svg}"
DRAFT_KEYS = {"format", "declared_unit", "declared_unit_raw", "applied_factor", "bbox", "solids",
              "closed_solids", "volume", "area", "null_reason", "triangles"}
EXIT = {"ok": 0, "failed": 1, "unreadable": 2, "empty": 3, "too_complex": 4}
SPECIAL = "A&B <x> \"q\" 'a' & <script>alert(1)</script>"
WIDE = "Lat\u4e2d\u6587\u03a9\U0001F600end"
UNITS = {"units_1": ("inch", ""), "units_2": ("foot", ""), "units_4": ("mm", ""), "units_5": ("cm", ""),
         "units_6": ("m", ""), "units_13": ("micrometre", ""), "units_0": ("", ""), "units_missing": ("", ""),
         "units_3": ("", "3"), "units_21": ("", "21")}

case, rc, stdout_path, job = sys.argv[1:5]
extra = dict(a.split("=", 1) for a in sys.argv[5:])
facts = []
checks = []


def fact(k, v):
    facts.append((k, v))


def need(name, cond):
    checks.append((name, bool(cond)))


def g(x):
    return "%.9g" % x


with open(stdout_path, "rb") as fh:
    raw = fh.read().decode("utf-8", "replace")
lines = [ln for ln in raw.split("\n") if ln.strip()]
vlines = [ln for ln in lines if ln.startswith("CADVERDICT ")]
v = {}
if vlines:
    try:
        v = json.loads(vlines[-1][len("CADVERDICT "):])
    except ValueError:
        v = {}
verdict = v.get("verdict", "none") if v.get("stage", "dxf") == "dxf" else "wrong_stage"
reason = v.get("reason") or "none"
last_is_verdict = 1 if lines and lines[-1].startswith("CADVERDICT ") and v else 0
fact("last_line_verdict", last_is_verdict)
if extra.get("log") != "1" and not case.startswith("usage_"):
    need("last_line_verdict", last_is_verdict)
if case.startswith("usage_"):
    need("rc_64", rc == "64")
    need("no_verdict", not vlines)
else:
    need("rc_matches_verdict", verdict in EXIT and str(EXIT[verdict]) == rc)
if v:
    fact("skipped", "+".join("%s-%s" % kv for kv in sorted(v.get("skipped", {}).items())) or "none")
    fact("entities_drawn", v.get("entities_drawn"))
    fact("auditor_fixes", v.get("auditor_fixes"))
    fact("verdict_texts", v.get("texts"))
    fact("uncovered", v.get("uncovered"))
    fact("render_substituted", v.get("render_substituted"))
    fact("hatch_truncated", v.get("hatch_truncated"))
    fact("page_mm", "%sx%s" % (v.get("page_w_mm"), v.get("page_h_mm")))

svg_path = os.path.join(job, "out.svg")
draft_path = os.path.join(job, "out.measure.json")
texts_path = os.path.join(job, "out.texts.json")
root = None
svg_texts = []
entries = []
draft = None
if verdict == "ok":
    try:
        root = ET.parse(svg_path).getroot()
        fact("svg_parse", "strict_ok")
    except Exception as exc:
        fact("svg_parse", exc.__class__.__name__)
    need("svg_parses", root is not None)
    if root is not None:
        svg_texts = [el.text or "" for el in root.iter(NS + "text")]
        fact("svg_texts", len(svg_texts))
        need("svg_texts_equal_verdict", len(svg_texts) == v.get("texts"))
        try:
            pw = float(root.get("width")[:-2])
            ph = float(root.get("height")[:-2])
            page_ok = pw == v.get("page_w_mm") and ph == v.get("page_h_mm")
        except (TypeError, ValueError):
            page_ok = False
        fact("page_equals_svg_root", int(page_ok))
        need("page_equals_svg_root", page_ok)
    tj = None
    try:
        with open(texts_path, "rb") as fh:
            blob = fh.read()
        ascii_ok = all(b < 128 for b in blob)
        tj = json.loads(blob.decode("ascii"))
    except Exception as exc:
        ascii_ok = False
        fact("texts_json", exc.__class__.__name__)
    shape_ok = (isinstance(tj, dict) and set(tj) == {"texts"} and isinstance(tj["texts"], list)
                and all(isinstance(e, dict) and set(e) - {"invisible"} == {"text", "covered"} and e.get("invisible", True) is True
                        and isinstance(e["text"], str) and isinstance(e["covered"], str) for e in tj["texts"]))
    if shape_ok:
        entries = tj["texts"]

    def subseq(sub, full):
        it = iter(full)
        return all(c in it for c in sub)

    order_ok = shape_ok and [e["text"] for e in entries] == svg_texts and len(entries) == v.get("texts")
    covered_ok = shape_ok and all(subseq(e["covered"], e["text"]) for e in entries)
    uncovered_ok = shape_ok and v.get("uncovered") == sum(len(e["text"]) - len(e["covered"]) for e in entries)
    fact("texts_json_ascii", int(ascii_ok))
    fact("texts_json_equals_svg", int(order_ok))
    fact("covered_subsequence", int(covered_ok))
    need("texts_json_ascii", ascii_ok)
    need("texts_json_equals_svg", order_ok)
    need("covered_subsequence", covered_ok)
    need("uncovered_sum", uncovered_ok)

    def rendered(e):
        out, rest = [], list(e["covered"])
        for c in e["text"]:
            if rest and c == rest[0]:
                out.append(c)
                rest.pop(0)
            else:
                out.append("\ufffd")
        return "".join(out)

    render_root = None
    try:
        render_root = ET.parse(os.path.join(job, "out.render.svg")).getroot()
    except Exception as exc:
        fact("render_svg", exc.__class__.__name__)
    render_texts = [el.text or "" for el in render_root.iter(NS + "text")] if render_root is not None else None
    expected_render = [rendered(e) for e in entries]
    structure_ok = False
    if render_root is not None and root is not None:
        a = [(el.tag, sorted(el.attrib.items())) for el in root.iter()]
        b = [(el.tag, sorted(el.attrib.items())) for el in render_root.iter()]
        structure_ok = a == b
    subst = sum(1 for e in entries for x, y in zip(e["text"], rendered(e)) if x != y)
    render_ok = render_texts == expected_render
    fact("render_svg_texts_ok", int(render_ok))
    fact("render_svg_structure_equal", int(structure_ok))
    need("render_svg_texts_ok", render_ok)
    need("render_svg_structure_equal", structure_ok)
    need("render_substituted_count", v.get("render_substituted") == subst == v.get("uncovered"))
    if extra.get("measure") == "1":
        try:
            with open(draft_path, "rb") as fh:
                draft = json.loads(fh.read().decode("ascii"))
        except Exception as exc:
            fact("draft", exc.__class__.__name__)
        need("draft_present", draft is not None)
elif not extra.get("keep_outputs"):
    absent = not any(os.path.exists(os.path.join(job, n)) for n in ("out.svg", "out.render.svg", "out.measure.json", "out.texts.json"))
    fact("outputs_absent", int(absent))
    need("outputs_absent", absent)

bbox = None
if draft is not None:
    b = draft.get("bbox") if isinstance(draft.get("bbox"), dict) else {}
    ok = (set(draft) == DRAFT_KEYS and draft["format"] == "dxf" and draft["applied_factor"] == 1
          and draft["solids"] is None and draft["closed_solids"] is None and draft["volume"] is None
          and draft["area"] is None and draft["null_reason"] == "not_applicable_2d" and draft["triangles"] == 0
          and set(b) == {"min", "max"} and len(b.get("min", [])) == 2 and len(b.get("max", [])) == 2
          and isinstance(draft["declared_unit"], str) and isinstance(draft["declared_unit_raw"], str))
    fact("draft_contract", int(ok))
    need("draft_contract", ok)
    if ok:
        bbox = (b["min"][0], b["min"][1], b["max"][0], b["max"][1])
        fact("bbox", "+".join(g(x) for x in bbox))
        fact("declared_unit", draft["declared_unit"] or "empty")
        fact("declared_unit_raw", draft["declared_unit_raw"] or "empty")


def expect(vd, rs=""):
    need("verdict_%s" % vd, verdict == vd)
    if rs:
        need("reason_%s" % rs, reason == rs)


def near(box, want, tol):
    return box is not None and all(abs(a - b) <= tol for a, b in zip(box, want))


skipped = v.get("skipped", {})
pw, ph = v.get("page_w_mm") or 0.0, v.get("page_h_mm") or 0.0
if case == "layers_off_frozen":
    expect("ok")
    blob = open(svg_path, "rb").read() if os.path.exists(svg_path) else b""
    need("only_visible_text", svg_texts == ["VISIBLE"])
    need("hidden_absent", b"HIDDEN_OFF" not in blob and b"HIDDEN_FROZEN" not in blob)
    fact("text_list", "+".join(svg_texts) or "none")
elif case in ("all_hidden",):
    expect("empty", "nothing_drawable")
elif case == "never_resolved":
    expect("ok")
    need("skipped_exact", skipped == {"IMAGE": 2, "PDFUNDERLAY": 1, "XREF": 1, "OLE2FRAME": 1})
    other = extra.get("compare", "")
    import hashlib
    a = hashlib.sha256(open(svg_path, "rb").read()).hexdigest() if os.path.exists(svg_path) else "none"
    bsha = hashlib.sha256(open(other, "rb").read()).hexdigest() if other and os.path.exists(other) else "none"
    fact("svg_sha256_confined", a)
    fact("svg_sha256_unconfined", bsha)
    need("confined_equals_unconfined", a != "none" and a == bsha)
elif case == "3dsolids":
    expect("empty", "nothing_drawable")
    need("skipped_3dsolid_4", skipped == {"3DSOLID": 4})
elif case == "uncommon":
    expect("ok")
    need("skipped_3dsolid_1", skipped.get("3DSOLID") == 1)
    need("min_x_above_21", bbox is not None and bbox[0] > 21.0)
elif case in ("wrong_extents", "header_far_extents"):
    expect("ok")
    need("bbox_from_line", near(bbox, (10.0, 20.0, 40.0, 30.0), 1e-9))
elif case == "xline_ray":
    expect("ok")
    need("bbox_rectangle", near(bbox, (0.0, 0.0, 100.0, 50.0), 1e-9))
    inner = (min(pw, ph) - 20.0) / (max(pw, ph) - 20.0) if max(pw, ph) > 20.0 else 0.0
    fact("inner_page_aspect", g(inner))
    need("page_aspect", abs(inner - 0.5) <= 1e-3)
elif case == "far_point":
    expect("ok")
    need("far_point_in_bbox", bbox is not None and bbox[2] >= 1e5)
elif case == "single_point":
    expect("empty", "zero_extent")
elif case == "wide":
    expect("ok")
    need("page_wide", pw == 297.0 and ph < 30.0)
elif case == "tall":
    expect("ok")
    need("page_tall", ph == 297.0 and pw < 30.0)
elif case in UNITS:
    expect("ok")
    unit, raw_code = UNITS[case]
    need("unit_mapping", draft is not None and draft.get("declared_unit") == unit and draft.get("declared_unit_raw") == raw_code)
elif case == "text_round_trip":
    expect("ok")
    need("special_exact", SPECIAL in svg_texts)
    need("attrib_exact", "Attr<&>" in svg_texts)
    fact("special_present", int(SPECIAL in svg_texts))
    fact("attrib_present", int("Attr<&>" in svg_texts))
elif case == "text_coverage":
    expect("ok")
    maps = dict(kv.split(":") for kv in extra.get("maps", "").split("+") if kv)
    wide = [e for e in entries if e["text"] == WIDE]
    want = "".join(c for c in WIDE if maps.get("%x" % ord(c), "1") == "1")
    lacking = "".join(c for c in WIDE if maps.get("%x" % ord(c), "1") == "0")
    fact("dejavu_maps", "+".join("%s:%s" % kv for kv in sorted(maps.items())) or "none")
    fact("wide_covered", wide[0]["covered"].encode("unicode_escape").decode("ascii") if wide else "none")
    need("maps_probed", set(maps) == {"4e2d", "6587", "3a9", "1f600"})
    need("wide_text_present", len(wide) == 1)
    need("wide_covered_equals_font", bool(wide) and wide[0]["covered"] == want)
    need("cjk_uncovered", bool(wide) and "\u4e2d" not in wide[0]["covered"] and "\u6587" not in wide[0]["covered"])
    need("uncovered_at_least_lacking", (v.get("uncovered") or 0) >= len(lacking) >= 2)
    fact("plan_premise_1f600_uncovered", "false" if maps.get("1f600") == "1" else "true")
elif case == "text_illegal_chars":
    expect("ok")
    need("replacement_chars", svg_texts == ["AB\ufffdCD\ufffdEF"])
    fact("text_escaped", "+".join(t.encode("unicode_escape").decode("ascii") for t in svg_texts) or "none")
elif case == "font_fresh_cache":
    expect("failed", "font_unavailable")
elif case == "font_warm_cache":
    expect("failed", "font_unavailable")
    fact("cache_warm_before_run", extra.get("warm", "0"))
    need("cache_warm_before_run", extra.get("warm") == "1")
elif case == "drawn_entities_over_cap":
    expect("too_complex", "drawn_entities_over_cap")
elif case == "block_cycle":
    expect("too_complex", "block_cycle")
elif case == "recursion":
    expect("too_complex", "recursion")
elif case == "nonnumeric_inf_unguarded":
    fact("expected_without_guard", "failed/draw_exception")
    fact("matches_expected", int(verdict == "failed" and reason == "draw_exception"))
elif case == "hatch_pattern_default":
    expect("ok")
    need("hatch_truncated_0", v.get("hatch_truncated") == 0)
elif case == "hatch_timeout_signal":
    need("hatch_truncated_counted", (v.get("hatch_truncated") or 0) >= 1)
elif case in ("usage_cap_zero", "usage_no_texts"):
    pass
elif case == "corpus":
    name = extra.get("file", "")
    if name == "3dsolids":
        expect("empty", "nothing_drawable")
    else:
        expect("ok")
    need("auditor_fixes_0", v.get("auditor_fixes") == 0)
    if name == "legacy_cp1251":
        need("text_exact", svg_texts == ["\u0414\u0435\u0442\u0430\u043b\u044c"])
    if name == "legacy_latin1":
        need("text_exact", svg_texts == ["B\u00fcgel \u00d85"])
    if name in ("legacy_cp1251", "legacy_latin1"):
        fact("text_list", "+".join(t.encode("unicode_escape").decode("ascii") for t in svg_texts) or "none")
    if "prov_sha" in extra:
        fact("provenance_sha256_match", int(extra["prov_sha"] == extra.get("sha")))
        need("provenance_sha256_match", extra["prov_sha"] == extra.get("sha"))
else:
    need("known_case", False)

failed = [n for n, ok in checks if not ok]
fact("checks", len(checks))
fact("failed_checks", "+".join(failed) or "none")
out = ["verdict=%s" % verdict, "reason=%s" % reason, "rc=%s" % rc]
for k, val in facts:
    out.append("%s=%s" % (k, str(val).replace(",", "_").replace("\n", "_")))
if case == "nonnumeric_inf_unguarded":
    out.append("result=%s" % ("fact" if not failed else "fail"))
else:
    out.append("result=%s" % ("ok" if not failed else "fail"))
print(",".join(out))
PY
}

helper_exec() {
  local ro=$1 job=$2 name=$3 rc=0
  shift 3
  MOUNTS=()
  run_confined "$ro" "$job" "$WORK/$name.out" "$WORK/$name.err" /usr/local/bin/cad-dxf "$@" || rc=$?
  printf '%s\n' "$rc"
}

helper_unconfined() {
  local job=$1 name=$2 rc=0
  shift 2
  MOUNTS=()
  run_unconfined "$job" "$WORK/$name.out" "$WORK/$name.err" /usr/local/bin/cad-dxf "$@" || rc=$?
  printf '%s\n' "$rc"
}

std_args() {
  printf '%s\n' --max-drawn-entities "${1:-$MAX_DRAWN}" --texts /job/out.texts.json --render-svg /job/out.render.svg \
    --measure /job/out.measure.json /job/in.dxf /job/out.svg
}

write_harness() {
  mkdir -p "$WORK/wrap"
  chmod 0755 "$WORK/wrap"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
  cat > "$WORK/wrap/harness.py" <<'PY'
import importlib.machinery
import importlib.util
import os
import sys

loader = importlib.machinery.SourceFileLoader("cad_dxf", "/usr/local/bin/cad-dxf")
spec = importlib.util.spec_from_loader("cad_dxf", loader)
mod = importlib.util.module_from_spec(spec)
loader.exec_module(mod)
setting = sys.argv[1]
if setting == "hatch0":
    mod.HATCH_TIMEOUT_S = 0.0
elif setting == "outline":
    mod.TEXT_AS_OUTLINE = True
elif setting == "outline_lines":
    from ezdxf.addons.drawing import pipeline, recorder
    mod.TEXT_AS_OUTLINE = True
    outdir = sys.argv[2]
    rest = sys.argv[3:]
    state = {"i": -1, "want": -1, "on": False}
    original_text = pipeline.RenderPipeline2d.draw_text
    original_store = recorder.Recorder.store

    def draw_text(self, *args, **kwargs):
        state["i"] += 1
        if state["i"] == state["want"]:
            state["on"] = True
            try:
                original_text(self, *args, **kwargs)
            finally:
                state["on"] = False

    def store(self, record, properties):
        if state["on"]:
            original_store(self, record, properties)

    pipeline.RenderPipeline2d.draw_text = draw_text
    recorder.Recorder.store = store
    sys.argv = ["cad-dxf"] + rest
    rc = mod.main()
    calls = state["i"] + 1
    for want in range(calls):
        state["i"] = -1
        state["want"] = want
        sys.argv = ["cad-dxf"] + rest[:-1] + [os.path.join(outdir, "line_%03d.svg" % want)]
        rc = rc or mod.main()
    with open(os.path.join(outdir, "lines.count"), "w") as fh:
        fh.write("%d\n" % calls)
    sys.exit(rc)
else:
    raise SystemExit("unknown setting " + setting)
sys.argv = ["cad-dxf"] + sys.argv[2:]
sys.exit(mod.main())
PY
  chmod 0644 "$WORK/wrap/"*.py
}

harness_run() {
  local job=$1 name=$2 rc=0
  shift 2
  docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -v "$job:/job" -v "$WORK/wrap:/wrap:ro" \
    --entrypoint /opt/venv/bin/python "$TAG" -I -B /wrap/harness.py "$@" \
    > "$WORK/$name.out" 2> "$WORK/$name.err" </dev/null || rc=$?
  printf '%s\n' "$rc"
}

job_with() {
  local job
  job=$(new_job)
  cp "$1" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  printf '%s\n' "$job"
}

helper_row() {
  local group=$1 name=$2 input=$3 src=$4 rc=$5 job=$6 j
  shift 6
  if probe_refused "$WORK/$name.err"; then
    j="verdict=none,reason=probe_refused,rc=$rc,result=fail"
  else
    j=$(python3 "$WORK/judge.py" "$name" "$rc" "$WORK/$name.out" "$job" "$@")
  fi
  printf 'HELPER,group=%s,case=%s,input=%s,sha256=%s,%s\n' "$group" "$name" "$input" "$(sha_of "$src")" "$j" >> "$LINES"
  case "$j" in *,result=fail) tail -n 5 "$WORK/$name.err" >&2 || true ;; esac
}

sem_case() {
  local name=$1 rel=$2 job rc args
  job=$(job_with "$CORPUS/$rel")
  args=$(std_args)
  rc=$(helper_exec "$DXF_RO" "$job" "$name" $args)
  helper_row semantics "$name" "$rel" "$CORPUS/$rel" "$rc" "$job" measure=1
  rm -rf "$job"
}

group_semantics() {
  local c job job2 rc rc2 args
  disk_guard
  for c in layers_off_frozen all_hidden wrong_extents header_far_extents xline_ray far_point single_point wide tall \
    units_0 units_1 units_2 units_3 units_4 units_5 units_6 units_13 units_21 units_missing; do
    sem_case "$c" "dxf/$c.dxf"
  done
  sem_case 3dsolids public/3dsolids.dxf
  sem_case uncommon public/uncommon.dxf
  args=$(std_args)
  job=$(job_with "$CORPUS/dxf/never_resolved.dxf")
  job2=$(job_with "$CORPUS/dxf/never_resolved.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" never_resolved $args)
  rc2=$(helper_unconfined "$job2" never_resolved_unconfined $args)
  helper_row semantics never_resolved dxf/never_resolved.dxf "$CORPUS/dxf/never_resolved.dxf" "$rc" "$job" \
    measure=1 "compare=$job2/out.svg"
  [ "$rc2" = 0 ] || echo "never_resolved unconfined control rc=$rc2" >&2
  rm -rf "$job" "$job2"
}

group_safety() {
  local job rc args maps cache warm gen
  disk_guard
  args=$(std_args)
  job=$(job_with "$CORPUS/dxf/text_cases.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" text_round_trip $args)
  cp "$WORK/text_round_trip.out" "$WORK/text_coverage.out"
  cp "$WORK/text_round_trip.err" "$WORK/text_coverage.err"
  maps=$(docker run --rm --network none --entrypoint /opt/venv/bin/python "$TAG" -I -B -c '
from fontTools.ttLib import TTFont
c = TTFont("/usr/share/fonts/truetype/dejavu/DejaVuSans.ttf").getBestCmap()
print("+".join("%x:%d" % (o, o in c) for o in (0x4e2d, 0x6587, 0x3a9, 0x1f600)))
' </dev/null)
  helper_row safety text_round_trip dxf/text_cases.dxf "$CORPUS/dxf/text_cases.dxf" "$rc" "$job" measure=1
  helper_row safety text_coverage dxf/text_cases.dxf "$CORPUS/dxf/text_cases.dxf" "$rc" "$job" measure=1 "maps=$maps"
  rm -rf "$job"
  printf 'DEVIATION,key=text_coverage_emoji,group=safety,plan_expectation=covered_lacks_U+4E2D_U+6587_U+1F600_and_uncovered_ge_3,observed=DejaVuSans.ttf_cmap_%s,judged=covered_equals_the_font_cmap_and_uncovered_ge_the_unmapped_count\n' "$maps" >> "$LINES"

  job=$(job_with "$CORPUS/dxf/text_illegal_chars.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" text_illegal_chars $args)
  helper_row safety text_illegal_chars dxf/text_illegal_chars.dxf "$CORPUS/dxf/text_illegal_chars.dxf" "$rc" "$job" measure=1
  rm -rf "$job"

  job=$(job_with "$CORPUS/dxf/text_cases.dxf")
  rc=$(helper_exec "$DXF_NOFONT_RO" "$job" font_fresh_cache $args)
  helper_row safety font_fresh_cache dxf/text_cases.dxf "$CORPUS/dxf/text_cases.dxf" "$rc" "$job" measure=1
  rm -rf "$job"

  job=$(job_with "$CORPUS/dxf/text_cases.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" font_warm_up $args)
  cache="$job/.home/.cache/ezdxf/font_manager_cache.json"
  warm=0
  if [ "$rc" = 0 ] && [ -s "$cache" ]; then warm=1; fi
  rm -f "$job/out.svg" "$job/out.render.svg" "$job/out.texts.json" "$job/out.measure.json"
  rc=$(helper_exec "$DXF_NOFONT_RO" "$job" font_warm_cache $args)
  helper_row safety font_warm_cache dxf/text_cases.dxf "$CORPUS/dxf/text_cases.dxf" "$rc" "$job" measure=1 "warm=$warm"
  rm -rf "$job"

  job=$(job_with "$CORPUS/synthetic/blocks_minsert.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" drawn_entities_over_cap $(std_args 3))
  helper_row safety drawn_entities_over_cap synthetic/blocks_minsert.dxf "$CORPUS/synthetic/blocks_minsert.dxf" "$rc" "$job"
  rm -rf "$job"

  job=$(job_with "$CORPUS/dxf/cyclic_self.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" block_cycle $args)
  helper_row safety block_cycle dxf/cyclic_self.dxf "$CORPUS/dxf/cyclic_self.dxf" "$rc" "$job"
  rm -rf "$job"

  gen="$WORK/nest_d250.dxf"
  python3 - "$REPO_ROOT/scripts/cad-corpus-gen.py" "$gen" <<'PY'
import importlib.util
import sys
spec = importlib.util.spec_from_file_location("cad_corpus_gen", sys.argv[1])
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)
with open(sys.argv[2], "wb") as fh:
    fh.write(mod.build_dxf_nest(250))
PY
  job=$(job_with "$gen")
  rc=$(helper_exec "$DXF_RO" "$job" recursion $args)
  helper_row safety recursion generated/nest_d250.dxf "$gen" "$rc" "$job"
  rm -rf "$job"

  job=$(job_with "$CORPUS/dxf/nonnumeric_inf.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" nonnumeric_inf_unguarded $args)
  helper_row safety nonnumeric_inf_unguarded dxf/nonnumeric_inf.dxf "$CORPUS/dxf/nonnumeric_inf.dxf" "$rc" "$job"
  rm -rf "$job"

  job=$(new_job)
  docker run --rm --network none --user nobody -e HOME=/tmp -v "$job:/job" --entrypoint /opt/venv/bin/python "$TAG" -I -B -c '
import ezdxf
d = ezdxf.new("R2018")
h = d.modelspace().add_hatch()
h.set_pattern_fill("ANSI31", scale=0.5)
h.paths.add_polyline_path([(0, 0), (100, 0), (100, 100), (0, 100)], is_closed=True)
d.saveas("/job/gen.dxf")
' </dev/null >&2
  gen="$WORK/pattern_hatch.dxf"
  cp "$job/gen.dxf" "$gen"
  rm -rf "$job"
  job=$(job_with "$gen")
  rc=$(helper_exec "$DXF_RO" "$job" hatch_pattern_default $args)
  helper_row safety hatch_pattern_default generated/pattern_hatch.dxf "$gen" "$rc" "$job" measure=1
  rm -rf "$job"
  job=$(job_with "$gen")
  rc=$(harness_run "$job" hatch_timeout_signal hatch0 $args)
  helper_row safety hatch_timeout_signal generated/pattern_hatch.dxf "$gen" "$rc" "$job" measure=1 keep_outputs=1
  rm -rf "$job"
  printf 'GAP,key=hatch_timeout_partial,group=safety,ezdxf_signal=none_but_a_print,checked=ezdxf_1.4.4_Frontend.draw_hatch_pattern_prints_hatching_timeout_of_to_stdout_and_keeps_the_partial_lines,helper=counts_that_print_per_draw_hatch_pattern_call_into_hatch_truncated,proof=rows_hatch_pattern_default_and_hatch_timeout_signal,corpus_hatches=solid_only\n' >> "$LINES"

  job=$(job_with "$CORPUS/dxf/wide.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" usage_cap_zero $(std_args 0))
  helper_row safety usage_cap_zero dxf/wide.dxf "$CORPUS/dxf/wide.dxf" "$rc" "$job"
  rm -rf "$job"
  job=$(job_with "$CORPUS/dxf/wide.dxf")
  rc=$(helper_exec "$DXF_RO" "$job" usage_no_texts --max-drawn-entities "$MAX_DRAWN" --render-svg /job/out.render.svg /job/in.dxf /job/out.svg)
  helper_row safety usage_no_texts dxf/wide.dxf "$CORPUS/dxf/wide.dxf" "$rc" "$job"
  rm -rf "$job"
}

write_corpus_tools() {
  cat > "$WORK/wrap/compare.py" <<'PY'
import sys
import numpy as np
from PIL import Image


def ink(path):
    a = np.asarray(Image.open(path).convert("RGB"))
    return (a < 160).any(axis=2)


def near(mask, r):
    h, w = mask.shape
    p = np.pad(mask, r)
    out = np.zeros_like(mask)
    for dy in range(-r, r + 1):
        for dx in range(-r, r + 1):
            if dx * dx + dy * dy <= r * r:
                out |= p[r + dy:r + dy + h, r + dx:r + dx + w]
    return out


text = ink(sys.argv[1])
outline = ink(sys.argv[2])
if text.shape != outline.shape:
    print("shape_equal=0,ink_text=na,ink_outline=na,text_in_outline=0,outline_in_text=0")
    sys.exit(0)
nt, no = int(text.sum()), int(outline.sum())
tio = float((outline & near(text, 2)).sum()) / no if no else 0.0
oit = float((text & near(outline, 2)).sum()) / nt if nt else 0.0
print("shape_equal=1,ink_text=%d,ink_outline=%d,text_in_outline=%.4f,outline_in_text=%.4f" % (nt, no, tio, oit))
PY
  cat > "$WORK/wrap/inkbox.py" <<'PY'
import sys
import numpy as np
from PIL import Image
a = np.asarray(Image.open(sys.argv[1]).convert("RGB"))
k = (a < 160).any(axis=2)
ys, xs = np.nonzero(k)
if len(ys) == 0:
    print("none")
else:
    print("%d+%d+%d+%d" % (xs.min(), ys.min(), xs.max(), ys.max()))
PY
  chmod 0644 "$WORK/wrap/"*.py
}

wrap_field() { sed -n "s/.*[,]$1=\([^,]*\).*/\1/p" "$2" | head -n 1; }

prov_sha() {
  awk -F'|' -v t="$1" '
    /^### dxf-replicate from uncommon.dxf/ { on = 1; next }
    /^### / { on = 0 }
    on && $2 ~ "^ *" t " *$" { gsub(/ /, "", $5); print $5 }
  ' "$CORPUS/PROVENANCE.md"
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

corpus_row() {
  local name=$1 input=$2 src=$3 extra=${4:-} job wrapout rc_w=0 j wf result wrc
  disk_guard
  job=$(job_with "$src")
  mkdir -p "$job/wrap"
  chmod 0777 "$job/wrap"
  wrapout="$WORK/corpus_$name.wrap"
  docker run --rm "${LIMITS[@]}" "${STAGE_ENV[@]}" -e "PROBE_RO=$DXF_RO" -e "PROBE_RW=$RW_SET" \
    -v "$WORK/probe:/probe:ro" -v "$WORK/wrap:/wrap:ro" -v "$job:/job" --entrypoint /usr/bin/python3 "$TAG" \
    -I -B /wrap/measure_wrap.py --budget-s 600 --outdir /job/wrap -- \
    /probe/sandbox-probe /usr/local/bin/cad-dxf $(std_args) > "$wrapout" 2> "$WORK/corpus_$name.err" </dev/null || rc_w=$?
  cp "$job/wrap/child.log" "$WORK/corpus_$name.out" 2>/dev/null || : > "$WORK/corpus_$name.out"
  wrc=$(wrap_field rc "$wrapout")
  if probe_refused "$WORK/corpus_$name.out"; then
    j="verdict=none,reason=probe_refused,rc=${wrc:-na},result=fail"
  else
    j=$(python3 "$WORK/judge.py" corpus "${wrc:-na}" "$WORK/corpus_$name.out" "$job" log=1 measure=1 "file=$name" "sha=$(sha_of "$src")" $extra)
  fi
  result=${j##*,result=}
  j=${j%,result=*}
  wf="wall_s=$(wrap_field wall_s "$wrapout"),child_rss_mib=$(wrap_field child_rss_mib "$wrapout"),cg_peak_mib=$(wrap_field cg_peak_mib "$wrapout"),oom_kill=$(wrap_field oom_kill "$wrapout"),timeout=$(wrap_field timeout "$wrapout"),wrap_rc=$rc_w"
  if [ "$(wrap_field timeout "$wrapout")" != 0 ] || [ "$(wrap_field oom_kill "$wrapout")" != 0 ] || [ "$rc_w" != 0 ]; then result=fail; fi
  printf 'CORPUS,group=corpus,file=%s,input=%s,sha256=%s,bytes=%s,%s,%s,result=%s\n' "$name" "$input" "$(sha_of "$src")" \
    "$(wc -c < "$src" | tr -d ' ')" "$j" "$wf" "$result" >> "$LINES"
  [ "$result" != fail ] || tail -n 8 "$WORK/corpus_$name.out" >&2 || true
  if [ -s "$job/out.render.svg" ]; then cp "$job/out.render.svg" "$WORK/fid_$name.svg"; fi
  rm -rf "$job"
}

rsvg_png() {
  local ro=$1 job=$2 svg=$3 png=$4 size=$5 bg=$6 sw sh left top rc=0
  read -r sw sh left top <<< "$(letterbox_size "$job/$svg" "$size")"
  MOUNTS=()
  if [ "$bg" = opaque ]; then
    run_confined "$ro" "$job" "$WORK/rsvg.out" "$WORK/rsvg.err" /usr/bin/rsvg-convert -b white -w "$sw" -h "$sh" \
      --page-width "$size" --page-height "$size" --left "$left" --top "$top" -o "/job/$png" "/job/$svg" || rc=$?
  else
    run_confined "$ro" "$job" "$WORK/rsvg.out" "$WORK/rsvg.err" /usr/bin/rsvg-convert -w "$sw" -h "$sh" \
      --page-width "$size" --page-height "$size" --left "$left" --top "$top" -o "/job/$png" "/job/$svg" || rc=$?
  fi
  printf '%s %s %s %s %s\n' "$rc" "$sw" "$sh" "$left" "$top"
}

letterbox_size() {
  python3 - "$1" "$2" <<'PY'
import math, sys
import xml.etree.ElementTree as ET
root = ET.parse(sys.argv[1]).getroot()
s = int(sys.argv[2])
w = float(root.get("width")[:-2])
h = float(root.get("height")[:-2])
k = s * 0.94 / max(w, h)
sw = max(1, math.floor(w * k + 0.5))
sh = max(1, math.floor(h * k + 0.5))
print(sw, sh, (s - sw) // 2, (s - sh) // 2)
PY
}

in_image() {
  local job=$1
  shift
  docker run --rm --network none --user nobody -e HOME=/tmp -v "$job:/job" -v "$WORK/wrap:/wrap:ro" \
    --entrypoint /opt/venv/bin/python "$TAG" -I -B "$@" </dev/null
}

fidelity_row() {
  local name=$1 src=$2 job rc orc page_equal r1 r2 cmp result tw th ow oh texts uncovered
  job=$(new_job)
  if [ -s "$WORK/fid_$name.svg" ]; then
    cp "$WORK/fid_$name.svg" "$job/text.svg"
  else
    cp "$src" "$job/in.dxf"
    chmod 0644 "$job/in.dxf"
    rc=$(helper_exec "$DXF_RO" "$job" "fid_text_$name" $(std_args))
    [ "$rc" != 0 ] || mv "$job/out.render.svg" "$job/text.svg"
    rm -f "$job/out.svg" "$job/out.texts.json" "$job/out.measure.json"
  fi
  cp "$src" "$job/in.dxf"
  chmod 0644 "$job/in.dxf"
  orc=$(harness_run "$job" "fid_outline_$name" outline $(std_args))
  [ ! -s "$job/out.svg" ] || mv "$job/out.svg" "$job/outline.svg"
  result=fail
  cmp="shape_equal=0,ink_text=na,ink_outline=na,text_in_outline=0,outline_in_text=0"
  page_equal=0
  if [ -s "$job/text.svg" ] && [ -s "$job/outline.svg" ] && [ "$orc" = 0 ]; then
    read -r tw th <<< "$(python3 -c 'import sys, xml.etree.ElementTree as ET; r = ET.parse(sys.argv[1]).getroot(); print(r.get("width"), r.get("height"))' "$job/text.svg")"
    read -r ow oh <<< "$(python3 -c 'import sys, xml.etree.ElementTree as ET; r = ET.parse(sys.argv[1]).getroot(); print(r.get("width"), r.get("height"))' "$job/outline.svg")"
    [ "$tw/$th" != "$ow/$oh" ] || page_equal=1
    read -r r1 _ <<< "$(rsvg_png "$RSVG_RO" "$job" text.svg text.png "$FRAME" opaque)"
    read -r r2 _ <<< "$(rsvg_png "$RSVG_RO" "$job" outline.svg outline.png "$FRAME" opaque)"
    if [ "$r1" = 0 ] && [ "$r2" = 0 ]; then
      cmp=$(in_image "$job" /wrap/compare.py /job/text.png /job/outline.png)
      if [ "$page_equal" = 1 ] && python3 - "$cmp" <<'PY'
import sys
kv = dict(p.split("=", 1) for p in sys.argv[1].split(","))
ok = kv.get("shape_equal") == "1" and float(kv["text_in_outline"]) >= 0.99 and float(kv["outline_in_text"]) >= 0.99
sys.exit(0 if ok else 1)
PY
      then result=ok; fi
    fi
  fi
  texts=$(grep -o '<text' "$job/text.svg" 2>/dev/null | wc -l | tr -d ' ')
  printf 'FIDELITY,group=corpus,file=%s,sha256=%s,size=%s,background=opaque,page_mm=%s,page_equal=%s,outline_rc=%s,svg_texts=%s,%s,threshold=0.99,result=%s\n' \
    "$name" "$(sha_of "$src")" "$FRAME" "${tw:-na}x${th:-na}" "$page_equal" "$orc" "${texts:-0}" "$cmp" "$result" >> "$LINES"
  rm -rf "$job"
}

hexbox_finding() {
  local job a b c
  job=$(new_job)
  python3 - "$job" <<'PY'
import sys
d = sys.argv[1]
cases = {"plain_fs600": (600, 1, "Latend"), "cjk_fs600": (600, 1, "Lat中文end"), "cjk_fs60_x10": (60, 10, "Lat中文end")}
for name, (fs, m, t) in cases.items():
    with open("%s/%s.svg" % (d, name), "w", encoding="utf-8") as fh:
        fh.write('<?xml version="1.0" encoding="utf-8"?><svg xmlns="http://www.w3.org/2000/svg" width="1000px" height="1000px" viewBox="0 0 10000 10000">'
                 '<text transform="matrix(%d 0 0 %d 100 5000)" font-family="DejaVu Sans" font-size="%d" fill="#000000">%s</text></svg>' % (m, m, fs, t))
PY
  MOUNTS=()
  for c in plain_fs600 cjk_fs600 cjk_fs60_x10; do
    run_confined "$RSVG_RO" "$job" "$WORK/hex.out" "$WORK/hex.err" /usr/bin/rsvg-convert -b white -o "/job/$c.png" "/job/$c.svg" || true
  done
  a=$(in_image "$job" /wrap/inkbox.py /job/plain_fs600.png)
  b=$(in_image "$job" /wrap/inkbox.py /job/cjk_fs600.png)
  c=$(in_image "$job" /wrap/inkbox.py /job/cjk_fs60_x10.png)
  printf 'FINDING,key=rsvg_hexbox_baseline,group=corpus,setup=baseline_y_500px_of_1000_scale_0.1,inkbox_x0y0x1y1_plain_fs600=%s,inkbox_cjk_fs600=%s,inkbox_cjk_fs60_matrix10=%s,meaning=a_text_element_holding_characters_outside_DejaVu_is_drawn_by_pango_with_hex_boxes_and_librsvg_2.54.7_moves_the_whole_line_off_its_baseline_unless_the_device_scale_of_the_text_is_1\n' \
    "$a" "$b" "$c" >> "$LINES"
  rm -rf "$job"
}

group_corpus() {
  local f t src
  disk_guard
  write_corpus_tools
  for f in public/3dsolids public/acad_table_with_blk_ref public/columns_R2007 public/dimension_in_nested_blocks \
    public/hatches_1 public/hatches_2 public/houses_of_parliament_georeferenced public/uncommon \
    synthetic/blocks_minsert synthetic/comments_999 synthetic/legacy_cp1251 synthetic/legacy_latin1; do
    corpus_row "${f#*/}" "$f.dxf" "$CORPUS/$f.dxf"
  done
  for t in 2MiB 5MiB 10MiB 20MiB 50MiB; do
    src=$(replicate_src "$t")
    corpus_row "dxf-replicate-uncommon-$t" "scaled/dxf-replicate-uncommon-$t.dxf" "$src" "prov_sha=$(prov_sha "$t")"
  done
  for f in synthetic/legacy_cp1251 synthetic/legacy_latin1 public/columns_R2007 public/dimension_in_nested_blocks \
    public/hatches_2 public/uncommon public/acad_table_with_blk_ref; do
    fidelity_row "${f#*/}" "$CORPUS/$f.dxf"
  done
  write_line_tools
  fidelity_lines text_cases "$CORPUS/dxf/text_cases.dxf"
  printf 'RULE,key=line_width_bound,group=corpus,bound=max(3.5pct_of_outline_ink_width_1.0px),why=both_widths_are_integer_pixel_ink_box_spans_so_a_bound_under_1px_tests_instrument_resolution_not_kerning,source=75-CONTEXT_R-9_addendum\n' >> "$LINES"
  printf 'RULE,key=substituted_line_gate,group=corpus,gated=start_within_1px+covered_prefix_baseline_within_1px+inside_page,recorded_not_gated=width+height,why=U+FFFD_and_ezdxf_glyphs_for_uncovered_characters_differ_in_shape,source=75-CONTEXT_R-9_addendum\n' >> "$LINES"
  printf 'KNOWN_LIMIT,key=uncovered_as_fffd,group=corpus,png_pdf=characters_outside_the_resolved_DejaVu_cmap_are_drawn_as_U+FFFD_from_the_render_only_SVG,client_svg=keeps_them_exact,texts_json=lists_them_outside_covered,source=75-CONTEXT_R-9,docs=phase_76_client_documentation\n' >> "$LINES"
  printf 'KNOWN_LIMIT,key=pango_kerning,group=corpus,source=75-RESEARCH.md_Pattern_1_tiny_huge_row,start=exact,height=exact,width=up_to_3.3pct_narrower,affects=centre_right_FIT_ALIGNED_text,accepted_by=75-CONTEXT_R-1,docs=phase_76_client_documentation\n' >> "$LINES"
  hexbox_finding
}

out_row() {
  local file=$1 facts=$2 ok=$3 path="$DXF_OUT/$1" result=fail
  if [ "$ok" = 1 ] && [ -s "$path" ]; then result=ok; fi
  printf 'OUT,group=writeout,file=%s,bytes=%s,sha256=%s,%s,result=%s\n' "$file" \
    "$( [ -f "$path" ] && wc -c < "$path" | tr -d ' ' || echo 0 )" "$( [ -f "$path" ] && sha_of "$path" || echo none )" \
    "$facts" "$result" >> "$LINES"
}

both_ok() {
  if [ "$1" = 0 ]; then
    case ",$3," in
      *",$2,"*) echo 1 ;;
      *) echo 0 ;;
    esac
  else
    echo 0
  fi
}

pdf_text_fields() {
  python3 - "$1" "${2:-}" <<'PY'
import json, subprocess, sys
pdf, texts = sys.argv[1], sys.argv[2]
with open(pdf, "rb") as fh:
    magic = fh.read(5) == b"%PDF-"
out = subprocess.run(["pdftotext", "-raw", pdf, "-"], capture_output=True).stdout.decode("utf-8", "replace")
got = "".join(out.split())
fields = ["pdf_magic=%d" % magic, "pdf_text_chars=%d" % len(got)]
if texts:
    with open(texts, encoding="ascii") as fh:
        want = "".join("".join(e["covered"].split()) for e in json.load(fh)["texts"] if not e.get("invisible"))
    it = iter(got)
    fields.append("covered_chars=%d" % len(want))
    fields.append("covered_subsequence=%d" % int(len(want) > 0 and all(c in it for c in want)))
print(",".join(fields))
PY
}

png_facts() {
  python3 - "$1" <<'PY'
import struct, sys
with open(sys.argv[1], "rb") as fh:
    data = fh.read()
if data[:8] != b"\x89PNG\r\n\x1a\n":
    print("png_w=0,png_h=0,color_type=na")
    sys.exit(0)
w, h = struct.unpack(">II", data[16:24])
print("png_w=%d,png_h=%d,color_type=%d" % (w, h, data[25]))
PY
}

draw_out() {
  local name=$1 src=$2 job rc
  job=$(job_with "$src")
  rc=$(helper_exec "$DXF_RO" "$job" "out_$name" $(std_args))
  printf '%s %s\n' "$rc" "$job"
}

group_writeout() {
  local n src job rc facts pdfrc r sw sh left top spec
  disk_guard
  mkdir -p "$DXF_OUT"
  : > "$WORK/frames.rows"
  for n in text_cases:dxf legacy_cp1251:synthetic uncommon:public hatches_2:public wide:dxf tall:dxf wrong_extents:dxf units_3:dxf text_alpha:dxf text_alpha_faint:dxf; do
    src="$CORPUS/${n#*:}/${n%%:*}.dxf"
    n=${n%%:*}
    read -r rc job <<< "$(draw_out "$n" "$src")"
    facts="source=${src#$CORPUS/},source_sha256=$(sha_of "$src"),helper_rc=$rc,$(verdict_fields "$WORK/out_$n.out" | cut -d, -f1-3)"
    case "$n" in
      wrong_extents|units_3)
        cp "$job/out.measure.json" "$DXF_OUT/$n.measure.json" 2>/dev/null || true
        out_row "$n.measure.json" "$facts" "$( [ "$rc" = 0 ] && echo 1 || echo 0 )" ;;
      *)
        cp "$job/out.svg" "$DXF_OUT/$n.svg" 2>/dev/null || true
        cp "$job/out.texts.json" "$DXF_OUT/$n.texts.json" 2>/dev/null || true
        out_row "$n.svg" "$facts,svg_texts=$(grep -o '<text' "$job/out.svg" 2>/dev/null | wc -l | tr -d ' ')" "$( [ "$rc" = 0 ] && echo 1 || echo 0 )"
        out_row "$n.texts.json" "$facts" "$( [ "$rc" = 0 ] && echo 1 || echo 0 )"
        cp "$job/out.render.svg" "$DXF_OUT/$n.render.svg" 2>/dev/null || true
        out_row "$n.render.svg" "$facts,render_svg_texts=$(grep -o '<text' "$job/out.render.svg" 2>/dev/null | wc -l | tr -d ' '),substituted=$(grep -o $'\xef\xbf\xbd' "$job/out.render.svg" 2>/dev/null | wc -l | tr -d ' ')" "$( [ "$rc" = 0 ] && echo 1 || echo 0 )"
        if [ "$n" = wide ]; then
          cp "$job/out.measure.json" "$DXF_OUT/wide.measure.json" 2>/dev/null || true
          out_row wide.measure.json "$facts" "$( [ "$rc" = 0 ] && echo 1 || echo 0 )"
        fi ;;
    esac
    case "$n" in
      text_cases|legacy_cp1251|uncommon|hatches_2|text_alpha|text_alpha_faint)
        pdfrc=0
        MOUNTS=()
        run_confined "$RSVG_RO" "$job" "$WORK/pdf.out" "$WORK/pdf.err" /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/out.render.svg || pdfrc=$?
        cp "$job/out.pdf" "$DXF_OUT/$n.pdf" 2>/dev/null || true
        facts="source=$n.render.svg,rsvg_rc=$pdfrc,ruleset=rsvg,$(pdf_text_fields "$DXF_OUT/$n.pdf" "$DXF_OUT/$n.texts.json")"
        out_row "$n.pdf" "$facts" "$(both_ok "$pdfrc" covered_subsequence=1 "$facts")"
        ;;
    esac
    if [ "$n" = text_cases ]; then
      pdfrc=0
      run_confined "$RSVG_NOFONT_RO" "$job" "$WORK/pdf.out" "$WORK/pdf.err" /usr/bin/rsvg-convert -f pdf -b white -o /job/nofont.pdf /job/out.render.svg || pdfrc=$?
      cp "$job/nofont.pdf" "$DXF_OUT/nofont.pdf" 2>/dev/null || true
      facts="source=text_cases.render.svg,rsvg_rc=$pdfrc,ruleset=rsvg_without_usr_share_fonts,$(pdf_text_fields "$DXF_OUT/nofont.pdf")"
      out_row nofont.pdf "$facts" "$(both_ok "$pdfrc" pdf_text_chars=0 "$facts")"
      rm -f "$job/out.svg" "$job/out.render.svg" "$job/out.texts.json" "$job/out.measure.json"
      r=$(harness_run "$job" out_outline outline $(std_args))
      pdfrc=1
      if [ "$r" = 0 ]; then
        pdfrc=0
        run_confined "$RSVG_RO" "$job" "$WORK/pdf.out" "$WORK/pdf.err" /usr/bin/rsvg-convert -f pdf -b white -o /job/outline.pdf /job/out.svg || pdfrc=$?
      fi
      cp "$job/outline.pdf" "$DXF_OUT/outline.pdf" 2>/dev/null || true
      facts="source=text_cases.dxf_stock_svg_backend_text_as_paths,helper_rc=$r,rsvg_rc=$pdfrc,ruleset=rsvg,$(pdf_text_fields "$DXF_OUT/outline.pdf")"
      out_row outline.pdf "$facts" "$(both_ok "$pdfrc" pdf_text_chars=0 "$facts")"
    fi
    if [ "$n" = wide ] || [ "$n" = tall ] || [ "$n" = text_alpha_faint ]; then
      cp "$job/out.render.svg" "$job/frame.svg" 2>/dev/null || true
    fi
    if [ "$n" = wide ] || [ "$n" = text_alpha_faint ]; then
      for spec in 1280:opaque 1280:transparent; do
        read -r r sw sh left top <<< "$(rsvg_png "$RSVG_RO" "$job" frame.svg out.png "${spec%%:*}" "${spec#*:}")"
        cp "$job/out.png" "$DXF_OUT/${n}_${spec%%:*}_${spec#*:}.png" 2>/dev/null || true
        frame_line "${n}_${spec%%:*}_${spec#*:}.png" "${spec%%:*}" "${spec#*:}" "$job/frame.svg" "$sw" "$sh" "$left" "$top"
        out_row "${n}_${spec%%:*}_${spec#*:}.png" "source=$n.render.svg,rsvg_rc=$r,ruleset=rsvg,scaled=${sw}x${sh},offset=${left}+${top},$(png_facts "$DXF_OUT/${n}_${spec%%:*}_${spec#*:}.png")" \
          "$( [ "$r" = 0 ] && echo 1 || echo 0 )"
        rm -f "$job/out.png"
      done
    fi
    if [ "$n" = tall ]; then
      read -r r sw sh left top <<< "$(rsvg_png "$RSVG_RO" "$job" frame.svg out.png 256 opaque)"
      cp "$job/out.png" "$DXF_OUT/tall_256_opaque.png" 2>/dev/null || true
      frame_line tall_256_opaque.png 256 opaque "$job/frame.svg" "$sw" "$sh" "$left" "$top"
      out_row tall_256_opaque.png "source=tall.render.svg,rsvg_rc=$r,ruleset=rsvg,scaled=${sw}x${sh},offset=${left}+${top},$(png_facts "$DXF_OUT/tall_256_opaque.png")" \
        "$( [ "$r" = 0 ] && echo 1 || echo 0 )"
    fi
    rm -rf "$job"
  done
  { printf 'name\tsize\tbackground\tw_mm\th_mm\tsw\tsh\tleft\ttop\n'; cat "$WORK/frames.rows"; } > "$DXF_OUT/frames.tsv"
  out_row frames.tsv "rows=$(wc -l < "$WORK/frames.rows" | tr -d ' ')" "$( [ "$(wc -l < "$WORK/frames.rows" | tr -d ' ')" = 5 ] && echo 1 || echo 0 )"
}

frame_line() {
  local name=$1 size=$2 bg=$3 svg=$4 sw=$5 sh=$6 left=$7 top=$8 w h
  read -r w h <<< "$(python3 -c 'import sys, xml.etree.ElementTree as ET; r = ET.parse(sys.argv[1]).getroot(); print(r.get("width")[:-2], r.get("height")[:-2])' "$svg")"
  printf '%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' "$name" "$size" "$bg" "$w" "$h" "$sw" "$sh" "$left" "$top" >> "$WORK/frames.rows"
}

write_line_tools() {
  cat > "$WORK/wrap/lines.py" <<'PY'
import json
import math
import re
import sys
import numpy as np
from PIL import Image


def ink(path):
    a = np.asarray(Image.open(path).convert("RGB"))
    return (a < 160).any(axis=2)


def frame(matrix):
    m = [float(x) for x in re.findall(r"[-0-9.eE+]+", matrix)]
    ux, uy = m[0], m[1]
    k = math.hypot(ux, uy)
    nx, ny = m[2], m[3]
    j = math.hypot(nx, ny)
    return ux / k, uy / k, nx / j, ny / j


def coords(mask, f):
    ys, xs = np.nonzero(mask)
    ux, uy, nx, ny = f
    return xs, ys, xs * ux + ys * uy, xs * nx + ys * ny


def box(mask, f):
    xs, ys, u, v = coords(mask, f)
    if len(xs) == 0:
        return None
    return [float(u.min()), float(u.max()), float(v.min()), float(v.max()),
            int(xs.min()), int(ys.min()), int(xs.max()), int(ys.max())]


def bottom_in(mask, f, lo, hi):
    xs, ys, u, v = coords(mask, f)
    sel = (u >= lo) & (u <= hi)
    if not sel.any():
        return None
    return float(v[sel].max())


d = sys.argv[1]
with open(sys.argv[2]) as fh:
    rows = [ln.rstrip("\n").split("\t") for ln in fh if ln.strip()]
for line, variant, text_png, outline_png, matrix_file, prefix_png in rows:
    with open("%s/%s" % (d, matrix_file)) as fh:
        f = frame(fh.read())
    t = ink("%s/%s" % (d, text_png))
    o = ink("%s/%s" % (d, outline_png))
    out = {"line": int(line), "variant": variant, "text": box(t, f), "outline": box(o, f)}
    if prefix_png != "-":
        p = box(ink("%s/%s" % (d, prefix_png)), f)
        out["prefix"] = p
        if p is not None:
            out["prefix_bottom_text"] = bottom_in(t, f, p[0], p[1] - 1)
            out["prefix_bottom_outline"] = bottom_in(o, f, p[0], p[1] - 1)
    print(json.dumps(out))
PY
  chmod 0644 "$WORK/wrap/lines.py"
}

split_lines() {
  python3 - "$@" <<'PY'
import json
import os
import sys
import xml.etree.ElementTree as ET

NS = "http://www.w3.org/2000/svg"
ET.register_namespace("", NS)
src, outdir, prefix, texts_json = sys.argv[1:5]
with open(texts_json, encoding="ascii") as fh:
    entries = json.load(fh)["texts"]
tree = ET.parse(src)
root = tree.getroot()
texts = []
for parent in list(root.iter()):
    for child in list(parent):
        if child.tag == "{%s}path" % NS:
            parent.remove(child)
        elif child.tag == "{%s}text" % NS:
            texts.append((parent, child))
for parent, child in texts:
    parent.remove(child)
for i, (parent, child) in enumerate(texts):
    parent.append(child)
    tree.write(os.path.join(outdir, "%s_%03d.svg" % (prefix, i)), encoding="utf-8", xml_declaration=True)
    with open(os.path.join(outdir, "%s_%03d.matrix" % (prefix, i)), "w") as fh:
        fh.write(child.get("transform") + "\n")
    if prefix == "text" and i < len(entries) and entries[i]["covered"] != entries[i]["text"]:
        full = child.text or ""
        cut = full.find("�")
        head = full[:cut] if cut >= 0 else full
        if head:
            child.text = head
            tree.write(os.path.join(outdir, "prefix_%03d.svg" % i), encoding="utf-8", xml_declaration=True)
            child.text = full
    parent.remove(child)
print(len(texts))
PY
}

judge_lines() {
  python3 - "$@" <<'PY'
import json
import sys

import xml.etree.ElementTree as ET

metrics_path, texts_json, pdf_text, client_svg, left, top, sw, sh = sys.argv[1:9]
left, top, sw, sh = int(left), int(top), int(sw), int(sh)
client_texts = [el.text or "" for el in ET.parse(client_svg).getroot().iter("{http://www.w3.org/2000/svg}text")]
with open(texts_json, encoding="ascii") as fh:
    entries = json.load(fh)["texts"]
with open(pdf_text, encoding="utf-8", errors="replace") as fh:
    pdf = "".join(fh.read().split())
rows = [json.loads(ln) for ln in open(metrics_path) if ln.strip()]


def esc(s):
    return s.encode("unicode_escape").decode("ascii").replace(",", "\\x2c")


for r in rows:
    i, variant = r["line"], r["variant"]
    e = entries[i] if i < len(entries) else {"text": "", "covered": ""}
    substituted = sum(1 for c in e["text"] if c not in e["covered"])
    t, o = r["text"], r["outline"]
    f = ["line=%d" % i, "variant=%s" % variant, "text=%s" % esc(e["text"]), "substituted=%d" % substituted]
    checks = []
    if t is None and o is None:
        present = "".join(e["covered"].split()) in pdf and bool(e["covered"].strip())
        in_client = i < len(client_texts) and client_texts[i] == e["text"] and bool(e["text"])
        f += ["both_empty=1", "in_client_svg_and_texts_json=%d" % in_client, "in_pdf_text=%d" % present]
        checks += [present, in_client]
        gated = "both_empty+pdf_text"
    elif t is None or o is None:
        f += ["both_empty=0", "text_ink=%d" % (t is not None), "outline_ink=%d" % (o is not None)]
        checks.append(False)
        gated = "ink_on_both_sides"
    else:
        d_start = t[0] - o[0]
        d_height = (t[3] - t[2]) - (o[3] - o[2])
        ow = o[1] - o[0]
        tw = t[1] - t[0]
        bound = max(0.035 * ow, 1.0)
        inside = t[4] >= left and t[5] >= top and t[6] < left + sw and t[7] < top + sh
        f += ["both_empty=0", "d_start_px=%.2f" % d_start]
        if substituted:
            pb_t, pb_o = r.get("prefix_bottom_text"), r.get("prefix_bottom_outline")
            if pb_t is None or pb_o is None:
                d_base = None
            else:
                d_base = pb_t - pb_o
            f += ["baseline_basis=covered_prefix", "d_baseline_px=%s" % ("na" if d_base is None else "%.2f" % d_base)]
            checks += [abs(d_start) <= 1.0, d_base is not None and abs(d_base) <= 1.0, inside]
            gated = "start+prefix_baseline+inside_page"
        else:
            d_base = t[3] - o[3]
            f += ["baseline_basis=line", "d_baseline_px=%.2f" % d_base]
            checks += [abs(d_start) <= 1.0, abs(d_base) <= 1.0, abs(d_height) <= 1.0, abs(tw - ow) <= bound, inside]
            gated = "start+baseline+height+width+inside_page"
        f += ["d_height_px=%.2f" % d_height, "outline_width_px=%.0f" % ow, "text_width_px=%.0f" % tw,
              "d_width_px=%.0f" % (tw - ow), "d_width_pct=%.2f" % (100.0 * (tw - ow) / ow if ow else 0.0),
              "width_bound_px=%.2f" % bound, "inside_page=%d" % inside]
    check = "pass" if all(checks) else "fail"
    f += ["gated=%s" % gated, "check=%s" % check]
    if variant == "client_before":
        f += ["expected=fail", "result=%s" % ("ok" if check == "fail" else "fail")]
    else:
        f += ["result=%s" % ("ok" if check == "pass" else "fail")]
    print("LINE,group=corpus,file=text_cases," + ",".join(f))
PY
}

fidelity_lines() {
  local name=$1 src=$2 job ld rc n i sw sh left top r cmp orc pdfrc lines_ok lines_fail teeth result page_equal subst f
  job=$(job_with "$src")
  rc=$(helper_exec "$DXF_RO" "$job" "lines_$name" $(std_args))
  ld=$(new_job)
  if [ "$rc" != 0 ] || [ ! -s "$job/out.render.svg" ]; then
    printf 'FIDELITY,group=corpus,file=%s,mode=per_line,helper_rc=%s,result=fail\n' "$name" "$rc" >> "$LINES"
    rm -rf "$job" "$ld"
    return 0
  fi
  cp "$job/out.svg" "$ld/client.svg"
  cp "$job/out.render.svg" "$ld/render.svg"
  cp "$job/out.texts.json" "$ld/texts.json"
  pdfrc=0
  MOUNTS=()
  run_confined "$RSVG_RO" "$job" "$WORK/lines_pdf.out" "$WORK/lines_pdf.err" /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/out.render.svg || pdfrc=$?
  pdftotext -raw "$job/out.pdf" "$ld/pdf.txt" 2>/dev/null || : > "$ld/pdf.txt"
  rm -f "$job/out.svg" "$job/out.render.svg" "$job/out.texts.json" "$job/out.measure.json" "$job/out.pdf"
  orc=$(harness_run "$job" "lines_full_$name" outline $(std_args))
  [ ! -s "$job/out.svg" ] || cp "$job/out.svg" "$ld/outline.svg"
  mkdir -p "$job/ol"
  chmod 0777 "$job/ol"
  r=$(harness_run "$job" "lines_outline_$name" outline_lines /job/ol $(std_args))
  cp "$job/ol"/line_*.svg "$ld/" 2>/dev/null || true
  n=$(split_lines "$ld/render.svg" "$ld" text "$ld/texts.json")
  split_lines "$ld/client.svg" "$ld" client "$ld/texts.json" >/dev/null
  read -r sw sh left top <<< "$(letterbox_size "$ld/render.svg" "$FRAME")"
  : > "$ld/manifest.tsv"
  for f in "$ld"/text_*.svg "$ld"/line_*.svg "$ld"/prefix_*.svg "$ld/outline.svg" "$ld/render.svg"; do
    [ -f "$f" ] || continue
    run_confined "$RSVG_RO" "$ld" "$WORK/lines_r.out" "$WORK/lines_r.err" /usr/bin/rsvg-convert -b white -w "$sw" -h "$sh" \
      --page-width "$FRAME" --page-height "$FRAME" --left "$left" --top "$top" -o "/job/$(basename "${f%.svg}").png" "/job/$(basename "$f")" || true
  done
  i=0
  while [ "$i" -lt "$n" ]; do
    printf '%d\trender\ttext_%03d.png\tline_%03d.png\ttext_%03d.matrix\t%s\n' "$i" "$i" "$i" "$i" \
      "$( [ -f "$ld/$(printf 'prefix_%03d.svg' "$i")" ] && printf 'prefix_%03d.png' "$i" || echo - )" >> "$ld/manifest.tsv"
    if [ -f "$ld/$(printf 'prefix_%03d.svg' "$i")" ]; then
      run_confined "$RSVG_RO" "$ld" "$WORK/lines_r.out" "$WORK/lines_r.err" /usr/bin/rsvg-convert -b white -w "$sw" -h "$sh" \
        --page-width "$FRAME" --page-height "$FRAME" --left "$left" --top "$top" -o "/job/$(printf 'client_%03d.png' "$i")" "/job/$(printf 'client_%03d.svg' "$i")" || true
      printf '%d\tclient_before\tclient_%03d.png\tline_%03d.png\tclient_%03d.matrix\tprefix_%03d.png\n' "$i" "$i" "$i" "$i" "$i" >> "$ld/manifest.tsv"
    fi
    i=$((i + 1))
  done
  in_image "$ld" /wrap/lines.py /job /job/manifest.tsv > "$ld/metrics.jsonl"
  judge_lines "$ld/metrics.jsonl" "$ld/texts.json" "$ld/pdf.txt" "$ld/client.svg" "$left" "$top" "$sw" "$sh" >> "$LINES"
  lines_ok=$(grep -cE "^LINE,group=corpus,file=$name,line=[0-9]+,variant=render,.*,result=ok$" "$LINES" || true)
  lines_fail=$(grep -cE "^LINE,group=corpus,file=$name,line=[0-9]+,variant=render,.*,result=fail$" "$LINES" || true)
  teeth=$(grep -cE "^LINE,group=corpus,file=$name,line=[0-9]+,variant=client_before,.*,check=fail,expected=fail,result=ok$" "$LINES" || true)
  subst=$(grep -cE "^LINE,group=corpus,file=$name,line=[0-9]+,variant=client_before," "$LINES" || true)
  cmp="shape_equal=0"
  [ ! -s "$ld/outline.png" ] || [ ! -s "$ld/render.png" ] || cmp=$(in_image "$ld" /wrap/compare.py /job/render.png /job/outline.png)
  page_equal=0
  if [ -s "$ld/outline.svg" ] && [ "$(python3 -c 'import sys, xml.etree.ElementTree as ET; print(*[(lambda r: r.get("width") + r.get("height"))(ET.parse(p).getroot()) for p in sys.argv[1:]])' "$ld/render.svg" "$ld/outline.svg" | awk '{print ($1 == $2)}')" = 1 ]; then page_equal=1; fi
  result=fail
  if [ "$n" -gt 0 ] && [ "$lines_ok" = "$n" ] && [ "$lines_fail" = 0 ] && [ "$subst" -ge 1 ] && [ "$teeth" = "$subst" ] \
     && [ "$(cat "$job/ol/lines.count" 2>/dev/null)" = "$n" ] && [ "$page_equal" = 1 ] && [ "$pdfrc" = 0 ] && [ "$orc" = 0 ] && [ "$r" = 0 ]; then
    result=ok
  fi
  printf 'FIDELITY,group=corpus,file=%s,sha256=%s,mode=per_line,source_svg=render,size=%s,background=opaque,lines=%s,outline_lines=%s,lines_ok=%s,lines_fail=%s,client_before_rows=%s,client_before_failing=%s,page_equal=%s,%s,result=%s\n' \
    "$name" "$(sha_of "$src")" "$FRAME" "$n" "$(cat "$job/ol/lines.count" 2>/dev/null || echo na)" "$lines_ok" "$lines_fail" "$subst" "$teeth" "$page_equal" \
    "$(printf '%s' "$cmp" | sed 's/text_in_outline=/info_coverage_text_in_outline=/; s/outline_in_text=/info_coverage_outline_in_text=/')" "$result" >> "$LINES"
  rm -rf "$job" "$ld"
}

group_rows() { grep -cE "^(HELPER|CORPUS|FIDELITY|LINE|OUT),group=$1," "$2" || true; }

group_fails() { grep -E "^(HELPER|CORPUS|FIDELITY|LINE|OUT),group=$1," "$2" | grep -c ',result=fail$' || true; }

helper_mode() {
  local groups g rows fails all_rows=0 all_fails=0
  need_tool pdftotext
  setup helper
  write_judge
  write_harness
  case "$GROUP" in
    all) groups="semantics safety corpus" ;;
    *) groups=$GROUP ;;
  esac
  [ "$WRITE_OUT" = 0 ] || groups="$groups writeout"
  printf '%s\n' "$LOCK_LINE" > "$WORK/lock.txt"
  evid_put "$WORK/lock.txt" '^LOCK,'
  for g in $groups; do
    LINES="$WORK/lines-$g.txt"
    : > "$LINES"
    sed "s/^HDR,mode=helper,/HDR,mode=helper,group=$g,/" "$HDR_LINES" >> "$LINES"
    printf 'HDR,mode=helper,group=%s,ro_cad_dxf=%s,ro_cad_dxf_nofont=%s,ro_rsvg=%s,ro_rsvg_nofont=%s,rw=%s\n' \
      "$g" "$DXF_RO" "$DXF_NOFONT_RO" "$RSVG_RO" "$RSVG_NOFONT_RO" "$RW_SET" >> "$LINES"
    "group_$g"
    rows=$(group_rows "$g" "$LINES")
    fails=$(group_fails "$g" "$LINES")
    [ "$rows" -gt 0 ] || fails=$((fails + 1))
    all_rows=$((all_rows + rows))
    all_fails=$((all_fails + fails))
    printf 'SUMMARY,mode=helper,group=%s,rows=%s,fail=%s,facts=%s\n' "$g" "$rows" "$fails" \
      "$(grep -cE "^HELPER,group=$g,.*,result=fact$" "$LINES" || true)" >> "$LINES"
    evid_put "$LINES" "^[A-Z_]+,(mode=helper,|key=[^,]*,)?group=$g,"
    cat "$LINES"
  done
  if [ "$GROUP" = all ]; then
    printf 'SUMMARY,mode=helper,group=all,groups=%s,rows=%s,fail=%s,commit=%s\n' \
      "$(printf '%s' "$groups" | tr ' ' '+')" "$all_rows" "$all_fails" "$(git rev-parse HEAD)" > "$WORK/all.txt"
    evid_put "$WORK/all.txt" '^SUMMARY,mode=helper,group=all,'
    cat "$WORK/all.txt"
  fi
  [ "$all_fails" = 0 ]
}

measured_ro() {
  local stage=$1 engine=$2 line ro mult
  line=$(grep "^MEASURED,stage=$stage," "$LANDLOCK_EVID" 2>/dev/null || true)
  [ "$(printf '%s\n' "$line" | grep -c .)" = 1 ] || return 0
  ro=$(printf '%s' "$line" | sed -n 's/^.*,ro=\([^,]*\),engines=.*$/\1/p')
  mult=$(printf '%s' "$line" | sed -n 's/^.*,multiarch=\(.*\)$/\1/p')
  [ -n "$ro" ] && [ "$mult" = "/usr/lib/$TRIPLET" ] || return 0
  printf '%s:%s:%s\n' "$ro" "$mult" "$engine"
}

write_pixelcheck_exact() {
  mkdir -p "$WORK/pc"
  chmod 0755 "$WORK/pc"
  cp "$PIXELCHECK" "$WORK/pc/pixelcheck.py"
  cat > "$WORK/pc/exact.py" <<'PY'
import contextlib
import io
import sys

sys.path.insert(0, "/pc")
import pixelcheck as pc

for path in sys.argv[1:]:
    name = path.rsplit("/", 1)[-1][:-len(".png")]
    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        rc = pc.single(path, "drawing")
    img = pc.load_rgb(path)
    h, w, mask, dark, frac, box = pc.stats(img)
    margin = -1 if box is None else min(box[0], box[1], w - 1 - box[2], h - 1 - box[3])
    print("%s\t%s\tframe_rc=%d,frac_exact=%r,dark_exact=%r,margin_exact=%d,margin_need=%d,frac_min=%r,dark_min=%r" % (
        name, buf.getvalue().strip(), rc, frac, dark, margin, int(pc.MARGIN_MIN * min(w, h)), pc.DRAW_FRAC_MIN, pc.DRAW_DARK_MIN))
PY
  chmod 0644 "$WORK/pc/"*.py
}

judge_frame_png() {
  local job=$1 name=$2 size=$3 bg=$4 sw=$5 sh=$6 left=$7 top=$8 png rc=0 args=()
  png="f_${size}_${bg}.png"
  if [ "$bg" = opaque ]; then args=(-b white); fi
  MOUNTS=()
  run_confined "$RSVG_RO" "$job" "$WORK/rsvg.out" "$WORK/rsvg.err" /usr/bin/rsvg-convert ${args[@]+"${args[@]}"} \
    -w "$sw" -h "$sh" --page-width "$size" --page-height "$size" --left "$left" --top "$top" -o "/job/$png" /job/clean.svg || rc=$?
  if probe_refused "$WORK/rsvg.err" || [ "$rc" != 0 ] || [ ! -s "$job/$png" ]; then
    printf 'FRAME,drawing=%s,size=%s,background=%s,rsvg_rc=%s,stderr=%s,result=fail\n' "$name" "$size" "$bg" "$rc" \
      "$(head -n 1 "$WORK/rsvg.err" | tr -c 'A-Za-z0-9 ._/:-' '_' | cut -c1-120)" >> "$LINES"
    JUDGE_FAIL=1
    return 0
  fi
  cp "$job/$png" "$WORK/frames/${name}_${size}_${bg}.png"
  chmod 0644 "$WORK/frames/${name}_${size}_${bg}.png"
  printf '%s\t%s\t%s\t%s\t%s\n' "$name" "$size" "$bg" "$sw:$sh:$left:$top" \
    "$("$WORK/tools/cad-pixel-judge" --size "$size" --background "$bg" --drawing --letterbox-svg "$job/clean.svg" \
       "$WORK/frames/${name}_${size}_${bg}.png" 2>/dev/null || true)" >> "$WORK/go.tsv"
}

judge_drawing() {
  local name=$1 src=$2 job rc vf sv size bg lb sw sh left top
  disk_guard
  job=$(job_with "$src")
  rc=$(helper_exec "$DXF_RO" "$job" "judge_$name" --max-drawn-entities "$MAX_DRAWN" --texts /job/texts.json \
    --render-svg /job/draw.render.svg --measure /job/measure.json /job/in.dxf /job/draw.svg)
  vf=$(verdict_fields "$WORK/judge_$name.out")
  if probe_refused "$WORK/judge_$name.err" || [ "$rc" != 0 ] || [ "$(field verdict "$vf")" != ok ] || [ ! -s "$job/draw.render.svg" ]; then
    printf 'FRAME_SOURCE,drawing=%s,input=%s,sha256=%s,rc=%s,%s,result=fail\n' "$name" "${src#"$CORPUS"/}" "$(sha_of "$src")" "$rc" "$vf" >> "$LINES"
    JUDGE_FAIL=1
    rm -rf "$job"
    return 0
  fi
  sv=$("$WORK/tools/cad-svg-check" --max-bytes "$SVG_MAX" --out "$job/clean.svg" "$job/draw.render.svg" 2>&1 || true)
  case "$sv" in
    GOSVG,*,ok=1,*) ;;
    *)
      printf 'FRAME_SOURCE,drawing=%s,input=%s,sha256=%s,rc=%s,%s,svg_check=%s,result=fail\n' "$name" "${src#"$CORPUS"/}" "$(sha_of "$src")" \
        "$rc" "$vf" "$(printf '%s' "$sv" | sed 's/^GOSVG,svg=[^,]*,//' | tr ',' ';')" >> "$LINES"
      JUDGE_FAIL=1
      rm -rf "$job"
      return 0 ;;
  esac
  chmod 0644 "$job/clean.svg"
  cp "$job/clean.svg" "$WORK/frames/$name.svg"
  printf 'FRAME_SOURCE,drawing=%s,input=%s,sha256=%s,rc=0,%s,clean_svg_sha256=%s,%s,result=ok\n' "$name" "${src#"$CORPUS"/}" "$(sha_of "$src")" \
    "$vf" "$(sha_of "$job/clean.svg")" "$(printf '%s' "$sv" | sed 's/^GOSVG,svg=[^,]*,//; s/,reason=none$//')" >> "$LINES"
  for size in $JUDGE_SIZES; do
    lb=$("$WORK/tools/cad-pixel-judge" --print-letterbox --svg "$job/clean.svg" --size "$size" || true)
    read -r sw sh left top <<< "$(printf '%s' "$lb" | sed -n 's/^LETTERBOX,sw=\([0-9]*\),sh=\([0-9]*\),left=\([0-9]*\),top=\([0-9]*\)$/\1 \2 \3 \4/p')"
    if [ -z "${top:-}" ]; then
      printf 'FRAME,drawing=%s,size=%s,reason=no_go_letterbox,result=fail\n' "$name" "$size" >> "$LINES"
      JUDGE_FAIL=1
      continue
    fi
    for bg in opaque transparent; do
      judge_frame_png "$job" "$name" "$size" "$bg" "$sw" "$sh" "$left" "$top"
    done
  done
  rm -rf "$job"
}

judge_blank() {
  local lb
  chmod 0777 "$WORK/frames"
  docker run --rm --network none --user nobody -e HOME=/tmp -v "$WORK/frames:/frames" --entrypoint /opt/venv/bin/python "$TAG" -I -B -c \
    'from PIL import Image; Image.new("RGB", (1280, 1280), (255, 255, 255)).save("/frames/blank_1280_opaque.png")' </dev/null
  chmod 0755 "$WORK/frames"
  [ -s "$WORK/frames/blank_1280_opaque.png" ] || fail "the blank frame was not written"
  lb=$("$WORK/tools/cad-pixel-judge" --print-letterbox --svg "$WORK/frames/legacy_cp1251.svg" --size 1280 \
    | sed -n 's/^LETTERBOX,sw=\([0-9]*\),sh=\([0-9]*\),left=\([0-9]*\),top=\([0-9]*\)$/\1:\2:\3:\4/p')
  printf '%s\t%s\t%s\t%s\t%s\n' blank 1280 opaque "$lb" \
    "$("$WORK/tools/cad-pixel-judge" --size 1280 --background opaque --drawing --letterbox-svg "$WORK/frames/legacy_cp1251.svg" \
       "$WORK/frames/blank_1280_opaque.png" 2>/dev/null || true)" >> "$WORK/go.tsv"
}

judge_classify() {
  python3 - "$WORK/go.tsv" "$WORK/pix.tsv" "$1" <<'PY'
import sys

go_path, pix_path, expected = sys.argv[1], sys.argv[2], int(sys.argv[3])


def fields(line, prefix):
    out = {}
    if not line.startswith(prefix + ","):
        return None
    for part in line[len(prefix) + 1:].split(","):
        k, _, v = part.partition("=")
        out[k] = v
    return out


go = {}
for raw in open(go_path, encoding="utf-8"):
    name, size, bg, lb, g = raw.rstrip("\n").split("\t", 4)
    go[(name, int(size), bg)] = (lb, fields(g, "GOJUDGE"))
pix = {}
for raw in open(pix_path, encoding="utf-8"):
    parts = raw.rstrip("\n").split("\t")
    if len(parts) != 3:
        continue
    name, size, bg = parts[0].rsplit("_", 2)
    pix[(name, int(size), bg)] = (fields(parts[1], "PIX"), dict(p.split("=", 1) for p in parts[2].split(",")))

E2P_EXPECTED = {("far_point", 256, "opaque")}
rows = []
agree = explained = unexplained = real = real_go_ok = 0
classes = {"E1": 0, "E2": 0, "E2'": 0, "E3": 0}
e2p = set()
boxes = {}
for key in sorted(go, key=lambda k: (k[0] == "blank", k[0], k[1], k[2])):
    name, size, bg = key
    lb, g = go[key]
    p, ex = pix.get(key, (None, None))
    go_ok = 1 if g and g.get("ok") == "1" else 0
    pix_ok = 1 if p and p.get("ok") == "1" else 0
    if g:
        w, h = (int(x) for x in g["box"].split("x"))
        x0, y0 = int(g.get("box_x", "-1")), int(g.get("box_y", "-1"))
        boxes[key] = (x0, y0, x0 + w - 1, y0 + h - 1) if w > 0 else None
        rows.append("GOJUDGE,drawing=%s,size=%d,background=%s,letterbox=%s,ok=%d,count=%s,frac6=%s,dark=%s,box=%s,box_x=%s,box_y=%s,margin_px=%s,alpha=%s,frac_transparent=%s" % (
            name, size, bg, lb.replace(":", "x", 1).replace(":", "+"), go_ok, g.get("count"), g.get("frac6"), g.get("dark"), g.get("box"),
            g.get("box_x"), g.get("box_y"), g.get("margin_px"), g.get("alpha"), g.get("frac_transparent")))
    else:
        boxes[key] = None
        rows.append("GOJUDGE,drawing=%s,size=%d,background=%s,letterbox=%s,ok=0,output=none" % (name, size, bg, lb))
    if p:
        rows.append("PIX,drawing=%s,size=%d,background=%s,frac=%s,margin_px=%s,dark=%s,bbox=%s,w=%s,h=%s,mode=%s,ok=%s,frac_exact=%s,dark_exact=%s,margin_need=%s" % (
            name, size, bg, p.get("frac"), p.get("margin_px"), p.get("dark"), p.get("bbox"), p.get("w"), p.get("h"), p.get("mode"), p.get("ok"),
            ex.get("frac_exact"), ex.get("dark_exact"), ex.get("margin_need")))
    else:
        rows.append("PIX,drawing=%s,size=%d,background=%s,ok=none" % (name, size, bg))
    if name != "blank":
        real += 1
        real_go_ok += go_ok
    if p is None:
        unexplained += 1
        rows.append("DIVERGENCE,class=unexplained,drawing=%s,size=%d,background=%s,go_ok=%d,pix_ok=none,why=pixelcheck_printed_no_verdict" % (name, size, bg, go_ok))
        continue
    frac, dark = float(ex["frac_exact"]), float(ex["dark_exact"])
    margin, need = int(ex["margin_exact"]), int(ex["margin_need"])
    frac_fail = frac < float(ex["frac_min"])
    dark_fail = dark < float(ex["dark_min"])
    margin_fail = margin < need
    recomputed = 0 if (margin < 0 or frac_fail or dark_fail or margin_fail) else 1
    if recomputed != pix_ok:
        unexplained += 1
        rows.append("DIVERGENCE,class=unexplained,drawing=%s,size=%d,background=%s,go_ok=%d,pix_ok=%d,why=exact_values_do_not_reproduce_the_pixelcheck_verdict" % (name, size, bg, go_ok, pix_ok))
        continue
    facts = "frac_exact=%r;dark_exact=%r;margin=%d;need=%d;frac_fail=%d;dark_fail=%d;margin_fail=%d" % (
        frac, dark, margin, need, frac_fail, dark_fail, margin_fail)
    if go_ok == 0 and name != "blank":
        cls, why = "unexplained", "a_real_frame_rejected_by_go"
        if g and key in E2P_EXPECTED and pix_ok == 0:
            lw, lh = (int(x) for x in lb.split(":")[:2])
            bw, bh = (int(x) for x in g["box"].split("x"))
            if int(g.get("count", "0")) >= 1 and max(bw, bh) < 0.5 * max(lw, lh):
                cls, why = "E2'", "both_judges_reject_a_feature_below_one_pixel_at_this_size_go_J2_span_%d_of_%d" % (max(bw, bh), max(lw, lh))
        if cls == "unexplained":
            unexplained += 1
        else:
            explained += 1
            classes[cls] += 1
            e2p.add(key)
        rows.append("DIVERGENCE,class=%s,drawing=%s,size=%d,background=%s,go_ok=%d,pix_ok=%d,why=%s,facts=%s" % (cls, name, size, bg, go_ok, pix_ok, why, facts))
        continue
    if go_ok == pix_ok:
        agree += 1
        continue
    cls = "unexplained"
    why = "no_registered_class"
    if go_ok == 1 and name != "blank":
        if bg == "transparent":
            cls, why = "E3", "pixelcheck_converts_RGBA_to_RGB_so_transparent_black_counts_as_ink"
        elif name == "far_point" and (frac_fail or dark_fail) and not margin_fail:
            cls, why = "E2", "far_point_frame_accepted_under_R-4_and_R-11_pixelcheck_rejects_on_frac_or_dark"
        elif dark_fail and not frac_fail and not margin_fail:
            cls, why = "E1", "pixelcheck_rejects_only_on_dark"
    if cls == "unexplained":
        unexplained += 1
    else:
        explained += 1
        classes[cls] += 1
    rows.append("DIVERGENCE,class=%s,drawing=%s,size=%d,background=%s,go_ok=%d,pix_ok=%d,why=%s,facts=%s" % (cls, name, size, bg, go_ok, pix_ok, why, facts))

twins = twins_ok = twins_excluded = 0
for (name, size, bg), box in sorted(boxes.items()):
    if bg != "transparent" or name == "blank":
        continue
    twin = boxes.get((name, size, "opaque"))
    if (name, size, "opaque") in e2p and go.get((name, size, "transparent"), ("", {}))[1].get("ok") == "1":
        twins_excluded += 1
        rows.append("TWIN,drawing=%s,size=%d,transparent_box=%s,opaque_box=%s,compared=0,class=E2',why=the_opaque_twin_lost_the_sub_pixel_feature_and_is_rejected_so_there_is_no_accepted_box_to_match,ok=na" % (
            name, size, ":".join(map(str, box)) if box else "none", ":".join(map(str, twin)) if twin else "none"))
        continue
    twins += 1
    ok = 0
    diff = "na"
    if box and twin:
        d = max(abs(a - b) for a, b in zip(box, twin))
        diff = str(d)
        ok = 1 if d <= 2 else 0
    twins_ok += ok
    rows.append("TWIN,drawing=%s,size=%d,transparent_box=%s,opaque_box=%s,max_edge_diff_px=%s,slack_px=2,ok=%d" % (
        name, size, ":".join(map(str, box)) if box else "none", ":".join(map(str, twin)) if twin else "none", diff, ok))

blank_go = go.get(("blank", 1280, "opaque"), ("", None))[1]
blank_pix = pix.get(("blank", 1280, "opaque"), (None, None))[0]
blank_both = 1 if blank_go and blank_go.get("ok") == "0" and blank_pix and blank_pix.get("ok") == "0" else 0
rows.append("BLANK,size=1280,background=opaque,go_ok=%s,pix_ok=%s,rejected_by_both=%d" % (
    blank_go.get("ok") if blank_go else "none", blank_pix.get("ok") if blank_pix else "none", blank_both))
ok = (unexplained == 0 and real == expected and e2p == E2P_EXPECTED and real_go_ok == real - len(e2p)
      and twins + twins_excluded == expected // 2 and twins_excluded == len(e2p) and twins_ok == twins and blank_both == 1)
rows.append("JUDGE_SUMMARY,frames=%d,agreements=%d,explained=%d,explained_e1=%d,explained_e2=%d,explained_e2p=%d,explained_e3=%d,unexplained_divergence=%d,real_go_ok=%d,real_frames=%d,expected_real_frames=%d,real_rejected_e2p=%d,twins_ok=%d,twins=%d,twins_excluded_e2p=%d,blank_rejected_by_both=%d,result=%s" % (
    real + 1, agree, explained, classes["E1"], classes["E2"], classes["E2'"], classes["E3"], unexplained, real_go_ok, real, expected, len(e2p),
    twins_ok, twins, twins_excluded, blank_both, "ok" if ok else "fail"))
print("\n".join(rows))
PY
}

judge_mode() {
  local f n drawings svg_mib expected
  setup judge
  DXF_RO=$(measured_ro dxf /usr/local/bin/cad-dxf)
  RSVG_RO=$(measured_ro rsvg /usr/bin/rsvg-convert)
  [ -n "$DXF_RO" ] && [ -n "$RSVG_RO" ] \
    || fail "$LANDLOCK_EVID carries no single readable MEASURED line for dxf and rsvg; run scripts/cad-worker-sandbox-measure.sh --stages guard,dxf,rsvg first"
  svg_mib=$(sed -n 's/^[[:space:]]*CADMaxDXFSVGBytes[[:space:]]*=[[:space:]]*\([0-9][0-9]*\) << 20$/\1/p' "$REPO_ROOT/internal/convert/cad.go")
  [ -n "$svg_mib" ] || fail "CADMaxDXFSVGBytes is not N << 20 in internal/convert/cad.go"
  SVG_MAX=$((svg_mib << 20))
  mkdir -p "$WORK/tools" "$WORK/frames"
  chmod 0755 "$WORK/frames"
  for f in cad-svg-check cad-pixel-judge; do
    GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$WORK/tools/$f" "./scripts/cmd/$f"
  done
  write_pixelcheck_exact
  JUDGE_FAIL=0
  : > "$WORK/go.tsv"
  {
    printf 'HDR,mode=judge,ro_cad_dxf=%s,ro_rsvg=%s,rw=%s,landlock_evidence=evidence/75-08-landlock.txt\n' "$DXF_RO" "$RSVG_RO" "$RW_SET"
    printf 'HDR,mode=judge,go_judge=scripts/cmd/cad-pixel-judge --drawing --letterbox-svg,letterbox=scripts/cmd/cad-pixel-judge --print-letterbox,svg_validator=scripts/cmd/cad-svg-check,pixelcheck=scripts/cad-measure/pixelcheck.py single --mode drawing,pixelcheck_sha256=%s,pixelcheck_python=/opt/venv/bin/python\n' "$(sha_of "$PIXELCHECK")"
    printf "RULE,key=judge_divergence_classes,registered=before_any_frame,E1=pixelcheck_rejects_only_on_dark_while_go_accepts,E2=far_point_rejected_by_pixelcheck_on_frac_or_dark_and_accepted_by_go_under_R-4_and_R-11,E2'=both_judges_reject_a_feature_below_one_pixel_at_the_requested_size_expected_only_for_far_point_256_opaque_by_go_J2,E3=transparent_frame_flattened_by_pixelcheck,other=unexplained,source=75-08-PLAN_Task_1+75-CONTEXT_R-11\n"
    printf "RULE,key=judge_requirements,real_frames=all_ok_in_go_except_E2'_far_point_256_opaque,twins=transparent_ink_box_equals_opaque_twin_within_2px_when_both_are_accepted_and_the_E2'_pair_is_recorded_uncompared,blank_1280=rejected_by_both,unexplained=0,go_drawing_J1=at_least_one_ink_pixel_R-11,thresholds=75-04_except_R-11_J1\n"
  } >> "$LINES"
  drawings=0
  for f in $JUDGE_DRAWINGS; do
    judge_drawing "${f#*/}" "$CORPUS/$f.dxf"
    drawings=$((drawings + 1))
  done
  judge_blank
  docker run --rm --network none --user nobody -e HOME=/tmp -v "$WORK/frames:/frames:ro" -v "$WORK/pc:/pc:ro" \
    --entrypoint sh "$TAG" -c '/opt/venv/bin/python -I -B /pc/exact.py /frames/*.png' > "$WORK/pix.tsv" </dev/null \
    || fail "pixelcheck did not run in the engine image"
  expected=$((drawings * 6))
  judge_classify "$expected" >> "$LINES"
  n=$(grep -c '^JUDGE_SUMMARY,.*,result=ok$' "$LINES" || true)
  [ "$JUDGE_FAIL" = 0 ] || sed -i.bak 's/^\(JUDGE_SUMMARY,.*\),result=ok$/\1,frame_production=failed,result=fail/' "$LINES"
  rm -f "$LINES.bak"
  mkdir -p "$(dirname "$EVID")"
  cp "$LINES" "$EVID"
  grep -E '^(FRAME_SOURCE|FRAME|DIVERGENCE|TWIN|BLANK|JUDGE_SUMMARY),' "$LINES" | grep -vE '^TWIN,.*,ok=1$|^FRAME_SOURCE,.*,result=ok$'
  [ "$JUDGE_FAIL" = 0 ] && [ "$n" = 1 ]
}

[ $# -ge 1 ] || usage
MODE=$1
shift
WRITE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT_OVERRIDE=${2:?}; shift 2 ;;
    --write) [ "$MODE" = fixtures ] || usage; WRITE=1; shift ;;
    --group)
      [ "$MODE" = helper ] || usage
      case "${2:-}" in semantics|safety|corpus|all) GROUP=$2 ;; *) usage ;; esac
      shift 2 ;;
    --write-out) [ "$MODE" = helper ] || usage; WRITE_OUT=1; shift ;;
    *) usage ;;
  esac
done
if [ "$MODE" = helper ]; then
  EVID=$HELPER_EVID
  EVID_NAME=$HELPER_EVID_NAME
fi
if [ "$MODE" = judge ]; then
  EVID=$JUDGE_EVID
  EVID_NAME=$JUDGE_EVID_NAME
fi
[ -z "$OUT_OVERRIDE" ] || EVID=$OUT_OVERRIDE

case "$MODE" in
  tracer) tracer ;;
  fixtures) fixtures ;;
  helper) helper_mode ;;
  judge) judge_mode ;;
  *) usage ;;
esac

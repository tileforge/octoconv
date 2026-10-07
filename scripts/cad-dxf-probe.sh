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
FIXTURE_COUNT=41

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/cad-dxf-probe.sh tracer   [--out FILE]
  scripts/cad-dxf-probe.sh fixtures [--write] [--out FILE]

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

Every mode rewrites its own lines in the evidence file (default
.planning/phases/75-dxf-2d-pipeline/evidence/75-01-engine.txt) and removes the image tags it
built. Needs a native arm64 docker server, go, python3 and pdftotext.
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
  build_probe "$ARCH" "$WORK/probe"
  LINES="$WORK/lines.txt"
  : > "$LINES"
  {
    printf '%s\n' "$LOCK_LINE"
    header_lines "$mode" "$TAG"
    [ "$mode" != tracer ] || printf 'HDR,mode=%s,ro_cad_dxf=%s,ro_rsvg=%s,rw=%s\n' "$mode" "$DXF_RO" "$RSVG_RO" "$RW_SET"
  } >> "$LINES"
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
  run_confined "$DXF_RO" "$job" "$so" "$se" /usr/local/bin/cad-dxf /job/in.dxf /job/draw.svg || rc=$?
  result=fail
  if probe_refused "$se"; then
    vf="verdict=none,reason=probe_refused"; sf="svg=none"
  elif [ ! -s "$job/draw.svg" ]; then
    vf=$(verdict_fields "$so"); sf="svg=none"
    tail -n 8 "$se" >&2
  else
    vf=$(verdict_fields "$so")
    sf=$(svg_fields "$job/draw.svg" "$expect" "$WORK/$name.texts")
    if [ "$rc" = 0 ] && [ "$(field verdict "$vf")" = ok ] && [ "$(field elements_ok "$sf")" = 1 ] \
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
  cp "$job/draw.svg" "$WORK/$name.svg"

  so="$WORK/$name.pdf.out"; se="$WORK/$name.pdf.err"
  rc=0
  run_confined "$RSVG_RO" "$job" "$so" "$se" /usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf /job/draw.svg || rc=$?
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

  read -r sw sh left top <<< "$(letterbox_args "$job/draw.svg")"
  so="$WORK/$name.png.out"; se="$WORK/$name.png.err"
  rc=0
  run_confined "$RSVG_RO" "$job" "$so" "$se" /usr/bin/rsvg-convert -b white -w "$sw" -h "$sh" \
    --page-width "$FRAME" --page-height "$FRAME" --left "$left" --top "$top" -o /job/out.png /job/draw.svg || rc=$?
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
  run_unconfined "$ctl" "$WORK/ctl1.out" "$WORK/ctl1.err" /usr/local/bin/cad-dxf /outside/in.dxf /job/draw.svg || crc=$?
  if [ "$crc" = 0 ] && [ -s "$ctl/draw.svg" ]; then control=unconfined_ok; else control=unconfined_failed; fi
  rc=0
  run_confined "$DXF_RO" "$job" "$WORK/neg1.out" "$WORK/neg1.err" /usr/local/bin/cad-dxf /outside/in.dxf /job/draw.svg || rc=$?
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
    text_cases|text_illegal_chars|layers_off_frozen|all_hidden|never_resolved|wide|tall|far_point|single_point|xline_ray|units_*|header_far_extents|wrong_extents) echo strict_clean ;;
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

[ $# -ge 1 ] || usage
MODE=$1
shift
WRITE=0
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT_OVERRIDE=${2:?}; shift 2 ;;
    --write) [ "$MODE" = fixtures ] || usage; WRITE=1; shift ;;
    *) usage ;;
  esac
done
[ -z "$OUT_OVERRIDE" ] || EVID=$OUT_OVERRIDE

case "$MODE" in
  tracer) tracer ;;
  fixtures) fixtures ;;
  *) usage ;;
esac

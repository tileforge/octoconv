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
HELPER_EVID="$PHASE_DIR/evidence/75-02-helper.txt"
HELPER_EVID_NAME="evidence/75-02-helper.txt"
DXF_OUT="$CORPUS/dxf-out"
MAX_DRAWN=850155
CAD_CACHE="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
WRAP_PY="$REPO_ROOT/scripts/cad-measure/measure_wrap.py"
GROUP=all
WRITE_OUT=0

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/cad-dxf-probe.sh tracer   [--out FILE]
  scripts/cad-dxf-probe.sh fixtures [--write] [--out FILE]
  scripts/cad-dxf-probe.sh helper   [--group semantics|safety|corpus|all] [--write-out] [--out FILE]

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
  elif [ "${mode%%,*}" = helper ]; then
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
                and all(isinstance(e, dict) and set(e) == {"text", "covered"}
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
    if extra.get("measure") == "1":
        try:
            with open(draft_path, "rb") as fh:
                draft = json.loads(fh.read().decode("ascii"))
        except Exception as exc:
            fact("draft", exc.__class__.__name__)
        need("draft_present", draft is not None)
elif not extra.get("keep_outputs"):
    absent = not any(os.path.exists(os.path.join(job, n)) for n in ("out.svg", "out.measure.json", "out.texts.json"))
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
  printf '%s\n' --max-drawn-entities "${1:-$MAX_DRAWN}" --texts /job/out.texts.json --measure /job/out.measure.json /job/in.dxf /job/out.svg
}

write_harness() {
  mkdir -p "$WORK/wrap"
  chmod 0755 "$WORK/wrap"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
  cat > "$WORK/wrap/harness.py" <<'PY'
import importlib.machinery
import importlib.util
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
  rm -f "$job/out.svg" "$job/out.texts.json" "$job/out.measure.json"
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
  rc=$(helper_exec "$DXF_RO" "$job" usage_no_texts --max-drawn-entities "$MAX_DRAWN" /job/in.dxf /job/out.svg)
  helper_row safety usage_no_texts dxf/wide.dxf "$CORPUS/dxf/wide.dxf" "$rc" "$job"
  rm -rf "$job"
}

group_rows() { grep -cE "^(HELPER|CORPUS|FIDELITY|OUT),group=$1," "$2" || true; }

group_fails() { grep -E "^(HELPER|CORPUS|FIDELITY|OUT),group=$1," "$2" | grep -c ',result=fail$' || true; }

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
[ -z "$OUT_OVERRIDE" ] || EVID=$OUT_OVERRIDE

case "$MODE" in
  tracer) tracer ;;
  fixtures) fixtures ;;
  helper) helper_mode ;;
  *) usage ;;
esac

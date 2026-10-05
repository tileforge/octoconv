#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

usage() {
  printf '%s\n' \
    'Usage:' \
    '  scripts/cad-worker-sandbox-measure.sh [--amd64] [--out FILE]' \
    '  scripts/cad-worker-sandbox-measure.sh --recheck [--out FILE]' \
    'Exit codes: 64 usage, 69 docker or go missing, 1 a non-identical result, a probe failure or an emulated image.' >&2
  exit 64
}

PHASE_EVID="$REPO_ROOT/.planning/phases/74-3d-render-measurements/evidence"
MAIN_EVIDENCE="$PHASE_EVID/74-06-landlock.txt"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
W15_SRC="$CAD_CACHE_DIR/scaled/stl-ascii-nasa-sls-block1-1MiB.stl"
W15_SHA=77492adbca35ff7f0c583732208324a834170aec9cb76eee7ad4e0b853c18ef0
GO_AMD_DERIVED=x86_64-linux-gnu
RENDER_VARIANTS=(iso:1280:opaque top:2048:opaque iso:1280:transparent)
GO_TOOLCHAIN=go1.26.4
MIN_FREE_KIB=6291456
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
ENVS=(-e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2)
CANARY=/etc/hostname

OUT=""
RECHECK=0
AMD64=0
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=${2:?}; shift 2 ;;
    --amd64) AMD64=1; shift ;;
    --recheck) RECHECK=1; shift ;;
    -h|--help) usage ;;
    *) echo "error: unknown argument $1" >&2; usage ;;
  esac
done
if [ -z "$OUT" ]; then
  if [ "$RECHECK" = 1 ]; then OUT="${TMPDIR:-/tmp}/74-06-recheck.txt"; else OUT=$MAIN_EVIDENCE; fi
fi

command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
command -v go >/dev/null 2>&1 || { echo "error: go is required to build the probe" >&2; exit 69; }
command -v python3 >/dev/null 2>&1 || { echo "error: python3 is required" >&2; exit 69; }

MAX_ENTITIES=$(sed -n 's/^[[:space:]]*CADMaxSTEPEntities[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' internal/convert/cad.go)
MAX_NAUO=$(sed -n 's/^[[:space:]]*CADMaxNAUO[[:space:]]*=[[:space:]]*\([0-9][0-9]*\).*/\1/p' internal/convert/cad.go)
[ -n "$MAX_ENTITIES" ] && [ -n "$MAX_NAUO" ] || { echo "error: the guard caps are not in internal/convert/cad.go" >&2; exit 1; }

if [ "$RECHECK" = 1 ]; then
  [ -s "$MAIN_EVIDENCE" ] || { echo "error: $MAIN_EVIDENCE is missing; run the main measurement first" >&2; exit 1; }
  [ "$OUT" != "$MAIN_EVIDENCE" ] || { echo "error: --recheck must not overwrite $MAIN_EVIDENCE" >&2; exit 64; }
fi

mkdir -p "$(dirname "$OUT")"
: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }
note() { printf '%s\n' "$*" >> "$OUT"; printf '%s\n' "$*" >&2; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-worker-landlock.XXXXXX")
CREATED_TAGS=()
cleanup() {
  local t
  rm -rf "$WORK"
  for t in ${CREATED_TAGS[@]+"${CREATED_TAGS[@]}"}; do
    docker rmi "$t" >/dev/null 2>&1 || true
  done
}
trap cleanup EXIT
mkdir -p "$WORK/probe" "$WORK/ref" "$WORK/trace" "$WORK/stage"
chmod 0755 "$WORK" "$WORK/probe"
RUN_OUT="$WORK/run.out"
RUN_ERR="$WORK/run.err"

disk_guard() {
  local free
  free=$(df -k "$HOME" | awk 'NR==2 {print $4}')
  if [ "$free" -lt "$MIN_FREE_KIB" ]; then
    emit "FAIL,reason=free disk $((free / 1024)) MiB is below the 6 GiB floor"
    exit 1
  fi
}

inputs_short() {
  shasum -a 256 Dockerfile.cad-worker cmd/cad-worker/requirements-mesh.txt cmd/cad-worker/requirements-vtk.txt \
    cmd/cad-worker/vtk_keeplist.py cmd/cad-worker/vtk-keeplist.txt \
    cmd/cad-worker/helper/cad-mesh cmd/cad-worker/helper/cad-guard cmd/cad-worker/helper/cad-render \
    | awk '{print $1}' | shasum -a 256 | cut -c1-12
}

stage_context() {
  local dir=$1
  mkdir -p "$dir/cmd/cad-worker/helper"
  cp Dockerfile.cad-worker "$dir/Dockerfile.cad-worker"
  cp cmd/cad-worker/requirements-mesh.txt cmd/cad-worker/requirements-vtk.txt cmd/cad-worker/vtk_keeplist.py \
    cmd/cad-worker/vtk-keeplist.txt "$dir/cmd/cad-worker/"
  cp cmd/cad-worker/helper/cad-mesh cmd/cad-worker/helper/cad-guard cmd/cad-worker/helper/cad-render "$dir/cmd/cad-worker/helper/"
}

disk_guard
TAG="octoconv-cad-worker:measure-$(inputs_short)"
if ! docker image inspect "$TAG" >/dev/null 2>&1; then
  stage_context "$WORK/stage"
  cad_log "building $TAG"
  CREATED_TAGS+=("$TAG")
  docker build -f "$WORK/stage/Dockerfile.cad-worker" --target engine -t "$TAG" "$WORK/stage" >&2 \
    || { emit "FAIL,reason=image build failed"; exit 1; }
fi
cad_require_native "$TAG" || exit 1
IMAGE_ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
SERVER_ARCH=$(docker version --format '{{.Server.Arch}}')

CGO_ENABLED=0 GOOS=linux GOARCH="$IMAGE_ARCH" GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$WORK/probe/sandbox-probe" ./scripts/cmd/sandbox-probe
chmod 0755 "$WORK/probe/sandbox-probe"

IMAGE_FACTS=$(docker run --rm --network none --entrypoint sh "$TAG" -c '
  printf "kernel=%s\n" "$(uname -r)"
  printf "python=%s\n" "$(/opt/venv/bin/python --version 2>&1)"
  printf "ocp=%s\n" "$(/opt/venv/bin/python -I -B -c "import importlib.metadata as m; print(m.version(\"cadquery-ocp-novtk\"))")"
  printf "guard_interp=%s\n" "$(readlink -f /usr/bin/python3)"
  printf "mesh_interp=%s\n" "$(readlink -f /opt/venv/bin/python)"
  printf "triplets=%s\n" "$(ls -d /usr/lib/*-linux-gnu | sed "s#^/usr/lib/##" | paste -sd: -)"
')
fact() { printf '%s\n' "$IMAGE_FACTS" | sed -n "s/^$1=//p"; }
GUARD_INTERP=$(fact guard_interp)
MESH_INTERP=$(fact mesh_interp)
NATIVE_TRIPLET=$(fact triplets)
case "$NATIVE_TRIPLET" in
  *:*|"") emit "FAIL,reason=the image carries no single multiarch directory: '$NATIVE_TRIPLET'"; exit 1 ;;
esac
MULTIARCH_DIR="/usr/lib/$NATIVE_TRIPLET"

go_triplet() {
  sed -n 's/^var cadMultiarchLib = .*"'"$1"'": "\([^"]*\)".*/\1/p' internal/convert/cadsandbox.go
}

emit "# Plan 74-06: Landlock sets of the cad-guard, cad-mesh and cad-render stages, measured on the production engine image"
emit "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
emit "HDR,server_arch=$SERVER_ARCH,image_arch=$IMAGE_ARCH,image=$TAG"
emit "HDR,image_id=$(docker image inspect "$TAG" --format '{{.Id}}'),user=$(docker image inspect "$TAG" --format '{{.Config.User}}')"
emit "HDR,kernel=$(fact kernel)"
emit "HDR,python=$(fact python),ocp=$(fact ocp)"
emit "HDR,guard_interp=$GUARD_INTERP,mesh_interp=$MESH_INTERP,multiarch=$NATIVE_TRIPLET"
emit "HDR,cpus=2,memory=4g,network=none,env=HOME=/job;LANG=C.UTF-8;TMPDIR=/job;LP_NUM_THREADS=2;PATH=image,rw=/job,canary=$CANARY"
emit "HDR,go=$(GOTOOLCHAIN=$GO_TOOLCHAIN go version),host_noise=$(docker ps -q | wc -l | tr -d ' ')"

add_input() { printf '%s\t%s\t%s\t%s\t%s\n' "$2" "$3" "$4" "$5" "${6:--}" >> "$WORK/inputs.$1.tsv"; }
STAGES="guard mesh render"
for s in $STAGES; do : > "$WORK/inputs.$s.tsv"; done
add_cad_input() {
  add_input guard "$@"
  add_input mesh "$@"
}
if [ "$RECHECK" = 1 ]; then
  add_cad_input units_inch "$CORPUS/synthetic/units_inch.step" step step
  add_cad_input surfaces_only "$CORPUS/synthetic/surfaces_only.iges" iges iges
  add_input mesh stl_ascii_box "$CORPUS/synthetic/stl_ascii_box.stl" stl stl
else
  add_cad_input units_inch "$CORPUS/synthetic/units_inch.step" step step
  add_cad_input LM8UU "$CORPUS/public/LM8UU.step" step step
  add_cad_input nist_ctc_05 "$CORPUS/public/nist_ctc_05_asme1_ap242-e1.stp" step step
  add_cad_input nist_d2mi_906 "$CORPUS/public/nist-d2mi-827-9999-906.stp" step step
  add_cad_input vslot_finite_box "$CORPUS/public/V-Slot-20x20-100mm.step" step step
  add_cad_input legacy_cp1251 "$CORPUS/synthetic/legacy_cp1251.step" step step
  add_cad_input assembly_nauo4 "$CORPUS/synthetic/assembly_nauo4.step" step step
  add_cad_input surfaces_only "$CORPUS/synthetic/surfaces_only.iges" iges iges
  add_cad_input iges_no_newlines "$CORPUS/synthetic/iges_no_newlines.iges" iges iges
  add_cad_input festo_igs "$CAD_CACHE_DIR/fetched/freecad-festo-valve-5-2.igs" iges iges
  add_input mesh stl_ascii_box "$CORPUS/synthetic/stl_ascii_box.stl" stl stl
  add_input mesh stl_binary_solid_header "$CORPUS/synthetic/stl_binary_solid_header.stl" stl stl
  add_input mesh nasa_hubble "$CORPUS/public/nasa-hubble-all-pieces.stl" stl stl
fi
check_inputs() {
  local label path fmt ext var stage
  for stage in guard mesh; do
    while IFS=$'\t' read -r -u 3 label path fmt ext var; do
      [ -f "$path" ] || { emit "FAIL,input=$label,reason=$path is missing; run scripts/cad-corpus-fetch.sh"; exit 1; }
    done 3< "$WORK/inputs.$stage.tsv"
  done
}
check_inputs
if [ "$RECHECK" != 1 ]; then
  [ -f "$W15_SRC" ] && [ "$(shasum -a 256 < "$W15_SRC" | cut -c1-64)" = "$W15_SHA" ] \
    || { emit "FAIL,input=w15,reason=$W15_SRC is missing or is not the 71 ladder slice sha256 $W15_SHA"; exit 1; }
fi

ARGV=()
stage_argv() {
  local stage=$1 fmt=$2 ext=$3 dir=$4 var=${5:--} view size bg
  case "$stage" in
    guard) ARGV=(/usr/local/bin/cad-guard --format "$fmt" --max-entities "$MAX_ENTITIES" --max-nauo "$MAX_NAUO" "$dir/in.$ext") ;;
    mesh)
      if [ "$fmt" = stl ]; then
        ARGV=(/usr/local/bin/cad-mesh stats --measure /job/measure.json "$dir/in.$ext")
      else
        ARGV=(/usr/local/bin/cad-mesh --format "$fmt" "$dir/in.$ext" /job/out.stl --quality standard --measure /job/measure.json)
      fi ;;
    render)
      IFS=: read -r view size bg <<< "$var"
      ARGV=(/usr/local/bin/cad-render --view "$view" --size "$size" --background "$bg" "$dir/in.$ext" /job/out.png) ;;
  esac
}

newdir() {
  local d
  d=$(mktemp -d "$WORK/job.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

EXTRA=()
JOB=""
RC=0
run_case() {
  local ro=$1 infile=$2 ext=$3 job rc=0
  shift 3
  job=$(newdir)
  if [ "$ro" = "-" ]; then
    docker run --rm "${LIMITS[@]}" "${ENVS[@]}" ${EXTRA[@]+"${EXTRA[@]}"} \
      -v "$job:/job" ${infile:+-v "$infile:/job/in.$ext:ro"} "$TAG" "$@" > "$RUN_OUT" 2> "$RUN_ERR" </dev/null || rc=$?
  else
    docker run --rm "${LIMITS[@]}" "${ENVS[@]}" -e "PROBE_RO=$ro" -e PROBE_RW=/job \
      --entrypoint /probe/sandbox-probe -v "$WORK/probe:/probe:ro" ${EXTRA[@]+"${EXTRA[@]}"} \
      -v "$job:/job" ${infile:+-v "$infile:/job/in.$ext:ro"} "$TAG" "$@" > "$RUN_OUT" 2> "$RUN_ERR" </dev/null || rc=$?
  fi
  JOB=$job
  RC=$rc
}

probe_failure() {
  grep -aqE 'the ruleset confines nothing|is not readable before the ruleset|not a permission denial|^apply:' "$RUN_ERR"
}

probe_failure_exit() {
  local stage=$1 label=$2
  emit "PROBE_FAIL,stage=$stage,input=$label,rc=$RC,message=$(grep -a -m1 -E 'the ruleset confines nothing|is not readable before|not a permission denial|^apply:' "$RUN_ERR" | tr ',' ' ')"
  rm -rf "$JOB"
  exit 1
}

signature() {
  local stage=$1 job=$2 v s m
  v=$(grep -a '^CADVERDICT ' "$RUN_OUT" | tail -n 1 || true)
  if [ -z "$v" ]; then
    echo missing
    return 0
  fi
  case "$stage" in
    mesh)
      if [ -s "$job/out.stl" ]; then s=$(shasum -a 256 < "$job/out.stl" | cut -c1-64); else s=none; fi
      if [ -s "$job/measure.json" ]; then m=$(shasum -a 256 < "$job/measure.json" | cut -c1-64); else m=none; fi
      printf '%s|stl=%s|measure=%s\n' "$v" "$s" "$m" ;;
    render)
      if [ -s "$job/out.png" ]; then s=$(shasum -a 256 < "$job/out.png" | cut -c1-64); else s=none; fi
      printf '%s|png=%s\n' "$v" "$s" ;;
    *) printf '%s\n' "$v" ;;
  esac
}

verdict_of() { printf '%s' "$1" | sed -n 's/.*"verdict":"\([a-z_]*\)".*/\1/p'; }
short_sig() { printf '%s' "$1" | shasum -a 256 | cut -c1-16; }

build_render_inputs() {
  local label path fmt ext var name v
  mkdir -p "$WORK/stl"
  : > "$WORK/inputs.render.tsv"
  for name in $RENDER_NAMES; do
    path="$WORK/stl/$name.stl"
    [ -s "$path" ] || continue
    for v in "${RENDER_VARIANTS[@]}"; do
      add_input render "$name@$v" "$path" stl stl "$v"
    done
  done
}

RENDER_NAMES=""
make_refs() {
  local stage label path fmt ext var sig attempt
  for stage in $STAGES; do
    if [ "$stage" = render ]; then
      mkdir -p "$WORK/stl"
      if [ "$RECHECK" != 1 ]; then
        cp "$W15_SRC" "$WORK/stl/w15_prefix.stl"
        RENDER_NAMES="$RENDER_NAMES w15_prefix"
      fi
      for label in stl_ascii_box; do cp "$CORPUS/synthetic/$label.stl" "$WORK/stl/$label.stl"; RENDER_NAMES="$RENDER_NAMES $label"; done
      if [ "$RECHECK" != 1 ]; then
        cp "$CORPUS/synthetic/stl_binary_solid_header.stl" "$WORK/stl/stl_binary_solid_header.stl"
        cp "$CORPUS/public/nasa-hubble-all-pieces.stl" "$WORK/stl/nasa_hubble.stl"
        RENDER_NAMES="$RENDER_NAMES stl_binary_solid_header nasa_hubble"
      fi
      build_render_inputs
    fi
    while IFS=$'\t' read -r -u 3 label path fmt ext var; do
      attempt=1
      while :; do
        ARGV=()
        stage_argv "$stage" "$fmt" "$ext" /job "$var"
        run_case - "$path" "$ext" "${ARGV[@]}"
        sig=$(signature "$stage" "$JOB")
        if [ "$sig" != missing ] && [ "$stage" = mesh ] && [ "$fmt" != stl ] && [ -s "$JOB/out.stl" ]; then
          mkdir -p "$WORK/stl"
          cp "$JOB/out.stl" "$WORK/stl/$label.stl"
          RENDER_NAMES="$RENDER_NAMES $label"
        fi
        if [ "$stage" = render ] && [ "$sig" != missing ]; then
          case "$sig" in
            *'"verdict":"ok"'*"|png="[0-9a-f]*) ;;
            *) emit "FAIL,stage=render,input=$label,reason=the unconfined reference render is not an ok frame, so a confined match would prove nothing: $(printf '%s' "$sig" | cut -c1-120 | tr ',' ';')"; exit 1 ;;
          esac
        fi
        rm -rf "$JOB"
        [ "$sig" = missing ] || break
        [ "$attempt" -lt 3 ] || { emit "FAIL,stage=$stage,input=$label,reason=the unconfined reference run printed no verdict after 3 attempts"; exit 1; }
        attempt=$((attempt + 1))
      done
      printf '%s' "$sig" > "$WORK/ref/$stage.$label"
      emit "REF,stage=$stage,input=$label,fmt=$fmt,verdict=$(verdict_of "$sig"),sig=$(short_sig "$sig"),attempts=$attempt"
    done 3< "$WORK/inputs.$stage.tsv"
  done
}

CONTENT=""
ATTEMPTS=0
CONF_RC=0
judge() {
  local ro=$1 stage=$2 label=$3 fmt=$4 ext=$5 infile=$6 var=${7:--} ref sig attempt=1
  ref=$(cat "$WORK/ref/$stage.$label")
  while [ "$attempt" -le 2 ]; do
    ARGV=()
    stage_argv "$stage" "$fmt" "$ext" /job "$var"
    run_case "$ro" "$infile" "$ext" "${ARGV[@]}"
    if probe_failure; then probe_failure_exit "$stage" "$label"; fi
    sig=$(signature "$stage" "$JOB")
    rm -rf "$JOB"
    CONF_RC=$RC
    ATTEMPTS=$attempt
    if [ "$sig" = "$ref" ]; then
      CONTENT=identical
      return 0
    fi
    if [ "$sig" = missing ]; then CONTENT=missing; else CONTENT=different; fi
    attempt=$((attempt + 1))
  done
}

ro_of() { paste -sd: "$1"; }
without() { grep -vxF "$2" "$1" || true; }

SUFF=0
DIFF=0
sufficiency() {
  local stage=$1 setname=$2 setfile=$3 ro label path fmt ext var
  ro=$(ro_of "$setfile")
  while IFS=$'\t' read -r -u 3 label path fmt ext var; do
    judge "$ro" "$stage" "$label" "$fmt" "$ext" "$path" "$var"
    emit "SUFFICIENT,stage=$stage,input=$label,set=$setname,result=$CONTENT,confined_rc=$CONF_RC,attempts=$ATTEMPTS"
    if [ "$CONTENT" = identical ]; then SUFF=$((SUFF + 1)); else DIFF=$((DIFF + 1)); fi
  done 3< "$WORK/inputs.$stage.tsv"
}

FIRST_CHANGE=""
first_change() {
  local stage=$1 setfile=$2 ro label path fmt ext var
  ro=$(ro_of "$setfile")
  FIRST_CHANGE=""
  while IFS=$'\t' read -r -u 3 label path fmt ext var; do
    judge "$ro" "$stage" "$label" "$fmt" "$ext" "$path" "$var"
    if [ "$CONTENT" != identical ]; then
      FIRST_CHANGE="$label:$CONTENT"
      return 0
    fi
  done 3< "$WORK/inputs.$stage.tsv"
}

LEAKED=0
EFFECT_DENIED=0
effect_case() {
  local stage=$1 setname=$2 setfile=$3 ro sig_u sig_c produced=no result why
  ro=$(ro_of "$setfile")
  rm -rf "$WORK/outside"
  mkdir -p "$WORK/outside"
  chmod 0755 "$WORK/outside"
  local fmt=step ext=step var=-
  if [ "$stage" = render ]; then
    fmt=stl ext=stl var=iso:1280:opaque
    cp "$WORK/stl/units_inch.stl" "$WORK/outside/in.stl"
    chmod 0644 "$WORK/outside/in.stl"
  else
    cp "$CORPUS/synthetic/units_inch.step" "$WORK/outside/in.step"
    chmod 0644 "$WORK/outside/in.step"
  fi
  EXTRA=(-v "$WORK/outside:/outside:ro")
  ARGV=()
  stage_argv "$stage" "$fmt" "$ext" /outside "$var"
  emit "ARGV,stage=$stage,input=outside/units_inch,argv=$(printf '%s ' "${ARGV[@]}" | sed 's/ $//' | tr ',' ';')"
  run_case - "" "$ext" "${ARGV[@]}"
  sig_u=$(signature "$stage" "$JOB")
  rm -rf "$JOB"
  case "$sig_u" in
    *'"verdict":"ok"'*) ;;
    *) emit "FAIL,stage=$stage,reason=the unconfined run on the outside copy did not print an ok verdict, so its denial would prove nothing"; exit 1 ;;
  esac
  run_case "$ro" "" "$ext" "${ARGV[@]}"
  if probe_failure; then probe_failure_exit "$stage" outside/units_inch; fi
  sig_c=$(signature "$stage" "$JOB")
  if [ "$stage" = render ]; then
    [ -s "$JOB/out.png" ] && produced=yes
  else
    [ -s "$JOB/out.stl" ] && produced=yes
  fi
  why=$(printf '%s' "$sig_c" | cut -c1-60 | tr ',' ';')
  rm -rf "$JOB"
  EXTRA=()
  result=denied
  case "$sig_c" in *'"verdict":"ok"'*) result=LEAKED ;; esac
  [ "$stage" = guard ] || [ "$produced" = no ] || result=LEAKED
  if [ "$result" = denied ]; then EFFECT_DENIED=$((EFFECT_DENIED + 1)); else LEAKED=$((LEAKED + 1)); fi
  emit "EFFECT,stage=$stage,set=$setname,input=outside/units_inch,unconfined=ok,result=$result,produced=$produced,confined_rc=$RC,canary=$CANARY,confined_signature=$why"
  [ "$result" = denied ] || exit 1
}

venv_effect() {
  local setfile=$1 ro unconf conf
  ro=$(ro_of "$setfile")
  run_case - "" step /usr/bin/python3 -I -B -c 'import sys
try:
    open("/opt/venv/pyvenv.cfg").read()
    print("READ")
except PermissionError:
    print("DENIED")'
  unconf=$(tr -d '\n' < "$RUN_OUT")
  rm -rf "$JOB"
  [ "$unconf" = READ ] || { emit "FAIL,stage=guard,reason=/opt/venv/pyvenv.cfg is not readable without a ruleset ($unconf)"; exit 1; }
  run_case "$ro" "" step /usr/bin/python3 -I -B -c 'import sys
try:
    open("/opt/venv/pyvenv.cfg").read()
    print("READ")
except PermissionError:
    print("DENIED")'
  if probe_failure; then probe_failure_exit guard venv; fi
  conf=$(tr -d '\n' < "$RUN_OUT")
  rm -rf "$JOB"
  if [ "$conf" = DENIED ]; then
    EFFECT_DENIED=$((EFFECT_DENIED + 1))
    emit "EFFECT,stage=guard,set=measured,target=/opt/venv/pyvenv.cfg,unconfined=READ,result=denied,confined_rc=$RC,canary=$CANARY"
  else
    LEAKED=$((LEAKED + 1))
    emit "EFFECT,stage=guard,set=measured,target=/opt/venv/pyvenv.cfg,unconfined=READ,result=LEAKED,confined=$conf"
    exit 1
  fi
}

COLLAPSE_PY='import re, sys
BROAD = {"/etc", "/usr/bin", "/usr/sbin", "/bin", "/sbin", "/usr/lib", "/lib", "/usr", "/", "/usr/share",
         "/var", "/dev", "/proc", "/sys", "/run", "/usr/local", "/usr/local/bin", "/opt"}
trip = sys.argv[2]
def entry(p):
    parts = p.split("/")[1:]
    if p.startswith("/opt/venv/"): return "/opt/venv"
    if p.startswith("/usr/lib/python3"): return "/usr/lib/" + parts[2]
    if p == "/usr/lib/" + trip or p.startswith("/usr/lib/" + trip + "/"): return None
    if p.startswith("/proc/") or p.startswith("/dev/"): return p
    if p.startswith("/sys/devices/system/cpu/"): return "/sys/devices/system/cpu"
    if p.startswith("/sys/"): return p
    if p.startswith("/usr/share/") and len(parts) > 3: return "/usr/share/" + parts[2]
    if p.startswith("/etc/") and len(parts) > 2: return "/etc/" + parts[1]
    if p.startswith("/usr/lib/") and len(parts) > 3: return "/usr/lib/" + parts[2]
    d = p.rsplit("/", 1)[0] or "/"
    return p if d in BROAD else d
entries, tmp = set(), set()
for line in open(sys.argv[1]):
    line = line.rstrip("\n")
    if not line.startswith("  /"):
        continue
    path = re.sub(r"^/proc/[0-9]+/", "/proc/self/", line.strip().split(" -> ")[-1])
    if path.startswith("/tmp/") or path == "/tmp":
        tmp.add(path)
        continue
    e = entry(path)
    if e is not None:
        entries.add(e)
for extra in sys.argv[3:]:
    entries.add(extra)
dirs = sorted(e for e in entries if not any(o != e and e.startswith(o + "/") for o in entries))
for e in dirs: print("ENTRY " + e)
for t in sorted(tmp): print("TMP " + t)'

engine_path() { printf '/usr/local/bin/cad-%s' "$1"; }
interp_of() { if [ "$1" = guard ]; then printf '%s' "$GUARD_INTERP"; else printf '%s' "$MESH_INTERP"; fi; }

trace_stage() {
  local stage=$1 idx=0 label path fmt ext var jobd n tree tmpn seen=""
  : > "$WORK/trace/$stage.all"
  while IFS=$'\t' read -r -u 3 label path fmt ext var; do
    if [ "$stage" = render ]; then
      case " $seen " in
        *" ${label%%@*} "*) [ "${label%%@*}" = units_inch ] || [ "${label%%@*}" = nasa_hubble ] || continue ;;
      esac
      seen="$seen ${label%%@*}"
    fi
    idx=$((idx + 1))
    jobd=$(newdir)
    cp "$path" "$jobd/in.$ext"
    ARGV=()
    stage_argv "$stage" "$fmt" "$ext" /job "$var"
    if ! scripts/sandbox-paths-measure.sh trace "$TAG" "$jobd" \
         env HOME=/job TMPDIR=/job LANG=C.UTF-8 LP_NUM_THREADS=2 "${ARGV[@]}" > "$WORK/trace/$stage.$idx" 2>> "$WORK/trace.err" </dev/null; then
      emit "FAIL,stage=$stage,input=$label,reason=trace failed: $(tail -n 2 "$WORK/trace.err" | tr '\n,' '  ')"
      exit 1
    fi
    rm -rf "$jobd"
    n=$(grep -c '^  /' "$WORK/trace/$stage.$idx" || true)
    if [ "$n" -lt 1 ]; then
      emit "FAIL,stage=$stage,input=$label,reason=the trace recorded no path"
      exit 1
    fi
    cat "$WORK/trace/$stage.$idx" >> "$WORK/trace/$stage.all"
    tree=$(grep -cE '^  (/opt/venv/|/usr/lib/python3)' "$WORK/trace/$stage.$idx" || true)
    emit "TRACE,stage=$stage,input=$label,paths=$n,python_tree_paths=$tree,list=$(grep '^  /' "$WORK/trace/$stage.$idx" | grep -vE "^  (/opt/venv/|/usr/lib/python3|/usr/lib/$NATIVE_TRIPLET/|/lib/$NATIVE_TRIPLET/)" | sed 's/^  //; s/ -> /=>/' | paste -sd: - | tr ',' ';')"
  done 3< "$WORK/inputs.$stage.tsv"
  python3 -c "$COLLAPSE_PY" "$WORK/trace/$stage.all" "$NATIVE_TRIPLET" "$(engine_path "$stage")" "$(interp_of "$stage")" "$MULTIARCH_DIR" > "$WORK/collapse.$stage"
  grep '^ENTRY ' "$WORK/collapse.$stage" | sed 's/^ENTRY //' > "$WORK/set.cand.$stage"
  tmpn=$(grep -c '^TMP ' "$WORK/collapse.$stage" || true)
  if [ "$tmpn" -gt 0 ]; then
    emit "TMP_NEED,stage=$stage,paths=$(grep '^TMP ' "$WORK/collapse.$stage" | sed 's/^TMP //' | paste -sd: -)"
  else
    emit "TMP_NEED,stage=$stage,none"
  fi
  emit "CANDIDATE,stage=$stage,rw=/job,ro=$(ro_of "$WORK/set.cand.$stage")"
}

REMOVAL_ROWS=0
removal_table() {
  local stage=$1 e
  : > "$WORK/set.meas.$stage"
  while IFS= read -r e; do
    [ -n "$e" ] || continue
    without "$WORK/set.cand.$stage" "$e" > "$WORK/set.try"
    first_change "$stage" "$WORK/set.try"
    REMOVAL_ROWS=$((REMOVAL_ROWS + 1))
    if [ -z "$FIRST_CHANGE" ]; then
      emit "REMOVAL,stage=$stage,entry=$e,result=removable,detail=every input identical without it"
    else
      emit "REMOVAL,stage=$stage,entry=$e,result=load-bearing,first_changed=$FIRST_CHANGE"
      printf '%s\n' "$e" >> "$WORK/set.meas.$stage"
    fi
  done < "$WORK/set.cand.$stage"
}

NEG=0
negative_control() {
  local stage=$1 drop
  drop=$(grep -E '^(/usr/lib/python|/opt/venv)' "$WORK/set.meas.$stage" | head -n 1 || true)
  [ -n "$drop" ] || drop=$(head -n 1 "$WORK/set.meas.$stage")
  without "$WORK/set.meas.$stage" "$drop" > "$WORK/set.neg"
  first_change "$stage" "$WORK/set.neg"
  if [ -n "$FIRST_CHANGE" ]; then
    NEG=$((NEG + 1))
    emit "NEGATIVE_CONTROL,stage=$stage,dropped=$drop,first_changed=$FIRST_CHANGE,result=failed_as_expected"
  else
    emit "NEGATIVE_CONTROL,stage=$stage,dropped=$drop,result=passed_unexpectedly"
    exit 1
  fi
}

measured_line() {
  local stage=$1 ro eng
  eng=$(engine_path "$stage")
  ro=$(grep -vxF "$eng" "$WORK/set.meas.$stage" | grep -vxF "$MULTIARCH_DIR" | paste -sd: -)
  emit "MEASURED,stage=$stage,ro=$ro,engines=cad-$stage,multiarch=$MULTIARCH_DIR"
  local ps
  ps=$(grep -E '^/(proc|sys|tmp)' "$WORK/set.meas.$stage" | paste -sd: - || true)
  emit "PROC_SYS_TMP,stage=$stage,entries=${ps:-none}"
}

measured_stage_set() {
  local stage=$1 line ro eng mult
  line=$(grep "^MEASURED,stage=$stage," "$MAIN_EVIDENCE" || true)
  [ "$(printf '%s\n' "$line" | grep -c .)" = 1 ] || { emit "FAIL,reason=$MAIN_EVIDENCE does not carry exactly one MEASURED line for $stage"; exit 1; }
  ro=$(printf '%s' "$line" | sed -n 's/^.*,ro=\([^,]*\),engines=.*$/\1/p')
  eng=$(printf '%s' "$line" | sed -n 's/^.*,engines=\([^,]*\),multiarch=.*$/\1/p')
  mult=$(printf '%s' "$line" | sed -n 's/^.*,multiarch=\(.*\)$/\1/p')
  [ -n "$ro" ] && [ -n "$eng" ] && [ -n "$mult" ] || { emit "FAIL,reason=the MEASURED line of $stage cannot be read"; exit 1; }
  [ "$mult" = "$MULTIARCH_DIR" ] || { emit "FAIL,reason=the measured multiarch directory $mult is not this image's $MULTIARCH_DIR"; exit 1; }
  { printf '%s\n' "$ro" | tr ':' '\n'; printf '/usr/local/bin/%s\n%s\n' "$eng" "$mult"; } > "$WORK/set.meas.$stage"
}

amd64_proof() {
  local tag_a arch entries job out_g out_m tri_a vg vm tri
  disk_guard
  tag_a="$TAG-amd64"
  stage_context "$WORK/stage-amd64"
  CREATED_TAGS+=("$tag_a")
  cad_log "building $tag_a (emulated, a build proof and not a Landlock measurement)"
  docker buildx build --platform linux/amd64 --target engine --load -f "$WORK/stage-amd64/Dockerfile.cad-worker" -t "$tag_a" "$WORK/stage-amd64" >&2 \
    || { emit "FAIL,reason=the amd64 build failed"; exit 1; }
  arch=$(docker image inspect "$tag_a" --format '{{.Architecture}}')
  [ "$arch" = amd64 ] || { emit "FAIL,reason=the amd64 build produced a $arch image"; exit 1; }
  entries=$(cat "$WORK/set.meas.guard" "$WORK/set.meas.mesh" | grep -vE '^/usr/lib/(aarch64|x86_64)-linux-gnu$' | LC_ALL=C sort -u)
  job=$(newdir)
  cp "$CORPUS/synthetic/units_inch.step" "$job/in.step"
  chmod 0644 "$job/in.step"
  out_g=$(docker run --rm --platform linux/amd64 --network none --user nobody -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -v "$job:/job" --entrypoint sh "$tag_a" -c '
      tri=$(ls -d /usr/lib/*-linux-gnu | sed "s#^/usr/lib/##" | paste -sd: -)
      printf "TRIPLET %s\n" "$tri"
      test -d /usr/lib/x86_64-linux-gnu || { echo "NO_TRIPLET_DIR"; exit 11; }
      n=0
      for p in "$@"; do
        test -e "$p" || { printf "MISSING %s\n" "$p"; exit 12; }
        n=$((n + 1))
      done
      printf "ENTRIES %s\n" "$n"
      /opt/venv/bin/python -I -B -c "import OCP.STEPControl, OCP.IGESControl, OCP.BRepMesh, OCP.StlAPI, OCP.BRepBndLib, OCP.Interface" || exit 13
      echo "SMOKE ok"
      /usr/local/bin/cad-guard --format step --max-entities '"$MAX_ENTITIES"' --max-nauo '"$MAX_NAUO"' /job/in.step 2>/dev/null | grep -a "^CADVERDICT " | tail -n 1
      /usr/local/bin/cad-mesh --format step /job/in.step /job/out.stl 2>/dev/null | grep -a "^CADVERDICT " | tail -n 1
    ' sh $entries </dev/null 2>"$WORK/amd64.err") || { emit "FAIL,reason=the amd64 proof script failed: $(printf '%s' "$out_g" | tr '\n,' '  ') $(tail -n 3 "$WORK/amd64.err" | tr '\n,' '  ')"; exit 1; }
  rm -rf "$job"
  tri_a=$(printf '%s\n' "$out_g" | sed -n 's/^TRIPLET //p')
  tri=$(printf '%s\n' "$out_g" | sed -n 's/^ENTRIES //p')
  vg=$(printf '%s\n' "$out_g" | grep -a '"stage":"guard"' | tail -n 1 || true)
  vm=$(printf '%s\n' "$out_g" | grep -a '"stage":"mesh"' | tail -n 1 || true)
  case "$vg" in *'"verdict":"ok"'*) ;; *) emit "FAIL,reason=cad-guard in the amd64 image did not print ok on units_inch.step: $vg"; exit 1 ;; esac
  case "$vm" in *'"verdict":"ok"'*'"triangles":12'*|*'"triangles":12'*'"verdict":"ok"'*) ;; *) emit "FAIL,reason=cad-mesh in the amd64 image did not give 12 triangles on units_inch.step: $vm"; exit 1 ;; esac
  [ "$tri_a" = x86_64-linux-gnu ] || { emit "FAIL,reason=the amd64 image carries $tri_a, not x86_64-linux-gnu"; exit 1; }
  AMD64_TRIPLET=$tri_a
  emit "AMD64,image_arch=amd64,server_arch=$SERVER_ARCH,image=$tag_a,kind=build_proof,landlock=not_measured,triplet_dir=present,multiarch=$tri_a,entries_present=$tri,smoke=ok,guard=ok,mesh_triangles=12,unconfined=1,emulated=1"
  docker rmi "$tag_a" >/dev/null 2>&1 || true
}

make_refs

if [ "$RECHECK" = 1 ]; then
  for s in $STAGES; do measured_stage_set "$s"; done
  RECHECKED=0
  for s in $STAGES; do
    ro=$(ro_of "$WORK/set.meas.$s")
    while IFS=$'\t' read -r -u 3 label path fmt ext var; do
      judge "$ro" "$s" "$label" "$fmt" "$ext" "$path" "$var"
      emit "RECHECK,stage=$s,input=$label,result=$CONTENT,confined_rc=$CONF_RC"
      [ "$CONTENT" = identical ] || exit 1
      RECHECKED=$((RECHECKED + 1))
    done 3< "$WORK/inputs.$s.tsv"
  done
  EXPECTED=0
  for s in $STAGES; do EXPECTED=$((EXPECTED + $(wc -l < "$WORK/inputs.$s.tsv" | tr -d ' '))); done
  [ "$RECHECKED" = "$EXPECTED" ] && [ "$RECHECKED" -gt 0 ] || { emit "FAIL,reason=the recheck covered $RECHECKED of $EXPECTED cases"; exit 1; }
  emit "RECHECK-OK,cases=$RECHECKED,image=$TAG"
  exit 0
fi

for s in $STAGES; do
  disk_guard
  trace_stage "$s"
done
for s in $STAGES; do
  sufficiency "$s" candidate "$WORK/set.cand.$s"
  [ "$DIFF" = 0 ] || { emit "FAIL,stage=$s,reason=the candidate set is not sufficient; add what is missing by hand"; exit 1; }
  effect_case "$s" candidate "$WORK/set.cand.$s"
done
for s in $STAGES; do
  removal_table "$s"
  sufficiency "$s" measured "$WORK/set.meas.$s"
  [ "$DIFF" = 0 ] || { emit "INTERACTION,stage=$s,detail=the load-bearing entries alone are not sufficient"; exit 1; }
  effect_case "$s" measured "$WORK/set.meas.$s"
  negative_control "$s"
  measured_line "$s"
done
venv_effect "$WORK/set.meas.guard"

if grep -qx '/opt/venv' "$WORK/set.meas.guard"; then
  emit "FAIL,stage=guard,reason=the guard set reaches /opt/venv: the stage that reads hostile bytes first must not reach OCP"
  exit 1
fi

AMD64_TRIPLET=""
if [ "$AMD64" = 1 ]; then
  amd64_proof
else
  AMD64_TRIPLET=$GO_AMD_DERIVED
  emit "AMD64,image_arch=amd64,status=deferred,landlock=not_measured,render=not_measured,note=the amd64 render Landlock set and the emulated amd64 build are not measured here; run with --amd64 for the guard and mesh build proof"
fi
GO_ARM=$(go_triplet arm64)
GO_AMD=$(go_triplet amd64)
if [ "$NATIVE_TRIPLET" = "$GO_ARM" ] && [ "$AMD64_TRIPLET" = "$GO_AMD" ]; then GO_MATCH=match; else GO_MATCH=MISMATCH; fi
emit "MULTIARCH,arm64=$NATIVE_TRIPLET,amd64=$AMD64_TRIPLET,amd64_status=derived,go_map=$GO_MATCH"
[ "$GO_MATCH" = match ] || { emit "FAIL,reason=cadMultiarchLib does not equal the observed triplets"; exit 1; }

PST=$(grep -hE '^/(proc|sys|tmp)' "$WORK/set.meas.guard" "$WORK/set.meas.mesh" | LC_ALL=C sort -u | paste -sd: - || true)
emit "SUMMARY,stages=3,inputs=$(cat "$WORK"/inputs.*.tsv | wc -l | tr -d ' '),sufficient=$SUFF,different=$DIFF,leaked=$LEAKED,effect_denied=$EFFECT_DENIED,negative_controls=$NEG,removal_rows=$REMOVAL_ROWS,proc_sys_tmp=${PST:-none}"

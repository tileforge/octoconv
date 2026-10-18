#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

usage() {
  printf '%s\n' \
    'Usage:' \
    '  scripts/cad-worker-sandbox-measure.sh [--stages LIST] [--amd64] [--out FILE]' \
    '  scripts/cad-worker-sandbox-measure.sh --recheck [--stages LIST] [--out FILE]' \
    'LIST is a comma-separated subset of guard,mesh,render,dxf,rsvg. The default guard,mesh,render is the 74-06 run and' \
    'writes evidence/74-06-landlock.txt; a list with dxf writes .planning/milestones/v1.16-phases/75-dxf-2d-pipeline/evidence/75-08-landlock.txt.' \
    'render needs mesh and rsvg needs dxf in the same list: their inputs are the outputs of those references.' \
    'Exit codes: 64 usage, 69 docker or go missing, 1 a non-identical result, a probe failure or an emulated image.' >&2
  exit 64
}

PHASE_EVID="$REPO_ROOT/.planning/milestones/v1.16-phases/74-3d-render-measurements/evidence"
DRAW_EVID="$REPO_ROOT/.planning/milestones/v1.16-phases/75-dxf-2d-pipeline/evidence"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
W15_SRC="$CAD_CACHE_DIR/scaled/stl-ascii-nasa-sls-block1-1MiB.stl"
W15_SHA=77492adbca35ff7f0c583732208324a834170aec9cb76eee7ad4e0b853c18ef0
GO_AMD_DERIVED=x86_64-linux-gnu
RENDER_VARIANTS=(iso:1280:opaque top:2048:opaque iso:1280:transparent)
GO_TOOLCHAIN=go1.26.4
MIN_FREE_KIB=6291456
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
ENVS=(-e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2)
IMAGE_PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
DRAW_ENVS=(-e "PATH=$IMAGE_PATH" -e TMPDIR=/job -e HOME=/job/.home -e XDG_CACHE_HOME=/job/.home/.cache -e XDG_CONFIG_HOME=/job/.home/.config)
DRAW_ENV_LINE="PATH=image;TMPDIR=/job;HOME=/job/.home;XDG_CACHE_HOME=/job/.home/.cache;XDG_CONFIG_HOME=/job/.home/.config;LANG=unset;LC_ALL=unset"
CANARY=/etc/hostname

OUT=""
RECHECK=0
AMD64=0
STAGE_LIST=guard,mesh,render
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=${2:?}; shift 2 ;;
    --amd64) AMD64=1; shift ;;
    --recheck) RECHECK=1; shift ;;
    --stages) STAGE_LIST=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    *) echo "error: unknown argument $1" >&2; usage ;;
  esac
done

for s in $(printf '%s' "$STAGE_LIST" | tr ',' ' '); do
  case "$s" in guard|mesh|render|dxf|rsvg) ;; *) echo "error: unknown stage $s" >&2; usage ;; esac
done
STAGES=""
for s in guard mesh render dxf rsvg; do
  case ",$STAGE_LIST," in *",$s,"*) STAGES="${STAGES:+$STAGES }$s" ;; esac
done
[ -n "$STAGES" ] || usage
has_stage() { case " $STAGES " in *" $1 "*) return 0 ;; esac; return 1; }
if has_stage render && ! has_stage mesh; then echo "error: render needs mesh in --stages" >&2; usage; fi
if has_stage rsvg && ! has_stage dxf; then echo "error: rsvg needs dxf in --stages" >&2; usage; fi
DRAW=0
has_stage dxf && DRAW=1
if [ "$DRAW" = 1 ]; then
  MAIN_EVIDENCE="$DRAW_EVID/75-08-landlock.txt"
  TITLE="# Plan 75-08: Landlock sets of the $(printf '%s' "$STAGES" | sed 's/ /, /g') stages, measured on the production engine image"
else
  MAIN_EVIDENCE="$PHASE_EVID/74-06-landlock.txt"
  TITLE="# Plan 74-06: Landlock sets of the cad-guard, cad-mesh and cad-render stages, measured on the production engine image"
fi
if [ -z "$OUT" ]; then
  if [ "$RECHECK" = 1 ]; then OUT="${TMPDIR:-/tmp}/$(basename "$MAIN_EVIDENCE" .txt)-recheck.txt"; else OUT=$MAIN_EVIDENCE; fi
fi

command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
command -v go >/dev/null 2>&1 || { echo "error: go is required to build the probe" >&2; exit 69; }
command -v python3 >/dev/null 2>&1 || { echo "error: python3 is required" >&2; exit 69; }

go_const() { sed -n "s/^[[:space:]]*$1[[:space:]]*=[[:space:]]*\([0-9][0-9.e+]*\).*/\1/p" "$2"; }
MAX_ENTITIES=$(go_const CADMaxSTEPEntities internal/convert/cad.go)
MAX_NAUO=$(go_const CADMaxNAUO internal/convert/cad.go)
[ -n "$MAX_ENTITIES" ] && [ -n "$MAX_NAUO" ] || { echo "error: the guard caps are not in internal/convert/cad.go" >&2; exit 1; }
DXF_INSERTS=$(go_const CADMaxDXFExpandedInserts internal/convert/cad.go)
DXF_ENTITIES=$(go_const CADMaxDXFExpandedEntities internal/convert/cad.go)
DXF_NESTING=$(go_const CADMaxDXFNesting internal/convert/cad.go)
DXF_MAX_ABS=$(printf '%g' "$(go_const MaxAbsCoordinate internal/measurements/measurements.go)")
if [ "$DRAW" = 1 ]; then
  [ -n "$DXF_INSERTS" ] && [ -n "$DXF_ENTITIES" ] && [ -n "$DXF_NESTING" ] && [ "$DXF_MAX_ABS" = 1e+12 ] \
    || { echo "error: the DXF caps are not in internal/convert/cad.go and internal/measurements/measurements.go" >&2; exit 1; }
  SVG_MAX_MIB=$(sed -n 's/^[[:space:]]*CADMaxDXFSVGBytes[[:space:]]*=[[:space:]]*\([0-9][0-9]*\) << 20$/\1/p' internal/convert/cad.go)
  [ -n "$SVG_MAX_MIB" ] || { echo "error: CADMaxDXFSVGBytes is not N << 20 in internal/convert/cad.go" >&2; exit 1; }
  SVG_MAX=$((SVG_MAX_MIB << 20))
fi

go_set() {
  awk -v v="$1" '$0 ~ "^var " v " = \\[\\]string\\{" {on = 1; next} on && /^}/ {exit} on {gsub(/[",[:space:]]/, ""); if ($0 != "") print}' internal/convert/cadsandbox.go
}

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
mkdir -p "$WORK/probe" "$WORK/ref" "$WORK/trace" "$WORK/stage" "$WORK/tools" "$WORK/svg"
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

context_files() {
  printf '%s\n' Dockerfile.cad-worker cmd/cad-worker/vtk_keeplist.py cmd/cad-worker/vtk-keeplist.txt
  ls cmd/cad-worker/requirements-*.txt cmd/cad-worker/helper/*
}

inputs_short() {
  context_files | LC_ALL=C sort | while IFS= read -r f; do shasum -a 256 "$f"; done \
    | awk '{print $1}' | shasum -a 256 | cut -c1-12
}

stage_context() {
  local dir=$1 f
  mkdir -p "$dir/cmd/cad-worker/helper"
  for f in $(context_files); do cp "$f" "$dir/$f"; done
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
if [ "$DRAW" = 1 ]; then
  for t in cad-svg-check cad-pdf-check cad-pixel-judge; do
    GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$WORK/tools/$t" "./scripts/cmd/$t"
  done
fi

IMAGE_FACTS=$(docker run --rm --network none --entrypoint sh "$TAG" -c '
  printf "kernel=%s\n" "$(uname -r)"
  printf "python=%s\n" "$(/opt/venv/bin/python --version 2>&1)"
  printf "ocp=%s\n" "$(/opt/venv/bin/python -I -B -c "import importlib.metadata as m; print(m.version(\"cadquery-ocp-novtk\"))")"
  printf "ezdxf=%s\n" "$(/opt/venv/bin/python -I -B -c "import importlib.metadata as m; print(m.version(\"ezdxf\"))" 2>/dev/null || echo none)"
  printf "librsvg2_bin=%s\n" "$(dpkg-query -W -f="\${Version}" librsvg2-bin 2>/dev/null || echo none)"
  printf "fonts_dejavu_core=%s\n" "$(dpkg-query -W -f="\${Version}" fonts-dejavu-core 2>/dev/null || echo none)"
  printf "guard_interp=%s\n" "$(readlink -f /usr/bin/python3)"
  printf "mesh_interp=%s\n" "$(readlink -f /opt/venv/bin/python)"
  printf "rsvg_engine=%s\n" "$(readlink -f /usr/bin/rsvg-convert 2>/dev/null || echo none)"
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

tracked_dirt() {
  git status --porcelain --untracked-files=no | awk '{print $2}' | grep -vxF "${OUT#"$REPO_ROOT"/}" || true
}

emit "$TITLE"
emit "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
emit "HDR,commit=$(git rev-parse HEAD),tracked_dirt=$(tracked_dirt | grep -c . || true),dirty=$(tracked_dirt | paste -sd ';' -),script_sha256=$(shasum -a 256 scripts/cad-worker-sandbox-measure.sh | cut -c1-64),stages=$(printf '%s' "$STAGES" | tr ' ' '+')"
emit "HDR,server_arch=$SERVER_ARCH,image_arch=$IMAGE_ARCH,image=$TAG"
emit "HDR,image_id=$(docker image inspect "$TAG" --format '{{.Id}}'),user=$(docker image inspect "$TAG" --format '{{.Config.User}}')"
emit "HDR,kernel=$(fact kernel)"
emit "HDR,python=$(fact python),ocp=$(fact ocp)"
emit "HDR,guard_interp=$GUARD_INTERP,mesh_interp=$MESH_INTERP,multiarch=$NATIVE_TRIPLET"
emit "HDR,cpus=2,memory=4g,network=none,env=HOME=/job;LANG=C.UTF-8;TMPDIR=/job;LP_NUM_THREADS=2;PATH=image,rw=/job,canary=$CANARY"
if [ "$DRAW" = 1 ]; then
  emit "HDR,ezdxf=$(fact ezdxf),librsvg2_bin=$(fact librsvg2_bin),fonts_dejavu_core=$(fact fonts_dejavu_core),rsvg_engine=$(fact rsvg_engine)"
  emit "HDR,draw_stages=dxf+rsvg,env=$DRAW_ENV_LINE,home_dirs=.home+.home/.cache+.home/.config,also_present=HOSTNAME;DEBIAN_FRONTEND(image);PROBE_RO;PROBE_RW(probe only),rw=/job,canary=$CANARY"
  emit "HDR,guard_dxf_argv=--format dxf --max-expanded-inserts $DXF_INSERTS --max-expanded-entities $DXF_ENTITIES --max-nesting $DXF_NESTING --max-abs-coordinate $DXF_MAX_ABS,dxf_argv=--max-drawn-entities $DXF_ENTITIES --texts /job/texts.json --render-svg /job/draw.render.svg --measure /job/measure.json /job/in.dxf /job/draw.svg"
fi
emit "HDR,go=$(GOTOOLCHAIN=$GO_TOOLCHAIN go version),host_noise=$(docker ps -q | wc -l | tr -d ' ')"

for s in $STAGES; do : > "$WORK/inputs.$s.tsv"; done
add_input() { has_stage "$1" || return 0; printf '%s\t%s\t%s\t%s\t%s\n' "$2" "$3" "$4" "$5" "${6:--}" >> "$WORK/inputs.$1.tsv"; }
add_cad_input() {
  add_input guard "$@"
  add_input mesh "$@"
}
add_dxf_input() {
  local label=$1
  shift
  add_input guard "dxf_$label" "$@"
  add_input dxf "$label" "$@"
}
GUARD_DXF_CORPUS="public/3dsolids public/hatches_1 public/houses_of_parliament_georeferenced synthetic/blocks_minsert synthetic/comments_999"
DXF_STAGE_INPUTS="dxf/text_cases synthetic/legacy_cp1251 synthetic/legacy_latin1 public/columns_R2007 public/uncommon
  dxf/never_resolved dxf/layers_off_frozen public/hatches_2 public/acad_table_with_blk_ref public/dimension_in_nested_blocks"
if [ "$RECHECK" = 1 ]; then
  add_cad_input units_inch "$CORPUS/synthetic/units_inch.step" step step
  add_cad_input surfaces_only "$CORPUS/synthetic/surfaces_only.iges" iges iges
  add_input mesh stl_ascii_box "$CORPUS/synthetic/stl_ascii_box.stl" stl stl
  if [ "$DRAW" = 1 ]; then
    add_dxf_input legacy_cp1251 "$CORPUS/synthetic/legacy_cp1251.dxf" dxf dxf
    add_dxf_input text_cases "$CORPUS/dxf/text_cases.dxf" dxf dxf
  fi
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
  if [ "$DRAW" = 1 ]; then
    for f in $DXF_STAGE_INPUTS; do add_dxf_input "${f#*/}" "$CORPUS/$f.dxf" dxf dxf; done
    for f in $GUARD_DXF_CORPUS; do add_input guard "dxf_${f#*/}" "$CORPUS/$f.dxf" dxf dxf; done
    for f in "$CORPUS"/dxf/*.dxf; do
      case " $DXF_STAGE_INPUTS " in *" dxf/$(basename "$f" .dxf) "*) continue ;; esac
      add_input guard "fixture_$(basename "$f" .dxf)" "$f" dxf dxf
    done
  fi
fi
check_inputs() {
  local label path fmt ext var stage dup
  for stage in guard mesh dxf; do
    has_stage "$stage" || continue
    dup=$(cut -f1 "$WORK/inputs.$stage.tsv" | sort | uniq -d | paste -sd ';' -)
    [ -z "$dup" ] || { emit "FAIL,stage=$stage,reason=duplicate input labels $dup"; exit 1; }
    while IFS=$'\t' read -r -u 3 label path fmt ext var; do
      [ -f "$path" ] || { emit "FAIL,input=$label,reason=$path is missing; run scripts/cad-corpus-fetch.sh"; exit 1; }
    done 3< "$WORK/inputs.$stage.tsv"
  done
}
check_inputs
if [ "$RECHECK" != 1 ] && has_stage render; then
  [ -f "$W15_SRC" ] && [ "$(shasum -a 256 < "$W15_SRC" | cut -c1-64)" = "$W15_SHA" ] \
    || { emit "FAIL,input=w15,reason=$W15_SRC is missing or is not the 71 ladder slice sha256 $W15_SHA"; exit 1; }
fi

in_name() { if [ "$1" = rsvg ]; then printf 'clean.svg'; else printf 'in.%s' "$2"; fi; }

ARGV=()
stage_argv() {
  local stage=$1 fmt=$2 ext=$3 dir=$4 var=${5:--} view size bg kind sw sh left top
  case "$stage" in
    guard)
      if [ "$fmt" = dxf ]; then
        ARGV=(/usr/local/bin/cad-guard --format dxf --max-expanded-inserts "$DXF_INSERTS" --max-expanded-entities "$DXF_ENTITIES"
          --max-nesting "$DXF_NESTING" --max-abs-coordinate "$DXF_MAX_ABS" "$dir/in.$ext")
      else
        ARGV=(/usr/local/bin/cad-guard --format "$fmt" --max-entities "$MAX_ENTITIES" --max-nauo "$MAX_NAUO" "$dir/in.$ext")
      fi ;;
    mesh)
      if [ "$fmt" = stl ]; then
        ARGV=(/usr/local/bin/cad-mesh stats --measure /job/measure.json "$dir/in.$ext" /job/mesh.stl)
      else
        ARGV=(/usr/local/bin/cad-mesh --format "$fmt" "$dir/in.$ext" /job/out.stl --quality standard --measure /job/measure.json)
      fi ;;
    render)
      IFS=: read -r view size bg <<< "$var"
      ARGV=(/usr/local/bin/cad-render --view "$view" --size "$size" --background "$bg" "$dir/in.$ext" /job/out.png) ;;
    dxf)
      ARGV=(/usr/local/bin/cad-dxf --max-drawn-entities "$DXF_ENTITIES" --texts /job/texts.json --render-svg /job/draw.render.svg
        --measure /job/measure.json "$dir/in.$ext" /job/draw.svg) ;;
    rsvg)
      IFS=: read -r kind size bg sw sh left top <<< "$var"
      if [ "$kind" = pdf ]; then
        ARGV=(/usr/bin/rsvg-convert -f pdf -b white -o /job/out.pdf "$dir/clean.svg")
      else
        ARGV=(/usr/bin/rsvg-convert)
        if [ "$bg" = opaque ]; then ARGV+=(-b white); fi
        ARGV+=(-w "$sw" -h "$sh" --page-width "$size" --page-height "$size" --left "$left" --top "$top" -o /job/out.png "$dir/clean.svg")
      fi ;;
  esac
}

newdir() {
  local d
  d=$(mktemp -d "$WORK/job.XXXXXX")
  chmod 0777 "$d"
  case "${1:-}" in
    dxf|rsvg)
      mkdir -p "$d/.home/.cache" "$d/.home/.config"
      chmod 0777 "$d/.home" "$d/.home/.cache" "$d/.home/.config" ;;
  esac
  printf '%s\n' "$d"
}

RUN_ENVS=()
stage_env() {
  case "$1" in
    dxf|rsvg) RUN_ENVS=("${DRAW_ENVS[@]}") ;;
    *) RUN_ENVS=("${ENVS[@]}") ;;
  esac
}

EXTRA=()
JOB=""
RC=0
run_case() {
  local stage=$1 ro=$2 infile=$3 ext=$4 job rc=0 name
  shift 4
  job=$(newdir "$stage")
  stage_env "$stage"
  name=$(in_name "$stage" "$ext")
  if [ "$ro" = "-" ]; then
    docker run --rm "${LIMITS[@]}" "${RUN_ENVS[@]}" ${EXTRA[@]+"${EXTRA[@]}"} \
      -v "$job:/job" ${infile:+-v "$infile:/job/$name:ro"} "$TAG" "$@" > "$RUN_OUT" 2> "$RUN_ERR" </dev/null || rc=$?
  else
    docker run --rm "${LIMITS[@]}" "${RUN_ENVS[@]}" -e "PROBE_RO=$ro" -e PROBE_RW=/job \
      --entrypoint /probe/sandbox-probe -v "$WORK/probe:/probe:ro" ${EXTRA[@]+"${EXTRA[@]}"} \
      -v "$job:/job" ${infile:+-v "$infile:/job/$name:ro"} "$TAG" "$@" > "$RUN_OUT" 2> "$RUN_ERR" </dev/null || rc=$?
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

sha_or_none() { if [ -s "$1" ]; then shasum -a 256 < "$1" | cut -c1-64; else echo none; fi; }
pdf_sha() { perl -0777 -pe 's{/CreationDate \(D:[^)]*\)}{}g' "$1" | shasum -a 256 | cut -c1-64; }

signature() {
  local stage=$1 job=$2 v s m r t n txt
  if [ "$stage" = rsvg ]; then
    if [ -s "$job/out.pdf" ]; then
      txt=$("$WORK/tools/cad-pdf-check" --print-text "$job/out.pdf" 2>/dev/null || true)
      printf 'pdf=%s|pdf_text=%s|text_bytes=%s\n' "$(pdf_sha "$job/out.pdf")" \
        "$(printf '%s' "$txt" | shasum -a 256 | cut -c1-64)" "$(printf '%s' "$txt" | wc -c | tr -d ' ')"
    elif [ -s "$job/out.png" ]; then
      printf 'png=%s\n' "$(sha_or_none "$job/out.png")"
    else
      echo missing
    fi
    return 0
  fi
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
    dxf)
      s=$(sha_or_none "$job/draw.svg"); r=$(sha_or_none "$job/draw.render.svg")
      t=$(sha_or_none "$job/texts.json"); m=$(sha_or_none "$job/measure.json")
      n=0
      if [ -s "$job/draw.svg" ]; then n=$(grep -o '<text[ />]' "$job/draw.svg" | wc -l | tr -d ' '); fi
      printf '%s|svg=%s|render_svg=%s|texts=%s|measure=%s|text_elements=%s\n' "$v" "$s" "$r" "$t" "$m" "$n" ;;
    *) printf '%s\n' "$v" ;;
  esac
}

verdict_of() { printf '%s' "$1" | sed -n 's/.*"verdict":"\([a-z_]*\)".*/\1/p'; }
reason_of() { printf '%s' "$1" | sed -n 's/.*"reason":"\([a-z_]*\)".*/\1/p'; }
sig_field() { printf '%s' "$2" | tr '|' '\n' | sed -n "s/^$1=//p" | head -n 1; }
short_sig() { printf '%s' "$1" | shasum -a 256 | cut -c1-16; }
sig_ok() {
  case "$1" in
    rsvg) [ "$2" != missing ] ;;
    *) case "$2" in *'"verdict":"ok"'*) return 0 ;; esac; return 1 ;;
  esac
}

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

letterbox_of() {
  "$WORK/tools/cad-pixel-judge" --print-letterbox --svg "$1" --size "$2" \
    | sed -n 's/^LETTERBOX,sw=\([0-9]*\),sh=\([0-9]*\),left=\([0-9]*\),top=\([0-9]*\)$/\1:\2:\3:\4/p'
}

SVG_NAMES=""
build_rsvg_inputs() {
  local name svg lb1 lb2
  : > "$WORK/inputs.rsvg.tsv"
  for name in $SVG_NAMES; do
    svg="$WORK/svg/$name.svg"
    lb1=$(letterbox_of "$svg" 1280)
    lb2=$(letterbox_of "$svg" 2048)
    [ -n "$lb1" ] && [ -n "$lb2" ] || { emit "FAIL,stage=rsvg,input=$name,reason=no Go letterbox for the validated SVG"; exit 1; }
    add_input rsvg "$name@pdf" "$svg" svg svg "pdf:0:white:0:0:0:0"
    add_input rsvg "$name@png:1280:opaque" "$svg" svg svg "png:1280:opaque:$lb1"
    add_input rsvg "$name@png:2048:transparent" "$svg" svg svg "png:2048:transparent:$lb2"
  done
}

keep_svg() {
  local label=$1 job=$2 res
  res=$("$WORK/tools/cad-svg-check" --max-bytes "$SVG_MAX" --out "$WORK/svg/$label.svg" "$job/draw.render.svg" 2>&1 || true)
  case "$res" in
    GOSVG,*,ok=1,*) SVG_NAMES="$SVG_NAMES $label" ;;
    *) emit "FAIL,stage=dxf,input=$label,reason=the render-only SVG of the reference fails the Go validator: $(printf '%s' "$res" | cut -c1-160 | tr ',' ';')"; exit 1 ;;
  esac
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
    [ "$stage" != rsvg ] || build_rsvg_inputs
    while IFS=$'\t' read -r -u 3 label path fmt ext var; do
      attempt=1
      while :; do
        ARGV=()
        stage_argv "$stage" "$fmt" "$ext" /job "$var"
        run_case "$stage" - "$path" "$ext" "${ARGV[@]}"
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
        if [ "$stage" = dxf ] && [ "$sig" != missing ]; then
          case "$sig" in
            *'"verdict":"ok"'*) [ -s "$JOB/draw.render.svg" ] && [ -s "$JOB/draw.svg" ] \
                || { emit "FAIL,stage=dxf,input=$label,reason=the unconfined reference printed ok without both SVGs"; exit 1; }
              keep_svg "$label" "$JOB" ;;
            *) emit "FAIL,stage=dxf,input=$label,reason=the unconfined reference drawing is not ok, so a confined match would prove nothing: $(printf '%s' "$sig" | cut -c1-160 | tr ',' ';')"; exit 1 ;;
          esac
        fi
        rm -rf "$JOB"
        [ "$sig" = missing ] || break
        [ "$attempt" -lt 3 ] || { emit "FAIL,stage=$stage,input=$label,reason=the unconfined reference run printed no verdict after 3 attempts"; exit 1; }
        attempt=$((attempt + 1))
      done
      printf '%s' "$sig" > "$WORK/ref/$stage.$label"
      case "$stage" in
        rsvg) emit "REF,stage=$stage,input=$label,fmt=$fmt,output=$(printf '%s' "$sig" | cut -d= -f1),text_bytes=$(sig_field text_bytes "$sig"),sig=$(short_sig "$sig"),attempts=$attempt" ;;
        dxf) emit "REF,stage=$stage,input=$label,fmt=$fmt,verdict=$(verdict_of "$sig"),text_elements=$(sig_field text_elements "$sig"),sig=$(short_sig "$sig"),attempts=$attempt" ;;
        *) emit "REF,stage=$stage,input=$label,fmt=$fmt,verdict=$(verdict_of "$sig"),sig=$(short_sig "$sig"),attempts=$attempt" ;;
      esac
    done 3< "$WORK/inputs.$stage.tsv"
  done
}

CONTENT=""
ATTEMPTS=0
CONF_RC=0
LAST_SIG=""
judge() {
  local ro=$1 stage=$2 label=$3 fmt=$4 ext=$5 infile=$6 var=${7:--} ref sig attempt=1
  ref=$(cat "$WORK/ref/$stage.$label")
  while [ "$attempt" -le 2 ]; do
    ARGV=()
    stage_argv "$stage" "$fmt" "$ext" /job "$var"
    run_case "$stage" "$ro" "$infile" "$ext" "${ARGV[@]}"
    if probe_failure; then probe_failure_exit "$stage" "$label"; fi
    sig=$(signature "$stage" "$JOB")
    LAST_SIG=$sig
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

out_file() {
  case "$1" in
    render) printf 'out.png' ;;
    dxf) printf 'draw.svg' ;;
    rsvg) printf 'out.pdf' ;;
    *) printf 'out.stl' ;;
  esac
}

LEAKED=0
EFFECT_DENIED=0
effect_case() {
  local stage=$1 setname=$2 setfile=$3 ro sig_u sig_c produced=no result why fmt ext var src label name
  ro=$(ro_of "$setfile")
  rm -rf "$WORK/outside"
  mkdir -p "$WORK/outside"
  chmod 0755 "$WORK/outside"
  case "$stage" in
    render) fmt=stl ext=stl var=iso:1280:opaque src="$WORK/stl/units_inch.stl" label=units_inch ;;
    dxf) fmt=dxf ext=dxf var=- src="$CORPUS/synthetic/legacy_cp1251.dxf" label=legacy_cp1251 ;;
    rsvg) fmt=svg ext=svg var=pdf:0:white:0:0:0:0 src="$WORK/svg/legacy_cp1251.svg" label=legacy_cp1251 ;;
    *) fmt=step ext=step var=- src="$CORPUS/synthetic/units_inch.step" label=units_inch ;;
  esac
  name=$(in_name "$stage" "$ext")
  cp "$src" "$WORK/outside/$name"
  chmod 0644 "$WORK/outside/$name"
  EXTRA=(-v "$WORK/outside:/outside:ro")
  ARGV=()
  stage_argv "$stage" "$fmt" "$ext" /outside "$var"
  emit "ARGV,stage=$stage,input=outside/$label,argv=$(printf '%s ' "${ARGV[@]}" | sed 's/ $//' | tr ',' ';')"
  run_case "$stage" - "" "$ext" "${ARGV[@]}"
  sig_u=$(signature "$stage" "$JOB")
  rm -rf "$JOB"
  sig_ok "$stage" "$sig_u" \
    || { emit "FAIL,stage=$stage,reason=the unconfined run on the outside copy did not succeed, so its denial would prove nothing"; exit 1; }
  run_case "$stage" "$ro" "" "$ext" "${ARGV[@]}"
  if probe_failure; then probe_failure_exit "$stage" "outside/$label"; fi
  sig_c=$(signature "$stage" "$JOB")
  [ -s "$JOB/$(out_file "$stage")" ] && produced=yes
  if [ "$stage" = rsvg ]; then
    why=$(head -n 1 "$RUN_ERR" | cut -c1-80 | tr ',' ';')
  else
    why=$(printf '%s' "$sig_c" | cut -c1-60 | tr ',' ';')
  fi
  rm -rf "$JOB"
  EXTRA=()
  result=denied
  case "$sig_c" in *'"verdict":"ok"'*) result=LEAKED ;; esac
  [ "$stage" = guard ] || [ "$produced" = no ] || result=LEAKED
  if [ "$result" = denied ]; then EFFECT_DENIED=$((EFFECT_DENIED + 1)); else LEAKED=$((LEAKED + 1)); fi
  emit "EFFECT,stage=$stage,set=$setname,input=outside/$label,unconfined=ok,result=$result,produced=$produced,confined_rc=$RC,canary=$CANARY,confined_signature=$why"
  [ "$result" = denied ] || exit 1
}

venv_effect() {
  local setfile=$1 ro unconf conf
  ro=$(ro_of "$setfile")
  run_case guard - "" step /usr/bin/python3 -I -B -c 'import sys
try:
    open("/opt/venv/pyvenv.cfg").read()
    print("READ")
except PermissionError:
    print("DENIED")'
  unconf=$(tr -d '\n' < "$RUN_OUT")
  rm -rf "$JOB"
  [ "$unconf" = READ ] || { emit "FAIL,stage=guard,reason=/opt/venv/pyvenv.cfg is not readable without a ruleset ($unconf)"; exit 1; }
  run_case guard "$ro" "" step /usr/bin/python3 -I -B -c 'import sys
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
    if extra:
        entries.add(extra)
dirs = sorted(e for e in entries if not any(o != e and e.startswith(o + "/") for o in entries))
for e in dirs: print("ENTRY " + e)
for t in sorted(tmp): print("TMP " + t)'

engine_path() {
  case "$1" in
    rsvg) printf '/usr/bin/rsvg-convert' ;;
    *) printf '/usr/local/bin/cad-%s' "$1" ;;
  esac
}
engine_name() { basename "$(engine_path "$1")"; }
interp_of() {
  case "$1" in
    guard) printf '%s' "$GUARD_INTERP" ;;
    rsvg) : ;;
    *) printf '%s' "$MESH_INTERP" ;;
  esac
}
seed_set() {
  case "$1" in
    dxf) go_set cadDXFReadOnly ;;
    rsvg) go_set cadRsvgReadOnly ;;
  esac
}

TRACE_ENV=()
trace_env() {
  case "$1" in
    dxf|rsvg) TRACE_ENV=(-i "PATH=$IMAGE_PATH" TMPDIR=/job HOME=/job/.home XDG_CACHE_HOME=/job/.home/.cache XDG_CONFIG_HOME=/job/.home/.config) ;;
    *) TRACE_ENV=(HOME=/job TMPDIR=/job LANG=C.UTF-8 LP_NUM_THREADS=2) ;;
  esac
}

trace_stage() {
  local stage=$1 idx=0 label path fmt ext var jobd n tree tmpn seen="" extras wrap
  : > "$WORK/trace/$stage.all"
  while IFS=$'\t' read -r -u 3 label path fmt ext var; do
    if [ "$stage" = render ]; then
      case " $seen " in
        *" ${label%%@*} "*) [ "${label%%@*}" = units_inch ] || [ "${label%%@*}" = nasa_hubble ] || continue ;;
      esac
      seen="$seen ${label%%@*}"
    fi
    if [ "$stage" = rsvg ]; then
      case "$label" in
        *@pdf) ;;
        text_cases@*|uncommon@*) ;;
        *) continue ;;
      esac
    fi
    idx=$((idx + 1))
    jobd=$(newdir "$stage")
    cp "$path" "$jobd/$(in_name "$stage" "$ext")"
    ARGV=()
    stage_argv "$stage" "$fmt" "$ext" /job "$var"
    trace_env "$stage"
    wrap=()
    if [ "$stage" != rsvg ] && [ "$(verdict_of "$(cat "$WORK/ref/$stage.$label")")" != ok ]; then
      wrap=(/bin/sh -c '"$@"; exit 0' sh)
    fi
    if ! scripts/sandbox-paths-measure.sh trace "$TAG" "$jobd" \
         ${wrap[@]+"${wrap[@]}"} env "${TRACE_ENV[@]}" "${ARGV[@]}" > "$WORK/trace/$stage.$idx" 2>> "$WORK/trace.err" </dev/null; then
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
    emit "TRACE,stage=$stage,input=$label,paths=$n,python_tree_paths=$tree,exit_masked_by_sh=$([ "${#wrap[@]}" -gt 0 ] && echo 1 || echo 0),list=$(grep '^  /' "$WORK/trace/$stage.$idx" | grep -vE "^  (/opt/venv/|/usr/lib/python3|/usr/lib/$NATIVE_TRIPLET/|/lib/$NATIVE_TRIPLET/)" | sed 's/^  //; s/ -> /=>/' | paste -sd: - | tr ',' ';')"
  done 3< "$WORK/inputs.$stage.tsv"
  extras=$(seed_set "$stage" | paste -sd ' ' -)
  python3 -c "$COLLAPSE_PY" "$WORK/trace/$stage.all" "$NATIVE_TRIPLET" "$(engine_path "$stage")" "$(interp_of "$stage")" "$MULTIARCH_DIR" $extras > "$WORK/collapse.$stage"
  grep '^ENTRY ' "$WORK/collapse.$stage" | sed 's/^ENTRY //' > "$WORK/set.cand.$stage"
  tmpn=$(grep -c '^TMP ' "$WORK/collapse.$stage" || true)
  if [ "$tmpn" -gt 0 ]; then
    emit "TMP_NEED,stage=$stage,paths=$(grep '^TMP ' "$WORK/collapse.$stage" | sed 's/^TMP //' | paste -sd: -)"
  else
    emit "TMP_NEED,stage=$stage,none"
  fi
  if [ -n "$extras" ]; then
    emit "SEED,stage=$stage,source=internal/convert/cadsandbox.go,ro=$(printf '%s' "$extras" | tr ' ' ':')"
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

font_negative_control() {
  local stage=$1 drop=/usr/share/fonts ro label path fmt ext var ref tested=0 dropped=0 first="" bad="" got expect
  if ! grep -qxF "$drop" "$WORK/set.meas.$stage"; then
    emit "NEGATIVE_CONTROL,stage=$stage,dropped=$drop,result=not_in_measured_set"
    exit 1
  fi
  without "$WORK/set.meas.$stage" "$drop" > "$WORK/set.neg"
  ro=$(ro_of "$WORK/set.neg")
  if [ "$stage" = dxf ]; then expect=verdict_failed_font_unavailable_and_no_svg; else expect=pdf_text_empty; fi
  while IFS=$'\t' read -r -u 3 label path fmt ext var; do
    ref=$(cat "$WORK/ref/$stage.$label")
    if [ "$stage" = dxf ]; then
      [ "$(sig_field text_elements "$ref")" -gt 0 ] || continue
    else
      case "$label" in *@pdf) ;; *) continue ;; esac
      [ "$(sig_field text_bytes "$ref")" -gt 0 ] || continue
    fi
    tested=$((tested + 1))
    judge "$ro" "$stage" "$label" "$fmt" "$ext" "$path" "$var"
    if [ "$stage" = dxf ]; then
      got="$(verdict_of "$LAST_SIG")/$(reason_of "$LAST_SIG")/svg=$(sig_field svg "$LAST_SIG")"
      [ "$got" = failed/font_unavailable/svg=none ] && dropped=$((dropped + 1)) || bad="${bad:+$bad;}$label=$got"
    else
      got="text_bytes=$(sig_field text_bytes "$LAST_SIG")"
      [ "$CONTENT" = different ] && [ "$got" = text_bytes=0 ] && dropped=$((dropped + 1)) || bad="${bad:+$bad;}$label=$CONTENT/$got"
    fi
    [ -n "$first" ] || first="$label:$CONTENT:$got"
  done 3< "$WORK/inputs.$stage.tsv"
  if [ "$tested" -gt 0 ] && [ "$dropped" = "$tested" ]; then
    NEG=$((NEG + 1))
    emit "NEGATIVE_CONTROL,stage=$stage,dropped=$drop,text_inputs=$tested,text_dropped=$dropped,expect=$expect,first_changed=$first,result=failed_as_expected"
  else
    emit "NEGATIVE_CONTROL,stage=$stage,dropped=$drop,text_inputs=$tested,text_dropped=$dropped,expect=$expect,not_dropped=${bad:-none},result=passed_unexpectedly"
    exit 1
  fi
}

never_resolved_trace() {
  local jobd hits input_opens calls verdict skipped confined
  jobd=$(newdir dxf)
  cp "$CORPUS/dxf/never_resolved.dxf" "$jobd/in.dxf"
  chmod 0644 "$jobd/in.dxf"
  ARGV=()
  stage_argv dxf dxf dxf /job -
  trace_env dxf
  docker run --rm --user root --cap-add SYS_PTRACE -v "$jobd:/job" -e PROBE_USER=nobody --entrypoint sh "$TAG" -c '
    apt-get update -qq >/dev/null 2>&1
    apt-get install -y -qq strace >/dev/null 2>&1
    command -v strace >/dev/null || { echo "strace could not be installed" >&2; exit 1; }
    chmod 0777 /job
    setpriv --reuid "$PROBE_USER" --regid nogroup --clear-groups strace -f -qq -s 4096 -e trace=%file -o /job/.never.trace "$@"
  ' sh env "${TRACE_ENV[@]}" "${ARGV[@]}" > "$WORK/never.out" 2> "$WORK/never.err" </dev/null || true
  [ -s "$jobd/.never.trace" ] || { emit "FAIL,stage=dxf,input=never_resolved,reason=the raw trace was not written: $(tail -n 2 "$WORK/never.err" | tr '\n,' '  ')"; exit 1; }
  verdict=$(grep -a '^CADVERDICT ' "$WORK/never.out" | tail -n 1 || true)
  hits=$(grep -cE '"/etc/passwd"|"/etc/hostname"|outside/secret\.png"' "$jobd/.never.trace" || true)
  input_opens=$(grep -cE 'open(at)?\(.*"/job/in\.dxf"' "$jobd/.never.trace" || true)
  calls=$(grep -c . "$jobd/.never.trace" || true)
  skipped=$(printf '%s' "$verdict" | sed -n 's/.*"skipped":\({[^}]*}\).*/\1/p' | tr ',' ';')
  confined=$(grep -E '^SUFFICIENT,stage=dxf,input=never_resolved,set=measured,' "$OUT" | sed -n 's/.*,result=\([a-z]*\),.*/\1/p')
  rm -rf "$jobd"
  emit "NEVER_RESOLVED,trace_hits=$hits,named=/etc/passwd;/etc/hostname;*outside/secret.png,syscalls=trace_%file_all_results,traced_calls=$calls,input_opens=$input_opens,verdict=$(verdict_of "$verdict"),skipped=${skipped:-none},confined_measured=${confined:-none},unconfined=1"
  [ "$hits" = 0 ] && [ "$input_opens" -gt 0 ] && [ "$(verdict_of "$verdict")" = ok ] && [ "$confined" = identical ] \
    || { emit "FAIL,stage=dxf,input=never_resolved,reason=the helper touched a referenced path, or the trace saw no open of the input, or the confined run differs"; exit 1; }
}

measured_line() {
  local stage=$1 ro eng
  eng=$(engine_path "$stage")
  ro=$(grep -vxF "$eng" "$WORK/set.meas.$stage" | grep -vxF "$MULTIARCH_DIR" | paste -sd: -)
  emit "MEASURED,stage=$stage,ro=$ro,engines=$(engine_name "$stage"),multiarch=$MULTIARCH_DIR"
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
  [ "$eng" = "$(engine_name "$stage")" ] || { emit "FAIL,reason=the MEASURED line of $stage names engine $eng"; exit 1; }
  [ "$mult" = "$MULTIARCH_DIR" ] || { emit "FAIL,reason=the measured multiarch directory $mult is not this image's $MULTIARCH_DIR"; exit 1; }
  { printf '%s\n' "$ro" | tr ':' '\n'; printf '%s\n%s\n' "$(engine_path "$stage")" "$mult"; } > "$WORK/set.meas.$stage"
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
  case "$s" in
    dxf|rsvg) font_negative_control "$s" ;;
    *) negative_control "$s" ;;
  esac
  measured_line "$s"
done
if has_stage guard; then
  venv_effect "$WORK/set.meas.guard"
  if grep -qx '/opt/venv' "$WORK/set.meas.guard"; then
    emit "FAIL,stage=guard,reason=the guard set reaches /opt/venv: the stage that reads hostile bytes first must not reach OCP"
    exit 1
  fi
fi
if has_stage dxf; then
  never_resolved_trace
fi

AMD64_TRIPLET=""
if [ "$AMD64" = 1 ]; then
  amd64_proof
else
  AMD64_TRIPLET=$GO_AMD_DERIVED
  if [ "$DRAW" = 1 ]; then
    emit "AMD64,image_arch=amd64,status=deferred,landlock=not_measured,stages=$(printf '%s' "$STAGES" | tr ' ' '+'),note=an emulated amd64 process has no Landlock; the amd64 sets of these stages are not measured here and the multiarch row stays derived"
  else
    emit "AMD64,image_arch=amd64,status=deferred,landlock=not_measured,render=not_measured,note=the amd64 render Landlock set and the emulated amd64 build are not measured here; run with --amd64 for the guard and mesh build proof"
  fi
fi
GO_ARM=$(go_triplet arm64)
GO_AMD=$(go_triplet amd64)
if [ "$NATIVE_TRIPLET" = "$GO_ARM" ] && [ "$AMD64_TRIPLET" = "$GO_AMD" ]; then GO_MATCH=match; else GO_MATCH=MISMATCH; fi
emit "MULTIARCH,arm64=$NATIVE_TRIPLET,amd64=$AMD64_TRIPLET,amd64_status=derived,go_map=$GO_MATCH"
[ "$GO_MATCH" = match ] || { emit "FAIL,reason=cadMultiarchLib does not equal the observed triplets"; exit 1; }

PST=$(for s in $STAGES; do cat "$WORK/set.meas.$s"; done | grep -E '^/(proc|sys|tmp)' | LC_ALL=C sort -u | paste -sd: - || true)
emit "SUMMARY,stages=$(printf '%s\n' $STAGES | grep -c .),inputs=$(cat "$WORK"/inputs.*.tsv | wc -l | tr -d ' '),sufficient=$SUFF,different=$DIFF,leaked=$LEAKED,effect_denied=$EFFECT_DENIED,negative_controls=$NEG,removal_rows=$REMOVAL_ROWS,proc_sys_tmp=${PST:-none}"

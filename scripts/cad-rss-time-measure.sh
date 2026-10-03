#!/usr/bin/env bash
# cad-rss-time-measure.sh -- Phase 71 (EVAL-03) peak RSS and wall time of the
# CAD helpers, one container per (file, operation, memory limit, repeat).
#
# memory.peak is cumulative per container, so a container shared between cells
# reports one peak for all of them. An emulated image is not a measurement:
# cad_require_native refuses it. A render is judged by pixelcheck on the PNG,
# never by the helper's exit code.
#
# Usage:
#   scripts/cad-rss-time-measure.sh [--ocp V] [--variant NAME] [--memory 1g,2g,4g]
#       [--cpus 2] [--repeats N] [--ops mesh,render] [--budget-s N]
#       [--confine-ro PATH:PATH:...] [--out FILE] FILE-OR-DIR...
#
# Exit codes: 64 usage, 69 docker missing, 1 a cell produced no WRAP line or
# there were no rows.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

OCP=7.9.3.1.1
VARIANT=full
MEMS=1g,2g,4g
CPUS=2
REPEATS=1
OPS=mesh,render
BUDGET=${OCTOCONV_CAD_CELL_BUDGET:-900}
CONFINE=""
OUT=cad-rss-time-measurement.txt
INPUTS=()

usage() {
  sed -n '/^# Usage:/,/^# Exit codes/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

while [ $# -gt 0 ]; do
  case "$1" in
    --ocp) OCP=${2:?}; shift 2 ;;
    --variant) VARIANT=${2:?}; shift 2 ;;
    --memory) MEMS=${2:?}; shift 2 ;;
    --cpus) CPUS=${2:?}; shift 2 ;;
    --repeats) REPEATS=${2:?}; shift 2 ;;
    --ops) OPS=${2:?}; shift 2 ;;
    --budget-s) BUDGET=${2:?}; shift 2 ;;
    --confine-ro) CONFINE=${2:?}; shift 2 ;;
    --out) OUT=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    --) shift; while [ $# -gt 0 ]; do INPUTS+=("$1"); shift; done ;;
    -*) echo "error: unknown option $1" >&2; usage ;;
    *) INPUTS+=("$1"); shift ;;
  esac
done
[ ${#INPUTS[@]} -gt 0 ] || usage

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 69
fi

abspath() {
  local p=$1
  if [ -d "$p" ]; then (cd "$p" && pwd -P)
  else printf '%s/%s\n' "$(cd "$(dirname "$p")" && pwd -P)" "$(basename "$p")"; fi
}

FILES=()
for item in "${INPUTS[@]}"; do
  if [ -d "$item" ]; then
    while IFS= read -r f; do FILES+=("$f"); done < <(
      find "$(abspath "$item")" -type f \( -iname '*.step' -o -iname '*.stp' -o -iname '*.iges' \
        -o -iname '*.igs' -o -iname '*.stl' -o -iname '*.dxf' \) | LC_ALL=C sort)
  elif [ -f "$item" ]; then
    FILES+=("$(abspath "$item")")
  else
    echo "error: no such file or directory in the argument list" >&2
    exit 64
  fi
done

: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }

TAG=$(cad_build_image "$OCP" "$VARIANT")
cad_require_native "$TAG" || exit 1
IMAGE_ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-measure.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

PROBE_MOUNT=()
PROBE_ENV=()
if [ -n "$CONFINE" ]; then
  mkdir -p "$WORK/probe"
  CGO_ENABLED=0 GOOS=linux GOARCH="$IMAGE_ARCH" go build -o "$WORK/probe/sandbox-probe" ./scripts/cmd/sandbox-probe
  chmod 0755 "$WORK/probe" "$WORK/probe/sandbox-probe"
  PROBE_MOUNT=(-v "$WORK/probe:/probe:ro")
  PROBE_ENV=(-e "PROBE_RO=$CONFINE" -e PROBE_RW=/job)
fi

cad_evidence_header "$TAG" "$CPUS" "$MEMS" | tee -a "$OUT"
[ -z "$CONFINE" ] || emit "HDR,confine_ro=$CONFINE"

PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure

normalize_fmt() {
  case "$1" in
    stp|step) echo step ;;
    igs|iges) echo iges ;;
    *) echo "$1" ;;
  esac
}

newdir() {
  local d
  d=$(mktemp -d "$WORK/cell.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

docker_cell() {
  local mem=$1 jobdir=$2 file=$3 ext=$4 emit_text=$5
  shift 5
  docker run --rm --network none --cpus "$CPUS" --memory "$mem" --memory-swap "$mem" --user nobody \
    -v "$jobdir:/job" -v "$file:/job/in.$ext:ro" \
    ${PROBE_MOUNT[@]+"${PROBE_MOUNT[@]}"} ${PROBE_ENV[@]+"${PROBE_ENV[@]}"} \
    -e "CAD_EMIT_TEXT=$emit_text" \
    "$TAG" "$@"
}

ROWS=0
FAILED=0
OOM=0
TIMEOUTS=0
NOWRAP=0
CELL=0

for file in "${FILES[@]}"; do
  ext=$(printf '%s' "${file##*.}" | tr '[:upper:]' '[:lower:]')
  fmt=$(normalize_fmt "$ext")
  case "$file" in
    "$REPO_ROOT"/*|"$CAD_CACHE_DIR"/*)
      src=public
      name=$(basename "$file" | tr ',' '_')
      emit_text=1 ;;
    *)
      src=private
      name=-
      emit_text=0 ;;
  esac
  sha=$(shasum -a 256 "$file" | awk '{print $1}')
  bytes=$(wc -c < "$file" | tr -d ' ')

  IFS=, read -r -a ops <<< "$OPS"
  IFS=, read -r -a mems <<< "$MEMS"
  for op in "${ops[@]}"; do
    case "$fmt:$op" in
      step:mesh|step:render|stl:render) ;;
      *) emit "SKIP,src=$src,name=$name,fmt=$fmt,op=$op,reason=not-supported-by-tracer-helpers"; continue ;;
    esac
    for mem in "${mems[@]}"; do
      rep=1
      while [ "$rep" -le "$REPEATS" ]; do
        CELL=$((CELL + 1))
        jobdir=$(newdir)
        pix=-
        premesh_ok=1
        in_ext=$ext
        if [ "$op" = mesh ]; then
          inner=("${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job)
        else
          if [ "$fmt" = step ]; then
            pre=$(newdir)
            if docker_cell "$mem" "$pre" "$file" "$ext" 0 "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job >/dev/null 2>&1 \
               && [ -f "$pre/part.stl" ]; then
              cp "$pre/part.stl" "$jobdir/part.stl"
            else
              premesh_ok=0
            fi
            rm -rf "$pre"
            stl_in=/job/part.stl
          else
            stl_in="/job/in.$ext"
          fi
          inner=("${PY[@]}" "$LIB/cad_render.py" "$stl_in" /job/render.png)
        fi
        if [ -n "$CONFINE" ]; then
          inner=(/probe/sandbox-probe "${inner[@]}")
          confined=1
        else
          confined=0
        fi

        wrap=""
        if [ "$premesh_ok" = 1 ]; then
          wrap=$(docker_cell "$mem" "$jobdir" "$file" "$ext" "$emit_text" \
                   "${PY[@]}" "$LIB/measure_wrap.py" --budget-s "$BUDGET" --outdir /job -- "${inner[@]}" \
                 | grep -a '^WRAP,' | tail -n 1 || true)
        fi

        if [ -z "$wrap" ]; then
          NOWRAP=$((NOWRAP + 1))
          echo "error: cell $CELL (op=$op mem=$mem rep=$rep) produced no WRAP line" >&2
          rm -rf "$jobdir"
          rep=$((rep + 1))
          continue
        fi

        wrap=${wrap#WRAP,}
        head=${wrap%%,out=*}
        out=${wrap#*,out=}
        rc=$(printf '%s' "$head" | sed -n 's/^rc=\([0-9]*\),.*/\1/p')

        if [ "$op" = render ]; then
          pix=fail
          if [ "$rc" = 0 ] && [ -f "$jobdir/render.png" ]; then
            pixline=$(docker run --rm --network none --user nobody -v "$jobdir:/job:ro" "$TAG" \
                        "${PY[@]}" "$LIB/pixelcheck.py" single /job/render.png 2>/dev/null | grep -a '^PIX,' | tail -n 1 || true)
            if [ -n "$pixline" ]; then
              emit "$pixline"
              case "$pixline" in *,ok=1) pix=ok ;; esac
            fi
          fi
        fi

        emit "ROW,src=$src,name=$name,sha=$sha,fmt=$fmt,bytes=$bytes,op=$op,ocp=$OCP,variant=$VARIANT,mem=$mem,cpus=$CPUS,confined=$confined,rep=$rep,$head,pix=$pix,out=$out"

        ROWS=$((ROWS + 1))
        if [ "$rc" != 0 ] || [ "$pix" = fail ]; then FAILED=$((FAILED + 1)); fi
        case "$head" in *,oom_kill=0,*|*,oom_kill=na,*) ;; *) OOM=$((OOM + 1)) ;; esac
        case "$head" in *,timeout=1,*) TIMEOUTS=$((TIMEOUTS + 1)) ;; esac

        rm -rf "$jobdir"
        rep=$((rep + 1))
      done
    done
  done
done

emit "SUMMARY,rows=$ROWS,failed=$FAILED,oom=$OOM,timeout=$TIMEOUTS"

if [ "$ROWS" -eq 0 ] || [ "$NOWRAP" -gt 0 ]; then
  exit 1
fi

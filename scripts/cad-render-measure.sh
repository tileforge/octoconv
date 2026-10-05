#!/usr/bin/env bash
# cad-render-measure.sh -- Phase 71 (EVAL-01) pixel-judged render suite for the
# CAD helpers: seven views of real and degenerate parts, size ladder, a
# transparent render, determinism, tessellation quality and the DXF drawing
# path. Every container call is a separate docker run with --network none,
# --cpus 2, --memory 2g and --user nobody. A PNG is judged by pixelcheck in its
# own container, never by the exit code of the renderer.
#
# A cell that fails is retried up to three times and every retry is printed as a
# RETRY line, so a bind-mount flake cannot become a measurement unseen.
#
# Usage:
#   scripts/cad-render-measure.sh [--ocp V] [--variant NAME] [--out-dir DIR]
#
# Exit codes: 64 usage, 69 docker missing, 1 no parts, a failed pixel check or
# a non-deterministic render.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"
. "$REPO_ROOT/scripts/cad-measure/verdicts.sh"

OCP=7.9.3.1.1
VARIANT=full
OUT_DIR="$REPO_ROOT/.planning/phases/71-evaluation-gate/evidence"
BUDGET=${OCTOCONV_CAD_CELL_BUDGET:-300}

usage() {
  sed -n '/^# Usage:/,/^# Exit codes/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

while [ $# -gt 0 ]; do
  case "$1" in
    --ocp) OCP=${2:?}; shift 2 ;;
    --variant) VARIANT=${2:?}; shift 2 ;;
    --out-dir) OUT_DIR=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    *) echo "error: unknown argument $1" >&2; usage ;;
  esac
done

if ! command -v docker >/dev/null 2>&1; then
  echo "error: docker is required" >&2
  exit 69
fi

mkdir -p "$OUT_DIR"
OUT_DIR=$(cd "$OUT_DIR" && pwd -P)
OUTFILE="$OUT_DIR/71-04-render.txt"
: > "$OUTFILE"
emit() { printf '%s\n' "$*" | tee -a "$OUTFILE"; }

CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
VIEWS=(iso front back left right top bottom)
PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure
LIMITS=(--network none --cpus 2 --memory 2g --memory-swap 2g --user nobody)

REAL=(
  "slide|$CORPUS/public/LinearSlide-MGN15x-Block.step|asym"
  "corner|$CORPUS/public/2020_corner_bracket-Corner.step|asym"
  "lm8uu|$CORPUS/public/LM8UU.step|sym"
  "sc8uu_assy|$CORPUS/public/SC8UU.step|sym"
  "hubble|$CORPUS/public/nasa-hubble-all-pieces.stl|asym"
)
ROBUST=(
  "lbracket|$CORPUS/synthetic/asymmetric_l_bracket.step|asym"
  "plate|$CORPUS/synthetic/degenerate_plate.step|sym"
  "rod|$CORPUS/synthetic/degenerate_rod.step|sym"
  "face|$CORPUS/synthetic/single_face.step|sym"
  "surfaces|$CORPUS/synthetic/surfaces_only.iges|sym"
  "binstl|$CORPUS/synthetic/stl_binary_solid_header.stl|sym"
)
DRAWINGS=(
  "hatches_2|$CORPUS/public/hatches_2.dxf"
  "acad_table|$CORPUS/public/acad_table_with_blk_ref.dxf"
  "uncommon_dim|$CORPUS/public/uncommon.dxf"
  "nested_dim|$CORPUS/public/dimension_in_nested_blocks.dxf"
  "columns_mtext|$CORPUS/public/columns_R2007.dxf"
  "legacy_cp1251|$CORPUS/synthetic/legacy_cp1251.dxf"
  "legacy_latin1|$CORPUS/synthetic/legacy_latin1.dxf"
  "houses_geo|$CORPUS/public/houses_of_parliament_georeferenced.dxf"
)

TAG=$(cad_build_image "$OCP" "$VARIANT")
cad_require_native "$TAG" || exit 1

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-render.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
PNG="$WORK/png"
SHEETS="$WORK/sheets"
mkdir -p "$PNG" "$SHEETS" "$WORK/stl" "$WORK/svg"
chmod 0777 "$PNG" "$SHEETS" "$WORK/stl" "$WORK/svg"

emit "# Plan 71-04 render suite"
cad_evidence_header "$TAG" 2 2g | tee -a "$OUTFILE"

FAIL=0
PNGS=0
RETRIES=0
VIEWS_EXPECTED=0
VIEWS_DIFFER=0
DETERMINISM=identical
DETERMINISM_RUNS=0
PARTS=0
ROBUST_DONE=0
DRAWINGS_DONE=0

newdir() {
  local d
  d=$(mktemp -d "$WORK/cell.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

lower_ext() { printf '%s' "${1##*.}" | tr '[:upper:]' '[:lower:]'; }

CELL_DIR=""
CELL_HEAD=""
CELL_OUT=""
CELL_RC=""
CELL_ATTEMPTS=0

run_cell() {
  local expect=$1 wrap
  shift
  local -a mounts=()
  while [ "$1" != "--" ]; do mounts+=("$1"); shift; done
  shift
  CELL_ATTEMPTS=0
  CELL_HEAD=""
  CELL_OUT=""
  CELL_RC=""
  while [ "$CELL_ATTEMPTS" -lt 3 ]; do
    CELL_ATTEMPTS=$((CELL_ATTEMPTS + 1))
    CELL_DIR=$(newdir)
    wrap=$(docker run --rm "${LIMITS[@]}" -e CAD_EMIT_TEXT=1 -v "$CELL_DIR:/job" ${mounts[@]+"${mounts[@]}"} "$TAG" \
             "${PY[@]}" "$LIB/measure_wrap.py" --budget-s "$BUDGET" --outdir /job -- "$@" \
           | grep -a '^WRAP,' | tail -n 1 || true)
    if [ -n "$wrap" ]; then
      wrap=${wrap#WRAP,}
      CELL_HEAD=${wrap%%,out=*}
      CELL_OUT=${wrap#*,out=}
      CELL_RC=$(printf '%s' "$CELL_HEAD" | sed -n 's/^rc=\([0-9]*\),.*/\1/p')
      if [ "$CELL_RC" = 0 ] && [ -f "$CELL_DIR/$expect" ]; then
        return 0
      fi
    fi
    RETRIES=$((RETRIES + 1))
    emit "RETRY,what=$expect,attempt=$CELL_ATTEMPTS,wrap=$([ -n "$wrap" ] && echo yes || echo no),rc=${CELL_RC:-na}"
    if [ "$CELL_ATTEMPTS" -lt 3 ]; then rm -rf "$CELL_DIR"; fi
  done
  return 1
}

pixel_tool() {
  local out
  out=$(docker run --rm --network none --user nobody -v "$PNG:/png:ro" "$TAG" "${PY[@]}" "$LIB/pixelcheck.py" "$@" 2>/dev/null \
        | grep -a -E '^(PIX|VIEWS|SAME|TRANSPARENT),' | tail -n 1 || true)
  if [ -z "$out" ]; then
    out=$(docker run --rm --network none --user nobody -v "$PNG:/png:ro" "$TAG" "${PY[@]}" "$LIB/pixelcheck.py" "$@" 2>/dev/null \
          | grep -a -E '^(PIX|VIEWS|SAME|TRANSPARENT),' | tail -n 1 || true)
  fi
  printf '%s\n' "$out"
}

judge() {
  local label=$1 item=$2 role=$3 view=$4 size=$5 mode=${6:-part} line
  line=$(pixel_tool single "/png/$label.png" --mode "$mode")
  if [ -z "$line" ]; then
    emit "PIX,item=$item,role=$role,view=$view,size=$size,label=$label,error=no-output,ok=0"
    FAIL=$((FAIL + 1))
    return 0
  fi
  emit "PIX,item=$item,role=$role,view=$view,size=$size,label=$label,${line#PIX,}"
  case "$line" in *,ok=1) ;; *) FAIL=$((FAIL + 1)) ;; esac
}

STL_DIR=""
ensure_mesh() {
  local name=$1 path=$2 quality=$3 ext dir
  ext=$(lower_ext "$path")
  dir="$WORK/stl/$name-$quality"
  STL_DIR=$dir
  [ -f "$dir/part.stl" ] && return 0
  mkdir -p "$dir"
  chmod 0777 "$dir"
  if [ "$ext" = stl ]; then
    cp "$path" "$dir/part.stl"
    chmod 0644 "$dir/part.stl"
    emit "MESH,item=$name,quality=$quality,source=stl-input"
    return 0
  fi
  if run_cell part.stl -v "$path:/job/in.$ext:ro" -- \
       "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --quality "$quality"; then
    cp "$CELL_DIR/part.stl" "$dir/part.stl"
    chmod 0644 "$dir/part.stl"
    emit "MESH,item=$name,quality=$quality,attempts=$CELL_ATTEMPTS,$CELL_HEAD,out=$CELL_OUT"
    rm -rf "$CELL_DIR"
    return 0
  fi
  emit "MESH,item=$name,quality=$quality,attempts=$CELL_ATTEMPTS,status=failed,rc=${CELL_RC:-na}"
  FAIL=$((FAIL + 1))
  return 1
}

render_one() {
  local label=$1 item=$2 role=$3 view=$4 size=$5 bg=$6 lp=$7 stldir=$8 quality=$9
  if run_cell render.png -v "$stldir:/in:ro" -- \
       "${PY[@]}" "$LIB/cad_render.py" /in/part.stl /job/render.png \
       --view "$view" --size "$size" --background "$bg" --lp-threads "$lp"; then
    cp "$CELL_DIR/render.png" "$PNG/$label.png"
    cp "$CELL_DIR/render.png.json" "$PNG/$label.png.json"
    chmod 0644 "$PNG/$label.png" "$PNG/$label.png.json"
    emit "REN,label=$label,item=$item,role=$role,view=$view,size=$size,bg=$bg,lp=$lp,quality=$quality,attempts=$CELL_ATTEMPTS,$CELL_HEAD,out=$CELL_OUT"
    rm -rf "$CELL_DIR"
    PNGS=$((PNGS + 1))
    return 0
  fi
  emit "REN,label=$label,item=$item,role=$role,view=$view,size=$size,bg=$bg,lp=$lp,quality=$quality,attempts=$CELL_ATTEMPTS,status=failed,rc=${CELL_RC:-na}"
  FAIL=$((FAIL + 1))
  return 1
}

check_views() {
  local item=$1 line
  line=$(pixel_tool views "/png/${item}_iso_1280.png" "/png/${item}_front_1280.png" "/png/${item}_top_1280.png")
  VIEWS_EXPECTED=$((VIEWS_EXPECTED + 1))
  if [ -z "$line" ]; then
    emit "VIEWS,item=$item,error=no-output,ok=0"
    FAIL=$((FAIL + 1))
    return 0
  fi
  emit "VIEWS,item=$item,views=iso:front:top,${line#VIEWS,}"
  case "$line" in
    *,ok=1) VIEWS_DIFFER=$((VIEWS_DIFFER + 1)) ;;
    *) FAIL=$((FAIL + 1)) ;;
  esac
}

emit "# calibrated pixelcheck constants (pixelcheck.py)"
grep -E '^[A-Z_]+ = [0-9.]+$' "$REPO_ROOT/scripts/cad-measure/pixelcheck.py" | sed 's/ = /=/; s/^/CAL,/' | tee -a "$OUTFILE"

emit "# camera constants (cad_render.py), draft for RNDR-02: orthographic, Z up, camera = bbox centre + direction * camera_distance_diag * bbox diagonal"
python3 - "$REPO_ROOT/scripts/cad-measure/cad_render.py" <<'PY' | tee -a "$OUTFILE"
import runpy, sys
ns = runpy.run_path(sys.argv[1])
for name, (d, up) in ns["VIEWS"].items():
    print("VIEWDEF,view=%s,direction=%s,up=%s" % (name, ":".join("%g" % x for x in d), ":".join("%g" % x for x in up)))
print("VIEWDEF,projection=orthographic,camera_distance_diag=%g,margin_default=0.06,feature_angle=%g,fill=%s,edge=%s,fxaa=on,edge_lighting=off" % (
    ns["CAMERA_DISTANCE"], ns["FEATURE_ANGLE"], ":".join("%g" % x for x in ns["FILL"]), ":".join("%g" % x for x in ns["EDGE"])))
print("VIEWDEF,lights=%s" % ";".join("%s@%g" % (":".join("%g" % x for x in v), i) for v, i in ns["LIGHTS"]))
PY

emit "# parts: seven views at 1280, white background, standard quality"
parts_loop() {
  local role=$1 rec name path asym stldir v ok
  shift
  for rec in "$@"; do
    IFS='|' read -r name path asym <<< "$rec"
    if ! ensure_mesh "$name" "$path" standard; then continue; fi
    stldir=$STL_DIR
    ok=1
    for v in "${VIEWS[@]}"; do
      if render_one "${name}_${v}_1280" "$name" "$role" "$v" 1280 white default "$stldir" standard; then
        judge "${name}_${v}_1280" "$name" "$role" "$v" 1280
      else
        ok=0
      fi
    done
    if [ "$ok" = 1 ]; then
      if [ "$role" = real ]; then PARTS=$((PARTS + 1)); else ROBUST_DONE=$((ROBUST_DONE + 1)); fi
    fi
    if [ "$asym" = asym ] && [ "$ok" = 1 ]; then check_views "$name"; fi
  done
}
parts_loop real "${REAL[@]}"
emit "# robustness fixtures: seven views at 1280"
parts_loop robust "${ROBUST[@]}"

emit "# size ladder: iso of the first real part at 512, 1280 (above) and 2048"
IFS='|' read -r r_name r_path r_asym <<< "${REAL[0]}"
ensure_mesh "$r_name" "$r_path" standard || true
for sz in 512 2048; do
  if render_one "${r_name}_iso_$sz" "$r_name" size iso "$sz" white default "$STL_DIR" standard; then
    judge "${r_name}_iso_$sz" "$r_name" size iso "$sz"
  fi
done

emit "# transparent background"
if render_one "${r_name}_iso_transparent" "$r_name" transparent iso 1280 transparent default "$STL_DIR" standard; then
  tline=$(pixel_tool transparent "/png/${r_name}_iso_transparent.png")
  emit "TRANSPARENT,item=$r_name,${tline#TRANSPARENT,}"
  case "$tline" in *,ok=1) ;; *) FAIL=$((FAIL + 1)) ;; esac
  judge "${r_name}_iso_transparent" "$r_name" transparent iso 1280
fi

emit "# determinism: the iso render of every real part, twice, same architecture"
for rec in "${REAL[@]}"; do
  IFS='|' read -r name path asym <<< "$rec"
  ensure_mesh "$name" "$path" standard || continue
  if [ -f "$PNG/${name}_iso_1280.png" ] && render_one "${name}_iso_1280_rerun" "$name" determinism iso 1280 white default "$STL_DIR" standard; then
    sline=$(pixel_tool same "/png/${name}_iso_1280.png" "/png/${name}_iso_1280_rerun.png")
    DETERMINISM_RUNS=$((DETERMINISM_RUNS + 1))
    emit "SAME,item=$name,kind=determinism,${sline#SAME,}"
    case "$sline" in *,ok=1) ;; *) DETERMINISM=DIFFERENT; FAIL=$((FAIL + 1)) ;; esac
  fi
done

emit "# LP_NUM_THREADS=1 against the default thread count (informative, not a criterion)"
for rec in "${REAL[@]}"; do
  IFS='|' read -r name path asym <<< "$rec"
  ensure_mesh "$name" "$path" standard || continue
  if [ -f "$PNG/${name}_iso_1280.png" ] && render_one "${name}_iso_1280_lp1" "$name" lp1 iso 1280 white 1 "$STL_DIR" standard; then
    sline=$(pixel_tool same "/png/${name}_iso_1280.png" "/png/${name}_iso_1280_lp1.png")
    emit "SAME,item=$name,kind=lp1-vs-default-informative,${sline#SAME,}"
  fi
done

emit "# tessellation quality: iso of every real part at draft, standard, fine"
for rec in "${REAL[@]}"; do
  IFS='|' read -r name path asym <<< "$rec"
  for q in draft fine; do
    if ensure_mesh "$name" "$path" "$q"; then
      if render_one "${name}_iso_q${q}" "$name" quality iso 1280 white default "$STL_DIR" "$q"; then
        judge "${name}_iso_q${q}" "$name" quality iso 1280
      fi
    fi
  done
done

emit "# DXF drawing path: cad_dxf -> rsvg-convert PNG 1280x1280 on white"
for rec in "${DRAWINGS[@]}"; do
  IFS='|' read -r name path <<< "$rec"
  if run_cell out.svg -v "$path:/job/in.dxf:ro" -- "${PY[@]}" "$LIB/cad_dxf.py" /job/in.dxf /job; then
    emit "DRW,item=$name,op=dxf,attempts=$CELL_ATTEMPTS,$CELL_HEAD,out=$CELL_OUT"
    mkdir -p "$WORK/svg/$name"
    chmod 0777 "$WORK/svg/$name"
    cp "$CELL_DIR/out.svg" "$WORK/svg/$name/out.svg"
    chmod 0644 "$WORK/svg/$name/out.svg"
    rm -rf "$CELL_DIR"
    if run_cell out.png -v "$WORK/svg/$name:/in:ro" -- \
         rsvg-convert -f png -w 1280 -h 1280 --keep-aspect-ratio -b '#FFFFFF' /in/out.svg -o /job/out.png; then
      cp "$CELL_DIR/out.png" "$PNG/dxf_$name.png"
      chmod 0644 "$PNG/dxf_$name.png"
      emit "DRW,item=$name,op=rsvg-png,attempts=$CELL_ATTEMPTS,$CELL_HEAD,out=$CELL_OUT"
      rm -rf "$CELL_DIR"
      PNGS=$((PNGS + 1))
      judge "dxf_$name" "$name" drawing png 1280 drawing
      DRAWINGS_DONE=$((DRAWINGS_DONE + 1))
    else
      emit "DRW,item=$name,op=rsvg-png,attempts=$CELL_ATTEMPTS,status=failed,rc=${CELL_RC:-na}"
      FAIL=$((FAIL + 1))
    fi
  else
    emit "DRW,item=$name,op=dxf,attempts=$CELL_ATTEMPTS,status=failed,rc=${CELL_RC:-na}"
    FAIL=$((FAIL + 1))
  fi
done

sheet() {
  local out=$1 rows=$2 cols=$3 attempt=1
  shift 3
  local -a files=()
  local f
  for f in "$@"; do files+=("/png/$f.png"); done
  while [ "$attempt" -le 2 ]; do
    if docker run --rm --network none --user nobody -v "$PNG:/png:ro" -v "$SHEETS:/out" "$TAG" \
         "${PY[@]}" "$LIB/contact_sheet.py" --out "/out/$out" --rows "$rows" --cols "$cols" "${files[@]}" >/dev/null \
       && [ -s "$SHEETS/$out" ]; then
      cp "$SHEETS/$out" "$OUT_DIR/71-04-contact-${out#contact-}"
      emit "SHEET,file=71-04-contact-${out#contact-},bytes=$(wc -c < "$SHEETS/$out" | tr -d ' ')"
      return 0
    fi
    RETRIES=$((RETRIES + 1))
    emit "RETRY,what=$out,attempt=$attempt"
    attempt=$((attempt + 1))
  done
  emit "SHEET,file=71-04-contact-${out#contact-},status=failed"
  FAIL=$((FAIL + 1))
}

join_names() {
  local first=1 rec name out=""
  for rec in "$@"; do
    IFS='|' read -r name _ <<< "$rec"
    if [ "$first" = 1 ]; then out=$name; first=0; else out="$out,$name"; fi
  done
  printf '%s' "$out"
}

real_names=$(join_names "${REAL[@]}")
robust_names=$(join_names "${ROBUST[@]}")
dxf_names=$(join_names "${DRAWINGS[@]}")
view_cols=$(IFS=,; printf '%s' "${VIEWS[*]}")

files=()
for rec in "${REAL[@]}"; do IFS='|' read -r name _ <<< "$rec"; for v in "${VIEWS[@]}"; do files+=("${name}_${v}_1280"); done; done
sheet contact-views.png "$real_names" "$view_cols" "${files[@]}"

files=()
for rec in "${ROBUST[@]}"; do IFS='|' read -r name _ <<< "$rec"; for v in "${VIEWS[@]}"; do files+=("${name}_${v}_1280"); done; done
sheet contact-robustness.png "$robust_names" "$view_cols" "${files[@]}"

files=()
for rec in "${REAL[@]}"; do IFS='|' read -r name _ <<< "$rec"; files+=("${name}_iso_qdraft" "${name}_iso_1280" "${name}_iso_qfine"); done
sheet contact-quality.png "$real_names" "draft,standard,fine" "${files[@]}"

files=()
for rec in "${DRAWINGS[@]}"; do IFS='|' read -r name _ <<< "$rec"; files+=("dxf_$name"); done
sheet contact-dxf.png "$dxf_names" "drawing" "${files[@]}"

emit "# observed pixel ranges over the real parts in part mode (calibration evidence)"
awk -F, '
  /^PIX,/ && /role=real/ && /mode=part/ {
    for (i = 1; i <= NF; i++) { split($i, kv, "="); v[kv[1]] = kv[2] }
    n++
    if (n == 1 || v["frac"] + 0 < fmin) fmin = v["frac"] + 0
    if (n == 1 || v["frac"] + 0 > fmax) fmax = v["frac"] + 0
    if (n == 1 || v["dark"] + 0 < dmin) dmin = v["dark"] + 0
    if (n == 1 || v["margin_px"] + 0 < mmin) mmin = v["margin_px"] + 0
  }
  END { printf "CALOBS,real_pngs=%d,frac_min=%.4f,frac_max=%.4f,dark_min=%.4f,margin_px_min=%d\n", n, fmin, fmax, dmin, mmin }
' "$OUTFILE" | tee -a "$OUTFILE"

emit "RENDER_INFO,views_expected=$VIEWS_EXPECTED,robust_parts=$ROBUST_DONE,drawings=$DRAWINGS_DONE,retries=$RETRIES,determinism_runs=$DETERMINISM_RUNS"
EXPECT_PNGS=$(( ((${#VIEWS[@]} + 4) * ${#REAL[@]}) + (${#VIEWS[@]} * ${#ROBUST[@]}) + 3 + ${#DRAWINGS[@]} ))
if render_verdict "$PARTS" "$ROBUST_DONE" "$DRAWINGS_DONE" "$PNGS" "$FAIL" "$DETERMINISM_RUNS" "$DETERMINISM" \
     "${#REAL[@]}" "${#ROBUST[@]}" "${#DRAWINGS[@]}" "$EXPECT_PNGS"; then
  emit "RENDER_SUMMARY,parts=$PARTS,pngs=$PNGS,fail=$FAIL,views_differ=$VIEWS_DIFFER,determinism=$DETERMINISM"
else
  emit "RENDER_SUMMARY,status=FAILED,parts=$PARTS,expected_parts=${#REAL[@]},pngs=$PNGS,expected_pngs=$EXPECT_PNGS,fail=$FAIL,views_differ=$VIEWS_DIFFER,determinism=$DETERMINISM,determinism_runs=$DETERMINISM_RUNS"
  exit 1
fi

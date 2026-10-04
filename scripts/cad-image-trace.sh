#!/usr/bin/env bash
# cad-image-trace.sh -- Phase 71 (EVAL-01, EVAL-04) library trace, VTK pruning
# check and OCP wheel licence record for the CAD measurement image.
#
# Usage:
#   scripts/cad-image-trace.sh libs        [--out FILE]
#   scripts/cad-image-trace.sh prune-check [--out FILE]
#   scripts/cad-image-trace.sh licences    [--out FILE]
#
# libs         LD_DEBUG and /proc/self/maps of every operation on both OCP pins
#              (render, dxf and rsvg only on the pinned one), the DT_NEEDED chain,
#              wheel libraries never loaded, the generated VTK keep-list.
# prune-check  full and pruned image of one build: size, byte-identical renders,
#              the other helpers, and two stale keep-list cases.
# licences     wheel library inventory, upstream licences, D-18 outcome.
#
# PIN, LP and PAR come from evidence/71-06-limits.txt (PIN_DECISION,
# THREADS_DECISION). An emulated image is not a measurement.
#
# Exit codes: 64 usage, 69 docker missing, 3 libs: the research expectation did
# not match (the evidence is written, a human decides), 1 a measurement failed.

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

EVID="$REPO_ROOT/.planning/phases/71-evaluation-gate/evidence"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
LIMITS_FILE="$EVID/71-06-limits.txt"
KEEPLIST="$REPO_ROOT/scripts/cad-measure/vtk-keeplist.txt"
PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure
SITE=/opt/venv/lib/python3.11/site-packages
LIMITS=(--network none --cpus 2 --memory 2g --memory-swap 2g --user nobody)
VIEWS=(iso front back left right top bottom)

usage() {
  sed -n '/^# Usage:/,/^# PIN, LP/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

CMD=${1:-}
[ -n "$CMD" ] || usage
shift
OUT=""
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    *) echo "error: unknown argument $1" >&2; usage ;;
  esac
done
case "$CMD" in
  libs) : "${OUT:=$EVID/71-07-libtrace.txt}" ;;
  prune-check) : "${OUT:=$EVID/71-07-prune.txt}" ;;
  licences) : "${OUT:=$EVID/71-07-licences.txt}" ;;
  *) usage ;;
esac

command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
[ -s "$LIMITS_FILE" ] || { echo "error: $LIMITS_FILE is missing" >&2; exit 1; }

PIN=$(sed -n 's/^PIN_DECISION,choice=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
LP=$(sed -n 's/^THREADS_DECISION,lp_num_threads=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
PAR=$(sed -n 's/^THREADS_DECISION,lp_num_threads=[^,]*,occt_parallel=\([^,]*\),.*/\1/p' "$LIMITS_FILE")
[ -n "$PIN" ] && [ -n "$LP" ] && [ -n "$PAR" ] || { echo "error: PIN_DECISION or THREADS_DECISION missing in $LIMITS_FILE" >&2; exit 1; }
case "$PIN" in
  7.9.3.1.1) OTHER=8.0.1.0.0 ;;
  8.0.1.0.0) OTHER=7.9.3.1.1 ;;
  *) echo "error: unknown pin $PIN" >&2; exit 1 ;;
esac

mkdir -p "$(dirname "$OUT")"
: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-trace.XXXXXX")
trap 'rm -rf "$WORK"' EXIT

newdir() {
  local d
  d=$(mktemp -d "$WORK/job.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

field() {
  local h=",$1," v
  v=${h#*,$2=}
  [ "$v" != "$h" ] || { echo ""; return 0; }
  printf '%s\n' "${v%%,*}"
}

mib() { awk -v b="$1" 'BEGIN { printf "%.1f", b / 1048576 }'; }

mesh_plain() {
  local tag=$1 src=$2 dst=$3 mem=${4:-2g} attempt=1 job ext rc
  ext=$(printf '%s' "${src##*.}" | tr '[:upper:]' '[:lower:]')
  while [ "$attempt" -le 3 ]; do
    job=$(newdir)
    rc=0
    docker run --rm --network none --cpus 2 --memory "$mem" --memory-swap "$mem" --user nobody \
      -v "$job:/job" -v "$src:/job/in.$ext:ro" "$tag" \
      "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --parallel "$PAR" >/dev/null 2>&1 </dev/null || rc=$?
    if [ -s "$job/part.stl" ]; then
      cp "$job/part.stl" "$dst"
      rm -rf "$job"
      return 0
    fi
    rm -rf "$job"
    attempt=$((attempt + 1))
  done
  return 1
}

if [ "$CMD" = libs ]; then
  TAG_PIN=$(cad_build_image "$PIN" full)
  TAG_OTHER=$(cad_build_image "$OTHER" full)
  cad_require_native "$TAG_PIN" || exit 1
  cad_require_native "$TAG_OTHER" || exit 1

  emit "# Plan 71-07 library trace: which wheel libraries load on which operation (D-18 measurement)"
  cad_evidence_header "$TAG_PIN" 2 2g | tee -a "$OUT"
  emit "HDR,pin=$PIN,other_pin=$OTHER,lp_threads=$LP,occt_parallel=$PAR,other_image=$TAG_OTHER"

  STLDIR="$WORK/stl"; SVGDIR="$WORK/svg"; MAPDIR="$WORK/rendermaps"
  mkdir -p "$STLDIR" "$SVGDIR" "$MAPDIR"
  chmod 0755 "$STLDIR" "$SVGDIR"

  IMPORT_PY='import OCP
libs = set()
with open("/proc/self/maps") as fh:
    for line in fh:
        p = line.split(None, 5)
        if len(p) == 6 and ".so" in p[5]:
            libs.add(p[5].strip())
with open("/job/maps.txt", "w") as fh:
    fh.write("\n".join(sorted(libs)) + "\n")'

  ELF_PY='import os, struct, sys
SITE = "/opt/venv/lib/python3.11/site-packages"
LIBS = SITE + "/cadquery_ocp_novtk.libs"
pin = os.environ["PIN"]
def needed(path):
    with open(path, "rb") as fh:
        d = fh.read()
    phoff = struct.unpack_from("<Q", d, 0x20)[0]
    phentsize, phnum = struct.unpack_from("<HH", d, 0x36)
    loads, dyn = [], None
    for i in range(phnum):
        p_type, _f, off, va, _pa, fsz, _msz, _al = struct.unpack_from("<IIQQQQQQ", d, phoff + i * phentsize)
        if p_type == 1:
            loads.append((va, off, fsz))
        elif p_type == 2:
            dyn = (off, fsz)
    def to_off(v):
        for va, off, fsz in loads:
            if va <= v < va + fsz:
                return v - va + off
    names, strtab = [], None
    for i in range(0, dyn[1], 16):
        tag, val = struct.unpack_from("<qQ", d, dyn[0] + i)
        if tag == 0:
            break
        if tag == 1:
            names.append(val)
        elif tag == 5:
            strtab = to_off(val)
    return [d[strtab + n:d.index(b"\0", strtab + n)].decode() for n in names]
targets = []
for name in sorted(os.listdir(SITE + "/OCP")):
    if name.startswith("OCP.cpython-311") and name.endswith(".so"):
        targets.append(("OCP/" + name, SITE + "/OCP/" + name))
for name in sorted(os.listdir(LIBS)):
    if name.startswith(("libTKService", "libfreeimage", "libraw", "libjasper")):
        targets.append((name, LIBS + "/" + name))
for label, path in targets:
    for n in needed(path):
        print("NEEDED,ocp=%s,from=%s,to=%s" % (pin, label, n))
for name in sorted(os.listdir(LIBS)):
    print("LIBFILE,ocp=%s,file=%s,bytes=%d" % (pin, name, os.path.getsize(LIBS + "/" + name)))'

  : > "$WORK/ops.tsv"
  KEEP_STL_AS=""
  MAPN=0

  count_hits() {
    local pat=$1 file=$2
    if [ -s "$file" ]; then grep -cE "$pat" "$file" || true; else echo 0; fi
  }

  trace_op() {
    local op=$1 input=$2 ocp=$3 tag=$4 expect=$5 infile=$6 ext=$7 maps=$8
    shift 8
    local attempt=1 rc job ok libfile="$WORK/libfiles.$ocp"
    while [ "$attempt" -le 3 ]; do
      job=$(newdir)
      rc=0
      if [ -n "$infile" ]; then
        docker run --rm "${LIMITS[@]}" -e LD_DEBUG=files -e LD_DEBUG_OUTPUT=/job/ld \
          -v "$job:/job" -v "$infile:/job/in.$ext:ro" "$tag" "$@" >"$WORK/cell.log" 2>&1 || rc=$?
      else
        docker run --rm "${LIMITS[@]}" -e LD_DEBUG=files -e LD_DEBUG_OUTPUT=/job/ld \
          -v "$job:/job" "$tag" "$@" >"$WORK/cell.log" 2>&1 || rc=$?
      fi
      ok=1
      ls "$job"/ld.* >/dev/null 2>&1 || ok=0
      if [ "$maps" = 1 ] && [ ! -s "$job/maps.txt" ]; then ok=0; fi
      if [ "$ok" = 1 ] && [ "$rc" = "$expect" ]; then break; fi
      emit "RETRY,op=$op,input=$input,ocp=$ocp,attempt=$attempt,rc=$rc,expected_rc=$expect,ld_file=$ok"
      if [ "$attempt" -lt 3 ]; then rm -rf "$job"; fi
      attempt=$((attempt + 1))
    done
    [ "$attempt" -le 3 ] || attempt=3
    local rc_ok=0
    [ "$rc" = "$expect" ] && rc_ok=1

    cat "$job"/ld.* 2>/dev/null | grep -a 'file=' | sed -n 's/.*file=\([^ ]*\) \[.*/\1/p' | sed 's|.*/||' | sort -u > "$WORK/names.ld"
    if [ "$maps" = 1 ] && [ -s "$job/maps.txt" ]; then
      sed 's|.*/||' "$job/maps.txt" | sort -u > "$WORK/names.maps"
    else
      : > "$WORK/names.maps"
    fi
    local fi_ld fi_mp raw_ld raw_mp jp_ld jp_mp
    fi_ld=$(count_hits 'libfreeimage' "$WORK/names.ld"); fi_mp=$(count_hits 'libfreeimage' "$WORK/names.maps")
    raw_ld=$(count_hits '^libraw[-.]' "$WORK/names.ld"); raw_mp=$(count_hits '^libraw[-.]' "$WORK/names.maps")
    jp_ld=$(count_hits 'libjasper' "$WORK/names.ld"); jp_mp=$(count_hits 'libjasper' "$WORK/names.maps")
    local fi=0 raw=0 jp=0
    [ $((fi_ld + fi_mp)) -gt 0 ] && fi=1
    [ $((raw_ld + raw_mp)) -gt 0 ] && raw=1
    [ $((jp_ld + jp_mp)) -gt 0 ] && jp=1
    sort -u "$WORK/names.ld" "$WORK/names.maps" > "$WORK/names.all"
    comm -12 "$WORK/names.all" "$libfile" > "$WORK/names.ocp"
    cat "$WORK/names.ocp" >> "$WORK/loaded.$ocp"
    local ocpn vtkn=0 src=ld_debug
    ocpn=$(wc -l < "$WORK/names.ocp" | tr -d ' ')
    if [ "$maps" = 1 ]; then
      src=ld_debug+maps
      if [ -s "$job/maps.txt" ]; then vtkn=$(grep -c '/vtkmodules/' "$job/maps.txt" || true); fi
    fi
    emit "LIBTRACE,op=$op,input=$input,ocp=$ocp,rc=$rc,freeimage=$fi,libraw=$raw,libjasper=$jp,ocp_libs_loaded=$ocpn,vtk_so_mapped=$vtkn,source=$src,fi_ld=$fi_ld,fi_maps=$fi_mp,raw_ld=$raw_ld,raw_maps=$raw_mp,jp_ld=$jp_ld,jp_maps=$jp_mp,rc_ok=$rc_ok,attempts=$attempt"
    printf '%s\t%s\t%s\t%s\t%s\t%s\n' "$ocp" "$op" "$fi" "$raw" "$jp" "$ocpn" >> "$WORK/ops.tsv"
    if [ "$ocp" = "$PIN" ] && [ "$maps" = 1 ] && [ -s "$job/maps.txt" ] && [ "${op#render}" != "$op" ]; then
      MAPN=$((MAPN + 1))
      cp "$job/maps.txt" "$MAPDIR/$(printf '%03d-%s' "$MAPN" "$op-$input" | tr -c 'A-Za-z0-9._-' '_').txt"
    fi
    if [ -n "$KEEP_STL_AS" ] && [ -s "$job/part.stl" ]; then cp "$job/part.stl" "$STLDIR/$KEEP_STL_AS"; fi
    if [ -n "${KEEP_SVG_AS:-}" ] && [ -s "$job/out.svg" ]; then cp "$job/out.svg" "$SVGDIR/$KEEP_SVG_AS"; fi
    rm -rf "$job"
  }

  for ocp_tag in "$PIN|$TAG_PIN" "$OTHER|$TAG_OTHER"; do
    ocp=${ocp_tag%%|*}
    tag=${ocp_tag#*|}
    docker run --rm --network none --entrypoint "${PY[0]}" -e "PIN=$ocp" "$tag" "${PY[@]:1}" -c "$ELF_PY" > "$WORK/elf.$ocp"
    grep '^NEEDED,' "$WORK/elf.$ocp" | while IFS= read -r l; do emit "$l"; done
    grep '^LIBFILE,' "$WORK/elf.$ocp" | sed 's/^.*,file=\([^,]*\),.*/\1/' | sort -u > "$WORK/libfiles.$ocp"
    : > "$WORK/loaded.$ocp"
    emit "LIBFILES,ocp=$ocp,count=$(wc -l < "$WORK/libfiles.$ocp" | tr -d ' '),bytes=$(grep '^LIBFILE,' "$WORK/elf.$ocp" | sed 's/.*,bytes=//' | awk '{s += $1} END {print s}')"
    emit "LDD,ocp=$ocp,freeimage_in_ocp_ldd=$(docker run --rm --network none --entrypoint sh "$tag" -c \
      "ldd $SITE/OCP/OCP.cpython-311-*.so 2>/dev/null | grep -cE 'libfreeimage|libraw|libjasper' || true")"
  done

  run_ocp_ops() {
    local ocp=$1 tag=$2 f name ext
    trace_op import "import-OCP" "$ocp" "$tag" 0 "" x 1 "${PY[@]}" -c "$IMPORT_PY"
    for spec in "mesh-step|slide|public/LinearSlide-MGN15x-Block.step" \
                "mesh-step|assembly|public/SC8UU.step" \
                "mesh-step|units_inch|synthetic/units_inch.step" \
                "mesh-step|legacy_cp1251|synthetic/legacy_cp1251.step" \
                "mesh-iges|surfaces_only|synthetic/surfaces_only.iges"; do
      IFS='|' read -r op name rel <<< "$spec"
      f="$CORPUS/$rel"
      ext=$(printf '%s' "${f##*.}" | tr '[:upper:]' '[:lower:]')
      KEEP_STL_AS=""
      [ "$ocp" != "$PIN" ] || [ "$name" != slide ] || KEEP_STL_AS=slide.stl
      trace_op "$op" "$name" "$ocp" "$tag" 0 "$f" "$ext" 1 \
        "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$ext" /job --parallel "$PAR" --dump-maps /job/maps.txt
      KEEP_STL_AS=""
    done
    local iges="$CAD_CACHE_DIR/fetched/freecad-mk8-hotend.iges"
    if [ -f "$iges" ]; then
      trace_op mesh-iges freecad-mk8-hotend "$ocp" "$tag" 0 "$iges" iges 1 \
        "${PY[@]}" "$LIB/cad_mesh.py" mesh /job/in.iges /job --parallel "$PAR" --dump-maps /job/maps.txt
    else
      emit "SKIP,op=mesh-iges,input=freecad-mk8-hotend,reason=not-in-cache"
    fi
    for spec in "nasa-hubble|public/nasa-hubble-all-pieces.stl" "stl_ascii_box|synthetic/stl_ascii_box.stl"; do
      IFS='|' read -r name rel <<< "$spec"
      trace_op stlstats "$name" "$ocp" "$tag" 0 "$CORPUS/$rel" stl 1 \
        "${PY[@]}" "$LIB/cad_mesh.py" stlstats /job/in.stl /job --dump-maps /job/maps.txt
    done
  }

  run_ocp_ops "$PIN" "$TAG_PIN"
  run_ocp_ops "$OTHER" "$TAG_OTHER"

  for spec in "degenerate_plate|synthetic/degenerate_plate.step" "degenerate_rod|synthetic/degenerate_rod.step" \
              "single_face|synthetic/single_face.step"; do
    IFS='|' read -r name rel <<< "$spec"
    mesh_plain "$TAG_PIN" "$CORPUS/$rel" "$STLDIR/$name.stl" || { emit "FAIL,reason=no-stl-for-$name"; exit 1; }
  done
  python3 - "$STLDIR" <<'PYEOF'
import struct, sys
d = sys.argv[1]
with open(d + "/empty.stl", "wb") as fh:
    fh.write(b"\0" * 80 + struct.pack("<I", 0))
with open(d + "/zero_extent.stl", "wb") as fh:
    fh.write(b"\0" * 80 + struct.pack("<I", 1) + struct.pack("<12fH", 0, 0, 1, 1, 1, 1, 1, 1, 1, 1, 1, 1, 0))
PYEOF
  [ -s "$STLDIR/slide.stl" ] || { emit "FAIL,reason=no-stl-for-slide"; exit 1; }

  render_op() {
    local op=$1 name=$2 expect=$3
    shift 3
    trace_op "$op" "$name" "$PIN" "$TAG_PIN" "$expect" "$STLDIR/$name.stl" stl 1 \
      "${PY[@]}" "$LIB/cad_render.py" /job/in.stl /job/out.png "$@" --dump-maps /job/maps.txt
  }
  for v in "${VIEWS[@]}"; do render_op render-view slide 0 --view "$v" --size 1280 --background white; done
  render_op render-transparent slide 0 --view iso --background transparent
  render_op render-512 slide 0 --view iso --size 512
  render_op render-2048 slide 0 --view iso --size 2048
  render_op render-lp1 slide 0 --view iso --lp-threads 1
  render_op render-lpN slide 0 --view iso --lp-threads "$LP"
  for name in degenerate_plate degenerate_rod single_face; do render_op render-degenerate "$name" 0 --view iso; done
  render_op render-exit2 empty 2 --view iso
  emit "NOTE,op=render-exit3,reason=vtkSTLReader drops every degenerate triangle (nan, inf, three equal points): such input reaches exit 2, and exit 3 needs a cell with an empty bounding box, so the branch is unreachable from an STL; the imports at the top of render() are the same on both exits"
  render_op render-exit3-unreachable zero_extent 2 --view iso

  for spec in "hatches_2|public/hatches_2.dxf" "blocks_minsert|synthetic/blocks_minsert.dxf" \
              "legacy_cp1251|synthetic/legacy_cp1251.dxf" "columns_R2007|public/columns_R2007.dxf"; do
    IFS='|' read -r name rel <<< "$spec"
    KEEP_SVG_AS="$name.svg"
    trace_op dxf "$name" "$PIN" "$TAG_PIN" 0 "$CORPUS/$rel" dxf 1 \
      "${PY[@]}" "$LIB/cad_dxf.py" /job/in.dxf /job --dump-maps /job/maps.txt
    KEEP_SVG_AS=""
    [ -s "$SVGDIR/$name.svg" ] || { emit "FAIL,reason=no-svg-for-$name"; exit 1; }
    trace_op rsvg-png "$name" "$PIN" "$TAG_PIN" 0 "$SVGDIR/$name.svg" svg 0 \
      rsvg-convert -f png -w 1280 -h 1280 --keep-aspect-ratio -b '#FFFFFF' /job/in.svg -o /job/out.png
    trace_op rsvg-pdf "$name" "$PIN" "$TAG_PIN" 0 "$SVGDIR/$name.svg" svg 0 \
      rsvg-convert -f pdf /job/in.svg -o /job/out.pdf
  done
  emit "LDD_RSVG,freeimage_libraw_jasper_in_ldd=$(docker run --rm --network none --entrypoint sh "$TAG_PIN" -c \
    "ldd /usr/bin/rsvg-convert | grep -cE 'libfreeimage|libraw|libjasper' || true")"

  for ocp in "$PIN" "$OTHER"; do
    sort -u "$WORK/loaded.$ocp" > "$WORK/loaded.$ocp.u"
    grep '^LIBFILE,' "$WORK/elf.$ocp" | while IFS= read -r l; do
      file=$(field "$l" file); bytes=$(field "$l" bytes)
      if ! grep -Fxq "$file" "$WORK/loaded.$ocp.u"; then emit "UNLOADED,ocp=$ocp,file=$file,bytes=$bytes"; fi
    done
    awk -F'\t' -v P="$ocp" '
      $1 == P {
        ops++; if ($3 == 1) fi++; if ($4 == 1) rw++; if ($5 == 1) jp++
        if ($6 == 0 && !($2 in seen)) { seen[$2] = 1; free = free (free == "" ? "" : "+") $2 }
      }
      END { printf "D18_TRACE,ocp=%s,ops=%d,freeimage_ops=%d,libraw_ops=%d,libjasper_ops=%d,ocp_free_ops=%s\n", P, ops, fi, rw, jp, (free == "" ? "none" : free) }' \
      "$WORK/ops.tsv" | while IFS= read -r l; do emit "$l"; done
  done

  MISMATCH=$(awk -F'\t' '
    {
      jp_expected = ($1 == "8.0.1.0.0") ? 1 : 0
      if ($6 > 0) {
        if ($3 != 1 || $4 != 1 || $5 != jp_expected)
          printf "%s/%s(fi=%s,raw=%s,jp=%s,want_jp=%d);", $1, $2, $3, $4, $5, jp_expected
      } else if ($3 != 0 || $4 != 0 || $5 != 0) {
        printf "%s/%s(ocp-free op loads fi=%s,raw=%s,jp=%s);", $1, $2, $3, $4, $5
      }
    }' "$WORK/ops.tsv")
  if [ -z "$MISMATCH" ]; then
    emit "EXPECTATION,research=V3-V7,match=1,detail=every op that imports OCP loads libfreeimage and libraw on both pins; libjasper only on 8.0.1.0.0; render dxf rsvg and stlstats load none"
  else
    emit "EXPECTATION,research=V3-V7,match=0,detail=$MISMATCH"
  fi

  VTK_VERSION=$(docker run --rm --network none --entrypoint "${PY[0]}" "$TAG_PIN" "${PY[@]:1}" \
    -c 'import importlib.metadata as m; print(m.version("vtk"))')
  MAPS=("$MAPDIR"/*.txt)
  python3 scripts/cad-measure/vtk_keeplist.py collect --vtk-version "$VTK_VERSION" --out "$KEEPLIST" "${MAPS[@]}" | while IFS= read -r l; do emit "$l"; done
  python3 scripts/cad-measure/vtk_keeplist.py collect --vtk-version "$VTK_VERSION" --out "$WORK/keeplist.again" "${MAPS[@]}" >/dev/null
  if cmp -s "$KEEPLIST" "$WORK/keeplist.again"; then SAME=1; else SAME=0; fi
  emit "KEEPLIST_REGEN,same=$SAME,entries=$(($(wc -l < "$KEEPLIST") - 2)),maps_files=${#MAPS[@]},vtk_version=$VTK_VERSION"

  [ -z "$MISMATCH" ] || { echo "expectation mismatch: a human decides before the pruning step" >&2; exit 3; }
  [ "$SAME" = 1 ] || exit 1
  exit 0
fi

if [ "$CMD" = prune-check ]; then
  [ -s "$KEEPLIST" ] || { echo "error: $KEEPLIST is missing; run libs first" >&2; exit 1; }
  TAG_FULL=$(cad_build_image "$PIN" full)
  TAG_PRUNED=$(cad_build_image "$PIN" pruned)
  cad_require_native "$TAG_FULL" || exit 1
  cad_require_native "$TAG_PRUNED" || exit 1
  FAILED=0

  emit "# Plan 71-07 prune check: the pruned image of one build against the full image (D-17)"
  cad_evidence_header "$TAG_PRUNED" 2 2g | tee -a "$OUT"
  emit "HDR,pin=$PIN,lp_threads=$LP,occt_parallel=$PAR,keeplist_sha256=$(shasum -a 256 "$KEEPLIST" | awk '{print $1}')"
  emit "PRUNE_BUILD,pin=$PIN,full=$TAG_FULL,pruned=$TAG_PRUNED,full_id=$(docker image inspect "$TAG_FULL" --format '{{.Id}}'),pruned_id=$(docker image inspect "$TAG_PRUNED" --format '{{.Id}}')"

  vtk_kb() {
    docker run --rm --network none --entrypoint sh "$1" -c \
      "du -sk --apparent-size $SITE/vtkmodules $SITE/vtk.libs 2>/dev/null | awk '{s += \$1} END {print s}'"
  }
  KB_FULL=$(vtk_kb "$TAG_FULL")
  KB_PRUNED=$(vtk_kb "$TAG_PRUNED")
  IMG_FULL=$(docker image inspect "$TAG_FULL" --format '{{.Size}}')
  IMG_PRUNED=$(docker image inspect "$TAG_PRUNED" --format '{{.Size}}')
  SIZE_LINE=$(awk -v kf="$KB_FULL" -v kp="$KB_PRUNED" -v bf="$IMG_FULL" -v bp="$IMG_PRUNED" 'BEGIN {
      dv = (kf - kp) / 1024; di = (bf - bp) / 1048576
      gap = di - dv; if (gap < 0) gap = -gap
      ok = (bp < bf && dv > 0 && gap <= 0.05 * dv) ? 1 : 0
      printf "PRUNE_SIZE,vtk_full_mib=%.1f,vtk_pruned_mib=%.1f,image_full_mib=%.1f,image_pruned_mib=%.1f,delta_vtk_mib=%.1f,delta_image_mib=%.1f,gap_mib=%.1f,gap_pct_of_delta_vtk=%.2f,whiteout_free=%d\n",
        kf / 1024, kp / 1024, bf / 1048576, bp / 1048576, dv, di, gap, (dv > 0 ? 100 * gap / dv : 0), ok }')
  emit "$SIZE_LINE"
  case "$SIZE_LINE" in *whiteout_free=1) ;; *) FAILED=1; emit "FAIL,reason=image size difference does not match the VTK size difference (whiteout or no reduction)" ;; esac

  STLDIR="$WORK/stl"; mkdir -p "$STLDIR"; chmod 0755 "$STLDIR"
  INPUTS="$WORK/inputs.tsv"; : > "$INPUTS"
  add_input() { printf '%s\t%s\t%s\t%s\n' "$1" "$2" "$3" "$4" >> "$INPUTS"; }

  while IFS= read -r f; do
    name=$(basename "$f")
    if mesh_plain "$TAG_FULL" "$f" "$STLDIR/mesh-$name.stl" 4g; then
      add_input "mesh-$name" "$STLDIR/mesh-$name.stl" 2g corpus
    else
      emit "PRUNE_SKIP,input=$name,reason=mesh-gave-no-stl-after-3-attempts"
    fi
  done < <(find "$CORPUS" -type f \( -iname '*.step' -o -iname '*.stp' -o -iname '*.iges' -o -iname '*.igs' \) | LC_ALL=C sort)
  while IFS= read -r f; do add_input "$(basename "$f")" "$f" 2g corpus; done \
    < <(find "$CORPUS" -type f -iname '*.stl' | LC_ALL=C sort)
  if [ -d "$CAD_CACHE_DIR/fetched" ]; then
    while IFS= read -r f; do
      name=$(basename "$f")
      case "$name" in
        *.stl|*.STL) add_input "fetched-$name" "$f" 4g fetched ;;
        *)
          if mesh_plain "$TAG_FULL" "$f" "$STLDIR/fetched-$name.stl" 4g; then
            add_input "fetched-$name" "$STLDIR/fetched-$name.stl" 4g fetched
          else
            emit "PRUNE_SKIP,input=$name,reason=mesh-gave-no-stl-after-3-attempts"
          fi ;;
      esac
    done < <(find "$CAD_CACHE_DIR/fetched" -type f | LC_ALL=C sort)
  fi

  RENDER_SH='in=$1; shift
for spec in "$@"; do
  view=${spec%%:*}; rest=${spec#*:}
  size=${rest%%:*}; rest=${rest#*:}
  bg=${rest%%:*}; lp=${rest#*:}
  rm -f /tmp/o.png /tmp/o.png.json
  /opt/venv/bin/python -I -B /opt/cad-measure/cad_render.py "$in" /tmp/o.png --view "$view" --size "$size" --background "$bg" --lp-threads "$lp" >/dev/null 2>&1
  rc=$?
  if [ -s /tmp/o.png ]; then sha=$(sha256sum /tmp/o.png | cut -d" " -f1); else sha=none; fi
  echo "PNG,rc=$rc,spec=$spec,sha=$sha"
done'

  render_batch() {
    local tag=$1 stl=$2 mem=$3 outf=$4 want attempt=1 got
    shift 4
    want=$#
    while [ "$attempt" -le 3 ]; do
      docker run --rm --network none --cpus 2 --memory "$mem" --memory-swap "$mem" --user nobody \
        -v "$stl:/in/in.stl:ro" --entrypoint sh "$tag" -c "$RENDER_SH" sh /in/in.stl "$@" > "$outf" 2>/dev/null </dev/null || true
      got=$(grep -c '^PNG,' "$outf" || true)
      [ "$got" = "$want" ] && return 0
      emit "RETRY,tag=$tag,input=$(basename "$stl"),attempt=$attempt,lines=$got,expected=$want"
      attempt=$((attempt + 1))
    done
    return 1
  }

  VIEW_SPECS=(); for v in "${VIEWS[@]}"; do VIEW_SPECS+=("$v:1280:white:$LP"); done
  BRANCH_SPECS=("iso:1280:transparent:$LP" "iso:512:white:$LP" "iso:2048:white:$LP" "iso:1280:white:1")
  ISO_SPEC="iso:1280:white:$LP"
  REAL_PART="mesh-LinearSlide-MGN15x-Block.step"

  PNGS=0; SAME=0; DIFFERENT=0; BROKEN=0
  : > "$WORK/ref.sha"
  while IFS="$(printf '\t')" read -r name stl mem kind; do
    case "$kind" in
      fetched) specs=("$ISO_SPEC") ;;
      *) specs=("${VIEW_SPECS[@]}"); [ "$name" != "$REAL_PART" ] || specs+=("${BRANCH_SPECS[@]}") ;;
    esac
    if ! render_batch "$TAG_FULL" "$stl" "$mem" "$WORK/png.full" "${specs[@]}" ||
       ! render_batch "$TAG_PRUNED" "$stl" "$mem" "$WORK/png.pruned" "${specs[@]}"; then
      emit "PRUNE_PNG,input=$name,view=all,size=-,bg=-,same=0,detail=container-gave-no-result-after-3-attempts"
      PNGS=$((PNGS + ${#specs[@]})); BROKEN=$((BROKEN + ${#specs[@]}))
      continue
    fi
    paste -d'#' "$WORK/png.full" "$WORK/png.pruned" > "$WORK/png.pair"
    while IFS='#' read -r lf lp; do
      spec=$(field "$lf" spec); rc_f=$(field "$lf" rc); sha_f=$(field "$lf" sha)
      rc_p=$(field "$lp" rc); sha_p=$(field "$lp" sha)
      IFS=: read -r view size bg lpt <<< "$spec"
      PNGS=$((PNGS + 1))
      if [ "$rc_f" = 0 ] && [ "$rc_p" = 0 ] && [ "$sha_f" != none ] && [ "$sha_p" != none ]; then
        if [ "$sha_f" = "$sha_p" ]; then
          same=1; SAME=$((SAME + 1))
        else
          same=0; DIFFERENT=$((DIFFERENT + 1))
        fi
        detail=""
      else
        same=0; BROKEN=$((BROKEN + 1)); detail=",detail=rc_full=$rc_f/rc_pruned=$rc_p"
      fi
      emit "PRUNE_PNG,input=$name,view=$view,size=$size,bg=$bg,same=$same,lp=$lpt,sha=${sha_f:0:16}$detail"
      printf '%s|%s\t%s\n' "$name" "$spec" "$sha_f" >> "$WORK/ref.sha"
    done < "$WORK/png.pair"
  done < "$INPUTS"
  emit "PRUNE_IDENTITY,pngs=$PNGS,identical=$SAME,different=$DIFFERENT,failed=$BROKEN"
  [ "$DIFFERENT" = 0 ] && [ "$BROKEN" = 0 ] && [ "$SAME" -gt 0 ] || { FAILED=1; emit "FAIL,reason=pruned renders differ from or fail against the full image"; }

  run_other() {
    local variant=$1 file=$2
    scripts/cad-rss-time-measure.sh --ocp "$PIN" --variant "$variant" --memory 2g --repeats 1 \
      --ops mesh,stlstats,dxf,rsvg-png --parallel "$PAR" --lp-threads "$LP" --out "$file" "$CORPUS" >/dev/null || true
  }
  run_other full "$WORK/other.full"
  run_other pruned "$WORK/other.pruned"
  OTHER_LINE=$(awk -F, '
    function kv(s, k,    n, i, parts) {
      n = split(s, parts, ",")
      for (i = 1; i <= n; i++) if (index(parts[i], k "=") == 1) return substr(parts[i], length(k) + 2)
      return ""
    }
    FNR == 1 { side = (FILENAME == ARGV[1]) ? "f" : "p" }
    /^ROW,/ {
      head = $0; sub(/,out=.*$/, "", head)
      out = $0; sub(/^.*,out=/, "", out)
      n = split(out, parts, ";"); o = ""
      for (i = 1; i <= n; i++) if (parts[i] !~ /^t_/) o = o parts[i] ";"
      key = kv(head, "sha") "|" kv(head, "op")
      val = "rc=" kv(head, "rc") "/pix=" kv(head, "pix") "/" o
      nm[key] = kv(head, "name")
      if (side == "f") full[key] = val; else pr[key] = val
    }
    END {
      for (k in full) {
        rows++
        if ((k in pr) && pr[k] == full[k]) same++
        else { diff++; printf "PRUNE_OTHER_DIFF,name=%s,op=%s,full=%s,pruned=%s\n", nm[k], substr(k, index(k, "|") + 1), full[k], (k in pr ? pr[k] : "missing") > "/dev/stderr" }
      }
      for (k in pr) if (!(k in full)) { rows++; diff++ }
      printf "PRUNE_OTHER,rows=%d,same=%d,diff=%d\n", rows, same, diff
    }' "$WORK/other.full" "$WORK/other.pruned" 2> "$WORK/other.diff")
  cat "$WORK/other.diff" | while IFS= read -r l; do emit "$l"; done
  emit "$OTHER_LINE"
  case "$OTHER_LINE" in *,diff=0) ;; *) FAILED=1 ;; esac

  K=$(mktemp -d "$WORK/stale.XXXXXX")
  chmod 0755 "$K"
  sed '1s/.*/vtk_version=9.7.0/' "$KEEPLIST" > "$K/stale.txt"
  chmod 0644 "$K/stale.txt"
  RC=0
  docker run --rm --network none --user root -v "$K:/k:ro" "$TAG_FULL" \
    "${PY[@]}" "$LIB/vtk_keeplist.py" prune --keeplist /k/stale.txt --site "$SITE" > "$K/out.version" 2>&1 || RC=$?
  if grep -q 'keep-list stale' "$K/out.version" && [ "$RC" -ne 0 ]; then
    emit "STALE_TEST,case=version,result=detected,rc=$RC,message=$(head -n 1 "$K/out.version")"
  else
    emit "STALE_TEST,case=version,result=MISSED,rc=$RC,message=$(head -n 1 "$K/out.version")"; FAILED=1
  fi

  grep -v 'vtkFiltersCore' "$KEEPLIST" > "$K/nomod.txt"
  chmod 0644 "$K/nomod.txt"
  REF_SHA=$(awk -F'\t' -v key="$REAL_PART|$ISO_SPEC" '$1 == key {print $2; exit}' "$WORK/ref.sha")
  RC=0
  docker run --rm --network none --user root -v "$K:/k:ro" -v "$STLDIR/$REAL_PART.stl:/in/in.stl:ro" --entrypoint sh "$TAG_FULL" -c '
    '"${PY[*]}"' '"$LIB"'/vtk_keeplist.py prune --keeplist /k/nomod.txt --site '"$SITE"'; echo "PRUNE_RC=$?"
    rm -f /tmp/o.png
    setpriv --reuid nobody --regid nogroup --clear-groups '"${PY[*]}"' '"$LIB"'/cad_render.py /in/in.stl /tmp/o.png --view iso --lp-threads '"$LP"' >/tmp/render.log 2>&1
    echo "RENDER_RC=$?"
    tail -n 1 /tmp/render.log
    if [ -s /tmp/o.png ]; then echo "SHA=$(sha256sum /tmp/o.png | cut -d" " -f1)"; else echo "SHA=none"; fi' > "$K/out.missing" 2>&1 || RC=$?
  P_RC=$(sed -n 's/^PRUNE_RC=//p' "$K/out.missing"); R_RC=$(sed -n 's/^RENDER_RC=//p' "$K/out.missing")
  R_SHA=$(sed -n 's/^SHA=//p' "$K/out.missing"); R_MSG=$(grep -a -v '^\(PRUNE_RC\|RENDER_RC\|SHA\|PRUNED\)' "$K/out.missing" | tail -n 1 | tr ',;' '__')
  if [ -n "$REF_SHA" ] && { [ "$R_RC" != 0 ] || [ "$R_SHA" != "$REF_SHA" ]; }; then
    emit "STALE_TEST,case=missing_module,result=detected,module=vtkFiltersCore,removed_entries=$(($(wc -l < "$KEEPLIST") - $(wc -l < "$K/nomod.txt"))),prune_rc=$P_RC,render_rc=$R_RC,sha=${R_SHA:0:16},reference_sha=${REF_SHA:0:16},message=$R_MSG"
  else
    emit "STALE_TEST,case=missing_module,result=MISSED,module=vtkFiltersCore,prune_rc=$P_RC,render_rc=$R_RC,sha=${R_SHA:-none},reference_sha=${REF_SHA:-none}"; FAILED=1
  fi

  [ "$FAILED" = 0 ] || exit 1
  exit 0
fi

echo "error: $CMD is not implemented in this version" >&2
exit 64

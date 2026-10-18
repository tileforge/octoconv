#!/usr/bin/env bash
# cad-prune-identity.sh -- Phase 71 (EVAL-01) iso renders of five real parts in two
# measurement images of one OCP pin, compared byte for byte.
#
# Usage:
#   scripts/cad-prune-identity.sh --old TAG --new TAG [--out FILE]
#
# The parts are meshed once in the --old image, then each STL is rendered (view iso,
# 1280, white, LP_NUM_THREADS from evidence/71-06-limits.txt) in both images, one
# container per render, --network none --cpus 2 --memory 2g --user nobody.
#
# Exit codes: 64 usage or two images that are not one pin, 69 docker missing, 1 a render or mesh
# failed, the PNGs differ, or fewer parts were compared than the script lists.

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"
. "$REPO_ROOT/scripts/cad-measure/verdicts.sh"

EVID="$REPO_ROOT/.planning/milestones/v1.16-phases/71-evaluation-gate/evidence"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad/public"
PARTS=(LinearSlide-MGN15x-Block.step LM8UU.step SC8UU.step V-Slot-20x20-100mm.step 2020_corner_bracket-Corner.step)
PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure

OLD=""
NEW=""
OUT="$EVID/71-07-prune-arch.txt"
while [ $# -gt 0 ]; do
  case "$1" in
    --old) OLD=${2:?}; shift 2 ;;
    --new) NEW=${2:?}; shift 2 ;;
    --out) OUT=${2:?}; shift 2 ;;
    *) sed -n '/^# Usage:/,/^# The parts/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 64 ;;
  esac
done
[ -n "$OLD" ] && [ -n "$NEW" ] || { echo "error: --old and --new are required" >&2; exit 64; }
command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }

LP=$(sed -n 's/^THREADS_DECISION,lp_num_threads=\([^,]*\),.*/\1/p' "$EVID/71-06-limits.txt")
PAR=$(sed -n 's/^THREADS_DECISION,lp_num_threads=[^,]*,occt_parallel=\([^,]*\),.*/\1/p' "$EVID/71-06-limits.txt")
[ -n "$LP" ] && [ -n "$PAR" ] || { echo "error: THREADS_DECISION missing in 71-06-limits.txt" >&2; exit 1; }
for t in "$OLD" "$NEW"; do
  docker image inspect "$t" >/dev/null 2>&1 || { echo "error: no such image $t" >&2; exit 1; }
  cad_require_native "$t" || exit 1
done
image_pair_verdict "$OLD" "$NEW" "$(docker image inspect "$OLD" --format '{{.Id}}')" "$(docker image inspect "$NEW" --format '{{.Id}}')" ||
  { echo "error: --old and --new must be two different images of one OCP pin" >&2; exit 64; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-prune-identity.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
chmod 0755 "$WORK"
mkdir -p "$(dirname "$OUT")"
: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }

mesh() {
  local src=$1 dst=$2 attempt=1 job
  while [ "$attempt" -le 3 ]; do
    job=$(mktemp -d "$WORK/job.XXXXXX")
    chmod 0777 "$job"
    docker run --rm --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody \
      -v "$job:/job" -v "$src:/job/in.step:ro" "$OLD" \
      "${PY[@]}" "$LIB/cad_mesh.py" mesh /job/in.step /job --parallel "$PAR" >/dev/null 2>&1 </dev/null || true
    if [ -s "$job/part.stl" ]; then cp "$job/part.stl" "$dst"; rm -rf "$job"; return 0; fi
    rm -rf "$job"
    attempt=$((attempt + 1))
  done
  return 1
}

render_sha() {
  local tag=$1 stl=$2 out
  out=$(docker run --rm --network none --cpus 2 --memory 2g --memory-swap 2g --user nobody \
    -v "$stl:/in/in.stl:ro" --entrypoint sh "$tag" -c '
      rm -f /tmp/o.png
      /opt/venv/bin/python -I -B /opt/cad-measure/cad_render.py /in/in.stl /tmp/o.png --view iso --size 1280 --background white --lp-threads "$1" >/dev/null 2>&1
      rc=$?
      if [ -s /tmp/o.png ]; then echo "$rc $(sha256sum /tmp/o.png | cut -d" " -f1)"; else echo "$rc none"; fi' sh "$LP" </dev/null 2>/dev/null || true)
  printf '%s\n' "${out:-1 none}"
}

emit "# Phase 71 image rebuild check: iso renders of five real parts, old image against new image"
cad_evidence_header "$NEW" 2 2g | tee -a "$OUT"
emit "HDR,old=$OLD,old_id=$(docker image inspect "$OLD" --format '{{.Id}}')"
emit "HDR,new=$NEW,new_id=$(docker image inspect "$NEW" --format '{{.Id}}')"
emit "HDR,lp_threads=$LP,occt_parallel=$PAR,view=iso,size=1280,background=white"

bytes_old=$(docker image inspect "$OLD" --format '{{.Size}}')
bytes_new=$(docker image inspect "$NEW" --format '{{.Size}}')
emit "IMAGE_BYTES,old=$bytes_old,new=$bytes_new,delta_bytes=$((bytes_new - bytes_old)),old_mib=$(awk -v b="$bytes_old" 'BEGIN { printf "%.1f", b / 1048576 }'),new_mib=$(awk -v b="$bytes_new" 'BEGIN { printf "%.1f", b / 1048576 }'),old_layers=$(docker image inspect "$OLD" --format '{{len .RootFS.Layers}}'),new_layers=$(docker image inspect "$NEW" --format '{{len .RootFS.Layers}}')"
tree_sha() {
  docker run --rm --network none --entrypoint sh "$1" -c '
    cd /opt && find venv cad-measure -type f -not -name "vtk_keeplist*" -not -path "cad-measure/__pycache__/*" -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum | cut -d" " -f1'
}
emit "TREE_SHA,old=$(tree_sha "$OLD"),new=$(tree_sha "$NEW"),scope=/opt/venv and /opt/cad-measure without vtk_keeplist and pyc of the helpers"

TOTAL=0; SAME=0; DIFF=0; BAD=0
for part in "${PARTS[@]}"; do
  TOTAL=$((TOTAL + 1))
  if ! mesh "$CORPUS/$part" "$WORK/part.stl"; then
    BAD=$((BAD + 1)); emit "IDENTITY_PNG,part=$part,same=0,detail=mesh-gave-no-stl-after-3-attempts"; continue
  fi
  read -r rc_o sha_o <<< "$(render_sha "$OLD" "$WORK/part.stl")"
  read -r rc_n sha_n <<< "$(render_sha "$NEW" "$WORK/part.stl")"
  if [ "$rc_o" = 0 ] && [ "$rc_n" = 0 ] && [ "$sha_o" != none ] && [ "$sha_n" != none ]; then
    if [ "$sha_o" = "$sha_n" ]; then SAME=$((SAME + 1)); same=1; else DIFF=$((DIFF + 1)); same=0; fi
    emit "IDENTITY_PNG,part=$part,same=$same,stl_bytes=$(wc -c < "$WORK/part.stl" | tr -d ' '),sha_old=$sha_o,sha_new=$sha_n"
  else
    BAD=$((BAD + 1))
    emit "IDENTITY_PNG,part=$part,same=0,detail=rc_old=$rc_o/rc_new=$rc_n"
  fi
done
emit "IDENTITY,parts=$TOTAL,identical=$SAME,different=$DIFF,failed=$BAD,expected=${#PARTS[@]}"
identity_verdict "$TOTAL" "$SAME" "$DIFF" "$BAD" "${#PARTS[@]}"

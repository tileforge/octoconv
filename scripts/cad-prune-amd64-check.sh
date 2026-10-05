#!/usr/bin/env bash
# cad-prune-amd64-check.sh -- Phase 71 (EVAL-01) the VTK prune on an amd64 build.
#
# Usage:
#   scripts/cad-prune-amd64-check.sh [--old-rev REV] [--ocp V] [--out FILE]
#
# Builds the measurement image for linux/amd64 (emulated; a build proof, not a
# measurement) with the committed keep-list and runs the prune and the import check
# of the final stage. With --old-rev it also builds the venv-pruned stage from the
# keep-list and vtk_keeplist.py of that revision and records the refusal.
# The images are tagged octoconv-cad-measure-amd64-proof and removed afterwards.
#
# Exit codes: 64 usage, 69 docker missing, 1 the committed keep-list failed on amd64
# or the old revision unexpectedly passed.

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
EVID="$REPO_ROOT/.planning/phases/71-evaluation-gate/evidence"
. "$REPO_ROOT/scripts/cad-measure/verdicts.sh"

OCP=7.9.3.1.1
OLD_REV=""
OUT="$EVID/71-07-prune-amd64.txt"
while [ $# -gt 0 ]; do
  case "$1" in
    --old-rev) OLD_REV=${2:?}; shift 2 ;;
    --ocp) OCP=${2:?}; shift 2 ;;
    --out) OUT=${2:?}; shift 2 ;;
    *) sed -n '/^# Usage:/,/^# The images/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 64 ;;
  esac
done
command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-prune-amd64.XXXXXX")
REPO=octoconv-cad-measure-amd64-proof
TAGS=""
trap 'for t in $TAGS; do docker rmi "$t" >/dev/null 2>&1 || true; done; rm -rf "$WORK"' EXIT
mkdir -p "$(dirname "$OUT")"
: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }

stage() {
  local dir=$1 rev=$2 f
  mkdir -p "$dir/scripts/cad-measure"
  cp Dockerfile.cad-measure "$dir/"
  cp scripts/cad-measure/requirements-*.txt "$dir/scripts/cad-measure/"
  if [ -z "$rev" ]; then
    cp scripts/cad-measure/*.py scripts/cad-measure/vtk-keeplist.txt "$dir/scripts/cad-measure/"
  else
    cp scripts/cad-measure/*.py "$dir/scripts/cad-measure/"
    for f in vtk_keeplist.py vtk-keeplist.txt; do git show "$rev:scripts/cad-measure/$f" > "$dir/scripts/cad-measure/$f"; done
  fi
}

emit "# Phase 71 amd64 prune build proof (emulated, not a measurement)"
emit "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),server_arch=$(docker version --format '{{.Server.Arch}}'),ocp=$OCP,platform=linux/amd64"
emit "HDR,keeplist_sha256=$(shasum -a 256 scripts/cad-measure/vtk-keeplist.txt | awk '{print $1}'),head=$(git rev-parse --short HEAD)"

FAILED=0
if [ -n "$OLD_REV" ]; then
  stage "$WORK/old" "$OLD_REV"
  rc=0
  docker buildx build --platform linux/amd64 --target venv-pruned --no-cache-filter venv-pruned --progress=plain \
    -f "$WORK/old/Dockerfile.cad-measure" --build-arg "OCP_VERSION=$OCP" --build-arg VENV_STAGE=venv-pruned \
    "$WORK/old" > "$WORK/old.log" 2>&1 || rc=$?
  msg=$(grep -a 'keep-list stale' "$WORK/old.log" | head -n 1 | sed 's/^#[0-9]* [0-9.]* //' | tr ',' ';' || true)
  if [ "$rc" != 0 ] && [ -n "$msg" ]; then
    emit "AMD64_PRUNE,list=old,rev=$(git rev-parse --short "$OLD_REV"),result=refused,build_rc=$rc,message=$msg"
  else
    emit "AMD64_PRUNE,list=old,rev=$(git rev-parse --short "$OLD_REV"),result=NOT-REFUSED,build_rc=$rc"
    FAILED=1
  fi
fi

stage "$WORK/new" ""
TAG="$REPO:pruned-amd64"
TAGS="$TAG"
rc=0
docker buildx build --platform linux/amd64 --load --no-cache-filter venv-pruned --progress=plain \
  -f "$WORK/new/Dockerfile.cad-measure" --build-arg "OCP_VERSION=$OCP" --build-arg VENV_STAGE=venv-pruned \
  -t "$TAG" "$WORK/new" > "$WORK/new.log" 2>&1 || rc=$?
pruned=$(grep -a -o 'PRUNED,kept=[0-9]*,removed=[0-9]*,bytes_removed=[0-9]*' "$WORK/new.log" | head -n 1 || true)
if [ "$rc" = 0 ] && [ -n "$pruned" ]; then
  arch=$(docker image inspect "$TAG" --format '{{.Architecture}}')
  emit "AMD64_PRUNE,list=new,result=built,$pruned,import_check=ok,image_arch=$arch,image_id=$(docker image inspect "$TAG" --format '{{.Id}}'),size_bytes=$(docker image inspect "$TAG" --format '{{.Size}}')"
  entries=$(($(wc -l < scripts/cad-measure/vtk-keeplist.txt) - 2))
  if kept_verdict "$entries" "$pruned"; then
    emit "AMD64_KEPT,entries=$entries,kept=$entries,all_listed_modules_present=1"
  else
    emit "AMD64_KEPT,entries=$entries,detail=$pruned,all_listed_modules_present=0"; FAILED=1
  fi
else
  emit "AMD64_PRUNE,list=new,result=FAILED,build_rc=$rc"
  tail -n 15 "$WORK/new.log" >&2
  FAILED=1
fi
emit "AMD64_SUMMARY,failed=$FAILED"
[ "$FAILED" = 0 ]

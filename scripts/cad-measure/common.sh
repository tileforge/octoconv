#!/usr/bin/env bash
# Shared helpers for the Phase 71 CAD measurement scripts.
#
# Source it:   . scripts/cad-measure/common.sh
# or run it:   scripts/cad-measure/common.sh tag   OCP VARIANT
#              scripts/cad-measure/common.sh build OCP VARIANT
# The tag goes to stdout, build logs to stderr.

CAD_REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
CAD_CACHE_DIR="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
CAD_PRIVATE_DIR="${OCTOCONV_CAD_PRIVATE_CORPUS:-}"

cad_log() { echo "$*" >&2; }

cad_inputs_hash() {
  (
    cd "$CAD_REPO_ROOT"
    {
      ls Dockerfile.cad-measure
      ls scripts/cad-measure/requirements-*.txt
      ls scripts/cad-measure/*.py
      if [ -f scripts/cad-measure/vtk-keeplist.txt ]; then ls scripts/cad-measure/vtk-keeplist.txt; fi
      if [ -d scripts/cad-measure/negative ]; then find scripts/cad-measure/negative -type f; fi
    } | LC_ALL=C sort | while IFS= read -r f; do
      printf '%s  %s\n' "$(shasum -a 256 "$f" | awk '{print $1}')" "$f"
    done | shasum -a 256 | cut -c1-12
  )
}

cad_image_tag() {
  local ocp=$1 variant=$2
  printf 'octoconv-cad-measure:ocp%s-%s-%s' "$ocp" "$variant" "$(cad_inputs_hash)"
}

cad_build_image() {
  local ocp=$1 variant=$2 tag stage
  tag=$(cad_image_tag "$ocp" "$variant")
  if docker image inspect "$tag" >/dev/null 2>&1; then
    printf '%s\n' "$tag"
    return 0
  fi
  stage=$(mktemp -d "${TMPDIR:-/tmp}/cad-build.XXXXXX")
  mkdir -p "$stage/scripts/cad-measure"
  cp "$CAD_REPO_ROOT/Dockerfile.cad-measure" "$stage/"
  cp "$CAD_REPO_ROOT"/scripts/cad-measure/requirements-*.txt "$CAD_REPO_ROOT"/scripts/cad-measure/*.py \
    "$stage/scripts/cad-measure/"
  cad_log "building $tag"
  if ! docker build -f "$stage/Dockerfile.cad-measure" --build-arg "OCP_VERSION=$ocp" -t "$tag" "$stage" >&2; then
    rm -rf "$stage"
    cad_log "error: image build failed"
    return 1
  fi
  rm -rf "$stage"
  printf '%s\n' "$tag"
}

cad_require_native() {
  local tag=$1 image_arch server_arch
  image_arch=$(docker image inspect "$tag" --format '{{.Architecture}}')
  server_arch=$(docker version --format '{{.Server.Arch}}')
  if [ -z "$image_arch" ] || [ "$image_arch" != "$server_arch" ]; then
    cad_log "emulated: not a measurement ($tag is '$image_arch' on a '$server_arch' server)"
    return 1
  fi
}

cad_evidence_header() {
  local tag=$1 cpus=$2 memory=$3 inner
  inner='
    printf "HDR,kernel=%s\n" "$(uname -r)"
    printf "HDR,python=%s\n" "$(/opt/venv/bin/python --version 2>&1)"
    /opt/venv/bin/python -I -B - <<PY
import importlib.metadata as m
for k, p in (("ocp", "cadquery-ocp-novtk"), ("vtk", "vtk"), ("ezdxf", "ezdxf")):
    print("HDR,%s=%s" % (k, m.version(p)))
PY
    printf "HDR,mesa=%s\n" "$(dpkg-query -W -f="\${Version}" libosmesa6)"
    printf "HDR,rsvg=%s\n" "$(rsvg-convert --version 2>&1 | head -n 1)"
  '
  printf 'HDR,date_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'HDR,server_arch=%s\n' "$(docker version --format '{{.Server.Arch}}')"
  printf 'HDR,image=%s\n' "$tag"
  printf 'HDR,image_id=%s\n' "$(docker image inspect "$tag" --format '{{.Id}}')"
  docker run --rm --network none --entrypoint sh "$tag" -c "$inner"
  printf 'HDR,cpus=%s\n' "$cpus"
  printf 'HDR,memory=%s\n' "$memory"
  printf 'HDR,host_noise=%s\n' "$(docker ps -q | wc -l | tr -d ' ')"
  printf 'HDR,go=%s\n' "$(go version 2>/dev/null || echo na)"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  set -euo pipefail
  case "${1:-}" in
    tag)   [ $# -eq 3 ] || { echo "usage: $0 tag OCP VARIANT" >&2; exit 64; }
           cad_image_tag "$2" "$3"; echo ;;
    build) [ $# -eq 3 ] || { echo "usage: $0 build OCP VARIANT" >&2; exit 64; }
           command -v docker >/dev/null || { echo "error: docker is required" >&2; exit 69; }
           cad_build_image "$2" "$3" ;;
    *)     echo "usage: $0 {tag|build} OCP VARIANT" >&2; exit 64 ;;
  esac
fi

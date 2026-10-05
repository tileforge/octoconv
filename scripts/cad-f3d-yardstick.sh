#!/usr/bin/env bash
# cad-f3d-yardstick.sh -- Phase 71 (EVAL-01, D-16) f3d 3.5.0 as a visual
# yardstick next to the VTK + OSMesa render. f3d has no Linux arm64 build, so it
# runs as an emulated linux/amd64 container without Landlock. That makes it a
# picture to compare against, never a timing, a sandbox proof or a reason to
# choose a renderer by itself.
#
# The same parts are meshed and rendered with VTK in the native measurement
# image, rendered again with f3d on the same STL, and tiled into one sheet.
#
# Usage:
#   scripts/cad-f3d-yardstick.sh [--out-dir DIR] [STL...]
#
# Without STL arguments the real parts of scripts/cad-render-measure.sh are used.
# Exit codes: 64 usage, 69 docker/gh missing, 1 download, build, render or sheet
# failure, or a digest mismatch.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"
. "$REPO_ROOT/scripts/cad-measure/verdicts.sh"

F3D_VERSION=3.5.0
F3D_ASSET=F3D-3.5.0-Linux-x86_64.tar.gz
F3D_SHA256=3e4590c709e6ecd14ec3088527dd625f1ed79b970009eb3a4baef9b410650f10
F3D_REPO=f3d-app/f3d

OCP=7.9.3.1.1
VARIANT=full
OUT_DIR="$REPO_ROOT/.planning/phases/71-evaluation-gate/evidence"
BUDGET=${OCTOCONV_CAD_CELL_BUDGET:-300}

usage() {
  sed -n '/^# Usage:/,/^# Exit codes/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

STL_ARGS=()
while [ $# -gt 0 ]; do
  case "$1" in
    --out-dir) OUT_DIR=${2:?}; shift 2 ;;
    -h|--help) usage ;;
    -*) echo "error: unknown argument $1" >&2; usage ;;
    *) STL_ARGS+=("$1"); shift ;;
  esac
done

for tool in docker gh curl shasum jq python3; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "error: $tool is required" >&2
    exit 69
  fi
done

mkdir -p "$OUT_DIR"
OUT_DIR=$(cd "$OUT_DIR" && pwd -P)
OUTFILE="$OUT_DIR/71-05-f3d.txt"
SHEET_OUT="$OUT_DIR/71-05-contact-f3d.png"
: > "$OUTFILE"
emit() { printf '%s\n' "$*" | tee -a "$OUTFILE"; }

BASE_IMAGE=$(sed -n 's/^BASE_IMAGE="\(.*\)"$/\1/p' "$REPO_ROOT/scripts/cad-measure/lock.sh")
if [ -z "$BASE_IMAGE" ]; then
  echo "error: BASE_IMAGE not found in scripts/cad-measure/lock.sh" >&2
  exit 1
fi

PY=(/opt/venv/bin/python -I -B)
LIB=/opt/cad-measure
LIMITS=(--network none --cpus 2 --memory 2g --memory-swap 2g --user nobody)
VIEWS=(iso front top)
SIZE=1280
TITLE="f3d 3.5.0, emulated amd64, no Landlock - yardstick only"

now_ms() { python3 -c 'import time; print(int(time.time() * 1000))'; }

emit "# Plan 71-05 f3d yardstick"
emit "# f3d runs as emulated linux/amd64 without Landlock: timings are pessimistic, not a basis for a decision, and nothing here is a sandbox proof"
emit "APPROVAL,item=libssl3,scope=one-off octoconv-f3d-yardstick image only,source=digest-pinned bookworm-slim apt,reason=f3d needs libcrypto.so.3,approved_by=user via coordinator relay,not_in=Dockerfile.cad-measure and production images"
emit "DEVIATION,item=f3d-asset,chose=$F3D_ASSET,instead_of=F3D-3.5.0-Linux-x86_64.tar.xz,reason=bookworm-slim has no xz and xz-utils would be another unpinned package"
emit "DEVIATION,item=contact_sheet.py,change=optional --title caption,reason=plan requires a caption on the sheet"

assets_line() {
  local tag=$1 json names linux x86 arm
  json=$(gh api "repos/$F3D_REPO/releases/tags/$tag")
  names=$(printf '%s' "$json" | jq -r '.assets[].name')
  linux=$(printf '%s\n' "$names" | grep -i 'linux' || true)
  x86=$(printf '%s\n' "$linux" | grep -ic 'x86_64' || true)
  arm=$(printf '%s\n' "$linux" | grep -Eic 'arm64|aarch64' || true)
  printf '%s\n' "$names" | while IFS= read -r n; do
    [ -n "$n" ] && printf 'F3D_ASSET_NAME,release=%s,name=%s\n' "$tag" "$n"
  done | tee -a "$OUTFILE" >/dev/null
  emit "F3D_ASSETS,release=$tag,linux_x86_64=$([ "$x86" -gt 0 ] && echo 1 || echo 0),linux_arm64=$([ "$arm" -gt 0 ] && echo 1 || echo 0)"
  if [ "$tag" = "v$F3D_VERSION" ]; then
    API_DIGEST=$(printf '%s' "$json" | jq -r --arg n "$F3D_ASSET" '.assets[] | select(.name == $n) | .digest')
  fi
}

API_DIGEST=""
assets_line "v$F3D_VERSION"
LATEST=$(gh api "repos/$F3D_REPO/releases/latest" --jq .tag_name)
emit "F3D_LATEST,release=$LATEST"
if [ "$LATEST" != "v$F3D_VERSION" ]; then
  assets_line "$LATEST"
fi

emit "F3D_PIN,version=$F3D_VERSION,asset=$F3D_ASSET,sha256=$F3D_SHA256,api_digest=${API_DIGEST:-none}"
if [ "$API_DIGEST" != "sha256:$F3D_SHA256" ]; then
  emit "error: F3D_SHA256 does not equal the GitHub asset digest ($API_DIGEST)"
  exit 1
fi

F3D_DIR="$CAD_CACHE_DIR/f3d"
mkdir -p "$F3D_DIR"
TARBALL="$F3D_DIR/$F3D_ASSET"
verify_tarball() {
  [ -f "$TARBALL" ] && [ "$(shasum -a 256 "$TARBALL" | awk '{print $1}')" = "$F3D_SHA256" ]
}
if ! verify_tarball; then
  rm -f "$TARBALL"
  cad_log "downloading $F3D_ASSET"
  curl -fsSL --retry 3 -o "$TARBALL" "https://github.com/$F3D_REPO/releases/download/v$F3D_VERSION/$F3D_ASSET"
  if ! verify_tarball; then
    rm -f "$TARBALL"
    emit "error: sha256 mismatch for $F3D_ASSET, removed"
    exit 1
  fi
fi
emit "F3D_TARBALL,sha256=$F3D_SHA256,bytes=$(wc -c < "$TARBALL" | tr -d ' '),verified=1"

DOCKERFILE=$(cat <<EOF
FROM $BASE_IMAGE
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \\
 && apt-get install -y --no-install-recommends libosmesa6 libgl1 libssl3 \\
 && ln -s libOSMesa.so.8 /usr/lib/x86_64-linux-gnu/libOSMesa.so \\
 && rm -rf /var/lib/apt/lists/*
COPY $F3D_ASSET /tmp/f3d.tar.gz
RUN mkdir /opt/f3d \\
 && tar -xzf /tmp/f3d.tar.gz -C /opt/f3d --strip-components=1 \\
 && rm /tmp/f3d.tar.gz \\
 && ln -s /opt/f3d/bin/f3d /usr/local/bin/f3d
USER nobody
EOF
)
F3D_HASH=$(printf '%s\n%s\n' "$DOCKERFILE" "$F3D_SHA256" | shasum -a 256 | cut -c1-12)
F3D_TAG="octoconv-f3d-yardstick:$F3D_HASH"
if ! docker image inspect "$F3D_TAG" >/dev/null 2>&1; then
  cad_log "building $F3D_TAG (linux/amd64, emulated)"
  printf '%s\n' "$DOCKERFILE" | docker build --platform linux/amd64 -f - -t "$F3D_TAG" "$F3D_DIR" >&2
fi

TAG=$(cad_build_image "$OCP" "$VARIANT")
cad_require_native "$TAG" || exit 1

emit "# measurement image header (VTK side, native)"
cad_evidence_header "$TAG" 2 2g | tee -a "$OUTFILE"
F3D_RUN=(docker run --rm --platform linux/amd64 "${LIMITS[@]}")
emit "HDR,f3d_image=$F3D_TAG"
emit "HDR,f3d_image_id=$(docker image inspect "$F3D_TAG" --format '{{.Id}}')"
emit "HDR,f3d_image_arch=$(docker image inspect "$F3D_TAG" --format '{{.Architecture}}')"
emit "HDR,f3d_version=$("${F3D_RUN[@]}" "$F3D_TAG" f3d --version 2>&1 | head -n 1)"
emit "HDR,f3d_backends=$("${F3D_RUN[@]}" "$F3D_TAG" f3d --no-config --list-rendering-backends 2>&1 | tr '\n' ';')"

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-f3d.XXXXXX")
trap 'rm -rf "$WORK"' EXIT
PNG="$WORK/png"
SHEETS="$WORK/sheets"
mkdir -p "$PNG" "$SHEETS" "$WORK/stl"
chmod 0777 "$PNG" "$SHEETS" "$WORK/stl"

PARTS=()
if [ ${#STL_ARGS[@]} -gt 0 ]; then
  for p in "${STL_ARGS[@]}"; do
    b=$(basename "$p")
    PARTS+=("${b%.*}|$(cd "$(dirname "$p")" && pwd -P)/$b")
  done
else
  CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
  while IFS= read -r rec; do
    rec=${rec#  \"}
    rec=${rec%\"}
    rec=${rec//\$CORPUS/$CORPUS}
    name=${rec%%|*}
    rest=${rec#*|}
    PARTS+=("$name|${rest%%|*}")
  done < <(sed -n '/^REAL=(/,/^)/p' "$REPO_ROOT/scripts/cad-render-measure.sh" | grep '^  "')
fi
if [ ${#PARTS[@]} -eq 0 ]; then
  emit "error: no parts"
  exit 1
fi

FAIL=0
RETRIES=0
CELL_DIR=""
CELL_WALL_MS=0

retry_run() {
  local fn=$1 expect=$2 what=$3 attempt=0 t0
  while [ "$attempt" -lt 3 ]; do
    attempt=$((attempt + 1))
    CELL_DIR=$(mktemp -d "$WORK/cell.XXXXXX")
    chmod 0777 "$CELL_DIR"
    t0=$(now_ms)
    if "$fn" >"$CELL_DIR/.log" 2>&1 && [ -s "$CELL_DIR/$expect" ]; then
      CELL_WALL_MS=$(($(now_ms) - t0))
      return 0
    fi
    RETRIES=$((RETRIES + 1))
    emit "RETRY,what=$what,attempt=$attempt,log=$(tr '\n' ' ' < "$CELL_DIR/.log" | cut -c1-200)"
    rm -rf "$CELL_DIR"
  done
  return 1
}

lower_ext() { printf '%s' "${1##*.}" | tr '[:upper:]' '[:lower:]'; }

CELL_SRC=""
CELL_EXT=""
CELL_STL=""
CELL_VIEW=""
CELL_ARGS=()

mesh_cell() {
  docker run --rm "${LIMITS[@]}" -v "$CELL_DIR:/job" -v "$CELL_SRC:/job/in.$CELL_EXT:ro" "$TAG" \
    "${PY[@]}" "$LIB/cad_mesh.py" mesh "/job/in.$CELL_EXT" /job --quality standard
}

vtk_cell() {
  docker run --rm "${LIMITS[@]}" -v "$CELL_DIR:/job" -v "$CELL_STL:/in:ro" "$TAG" \
    "${PY[@]}" "$LIB/cad_render.py" /in/part.stl /job/render.png --view "$CELL_VIEW" --size "$SIZE" \
    --background white --lp-threads default
}

f3d_cell() {
  "${F3D_RUN[@]}" -e HOME=/job -e XDG_CACHE_HOME=/job/.cache -v "$CELL_DIR:/job" -v "$CELL_STL:/in:ro" "$F3D_TAG" \
    f3d /in/part.stl --output=/job/render.png "${CELL_ARGS[@]}"
}

# f3d ignores --camera-view-up when the camera direction is parallel to --up, so
# the top view needs --up=+Y to put +Y at the top of the frame as the VTK helper does.
# f3d --camera-direction is the direction the camera looks along; the VTK
# helper places the camera at centre + direction, so the f3d flag is the negation.
f3d_view_flags() {
  case "$1" in
    iso)   echo "--up=+Z --camera-direction=-1,1,-1 --camera-view-up=0,0,1" ;;
    front) echo "--up=+Z --camera-direction=0,1,0 --camera-view-up=0,0,1" ;;
    top)   echo "--up=+Y --camera-direction=0,0,-1 --camera-view-up=0,1,0" ;;
  esac
}

F3D_COMMON="--no-config --rendering-backend=osmesa --resolution=$SIZE,$SIZE --background-color=1,1,1 --camera-orthographic --grid=false --axis=false --filename=false --metadata=false"

F3D_HELP=$("${F3D_RUN[@]}" "$F3D_TAG" f3d --help 2>&1 || true)
for flag in no-config rendering-backend resolution up camera-orthographic grid axis filename metadata background-color camera-direction camera-view-up output; do
  if printf '%s\n' "$F3D_HELP" | grep -q -- "--$flag"; then
    emit "F3D_FLAG,name=--$flag,present=1"
  else
    emit "F3D_FLAG,name=--$flag,present=0"
    FAIL=$((FAIL + 1))
  fi
done
if [ "$FAIL" -gt 0 ]; then
  emit "error: f3d flags missing, not rendering"
  exit 1
fi
emit "F3D_NOTE,top view uses --up=+Y because f3d ignores --camera-view-up when the direction is parallel to --up (measured: with --up=+Z the part is rotated 90 degrees against VTK)"
emit "F3D_NOTE,--edges is not used: f3d --edges draws every triangle edge, VTK draws feature edges only, so the default f3d look is the fair comparison"

pixel_tool() {
  docker run --rm --network none --user nobody -v "$PNG:/png:ro" "$TAG" "${PY[@]}" "$LIB/pixelcheck.py" "$@" 2>/dev/null \
    | grep -a -E '^PIX,' | tail -n 1 || true
}

NAMES=()
FILES=()
F3D_OK=0
VTK_OK=0
for rec in "${PARTS[@]}"; do
  name=${rec%%|*}
  path=${rec#*|}
  NAMES+=("$name")
  ext=$(lower_ext "$path")
  sdir="$WORK/stl/$name"
  mkdir -p "$sdir"
  chmod 0777 "$sdir"
  if [ "$ext" = stl ]; then
    cp "$path" "$sdir/part.stl"
    emit "MESH,item=$name,source=stl-input"
  else
    CELL_SRC=$path
    CELL_EXT=$ext
    if retry_run mesh_cell part.stl "mesh-$name"; then
      cp "$CELL_DIR/part.stl" "$sdir/part.stl"
      emit "MESH,item=$name,quality=standard,wall_ms=$CELL_WALL_MS,src=$(basename "$path")"
      rm -rf "$CELL_DIR"
    else
      emit "MESH,item=$name,status=failed"
      FAIL=$((FAIL + 1))
      for v in "${VIEWS[@]}"; do FILES+=("/png/MISSING-vtk-$name-$v.png" "/png/MISSING-f3d-$name-$v.png"); done
      continue
    fi
  fi
  chmod 0644 "$sdir/part.stl"
  CELL_STL=$sdir
  for v in "${VIEWS[@]}"; do
    CELL_VIEW=$v
    if retry_run vtk_cell render.png "vtk-$name-$v"; then
      cp "$CELL_DIR/render.png" "$PNG/vtk_${name}_$v.png"
      VTK_OK=$((VTK_OK + 1))
      emit "VTK,item=$name,view=$v,wall_ms=$CELL_WALL_MS,arch=native"
      rm -rf "$CELL_DIR"
    else
      emit "VTK,item=$name,view=$v,status=failed"
      FAIL=$((FAIL + 1))
    fi
    read -r -a CELL_ARGS <<< "$F3D_COMMON $(f3d_view_flags "$v")"
    if retry_run f3d_cell render.png "f3d-$name-$v"; then
      cp "$CELL_DIR/render.png" "$PNG/f3d_${name}_$v.png"
      F3D_OK=$((F3D_OK + 1))
      emit "F3D,item=$name,view=$v,wall_ms=$CELL_WALL_MS,arch=emulated-amd64,note=emulation-pessimistic-not-a-basis-for-a-decision"
      emit "F3DCMD,item=$name,view=$v,cmd=docker run --rm --platform linux/amd64 ${LIMITS[*]} -e HOME=/job -e XDG_CACHE_HOME=/job/.cache -v STLDIR:/in:ro -v OUTDIR:/job $F3D_TAG f3d /in/part.stl --output=/job/render.png ${CELL_ARGS[*]}"
      rm -rf "$CELL_DIR"
      line=$(pixel_tool single "/png/f3d_${name}_$v.png" --mode part)
      emit "F3DPIX,item=$name,view=$v,informative=1,${line#PIX,}"
    else
      emit "F3D,item=$name,view=$v,status=failed"
      FAIL=$((FAIL + 1))
      if [ "$F3D_OK" -eq 0 ]; then
        emit "error: the first f3d render failed, stopping"
        exit 1
      fi
    fi
    FILES+=("/png/vtk_${name}_$v.png" "/png/f3d_${name}_$v.png")
  done
done
chmod 0644 "$PNG"/*.png 2>/dev/null || true

cols=""
for v in "${VIEWS[@]}"; do
  cols="${cols:+$cols,}VTK $v,f3d $v"
done
rows=$(IFS=,; printf '%s' "${NAMES[*]}")
attempt=1
SHEET_OK=0
while [ "$attempt" -le 2 ]; do
  if docker run --rm --network none --user nobody -v "$PNG:/png:ro" -v "$SHEETS:/out" "$TAG" \
       "${PY[@]}" "$LIB/contact_sheet.py" --out /out/sheet.png --rows "$rows" --cols "$cols" \
       --title "$TITLE" "${FILES[@]}" >/dev/null && [ -s "$SHEETS/sheet.png" ]; then
    cp "$SHEETS/sheet.png" "$SHEET_OUT"
    SHEET_OK=1
    emit "SHEET,file=$(basename "$SHEET_OUT"),bytes=$(wc -c < "$SHEET_OUT" | tr -d ' '),title=$TITLE"
    break
  fi
  RETRIES=$((RETRIES + 1))
  emit "RETRY,what=sheet,attempt=$attempt"
  attempt=$((attempt + 1))
done
if [ "$SHEET_OK" != 1 ]; then
  emit "SHEET,status=failed"
  FAIL=$((FAIL + 1))
fi

EXPECT_RENDERS=$(( ${#NAMES[@]} * ${#VIEWS[@]} ))
if f3d_verdict "$FAIL" "$VTK_OK" "$F3D_OK" "$EXPECT_RENDERS" "$SHEET_OK"; then
  emit "F3D_SUMMARY,parts=${#NAMES[@]},fail=$FAIL,retries=$RETRIES,vtk_renders=$VTK_OK,f3d_renders=$F3D_OK,expected_renders=$EXPECT_RENDERS,sheet=1"
else
  emit "F3D_SUMMARY,status=FAILED,parts=${#NAMES[@]},fail=$FAIL,retries=$RETRIES,vtk_renders=$VTK_OK,f3d_renders=$F3D_OK,expected_renders=$EXPECT_RENDERS,sheet=$SHEET_OK"
  exit 1
fi

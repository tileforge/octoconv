#!/usr/bin/env bash

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

PHASE_DIR="$REPO_ROOT/.planning/phases/74-3d-render-measurements"
EVID="$PHASE_DIR/evidence/74-01-wave0.txt"
EVID71="$REPO_ROOT/.planning/phases/71-evaluation-gate/evidence"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
CAD_CACHE="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
OLD_IMAGE="octoconv-cad-measure:ocp7.9.3.1.1-pruned-dc621b94ee5d"
DOCKERFILE="$REPO_ROOT/Dockerfile.cad-worker"
WRAP_PY="$REPO_ROOT/scripts/cad-measure/measure_wrap.py"
GEN_PY="$REPO_ROOT/scripts/cad-corpus-gen.py"
UNIT_PY="$REPO_ROOT/scripts/cad-unit-probe.py"
GO_TOOLCHAIN=go1.26.4
BASE_HELPER_REV=e3cab948c8a5a29d06791d1caa87c921aa01ecce
WALL_LIMIT_S=210
RSS_LIMIT_MIB=2867.2
STEP_COPIES_TARGET=65
STEP_COPIES_SHA_73=d5dc17ee0168ec32d3b6d85c54ae37d8f428c9bc164762b39d1e10ea729fc47e
MIN_FREE_GIB=6
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)
PYV=(/opt/venv/bin/python -I -B)
LOCKED=(requirements-vtk.txt vtk_keeplist.py vtk-keeplist.txt)
IDENTITY_PARTS=(LinearSlide-MGN15x-Block.step LM8UU.step SC8UU.step V-Slot-20x20-100mm.step 2020_corner_bracket-Corner.step)
RENDER_RW=/job
OUT_OVERRIDE=""

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/cad-render-probe.sh tracer   [--out FILE]
  scripts/cad-render-probe.sh identity [--out FILE]
  scripts/cad-render-probe.sh cost     [--out FILE]
  scripts/cad-render-probe.sh units    [--out FILE]
  scripts/cad-render-probe.sh mesh     [--out FILE]
  scripts/cad-render-probe.sh judge    [--out FILE]

tracer    build the engine image of Dockerfile.cad-worker, mesh two real STEP files with
          cad-mesh, render each STL with cad-render under a real Landlock ruleset (iso 1280
          opaque, iso 1280 transparent, top 2048 opaque), judge every PNG with pixelcheck.py
          from the 71 measurement image, and prove that an STL outside every grant yields
          no PNG.
identity  render five real parts with the 71 cad_render.py in the 71 pruned image and with
          cad-render in the engine image (iso 1280, LP_NUM_THREADS=2) and compare the PNGs
          byte for byte.
cost      render the 5944-triangle part and the heaviest in-cap 71 STL at 1280 and 2048 px
          under the production limits, twice each, and derive the triangle cap and the STL
          byte ceiling from the measured frame cost.
units     measure the scale OCP applied to inch STEP against the file's own coordinates and
          derive the runtime source of that factor.

mesh      run cad-mesh --measure on the corpus and the stats path on STL files on the engine
          image, order the triangle counts of the three qualities, prove the standard STL is
          byte-identical to the pre-change helper, and measure wall and memory at the STEP
          and STL ceilings. Writes evidence/74-02-mesh.txt.

judge     render the five identity parts in all seven views at 1280, the 71 ladder ASCII slice
          (W15) at 256, 1280 and 2048, two parts at 256, 512 and 2048, and each part transparent,
          under the production limits and a real Landlock ruleset; judge every PNG with the Go
          judge (scripts/cmd/cad-pixel-judge) and with pixelcheck.py from the 71 image; list
          every disagreement. Writes evidence/74-06-judge.txt.

Every mode rewrites its own lines in the evidence file (default
.planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt, mesh mode
74-02-mesh.txt) and removes the image tags it built. Needs a native arm64 docker server, go and python3.
Exit codes: 64 usage, 69 a tool is missing, 1 a check failed.
USAGE
  exit 64
}

fail() { echo "FAIL: $*" >&2; exit 1; }

need_tools() {
  command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
  command -v go >/dev/null 2>&1 || { echo "error: go is required to build the probe" >&2; exit 69; }
  command -v python3 >/dev/null 2>&1 || { echo "error: python3 is required" >&2; exit 69; }
}

sha_of() { shasum -a 256 "$1" | awk '{print $1}'; }

free_gib() { df -k / | awk 'NR == 2 { print int($4 / 1048576) }'; }

disk_guard() {
  local free
  free=$(free_gib)
  if [ "$free" -lt "$MIN_FREE_GIB" ]; then
    echo "STOP,reason=disk_below_threshold,free_gib=$free,threshold_gib=$MIN_FREE_GIB" >&2
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

render_ro_for() {
  printf '/opt/venv:/usr/bin/python3.11:/usr/lib/python3.11:/usr/lib/%s:/usr/local/bin/cad-render' "$(triplet_for "$1")"
}

lock_line() {
  local f dirt=0 shas=""
  for f in "${LOCKED[@]}"; do
    if ! cmp -s "$REPO_ROOT/scripts/cad-measure/$f" "$REPO_ROOT/cmd/cad-worker/$f"; then
      printf 'LOCK,file=%s,result=fail\n' "$f"
      return 1
    fi
    shas="$shas$(sha_of "$REPO_ROOT/cmd/cad-worker/$f" | cut -c1-12)+"
  done
  [ "$(grep -c 'hash=sha256:' "$REPO_ROOT/cmd/cad-worker/requirements-vtk.txt")" = 2 ] || dirt=1
  [ "$(grep -cE '^vtk==9\.7\.1 ' "$REPO_ROOT/cmd/cad-worker/requirements-vtk.txt")" = 1 ] || dirt=1
  [ "$dirt" = 0 ] || { printf 'LOCK,file=requirements-vtk.txt,result=fail,reason=shape\n'; return 1; }
  printf 'LOCK,files=%s,sha256_12=%s,cmp=identical,vtk=9.7.1,hashes=2,result=ok\n' "${LOCKED[*]}" "${shas%+}"
}

inputs_short() {
  {
    shasum -a 256 "$DOCKERFILE"
    for f in "${LOCKED[@]}"; do shasum -a 256 "$REPO_ROOT/cmd/cad-worker/$f"; done
    shasum -a 256 "$REPO_ROOT/cmd/cad-worker/requirements-mesh.txt" \
      "$REPO_ROOT/cmd/cad-worker/helper/cad-mesh" "$REPO_ROOT/cmd/cad-worker/helper/cad-render" \
      "$REPO_ROOT/cmd/cad-worker/helper/cad-guard"
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
  tag="octoconv-cad-worker:engine74-$(inputs_short)"
  if docker image inspect "$tag" >/dev/null 2>&1; then
    printf '%s\n' "$tag"
    return 0
  fi
  stage=$(mktemp -d "${TMPDIR:-/tmp}/cad-render-build.XXXXXX")
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
  d=$(mktemp -d "$1/job.XXXXXX")
  chmod 0777 "$d"
  printf '%s\n' "$d"
}

probe_refused() {
  grep -aqE 'the ruleset confines nothing|is not readable before the ruleset|not a permission denial|^apply:' "$1"
}

tracked_dirt() {
  git status --porcelain --untracked-files=no | awk '{print $2}' | grep -vc "evidence/74-01-wave0.txt" || true
}

dpkg_osmesa() {
  docker run --rm --network none --entrypoint dpkg-query "$1" -W -f='${Version}' libosmesa6
}

header_lines() {
  local mode=$1 tag=$2
  printf 'HDR,stage=%s,date_utc=%s\n' "$mode" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  printf 'HDR,stage=%s,commit=%s,tracked_dirt=%s\n' "$mode" "$(git rev-parse HEAD)" "$(tracked_dirt)"
  printf 'HDR,stage=%s,image=%s\n' "$mode" "$tag"
  printf 'HDR,stage=%s,image_id=%s\n' "$mode" "$(docker image inspect "$tag" --format '{{.Id}}')"
  printf 'HDR,stage=%s,server_arch=%s\n' "$mode" "$(docker version --format '{{.Server.Arch}}')"
  printf 'HDR,stage=%s,kernel=%s\n' "$mode" "$(docker run --rm --network none --entrypoint uname "$tag" -r)"
  printf 'HDR,stage=%s,libosmesa6_new=%s,libosmesa6_71=%s\n' "$mode" "$(dpkg_osmesa "$tag")" "$(dpkg_osmesa "$OLD_IMAGE")"
  printf 'HDR,stage=%s,limits=cpus2_memory4g_network_none_user_nobody,env=HOME=/job_LANG=C.UTF-8_TMPDIR=/job_LP_NUM_THREADS=2\n' "$mode"
}

evid_put() {
  local lines=$1 pattern=$2 keep
  mkdir -p "$(dirname "$EVID")"
  keep=$(mktemp "${TMPDIR:-/tmp}/cad-render-keep.XXXXXX")
  if [ -f "$EVID" ]; then grep -vE "$pattern" "$EVID" > "$keep" || true; fi
  { cat "$keep" "$lines"; } > "$EVID"
  rm -f "$keep"
}

setup() {
  need_tools
  [ "$(docker version --format '{{.Server.Arch}}')" = arm64 ] || fail "docker server is not arm64"
  [ "$(GOTOOLCHAIN=$GO_TOOLCHAIN go env GOVERSION)" = "$GO_TOOLCHAIN" ] || fail "go toolchain is not $GO_TOOLCHAIN"
  docker image inspect "$OLD_IMAGE" >/dev/null 2>&1 || fail "the 71 pruned image $OLD_IMAGE is missing"
  cad_require_native "$OLD_IMAGE" || fail "71 image is not native"
  disk_guard
  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-render-probe.XXXXXX")
  chmod 0755 "$WORK"
  trap cleanup EXIT
  LOCK_LINE=$(lock_line) || { echo "$LOCK_LINE" >&2; fail "lock copies differ from the 71 sources"; }
  TAG=$(build_engine)
  BUILT_TAGS+=("$TAG")
  cad_require_native "$TAG" || fail "engine image is not native"
  ARCH=$(docker image inspect "$TAG" --format '{{.Architecture}}')
  RENDER_RO=$(render_ro_for "$ARCH")
  build_probe "$ARCH" "$WORK/probe"
  LINES="$WORK/lines.txt"
  : > "$LINES"
}

pix_of() {
  local png=$1 kind=${2:-single}
  docker run --rm --network none --user nobody -v "$png:/in/o.png:ro" --entrypoint "${PYV[0]}" "$OLD_IMAGE" \
    "${PYV[@]:1}" /opt/cad-measure/pixelcheck.py "$kind" /in/o.png 2>/dev/null </dev/null || true
}

pix_ok() { printf '%s' "$1" | grep -q 'ok=1$'; }

pix_detail() { printf '%s' "$1" | head -n 1 | sed -e 's/^[A-Z]*,//' -e 's/,/;/g'; }

png_dims() {
  python3 - "$1" <<'PY'
import struct, sys
with open(sys.argv[1], "rb") as fh:
    head = fh.read(24)
if head[:8] != b"\x89PNG\r\n\x1a\n" or head[12:16] != b"IHDR":
    print("0 0")
else:
    w, h = struct.unpack(">II", head[16:24])
    print(w, h)
PY
}

verdict_fields() {
  python3 - "$1" "$2" "$3" "$4" <<'PY'
import json, sys
path, view, size, bg = sys.argv[1:5]
last = ""
with open(path, "rb") as fh:
    for raw in fh:
        line = raw.decode("ascii", "replace").rstrip("\n")
        if line.startswith("CADVERDICT "):
            last = line
if not last:
    print("verdict=none,verdict_match=0,triangles=na")
    sys.exit(0)
try:
    obj = json.loads(last[len("CADVERDICT "):])
except ValueError:
    print("verdict=unparsable,verdict_match=0,triangles=na")
    sys.exit(0)
match = (
    obj.get("stage") == "render" and obj.get("verdict") == "ok"
    and obj.get("view") == view and str(obj.get("size")) == size and obj.get("background") == bg
)
print("verdict=%s,verdict_match=%d,triangles=%s" % (obj.get("verdict"), 1 if match else 0, obj.get("triangles", "na")))
PY
}

mesh_prod() {
  local src=$1 fmt=$2 dst=$3 job log rc=0
  job=$(new_job "$WORK")
  log="$WORK/mesh.log"
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -v "$job:/job" -v "$src:/job/in.$fmt:ro" "$TAG" \
    /usr/local/bin/cad-mesh --format "$fmt" /job/in.$fmt /job/out.stl > "$log" 2>&1 </dev/null || rc=$?
  if [ "$rc" = 0 ] && [ -s "$job/out.stl" ]; then
    cp "$job/out.stl" "$dst"
    rm -rf "$job"
    return 0
  fi
  tail -n 5 "$log" >&2
  rm -rf "$job"
  return 1
}

run_render_confined() {
  local stl=$1 view=$2 size=$3 bg=$4 job=$5 stdout=$6 stderr=$7 rc=0
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -e "PROBE_RO=$RENDER_RO" -e PROBE_RW="$RENDER_RW" \
    --entrypoint /probe/sandbox-probe -v "$WORK/probe:/probe:ro" \
    -v "$job:/job" -v "$stl:/job/in.stl:ro" "$TAG" \
    /usr/local/bin/cad-render --view "$view" --size "$size" --background "$bg" /job/in.stl /job/out.png \
    > "$stdout" 2> "$stderr" </dev/null || rc=$?
  return $rc
}

tracer() {
  setup
  local ok=1 name src fmt stl job so se rc view size bg kind pix tpix vf dims w h result stage_ok
  {
    printf '%s\n' "$LOCK_LINE"
    header_lines tracer "$TAG"
  } >> "$LINES"

  for name in units_inch LinearSlide; do
    case "$name" in
      units_inch) src="$CORPUS/synthetic/units_inch.step" ;;
      LinearSlide) src="$CORPUS/public/LinearSlide-MGN15x-Block.step" ;;
    esac
    fmt=step
    stl="$WORK/$name.stl"
    if ! mesh_prod "$src" "$fmt" "$stl"; then
      printf 'TRACER,input=%s,result=fail,reason=mesh_failed\n' "$name" >> "$LINES"
      ok=0
      continue
    fi
    for spec in iso:1280:opaque iso:1280:transparent top:2048:opaque; do
      disk_guard
      view=${spec%%:*}
      size=${spec#*:}; size=${size%%:*}
      bg=${spec##*:}
      job=$(new_job "$WORK")
      so="$WORK/render.out"; se="$WORK/render.err"
      rc=0
      run_render_confined "$stl" "$view" "$size" "$bg" "$job" "$so" "$se" || rc=$?
      result=fail
      pix=""; tpix=""; w=0; h=0
      if probe_refused "$se" || probe_refused "$so"; then
        vf="verdict=none,verdict_match=0,triangles=na"
        pix="probe_refused"
      elif [ ! -s "$job/out.png" ]; then
        vf=$(verdict_fields "$so" "$view" "$size" "$bg")
        pix="no_png"
        tail -n 8 "$se" >&2
      else
        vf=$(verdict_fields "$so" "$view" "$size" "$bg")
        read -r w h <<< "$(png_dims "$job/out.png")"
        pix=$(pix_of "$job/out.png" single)
        stage_ok=1
        pix_ok "$pix" || stage_ok=0
        if [ "$bg" = transparent ]; then
          tpix=$(pix_of "$job/out.png" transparent)
          pix_ok "$tpix" || stage_ok=0
        fi
        case "$vf" in *verdict_match=1*) ;; *) stage_ok=0 ;; esac
        [ "$rc" = 0 ] || stage_ok=0
        [ "$w" = "$size" ] && [ "$h" = "$size" ] || stage_ok=0
        [ "$stage_ok" = 1 ] && result=ok
      fi
      [ "$result" = ok ] || ok=0
      printf 'TRACER,input=%s,view=%s,size=%s,background=%s,rc=%s,confined=1,%s,png_w=%s,png_h=%s,png_bytes=%s,pix_ok=%s,pix=%s,tpix=%s,result=%s\n' \
        "$name" "$view" "$size" "$bg" "$rc" "$vf" "$w" "$h" "$( [ -f "$job/out.png" ] && wc -c < "$job/out.png" | tr -d ' ' || echo 0 )" \
        "$(pix_ok "$pix" && echo 1 || echo 0)" "$(pix_detail "$pix")" "$(pix_detail "$tpix")" "$result" >> "$LINES"
      rm -rf "$job"
    done
  done

  local ctl_job ctl_rc=0 neg_job neg_rc=0 control png_state verdict_ok neg_result=fail
  ctl_job=$(new_job "$WORK")
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -v "$ctl_job:/job" -v "$WORK/LinearSlide.stl:/outside/in.stl:ro" "$TAG" \
    /usr/local/bin/cad-render --view iso --size 1280 --background opaque /outside/in.stl /job/out.png \
    > "$WORK/ctl.out" 2> "$WORK/ctl.err" </dev/null || ctl_rc=$?
  if [ "$ctl_rc" = 0 ] && [ -s "$ctl_job/out.png" ] && pix_ok "$(pix_of "$ctl_job/out.png" single)"; then
    control=unconfined_ok
  else
    control=unconfined_failed
  fi
  rm -rf "$ctl_job"

  neg_job=$(new_job "$WORK")
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -e "PROBE_RO=$RENDER_RO" -e PROBE_RW="$RENDER_RW" \
    --entrypoint /probe/sandbox-probe -v "$WORK/probe:/probe:ro" \
    -v "$neg_job:/job" -v "$WORK/LinearSlide.stl:/outside/in.stl:ro" "$TAG" \
    /usr/local/bin/cad-render --view iso --size 1280 --background opaque /outside/in.stl /job/out.png \
    > "$WORK/neg.out" 2> "$WORK/neg.err" </dev/null || neg_rc=$?
  verdict_ok=$(grep -ac '"verdict":"ok"' "$WORK/neg.out" || true)
  if [ -s "$neg_job/out.png" ]; then png_state=present; else png_state=absent; fi
  if probe_refused "$WORK/neg.err" || probe_refused "$WORK/neg.out"; then
    png_state=probe_refused
  elif [ "$png_state" = absent ] && [ "$verdict_ok" = 0 ] && [ "$control" = unconfined_ok ]; then
    neg_result=denied
  fi
  [ "$neg_result" = denied ] || ok=0
  printf 'TRACER,input=LinearSlide_outside,rc=%s,confined=1,png=%s,verdict_ok_lines=%s,control=%s,control_rc=%s,result=%s\n' \
    "$neg_rc" "$png_state" "$verdict_ok" "$control" "$ctl_rc" "$neg_result" >> "$LINES"
  rm -rf "$neg_job"

  printf 'SUMMARY,stage=tracer,runs_ok=%s,runs_total=%s,negative=%s,control=%s\n' \
    "$(grep -c '^TRACER,input=[A-Za-z_]*,view=.*result=ok$' "$LINES" || true)" 6 "$neg_result" "$control" >> "$LINES"
  evid_put "$LINES" '^HDR,stage=tracer,|^LOCK,|^TRACER,|^SUMMARY,stage=tracer,'
  cat "$LINES"
  if [ "$ok" = 1 ]; then
    echo TRACER-OK
    return 0
  fi
  echo TRACER-FAILED >&2
  return 1
}

mesh_old() {
  local src=$1 dst=$2 par=$3 attempt=1 job
  while [ "$attempt" -le 3 ]; do
    job=$(new_job "$WORK")
    docker run --rm "${LIMITS[@]}" -v "$job:/job" -v "$src:/job/in.step:ro" "$OLD_IMAGE" \
      "${PYV[@]}" /opt/cad-measure/cad_mesh.py mesh /job/in.step /job --parallel "$par" >/dev/null 2>&1 </dev/null || true
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

render_old() {
  local stl=$1 png=$2 lp=$3
  docker run --rm --network none --cpus 2 --memory 2g --memory-swap 2g --user nobody \
    -v "$stl:/in/in.stl:ro" -v "$(dirname "$png"):/out" --entrypoint "${PYV[0]}" "$OLD_IMAGE" \
    "${PYV[@]:1}" /opt/cad-measure/cad_render.py /in/in.stl "/out/$(basename "$png")" --view iso --size 1280 \
    --background white --lp-threads "$lp" >/dev/null 2>&1 </dev/null
}

render_new() {
  local stl=$1 png=$2
  docker run --rm --network none --cpus 2 --memory 2g --memory-swap 2g --user nobody \
    -e HOME=/out -e LANG=C.UTF-8 -e TMPDIR=/out -e LP_NUM_THREADS=2 \
    -v "$stl:/in/in.stl:ro" -v "$(dirname "$png"):/out" "$TAG" \
    /usr/local/bin/cad-render --view iso --size 1280 --background opaque /in/in.stl "/out/$(basename "$png")" >/dev/null 2>&1 </dev/null
}

identity() {
  setup
  local lp par part stl pold pnew rc_o rc_n same diff bad total sha_o sha_n ok=1
  lp=$(sed -n 's/^THREADS_DECISION,lp_num_threads=\([^,]*\),.*/\1/p' "$EVID71/71-06-limits.txt")
  par=$(sed -n 's/^THREADS_DECISION,lp_num_threads=[^,]*,occt_parallel=\([^,]*\),.*/\1/p' "$EVID71/71-06-limits.txt")
  [ -n "$lp" ] && [ -n "$par" ] || fail "THREADS_DECISION missing in 71-06-limits.txt"
  header_lines identity "$TAG" >> "$LINES"
  printf 'HDR,stage=identity,lp_threads_71=%s,occt_parallel_71=%s,view=iso,size=1280,background_71=white,background_new=opaque\n' "$lp" "$par" >> "$LINES"
  total=0; same=0; diff=0; bad=0
  for part in "${IDENTITY_PARTS[@]}"; do
    disk_guard
    total=$((total + 1))
    stl="$WORK/$part.stl"
    if ! mesh_old "$CORPUS/public/$part" "$stl" "$par"; then
      bad=$((bad + 1))
      printf 'IDENTITY_PNG,part=%s,same=0,detail=mesh-gave-no-stl-after-3-attempts\n' "$part" >> "$LINES"
      continue
    fi
    pold="$WORK/o71"; pnew="$WORK/n74"
    rm -rf "$pold" "$pnew"; mkdir -m 0777 "$pold" "$pnew"
    rc_o=0; rc_n=0
    render_old "$stl" "$pold/render.png" "$lp" || rc_o=$?
    render_new "$stl" "$pnew/render.png" || rc_n=$?
    if [ "$rc_o" = 0 ] && [ "$rc_n" = 0 ] && [ -s "$pold/render.png" ] && [ -s "$pnew/render.png" ]; then
      sha_o=$(sha_of "$pold/render.png"); sha_n=$(sha_of "$pnew/render.png")
      if [ "$sha_o" = "$sha_n" ]; then
        same=$((same + 1)); printf 'IDENTITY_PNG,part=%s,same=1,stl_bytes=%s,sha_old=%s,sha_new=%s\n' "$part" "$(wc -c < "$stl" | tr -d ' ')" "$sha_o" "$sha_n" >> "$LINES"
      else
        diff=$((diff + 1)); printf 'IDENTITY_PNG,part=%s,same=0,stl_bytes=%s,sha_old=%s,sha_new=%s\n' "$part" "$(wc -c < "$stl" | tr -d ' ')" "$sha_o" "$sha_n" >> "$LINES"
        printf 'IDENTITY_DIFF,part=%s,libosmesa6_71=%s,libosmesa6_new=%s,pixelcheck_same=%s\n' "$part" "$(dpkg_osmesa "$OLD_IMAGE")" "$(dpkg_osmesa "$TAG")" \
          "$(docker run --rm --network none --user nobody -v "$pold:/a:ro" -v "$pnew:/b:ro" --entrypoint "${PYV[0]}" "$OLD_IMAGE" "${PYV[@]:1}" /opt/cad-measure/pixelcheck.py same /a/render.png /b/render.png 2>&1 | tr ',\n' ';;')" >> "$LINES"
        break
      fi
    else
      bad=$((bad + 1))
      printf 'IDENTITY_PNG,part=%s,same=0,detail=rc_old=%s/rc_new=%s\n' "$part" "$rc_o" "$rc_n" >> "$LINES"
    fi
  done
  printf 'IDENTITY,parts=%s,identical=%s,different=%s,failed=%s,expected=%s\n' "$total" "$same" "$diff" "$bad" "${#IDENTITY_PARTS[@]}" >> "$LINES"
  [ "$same" = "${#IDENTITY_PARTS[@]}" ] && [ "$diff" = 0 ] && [ "$bad" = 0 ] || ok=0
  evid_put "$LINES" '^HDR,stage=identity,|^IDENTITY_PNG,|^IDENTITY_DIFF,|^IDENTITY,'
  cat "$LINES"
  [ "$ok" = 1 ]
}

wrap_field() {
  grep '^WRAP,' "$2" | tail -n 1 | tr ',' '\n' | sed -n "s/^$1=//p" | head -n 1
}

gen_heavy() {
  local outdir=$1
  docker run --rm --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/tmp -e LANG=C.UTF-8 \
    -v "$REPO_ROOT:/src:ro" -v "$CORPUS/public:/base:ro" -v "$outdir:/out" -w /src \
    --entrypoint "${PYV[0]}" "$TAG" "${PYV[@]:1}" scripts/cad-corpus-gen.py \
    --scaled-from /base/LinearSlide-MGN15x-Block.step --kind step-nauo --targets 1000 --scaled-dir /out >&2
}

cost_row() {
  local label=$1 stl=$2 size=$3 rep=$4 triangles=$5 job wrapout rc_w=0 pix v
  disk_guard
  job=$(new_job "$WORK")
  wrapout="$WORK/wrap.out"
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -v "$WORK/wrap:/wrap:ro" -v "$job:/job" -v "$stl:/job/in.stl:ro" --entrypoint "${PYV[0]}" "$TAG" \
    "${PYV[@]:1}" /wrap/measure_wrap.py --budget-s 300 --outdir /job -- \
    /usr/local/bin/cad-render --view iso --size "$size" --background opaque /job/in.stl /job/out.png \
    > "$wrapout" 2> "$WORK/wrap.err" </dev/null || rc_w=$?
  pix=fail
  if [ -s "$job/out.png" ] && pix_ok "$(pix_of "$job/out.png" single)"; then pix=ok; fi
  v=$(verdict_fields "$job/child.log" iso "$size" opaque)
  printf 'COST_ROW,file=%s,sha256=%s,stl_records=%s,size=%s,rep=%s,rc=%s,wall_s=%s,child_rss_mib=%s,cg_peak_mib=%s,oom_kill=%s,pix=%s,wrap_rc=%s,%s\n' \
    "$label" "$(sha_of "$stl")" "$triangles" "$size" "$rep" "$(wrap_field rc "$wrapout")" "$(wrap_field wall_s "$wrapout")" \
    "$(wrap_field child_rss_mib "$wrapout")" "$(wrap_field cg_peak_mib "$wrapout")" "$(wrap_field oom_kill "$wrapout")" "$pix" "$rc_w" "$v"
  rm -rf "$job"
}

derive_cost() {
  python3 - "$1" <<'PY'
import math
import sys
from decimal import Decimal, ROUND_FLOOR

LIMIT_MIB = Decimal("2867.2")
BASE_MIB = Decimal("517.0")
PARSE_MIB = Decimal("24.9")
PER_MTRI = Decimal("579.16")
CAP_MAX = 4014975
MIB = 1048576

rows = []
with open(sys.argv[1]) as fh:
    for line in fh:
        if line.startswith("COST_ROW,"):
            rows.append(dict(kv.split("=", 1) for kv in line.strip().split(",")[1:] if "=" in kv))

def peak(r):
    vals = []
    for k in ("child_rss_mib", "cg_peak_mib"):
        try:
            vals.append(int(r[k]))
        except (KeyError, ValueError):
            pass
    return max(vals) if vals else None

files = sorted({r["file"] for r in rows})
heavy = [r for r in rows if r["file"].startswith("step-nauo") and r["size"] == "2048"]
stop = []
for r in heavy:
    p = peak(r)
    if r.get("rc") != "0" or r.get("pix") != "ok" or p is None or Decimal(p) > LIMIT_MIB:
        stop.append("rep=%s,rc=%s,pix=%s,peak=%s" % (r.get("rep"), r.get("rc"), r.get("pix"), p))
if not heavy or stop:
    print("STOP,reason=heavy_2048_outside_the_70_percent_line,rows=%s" % ("+".join(stop) or "none"))
    sys.exit(3)
frames = []
parts = []
for f in files:
    p2 = [peak(r) for r in rows if r["file"] == f and r["size"] == "2048"]
    p1 = [peak(r) for r in rows if r["file"] == f and r["size"] == "1280"]
    if None in p2 or None in p1 or not p1 or not p2:
        print("STOP,reason=peak_missing,file=%s" % f)
        sys.exit(3)
    frames.append(max(p2) - max(p1))
    parts.append("%s:max2048=%d-max1280=%d" % (f, max(p2), max(p1)))
frame = max(0, max(frames))
cap_raw = int(((LIMIT_MIB - BASE_MIB - PARSE_MIB - Decimal(frame)) / PER_MTRI * Decimal(1000000)).to_integral_value(rounding=ROUND_FLOOR))
cap = min(cap_raw, CAP_MAX)
ceiling = ((cap * 50 + 84) // MIB) * MIB
print("DERIVED,key=cad_frame_cost_2048_mib,value=%d,formula=max_over_files(max_peak_2048-max_peak_1280)_at_least_0,inputs=%s" % (frame, ";".join(parts)))
print("DERIVED,key=cad_render_triangle_cap,value=%d,formula=floor((2867.2-517.0-24.9-%d)/579.16*1e6)=%d_clamped_to_%d,inputs=frame_mib=%d" % (cap, frame, cap_raw, CAP_MAX, frame))
print("DERIVED,key=cad_stl_ceiling_bytes,value=%d,formula=floor((%d*50+84)/1048576)*1048576,inputs=cap=%d;mib=%d" % (ceiling, cap, cap, ceiling // MIB))
PY
}

cost() {
  setup
  local ok=1 light heavy heavy_src heavy_sha regen_sha want_sha name size rep tri_l tri_h out rc
  mkdir -p "$WORK/wrap" "$WORK/gen"
  chmod 0755 "$WORK/wrap"
  chmod 0777 "$WORK/gen"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
  header_lines cost "$TAG" >> "$LINES"
  grep -q 'triangles:5944' "$EVID71/71-04-render.txt" || fail "no 5944-triangle row in 71-04-render.txt"
  light="$WORK/light.stl"
  mesh_prod "$CORPUS/public/LinearSlide-MGN15x-Block.step" step "$light" || fail "mesh of the 5944-triangle part failed"
  tri_l=$(( ($(wc -c < "$light" | tr -d ' ') - 84) / 50 ))

  name=step-nauo-LinearSlide-MGN15x-Block-n1000.step
  want_sha=$(grep -m1 "^ROW,.*,name=$name,sha=" "$EVID71/71-06-ladder-4g-ocp7.9.3.1.1-par-off-lp2.txt" | sed -n 's/.*,sha=\([0-9a-f]*\),.*/\1/p')
  [ -n "$want_sha" ] || fail "no 71 ROW for $name"
  gen_heavy "$WORK/gen" || fail "step-nauo generation failed"
  regen_sha=$(sha_of "$WORK/gen/$name")
  rm -f "$WORK/gen/$name"
  heavy_src="$CAD_CACHE/scaled/$name"
  [ -f "$heavy_src" ] || fail "the 71 input $heavy_src is missing and the regenerated file has sha $regen_sha, not $want_sha"
  heavy_sha=$(sha_of "$heavy_src")
  [ "$heavy_sha" = "$want_sha" ] || fail "cached $name sha $heavy_sha differs from the 71 row $want_sha"
  printf 'HDR,stage=cost,heavy_input=%s,heavy_sha256=%s,sha_equals_71_row=1,source=71_scaled_cache,regenerated_sha256=%s,regenerated_equals_71_row=%s,lp_num_threads=2,view=iso,background=opaque\n' \
    "$name" "$heavy_sha" "$regen_sha" "$([ "$regen_sha" = "$want_sha" ] && echo 1 || echo 0)" >> "$LINES"
  heavy="$WORK/heavy.stl"
  mesh_prod "$heavy_src" step "$heavy" || fail "mesh of the heavy part failed"
  tri_h=$(( ($(wc -c < "$heavy" | tr -d ' ') - 84) / 50 ))
  printf 'HDR,stage=cost,heavy_stl_records=%s,heavy_stl_bytes=%s,71_vtk_triangles=3816000\n' "$tri_h" "$(wc -c < "$heavy" | tr -d ' ')" >> "$LINES"

  for spec in "LinearSlide-MGN15x-Block.step:$light:$tri_l" "$name:$heavy:$tri_h"; do
    out=${spec%%:*}
    rest=${spec#*:}
    for size in 1280 2048; do
      for rep in 1 2; do
        cost_row "$out" "${rest%%:*}" "$size" "$rep" "${rest##*:}" >> "$LINES"
      done
    done
  done
  rm -f "$heavy"

  rc=0
  derive_cost "$LINES" > "$WORK/derived.txt" || rc=$?
  cat "$WORK/derived.txt" >> "$LINES"
  if [ "$rc" != 0 ]; then ok=0; fi
  awk -F, '/^COST_ROW,/{n++; if($0 !~ /,rc=0,/ || $0 !~ /,pix=ok,/) bad++; if($0 ~ /^COST_ROW,file=LinearSlide/ && $0 !~ /,triangles=5944$/) bad++} END{printf "SUMMARY,stage=cost,rows=%d,bad_rows=%d\n", n, bad+0}' "$LINES" >> "$LINES"
  evid_put "$LINES" '^HDR,stage=cost,|^COST_ROW,|^DERIVED,key=(cad_frame_cost_2048_mib|cad_render_triangle_cap|cad_stl_ceiling_bytes),|^STOP,|^SUMMARY,stage=cost,'
  cat "$LINES" | grep -v '^HDR'
  [ "$ok" = 1 ] || return "$rc"
  grep -q 'bad_rows=0$' "$LINES"
}

units() {
  setup
  local ok=1 files f rc=0
  cp "$UNIT_PY" "$WORK/cad-unit-probe.py"
  chmod 0644 "$WORK/cad-unit-probe.py"
  header_lines units "$TAG" >> "$LINES"
  files=(synthetic/units_inch.step public/nist-d2mi-827-9999-906.stp public/nist_ctc_05_asme1_ap242-e1.stp public/LM8UU.step synthetic/legacy_cp1251.step)
  local args=()
  for f in "${files[@]}"; do args+=("/corpus/$f"); done
  docker run --rm "${LIMITS[@]}" -e HOME=/tmp -e LANG=C.UTF-8 -e LP_NUM_THREADS=2 \
    -v "$CORPUS:/corpus:ro" -v "$WORK:/u:ro" --entrypoint "${PYV[0]}" "$TAG" \
    "${PYV[@]:1}" /u/cad-unit-probe.py "${args[@]}" > "$WORK/units.out" 2> "$WORK/units.err" </dev/null || rc=$?
  [ "$rc" = 0 ] || { cat "$WORK/units.err" >&2; fail "unit probe failed rc=$rc"; }
  grep '^UNIT_ROW,' "$WORK/units.out" >> "$LINES"
  python3 - "$WORK/units.out" >> "$LINES" <<'PY' || ok=0
import sys

rows = []
with open(sys.argv[1]) as fh:
    for line in fh:
        if line.startswith("UNIT_ROW,"):
            rows.append(dict(kv.split("=", 1) for kv in line.strip().split(",")[1:] if "=" in kv))

def num(v):
    try:
        return float(v)
    except (TypeError, ValueError):
        return None

def near(a, b):
    return a is not None and b is not None and abs(a - b) <= 1e-6 * abs(b)

control = next((r for r in rows if r["file"] == "units_inch.step"), None)
control_ratio = num(control["ratio"]) if control else None
control_ok = near(control_ratio, 25.4)
print("UNITS_CONTROL,file=units_inch.step,ratio=%s,expected=25.4,tolerance=1e-6_relative,result=%s" % (control_ratio, "ok" if control_ok else "fail"))

sources = (("model_length_unit", "model_length_unit"), ("fileunits", "fileunits_factor"), ("text_unit", "text_factor"))
chosen = "none"
detail = []
for name, key in sources:
    failing = [r["file"] for r in rows if not near(num(r.get(key)), num(r.get("ratio")))]
    qualifies = not failing
    detail.append("%s=%s" % (name, "qualifies" if qualifies else "fails_on_" + "+".join(failing)))
    for r in rows:
        ratio = num(r.get("ratio"))
        clean = near(ratio, 1.0) or near(ratio, 25.4)
        if r["file"] in failing and num(r.get(key)) is not None and ratio is not None and not clean:
            print("GAP,key=unit_ratio_mismatch,value=%s,reason=source %s gives %s but the measured ratio is %s, which is no unit factor; the ratio compares the OCP vertex bbox with the VERTEX_POINT bbox of the text and is not a pure unit measurement on this file" % (r["file"], name, r.get(key), r["ratio"]))
    if qualifies and chosen == "none":
        chosen = name
print("DERIVED,key=cad_unit_factor_source,value=%s,formula=first_source_in_order_whose_factor_equals_the_measured_ratio_within_1e-6_relative_on_every_STEP_row,inputs=%s" % (chosen, ";".join(detail)))

ctc = next((r for r in rows if r["file"].startswith("nist_ctc_05")), None)
ratio = num(ctc["ratio"]) if ctc else None
value = "undetermined"
for cand in (1.0, 25.4):
    if near(ratio, cand):
        value = "%g" % cand
print("DERIVED,key=nist_ctc_05_factor,value=%s,formula=ratio_rounded_to_nearest_of_1_or_25.4_when_within_1e-6_relative_else_undetermined,inputs=ratio=%s" % (value, ratio))
if chosen == "none" or value == "undetermined":
    print("GAP,key=unit_factor_source,value=%s,reason=plan 74-02 reports such files as unspecified with source none (R-04); ctc_05=%s" % (chosen, value))
print("GAP,key=iges_inch_scaling,value=unproven,reason=no inch IGES in the corpus (73 GAP)")
sys.exit(0 if control_ok else 1)
PY
  printf 'SUMMARY,stage=units,rows=%s,control=%s\n' "$(grep -c '^UNIT_ROW,' "$LINES" || true)" "$(grep -o '^UNITS_CONTROL.*result=[a-z]*' "$LINES" | sed 's/.*result=//')" >> "$LINES"
  evid_put "$LINES" '^HDR,stage=units,|^UNIT_ROW,|^UNITS_CONTROL,|^DERIVED,key=(cad_unit_factor_source|nist_ctc_05_factor),|^GAP,|^SUMMARY,stage=units,'
  grep -v '^HDR' "$LINES"
  [ "$ok" = 1 ]
}

run_mesh() {
  local src=$1 fmt=$2 job=$3
  shift 3
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -v "$job:/job" -v "$src:/job/in.$fmt:ro" "$TAG" \
    /usr/local/bin/cad-mesh --format "$fmt" "$@" /job/in.$fmt /job/out.stl \
    > "$job/stdout.txt" 2> "$job/stderr.txt" </dev/null
}

run_stats() {
  local src=$1 job=$2
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job \
    -v "$job:/job" -v "$src:/job/in.stl:ro" "$TAG" \
    /usr/local/bin/cad-mesh stats --measure /job/draft.json /job/in.stl \
    > "$job/stdout.txt" 2> "$job/stderr.txt" </dev/null
}

draft_row() {
  python3 - "$1" "$2" "$3" "$4" <<'PY'
import json
import math
import sys

label, path, rc, kind = sys.argv[1:5]
EXPECT = {
    "units_inch.step": {"volume": 98322.38, "area": 14193.51, "tol": 0.01, "applied_factor": 25.4, "declared_unit": "inch"},
    "nist_ctc_05_asme1_ap242-e1.stp": {"applied_factor": 25.4, "declared_unit": "inch"},
    "LM8UU.step": {"applied_factor": 1.0, "declared_unit": "mm", "positive": True},
    "legacy_cp1251.step": {"applied_factor": 1.0, "declared_unit": "mm"},
    "open_shell.step": {"reason": "no_solids"},
    "single_face.step": {"reason": "no_solids"},
    "stl_ascii_box.stl": {"volume": 6000.0, "area": 2200.0, "tol": 1e-6, "relative": True},
    "nasa-hubble-all-pieces.stl": {"reason": "stl_not_watertight"},
}
problems = []
try:
    with open(path) as fh:
        d = json.load(fh)
except (OSError, ValueError):
    print("rc=%s,detail=no_draft,result=fail" % rc)
    sys.exit(0)
vol, area, reason = d.get("volume"), d.get("area"), d.get("null_reason", "")
if (vol is None) != (area is None) or (vol is None) != (reason != ""):
    problems.append("null_pairing")
if vol is not None and not (math.isfinite(vol) and vol > 0 and math.isfinite(area) and area > 0):
    problems.append("volume_area_not_positive")
if d.get("format") != kind:
    problems.append("format")
box = d.get("bbox", {})
lo, hi = box.get("min", []), box.get("max", [])
if len(lo) != 3 or len(hi) != 3 or not all(math.isfinite(x) for x in lo + hi) or any(a > b for a, b in zip(lo, hi)):
    problems.append("bbox")
if d.get("solids") is not None and d.get("closed_solids", 0) > d["solids"]:
    problems.append("closed_gt_solids")
exp = EXPECT.get(label, {})
tol = exp.get("tol", 0.0)
for key in ("volume", "area"):
    if key in exp:
        got = d.get(key)
        bound = tol * exp[key] if exp.get("relative") else tol
        if got is None or abs(got - exp[key]) > bound:
            problems.append("%s_expected_%s" % (key, exp[key]))
if "applied_factor" in exp and (d.get("applied_factor") is None or abs(d["applied_factor"] - exp["applied_factor"]) > 1e-9):
    problems.append("applied_factor")
if "declared_unit" in exp and d.get("declared_unit") != exp["declared_unit"]:
    problems.append("declared_unit")
if "reason" in exp and reason != exp["reason"]:
    problems.append("reason_expected_" + exp["reason"])
if exp.get("positive") and vol is None:
    problems.append("volume_missing")
if rc != "0":
    problems.append("rc")


def fmt(x):
    return "null" if x is None else repr(x)


print("rc=%s,solids=%s,closed=%s,volume=%s,area=%s,reason=%s,declared_unit=%s,declared_raw=%s,applied_factor=%s,bbox=%s|%s,triangles=%s,detail=%s,result=%s" % (
    rc, fmt(d.get("solids")), fmt(d.get("closed_solids")), fmt(vol), fmt(area), reason or "none", d.get("declared_unit") or "none",
    (d.get("declared_unit_raw") or "none").replace(",", "_").replace(" ", "_"), fmt(d.get("applied_factor")),
    ";".join(repr(x) for x in lo), ";".join(repr(x) for x in hi), d.get("triangles"),
    "+".join(problems) or "none", "fail" if problems else "ok"))
PY
}

stl_triangles() { echo $(( ($(wc -c < "$1" | tr -d ' ') - 84) / 50 )); }

mesh_draft_rows() {
  local rel fmt label job rc row
  for rel in synthetic/units_inch.step synthetic/legacy_cp1251.step public/LM8UU.step \
    public/nist_ctc_05_asme1_ap242-e1.stp synthetic/degenerate_plate.step synthetic/open_shell.step \
    synthetic/single_face.step synthetic/surfaces_only.iges public/nist-d2mi-827-9999-906.stp; do
    disk_guard
    label=$(basename "$rel")
    case "$label" in *.iges) fmt=iges ;; *) fmt=step ;; esac
    job=$(new_job "$WORK")
    rc=0
    run_mesh "$CORPUS/$rel" "$fmt" "$job" --measure /job/draft.json || rc=$?
    row=$(draft_row "$label" "$job/draft.json" "$rc" "$fmt")
    printf 'DRAFT,file=%s,%s\n' "$label" "$row" >> "$LINES"
    case "$row" in *result=ok) ;; *) tail -n 5 "$job/stderr.txt" >&2 ;; esac
    rm -rf "$job"
  done
}

mesh_stats_rows() {
  local rel label job rc row
  for rel in synthetic/stl_ascii_box.stl synthetic/stl_binary_solid_header.stl public/nasa-hubble-all-pieces.stl; do
    label=$(basename "$rel")
    job=$(new_job "$WORK")
    rc=0
    run_stats "$CORPUS/$rel" "$job" || rc=$?
    row=$(draft_row "$label" "$job/draft.json" "$rc" stl)
    printf 'STATS,file=%s,%s\n' "$label" "$row" >> "$LINES"
    case "$row" in *result=ok) ;; *) tail -n 5 "$job/stderr.txt" >&2 ;; esac
    rm -rf "$job"
  done
}

mesh_quality_rows() {
  local spec label job q rc counts tri res
  for spec in public/LinearSlide-MGN15x-Block.step synthetic/degenerate_plate.step; do
    label=$(basename "$spec")
    counts=""
    res=ok
    for q in draft standard fine; do
      disk_guard
      job=$(new_job "$WORK")
      rc=0
      run_mesh "$CORPUS/$spec" step "$job" --quality "$q" || rc=$?
      if [ "$rc" = 0 ] && [ -s "$job/out.stl" ]; then tri=$(stl_triangles "$job/out.stl"); else tri=na; res=fail; fi
      counts="$counts $tri"
      rm -rf "$job"
    done
    set -- $counts
    if [ "$res" = ok ] && ! { [ "$1" -le "$2" ] && [ "$2" -le "$3" ]; }; then res=fail; fi
    printf 'QUALITY,file=%s,draft=%s,standard=%s,fine=%s,result=%s\n' "$label" "$1" "$2" "$3" "$res" >> "$LINES"
  done
}

mesh_identity_rows() {
  local spec label jn jo rc_n rc_o sha_n sha_o res
  mkdir -p "$WORK/old"
  chmod 0755 "$WORK/old"
  git show "$BASE_HELPER_REV:cmd/cad-worker/helper/cad-mesh" > "$WORK/old/cad-mesh"
  chmod 0755 "$WORK/old/cad-mesh"
  for spec in synthetic/units_inch.step public/LM8UU.step; do
    label=$(basename "$spec")
    jn=$(new_job "$WORK")
    jo=$(new_job "$WORK")
    rc_n=0
    rc_o=0
    run_mesh "$CORPUS/$spec" step "$jn" --quality standard --measure /job/draft.json || rc_n=$?
    docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
      -v "$jo:/job" -v "$CORPUS/$spec:/job/in.step:ro" -v "$WORK/old:/old:ro" --entrypoint "${PYV[0]}" "$TAG" \
      "${PYV[@]:1}" /old/cad-mesh --format step /job/in.step /job/out.stl > /dev/null 2>&1 </dev/null || rc_o=$?
    res=fail
    sha_n=none
    sha_o=none
    if [ "$rc_n" = 0 ] && [ "$rc_o" = 0 ] && [ -s "$jn/out.stl" ] && [ -s "$jo/out.stl" ]; then
      sha_n=$(sha_of "$jn/out.stl")
      sha_o=$(sha_of "$jo/out.stl")
      [ "$sha_n" = "$sha_o" ] && res=ok
    fi
    printf 'IDENTITY_STL,file=%s,base_rev=%s,rc_new=%s,rc_old=%s,stl_bytes=%s,sha_old=%s,sha_new=%s,result=%s\n' \
      "$label" "$BASE_HELPER_REV" "$rc_n" "$rc_o" "$( [ -f "$jn/out.stl" ] && wc -c < "$jn/out.stl" | tr -d ' ' || echo 0 )" "$sha_o" "$sha_n" "$res" >> "$LINES"
    rm -rf "$jn"
    rm -rf "$jo"
  done
}

prep_wrap() {
  mkdir -p "$WORK/wrap" "$WORK/gen"
  chmod 0755 "$WORK/wrap"
  chmod 0777 "$WORK/gen"
  cp "$WRAP_PY" "$WORK/wrap/measure_wrap.py"
}

verdict_of() {
  python3 - "$1" <<'PY'
import json
import sys

last = ""
try:
    with open(sys.argv[1], "rb") as fh:
        for raw in fh:
            line = raw.decode("ascii", "replace")
            if line.startswith("CADVERDICT "):
                last = line
    o = json.loads(last[len("CADVERDICT "):])
    print("verdict=%s,triangles=%s" % (o.get("verdict"), o.get("triangles")))
except (OSError, ValueError):
    print("verdict=none,triangles=na")
PY
}

mesh_wall_row() {
  local src name sha job wrapout rc_w=0 wall peak rss oom v res=ok
  prep_wrap
  name="step-copies-freecad-mit-e-vent-t-slot-${STEP_COPIES_TARGET}MiB.step"
  docker run --rm --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/tmp -e LANG=C.UTF-8 \
    -v "$REPO_ROOT:/src:ro" -v "$CAD_CACHE/fetched:/base:ro" -v "$WORK/gen:/out" -w /src \
    --entrypoint "${PYV[0]}" "$TAG" "${PYV[@]:1}" scripts/cad-corpus-gen.py \
    --scaled-from /base/freecad-mit-e-vent-t-slot.step --kind step-copies --targets "$STEP_COPIES_TARGET" --scaled-dir /out >&2 \
    || fail "step-copies generation failed"
  src="$WORK/gen/$name"
  [ -f "$src" ] || fail "$name was not generated"
  sha=$(sha_of "$src")
  job=$(new_job "$WORK")
  wrapout="$WORK/wrap.out"
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job -e LP_NUM_THREADS=2 \
    -v "$WORK/wrap:/wrap:ro" -v "$job:/job" -v "$src:/job/in.step:ro" --entrypoint "${PYV[0]}" "$TAG" \
    "${PYV[@]:1}" /wrap/measure_wrap.py --budget-s 400 --outdir /job -- \
    /usr/local/bin/cad-mesh --format step --measure /job/draft.json /job/in.step /job/out.stl \
    > "$wrapout" 2> "$WORK/wrap.err" </dev/null || rc_w=$?
  wall=$(wrap_field wall_s "$wrapout")
  peak=$(wrap_field cg_peak_mib "$wrapout")
  rss=$(wrap_field child_rss_mib "$wrapout")
  oom=$(wrap_field oom_kill "$wrapout")
  v=$(verdict_of "$job/child.log")
  [ "$(wrap_field rc "$wrapout")" = 0 ] && [ "$rc_w" = 0 ] || res=fail
  [ "$oom" = 0 ] || res=fail
  case "$v" in verdict=ok,*) ;; *) res=fail ;; esac
  python3 -c "import sys; sys.exit(0 if float(sys.argv[1] or 9999) <= $WALL_LIMIT_S else 1)" "$wall" || res=fail
  [ -s "$job/draft.json" ] || res=fail
  printf 'WALL,file=%s,bytes=%s,sha256=%s,sha_equals_73_row=%s,rc=%s,wall_s=%s,limit_s=%s,child_rss_mib=%s,cg_peak_mib=%s,oom_kill=%s,%s,draft_bytes=%s,result=%s\n' \
    "$name" "$(wc -c < "$src" | tr -d ' ')" "$sha" "$([ "$sha" = "$STEP_COPIES_SHA_73" ] && echo 1 || echo 0)" "$(wrap_field rc "$wrapout")" "$wall" "$WALL_LIMIT_S" "$rss" "$peak" "$oom" "$v" \
    "$( [ -f "$job/draft.json" ] && wc -c < "$job/draft.json" | tr -d ' ' || echo 0 )" "$res" >> "$LINES"
  rm -f "$src"
  rm -rf "$job"
  [ "$res" = ok ] || WALL_STOP=1
}

tile_stl() {
  python3 - "$1" "$2" "$3" <<'PY'
import math
import struct
import sys

base, out, ceiling = sys.argv[1], sys.argv[2], int(sys.argv[3])
with open(base, "rb") as fh:
    blob = fh.read()
count = struct.unpack("<I", blob[80:84])[0]
recs = [list(struct.unpack("<12fH", blob[84 + 50 * i:134 + 50 * i])) for i in range(count)]
span = [0.0, 0.0]
for axis in (0, 1):
    vals = [r[3 + 3 * v + axis] for r in recs for v in range(3)]
    span[axis] = max(1.0, (max(vals) - min(vals)) * 2.0)
tiles = (ceiling - 84) // (50 * count)
side = int(math.ceil(math.sqrt(tiles)))
with open(out, "wb") as fh:
    fh.write(b"octoconv probe tile".ljust(80, b"\0") + struct.pack("<I", tiles * count))
    for t in range(tiles):
        dx, dy = (t % side) * span[0], (t // side) * span[1]
        buf = bytearray()
        for r in recs:
            v = list(r)
            for k in range(3):
                v[3 + 3 * k] += dx
                v[4 + 3 * k] += dy
            buf += struct.pack("<12fH", *v)
        fh.write(buf)
print(tiles * count)
PY
}

draft_tail() {
  python3 - "$1" <<'PY'
import json
import sys

try:
    with open(sys.argv[1]) as fh:
        d = json.load(fh)
    print("reason=%s,volume=%s" % (d["null_reason"] or "none", d["volume"]))
except (OSError, ValueError, KeyError):
    print("reason=no_draft,volume=na")
PY
}

mesh_rss_row() {
  local ceiling tile job wrapout rc_w=0 expect peak rss v res=ok bytes oom rc
  ceiling=$(sed -n 's/^DERIVED,key=cad_stl_ceiling_bytes,value=\([0-9]*\),.*/\1/p' "$PHASE_DIR/evidence/74-01-wave0.txt")
  [ -n "$ceiling" ] || fail "cad_stl_ceiling_bytes is missing from 74-01-wave0.txt"
  job=$(new_job "$WORK")
  run_mesh "$CORPUS/synthetic/units_inch.step" step "$job" || fail "tile base mesh failed"
  tile="$WORK/ceiling-tile.stl"
  expect=$(tile_stl "$job/out.stl" "$tile" "$ceiling")
  rm -rf "$job"
  bytes=$(wc -c < "$tile" | tr -d ' ')
  job=$(new_job "$WORK")
  wrapout="$WORK/wrap.out"
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e TMPDIR=/job \
    -v "$WORK/wrap:/wrap:ro" -v "$job:/job" -v "$tile:/job/in.stl:ro" --entrypoint "${PYV[0]}" "$TAG" \
    "${PYV[@]:1}" /wrap/measure_wrap.py --budget-s 400 --outdir /job -- \
    /usr/local/bin/cad-mesh stats --measure /job/draft.json /job/in.stl \
    > "$wrapout" 2> "$WORK/wrap.err" </dev/null || rc_w=$?
  rss=$(wrap_field child_rss_mib "$wrapout")
  peak=$(wrap_field cg_peak_mib "$wrapout")
  oom=$(wrap_field oom_kill "$wrapout")
  rc=$(wrap_field rc "$wrapout")
  v=$(verdict_of "$job/child.log")
  [ "$rc" = 0 ] && [ "$rc_w" = 0 ] && [ "$oom" = 0 ] || res=fail
  [ "$v" = "verdict=ok,triangles=$expect" ] || res=fail
  python3 -c "import sys; sys.exit(0 if max(float(sys.argv[1] or 99999), float(sys.argv[2] or 99999)) <= $RSS_LIMIT_MIB else 1)" "$rss" "$peak" || res=fail
  printf 'STATS_RSS,bytes=%s,ceiling_bytes=%s,triangles_expected=%s,rc=%s,wall_s=%s,child_rss_mib=%s,cg_peak_mib=%s,limit_mib=%s,oom_kill=%s,%s,%s,result=%s\n' \
    "$bytes" "$ceiling" "$expect" "$rc" "$(wrap_field wall_s "$wrapout")" "$rss" "$peak" "$RSS_LIMIT_MIB" "$oom" "$v" \
    "$(draft_tail "$job/draft.json")" "$res" >> "$LINES"
  rm -f "$tile"
  rm -rf "$job"
}

mesh() {
  setup
  local bad rows
  WALL_STOP=0
  header_lines mesh "$TAG" >> "$LINES"
  mesh_draft_rows
  mesh_stats_rows
  mesh_quality_rows
  mesh_identity_rows
  mesh_wall_row
  mesh_rss_row
  rows=$(grep -cE '^(DRAFT|STATS|QUALITY|IDENTITY_STL|WALL|STATS_RSS),' "$LINES" || true)
  bad=$(grep -E '^(DRAFT|STATS|QUALITY|IDENTITY_STL|WALL|STATS_RSS),' "$LINES" | grep -vc 'result=ok' || true)
  if [ "$WALL_STOP" = 1 ]; then
    printf 'STOP,reason=mesh_measure_wall_or_budget_not_ok,limit_s=%s\n' "$WALL_LIMIT_S" >> "$LINES"
  fi
  if [ "$bad" = 0 ]; then
    printf 'SUMMARY,result=ok,stage=mesh,rows=%s,bad_rows=0\n' "$rows" >> "$LINES"
  else
    printf 'SUMMARY,result=fail,stage=mesh,rows=%s,bad_rows=%s\n' "$rows" "$bad" >> "$LINES"
  fi
  evid_put "$LINES" '^(HDR|DRAFT|STATS|QUALITY|IDENTITY_STL|WALL|STATS_RSS|STOP|SUMMARY),'
  grep -v '^HDR' "$LINES"
  [ "$bad" = 0 ]
}


JUDGE_VIEWS=(iso front back left right top bottom)
W15_PNG_SRC="$CAD_CACHE/scaled/stl-ascii-nasa-sls-block1-1MiB.stl"
W15_PNG_SHA=77492adbca35ff7f0c583732208324a834170aec9cb76eee7ad4e0b853c18ef0

blank_png() {
  python3 - "$1" "$2" "$3" <<'PY'
import struct, sys, zlib
path, size, kind = sys.argv[1], int(sys.argv[2]), sys.argv[3]
def chunk(t, d):
    c = struct.pack(">I", len(d)) + t + d
    return c + struct.pack(">I", zlib.crc32(t + d) & 0xffffffff)
if kind == "transparent":
    ctype, row = 6, b"\x00" + b"\x00" * (4 * size)
else:
    ctype, row = 2, b"\x00" + b"\xff" * (3 * size)
raw = row * size
with open(path, "wb") as fh:
    fh.write(b"\x89PNG\r\n\x1a\n")
    fh.write(chunk(b"IHDR", struct.pack(">IIBBBBB", size, size, 8, ctype, 0, 0, 0)))
    fh.write(chunk(b"IDAT", zlib.compress(raw, 6)))
    fh.write(chunk(b"IEND", b""))
PY
}

go_judge() {
  local png=$1 size=$2 bg=$3
  "$WORK/cad-pixel-judge" --size "$size" --background "$bg" "$png" 2>/dev/null || true
}

go_field() { printf '%s' "$1" | tr ',' '\n' | sed -n "s/^$2=//p" | head -n 1; }

judge_pair() {
  local id=$1 png=$2 size=$3 bg=$4 kind=$5 gj gok gmode pix pok tpix tok gfacts
  gj=$(go_judge "$png" "$size" "$bg")
  gok=$(go_field "$gj" ok)
  gmode=$(go_field "$gj" mode)
  gfacts=$(printf '%s' "$gj" | sed 's/^GOJUDGE,png=[^,]*,//')
  pix=$(pix_of "$png" single)
  if pix_ok "$pix"; then pok=1; else pok=0; fi
  tpix=""
  if [ "$bg" = transparent ]; then
    tpix=$(pix_of "$png" transparent)
    if pix_ok "$tpix"; then tok=1; else tok=0; fi
    [ "$tok" = 1 ] || pok=0
  fi
  printf 'GOJUDGE,frame=%s,%s\n' "$id" "$gfacts" >> "$LINES"
  printf 'PIX,frame=%s,ok=%s,single=%s,transparent=%s\n' "$id" "$pok" "$(pix_detail "$pix")" "$(pix_detail "$tpix")" >> "$LINES"
  JUDGE_FRAMES=$((JUDGE_FRAMES + 1))
  if [ "$gok" = "$pok" ]; then
    JUDGE_AGREE=$((JUDGE_AGREE + 1))
  elif [ "$pok" = 0 ] && [ "$gok" = 1 ] && [ "$gmode" = thin ]; then
    JUDGE_THIN=$((JUDGE_THIN + 1))
    printf 'DIVERGENCE,frame=%s,class=thin_rule,pixelcheck_ok=0,go_ok=1,go_mode=thin,go=%s,pix=%s\n' "$id" "$(printf '%s' "$gfacts" | tr ',' ';')" "$(pix_detail "$pix")" >> "$LINES"
  else
    JUDGE_UNEXPLAINED=$((JUDGE_UNEXPLAINED + 1))
    printf 'DIVERGENCE,frame=%s,class=UNEXPLAINED,pixelcheck_ok=%s,go_ok=%s,go_mode=%s,go=%s,pix=%s\n' "$id" "$pok" "${gok:-none}" "${gmode:-none}" "$(printf '%s' "$gfacts" | tr ',' ';')" "$(pix_detail "$pix")" >> "$LINES"
  fi
  case "$kind" in
    real)
      JUDGE_REAL=$((JUDGE_REAL + 1))
      if [ "$gok" = 1 ]; then JUDGE_REAL_OK=$((JUDGE_REAL_OK + 1)); fi ;;
    w15)
      JUDGE_W15=$((JUDGE_W15 + 1))
      if [ "$gok" = 1 ]; then JUDGE_W15_OK=$((JUDGE_W15_OK + 1)); else W15_FAILED="$W15_FAILED $size"; fi ;;
  esac
}

judge_frame() {
  local id=$1 stl=$2 view=$3 size=$4 bg=$5 kind=$6 job so se rc=0 vf w=0 h=0 png
  disk_guard
  job=$(new_job "$WORK")
  so="$WORK/render.out"; se="$WORK/render.err"
  run_render_confined "$stl" "$view" "$size" "$bg" "$job" "$so" "$se" || rc=$?
  vf=$(verdict_fields "$so" "$view" "$size" "$bg")
  if probe_refused "$se" || probe_refused "$so" || [ ! -s "$job/out.png" ]; then
    printf 'FRAME,id=%s,kind=%s,view=%s,size=%s,background=%s,rc=%s,png=absent,%s\n' "$id" "$kind" "$view" "$size" "$bg" "$rc" "$vf" >> "$LINES"
    JUDGE_RENDER_FAILED=$((JUDGE_RENDER_FAILED + 1))
    tail -n 5 "$se" >&2
    rm -rf "$job"
    return 0
  fi
  png="$WORK/frames/$id.png"
  mkdir -p "$WORK/frames"
  cp "$job/out.png" "$png"
  read -r w h <<< "$(png_dims "$png")"
  printf 'FRAME,id=%s,kind=%s,view=%s,size=%s,background=%s,rc=%s,png_w=%s,png_h=%s,png_sha256_12=%s,%s\n' "$id" "$kind" "$view" "$size" "$bg" "$rc" "$w" "$h" "$(sha_of "$png" | cut -c1-12)" "$vf" >> "$LINES"
  rm -rf "$job"
  judge_pair "$id" "$png" "$size" "$bg" "$kind"
  rm -f "$png"
}

judge_cmd() {
  setup
  JUDGE_FRAMES=0; JUDGE_AGREE=0; JUDGE_THIN=0; JUDGE_UNEXPLAINED=0
  JUDGE_REAL=0; JUDGE_REAL_OK=0; JUDGE_W15=0; JUDGE_W15_OK=0; JUDGE_RENDER_FAILED=0
  W15_FAILED=""
  local part stl view size ok=1 bad_blank=0
  CGO_ENABLED=0 GOTOOLCHAIN="$GO_TOOLCHAIN" go build -o "$WORK/cad-pixel-judge" ./scripts/cmd/cad-pixel-judge
  [ "$(sha_of "$W15_PNG_SRC")" = "$W15_PNG_SHA" ] || fail "$W15_PNG_SRC is not the 71 ladder slice $W15_PNG_SHA"
  header_lines judge "$TAG" >> "$LINES"
  printf 'HDR,stage=judge,render_ro=%s,go=%s\n' "$RENDER_RO" "$(GOTOOLCHAIN=$GO_TOOLCHAIN go version | tr ' ' '_')" >> "$LINES"
  printf 'HDR,stage=judge,rule=a_frame_may_differ_only_when_pixelcheck_says_ok0_and_the_Go_judge_says_ok_via_mode_thin\n' >> "$LINES"

  for part in "${IDENTITY_PARTS[@]}"; do
    stl="$WORK/$part.stl"
    mesh_prod "$CORPUS/public/$part" step "$stl" || fail "cad-mesh gave no STL for $part"
    for view in "${JUDGE_VIEWS[@]}"; do
      judge_frame "$part.$view.1280.opaque" "$stl" "$view" 1280 opaque real
    done
    judge_frame "$part.iso.1280.transparent" "$stl" iso 1280 transparent real
    case "$part" in
      LM8UU.step|V-Slot-20x20-100mm.step)
        for size in 256 512 2048; do
          judge_frame "$part.iso.$size.opaque" "$stl" iso "$size" opaque real
        done ;;
    esac
  done

  for size in 256 1280 2048; do
    judge_frame "w15.iso.$size.opaque" "$W15_PNG_SRC" iso "$size" opaque w15
  done

  local blank_o blank_t go_o go_t pix_o pix_t
  blank_o="$WORK/blank_opaque.png"; blank_t="$WORK/blank_transparent.png"
  blank_png "$blank_o" 1280 opaque
  blank_png "$blank_t" 1280 transparent
  go_o=$(go_field "$(go_judge "$blank_o" 1280 opaque)" ok)
  go_t=$(go_field "$(go_judge "$blank_t" 1280 transparent)" ok)
  if pix_ok "$(pix_of "$blank_o" single)"; then pix_o=1; else pix_o=0; fi
  if pix_ok "$(pix_of "$blank_t" transparent)"; then pix_t=1; else pix_t=0; fi
  printf 'BLANK,background=opaque,size=1280,go_ok=%s,pixelcheck_ok=%s\n' "${go_o:-none}" "$pix_o" >> "$LINES"
  printf 'BLANK,background=transparent,size=1280,go_ok=%s,pixelcheck_ok=%s\n' "${go_t:-none}" "$pix_t" >> "$LINES"
  if [ "${go_o:-none}" = 0 ] && [ "$pix_o" = 0 ] && [ "${go_t:-none}" = 0 ] && [ "$pix_t" = 0 ]; then bad_blank=0; else bad_blank=1; fi

  if [ "$JUDGE_UNEXPLAINED" != 0 ] || [ "$JUDGE_RENDER_FAILED" != 0 ] || [ "$JUDGE_REAL_OK" != "$JUDGE_REAL" ] \
    || [ "$JUDGE_W15" != 3 ] || [ "$JUDGE_W15_OK" != 3 ] || [ "$bad_blank" != 0 ]; then ok=0; fi
  printf 'JUDGE_SUMMARY,frames=%s,agreements=%s,thin_only_divergences=%s,unexplained_divergence=%s,render_failed=%s,real_frames=%s,real_go_ok=%s,w15_frames=%s,w15_go_ok=%s,blank_rejected_by_both=%s,result=%s\n' \
    "$JUDGE_FRAMES" "$JUDGE_AGREE" "$JUDGE_THIN" "$JUDGE_UNEXPLAINED" "$JUDGE_RENDER_FAILED" "$JUDGE_REAL" "$JUDGE_REAL_OK" "$JUDGE_W15" "$JUDGE_W15_OK" "$([ "$bad_blank" = 0 ] && echo 1 || echo 0)" "$([ "$ok" = 1 ] && echo ok || echo fail)" >> "$LINES"
  case " $W15_FAILED " in
    *" 256 "*) printf 'STOP,reason=the_W15_thin_frame_at_256_px_failed_the_Go_judge,thresholds_are_D-06_to_settle=1\n' >> "$LINES" ;;
  esac
  evid_put "$LINES" '^(HDR,stage=judge|FRAME|GOJUDGE|PIX|DIVERGENCE|BLANK|JUDGE_SUMMARY|STOP|LOCK),'
  grep -v '^HDR' "$LINES"
  [ "$ok" = 1 ]
}

[ $# -ge 1 ] || usage
MODE=$1
shift
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT_OVERRIDE=${2:?}; shift 2 ;;
    *) usage ;;
  esac
done
[ "$MODE" != mesh ] || EVID="$PHASE_DIR/evidence/74-02-mesh.txt"
[ "$MODE" != judge ] || EVID="$PHASE_DIR/evidence/74-06-judge.txt"
[ -z "$OUT_OVERRIDE" ] || EVID=$OUT_OVERRIDE

case "$MODE" in
  tracer) tracer ;;
  identity) identity ;;
  cost) cost ;;
  units) units ;;
  mesh) mesh ;;
  judge) judge_cmd ;;
  *) usage ;;
esac

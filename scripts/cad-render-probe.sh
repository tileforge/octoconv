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

Every mode rewrites its own lines in the evidence file (default
.planning/phases/74-3d-render-measurements/evidence/74-01-wave0.txt) and removes the
image tags it built. Needs a native arm64 docker server, go and python3.
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

[ $# -ge 1 ] || usage
MODE=$1
shift
while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT_OVERRIDE=${2:?}; shift 2 ;;
    *) usage ;;
  esac
done
[ -z "$OUT_OVERRIDE" ] || EVID=$OUT_OVERRIDE

case "$MODE" in
  tracer) tracer ;;
  identity) identity ;;
  *) usage ;;
esac

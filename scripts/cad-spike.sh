#!/usr/bin/env bash
# cad-spike.sh -- phase 73 plan 01: the cad tracer and the OCP bomb spikes.
#
# Usage:
#   scripts/cad-spike.sh tracer
#
# tracer   build the production engine image, run cad-mesh on the three inch STEP
#          files under a real Landlock ruleset, judge the STL by content and prove
#          that the same bytes outside the job directory yield no STL.
#
# Needs a native docker server (an emulated image cannot establish Landlock).
# Exit codes: 64 usage, 69 docker or go missing, 1 a check failed.

set -euo pipefail
export LC_ALL=C

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"
. "$REPO_ROOT/scripts/cad-measure/common.sh"

PHASE_DIR="$REPO_ROOT/.planning/phases/73-cad-class-convert-slice-confined"
EVID="$PHASE_DIR/evidence/73-01-spikes.txt"
CORPUS="$REPO_ROOT/internal/convert/testdata/cad"
LOCK_COMMON="$REPO_ROOT/scripts/cad-measure/requirements-common.txt"
LOCK_OCP="$REPO_ROOT/scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt"
SUBSET="$REPO_ROOT/cmd/cad-worker/requirements-mesh.txt"
HELPER="$REPO_ROOT/cmd/cad-worker/helper/cad-mesh"
DOCKERFILE="$REPO_ROOT/Dockerfile.cad-worker"
JUDGE="$REPO_ROOT/scripts/cad-stl-judge.py"
LOCK_COMMON_SHA=9d391daad60a17595077c7aa68f16890c95511a617dbb209fefaf0b5b69a78da
LOCK_OCP_SHA=72b833ac43ac4cdab8fbb694ed9b2954472f2c1ab09b4ebc32684bf2387147df
BASE_DIGEST=sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251
GO_TOOLCHAIN=go1.26.4
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)

usage() {
  sed -n '/^# Usage:/,/^# Needs/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2
  exit 64
}

fail() { echo "FAIL: $*" >&2; exit 1; }

need_tools() {
  command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
  command -v go >/dev/null 2>&1 || { echo "error: go is required to build the probe" >&2; exit 69; }
  command -v python3 >/dev/null 2>&1 || { echo "error: python3 is required" >&2; exit 69; }
}

sha_of() { shasum -a 256 "$1" | awk '{print $1}'; }

block_of() {
  awk -v name="$2" '
    index($0, name "==") == 1 { p = 1; print; next }
    p && /^[A-Za-z0-9]/ { p = 0 }
    p { print }
  ' "$1"
}

lock_check() {
  local common_sha ocp_sha pkg have want rc=0
  common_sha=$(sha_of "$LOCK_COMMON")
  ocp_sha=$(sha_of "$LOCK_OCP")
  if [ "$common_sha" = "$LOCK_COMMON_SHA" ]; then
    printf 'LOCK,file=scripts/cad-measure/requirements-common.txt,sha256=%s,result=ok\n' "$common_sha"
  else
    printf 'LOCK,file=scripts/cad-measure/requirements-common.txt,sha256=%s,expected=%s,result=fail\n' "$common_sha" "$LOCK_COMMON_SHA"; rc=1
  fi
  if [ "$ocp_sha" = "$LOCK_OCP_SHA" ]; then
    printf 'LOCK,file=scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt,sha256=%s,result=ok\n' "$ocp_sha"
  else
    printf 'LOCK,file=scripts/cad-measure/requirements-ocp-7.9.3.1.1.txt,sha256=%s,expected=%s,result=fail\n' "$ocp_sha" "$LOCK_OCP_SHA"; rc=1
  fi
  have=$(grep -E '^[A-Za-z0-9]' "$SUBSET" | sed 's/==.*//' | sort | tr '\n' '+' | sed 's/+$//')
  want="cadquery-ocp-novtk+cadquery-ocp-proxy+numpy"
  if [ "$have" = "$want" ]; then
    printf 'LOCK,subset=requirements-mesh.txt,packages=%s,result=ok\n' "$have"
  else
    printf 'LOCK,subset=requirements-mesh.txt,packages=%s,expected=%s,result=fail\n' "$have" "$want"; rc=1
  fi
  for pkg in numpy cadquery-ocp-novtk cadquery-ocp-proxy; do
    local src=$LOCK_OCP
    [ "$pkg" = numpy ] && src=$LOCK_COMMON
    if [ "$(block_of "$SUBSET" "$pkg")" = "$(block_of "$src" "$pkg")" ] && [ -n "$(block_of "$SUBSET" "$pkg")" ]; then
      printf 'LOCK,block=%s,diff=empty,hashes=%s,result=ok\n' "$pkg" "$(block_of "$SUBSET" "$pkg" | grep -c -- '--hash=')"
    else
      printf 'LOCK,block=%s,diff=nonempty,result=fail\n' "$pkg"; rc=1
    fi
  done
  return $rc
}

inputs_short() {
  shasum -a 256 "$DOCKERFILE" "$SUBSET" "$HELPER" | awk '{print $1}' | shasum -a 256 | cut -c1-12
}

ensure_image() {
  local tag stage
  tag="octoconv-cad-worker:engine-$(inputs_short)"
  if docker image inspect "$tag" >/dev/null 2>&1; then
    printf '%s\n' "$tag"
    return 0
  fi
  stage=$(mktemp -d "${TMPDIR:-/tmp}/cad-worker-build.XXXXXX")
  mkdir -p "$stage/cmd/cad-worker/helper"
  cp "$DOCKERFILE" "$stage/Dockerfile.cad-worker"
  cp "$SUBSET" "$stage/cmd/cad-worker/requirements-mesh.txt"
  cp "$HELPER" "$stage/cmd/cad-worker/helper/cad-mesh"
  cad_log "building $tag"
  if ! docker build -f "$stage/Dockerfile.cad-worker" --target engine -t "$tag" "$stage" >&2; then
    rm -rf "$stage"
    cad_log "error: image build failed"
    return 1
  fi
  rm -rf "$stage"
  printf '%s\n' "$tag"
}

triplet_for() {
  case "$1" in
    arm64) echo aarch64-linux-gnu ;;
    amd64) echo x86_64-linux-gnu ;;
    *) echo "error: no multiarch triplet for $1" >&2; return 1 ;;
  esac
}

probe_ro_for() {
  printf '/opt/venv:/usr/bin/python3.11:/usr/lib/%s:/usr/lib/python3.11:/usr/local/bin/cad-mesh' "$(triplet_for "$1")"
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

tracer_verdict_count() {
  grep -ac '"verdict":"ok"' "$1" || true
}

tracer() {
  need_tools
  [ "$(docker version --format '{{.Server.Arch}}')" = arm64 ] || fail "docker server is not arm64"
  [ "$(GOTOOLCHAIN=$GO_TOOLCHAIN go env GOVERSION)" = "$GO_TOOLCHAIN" ] || fail "go toolchain is not $GO_TOOLCHAIN"

  local out_lock
  out_lock=$(mktemp "${TMPDIR:-/tmp}/cad-spike-lock.XXXXXX")
  if ! lock_check | tee "$out_lock"; then
    rm -f "$out_lock"
    fail "lock check"
  fi

  local tag image_id arch ro ok=1
  tag=$(ensure_image)
  cad_require_native "$tag" || fail "image is not native"
  image_id=$(docker image inspect "$tag" --format '{{.Id}}')
  arch=$(docker image inspect "$tag" --format '{{.Architecture}}')
  ro=$(probe_ro_for "$arch")

  work=$(mktemp -d "${TMPDIR:-/tmp}/cad-spike.XXXXXX")
  trap 'rm -rf "$work"' EXIT
  chmod 0755 "$work"
  build_probe "$arch" "$work/probe"

  local lines="$work/lines.txt"
  : > "$lines"
  {
    printf 'HDR,stage=tracer,date_utc=%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'HDR,stage=tracer,commit=%s\n' "$(git rev-parse HEAD)"
    printf 'HDR,stage=tracer,image=%s\n' "$tag"
    printf 'HDR,stage=tracer,image_id=%s\n' "$image_id"
    printf 'HDR,stage=tracer,server_arch=%s\n' "$(docker version --format '{{.Server.Arch}}')"
    printf 'HDR,stage=tracer,kernel=%s\n' "$(docker run --rm --network none --entrypoint uname "$tag" -r)"
    printf 'HDR,stage=tracer,base_digest=%s\n' "$BASE_DIGEST"
    printf 'HDR,stage=tracer,probe_ro=%s\n' "$ro"
    printf 'HDR,stage=tracer,limits=cpus2_memory4g_network_none_user_nobody\n'
  } >> "$lines"

  local name path judge_args job log rc t0 t1 judge_line judge_fields triangles result
  for name in units_inch d2mi ctc_05; do
    case "$name" in
      units_inch) path="$CORPUS/synthetic/units_inch.step"
        judge_args=(--expect-triangles 12 --expect-extents 25.4,50.8,76.2) ;;
      d2mi) path="$CORPUS/public/nist-d2mi-827-9999-906.stp"
        judge_args=(--step-source "$path") ;;
      ctc_05) path="$CORPUS/public/nist_ctc_05_asme1_ap242-e1.stp"
        judge_args=(--step-source "$path") ;;
    esac
    job=$(new_job "$work")
    log="$work/$name.log"
    rc=0
    t0=$(date +%s)
    docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e LP_NUM_THREADS=2 \
      -e "PROBE_RO=$ro" -e PROBE_RW=/job \
      --entrypoint /probe/sandbox-probe -v "$work/probe:/probe:ro" \
      -v "$job:/job" -v "$path:/job/in.step:ro" "$tag" \
      /usr/local/bin/cad-mesh --format step /job/in.step /job/out.stl > "$log" 2>&1 </dev/null || rc=$?
    t1=$(date +%s)
    if probe_refused "$log"; then
      judge_line="JUDGE,result=fail,reason=probe_refused"
      result=fail
    elif [ ! -s "$job/out.stl" ]; then
      judge_line="JUDGE,result=fail,reason=no_stl"
      result=fail
    elif judge_line=$(python3 "$JUDGE" "$job/out.stl" --verdict-log "$log" ${judge_args[@]+"${judge_args[@]}"}); then
      result=ok
    else
      result=fail
    fi
    [ "$result" = ok ] || ok=0
    triangles=$(printf '%s' "$judge_line" | sed -n 's/.*,triangles=\([0-9]*\).*/\1/p')
    judge_fields=$(printf '%s' "$judge_line" | sed -e 's/^JUDGE,//' -e 's/^stl=[^,]*,//' -e 's/^triangles=[0-9]*,//' -e 's/,result=[a-z]*//')
    printf 'TRACER,input=%s,bytes=%s,sha256=%s,rc=%s,wall_s=%s,triangles=%s,confined=1,%s,result=%s\n' \
      "$name" "$(wc -c < "$path" | tr -d ' ')" "$(sha_of "$path")" "$rc" "$((t1 - t0))" "${triangles:-na}" "$judge_fields" "$result" >> "$lines"
    [ "$result" = ok ] || { echo "---- $name log tail ----" >&2; tail -n 20 "$log" >&2; }
  done

  local ctl_job ctl_log ctl_rc=0 neg_job neg_log neg_rc=0 neg_result=fail verdict_ok stl_state control
  ctl_job=$(new_job "$work")
  ctl_log="$work/control.log"
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e LP_NUM_THREADS=2 \
    -v "$ctl_job:/job" -v "$CORPUS/synthetic/units_inch.step:/outside/in.step:ro" "$tag" \
    /usr/local/bin/cad-mesh --format step /outside/in.step /job/out.stl > "$ctl_log" 2>&1 </dev/null || ctl_rc=$?
  if [ -s "$ctl_job/out.stl" ] && python3 "$JUDGE" "$ctl_job/out.stl" --verdict-log "$ctl_log" --expect-triangles 12 >/dev/null; then
    control=unconfined_ok
  else
    control=unconfined_failed
  fi

  neg_job=$(new_job "$work")
  neg_log="$work/negative.log"
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e LP_NUM_THREADS=2 \
    -e "PROBE_RO=$ro" -e PROBE_RW=/job \
    --entrypoint /probe/sandbox-probe -v "$work/probe:/probe:ro" \
    -v "$neg_job:/job" -v "$CORPUS/synthetic/units_inch.step:/outside/in.step:ro" "$tag" \
    /usr/local/bin/cad-mesh --format step /outside/in.step /job/out.stl > "$neg_log" 2>&1 </dev/null || neg_rc=$?
  verdict_ok=$(tracer_verdict_count "$neg_log")
  if [ -s "$neg_job/out.stl" ]; then stl_state=present; else stl_state=absent; fi
  if probe_refused "$neg_log"; then
    neg_result=fail
    stl_state=probe_refused
  elif [ "$stl_state" = absent ] && [ "$verdict_ok" = 0 ] && [ "$control" = unconfined_ok ]; then
    neg_result=denied
  fi
  [ "$neg_result" = denied ] || ok=0
  printf 'TRACER,input=units_inch_outside,rc=%s,confined=1,stl=%s,verdict_ok_lines=%s,control=%s,control_rc=%s,result=%s\n' \
    "$neg_rc" "$stl_state" "$verdict_ok" "$control" "$ctl_rc" "$neg_result" >> "$lines"

  mkdir -p "$(dirname "$EVID")"
  local keep
  keep=$(mktemp "${TMPDIR:-/tmp}/cad-spike-keep.XXXXXX")
  if [ -f "$EVID" ]; then
    grep -v -e '^HDR,stage=tracer,' -e '^LOCK,' -e '^TRACER,' "$EVID" > "$keep" || true
  fi
  { cat "$out_lock" "$lines" "$keep"; } > "$EVID"
  rm -f "$keep" "$out_lock"
  cat "$lines"

  docker rmi "$tag" >/dev/null 2>&1 || true
  if [ "$ok" = 1 ]; then
    echo TRACER-OK
    return 0
  fi
  echo "TRACER-FAILED" >&2
  return 1
}

[ $# -ge 1 ] || usage
mode=$1
shift
case "$mode" in
  tracer) tracer "$@" ;;
  -h|--help) usage ;;
  *) echo "error: unknown mode $mode" >&2; usage ;;
esac

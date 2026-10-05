#!/usr/bin/env bash

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
SPIKE_PY="$REPO_ROOT/scripts/cad-spike.py"
WRAP_PY="$REPO_ROOT/scripts/cad-measure/measure_wrap.py"
CORPUS_GEN="$REPO_ROOT/scripts/cad-corpus-gen.py"
CAD_CACHE="${OCTOCONV_CAD_CACHE:-$HOME/.cache/octoconv/cad-corpus}"
SPIKE_CACHE="$CAD_CACHE/spike73"
CELL_BUDGET_S=400
MIN_FREE_GIB=7
MIB=1048576
STEP_CEILING_BYTES=$((63 * MIB))
IGES_CEILING_BYTES=$((155 * MIB))
HIER_FILE_LIMIT_BYTES=$((512 * MIB))
HIER_MAX_K=16
LOCK_COMMON_SHA=9d391daad60a17595077c7aa68f16890c95511a617dbb209fefaf0b5b69a78da
LOCK_OCP_SHA=72b833ac43ac4cdab8fbb694ed9b2954472f2c1ab09b4ebc32684bf2387147df
BASE_DIGEST=sha256:3783cc01769c7b2b1b83a5c5ad96c815348e28ed7da68e2e3687004faa906251
GO_TOOLCHAIN=go1.26.4
LIMITS=(--network none --cpus 2 --memory 4g --memory-swap 4g --user nobody)

usage() {
  cat >&2 <<'USAGE'
Usage:
  scripts/cad-spike.sh tracer
  scripts/cad-spike.sh spikes [--only S0,S1,S2,S3,S3M,S4,S5,S5I]
  scripts/cad-spike.sh rules [--check]
  scripts/cad-spike.sh finish

tracer   build the production engine image, run cad-mesh on the three inch STEP
         files under a real Landlock ruleset, judge the STL by content and prove
         that the same bytes outside the job directory yield no STL.
spikes   run every bomb shape against the real OCP reader in that image, one cell
         at a time, and write the observations, then the RULE and DERIVED lines
         computed from them, to the evidence file.
         --only reruns the listed sections and keeps the other rows.
rules    print the RULE and DERIVED lines computed from the evidence rows;
         --check fails when the lines in the evidence file differ from them.
finish   recompute the GAP, LOADCHECK_SUMMARY, RULE, DERIVED and SUMMARY lines
         from the ROW and LOADCHECK lines, without running anything.

Needs a native docker server. Exit codes: 64 usage, 69 docker or go missing, 1 a check failed.
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

free_gib() {
  df -k / | awk 'NR == 2 { print int($4 / 1048576) }'
}

disk_guard() {
  local free
  free=$(free_gib)
  if [ "$free" -lt "$MIN_FREE_GIB" ]; then
    echo "STOP,reason=disk_below_threshold,free_gib=$free,threshold_gib=$MIN_FREE_GIB" >&2
    exit 3
  fi
}

format_of() {
  case "$1" in
    *.step|*.stp) echo step ;;
    *.igs|*.iges) echo iges ;;
    *) echo "error: unknown CAD extension $1" >&2; return 1 ;;
  esac
}

CELL_JOB=""

cell() {
  local mode=$1 fmt=$2 input=$3 regex=${4:-0} entry flags
  disk_guard
  CELL_JOB=$(new_job "$WORK")
  if [ "$mode" = read ]; then
    entry=/opt/venv/bin/python
    flags=(-I -B)
    inner=(/opt/venv/bin/python -I -B /spike/cad-spike.py read --format "$fmt" /in/input /job)
  else
    entry=/usr/bin/python3
    flags=(-IB)
    inner=(/usr/bin/python3 -IB /spike/cad-spike.py count --format "$fmt" /in/input)
    [ "$regex" = 1 ] && inner+=(--with-regex)
  fi
  docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e LP_NUM_THREADS=2 \
    -v "$WORK/spike:/spike:ro" -v "$CELL_JOB:/job" -v "$input:/in/input:ro" \
    --entrypoint "$entry" "$TAG" "${flags[@]}" /spike/measure_wrap.py --budget-s "$CELL_BUDGET_S" --outdir /job -- "${inner[@]}" \
    > "$WORK/wrap.out" 2> "$WORK/wrap.err" </dev/null || true
}

emit() {
  printf '%s\n' "$1" | tee -a "$SPIKES_OUT"
}

emit_row() {
  local spike=$1 input=$2 fmt=$3 count_json=$4
  shift 4
  local args=(--spike "$spike" --format "$fmt" --input "$input" --image-id "$IMAGE_ID" --wrap-file "$WORK/wrap.out" --read-json "$CELL_JOB/read.json")
  [ -n "$count_json" ] && args+=(--count-json "$count_json")
  local extra
  for extra in "$@"; do args+=(--extra "$extra"); done
  emit "$(python3 "$SPIKE_PY" rowline "${args[@]}")"
}

wrap_field() {
  sed -n "s/^WRAP,.*[,]$1=\\([^,]*\\).*/\\1/p" "$WORK/wrap.out" | tail -n 1
}

cell_stopped() {
  local oom
  oom=$(wrap_field oom_kill)
  [ "$(wrap_field timeout)" = 1 ] && return 0
  case "$oom" in
    ''|*[!0-9]*) return 1 ;;
  esac
  [ "$oom" -gt 0 ]
}

spike_s0() {
  local f fmt name first=1
  local files=()
  for f in "$CORPUS"/public/*.step "$CORPUS"/public/*.stp "$CORPUS"/public/*.igs "$CORPUS"/public/*.iges \
           "$CORPUS"/synthetic/*.step "$CORPUS"/synthetic/*.iges \
           "$CAD_CACHE"/fetched/*.step "$CAD_CACHE"/fetched/*.stp "$CAD_CACHE"/fetched/*.igs "$CAD_CACHE"/fetched/*.iges; do
    [ -f "$f" ] && files+=("$f")
  done
  [ "${#files[@]}" -gt 0 ] || fail "no corpus files"
  for f in "${files[@]}"; do
    fmt=$(format_of "$f")
    case "$f" in
      "$CORPUS"/*) name="${f#"$CORPUS"/}" ;;
      *) name="fetched/$(basename "$f")" ;;
    esac
    cell read "$fmt" "$f"
    if [ "$first" = 1 ] && [ -s "$CELL_JOB/read.json" ]; then
      first=0
      python3 "$SPIKE_PY" binding --read-json "$CELL_JOB/read.json" | while IFS= read -r line; do emit "$line"; done
    fi
    emit "$(python3 "$SPIKE_PY" loadcheck --name "$name" --read-json "$CELL_JOB/read.json")"
    rm -rf "$CELL_JOB"
  done
}

spike_simple() {
  local spike=$1 pattern=$2 f
  for f in "$SPIKE_CACHE"/$pattern; do
    [ -f "$f" ] || continue
    cell read step "$f"
    emit_row "$spike" "$f" step ""
    rm -rf "$CELL_JOB"
  done
}

spike_s4() {
  local f cj
  for f in "$SPIKE_CACHE"/s4_*.step; do
    [ -f "$f" ] || continue
    cj="$WORK/s4-count.json"
    python3 "$SPIKE_PY" count --format step --with-regex "$f" > "$cj"
    cell read step "$f"
    emit_row S4 "$f" step "$cj"
    rm -rf "$CELL_JOB"
  done
}

spike_s3() {
  local kind k out gj instances stop leaf
  leaf=/corpus/public/LinearSlide-MGN15x-Block.step
  for kind in nauo mapped; do
    k=4
    stop=0
    while [ "$k" -le "$HIER_MAX_K" ] && [ "$stop" = 0 ]; do
      disk_guard
      out="$SPIKE_CACHE/hier_${kind}_K$(printf '%02d' "$k").step"
      gj="$WORK/hier-gen.json"
      rm -f "$out" "$gj"
      instances=$((1 << k))
      CELL_JOB=$(new_job "$WORK")
      docker run --rm "${LIMITS[@]}" -e HOME=/job -e LANG=C.UTF-8 -e LP_NUM_THREADS=2 \
        -v "$WORK/spike:/spike:ro" -v "$CELL_JOB:/job" -v "$CORPUS:/corpus:ro" -v "$SPIKE_CACHE:/gen" \
        --entrypoint /opt/venv/bin/python "$TAG" -I -B /spike/measure_wrap.py --budget-s "$CELL_BUDGET_S" --outdir /job -- \
        /opt/venv/bin/python -I -B /spike/cad-spike.py gen-hierarchy --kind "$kind" --levels "$k" --leaf "$leaf" "/gen/$(basename "$out")" \
        > "$WORK/wrap.out" 2> "$WORK/wrap.err" </dev/null || true
      cp "$CELL_JOB/child.log" "$gj" 2>/dev/null || : > "$gj"
      if [ ! -s "$out" ]; then
        emit_row S3 "$out" step "" "kind=$kind" "K=$k" "instances=$instances" "op=gen"
        rm -rf "$CELL_JOB"
        stop=1
        continue
      fi
      if [ "$(wc -c < "$out" | tr -d ' ')" -gt "$HIER_FILE_LIMIT_BYTES" ]; then
        emit "ROW,spike=S3,kind=$kind,K=$k,instances=$instances,op=skip,reason=generated_file_above_512MiB,bytes=$(wc -c < "$out" | tr -d ' '),image_id=$IMAGE_ID"
        rm -rf "$CELL_JOB" "$out"
        stop=1
        continue
      fi
      rm -rf "$CELL_JOB"
      cell read step "$out"
      emit_row S3 "$out" step "$gj" "kind=$kind" "K=$k" "instances=$instances" "op=mesh"
      cell_stopped && stop=1
      rm -rf "$CELL_JOB" "$out"
      k=$((k + 1))
    done
  done
}

gen_corpus() {
  local base=$1 basedir=$2 kind=$3 target=$4
  docker run --rm --network none --cpus 2 --memory 4g --memory-swap 4g --user nobody -e HOME=/tmp -e LANG=C.UTF-8 \
    -v "$REPO_ROOT:/src:ro" -v "$basedir:/base:ro" -v "$SPIKE_CACHE:/out" -w /src \
    --entrypoint /opt/venv/bin/python "$TAG" -I -B scripts/cad-corpus-gen.py \
    --scaled-from "/base/$base" --kind "$kind" --targets "$target" --scaled-dir /out >&2
}

make_s5_files() {
  local t out size limit tries best_size seen
  t=$(python3 "$SPIKE_PY" copies-target --base "$CAD_CACHE/fetched/freecad-mit-e-vent-t-slot.step" --limit-bytes "$STEP_CEILING_BYTES" | sed -n 's/.*target=\([0-9]*\),.*/\1/p')
  [ -n "$t" ] || fail "no step-copies target under the ceiling"
  gen_corpus freecad-mit-e-vent-t-slot.step "$CAD_CACHE/fetched" step-copies "$t"
  S5_STEP="$SPIKE_CACHE/step-copies-freecad-mit-e-vent-t-slot-${t}MiB.step"
  [ "$(wc -c < "$S5_STEP" | tr -d ' ')" -le "$STEP_CEILING_BYTES" ] || fail "step copies file above 63 MiB"

  gen_corpus LinearSlide-MGN15x-Block.step "$CORPUS/public" step-nauo 1518
  S5_NAUO="$SPIKE_CACHE/step-nauo-LinearSlide-MGN15x-Block-n1518.step"
  [ -f "$S5_NAUO" ] || fail "step-nauo 1518 missing"

  limit=24500000
  S5_IGES=""
  best_size=0
  seen=" "
  tries=0
  while [ "$tries" -lt 4 ]; do
    t=$(python3 "$SPIKE_PY" copies-target --base "$CORPUS/public/LinearSlide-MGN15x-Block.step" --limit-bytes "$limit" | sed -n 's/.*target=\([0-9]*\),.*/\1/p')
    case "$seen" in *" $t "*) break ;; esac
    seen="$seen$t "
    gen_corpus LinearSlide-MGN15x-Block.step "$CORPUS/public" step-copies "$t"
    gen_corpus "step-copies-LinearSlide-MGN15x-Block-${t}MiB.step" "$SPIKE_CACHE" iges-from-step 1 || true
    out="$SPIKE_CACHE/iges-step-copies-LinearSlide-MGN15x-Block-${t}MiB.igs"
    [ -f "$out" ] || fail "iges copies file missing"
    size=$(wc -c < "$out" | tr -d ' ')
    if [ "$size" -le "$IGES_CEILING_BYTES" ] && [ "$size" -gt "$best_size" ]; then
      S5_IGES=$out
      best_size=$size
    fi
    if [ "$size" -le "$IGES_CEILING_BYTES" ] && [ $((size * 100)) -ge $((IGES_CEILING_BYTES * 93)) ]; then
      break
    fi
    limit=$((limit * IGES_CEILING_BYTES * 985 / (size * 1000)))
    tries=$((tries + 1))
  done
  [ -n "$S5_IGES" ] || fail "no IGES copies file at or below 155 MiB"
}

spike_s3m() {
  local k out gj stop=0
  k=4
  while [ "$k" -le "$HIER_MAX_K" ] && [ "$stop" = 0 ]; do
    disk_guard
    out="$SPIKE_CACHE/hier_mappedtext_K$(printf '%02d' "$k").step"
    gj="$WORK/hier-gen.json"
    python3 "$SPIKE_PY" gen-mapped-text --levels "$k" "$out" > "$gj"
    cell read step "$out"
    emit_row S3 "$out" step "$gj" "kind=mapped_text" "K=$k" "instances=$((1 << k))" "op=mesh"
    cell_stopped && stop=1
    rm -rf "$CELL_JOB" "$out"
    k=$((k + 1))
  done
}

spike_s5() {
  local f fmt series
  make_s5_files
  for f in "$S5_STEP:step:step_copies_63MiB" "$S5_NAUO:step:step_nauo_1518" "$S5_IGES:iges:iges_copies_155MiB"; do
    series=${f##*:}
    fmt=${f#*:}
    fmt=${fmt%%:*}
    f=${f%%:*}
    cell count "$fmt" "$f"
    emit_row S5 "$f" "$fmt" "$CELL_JOB/child.log" "op=count" "series=$series"
    rm -rf "$CELL_JOB"
    cell read "$fmt" "$f"
    emit_row S5 "$f" "$fmt" "" "op=mesh" "series=$series"
    rm -rf "$CELL_JOB"
  done
}

spike_s5i() {
  local target t step out
  for target in 60 90 105 120 135; do
    disk_guard
    t=$(( (target * 181 + 74) / 148 ))
    gen_corpus LinearSlide-MGN15x-Block.step "$CORPUS/public" step-copies "$t"
    step="$SPIKE_CACHE/step-copies-LinearSlide-MGN15x-Block-${t}MiB.step"
    gen_corpus "$(basename "$step")" "$SPIKE_CACHE" iges-from-step 1 || true
    out="$SPIKE_CACHE/iges-step-copies-LinearSlide-MGN15x-Block-${t}MiB.igs"
    [ -f "$out" ] || fail "iges ladder file missing"
    cell read iges "$out"
    emit_row S5 "$out" iges "" "op=mesh" "series=iges_ladder_${target}MiB"
    rm -rf "$CELL_JOB" "$out" "$step"
  done
}

finish_evidence() {
  local complete=$1 keep body rules_file
  mkdir -p "$(dirname "$EVID")"
  keep=$(mktemp "${TMPDIR:-/tmp}/cad-spike-keep.XXXXXX")
  body=$(mktemp "${TMPDIR:-/tmp}/cad-spike-body.XXXXXX")
  rules_file=$(mktemp "${TMPDIR:-/tmp}/cad-spike-rules.XXXXXX")
  if [ -f "$EVID" ]; then
    grep -e '^HDR,stage=tracer,' -e '^LOCK,' -e '^TRACER,' "$EVID" > "$keep" || true
  fi
  { cat "$keep"; cat "$SPIKES_HDR"; cat "$SPIKES_OUT"; } > "$body"
  if [ "$complete" = 1 ]; then
    python3 "$SPIKE_PY" rules "$body" > "$rules_file"
    cat "$rules_file" >> "$body"
    printf 'SUMMARY,rows=%s,rules=%s,derived=%s,gaps=%s\n' "$(grep -c '^ROW,' "$body")" "$(grep -c '^RULE,' "$body")" "$(grep -c '^DERIVED,' "$body")" "$(grep -c '^GAP,' "$body")" >> "$body"
  else
    printf 'HDR,stage=spikes,complete=0\n' >> "$body"
  fi
  cp "$body" "$EVID"
  rm -f "$keep" "$body" "$rules_file"
}

want() {
  [ -z "$ONLY" ] && return 0
  case ",$ONLY," in
    *",$1,"*) return 0 ;;
    *) return 1 ;;
  esac
}

drop_sections() {
  local section
  for section in $(printf '%s' "$ONLY" | tr ',' ' '); do
    case "$section" in
      S0) grep -v -e '^BINDING,' -e '^LOADCHECK,' "$1" > "$1.tmp" || true ;;
      S1|S2|S4) grep -v "^ROW,spike=$section," "$1" > "$1.tmp" || true ;;
      S5) awk '!(/^ROW,spike=S5,/ && !/,series=iges_ladder_/)' "$1" > "$1.tmp" ;;
      S5I) awk '!(/^ROW,spike=S5,/ && /,series=iges_ladder_/)' "$1" > "$1.tmp" ;;
      S3) awk '!(/^ROW,spike=S3,/ && (/,kind=nauo,/ || /,kind=mapped,/))' "$1" > "$1.tmp" ;;
      S3M) awk '!(/^ROW,spike=S3,/ && /,kind=mapped_text,/)' "$1" > "$1.tmp" ;;
      *) fail "unknown section $section" ;;
    esac
    mv "$1.tmp" "$1"
  done
}

spikes() {
  ONLY=""
  if [ "${1:-}" = --only ]; then
    ONLY=${2:?--only needs a list}
    shift 2
  fi
  need_tools
  [ "$(docker version --format '{{.Server.Arch}}')" = arm64 ] || fail "docker server is not arm64"
  local running
  running=$(docker ps --format '{{.Image}}')
  if printf '%s\n' "$running" | grep -q octoconv-cad; then fail "another CAD measurement container is running"; fi
  [ "$(ls "$CAD_CACHE/fetched" 2>/dev/null | wc -l | tr -d ' ')" = 17 ] || fail "the phase 71 fetched corpus is not present (17 files); run scripts/cad-corpus-fetch.sh"
  disk_guard

  lock_check > /dev/null || fail "lock check"
  TAG=$(ensure_image)
  cad_require_native "$TAG" || fail "image is not native"
  IMAGE_ID=$(docker image inspect "$TAG" --format '{{.Id}}')

  WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-spike.XXXXXX")
  chmod 0755 "$WORK"
  mkdir -p "$WORK/spike" "$SPIKE_CACHE"
  chmod 0755 "$WORK/spike"
  chmod 0777 "$SPIKE_CACHE"
  cp "$SPIKE_PY" "$WRAP_PY" "$WORK/spike/"
  chmod 0644 "$WORK/spike/"*
  SPIKES_OUT="$WORK/spikes-rows.txt"
  SPIKES_HDR="$WORK/spikes-hdr.txt"
  : > "$SPIKES_OUT"
  local hdr_stage=spikes
  if [ -n "$ONLY" ]; then
    [ -f "$EVID" ] || fail "no evidence file to append to"
    grep -E '^(BINDING|LOADCHECK|ROW),' "$EVID" > "$SPIKES_OUT" || true
    drop_sections "$SPIKES_OUT"
    grep '^HDR,stage=spikes' "$EVID" > "$SPIKES_HDR.old" || true
    hdr_stage=spikes_append
  else
    : > "$SPIKES_HDR.old"
  fi
  {
    cat "$SPIKES_HDR.old"
    printf 'HDR,stage=%s,only=%s\n' "$hdr_stage" "${ONLY:-all}"
    printf 'HDR,stage=%s,date_utc=%s\n' "$hdr_stage" "$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    printf 'HDR,stage=%s,commit=%s\n' "$hdr_stage" "$(git rev-parse HEAD)"
    printf 'HDR,stage=%s,image=%s\n' "$hdr_stage" "$TAG"
    printf 'HDR,stage=%s,image_id=%s\n' "$hdr_stage" "$IMAGE_ID"
    printf 'HDR,stage=%s,server_arch=%s\n' "$hdr_stage" "$(docker version --format '{{.Server.Arch}}')"
    printf 'HDR,stage=%s,kernel=%s\n' "$hdr_stage" "$(docker run --rm --network none --entrypoint uname "$TAG" -r)"
    printf 'HDR,stage=%s,python=%s\n' "$hdr_stage" "$(docker run --rm --network none --entrypoint /opt/venv/bin/python "$TAG" -I -B --version 2>&1 | tr ' ' '_')"
    printf 'HDR,stage=%s,ocp=%s\n' "$hdr_stage" "$(docker run --rm --network none --entrypoint /opt/venv/bin/python "$TAG" -I -B -c 'import importlib.metadata as m; print(m.version("cadquery-ocp-novtk"))')"
    printf 'HDR,stage=%s,limits=cpus2_memory4g_memswap4g_network_none_user_nobody_lp2\n' "$hdr_stage"
    printf 'HDR,stage=%s,cell_budget_s=%s\n' "$hdr_stage" "$CELL_BUDGET_S"
    printf 'HDR,stage=%s,host_containers=%s\n' "$hdr_stage" "$(docker ps -q | wc -l | tr -d ' ')"
    printf 'HDR,stage=%s,free_gib_at_start=%s\n' "$hdr_stage" "$(free_gib)"
  } > "$SPIKES_HDR"
  trap 'finish_evidence 0; docker rmi "$TAG" >/dev/null 2>&1 || true; rm -rf "$WORK"' EXIT

  python3 "$SPIKE_PY" gen-bombs "$SPIKE_CACHE" > "$WORK/bombs.txt"

  if want S0; then spike_s0; fi
  if want S1; then spike_simple S1 's1_*.step'; fi
  if want S2; then spike_simple S2 's2_*.step'; fi
  if want S4; then spike_s4; fi
  if want S3; then spike_s3; fi
  if want S3M; then spike_s3m; fi
  if want S5; then spike_s5; fi
  if want S5I; then spike_s5i; fi

  trap - EXIT
  finish_evidence 1
  docker rmi "$TAG" >/dev/null 2>&1 || true
  rm -rf "$WORK"

  if ! python3 "$SPIKE_PY" rules "$EVID" --check; then fail "rules --check after the run"; fi
  local stops
  if ! stops=$(python3 "$SPIKE_PY" stops "$EVID"); then
    printf '%s\n' "$stops"
    echo "SPIKES-STOP: a stop condition of the plan holds; return a checkpoint, do not commit" >&2
    return 2
  fi
  echo SPIKES-OK
}

finish_mode() {
  local body rules_file
  need_tools
  [ -f "$EVID" ] || fail "no evidence file $EVID"
  body=$(mktemp "${TMPDIR:-/tmp}/cad-spike-body.XXXXXX")
  rules_file=$(mktemp "${TMPDIR:-/tmp}/cad-spike-rules.XXXXXX")
  grep -v -e '^RULE,' -e '^DERIVED,' -e '^SUMMARY,' -e '^LOADCHECK_SUMMARY,' -e '^GAP,' -e '^HDR,stage=spikes,complete=0' "$EVID" > "$body" || true
  python3 "$SPIKE_PY" rules "$body" > "$rules_file"
  cat "$rules_file" >> "$body"
  printf 'SUMMARY,rows=%s,rules=%s,derived=%s,gaps=%s\n' "$(grep -c '^ROW,' "$body")" "$(grep -c '^RULE,' "$body")" "$(grep -c '^DERIVED,' "$body")" "$(grep -c '^GAP,' "$body")" >> "$body"
  cp "$body" "$EVID"
  rm -f "$body" "$rules_file"
  python3 "$SPIKE_PY" rules "$EVID" --check
}

rules_mode() {
  need_tools
  [ -f "$EVID" ] || fail "no evidence file $EVID"
  case "${1:-}" in
    "") python3 "$SPIKE_PY" rules "$EVID" ;;
    --check) python3 "$SPIKE_PY" rules "$EVID" --check ;;
    *) usage ;;
  esac
}

[ $# -ge 1 ] || usage
mode=$1
shift
case "$mode" in
  tracer) tracer "$@" ;;
  spikes) spikes "$@" ;;
  rules) rules_mode "$@" ;;
  finish) finish_mode "$@" ;;
  -h|--help) usage ;;
  *) echo "error: unknown mode $mode" >&2; usage ;;
esac

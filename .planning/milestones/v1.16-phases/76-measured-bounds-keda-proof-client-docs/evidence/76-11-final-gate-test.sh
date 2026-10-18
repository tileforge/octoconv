#!/usr/bin/env bash
set -uo pipefail

REPO=$(git -C "$(dirname "$0")" rev-parse --show-toplevel)
WRAPPER="$REPO/scripts/final-live-gate.sh"
T=$(mktemp -d)
trap 'pkill -f "$T/" 2>/dev/null; rm -rf "$T"' EXIT
STUB="$T/bin"
mkdir -p "$STUB"
U1=11111111-2222-3333-4444-555555555555
U2=66666666-7777-8888-9999-000000000000
REAL_MKTEMP=$(command -v mktemp)
REAL_GIT=$(command -v git)

cat > "$STUB/git" <<EOF
#!/usr/bin/env bash
if [ "\${1:-}" = status ] && [ -n "\${STUB_GIT_STATUS_FAIL:-}" ]; then
  exit 128
fi
exec "$REAL_GIT" "\$@"
EOF

cat > "$STUB/mktemp" <<EOF
#!/usr/bin/env bash
if [ "\$*" = -d ] && [ -n "\${STUB_TMPDIR:-}" ]; then
  exec "$REAL_MKTEMP" -d "\$STUB_TMPDIR/tmp.XXXXXXXX"
fi
exec "$REAL_MKTEMP" "\$@"
EOF

cat > "$STUB/docker" <<'EOF'
#!/usr/bin/env bash
S=$STUB_STATE
echo "docker $*" >> "$S/docker.log"
a="$*"
case "$a" in
  "run --rm alpine df -k /")
    echo "Filesystem 1K-blocks Used Available Use% Mounted on"
    echo "overlay 49545216 28086532 $(cat "$S/free_kib") 57% /" ;;
  "volume ls -q") cat "$S/volumes" ;;
  "ps -q --filter label=com.docker.compose.project=octoconv") [ -z "${STUB_PS_FAIL:-}" ] || exit 1; cat "$S/running" 2>/dev/null ;;
  "ps -aq --filter label=com.docker.compose.project=octoconv") echo c1; echo c2; echo c3 ;;
  "images --no-trunc --format {{.Repository}}:{{.Tag}} {{.ID}}") cat "$S/images_before" ;;
  "images -q --filter reference=octoconv-*:sandbox-liveproof-*") [ -z "${STUB_SANDBOX_IMAGES_FAIL:-}" ] || exit 1; cat "$S/sandbox_left" 2>/dev/null ;;
  "version --format {{.Server.Arch}}") echo arm64 ;;
  "compose -f docker-compose.yml config --format json")
    echo '{"name":"octoconv","services":{"api":{"build":{"context":"."}},"worker":{"build":{"context":"."}},"postgres":{"image":"postgres:18"},"minio":{"image":"m","environment":{"MINIO_ROOT_USER":"minioadmin","MINIO_ROOT_PASSWORD":"minioadmin"}}}}' ;;
  "compose config --format json")
    if [ -n "${STUB_NO_MINIO_CREDS:-}" ]; then
      echo '{"name":"octoconv","services":{"api":{"build":{"context":"."}},"worker":{"build":{"context":"."}},"postgres":{"image":"postgres:18"},"minio":{"image":"m"}}}'
    else
      echo '{"name":"octoconv","services":{"api":{"build":{"context":"."}},"worker":{"build":{"context":"."}},"postgres":{"image":"postgres:18"},"minio":{"image":"m","environment":{"MINIO_ROOT_USER":"minioadmin","MINIO_ROOT_PASSWORD":"minioadmin"}}}}'
    fi ;;
  "compose start postgres minio") [ -z "${STUB_COMPOSE_START_FAIL:-}" ] || exit 1 ;;
  "logs --since "*" octoconv-tectonic-worker") cat "$S/tect_log" ;;
  "inspect -f {{.Image}} octoconv-tectonic-worker") echo sha256:tect1 ;;
  "inspect -f {{range .Config.Env}}{{println .}}{{end}} octoconv-tectonic-worker") [ -z "${STUB_TECT_ENV_FAIL:-}" ] || exit 1; cat "$S/tect_env" ;;
  "inspect -f {{index .Config.Labels \"com.docker.compose.service\"}} {{.Config.Image}} {{.Image}} "*) cat "$S/inspect" ;;
  "image inspect -f {{.Id}} "*) awk -v n="${@: -1}" '$1 == n { print $2 }' "$S/images_now" ;;
  "compose exec -T postgres psql "*)
    case "$a" in
      *string_agg*) [ "${STUB_PSQL_FAIL:-}" != clients ] || exit 1; echo "cad-liveproof-1760000000" ;;
      *"count(*)"*) [ "${STUB_PSQL_FAIL:-}" != others ] || exit 1; echo 0 ;;
      *"select j.id"*) [ "${STUB_PSQL_FAIL:-}" != jobs ] || exit 1; cat "$S/job_ids" ;;
    esac ;;
  "compose exec -T minio mc alias set "*) [ -z "${STUB_MC_ALIAS_FAIL:-}" ] || exit 1 ;;
  "compose exec -T minio mc ls --recursive --json local/octoconv")
    n=$(cat "$S/ls.count" 2>/dev/null || echo 0)
    n=$((n + 1))
    echo "$n" > "$S/ls.count"
    if [ "${STUB_MC_LS_KEYS:-}" = bucket ]; then
      sed 's/"key":"/"key":"octoconv\//' "$S/bucket"
    else
      cat "$S/bucket"
    fi
    case "${STUB_MC_LS_TAIL:-}" in
      error) printf '{\n "status": "error",\n "error": {\n  "message": "Unable to list folder.",\n  "cause": {}\n }\n}\n' ;;
      text) echo "mc: <ERROR> Unable to list folder." ;;
    esac
    if [ -n "${STUB_MC_LS_RC:-}" ] && { [ -z "${STUB_MC_LS_ON:-}" ] || [ "$STUB_MC_LS_ON" = "$n" ]; }; then
      exit "$STUB_MC_LS_RC"
    fi ;;
  "compose exec -T minio mc rm "*)
    [ -z "${STUB_MC_RM_FAIL:-}" ] || exit 1
    [ -z "${STUB_MC_RM_NOOP:-}" ] || exit 0
    t=${@: -1}
    t=${t#local/octoconv/}
    grep -vF "\"key\":\"$t" "$S/bucket" > "$S/bucket.new" || true
    mv "$S/bucket.new" "$S/bucket" ;;
  "compose exec -T minio mc du local/octoconv") printf '3.1GiB\t1314 objects\toctoconv\n' ;;
esac
exit 0
EOF

cat > "$STUB/kubectl" <<'EOF'
#!/usr/bin/env bash
cat "$STUB_STATE/pods" 2>/dev/null
exit 0
EOF

cat > "$STUB/lsof" <<'EOF'
#!/usr/bin/env bash
port=""
for a in "$@"; do case "$a" in -iTCP:*) port=${a#-iTCP:} ;; esac; done
if grep -qx "$port" "$STUB_STATE/taken" 2>/dev/null; then
  echo "COMMAND PID USER"
  echo "holderproc 4242 stub TCP *:$port (LISTEN)"
  exit 0
fi
exit 1
EOF

cat > "$STUB/npm" <<'EOF'
#!/usr/bin/env bash
echo "npm $*" >> "$STUB_STATE/npm.log"
case "$*" in
  "--prefix frontend ci") [ -z "${STUB_NPM_TOUCH_LOCK:-}" ] || echo '{"changed":1}' > frontend/package-lock.json; echo "added 10 packages" ;;
  "--prefix frontend test") echo " Test Files  3 passed (3)"; echo "      Tests  12 passed (12)" ;;
  "--prefix frontend run build") echo "built in 1.0s" ;;
esac
exit 0
EOF

cat > "$STUB/go" <<'EOF'
#!/usr/bin/env bash
[ "$1" = version ] && echo "go version ${GOTOOLCHAIN:-go0} darwin/arm64"
exit 0
EOF

cat > "$STUB/sysctl" <<'EOF'
#!/usr/bin/env bash
l=$(cat "$STUB_STATE/load")
echo "{ $l $l $l }"
EOF
chmod +x "$STUB"/*

mkrepo() {
  local r=$1
  mkdir -p "$r/scripts" "$r/frontend" "$r/deploy/tectonic-bundle"
  cp "$WRAPPER" "$r/scripts/final-live-gate.sh"
  cat > "$r/scripts/ci-local.sh" <<'EOF'
#!/usr/bin/env bash
cd "$(dirname "$0")/.."
env | grep -E '^(CI_LOCAL_E2E_[A-Z_]+|GOTOOLCHAIN|CI_LOCAL_EVIDENCE_DIR)=' | sort > "$STUB_STATE/ci-local.env"
echo "$*" > "$STUB_STATE/ci-local.args"
L="$CI_LOCAL_EVIDENCE_DIR/ci-local-$(date -u +%Y%m%dT%H%M%SZ).log"
h=$(date -u +%H:%M:%S)
{
  echo "=== ci-local stub ==="
  echo "### tier: helm (started $h UTC)"
  echo "### tier helm: PASS"
  echo "### tier: gate (started $h UTC)"
  printf 'ok  \tgithub.com/apaderin/octoconv/internal/convert\t%ss\n' "${STUB_CONVERT_S:-400.1}"
  echo "### tier gate: PASS"
  echo "### tier: race (started $h UTC)"
  printf 'ok  \tgithub.com/apaderin/octoconv/internal/convert\t999.0s\n'
  echo "### tier race: PASS"
  echo "### tier: e2e (started $h UTC)"
  [ -n "${STUB_NO_TECT_PASS:-}" ] || echo "--- PASS: TestMultiFileLaTeXProjectE2E (1.00s)"
  echo "--- PASS: TestMultiFileRefusalsE2E (1.00s)"
  [ -z "${STUB_E2E_FAIL_LINE:-}" ] || echo "--- FAIL: TestSomethingE2E (1.00s)"
  echo "### tier e2e: PASS"
  echo "  ALL REQUESTED TIERS PASSED"
} > "$L"
if [ -n "${STUB_LOW_DISK_SLEEP:-}" ]; then
  echo 1000 > "$STUB_STATE/free_kib"
  sleep "$STUB_LOW_DISK_SLEEP"
fi
exit "${STUB_CILOCAL_RC:-0}"
EOF
  cat > "$r/scripts/sandbox-liveproof.sh" <<'EOF'
#!/usr/bin/env bash
echo "$*" > "$STUB_STATE/sandbox.args"
echo "== image: building octoconv-image:sandbox-liveproof-1 from Dockerfile.worker =="
echo "image id: sha256:sbx1"
printf '%b' "${STUB_SANDBOX_EXTRA:-}"
echo "${STUB_SANDBOX_LAST:-ALL PASS (40 checks)}"
exit "${STUB_SANDBOX_RC:-0}"
EOF
  cat > "$r/scripts/cad-liveproof.sh" <<'EOF'
#!/usr/bin/env bash
if [ -n "${STUB_CAD_SKIP_PREFIX:-}" ]; then
  grep -vF "\"key\":\"$STUB_CAD_SKIP_PREFIX" "$STUB_STATE/run_objects" >> "$STUB_STATE/bucket" || true
else
  cat "$STUB_STATE/run_objects" >> "$STUB_STATE/bucket"
fi
out=""
while [ $# -gt 0 ]; do case "$1" in --out) out=$2; shift 2 ;; *) shift ;; esac; done
env | grep -E '^(CAD_LIVEPROOF_[A-Z_]+|GOTOOLCHAIN)=' | sort > "$STUB_STATE/cad.env"
n=$(cat "$STUB_STATE/cad.count" 2>/dev/null || echo 0)
n=$((n + 1))
echo "$n" > "$STUB_STATE/cad.count"
c=${STUB_CAD_COMMIT:-$(git rev-parse HEAD)}
{
  echo "HDR,commit=$c"
  echo "HDR,tracked_dirt=0"
  echo "HDR,api_image_id=sha256:api1"
  echo "HDR,cad_worker_image_id=sha256:cad1"
  echo "ROW_A,result=ok"
  echo "ROW_B,result=ok"
  echo "ROW_A,result=ok"
  echo "SUMMARY,result=ALL PASS,formats=1"
} > "$out"
eval "loads=\${STUB_CAD_LOADS_$n:-5 5 5 5 5 5 5 5}"
i=0
: > "${out%.txt}-load.txt"
while read -r key; do
  i=$((i + 1))
  l=$(echo "$loads" | awk -v i="$i" '{ print $i }')
  af=$(awk -v l="$l" 'BEGIN { print (l <= 11) ? l : 10 }')
  printf '%s,%s,%s,%s,%s,%s\n' "$i" "$(date -u +%FT%TZ)" "$l" "$key" "$af" 0 >> "${out%.txt}-load.txt"
done < <(cut -d, -f1 "$out")
[ -z "${STUB_CAD_NO_LOAD:-}" ] || rm -f "${out%.txt}-load.txt"
[ -z "${STUB_CAD_ADD_VOLUME:-}" ] || echo octoconv_newvol >> "$STUB_STATE/volumes"
if [ -n "${STUB_CAD_HANG:-}" ]; then
  touch "$STUB_STATE/cad.running"
  sleep 30
fi
exit 0
EOF
  chmod +x "$r/scripts/"*.sh
  echo '{"name":"f"}' > "$r/frontend/package.json"
  echo '{"lockfileVersion":3}' > "$r/frontend/package-lock.json"
  echo "services: {}" > "$r/docker-compose.yml"
  printf 'deploy/tectonic-bundle/octoconv-bundle.ttb\nfrontend/node_modules\n' > "$r/.gitignore"
  git -C "$r" init -q -b gsd/test
  git -C "$r" add -A
  git -C "$r" -c user.name=t -c user.email=t@t -c commit.gpgsign=false commit -q -m init
  echo bundle > "$r/deploy/tectonic-bundle/octoconv-bundle.ttb"
}

mcobj() {
  printf '{"status":"success","type":"file","lastModified":"2026-10-17T00:00:00.000Z","size":%s,"key":"%s","etag":"0123456789abcdef0123456789abcdef","url":"http://localhost:9000/octoconv/","versionOrdinal":1,"storageClass":"STANDARD"}\n' "$2" "$1"
}

reset_state() {
  local s=$1 u k
  mkdir -p "$s"
  {
    mcobj "uploads/99999999-aaaa-bbbb-cccc-dddddddddddd/0-other.dxf" 7
    mcobj "results/${U1}0/0-near.png" 7
  } > "$s/bucket"
  : > "$s/run_objects"
  for u in "$U1" "$U2"; do
    for k in "uploads/$u/0-a.dxf" "uploads/$u/1-b.dxf" "results/$u/0-a.png" "results/$u/1-a.json"; do
      mcobj "$k" 50 >> "$s/run_objects"
    done
  done
  echo 21458684 > "$s/free_kib"
  printf 'octoconv_minio_data\nother_project_db\n' > "$s/volumes"
  : > "$s/running"
  : > "$s/taken"
  printf 'kube-system   coredns-1   1/1   Running   0   1d\n' > "$s/pods"
  printf 'octoconv-api:latest sha256:old\npostgres:18 sha256:pg\n' > "$s/images_before"
  printf 'octoconv-api sha256:api1\noctoconv-worker sha256:w1\noctoconv-cad-worker sha256:cad1\n' > "$s/images_now"
  printf 'api octoconv-api sha256:api1\nworker octoconv-worker sha256:w1\npostgres postgres:18 sha256:pg\n' > "$s/inspect"
  printf '2026/10/16 tectonic-worker starting\n2026/10/16 \xf0\x9f\x94\x92 tectonic sandbox: landlock via /usr/local/bin/sandbox-exec --\n' > "$s/tect_log"
  printf 'PATH=/usr/bin\nTECTONIC_ENGINE_TIMEOUT=60s\n' > "$s/tect_env"
  printf '%s\n%s\n' "$U1" "$U2" > "$s/job_ids"
  echo 3.00 > "$s/load"
}

PASSES=0
FAILS=0
check() {
  local name=$1; shift
  if "$@"; then PASSES=$((PASSES + 1)); echo "PASS: $name"; else FAILS=$((FAILS + 1)); echo "FAIL: $name"; fi
}
has() { grep -qF -- "$2" "$1" 2>/dev/null; }
hasnt() { ! grep -qF -- "$2" "$1" 2>/dev/null; }
hasre() { grep -qE -- "$2" "$1" 2>/dev/null; }
count() { grep -cE -- "$2" "$1" 2>/dev/null || true; }

CL="^CLEANUP,step=cad,run=1,scope=[^,]+,clients=cad-liveproof-1760000000,other_clients_since=0,"
CE="invalid_ids=0,bucket_before=[^,]+,bucket_after=[^,]+,result="
LSCALL="docker compose exec -T minio mc ls --recursive --json local/octoconv"

N=0
run_wrapper() {
  N=$((N + 1))
  R="$T/case$N/repo"
  S="$T/case$N/state"
  E="$T/case$N/ev"
  mkrepo "$R"
  reset_state "$S"
  mkdir -p "$E"
  [ -z "${PREP:-}" ] || eval "$PREP"
  ( cd "$R" && env PATH="$STUB:$PATH" STUB_STATE="$S" "$@" bash scripts/final-live-gate.sh --evidence-dir "$E" ${STEPS_ARG:+--steps "$STEPS_ARG"} > "$T/case$N/console" 2>&1 < /dev/null )
  RC=$?
  F="$E/76-11-final-gate.txt"
}

run_wrapper_term() {
  local stop=$1 pid d
  shift
  N=$((N + 1))
  R="$T/case$N/repo"
  S="$T/case$N/state"
  E="$T/case$N/ev"
  mkrepo "$R"
  reset_state "$S"
  mkdir -p "$E" "$T/case$N/tmp"
  [ -z "${PREP:-}" ] || eval "$PREP"
  ( cd "$R" && exec env PATH="$STUB:$PATH" STUB_STATE="$S" TMPDIR="$T/case$N/tmp" STUB_TMPDIR="$T/case$N/tmp" "$@" bash scripts/final-live-gate.sh --evidence-dir "$E" > "$T/case$N/console" 2>&1 < /dev/null ) &
  pid=$!
  for _ in $(seq 1 100); do
    [ ! -e "$S/cad.running" ] || break
    sleep 0.2
  done
  if [ -n "$stop" ]; then
    d=$(find "$T/case$N/tmp" -mindepth 1 -maxdepth 1 -type d)
    printf '%s\n' "$stop" > "$d/stop"
  fi
  kill -TERM "$pid"
  wait "$pid"
  RC=$?
  F="$E/76-11-final-gate.txt"
}

cleanup_fail_checks() {
  local c=$1
  check "$c: CAD_RUN cleanup=fail" hasre "$F" '^CAD_RUN,run=1,rc=0,all_pass=1,.*,cleanup=fail,transcript=76-11-liveproof.txt,result=fail$'
  check "$c: cad FAIL" hasre "$F" '^STEP,name=cad,rc=1,result=FAIL,'
  check "$c: frontend not run after fail" hasre "$F" '^NOT_RUN,step=frontend,reason=after_fail$'
  check "$c: SUMMARY FAIL" hasre "$F" '^SUMMARY,result=FAIL,'
  check "$c: exit 1" test "$RC" = 1
  check "$c: never ALL PASS" hasnt "$F" "result=ALL PASS"
}

STEPS_ARG=preflight,frontend run_wrapper
check "smoke: exit 2" test "$RC" = 2
check "smoke: INCOMPLETE" hasre "$F" '^SUMMARY,result=INCOMPLETE,'
check "smoke: four NOT_RUN" test "$(count "$F" '^NOT_RUN,step=')" = 4
check "smoke: frontend PASS" hasre "$F" '^STEP,name=frontend,rc=0,result=PASS,'
check "smoke: preflight PASS" hasre "$F" '^STEP,name=preflight,rc=0,result=PASS,'
check "smoke: no ALL PASS" hasnt "$F" "SUMMARY,result=ALL PASS"
for k in date_utc commit tracked_dirt branch clone go server_arch load1 vm_free_gib; do
  check "smoke: HDR $k" hasre "$F" "^HDR,$k="
done
check "smoke: HDR go is go1.26.4" hasre "$F" '^HDR,go=go1.26.4$'
check "smoke: HDR tracked_dirt 0" hasre "$F" '^HDR,tracked_dirt=0$'
check "smoke: volumes before" hasre "$F" '^VOLUMES,phase=before,count=2,sha256=[0-9a-f]{64}$'
check "smoke: volumes equal" hasre "$F" '^VOLUMES,equal=1$'
check "smoke: npm ci test build" test "$(cat "$S/npm.log" | tr '\n' '|')" = "npm --prefix frontend ci|npm --prefix frontend test|npm --prefix frontend run build|"
check "smoke: override removed" test ! -e "$R/docker-compose.override.yml"
check "smoke: e2e ports file removed" test ! -e "$R/docker-compose.e2e-ports.yml"
check "smoke: frontend transcript" has "$E/76-11-frontend.txt" "Tests  12 passed (12)"
check "smoke: ci-local never ran" test ! -e "$S/ci-local.args"
STEPS_ARG=""

run_wrapper
HEADC=$(git -C "$R" rev-parse HEAD)
check "full: exit 0" test "$RC" = 0
check "full: ALL PASS" hasre "$F" "^SUMMARY,result=ALL PASS,commit=$HEADC,steps=6,pass=6,fail=0,not_run=0$"
for s in preflight ci-local tectonic sandbox cad frontend; do
  check "full: STEP $s PASS" hasre "$F" "^STEP,name=$s,rc=0,result=PASS,started_utc=[0-9T:Z-]+,ended_utc=[0-9T:Z-]+,transcript=."
done
check "full: no NOT_RUN" test "$(count "$F" '^NOT_RUN,')" = 0
check "full: CONVERT_TIME from the gate tier" hasre "$F" '^CONVERT_TIME,seconds=400.1,load1=3.00,load1_start=3.00,load1_end=3.00,budget_s=540,result=ok$'
check "full: TECTONIC" hasre "$F" '^TECTONIC,e2e_pass=TestMultiFileLaTeXProjectE2E;TestMultiFileRefusalsE2E,landlock=1,sandbox_disabled=0,image_id=sha256:tect1,result=ok$'
check "full: IMAGES e2e" hasre "$F" '^IMAGES,stack=e2e,api=sha256:api1;worker=sha256:w1$'
check "full: IMAGES_CHECK e2e" hasre "$F" '^IMAGES_CHECK,stack=e2e,built_services=2,expected=2,tag_mismatch=0,ids_present_before_run=0,result=ok$'
check "full: IMAGES cad" hasre "$F" '^IMAGES,stack=cad,api=sha256:api1;cad-worker=sha256:cad1$'
check "full: ci-local knobs" test "$(grep -c -E '^CI_LOCAL_E2E_(API_PORT=18090|EXTRA_COMPOSE_FILES=/.*/docker-compose.e2e-ports.yml|TEARDOWN=stop|GO_TEST_FLAGS=-v)$' "$S/ci-local.env")" = 4
check "full: ci-local toolchain" has "$S/ci-local.env" "GOTOOLCHAIN=go1.26.4"
check "full: ci-local evidence dir" has "$S/ci-local.env" "CI_LOCAL_EVIDENCE_DIR=$E"
check "full: ci-local tiers" test "$(cat "$S/ci-local.args")" = "helm gate race docker-build e2e"
check "full: sandbox classes" test "$(cat "$S/sandbox.args")" = "image pandoc ocr av audio html probe document cad"
check "full: cad api base" has "$S/cad.env" "CAD_LIVEPROOF_API_BASE=http://127.0.0.1:18090"
check "full: cad quiet gate" has "$S/cad.env" "CAD_LIVEPROOF_QUIET_LOAD=11"
check "full: cad toolchain" has "$S/cad.env" "GOTOOLCHAIN=go1.26.4"
check "full: CAD_RUN cleanup ok" hasre "$F" "^CAD_RUN,run=1,rc=0,all_pass=1,tracked_dirt=0,commit=$HEADC,commit_match=1,cleanup=ok,transcript=76-11-liveproof.txt,result=ok$"
check "full: QUIET clean" hasre "$F" '^QUIET,step=cad,run=1,rows=3,contaminated=0,bound=11'
check "full: cad cleanup line" hasre "$F" "^CLEANUP,step=cad,.*clients=cad-liveproof-1760000000,other_clients_since=0,jobs=2,objects_deleted=8,bytes_deleted=400,.*result=ok$"
check "full: sandbox cleanup line" hasre "$F" '^CLEANUP,step=sandbox,api_clients=0,minio_objects_deleted=0,leftover_tags=0,'
check "full: sandbox cleanup result ok" hasre "$F" '^CLEANUP,step=sandbox,api_clients=0,minio_objects_deleted=0,leftover_tags=0,reason=no_api_stack_and_no_clients,result=ok$'
check "full: QUIET result ok" hasre "$F" '^QUIET,step=cad,run=1,rows=3,contaminated=0,bound=11,contaminated_rows=,result=ok$'
check "full: mc rm only for the two jobs" test "$(grep -c ' mc rm ' "$S/docker.log")" = 4
check "full: mc rm job 1 uploads" has "$S/docker.log" "mc rm --recursive --force local/octoconv/uploads/$U1/"
check "full: mc rm job 2 results" has "$S/docker.log" "mc rm --recursive --force local/octoconv/results/$U2/"
check "full: mc rm targets exact" test "$(grep ' mc rm ' "$S/docker.log" | tr '\n' '|')" = "docker compose exec -T minio mc rm --recursive --force local/octoconv/uploads/$U1/|docker compose exec -T minio mc rm --recursive --force local/octoconv/results/$U1/|docker compose exec -T minio mc rm --recursive --force local/octoconv/uploads/$U2/|docker compose exec -T minio mc rm --recursive --force local/octoconv/results/$U2/|"
check "full: CLEANUP new fields" hasre "$F" "${CL}jobs=2,objects_deleted=8,bytes_deleted=400,jobs_without_upload=0,leftover_objects=0,${CE}ok$"
check "full: two bucket listings" test "$(grep -cxF "docker compose exec -T minio mc ls --recursive --json local/octoconv" "$S/docker.log")" = 2
check "full: no per-prefix disk usage" hasnt "$S/docker.log" "mc du --json"
check "full: bucket keeps two foreign lines" test "$(wc -l < "$S/bucket" | tr -d ' ')" = 2
check "full: foreign upload kept" has "$S/bucket" "\"key\":\"uploads/99999999-aaaa-bbbb-cccc-dddddddddddd/0-other.dxf\""
check "full: foreign near-prefix result kept" has "$S/bucket" "\"key\":\"results/${U1}0/0-near.png\""
for u in "$U1" "$U2"; do
  for p in uploads results; do
    check "full: no $p key left for $u" hasnt "$S/bucket" "\"key\":\"$p/$u/"
  done
done
check "full: compose stop" has "$S/docker.log" "docker compose -p octoconv stop"
check "full: no down" hasnt "$S/docker.log" " down"
check "full: no prune" hasnt "$S/docker.log" "prune"
check "full: no rmi" hasnt "$S/docker.log" "rmi"
check "full: sandbox transcript" has "$E/76-11-sandbox-liveproof.txt" "ALL PASS (40 checks)"
check "full: cad transcript" has "$E/76-11-liveproof.txt" "SUMMARY,result=ALL PASS"
check "full: tectonic log kept" has "$E/76-11-tectonic-worker.log" "landlock via"
check "full: watchdog sampled" hasre "$E/76-11-watchdog.txt" 'free_kib=21458684'
check "full: DISK line" hasre "$F" '^DISK,vm_free_min_kib=21458684,'
check "full: overrides removed" test ! -e "$R/docker-compose.override.yml"
check "full: last line SUMMARY" hasre <(tail -1 "$F") '^SUMMARY,'

run_wrapper STUB_SANDBOX_EXTRA='NOT RUN: document emulated-amd64-refuses (the docker server is amd64: nothing is emulated here)\n' STUB_SANDBOX_LAST='ALL PASS (39 checks, 1 not run)'
check "sandbox not run: exit 2" test "$RC" = 2
check "sandbox not run: INCOMPLETE" hasre "$F" '^SUMMARY,result=INCOMPLETE,.*not_run=1$'
check "sandbox not run: NOT_RUN line" hasre "$F" '^NOT_RUN,step=sandbox,reason=document_emulated-amd64-refuses'
check "sandbox not run: STEP NOT_RUN" hasre "$F" '^STEP,name=sandbox,rc=0,result=NOT_RUN,'
check "sandbox not run: cad still ran" hasre "$F" '^STEP,name=cad,rc=0,result=PASS,'
check "sandbox not run: never ALL PASS" hasnt "$F" "result=ALL PASS"

run_wrapper STUB_SANDBOX_LAST='FAILURES PRESENT (40 checks run)' STUB_SANDBOX_RC=1
check "sandbox fail: exit 1" test "$RC" = 1
check "sandbox fail: STEP FAIL" hasre "$F" '^STEP,name=sandbox,rc=1,result=FAIL,'
check "sandbox fail: cad not run after fail" hasre "$F" '^NOT_RUN,step=cad,reason=after_fail$'
check "sandbox fail: SUMMARY FAIL" hasre "$F" '^SUMMARY,result=FAIL,'

PREP='echo 3.00 > "$S/load"' run_wrapper STUB_CONVERT_S=600.5
check "d19 stop: exit 1" test "$RC" = 1
check "d19 stop: STOP line" hasre "$F" '^STOP,reason=convert_time_over_budget,seconds=600.5,load1=3.00,budget_s=540$'
check "d19 stop: CONVERT_TIME STOP" hasre "$F" '^CONVERT_TIME,seconds=600.5,.*result=STOP$'
check "d19 stop: ci-local FAIL" hasre "$F" '^STEP,name=ci-local,rc=1,result=FAIL,'
check "d19 stop: tectonic not run" hasre "$F" '^NOT_RUN,step=tectonic,reason=after_fail$'

PREP='echo 9.50 > "$S/load"' run_wrapper STUB_CONVERT_S=600.5
check "d19 loaded: exit 2" test "$RC" = 2
check "d19 loaded: contaminated" hasre "$F" '^CONVERT_TIME,seconds=600.5,load1=9.50,.*result=contaminated$'
check "d19 loaded: NOT_RUN ci-local" hasre "$F" '^NOT_RUN,step=ci-local,reason=convert_time_over_budget_at_load1_9.50$'
check "d19 loaded: no STOP" hasnt "$F" "STOP,"
check "d19 loaded: INCOMPLETE" hasre "$F" '^SUMMARY,result=INCOMPLETE,'

run_wrapper STUB_CILOCAL_RC=1
check "ci-local rc1: FAIL" hasre "$F" '^STEP,name=ci-local,rc=1,result=FAIL,'
check "ci-local rc1: exit 1" test "$RC" = 1

PREP='printf "starting\n" > "$S/tect_log"' run_wrapper
check "tectonic no landlock: FAIL" hasre "$F" '^STEP,name=tectonic,rc=1,result=FAIL,'
check "tectonic no landlock: line" hasre "$F" '^TECTONIC,.*landlock=0,.*result=fail$'

PREP='printf "landlock via x\nWARN TECTONIC_SANDBOX_DISABLED=true\n" | sed "s/^landlock/tectonic sandbox: landlock/" > "$S/tect_log"' run_wrapper
check "tectonic disabled log: FAIL" hasre "$F" '^STEP,name=tectonic,rc=1,result=FAIL,'

PREP='echo TECTONIC_SANDBOX_DISABLED=true >> "$S/tect_env"' run_wrapper
check "tectonic disabled env: FAIL" hasre "$F" '^STEP,name=tectonic,rc=1,result=FAIL,'

PREP='echo TECTONIC_SANDBOX_DISABLED= >> "$S/tect_env"' run_wrapper
check "tectonic empty env as compose forwards it: PASS" hasre "$F" '^STEP,name=tectonic,rc=0,result=PASS,'
check "tectonic empty env: TECTONIC ok" hasre "$F" '^TECTONIC,e2e_pass=TestMultiFileLaTeXProjectE2E;TestMultiFileRefusalsE2E,landlock=1,sandbox_disabled=0,image_id=sha256:tect1,result=ok$'
check "tectonic empty env: value recorded" hasre "$F" '^TECTONIC_DETAIL,.*disabled_env=0,disabled_env_value=empty,'

PREP='echo TECTONIC_SANDBOX_DISABLED=false >> "$S/tect_env"' run_wrapper
check "tectonic env false: PASS" hasre "$F" '^STEP,name=tectonic,rc=0,result=PASS,'
check "tectonic env false: value recorded" hasre "$F" '^TECTONIC_DETAIL,.*disabled_env=0,disabled_env_value=false,'

run_wrapper STUB_NO_TECT_PASS=1
check "tectonic missing PASS line: FAIL" hasre "$F" '^STEP,name=tectonic,rc=1,result=FAIL,'

run_wrapper STUB_E2E_FAIL_LINE=1
check "tectonic step sees a FAIL line" hasre "$F" '^STEP,name=tectonic,rc=1,result=FAIL,'

run_wrapper STUB_CAD_COMMIT=0000000000000000000000000000000000000000
check "cad other commit: FAIL" hasre "$F" '^STEP,name=cad,rc=1,result=FAIL,'
check "cad other commit: exit 1" test "$RC" = 1

PREP='printf "octoconv-api sha256:other\noctoconv-worker sha256:w1\noctoconv-cad-worker sha256:cad1\n" > "$S/images_now"' run_wrapper
check "e2e tag mismatch: ci-local FAIL" hasre "$F" '^IMAGES_CHECK,stack=e2e,.*tag_mismatch=1,.*result=fail$'

PREP='printf "worker octoconv-worker sha256:w1\n" > "$S/inspect"' run_wrapper
check "e2e missing service: FAIL" hasre "$F" '^IMAGES_CHECK,stack=e2e,built_services=1,expected=2,.*result=fail$'

PREP='printf "octoconv-api:latest sha256:api1\n" > "$S/images_before"' run_wrapper
check "ids present before: recorded" hasre "$F" '^IMAGES_CHECK,stack=e2e,.*ids_present_before_run=1,result=ok$'

run_wrapper STUB_CAD_ADD_VOLUME=1
check "volumes changed: equal 0" hasre "$F" '^VOLUMES,equal=0$'
check "volumes changed: FAIL" hasre "$F" '^SUMMARY,result=FAIL,'
check "volumes changed: exit 1" test "$RC" = 1

N=$((N + 1))
R="$T/case$N/repo"; S="$T/case$N/state"; E="$T/case$N/ev"
mkrepo "$R"; reset_state "$S"; mkdir -p "$E"
echo "SUMMARY,result=FAIL,old attempt" > "$E/76-11-final-gate.txt"
( cd "$R" && env PATH="$STUB:$PATH" STUB_STATE="$S" bash scripts/final-live-gate.sh --evidence-dir "$E" > "$T/case$N/console" 2>&1 < /dev/null )
RC=$?
check "existing transcript: exit 64" test "$RC" = 64
check "existing transcript: untouched" test "$(cat "$E/76-11-final-gate.txt")" = "SUMMARY,result=FAIL,old attempt"

N=$((N + 1))
R="$T/case$N/repo"; S="$T/case$N/state"; E="$T/case$N/ev"
mkrepo "$R"; reset_state "$S"; mkdir -p "$E"
( cd "$R" && env PATH="$STUB:$PATH" STUB_STATE="$S" bash scripts/final-live-gate.sh --evidence-dir "$E" --steps preflight,bogus > /dev/null 2>&1 < /dev/null )
check "bad steps: exit 64" test "$?" = 64
( cd "$R" && env PATH="$STUB:$PATH" STUB_STATE="$S" bash scripts/final-live-gate.sh > /dev/null 2>&1 < /dev/null )
check "no evidence dir: exit 64" test "$?" = 64

PREP='echo 18090 > "$S/taken"' run_wrapper
check "port taken: preflight FAIL" hasre "$F" '^STEP,name=preflight,rc=1,result=FAIL,'
check "port taken: holder named" has "$F" "holderproc"
check "port taken: exit 1" test "$RC" = 1
check "port taken: ci-local not run" hasre "$F" '^NOT_RUN,step=ci-local,reason=after_fail$'
check "port taken: no ci-local" test ! -e "$S/ci-local.args"

PREP='printf "keda   keda-operator-1   1/1   Running   0   1d\n" >> "$S/pods"' run_wrapper
check "keda pod: preflight FAIL" hasre "$F" '^STEP,name=preflight,rc=1,result=FAIL,'

PREP='echo c9 > "$S/running"' run_wrapper
check "compose running: preflight FAIL" hasre "$F" '^STEP,name=preflight,rc=1,result=FAIL,'

PREP='rm "$R/deploy/tectonic-bundle/octoconv-bundle.ttb"' run_wrapper
check "no bundle: preflight FAIL" hasre "$F" '^STEP,name=preflight,rc=1,result=FAIL,'

PREP='echo "keep me" > "$R/docker-compose.override.yml"' run_wrapper
check "pre-existing override: preflight FAIL" hasre "$F" '^STEP,name=preflight,rc=1,result=FAIL,'
check "pre-existing override: kept" test "$(cat "$R/docker-compose.override.yml" 2>/dev/null)" = "keep me"

PREP='echo 5000000 > "$S/free_kib"' run_wrapper
check "low disk at preflight: exit 3" test "$RC" = 3
check "low disk at preflight: STOP" hasre "$F" '^STOP,reason=vm_disk_below_floor,free_kib=5000000$'
check "low disk at preflight: no ci-local" test ! -e "$S/ci-local.args"

PREP='echo dirty >> "$R/frontend/package.json"' run_wrapper
check "tracked dirt: HDR 1" hasre "$F" '^HDR,tracked_dirt=1$'
check "tracked dirt: never ALL PASS" hasnt "$F" "result=ALL PASS"
check "tracked dirt: FAIL" hasre "$F" '^SUMMARY,result=FAIL,'

STEPS_ARG=preflight,frontend run_wrapper STUB_NPM_TOUCH_LOCK=1
check "lock changed: frontend FAIL" hasre "$F" '^STEP,name=frontend,rc=1,result=FAIL,'
STEPS_ARG=""

PREP='printf "%s\n\n../x\n" "$U1" > "$S/job_ids"' run_wrapper
check "bad job id: never rm outside a job prefix" test "$(grep -c ' mc rm ' "$S/docker.log")" = 2
check "bad job id: no bare prefix" hasnt "$S/docker.log" "local/octoconv/uploads//"
check "bad job id: cleanup fail recorded" hasre "$F" '^CLEANUP,step=cad,.*invalid_ids=1,.*result=fail$'
cleanup_fail_checks "bad job id"

for k in STUB_COMPOSE_START_FAIL=1 STUB_NO_MINIO_CREDS=1 STUB_PSQL_FAIL=clients STUB_PSQL_FAIL=others STUB_PSQL_FAIL=jobs STUB_MC_ALIAS_FAIL=1 STUB_MC_RM_FAIL=1; do
  run_wrapper "$k"
  check "cleanup $k: CLEANUP fail" hasre "$F" '^CLEANUP,step=cad,.*,result=fail$'
  cleanup_fail_checks "cleanup $k"
  case "$k" in
    STUB_PSQL_FAIL=jobs)
      check "cleanup $k: jobs 0" hasre "$F" '^CLEANUP,step=cad,.*,jobs=0,'
      check "cleanup $k: no mc rm" test "$(grep -c ' mc rm ' "$S/docker.log")" = 0 ;;
    STUB_MC_RM_FAIL=1)
      check "cleanup $k: objects_deleted 0" hasre "$F" '^CLEANUP,step=cad,.*,objects_deleted=0,' ;;
  esac
done

run_wrapper STUB_MC_LS_RC=124
check "ls timeout: CLEANUP fail unknown" hasre "$F" "${CL}jobs=0,objects_deleted=0,bytes_deleted=0,jobs_without_upload=unknown,leftover_objects=unknown,${CE}fail$"
check "ls timeout: no mc rm" test "$(grep -c ' mc rm ' "$S/docker.log")" = 0
check "ls timeout: one listing" test "$(grep -cxF "$LSCALL" "$S/docker.log")" = 1
cleanup_fail_checks "ls timeout"

run_wrapper STUB_MC_LS_RC=1 STUB_MC_LS_ON=2
check "ls after rm fails: CLEANUP fail leftover unknown" hasre "$F" "${CL}jobs=2,objects_deleted=8,bytes_deleted=400,jobs_without_upload=0,leftover_objects=unknown,${CE}fail$"
check "ls after rm fails: four mc rm" test "$(grep -c ' mc rm ' "$S/docker.log")" = 4
check "ls after rm fails: two listings" test "$(grep -cxF "$LSCALL" "$S/docker.log")" = 2
cleanup_fail_checks "ls after rm fails"

for k in STUB_MC_LS_TAIL=error STUB_MC_LS_TAIL=text; do
  run_wrapper "$k"
  check "ls $k: CLEANUP fail unknown" hasre "$F" "${CL}jobs=0,objects_deleted=0,bytes_deleted=0,jobs_without_upload=unknown,leftover_objects=unknown,${CE}fail$"
  check "ls $k: no mc rm" test "$(grep -c ' mc rm ' "$S/docker.log")" = 0
  cleanup_fail_checks "ls $k"
done

run_wrapper STUB_MC_RM_NOOP=1
check "rm leaves objects: CLEANUP leftover 8" hasre "$F" "${CL}jobs=2,objects_deleted=8,bytes_deleted=400,jobs_without_upload=0,leftover_objects=8,${CE}fail$"
check "rm leaves objects: four mc rm" test "$(grep -c ' mc rm ' "$S/docker.log")" = 4
cleanup_fail_checks "rm leaves objects"

run_wrapper STUB_MC_LS_KEYS=bucket
check "keys not bucket-relative: CLEANUP jobs_without_upload 2" hasre "$F" "${CL}jobs=2,objects_deleted=0,bytes_deleted=0,jobs_without_upload=2,leftover_objects=0,${CE}fail$"
check "keys not bucket-relative: no mc rm" test "$(grep -c ' mc rm ' "$S/docker.log")" = 0
cleanup_fail_checks "keys not bucket-relative"

run_wrapper STUB_CAD_SKIP_PREFIX="results/$U2/"
check "refused job: cad PASS" hasre "$F" '^STEP,name=cad,rc=0,result=PASS,'
check "refused job: ALL PASS" hasre "$F" '^SUMMARY,result=ALL PASS,'
check "refused job: exit 0" test "$RC" = 0
check "refused job: three mc rm" test "$(grep -c ' mc rm ' "$S/docker.log")" = 3
check "refused job: no rm of absent results" hasnt "$S/docker.log" "local/octoconv/results/$U2/"
check "refused job: uploads of job 2 deleted" has "$S/docker.log" "mc rm --recursive --force local/octoconv/uploads/$U2/"
check "refused job: CLEANUP ok" hasre "$F" "${CL}jobs=2,objects_deleted=6,bytes_deleted=300,jobs_without_upload=0,leftover_objects=0,${CE}ok$"

run_wrapper STUB_CAD_LOADS_1="5 5 5 5 5 12 5 5" STUB_CAD_LOADS_2="5 5 5 5 5 5 5 5"
check "quiet rerun: cad PASS" hasre "$F" '^STEP,name=cad,rc=0,result=PASS,'
check "quiet rerun: run 1 contaminated" hasre "$F" '^QUIET,step=cad,run=1,rows=3,contaminated=1,'
check "quiet rerun: coverage" hasre "$F" '^QUIET_COVERAGE,step=cad,rows=3,uncovered=0,result=ok$'
check "quiet rerun: rerun transcript" has "$E/76-11-liveproof-rerun.txt" "SUMMARY,result=ALL PASS"
check "quiet rerun: two cad runs" test "$(cat "$S/cad.count")" = 2
check "quiet rerun: two cleanups" test "$(count "$F" '^CLEANUP,step=cad,')" = 2
check "quiet rerun: both cleanups found the run objects" test "$(count "$F" "^CLEANUP,step=cad,run=[12],.*,objects_deleted=8,bytes_deleted=400,jobs_without_upload=0,leftover_objects=0,${CE}ok$")" = 2
check "quiet rerun: ALL PASS" hasre "$F" '^SUMMARY,result=ALL PASS,'
check "quiet rerun: both CAD_RUN cleanup ok" test "$(count "$F" '^CAD_RUN,.*,cleanup=ok,transcript=')" = 2

run_wrapper STUB_CAD_LOADS_1="5 5 5 5 5 12 5 5" STUB_CAD_LOADS_2="5 5 5 5 5 12 5 5"
check "quiet both: NOT_RUN" hasre "$F" '^NOT_RUN,step=cad,reason=quiet_host_uncovered_rows_1$'
check "quiet both: INCOMPLETE" hasre "$F" '^SUMMARY,result=INCOMPLETE,'

run_wrapper_term "" STUB_CAD_HANG=1 STUB_PSQL_FAIL=jobs
check "term in cad: CLEANUP fail" hasre "$F" '^CLEANUP,step=cad,run=1,.*,result=fail$'
check "term in cad: CAD_RUN cleanup=fail not_run" hasre "$F" '^CAD_RUN,run=1,rc=[0-9]+,cleanup=fail,transcript=76-11-liveproof.txt,result=not_run,reason=stopped_terminated$'
check "term in cad: STOP" hasre "$F" '^STOP,reason=terminated$'
check "term in cad: STEP NOT_RUN" hasre "$F" '^STEP,name=cad,rc=[0-9]+,result=NOT_RUN,'
check "term in cad: frontend NOT_RUN" hasre "$F" '^NOT_RUN,step=frontend,reason=stopped_terminated$'
check "term in cad: INCOMPLETE" hasre "$F" '^SUMMARY,result=INCOMPLETE,'
check "term in cad: exit 2" test "$RC" = 2

run_wrapper_term "docker_unresponsive,misses=3" STUB_CAD_HANG=1
check "docker unresponsive in cad: CAD_RUN cleanup=not_run" hasre "$F" '^CAD_RUN,run=1,rc=[0-9]+,cleanup=not_run,transcript=76-11-liveproof.txt,result=not_run,reason=stopped_docker_unresponsive$'
check "docker unresponsive in cad: no CLEANUP" test "$(count "$F" '^CLEANUP,step=cad,')" = 0
check "docker unresponsive in cad: no compose start" hasnt "$S/docker.log" "compose start postgres minio"
check "docker unresponsive in cad: STOP" hasre "$F" '^STOP,reason=docker_unresponsive,misses=3$'
check "docker unresponsive in cad: INCOMPLETE" hasre "$F" '^SUMMARY,result=INCOMPLETE,'
check "docker unresponsive in cad: exit 2" test "$RC" = 2

PREP='echo sha256:left1 > "$S/sandbox_left"' run_wrapper
check "sandbox leftover tag: CLEANUP fail" hasre "$F" '^CLEANUP,step=sandbox,api_clients=0,minio_objects_deleted=0,leftover_tags=1,reason=no_api_stack_and_no_clients,result=fail$'
check "sandbox leftover tag: STEP FAIL" hasre "$F" '^STEP,name=sandbox,rc=1,result=FAIL,'

run_wrapper STUB_SANDBOX_IMAGES_FAIL=1
check "sandbox listing fails: CLEANUP fail" hasre "$F" '^CLEANUP,step=sandbox,api_clients=0,minio_objects_deleted=0,leftover_tags=unknown,reason=no_api_stack_and_no_clients,result=fail$'
check "sandbox listing fails: STEP FAIL" hasre "$F" '^STEP,name=sandbox,rc=1,result=FAIL,'
check "sandbox listing fails: exit 1" test "$RC" = 1

run_wrapper STUB_TECT_ENV_FAIL=1
check "tectonic env unreadable: value recorded" hasre "$F" '^TECTONIC_DETAIL,.*disabled_env_value=unreadable,'
check "tectonic env unreadable: TECTONIC fail" hasre "$F" '^TECTONIC,.*result=fail$'
check "tectonic env unreadable: STEP FAIL" hasre "$F" '^STEP,name=tectonic,rc=1,result=FAIL,'
check "tectonic env unreadable: exit 1" test "$RC" = 1

run_wrapper STUB_PS_FAIL=1
check "docker ps fails: compose_running fail" hasre "$F" '^PREFLIGHT,check=compose_running,result=fail,docker_ps_rc=1$'
check "docker ps fails: preflight FAIL" hasre "$F" '^STEP,name=preflight,rc=1,result=FAIL,'
check "docker ps fails: exit 1" test "$RC" = 1

run_wrapper STUB_GIT_STATUS_FAIL=1
check "git status fails: HDR unknown" hasre "$F" '^HDR,tracked_dirt=unknown$'
check "git status fails: preflight tracked_dirt fail" hasre "$F" '^PREFLIGHT,check=tracked_dirt,result=fail,count=unknown$'
check "git status fails: DIRT end unknown" hasre "$F" '^DIRT,phase=end,tracked_dirt=unknown$'
check "git status fails: SUMMARY FAIL" hasre "$F" '^SUMMARY,result=FAIL,'
check "git status fails: exit 1" test "$RC" = 1

run_wrapper STUB_CAD_NO_LOAD=1
check "no load file: QUIET fail" hasre "$F" '^QUIET,step=cad,run=1,rows=0,contaminated=0,bound=11,contaminated_rows=,result=fail$'
check "no load file: CAD_RUN fail" hasre "$F" '^CAD_RUN,run=1,.*,result=fail$'
check "no load file: cad FAIL" hasre "$F" '^STEP,name=cad,rc=1,result=FAIL,'
check "no load file: exit 1" test "$RC" = 1

run_wrapper STUB_LOW_DISK_SLEEP=100
check "watchdog: exit 3" test "$RC" = 3
check "watchdog: STOP" hasre "$F" '^STOP,reason=vm_disk_below_floor,free_kib=1000$'
check "watchdog: ci-local NOT_RUN" hasre "$F" '^STEP,name=ci-local,rc=[0-9]+,result=NOT_RUN,'
check "watchdog: remaining NOT_RUN" hasre "$F" '^NOT_RUN,step=cad,reason=stopped_vm_disk$'
check "watchdog: summary" hasre "$F" '^SUMMARY,result=INCOMPLETE,'
check "watchdog: step killed" test -z "$(pgrep -f "sleep 100" 2>/dev/null)"
check "watchdog: overrides removed" test ! -e "$R/docker-compose.override.yml"

echo "RESULT: ${PASSES} passed, ${FAILS} failed"
[ "$FAILS" -eq 0 ]

#!/usr/bin/env bash
set -uo pipefail

REPO=$(cd "$(dirname "$0")/../../../.." && pwd)
SCRIPT="$REPO/scripts/keda-cad-loadproof.sh"
T=$(mktemp -d)
trap 'pkill -f "$T/" 2>/dev/null; rm -rf "$T"' EXIT
B="$T/bin"
mkdir -p "$B"
REAL_SLEEP=$(command -v sleep)

stub() { { printf '%s\n' '#!/usr/bin/env bash'; cat; } > "$B/$1"; chmod +x "$B/$1"; }

stub sleep <<EOF
echo "\$0 \$*" >> "\$STUB_STATE/calls.sleep"
exec "$REAL_SLEEP" 0.05
EOF

stub sysctl <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.sysctl"
[ "$*" = "-n vm.loadavg" ] || { echo "sysctl $*" >> "$STUB_STATE/unhandled"; exit 1; }
if [ -e "$STUB_STATE/hot_once" ]; then
  rm -f "$STUB_STATE/hot_once"
  echo '{ 12.00 9.00 9.00 }'
elif [ -e "$STUB_STATE/hot_on" ]; then
  echo '{ 12.00 9.00 9.00 }'
else
  echo '{ 4.00 4.00 4.00 }'
fi
EOF

stub orbctl <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.orbctl"
[ "$*" = "config get k8s.enable" ] || { echo "orbctl $*" >> "$STUB_STATE/unhandled"; exit 1; }
echo true
EOF

stub lsof <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.lsof"
exit 1
EOF

stub go <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.go"
case "$*" in
  "run ./cmd/manage-clients create "*) echo "api key: stubkey0123456789" ;;
  *) echo "go $*" >> "$STUB_STATE/unhandled"; exit 1 ;;
esac
EOF

stub helm <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.helm"
case "$*" in
  "search repo kedacore/keda --versions") printf 'NAME\tCHART VERSION\tAPP VERSION\tDESCRIPTION\nkedacore/keda\t2.20.1\t2.20.1\tKEDA\n' ;;
  "list -A -o json") echo '[]' ;;
esac
exit 0
EOF

stub docker <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.docker"
a="$*"
case "$a" in
  "ps -q --filter label=com.docker.compose.project=octoconv") ;;
  "ps --format {{.Names}}") ;;
  "run --rm alpine df -k /")
    echo "Filesystem 1K-blocks Used Available Use% Mounted on"
    echo "overlay 60000000 1000000 20000000 5% /" ;;
  "image inspect octoconv-"*":dev --format {{.Id}}")
    printf 'sha256:%s\n' "$(printf '%s' "$3" | shasum -a 256 | cut -c1-64)" ;;
  "run --name "*)
    prev=""
    for x in "$@"; do
      if [ "$prev" = -v ]; then
        case "$x" in *:/out) printf 'ISO-10303-21;\nstub\n' > "${x%:/out}/step-nauo-LinearSlide-MGN15x-Block-n1000.step" ;; esac
      fi
      prev=$x
    done ;;
  "rm -fv "*) ;;
  *) echo "docker $a" >> "$STUB_STATE/unhandled"; exit 1 ;;
esac
exit 0
EOF

stub curl <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.curl"
S=$STUB_STATE
put() { printf '%s\n' "$2" > "$S/.$1.$$" && mv -f "$S/.$1.$$" "$S/$1"; }
out="" method=GET url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) out=$2; shift 2 ;;
    -X) method=$2; shift 2 ;;
    -w | -H | -F | --max-time) shift 2 ;;
    -s) shift ;;
    *) url=$1; shift ;;
  esac
done
case "$method $url" in
  "GET "*/healthz)
    [ -z "$out" ] || echo '{}' > "$out"
    printf 200 ;;
  "POST "*/v1/jobs)
    n=$(( $(cat "$S/n" 2>/dev/null || echo 0) + 1 ))
    put n "$n"
    id=$(printf '00000000-0000-4000-8000-%012d' "$n")
    printf '{"job_id":"%s"}\n' "$id" > "$out"
    put last "$id"
    if [ "$n" = 1 ]; then put counts "1 1 1"; else put counts "2 2 2"; fi
    printf 202 ;;
  "GET "*/v1/jobs/*)
    id=${url##*/}
    j=$((10#${id##*-}))
    echo '{"status":"done"}'
    [ "$id" != "$(cat "$S/last" 2>/dev/null)" ] || put counts "0 0 0"
    if [ "${STUB_HOT:-}" = calibration ] && [ "$j" = 1 ]; then touch "$S/hot_once"; fi
    if [ "${STUB_HOT:-}" = timed ] && [ "$j" = 4 ]; then touch "$S/hot_on"; fi ;;
  *) echo "curl $method $url" >> "$S/unhandled"; exit 7 ;;
esac
EOF

stub kubectl <<'EOF'
echo "$0 $*" >> "$STUB_STATE/calls.kubectl"
S=$STUB_STATE
put() { printf '%s\n' "$2" > "$S/.$1.$$" && mv -f "$S/.$1.$$" "$S/$1"; }
now() { date -u +%Y-%m-%dT%H:%M:%SZ; }
counts() { cat "$S/counts" 2>/dev/null || echo "0 0 0"; }
cad_id() { printf '%s' octoconv-cad-worker:dev | shasum -a 256 | cut -c1-64; }
if [ "${1:-}" = -n ]; then shift 2; fi
a="$*"
case "$a" in
  "config current-context") echo orbstack ;;
  "get nodes") ;;
  "get nodes -o json") echo '{"items":[{"status":{"allocatable":{"cpu":"11","memory":"16Gi"}}}]}' ;;
  "get ns -o json") echo '{"items":[{"metadata":{"name":"default"}},{"metadata":{"name":"kube-system"}}]}' ;;
  "get ns "*) exit 1 ;;
  "get crd -o json") echo '{"items":[]}' ;;
  "get apiservice v1beta1.external.metrics.k8s.io") ;;
  "get apiservice v1beta1.external.metrics.k8s.io -o jsonpath="*Available*) printf True ;;
  "get pods -A -o json") echo '{"items":[{"metadata":{"namespace":"kube-system","name":"coredns-stub"},"spec":{"nodeName":"orbstack","containers":[{"resources":{"requests":{"cpu":"100m","memory":"128Mi"}}}]},"status":{"phase":"Running","containerStatuses":[{"restartCount":0}]}}]}' ;;
  "get pods -A --no-headers") ;;
  "get pods -o json") echo '{"items":[]}' ;;
  "get deployment "*" -o json")
    d=$3
    read -r sr st rr <<< "0 0 0"
    [ "$d" != cad-worker ] || read -r sr st rr <<< "$(counts)"
    printf '{"spec":{"replicas":%s,"selector":{"matchLabels":{"app.kubernetes.io/component":"%s"}},"template":{"spec":{"containers":[{"resources":{"requests":{"cpu":"2","memory":"4Gi"},"limits":{"cpu":"2","memory":"4Gi"}}}]}}},"status":{"replicas":%s,"readyReplicas":%s}}\n' "$sr" "$d" "$st" "$rr" ;;
  "get deployment "*) ;;
  "get pod -l app.kubernetes.io/component=redis -o jsonpath="*) printf redis-0 ;;
  "exec redis-0 -- redis-cli SADD asynq:queues "*)
    shift 6
    put members "$( { cat "$S/members" 2>/dev/null; printf '%s\n' "$@"; } | LC_ALL=C sort -u)" ;;
  "exec redis-0 -- redis-cli SMEMBERS asynq:queues") cat "$S/members" 2>/dev/null ;;
  "get scaledobject -o json") echo '{"items":[{"spec":{"scaleTargetRef":{"name":"cad-worker"}}}]}' ;;
  "get scaledobject worker-cad-scaledobject -o json") echo '{"status":{"conditions":[{"type":"Fallback","status":"False"}],"externalMetricNames":["s0-redis-cad"]}}' ;;
  "get --raw /apis/external.metrics.k8s.io/"*) echo '{"items":[{"value":"0"}]}' ;;
  "get pod -l app.kubernetes.io/component=cad-worker -o json")
    read -r _ st _ <<< "$(counts)"
    t=$(now)
    items=""
    i=1
    while [ "$i" -le "$st" ]; do
      items="$items${items:+,}{\"metadata\":{\"name\":\"cad-worker-stub-$i\",\"creationTimestamp\":\"$t\"},\"status\":{\"conditions\":[{\"type\":\"Ready\",\"status\":\"True\",\"lastTransitionTime\":\"$t\"}]}}"
      i=$((i + 1))
    done
    printf '{"items":[%s]}\n' "$items" ;;
  "get pod "*" -o json") printf '{"status":{"conditions":[{"type":"Ready","status":"True","lastTransitionTime":"%s"}]}}\n' "$(now)" ;;
  "get pod "*" -o jsonpath={.status.containerStatuses[0].imageID}") printf 'docker://sha256:%s' "$(cad_id)" ;;
  "logs "*) echo "level=INFO msg=\"cad sandbox proven\"" ;;
  "get events --field-selector "*" -o json") printf '{"items":[{"firstTimestamp":"%s","message":"Container image already present on machine"}]}\n' "$(now)" ;;
  "exec deploy/postgres -- psql "*)
    sql=${*: -1}
    case "$sql" in
      "select attempts "*) echo 1 ;;
      "select count(*) "*) echo 1 ;;
      "select extract(epoch from finished_at) "*) perl -MTime::HiRes=time -e 'printf "%.3f\n", time' ;;
      "select round("*) echo 12.3 ;;
      *) echo "psql $sql" >> "$S/unhandled"; exit 1 ;;
    esac ;;
  "scale "* | "wait "* | "port-forward "* | "describe "* | "delete "* | "get hpa"*) ;;
  *) echo "kubectl $a" >> "$S/unhandled"; exit 1 ;;
esac
exit 0
EOF

PASSES=0
FAILS=0
check() {
  local name=$1; shift
  if "$@"; then PASSES=$((PASSES + 1)); echo "PASS: $name"; else FAILS=$((FAILS + 1)); echo "FAIL: $name"; fi
}
has() { grep -qF -- "$2" "$1" 2>/dev/null; }
hasnt() { [ -f "$1" ] && ! grep -qF -- "$2" "$1"; }
hasre() { grep -qE -- "$2" "$1" 2>/dev/null; }
nore() { [ -f "$1" ] && ! grep -qE -- "$2" "$1"; }

RESOLVED=1
for tool in kubectl helm docker orbctl curl go lsof sysctl sleep; do
  p=$(PATH="$B:$PATH" command -v "$tool")
  if [ "$p" = "$B/$tool" ]; then echo "STUB: $tool -> $p"; else echo "ABORT: $tool resolves to ${p:-nothing}, not $B/$tool"; RESOLVED=0; fi
done
[ "$RESOLVED" = 1 ] || { echo "RESULT: 0 passed, 1 failed"; exit 1; }

N=0
run_case() {
  N=$((N + 1))
  C="$T/case$N"
  S="$C/state"
  mkdir -p "$S" "$C/ev" "$C/tmp"
  env PATH="$B:$PATH" STUB_STATE="$S" TMPDIR="$C/tmp" "$@" bash "$SCRIPT" --evidence-dir "$C/ev" > "$C/console" 2>&1 < /dev/null
  RC=$?
  F="$C/ev/76-09-keda-loadproof.txt"
}

run_case
check "clean: evidence written" test -s "$F"
check "clean: exit 0" test "$RC" = 0
check "clean: QUIET calibration contaminated=0" hasre "$F" '^QUIET,step=calibration,load1_start=4\.00,load1_end=4\.00,.*,contaminated=0$'
check "clean: QUIET timed contaminated=0" hasre "$F" '^QUIET,step=timed,load1_start=4\.00,load1_end=4\.00,.*,contaminated=0$'
check "clean: CALIBRATION ok" hasre "$F" '^CALIBRATION,.*,result=ok$'
check "clean: DRAIN ok" hasre "$F" '^DRAIN,.*,result=ok$'
check "clean: SUMMARY PASS" hasre "$F" '^SUMMARY,mode=keda_cad_loadproof,.*,result=PASS$'
check "clean: no FAIL line" nore "$F" '^FAIL,'
check "clean: RESTORE ok" hasre "$F" '^RESTORE,.*,result=ok$'
check "clean: kubectl stub answered" test -s "$S/calls.kubectl"
check "clean: helm stub answered" test -s "$S/calls.helm"
check "clean: docker stub answered" test -s "$S/calls.docker"
check "clean: curl stub answered" test -s "$S/calls.curl"
check "clean: sysctl stub answered" test -s "$S/calls.sysctl"
check "clean: pod list follows the replica count" nore "$F" '^POD_EVENTS,pod=cad-worker-stub-0,'
check "clean: no unhandled stub call" test ! -s "$S/unhandled"

run_case STUB_HOT=calibration
check "hot calibration: evidence written" test -s "$F"
check "hot calibration: QUIET calibration contaminated=1" hasre "$F" '^QUIET,step=calibration,load1_start=4\.00,load1_end=12\.00,.*,contaminated=1$'
check "hot calibration: FAIL names the contamination" hasre "$F" '^FAIL,step=calibration,reason=.*contaminated'
check "hot calibration: no CALIBRATION line" nore "$F" '^CALIBRATION,'
check "hot calibration: no TIMED_SUBMIT line" nore "$F" '^TIMED_SUBMIT,'
check "hot calibration: SUMMARY FAIL" hasre "$F" '^SUMMARY,mode=keda_cad_loadproof,.*,result=FAIL$'
check "hot calibration: never PASS" nore "$F" 'result=PASS'
check "hot calibration: exit 1" test "$RC" = 1
check "hot calibration: no unhandled stub call" test ! -s "$S/unhandled"

run_case STUB_HOT=timed
check "hot timed: evidence written" test -s "$F"
check "hot timed: QUIET calibration contaminated=0" hasre "$F" '^QUIET,step=calibration,.*,contaminated=0$'
check "hot timed: QUIET timed contaminated=1" hasre "$F" '^QUIET,step=timed,load1_start=4\.00,load1_end=12\.00,load1_max_sampled=12,.*,contaminated=1$'
check "hot timed: FAIL names the contamination" hasre "$F" '^FAIL,step=timed.*contaminated'
check "hot timed: no DRAIN line" nore "$F" '^DRAIN,'
check "hot timed: SUMMARY FAIL" hasre "$F" '^SUMMARY,mode=keda_cad_loadproof,.*,result=FAIL$'
check "hot timed: never PASS" nore "$F" 'result=PASS'
check "hot timed: exit 1" test "$RC" = 1
check "hot timed: no unhandled stub call" test ! -s "$S/unhandled"

echo "RESULT: ${PASSES} passed, ${FAILS} failed"
[ "$FAILS" -eq 0 ]

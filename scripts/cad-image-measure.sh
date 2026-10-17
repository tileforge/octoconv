#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/cad-measure/common.sh"

IMAGE=""
OUT=""
REPEATS=3
PREFLIGHTS=3
PORT=5055
QUIET_LOAD=11
MAX_ATTEMPTS=6
QUIET_WAIT_S=3600
FLOOR_KIB=6291456
DERIVED="$CAD_REPO_ROOT/.planning/phases/76-measured-bounds-keda-proof-client-docs/evidence/76-03-derived.txt"

usage() {
  echo "usage: $0 --image REF --out FILE [--repeats 3] [--preflights 3] [--port 5055] [--quiet-load 11]" >&2
  exit 64
}

while [ $# -gt 0 ]; do
  case "$1" in
    --image) [ $# -ge 2 ] || usage; IMAGE=$2; shift 2 ;;
    --out) [ $# -ge 2 ] || usage; OUT=$2; shift 2 ;;
    --repeats) [ $# -ge 2 ] || usage; REPEATS=$2; shift 2 ;;
    --preflights) [ $# -ge 2 ] || usage; PREFLIGHTS=$2; shift 2 ;;
    --port) [ $# -ge 2 ] || usage; PORT=$2; shift 2 ;;
    --quiet-load) [ $# -ge 2 ] || usage; QUIET_LOAD=$2; shift 2 ;;
    *) usage ;;
  esac
done
[ -n "$IMAGE" ] || usage
[ -n "$OUT" ] || usage
for n in "$REPEATS" "$PREFLIGHTS" "$PORT" "$QUIET_LOAD"; do
  case "$n" in ''|*[!0-9]*) usage ;; esac
done
[ "$REPEATS" -ge 1 ] || usage
[ "$PREFLIGHTS" -ge 1 ] || usage

command -v docker >/dev/null || { echo "error: docker is required" >&2; exit 69; }
for tool in jq curl perl awk lsof git; do
  command -v "$tool" >/dev/null || { echo "error: $tool is required" >&2; exit 69; }
done

kt() {
  local n=$1
  shift
  perl -e '
    my $n = shift @ARGV;
    my $pid = fork();
    die "fork: $!" unless defined $pid;
    if ($pid == 0) { exec @ARGV or exit 127; }
    local $SIG{ALRM} = sub { kill "KILL", $pid; waitpid($pid, 0); print STDERR "timeout after ${n}s: @ARGV\n"; exit 124; };
    alarm $n;
    waitpid($pid, 0);
    alarm 0;
    exit($? >> 8);
  ' "$n" "$@"
}

NET=octoconv-cad76-pull
REG=octoconv-cad76-registry
DIND=octoconv-cad76-dind
REPO=octoconv-cad76-measure
REG_IN_NET="$REG:5000"
LOCAL_REG="localhost:$PORT"
PREFLIGHT_PREFIX="octoconv-cad-preflight-$$"

WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-image-measure.XXXXXX")
TMP_OUT="$WORK/evidence.txt"
: > "$TMP_OUT"
TAGGED=""
CREATED_NET=0
CREATED_REG=0
CURRENT_PREFLIGHT=""
CLEAN=0
OK=0

emit() { printf '%s\n' "$*" | tee -a "$TMP_OUT"; }

die() { echo "error: $*" >&2; exit 1; }

cleanup() {
  local rc=$? t
  if [ -n "$CURRENT_PREFLIGHT" ]; then kt 60 docker rm -fv "$CURRENT_PREFLIGHT" >/dev/null 2>&1 || true; fi
  kt 120 docker rm -fv "$DIND" >/dev/null 2>&1 || true
  if [ "$CREATED_REG" = 1 ]; then kt 120 docker rm -fv "$REG" >/dev/null 2>&1 || true; fi
  if [ "$CREATED_NET" = 1 ]; then kt 60 docker network rm "$NET" >/dev/null 2>&1 || true; fi
  for t in $TAGGED; do kt 60 docker rmi "$t" >/dev/null 2>&1 || true; done
  if [ "$OK" != 1 ] && [ -s "$TMP_OUT" ]; then
    mkdir -p "$(dirname "$OUT")"
    cp "$TMP_OUT" "${OUT%.txt}-failed.txt"
    echo "partial evidence: ${OUT%.txt}-failed.txt" >&2
  fi
  rm -rf "$WORK"
  exit "$rc"
}
trap cleanup EXIT

now() { perl -MTime::HiRes=time -e 'printf "%.3f\n", time'; }

ts_epoch() {
  perl -MTime::Local=timegm -e '
    my $s = shift;
    $s =~ /^(\d{4})-(\d\d)-(\d\d)T(\d\d):(\d\d):(\d\d)(\.\d+)?Z$/ or die "bad timestamp $s\n";
    printf "%.3f\n", timegm($6, $5, $4, $3, $2 - 1, $1) + ($7 // 0);
  ' "$1"
}

load1() {
  if [ -r /proc/loadavg ]; then
    awk '{ print $1 }' /proc/loadavg
  else
    sysctl -n vm.loadavg | awk '{ print $2 }'
  fi
}

vm_free_kib() { kt 120 docker run --rm alpine df -k / | awk 'NR == 2 { print $4 }'; }

gib() { awk -v k="$1" 'BEGIN { printf "%.2f", k / 1048576 }'; }

mib() { awk -v b="$1" 'BEGIN { printf "%.1f", b / 1048576 }'; }

over() { awk -v l="$1" -v q="$QUIET_LOAD" 'BEGIN { exit !(l > q) }'; }

require_floor() {
  local k
  k=$(vm_free_kib)
  case "$k" in ''|*[!0-9]*) echo "error: cannot read the VM free disk" >&2; exit 3 ;; esac
  if [ "$k" -lt "$FLOOR_KIB" ]; then
    echo "error: VM free disk $(gib "$k") GiB is below the 6 GiB floor" >&2
    exit 3
  fi
}

wait_quiet() {
  local waited=0 l
  while :; do
    l=$(load1)
    if ! over "$l"; then echo "$l"; return 0; fi
    [ "$waited" -lt "$QUIET_WAIT_S" ] || die "load1 $l stayed above $QUIET_LOAD for ${QUIET_WAIT_S}s"
    sleep 15
    waited=$((waited + 15))
  done
}

index_digest() {
  kt 120 docker buildx imagetools inspect "$1" 2>/dev/null | awk '/^Digest:/ { print $2; exit }'
}

kt 30 docker image inspect "$IMAGE" >/dev/null 2>&1 || die "missing image $IMAGE"
cad_require_native "$IMAGE" || exit 1
[ -f "$DERIVED" ] || die "missing $DERIVED"
ENGINE=$(awk -F, '/^DERIVED,key=cad_engine_timeout_s,/ { for (i = 2; i <= NF; i++) if ($i ~ /^value=/) { sub(/^value=/, "", $i); print $i } }' "$DERIVED")
case "$ENGINE" in ''|*[!0-9]*) die "no single DERIVED cad_engine_timeout_s in $DERIVED" ;; esac

require_floor
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then die "port $PORT is in use"; fi
if [ -n "$(kt 30 docker ps -q --filter label=com.docker.compose.project=octoconv)" ]; then
  die "an octoconv compose container is running; run docker compose -p octoconv stop first"
fi
if command -v kubectl >/dev/null; then
  K8S_PODS=$(kt 60 kubectl get pods -A --no-headers 2>/dev/null | awk '$1 == "octoconv"' | wc -l | tr -d ' ')
  [ "$K8S_PODS" = 0 ] || die "$K8S_PODS octoconv pods run in k8s"
fi
for name in "$REG" "$DIND"; do
  if [ -n "$(kt 30 docker ps -aq --filter "name=^${name}\$")" ]; then die "container $name already exists"; fi
done
if kt 30 docker network inspect "$NET" >/dev/null 2>&1; then die "network $NET already exists"; fi
if kt 30 docker image inspect "$LOCAL_REG/$REPO:prod" >/dev/null 2>&1; then die "tag $LOCAL_REG/$REPO:prod already exists"; fi

REG_DIGEST=$(index_digest registry:2)
DIND_DIGEST=$(index_digest docker:dind)
case "$REG_DIGEST" in sha256:????????????????????????????????????????????????????????????????) ;; *) die "cannot resolve the registry:2 digest" ;; esac
case "$DIND_DIGEST" in sha256:????????????????????????????????????????????????????????????????) ;; *) die "cannot resolve the docker:dind digest" ;; esac
REG_IMAGE="registry@$REG_DIGEST"
DIND_IMAGE="docker@$DIND_DIGEST"

COMMIT=$(git -C "$CAD_REPO_ROOT" rev-parse HEAD)
DIRT=$(git -C "$CAD_REPO_ROOT" status --porcelain --untracked-files=no | wc -l | tr -d ' ')
IMAGE_ID=$(kt 30 docker image inspect "$IMAGE" --format '{{.Id}}')

kt 300 bash -c ". '$SCRIPT_DIR/cad-measure/common.sh'; cad_evidence_header '$IMAGE' 2 4g" | tee -a "$TMP_OUT"
emit "HDR,commit=$COMMIT"
emit "HDR,tracked_dirt=$DIRT"
emit "HDR,load1=$(load1)"
emit "HDR,vm_free_gib=$(gib "$(vm_free_kib)")"
emit "HDR,registry_digest=$REG_DIGEST"
emit "HDR,dind_digest=$DIND_DIGEST"
emit "HDR,script=scripts/cad-image-measure.sh,repeats=$REPEATS,preflights=$PREFLIGHTS,port=$PORT,quiet_load=$QUIET_LOAD,engine_timeout_source=evidence/76-03-derived.txt"
emit "RULE,key=cold_start,rule=cold_start_s = COLD_PULL_DIST.wall_max + PULL_CALC(100).transfer_s + PREFLIGHT_DIST.max"
emit "RULE,key=premise,rule=premise_holds = cold_start_s <= 0.1 x cad_engine_timeout_s ($ENGINE from evidence/76-03-derived.txt)"
emit "RULE,key=decision,rule=value = 0 per D-09; a failed premise is reported and not acted on"
emit "RULE,key=quiet_host,rule=a timed row counts only when load1 <= $QUIET_LOAD at its start and its end; a contaminated row is recorded as CONTAMINATED and repeated"

kt 60 docker network create "$NET" >/dev/null
CREATED_NET=1
kt 300 docker run -d --name "$REG" --network "$NET" -p "127.0.0.1:$PORT:5000" "$REG_IMAGE" >/dev/null
CREATED_REG=1
ready=0
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null "http://127.0.0.1:$PORT/v2/" 2>/dev/null; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || die "the registry did not come up"
emit "REGISTRY,image=$REG_IMAGE,port=$PORT"

DST="$LOCAL_REG/$REPO:prod"
kt 60 docker tag "$IMAGE" "$DST"
TAGGED="$DST"
kt 900 docker push "$DST" >&2

MF="$WORK/manifest.json"
curl -fsS \
  -H 'Accept: application/vnd.oci.image.manifest.v1+json' \
  -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
  "http://127.0.0.1:$PORT/v2/$REPO/manifests/prod" > "$MF"
NLAYERS=$(jq '.layers | length' "$MF")
case "$NLAYERS" in ''|null|0|*[!0-9]*) die "the manifest has no layers" ;; esac
COMPRESSED=$(jq '[.layers[].size] | add' "$MF")
UNCOMPRESSED=$(kt 30 docker image inspect "$IMAGE" --format '{{.Size}}')
emit "IMAGE_SIZE,image_id=$IMAGE_ID,uncompressed_bytes=$UNCOMPRESSED,compressed_bytes=$COMPRESSED,uncompressed_mib=$(mib "$UNCOMPRESSED"),compressed_mib=$(mib "$COMPRESSED"),layers=$NLAYERS"
jq -r '.layers[] | "\(.digest) \(.size)"' "$MF" | while read -r dg sz; do
  emit "LAYER,digest=$dg,compressed_bytes=$sz"
done

start_dind() {
  local ready=0
  kt 300 docker run -d --name "$DIND" --privileged --network "$NET" -v /var/lib/docker \
    --entrypoint dockerd "$DIND_IMAGE" \
    --host=unix:///var/run/docker.sock --insecure-registry "$REG_IN_NET" >/dev/null
  for _ in $(seq 1 60); do
    if kt 30 docker exec "$DIND" docker info >/dev/null 2>&1; then ready=1; break; fi
    sleep 2
  done
  [ "$ready" = 1 ] || die "dind did not come up"
}

stop_dind() { kt 120 docker rm -fv "$DIND" >/dev/null; }

DIND_NOTED=0
note_dind() {
  local driver
  driver=$(kt 30 docker exec "$DIND" docker info --format '{{.Driver}}')
  emit "DIND,image=$DIND_IMAGE,network=$NET,privileged=1,volume=anonymous,fresh_daemon_per_pull=1,storage_driver=$driver,docker=$(kt 30 docker exec "$DIND" docker version --format '{{.Server.Version}}')"
  [ "$driver" != vfs ] || emit "DIND,note=vfs storage driver: pull time is not representative"
}

pull_once() {
  local rep=$1 attempt=$2 log="$WORK/pull-$1-$2.log" t0 t1 rc=0 wall ids fetched exists l0 l1
  require_floor
  start_dind
  if [ "$DIND_NOTED" = 0 ]; then note_dind; DIND_NOTED=1; fi
  [ -z "$(kt 30 docker exec "$DIND" docker images -aq)" ] || die "the dind daemon is not empty before pull $rep"
  l0=$(wait_quiet)
  t0=$(now)
  kt 900 docker exec "$DIND" docker pull "$REG_IN_NET/$REPO:prod" > "$log" 2>&1 || rc=$?
  t1=$(now)
  l1=$(load1)
  [ "$rc" = 0 ] || { cat "$log" >&2; die "pull $rep failed rc=$rc"; }
  wall=$(awk -v a="$t0" -v b="$t1" 'BEGIN { printf "%.2f", b - a }')
  ids=$(grep -Eo '^[0-9a-f]{12}: (Pulling fs layer|Already exists|Pull complete)' "$log" | cut -d: -f1 | sort -u | wc -l | tr -d ' ')
  fetched=$(grep -Eo '^[0-9a-f]{12}: Pull complete' "$log" | cut -d: -f1 | sort -u | wc -l | tr -d ' ')
  exists=$(grep -Ec '^[0-9a-f]{12}: Already exists' "$log" || true)
  stop_dind
  if [ "$exists" != 0 ]; then cat "$log" >&2; die "a layer was already present in the daemon: not a cold pull ($rep)"; fi
  if [ "$ids" = 0 ] || [ "$fetched" != "$ids" ] || [ "$ids" != "$NLAYERS" ]; then
    cat "$log" >&2
    die "pull $rep: fetched=$fetched layers=$ids manifest_layers=$NLAYERS"
  fi
  if over "$l0" || over "$l1"; then
    emit "CONTAMINATED,row=COLD_PULL,rep=$rep,attempt=$attempt,wall_s=$wall,load1_start=$l0,load1_end=$l1"
    return 0
  fi
  CLEAN=1
  emit "COLD_PULL,rep=$rep,wall_s=$wall,layers=$ids,fetched=$fetched,already_exists=0,attempt=$attempt,load1_start=$l0,load1_end=$l1,medium=loopback"
  echo "$wall" >> "$WORK/pull-walls"
}

preflight_once() {
  local rep=$1 attempt=$2 name="$PREFLIGHT_PREFIX-$1-$2" logs="$WORK/preflight-$1-$2.log" l0 l1 status started landlock proven pg s0 s1 s2
  require_floor
  l0=$(wait_quiet)
  CURRENT_PREFLIGHT=$name
  kt 120 docker run -d --name "$name" --network none --cpus 2 --memory 4g --memory-swap 4g \
    -e DATABASE_URL=postgres://gate:gate@127.0.0.1:1/gate "$IMAGE" >/dev/null
  status=$(kt 300 docker wait "$name") || die "preflight $rep did not exit within 300 s"
  l1=$(load1)
  started=$(kt 30 docker inspect -f '{{.State.StartedAt}}' "$name")
  kt 60 docker logs -t "$name" > "$logs" 2>&1
  kt 60 docker rm -fv "$name" >/dev/null
  CURRENT_PREFLIGHT=""
  [ "$status" != 0 ] || { cat "$logs" >&2; die "preflight $rep exited 0, want non-zero"; }
  if grep -qF 'cad sandbox preflight:' "$logs"; then cat "$logs" >&2; die "preflight $rep: the sandbox preflight failed"; fi
  landlock=$(grep -F '🔒 cad sandbox: landlock via /usr/local/bin/cad-worker __sandbox-exec' "$logs" | head -n 1 | awk '{ print $1 }')
  proven=$(grep -F '🔒 cad sandbox proven:' "$logs" | head -n 1 | awk '{ print $1 }')
  pg=$(grep -F 'postgres:' "$logs" | head -n 1 | awk '{ print $1 }')
  [ -n "$landlock" ] || { cat "$logs" >&2; die "preflight $rep: no landlock line"; }
  [ -n "$proven" ] || { cat "$logs" >&2; die "preflight $rep: no sandbox proven line"; }
  [ -n "$pg" ] || { cat "$logs" >&2; die "preflight $rep: never reached the postgres attempt"; }
  s0=$(ts_epoch "$started")
  s1=$(ts_epoch "$landlock")
  s2=$(ts_epoch "$proven")
  awk -v a="$s0" -v b="$s1" -v c="$s2" 'BEGIN { exit !(a <= b && b <= c) }' || die "preflight $rep: timestamps out of order ($started $landlock $proven)"
  if over "$l0" || over "$l1"; then
    emit "CONTAMINATED,row=PREFLIGHT,rep=$rep,attempt=$attempt,load1_start=$l0,load1_end=$l1"
    return 0
  fi
  CLEAN=1
  emit "PREFLIGHT,rep=$rep,started_at=$started,landlock_at=$landlock,proven_at=$proven,preflight_s=$(awk -v a="$s0" -v c="$s2" 'BEGIN { printf "%.2f", c - a }'),postgres_attempt=1,exit=$status,attempt=$attempt,load1_start=$l0,load1_end=$l1"
  awk -v a="$s0" -v c="$s2" 'BEGIN { printf "%.2f\n", c - a }' >> "$WORK/preflight-walls"
}

repeat_clean() {
  local fn=$1 rep=$2 attempt=1
  while [ "$attempt" -le "$MAX_ATTEMPTS" ]; do
    CLEAN=0
    "$fn" "$rep" "$attempt"
    [ "$CLEAN" = 0 ] || return 0
    attempt=$((attempt + 1))
  done
  die "$fn rep $rep had no clean attempt in $MAX_ATTEMPTS"
}

rank() {
  sort -n "$1" | awk -v p="$2" '{ a[NR] = $1 } END { r = int((NR * p + 99) / 100); if (r < 1) r = 1; print a[r] }'
}

: > "$WORK/pull-walls"
for rep in $(seq 1 "$REPEATS"); do
  repeat_clean pull_once "$rep"
done
PULL_MAX=$(sort -n "$WORK/pull-walls" | tail -n 1)
emit "COLD_PULL_DIST,n=$(wc -l < "$WORK/pull-walls" | tr -d ' '),wall_min=$(sort -n "$WORK/pull-walls" | head -n 1),wall_median=$(rank "$WORK/pull-walls" 50),wall_max=$PULL_MAX"

T100=""
for bw in 100 1000 10000; do
  t=$(awk -v b="$COMPRESSED" -v w="$bw" 'BEGIN { printf "%.1f", b * 8 / (w * 1000000) }')
  [ "$bw" != 100 ] || T100=$t
  emit "PULL_CALC,bandwidth_mbit=$bw,transfer_s=$t,compressed_bytes=$COMPRESSED,label=calculated,note=compressed bytes x 8 / bandwidth; node time is about transfer_s plus the unpack time of COLD_PULL"
done

: > "$WORK/preflight-walls"
for rep in $(seq 1 "$PREFLIGHTS"); do
  repeat_clean preflight_once "$rep"
done
PRE_MAX=$(sort -n "$WORK/preflight-walls" | tail -n 1)
emit "PREFLIGHT_DIST,n=$(wc -l < "$WORK/preflight-walls" | tr -d ' '),min=$(sort -n "$WORK/preflight-walls" | head -n 1),median=$(rank "$WORK/preflight-walls" 50),max=$PRE_MAX"

COLD=$(awk -v a="$PULL_MAX" -v b="$T100" -v c="$PRE_MAX" 'BEGIN { printf "%.2f", a + b + c }')
LIMIT=$(awk -v e="$ENGINE" 'BEGIN { printf "%.1f", e / 10 }')
HOLDS=$(awk -v c="$COLD" -v l="$LIMIT" 'BEGIN { print (c <= l) ? 1 : 0 }')
emit "DECISION,key=keda_cad_min_replica_count,value=0,source=D-09,cold_start_s=$COLD,engine_timeout_s=$ENGINE,premise_limit_s=$LIMIT,premise_holds=$HOLDS"
emit "HDR,end_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ),end_load1=$(load1),end_vm_free_gib=$(gib "$(vm_free_kib)")"
emit "SUMMARY,mode=image_measure,result=ok,premise_holds=$HOLDS"

mkdir -p "$(dirname "$OUT")"
cp "$TMP_OUT" "$OUT"
OK=1
cad_log "wrote $OUT"

#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/cad-measure/common.sh"

PORT=5055
REPEATS=3
EVID_DIR="$CAD_REPO_ROOT/.planning/milestones/v1.16-phases/71-evaluation-gate/evidence"
OUT="$EVID_DIR/71-08-image-size.txt"

usage() { echo "usage: $0 [--port 5055] [--repeats 3] [--out FILE]" >&2; exit 64; }

while [ $# -gt 0 ]; do
  case "$1" in
    --port) [ $# -ge 2 ] || usage; PORT=$2; shift 2 ;;
    --repeats) [ $# -ge 2 ] || usage; REPEATS=$2; shift 2 ;;
    --out) [ $# -ge 2 ] || usage; OUT=$2; shift 2 ;;
    *) usage ;;
  esac
done
case "$PORT" in ''|*[!0-9]*) usage ;; esac
case "$REPEATS" in ''|*[!0-9]*) usage ;; esac
[ "$REPEATS" -ge 1 ] || usage

command -v docker >/dev/null || { echo "error: docker is required" >&2; exit 69; }
for tool in jq curl perl awk; do
  command -v "$tool" >/dev/null || { echo "error: $tool is required" >&2; exit 1; }
done

NET=octoconv-cad-pull
REG=octoconv-cad-registry
DIND=octoconv-cad-dind
REPO=octoconv-cad-measure
REG_IN_NET="$REG:5000"
LOCAL_REG="localhost:$PORT"

LIMITS="$EVID_DIR/71-06-limits.txt"
PIN=$(sed -n 's/^PIN_DECISION,choice=\([^,]*\),.*/\1/p' "$LIMITS")
[ -n "$PIN" ] || { echo "error: no PIN_DECISION in $LIMITS" >&2; exit 1; }

TMP_OUT=$(mktemp "${TMPDIR:-/tmp}/cad-image-size.XXXXXX")
WORK=$(mktemp -d "${TMPDIR:-/tmp}/cad-image-size-work.XXXXXX")
TAGGED=""

emit() { printf '%s\n' "$*" | tee -a "$TMP_OUT"; }

cleanup() {
  local t
  docker rm -fv "$DIND" >/dev/null 2>&1 || true
  docker rm -fv "$REG" >/dev/null 2>&1 || true
  docker network rm "$NET" >/dev/null 2>&1 || true
  for t in $TAGGED; do docker rmi "$t" >/dev/null 2>&1 || true; done
  rm -rf "$WORK" "$TMP_OUT"
}
trap cleanup EXIT

now() { perl -MTime::HiRes=time -e 'printf "%.3f\n", time'; }

mib() { awk -v b="$1" 'BEGIN { printf "%.1f", b / 1048576 }'; }

index_digest() {
  docker buildx imagetools inspect "$1" 2>/dev/null | awk '/^Digest:/ { print $2; exit }'
}

if [ -n "$(docker ps -aq --filter "name=^${REG}\$" --filter "name=^${DIND}\$")" ]; then
  echo "error: $REG or $DIND already exists" >&2
  exit 1
fi
if lsof -nP -iTCP:"$PORT" -sTCP:LISTEN >/dev/null 2>&1; then
  echo "error: port $PORT is in use" >&2
  exit 1
fi

PRUNED_TAG=$(cad_image_tag "$PIN" pruned)
FULL_TAG=$(cad_image_tag "$PIN" full)
VARIANTS="pruned full"
docker image inspect "$PRUNED_TAG" >/dev/null 2>&1 || { echo "error: missing image $PRUNED_TAG" >&2; exit 1; }
docker image inspect "$FULL_TAG" >/dev/null 2>&1 || { echo "error: missing image $FULL_TAG" >&2; exit 1; }
cad_require_native "$PRUNED_TAG"

variant_tag() {
  case "$1" in
    pruned) echo "$PRUNED_TAG" ;;
    full) echo "$FULL_TAG" ;;
  esac
}

REG_DIGEST=$(index_digest registry:2)
DIND_DIGEST=$(index_digest docker:dind)
case "$REG_DIGEST" in sha256:????????????????????????????????????????????????????????????????) ;; *) echo "error: cannot resolve registry:2 digest" >&2; exit 1 ;; esac
case "$DIND_DIGEST" in sha256:????????????????????????????????????????????????????????????????) ;; *) echo "error: cannot resolve docker:dind digest" >&2; exit 1 ;; esac
REG_IMAGE="registry@$REG_DIGEST"
DIND_IMAGE="docker@$DIND_DIGEST"

cad_evidence_header "$PRUNED_TAG" na na | tee -a "$TMP_OUT"
emit "HDR,pin=$PIN"
emit "HDR,script=scripts/cad-image-size-measure.sh,repeats=$REPEATS,port=$PORT"

docker network create "$NET" >/dev/null
docker run -d --name "$REG" --network "$NET" -p "127.0.0.1:$PORT:5000" "$REG_IMAGE" >/dev/null
ready=0
for _ in $(seq 1 30); do
  if curl -fsS -o /dev/null "http://127.0.0.1:$PORT/v2/" 2>/dev/null; then ready=1; break; fi
  sleep 1
done
[ "$ready" = 1 ] || { echo "error: registry did not come up" >&2; exit 1; }
emit "REGISTRY,image=$REG_IMAGE,port=$PORT"

for v in $VARIANTS; do
  src=$(variant_tag "$v")
  dst="$LOCAL_REG/$REPO:$v"
  docker tag "$src" "$dst"
  TAGGED="$TAGGED $dst"
  docker push "$dst" >&2
done

compressed_bytes_of=""
for v in $VARIANTS; do
  src=$(variant_tag "$v")
  mf="$WORK/manifest-$v.json"
  curl -fsS \
    -H 'Accept: application/vnd.oci.image.manifest.v1+json' \
    -H 'Accept: application/vnd.docker.distribution.manifest.v2+json' \
    "http://127.0.0.1:$PORT/v2/$REPO/manifests/$v" > "$mf"
  if [ "$(jq -r '.layers | length' "$mf")" = 0 ] || [ "$(jq -r '.layers | length' "$mf")" = null ]; then
    echo "error: manifest of $v has no layers" >&2
    exit 1
  fi
  sum=$(jq '[.layers[].size] | add' "$mf")
  nlayers=$(jq '.layers | length' "$mf")
  uncompressed=$(docker image inspect "$src" --format '{{.Size}}')
  image_id=$(docker image inspect "$src" --format '{{.Id}}')
  emit "IMAGE_SIZE,variant=$v,ocp=$PIN,image_id=$image_id,uncompressed_mib=$(mib "$uncompressed"),compressed_mib=$(mib "$sum"),layers=$nlayers,uncompressed_bytes=$uncompressed,compressed_bytes=$sum"
  jq -r '.layers[] | "\(.digest) \(.size)"' "$mf" | while read -r dg sz; do
    emit "LAYER,variant=$v,digest=$dg,compressed_bytes=$sz"
  done
  compressed_bytes_of="$compressed_bytes_of $v=$sum"
done

start_dind() {
  local ready=0
  # dockerd treats every registry except localhost as TLS-only unless it is listed here
  docker run -d --name "$DIND" --privileged --network "$NET" -v /var/lib/docker \
    --entrypoint dockerd "$DIND_IMAGE" \
    --host=unix:///var/run/docker.sock --insecure-registry "$REG_IN_NET" >/dev/null
  for _ in $(seq 1 60); do
    if docker exec "$DIND" docker info >/dev/null 2>&1; then ready=1; break; fi
    sleep 2
  done
  [ "$ready" = 1 ] || { echo "error: dind did not come up" >&2; exit 1; }
}

stop_dind() { docker rm -fv "$DIND" >/dev/null; }

dind_empty() { [ -z "$(docker exec "$DIND" docker images -aq)" ]; }

DIND_NOTED=0
note_dind() {
  local driver status
  driver=$(docker exec "$DIND" docker info --format '{{.Driver}}')
  status=$(docker exec "$DIND" docker info --format '{{.DriverStatus}}' | tr -d '[]' | tr ',' ';')
  emit "DIND,image=$DIND_IMAGE,network=$NET,privileged=1,volume=anonymous,fresh_daemon_per_pull=1"
  emit "DIND,storage_driver=$driver,driver_status=$status,docker=$(docker exec "$DIND" docker version --format '{{.Server.Version}}')"
  if [ "$driver" = vfs ]; then
    emit "DIND,note=vfs storage driver: pull time is not representative"
  fi
}

pull_once() {
  local v=$1 rep=$2 log="$WORK/pull-$1-$2.log" t0 t1 rc=0 wall ids fetched exists layers
  start_dind
  if [ "$DIND_NOTED" = 0 ]; then note_dind; DIND_NOTED=1; fi
  dind_empty || { echo "error: dind daemon is not empty before pull $v/$rep" >&2; exit 1; }
  t0=$(now)
  docker exec "$DIND" docker pull "$REG_IN_NET/$REPO:$v" > "$log" 2>&1 || rc=$?
  t1=$(now)
  [ "$rc" = 0 ] || { cat "$log" >&2; echo "error: pull $v/$rep failed rc=$rc" >&2; exit 1; }
  wall=$(awk -v a="$t0" -v b="$t1" 'BEGIN { printf "%.2f", b - a }')
  ids=$(grep -Eo '^[0-9a-f]{12}: (Pulling fs layer|Already exists|Pull complete)' "$log" | cut -d: -f1 | sort -u | wc -l | tr -d ' ')
  fetched=$(grep -Eo '^[0-9a-f]{12}: Pull complete' "$log" | cut -d: -f1 | sort -u | wc -l | tr -d ' ')
  exists=$(grep -Ec '^[0-9a-f]{12}: Already exists' "$log" || true)
  layers=$ids
  if [ "$exists" != 0 ]; then
    cat "$log" >&2
    echo "error: a layer was already present in the daemon: not a cold pull ($v/$rep)" >&2
    exit 1
  fi
  if [ "$layers" = 0 ] || [ "$fetched" != "$layers" ]; then
    cat "$log" >&2
    echo "error: pull $v/$rep: fetched=$fetched layers=$layers" >&2
    exit 1
  fi
  emit "COLD_PULL,variant=$v,rep=$rep,wall_s=$wall,layers=$layers,fetched=$fetched,already_exists=$exists,medium=loopback,measures=download+decompress+extract over loopback not network"
  echo "$wall" >> "$WORK/walls-$v"
  stop_dind
}

rank() {
  local file=$1 pct=$2
  sort -n "$file" | awk -v p="$pct" '{ a[NR] = $1 } END { r = int((NR * p + 99) / 100); if (r < 1) r = 1; print a[r] }'
}

dist_line() {
  local v=$1 f="$WORK/walls-$1"
  emit "COLD_PULL_DIST,variant=$v,n=$(wc -l < "$f" | tr -d ' '),wall_min=$(sort -n "$f" | head -n 1),wall_median=$(rank "$f" 50),wall_max=$(sort -n "$f" | tail -n 1)"
}

for rep in $(seq 1 "$REPEATS"); do
  pull_once pruned "$rep"
done
dist_line pruned
for v in $VARIANTS; do
  [ "$v" = pruned ] && continue
  pull_once "$v" 1
  dist_line "$v"
done

for v in $VARIANTS; do
  for kv in $compressed_bytes_of; do
    [ "${kv%%=*}" = "$v" ] || continue
    bytes=${kv#*=}
    for bw in 100 1000 10000; do
      emit "PULL_CALC,variant=$v,compressed_mib=$(mib "$bytes"),bandwidth_mbit=$bw,transfer_s=$(awk -v b="$bytes" -v w="$bw" 'BEGIN { printf "%.1f", b * 8 / (w * 1000000) }'),label=рассчитано,note=compressed bytes * 8 / bandwidth; node time is about transfer_s plus the unpack time of COLD_PULL"
    done
  done
done

fleet=$(cd "$CAD_REPO_ROOT" && docker compose config --images 2>/dev/null | sort -u || true)
if [ -z "$fleet" ]; then
  emit "FLEET,status=compose-images-unavailable"
else
  while IFS= read -r img; do
    [ -n "$img" ] || continue
    if sz=$(docker image inspect "$img" --format '{{.Size}}' 2>/dev/null); then
      emit "FLEET,image=$img,uncompressed_mib=$(mib "$sz"),note=as found on the host, not rebuilt"
    else
      emit "FLEET,image=$img,status=absent"
    fi
  done <<EOF
$fleet
EOF
fi

emit "D15,threshold=none,renderer_choice_unchanged=1,phase76_input=keda.cad.minReplicaCount"

mkdir -p "$(dirname "$OUT")"
cp "$TMP_OUT" "$OUT"
cad_log "wrote $OUT"

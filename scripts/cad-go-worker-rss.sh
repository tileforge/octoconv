#!/usr/bin/env bash
# cad-go-worker-rss.sh -- Phase 71 (EVAL-03, D-07) resident memory of a live Go
# worker process, read from /proc/1/status inside the compose worker container:
# VmRSS idle, then VmHWM after one real image job (done).
#
# The image worker is an analogue of the future cad-worker (same process() and
# engineExec shape); the line says so. A temporary API client is created for the
# run, its key is never printed or written, and it is revoked afterwards. The
# stack is stopped, never removed: no `down`, no volume deleted.
#
# Usage:
#   scripts/cad-go-worker-rss.sh [--out FILE] [--timeout-s 300]
#
# Exit codes: 64 usage, 69 docker missing, 1 precondition or measurement failure.

set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd -P)"
cd "$REPO_ROOT"

OUT=go-worker-rss.txt
TIMEOUT_S=300
FIXTURE=scripts/fixtures/quota-burst-heavy.png
API=http://localhost:8090

while [ $# -gt 0 ]; do
  case "$1" in
    --out) OUT=${2:?}; shift 2 ;;
    --timeout-s) TIMEOUT_S=${2:?}; shift 2 ;;
    -h|--help) sed -n '2,/^$/p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//' >&2; exit 64 ;;
    *) echo "error: unknown option $1" >&2; exit 64 ;;
  esac
done

command -v docker >/dev/null 2>&1 || { echo "error: docker is required" >&2; exit 69; }
[ -f "$FIXTURE" ] || { echo "error: $FIXTURE is missing" >&2; exit 1; }

if [ -n "$(docker compose ps -q 2>/dev/null)" ]; then
  echo "error: the compose stack already has containers; stop it first, this script does not touch a running stack" >&2
  exit 1
fi

: > "$OUT"
emit() { printf '%s\n' "$*" | tee -a "$OUT"; }

vm_kib() {
  docker compose exec -T worker cat /proc/1/status | tr -d '\r' | awk -v k="$1:" '$1 == k { print $2 }'
}
mib() { awk -v k="$1" 'BEGIN { printf "%.1f", k / 1024 }'; }

CLIENT_ID=""
KEY=""
SALT=""
cleanup() {
  local rc=$?
  if [ -n "$CLIENT_ID" ] && [ -n "$SALT" ]; then
    DATABASE_URL=postgres://octo:octo-pass@localhost:5434/octo_db API_KEY_SALT=$SALT \
      go run ./cmd/manage-clients revoke "$CLIENT_ID" primary >/dev/null 2>&1 \
      && echo "key revoked" >&2 || echo "warning: revoke failed for the probe client" >&2
  fi
  docker compose stop >/dev/null 2>&1 || true
  exit "$rc"
}
trap cleanup EXIT

docker compose up -d --wait postgres redis minio
docker compose run --rm createbucket >/dev/null
docker compose up -d --wait api worker

emit "HDR,date_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
emit "HDR,server_arch=$(docker version --format '{{.Server.Arch}}')"
emit "HDR,worker_image_id=$(docker inspect "$(docker compose ps -q worker)" --format '{{.Image}}')"
emit "HDR,worker_image_created=$(docker image inspect "$(docker inspect "$(docker compose ps -q worker)" --format '{{.Image}}')" --format '{{.Created}}')"
emit "HDR,worker_concurrency=$(docker compose exec -T worker printenv WORKER_CONCURRENCY | tr -d '\r')"
emit "HDR,host_noise=$(docker ps -q | wc -l | tr -d ' ')"
emit "HDR,fixture_bytes=$(wc -c < "$FIXTURE" | tr -d ' ')"

sleep 5
IDLE_RSS=$(vm_kib VmRSS)
IDLE_HWM=$(vm_kib VmHWM)
[ -n "$IDLE_RSS" ] && [ -n "$IDLE_HWM" ] || { echo "error: could not read /proc/1/status of the worker" >&2; exit 1; }
emit "IDLE,pid1_comm=$(docker compose exec -T worker cat /proc/1/comm | tr -d '\r'),vmrss_mib=$(mib "$IDLE_RSS"),vmhwm_mib=$(mib "$IDLE_HWM")"

SALT=$(docker compose exec -T api printenv API_KEY_SALT | tr -d '\r\n')
[ -n "$SALT" ] || { echo "error: could not read the API key pepper from the api container" >&2; exit 1; }

CREATED=$(DATABASE_URL=postgres://octo:octo-pass@localhost:5434/octo_db API_KEY_SALT=$SALT \
  go run ./cmd/manage-clients create cad-rss-probe)
CLIENT_ID=$(printf '%s\n' "$CREATED" | awk '/^client id:/ { print $3 }')
KEY=$(printf '%s\n' "$CREATED" | awk '/^api key/ { print $NF }')
CREATED=""
[ -n "$CLIENT_ID" ] && [ -n "$KEY" ] || { echo "error: manage-clients create did not return an id and a key" >&2; exit 1; }

RESP=$(curl -sS -H @<(printf 'Authorization: ApiKey %s\n' "$KEY") \
  -F "file=@$FIXTURE" -F target=webp "$API/v1/jobs")
JOB_ID=$(printf '%s' "$RESP" | sed -n 's/.*"job_id":"\([^"]*\)".*/\1/p')
[ -n "$JOB_ID" ] || { echo "error: POST /v1/jobs returned no job_id" >&2; exit 1; }

STATUS=""
START=$(date +%s)
while [ $(( $(date +%s) - START )) -lt "$TIMEOUT_S" ]; do
  BODY=$(curl -sS -H @<(printf 'Authorization: ApiKey %s\n' "$KEY") "$API/v1/jobs/$JOB_ID")
  STATUS=$(printf '%s' "$BODY" | sed -n 's/.*"status":"\([^"]*\)".*/\1/p')
  case "$STATUS" in done|failed) break ;; esac
  sleep 1
done
JOB_S=$(( $(date +%s) - START ))
KEY=""

AFTER_RSS=$(vm_kib VmRSS)
AFTER_HWM=$(vm_kib VmHWM)
emit "JOB,status=${STATUS:-timeout},seconds=$JOB_S,target=webp"
[ "$STATUS" = "done" ] || { echo "error: job ended as '${STATUS:-timeout}'" >&2; exit 1; }

emit "GO_WORKER,service=worker,vmrss_idle_mib=$(mib "$IDLE_RSS"),vmhwm_after_job_mib=$(mib "$AFTER_HWM"),job_status=done,vmrss_after_job_mib=$(mib "$AFTER_RSS"),analogue=image-class_Go_worker_not_cad-worker"
